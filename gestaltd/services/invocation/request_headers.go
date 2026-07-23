package invocation

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/egress"
)

type invokeRequestHeadersCtxKey struct{}

type staticHeaderProvider interface {
	StaticHeaders() map[string]string
}

// WithInvokeRequestHeaders attaches outbound header overrides requested by a
// nested app invoke to the invocation context.
func WithInvokeRequestHeaders(ctx context.Context, headers map[string]string) context.Context {
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
	return context.WithValue(ctx, invokeRequestHeadersCtxKey{}, normalized)
}

// InvokeRequestHeadersFromContext returns outbound header overrides requested
// for the current invocation, if any.
func InvokeRequestHeadersFromContext(ctx context.Context) map[string]string {
	if ctx == nil {
		return nil
	}
	headers, _ := ctx.Value(invokeRequestHeadersCtxKey{}).(map[string]string)
	return headers
}

// ApplyInvokeHeaderOverrides validates requested header overrides against the
// target provider's declared static headers and attaches them to the outbound
// invocation context.
func ApplyInvokeHeaderOverrides(ctx context.Context, prov core.Provider) (context.Context, error) {
	requested := InvokeRequestHeadersFromContext(ctx)
	if len(requested) == 0 {
		return ctx, nil
	}
	staticHeaders := providerStaticHeaders(prov)
	if len(staticHeaders) == 0 {
		return ctx, fmt.Errorf("%w: provider does not allow header overrides", ErrInvalidInvocation)
	}
	overrides := make(map[string]string, len(requested))
	for name, value := range requested {
		staticKey, ok := staticHeaderKey(staticHeaders, name)
		if !ok {
			return ctx, fmt.Errorf("%w: header %q is not overridable for provider", ErrInvalidInvocation, name)
		}
		overrides[staticKey] = value
	}
	return egress.WithOutboundHeaderOverrides(ctx, overrides), nil
}

func providerStaticHeaders(prov core.Provider) map[string]string {
	if prov == nil {
		return nil
	}
	if hp, ok := prov.(staticHeaderProvider); ok {
		return hp.StaticHeaders()
	}
	return nil
}

func staticHeaderKey(staticHeaders map[string]string, requested string) (string, bool) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return "", false
	}
	canonical := http.CanonicalHeaderKey(requested)
	for name := range staticHeaders {
		if http.CanonicalHeaderKey(name) == canonical {
			return http.CanonicalHeaderKey(name), true
		}
	}
	return "", false
}
