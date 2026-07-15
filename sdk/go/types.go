package gestalt

import (
	"context"
	"net/http"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/go/migrations"
)

// ProviderKind identifies the protocol surface a provider implements.
type ProviderKind string

const (
	// ProviderKindApp is the main executable provider surface used by
	// integration apps.
	ProviderKindApp ProviderKind = "integration"
	// ProviderKindIdentity serves OAuth 2.0 / OpenID Connect identity flows.
	ProviderKindIdentity ProviderKind = "identity"
	// ProviderKindAuthorization serves generic authorization model,
	// relationship, and access-check flows.
	ProviderKindAuthorization ProviderKind = "authorization"
	// ProviderKindIndexedDB serves the IndexedDB surface.
	ProviderKindIndexedDB ProviderKind = "indexeddb"
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
	ProviderKindExternalCredential ProviderKind = "externalcredentials"
	// ProviderKindRuntime serves hosted executable-runtime management.
	ProviderKindRuntime ProviderKind = "runtime"
	// ProviderKindTest serves the minimal provider surface used to validate
	// provider registration and invocation.
	ProviderKindTest ProviderKind = "test"
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

// MigrationsProvider is implemented by providers that run IndexedDB migrations
// before configuration.
type MigrationsProvider interface {
	Provider
	MigrationOptions(ctx context.Context, name string, config map[string]any) (migrations.RunOptions, string, error)
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
	CatalogForRequest(ctx context.Context, token string) (*Catalog, error)
}

// WorkflowDeclarationsProvider is implemented by integration providers that
// declare static workflow definitions reconciled by the host.
type WorkflowDeclarationsProvider interface {
	DeclaredWorkflowDefinitions() ([]WorkflowDefinitionSpec, error)
}

// Subject identifies the caller that initiated an operation.
type Subject struct {
	ID          string
	Email       string
	DisplayName string
	Scopes      []string
	Permissions []SubjectPermission
}

// SubjectPermission bounds which provider operations the current caller can
// reach. An empty Operations list means every operation for the app.
type SubjectPermission struct {
	App        string
	Operations []string
}

// Credential describes the resolved credential used to authorize a request.
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

// Host describes public host metadata attached to a request.
type Host struct {
	PublicBaseURL string
}

// OperationResult is the serialized result returned by the provider runtime.
type OperationResult struct {
	Status  int
	Headers http.Header
	Body    []byte
}

// OK reports whether Status is in the HTTP 2xx range.
func (r *OperationResult) OK() bool {
	return r != nil && r.Status >= 200 && r.Status < 300
}

// Bytes returns a copy of Body.
func (r *OperationResult) Bytes() []byte {
	if r == nil || r.Body == nil {
		return nil
	}
	return append([]byte(nil), r.Body...)
}

// Text decodes Body as UTF-8 text.
func (r *OperationResult) Text() string {
	if r == nil {
		return ""
	}
	return string(r.Body)
}

type connectionParamsKey struct{}
type subjectKey struct{}
type agentSubjectKey struct{}
type credentialKey struct{}
type accessKey struct{}
type hostKey struct{}
type idempotencyKeyKey struct{}
type workflowKey struct{}
type toolRefsKey struct{}

type toolRefsContext struct {
	Set  bool
	Refs []AgentToolRef
}

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

// WithAgentSubject returns a child context carrying the original agent caller
// when an agent tool executes under a delegated identity.
func WithAgentSubject(ctx context.Context, subject Subject) context.Context {
	return context.WithValue(ctx, agentSubjectKey{}, subject)
}

// AgentSubjectFromContext extracts the original agent caller for delegated
// agent tool requests. It returns the zero value when the request was not
// delegated from an agent.
func AgentSubjectFromContext(ctx context.Context) Subject {
	subject, _ := ctx.Value(agentSubjectKey{}).(Subject)
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

// WithHostContext returns a child context carrying public host metadata for the
// current request.
func WithHostContext(ctx context.Context, host Host) context.Context {
	return context.WithValue(ctx, hostKey{}, host)
}

// HostContextFromContext extracts public host metadata from ctx.
func HostContextFromContext(ctx context.Context) Host {
	host, _ := ctx.Value(hostKey{}).(Host)
	return host
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

// WithWorkflowContext attaches workflow callback metadata to the context.
// The workflow object uses a JSON-style lowerCamelCase shape such as runId,
// target.steps[0].app.name, trigger.activationId, and trigger.event.specVersion.
func WithWorkflowContext(ctx context.Context, workflow map[string]any) context.Context {
	return context.WithValue(ctx, workflowKey{}, workflow)
}

// WorkflowContextFromContext returns workflow callback metadata attached by
// WithWorkflowContext. The workflow object uses a JSON-style lowerCamelCase
// shape such as runId, target.steps[0].app.name, trigger.activationId, and
// trigger.event.specVersion.
func WorkflowContextFromContext(ctx context.Context) map[string]any {
	workflow, _ := ctx.Value(workflowKey{}).(map[string]any)
	return workflow
}

// WithToolRefsContext attaches the agent tool refs granted to the current
// operation request.
func WithToolRefsContext(ctx context.Context, refs []AgentToolRef) context.Context {
	return context.WithValue(ctx, toolRefsKey{}, toolRefsContext{
		Set:  true,
		Refs: copyAgentToolRefs(refs),
	})
}

// ToolRefsFromContext returns a defensive copy of the request tool refs.
func ToolRefsFromContext(ctx context.Context) []AgentToolRef {
	refs, _ := ctx.Value(toolRefsKey{}).(toolRefsContext)
	return copyAgentToolRefs(refs.Refs)
}

// ToolRefsSetFromContext reports whether the host attached a tool-ref context
// to the current operation request.
func ToolRefsSetFromContext(ctx context.Context) bool {
	refs, _ := ctx.Value(toolRefsKey{}).(toolRefsContext)
	return refs.Set
}

func copyAgentToolRefs(refs []AgentToolRef) []AgentToolRef {
	if len(refs) == 0 {
		return nil
	}
	out := make([]AgentToolRef, 0, len(refs))
	for _, ref := range refs {
		copied := ref
		if ref.RunAs != nil {
			runAs := *ref.RunAs
			copied.RunAs = &runAs
		}
		out = append(out, copied)
	}
	return out
}
