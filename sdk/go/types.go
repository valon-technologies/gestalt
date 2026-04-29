package gestalt

import (
	"context"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
)

// ProviderKind identifies the protocol surface a provider implements.
type ProviderKind string

const (
	// ProviderKindIntegration is the main executable provider surface used by
	// integration plugins.
	ProviderKindIntegration ProviderKind = "integration"
	// ProviderKindAuthentication serves interactive login and token-validation
	// flows.
	ProviderKindAuthentication ProviderKind = "authentication"
	// ProviderKindAuthorization serves authorization decision and control-plane flows.
	ProviderKindAuthorization ProviderKind = "authorization"
	// ProviderKindIndexedDB serves the IndexedDB-style datastore surface.
	ProviderKindIndexedDB ProviderKind = "datastore"
	// ProviderKindCache serves the cache surface.
	ProviderKindCache ProviderKind = "cache"
	// ProviderKindSecrets serves secret lookups.
	ProviderKindSecrets ProviderKind = "secrets"
	// ProviderKindS3 serves the S3-compatible object storage surface.
	ProviderKindS3 ProviderKind = "s3"
	// ProviderKindWorkflow serves the workflow orchestration surface.
	ProviderKindWorkflow ProviderKind = "workflow"
	// ProviderKindAgent serves the agent run surface.
	ProviderKindAgent ProviderKind = "agent"
	// ProviderKindExternalCredential serves the external-credential surface.
	ProviderKindExternalCredential ProviderKind = "external_credential"
	// ProviderKindRuntime serves hosted executable-plugin runtime management.
	ProviderKindRuntime ProviderKind = "runtime"
)

// ProviderMetadata describes a provider instance independent of its concrete
// kind.
type ProviderMetadata struct {
	Kind        ProviderKind
	Name        string
	DisplayName string
	Description string
	Version     string
}

// Provider is the common startup contract shared by every provider kind.
type Provider interface {
	Configure(ctx context.Context, name string, config map[string]any) error
}

// MetadataProvider is implemented by providers that can describe themselves
// without consulting the manifest or host-side configuration.
type MetadataProvider interface {
	Metadata() ProviderMetadata
}

// HealthChecker is implemented by providers that can report their runtime health
// to the host.
type HealthChecker interface {
	HealthCheck(ctx context.Context) error
}

// Starter is implemented by providers that need an explicit post-configuration
// start phase. The context only bounds synchronous startup work; long-running
// background work must own its own cancellation and stop from Close.
type Starter interface {
	Start(ctx context.Context) error
}

// Closer is implemented by providers that need explicit shutdown handling.
type Closer interface {
	Close() error
}

// WarningsProvider is implemented by providers that expose non-fatal startup or
// environment warnings the host should surface.
type WarningsProvider interface {
	Warnings() []string
}

// SessionCatalogProvider is implemented by integration providers that derive
// additional operations from the current request context.
type SessionCatalogProvider interface {
	CatalogForRequest(ctx context.Context, token string) (*proto.Catalog, error)
}

