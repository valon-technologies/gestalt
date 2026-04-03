package server

import (
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

	if s.webUI != nil {
		r.NotFound(s.webUI.ServeHTTP)
	}
}

func (s *Server) mountCoreRoutes(r chi.Router) {
	r.Get("/health", s.healthCheck)
	r.Get("/ready", s.readinessCheck)
	if s.prometheusMetrics != nil {
		r.Group(func(r chi.Router) {
			r.Use(s.authMiddleware)
			r.Handle("/metrics", s.prometheusMetrics)
		})
	}
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
