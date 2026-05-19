package server

import (
	"bytes"
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
	Steps []workflowStepTargetRequest `json:"steps,omitempty"`
}

type workflowPluginTargetRequest struct {
	Name           string               `json:"name,omitempty"`
	Operation      string               `json:"operation"`
	Connection     string               `json:"connection,omitempty"`
	Instance       string               `json:"instance,omitempty"`
	CredentialMode string               `json:"credentialMode,omitempty"`
	Input          workflowValueRequest `json:"input,omitempty"`
}

type workflowAgentTargetRequest struct {
	ProviderName   string                   `json:"provider,omitempty"`
	Model          string                   `json:"model,omitempty"`
	SessionKey     string                   `json:"sessionKey,omitempty"`
	Prompt         workflowTextRequest      `json:"prompt,omitempty"`
	Messages       []workflowMessageRequest `json:"messages,omitempty"`
	ToolRefs       []agentToolRefRequest    `json:"tools,omitempty"`
	ResponseSchema map[string]any           `json:"responseSchema,omitempty"`
	ModelOptions   map[string]any           `json:"modelOptions,omitempty"`
}

type workflowStepTargetRequest struct {
	ID             string                          `json:"id,omitempty"`
	Inputs         map[string]workflowValueRequest `json:"inputs,omitempty"`
	Plugin         *workflowPluginTargetRequest    `json:"plugin,omitempty"`
	Agent          *workflowAgentTargetRequest     `json:"agent,omitempty"`
	OutputDelivery *workflowOutputDeliveryRequest  `json:"outputDelivery,omitempty"`
	Metadata       map[string]any                  `json:"metadata,omitempty"`
	TimeoutSeconds int                             `json:"timeoutSeconds,omitempty"`
	When           *workflowStepWhenRequest        `json:"when,omitempty"`
}

type workflowTextRequest struct {
	Template string `json:"template,omitempty"`
}

func (r *workflowTextRequest) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		r.Template = text
		return nil
	}
	type alias workflowTextRequest
	var out alias
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	*r = workflowTextRequest(out)
	return nil
}

type workflowOutputDeliveryRequest struct {
	Plugin *workflowPluginTargetRequest `json:"plugin,omitempty"`
}

type workflowMessageRequest struct {
	Role     string              `json:"role,omitempty"`
	Text     workflowTextRequest `json:"text,omitempty"`
	Metadata map[string]any      `json:"metadata,omitempty"`
}

type workflowStepWhenRequest struct {
	Value     workflowValueRequest `json:"value,omitempty"`
	Equals    any                  `json:"equals,omitempty"`
	EqualsSet bool                 `json:"-"`
}

func (r *workflowStepWhenRequest) UnmarshalJSON(data []byte) error {
	type alias struct {
		Value  workflowValueRequest `json:"value,omitempty"`
		Equals any                  `json:"equals,omitempty"`
	}
	var out alias
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.Value = out.Value
	r.Equals = out.Equals
	_, r.EqualsSet = raw["equals"]
	return nil
}

type workflowStepOutputSourceRequest struct {
	StepID string `json:"stepId,omitempty"`
	Path   string `json:"path,omitempty"`
}

type workflowValueRequest struct {
	Literal         any
	LiteralSet      bool
	Object          map[string]workflowValueRequest
	ObjectSet       bool
	Array           []workflowValueRequest
	ArraySet        bool
	Template        *workflowTextRequest
	RunInput      string
	SignalPayload string
	StepOutput    *workflowStepOutputSourceRequest
}

