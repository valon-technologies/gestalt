package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/core"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowmanager"
)

type workflowEventTriggerMatchRequest struct {
	Type    string `json:"type"`
	Source  string `json:"source,omitempty"`
	Subject string `json:"subject,omitempty"`
}

type workflowEventTriggerUpsertRequest struct {
	Provider string                           `json:"provider,omitempty"`
	Match    workflowEventTriggerMatchRequest `json:"match"`
	Target   workflowScheduleTargetRequest    `json:"target"`
	Paused   bool                             `json:"paused,omitempty"`
}

type workflowEventTriggerMatchInfo struct {
	Type    string `json:"type"`
	Source  string `json:"source,omitempty"`
	Subject string `json:"subject,omitempty"`
}

type workflowEventTriggerInfo struct {
	ID        string                        `json:"id"`
	Provider  string                        `json:"provider"`
	Match     workflowEventTriggerMatchInfo `json:"match"`
	Target    workflowScheduleTargetInfo    `json:"target"`
	Paused    bool                          `json:"paused"`
	CreatedAt *time.Time                    `json:"createdAt,omitempty"`
	UpdatedAt *time.Time                    `json:"updatedAt,omitempty"`
}

func (s *Server) listGlobalWorkflowEventTriggers(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveWorkflowScheduleActor(w, r)
	if !ok {
		return
	}
	triggers, err := s.workflowSchedules.ListEventTriggers(r.Context(), p)
	if err != nil {
		s.writeWorkflowEventTriggerManagerError(w, r, "", "", "", err)
		return
	}
	out := make([]workflowEventTriggerInfo, 0, len(triggers))
	for _, managed := range triggers {
		out = append(out, workflowEventTriggerInfoFromManaged(managed))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createGlobalWorkflowEventTrigger(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveWorkflowScheduleActor(w, r)
	if !ok {
		return
	}

	var req workflowEventTriggerUpsertRequest
	if err := decodeWorkflowJSONBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if !workflowScheduleTargetRequestHasOneKind(req.Target) {
		writeError(w, http.StatusBadRequest, "workflow target must set exactly one of plugin or agent")
		return
	}
	if err := validatePublicWorkflowTargetRequest(req.Target); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(req.Match.Type) == "" {
		writeError(w, http.StatusBadRequest, "workflow trigger match.type is required")
		return
	}
	managed, err := s.workflowSchedules.CreateEventTrigger(r.Context(), p, workflowmanager.EventTriggerUpsert{
		ProviderName: strings.TrimSpace(req.Provider),
		Match:        workflowEventMatchFromRequest(req.Match),
		Target:       workflowScheduleTargetFromRequest(req.Target),
		Paused:       req.Paused,
	})
	if err != nil {
		s.writeWorkflowEventTriggerManagerError(w, r, workflowScheduleTargetErrorPlugin(req.Target), workflowScheduleTargetErrorOperation(req.Target), "", err)
		return
	}
	writeJSON(w, http.StatusCreated, workflowEventTriggerInfoFromManaged(managed))
}

func (s *Server) getGlobalWorkflowEventTrigger(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveWorkflowScheduleActor(w, r)
	if !ok {
		return
	}
	managed, err := s.workflowSchedules.GetEventTrigger(r.Context(), p, chi.URLParam(r, "triggerID"))
	if err != nil {
		s.writeWorkflowEventTriggerManagerError(w, r, "", "", chi.URLParam(r, "triggerID"), err)
		return
	}
	writeJSON(w, http.StatusOK, workflowEventTriggerInfoFromManaged(managed))
}

func (s *Server) updateGlobalWorkflowEventTrigger(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveWorkflowScheduleActor(w, r)
	if !ok {
		return
	}
	triggerID := chi.URLParam(r, "triggerID")

	var req workflowEventTriggerUpsertRequest
	if err := decodeWorkflowJSONBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if !workflowScheduleTargetRequestHasOneKind(req.Target) {
		writeError(w, http.StatusBadRequest, "workflow target must set exactly one of plugin or agent")
		return
	}
	if err := validatePublicWorkflowTargetRequest(req.Target); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(req.Match.Type) == "" {
		writeError(w, http.StatusBadRequest, "workflow trigger match.type is required")
		return
	}
	managed, err := s.workflowSchedules.UpdateEventTrigger(r.Context(), p, triggerID, workflowmanager.EventTriggerUpsert{
		ProviderName: strings.TrimSpace(req.Provider),
		Match:        workflowEventMatchFromRequest(req.Match),
		Target:       workflowScheduleTargetFromRequest(req.Target),
		Paused:       req.Paused,
	})
	if err != nil {
		s.writeWorkflowEventTriggerManagerError(w, r, workflowScheduleTargetErrorPlugin(req.Target), workflowScheduleTargetErrorOperation(req.Target), triggerID, err)
		return
	}
	writeJSON(w, http.StatusOK, workflowEventTriggerInfoFromManaged(managed))
}

func (s *Server) deleteGlobalWorkflowEventTrigger(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveWorkflowScheduleActor(w, r)
	if !ok {
		return
	}
	triggerID := chi.URLParam(r, "triggerID")
	if err := s.workflowSchedules.DeleteEventTrigger(r.Context(), p, triggerID); err != nil {
		s.writeWorkflowEventTriggerManagerError(w, r, "", "", triggerID, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) pauseGlobalWorkflowEventTrigger(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveWorkflowScheduleActor(w, r)
	if !ok {
		return
	}
	triggerID := chi.URLParam(r, "triggerID")
	managed, err := s.workflowSchedules.PauseEventTrigger(r.Context(), p, triggerID)
	if err != nil {
		s.writeWorkflowEventTriggerManagerError(w, r, "", "", triggerID, err)
		return
	}
	writeJSON(w, http.StatusOK, workflowEventTriggerInfoFromManaged(managed))
}

func (s *Server) resumeGlobalWorkflowEventTrigger(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveWorkflowScheduleActor(w, r)
	if !ok {
		return
	}
	triggerID := chi.URLParam(r, "triggerID")
	managed, err := s.workflowSchedules.ResumeEventTrigger(r.Context(), p, triggerID)
	if err != nil {
		s.writeWorkflowEventTriggerManagerError(w, r, "", "", triggerID, err)
		return
	}
	writeJSON(w, http.StatusOK, workflowEventTriggerInfoFromManaged(managed))
}

func workflowEventMatchFromRequest(match workflowEventTriggerMatchRequest) coreworkflow.EventMatch {
	return coreworkflow.EventMatch{
		Type:    strings.TrimSpace(match.Type),
		Source:  strings.TrimSpace(match.Source),
		Subject: strings.TrimSpace(match.Subject),
	}
}

func workflowEventTriggerInfoFromManaged(managed *workflowmanager.ManagedEventTrigger) workflowEventTriggerInfo {
	if managed == nil {
		return workflowEventTriggerInfo{}
	}
	return workflowEventTriggerInfoFromCore(managed.Trigger, strings.TrimSpace(managed.ProviderName))
}

func workflowEventTriggerInfoFromCore(trigger *coreworkflow.EventTrigger, providerName string) workflowEventTriggerInfo {
	info := workflowEventTriggerInfo{Provider: providerName}
	if trigger == nil {
		return info
	}
	info.ID = trigger.ID
	info.Match = workflowEventTriggerMatchInfo{
		Type:    trigger.Match.Type,
		Source:  trigger.Match.Source,
		Subject: trigger.Match.Subject,
	}
	info.Target = workflowScheduleTargetInfoFromCore(trigger.Target)
	info.Paused = trigger.Paused
	info.CreatedAt = trigger.CreatedAt
	info.UpdatedAt = trigger.UpdatedAt
	return info
}

func (s *Server) writeWorkflowEventTriggerManagerError(w http.ResponseWriter, r *http.Request, pluginName, operation, triggerID string, err error) {
	switch {
	case errors.Is(err, workflowmanager.ErrWorkflowNotConfigured),
		errors.Is(err, workflowmanager.ErrExecutionRefsNotConfigured):
		writeError(w, http.StatusPreconditionFailed, err.Error())
	case errors.Is(err, workflowmanager.ErrWorkflowScheduleSubject):
		writeError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, workflowmanager.ErrWorkflowEventMatchRequired):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, workflowmanager.ErrDuplicateExecutionRefs):
		writeError(w, http.StatusInternalServerError, err.Error())
	case errors.Is(err, invocation.ErrProviderNotFound),
		errors.Is(err, invocation.ErrOperationNotFound),
		errors.Is(err, invocation.ErrNotAuthenticated),
		errors.Is(err, invocation.ErrAuthorizationDenied),
		errors.Is(err, invocation.ErrScopeDenied),
		errors.Is(err, invocation.ErrNoCredential),
		errors.Is(err, invocation.ErrReconnectRequired),
		errors.Is(err, invocation.ErrAmbiguousInstance),
		errors.Is(err, invocation.ErrUserResolution),
		errors.Is(err, invocation.ErrInvalidInvocation),
		errors.Is(err, invocation.ErrInternal),
		errors.Is(err, core.ErrMCPOnly):
		s.writeWorkflowScheduleTargetError(w, r, pluginName, operation, err)
	case errors.Is(err, core.ErrNotFound):
		s.writeWorkflowEventTriggerProviderError(r.Context(), w, pluginName, triggerID, err)
	default:
		s.writeWorkflowEventTriggerProviderError(r.Context(), w, pluginName, triggerID, err)
	}
}

func (s *Server) writeWorkflowEventTriggerProviderError(ctx context.Context, w http.ResponseWriter, pluginName, triggerID string, err error) {
	switch {
	case errors.Is(err, core.ErrNotFound):
		writeError(w, http.StatusNotFound, fmt.Sprintf("workflow trigger %q not found", triggerID))
	default:
		slog.ErrorContext(ctx, "workflow trigger provider error",
			"plugin", pluginName,
			"trigger_id", triggerID,
			"error", err,
		)
		if strings.TrimSpace(pluginName) == "" {
			writeError(w, http.StatusInternalServerError, "workflow trigger request failed")
			return
		}
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("workflow trigger request failed for integration %q", pluginName))
	}
}
