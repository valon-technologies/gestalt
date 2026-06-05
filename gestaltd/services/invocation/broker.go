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

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/services/access"
	"github.com/valon-technologies/gestalt/server/services/apps/registry"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	tracerName         = "gestaltd"
	graphQLOperationID = "graphql"
	resultBodyLogLimit = 4096

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

type Broker struct {
	providers         *registry.ProviderMap[core.Provider]
	users             UserStore
	externalCreds     core.ExternalCredentialProvider
	connMapper        ConnectionMapper
	mcpMapper         ConnectionMapper
	connectionRuntime ConnectionRuntimeResolver
	access            *access.Enforcer
	logger            *slog.Logger
	tracerProvider    trace.TracerProvider
}

type BrokerOption func(*Broker)

func WithConnectionMapper(m ConnectionMapper) BrokerOption {
	return func(b *Broker) { b.connMapper = m }
}

func WithMCPConnectionMapper(m ConnectionMapper) BrokerOption {
	return func(b *Broker) { b.mcpMapper = m }
}

func WithConnectionRuntime(r ConnectionRuntimeResolver) BrokerOption {
	return func(b *Broker) { b.connectionRuntime = r }
}

func WithEnforcer(enforcer *access.Enforcer) BrokerOption {
	return func(b *Broker) { b.access = enforcer }
}

func WithLogger(l *slog.Logger) BrokerOption {
	return func(b *Broker) { b.logger = l }
}

func WithTracerProvider(provider trace.TracerProvider) BrokerOption {
	return func(b *Broker) { b.tracerProvider = provider }
}

func NewBroker(providers *registry.ProviderMap[core.Provider], users UserStore, externalCreds core.ExternalCredentialProvider, opts ...BrokerOption) *Broker {
	b := &Broker{providers: providers, users: users, externalCreds: externalCreds}
	for _, o := range opts {
		o(b)
	}
	return b
}

func (b *Broker) log() *slog.Logger {
	if b != nil && b.logger != nil {
		return b.logger
	}
	return slog.Default()
}

