package invocation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/egress"
	"github.com/valon-technologies/gestalt/server/internal/paraminterp"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/registry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	tokenRefreshThreshold = 5 * time.Minute
	tracerName            = "gestaltd"

	attrProvider       = attribute.Key("gestalt.provider")
	attrOperation      = attribute.Key("gestalt.operation")
	attrUserID         = attribute.Key("gestalt.user_id")
	attrConnectionMode = attribute.Key("gestalt.connection_mode")
)

type connectionCtxKey struct{}

func WithConnection(ctx context.Context, connection string) context.Context {
	return context.WithValue(ctx, connectionCtxKey{}, connection)
}

func ConnectionFromContext(ctx context.Context) string {
	v, _ := ctx.Value(connectionCtxKey{}).(string)
	return v
}

type Invoker interface {
	Invoke(ctx context.Context, p *principal.Principal, providerName, instance, operation string, params map[string]any) (*core.OperationResult, error)
}

type CapabilityLister interface {
	ListCapabilities() []core.Capability
}

var (
	_ Invoker          = (*Broker)(nil)
	_ CapabilityLister = (*Broker)(nil)
)

type ConnectionMapper interface {
	ConnectionForProvider(provider string) string
}

type ConnectionMap map[string]string

func (m ConnectionMap) ConnectionForProvider(provider string) string {
	return m[provider]
}

// OAuthRefresher is the subset of OAuthHandler that the broker needs for
// token refresh. Defined here to avoid importing the bootstrap package.
type OAuthRefresher interface {
	RefreshToken(ctx context.Context, refreshToken string) (*core.TokenResponse, error)
	RefreshTokenWithURL(ctx context.Context, refreshToken, tokenURL string) (*core.TokenResponse, error)
	TokenURL() string
}

// RefresherResolver returns the connection auth map, blocking until providers
// finish loading if needed.
type RefresherResolver func() map[string]map[string]OAuthRefresher

type Broker struct {
	providers      *registry.PluginMap[core.Provider]
	datastore      core.Datastore
	connMapper     ConnectionMapper
	connectionAuth RefresherResolver
}

type BrokerOption func(*Broker)

func WithConnectionMapper(m ConnectionMapper) BrokerOption {
	return func(b *Broker) { b.connMapper = m }
}

func WithConnectionAuth(r RefresherResolver) BrokerOption {
	return func(b *Broker) { b.connectionAuth = r }
}

func NewBroker(providers *registry.PluginMap[core.Provider], ds core.Datastore, opts ...BrokerOption) *Broker {
	b := &Broker{providers: providers, datastore: ds}
	for _, o := range opts {
		o(b)
	}
	return b
}

func (b *Broker) ListProviders() []string {
	return b.providers.List()
}

func (b *Broker) ListCapabilities() []core.Capability {
	if b == nil || b.providers == nil {
		return nil
	}

	var caps []core.Capability
	for _, name := range b.providers.List() {
		prov, err := b.providers.Get(name)
		if err != nil {
			continue
		}
		caps = append(caps, capabilitiesForProvider(name, prov)...)
	}
	return caps
}

func (b *Broker) Invoke(ctx context.Context, p *principal.Principal, providerName, instance, operation string, params map[string]any) (*core.OperationResult, error) {
	ctx, span := otel.Tracer(tracerName).Start(ctx, "broker.invoke",
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	defer span.End()

	span.SetAttributes(
		attrProvider.String(providerName),
		attrOperation.String(operation),
	)

	fail := func(err error) (*core.OperationResult, error) {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	prov, err := b.providers.Get(providerName)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			err = fmt.Errorf("%w: %q", ErrProviderNotFound, providerName)
		} else {
			err = fmt.Errorf("%w: looking up provider: %v", ErrInternal, err)
		}
		return fail(err)
	}

	span.SetAttributes(attrConnectionMode.String(string(prov.ConnectionMode())))

	if p == nil {
		return fail(ErrNotAuthenticated)
	}

	if p.UserID != "" {
		span.SetAttributes(attrUserID.String(p.UserID))
	}

	if p.Scopes != nil && !slices.Contains(p.Scopes, providerName) {
		return fail(fmt.Errorf("%w: %s", ErrScopeDenied, providerName))
	}

	ops := prov.ListOperations()
	found := false
	for _, op := range ops {
		if op.Name == operation {
			found = true
			break
		}
	}
	if !found {
		return fail(fmt.Errorf("%w: %q on provider %q", ErrOperationNotFound, operation, providerName))
	}

	conn := ConnectionFromContext(ctx)
	if conn == "" && b.connMapper != nil {
		conn = b.connMapper.ConnectionForProvider(providerName)
	}

	ctx, accessToken, err := b.resolveToken(ctx, prov, p, providerName, conn, instance)
	if err != nil {
		return fail(err)
	}

	ctx = b.withSubject(ctx, p)

	result, err := prov.Execute(ctx, operation, params, accessToken)
	if err != nil {
		return fail(err)
	}

	return result, nil
}

