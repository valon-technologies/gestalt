package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/core"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowmanager"
)

type workflowScheduleTargetRequest struct {
	Plugin *workflowPluginTargetRequest `json:"plugin,omitempty"`
	Agent  *workflowAgentTargetRequest  `json:"agent,omitempty"`
}

type workflowPluginTargetRequest struct {
	Name           string         `json:"name,omitempty"`
	Operation      string         `json:"operation"`
	Connection     string         `json:"connection,omitempty"`
	Instance       string         `json:"instance,omitempty"`
	CredentialMode string         `json:"credentialMode,omitempty"`
	Input          map[string]any `json:"input,omitempty"`
}

type workflowAgentTargetRequest struct {
	ProviderName   string                         `json:"provider,omitempty"`
	Model          string                         `json:"model,omitempty"`
	Prompt         string                         `json:"prompt,omitempty"`
	Messages       []agentMessageRequest          `json:"messages,omitempty"`
	ToolRefs       []agentToolRefRequest          `json:"toolRefs,omitempty"`
	OutputDelivery *workflowOutputDeliveryRequest `json:"outputDelivery,omitempty"`
	ResponseSchema map[string]any                 `json:"responseSchema,omitempty"`
	Metadata       map[string]any                 `json:"metadata,omitempty"`
	ModelOptions   map[string]any                 `json:"modelOptions,omitempty"`
	TimeoutSeconds int                            `json:"timeoutSeconds,omitempty"`
}

type workflowOutputDeliveryRequest struct {
	Target         workflowPluginTargetRequest    `json:"target"`
	InputBindings  []workflowOutputBindingRequest `json:"inputBindings,omitempty"`
	CredentialMode string                         `json:"credentialMode,omitempty"`
}

type workflowOutputBindingRequest struct {
	InputField string                           `json:"inputField"`
	Value      workflowOutputValueSourceRequest `json:"value"`
}

type workflowOutputValueSourceRequest struct {
	AgentOutput    string `json:"agentOutput,omitempty"`
	SignalPayload  string `json:"signalPayload,omitempty"`
	SignalMetadata string `json:"signalMetadata,omitempty"`
	Literal        any    `json:"literal,omitempty"`
}

type workflowScheduleUpsertRequest struct {
	Provider string                        `json:"provider,omitempty"`
	Cron     string                        `json:"cron"`
	Timezone string                        `json:"timezone,omitempty"`
	Target   workflowScheduleTargetRequest `json:"target"`
	Paused   bool                          `json:"paused,omitempty"`
}

type workflowScheduleTargetInfo struct {
	Plugin *workflowPluginTargetInfo `json:"plugin,omitempty"`
	Agent  *workflowAgentTargetInfo  `json:"agent,omitempty"`
}

type workflowPluginTargetInfo struct {
	Name           string         `json:"name"`
	Operation      string         `json:"operation"`
	Connection     string         `json:"connection,omitempty"`
	Instance       string         `json:"instance,omitempty"`
	CredentialMode string         `json:"credentialMode,omitempty"`
	Input          map[string]any `json:"input,omitempty"`
}

type workflowAgentTargetInfo struct {
	ProviderName   string                      `json:"provider,omitempty"`
	Model          string                      `json:"model,omitempty"`
	Prompt         string                      `json:"prompt,omitempty"`
	Messages       []agentMessageRequest       `json:"messages,omitempty"`
	ToolRefs       []agentToolRefRequest       `json:"toolRefs,omitempty"`
	OutputDelivery *workflowOutputDeliveryInfo `json:"outputDelivery,omitempty"`
	ResponseSchema map[string]any              `json:"responseSchema,omitempty"`
	Metadata       map[string]any              `json:"metadata,omitempty"`
	ModelOptions   map[string]any              `json:"modelOptions,omitempty"`
	TimeoutSeconds int                         `json:"timeoutSeconds,omitempty"`
}

type workflowOutputDeliveryInfo struct {
	Target         workflowPluginTargetInfo    `json:"target"`
	InputBindings  []workflowOutputBindingInfo `json:"inputBindings,omitempty"`
	CredentialMode string                      `json:"credentialMode,omitempty"`
}

type workflowOutputBindingInfo struct {
	InputField string                        `json:"inputField"`
	Value      workflowOutputValueSourceInfo `json:"value"`
}