func (r *workflowValueRequest) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		r.Literal = nil
		r.LiteralSet = true
		return nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err == nil {
		if len(object) == 1 {
			for key, raw := range object {
				switch key {
				case "literal":
					if err := json.Unmarshal(raw, &r.Literal); err != nil {
						return err
					}
					r.LiteralSet = true
					return nil
				case "object":
					if err := json.Unmarshal(raw, &r.Object); err != nil {
						return err
					}
					r.ObjectSet = true
					return nil
				case "array":
					if err := json.Unmarshal(raw, &r.Array); err != nil {
						return err
					}
					r.ArraySet = true
					return nil
				case "template":
					var text workflowTextRequest
					if err := json.Unmarshal(raw, &text); err != nil {
						return err
					}
					r.Template = &text
					return nil
				case "runInput":
					return json.Unmarshal(raw, &r.RunInput)
				case "signalPayload":
					return json.Unmarshal(raw, &r.SignalPayload)
				case "stepOutput":
					var source workflowStepOutputSourceRequest
					if err := json.Unmarshal(raw, &source); err != nil {
						return err
					}
					r.StepOutput = &source
					return nil
				}
			}
		}
		values := make(map[string]workflowValueRequest, len(object))
		for key, raw := range object {
			var value workflowValueRequest
			if err := json.Unmarshal(raw, &value); err != nil {
				return err
			}
			values[key] = value
		}
		r.Object = values
		r.ObjectSet = true
		return nil
	}
	var array []workflowValueRequest
	if err := json.Unmarshal(data, &array); err == nil {
		r.Array = array
		r.ArraySet = true
		return nil
	}
	var literal any
	if err := json.Unmarshal(data, &literal); err != nil {
		return err
	}
	r.Literal = literal
	r.LiteralSet = true
	return nil
}

type workflowScheduleUpsertRequest struct {
	Provider string                        `json:"provider,omitempty"`
	Cron     string                        `json:"cron"`
	Timezone string                        `json:"timezone,omitempty"`
	Target   workflowScheduleTargetRequest `json:"target"`
	Paused   bool                          `json:"paused,omitempty"`
}

type workflowScheduleTargetInfo struct {
	Steps []workflowStepTargetInfo `json:"steps,omitempty"`
}

type workflowPluginTargetInfo struct {
	Name           string `json:"name"`
	Operation      string `json:"operation"`
	Connection     string `json:"connection,omitempty"`
	Instance       string `json:"instance,omitempty"`
	CredentialMode string `json:"credentialMode,omitempty"`
	Input          any    `json:"input,omitempty"`
}

type workflowAgentTargetInfo struct {
	ProviderName   string                `json:"provider,omitempty"`
	Model          string                `json:"model,omitempty"`
	SessionKey     string                `json:"sessionKey,omitempty"`
	Prompt         *workflowTextInfo     `json:"prompt,omitempty"`
	Messages       []workflowMessageInfo `json:"messages,omitempty"`
	ToolRefs       []agentToolRefRequest `json:"tools,omitempty"`
	ResponseSchema map[string]any        `json:"responseSchema,omitempty"`
	ModelOptions   map[string]any        `json:"modelOptions,omitempty"`
}

type workflowStepTargetInfo struct {
	ID             string                      `json:"id,omitempty"`
	Inputs         map[string]any              `json:"inputs,omitempty"`
	Plugin         *workflowPluginTargetInfo   `json:"plugin,omitempty"`
	Agent          *workflowAgentTargetInfo    `json:"agent,omitempty"`
	OutputDelivery *workflowOutputDeliveryInfo `json:"outputDelivery,omitempty"`
	Metadata       map[string]any              `json:"metadata,omitempty"`
	TimeoutSeconds int                         `json:"timeoutSeconds,omitempty"`
	When           *workflowStepWhenInfo       `json:"when,omitempty"`
}

type workflowTextInfo struct {
	Template string `json:"template,omitempty"`
}

type workflowOutputDeliveryInfo struct {
	Plugin *workflowPluginTargetInfo `json:"plugin,omitempty"`
}

type workflowMessageInfo struct {
	Role     string            `json:"role,omitempty"`
	Text     *workflowTextInfo `json:"text,omitempty"`
	Metadata map[string]any    `json:"metadata,omitempty"`
}

type workflowStepWhenInfo struct {
	Value     any
	Equals    any
	EqualsSet bool
}

