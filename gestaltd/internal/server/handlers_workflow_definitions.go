package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/core"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowmanager"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type workflowDefinitionInfo struct {
	ID                 string                     `json:"id"`
	Provider           string                     `json:"provider"`
	Generation         int64                      `json:"generation"`
	Target             workflowScheduleTargetInfo `json:"target"`
	Activations        []workflowActivationInfo   `json:"activations"`
	Paused             bool                       `json:"paused"`
	CreatedBySubjectID string                     `json:"createdBySubjectId,omitempty"`
	CreatedAt          *time.Time                 `json:"createdAt,omitempty"`
	UpdatedAt          *time.Time                 `json:"updatedAt,omitempty"`
	RunAs              *workflowRunAsInfo         `json:"runAs,omitempty"`
}

type workflowRunAsInfo struct {
	SubjectID           string `json:"subjectId,omitempty"`
	CredentialSubjectID string `json:"credentialSubjectId,omitempty"`
}

type workflowActivationInfo struct {
	ID       string                          `json:"id"`
	Input    any                             `json:"input,omitempty"`
	Paused   bool                            `json:"paused"`
	Schedule *workflowScheduleActivationInfo `json:"schedule,omitempty"`
	Event    *workflowEventActivationInfo    `json:"event,omitempty"`
}

type workflowScheduleActivationInfo struct {
	Cron     string `json:"cron"`
	Timezone string `json:"timezone,omitempty"`
}

type workflowEventActivationInfo struct {
	Match workflowEventMatchInfo `json:"match"`
}

type workflowEventMatchInfo struct {
	Type    string `json:"type"`
	Source  string `json:"source,omitempty"`
	Subject string `json:"subject,omitempty"`
}

type workflowDefinitionListResponse struct {
	Definitions []workflowDefinitionInfo `json:"definitions"`
}

type workflowDefinitionPauseRequest struct {
	Paused *bool `json:"paused"`
}

func (s *Server) listGlobalWorkflowDefinitions(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveWorkflowActor(w, r)
	if !ok {
		return
	}
	resp, err := s.workflowSchedules.ListDefinitions(r.Context(), p)
	if err != nil {
		s.writeWorkflowDefinitionManagerError(w, r, "", err)
		return
	}
	if resp == nil {
		resp = &workflowmanager.ListDefinitionsResponse{}
	}
	out := make([]workflowDefinitionInfo, 0, len(resp.Definitions))
	for _, managed := range resp.Definitions {
		if managed == nil || managed.Definition == nil {
			continue
		}
		out = append(out, workflowDefinitionInfoFromManaged(managed))
	}
	writeJSON(w, http.StatusOK, workflowDefinitionListResponse{Definitions: out})
}

func (s *Server) getGlobalWorkflowDefinition(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveWorkflowActor(w, r)
	if !ok {
		return
	}
	definitionID := chi.URLParam(r, "definitionID")
	managed, err := s.workflowSchedules.GetDefinition(r.Context(), p, definitionID)
	if err != nil {
		s.writeWorkflowDefinitionManagerError(w, r, definitionID, err)
		return
	}
	writeJSON(w, http.StatusOK, workflowDefinitionInfoFromManaged(managed))
}

func (s *Server) setGlobalWorkflowDefinitionPaused(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveWorkflowActor(w, r)
	if !ok {
		return
	}
	req, ok := workflowDefinitionPauseRequestFromBody(w, r)
	if !ok {
		return
	}
	definitionID := chi.URLParam(r, "definitionID")
	managed, err := s.workflowSchedules.SetDefinitionPaused(r.Context(), p, definitionID, *req.Paused)
	if err != nil {
		s.writeWorkflowDefinitionManagerError(w, r, definitionID, err)
		return
	}
	writeJSON(w, http.StatusOK, workflowDefinitionInfoFromManaged(managed))
}

func (s *Server) setGlobalWorkflowActivationPaused(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveWorkflowActor(w, r)
	if !ok {
		return
	}
	req, ok := workflowDefinitionPauseRequestFromBody(w, r)
	if !ok {
		return
	}
	definitionID := chi.URLParam(r, "definitionID")
	managed, err := s.workflowSchedules.SetActivationPaused(r.Context(), p, definitionID, chi.URLParam(r, "activationID"), *req.Paused)
	if err != nil {
		s.writeWorkflowDefinitionManagerError(w, r, definitionID, err)
		return
	}
	writeJSON(w, http.StatusOK, workflowDefinitionInfoFromManaged(managed))
}

