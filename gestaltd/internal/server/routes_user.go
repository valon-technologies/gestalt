package server

import (
	"github.com/go-chi/chi/v5"
	agentshttp "github.com/valon-technologies/gestalt/server/services/agents/http"
	identityhttp "github.com/valon-technologies/gestalt/server/services/identity/http"
	pluginshttp "github.com/valon-technologies/gestalt/server/services/plugins/http"
	providerdevhttp "github.com/valon-technologies/gestalt/server/services/providerdev/http"
	workflowshttp "github.com/valon-technologies/gestalt/server/services/workflows/http"
)

func (s *Server) mountAuthenticatedRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.authMiddleware)

		pluginshttp.MountAuthenticated(r, pluginshttp.AuthenticatedHandlers{
			ListIntegrations:      s.listIntegrations,
			DisconnectIntegration: s.disconnectIntegration,
		})
		workflowshttp.Mount(r, workflowshttp.Handlers{
			ListSchedules:      s.listGlobalWorkflowSchedules,
			CreateSchedule:     s.createWorkflowSchedule,
			GetSchedule:        s.getGlobalWorkflowSchedule,
			UpdateSchedule:     s.updateGlobalWorkflowSchedule,
			DeleteSchedule:     s.deleteGlobalWorkflowSchedule,
			PauseSchedule:      s.pauseGlobalWorkflowSchedule,
			ResumeSchedule:     s.resumeGlobalWorkflowSchedule,
			ListEventTriggers:  s.listGlobalWorkflowEventTriggers,
			CreateEventTrigger: s.createGlobalWorkflowEventTrigger,
			GetEventTrigger:    s.getGlobalWorkflowEventTrigger,
			UpdateEventTrigger: s.updateGlobalWorkflowEventTrigger,
			DeleteEventTrigger: s.deleteGlobalWorkflowEventTrigger,
			PauseEventTrigger:  s.pauseGlobalWorkflowEventTrigger,
			ResumeEventTrigger: s.resumeGlobalWorkflowEventTrigger,
			PublishEvent:       s.publishWorkflowEvent,
			ListRuns:           s.listGlobalWorkflowRuns,
			GetRun:             s.getGlobalWorkflowRun,
			CancelRun:          s.cancelGlobalWorkflowRun,
		})
		agentshttp.Mount(r, agentshttp.Handlers{
			CreateSession:      s.createAgentSession,
			ListSessions:       s.listAgentSessions,
			ListProviders:      s.listAgentProviders,
			GetSession:         s.getAgentSession,
			UpdateSession:      s.updateAgentSession,
			CreateTurn:         s.createAgentTurn,
			ListTurns:          s.listAgentTurns,
			GetTurn:            s.getAgentTurn,
			CancelTurn:         s.cancelAgentTurn,
			ListTurnEvents:     s.listAgentTurnEvents,
			StreamTurnEvents:   s.streamAgentTurnEvents,
			ListInteractions:   s.listAgentTurnInteractions,
			ResolveInteraction: s.resolveAgentInteraction,
		})
		identityhttp.Mount(r, identityhttp.Handlers{
			StartOAuth:         s.startIntegrationOAuth,
			ConnectManual:      s.connectManual,
			CreateAPIToken:     s.createAPIToken,
			ListAPITokens:      s.listAPITokens,
			RevokeAllAPITokens: s.revokeAllAPITokens,
			RevokeAPIToken:     s.revokeAPIToken,
		})
		s.mountAuthorizationSubjectRoutes(r)
		providerdevhttp.Mount(r, providerdevhttp.Handlers{
			CreateAttachment: s.createProviderDevSession,
			ListAttachments:  s.listProviderDevAttachments,
			GetAttachment:    s.getProviderDevAttachment,
		})

	})

	pluginshttp.MountOperations(r, pluginshttp.OperationHandlers{
		ListOperations:            s.listOperations,
		ExecuteOperation:          s.executeOperation,
		PluginRouteAuthMiddleware: s.pluginRouteAuthMiddleware,
	})
}
