package invocation

import (
	"context"
	"net"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/authorization"
)

type InvocationMeta struct {
	RequestID string
	Depth     int
	CallChain []string // "provider/operation" entries
}

type invocationMetaKey struct{}

func MetaFromContext(ctx context.Context) *InvocationMeta {
	meta, _ := ctx.Value(invocationMetaKey{}).(*InvocationMeta)
	return meta
}

func ContextWithMeta(ctx context.Context, meta *InvocationMeta) context.Context {
	return context.WithValue(ctx, invocationMetaKey{}, meta)
}

func ensureMeta(ctx context.Context) (context.Context, *InvocationMeta) {
	meta := MetaFromContext(ctx)
	if meta != nil {
		return ctx, meta
	}
	meta = &InvocationMeta{RequestID: uuid.NewString()}
	return ContextWithMeta(ctx, meta), meta
}

type requestMetaCtxKey struct{}
type auditEntryCtxKey struct{}
type runAsAuditCtxKey struct{}

type RequestMeta struct {
	ClientIP   string
	RemoteAddr string
	UserAgent  string
}

type RunAsAuditContext struct {
	AgentSubject *core.RunAsSubject
	RunAsSubject *core.RunAsSubject
}

type CredentialContext struct {
	Mode       core.ConnectionMode
	SubjectID  string
	Connection string
	Instance   string
}

type HostContext struct {
	PublicBaseURL string
}

type invocationSurfaceCtxKey struct{}
type httpBindingCtxKey struct{}
type credentialCtxKey struct{}
type accessCtxKey struct{}
type hostCtxKey struct{}
type workflowCtxKey struct{}
type internalConnectionAccessCtxKey struct{}

type InvocationSurface string

type AccessContext = authorization.AccessContext

const (
	InvocationSurfaceHTTP        InvocationSurface = "http"
	InvocationSurfaceHTTPBinding InvocationSurface = "http_binding"
	InvocationSurfaceMCP         InvocationSurface = "mcp"
)

func WithRequestMeta(ctx context.Context, meta RequestMeta) context.Context {
	return context.WithValue(ctx, requestMetaCtxKey{}, meta)
}

func RequestMetaFromContext(ctx context.Context) RequestMeta {
	m, _ := ctx.Value(requestMetaCtxKey{}).(RequestMeta)
	return m
}

func withAuditEntry(ctx context.Context, entry *core.AuditEntry) context.Context {
	return context.WithValue(ctx, auditEntryCtxKey{}, entry)
}

func auditEntryFromContext(ctx context.Context) *core.AuditEntry {
	entry, _ := ctx.Value(auditEntryCtxKey{}).(*core.AuditEntry)
	return entry
}

func WithRunAsAudit(ctx context.Context, agentSubject, runAsSubject *core.RunAsSubject) context.Context {
	runAsSubject = core.NormalizeRunAsSubject(runAsSubject)
	if runAsSubject == nil {
		return ctx
	}
	audit := RunAsAuditContext{
		AgentSubject: core.NormalizeRunAsSubject(agentSubject),
		RunAsSubject: runAsSubject,
	}
	return context.WithValue(ctx, runAsAuditCtxKey{}, audit)
}

func RunAsAuditFromContext(ctx context.Context) RunAsAuditContext {
	audit, _ := ctx.Value(runAsAuditCtxKey{}).(RunAsAuditContext)
	audit.AgentSubject = core.NormalizeRunAsSubject(audit.AgentSubject)
	audit.RunAsSubject = core.NormalizeRunAsSubject(audit.RunAsSubject)
	return audit
}

func WithCredentialContext(ctx context.Context, cred CredentialContext) context.Context {
	return context.WithValue(ctx, credentialCtxKey{}, cred)
}

func CredentialContextFromContext(ctx context.Context) CredentialContext {
	cred, _ := ctx.Value(credentialCtxKey{}).(CredentialContext)
	return cred
}

func WithAccessContext(ctx context.Context, access AccessContext) context.Context {
	return context.WithValue(ctx, accessCtxKey{}, access)
}

func AccessContextFromContext(ctx context.Context) AccessContext {
	access, _ := ctx.Value(accessCtxKey{}).(AccessContext)
	return access
}

func WithHostContext(ctx context.Context, host HostContext) context.Context {
	return context.WithValue(ctx, hostCtxKey{}, host)
}

func HostContextFromContext(ctx context.Context) HostContext {
	host, _ := ctx.Value(hostCtxKey{}).(HostContext)
	return host
}

func WithWorkflowContext(ctx context.Context, workflow map[string]any) context.Context {
	return context.WithValue(ctx, workflowCtxKey{}, workflow)
}

func WithWorkflowContextString(ctx context.Context, key, value string) context.Context {
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if key == "" || value == "" {
		return ctx
	}
	workflow := WorkflowContextFromContext(ctx)
	if len(workflow) == 0 {
		return WithWorkflowContext(ctx, map[string]any{key: value})
	}
	if strings.TrimSpace(WorkflowContextString(workflow, key)) == value {
		return ctx
	}
	updated := make(map[string]any, len(workflow)+1)
	for currentKey, currentValue := range workflow {
		updated[currentKey] = currentValue
	}
	updated[key] = value
	return WithWorkflowContext(ctx, updated)
}

func WorkflowContextFromContext(ctx context.Context) map[string]any {
	workflow, _ := ctx.Value(workflowCtxKey{}).(map[string]any)
	return workflow
}

func WorkflowContextMap(value map[string]any, key string) map[string]any {
	raw, ok := value[key]
	if !ok {
		return nil
	}
	typed, _ := raw.(map[string]any)
	return typed
}

func WorkflowContextString(value map[string]any, key string) string {
	raw, ok := value[key]
	if !ok {
		return ""
	}
	typed, _ := raw.(string)
	return typed
}

func WithInvocationSurface(ctx context.Context, surface InvocationSurface) context.Context {
	return context.WithValue(ctx, invocationSurfaceCtxKey{}, surface)
}

func InvocationSurfaceFromContext(ctx context.Context) InvocationSurface {
	surface, _ := ctx.Value(invocationSurfaceCtxKey{}).(InvocationSurface)
	return surface
}

func WithHTTPBinding(ctx context.Context, binding string) context.Context {
	binding = strings.TrimSpace(binding)
	if binding == "" {
		return ctx
	}
	return context.WithValue(ctx, httpBindingCtxKey{}, binding)
}

func HTTPBindingFromContext(ctx context.Context) string {
	binding, _ := ctx.Value(httpBindingCtxKey{}).(string)
	return strings.TrimSpace(binding)
}

func WithInternalConnectionAccess(ctx context.Context) context.Context {
	return context.WithValue(ctx, internalConnectionAccessCtxKey{}, true)
}

func InternalConnectionAccessFromContext(ctx context.Context) bool {
	allowed, _ := ctx.Value(internalConnectionAccessCtxKey{}).(bool)
	return allowed
}

const xForwardedForHeader = "X-Forwarded-For"

func ClientIP(r *http.Request) string {
	if xff := r.Header.Get(xForwardedForHeader); xff != "" {
		if ip := strings.TrimSpace(strings.SplitN(xff, ",", 2)[0]); ip != "" {
			return ip
		}
	}
	return RemoteAddrIP(r)
}

func RemoteAddrIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