func (b *Broker) ResolveToken(ctx context.Context, p *principal.Principal, providerName, connection, instance string) (string, error) {
	if p != nil && p.Scopes != nil && !slices.Contains(p.Scopes, providerName) {
		return "", fmt.Errorf("%w: %s", ErrScopeDenied, providerName)
	}
	prov, err := b.providers.Get(providerName)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return "", fmt.Errorf("%w: %q", ErrProviderNotFound, providerName)
		}
		return "", fmt.Errorf("%w: looking up provider: %v", ErrInternal, err)
	}
	_, tok, resolveErr := b.resolveToken(ctx, prov, p, providerName, connection, instance)
	return tok, resolveErr
}

func (b *Broker) withSubject(ctx context.Context, p *principal.Principal) context.Context {
	return egress.WithSubjectFromPrincipal(ctx, p)
}

func (b *Broker) resolveToken(ctx context.Context, prov core.Provider, p *principal.Principal, providerName, connection, instance string) (context.Context, string, error) {
	mode := prov.ConnectionMode()
	switch mode {
	case core.ConnectionModeNone:
		return ctx, "", nil

	case core.ConnectionModeUser, "":
		if p.UserID == "" {
			if p.Identity == nil || p.Identity.Email == "" {
				return ctx, "", fmt.Errorf("%w: principal has no user ID or email", ErrUserResolution)
			}
			dbUser, err := b.datastore.FindOrCreateUser(ctx, p.Identity.Email)
			if err != nil {
				return ctx, "", fmt.Errorf("%w: %v", ErrUserResolution, err)
			}
			if dbUser == nil || dbUser.ID == "" {
				return ctx, "", fmt.Errorf("%w: no user record returned", ErrUserResolution)
			}
			p.UserID = dbUser.ID
		}
		return b.resolveUserToken(ctx, prov, p.UserID, providerName, connection, instance)

	case core.ConnectionModeIdentity:
		return b.resolveUserToken(ctx, prov, principal.IdentityPrincipal, providerName, connection, instance)

	case core.ConnectionModeEither:
		if p.UserID != "" {
			enrichedCtx, tok, err := b.resolveUserToken(ctx, prov, p.UserID, providerName, connection, instance)
			if err == nil {
				return enrichedCtx, tok, nil
			}
			if errors.Is(err, ErrAmbiguousInstance) {
				return ctx, "", err
			}
			if !errors.Is(err, ErrNoToken) {
				return ctx, "", err
			}
		}
		return b.resolveUserToken(ctx, prov, principal.IdentityPrincipal, providerName, connection, instance)

	default:
		return ctx, "", fmt.Errorf("%w: unknown connection mode %q", ErrInternal, mode)
	}
}

