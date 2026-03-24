package proxy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/textproto"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/egress"
	"github.com/valon-technologies/gestalt/plugins/bindings/internal/httpjson"
)

var _ core.Binding = (*Binding)(nil)

type Binding struct {
	name string
	cfg  proxyConfig
}

func New(name string, cfg proxyConfig) *Binding {
	return &Binding{name: name, cfg: cfg}
}

func (b *Binding) Name() string           { return b.name }
func (b *Binding) Kind() core.BindingKind { return core.BindingSurface }

func (b *Binding) Start(_ context.Context) error { return nil }
func (b *Binding) Close() error                  { return nil }

func (b *Binding) Routes() []core.Route {
	patterns := b.cfg.routePatterns()
	routes := make([]core.Route, 0, len(b.cfg.methods())*len(patterns))
	for _, method := range b.cfg.methods() {
		for _, pattern := range patterns {
			routes = append(routes, core.Route{
				Method:  method,
				Pattern: pattern,
				Handler: http.HandlerFunc(b.handle),
			})
		}
	}
	return routes
}

func (b *Binding) handle(w http.ResponseWriter, r *http.Request) {
	norm, err := b.normalize(r)
	if err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if r.Method == http.MethodConnect {
		norm.Note = "CONNECT proxying is not implemented yet"
	} else {
		norm.Note = "proxy binding skeleton: request normalized, dispatch not wired"
	}

	httpjson.WriteJSON(w, http.StatusNotImplemented, norm)
}

func (b *Binding) normalize(r *http.Request) (normalizedRequest, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return normalizedRequest{}, fmt.Errorf("reading request body: %w", err)
	}
	defer func() { _ = r.Body.Close() }()

	headers := normalizeHeaders(r.Header)
	target := egress.Target{
		Method: r.Method,
		Host:   resolveHost(r),
		Path:   resolvePath(r, b.cfg.normalizedPath()),
	}

	policy := egress.PolicyInput{
		Subject: egress.Subject{
			Kind: egress.SubjectSystem,
			ID:   b.name,
		},
		Target:  target,
		Headers: headers,
	}

	norm := normalizedRequest{
		Policy: policy,
		Target: target,
	}
	if len(body) > 0 {
		norm.Body = string(body)
	}
	return norm, nil
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
