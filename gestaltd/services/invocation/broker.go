package invocation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/registry"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/observability"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/types/known/structpb"
)

const (
	tracerName         = "gestaltd"
	graphQLOperationID = core.GraphQLCapabilityID
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

type RemoteCredentialDelegated interface {
	RemoteCredentialDelegated() bool
}

type Invoker interface {
	Invoke(ctx context.Context, p *principal.Principal, providerName, instance, operation string, params map[string]any) (*core.OperationResult, error)
}

// StreamingInvoker is implemented by invokers that support streaming
// operation responses. AppServer.InvokeStream checks for this interface.
type StreamingInvoker interface {
	InvokeStream(ctx context.Context, p *principal.Principal, providerName, instance, operation string, params map[string]any) (core.StreamReader, error)
}

// StreamFinalizer lets a transport report that it stopped consuming a stream
// before the reader could observe its terminal frame or error.
type StreamFinalizer interface {
	FinalizeStream(error)
}

// MaybeStreamingInvoker is implemented by invokers that resolve unary-vs-stream
// in a single catalog lookup.
type MaybeStreamingInvoker interface {
	InvokeMaybeStream(ctx context.Context, p *principal.Principal, providerName, instance, operation string, params map[string]any) (*InvokeOutcome, error)
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
	providers                     *registry.ProviderMap[core.Provider]
	users                         UserStore
	externalCreds                 core.ExternalCredentialProvider
	invocationRecorder            observability.InvocationRecordRecorder
	connectionInstancePreferences ConnectionInstancePreferenceStore
	connMapper                    ConnectionMapper
	mcpMapper                     ConnectionMapper
	connectionRuntime             ConnectionRuntimeResolver
	appAccessProfiles             core.AppAccessProfileStore
	authorization                 core.AuthorizationProvider
	providerKinds                 map[string]ProviderKind
	authorizationPolicies         map[string]string
	logger                        *slog.Logger
	tracerProvider                trace.TracerProvider
}

type BrokerOption func(*Broker)

// ConnectionInstancePreferenceStore resolves a subject's preferred credential instance.
type ConnectionInstancePreferenceStore interface {
	PreferredInstance(ctx context.Context, subjectID, connectionID string) (string, error)
}

func WithConnectionInstancePreferences(store ConnectionInstancePreferenceStore) BrokerOption {
	return func(b *Broker) { b.connectionInstancePreferences = store }
}

func WithConnectionMapper(m ConnectionMapper) BrokerOption {
	return func(b *Broker) { b.connMapper = m }
}

func WithMCPConnectionMapper(m ConnectionMapper) BrokerOption {
	return func(b *Broker) { b.mcpMapper = m }
}

func WithConnectionRuntime(r ConnectionRuntimeResolver) BrokerOption {
	return func(b *Broker) { b.connectionRuntime = r }
}

func WithAuthorizationProvider(provider core.AuthorizationProvider) BrokerOption {
	return func(b *Broker) { b.authorization = provider }
}

// WithAppAccessProfiles installs the user-owned app capability policy. It is
// checked in the broker so every invocation surface shares the same decision.
func WithAppAccessProfiles(store core.AppAccessProfileStore) BrokerOption {
	return func(b *Broker) { b.appAccessProfiles = store }
}

func WithProviderKinds(kinds map[string]ProviderKind) BrokerOption {
	return func(b *Broker) { b.providerKinds = kinds }
}

func WithAuthorizationPolicies(policies map[string]string) BrokerOption {
	return func(b *Broker) { b.authorizationPolicies = policies }
}

func WithLogger(l *slog.Logger) BrokerOption {
	return func(b *Broker) { b.logger = l }
}

func WithTracerProvider(provider trace.TracerProvider) BrokerOption {
	return func(b *Broker) { b.tracerProvider = provider }
}

func WithInvocationRecorder(recorder observability.InvocationRecordRecorder) BrokerOption {
	return func(b *Broker) { b.invocationRecorder = recorder }
}

func NewBroker(providers *registry.ProviderMap[core.Provider], users UserStore, externalCreds core.ExternalCredentialProvider, opts ...BrokerOption) *Broker {
	b := &Broker{providers: providers, users: users, externalCreds: externalCreds}
	for _, o := range opts {
		o(b)
	}
	return b
}

func (b *Broker) recordCompletedInvocation(
	ctx context.Context,
	startedAt time.Time,
	provider string,
	operation string,
	transport string,
	connectionMode string,
	resultStatus int,
	failed bool,
) {
	recordOperationMetrics(ctx, startedAt, provider, operation, transport, connectionMode, resultStatus, failed)
	b.recordInvocationRecord(startedAt, provider, operation, resultStatus, failed)
}

// recordInvocationOutcome keeps dispatch-time stream metrics separate from
// completed invocation records. A stream owns its final record until the
// observing reader sees the terminal frame or error; every other outcome is
// finalized here so all non-stream paths share the same recorder wiring.
func (b *Broker) recordInvocationOutcome(
	ctx context.Context,
	startedAt time.Time,
	provider string,
	operation string,
	transport string,
	connectionMode string,
	resultStatus int,
	failed bool,
	streamPending bool,
) {
	if streamPending {
		recordOperationMetrics(ctx, startedAt, provider, operation, transport, connectionMode, resultStatus, failed)
		return
	}
	b.recordCompletedInvocation(ctx, startedAt, provider, operation, transport, connectionMode, resultStatus, failed)
}

func (b *Broker) recordInvocationRecord(
	startedAt time.Time,
	provider string,
	operation string,
	resultStatus int,
	failed bool,
) {
	if b == nil || b.invocationRecorder == nil {
		return
	}
	outcome := observability.InvocationPassed
	if failed {
		outcome = observability.InvocationFailed
	}
	b.invocationRecorder.RecordInvocation(observability.InvocationRecord{
		Provider:  provider,
		Operation: operation,
		Outcome:   outcome,
		Status:    resultStatus,
		Duration:  time.Since(startedAt),
		Timestamp: startedAt.UTC(),
	})
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
	metricOperation := metricutil.AttrValue(operation)
	metricTransport := metricutil.UnknownAttrValue
	metricConnectionMode := metricutil.UnknownAttrValue

	ctx, span := b.tracer().Start(ctx, "broker.invoke",
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	defer span.End()
	defer func() {
		resultStatus := operationResultStatus(result, err)
		b.recordCompletedInvocation(
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

	prov, err := b.providers.GetWithContext(ctx, providerName)
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
	setSubjectAttribute(p)
	ctx = withResolvedPrincipal(ctx, p)
	conn := ConnectionFromContext(ctx)
	opMeta, transport, resolvedConnection, err := b.resolveOperation(ctx, p, prov, providerName, operation, conn, instance)
	if err != nil {
		return fail(err)
	}
	if err := b.checkInvocationOperationAccess(ctx, p, providerName, opMeta.ID); err != nil {
		return fail(err)
	}
	if !providerDelegatesRemoteAuthorization(prov) {
		ctx, err = b.authorizeOperation(ctx, p, providerName, opMeta)
		if err != nil {
			return fail(err)
		}
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

	ctx, err = ApplyInvokeHeaderOverrides(ctx, prov)
	if err != nil {
		return fail(err)
	}
	ctx, accessToken, err := b.resolveToken(ctx, prov, p, providerName, conn, instance)
	if err != nil {
		b.persistReconnectRequired(ctx, p, providerName, conn, instance, err)
		return fail(err)
	}
	result, err = prov.Execute(ctx, operation, params, accessToken)
	if err != nil {
		b.persistReconnectRequired(ctx, p, providerName, conn, instance, err)
		return fail(err)
	}
	b.observePlugin5xxResult(ctx, span, p, providerName, opMeta.ID, transport, result)

	return result, nil
}

// InvokeStream resolves and authorizes a streaming operation invocation the
// same way Invoke does, then dispatches to the provider's StreamingExecutor.
// The returned StreamReader yields InvokeFrame frames (metadata first, then
// data). The caller is responsible for forwarding frames to the transport.
func (b *Broker) InvokeStream(ctx context.Context, p *principal.Principal, providerName, instance, operation string, params map[string]any) (core.StreamReader, error) {
	startedAt := time.Now()
	metricProvider := metricutil.UnknownAttrValue
	metricOperation := metricutil.AttrValue(operation)
	metricTransport := metricutil.UnknownAttrValue
	metricConnectionMode := metricutil.UnknownAttrValue
	var dispatchErr error

	ctx, span := b.tracer().Start(ctx, "broker.invoke_stream",
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	spanOwned := false
	defer func() {
		if !spanOwned {
			span.End()
		}
		// Record dispatch-phase metrics while a successfully dispatched stream
		// owns its final outcome. Dispatch failures (auth, resolve, token, etc.)
		// are completed here, mirroring unary Invoke.
		resultStatus := operationResultStatus(nil, dispatchErr)
		b.recordInvocationOutcome(
			ctx,
			startedAt,
			metricProvider,
			metricOperation,
			metricTransport,
			metricConnectionMode,
			resultStatus,
			operationResultFailed(resultStatus, dispatchErr),
			spanOwned && dispatchErr == nil,
		)
	}()

	span.SetAttributes(
		attrProvider.String(providerName),
		attrOperation.String(operation),
	)

	fail := func(err error) (core.StreamReader, error) {
		dispatchErr = err
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

	prov, err := b.providers.GetWithContext(ctx, providerName)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return fail(fmt.Errorf("%w: %q", ErrProviderNotFound, providerName))
		}
		return fail(fmt.Errorf("%w: looking up provider: %v", ErrInternal, err))
	}

	metricProvider = providerName

	if p == nil {
		return fail(ErrNotAuthenticated)
	}
	setSubjectAttribute(p)

	if !principal.AllowsProviderPermission(p, providerName) {
		return fail(fmt.Errorf("%w: %s", ErrScopeDenied, providerName))
	}
	setSubjectAttribute(p)
	ctx = withResolvedPrincipal(ctx, p)
	conn := ConnectionFromContext(ctx)
	opMeta, transport, resolvedConnection, err := b.resolveOperation(ctx, p, prov, providerName, operation, conn, instance)
	if err != nil {
		return fail(err)
	}
	if err := b.checkInvocationOperationAccess(ctx, p, providerName, opMeta.ID); err != nil {
		return fail(err)
	}
	if !providerDelegatesRemoteAuthorization(prov) {
		ctx, err = b.authorizeOperation(ctx, p, providerName, opMeta)
		if err != nil {
			return fail(err)
		}
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
		if operationConnection != "" && operationConnection != explicitConnection && !overrideAllowed {
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

	ctx, err = ApplyInvokeHeaderOverrides(ctx, prov)
	if err != nil {
		return fail(err)
	}
	ctx, accessToken, err := b.resolveToken(ctx, prov, p, providerName, conn, instance)
	if err != nil {
		b.persistReconnectRequired(ctx, p, providerName, conn, instance, err)
		return fail(err)
	}
	// Verify the catalog declares this operation as streaming before
	// dispatching; a unary operation on a provider that also implements
	// StreamingExecutor should not be sent down the streaming path.
	if opMeta.Response == nil || !opMeta.Response.IsStream() {
		return fail(fmt.Errorf("%w: %s.%s is not a streaming operation", ErrStreamingUnsupported, providerName, opMeta.ID))
	}
	streamExec, ok := prov.(core.StreamingExecutor)
	if !ok {
		return fail(fmt.Errorf("%w: %s.%s does not support streaming", ErrStreamingUnsupported, providerName, opMeta.ID))
	}
	reader, err := streamExec.ExecuteStream(ctx, operation, params, accessToken)
	if err != nil {
		b.persistReconnectRequired(ctx, p, providerName, conn, instance, err)
		return fail(err)
	}
	// Wrap the reader so the first metadata frame is observed for 5xx,
	// mirroring the unary observePlugin5xxResult telemetry. A streaming 5xx
	// arrives as a metadata frame with status >= 500, not a terminal
	// *OperationResult, so the observation happens on the first Recv.
	spanOwned = true
	return newObservingStreamReader(
		ctx,
		reader,
		b,
		span,
		startedAt,
		p,
		providerName,
		opMeta.ID,
		metricutil.AttrValue(transport),
	), nil
}

// InvokeOutcome is the discriminated result of InvokeMaybeStream. Exactly one
// of Unary or Stream is set.
type InvokeOutcome struct {
	Unary  *core.OperationResult
	Stream core.StreamReader
}

// IsStream reports whether this outcome carries a streaming response.
func (o *InvokeOutcome) IsStream() bool { return o != nil && o.Stream != nil }

// InvokeMaybeStream resolves and authorizes an invocation once, then dispatches
// to the unary or streaming executor based on the catalog response mode.
func (b *Broker) InvokeMaybeStream(ctx context.Context, p *principal.Principal, providerName, instance, operation string, params map[string]any) (*InvokeOutcome, error) {
	startedAt := time.Now()
	metricProvider := metricutil.UnknownAttrValue
	metricOperation := metricutil.AttrValue(operation)
	metricTransport := metricutil.UnknownAttrValue
	metricConnectionMode := metricutil.UnknownAttrValue
	var dispatchErr error
	var unaryResult *core.OperationResult

	ctx, span := b.tracer().Start(ctx, "broker.invoke_maybe_stream",
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	spanOwned := false
	defer func() {
		if !spanOwned {
			span.End()
		}
		resultStatus := operationResultStatus(unaryResult, dispatchErr)
		b.recordInvocationOutcome(
			ctx,
			startedAt,
			metricProvider,
			metricOperation,
			metricTransport,
			metricConnectionMode,
			resultStatus,
			operationResultFailed(resultStatus, dispatchErr),
			spanOwned && dispatchErr == nil,
		)
	}()

	span.SetAttributes(
		attrProvider.String(providerName),
		attrOperation.String(operation),
	)

	fail := func(err error) (*InvokeOutcome, error) {
		dispatchErr = err
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

	prov, err := b.providers.GetWithContext(ctx, providerName)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return fail(fmt.Errorf("%w: %q", ErrProviderNotFound, providerName))
		}
		return fail(fmt.Errorf("%w: looking up provider: %v", ErrInternal, err))
	}

	metricProvider = providerName

	if p == nil {
		return fail(ErrNotAuthenticated)
	}
	setSubjectAttribute(p)

	if !principal.AllowsProviderPermission(p, providerName) {
		return fail(fmt.Errorf("%w: %s", ErrScopeDenied, providerName))
	}
	p = principal.Canonicalized(p)
	setSubjectAttribute(p)
	ctx = withResolvedPrincipal(ctx, p)
	conn := ConnectionFromContext(ctx)
	opMeta, transport, resolvedConnection, err := b.resolveOperation(ctx, p, prov, providerName, operation, conn, instance)
	if err != nil {
		return fail(err)
	}
	if err := b.checkInvocationOperationAccess(ctx, p, providerName, opMeta.ID); err != nil {
		return fail(err)
	}
	if !providerDelegatesRemoteAuthorization(prov) {
		ctx, err = b.authorizeOperation(ctx, p, providerName, opMeta)
		if err != nil {
			return fail(err)
		}
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
		if operationConnection != "" && operationConnection != explicitConnection && !overrideAllowed {
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

	ctx, err = ApplyInvokeHeaderOverrides(ctx, prov)
	if err != nil {
		return fail(err)
	}
	ctx, accessToken, err := b.resolveToken(ctx, prov, p, providerName, conn, instance)
	if err != nil {
		b.persistReconnectRequired(ctx, p, providerName, conn, instance, err)
		return fail(err)
	}

	if opMeta.Response != nil && opMeta.Response.IsStream() {
		streamExec, ok := prov.(core.StreamingExecutor)
		if !ok {
			return fail(fmt.Errorf("%w: %s.%s does not support streaming", ErrStreamingUnsupported, providerName, opMeta.ID))
		}
		reader, err := streamExec.ExecuteStream(ctx, operation, params, accessToken)
		if err != nil {
			b.persistReconnectRequired(ctx, p, providerName, conn, instance, err)
			return fail(err)
		}
		spanOwned = true
		return &InvokeOutcome{Stream: newObservingStreamReader(
			ctx,
			reader,
			b,
			span,
			startedAt,
			p,
			providerName,
			opMeta.ID,
			metricutil.AttrValue(transport),
		)}, nil
	}

	result, err := prov.Execute(ctx, operation, params, accessToken)
	if err != nil {
		b.persistReconnectRequired(ctx, p, providerName, conn, instance, err)
		return fail(err)
	}
	b.observePlugin5xxResult(ctx, span, p, providerName, opMeta.ID, transport, result)
	unaryResult = result
	return &InvokeOutcome{Unary: result}, nil
}

// observingStreamReader wraps a StreamReader, inspects metadata frames for 5xx
// status, and records the invocation when the stream reaches its terminal
// frame or error. A second metadata frame is the protocol's terminal error
// frame, so it must finalize the record before the caller returns it. The
// context watcher and StreamFinalizer hook cover consumers that stop reading
// because the client disconnected or the transport failed to send a frame.
type observingStreamReader struct {
	inner           core.StreamReader
	broker          *Broker
	ctx             context.Context
	span            trace.Span
	startedAt       time.Time
	principal       *principal.Principal
	providerName    string
	operation       string
	transport       string
	mu              sync.Mutex
	metadataSeen    bool
	frameSeen       bool
	terminalPending bool
	finished        bool
	stopWatch       func() bool
	resultStatus    int
	resultErr       error
}

func newObservingStreamReader(
	ctx context.Context,
	inner core.StreamReader,
	broker *Broker,
	span trace.Span,
	startedAt time.Time,
	p *principal.Principal,
	providerName, operation, transport string,
) *observingStreamReader {
	reader := &observingStreamReader{
		inner:        inner,
		broker:       broker,
		ctx:          ctx,
		span:         span,
		startedAt:    startedAt,
		principal:    p,
		providerName: providerName,
		operation:    operation,
		transport:    transport,
	}
	reader.stopWatch = context.AfterFunc(ctx, func() {
		reader.finish(ctx.Err(), false, false)
	})
	return reader
}

func (r *observingStreamReader) Recv() (*core.InvokeFrame, error) {
	frame, err := r.inner.Recv()
	if err != nil {
		r.markTerminal(err)
		r.endSpan()
		return nil, err
	}
	if frame == nil {
		r.markTerminal(nil)
		r.endSpan()
		return nil, nil
	}
	r.mu.Lock()
	terminalMetadata := frame.Metadata != nil && r.frameSeen
	r.frameSeen = true
	if frame.Metadata != nil {
		r.metadataSeen = true
		result := &core.OperationResult{
			Status:  frame.Metadata.Status,
			Headers: frame.Metadata.Headers,
			Body:    frame.Data,
		}
		r.resultStatus = result.Status
		r.mu.Unlock()
		if terminalMetadata {
			r.markTerminal(nil)
		}
		r.broker.observePlugin5xxResult(r.ctx, r.span, r.principal, r.providerName, r.operation, r.transport, result)
		if terminalMetadata {
			r.endSpan()
		}
		return frame, nil
	}
	r.mu.Unlock()
	return frame, nil
}

func (r *observingStreamReader) markTerminal(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finished {
		return
	}
	r.terminalPending = true
	if err != nil && !errors.Is(err, io.EOF) && r.resultErr == nil {
		r.resultErr = err
	}
}

// FinalizeStream records an invocation when its transport stops consuming the
// reader before Recv can observe a terminal frame. It is safe to call more
// than once; only the first completion records the request.
func (r *observingStreamReader) FinalizeStream(err error) {
	if errors.Is(err, io.EOF) {
		r.endSpan()
		return
	}
	r.finish(err, false, true)
}

func (r *observingStreamReader) terminalResultStatusLocked() int {
	if validHTTPStatus(r.resultStatus) {
		return r.resultStatus
	}
	if r.metadataSeen || r.frameSeen {
		// The HTTP streaming writer commits 200 when a frame has arrived but
		// carries no explicit status.
		return http.StatusOK
	}
	if r.resultErr != nil {
		return operationResultStatus(nil, r.resultErr)
	}
	return http.StatusInternalServerError
}

// endSpan ends the tracing span and records the invocation exactly once. It is
// called on EOF, error, nil frames, and terminal metadata frames so callers
// that stop after an error frame cannot leave the request unrecorded.
func (r *observingStreamReader) endSpan() {
	r.finish(nil, true, true)
}

func (r *observingStreamReader) finish(err error, natural, stopWatch bool) {
	r.mu.Lock()
	if r.finished || (!natural && r.terminalPending) {
		r.mu.Unlock()
		return
	}
	if err != nil && !errors.Is(err, io.EOF) && r.resultErr == nil {
		r.resultErr = err
	}
	r.finished = true
	resultStatus := r.terminalResultStatusLocked()
	resultErr := r.resultErr
	contextStop := r.stopWatch
	r.mu.Unlock()

	if stopWatch && contextStop != nil {
		contextStop()
	}
	r.broker.recordInvocationRecord(
		r.startedAt,
		r.providerName,
		r.operation,
		resultStatus,
		operationResultFailed(resultStatus, resultErr),
	)
	r.span.End()
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

func truncateResultBodyForLog(body []byte) string {
	if len(body) <= resultBodyLogLimit {
		return string(body)
	}
	return string(body[:resultBodyLogLimit])
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
		b.recordCompletedInvocation(
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

	prov, err := b.providers.GetWithContext(ctx, providerName)
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
	if err := b.checkInvocationOperationAccess(ctx, p, providerName, graphQLOperationID); err != nil {
		return fail(err)
	}
	setSubjectAttribute(p)
	ctx = withResolvedPrincipal(ctx, p)
	if !providerDelegatesRemoteAuthorization(prov) {
		if err := b.checkAuthorizationAccess(ctx, p, providerName, graphQLOperationID); err != nil {
			return fail(err)
		}
	}

	conn := ConnectionFromContext(ctx)
	if conn == "" && b.connMapper != nil {
		conn = b.connMapper.ConnectionForProvider(providerName)
	}
	metricConnectionMode = metricutil.NormalizeConnectionMode(b.resolveConnectionMode(ctx, prov, providerName, conn))
	span.SetAttributes(attrConnectionMode.String(metricConnectionMode))

	ctx, err = ApplyInvokeHeaderOverrides(ctx, prov)
	if err != nil {
		return fail(err)
	}
	ctx, accessToken, err := b.resolveToken(ctx, prov, p, providerName, conn, instance)
	if err != nil {
		b.persistReconnectRequired(ctx, p, providerName, conn, instance, err)
		return fail(err)
	}
	result, err = graphQLProv.InvokeGraphQL(ctx, request, accessToken)
	if err != nil {
		b.persistReconnectRequired(ctx, p, providerName, conn, instance, err)
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
	if !principal.AllowsProviderPermission(p, providerName) {
		return ctx, "", fmt.Errorf("%w: %s", ErrScopeDenied, providerName)
	}
	ctx = withResolvedPrincipal(ctx, p)
	prov, err := b.providers.GetWithContext(ctx, providerName)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return ctx, "", fmt.Errorf("%w: %q", ErrProviderNotFound, providerName)
		}
		return ctx, "", fmt.Errorf("%w: looking up provider: %v", ErrInternal, err)
	}
	return b.resolveToken(ctx, prov, p, providerName, connection, instance)
}

func providerDelegatesRemoteAuthorization(prov core.Provider) bool {
	if prov == nil {
		return false
	}
	delegated, ok := prov.(RemoteCredentialDelegated)
	return ok && delegated.RemoteCredentialDelegated()
}

func (b *Broker) providerDelegatesRemoteAuthorization(ctx context.Context, providerName string) bool {
	if b == nil || b.providers == nil {
		return false
	}
	provider, err := b.providers.GetWithContext(ctx, providerName)
	return err == nil && providerDelegatesRemoteAuthorization(provider)
}

func (b *Broker) authorizationDecision(
	ctx context.Context,
	p *principal.Principal,
	providerName string,
	operationID string,
) (*proto.CheckAccessResponse, error) {
	subjectID, err := principal.ResolveCredentialSubjectID(ctx, b.users, p)
	if err != nil {
		return nil, err
	}
	resource := b.authorizationResource(ctx, providerName)
	return b.authorization.CheckAccess(
		ctx,
		accessRequest(p, subjectID, resource, operationID),
	)
}

// authorizationMapper exposes the shared app key -> policy alias -> resource
// mapping so invocation and the HTTP surfaces resolve identically.
func (b *Broker) authorizationMapper() AuthorizationResourceMapper {
	if b == nil {
		return AuthorizationResourceMapper{}
	}
	return NewAuthorizationResourceMapper(b.providerKinds, b.authorizationPolicies)
}

func (b *Broker) authorizationPolicy(providerName string) string {
	return b.authorizationMapper().Policy(providerName)
}

func (b *Broker) authorizationResource(ctx context.Context, providerName string) *proto.Resource {
	providerName = strings.TrimSpace(providerName)
	mapper := b.authorizationMapper()
	if b != nil {
		if strings.TrimSpace(b.authorizationPolicies[providerName]) != "" {
			return mapper.Resource(providerName)
		}
		if b.providers != nil {
			if _, err := b.providers.GetWithContext(ctx, providerName); err == nil {
				return &proto.Resource{Type: string(ProviderKindApp), Id: providerName}
			}
		}
		if b.providerKinds[providerName] != "" {
			return mapper.Resource(providerName)
		}
	}
	return mapper.Resource(providerName)
}

func (b *Broker) CheckOperationAccess(ctx context.Context, p *principal.Principal, providerName, operationID string) error {
	if !principal.AllowsOperationPermission(p, providerName, operationID) {
		return fmt.Errorf("%w: %s.%s", ErrAuthorizationDenied, providerName, operationID)
	}
	if err := b.checkAppAccess(ctx, p, providerName, operationID); err != nil {
		return err
	}
	if b.providerDelegatesRemoteAuthorization(ctx, providerName) {
		return nil
	}
	return b.checkAuthorizationAccess(ctx, p, providerName, operationID)
}

// checkInvocationOperationAccess is the shared caller-side operation gate for
// every invocation mode. Workspace authorization is applied separately because
// remote-delegated providers own that decision, but user app capabilities and
// token operation scopes must be enforced before any provider dispatch.
func (b *Broker) checkInvocationOperationAccess(ctx context.Context, p *principal.Principal, providerName, operationID string) error {
	if !principal.AllowsOperationPermission(p, providerName, operationID) {
		return fmt.Errorf("%w: %s.%s", ErrScopeDenied, providerName, operationID)
	}
	return b.checkAppAccess(ctx, p, providerName, operationID)
}

func (b *Broker) checkAppAccess(ctx context.Context, p *principal.Principal, providerName, operationID string) error {
	if b == nil || b.appAccessProfiles == nil || p == nil || principal.IsNonUserPrincipal(p) {
		return nil
	}
	subjectID, err := principal.ResolveAuthorizationSubjectID(ctx, b.users, p)
	if err != nil {
		if !errors.Is(err, principal.ErrOpaqueCredentialSubject) {
			return fmt.Errorf("%w: %s.%s: %v", ErrAuthorizationDenied, providerName, operationID, err)
		}
		subjectID = legacyAppAccessSubjectID(p)
		if subjectID == "" {
			return fmt.Errorf("%w: %s.%s: %v", ErrAuthorizationDenied, providerName, operationID, err)
		}
		// Older local callers can carry a non-UUID user ID. Preserve their
		// existing no-profile allow behavior, but honor a legacy raw profile
		// if one exists. Provider-namespaced opaque subjects are rejected above
		// instead of being allowed to bypass a canonical profile.
	}
	profile, err := b.appAccessProfiles.GetAppAccessProfile(ctx, subjectID, providerName)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("%w: %s.%s: %v", ErrAuthorizationDenied, providerName, operationID, err)
	}
	for _, enabled := range profile.EnabledOperations {
		if strings.TrimSpace(enabled) == strings.TrimSpace(operationID) {
			return nil
		}
	}
	return fmt.Errorf("%w: %s.%s", ErrAuthorizationDenied, providerName, operationID)
}

func legacyAppAccessSubjectID(p *principal.Principal) string {
	p = principal.Canonicalized(p)
	if p == nil || p.Kind != principal.KindUser {
		return ""
	}
	subjectID := strings.TrimSpace(p.SubjectID)
	userID := strings.TrimSpace(principal.UserIDFromSubjectID(subjectID))
	if userID == "" || strings.ContainsAny(userID, "|:/\\") {
		return ""
	}
	return subjectID
}

func (b *Broker) CheckProviderAccess(ctx context.Context, p *principal.Principal, providerName string) error {
	if !principal.AllowsProviderPermission(p, providerName) {
		return fmt.Errorf("%w: %s", ErrAuthorizationDenied, providerName)
	}
	if b.providerDelegatesRemoteAuthorization(ctx, providerName) {
		return nil
	}
	if b == nil || b.authorization == nil {
		return nil
	}
	decision, err := b.authorizationDecision(ctx, p, providerName, providerName)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrAuthorizationDenied, providerName, err)
	}
	if decision == nil || !decision.GetAllowed() {
		return fmt.Errorf("%w: %s", ErrAuthorizationDenied, providerName)
	}
	return nil
}

func accessRequest(p *principal.Principal, subjectID string, resource *proto.Resource, action string) *proto.CheckAccessRequest {
	req := SubjectAccessRequest(subjectID, action, resource)
	req.Subject.Properties = subjectAccessProperties(p)
	return req
}

// subjectAccessProperties is the subject metadata the evaluator sees for an
// invocation decision. Batched listing decisions reuse it so a listed operation
// is judged with the same subject the invocation decision judges.
func subjectAccessProperties(p *principal.Principal) *structpb.Struct {
	p = principal.Canonicalized(p)
	properties, _ := structpb.NewStruct(map[string]any{
		"scope":     strings.Join(p.Scopes, " "),
		"client_id": strings.TrimSpace(p.ClientID),
		"audience":  append([]string(nil), p.Audience...),
	})
	return properties
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

// chosenCredentialInstance returns the account the subject has chosen for this
// connection and whether a choice exists. Credentials with the same explicit
// provider account key count as one account, while keyless credentials remain
// distinct. Preferred wins among usable credentials; otherwise a sole account
// is implicitly chosen (including empty qualifier). ok=false means zero or
// ambiguous accounts — the connection is not connected.
func chosenCredentialInstance(credentials []*core.ExternalCredential, preferred string) (instance string, ok bool) {
	return core.ChooseCredentialInstance(credentials, preferred, time.Now())
}

func nonNilCredentials(credentials []*core.ExternalCredential) []*core.ExternalCredential {
	out := make([]*core.ExternalCredential, 0, len(credentials))
	for _, credential := range credentials {
		if credential != nil {
			out = append(out, credential)
		}
	}
	return out
}

// resolveChosenInstance fills an empty instance from the subject's chosen
// account for connectionID (validated preferred, else sole credential). A
// non-empty instance is returned unchanged. ok=false from
// chosenCredentialInstance leaves instance empty so callers surface
// ambiguity the same way as local resolve.
func (b *Broker) resolveChosenInstance(ctx context.Context, subjectID, connectionID, instance string) (string, error) {
	instance = strings.TrimSpace(instance)
	if instance != "" {
		return instance, nil
	}
	if b == nil || core.ExternalCredentialProviderMissing(b.externalCreds) {
		return "", fmt.Errorf("%w: external credentials provider is not configured", ErrInternal)
	}
	credentials, listErr := b.externalCreds.ListCredentials(ctx, subjectID, connectionID)
	if listErr != nil {
		return "", fmt.Errorf("%w: listing external credentials: %v", ErrInternal, listErr)
	}
	preferred := ""
	if b.connectionInstancePreferences != nil {
		var prefErr error
		preferred, prefErr = b.connectionInstancePreferences.PreferredInstance(ctx, subjectID, connectionID)
		if prefErr != nil {
			return "", fmt.Errorf("%w: resolving preferred instance: %v", ErrInternal, prefErr)
		}
	}
	if chosen, ok := chosenCredentialInstance(credentials, preferred); ok {
		return chosen, nil
	}
	return "", nil
}

func (b *Broker) ExpandCatalogTargets(ctx context.Context, p *principal.Principal, providerName string, targets []CatalogResolutionTarget) ([]CatalogResolutionTarget, error) {
	if len(targets) == 0 {
		targets = []CatalogResolutionTarget{{}}
	}
	if !principal.AllowsProviderPermission(p, providerName) {
		return nil, fmt.Errorf("%w: %s", ErrScopeDenied, providerName)
	}
	ctx = withResolvedPrincipal(ctx, p)
	prov, err := b.providers.GetWithContext(ctx, providerName)
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
	subjectID, err := principal.ResolveCredentialSubjectID(ctx, b.users, p)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUserResolution, err)
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
		credentials, listErr := b.externalCreds.ListCredentials(ctx, subjectID, connectionID)
		if listErr != nil {
			return nil, fmt.Errorf("%w: listing external credentials: %v", ErrInternal, listErr)
		}
		preferred := ""
		if b.connectionInstancePreferences != nil {
			var prefErr error
			preferred, prefErr = b.connectionInstancePreferences.PreferredInstance(ctx, subjectID, connectionID)
			if prefErr != nil {
				return nil, fmt.Errorf("%w: resolving preferred instance: %v", ErrInternal, prefErr)
			}
		}
		chosen, ok := chosenCredentialInstance(credentials, preferred)
		if ok {
			resolved := CatalogResolutionTarget{
				Connection: target.Connection,
				Instance:   chosen,
			}
			if _, seenAlready := seen[resolved]; !seenAlready {
				seen[resolved] = struct{}{}
				expanded = append(expanded, resolved)
			}
			continue
		}
		if len(nonNilCredentials(credentials)) == 0 {
			if _, seenAlready := seen[target]; !seenAlready {
				seen[target] = struct{}{}
				expanded = append(expanded, target)
			}
			continue
		}
		// Multiple accounts without a valid chosen preferred → not connected.
	}
	return expanded, nil
}

func (b *Broker) resolveToken(ctx context.Context, prov core.Provider, p *principal.Principal, providerName, connection, instance string) (context.Context, string, error) {
	mode := b.resolveConnectionMode(ctx, prov, providerName, connection)
	switch mode {
	case core.ConnectionModeNone:
		SetCredentialAudit(ctx, core.ConnectionModeNone, "", "", "")
		ctx = WithCredentialContext(ctx, CredentialContext{Mode: core.ConnectionModeNone})
		return ctx, "", nil

	case core.ConnectionModeSubject:
		subjectID, err := principal.ResolveCredentialSubjectID(ctx, b.users, p)
		if err != nil {
			return ctx, "", fmt.Errorf("%w: %v", ErrUserResolution, err)
		}
		if delegated, ok := prov.(RemoteCredentialDelegated); ok && delegated.RemoteCredentialDelegated() {
			connection = core.ResolveConnectionAlias(connection)
			if connection == "" {
				connection = core.AppConnectionName
			}
			// Same chosen-account gate as local resolve / catalog expand: empty
			// _instance must resolve to preferred-or-sole before forwarding
			// credential context to the remote app.
			instance, err = b.resolveChosenInstance(ctx, subjectID, b.connectionID(providerName, connection), instance)
			if err != nil {
				return ctx, "", err
			}
			SetCredentialAudit(ctx, core.ConnectionModeSubject, subjectID, connection, instance)
			ctx = WithCredentialContext(ctx, CredentialContext{
				Mode:       core.ConnectionModeSubject,
				SubjectID:  subjectID,
				Connection: connection,
				Instance:   instance,
			})
			return ctx, "", nil
		}
		return b.resolveSubjectCredential(ctx, prov, subjectID, providerName, connection, instance, core.ConnectionModeSubject, subjectID)

	default:
		return ctx, "", fmt.Errorf("%w: unknown connection mode %q", ErrInternal, mode)
	}
}

func (b *Broker) ResolveRuntimeConnectionCredential(ctx context.Context, p *principal.Principal, providerName, connection, instance string) (context.Context, ConnectionRuntimeCredential, ConnectionRuntimeInfo, error) {
	if !InternalConnectionAccessFromContext(ctx) {
		return ctx, ConnectionRuntimeCredential{}, ConnectionRuntimeInfo{}, fmt.Errorf("%w: runtime connection credential resolution requires internal access", ErrAuthorizationDenied)
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
		subjectID, err := principal.ResolveCredentialSubjectID(ctx, b.users, p)
		if err != nil {
			return ctx, ConnectionRuntimeCredential{}, info, fmt.Errorf("%w: %v", ErrUserResolution, err)
		}
		resolvedCtx, credential, err := b.resolveSubjectRuntimeCredential(ctx, nil, subjectID, providerName, connection, instance, core.ConnectionModeSubject, subjectID)
		return resolvedCtx, credential, info, err

	default:
		return ctx, ConnectionRuntimeCredential{}, info, fmt.Errorf("%w: unknown connection mode %q", ErrInternal, info.Mode)
	}
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
	instance, err := b.resolveChosenInstance(ctx, subjectID, connectionID, instance)
	if err != nil {
		return ctx, ConnectionRuntimeCredential{}, err
	}
	credentialConnection := strings.TrimSpace(connection)
	if credentialConnection == "" {
		credentialConnection = core.AppConnectionName
	}
	// Preserve the resolved instance before the provider call can fail. A
	// reconnect response may arrive while resolving a stored grant, before the
	// successful-response path below has a credential to attach to context.
	ctx = WithCredentialContext(ctx, CredentialContext{
		Mode:       credentialMode,
		SubjectID:  credentialSubjectID,
		Connection: credentialConnection,
		Instance:   instance,
	})

	runtimeInfo := ConnectionRuntimeInfo{}
	if b.connectionRuntime != nil {
		runtimeInfo, _ = b.connectionRuntime(providerName, connection)
	}

	authConfig := runtimeInfo.AuthConfig
	if runtimeInfo.AuthConfigResolver != nil {
		resolved, resolverErr := runtimeInfo.AuthConfigResolver(ctx)
		if resolverErr != nil {
			// Serve the stored credential without refresh capability rather
			// than failing the invocation on a discovery outage.
			b.log().WarnContext(ctx, "resolving connection auth config", "provider", providerName, "connection", connection, "error", resolverErr)
		} else {
			authConfig = resolved
		}
	}

	resp, err := b.externalCreds.ResolveCredential(ctx, &core.ResolveExternalCredentialRequest{
		Provider:            providerName,
		Connection:          connection,
		ConnectionID:        connectionID,
		Mode:                credentialMode,
		CredentialSubjectID: subjectID,
		Instance:            instance,
		Auth:                authConfig,
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
	SetCredentialAudit(ctx, credentialMode, credentialSubjectID, credentialConnection, storedCredential.Qualifier)
	ctx = WithCredentialContext(ctx, CredentialContext{
		Mode:       credentialMode,
		SubjectID:  credentialSubjectID,
		Connection: credentialConnection,
		Instance:   storedCredential.Qualifier,
	})

	metadataJSON := storedCredential.MetadataJSON
	if resp.MetadataJSON != "" {
		metadataJSON = resp.MetadataJSON
	}
	if metadataJSON != "" {
		connParams, err := core.ConnectionParamsFromMetadataJSON(metadataJSON)
		if err != nil {
			b.log().WarnContext(ctx, "malformed metadata JSON", "provider", providerName, "error", err)
		} else if len(connParams) > 0 {
			ctx = core.WithConnectionParams(ctx, connParams)
		}
	}
	if len(resp.Params) > 0 {
		ctx = core.WithConnectionParams(ctx, resp.Params)
	}

	return ctx, ConnectionRuntimeCredential{Token: strings.TrimSpace(resp.Token), ExpiresAt: resp.ExpiresAt}, nil
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
