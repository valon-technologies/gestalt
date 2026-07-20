package server

import (
	"github.com/go-chi/chi/v5"
)

func (s *Server) mountAuthenticatedStreamRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.authMiddleware)
		r.Get("/agent/turns/{turnID}/events/stream", s.streamAgentTurnEvents)
	})
}

func (s *Server) mountAuthenticatedRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.authMiddleware)

		r.Get("/apps", s.listIntegrations)
		r.Delete("/apps/{name}", s.disconnectIntegration)

		r.Post("/workflow/events", s.deliverWorkflowEvent)

		r.Get("/agent/providers", s.listAgentProviders)
		r.Post("/agent/harnesses/resolve", s.resolveAgentHarness)

		r.Get("/auth/session", s.authSession)
		r.Post("/auth/start-oauth", s.startIntegrationOAuth)
		r.Post("/auth/connect-manual", s.connectManual)

		r.Post("/tokens", s.createAPIToken)
		r.Delete("/tokens/{id}", s.revokeAPIToken)

	})

	r.With(s.pluginRouteAuthMiddleware("name")).Get("/apps/{name}/operations", s.listOperations)
	r.With(s.pluginRouteAuthMiddleware("integration")).Get("/{integration}/{operation}", s.executeOperation)
	r.With(s.pluginRouteAuthMiddleware("integration")).Post("/{integration}/{operation}", s.executeOperation)
}
