package invocation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/mcpupstream"
	"github.com/valon-technologies/gestalt/server/internal/metricutil"
	"github.com/valon-technologies/gestalt/server/internal/paraminterp"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/registry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/singleflight"
)

const (
	tokenRefreshThreshold = 5 * time.Minute
	tracerName            = "gestaltd"

	attrProvider       = attribute.Key("gestalt.provider")
	attrOperation      = attribute.Key("gestalt.operation")
	attrTransport      = attribute.Key("gestalt.transport")
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
	users          *coredata.UserService
	tokens         *coredata.TokenService
	connMapper     ConnectionMapper
	mcpMapper      ConnectionMapper
	connectionAuth RefresherResolver
	refreshGroup   singleflight.Group
}

type BrokerOption func(*Broker)

func WithConnectionMapper(m ConnectionMapper) BrokerOption {
	return func(b *Broker) { b.connMapper = m }
}

func WithMCPConnectionMapper(m ConnectionMapper) BrokerOption {
	return func(b *Broker) { b.mcpMapper = m }
}

func WithConnectionAuth(r RefresherResolver) BrokerOption {
	return func(b *Broker) { b.connectionAuth = r }
}

func NewBroker(providers *registry.PluginMap[core.Provider], users *coredata.UserService, tokens *coredata.TokenService, opts ...BrokerOption) *Broker {
	b := &Broker{providers: providers, users: users, tokens: tokens}
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

func (b *Broker) Invoke(ctx context.Context, p *principal.Principal, providerName, instance, operation string, params map[string]any) (_ *core.OperationResult, err error) {
	startedAt := time.Now()
	metricProvider := metricutil.UnknownAttrValue
	metricOperation := metricutil.UnknownAttrValue
	metricTransport := metricutil.UnknownAttrValue
	metricConnectionMode := metricutil.UnknownAttrValue

	ctx, span := otel.Tracer(tracerName).Start(ctx, "broker.invoke",
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	defer span.End()
	defer func() {
		recordOperationMetrics(
			ctx,
			startedAt,
			metricProvider,
			metricOperation,
			metricTransport,
			metricConnectionMode,
			err != nil,
		)
	}()

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

	metricProvider = providerName
	metricConnectionMode = metricutil.NormalizeConnectionMode(prov.ConnectionMode())
	span.SetAttributes(attrConnectionMode.String(metricConnectionMode))

	if p == nil {
		return fail(ErrNotAuthenticated)
	}

	if p.UserID != "" {
		span.SetAttributes(attrUserID.String(p.UserID))
	}

	if p.Scopes != nil && !slices.Contains(p.Scopes, providerName) {
		return fail(fmt.Errorf("%w: %s", ErrScopeDenied, providerName))
	}

	conn := ConnectionFromContext(ctx)

	transport, err := b.resolveTransport(ctx, p, prov, providerName, operation, conn, instance)
	if err != nil {
		return fail(err)
	}
	metricOperation = operation
	metricTransport = metricutil.AttrValue(transport)
	span.SetAttributes(attrTransport.String(metricTransport))

	if transport == catalog.TransportMCPPassthrough && InvocationSurfaceFromContext(ctx) == InvocationSurfaceHTTP {
		return fail(core.ErrMCPOnly)
	}

	if conn == "" {
		if transport == catalog.TransportMCPPassthrough {
			conn = b.mcpConnection(providerName)
		} else if ocp, ok := prov.(core.OperationConnectionProvider); ok {
			conn = ocp.ConnectionForOperation(operation)
		}
	}
	if conn == "" && b.connMapper != nil {
		conn = b.connMapper.ConnectionForProvider(providerName)
	}

	if transport == catalog.TransportMCPPassthrough {
		result, err := CallDirectTool(ctx, b, p, prov, providerName, operation, conn, instance, params, mcpupstream.CallToolMetaFromContext(ctx))
		if err != nil {
			return fail(err)
		}
		opResult, err := toolResultToOperationResult(result)
		if err != nil {
			return fail(fmt.Errorf("%w: converting tool result: %v", ErrInternal, err))
		}
		if result != nil {
			opResult.MCPResult = result
		}
		return opResult, nil
	}

	ctx, accessToken, err := b.resolveToken(ctx, prov, p, providerName, conn, instance)
	if err != nil {
		return fail(err)
	}

	result, err := prov.Execute(ctx, operation, params, accessToken)
	if err != nil {
		return fail(err)
	}

	return result, nil
}

func (b *Broker) resolveTransport(ctx context.Context, p *principal.Principal, prov core.Provider, providerName, operation, connection, instance string) (string, error) {
	if transport, ok := CatalogOperationTransport(providerCatalog(prov), operation); ok {
		return transport, nil
	}

	if _, ok := prov.(core.SessionCatalogProvider); ok {
		if connection == "" {
			connection = b.mcpConnection(providerName)
		}
		cat, err := resolveSessionCatalog(ctx, prov, providerName, b, p, connection, instance)
		if err != nil {
			return "", err
		}
		if transport, ok := CatalogOperationTransport(cat, operation); ok {
			return transport, nil
		}
	}

	return "", fmt.Errorf("%w: %q on provider %q", ErrOperationNotFound, operation, providerName)
}

func (b *Broker) mcpConnection(providerName string) string {
	if b.mcpMapper != nil {
		if conn := b.mcpMapper.ConnectionForProvider(providerName); conn != "" {
			return conn
		}
	}
	if b.connMapper != nil {
		return b.connMapper.ConnectionForProvider(providerName)
	}
	return ""
}

func toolResultToOperationResult(result *mcpgo.CallToolResult) (*core.OperationResult, error) {
	headers := http.Header{}
	headers.Set("Content-Type", "application/json")

	if result == nil {
		return &core.OperationResult{Status: http.StatusOK, Headers: headers, Body: `{}`}, nil
	}

	if result.IsError {
		return &core.OperationResult{
			Status:  http.StatusBadGateway,
			Headers: headers,
			Body:    `{"error":"operation failed"}`,
		}, nil
	}

	body, err := toolResultBody(result)
	if err != nil {
		return nil, err
	}

	return &core.OperationResult{Status: http.StatusOK, Headers: headers, Body: body}, nil
}

func toolResultBody(result *mcpgo.CallToolResult) (string, error) {
	if result.StructuredContent != nil {
		body, err := json.Marshal(result.StructuredContent)
		if err != nil {
			return "", err
		}
		return string(body), nil
	}

	if len(result.Content) == 1 {
		if text, ok := mcpgo.AsTextContent(result.Content[0]); ok && json.Valid([]byte(strings.TrimSpace(text.Text))) {
			return text.Text, nil
		}
	}

	body, err := json.Marshal(map[string]any{"content": result.Content})
	if err != nil {
		return "", err
	}
	return string(body), nil
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
			dbUser, err := b.users.FindOrCreateUser(ctx, p.Identity.Email)
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
		storedToken, err = b.tokens.Token(ctx, userID, providerName, connection, instance)
		if err != nil {
			if errors.Is(err, core.ErrNotFound) {
				return ctx, "", fmt.Errorf("%w: no token stored for integration %q instance %q", ErrNoToken, providerName, instance)
			}
			return ctx, "", fmt.Errorf("%w: retrieving integration token: %v", ErrInternal, err)
		}
	} else {
		tokens, listErr := b.tokens.ListTokensForConnection(ctx, userID, providerName, connection)
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
			return ctx, "", fmt.Errorf("%w: integration %q has %d connections (%v); specify which instance to use with the %q parameter",
				ErrAmbiguousInstance, providerName, len(tokens), instances, "_instance")
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

	accessToken, err := b.refreshTokenIfNeeded(ctx, storedToken, providerName, connection, metricutil.NormalizeConnectionMode(prov.ConnectionMode()))
	return ctx, accessToken, err
}

func (b *Broker) refreshTokenIfNeeded(ctx context.Context, token *core.IntegrationToken, providerName, connection, connectionMode string) (string, error) {
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

	key := token.UserID + ":" + providerName + ":" + connection + ":" + token.Instance
	v, err, _ := b.refreshGroup.Do(key, func() (any, error) {
		refreshCtx := context.WithoutCancel(ctx)
		startedAt := time.Now()
		resp, err := b.refreshOAuth(refreshCtx, refresher, token.RefreshToken)
		metricutil.RecordConnectionAuthMetrics(refreshCtx, startedAt, providerName, "oauth", "refresh", connectionMode, err != nil)
		return resp, err
	})
	if err != nil {
		fresh, fetchErr := b.tokens.Token(ctx, token.UserID, token.Integration, token.Connection, token.Instance)
		if fetchErr == nil && fresh != nil && fresh.AccessToken != token.AccessToken {
			return fresh.AccessToken, nil
		}
		token.RefreshErrorCount++
		token.UpdatedAt = time.Now()
		if storeErr := b.tokens.StoreToken(ctx, token); storeErr != nil {
			slog.WarnContext(ctx, "failed to persist refresh error count", "provider", providerName, "error", storeErr)
		}
		if time.Now().Before(*token.ExpiresAt) {
			return token.AccessToken, nil
		}
		return "", fmt.Errorf("token expired and refresh failed: %w", err)
	}

	resp := v.(*core.TokenResponse)
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

	if err := b.tokens.StoreToken(ctx, token); err != nil {
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
