package server

import "github.com/go-chi/chi/v5"

func (s *Server) routes() {
	r := s.router
	r.Use(maxBodyMiddleware(1 << 20)) // 1 MB

	if s.devMode {
		r.Use(devCORS)
	}

	s.mountCoreRoutes(r)
	s.mountMCPRoutes(r)
	s.mountDevRoutes(r)
	s.mountAPIRoutes(r)

	if s.webUI != nil {
		r.NotFound(s.webUI.ServeHTTP)
	}
}

func (s *Server) mountCoreRoutes(r chi.Router) {
	r.Get("/health", s.healthCheck)
	r.Get("/ready", s.readinessCheck)
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

func (s *Server) mountDevRoutes(r chi.Router) {
	if !s.devMode {
		return
	}
	r.Post("/api/dev-login", s.devLogin)
}

func (s *Server) mountAPIRoutes(r chi.Router) {
	r.Route("/api/v1", func(r chi.Router) {
		s.mountAuthRoutes(r)
		s.mountBindingRoutes(r)
		s.mountAuthenticatedRoutes(r)
	})
}
