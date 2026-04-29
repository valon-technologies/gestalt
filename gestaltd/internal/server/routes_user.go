package server

import "github.com/go-chi/chi/v5"

func (s *Server) mountAuthenticatedRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.authMiddleware)

		r.Get("/integrations", s.listIntegrations)
		r.Delete("/integrations/{name}", s.disconnectIntegration)

		r.Get("/workflow/schedules", s.workflowScheduleCRUDRemoved)
		r.Post("/workflow/schedules", s.workflowScheduleCRUDRemoved)
		r.Route("/workflow/schedules", func(r chi.Router) {
			r.Get("/", s.workflowScheduleCRUDRemoved)
			r.Post("/", s.workflowScheduleCRUDRemoved)
			r.Get("/{scheduleID}", s.workflowScheduleCRUDRemoved)
			r.Put("/{scheduleID}", s.workflowScheduleCRUDRemoved)
			r.Delete("/{scheduleID}", s.workflowScheduleCRUDRemoved)
			r.Post("/{scheduleID}/pause", s.workflowScheduleCRUDRemoved)
			r.Post("/{scheduleID}/resume", s.workflowScheduleCRUDRemoved)
		})
		r.Get("/workflow/event-triggers", s.workflowEventTriggerCRUDRemoved)
		r.Post("/workflow/event-triggers", s.workflowEventTriggerCRUDRemoved)
		r.Route("/workflow/event-triggers", func(r chi.Router) {
			r.Get("/", s.workflowEventTriggerCRUDRemoved)
			r.Post("/", s.workflowEventTriggerCRUDRemoved)
			r.Get("/{triggerID}", s.workflowEventTriggerCRUDRemoved)
			r.Put("/{triggerID}", s.workflowEventTriggerCRUDRemoved)
			r.Delete("/{triggerID}", s.workflowEventTriggerCRUDRemoved)
			r.Post("/{triggerID}/pause", s.workflowEventTriggerCRUDRemoved)
			r.Post("/{triggerID}/resume", s.workflowEventTriggerCRUDRemoved)
		})
		r.Post("/workflow/events", s.publishWorkflowEvent)
		r.Get("/workflow/runs", s.listGlobalWorkflowRuns)
		r.Route("/workflow/runs", func(r chi.Router) {
			r.Get("/", s.listGlobalWorkflowRuns)
			r.Get("/{runID}", s.getGlobalWorkflowRun)
			r.Post("/{runID}/cancel", s.cancelGlobalWorkflowRun)
		})
		r.Post("/agent/sessions", s.createAgentSession)
		r.Get("/agent/sessions", s.listAgentSessions)
		r.Get("/agent/providers", s.listAgentProviders)
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
			r.Get("/{turnID}/events/stream", s.streamAgentTurnEvents)
			r.Get("/{turnID}/interactions", s.listAgentTurnInteractions)
			r.Post("/{turnID}/interactions/{interactionID}/resolve", s.resolveAgentInteraction)
		})

		r.Post("/auth/start-oauth", s.startIntegrationOAuth)
		r.Post("/auth/connect-manual", s.connectManual)

		r.Post("/provider-dev/sessions", s.createProviderDevSession)
		r.Get("/provider-dev/sessions/{sessionID}/poll", s.pollProviderDevSession)
		r.Post("/provider-dev/sessions/{sessionID}/calls/{callID}", s.completeProviderDevCall)
		r.Delete("/provider-dev/sessions/{sessionID}", s.closeProviderDevSession)
		r.Post("/provider-dev/attachments", s.createProviderDevSession)
		r.Get("/provider-dev/attachments", s.listProviderDevAttachments)
		r.Get("/provider-dev/attachments/{attachmentID}", s.getProviderDevAttachment)

		r.Post("/tokens", s.createAPIToken)
		r.Get("/tokens", s.listAPITokens)
		r.Delete("/tokens", s.revokeAllAPITokens)
		r.Delete("/tokens/{id}", s.revokeAPIToken)

	})

	r.With(s.pluginRouteAuthMiddleware("name")).Get("/integrations/{name}/operations", s.listOperations)
	r.With(s.pluginRouteAuthMiddleware("integration")).Get("/{integration}/{operation}", s.executeOperation)
	r.With(s.pluginRouteAuthMiddleware("integration")).Post("/{integration}/{operation}", s.executeOperation)
}
