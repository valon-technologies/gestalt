package egress

import (
	"fmt"
	"net/http"
	"net/textproto"
	"strings"
)

// ResolvingRoundTripper runs requests through the shared egress resolver before
// delegating to the wrapped transport.
type ResolvingRoundTripper struct {
	base     http.RoundTripper
	resolver *Resolver
}

func NewResolvingRoundTripper(base http.RoundTripper, resolver *Resolver) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &ResolvingRoundTripper{base: base, resolver: resolver}
}

func (r *ResolvingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if r == nil || r.base == nil {
		return nil, fmt.Errorf("egress resolving round tripper is not initialized")
	}
	if req == nil || req.URL == nil {
		return nil, fmt.Errorf("nil request")
	}
	if r.resolver == nil {
		return r.base.RoundTrip(req)
	}

	resolved, err := r.resolver.Resolve(req.Context(), ResolutionInput{
		Target: Target{
			Method: req.Method,
			Host:   req.URL.Host,
			Path:   req.URL.Path,
		},
		Headers: requestHeaders(req.Header),
	})
	if err != nil {
		return nil, err
	}

	clone := req.Clone(req.Context())
	clone.Header = make(http.Header, len(resolved.Headers)+1)
	for key, value := range resolved.Headers {
		clone.Header.Set(key, value)
	}
	if resolved.Credential.Authorization != "" {
		clone.Header.Set("Authorization", resolved.Credential.Authorization)
	}

	return r.base.RoundTrip(clone)
}

func requestHeaders(h http.Header) map[string]string {
	if len(h) == 0 {
		return nil
	}
	out := make(map[string]string, len(h))
	for k, values := range h {
		if len(values) == 0 {
			continue
		}
		out[textproto.CanonicalMIMEHeaderKey(k)] = strings.Join(values, ",")
	}
	return out
}
