package server

import "github.com/go-chi/chi/v5"

func (s *Server) mountAuthRoutes(r chi.Router) {
	r.Get("/auth/info", s.authInfo)
	r.Get("/auth/login", s.startBrowserLogin)
	r.Post("/auth/login", s.startLogin)
	r.Get("/auth/login/callback", s.loginCallback)
	r.Post("/auth/logout", s.logout)
	r.Get("/auth/callback", s.integrationOAuthCallback)
	r.Post("/external-credentials/pending-connection", s.selectPendingConnection)
	// Deprecated: keep this auth route as a backwards-compatible alias while clients migrate to /external-credentials.
	r.Post("/auth/pending-connection", s.selectPendingConnection)
}
