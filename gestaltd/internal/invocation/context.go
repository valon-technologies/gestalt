package invocation

import (
	"context"
	"net"
	"net/http"
	"strings"

	"github.com/google/uuid"
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

type RequestMeta struct {
	ClientIP   string
	RemoteAddr string
	UserAgent  string
}

func WithRequestMeta(ctx context.Context, meta RequestMeta) context.Context {
	return context.WithValue(ctx, requestMetaCtxKey{}, meta)
}

func RequestMetaFromContext(ctx context.Context) RequestMeta {
	m, _ := ctx.Value(requestMetaCtxKey{}).(RequestMeta)
	return m
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
