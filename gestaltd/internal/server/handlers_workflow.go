package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/core"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/workflowmanager"
)

type workflowScheduleTargetRequest struct {
	Plugin     string         `json:"plugin,omitempty"`
	Operation  string         `json:"operation"`
	Connection string         `json:"connection,omitempty"`
	Instance   string         `json:"instance,omitempty"`
	Input      map[string]any `json:"input,omitempty"`
}

type workflowScheduleUpsertRequest struct {
	Provider string                        `json:"provider,omitempty"`
	Cron     string                        `json:"cron"`
	Timezone string                        `json:"timezone,omitempty"`
	Target   workflowScheduleTargetRequest `json:"target"`
	Paused   bool                          `json:"paused,omitempty"`
}

type workflowScheduleTargetInfo struct {
	Plugin     string         `json:"plugin,omitempty"`
	Operation  string         `json:"operation"`
	Connection string         `json:"connection,omitempty"`
	Instance   string         `json:"instance,omitempty"`
	Input      map[string]any `json:"input,omitempty"`
}

type workflowScheduleInfo struct {
	ID        string                     `json:"id"`
	Provider  string                     `json:"provider"`
	Cron      string                     `json:"cron"`
	Timezone  string                     `json:"timezone,omitempty"`
	Target    workflowScheduleTargetInfo `json:"target"`
	Paused    bool                       `json:"paused"`
	CreatedAt *time.Time                 `json:"createdAt,omitempty"`
	UpdatedAt *time.Time                 `json:"updatedAt,omitempty"`
	NextRunAt *time.Time                 `json:"nextRunAt,omitempty"`
}

