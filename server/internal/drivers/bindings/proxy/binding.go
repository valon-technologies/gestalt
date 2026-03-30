package proxy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/textproto"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/apiexec"
	"github.com/valon-technologies/gestalt/server/internal/drivers/bindings/internal/httpjson"
	"github.com/valon-technologies/gestalt/server/internal/egress"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

const maxRequestBodySize = 1 << 20 // 1 MB

var _ core.Binding = (*Binding)(nil)

type Binding struct {
	name     string
	provider string
	cfg      proxyConfig
	resolver egress.Resolver
	client   *http.Client
}

func New(name, provider string, cfg proxyConfig, resolver egress.Resolver, client *http.Client) *Binding {
	if client == nil {
		client = http.DefaultClient
	}
	return &Binding{name: name, provider: provider, cfg: cfg, resolver: resolver, client: client}
}

func (b *Binding) Name() string { return b.name }

func (b *Binding) Start(_ context.Context) error { return nil }
func (b *Binding) Close() error                  { return nil }

func (b *Binding) Routes() []core.Route {
	patterns := b.cfg.routePatterns()
	routes := make([]core.Route, 0, len(b.cfg.methods())*len(patterns))
	for _, method := range b.cfg.methods() {
		for _, pattern := range patterns {
			routes = append(routes, core.Route{
				Method:    method,
				Pattern:   pattern,
				Handler:   http.HandlerFunc(b.handle),
				Public:    false,
				ProxyAuth: true,
			})
		}
	}
	return routes
}

func (b *Binding) handle(w http.ResponseWriter, r *http.Request) {
	resolved, body, err := b.resolve(r)
	if err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	scheme := resolveScheme(r)
	result, err := egress.ExecuteHTTP(r.Context(), b.client, egress.HTTPRequestSpec{
		Target:      resolved.Target,
		BaseURL:     scheme + "://" + resolved.Target.Host,
		Headers:     resolved.Headers,
		Body:        body,
		ContentType: r.Header.Get("Content-Type"),
		Credential:  resolved.Credential,
		Check:       acceptAllStatuses,
		NoRetry:     true,
	})
	if err != nil {
		httpjson.WriteError(w, http.StatusBadGateway, err.Error())
		return
	}

	writeProxyResponse(w, result)
}

func writeProxyResponse(w http.ResponseWriter, result *core.OperationResult) {
	for key, values := range result.Headers {
		if isHopByHopHeader(key) {
			continue
		}
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}
	w.WriteHeader(result.Status)
	_, _ = io.WriteString(w, result.Body)
}

var hopByHopHeaders = map[string]bool{
	"Connection":          true,
	"Keep-Alive":          true,
	"Proxy-Authenticate":  true,
	"Proxy-Authorization": true,
	"Te":                  true,
	"Trailer":             true,
	"Transfer-Encoding":   true,
	"Upgrade":             true,
}

func isHopByHopHeader(key string) bool {
	return hopByHopHeaders[key]
}

func (b *Binding) resolve(r *http.Request) (egress.Resolution, []byte, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodySize))
	if err != nil {
		return egress.Resolution{}, nil, fmt.Errorf("reading request body: %w", err)
	}
	defer func() { _ = r.Body.Close() }()

	headers := sanitizeForwardHeaders(normalizeHeaders(r.Header))
	target := egress.Target{
		Provider: b.provider,
		Method:   r.Method,
		Host:     resolveHost(r),
		Path:     resolvePath(r, b.cfg.normalizedPath()),
	}

	ctx := b.subjectContext(r.Context())

	resolved, err := b.resolver.Resolve(ctx, egress.ResolutionInput{
		Target:  target,
		Headers: headers,
	})
	if err != nil {
		return egress.Resolution{}, nil, err
	}

	return resolved, body, nil
}

func (b *Binding) subjectContext(ctx context.Context) context.Context {
	ctx = egress.WithSubjectFromPrincipal(ctx, principal.FromContext(ctx))
	if _, ok := egress.SubjectFromContext(ctx); !ok {
		ctx = egress.WithSubject(ctx, egress.Subject{
			Kind: egress.SubjectSystem,
			ID:   b.name,
		})
	}
	return ctx
}

var acceptAllStatuses apiexec.ResponseChecker = func(int, []byte) error { return nil }

var sanitizedHeaders = func() map[string]bool {
	m := map[string]bool{
		"Authorization":     true,
		"Cookie":            true,
		"Host":              true,
		"Content-Length":    true,
		"X-Forwarded-Host":  true,
		"X-Forwarded-Proto": true,
	}
	for k := range hopByHopHeaders {
		m[k] = true
	}
	return m
}()

func sanitizeForwardHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	out := make(map[string]string, len(headers))
	for k, v := range headers {
		if sanitizedHeaders[k] {
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

const (
	schemeHTTP  = "http"
	schemeHTTPS = "https"
)

func resolveScheme(r *http.Request) string {
	if r.URL != nil && r.URL.Scheme != "" {
		return r.URL.Scheme
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto == schemeHTTP || proto == schemeHTTPS {
		return proto
	}
	return schemeHTTPS
}

func resolveHost(r *http.Request) string {
	if r.URL != nil && r.URL.Host != "" {
		return r.URL.Host
	}
	if host := r.Header.Get("X-Forwarded-Host"); host != "" {
		return host
	}
	if r.Host != "" {
		return r.Host
	}
	return ""
}

func resolvePath(r *http.Request, configuredPath string) string {
	suffix := strings.TrimPrefix(chi.URLParam(r, "*"), "/")
	if suffix == "" && shouldUseConfiguredPathFallback(r) {
		if fallback, ok := configuredPathSuffix(r, configuredPath); ok {
			suffix = fallback
		}
	}

	path := "/"
	if suffix != "" {
		path += suffix
	}
	if rawQuery := requestRawQuery(r); rawQuery != "" {
		return path + "?" + rawQuery
	}
	return path
}

func normalizeHeaders(headers http.Header) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	out := make(map[string]string, len(headers))
	for k, values := range headers {
		if len(values) == 0 {
			continue
		}
		out[textproto.CanonicalMIMEHeaderKey(k)] = strings.Join(values, ",")
	}
	return out
}

func configuredPathSuffix(r *http.Request, configuredPath string) (string, bool) {
	path := requestPath(r)
	switch {
	case configuredPath == "/":
		return strings.TrimPrefix(path, "/"), true
	case path == configuredPath:
		return "", true
	case strings.HasPrefix(path, configuredPath+"/"):
		return strings.TrimPrefix(path, configuredPath+"/"), true
	default:
		return "", false
	}
}

func shouldUseConfiguredPathFallback(r *http.Request) bool {
	rctx := chi.RouteContext(r.Context())
	return rctx == nil || len(rctx.RoutePatterns) == 0
}

func requestPath(r *http.Request) string {
	if r.URL != nil && r.URL.Path != "" {
		return r.URL.Path
	}
	if r.RequestURI == "" {
		return "/"
	}
	path, _, _ := strings.Cut(r.RequestURI, "?")
	if path == "" {
		return "/"
	}
	return path
}

func requestRawQuery(r *http.Request) string {
	if r.URL != nil {
		return r.URL.RawQuery
	}
	_, rawQuery, ok := strings.Cut(r.RequestURI, "?")
	if !ok {
		return ""
	}
	return rawQuery
}
