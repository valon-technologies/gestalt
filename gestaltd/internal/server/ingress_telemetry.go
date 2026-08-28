package server

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
)

const maxReferrerLen = 2048

func clientKindTelemetryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metricutil.AddHTTPServerMetricDims(r.Context(), metricutil.HTTPMetricDims{
			ClientKind: classifyClientKind(r),
		})
		next.ServeHTTP(w, r)
	})
}

func classifyClientKind(r *http.Request) string {
	if r.Header.Get(metricutil.HeaderGestaltClient) == metricutil.ClientKindCLI {
		return metricutil.ClientKindCLI
	}
	for name := range r.Header {
		if strings.HasPrefix(strings.ToLower(name), "sec-fetch-") {
			return metricutil.ClientKindWeb
		}
	}
	return metricutil.ClientKindUnknown
}

func ingressKindTelemetryMiddleware(kind string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			metricutil.AddHTTPServerMetricDims(r.Context(), metricutil.HTTPMetricDims{
				IngressKind: kind,
			})
			next.ServeHTTP(w, r)
		})
	}
}

func ingressKindTelemetryHandler(kind string, next http.Handler) http.HandlerFunc {
	return ingressKindTelemetryMiddleware(kind)(next).ServeHTTP
}

func (s *Server) uiAPIIngressTelemetryMiddleware(kind string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			dims := metricutil.HTTPMetricDims{IngressKind: kind}
			if classifyClientKind(r) == metricutil.ClientKindWeb {
				dims.ClientApp = classifyClientAppFromReferrer(s.mountedUIs, r)
			}
			metricutil.AddHTTPServerMetricDims(r.Context(), dims)
			next.ServeHTTP(w, r)
		})
	}
}

func (s *Server) uiAPIIngressTelemetryHandler(kind string, next http.Handler) http.HandlerFunc {
	return s.uiAPIIngressTelemetryMiddleware(kind)(next).ServeHTTP
}

func classifyClientAppFromReferrer(mountedUIs []MountedUI, r *http.Request) string {
	referer := strings.TrimSpace(r.Referer())
	if referer == "" || len(referer) > maxReferrerLen {
		return metricutil.ClientAppUnknown
	}
	parsed, err := url.Parse(referer)
	if err != nil || !referrerSameOrigin(r, parsed) {
		return metricutil.ClientAppUnknown
	}
	path := strings.TrimSpace(parsed.Path)
	if path == "" {
		path = "/"
	}
	mounted, ok := mountedUIForReferrerPath(mountedUIs, path)
	if !ok {
		return metricutil.ClientAppUnknown
	}
	name := strings.TrimSpace(mounted.Name)
	if name == "" {
		return metricutil.ClientAppUnknown
	}
	return name
}

func mountedUIForReferrerPath(mountedUIs []MountedUI, path string) (MountedUI, bool) {
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	var (
		best        MountedUI
		bestLen     int
		bestMatched bool
	)
	for i := range mountedUIs {
		candidate := mountedUIs[i]
		if candidate.Path == "" || !mountedUIPathMatches(path, candidate.Path) {
			continue
		}
		if !bestMatched || len(candidate.Path) > bestLen {
			best = candidate
			bestLen = len(candidate.Path)
			bestMatched = true
		}
	}
	return best, bestMatched
}

func referrerSameOrigin(r *http.Request, referer *url.URL) bool {
	if referer == nil || referer.Host == "" || referer.Scheme == "" {
		return false
	}
	if strings.TrimSpace(r.Host) == "" {
		return false
	}
	if !strings.EqualFold(referer.Host, r.Host) {
		return false
	}
	reqScheme := requestScheme(r)
	return strings.EqualFold(referer.Scheme, reqScheme)
}

func requestScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if r.URL != nil {
		if scheme := strings.TrimSpace(r.URL.Scheme); scheme != "" {
			return scheme
		}
	}
	if proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); proto != "" {
		return strings.ToLower(strings.TrimSpace(strings.Split(proto, ",")[0]))
	}
	return "http"
}
