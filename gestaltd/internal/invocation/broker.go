package invocation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/authorization"
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

func withResolvedPrincipal(ctx context.Context, p *principal.Principal) context.Context {
	if p == nil {
		return ctx
	}
	return principal.WithPrincipal(ctx, p)
}

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
	providers      *registry.ProviderMap[core.Provider]
	users          *coredata.UserService
	tokens         *coredata.TokenService
	authorizer     authorization.RuntimeAuthorizer
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

func WithAuthorizer(a authorization.RuntimeAuthorizer) BrokerOption {
	return func(b *Broker) { b.authorizer = a }
}

func NewBroker(providers *registry.ProviderMap[core.Provider], users *coredata.UserService, tokens *coredata.TokenService, opts ...BrokerOption) *Broker {
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

	if !principal.AllowsProviderPermission(p, providerName) {
		return fail(fmt.Errorf("%w: %s", ErrScopeDenied, providerName))
	}
	if err := b.resolveUserPrincipal(ctx, p); err != nil {
		return fail(err)
	}
	ctx = withResolvedPrincipal(ctx, p)
	if p.UserID != "" {
		span.SetAttributes(attrUserID.String(p.UserID))
	}
	if b.authorizer != nil {
		access, allowed := b.authorizer.ResolveAccess(ctx, p, providerName)
		if access.Policy != "" || access.Role != "" {
			ctx = WithAccessContext(ctx, access)
		}
		if p != nil && !p.HasUserContext() {
			if binding, ok := b.authorizer.Binding(p, providerName); ok {
				SetCredentialAudit(ctx, binding.Mode, binding.CredentialSubjectID, binding.Connection, binding.Instance)
			}
			if !b.authorizer.AllowOperation(ctx, p, providerName, operation) {
				return fail(fmt.Errorf("%w: %s.%s", ErrAuthorizationDenied, providerName, operation))
			}
		} else if !allowed {
			return fail(fmt.Errorf("%w: %s", ErrAuthorizationDenied, providerName))
		}
	}

	conn := ConnectionFromContext(ctx)
	conn, instance = b.identityTokenSelectors(p, providerName, conn, instance)

	opMeta, transport, resolvedConnection, err := b.resolveOperation(ctx, p, prov, providerName, operation, conn, instance)
	if err != nil {
		return fail(err)
	}
	if b.authorizer != nil && !b.authorizer.AllowCatalogOperation(ctx, p, providerName, opMeta) {
		return fail(fmt.Errorf("%w: %s.%s", ErrAuthorizationDenied, providerName, operation))
	}
	if !principal.AllowsOperationPermission(p, providerName, opMeta.ID) {
		return fail(fmt.Errorf("%w: %s.%s", ErrScopeDenied, providerName, opMeta.ID))
	}
	metricOperation = operation
	metricTransport = metricutil.AttrValue(transport)
	span.SetAttributes(attrTransport.String(metricTransport))

	if transport == catalog.TransportMCPPassthrough && InvocationSurfaceFromContext(ctx) == InvocationSurfaceHTTP {
		return fail(core.ErrMCPOnly)
	}

	if conn == "" {
		conn = resolvedConnection
	}
	if conn == "" {
		if transport == catalog.TransportMCPPassthrough {
			conn = b.mcpConnection(providerName)
		} else {
			conn = prov.ConnectionForOperation(operation)
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

func (b *Broker) resolveOperation(ctx context.Context, p *principal.Principal, prov core.Provider, providerName, operation, connection, instance string) (catalog.CatalogOperation, string, string, error) {
	sessionConnections := []string{connection}
	if connection == "" {
		sessionConnections = nil
		if mcpConnection := b.mcpConnection(providerName); mcpConnection != "" {
			sessionConnections = []string{mcpConnection}
		}
	}

	return ResolveOperation(ctx, prov, providerName, b, p, operation, sessionConnections, instance)
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

func (b *Broker) MCPConnection(providerName string) string {
	return b.mcpConnection(providerName)
}

func (b *Broker) identityTokenSelectors(p *principal.Principal, providerName, connection, instance string) (string, string) {
	if b.authorizer == nil || p == nil || p.HasUserContext() {
		return connection, instance
	}
	binding, ok := b.authorizer.Binding(p, providerName)
	if !ok {
		return connection, instance
	}
	if connection == "" {
		connection = binding.Connection
	}
	if instance == "" {
		instance = binding.Instance
	}
	return connection, instance
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

func (b *Broker) ResolveToken(ctx context.Context, p *principal.Principal, providerName, connection, instance string) (context.Context, string, error) {
	if !principal.AllowsProviderPermission(p, providerName) {
		return ctx, "", fmt.Errorf("%w: %s", ErrScopeDenied, providerName)
	}
	if err := b.resolveUserPrincipal(ctx, p); err != nil {
		return ctx, "", err
	}
	ctx = withResolvedPrincipal(ctx, p)
	if b.authorizer != nil && !b.authorizer.AllowProvider(ctx, p, providerName) {
		return ctx, "", fmt.Errorf("%w: %s", ErrAuthorizationDenied, providerName)
	}
	prov, err := b.providers.Get(providerName)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return ctx, "", fmt.Errorf("%w: %q", ErrProviderNotFound, providerName)
		}
		return ctx, "", fmt.Errorf("%w: looking up provider: %v", ErrInternal, err)
	}
	return b.resolveToken(ctx, prov, p, providerName, connection, instance)
}

func (b *Broker) resolveToken(ctx context.Context, prov core.Provider, p *principal.Principal, providerName, connection, instance string) (context.Context, string, error) {
	resolved := principal.FromContext(ctx)
	if resolved != nil {
		p = resolved
	}
	if b.authorizer != nil && p != nil && !p.HasUserContext() {
		if binding, ok := b.authorizer.Binding(p, providerName); ok {
			return b.resolveIdentityBinding(ctx, prov, providerName, connection, instance, binding)
		}
	}
	if resolved == nil {
		if err := b.resolveUserPrincipal(ctx, p); err != nil {
			return ctx, "", err
		}
		ctx = withResolvedPrincipal(ctx, p)
	}

	mode := prov.ConnectionMode()
	switch mode {
	case core.ConnectionModeNone:
		SetCredentialAudit(ctx, core.ConnectionModeNone, "", "", "")
		ctx = WithCredentialContext(ctx, CredentialContext{Mode: core.ConnectionModeNone})
		return ctx, "", nil

	case core.ConnectionModeUser, "":
		if p.UserID == "" {
			return ctx, "", fmt.Errorf("%w: principal has no user ID or email", ErrUserResolution)
		}
		return b.resolveUserToken(ctx, prov, p.UserID, providerName, connection, instance, core.ConnectionModeUser, principal.UserSubjectID(p.UserID))

	case core.ConnectionModeIdentity:
		identityID := strings.TrimSpace(p.UserID)
		if identityID == "" {
			return ctx, "", fmt.Errorf("%w: principal has no user ID or email", ErrUserResolution)
		}
		if b.users != nil {
			canonicalIdentityID, err := b.users.CanonicalIdentityIDForUser(ctx, identityID)
			if err != nil {
				return ctx, "", fmt.Errorf("%w: resolving identity owner: %v", ErrUserResolution, err)
			}
			identityID = canonicalIdentityID
		}
		return b.resolveIdentityToken(ctx, prov, identityID, providerName, connection, instance, core.ConnectionModeIdentity, principal.IdentitySubjectID(identityID))

	case core.ConnectionMode("either"):
		return ctx, "", fmt.Errorf("%w: unsupported connection mode %q", ErrInternal, mode)

	default:
		return ctx, "", fmt.Errorf("%w: unknown connection mode %q", ErrInternal, mode)
	}
}

func (b *Broker) resolveUserPrincipal(ctx context.Context, p *principal.Principal) error {
	if p == nil || p.UserID != "" || !p.HasUserContext() || p.Identity == nil || p.Identity.Email == "" {
		return nil
	}
	if b.users == nil {
		return fmt.Errorf("%w: no user service configured", ErrUserResolution)
	}
	dbUser, err := b.users.FindOrCreateUser(ctx, p.Identity.Email)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUserResolution, err)
	}
	if dbUser == nil || dbUser.ID == "" {
		return fmt.Errorf("%w: no user record returned", ErrUserResolution)
	}
	p.UserID = dbUser.ID
	if p.Kind == "" {
		p.Kind = principal.KindUser
	}
	if p.SubjectID == "" {
		p.SubjectID = principal.UserSubjectID(dbUser.ID)
	}
	if p.Identity != nil && p.Identity.DisplayName == "" {
		p.Identity.DisplayName = dbUser.DisplayName
	}
	return nil
}

func (b *Broker) resolveIdentityBinding(ctx context.Context, prov core.Provider, providerName, requestedConnection, requestedInstance string, binding authorization.CredentialBinding) (context.Context, string, error) {
	switch binding.Mode {
	case core.ConnectionModeNone:
		if requestedConnection != "" || requestedInstance != "" {
			return ctx, "", fmt.Errorf("%w: identity-token callers may not override connection or instance bindings", ErrAuthorizationDenied)
		}
		SetCredentialAudit(ctx, binding.Mode, binding.CredentialSubjectID, binding.Connection, binding.Instance)
		ctx = WithCredentialContext(ctx, CredentialContext{
			Mode:       binding.Mode,
			SubjectID:  binding.CredentialSubjectID,
			Connection: binding.Connection,
			Instance:   binding.Instance,
		})
		return ctx, "", nil
	case core.ConnectionModeIdentity:
		connection := binding.Connection
		instance := binding.Instance
		if (requestedConnection != "" && requestedConnection != binding.Connection) || (requestedInstance != "" && requestedInstance != binding.Instance) {
			return ctx, "", fmt.Errorf("%w: identity-token callers may not override connection or instance bindings", ErrAuthorizationDenied)
		}
		SetCredentialAudit(ctx, binding.Mode, binding.CredentialSubjectID, connection, instance)
		if binding.CredentialOwnerID == "" {
			return ctx, "", fmt.Errorf("%w: identity binding missing owner identity", ErrInternal)
		}
		return b.resolveIdentityToken(ctx, prov, binding.CredentialOwnerID, providerName, connection, instance, core.ConnectionModeIdentity, binding.CredentialSubjectID)
	default:
		return ctx, "", fmt.Errorf("%w: identity-token callers may only use identity or none providers", ErrAuthorizationDenied)
	}
}

func (b *Broker) resolveIdentityToken(ctx context.Context, prov core.Provider, identityID, providerName, connection, instance string, credentialMode core.ConnectionMode, credentialSubjectID string) (context.Context, string, error) {
	var storedToken *core.IntegrationToken
	var err error

	if instance != "" {
		storedToken, err = b.tokens.IdentityToken(ctx, identityID, providerName, connection, instance)
		if err != nil {
			if errors.Is(err, core.ErrNotFound) {
				return ctx, "", fmt.Errorf("%w: no token stored for integration %q instance %q", ErrNoToken, providerName, instance)
			}
			return ctx, "", fmt.Errorf("%w: retrieving identity token: %v", ErrInternal, err)
		}
	} else {
		tokens, listErr := b.tokens.ListIdentityTokensForConnection(ctx, identityID, providerName, connection)
		if listErr != nil {
			return ctx, "", fmt.Errorf("%w: listing identity tokens: %v", ErrInternal, listErr)
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
	SetCredentialAudit(ctx, credentialMode, credentialSubjectID, storedToken.Connection, storedToken.Instance)
	ctx = WithCredentialContext(ctx, CredentialContext{
		Mode:       credentialMode,
		SubjectID:  credentialSubjectID,
		Connection: storedToken.Connection,
		Instance:   storedToken.Instance,
	})

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

func (b *Broker) resolveUserToken(ctx context.Context, prov core.Provider, userID, providerName, connection, instance string, credentialMode core.ConnectionMode, credentialSubjectID string) (context.Context, string, error) {
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
	SetCredentialAudit(ctx, credentialMode, credentialSubjectID, storedToken.Connection, storedToken.Instance)
	ctx = WithCredentialContext(ctx, CredentialContext{
		Mode:       credentialMode,
		SubjectID:  credentialSubjectID,
		Connection: storedToken.Connection,
		Instance:   storedToken.Instance,
	})

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

// ResolveUserToken exposes the broker's refresh-aware user token lookup for
// callers that need a user-scoped credential even when the provider runtime
// connection mode would not normally resolve one.
func (b *Broker) ResolveUserToken(ctx context.Context, prov core.Provider, userID, providerName, connection, instance string) (context.Context, string, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return ctx, "", fmt.Errorf("%w: principal has no user ID or email", ErrUserResolution)
	}
	return b.resolveUserToken(ctx, prov, userID, providerName, connection, instance, core.ConnectionModeUser, principal.UserSubjectID(userID))
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

	tokenOwner := token.UserID
	if token.IdentityID != "" {
		tokenOwner = token.IdentityID
	}
	key := tokenOwner + ":" + providerName + ":" + connection + ":" + token.Instance
	v, err, _ := b.refreshGroup.Do(key, func() (any, error) {
		refreshCtx := context.WithoutCancel(ctx)
		startedAt := time.Now()
		resp, err := b.refreshOAuth(refreshCtx, refresher, token.RefreshToken)
		metricutil.RecordConnectionAuthMetrics(refreshCtx, startedAt, providerName, "oauth", "refresh", connectionMode, err != nil)
		return resp, err
	})
	if err != nil {
		var fresh *core.IntegrationToken
		var fetchErr error
		if token.IdentityID != "" {
			fresh, fetchErr = b.tokens.IdentityToken(ctx, token.IdentityID, token.Integration, token.Connection, token.Instance)
		} else {
			fresh, fetchErr = b.tokens.Token(ctx, token.UserID, token.Integration, token.Connection, token.Instance)
		}
		if fetchErr == nil && fresh != nil && fresh.AccessToken != token.AccessToken {
			return fresh.AccessToken, nil
		}
		token.RefreshErrorCount++
		token.UpdatedAt = time.Now()
		if storeErr := b.storeResolvedToken(ctx, token); storeErr != nil {
			slog.WarnContext(ctx, "failed to persist refresh error count", "provider", providerName, "error", storeErr)
		}
		if time.Now().Before(*token.ExpiresAt) {
			return token.AccessToken, nil
		}
		return "", fmt.Errorf("%w: token expired and refresh failed: %w", ErrReconnectRequired, err)
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

	if err := b.storeResolvedToken(ctx, token); err != nil {
		return "", fmt.Errorf("persisting refreshed token: %w", err)
	}
	return token.AccessToken, nil
}

func (b *Broker) storeResolvedToken(ctx context.Context, token *core.IntegrationToken) error {
	if token != nil && token.IdentityID != "" {
		return b.tokens.StoreIdentityToken(ctx, token)
	}
	return b.tokens.StoreToken(ctx, token)
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
