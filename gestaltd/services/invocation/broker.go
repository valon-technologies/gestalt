package invocation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/services/authorization"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"github.com/valon-technologies/gestalt/server/services/plugins/mcpupstream"
	"github.com/valon-technologies/gestalt/server/services/plugins/paraminterp"
	"github.com/valon-technologies/gestalt/server/services/plugins/registry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/singleflight"
)

const (
	tokenRefreshThreshold = 5 * time.Minute
	tracerName            = "gestaltd"
	graphQLOperationID    = "graphql"
	platformSubjectID     = "system:platform-config"

	attrProvider       = metricutil.AttrProvider
	attrOperation      = metricutil.AttrOperation
	attrTransport      = metricutil.AttrTransport
	attrSubjectID      = attribute.Key("gestalt.subject_id")
	attrConnectionMode = metricutil.AttrConnectionMode
)

type connectionCtxKey struct{}
type idempotencyKeyCtxKey struct{}

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

// WithIdempotencyKey carries a caller-supplied key to code that can claim it
// before side effects. This context value is not an exactly-once primitive by
// itself.
func WithIdempotencyKey(ctx context.Context, key string) context.Context {
	key = strings.TrimSpace(key)
	if key == "" {
		return ctx
	}
	return context.WithValue(ctx, idempotencyKeyCtxKey{}, key)
}

func IdempotencyKeyFromContext(ctx context.Context) string {
	v, _ := ctx.Value(idempotencyKeyCtxKey{}).(string)
	return strings.TrimSpace(v)
}

type Invoker interface {
	Invoke(ctx context.Context, p *principal.Principal, providerName, instance, operation string, params map[string]any) (*core.OperationResult, error)
}

type GraphQLRequest = core.GraphQLRequest

type GraphQLInvoker interface {
	InvokeGraphQL(ctx context.Context, p *principal.Principal, providerName, instance string, request GraphQLRequest) (*core.OperationResult, error)
}

type CapabilityLister interface {
	ListCapabilities() []core.Capability
}

type ProviderOverrideResolver interface {
	ResolveProviderOverride(ctx context.Context, p *principal.Principal, providerName string) (core.Provider, bool, error)
}

// UserStore is the user persistence surface the broker needs to canonicalize
// session identities before resolving user-scoped credentials.
type UserStore interface {
	FindOrCreateUser(ctx context.Context, email string) (*core.User, error)
}