func (i workflowStepWhenInfo) MarshalJSON() ([]byte, error) {
	out := map[string]any{}
	if i.Value != nil {
		out["value"] = i.Value
	}
	if i.EqualsSet {
		out["equals"] = i.Equals
	}
	return json.Marshal(out)
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
	if len(req.Target.Steps) == 0 {
		writeError(w, http.StatusBadRequest, "workflow target.steps is required")
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
		writeError(w, http.StatusBadRequest, "workflow target.steps is required")
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
	steps := make([]coreworkflow.Step, 0, len(target.Steps))
	for i := range target.Steps {
		step := target.Steps[i]
		steps = append(steps, coreworkflow.Step{
			ID:             strings.TrimSpace(step.ID),
			Inputs:         workflowValueMapFromRequest(step.Inputs),
			Plugin:         workflowPluginCallFromRequest(step.Plugin),
			Agent:          workflowAgentTurnFromRequest(step.Agent),
			OutputDelivery: workflowStepDeliveryFromRequest(step.OutputDelivery),
			Metadata:       maps.Clone(step.Metadata),
			TimeoutSeconds: step.TimeoutSeconds,
			When:           workflowStepWhenFromRequest(step.When),
		})
	}
	return coreworkflow.Target{Steps: steps}
}

func workflowScheduleTargetRequestHasOneKind(target workflowScheduleTargetRequest) bool {
	return len(target.Steps) > 0
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

func validatePublicWorkflowTargetRequest(target workflowScheduleTargetRequest) error {
	for i := range target.Steps {
		step := target.Steps[i]
		if step.Plugin != nil && strings.TrimSpace(step.Plugin.CredentialMode) != "" {
			return fmt.Errorf("workflow target.steps[%d].plugin.credentialMode is not supported on public requests", i)
		}
		if step.OutputDelivery != nil && step.OutputDelivery.Plugin != nil && strings.TrimSpace(step.OutputDelivery.Plugin.CredentialMode) != "" {
			return fmt.Errorf("workflow target.steps[%d].outputDelivery.plugin.credentialMode is not supported on public requests", i)
		}
	}
	return nil
}

func workflowPluginCallFromRequest(target *workflowPluginTargetRequest) *coreworkflow.PluginCall {
	if target == nil {
		return nil
	}
	return &coreworkflow.PluginCall{
		Name:           strings.TrimSpace(target.Name),
		Operation:      strings.TrimSpace(target.Operation),
		Connection:     strings.TrimSpace(target.Connection),
		Instance:       strings.TrimSpace(target.Instance),
		CredentialMode: core.NormalizeOptionalConnectionMode(core.ConnectionMode(target.CredentialMode)),
		Input:          workflowValueFromRequest(target.Input),
	}
}

func workflowAgentTurnFromRequest(target *workflowAgentTargetRequest) *coreworkflow.AgentTurn {
	if target == nil {
		return nil
	}
	return &coreworkflow.AgentTurn{
		ProviderName:   strings.TrimSpace(target.ProviderName),
		Model:          strings.TrimSpace(target.Model),
		SessionKey:     strings.TrimSpace(target.SessionKey),
		Prompt:         workflowTextFromRequest(target.Prompt),
		Messages:       workflowMessagesFromRequest(target.Messages),
		ToolRefs:       agentToolRefsFromRequest(target.ToolRefs),
		ResponseSchema: maps.Clone(target.ResponseSchema),
		ModelOptions:   maps.Clone(target.ModelOptions),
	}
}

func workflowStepWhenFromRequest(when *workflowStepWhenRequest) *coreworkflow.StepWhen {
	if when == nil {
		return nil
	}
	return &coreworkflow.StepWhen{
		Value:     workflowValueFromRequest(when.Value),
		Equals:    when.Equals,
		EqualsSet: when.EqualsSet,
	}
}

func workflowStepDeliveryFromRequest(delivery *workflowOutputDeliveryRequest) *coreworkflow.StepDelivery {
	if delivery == nil {
		return nil
	}
	return &coreworkflow.StepDelivery{Plugin: workflowPluginCallFromRequest(delivery.Plugin)}
}

func workflowTextFromRequest(text workflowTextRequest) coreworkflow.Text {
	return coreworkflow.Text{Template: strings.TrimSpace(text.Template)}
}

func workflowMessagesFromRequest(messages []workflowMessageRequest) []coreworkflow.AgentMessage {
	if len(messages) == 0 {
		return nil
	}
	out := make([]coreworkflow.AgentMessage, 0, len(messages))
	for i := range messages {
		out = append(out, coreworkflow.AgentMessage{
			Role:     strings.TrimSpace(messages[i].Role),
			Text:     workflowTextFromRequest(messages[i].Text),
			Metadata: maps.Clone(messages[i].Metadata),
		})
	}
	return out
}

func workflowValueMapFromRequest(values map[string]workflowValueRequest) map[string]coreworkflow.Value {
	if len(values) == 0 {
		return nil
	}
	return workflowValueObjectMapFromRequest(values)
}

func workflowValueObjectMapFromRequest(values map[string]workflowValueRequest) map[string]coreworkflow.Value {
	out := make(map[string]coreworkflow.Value, len(values))
	for key := range values {
		out[key] = workflowValueFromRequest(values[key])
	}
	return out
}

func workflowValueListFromRequest(values []workflowValueRequest) []coreworkflow.Value {
	out := make([]coreworkflow.Value, 0, len(values))
	for i := range values {
		out = append(out, workflowValueFromRequest(values[i]))
	}
	return out
}

func workflowValueFromRequest(value workflowValueRequest) coreworkflow.Value {
	out := coreworkflow.Value{
		Literal:         value.Literal,
		LiteralSet:      value.LiteralSet,
		RunInput:      strings.TrimSpace(value.RunInput),
		SignalPayload: strings.TrimSpace(value.SignalPayload),
	}
	if value.ObjectSet {
		out.Object = workflowValueObjectMapFromRequest(value.Object)
	}
	if value.ArraySet {
		out.Array = workflowValueListFromRequest(value.Array)
	}
	if value.Template != nil {
		text := workflowTextFromRequest(*value.Template)
		out.Template = &text
	}
	if value.StepOutput != nil {
		out.StepOutput = &coreworkflow.StepOutputSource{StepID: strings.TrimSpace(value.StepOutput.StepID), Path: strings.TrimSpace(value.StepOutput.Path)}
	}
	return out
}

func workflowScheduleTargetErrorPlugin(target workflowScheduleTargetRequest) string {
	if len(target.Steps) == 0 {
		return ""
	}
	step := target.Steps[0]
	if step.Plugin != nil {
		return strings.TrimSpace(step.Plugin.Name)
	}
	if step.Agent != nil {
		return "agent"
	}
	return ""
}

func workflowScheduleTargetErrorOperation(target workflowScheduleTargetRequest) string {
	if len(target.Steps) == 0 {
		return ""
	}
	step := target.Steps[0]
	if step.Plugin != nil {
		return strings.TrimSpace(step.Plugin.Operation)
	}
	if step.Agent != nil {
		return "turn"
	}
	return ""
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
	info := workflowScheduleTargetInfo{Steps: make([]workflowStepTargetInfo, 0, len(target.Steps))}
	for i := range target.Steps {
		step := target.Steps[i]
		info.Steps = append(info.Steps, workflowStepInfoFromCore(step))
	}
	return info
}

func workflowStepInfoFromCore(step coreworkflow.Step) workflowStepTargetInfo {
	return workflowStepTargetInfo{
		ID:             step.ID,
		Inputs:         workflowValueMapInfoFromCore(step.Inputs),
		Plugin:         workflowPluginInfoFromCore(step.Plugin),
		Agent:          workflowAgentInfoFromCore(step.Agent),
		OutputDelivery: workflowOutputDeliveryInfoFromCore(step.OutputDelivery),
		Metadata:       maps.Clone(step.Metadata),
		TimeoutSeconds: step.TimeoutSeconds,
		When:           workflowStepWhenInfoFromCore(step.When),
	}
}

func workflowPluginInfoFromCore(plugin *coreworkflow.PluginCall) *workflowPluginTargetInfo {
	if plugin == nil {
		return nil
	}
	return &workflowPluginTargetInfo{
		Name:           plugin.Name,
		Operation:      plugin.Operation,
		Connection:     userFacingConnectionName(plugin.Connection),
		Instance:       plugin.Instance,
		CredentialMode: string(plugin.CredentialMode),
		Input:          workflowValueInfoFromCore(plugin.Input),
	}
}

func workflowAgentInfoFromCore(agent *coreworkflow.AgentTurn) *workflowAgentTargetInfo {
	if agent == nil {
		return nil
	}
	return &workflowAgentTargetInfo{
		ProviderName:   agent.ProviderName,
		Model:          agent.Model,
		SessionKey:     agent.SessionKey,
		Prompt:         workflowTextInfoFromCore(agent.Prompt),
		Messages:       workflowMessagesInfoFromCore(agent.Messages),
		ToolRefs:       agentToolRefsToRequest(agent.ToolRefs),
		ResponseSchema: maps.Clone(agent.ResponseSchema),
		ModelOptions:   maps.Clone(agent.ModelOptions),
	}
}

func workflowStepWhenInfoFromCore(when *coreworkflow.StepWhen) *workflowStepWhenInfo {
	if when == nil {
		return nil
	}
	return &workflowStepWhenInfo{
		Value:     workflowValueInfoFromCore(when.Value),
		Equals:    when.Equals,
		EqualsSet: when.EqualsSet,
	}
}

func workflowOutputDeliveryInfoFromCore(delivery *coreworkflow.StepDelivery) *workflowOutputDeliveryInfo {
	if delivery == nil {
		return nil
	}
	return &workflowOutputDeliveryInfo{
		Plugin: workflowPluginInfoFromCore(delivery.Plugin),
	}
}

func workflowTextInfoFromCore(text coreworkflow.Text) *workflowTextInfo {
	if strings.TrimSpace(text.Template) == "" {
		return nil
	}
	return &workflowTextInfo{Template: text.Template}
}

func workflowMessagesInfoFromCore(messages []coreworkflow.AgentMessage) []workflowMessageInfo {
	if len(messages) == 0 {
		return nil
	}
	out := make([]workflowMessageInfo, 0, len(messages))
	for i := range messages {
		out = append(out, workflowMessageInfo{
			Role:     messages[i].Role,
			Text:     workflowTextInfoFromCore(messages[i].Text),
			Metadata: maps.Clone(messages[i].Metadata),
		})
	}
	return out
}

func workflowValueMapInfoFromCore(values map[string]coreworkflow.Value) map[string]any {
	if len(values) == 0 {
		return nil
	}
	return workflowValueObjectInfoFromCore(values)
}

func workflowValueObjectInfoFromCore(values map[string]coreworkflow.Value) map[string]any {
	out := make(map[string]any, len(values))
	for key := range values {
		out[key] = workflowValueInfoFromCore(values[key])
	}
	return out
}

func workflowValueInfoFromCore(value coreworkflow.Value) any {
	switch {
	case value.LiteralSet:
		return map[string]any{"literal": value.Literal}
	case value.Object != nil:
		return map[string]any{"object": workflowValueObjectInfoFromCore(value.Object)}
	case value.Array != nil:
		items := make([]any, 0, len(value.Array))
		for i := range value.Array {
			items = append(items, workflowValueInfoFromCore(value.Array[i]))
		}
		return map[string]any{"array": items}
	case value.Template != nil:
		return map[string]any{"template": workflowTextInfoFromCore(*value.Template)}
	case strings.TrimSpace(value.RunInput) != "":
		return map[string]any{"runInput": value.RunInput}
	case strings.TrimSpace(value.SignalPayload) != "":
		return map[string]any{"signalPayload": value.SignalPayload}
	case value.StepOutput != nil:
		return map[string]any{"stepOutput": map[string]any{"stepId": value.StepOutput.StepID, "path": value.StepOutput.Path}}
	default:
		return nil
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