func (s *Server) listGlobalWorkflowSchedules(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveWorkflowScheduleActor(w, r)
	if !ok {
		return
	}
	schedules, err := s.workflowSchedules.ListSchedules(r.Context(), p)
	if err != nil {
		s.writeWorkflowScheduleManagerError(w, r, "", "", "", err)
		return
	}
	out := make([]workflowScheduleInfo, 0, len(schedules))
	for _, managed := range schedules {
		out = append(out, workflowScheduleInfoFromManaged(managed))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createWorkflowSchedule(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveWorkflowScheduleActor(w, r)
	if !ok {
		return
	}

	var req workflowScheduleUpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	pluginName := strings.TrimSpace(req.Target.Plugin)
	if pluginName == "" {
		writeError(w, http.StatusBadRequest, "workflow target plugin is required")
		return
	}
	managed, err := s.workflowSchedules.CreateSchedule(r.Context(), p, workflowmanager.ScheduleUpsert{
		ProviderName: strings.TrimSpace(req.Provider),
		Cron:         strings.TrimSpace(req.Cron),
		Timezone:     strings.TrimSpace(req.Timezone),
		Target:       workflowScheduleTargetFromRequest(req.Target),
		Paused:       req.Paused,
	})
	if err != nil {
		s.writeWorkflowScheduleManagerError(w, r, pluginName, strings.TrimSpace(req.Target.Operation), "", err)
		return
	}
	writeJSON(w, http.StatusCreated, workflowScheduleInfoFromManaged(managed))
}

func (s *Server) getGlobalWorkflowSchedule(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveWorkflowScheduleActor(w, r)
	if !ok {
		return
	}
	managed, err := s.workflowSchedules.GetSchedule(r.Context(), p, chi.URLParam(r, "scheduleID"))
	if err != nil {
		s.writeWorkflowScheduleManagerError(w, r, "", "", chi.URLParam(r, "scheduleID"), err)
		return
	}
	writeJSON(w, http.StatusOK, workflowScheduleInfoFromManaged(managed))
}

func (s *Server) updateGlobalWorkflowSchedule(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveWorkflowScheduleActor(w, r)
	if !ok {
		return
	}
	scheduleID := chi.URLParam(r, "scheduleID")

	var req workflowScheduleUpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	pluginName := strings.TrimSpace(req.Target.Plugin)
	if pluginName == "" {
		writeError(w, http.StatusBadRequest, "workflow target plugin is required")
		return
	}
	managed, err := s.workflowSchedules.UpdateSchedule(r.Context(), p, scheduleID, workflowmanager.ScheduleUpsert{
		ProviderName: strings.TrimSpace(req.Provider),
		Cron:         strings.TrimSpace(req.Cron),
		Timezone:     strings.TrimSpace(req.Timezone),
		Target:       workflowScheduleTargetFromRequest(req.Target),
		Paused:       req.Paused,
	})
	if err != nil {
		s.writeWorkflowScheduleManagerError(w, r, pluginName, strings.TrimSpace(req.Target.Operation), scheduleID, err)
		return
	}
	writeJSON(w, http.StatusOK, workflowScheduleInfoFromManaged(managed))
}

func (s *Server) deleteGlobalWorkflowSchedule(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveWorkflowScheduleActor(w, r)
	if !ok {
		return
	}
	scheduleID := chi.URLParam(r, "scheduleID")
	if err := s.workflowSchedules.DeleteSchedule(r.Context(), p, scheduleID); err != nil {
		s.writeWorkflowScheduleManagerError(w, r, "", "", scheduleID, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) pauseGlobalWorkflowSchedule(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveWorkflowScheduleActor(w, r)
	if !ok {
		return
	}
	scheduleID := chi.URLParam(r, "scheduleID")
	managed, err := s.workflowSchedules.PauseSchedule(r.Context(), p, scheduleID)
	if err != nil {
		s.writeWorkflowScheduleManagerError(w, r, "", "", scheduleID, err)
		return
	}
	writeJSON(w, http.StatusOK, workflowScheduleInfoFromManaged(managed))
}

func (s *Server) resumeGlobalWorkflowSchedule(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveWorkflowScheduleActor(w, r)
	if !ok {
		return
	}
	scheduleID := chi.URLParam(r, "scheduleID")
	managed, err := s.workflowSchedules.ResumeSchedule(r.Context(), p, scheduleID)
	if err != nil {
		s.writeWorkflowScheduleManagerError(w, r, "", "", scheduleID, err)
		return
	}
	writeJSON(w, http.StatusOK, workflowScheduleInfoFromManaged(managed))
}

func (s *Server) resolveWorkflowScheduleActor(w http.ResponseWriter, r *http.Request) (*principal.Principal, bool) {
	p := principal.Canonicalized(PrincipalFromContext(r.Context()))
	if p == nil {
		writeError(w, http.StatusUnauthorized, "missing authorization")
		return nil, false
	}
	if strings.TrimSpace(p.SubjectID) == "" {
		writeError(w, http.StatusUnauthorized, "missing subject")
		return nil, false
	}
	return p, true
}

func workflowScheduleTargetFromRequest(target workflowScheduleTargetRequest) coreworkflow.Target {
	return coreworkflow.Target{
		PluginName: strings.TrimSpace(target.Plugin),
		Operation:  strings.TrimSpace(target.Operation),
		Connection: strings.TrimSpace(target.Connection),
		Instance:   strings.TrimSpace(target.Instance),
		Input:      maps.Clone(target.Input),
	}
}

func workflowScheduleInfoFromManaged(managed *workflowmanager.ManagedSchedule) workflowScheduleInfo {
	if managed == nil {
		return workflowScheduleInfo{}
	}
	return workflowScheduleInfoFromCore(managed.Schedule, strings.TrimSpace(managed.ProviderName))
}

func workflowScheduleInfoFromCore(schedule *coreworkflow.Schedule, providerName string) workflowScheduleInfo {
	info := workflowScheduleInfo{
		Provider: providerName,
	}
	if schedule == nil {
		return info
	}
	info.ID = schedule.ID
	info.Cron = schedule.Cron
	info.Timezone = schedule.Timezone
	info.Paused = schedule.Paused
	info.CreatedAt = schedule.CreatedAt
	info.UpdatedAt = schedule.UpdatedAt
	info.NextRunAt = schedule.NextRunAt
	info.Target = workflowScheduleTargetInfo{
		Plugin:     schedule.Target.PluginName,
		Operation:  schedule.Target.Operation,
		Connection: userFacingConnectionName(schedule.Target.Connection),
		Instance:   schedule.Target.Instance,
		Input:      maps.Clone(schedule.Target.Input),
	}
	return info
}

func (s *Server) writeWorkflowScheduleProviderError(ctx context.Context, w http.ResponseWriter, pluginName, scheduleID string, err error) {
	switch {
	case errors.Is(err, core.ErrNotFound):
		writeError(w, http.StatusNotFound, fmt.Sprintf("workflow schedule %q not found", scheduleID))
	default:
		slog.ErrorContext(ctx, "workflow schedule provider error",
			"plugin", pluginName,
			"schedule_id", scheduleID,
			"error", err,
		)
		if strings.TrimSpace(pluginName) == "" {
			writeError(w, http.StatusInternalServerError, "workflow schedule request failed")
			return
		}
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("workflow schedule request failed for integration %q", pluginName))
	}
}

func (s *Server) writeWorkflowScheduleManagerError(w http.ResponseWriter, r *http.Request, pluginName, operation, scheduleID string, err error) {
	switch {
	case errors.Is(err, workflowmanager.ErrWorkflowNotConfigured),
		errors.Is(err, workflowmanager.ErrExecutionRefsNotConfigured):
		writeError(w, http.StatusPreconditionFailed, err.Error())
	case errors.Is(err, workflowmanager.ErrWorkflowScheduleSubject):
		writeError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, workflowmanager.ErrDuplicateExecutionRefs):
		writeError(w, http.StatusInternalServerError, err.Error())
	case errors.Is(err, invocation.ErrProviderNotFound),
		errors.Is(err, invocation.ErrOperationNotFound),
		errors.Is(err, invocation.ErrNotAuthenticated),
		errors.Is(err, invocation.ErrAuthorizationDenied),
		errors.Is(err, invocation.ErrScopeDenied),
		errors.Is(err, invocation.ErrNoToken),
		errors.Is(err, invocation.ErrReconnectRequired),
		errors.Is(err, invocation.ErrAmbiguousInstance),
		errors.Is(err, invocation.ErrUserResolution),
		errors.Is(err, invocation.ErrInternal),
		errors.Is(err, core.ErrMCPOnly):
		s.writeWorkflowScheduleTargetError(w, r, pluginName, operation, err)
	case errors.Is(err, core.ErrNotFound):
		s.writeWorkflowScheduleProviderError(r.Context(), w, pluginName, scheduleID, err)
	default:
		s.writeWorkflowScheduleProviderError(r.Context(), w, pluginName, scheduleID, err)
	}
}

func (s *Server) writeWorkflowScheduleTargetError(w http.ResponseWriter, r *http.Request, pluginName, operation string, err error) {
	switch {
	case errors.Is(err, invocation.ErrProviderNotFound),
		errors.Is(err, invocation.ErrOperationNotFound),
		errors.Is(err, invocation.ErrNotAuthenticated),
		errors.Is(err, invocation.ErrAuthorizationDenied),
		errors.Is(err, invocation.ErrScopeDenied),
		errors.Is(err, invocation.ErrNoToken),
		errors.Is(err, invocation.ErrReconnectRequired),
		errors.Is(err, invocation.ErrAmbiguousInstance),
		errors.Is(err, invocation.ErrUserResolution),
		errors.Is(err, invocation.ErrInternal),
		errors.Is(err, core.ErrMCPOnly):
		s.writeInvocationError(w, r, pluginName, operation, err)
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}
