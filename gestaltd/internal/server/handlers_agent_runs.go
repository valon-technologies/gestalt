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
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/internal/agentmanager"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

const agentIdempotencyKeyHeader = "Idempotency-Key"

type agentMessageRequest struct {
	Role string `json:"role,omitempty"`
	Text string `json:"text,omitempty"`
}

type agentToolRefRequest struct {
	PluginName  string `json:"pluginName,omitempty"`
	Operation   string `json:"operation,omitempty"`
	Connection  string `json:"connection,omitempty"`
	Instance    string `json:"instance,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

type agentRunCreateRequest struct {
	ProviderName    string                `json:"provider,omitempty"`
	Model           string                `json:"model,omitempty"`
	Messages        []agentMessageRequest `json:"messages,omitempty"`
	ToolRefs        []agentToolRefRequest `json:"toolRefs,omitempty"`
	ToolSource      string                `json:"toolSource,omitempty"`
	ResponseSchema  map[string]any        `json:"responseSchema,omitempty"`
	SessionRef      string                `json:"sessionRef,omitempty"`
	Metadata        map[string]any        `json:"metadata,omitempty"`
	ProviderOptions map[string]any        `json:"providerOptions,omitempty"`
	IdempotencyKey  string                `json:"idempotencyKey,omitempty"`
}

type agentRunCancelRequest struct {
	Reason string `json:"reason,omitempty"`
}

type agentRunInfo struct {
	ID               string                `json:"id"`
	Provider         string                `json:"provider"`
	Model            string                `json:"model,omitempty"`
	Status           string                `json:"status,omitempty"`
	Messages         []agentMessageRequest `json:"messages,omitempty"`
	OutputText       string                `json:"outputText,omitempty"`
	StructuredOutput map[string]any        `json:"structuredOutput,omitempty"`
	StatusMessage    string                `json:"statusMessage,omitempty"`
	SessionRef       string                `json:"sessionRef,omitempty"`
	CreatedBy        *workflowActorInfo    `json:"createdBy,omitempty"`
	CreatedAt        *time.Time            `json:"createdAt,omitempty"`
	StartedAt        *time.Time            `json:"startedAt,omitempty"`
	CompletedAt      *time.Time            `json:"completedAt,omitempty"`
	ExecutionRef     string                `json:"executionRef,omitempty"`
}

func (s *Server) createGlobalAgentRun(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveAgentRunActor(w, r)
	if !ok {
		return
	}
	if s == nil || s.agentRuns == nil {
		writeError(w, http.StatusPreconditionFailed, agentmanager.ErrAgentNotConfigured.Error())
		return
	}
	var req agentRunCreateRequest
	if r.Body != nil {
		defer func() { _ = r.Body.Close() }()
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
	}
	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	if headerKey := strings.TrimSpace(r.Header.Get(agentIdempotencyKeyHeader)); headerKey != "" {
		if idempotencyKey != "" && idempotencyKey != headerKey {
			writeError(w, http.StatusBadRequest, "idempotency key header and body must match")
			return
		}
		idempotencyKey = headerKey
	}
	if err := validateAgentToolRefs(req.ToolRefs); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	toolSource, err := agentToolSourceModeFromRequest(req.ToolSource)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	managed, err := s.agentRuns.Run(r.Context(), p, coreagent.ManagerRunRequest{
		ProviderName:    strings.TrimSpace(req.ProviderName),
		Model:           strings.TrimSpace(req.Model),
		Messages:        agentMessagesFromRequest(req.Messages),
		ToolRefs:        agentToolRefsFromRequest(req.ToolRefs),
		ToolSource:      toolSource,
		ResponseSchema:  maps.Clone(req.ResponseSchema),
		SessionRef:      strings.TrimSpace(req.SessionRef),
		Metadata:        maps.Clone(req.Metadata),
		ProviderOptions: maps.Clone(req.ProviderOptions),
		IdempotencyKey:  idempotencyKey,
	})
	if err != nil {
		s.writeAgentRunManagerError(w, r, "", req, err)
		return
	}
	writeJSON(w, http.StatusCreated, agentRunInfoFromManaged(managed))
}

func (s *Server) listGlobalAgentRuns(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveAgentRunActor(w, r)
	if !ok {
		return
	}
	if s == nil || s.agentRuns == nil {
		writeError(w, http.StatusPreconditionFailed, agentmanager.ErrAgentNotConfigured.Error())
		return
	}
	providerFilter := strings.TrimSpace(r.URL.Query().Get("provider"))
	var (
		err  error
		runs []*coreagent.ManagedRun
	)
	if providerFilter != "" {
		runs, err = s.agentRuns.ListRunsByProvider(r.Context(), p, providerFilter)
	} else {
		runs, err = s.agentRuns.ListRuns(r.Context(), p)
	}
	if err != nil {
		s.writeAgentRunManagerError(w, r, "", agentRunCreateRequest{}, err)
		return
	}
	statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))
	out := make([]agentRunInfo, 0, len(runs))
	for _, managed := range runs {
		if managed == nil || managed.Run == nil {
			continue
		}
		if providerFilter != "" && strings.TrimSpace(managed.ProviderName) != providerFilter {
			continue
		}
		if statusFilter != "" && strings.TrimSpace(string(managed.Run.Status)) != statusFilter {
			continue
		}
		out = append(out, agentRunInfoFromManaged(managed))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getGlobalAgentRun(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveAgentRunActor(w, r)
	if !ok {
		return
	}
	if s == nil || s.agentRuns == nil {
		writeError(w, http.StatusPreconditionFailed, agentmanager.ErrAgentNotConfigured.Error())
		return
	}
	runID := chi.URLParam(r, "runID")
	managed, err := s.agentRuns.GetRun(r.Context(), p, runID)
	if err != nil {
		s.writeAgentRunManagerError(w, r, runID, agentRunCreateRequest{}, err)
		return
	}
	writeJSON(w, http.StatusOK, agentRunInfoFromManaged(managed))
}

func (s *Server) cancelGlobalAgentRun(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveAgentRunActor(w, r)
	if !ok {
		return
	}
	if s == nil || s.agentRuns == nil {
		writeError(w, http.StatusPreconditionFailed, agentmanager.ErrAgentNotConfigured.Error())
		return
	}
	var req agentRunCancelRequest
	if r.Body != nil {
		defer func() { _ = r.Body.Close() }()
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
	}
	runID := chi.URLParam(r, "runID")
	managed, err := s.agentRuns.CancelRun(r.Context(), p, runID, req.Reason)
	if err != nil {
		s.writeAgentRunManagerError(w, r, runID, agentRunCreateRequest{}, err)
		return
	}
	writeJSON(w, http.StatusOK, agentRunInfoFromManaged(managed))
}

func (s *Server) resolveAgentRunActor(w http.ResponseWriter, r *http.Request) (*principal.Principal, bool) {
	return s.resolveWorkflowScheduleActor(w, r)
}

func agentMessagesFromRequest(messages []agentMessageRequest) []coreagent.Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]coreagent.Message, 0, len(messages))
	for _, message := range messages {
		out = append(out, coreagent.Message{
			Role: strings.TrimSpace(message.Role),
			Text: message.Text,
		})
	}
	return out
}

func agentToolRefsFromRequest(refs []agentToolRefRequest) []coreagent.ToolRef {
	if len(refs) == 0 {
		return nil
	}
	out := make([]coreagent.ToolRef, 0, len(refs))
	for _, ref := range refs {
		out = append(out, coreagent.ToolRef{
			PluginName:  strings.TrimSpace(ref.PluginName),
			Operation:   strings.TrimSpace(ref.Operation),
			Connection:  strings.TrimSpace(ref.Connection),
			Instance:    strings.TrimSpace(ref.Instance),
			Title:       strings.TrimSpace(ref.Title),
			Description: strings.TrimSpace(ref.Description),
		})
	}
	return out
}

func validateAgentToolRefs(refs []agentToolRefRequest) error {
	for idx, ref := range refs {
		if strings.TrimSpace(ref.PluginName) == "" {
			return fmt.Errorf("toolRefs[%d].pluginName is required", idx)
		}
		if strings.TrimSpace(ref.Operation) == "" {
			return fmt.Errorf("toolRefs[%d].operation is required", idx)
		}
	}
	return nil
}

func agentToolSourceModeFromRequest(value string) (coreagent.ToolSourceMode, error) {
	switch strings.TrimSpace(value) {
	case "":
		return coreagent.ToolSourceModeUnspecified, nil
	case string(coreagent.ToolSourceModeExplicit):
		return coreagent.ToolSourceModeExplicit, nil
	case string(coreagent.ToolSourceModeInheritInvokes):
		return coreagent.ToolSourceModeInheritInvokes, nil
	default:
		return "", fmt.Errorf("unsupported agent tool source %q", value)
	}
}

func agentRunInfoFromManaged(managed *coreagent.ManagedRun) agentRunInfo {
	if managed == nil {
		return agentRunInfo{}
	}
	return agentRunInfoFromCore(managed.Run, strings.TrimSpace(managed.ProviderName))
}

func agentRunInfoFromCore(run *coreagent.Run, providerName string) agentRunInfo {
	info := agentRunInfo{Provider: providerName}
	if run == nil {
		return info
	}
	info.ID = run.ID
	info.Model = strings.TrimSpace(run.Model)
	info.Status = strings.TrimSpace(string(run.Status))
	info.Messages = agentMessageInfoFromCore(run.Messages)
	info.OutputText = run.OutputText
	info.StructuredOutput = maps.Clone(run.StructuredOutput)
	info.StatusMessage = run.StatusMessage
	info.SessionRef = run.SessionRef
	info.CreatedBy = agentActorInfoFromCore(run.CreatedBy)
	info.CreatedAt = run.CreatedAt
	info.StartedAt = run.StartedAt
	info.CompletedAt = run.CompletedAt
	info.ExecutionRef = strings.TrimSpace(run.ExecutionRef)
	return info
}

func agentMessageInfoFromCore(messages []coreagent.Message) []agentMessageRequest {
	if len(messages) == 0 {
		return nil
	}
	out := make([]agentMessageRequest, 0, len(messages))
	for _, message := range messages {
		out = append(out, agentMessageRequest{
			Role: strings.TrimSpace(message.Role),
			Text: message.Text,
		})
	}
	return out
}

func agentActorInfoFromCore(actor coreagent.Actor) *workflowActorInfo {
	if actor == (coreagent.Actor{}) {
		return nil
	}
	return &workflowActorInfo{
		SubjectID:   actor.SubjectID,
		SubjectKind: actor.SubjectKind,
		DisplayName: actor.DisplayName,
		AuthSource:  actor.AuthSource,
	}
}

func (s *Server) writeAgentRunManagerError(w http.ResponseWriter, r *http.Request, runID string, req agentRunCreateRequest, err error) {
	pluginName, operation := firstAgentToolTarget(req.ToolRefs)
	switch {
	case errors.Is(err, agentmanager.ErrAgentNotConfigured),
		errors.Is(err, agentmanager.ErrAgentRunMetadataNotConfigured):
		writeError(w, http.StatusPreconditionFailed, err.Error())
	case errors.Is(err, agentmanager.ErrAgentSubjectRequired):
		writeError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, agentmanager.ErrAgentRunCreationInProgress):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, agentmanager.ErrAgentCallerPluginRequired),
		errors.Is(err, agentmanager.ErrAgentInheritedSurfaceTool):
		writeError(w, http.StatusBadRequest, err.Error())
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
		s.writeAgentRunTargetError(w, r, pluginName, operation, err)
	case errors.Is(err, core.ErrNotFound):
		s.writeAgentRunProviderError(r.Context(), w, runID, err)
	default:
		s.writeAgentRunProviderError(r.Context(), w, runID, err)
	}
}

func (s *Server) writeAgentRunTargetError(w http.ResponseWriter, r *http.Request, pluginName, operation string, err error) {
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

func (s *Server) writeAgentRunProviderError(ctx context.Context, w http.ResponseWriter, runID string, err error) {
	switch {
	case errors.Is(err, core.ErrNotFound):
		writeError(w, http.StatusNotFound, fmt.Sprintf("agent run %q not found", runID))
	default:
		slog.ErrorContext(ctx, "agent run provider error", "run_id", runID, "error", err)
		writeError(w, http.StatusInternalServerError, "agent run request failed")
	}
}

func firstAgentToolTarget(refs []agentToolRefRequest) (string, string) {
	for _, ref := range refs {
		pluginName := strings.TrimSpace(ref.PluginName)
		operation := strings.TrimSpace(ref.Operation)
		if pluginName == "" && operation == "" {
			continue
		}
		return pluginName, operation
	}
	return "", ""
}
