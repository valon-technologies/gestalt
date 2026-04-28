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
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/workflowmanager"
)

type workflowScheduleTargetRequest struct {
	Plugin *workflowPluginTargetRequest `json:"plugin,omitempty"`
	Agent  *workflowAgentTargetRequest  `json:"agent,omitempty"`
}

type workflowPluginTargetRequest struct {
	Name       string         `json:"name,omitempty"`
	Operation  string         `json:"operation"`
	Connection string         `json:"connection,omitempty"`
	Instance   string         `json:"instance,omitempty"`
	Input      map[string]any `json:"input,omitempty"`
}

type workflowAgentTargetRequest struct {
	ProviderName    string                `json:"provider,omitempty"`
	Model           string                `json:"model,omitempty"`
	Prompt          string                `json:"prompt,omitempty"`
	Messages        []agentMessageRequest `json:"messages,omitempty"`
	ToolRefs        []agentToolRefRequest `json:"toolRefs,omitempty"`
	Tools           []agentToolRefRequest `json:"tools,omitempty"`
	ResponseSchema  map[string]any        `json:"responseSchema,omitempty"`
	Metadata        map[string]any        `json:"metadata,omitempty"`
	ProviderOptions map[string]any        `json:"providerOptions,omitempty"`
	TimeoutSeconds  int                   `json:"timeoutSeconds,omitempty"`
}

type workflowScheduleUpsertRequest struct {
	Provider   string                        `json:"provider,omitempty"`
	Cron       string                        `json:"cron"`
	Timezone   string                        `json:"timezone,omitempty"`
	Target     workflowScheduleTargetRequest `json:"target"`
	Completion workflowCompletionRequest     `json:"completion,omitempty"`
	Paused     bool                          `json:"paused,omitempty"`
}

type workflowScheduleTargetInfo struct {
	Plugin *workflowPluginTargetInfo `json:"plugin,omitempty"`
	Agent  *workflowAgentTargetInfo  `json:"agent,omitempty"`
}

type workflowPluginTargetInfo struct {
	Name       string         `json:"name"`
	Operation  string         `json:"operation"`
	Connection string         `json:"connection,omitempty"`
	Instance   string         `json:"instance,omitempty"`
	Input      map[string]any `json:"input,omitempty"`
}

type workflowAgentTargetInfo struct {
	ProviderName    string                `json:"provider,omitempty"`
	Model           string                `json:"model,omitempty"`
	Prompt          string                `json:"prompt,omitempty"`
	Messages        []agentMessageRequest `json:"messages,omitempty"`
	ToolRefs        []agentToolRefRequest `json:"toolRefs,omitempty"`
	ResponseSchema  map[string]any        `json:"responseSchema,omitempty"`
	Metadata        map[string]any        `json:"metadata,omitempty"`
	ProviderOptions map[string]any        `json:"providerOptions,omitempty"`
	TimeoutSeconds  int                   `json:"timeoutSeconds,omitempty"`
}

type workflowScheduleInfo struct {
	ID         string                     `json:"id"`
	Provider   string                     `json:"provider"`
	Cron       string                     `json:"cron"`
	Timezone   string                     `json:"timezone,omitempty"`
	Target     workflowScheduleTargetInfo `json:"target"`
	Completion *workflowCompletionInfo    `json:"completion,omitempty"`
	Paused     bool                       `json:"paused"`
	CreatedAt  *time.Time                 `json:"createdAt,omitempty"`
	UpdatedAt  *time.Time                 `json:"updatedAt,omitempty"`
	NextRunAt  *time.Time                 `json:"nextRunAt,omitempty"`
}

type workflowCompletionRequest struct {
	OnSuccess *workflowCompletionDeliveryRequest `json:"onSuccess,omitempty"`
	OnFailure *workflowCompletionDeliveryRequest `json:"onFailure,omitempty"`
}

type workflowCompletionDeliveryRequest struct {
	Plugin     *workflowPluginTargetRequest `json:"plugin,omitempty"`
	BestEffort bool                         `json:"bestEffort,omitempty"`
}

type workflowCompletionInfo struct {
	OnSuccess *workflowCompletionDeliveryInfo `json:"onSuccess,omitempty"`
	OnFailure *workflowCompletionDeliveryInfo `json:"onFailure,omitempty"`
}

