package server

import "github.com/go-chi/chi/v5"

func (s *Server) mountAuthenticatedRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.authMiddleware)

		r.Get("/integrations", s.listIntegrations)
		r.Delete("/integrations/{name}", s.disconnectIntegration)

		r.Route("/workflow/schedules", func(r chi.Router) {
			r.Get("/", s.listGlobalWorkflowSchedules)
			r.Post("/", s.createWorkflowSchedule)
			r.Get("/{scheduleID}", s.getGlobalWorkflowSchedule)
			r.Put("/{scheduleID}", s.updateGlobalWorkflowSchedule)
			r.Delete("/{scheduleID}", s.deleteGlobalWorkflowSchedule)
			r.Post("/{scheduleID}/pause", s.pauseGlobalWorkflowSchedule)
			r.Post("/{scheduleID}/resume", s.resumeGlobalWorkflowSchedule)
		})
		r.Route("/workflow/event-triggers", func(r chi.Router) {
			r.Get("/", s.listGlobalWorkflowEventTriggers)
			r.Post("/", s.createGlobalWorkflowEventTrigger)
			r.Get("/{triggerID}", s.getGlobalWorkflowEventTrigger)
			r.Put("/{triggerID}", s.updateGlobalWorkflowEventTrigger)
			r.Delete("/{triggerID}", s.deleteGlobalWorkflowEventTrigger)
			r.Post("/{triggerID}/pause", s.pauseGlobalWorkflowEventTrigger)
			r.Post("/{triggerID}/resume", s.resumeGlobalWorkflowEventTrigger)
		})
		r.Post("/workflow/events", s.publishWorkflowEvent)
		r.Route("/workflow/runs", func(r chi.Router) {
			r.Get("/", s.listGlobalWorkflowRuns)
			r.Get("/{runID}", s.getGlobalWorkflowRun)
			r.Post("/{runID}/cancel", s.cancelGlobalWorkflowRun)
		})
		r.Route("/agent/runs", func(r chi.Router) {
			r.Post("/", s.createGlobalAgentRun)
			r.Get("/", s.listGlobalAgentRuns)
			r.Get("/{runID}", s.getGlobalAgentRun)
			r.Post("/{runID}/cancel", s.cancelGlobalAgentRun)
		})

		r.Post("/auth/start-oauth", s.startIntegrationOAuth)
		r.Post("/auth/connect-manual", s.connectManual)

		r.Post("/tokens", s.createAPIToken)
		r.Get("/tokens", s.listAPITokens)
		r.Delete("/tokens", s.revokeAllAPITokens)
		r.Delete("/tokens/{id}", s.revokeAPIToken)

		r.Get("/identities", s.listLegacyManagedIdentities)
		r.Get("/identities/", s.listLegacyManagedIdentities)
		r.Post("/identities", s.legacyManagedIdentitiesGone)
		r.Post("/identities/", s.legacyManagedIdentitiesGone)
		r.Patch("/identities/{identityID}", s.legacyManagedIdentitiesGone)
		r.Delete("/identities/{identityID}", s.legacyManagedIdentitiesGone)
		r.Get("/identities/{identityID}/members", s.legacyManagedIdentitiesGone)
		r.Put("/identities/{identityID}/members", s.legacyManagedIdentitiesGone)
		r.Delete("/identities/{identityID}/members/{email}", s.legacyManagedIdentitiesGone)
		r.Get("/identities/{identityID}/grants", s.legacyManagedIdentitiesGone)
		r.Put("/identities/{identityID}/grants/{plugin}", s.legacyManagedIdentitiesGone)
		r.Delete("/identities/{identityID}/grants/{plugin}", s.legacyManagedIdentitiesGone)
		r.Get("/identities/{identityID}/tokens", s.legacyManagedIdentitiesGone)
		r.Post("/identities/{identityID}/tokens", s.legacyManagedIdentitiesGone)
		r.Delete("/identities/{identityID}/tokens/{id}", s.legacyManagedIdentitiesGone)
		r.Get("/identities/{identityID}/integrations", s.legacyManagedIdentitiesGone)
		r.Delete("/identities/{identityID}/integrations/{integration}", s.legacyManagedIdentitiesGone)
		r.Post("/identities/{identityID}/auth/start-oauth", s.legacyManagedIdentitiesGone)
		r.Post("/identities/{identityID}/auth/connect-manual", s.legacyManagedIdentitiesGone)
	})

	r.With(s.pluginRouteAuthMiddleware("name")).Get("/integrations/{name}/operations", s.listOperations)
	r.With(s.pluginRouteAuthMiddleware("integration")).Get("/{integration}/{operation}", s.executeOperation)
	r.With(s.pluginRouteAuthMiddleware("integration")).Post("/{integration}/{operation}", s.executeOperation)
}
