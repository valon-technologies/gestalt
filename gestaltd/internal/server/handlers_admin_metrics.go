package server

import (
	"bytes"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

type captureResponseWriter struct {
	header http.Header
	code   int
	body   bytes.Buffer
}

func (w *captureResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *captureResponseWriter) Write(p []byte) (int, error) {
	if w.code == 0 {
		w.code = http.StatusOK
	}
	return w.body.Write(p)
}

func (w *captureResponseWriter) WriteHeader(statusCode int) {
	w.code = statusCode
}

func (s *Server) scrapePrometheus(r *http.Request) (body []byte, status int, contentType string, ok bool) {
	if s.prometheusMetrics == nil {
		return nil, http.StatusServiceUnavailable, "", false
	}
	capture := &captureResponseWriter{}
	s.prometheusMetrics.ServeHTTP(capture, r.Clone(r.Context()))
	status = capture.code
	if status == 0 {
		status = http.StatusOK
	}
	contentType = capture.Header().Get("Content-Type")
	if contentType == "" {
		contentType = "text/plain; version=0.0.4; charset=utf-8"
	}
	return capture.body.Bytes(), status, contentType, true
}

func (s *Server) mountAdminMetricsRoutes(r chi.Router) {
	r.Get("/metrics", s.serveAdminPrometheusMetrics)
}

func (s *Server) serveAdminPrometheusMetrics(w http.ResponseWriter, r *http.Request) {
	body, status, contentType, ok := s.scrapePrometheus(r)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "Prometheus metrics are unavailable because telemetry metrics are disabled.")
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func (s *Server) mountAppAdminMetricsRoutes(r chi.Router) {
	r.With(s.pluginRouteAuthMiddleware("app"), s.appAdminAuthorizationMiddleware).
		Get("/apps/{app}/admin/metrics", s.getAppAdminMetrics)
}

func (s *Server) getAppAdminMetrics(w http.ResponseWriter, r *http.Request) {
	appName := strings.TrimSpace(chi.URLParam(r, "app"))
	if appName == "" {
		writeError(w, http.StatusBadRequest, "app is required")
		return
	}
	body, status, _, ok := s.scrapePrometheus(r)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "Prometheus metrics are unavailable because telemetry metrics are disabled.")
		return
	}
	if status != http.StatusOK {
		writeError(w, http.StatusServiceUnavailable, "Prometheus metrics are unavailable.")
		return
	}
	samples := samplesForProvider(parsePrometheus(string(body)), appName)
	writeJSON(w, http.StatusOK, summarizeAppMetrics(appName, samples))
}