// ConnectedToken is the normalized connection payload passed into post-connect
// hooks after the host completes the credential exchange.
type ConnectedToken struct {
	ID                string
	SubjectID         string
	Integration       string
	Connection        string
	Instance          string
	AccessToken       string
	RefreshToken      string
	Scopes            string
	ExpiresAt         *time.Time
	LastRefreshedAt   *time.Time
	RefreshErrorCount int
	MetadataJSON      string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// PostConnectCapable is implemented by integration providers that derive
// additional connection metadata after the host finishes the OAuth/manual
// credential exchange.
type PostConnectCapable interface {
	PostConnect(ctx context.Context, token *ConnectedToken) (map[string]string, error)
}

// Subject identifies the caller that initiated an operation.
type Subject struct {
	ID          string
	Kind        string
	DisplayName string
	AuthSource  string
}

// Credential describes the resolved connection or identity used to authorize a
// request.
type Credential struct {
	Mode       string
	SubjectID  string
	Connection string
	Instance   string
}

// Access summarizes the host-side policy decision attached to a request.
type Access struct {
	Policy string
	Role   string
}

// OperationResult is the serialized result returned by the provider runtime.
type OperationResult struct {
	Status int
	Body   string
}

type connectionParamsKey struct{}
type subjectKey struct{}
type credentialKey struct{}
type accessKey struct{}
type idempotencyKeyKey struct{}
type invocationTokenKey struct{}
type workflowKey struct{}

// WithConnectionParams returns a child context carrying the given connection
// parameters. The host calls this before invoking an executable operation so
// providers can retrieve resolved credentials via [ConnectionParams].
func WithConnectionParams(ctx context.Context, params map[string]string) context.Context {
	return context.WithValue(ctx, connectionParamsKey{}, params)
}

// ConnectionParams extracts the connection parameters stored by
// [WithConnectionParams]. Returns nil if none are present.
func ConnectionParams(ctx context.Context) map[string]string {
	params, _ := ctx.Value(connectionParamsKey{}).(map[string]string)
	return params
}

// WithSubject returns a child context carrying the current caller identity.
func WithSubject(ctx context.Context, subject Subject) context.Context {
	return context.WithValue(ctx, subjectKey{}, subject)
}

// SubjectFromContext extracts the current caller identity from ctx.
func SubjectFromContext(ctx context.Context) Subject {
	subject, _ := ctx.Value(subjectKey{}).(Subject)
	return subject
}

// WithCredential returns a child context carrying the resolved credential
// context for the current request.
func WithCredential(ctx context.Context, credential Credential) context.Context {
	return context.WithValue(ctx, credentialKey{}, credential)
}

// CredentialFromContext extracts the resolved credential context from ctx.
func CredentialFromContext(ctx context.Context) Credential {
	credential, _ := ctx.Value(credentialKey{}).(Credential)
	return credential
}

// WithAccess returns a child context carrying the host-side access decision for
// the current request.
func WithAccess(ctx context.Context, access Access) context.Context {
	return context.WithValue(ctx, accessKey{}, access)
}

// AccessFromContext extracts the host-side access decision from ctx.
func AccessFromContext(ctx context.Context) Access {
	access, _ := ctx.Value(accessKey{}).(Access)
	return access
}

// WithIdempotencyKey returns a child context carrying a caller-supplied
// idempotency key for the current operation.
func WithIdempotencyKey(ctx context.Context, key string) context.Context {
	key = strings.TrimSpace(key)
	if key == "" {
		return ctx
	}
	return context.WithValue(ctx, idempotencyKeyKey{}, key)
}

// IdempotencyKeyFromContext extracts the current operation idempotency key from
// ctx. It returns an empty string when the caller did not supply one.
func IdempotencyKeyFromContext(ctx context.Context) string {
	key, _ := ctx.Value(idempotencyKeyKey{}).(string)
	return strings.TrimSpace(key)
}

func withInvocationToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, invocationTokenKey{}, token)
}

func invocationTokenFromContext(ctx context.Context) string {
	token, _ := ctx.Value(invocationTokenKey{}).(string)
	return token
}

func InvocationTokenFromContext(ctx context.Context) string {
	return invocationTokenFromContext(ctx)
}

// WithWorkflowContext attaches workflow callback metadata to the context.
// The workflow object uses a JSON-style lowerCamelCase shape such as runId,
// target.plugin.pluginName, trigger.scheduleId, and trigger.event.specVersion.
func WithWorkflowContext(ctx context.Context, workflow map[string]any) context.Context {
	return context.WithValue(ctx, workflowKey{}, workflow)
}

// WorkflowContextFromContext returns workflow callback metadata attached by
// WithWorkflowContext. The workflow object uses a JSON-style lowerCamelCase
// shape such as runId, target.plugin.pluginName, trigger.scheduleId, and
// trigger.event.specVersion.
func WorkflowContextFromContext(ctx context.Context) map[string]any {
	workflow, _ := ctx.Value(workflowKey{}).(map[string]any)
	return workflow
}
