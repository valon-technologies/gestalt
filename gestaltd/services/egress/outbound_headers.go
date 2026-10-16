package egress

import (
	"context"
	"net/http"

	"github.com/valon-technologies/gestalt/server/internal/httpheaders"
)

type outboundHeaderOverridesCtxKey struct{}

// WithOutboundHeaderOverrides attaches per-invoke outbound HTTP header overrides.
func WithOutboundHeaderOverrides(ctx context.Context, headers map[string]string) context.Context {
	return httpheaders.WithOverrides(ctx, outboundHeaderOverridesCtxKey{}, headers)
}

// OutboundHeaderOverridesFromContext returns outbound header overrides for the
// current invocation, if any.
func OutboundHeaderOverridesFromContext(ctx context.Context) map[string]string {
	if ctx == nil {
		return nil
	}
	overrides, _ := ctx.Value(outboundHeaderOverridesCtxKey{}).(map[string]string)
	return overrides
}

// ApplyOutboundHeaderOverrides merges invocation-scoped header overrides onto a
// base header map.
func ApplyOutboundHeaderOverrides(ctx context.Context, headers map[string]string) map[string]string {
	overrides := OutboundHeaderOverridesFromContext(ctx)
	if len(overrides) == 0 {
		return headers
	}
	out := make(map[string]string, len(headers)+len(overrides))
	for name, value := range headers {
		out[http.CanonicalHeaderKey(name)] = value
	}
	for name, value := range overrides {
		out[name] = value
	}
	return out
}