func (b *Broker) resolveUserToken(ctx context.Context, prov core.Provider, userID, providerName, connection, instance string) (context.Context, string, error) {
	var storedToken *core.IntegrationToken
	var err error

	if instance != "" {
		storedToken, err = b.datastore.Token(ctx, userID, providerName, connection, instance)
		if err != nil {
			if errors.Is(err, core.ErrNotFound) {
				return ctx, "", fmt.Errorf("%w: no token stored for integration %q instance %q", ErrNoToken, providerName, instance)
			}
			return ctx, "", fmt.Errorf("%w: retrieving integration token: %v", ErrInternal, err)
		}
	} else {
		tokens, listErr := b.datastore.ListTokensForConnection(ctx, userID, providerName, connection)
		if listErr != nil {
			return ctx, "", fmt.Errorf("%w: listing tokens: %v", ErrInternal, listErr)
		}
		switch len(tokens) {
		case 0:
			return ctx, "", fmt.Errorf("%w: no token stored for integration %q", ErrNoToken, providerName)
		case 1:
			storedToken = tokens[0]
		default:
			instances := make([]string, len(tokens))
			for i, t := range tokens {
				instances[i] = t.Instance
			}
			return ctx, "", fmt.Errorf("%w: integration %q has %d connections (%v); specify which instance to use",
				ErrAmbiguousInstance, providerName, len(tokens), instances)
		}
	}

	if storedToken == nil {
		return ctx, "", fmt.Errorf("%w: no token stored for integration %q", ErrNoToken, providerName)
	}

	if storedToken.MetadataJSON != "" {
		var connParams map[string]string
		if err := json.Unmarshal([]byte(storedToken.MetadataJSON), &connParams); err != nil {
			slog.WarnContext(ctx, "malformed metadata JSON", "provider", providerName, "error", err)
		} else if len(connParams) > 0 {
			ctx = core.WithConnectionParams(ctx, connParams)
		}
	}

	accessToken, err := b.refreshTokenIfNeeded(ctx, storedToken, providerName, connection)
	return ctx, accessToken, err
}

func (b *Broker) refreshTokenIfNeeded(ctx context.Context, token *core.IntegrationToken, providerName, connection string) (string, error) {
	if token.RefreshToken == "" || token.ExpiresAt == nil {
		return token.AccessToken, nil
	}
	if time.Until(*token.ExpiresAt) > tokenRefreshThreshold {
		return token.AccessToken, nil
	}

	refresher := b.resolveRefresher(providerName, connection)
	if refresher == nil {
		return token.AccessToken, nil
	}

	resp, err := b.refreshOAuth(ctx, refresher, token.RefreshToken)
	if err != nil {
		fresh, fetchErr := b.datastore.Token(ctx, token.UserID, token.Integration, token.Connection, token.Instance)
		if fetchErr == nil && fresh != nil && fresh.AccessToken != token.AccessToken {
			return fresh.AccessToken, nil
		}
		token.RefreshErrorCount++
		token.UpdatedAt = time.Now()
		if storeErr := b.datastore.StoreToken(ctx, token); storeErr != nil {
			slog.WarnContext(ctx, "failed to persist refresh error count", "provider", providerName, "error", storeErr)
		}
		if time.Now().Before(*token.ExpiresAt) {
			return token.AccessToken, nil
		}
		return "", fmt.Errorf("token expired and refresh failed: %w", err)
	}

	now := time.Now()
	token.AccessToken = resp.AccessToken
	if resp.RefreshToken != "" {
		token.RefreshToken = resp.RefreshToken
	}
	if resp.ExpiresIn > 0 {
		t := now.Add(time.Duration(resp.ExpiresIn) * time.Second)
		token.ExpiresAt = &t
	} else {
		token.ExpiresAt = nil
	}
	token.LastRefreshedAt = &now
	token.RefreshErrorCount = 0
	token.UpdatedAt = now

	if err := b.datastore.StoreToken(ctx, token); err != nil {
		return "", fmt.Errorf("persisting refreshed token: %w", err)
	}
	return token.AccessToken, nil
}

func (b *Broker) resolveRefresher(integration, connection string) OAuthRefresher {
	if b.connectionAuth == nil {
		return nil
	}
	m := b.connectionAuth()
	if m == nil {
		return nil
	}
	return m[integration][connection]
}

func (b *Broker) refreshOAuth(ctx context.Context, refresher OAuthRefresher, refreshToken string) (*core.TokenResponse, error) {
	if cp := core.ConnectionParams(ctx); cp != nil {
		raw := refresher.TokenURL()
		resolved := paraminterp.Interpolate(raw, cp)
		if resolved != raw {
			return refresher.RefreshTokenWithURL(ctx, refreshToken, resolved)
		}
	}
	return refresher.RefreshToken(ctx, refreshToken)
}
