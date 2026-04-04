package server

import (
	"net/http"
	"strings"
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
	if s.prometheusMetrics != nil {
		r.Group(func(r chi.Router) {
			r.Use(s.authMiddleware)
			r.Handle("/metrics", s.prometheusMetrics)
		})
	}
}

func (s *Server) mountAdminUIRoutes(r chi.Router) {
	if s.adminUI == nil {
		return
	}

	adminHandler := stripRoutePrefix("/admin", s.adminUI)
	r.Handle("/admin", adminHandler)
	r.Handle("/admin/*", adminHandler)
}

func stripRoutePrefix(prefix string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, prefix)
		if path == "" {
			path = "/"
		} else if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}

		r2 := *r
		u := *r.URL
		u.Path = path
		r2.URL = &u
		next.ServeHTTP(w, &r2)
	})
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
