package server

import "github.com/go-chi/chi/v5"

func (s *Server) mountAuthenticatedRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.authMiddleware)

		r.Get("/integrations", s.listIntegrations)
		r.Delete("/integrations/{name}", s.disconnectIntegration)
		r.Get("/integrations/{name}/operations", s.listOperations)

		r.Get("/{integration}/{operation}", s.executeOperation)
		r.Post("/{integration}/{operation}", s.executeOperation)

		r.Post("/auth/start-oauth", s.startIntegrationOAuth)
		r.Post("/auth/connect-manual", s.connectManual)

		r.Post("/tokens", s.createAPIToken)
		r.Get("/tokens", s.listAPITokens)
		r.Delete("/tokens", s.revokeAllAPITokens)
		r.Delete("/tokens/{id}", s.revokeAPIToken)

		r.Get("/metrics/overview", s.operationMetricsOverview)
	})
}
