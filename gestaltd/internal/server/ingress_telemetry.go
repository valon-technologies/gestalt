package server

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"google.golang.org/grpc/metadata"
)

const maxReferrerLen = 2048

const subjectLabelSlotHeader = "X-Gestalt-Subject-Label-Slot"

type subjectLabelSlot struct {
	label string
	set   bool
}

var (
	subjectLabelSlotSeq   atomic.Uint64
	subjectLabelSlotStore sync.Map
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
	if p.Identity != nil {
		if email := strings.ToLower(strings.TrimSpace(p.Identity.Email)); email != "" {
			return email
		}
	}
	if kind, name, ok := core.ParseSubjectID(p.SubjectID); ok && kind == "service_account" {
		if name = strings.TrimSpace(name); name != "" {
			return name
		}
	}
	return metricutil.SubjectLabelUnknown
}

func addSubjectLabelMetricDims(ctx context.Context, r *http.Request, p *principal.Principal) {
	recordSubjectLabel(ctx, r, subjectLabelFromPrincipal(p))
}

func addUnknownSubjectLabelMetricDims(ctx context.Context, r *http.Request) {
	recordSubjectLabel(ctx, r, metricutil.SubjectLabelUnknown)
}

func bindSubjectLabelSlot(r *http.Request) func() {
	slot := &subjectLabelSlot{}
	id := subjectLabelSlotSeq.Add(1)
	subjectLabelSlotStore.Store(id, slot)
	r.Header.Set(subjectLabelSlotHeader, strconv.FormatUint(id, 10))
	return func() {
		subjectLabelSlotStore.Delete(id)
	}
}

func flushSubjectLabelSlot(r *http.Request) {
	slot := subjectLabelSlotForRequest(r)
	if slot == nil || !slot.set {
		return
	}
	metricutil.AddHTTPServerMetricDims(r.Context(), metricutil.HTTPMetricDims{
		SubjectLabel: slot.label,
	})
}

func recordSubjectLabel(ctx context.Context, r *http.Request, label string) {
	if r != nil {
		if slot := subjectLabelSlotForRequest(r); slot != nil {
			slot.label = label
			slot.set = true
			return
		}
	}
	if slot := subjectLabelSlotFromContext(ctx); slot != nil {
		slot.label = label
		slot.set = true
		return
	}
	metricCtx := metricContextForHTTPMetrics(ctx)
	if r != nil {
		metricCtx = r.Context()
	}
	metricutil.AddHTTPServerMetricDims(metricCtx, metricutil.HTTPMetricDims{
		SubjectLabel: label,
	})
}

func subjectLabelSlotForRequest(r *http.Request) *subjectLabelSlot {
	if r == nil {
		return nil
	}
	id, err := strconv.ParseUint(strings.TrimSpace(r.Header.Get(subjectLabelSlotHeader)), 10, 64)
	if err != nil {
		return nil
	}
	raw, ok := subjectLabelSlotStore.Load(id)
	if !ok {
		return nil
	}
	slot, ok := raw.(*subjectLabelSlot)
	if !ok {
		return nil
	}
	return slot
}

func subjectLabelSlotFromContext(ctx context.Context) *subjectLabelSlot {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil
	}
	for _, value := range md.Get(strings.ToLower(subjectLabelSlotHeader)) {
		id, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
		if err != nil {
			continue
		}
		raw, ok := subjectLabelSlotStore.Load(id)
		if !ok {
			return nil
		}
		slot, ok := raw.(*subjectLabelSlot)
		if !ok {
			return nil
		}
		return slot
	}
	return nil
}

func metricContextForHTTPMetrics(ctx context.Context) context.Context {
	return publicrpc.HTTPMetricContextFrom(ctx)
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