func (b *Broker) tracer() trace.Tracer {
	if b != nil && b.tracerProvider != nil {
		return b.tracerProvider.Tracer(tracerName)
	}
	return otel.Tracer(tracerName)
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

	ctx, span := b.tracer().Start(ctx, "broker.invoke",
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
		return fail(access.ErrNotAuthenticated)
	}
	setSubjectAttribute(p)

	if err := b.resolveUserPrincipal(ctx, p); err != nil {
		return fail(err)
	}
	ctx = withResolvedPrincipal(ctx, p)
	setSubjectAttribute(p)
	conn := ConnectionFromContext(ctx)
	opMeta, transport, resolvedConnection, err := b.resolveOperation(ctx, p, prov, providerName, operation, conn, instance)
	if err != nil {
		return fail(err)
	}
	if err := b.access.Require(ctx, p, access.Request{
		ResourceType:    providerName,
		Action:          opMeta.ID,
		CredentialScope: access.OperationCredentialScope,
	}); err != nil {
		return fail(err)
	}
	metricOperation = operation
	metricTransport = metricutil.AttrValue(transport)
	span.SetAttributes(attrTransport.String(metricTransport))
	ctx = WithCatalogOperation(ctx, providerName, opMeta)

	operationConnection := resolvedConnection
	if strings.TrimSpace(conn) != "" {
		if operationConnection == "" {
			operationConnection, err = ResolveOperationConnection(prov, opMeta.ID, params)
			if err != nil {
				return fail(err)
			}
		}
		operationConnection = core.ResolveConnectionAlias(operationConnection)
		explicitConnection := core.ResolveConnectionAlias(conn)
		overrideAllowed := transport == catalog.TransportApp || OperationConnectionOverrideAllowed(prov, opMeta.ID, params)
		overrideDenied := !overrideAllowed
		if operationConnection != "" && operationConnection != explicitConnection && overrideDenied {
			return fail(fmt.Errorf(
				"%w: operation %q on integration %q uses connection %q; omit the connection override or use that connection instead of %q",
				ErrInvalidInvocation,
				opMeta.ID,
				providerName,
				operationConnection,
				conn,
			))
		}
	}
	if conn == "" {
		conn = operationConnection
	}
	if conn == "" {
		operationConnection, err = ResolveOperationConnection(prov, opMeta.ID, params)
		if err != nil {
			return fail(err)
		}
		conn = operationConnection
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
	result, err = prov.Execute(ctx, operation, params, accessToken)
	if err != nil {
		return fail(err)
	}
	b.observePlugin5xxResult(ctx, span, p, providerName, opMeta.ID, transport, result)

	return result, nil
}

func (b *Broker) observePlugin5xxResult(ctx context.Context, span trace.Span, p *principal.Principal, providerName, operation, transport string, result *core.OperationResult) {
	if result == nil || transport != catalog.TransportApp || !validHTTPStatus(result.Status) || result.Status < http.StatusInternalServerError {
		return
	}

	status, statusClass := resultStatusAttributes(result.Status)
	span.SetStatus(codes.Error, "provider operation returned 5xx result")
	span.SetAttributes(
		metricutil.AttrResultStatus.String(status),
		metricutil.AttrResultStatusClass.String(statusClass),
	)

	attrs := []any{
		"provider", strings.TrimSpace(providerName),
		"operation", strings.TrimSpace(operation),
		"transport", strings.TrimSpace(transport),
		"result_status", result.Status,
		"result_status_class", statusClass,
		"result_body", truncateResultBodyForLog(result.Body),
	}
	if surface := InvocationSurfaceFromContext(ctx); surface != "" {
		attrs = append(attrs, "surface", string(surface))
	}
	if binding := HTTPBindingFromContext(ctx); binding != "" {
		attrs = append(attrs, "http_binding", binding)
	}
	if subjectID := resultSubjectID(p); subjectID != "" {
		attrs = append(attrs, "subject_id", subjectID)
	}

	b.log().WarnContext(ctx, "provider operation returned 5xx result", attrs...)
}

func truncateResultBodyForLog(body string) string {
	if len(body) <= resultBodyLogLimit {
		return body
	}
	return body[:resultBodyLogLimit]
}

func resultSubjectID(p *principal.Principal) string {
	p = principal.Canonicalized(p)
	if p == nil {
		return ""
	}
	return strings.TrimSpace(p.SubjectID)
}

func (b *Broker) InvokeGraphQL(ctx context.Context, p *principal.Principal, providerName, instance string, request GraphQLRequest) (result *core.OperationResult, err error) {
	startedAt := time.Now()
	metricProvider := metricutil.UnknownAttrValue
	metricOperation := metricutil.AttrValue("graphql")
	metricTransport := metricutil.AttrValue("graphql")
	metricConnectionMode := metricutil.UnknownAttrValue

	ctx, span := b.tracer().Start(ctx, "broker.invoke_graphql",
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
		return fail(access.ErrNotAuthenticated)
	}
	setSubjectAttribute(p)

	if err := b.resolveUserPrincipal(ctx, p); err != nil {
		return fail(err)
	}
	ctx = withResolvedPrincipal(ctx, p)
	setSubjectAttribute(p)
	if err := b.access.Require(ctx, p, access.Request{
		ResourceType:    providerName,
		Action:          graphQLOperationID,
		CredentialScope: access.OperationCredentialScope,
	}); err != nil {
		return fail(err)
	}

	conn := ConnectionFromContext(ctx)
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

func (b *Broker) ResolveToken(ctx context.Context, p *principal.Principal, providerName, connection, instance string) (context.Context, string, error) {
	if err := b.access.Require(ctx, p, access.Request{
		ResourceType:    providerName,
		CredentialScope: access.ProviderCredentialScope,
	}); err != nil {
		return ctx, "", err
	}
	if err := b.resolveUserPrincipal(ctx, p); err != nil {
		return ctx, "", err
	}
	ctx = withResolvedPrincipal(ctx, p)
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

func (b *Broker) connectionID(providerName, connection string) string {
	providerName = strings.TrimSpace(providerName)
	connection = strings.TrimSpace(connection)
	if connection == "" {
		connection = core.AppConnectionName
	}
	if b != nil && b.connectionRuntime != nil {
		if info, ok := b.connectionRuntime(providerName, connection); ok && strings.TrimSpace(info.ConnectionID) != "" {
			return strings.TrimSpace(info.ConnectionID)
		}
	}
	return providerName + ":" + connection
}

func (b *Broker) ExpandCatalogTargets(ctx context.Context, p *principal.Principal, providerName string, targets []CatalogResolutionTarget) ([]CatalogResolutionTarget, error) {
	if len(targets) == 0 {
		targets = []CatalogResolutionTarget{{}}
	}
	if err := b.access.Require(ctx, p, access.Request{
		ResourceType:    providerName,
		CredentialScope: access.ProviderCredentialScope,
	}); err != nil {
		return nil, err
	}
	if err := b.resolveUserPrincipal(ctx, p); err != nil {
		return nil, err
	}
	ctx = withResolvedPrincipal(ctx, p)
	prov, err := b.providers.Get(providerName)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return nil, fmt.Errorf("%w: %q", ErrProviderNotFound, providerName)
		}
		return nil, fmt.Errorf("%w: looking up provider: %v", ErrInternal, err)
	}
	if effectiveConnectionMode(ctx, prov) != core.ConnectionModeSubject {
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

		connectionID := b.connectionID(providerName, target.Connection)
		credentials, listErr := b.externalCreds.ListCredentialsForConnection(ctx, subjectID, connectionID)
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
	switch mode {
	case core.ConnectionModeNone:
		SetCredentialAudit(ctx, core.ConnectionModeNone, "", "", "")
		ctx = WithCredentialContext(ctx, CredentialContext{Mode: core.ConnectionModeNone})
		return ctx, "", nil

	case core.ConnectionModeSubject:
		subjectID := principal.EffectiveCredentialSubjectID(p)
		if subjectID == "" {
			return ctx, "", fmt.Errorf("%w: principal has no subject ID or email", ErrUserResolution)
		}
		return b.resolveSubjectCredential(ctx, prov, subjectID, providerName, connection, instance, core.ConnectionModeSubject, subjectID)

	default:
		return ctx, "", fmt.Errorf("%w: unknown connection mode %q", ErrInternal, mode)
	}
}

func (b *Broker) ResolveRuntimeConnectionCredential(ctx context.Context, p *principal.Principal, providerName, connection, instance string) (context.Context, ConnectionRuntimeCredential, ConnectionRuntimeInfo, error) {
	if !InternalConnectionAccessFromContext(ctx) {
		return ctx, ConnectionRuntimeCredential{}, ConnectionRuntimeInfo{}, fmt.Errorf("%w: runtime connection credential resolution requires internal access", access.ErrDenied)
	}
	if b == nil || b.connectionRuntime == nil {
		return ctx, ConnectionRuntimeCredential{}, ConnectionRuntimeInfo{}, fmt.Errorf("%w: runtime connection resolver is not configured", ErrNoCredential)
	}
	providerName = strings.TrimSpace(providerName)
	connection = core.ResolveConnectionAlias(connection)
	if connection == "" {
		connection = core.AppConnectionName
	}
	info, ok := b.connectionRuntime(providerName, connection)
	if !ok {
		return ctx, ConnectionRuntimeCredential{}, ConnectionRuntimeInfo{}, fmt.Errorf("%w: no runtime credential configured for provider %q connection %q", ErrNoCredential, providerName, connection)
	}
	switch core.NormalizeConnectionMode(info.Mode) {
	case core.ConnectionModeNone:
		SetCredentialAudit(ctx, core.ConnectionModeNone, "", "", "")
		ctx = WithCredentialContext(ctx, CredentialContext{Mode: core.ConnectionModeNone})
		return ctx, ConnectionRuntimeCredential{}, info, nil

	case core.ConnectionModeSubject:
		if err := b.resolveUserPrincipal(ctx, p); err != nil {
			return ctx, ConnectionRuntimeCredential{}, info, err
		}
		ctx = withResolvedPrincipal(ctx, p)
		subjectID := principal.EffectiveCredentialSubjectID(p)
		if subjectID == "" {
			return ctx, ConnectionRuntimeCredential{}, info, fmt.Errorf("%w: principal has no subject ID or email", ErrUserResolution)
		}
		resolvedCtx, credential, err := b.resolveSubjectRuntimeCredential(ctx, nil, subjectID, providerName, connection, instance, core.ConnectionModeSubject, subjectID)
		return resolvedCtx, credential, info, err

	default:
		return ctx, ConnectionRuntimeCredential{}, info, fmt.Errorf("%w: unknown connection mode %q", ErrInternal, info.Mode)
	}
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
	ctx, credential, err := b.resolveSubjectRuntimeCredential(ctx, prov, subjectID, providerName, connection, instance, credentialMode, credentialSubjectID)
	return ctx, credential.Token, err
}

func (b *Broker) resolveSubjectRuntimeCredential(ctx context.Context, prov core.Provider, subjectID, providerName, connection, instance string, credentialMode core.ConnectionMode, credentialSubjectID string) (context.Context, ConnectionRuntimeCredential, error) {
	if b == nil || core.ExternalCredentialProviderMissing(b.externalCreds) {
		return ctx, ConnectionRuntimeCredential{}, fmt.Errorf("%w: external credentials provider is not configured", ErrInternal)
	}

	connectionID := b.connectionID(providerName, connection)
	runtimeInfo := ConnectionRuntimeInfo{}
	if b.connectionRuntime != nil {
		runtimeInfo, _ = b.connectionRuntime(providerName, connection)
	}

	resp, err := b.externalCreds.ResolveCredential(ctx, &core.ResolveExternalCredentialRequest{
		Provider:            providerName,
		Connection:          connection,
		ConnectionID:        connectionID,
		Mode:                credentialMode,
		CredentialSubjectID: subjectID,
		Instance:            instance,
		Auth:                runtimeInfo.AuthConfig,
		ConnectionParams:    runtimeInfo.Params,
	})
	if err != nil {
		switch {
		case errors.Is(err, core.ErrNotFound):
			if instance != "" {
				return ctx, ConnectionRuntimeCredential{}, fmt.Errorf("%w: no external credential stored for integration %q instance %q", ErrNoCredential, providerName, instance)
			}
			return ctx, ConnectionRuntimeCredential{}, fmt.Errorf("%w: no external credential stored for integration %q", ErrNoCredential, providerName)
		case errors.Is(err, core.ErrAmbiguousCredential):
			return ctx, ConnectionRuntimeCredential{}, fmt.Errorf("%w: integration %q has multiple connections; specify which instance to use with the %q parameter",
				ErrAmbiguousInstance, providerName, "_instance")
		case errors.Is(err, core.ErrReconnectRequired):
			return ctx, ConnectionRuntimeCredential{}, fmt.Errorf("%w: resolving external credential: %v", ErrReconnectRequired, err)
		default:
			return ctx, ConnectionRuntimeCredential{}, fmt.Errorf("resolving external credential: %w", err)
		}
	}
	if resp == nil {
		return ctx, ConnectionRuntimeCredential{}, fmt.Errorf("%w: external credentials provider returned nil resolution", ErrInternal)
	}
	storedCredential := resp.Credential
	if storedCredential == nil {
		return ctx, ConnectionRuntimeCredential{}, fmt.Errorf("%w: no external credential stored for integration %q", ErrNoCredential, providerName)
	}
	credentialConnection := strings.TrimSpace(storedCredential.Connection)
	if credentialConnection == "" {
		credentialConnection = strings.TrimSpace(connection)
	}
	if credentialConnection == "" {
		credentialConnection = core.AppConnectionName
	}
	SetCredentialAudit(ctx, credentialMode, credentialSubjectID, credentialConnection, storedCredential.Instance)
	ctx = WithCredentialContext(ctx, CredentialContext{
		Mode:       credentialMode,
		SubjectID:  credentialSubjectID,
		Connection: credentialConnection,
		Instance:   storedCredential.Instance,
	})

	metadataJSON := storedCredential.MetadataJSON
	if resp.MetadataJSON != "" {
		metadataJSON = resp.MetadataJSON
	}
	if metadataJSON != "" {
		var connParams map[string]string
		if err := json.Unmarshal([]byte(metadataJSON), &connParams); err != nil {
			b.log().WarnContext(ctx, "malformed metadata JSON", "provider", providerName, "error", err)
		} else if len(connParams) > 0 {
			ctx = core.WithConnectionParams(ctx, connParams)
		}
	}
	if len(resp.Params) > 0 {
		ctx = core.WithConnectionParams(ctx, resp.Params)
	}

	expiresAt := resp.ExpiresAt
	if expiresAt == nil {
		expiresAt = storedCredential.ExpiresAt
	}
	token := strings.TrimSpace(resp.Token)
	if token == "" {
		token = storedCredential.AccessToken
	}
	return ctx, ConnectionRuntimeCredential{Token: token, ExpiresAt: expiresAt}, nil
}

// ResolveSubjectToken exposes the broker's refresh-aware token lookup for
// callers that need a specific subject-owned credential even when the provider
// runtime connection mode would not normally resolve one.
func (b *Broker) ResolveSubjectToken(ctx context.Context, prov core.Provider, subjectID, providerName, connection, instance string) (context.Context, string, error) {
	subjectID = strings.TrimSpace(subjectID)
	if subjectID == "" {
		return ctx, "", fmt.Errorf("%w: principal has no subject ID or email", ErrUserResolution)
	}
	return b.resolveSubjectCredential(ctx, prov, subjectID, providerName, connection, instance, core.ConnectionModeSubject, subjectID)
}
