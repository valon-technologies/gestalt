package server

import (
	"bytes"
	"compress/gzip"
	"io"
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
	req := r.Clone(r.Context())
	// Parsing needs plaintext. Do not ask the scrape handler to gzip, and
	// decompress if it still does.
	req.Header.Del("Accept-Encoding")
	s.prometheusMetrics.ServeHTTP(capture, req)
	status = capture.code
	if status == 0 {
		status = http.StatusOK
	}
	contentType = capture.Header().Get("Content-Type")
	if contentType == "" {
		contentType = "text/plain; version=0.0.4; charset=utf-8"
	}
	body = capture.body.Bytes()
	if strings.EqualFold(capture.Header().Get("Content-Encoding"), "gzip") {
		reader, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, http.StatusServiceUnavailable, contentType, true
		}
		decoded, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil {
			return nil, http.StatusServiceUnavailable, contentType, true
		}
		body = decoded
	}
	return body, status, contentType, true
}

func (s *Server) mountAdminMetricsRoutes(r chi.Router) {
	r.Get("/metrics", s.serveAdminPrometheusMetrics)
}

func (s *Server) serveAdminPrometheusMetrics(w http.ResponseWriter, r *http.Request) {
	if s.prometheusMetrics == nil {
		writeError(w, http.StatusServiceUnavailable, "prometheus metrics are unavailable because telemetry metrics are disabled")
		return
	}
	s.prometheusMetrics.ServeHTTP(w, r)
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
		writeError(w, http.StatusServiceUnavailable, "prometheus metrics are unavailable because telemetry metrics are disabled")
		return
	}
	if status != http.StatusOK {
		writeError(w, http.StatusServiceUnavailable, "prometheus metrics are unavailable because the metrics scrape failed")
		return
	}
	parsed, err := parsePrometheus(string(body))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "prometheus metrics are unavailable because the metrics scrape could not be parsed")
		return
	}
	samples := samplesForProvider(parsed, appName)
	writeJSON(w, http.StatusOK, summarizeAppMetrics(appName, samples))
}
