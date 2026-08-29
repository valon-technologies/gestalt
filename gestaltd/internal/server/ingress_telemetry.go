package server

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const maxReferrerLen = 2048

const subjectLabelMetadataKey = "gestaltd-subject-label"

type subjectLabelRecorder struct {
	label   string
	labeler *otelhttp.Labeler
}

type subjectLabelRecorderKey struct{}

func clientKindTelemetryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		kind := classifyClientKind(r)
		dims := metricutil.HTTPMetricDims{ClientKind: kind}
		if kind == metricutil.ClientKindCLI {
			dims.ClientVersion = metricutil.ClassifyKnownCLIVersion(
				r.Header.Get(metricutil.HeaderGestaltClientVersion),
			)
		}
		metricutil.AddHTTPServerMetricDims(r.Context(), dims)
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
				dims.ClientApp = s.classifyClientAppFromReferrer(r)
			}
			metricutil.AddHTTPServerMetricDims(r.Context(), dims)
			next.ServeHTTP(w, r)
		})
	}
}

func (s *Server) uiAPIIngressTelemetryHandler(kind string, next http.Handler) http.HandlerFunc {
	return s.uiAPIIngressTelemetryMiddleware(kind)(next).ServeHTTP
}

func subjectLabelRecorderMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r, recorder := withSubjectLabelRecorder(r)
		defer recorder.flush(r.Context())
		next.ServeHTTP(w, r)
	})
}

func (s *Server) classifyClientAppFromReferrer(r *http.Request) string {
	referer := strings.TrimSpace(r.Referer())
	if referer == "" || len(referer) > maxReferrerLen {
		return metricutil.ClientAppUnknown
	}
	parsed, err := url.Parse(referer)
	if err != nil || !s.referrerSameOrigin(r, parsed) {
		return metricutil.ClientAppUnknown
	}
	mounted, ok := s.mountedUIForPath(parsed.Path)
	if !ok {
		return metricutil.ClientAppUnknown
	}
	name := strings.TrimSpace(mounted.Name)
	if name == "" {
		return metricutil.ClientAppUnknown
	}
	return name
}

func subjectLabelFromPrincipal(p *principal.Principal) string {
	if p == nil {
		return metricutil.SubjectLabelUnknown
	}
	p = principal.Canonicalized(p)
	switch p.Kind {
	case principal.KindUser:
		if p.Identity == nil {
			return metricutil.SubjectLabelUnknown
		}
		if email := strings.ToLower(strings.TrimSpace(p.Identity.Email)); email != "" {
			return email
		}
	case principal.Kind("service_account"):
		kind, name, ok := core.ParseSubjectID(p.SubjectID)
		if !ok || kind != "service_account" {
			return metricutil.SubjectLabelUnknown
		}
		if name = strings.TrimSpace(name); name != "" {
			return name
		}
	}
	return metricutil.SubjectLabelUnknown
}

func addSubjectLabelMetricDims(ctx context.Context, p *principal.Principal) {
	recordSubjectLabel(ctx, subjectLabelFromPrincipal(p))
}

func recordSubjectLabel(ctx context.Context, label string) {
	if recorder, ok := ctx.Value(subjectLabelRecorderKey{}).(*subjectLabelRecorder); ok {
		recorder.setLabel(label)
		return
	}
	metricutil.AddHTTPServerMetricDims(ctx, metricutil.HTTPMetricDims{
		SubjectLabel: label,
	})
}

func withSubjectLabelRecorder(r *http.Request) (*http.Request, *subjectLabelRecorder) {
	var labeler *otelhttp.Labeler
	if l, ok := otelhttp.LabelerFromContext(r.Context()); ok {
		labeler = l
	}
	recorder := &subjectLabelRecorder{labeler: labeler}
	ctx := context.WithValue(r.Context(), subjectLabelRecorderKey{}, recorder)
	return r.WithContext(ctx), recorder
}

func (r *subjectLabelRecorder) setLabel(label string) {
	label = strings.TrimSpace(label)
	if label == "" {
		return
	}
	r.label = label
}

func (r *subjectLabelRecorder) flush(ctx context.Context) {
	label := strings.TrimSpace(r.label)
	if label == "" {
		label = metricutil.SubjectLabelUnknown
	}
	if r.labeler != nil {
		ctx = otelhttp.ContextWithLabeler(ctx, r.labeler)
	}
	metricutil.AddHTTPServerMetricDims(ctx, metricutil.HTTPMetricDims{
		SubjectLabel: label,
	})
}

func setSubjectLabelResponseMetadata(ctx context.Context, p *principal.Principal) {
	_ = grpc.SetHeader(ctx, metadata.Pairs(subjectLabelMetadataKey, subjectLabelFromPrincipal(p)))
}

func setSubjectLabelStreamResponseMetadata(stream grpc.ServerStream, p *principal.Principal) {
	_ = stream.SetHeader(metadata.Pairs(subjectLabelMetadataKey, subjectLabelFromPrincipal(p)))
}

func recordSubjectLabelResponseMetadata(ctx context.Context) {
	serverMetadata, ok := runtime.ServerMetadataFromContext(ctx)
	if !ok {
		return
	}
	for _, md := range []metadata.MD{serverMetadata.HeaderMD, serverMetadata.TrailerMD} {
		if labels := md.Get(subjectLabelMetadataKey); len(labels) > 0 {
			recordSubjectLabel(ctx, labels[0])
			return
		}
	}
}

func (s *Server) referrerSameOrigin(r *http.Request, referer *url.URL) bool {
	if referer == nil || referer.Host == "" || referer.Scheme == "" {
		return false
	}

	expectedOrigin := strings.TrimSpace(s.publicBaseURL)
	if expectedOrigin == "" {
		if strings.TrimSpace(r.Host) == "" {
			return false
		}
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		expectedOrigin = scheme + "://" + r.Host
	}

	canonicalExpected, ok := canonicalOriginFromBaseURL(expectedOrigin)
	if !ok {
		return false
	}
	canonicalReferer := strings.ToLower(referer.Scheme) + "://" + canonicalHost(referer)
	return canonicalReferer == canonicalExpected
}
