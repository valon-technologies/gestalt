package server

import "github.com/go-chi/chi/v5"

func (s *Server) mountAuthRoutes(r chi.Router) {
	r.Get("/auth/info", s.authInfo)
	r.Post("/auth/login", s.startLogin)
	r.Get("/auth/login/callback", s.loginCallback)
	r.Post("/auth/logout", s.logout)
	r.Get("/auth/callback", s.integrationOAuthCallback)
	r.Get("/auth/pending-connection", s.getPendingConnection)
	r.Post("/auth/pending-connection", s.selectPendingConnection)
}
