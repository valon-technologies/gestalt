package httpheaders

import (
	"context"
	"net/http"
	"strings"
)

// WithOverrides attaches trimmed, canonical header overrides using the caller's context key.
func WithOverrides(ctx context.Context, key any, headers map[string]string) context.Context {
	if ctx == nil || len(headers) == 0 {
		return ctx
	}
	normalized := make(map[string]string, len(headers))
	for name, value := range headers {
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if name == "" || value == "" {
			continue
		}
		normalized[http.CanonicalHeaderKey(name)] = value
	}
	if len(normalized) == 0 {
		return ctx
	}
	return context.WithValue(ctx, key, normalized)
}