type workflowCompletionDeliveryInfo struct {
	Plugin     *workflowPluginTargetInfo `json:"plugin,omitempty"`
	BestEffort bool                      `json:"bestEffort,omitempty"`
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
	if !workflowScheduleTargetRequestHasOneKind(req.Target) {
		writeError(w, http.StatusBadRequest, "workflow target must set exactly one of plugin or agent")
		return
	}
	managed, err := s.workflowSchedules.CreateSchedule(r.Context(), p, workflowmanager.ScheduleUpsert{
		ProviderName: strings.TrimSpace(req.Provider),
		Cron:         strings.TrimSpace(req.Cron),
		Timezone:     strings.TrimSpace(req.Timezone),
		Target:       workflowScheduleTargetFromRequest(req.Target),
		Completion:   workflowCompletionFromRequest(req.Completion),
		Paused:       req.Paused,
	})
	if err != nil {
		s.writeWorkflowScheduleManagerError(w, r, workflowScheduleTargetErrorPlugin(req.Target), workflowScheduleTargetErrorOperation(req.Target), "", err)
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
	if !workflowScheduleTargetRequestHasOneKind(req.Target) {
		writeError(w, http.StatusBadRequest, "workflow target must set exactly one of plugin or agent")
		return
	}
	managed, err := s.workflowSchedules.UpdateSchedule(r.Context(), p, scheduleID, workflowmanager.ScheduleUpsert{
		ProviderName: strings.TrimSpace(req.Provider),
		Cron:         strings.TrimSpace(req.Cron),
		Timezone:     strings.TrimSpace(req.Timezone),
		Target:       workflowScheduleTargetFromRequest(req.Target),
		Completion:   workflowCompletionFromRequest(req.Completion),
		Paused:       req.Paused,
	})
	if err != nil {
		s.writeWorkflowScheduleManagerError(w, r, workflowScheduleTargetErrorPlugin(req.Target), workflowScheduleTargetErrorOperation(req.Target), scheduleID, err)
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
	if target.Agent != nil {
		agentTarget := workflowAgentTargetFromRequest(target.Agent)
		return coreworkflow.Target{Agent: &agentTarget}
	}
	plugin := workflowPluginTargetFromRequest(target.Plugin)
	pluginTarget := coreworkflow.PluginTarget{
		PluginName: strings.TrimSpace(plugin.Name),
		Operation:  strings.TrimSpace(plugin.Operation),
		Connection: strings.TrimSpace(plugin.Connection),
		Instance:   strings.TrimSpace(plugin.Instance),
		Input:      maps.Clone(plugin.Input),
	}
	return coreworkflow.Target{
		Plugin: &pluginTarget,
	}
}

func workflowCompletionFromRequest(completion workflowCompletionRequest) coreworkflow.Completion {
	return coreworkflow.Completion{
		OnSuccess: workflowCompletionDeliveryFromRequest(completion.OnSuccess),
		OnFailure: workflowCompletionDeliveryFromRequest(completion.OnFailure),
	}
}

func workflowCompletionDeliveryFromRequest(delivery *workflowCompletionDeliveryRequest) *coreworkflow.CompletionDelivery {
	if delivery == nil || delivery.Plugin == nil {
		return nil
	}
	plugin := workflowPluginTargetFromRequest(delivery.Plugin)
	pluginTarget := coreworkflow.PluginTarget{
		PluginName: strings.TrimSpace(plugin.Name),
		Operation:  strings.TrimSpace(plugin.Operation),
		Connection: strings.TrimSpace(plugin.Connection),
		Instance:   strings.TrimSpace(plugin.Instance),
		Input:      maps.Clone(plugin.Input),
	}
	return &coreworkflow.CompletionDelivery{
		Plugin:     &pluginTarget,
		BestEffort: delivery.BestEffort,
	}
}

func workflowPluginTargetFromRequest(target *workflowPluginTargetRequest) workflowPluginTargetRequest {
	if target == nil {
		return workflowPluginTargetRequest{}
	}
	return *target
}

func workflowAgentTargetFromRequest(target *workflowAgentTargetRequest) coreworkflow.AgentTarget {
	if target == nil {
		return coreworkflow.AgentTarget{}
	}
	toolRefs := target.ToolRefs
	if len(toolRefs) == 0 {
		toolRefs = target.Tools
	}
	return coreworkflow.AgentTarget{
		ProviderName:    strings.TrimSpace(target.ProviderName),
		Model:           strings.TrimSpace(target.Model),
		Prompt:          strings.TrimSpace(target.Prompt),
		Messages:        agentMessagesFromRequest(target.Messages),
		ToolRefs:        agentToolRefsFromRequest(toolRefs),
		ToolSource:      coreagent.ToolSourceModeNativeSearch,
		ResponseSchema:  maps.Clone(target.ResponseSchema),
		Metadata:        maps.Clone(target.Metadata),
		ProviderOptions: maps.Clone(target.ProviderOptions),
		TimeoutSeconds:  target.TimeoutSeconds,
	}
}

func workflowScheduleTargetRequestHasOneKind(target workflowScheduleTargetRequest) bool {
	hasPlugin := workflowScheduleTargetRequestHasPluginFields(target)
	hasAgent := target.Agent != nil
	return hasPlugin != hasAgent
}

func workflowScheduleTargetRequestHasPluginFields(target workflowScheduleTargetRequest) bool {
	return workflowPluginTargetRequestHasFields(workflowPluginTargetFromRequest(target.Plugin))
}

func workflowPluginTargetRequestHasFields(target workflowPluginTargetRequest) bool {
	return strings.TrimSpace(target.Name) != "" ||
		strings.TrimSpace(target.Operation) != "" ||
		strings.TrimSpace(target.Connection) != "" ||
		strings.TrimSpace(target.Instance) != "" ||
		len(target.Input) > 0
}

func workflowScheduleTargetErrorPlugin(target workflowScheduleTargetRequest) string {
	if target.Agent != nil {
		return "agent"
	}
	return strings.TrimSpace(workflowPluginTargetFromRequest(target.Plugin).Name)
}

func workflowScheduleTargetErrorOperation(target workflowScheduleTargetRequest) string {
	if target.Agent != nil {
		return "turn"
	}
	return strings.TrimSpace(workflowPluginTargetFromRequest(target.Plugin).Operation)
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
	info.Target = workflowScheduleTargetInfoFromCore(schedule.Target)
	info.Completion = workflowCompletionInfoFromCore(schedule.Completion)
	return info
}

func workflowScheduleTargetInfoFromCore(target coreworkflow.Target) workflowScheduleTargetInfo {
	if target.Agent != nil {
		agentTarget := *target.Agent
		return workflowScheduleTargetInfo{
			Agent: &workflowAgentTargetInfo{
				ProviderName:    agentTarget.ProviderName,
				Model:           agentTarget.Model,
				Prompt:          agentTarget.Prompt,
				Messages:        agentMessageInfoFromCore(agentTarget.Messages),
				ToolRefs:        agentToolRefsToRequest(agentTarget.ToolRefs),
				ResponseSchema:  maps.Clone(agentTarget.ResponseSchema),
				Metadata:        maps.Clone(agentTarget.Metadata),
				ProviderOptions: maps.Clone(agentTarget.ProviderOptions),
				TimeoutSeconds:  agentTarget.TimeoutSeconds,
			},
		}
	}
	if target.Plugin == nil {
		return workflowScheduleTargetInfo{}
	}
	pluginTarget := *target.Plugin
	return workflowScheduleTargetInfo{
		Plugin: &workflowPluginTargetInfo{
			Name:       pluginTarget.PluginName,
			Operation:  pluginTarget.Operation,
			Connection: userFacingConnectionName(pluginTarget.Connection),
			Instance:   pluginTarget.Instance,
			Input:      maps.Clone(pluginTarget.Input),
		},
	}
}

func workflowCompletionInfoFromCore(completion coreworkflow.Completion) *workflowCompletionInfo {
	if coreworkflow.CompletionEmpty(completion) {
		return nil
	}
	return &workflowCompletionInfo{
		OnSuccess: workflowCompletionDeliveryInfoFromCore(completion.OnSuccess),
		OnFailure: workflowCompletionDeliveryInfoFromCore(completion.OnFailure),
	}
}

func workflowCompletionDeliveryInfoFromCore(delivery *coreworkflow.CompletionDelivery) *workflowCompletionDeliveryInfo {
	if coreworkflow.CompletionDeliveryEmpty(delivery) {
		return nil
	}
	return &workflowCompletionDeliveryInfo{
		Plugin:     workflowPluginTargetInfoFromCore(*delivery.Plugin),
		BestEffort: delivery.BestEffort,
	}
}

func workflowPluginTargetInfoFromCore(pluginTarget coreworkflow.PluginTarget) *workflowPluginTargetInfo {
	return &workflowPluginTargetInfo{
		Name:       pluginTarget.PluginName,
		Operation:  pluginTarget.Operation,
		Connection: userFacingConnectionName(pluginTarget.Connection),
		Instance:   pluginTarget.Instance,
		Input:      maps.Clone(pluginTarget.Input),
	}
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
		errors.Is(err, invocation.ErrNoCredential),
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
		errors.Is(err, invocation.ErrNoCredential),
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