type workflowOutputValueSourceInfo struct {
	AgentOutput    string `json:"agentOutput,omitempty"`
	SignalPayload  string `json:"signalPayload,omitempty"`
	SignalMetadata string `json:"signalMetadata,omitempty"`
	Literal        any    `json:"literal,omitempty"`
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
	managed, err := s.workflowSchedules.CreateSchedule(r.Context(), p, workflowmanager.ScheduleUpsert{
		ProviderName: strings.TrimSpace(req.Provider),
		Cron:         strings.TrimSpace(req.Cron),
		Timezone:     strings.TrimSpace(req.Timezone),
		Target:       workflowScheduleTargetFromRequest(req.Target),
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
	managed, err := s.workflowSchedules.UpdateSchedule(r.Context(), p, scheduleID, workflowmanager.ScheduleUpsert{
		ProviderName: strings.TrimSpace(req.Provider),
		Cron:         strings.TrimSpace(req.Cron),
		Timezone:     strings.TrimSpace(req.Timezone),
		Target:       workflowScheduleTargetFromRequest(req.Target),
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
		PluginName:     strings.TrimSpace(plugin.Name),
		Operation:      strings.TrimSpace(plugin.Operation),
		Connection:     strings.TrimSpace(plugin.Connection),
		Instance:       strings.TrimSpace(plugin.Instance),
		CredentialMode: core.ConnectionMode(strings.ToLower(strings.TrimSpace(plugin.CredentialMode))),
		Input:          maps.Clone(plugin.Input),
	}
	return coreworkflow.Target{
		Plugin: &pluginTarget,
	}
}

func decodeWorkflowJSONBody(r *http.Request, dst any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("invalid trailing JSON body")
		}
		return err
	}
	return nil
}

func workflowPluginTargetFromRequest(target *workflowPluginTargetRequest) workflowPluginTargetRequest {
	if target == nil {
		return workflowPluginTargetRequest{}
	}
	return *target
}

func validatePublicWorkflowTargetRequest(target workflowScheduleTargetRequest) error {
	if target.Plugin != nil {
		if strings.TrimSpace(target.Plugin.CredentialMode) != "" {
			return fmt.Errorf("workflow target plugin.credentialMode is not supported on public requests")
		}
		return nil
	}
	if target.Agent != nil && target.Agent.OutputDelivery != nil && strings.TrimSpace(target.Agent.OutputDelivery.Target.CredentialMode) != "" {
		return fmt.Errorf("workflow target agent.outputDelivery.target.credentialMode is not supported")
	}
	return nil
}

func workflowAgentTargetFromRequest(target *workflowAgentTargetRequest) coreworkflow.AgentTarget {
	if target == nil {
		return coreworkflow.AgentTarget{}
	}
	return coreworkflow.AgentTarget{
		ProviderName:   strings.TrimSpace(target.ProviderName),
		Model:          strings.TrimSpace(target.Model),
		Prompt:         strings.TrimSpace(target.Prompt),
		Messages:       agentMessagesFromRequest(target.Messages),
		ToolRefs:       agentToolRefsFromRequest(target.ToolRefs),
		OutputDelivery: workflowOutputDeliveryFromRequest(target.OutputDelivery),
		ResponseSchema: maps.Clone(target.ResponseSchema),
		Metadata:       maps.Clone(target.Metadata),
		ModelOptions:   maps.Clone(target.ModelOptions),
		TimeoutSeconds: target.TimeoutSeconds,
	}
}

func workflowOutputDeliveryFromRequest(delivery *workflowOutputDeliveryRequest) *coreworkflow.OutputDelivery {
	if delivery == nil {
		return nil
	}
	return &coreworkflow.OutputDelivery{
		Target: coreworkflow.PluginTarget{
			PluginName:     strings.TrimSpace(delivery.Target.Name),
			Operation:      strings.TrimSpace(delivery.Target.Operation),
			Connection:     strings.TrimSpace(delivery.Target.Connection),
			Instance:       strings.TrimSpace(delivery.Target.Instance),
			CredentialMode: core.ConnectionMode(strings.ToLower(strings.TrimSpace(delivery.Target.CredentialMode))),
			Input:          maps.Clone(delivery.Target.Input),
		},
		InputBindings:  workflowOutputBindingsFromRequest(delivery.InputBindings),
		CredentialMode: core.ConnectionMode(strings.ToLower(strings.TrimSpace(delivery.CredentialMode))),
	}
}

func workflowOutputBindingsFromRequest(bindings []workflowOutputBindingRequest) []coreworkflow.OutputBinding {
	if len(bindings) == 0 {
		return nil
	}
	out := make([]coreworkflow.OutputBinding, 0, len(bindings))
	for _, binding := range bindings {
		out = append(out, coreworkflow.OutputBinding{
			InputField: strings.TrimSpace(binding.InputField),
			Value: coreworkflow.OutputValueSource{
				AgentOutput:    strings.TrimSpace(binding.Value.AgentOutput),
				SignalPayload:  strings.TrimSpace(binding.Value.SignalPayload),
				SignalMetadata: strings.TrimSpace(binding.Value.SignalMetadata),
				Literal:        binding.Value.Literal,
			},
		})
	}
	return out
}

func workflowScheduleTargetRequestHasOneKind(target workflowScheduleTargetRequest) bool {
	hasPlugin := target.Plugin != nil
	hasAgent := target.Agent != nil
	return hasPlugin != hasAgent
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
	return info
}

func workflowScheduleTargetInfoFromCore(target coreworkflow.Target) workflowScheduleTargetInfo {
	if target.Agent != nil {
		agentTarget := *target.Agent
		return workflowScheduleTargetInfo{
			Agent: &workflowAgentTargetInfo{
				ProviderName:   agentTarget.ProviderName,
				Model:          agentTarget.Model,
				Prompt:         agentTarget.Prompt,
				Messages:       agentMessageInfoFromCore(agentTarget.Messages),
				ToolRefs:       agentToolRefsToRequest(agentTarget.ToolRefs),
				OutputDelivery: workflowOutputDeliveryInfoFromCore(agentTarget.OutputDelivery),
				ResponseSchema: maps.Clone(agentTarget.ResponseSchema),
				Metadata:       maps.Clone(agentTarget.Metadata),
				ModelOptions:   maps.Clone(agentTarget.ModelOptions),
				TimeoutSeconds: agentTarget.TimeoutSeconds,
			},
		}
	}
	if target.Plugin == nil {
		return workflowScheduleTargetInfo{}
	}
	pluginTarget := *target.Plugin
	return workflowScheduleTargetInfo{
		Plugin: &workflowPluginTargetInfo{
			Name:           pluginTarget.PluginName,
			Operation:      pluginTarget.Operation,
			Connection:     userFacingConnectionName(pluginTarget.Connection),
			Instance:       pluginTarget.Instance,
			CredentialMode: string(pluginTarget.CredentialMode),
			Input:          maps.Clone(pluginTarget.Input),
		},
	}
}

func workflowOutputDeliveryInfoFromCore(delivery *coreworkflow.OutputDelivery) *workflowOutputDeliveryInfo {
	if delivery == nil {
		return nil
	}
	return &workflowOutputDeliveryInfo{
		Target: workflowPluginTargetInfo{
			Name:       delivery.Target.PluginName,
			Operation:  delivery.Target.Operation,
			Connection: userFacingConnectionName(delivery.Target.Connection),
			Instance:   delivery.Target.Instance,
			Input:      maps.Clone(delivery.Target.Input),
		},
		InputBindings:  workflowOutputBindingInfoFromCore(delivery.InputBindings),
		CredentialMode: string(delivery.CredentialMode),
	}
}

func workflowOutputBindingInfoFromCore(bindings []coreworkflow.OutputBinding) []workflowOutputBindingInfo {
	if len(bindings) == 0 {
		return nil
	}
	out := make([]workflowOutputBindingInfo, 0, len(bindings))
	for _, binding := range bindings {
		out = append(out, workflowOutputBindingInfo{
			InputField: binding.InputField,
			Value: workflowOutputValueSourceInfo{
				AgentOutput:    binding.Value.AgentOutput,
				SignalPayload:  binding.Value.SignalPayload,
				SignalMetadata: binding.Value.SignalMetadata,
				Literal:        binding.Value.Literal,
			},
		})
	}
	return out
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
		errors.Is(err, invocation.ErrInvalidInvocation),
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
		errors.Is(err, invocation.ErrInvalidInvocation),
		errors.Is(err, invocation.ErrInternal),
		errors.Is(err, core.ErrMCPOnly):
		s.writeInvocationError(w, r, pluginName, operation, err)
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}
