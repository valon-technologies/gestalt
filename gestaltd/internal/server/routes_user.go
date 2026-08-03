package server

import (
	"github.com/go-chi/chi/v5"
)

func (s *Server) mountAuthenticatedStreamRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.authMiddleware)
		r.Get("/agents/{agentID}/runs/{runID}/events", s.contractAgentRunEvents)
	})
}

func (s *Server) mountAuthenticatedRoutes(r chi.Router) {
	s.mountAppAdminRegistryRoutes(r)

	r.Group(func(r chi.Router) {
		r.Use(s.authMiddleware)

		r.Get("/apps", s.listIntegrations)
		r.Delete("/apps/{name}", s.disconnectIntegration)

		r.Post("/workflow/events", s.deliverWorkflowEvent)

		r.Get("/auth/session", s.authSession)

		r.Post("/agents", s.createContractAgent)
		r.Get("/agents", s.listContractAgents)
		r.Route("/agents/{agentID}", func(r chi.Router) {
			r.Get("/", s.getContractAgent)
			r.Delete("/", s.archiveContractAgent)
			r.Patch("/config", s.updateContractAgentConfig)
			r.Post("/runs", s.createContractAgentRun)
			r.Get("/runs", s.listContractAgentRuns)
			r.Route("/runs/{runID}", func(r chi.Router) {
				r.Get("/", s.getContractAgentRun)
				r.Post("/cancel", s.cancelContractAgentRun)
				r.Get("/interactions", s.listContractAgentRunInteractions)
				r.Get("/interactions/{interactionID}", s.getContractAgentRunInteraction)
				r.Post("/interactions/{interactionID}/resolve", s.resolveContractAgentRunInteraction)
			})
		})

		r.Post("/auth/start-oauth", s.startIntegrationOAuth)
		r.Post("/auth/connect-manual", s.connectManual)

		r.Post("/tokens", s.createAPIToken)
		r.Delete("/tokens/{id}", s.revokeAPIToken)

	})

	r.With(s.pluginRouteAuthMiddleware("name")).Get("/apps/{name}/operations", s.listOperations)
	r.With(s.pluginRouteAuthMiddleware("integration")).Get("/{integration}/{operation}", s.executeOperation)
	r.With(s.pluginRouteAuthMiddleware("integration")).Post("/{integration}/{operation}", s.executeOperation)
}
