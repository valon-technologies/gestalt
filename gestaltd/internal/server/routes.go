package server

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func (s *Server) routes() {
	r := s.router
	r.Use(requestMetaMiddleware)
	r.Use(s.securityHeadersMiddleware)
	r.Use(maxBodyMiddleware(1 << 20)) // 1 MB

	s.mountCoreRoutes(r)
	s.mountMCPRoutes(r)
	s.mountAPIRoutes(r)
	s.mountAdminUIRoutes(r)

	if s.clientUI != nil {
		r.NotFound(s.clientUI.ServeHTTP)
	}
}

func (s *Server) mountCoreRoutes(r chi.Router) {
	r.Get("/health", s.healthCheck)
	r.Get("/ready", s.readinessCheck)
	r.Group(func(r chi.Router) {
		r.Use(s.authMiddleware)
		r.HandleFunc("/metrics", s.servePrometheusMetrics)
	})
}

func (s *Server) mountAdminUIRoutes(r chi.Router) {
	if s.adminUI == nil {
		return
	}

	r.Get("/admin", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/", http.StatusMovedPermanently)
	})
	r.Handle("/admin/*", http.StripPrefix("/admin", s.adminUI))
}

func (s *Server) mountMCPRoutes(r chi.Router) {
	if s.mcpHandler == nil {
		return
	}
	r.Group(func(r chi.Router) {
		r.Use(s.authMiddleware)
		r.Handle("/mcp", s.mcpHandler)
	})
}

func (s *Server) mountAPIRoutes(r chi.Router) {
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(middleware.Timeout(60 * time.Second))
		s.mountAuthRoutes(r)
		s.mountAuthenticatedRoutes(r)
	})
}

func (s *Server) servePrometheusMetrics(w http.ResponseWriter, r *http.Request) {
	if s.prometheusMetrics == nil {
		http.Error(w, "Prometheus metrics are unavailable because telemetry metrics are disabled.", http.StatusServiceUnavailable)
		return
	}
	s.prometheusMetrics.ServeHTTP(w, r)
}