var (
	_ Invoker              = (*Broker)(nil)
	_ GraphQLInvoker       = (*Broker)(nil)
	_ CapabilityLister     = (*Broker)(nil)
	_ TokenResolver        = (*Broker)(nil)
	_ subjectTokenResolver = (*Broker)(nil)
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
	providers         *registry.ProviderMap[core.Provider]
	users             UserStore
	externalCreds     core.ExternalCredentialProvider
	authorizer        authorization.RuntimeAuthorizer
	connMapper        ConnectionMapper
	mcpMapper         ConnectionMapper
	connectionAuth    RefresherResolver
	connectionRuntime ConnectionRuntimeResolver
	providerOverrides ProviderOverrideResolver
	refreshGroup      singleflight.Group
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

func WithConnectionRuntime(r ConnectionRuntimeResolver) BrokerOption {
	return func(b *Broker) { b.connectionRuntime = r }
}

func WithAuthorizer(a authorization.RuntimeAuthorizer) BrokerOption {
	return func(b *Broker) { b.authorizer = a }
}

func WithProviderOverrides(r ProviderOverrideResolver) BrokerOption {
	return func(b *Broker) { b.providerOverrides = r }
}

func NewBroker(providers *registry.ProviderMap[core.Provider], users UserStore, externalCreds core.ExternalCredentialProvider, opts ...BrokerOption) *Broker {
	b := &Broker{providers: providers, users: users, externalCreds: externalCreds}
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

func (b *Broker) Invoke(ctx context.Context, p *principal.Principal, providerName, instance, operation string, params map[string]any) (result *core.OperationResult, err error) {
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
		resultStatus := operationResultStatus(result, err)
		recordOperationMetrics(
			ctx,
			startedAt,
			metricProvider,
			metricOperation,
			metricTransport,
			metricConnectionMode,
			resultStatus,
			operationResultFailed(resultStatus, err),
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
	setSubjectAttribute := func(p *principal.Principal) {
		if p == nil {
			return
		}
		subjectID := strings.TrimSpace(p.SubjectID)
		if subjectID == "" && strings.TrimSpace(p.UserID) != "" {
			subjectID = principal.UserSubjectID(strings.TrimSpace(p.UserID))
		}
		if subjectID != "" {
			span.SetAttributes(attrSubjectID.String(subjectID))
		}
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

	if p == nil {
		return fail(ErrNotAuthenticated)
	}
	setSubjectAttribute(p)

	if !principal.AllowsProviderPermission(p, providerName) {
		return fail(fmt.Errorf("%w: %s", ErrScopeDenied, providerName))
	}
	if err := b.resolveUserPrincipal(ctx, p); err != nil {
		return fail(err)
	}
	ctx = withResolvedPrincipal(ctx, p)
	setSubjectAttribute(p)
	conn := ConnectionFromContext(ctx)
	if b.authorizer != nil {
		access, allowed := b.authorizer.ResolveAccess(ctx, p, providerName)
		if access.Policy != "" || access.Role != "" {
			ctx = WithAccessContext(ctx, access)
		}
		if !allowed {
			return fail(fmt.Errorf("%w: %s", ErrAuthorizationDenied, providerName))
		}
	}

	execProv := prov
	if b.providerOverrides != nil {
		override, ok, err := b.providerOverrides.ResolveProviderOverride(ctx, p, providerName)
		if err != nil {
			return fail(fmt.Errorf("%w: provider override: %v", ErrInternal, err))
		}
		if ok {
			execProv = override
		}
	}

	opMeta, transport, resolvedConnection, err := b.resolveOperation(ctx, p, execProv, providerName, operation, conn, instance)
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
			var err error
			conn, err = ResolveOperationConnection(execProv, opMeta.ID, params)
			if err != nil {
				return fail(err)
			}
		}
	}
	if conn == "" && b.connMapper != nil {
		conn = b.connMapper.ConnectionForProvider(providerName)
	}
	metricConnectionMode = metricutil.NormalizeConnectionMode(b.resolveConnectionMode(ctx, prov, providerName, conn))
	span.SetAttributes(attrConnectionMode.String(metricConnectionMode))

	if transport == catalog.TransportMCPPassthrough {
		toolResult, err := CallDirectTool(ctx, b, p, execProv, providerName, operation, conn, instance, params, mcpupstream.CallToolMetaFromContext(ctx))
		if err != nil {
			return fail(err)
		}
		opResult, err := toolResultToOperationResult(toolResult)
		if err != nil {
			return fail(fmt.Errorf("%w: converting tool result: %v", ErrInternal, err))
		}
		if toolResult != nil {
			opResult.MCPResult = toolResult
		}
		return opResult, nil
	}

	ctx, accessToken, err := b.resolveToken(ctx, prov, p, providerName, conn, instance)
	if err != nil {
		return fail(err)
	}

	result, err = execProv.Execute(ctx, operation, params, accessToken)
	if err != nil {
		return fail(err)
	}

	return result, nil
}

func (b *Broker) InvokeGraphQL(ctx context.Context, p *principal.Principal, providerName, instance string, request GraphQLRequest) (result *core.OperationResult, err error) {
	startedAt := time.Now()
	metricProvider := metricutil.UnknownAttrValue
	metricOperation := metricutil.AttrValue("graphql")
	metricTransport := metricutil.AttrValue("graphql")
	metricConnectionMode := metricutil.UnknownAttrValue

	ctx, span := otel.Tracer(tracerName).Start(ctx, "broker.invoke_graphql",
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	defer span.End()
	defer func() {
		resultStatus := operationResultStatus(result, err)
		recordOperationMetrics(
			ctx,
			startedAt,
			metricProvider,
			metricOperation,
			metricTransport,
			metricConnectionMode,
			resultStatus,
			operationResultFailed(resultStatus, err),
		)
	}()

	span.SetAttributes(
		attrProvider.String(providerName),
		attrOperation.String(graphQLOperationID),
		attrTransport.String(metricTransport),
	)

	fail := func(err error) (*core.OperationResult, error) {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	setSubjectAttribute := func(p *principal.Principal) {
		if p == nil {
			return
		}
		subjectID := strings.TrimSpace(p.SubjectID)
		if subjectID == "" && strings.TrimSpace(p.UserID) != "" {
			subjectID = principal.UserSubjectID(strings.TrimSpace(p.UserID))
		}
		if subjectID != "" {
			span.SetAttributes(attrSubjectID.String(subjectID))
		}
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
	graphQLProv, ok := prov.(core.GraphQLSurfaceInvoker)
	if !ok {
		return fail(fmt.Errorf("%w: %s.%s", ErrOperationNotFound, providerName, graphQLOperationID))
	}

	metricProvider = providerName

	if p == nil {
		return fail(ErrNotAuthenticated)
	}
	setSubjectAttribute(p)

	if !principal.AllowsProviderPermission(p, providerName) {
		return fail(fmt.Errorf("%w: %s", ErrScopeDenied, providerName))
	}
	if !principal.AllowsOperationPermission(p, providerName, graphQLOperationID) {
		return fail(fmt.Errorf("%w: %s.%s", ErrScopeDenied, providerName, graphQLOperationID))
	}
	if err := b.resolveUserPrincipal(ctx, p); err != nil {
		return fail(err)
	}
	ctx = withResolvedPrincipal(ctx, p)
	setSubjectAttribute(p)

	conn := ConnectionFromContext(ctx)
	if b.authorizer != nil {
		access, allowed := b.authorizer.ResolveAccess(ctx, p, providerName)
		if access.Policy != "" || access.Role != "" {
			ctx = WithAccessContext(ctx, access)
		}
		if !allowed {
			return fail(fmt.Errorf("%w: %s", ErrAuthorizationDenied, providerName))
		}
	}

	if conn == "" && b.connMapper != nil {
		conn = b.connMapper.ConnectionForProvider(providerName)
	}
	metricConnectionMode = metricutil.NormalizeConnectionMode(b.resolveConnectionMode(ctx, prov, providerName, conn))
	span.SetAttributes(attrConnectionMode.String(metricConnectionMode))

	ctx, accessToken, err := b.resolveToken(ctx, prov, p, providerName, conn, instance)
	if err != nil {
		return fail(err)
	}

	result, err = graphQLProv.InvokeGraphQL(ctx, request, accessToken)
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

func (b *Broker) resolveConnectionMode(ctx context.Context, prov core.Provider, providerName, connection string) core.ConnectionMode {
	if override := CredentialModeOverrideFromContext(ctx); override != "" {
		return override
	}
	if b != nil && b.connectionRuntime != nil {
		if info, ok := b.connectionRuntime(providerName, connection); ok && info.Mode != "" {
			return core.NormalizeConnectionMode(info.Mode)
		}
	}
	return effectiveConnectionMode(ctx, prov)
}

func (b *Broker) resolveConnectionExposure(providerName, connection string) core.ConnectionExposure {
	if b != nil && b.connectionRuntime != nil {
		if info, ok := b.connectionRuntime(providerName, connection); ok && info.Exposure != "" {
			return core.NormalizeConnectionExposure(info.Exposure)
		}
	}
	return core.ConnectionExposureUser
}

func allowsInternalConnection(ctx context.Context) bool {
	if InvocationSurfaceFromContext(ctx) == InvocationSurfaceHTTPBinding && HTTPBindingFromContext(ctx) != "" {
		return true
	}
	return InternalConnectionAccessFromContext(ctx)
}

func (b *Broker) ExpandCatalogTargets(ctx context.Context, p *principal.Principal, providerName string, targets []CatalogResolutionTarget) ([]CatalogResolutionTarget, error) {
	if len(targets) == 0 {
		targets = []CatalogResolutionTarget{{}}
	}
	if !principal.AllowsProviderPermission(p, providerName) {
		return nil, fmt.Errorf("%w: %s", ErrScopeDenied, providerName)
	}
	if err := b.resolveUserPrincipal(ctx, p); err != nil {
		return nil, err
	}
	ctx = withResolvedPrincipal(ctx, p)
	if b.authorizer != nil && !b.authorizer.AllowProvider(ctx, p, providerName) {
		return nil, fmt.Errorf("%w: %s", ErrAuthorizationDenied, providerName)
	}
	prov, err := b.providers.Get(providerName)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return nil, fmt.Errorf("%w: %q", ErrProviderNotFound, providerName)
		}
		return nil, fmt.Errorf("%w: looking up provider: %v", ErrInternal, err)
	}
	if effectiveConnectionMode(ctx, prov) != core.ConnectionModeUser {
		return targets, nil
	}
	if b == nil || core.ExternalCredentialProviderMissing(b.externalCreds) {
		return nil, fmt.Errorf("%w: external credentials provider is not configured", ErrInternal)
	}
	subjectID := principal.EffectiveCredentialSubjectID(p)
	if subjectID == "" {
		return nil, fmt.Errorf("%w: principal has no subject ID or email", ErrUserResolution)
	}

	expanded := make([]CatalogResolutionTarget, 0, len(targets))
	seen := make(map[CatalogResolutionTarget]struct{}, len(targets))
	for _, target := range targets {
		target.Connection = strings.TrimSpace(target.Connection)
		target.Instance = strings.TrimSpace(target.Instance)
		if target.Instance != "" {
			if _, ok := seen[target]; !ok {
				seen[target] = struct{}{}
				expanded = append(expanded, target)
			}
			continue
		}

		credentials, listErr := b.externalCreds.ListCredentialsForConnection(ctx, subjectID, providerName, target.Connection)
		if listErr != nil {
			return nil, fmt.Errorf("%w: listing external credentials: %v", ErrInternal, listErr)
		}
		if len(credentials) == 0 {
			if _, ok := seen[target]; !ok {
				seen[target] = struct{}{}
				expanded = append(expanded, target)
			}
			continue
		}
		nonNil := credentials[:0]
		for _, credential := range credentials {
			if credential != nil {
				nonNil = append(nonNil, credential)
			}
		}
		sort.Slice(nonNil, func(i, j int) bool {
			if nonNil[i].Connection != nonNil[j].Connection {
				return nonNil[i].Connection < nonNil[j].Connection
			}
			return nonNil[i].Instance < nonNil[j].Instance
		})
		for _, credential := range nonNil {
			resolved := CatalogResolutionTarget{
				Connection: strings.TrimSpace(credential.Connection),
				Instance:   strings.TrimSpace(credential.Instance),
			}
			if resolved.Connection == "" {
				resolved.Connection = target.Connection
			}
			if _, ok := seen[resolved]; ok {
				continue
			}
			seen[resolved] = struct{}{}
			expanded = append(expanded, resolved)
		}
	}
	return expanded, nil
}

func (b *Broker) resolveToken(ctx context.Context, prov core.Provider, p *principal.Principal, providerName, connection, instance string) (context.Context, string, error) {
	resolved := principal.FromContext(ctx)
	if resolved != nil {
		p = resolved
	}
	if resolved == nil {
		if err := b.resolveUserPrincipal(ctx, p); err != nil {
			return ctx, "", err
		}
		ctx = withResolvedPrincipal(ctx, p)
	}

	mode := b.resolveConnectionMode(ctx, prov, providerName, connection)
	if b.resolveConnectionExposure(providerName, connection) == core.ConnectionExposureInternal && !allowsInternalConnection(ctx) {
		return ctx, "", fmt.Errorf("%w: integration %q connection %q is internal", ErrAuthorizationDenied, providerName, strings.TrimSpace(connection))
	}
	switch mode {
	case core.ConnectionModeNone:
		SetCredentialAudit(ctx, core.ConnectionModeNone, "", "", "")
		ctx = WithCredentialContext(ctx, CredentialContext{Mode: core.ConnectionModeNone})
		return ctx, "", nil

	case core.ConnectionModePlatform:
		return b.resolvePlatformCredential(ctx, providerName, connection, instance)

	case core.ConnectionModeUser:
		subjectID := principal.EffectiveCredentialSubjectID(p)
		if subjectID == "" {
			return ctx, "", fmt.Errorf("%w: principal has no subject ID or email", ErrUserResolution)
		}
		return b.resolveSubjectCredential(ctx, prov, subjectID, providerName, connection, instance, core.ConnectionModeUser, subjectID)

	default:
		return ctx, "", fmt.Errorf("%w: unknown connection mode %q", ErrInternal, mode)
	}
}

func (b *Broker) resolvePlatformCredential(ctx context.Context, providerName, connection, instance string) (context.Context, string, error) {
	if b == nil || b.connectionRuntime == nil {
		return ctx, "", fmt.Errorf("%w: no deployment credential configured for integration %q", ErrNoCredential, providerName)
	}
	instance = strings.TrimSpace(instance)
	if instance != "" {
		return ctx, "", fmt.Errorf("%w: deployment-managed connection for integration %q does not support instances", ErrNoCredential, providerName)
	}
	connection = strings.TrimSpace(connection)
	if connection == "" {
		connection = core.PluginConnectionName
	}
	info, ok := b.connectionRuntime(providerName, connection)
	token := strings.TrimSpace(info.Token)
	if !ok || token == "" {
		return ctx, "", fmt.Errorf("%w: no deployment credential configured for integration %q connection %q", ErrNoCredential, providerName, connection)
	}
	SetCredentialAudit(ctx, core.ConnectionModePlatform, platformSubjectID, connection, "")
	ctx = WithCredentialContext(ctx, CredentialContext{
		Mode:       core.ConnectionModePlatform,
		SubjectID:  platformSubjectID,
		Connection: connection,
	})
	return ctx, token, nil
}

func (b *Broker) resolveUserPrincipal(ctx context.Context, p *principal.Principal) error {
	p = principal.Canonicalize(p)
	if p == nil || p.UserID != "" || principal.IsNonUserPrincipal(p) || p.Identity == nil || p.Identity.Email == "" {
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
	principal.Canonicalize(p)
	if p.Identity != nil && p.Identity.DisplayName == "" {
		p.Identity.DisplayName = dbUser.DisplayName
	}
	return nil
}

func (b *Broker) resolveSubjectCredential(ctx context.Context, prov core.Provider, subjectID, providerName, connection, instance string, credentialMode core.ConnectionMode, credentialSubjectID string) (context.Context, string, error) {
	if b == nil || core.ExternalCredentialProviderMissing(b.externalCreds) {
		return ctx, "", fmt.Errorf("%w: external credentials provider is not configured", ErrInternal)
	}

	var storedCredential *core.ExternalCredential
	var err error

	if instance != "" {
		storedCredential, err = b.externalCreds.GetCredential(ctx, subjectID, providerName, connection, instance)
		if err != nil {
			if errors.Is(err, core.ErrNotFound) {
				return ctx, "", fmt.Errorf("%w: no external credential stored for integration %q instance %q", ErrNoCredential, providerName, instance)
			}
			return ctx, "", fmt.Errorf("%w: retrieving external credential: %v", ErrInternal, err)
		}
	} else {
		tokens, listErr := b.externalCreds.ListCredentialsForConnection(ctx, subjectID, providerName, connection)
		if listErr != nil {
			return ctx, "", fmt.Errorf("%w: listing external credentials: %v", ErrInternal, listErr)
		}
		switch len(tokens) {
		case 0:
			return ctx, "", fmt.Errorf("%w: no external credential stored for integration %q", ErrNoCredential, providerName)
		case 1:
			storedCredential = tokens[0]
		default:
			instances := make([]string, len(tokens))
			for i, t := range tokens {
				instances[i] = t.Instance
			}
			return ctx, "", fmt.Errorf("%w: integration %q has %d connections (%v); specify which instance to use with the %q parameter",
				ErrAmbiguousInstance, providerName, len(tokens), instances, "_instance")
		}
	}

	if storedCredential == nil {
		return ctx, "", fmt.Errorf("%w: no external credential stored for integration %q", ErrNoCredential, providerName)
	}
	SetCredentialAudit(ctx, credentialMode, credentialSubjectID, storedCredential.Connection, storedCredential.Instance)
	ctx = WithCredentialContext(ctx, CredentialContext{
		Mode:       credentialMode,
		SubjectID:  credentialSubjectID,
		Connection: storedCredential.Connection,
		Instance:   storedCredential.Instance,
	})

	if storedCredential.MetadataJSON != "" {
		var connParams map[string]string
		if err := json.Unmarshal([]byte(storedCredential.MetadataJSON), &connParams); err != nil {
			slog.WarnContext(ctx, "malformed metadata JSON", "provider", providerName, "error", err)
		} else if len(connParams) > 0 {
			ctx = core.WithConnectionParams(ctx, connParams)
		}
	}

	accessToken, err := b.refreshCredentialIfNeeded(ctx, storedCredential, providerName, connection, metricutil.NormalizeConnectionMode(credentialMode))
	return ctx, accessToken, err
}

// ResolveSubjectToken exposes the broker's refresh-aware token lookup for
// callers that need a specific subject-owned credential even when the provider
// runtime connection mode would not normally resolve one.
func (b *Broker) ResolveSubjectToken(ctx context.Context, prov core.Provider, subjectID, providerName, connection, instance string) (context.Context, string, error) {
	subjectID = strings.TrimSpace(subjectID)
	if subjectID == "" {
		return ctx, "", fmt.Errorf("%w: principal has no subject ID or email", ErrUserResolution)
	}
	return b.resolveSubjectCredential(ctx, prov, subjectID, providerName, connection, instance, core.ConnectionModeUser, subjectID)
}

func (b *Broker) refreshCredentialIfNeeded(ctx context.Context, token *core.ExternalCredential, providerName, connection, connectionMode string) (string, error) {
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

	key := token.SubjectID + ":" + providerName + ":" + connection + ":" + token.Instance
	v, err, _ := b.refreshGroup.Do(key, func() (any, error) {
		refreshCtx := context.WithoutCancel(ctx)
		startedAt := time.Now()
		resp, err := b.refreshOAuth(refreshCtx, refresher, token.RefreshToken)
		metricutil.RecordConnectionAuthMetrics(refreshCtx, startedAt, providerName, "oauth", "refresh", connectionMode, err != nil)
		return resp, err
	})
	if err != nil {
		fresh, fetchErr := b.externalCreds.GetCredential(ctx, token.SubjectID, token.Integration, token.Connection, token.Instance)
		if fetchErr == nil && fresh != nil && fresh.AccessToken != token.AccessToken {
			return fresh.AccessToken, nil
		}
		token.RefreshErrorCount++
		token.UpdatedAt = time.Now()
		if storeErr := b.externalCreds.PutCredential(ctx, token); storeErr != nil {
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

	if err := b.externalCreds.PutCredential(ctx, token); err != nil {
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
