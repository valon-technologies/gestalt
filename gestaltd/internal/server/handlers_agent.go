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
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

const agentIdempotencyKeyHeader = "Idempotency-Key"
const defaultAgentListSummaryLimit = agentmanager.AgentListSummaryDefaultLimit
const maxAgentListLimit = agentmanager.AgentListMaxLimit
const defaultAgentTurnEventLimit = 100
const maxAgentTurnEventLimit = 1000
const agentTurnEventStreamUntilTerminal = "terminal"
const agentTurnEventStreamUntilBlockedOrTerminal = "blocked_or_terminal"
const agentTurnEventStreamHeartbeatInterval = 15 * time.Second

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
	System         string `json:"system,omitempty"`
	Plugin         string `json:"plugin,omitempty"`
	Operation      string `json:"operation,omitempty"`
	Connection     string `json:"connection,omitempty"`
	Instance       string `json:"instance,omitempty"`
	CredentialMode string `json:"credentialMode,omitempty"`
	Title          string `json:"title,omitempty"`
	Description    string `json:"description,omitempty"`
}

type agentSessionCreateRequest struct {
	ProviderName   string         `json:"provider,omitempty"`
	Model          string         `json:"model,omitempty"`
	ClientRef      string         `json:"clientRef,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	ModelOptions   map[string]any `json:"modelOptions,omitempty"`
	IdempotencyKey string         `json:"idempotencyKey,omitempty"`
}

type agentSessionUpdateRequest struct {
	ClientRef string         `json:"clientRef,omitempty"`
	State     string         `json:"state,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type agentTurnCreateRequest struct {
	Model          string                `json:"model,omitempty"`
	Messages       []agentMessageRequest `json:"messages,omitempty"`
	ToolRefs       []agentToolRefRequest `json:"toolRefs,omitempty"`
	ToolSource     string                `json:"toolSource,omitempty"`
	ResponseSchema map[string]any        `json:"responseSchema,omitempty"`
	Metadata       map[string]any        `json:"metadata,omitempty"`
	ModelOptions   map[string]any        `json:"modelOptions,omitempty"`
	IdempotencyKey string                `json:"idempotencyKey,omitempty"`
	toolRefsSet    bool
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

type agentProviderListInfo struct {
	Providers []agentProviderInfo `json:"providers"`
}

type agentProviderInfo struct {
	Name         string                         `json:"name"`
	Default      bool                           `json:"default,omitempty"`
	Capabilities *agentProviderCapabilitiesInfo `json:"capabilities,omitempty"`
}

type agentProviderCapabilitiesInfo struct {
	StreamingText        bool     `json:"streamingText,omitempty"`
	ToolCalls            bool     `json:"toolCalls,omitempty"`
	ParallelToolCalls    bool     `json:"parallelToolCalls,omitempty"`
	StructuredOutput     bool     `json:"structuredOutput,omitempty"`
	Interactions         bool     `json:"interactions,omitempty"`
	ResumableTurns       bool     `json:"resumableTurns,omitempty"`
	ReasoningSummaries   bool     `json:"reasoningSummaries,omitempty"`
	BoundedListHydration bool     `json:"boundedListHydration,omitempty"`
	SupportedToolSources []string `json:"supportedToolSources,omitempty"`
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
	ID         string                `json:"id"`
	TurnID     string                `json:"turnId"`
	Seq        int64                 `json:"seq"`
	Type       string                `json:"type"`
	Source     string                `json:"source"`
	Visibility string                `json:"visibility"`
	Data       map[string]any        `json:"data"`
	CreatedAt  *time.Time            `json:"createdAt"`
	Display    *agentTurnDisplayInfo `json:"display,omitempty"`
}

type agentTurnDisplayInfo struct {
	Kind      string `json:"kind,omitempty"`
	Phase     string `json:"phase,omitempty"`
	Text      string `json:"text,omitempty"`
	Label     string `json:"label,omitempty"`
	Ref       string `json:"ref,omitempty"`
	ParentRef string `json:"parentRef,omitempty"`
	Input     any    `json:"input,omitempty"`
	Output    any    `json:"output,omitempty"`
	Error     any    `json:"error,omitempty"`
	Action    string `json:"action,omitempty"`
	Format    string `json:"format,omitempty"`
	Language  string `json:"language,omitempty"`
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
		IdempotencyKey: idempotencyKey,
		ProviderName:   strings.TrimSpace(req.ProviderName),
		Model:          strings.TrimSpace(req.Model),
		ClientRef:      strings.TrimSpace(req.ClientRef),
		Metadata:       maps.Clone(req.Metadata),
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
	state, err := agentSessionStateFromRequest(r.URL.Query().Get("state"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	summaryOnly, limit, ok := parseAgentListQuery(w, r)
	if !ok {
		return
	}
	sessions, err := s.agentRuns.ListSessions(r.Context(), p, coreagent.ManagerListSessionsRequest{
		ProviderName: providerName,
		State:        state,
		Limit:        limit,
		SummaryOnly:  summaryOnly,
	})
	if err != nil {
		s.writeAgentManagerError(w, r, "session", "", nil, err)
		return
	}
	out := make([]agentSessionInfo, 0, len(sessions))
	for _, session := range sessions {
		if session == nil {
			continue
		}
		out = append(out, agentSessionInfoFromCoreView(session, summaryOnly))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) listAgentProviders(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.resolveAgentActor(w, r); !ok {
		return
	}
	if s == nil || s.agent == nil {
		writeError(w, http.StatusPreconditionFailed, agentmanager.ErrAgentNotConfigured.Error())
		return
	}

	defaultProvider := ""
	if name, _, err := s.agent.ResolveProviderSelection(""); err == nil {
		defaultProvider = strings.TrimSpace(name)
	}

	names := s.agent.ProviderNames()
	out := agentProviderListInfo{Providers: make([]agentProviderInfo, 0, len(names))}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		info := agentProviderInfo{
			Name:    name,
			Default: name == defaultProvider,
		}
		provider, err := s.agent.ResolveProvider(name)
		if err == nil && provider != nil {
			if caps, err := provider.GetCapabilities(r.Context(), coreagent.GetCapabilitiesRequest{}); err == nil {
				info.Capabilities = agentProviderCapabilitiesInfoFromCore(caps)
			} else {
				slog.WarnContext(r.Context(), "agent provider capabilities unavailable", "provider", name, "error", err)
			}
		}
		out.Providers = append(out.Providers, info)
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
		if err := decodeAgentTurnCreateRequest(r.Body, &req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if req.toolRefsSet && req.ToolRefs == nil {
			writeError(w, http.StatusBadRequest, "toolRefs cannot be null")
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
		IdempotencyKey: idempotencyKey,
		Model:          strings.TrimSpace(req.Model),
		SessionID:      strings.TrimSpace(sessionID),
		Messages:       agentMessagesFromRequest(req.Messages),
		ToolRefs:       agentToolRefsForCreateTurn(req),
		ToolRefsSet:    req.toolRefsSet,
		ToolSource:     toolSource,
		ResponseSchema: maps.Clone(req.ResponseSchema),
		Metadata:       maps.Clone(req.Metadata),
		ModelOptions:   maps.Clone(req.ModelOptions),
	})
	if err != nil {
		if errors.Is(err, agentmanager.ErrAgentSessionNotFound) {
			s.writeAgentManagerError(w, r, "session", sessionID, req.ToolRefs, err)
			return
		}
		s.writeAgentManagerError(w, r, "turn", sessionID, req.ToolRefs, err)
		return
	}
	writeJSON(w, http.StatusCreated, agentTurnInfoFromCore(turn))
}

func decodeAgentTurnCreateRequest(r io.Reader, req *agentTurnCreateRequest) error {
	body, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return io.EOF
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return err
	}
	rawToolRefs, hasToolRefs := fields["toolRefs"]
	if err := json.Unmarshal(body, req); err != nil {
		return err
	}
	req.toolRefsSet = hasToolRefs
	if hasToolRefs && bytes.Equal(bytes.TrimSpace(rawToolRefs), []byte("null")) {
		req.ToolRefs = nil
	}
	return nil
}

func (s *Server) listAgentTurns(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolveAgentActor(w, r)
	if !ok {
		return
	}
	sessionID := chi.URLParam(r, "sessionID")
	statusFilter, err := agentExecutionStatusFromRequest(r.URL.Query().Get("status"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	summaryOnly, limit, ok := parseAgentListQuery(w, r)
	if !ok {
		return
	}
	turns, err := s.agentRuns.ListTurns(r.Context(), p, coreagent.ManagerListTurnsRequest{
		SessionID:   sessionID,
		Status:      statusFilter,
		Limit:       limit,
		SummaryOnly: summaryOnly,
	})
	if err != nil {
		s.writeAgentManagerError(w, r, "session", sessionID, nil, err)
		return
	}
	out := make([]agentTurnInfo, 0, len(turns))
	for _, turn := range turns {
		if turn == nil {
			continue
		}
		out = append(out, agentTurnInfoFromCoreView(turn, summaryOnly))
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
	ctx := r.Context()
	events, err := s.agentRuns.ListTurnEvents(ctx, p, turnID, afterSeq, limit)
	if err != nil {
		s.writeAgentManagerError(w, r, "turn", turnID, nil, err)
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
	w.Header().Set("X-Accel-Buffering", "no")
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	lastWrite := time.Now()

	writeHeartbeat := func(comment string) {
		_, _ = w.Write([]byte(": "))
		_, _ = w.Write([]byte(comment))
		_, _ = w.Write([]byte("\n\n"))
		flusher.Flush()
		lastWrite = time.Now()
	}

	writeEvents := func(events []*coreagent.TurnEvent) bool {
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
			lastWrite = time.Now()
			if info.Seq > afterSeq {
				afterSeq = info.Seq
			}
		}
		return pageFull
	}

	writeStreamError := func(err error) {
		if agentTurnEventStreamContextDone(ctx, err) {
			return
		}
		slog.ErrorContext(ctx, "agent turn event stream failed", "turn_id", turnID, "error", err)
		info := agentTurnEventInfo{
			TurnID:     turnID,
			Type:       "stream.error",
			Source:     "gestaltd",
			Visibility: "public",
			Data: map[string]any{
				"message": "agent event stream failed",
			},
			Display: &agentTurnDisplayInfo{
				Kind:  "error",
				Phase: "failed",
				Text:  "agent event stream failed",
			},
		}
		payload, marshalErr := json.Marshal(info)
		if marshalErr != nil {
			slog.ErrorContext(ctx, "marshal agent turn stream error", "turn_id", turnID, "error", marshalErr)
			return
		}
		_, _ = w.Write([]byte("event: error\n"))
		_, _ = w.Write([]byte("data: "))
		_, _ = w.Write(payload)
		_, _ = w.Write([]byte("\n\n"))
		flusher.Flush()
		lastWrite = time.Now()
	}

	writeHeartbeat("stream-open")
	pageFull := writeEvents(events)
	for {
		if pageFull {
			events, err := s.agentRuns.ListTurnEvents(ctx, p, turnID, afterSeq, limit)
			if err != nil {
				writeStreamError(err)
				return
			}
			pageFull = writeEvents(events)
			continue
		}
		done, err := s.agentTurnStreamDone(ctx, p, turnID, until)
		if err != nil {
			writeStreamError(err)
			return
		}
		if done {
			events, err := s.agentRuns.ListTurnEvents(ctx, p, turnID, afterSeq, limit)
			if err != nil {
				writeStreamError(err)
				return
			}
			if len(events) == 0 {
				return
			}
			pageFull = writeEvents(events)
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if time.Since(lastWrite) >= agentTurnEventStreamHeartbeatInterval {
				writeHeartbeat("keepalive")
			}
			events, err := s.agentRuns.ListTurnEvents(ctx, p, turnID, afterSeq, limit)
			if err != nil {
				writeStreamError(err)
				return
			}
			pageFull = writeEvents(events)
		}
	}
}

func agentTurnEventStreamContextDone(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if ctx != nil {
		if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(err, ctxErr) {
			return true
		}
	}
	return false
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

func parseAgentListQuery(w http.ResponseWriter, r *http.Request) (bool, int, bool) {
	query := r.URL.Query()
	summaryOnly := false
	if raw := strings.TrimSpace(query.Get("summary")); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "summary must be a boolean")
			return false, 0, false
		}
		summaryOnly = value
	}
	switch view := strings.TrimSpace(query.Get("view")); view {
	case "":
	case "full":
		if summaryOnly {
			writeError(w, http.StatusBadRequest, "view=full cannot be combined with summary=true")
			return false, 0, false
		}
	case "summary":
		summaryOnly = true
	default:
		writeError(w, http.StatusBadRequest, "view must be full or summary")
		return false, 0, false
	}

	limit := 0
	if summaryOnly {
		limit = defaultAgentListSummaryLimit
	}
	if raw := strings.TrimSpace(query.Get("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > maxAgentListLimit {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", maxAgentListLimit))
			return false, 0, false
		}
		limit = value
	}
	return summaryOnly, limit, true
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
	for i := range refs {
		ref := refs[i]
		out = append(out, coreagent.ToolRef{
			System:         strings.TrimSpace(ref.System),
			Plugin:         strings.TrimSpace(ref.Plugin),
			Operation:      strings.TrimSpace(ref.Operation),
			Connection:     strings.TrimSpace(ref.Connection),
			Instance:       strings.TrimSpace(ref.Instance),
			CredentialMode: core.ConnectionMode(strings.ToLower(strings.TrimSpace(ref.CredentialMode))),
			Title:          strings.TrimSpace(ref.Title),
			Description:    strings.TrimSpace(ref.Description),
		})
	}
	return out
}

func agentToolRefsForCreateTurn(req agentTurnCreateRequest) []coreagent.ToolRef {
	return agentToolRefsFromRequest(req.ToolRefs)
}

func agentToolRefsToRequest(refs []coreagent.ToolRef) []agentToolRefRequest {
	if len(refs) == 0 {
		return nil
	}
	out := make([]agentToolRefRequest, 0, len(refs))
	for i := range refs {
		ref := refs[i]
		out = append(out, agentToolRefRequest{
			System:         strings.TrimSpace(ref.System),
			Plugin:         strings.TrimSpace(ref.Plugin),
			Operation:      strings.TrimSpace(ref.Operation),
			Connection:     strings.TrimSpace(ref.Connection),
			Instance:       strings.TrimSpace(ref.Instance),
			CredentialMode: string(ref.CredentialMode),
			Title:          strings.TrimSpace(ref.Title),
			Description:    strings.TrimSpace(ref.Description),
		})
	}
	return out
}

func validateAgentToolRefs(refs []agentToolRefRequest) error {
	for idx := range refs {
		ref := refs[idx]
		system := strings.TrimSpace(ref.System)
		plugin := strings.TrimSpace(ref.Plugin)
		operation := strings.TrimSpace(ref.Operation)
		connection := strings.TrimSpace(ref.Connection)
		instance := strings.TrimSpace(ref.Instance)
		credentialMode := strings.TrimSpace(ref.CredentialMode)
		if system == "" && plugin == "" {
			return fmt.Errorf("toolRefs[%d].plugin or system is required", idx)
		}
		if system != "" && plugin != "" {
			return fmt.Errorf("toolRefs[%d] must set exactly one of plugin or system", idx)
		}
		if system != "" {
			if system != coreagent.SystemToolWorkflow {
				return fmt.Errorf("toolRefs[%d].system %q is not supported", idx, system)
			}
			if operation == "" {
				return fmt.Errorf("toolRefs[%d].operation is required for system tool refs", idx)
			}
			if operation == "*" {
				return fmt.Errorf("toolRefs[%d].operation wildcard is not supported", idx)
			}
			if connection != "" || instance != "" || credentialMode != "" || strings.TrimSpace(ref.Title) != "" || strings.TrimSpace(ref.Description) != "" {
				return fmt.Errorf("toolRefs[%d] system refs cannot include connection, instance, credentialMode, title, or description", idx)
			}
		} else {
			if operation == "*" || connection == "*" || instance == "*" {
				return fmt.Errorf("toolRefs[%d] wildcard fields are not supported", idx)
			}
			if plugin == "*" && (operation != "" || connection != "" || instance != "" || credentialMode != "" || strings.TrimSpace(ref.Title) != "" || strings.TrimSpace(ref.Description) != "") {
				return fmt.Errorf("toolRefs[%d] global ref cannot include operation, connection, instance, credentialMode, title, or description", idx)
			}
		}
		switch strings.ToLower(credentialMode) {
		case "":
		default:
			return fmt.Errorf("toolRefs[%d].credentialMode is not supported on public agent requests", idx)
		}
	}
	return nil
}

func agentToolSourceModeFromRequest(value string) (coreagent.ToolSourceMode, error) {
	switch strings.TrimSpace(value) {
	case "":
		return coreagent.ToolSourceModeUnspecified, nil
	case string(coreagent.ToolSourceModeMCPCatalog):
		return coreagent.ToolSourceModeMCPCatalog, nil
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

func agentExecutionStatusFromRequest(value string) (coreagent.ExecutionStatus, error) {
	switch strings.TrimSpace(value) {
	case "":
		return "", nil
	case string(coreagent.ExecutionStatusPending):
		return coreagent.ExecutionStatusPending, nil
	case string(coreagent.ExecutionStatusRunning):
		return coreagent.ExecutionStatusRunning, nil
	case string(coreagent.ExecutionStatusSucceeded):
		return coreagent.ExecutionStatusSucceeded, nil
	case string(coreagent.ExecutionStatusFailed):
		return coreagent.ExecutionStatusFailed, nil
	case string(coreagent.ExecutionStatusCanceled):
		return coreagent.ExecutionStatusCanceled, nil
	case string(coreagent.ExecutionStatusWaitingForInput):
		return coreagent.ExecutionStatusWaitingForInput, nil
	default:
		return "", fmt.Errorf("unsupported agent turn status %q", value)
	}
}

func agentSessionInfoFromCore(session *coreagent.Session) agentSessionInfo {
	return agentSessionInfoFromCoreView(session, false)
}

func agentSessionInfoFromCoreView(session *coreagent.Session, summaryOnly bool) agentSessionInfo {
	info := agentSessionInfo{}
	if session == nil {
		return info
	}
	info.ID = session.ID
	info.Provider = strings.TrimSpace(session.ProviderName)
	info.Model = strings.TrimSpace(session.Model)
	info.ClientRef = strings.TrimSpace(session.ClientRef)
	info.State = strings.TrimSpace(string(session.State))
	info.CreatedBy = agentActorInfoFromCore(session.CreatedBy)
	info.CreatedAt = session.CreatedAt
	info.UpdatedAt = session.UpdatedAt
	info.LastTurnAt = session.LastTurnAt
	if !summaryOnly {
		info.Metadata = maps.Clone(session.Metadata)
	}
	return info
}

func agentProviderCapabilitiesInfoFromCore(caps *coreagent.ProviderCapabilities) *agentProviderCapabilitiesInfo {
	if caps == nil {
		return nil
	}
	return &agentProviderCapabilitiesInfo{
		StreamingText:        caps.StreamingText,
		ToolCalls:            caps.ToolCalls,
		ParallelToolCalls:    caps.ParallelToolCalls,
		StructuredOutput:     caps.StructuredOutput,
		Interactions:         caps.Interactions,
		ResumableTurns:       caps.ResumableTurns,
		ReasoningSummaries:   caps.ReasoningSummaries,
		BoundedListHydration: caps.BoundedListHydration,
		SupportedToolSources: agentToolSourceModesInfoFromCore(caps.SupportedToolSources),
	}
}

func agentToolSourceModesInfoFromCore(modes []coreagent.ToolSourceMode) []string {
	if len(modes) == 0 {
		return nil
	}
	out := make([]string, 0, len(modes))
	for _, mode := range modes {
		value := strings.TrimSpace(string(mode))
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func agentTurnInfoFromCore(turn *coreagent.Turn) agentTurnInfo {
	return agentTurnInfoFromCoreView(turn, false)
}

func agentTurnInfoFromCoreView(turn *coreagent.Turn, summaryOnly bool) agentTurnInfo {
	info := agentTurnInfo{}
	if turn == nil {
		return info
	}
	info.ID = turn.ID
	info.SessionID = strings.TrimSpace(turn.SessionID)
	info.Provider = strings.TrimSpace(turn.ProviderName)
	info.Model = strings.TrimSpace(turn.Model)
	info.Status = strings.TrimSpace(string(turn.Status))
	info.StatusMessage = turn.StatusMessage
	info.CreatedBy = agentActorInfoFromCore(turn.CreatedBy)
	info.CreatedAt = turn.CreatedAt
	info.StartedAt = turn.StartedAt
	info.CompletedAt = turn.CompletedAt
	info.ExecutionRef = strings.TrimSpace(turn.ExecutionRef)
	if !summaryOnly {
		info.Messages = agentMessageInfoFromCore(turn.Messages)
		info.OutputText = turn.OutputText
		info.StructuredOutput = maps.Clone(turn.StructuredOutput)
	}
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
		Display:    agentTurnDisplayInfoFromCore(event.Display),
	}
}

func agentTurnDisplayInfoFromCore(display *coreagent.TurnDisplay) *agentTurnDisplayInfo {
	if display == nil {
		return nil
	}
	return &agentTurnDisplayInfo{
		Kind:      strings.TrimSpace(display.Kind),
		Phase:     strings.TrimSpace(display.Phase),
		Text:      display.Text,
		Label:     display.Label,
		Ref:       display.Ref,
		ParentRef: display.ParentRef,
		Input:     display.Input,
		Output:    display.Output,
		Error:     display.Error,
		Action:    strings.TrimSpace(display.Action),
		Format:    strings.TrimSpace(display.Format),
		Language:  strings.TrimSpace(display.Language),
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
		errors.Is(err, agentmanager.ErrAgentWorkflowToolsNotConfigured),
		errors.Is(err, agentmanager.ErrAgentBoundedListUnsupported):
		writeError(w, http.StatusPreconditionFailed, err.Error())
	case errors.Is(err, agentmanager.ErrAgentProviderNotAvailable):
		writeError(w, http.StatusServiceUnavailable, err.Error())
	case errors.Is(err, agentmanager.ErrAgentSubjectRequired):
		writeError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, agentmanager.ErrAgentCallerPluginRequired),
		errors.Is(err, agentmanager.ErrAgentInheritedSurfaceTool),
		errors.Is(err, agentmanager.ErrAgentInteractionRequired),
		errors.Is(err, agentmanager.ErrAgentInvalidListRequest):
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
		errors.Is(err, invocation.ErrInvalidInvocation),
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
		errors.Is(err, invocation.ErrInvalidInvocation),
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
	for i := range refs {
		ref := refs[i]
		if systemName := strings.TrimSpace(ref.System); systemName != "" {
			return systemName, strings.TrimSpace(ref.Operation)
		}
		pluginName := strings.TrimSpace(ref.Plugin)
		operation := strings.TrimSpace(ref.Operation)
		if pluginName == "" && operation == "" {
			continue
		}
		return pluginName, operation
	}
	return "", ""
}
