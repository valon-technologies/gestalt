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
		r.Get("/workflow/runs", s.listGlobalWorkflowRuns)
		r.Route("/workflow/runs", func(r chi.Router) {
			r.Get("/", s.listGlobalWorkflowRuns)
			r.Get("/{runID}", s.getGlobalWorkflowRun)
			r.Post("/{runID}/cancel", s.cancelGlobalWorkflowRun)
		})

		r.Post("/agent/sessions", s.createAgentSession)
		r.Get("/agent/sessions", s.listAgentSessions)
		r.Get("/agent/providers", s.listAgentProviders)
		r.Post("/agent/harnesses/resolve", s.resolveAgentHarness)
		r.Route("/agent/sessions", func(r chi.Router) {
			r.Post("/", s.createAgentSession)
			r.Get("/", s.listAgentSessions)
			r.Get("/{sessionID}", s.getAgentSession)
			r.Patch("/{sessionID}", s.updateAgentSession)
			r.Post("/{sessionID}/turns", s.createAgentTurn)
			r.Get("/{sessionID}/turns", s.listAgentTurns)
		})
		r.Route("/agent/turns", func(r chi.Router) {
			r.Get("/{turnID}", s.getAgentTurn)
			r.Post("/{turnID}/cancel", s.cancelAgentTurn)
			r.Get("/{turnID}/events", s.listAgentTurnEvents)
			r.Get("/{turnID}/interactions", s.listAgentTurnInteractions)
			r.Post("/{turnID}/interactions/{interactionID}/resolve", s.resolveAgentInteraction)
		})

		r.Post("/auth/start-oauth", s.startIntegrationOAuth)
		r.Post("/auth/connect-manual", s.connectManual)

		r.Post("/tokens", s.createAPIToken)
		r.Get("/tokens", s.listAPITokens)
		r.Delete("/tokens", s.revokeAllAPITokens)
		r.Delete("/tokens/{id}", s.revokeAPIToken)

		r.Post("/authorization/check-access", s.checkAuthorizationAccess)
		r.Get("/authorization/relationships", s.listAuthorizationRelationships)
		r.Post("/authorization/relationships", s.addAuthorizationRelationship)
	})

	r.With(s.pluginRouteAuthMiddleware("name")).Get("/apps/{name}/operations", s.listOperations)
	r.With(s.pluginRouteAuthMiddleware("integration")).Get("/{integration}/{operation}", s.executeOperation)
	r.With(s.pluginRouteAuthMiddleware("integration")).Post("/{integration}/{operation}", s.executeOperation)
}
