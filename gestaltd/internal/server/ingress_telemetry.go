package server

import (
	"net/http"
	"strings"

	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
)

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
