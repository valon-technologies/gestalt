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
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/internal/agentmanager"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

const agentIdempotencyKeyHeader = "Idempotency-Key"
const defaultAgentTurnEventLimit = 100
const maxAgentTurnEventLimit = 1000
const agentTurnEventStreamUntilTerminal = "terminal"
const agentTurnEventStreamUntilBlockedOrTerminal = "blocked_or_terminal"

type agentMessageRequest struct {
	Role     string                    `json:"role,omitempty"`
	Text     string                    `json:"text,omitempty"`
	Parts    []agentMessagePartRequest `json:"parts,omitempty"`
	Metadata map[string]any            `json:"metadata,omitempty"`
}

type agentMessagePartRequest struct {
	Type       string                             `json:"type,omitempty"`
	Text       string                             `json:"text,omitempty"`
	JSON       map[string]any                     `json:"json,omitempty"`
	ToolCall   *agentMessagePartToolCallRequest   `json:"toolCall,omitempty"`
	ToolResult *agentMessagePartToolResultRequest `json:"toolResult,omitempty"`
	ImageRef   *agentMessagePartImageRefRequest   `json:"imageRef,omitempty"`
}

type agentMessagePartToolCallRequest struct {
	ID        string         `json:"id,omitempty"`
	ToolID    string         `json:"toolId,omitempty"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

type agentMessagePartToolResultRequest struct {
	ToolCallID string         `json:"toolCallId,omitempty"`
	Status     int            `json:"status,omitempty"`
	Content    string         `json:"content,omitempty"`
	Output     map[string]any `json:"output,omitempty"`
}

type agentMessagePartImageRefRequest struct {
	URI      string `json:"uri,omitempty"`
	MIMEType string `json:"mimeType,omitempty"`
}

type agentToolRefRequest struct {
	Plugin      string `json:"plugin,omitempty"`
	Operation   string `json:"operation,omitempty"`
	Connection  string `json:"connection,omitempty"`
	Instance    string `json:"instance,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

type agentSessionCreateRequest struct {
	ProviderName    string         `json:"provider,omitempty"`
	Model           string         `json:"model,omitempty"`
	ClientRef       string         `json:"clientRef,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	ProviderOptions map[string]any `json:"providerOptions,omitempty"`
	IdempotencyKey  string         `json:"idempotencyKey,omitempty"`
}

type agentSessionUpdateRequest struct {
	ClientRef string         `json:"clientRef,omitempty"`
	State     string         `json:"state,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type agentTurnCreateRequest struct {
	Model           string                `json:"model,omitempty"`
	Messages        []agentMessageRequest `json:"messages,omitempty"`
	ToolRefs        []agentToolRefRequest `json:"toolRefs,omitempty"`
	ToolSource      string                `json:"toolSource,omitempty"`
	ResponseSchema  map[string]any        `json:"responseSchema,omitempty"`
	Metadata        map[string]any        `json:"metadata,omitempty"`
	ProviderOptions map[string]any        `json:"providerOptions,omitempty"`
	IdempotencyKey  string                `json:"idempotencyKey,omitempty"`
}

type agentTurnCancelRequest struct {
	Reason string `json:"reason,omitempty"`
}

type agentInteractionResolveRequest struct {
	Resolution map[string]any `json:"resolution,omitempty"`
}

type agentSessionInfo struct {
	ID         string             `json:"id"`
	Provider   string             `json:"provider"`
	Model      string             `json:"model,omitempty"`
	ClientRef  string             `json:"clientRef,omitempty"`
	State      string             `json:"state,omitempty"`
	Metadata   map[string]any     `json:"metadata,omitempty"`
	CreatedBy  *workflowActorInfo `json:"createdBy,omitempty"`
	CreatedAt  *time.Time         `json:"createdAt,omitempty"`
	UpdatedAt  *time.Time         `json:"updatedAt,omitempty"`
	LastTurnAt *time.Time         `json:"lastTurnAt,omitempty"`
}

type agentTurnInfo struct {
	ID               string                `json:"id"`
	SessionID        string                `json:"sessionId"`
	Provider         string                `json:"provider"`
	Model            string                `json:"model,omitempty"`
	Status           string                `json:"status,omitempty"`
	Messages         []agentMessageRequest `json:"messages,omitempty"`
	OutputText       string                `json:"outputText,omitempty"`
	StructuredOutput map[string]any        `json:"structuredOutput,omitempty"`
	StatusMessage    string                `json:"statusMessage,omitempty"`
	CreatedBy        *workflowActorInfo    `json:"createdBy,omitempty"`
	CreatedAt        *time.Time            `json:"createdAt,omitempty"`
	StartedAt        *time.Time            `json:"startedAt,omitempty"`
	CompletedAt      *time.Time            `json:"completedAt,omitempty"`
	ExecutionRef     string                `json:"executionRef,omitempty"`
}

type agentTurnEventInfo struct {
	ID         string         `json:"id"`
	TurnID     string         `json:"turnId"`
	Seq        int64          `json:"seq"`
	Type       string         `json:"type"`
	Source     string         `json:"source"`
	Visibility string         `json:"visibility"`
	Data       map[string]any `json:"data"`
	CreatedAt  *time.Time     `json:"createdAt"`
}

type agentInteractionInfo struct {
	ID         string         `json:"id"`
	TurnID     string         `json:"turnId"`
	Type       string         `json:"type"`
	State      string         `json:"state"`
	Title      string         `json:"title,omitempty"`
	Prompt     string         `json:"prompt,omitempty"`
	Request    map[string]any `json:"request,omitempty"`
	Resolution map[string]any `json:"resolution,omitempty"`
	CreatedAt  *time.Time     `json:"createdAt,omitempty"`
	ResolvedAt *time.Time     `json:"resolvedAt,omitempty"`
}

func (s *Server) createAgentSession(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveAgentActor(w, r)
	if !ok {
		return
	}
	var req agentSessionCreateRequest
	if r.Body != nil {
		defer func() { _ = r.Body.Close() }()
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
	}
	idempotencyKey, ok := resolveAgentIdempotencyKey(w, r, req.IdempotencyKey)
	if !ok {
		return
	}
	session, err := s.agentRuns.CreateSession(r.Context(), p, coreagent.ManagerCreateSessionRequest{
		IdempotencyKey:  idempotencyKey,
		ProviderName:    strings.TrimSpace(req.ProviderName),
		Model:           strings.TrimSpace(req.Model),
		ClientRef:       strings.TrimSpace(req.ClientRef),
		Metadata:        maps.Clone(req.Metadata),
		ProviderOptions: maps.Clone(req.ProviderOptions),
	})
	if err != nil {
		s.writeAgentManagerError(w, r, "session", "", nil, err)
		return
	}
	writeJSON(w, http.StatusCreated, agentSessionInfoFromCore(session))
}

func (s *Server) listAgentSessions(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveAgentActor(w, r)
	if !ok {
		return
	}
	providerName := strings.TrimSpace(r.URL.Query().Get("provider"))
	sessions, err := s.agentRuns.ListSessions(r.Context(), p, providerName)
	if err != nil {
		s.writeAgentManagerError(w, r, "session", "", nil, err)
		return
	}
	stateFilter := strings.TrimSpace(r.URL.Query().Get("state"))
	out := make([]agentSessionInfo, 0, len(sessions))
	for _, session := range sessions {
		if session == nil {
			continue
		}
		if stateFilter != "" && strings.TrimSpace(string(session.State)) != stateFilter {
			continue
		}
		out = append(out, agentSessionInfoFromCore(session))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getAgentSession(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveAgentActor(w, r)
	if !ok {
		return
	}
	sessionID := chi.URLParam(r, "sessionID")
	session, err := s.agentRuns.GetSession(r.Context(), p, sessionID)
	if err != nil {
		s.writeAgentManagerError(w, r, "session", sessionID, nil, err)
		return
	}
	writeJSON(w, http.StatusOK, agentSessionInfoFromCore(session))
}

func (s *Server) updateAgentSession(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveAgentActor(w, r)
	if !ok {
		return
	}
	sessionID := chi.URLParam(r, "sessionID")
	var req agentSessionUpdateRequest
	if r.Body != nil {
		defer func() { _ = r.Body.Close() }()
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
	}
	state, err := agentSessionStateFromRequest(req.State)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	session, err := s.agentRuns.UpdateSession(r.Context(), p, coreagent.ManagerUpdateSessionRequest{
		SessionID: strings.TrimSpace(sessionID),
		ClientRef: strings.TrimSpace(req.ClientRef),
		State:     state,
		Metadata:  maps.Clone(req.Metadata),
	})
	if err != nil {
		s.writeAgentManagerError(w, r, "session", sessionID, nil, err)
		return
	}
	writeJSON(w, http.StatusOK, agentSessionInfoFromCore(session))
}

func (s *Server) createAgentTurn(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveAgentActor(w, r)
	if !ok {
		return
	}
	sessionID := chi.URLParam(r, "sessionID")
	var req agentTurnCreateRequest
	if r.Body != nil {
		defer func() { _ = r.Body.Close() }()
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
	}
	idempotencyKey, ok := resolveAgentIdempotencyKey(w, r, req.IdempotencyKey)
	if !ok {
		return
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
	turn, err := s.agentRuns.CreateTurn(r.Context(), p, coreagent.ManagerCreateTurnRequest{
		IdempotencyKey:  idempotencyKey,
		Model:           strings.TrimSpace(req.Model),
		SessionID:       strings.TrimSpace(sessionID),
		Messages:        agentMessagesFromRequest(req.Messages),
		ToolRefs:        agentToolRefsFromRequest(req.ToolRefs),
		ToolSource:      toolSource,
		ResponseSchema:  maps.Clone(req.ResponseSchema),
		Metadata:        maps.Clone(req.Metadata),
		ProviderOptions: maps.Clone(req.ProviderOptions),
	})
	if err != nil {
		s.writeAgentManagerError(w, r, "turn", sessionID, req.ToolRefs, err)
		return
	}
	writeJSON(w, http.StatusCreated, agentTurnInfoFromCore(turn))
}

func (s *Server) listAgentTurns(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveAgentActor(w, r)
	if !ok {
		return
	}
	sessionID := chi.URLParam(r, "sessionID")
	turns, err := s.agentRuns.ListTurns(r.Context(), p, sessionID)
	if err != nil {
		s.writeAgentManagerError(w, r, "session", sessionID, nil, err)
		return
	}
	statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))
	out := make([]agentTurnInfo, 0, len(turns))
	for _, turn := range turns {
		if turn == nil {
			continue
		}
		if statusFilter != "" && strings.TrimSpace(string(turn.Status)) != statusFilter {
			continue
		}
		out = append(out, agentTurnInfoFromCore(turn))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getAgentTurn(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveAgentActor(w, r)
	if !ok {
		return
	}
	turnID := chi.URLParam(r, "turnID")
	turn, err := s.agentRuns.GetTurn(r.Context(), p, turnID)
	if err != nil {
		s.writeAgentManagerError(w, r, "turn", turnID, nil, err)
		return
	}
	writeJSON(w, http.StatusOK, agentTurnInfoFromCore(turn))
}

func (s *Server) cancelAgentTurn(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveAgentActor(w, r)
	if !ok {
		return
	}
	turnID := chi.URLParam(r, "turnID")
	var req agentTurnCancelRequest
	if r.Body != nil {
		defer func() { _ = r.Body.Close() }()
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
	}
	turn, err := s.agentRuns.CancelTurn(r.Context(), p, turnID, req.Reason)
	if err != nil {
		s.writeAgentManagerError(w, r, "turn", turnID, nil, err)
		return
	}
	writeJSON(w, http.StatusOK, agentTurnInfoFromCore(turn))
}

func (s *Server) listAgentTurnEvents(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveAgentActor(w, r)
	if !ok {
		return
	}
	turnID := chi.URLParam(r, "turnID")
	afterSeq, limit, ok := parseAgentTurnEventQuery(w, r)
	if !ok {
		return
	}
	events, err := s.agentRuns.ListTurnEvents(r.Context(), p, turnID, afterSeq, limit)
	if err != nil {
		s.writeAgentManagerError(w, r, "turn", turnID, nil, err)
		return
	}
	out := make([]agentTurnEventInfo, 0, len(events))
	for _, event := range events {
		out = append(out, agentTurnEventInfoFromCore(event))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) streamAgentTurnEvents(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveAgentActor(w, r)
	if !ok {
		return
	}
	turnID := chi.URLParam(r, "turnID")
	afterSeq, limit, ok := parseAgentTurnEventQuery(w, r)
	if !ok {
		return
	}
	until, ok := parseAgentTurnEventStreamUntil(w, r)
	if !ok {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	ctx := r.Context()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		events, err := s.agentRuns.ListTurnEvents(ctx, p, turnID, afterSeq, limit)
		if err != nil {
			s.writeAgentManagerError(w, r, "turn", turnID, nil, err)
			return
		}
		pageFull := limit > 0 && len(events) == limit
		for _, event := range events {
			info := agentTurnEventInfoFromCore(event)
			payload, err := json.Marshal(info)
			if err != nil {
				slog.ErrorContext(ctx, "marshal agent turn event", "turn_id", turnID, "error", err)
				continue
			}
			_, _ = w.Write([]byte("data: "))
			_, _ = w.Write(payload)
			_, _ = w.Write([]byte("\n\n"))
			flusher.Flush()
			if info.Seq > afterSeq {
				afterSeq = info.Seq
			}
		}
		if pageFull {
			continue
		}
		done, err := s.agentTurnStreamDone(ctx, p, turnID, until)
		if err != nil {
			s.writeAgentManagerError(w, r, "turn", turnID, nil, err)
			return
		}
		if done {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) listAgentTurnInteractions(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveAgentActor(w, r)
	if !ok {
		return
	}
	turnID := chi.URLParam(r, "turnID")
	interactions, err := s.agentRuns.ListInteractions(r.Context(), p, turnID)
	if err != nil {
		s.writeAgentManagerError(w, r, "turn", turnID, nil, err)
		return
	}
	out := make([]agentInteractionInfo, 0, len(interactions))
	for _, interaction := range interactions {
		out = append(out, agentInteractionInfoFromCore(interaction))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) resolveAgentInteraction(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveAgentActor(w, r)
	if !ok {
		return
	}
	turnID := chi.URLParam(r, "turnID")
	interactionID := chi.URLParam(r, "interactionID")
	var req agentInteractionResolveRequest
	if r.Body != nil {
		defer func() { _ = r.Body.Close() }()
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
	}
	interaction, err := s.agentRuns.ResolveInteraction(r.Context(), p, turnID, interactionID, maps.Clone(req.Resolution))
	if err != nil {
		s.writeAgentManagerError(w, r, "interaction", interactionID, nil, err)
		return
	}
	writeJSON(w, http.StatusOK, agentInteractionInfoFromCore(interaction))
}

func (s *Server) resolveAgentActor(w http.ResponseWriter, r *http.Request) (*principal.Principal, bool) {
	if s == nil || s.agentRuns == nil {
		writeError(w, http.StatusPreconditionFailed, agentmanager.ErrAgentNotConfigured.Error())
		return nil, false
	}
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

func resolveAgentIdempotencyKey(w http.ResponseWriter, r *http.Request, bodyValue string) (string, bool) {
	idempotencyKey := strings.TrimSpace(bodyValue)
	if headerKey := strings.TrimSpace(r.Header.Get(agentIdempotencyKeyHeader)); headerKey != "" {
		if idempotencyKey != "" && idempotencyKey != headerKey {
			writeError(w, http.StatusBadRequest, "idempotency key header and body must match")
			return "", false
		}
		idempotencyKey = headerKey
	}
	return idempotencyKey, true
}

func parseAgentTurnEventQuery(w http.ResponseWriter, r *http.Request) (int64, int, bool) {
	afterSeq := int64(0)
	if raw := strings.TrimSpace(r.URL.Query().Get("after")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value < 0 {
			writeError(w, http.StatusBadRequest, "after must be a non-negative integer")
			return 0, 0, false
		}
		afterSeq = value
	}
	limit := defaultAgentTurnEventLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 || value > maxAgentTurnEventLimit {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 0 and %d", maxAgentTurnEventLimit))
			return 0, 0, false
		}
		limit = value
	}
	return afterSeq, limit, true
}

func parseAgentTurnEventStreamUntil(w http.ResponseWriter, r *http.Request) (string, bool) {
	value := strings.TrimSpace(r.URL.Query().Get("until"))
	switch value {
	case "", agentTurnEventStreamUntilTerminal:
		return agentTurnEventStreamUntilTerminal, true
	case agentTurnEventStreamUntilBlockedOrTerminal:
		return agentTurnEventStreamUntilBlockedOrTerminal, true
	default:
		writeError(w, http.StatusBadRequest, "until must be terminal or blocked_or_terminal")
		return "", false
	}
}

func (s *Server) agentTurnStreamDone(ctx context.Context, p *principal.Principal, turnID string, until string) (bool, error) {
	turn, err := s.agentRuns.GetTurn(ctx, p, turnID)
	if err != nil {
		return false, err
	}
	switch turn.Status {
	case coreagent.ExecutionStatusSucceeded, coreagent.ExecutionStatusFailed, coreagent.ExecutionStatusCanceled:
		return true, nil
	case coreagent.ExecutionStatusWaitingForInput:
		return until == agentTurnEventStreamUntilBlockedOrTerminal, nil
	default:
		return false, nil
	}
}

func agentMessagesFromRequest(messages []agentMessageRequest) []coreagent.Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]coreagent.Message, 0, len(messages))
	for _, message := range messages {
		out = append(out, coreagent.Message{
			Role:     strings.TrimSpace(message.Role),
			Text:     message.Text,
			Parts:    agentMessagePartsFromRequest(message.Parts),
			Metadata: maps.Clone(message.Metadata),
		})
	}
	return out
}

func agentMessagePartsFromRequest(parts []agentMessagePartRequest) []coreagent.MessagePart {
	if len(parts) == 0 {
		return nil
	}
	out := make([]coreagent.MessagePart, 0, len(parts))
	for _, part := range parts {
		var toolCall *coreagent.ToolCallPart
		if part.ToolCall != nil {
			toolCall = &coreagent.ToolCallPart{
				ID:        strings.TrimSpace(part.ToolCall.ID),
				ToolID:    strings.TrimSpace(part.ToolCall.ToolID),
				Arguments: maps.Clone(part.ToolCall.Arguments),
			}
		}
		var toolResult *coreagent.ToolResultPart
		if part.ToolResult != nil {
			toolResult = &coreagent.ToolResultPart{
				ToolCallID: strings.TrimSpace(part.ToolResult.ToolCallID),
				Status:     part.ToolResult.Status,
				Content:    part.ToolResult.Content,
				Output:     maps.Clone(part.ToolResult.Output),
			}
		}
		var imageRef *coreagent.ImageRefPart
		if part.ImageRef != nil {
			imageRef = &coreagent.ImageRefPart{
				URI:      strings.TrimSpace(part.ImageRef.URI),
				MIMEType: strings.TrimSpace(part.ImageRef.MIMEType),
			}
		}
		out = append(out, coreagent.MessagePart{
			Type:       coreagent.MessagePartType(strings.TrimSpace(part.Type)),
			Text:       part.Text,
			JSON:       maps.Clone(part.JSON),
			ToolCall:   toolCall,
			ToolResult: toolResult,
			ImageRef:   imageRef,
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
			Plugin:      strings.TrimSpace(ref.Plugin),
			Operation:   strings.TrimSpace(ref.Operation),
			Connection:  strings.TrimSpace(ref.Connection),
			Instance:    strings.TrimSpace(ref.Instance),
			Title:       strings.TrimSpace(ref.Title),
			Description: strings.TrimSpace(ref.Description),
		})
	}
	return out
}

func agentToolRefsToRequest(refs []coreagent.ToolRef) []agentToolRefRequest {
	if len(refs) == 0 {
		return nil
	}
	out := make([]agentToolRefRequest, 0, len(refs))
	for _, ref := range refs {
		out = append(out, agentToolRefRequest{
			Plugin:      strings.TrimSpace(ref.Plugin),
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
		if strings.TrimSpace(ref.Plugin) == "" {
			return fmt.Errorf("toolRefs[%d].plugin is required", idx)
		}
	}
	return nil
}

func agentToolSourceModeFromRequest(value string) (coreagent.ToolSourceMode, error) {
	switch strings.TrimSpace(value) {
	case "":
		return coreagent.ToolSourceModeUnspecified, nil
	case string(coreagent.ToolSourceModeNativeSearch):
		return coreagent.ToolSourceModeNativeSearch, nil
	default:
		return "", fmt.Errorf("unsupported agent tool source %q", value)
	}
}

func agentSessionStateFromRequest(value string) (coreagent.SessionState, error) {
	switch strings.TrimSpace(value) {
	case "":
		return "", nil
	case string(coreagent.SessionStateActive):
		return coreagent.SessionStateActive, nil
	case string(coreagent.SessionStateArchived):
		return coreagent.SessionStateArchived, nil
	default:
		return "", fmt.Errorf("unsupported agent session state %q", value)
	}
}

func agentSessionInfoFromCore(session *coreagent.Session) agentSessionInfo {
	info := agentSessionInfo{}
	if session == nil {
		return info
	}
	info.ID = session.ID
	info.Provider = strings.TrimSpace(session.ProviderName)
	info.Model = strings.TrimSpace(session.Model)
	info.ClientRef = strings.TrimSpace(session.ClientRef)
	info.State = strings.TrimSpace(string(session.State))
	info.Metadata = maps.Clone(session.Metadata)
	info.CreatedBy = agentActorInfoFromCore(session.CreatedBy)
	info.CreatedAt = session.CreatedAt
	info.UpdatedAt = session.UpdatedAt
	info.LastTurnAt = session.LastTurnAt
	return info
}

func agentTurnInfoFromCore(turn *coreagent.Turn) agentTurnInfo {
	info := agentTurnInfo{}
	if turn == nil {
		return info
	}
	info.ID = turn.ID
	info.SessionID = strings.TrimSpace(turn.SessionID)
	info.Provider = strings.TrimSpace(turn.ProviderName)
	info.Model = strings.TrimSpace(turn.Model)
	info.Status = strings.TrimSpace(string(turn.Status))
	info.Messages = agentMessageInfoFromCore(turn.Messages)
	info.OutputText = turn.OutputText
	info.StructuredOutput = maps.Clone(turn.StructuredOutput)
	info.StatusMessage = turn.StatusMessage
	info.CreatedBy = agentActorInfoFromCore(turn.CreatedBy)
	info.CreatedAt = turn.CreatedAt
	info.StartedAt = turn.StartedAt
	info.CompletedAt = turn.CompletedAt
	info.ExecutionRef = strings.TrimSpace(turn.ExecutionRef)
	return info
}

func agentTurnEventInfoFromCore(event *coreagent.TurnEvent) agentTurnEventInfo {
	if event == nil {
		return agentTurnEventInfo{}
	}
	data := maps.Clone(event.Data)
	if data == nil {
		data = map[string]any{}
	}
	return agentTurnEventInfo{
		ID:         event.ID,
		TurnID:     event.TurnID,
		Seq:        event.Seq,
		Type:       strings.TrimSpace(event.Type),
		Source:     strings.TrimSpace(event.Source),
		Visibility: strings.TrimSpace(event.Visibility),
		Data:       data,
		CreatedAt:  event.CreatedAt,
	}
}

func agentMessageInfoFromCore(messages []coreagent.Message) []agentMessageRequest {
	if len(messages) == 0 {
		return nil
	}
	out := make([]agentMessageRequest, 0, len(messages))
	for _, message := range messages {
		out = append(out, agentMessageRequest{
			Role:     strings.TrimSpace(message.Role),
			Text:     message.Text,
			Parts:    agentMessagePartsFromCore(message.Parts),
			Metadata: maps.Clone(message.Metadata),
		})
	}
	return out
}

func agentMessagePartsFromCore(parts []coreagent.MessagePart) []agentMessagePartRequest {
	if len(parts) == 0 {
		return nil
	}
	out := make([]agentMessagePartRequest, 0, len(parts))
	for _, part := range parts {
		var toolCall *agentMessagePartToolCallRequest
		if part.ToolCall != nil {
			toolCall = &agentMessagePartToolCallRequest{
				ID:        strings.TrimSpace(part.ToolCall.ID),
				ToolID:    strings.TrimSpace(part.ToolCall.ToolID),
				Arguments: maps.Clone(part.ToolCall.Arguments),
			}
		}
		var toolResult *agentMessagePartToolResultRequest
		if part.ToolResult != nil {
			toolResult = &agentMessagePartToolResultRequest{
				ToolCallID: strings.TrimSpace(part.ToolResult.ToolCallID),
				Status:     part.ToolResult.Status,
				Content:    part.ToolResult.Content,
				Output:     maps.Clone(part.ToolResult.Output),
			}
		}
		var imageRef *agentMessagePartImageRefRequest
		if part.ImageRef != nil {
			imageRef = &agentMessagePartImageRefRequest{
				URI:      strings.TrimSpace(part.ImageRef.URI),
				MIMEType: strings.TrimSpace(part.ImageRef.MIMEType),
			}
		}
		out = append(out, agentMessagePartRequest{
			Type:       strings.TrimSpace(string(part.Type)),
			Text:       part.Text,
			JSON:       maps.Clone(part.JSON),
			ToolCall:   toolCall,
			ToolResult: toolResult,
			ImageRef:   imageRef,
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

func agentInteractionInfoFromCore(interaction *coreagent.Interaction) agentInteractionInfo {
	if interaction == nil {
		return agentInteractionInfo{}
	}
	return agentInteractionInfo{
		ID:         strings.TrimSpace(interaction.ID),
		TurnID:     strings.TrimSpace(interaction.TurnID),
		Type:       strings.TrimSpace(string(interaction.Type)),
		State:      strings.TrimSpace(string(interaction.State)),
		Title:      strings.TrimSpace(interaction.Title),
		Prompt:     strings.TrimSpace(interaction.Prompt),
		Request:    maps.Clone(interaction.Request),
		Resolution: maps.Clone(interaction.Resolution),
		CreatedAt:  interaction.CreatedAt,
		ResolvedAt: interaction.ResolvedAt,
	}
}

func (s *Server) writeAgentManagerError(w http.ResponseWriter, r *http.Request, resource string, id string, toolRefs []agentToolRefRequest, err error) {
	pluginName, operation := firstAgentToolTarget(toolRefs)
	switch {
	case errors.Is(err, agentmanager.ErrAgentNotConfigured),
		errors.Is(err, agentmanager.ErrAgentProviderRequired),
		errors.Is(err, agentmanager.ErrAgentProviderNotAvailable),
		errors.Is(err, agentmanager.ErrAgentSessionMetadataNotConfigured),
		errors.Is(err, agentmanager.ErrAgentTurnMetadataNotConfigured):
		writeError(w, http.StatusPreconditionFailed, err.Error())
	case errors.Is(err, agentmanager.ErrAgentSubjectRequired):
		writeError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, agentmanager.ErrAgentSessionCreationInProgress),
		errors.Is(err, agentmanager.ErrAgentTurnCreationInProgress):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, agentmanager.ErrAgentCallerPluginRequired),
		errors.Is(err, agentmanager.ErrAgentInheritedSurfaceTool),
		errors.Is(err, agentmanager.ErrAgentInteractionRequired):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, agentmanager.ErrAgentInteractionNotFound):
		writeError(w, http.StatusNotFound, "agent interaction not found")
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
		s.writeAgentTargetError(w, r, pluginName, operation, err)
	case errors.Is(err, core.ErrNotFound):
		s.writeAgentProviderError(r.Context(), w, resource, id, err)
	default:
		s.writeAgentProviderError(r.Context(), w, resource, id, err)
	}
}

func (s *Server) writeAgentTargetError(w http.ResponseWriter, r *http.Request, pluginName, operation string, err error) {
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

func (s *Server) writeAgentProviderError(ctx context.Context, w http.ResponseWriter, resource string, id string, err error) {
	resource = strings.TrimSpace(resource)
	if resource == "" {
		resource = "agent resource"
	}
	switch {
	case errors.Is(err, core.ErrNotFound):
		writeError(w, http.StatusNotFound, fmt.Sprintf("%s %q not found", resource, id))
	case grpcstatus.Code(err) == codes.InvalidArgument:
		writeError(w, http.StatusBadRequest, grpcstatus.Convert(err).Message())
	default:
		slog.ErrorContext(ctx, "agent provider error", "resource", resource, "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "agent request failed")
	}
}

func firstAgentToolTarget(refs []agentToolRefRequest) (string, string) {
	for _, ref := range refs {
		pluginName := strings.TrimSpace(ref.Plugin)
		operation := strings.TrimSpace(ref.Operation)
		if pluginName == "" && operation == "" {
			continue
		}
		return pluginName, operation
	}
	return "", ""
}