func (s *Server) deleteGlobalWorkflowDefinition(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveWorkflowActor(w, r)
	if !ok {
		return
	}
	definitionID := chi.URLParam(r, "definitionID")
	if err := s.workflowSchedules.DeleteDefinition(r.Context(), p, definitionID); err != nil {
		s.writeWorkflowDefinitionManagerError(w, r, definitionID, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func workflowDefinitionPauseRequestFromBody(w http.ResponseWriter, r *http.Request) (workflowDefinitionPauseRequest, bool) {
	var req workflowDefinitionPauseRequest
	if r.Body != nil {
		defer func() { _ = r.Body.Close() }()
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return workflowDefinitionPauseRequest{}, false
		}
	}
	if req.Paused == nil {
		writeError(w, http.StatusBadRequest, "paused is required")
		return workflowDefinitionPauseRequest{}, false
	}
	return req, true
}

func workflowDefinitionInfoFromManaged(managed *workflowmanager.ManagedDefinition) workflowDefinitionInfo {
	if managed == nil {
		return workflowDefinitionInfo{}
	}
	return workflowDefinitionInfoFromCore(managed.Definition, strings.TrimSpace(managed.ProviderName))
}

func workflowDefinitionInfoFromCore(definition *coreworkflow.Definition, providerName string) workflowDefinitionInfo {
	info := workflowDefinitionInfo{Provider: providerName}
	if definition == nil {
		return info
	}
	info.ID = strings.TrimSpace(definition.ID)
	info.Generation = definition.Generation
	info.Target = workflowScheduleTargetInfoFromCore(definition.Target)
	info.Activations = workflowActivationsInfoFromCore(definition.Activations)
	info.Paused = definition.Paused
	info.CreatedBySubjectID = strings.TrimSpace(definition.CreatedBySubjectID)
	info.CreatedAt = definition.CreatedAt
	info.UpdatedAt = definition.UpdatedAt
	info.RunAs = workflowRunAsInfoFromCore(definition.RunAs)
	return info
}

func workflowRunAsInfoFromCore(runAs *core.RunAsSubject) *workflowRunAsInfo {
	runAs = core.NormalizeRunAsSubject(runAs)
	if runAs == nil {
		return nil
	}
	return &workflowRunAsInfo{
		SubjectID:           runAs.SubjectID,
		CredentialSubjectID: runAs.CredentialSubjectID,
	}
}

func workflowActivationsInfoFromCore(activations []coreworkflow.Activation) []workflowActivationInfo {
	if len(activations) == 0 {
		return []workflowActivationInfo{}
	}
	out := make([]workflowActivationInfo, 0, len(activations))
	for i := range activations {
		activation := activations[i]
		out = append(out, workflowActivationInfo{
			ID:       strings.TrimSpace(activation.ID),
			Input:    workflowValueInfoFromCore(activation.Input),
			Paused:   activation.Paused,
			Schedule: workflowScheduleActivationInfoFromCore(activation.Schedule),
			Event:    workflowEventActivationInfoFromCore(activation.Event),
		})
	}
	return out
}

func workflowScheduleActivationInfoFromCore(schedule *coreworkflow.ScheduleActivation) *workflowScheduleActivationInfo {
	if schedule == nil {
		return nil
	}
	return &workflowScheduleActivationInfo{
		Cron:     strings.TrimSpace(schedule.Cron),
		Timezone: strings.TrimSpace(schedule.Timezone),
	}
}

func workflowEventActivationInfoFromCore(event *coreworkflow.EventActivation) *workflowEventActivationInfo {
	if event == nil {
		return nil
	}
	return &workflowEventActivationInfo{Match: workflowEventMatchInfoFromCore(event.Match)}
}

func workflowEventMatchInfoFromCore(match coreworkflow.EventMatch) workflowEventMatchInfo {
	return workflowEventMatchInfo{
		Type:    strings.TrimSpace(match.Type),
		Source:  strings.TrimSpace(match.Source),
		Subject: strings.TrimSpace(match.Subject),
	}
}

func (s *Server) writeWorkflowDefinitionManagerError(w http.ResponseWriter, r *http.Request, definitionID string, err error) {
	switch {
	case errors.Is(err, workflowmanager.ErrWorkflowNotConfigured):
		writeError(w, http.StatusPreconditionFailed, err.Error())
	case errors.Is(err, workflowmanager.ErrWorkflowSubjectRequired):
		writeError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, invocation.ErrInvalidInvocation):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, core.ErrNotFound):
		s.writeWorkflowDefinitionProviderError(r.Context(), w, definitionID, err)
	default:
		s.writeWorkflowDefinitionProviderError(r.Context(), w, definitionID, err)
	}
}

func (s *Server) writeWorkflowDefinitionProviderError(ctx context.Context, w http.ResponseWriter, definitionID string, err error) {
	switch {
	case errors.Is(err, core.ErrNotFound):
		writeError(w, http.StatusNotFound, fmt.Sprintf("workflow definition %q not found", definitionID))
	case grpcstatus.Code(err) == codes.NotFound:
		writeError(w, http.StatusNotFound, fmt.Sprintf("workflow definition %q not found", definitionID))
	case grpcstatus.Code(err) == codes.InvalidArgument:
		writeError(w, http.StatusBadRequest, grpcstatus.Convert(err).Message())
	default:
		slog.ErrorContext(ctx, "workflow definition provider error",
			"definition_id", definitionID,
			"error", err,
		)
		writeError(w, http.StatusInternalServerError, "workflow definition request failed")
	}
}
