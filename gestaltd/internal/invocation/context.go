package invocation

import (
	"context"
	"net"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/authorization"
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

type RequestMeta struct {
	ClientIP   string
	RemoteAddr string
	UserAgent  string
}

type CredentialContext struct {
	Mode       core.ConnectionMode
	SubjectID  string
	Connection string
	Instance   string
}

type invocationSurfaceCtxKey struct{}
type credentialCtxKey struct{}
type accessCtxKey struct{}
type workflowCtxKey struct{}

type InvocationSurface string

type AccessContext = authorization.AccessContext

const InvocationSurfaceHTTP InvocationSurface = "http"

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

func WithWorkflowContext(ctx context.Context, workflow map[string]any) context.Context {
	return context.WithValue(ctx, workflowCtxKey{}, workflow)
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
