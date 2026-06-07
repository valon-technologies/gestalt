package server_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/internal/testutil/metrictest"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	"github.com/valon-technologies/gestalt/server/services/agents/agenttoolid"
	"github.com/valon-technologies/gestalt/server/services/agents/agentturnscope"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/observability"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

func newServerTestAgentToolIDs(t testing.TB) *agenttoolid.Codec {
	t.Helper()
	codec, err := agenttoolid.NewCodec([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("agenttoolid.NewCodec: %v", err)
	}
	return codec
}

func newServerTestAgentTurnScopes() *agentturnscope.Store {
	return agentturnscope.NewStore()
}

func TestAgentCreateTurnReportsProviderDeadlineAsUnavailable(t *testing.T) {
	t.Parallel()

	provider := newMemoryAgentProvider()
	provider.createTurnErr = context.DeadlineExceeded
	ts := newTestServer(t, func(cfg *server.Config) {
		services := testutil.NewStubServices(t)
		agentControl := &stubAgentControl{defaultProviderName: "managed", provider: provider}
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "ada-session" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: "ada@example.com", DisplayName: "Ada"}, nil
			},
		}
		cfg.Services = services
		cfg.Agent = agentControl
		cfg.AgentManager = agentmanager.New(agentmanager.Config{
			Agent:      agentControl,
			TurnScopes: newServerTestAgentTurnScopes(),
			ToolIDs:    newServerTestAgentToolIDs(t),
		})
	})
	testutil.CloseOnCleanup(t, ts)

	sessionReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/sessions", bytes.NewBufferString(`{"provider":"managed","model":"gpt-5.4"}`))
	sessionReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	sessionResp, err := http.DefaultClient.Do(sessionReq)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer func() { _ = sessionResp.Body.Close() }()
	if sessionResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(sessionResp.Body)
		t.Fatalf("create session status = %d body=%s, want 201", sessionResp.StatusCode, body)
	}
	var session map[string]any
	if err := json.NewDecoder(sessionResp.Body).Decode(&session); err != nil {
		t.Fatalf("decode session: %v", err)
	}

	turnReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/sessions/"+session["id"].(string)+"/turns", bytes.NewBufferString(`{"output":{"text":{}},"messages":[{"role":"user","text":"hello"}]}`))
	turnReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	turnResp, err := http.DefaultClient.Do(turnReq)
	if err != nil {
		t.Fatalf("create turn: %v", err)
	}
	defer func() { _ = turnResp.Body.Close() }()
	body, _ := io.ReadAll(turnResp.Body)
	if turnResp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("create turn status = %d body=%s, want 503", turnResp.StatusCode, body)
	}
	if !strings.Contains(string(body), "agent provider unavailable") {
		t.Fatalf("create turn body = %s, want unavailable message", body)
	}
}

func TestAgentRequestsRejectMissingProviderTokenPermission(t *testing.T) {
	t.Parallel()

	provider := newMemoryAgentProvider()
	services := testutil.NewStubServices(t)
	plaintext, hashed, err := principal.GenerateToken(principal.TokenTypeAPI)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	user := seedAPITokenWithPermissions(t, services, plaintext, hashed, "agent-user", []core.AccessPermission{{
		App:        "roadmap",
		Operations: []string{"sync"},
	}})
	ts := newTestServer(t, func(cfg *server.Config) {
		agentControl := &stubAgentControl{defaultProviderName: "managed", provider: provider}
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(context.Context, string) (*core.UserIdentity, error) {
				return nil, core.ErrNotFound
			},
		}
		cfg.Services = services
		cfg.Agent = agentControl
		cfg.AgentManager = agentmanager.New(agentmanager.Config{
			Agent:      agentControl,
			TurnScopes: newServerTestAgentTurnScopes(),
			ToolIDs:    newServerTestAgentToolIDs(t),
		})
	})
	testutil.CloseOnCleanup(t, ts)

	var createSessionCalled atomic.Bool
	provider.createSessionHook = func() {
		createSessionCalled.Store(true)
	}
	sessionReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/sessions", bytes.NewBufferString(`{"provider":"managed","model":"gpt-5.4"}`))
	sessionReq.Header.Set("Authorization", "Bearer "+plaintext)
	sessionResp, err := http.DefaultClient.Do(sessionReq)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer func() { _ = sessionResp.Body.Close() }()
	if sessionResp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(sessionResp.Body)
		t.Fatalf("create session status = %d body=%s, want 403", sessionResp.StatusCode, body)
	}
	if createSessionCalled.Load() {
		t.Fatal("agent provider CreateSession was called despite missing provider token permission")
	}

	now := time.Now().UTC().Truncate(time.Second)
	provider.mu.Lock()
	provider.sessions["session-managed"] = &coreagent.Session{
		ID:                 "session-managed",
		ProviderName:       "managed",
		Model:              "gpt-5.4",
		State:              coreagent.SessionStateActive,
		CreatedBySubjectID: principal.UserSubjectID(user.ID),
		CreatedAt:          &now,
		UpdatedAt:          &now,
	}
	provider.mu.Unlock()

	turnReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/sessions/session-managed/turns", bytes.NewBufferString(`{"output":{"text":{}},"messages":[{"role":"user","text":"hello"}]}`))
	turnReq.Header.Set("Authorization", "Bearer "+plaintext)
	turnResp, err := http.DefaultClient.Do(turnReq)
	if err != nil {
		t.Fatalf("create turn: %v", err)
	}
	defer func() { _ = turnResp.Body.Close() }()
	if turnResp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(turnResp.Body)
		t.Fatalf("create turn status = %d body=%s, want 403", turnResp.StatusCode, body)
	}
	if got := len(provider.capturedGetSessionRequests()); got != 0 {
		t.Fatalf("provider get session requests len = %d, want 0", got)
	}
	if got := len(provider.capturedTurnRequests()); got != 0 {
		t.Fatalf("provider turn requests len = %d, want 0", got)
	}
}

type stubAgentControl struct {
	defaultProviderName string
	provider            coreagent.Provider
}

func (s *stubAgentControl) ResolveProvider(ctx context.Context, name string) (string, coreagent.Provider, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = strings.TrimSpace(s.defaultProviderName)
	}
	if name == "" || s.provider == nil {
		return "", nil, agentmanager.ErrAgentProviderRequired
	}
	return name, s.provider, nil
}

func (s *stubAgentControl) ProviderNames() []string {
	if s.provider == nil || strings.TrimSpace(s.defaultProviderName) == "" {
		return nil
	}
	return []string{strings.TrimSpace(s.defaultProviderName)}
}

func (s *stubAgentControl) Ping(context.Context) error { return nil }

type memoryAgentProvider struct {
	coreagent.UnimplementedProvider
	mu                  sync.Mutex
	sessions            map[string]*coreagent.Session
	turns               map[string]*coreagent.Turn
	turnEvents          map[string][]*coreagent.TurnEvent
	interactions        map[string]*coreagent.Interaction
	turnRequests        []*proto.CreateAgentProviderTurnRequest
	getSessionRequests  []*proto.GetAgentProviderSessionRequest
	getTurnRequests     []*proto.GetAgentProviderTurnRequest
	listSessionRequests []*proto.ListAgentProviderSessionsRequest
	listTurnRequests    []*proto.ListAgentProviderTurnsRequest
	createSessionHook   func()
	createTurnHook      func(*coreagent.Turn)
	createTurnErr       error
	listTurnEventsErr   error
	capabilities        *coreagent.ProviderCapabilities
}

func newMemoryAgentProvider() *memoryAgentProvider {
	return &memoryAgentProvider{
		sessions:     map[string]*coreagent.Session{},
		turns:        map[string]*coreagent.Turn{},
		turnEvents:   map[string][]*coreagent.TurnEvent{},
		interactions: map[string]*coreagent.Interaction{},
	}
}

func (p *memoryAgentProvider) CreateSession(_ context.Context, req *proto.CreateAgentProviderSessionRequest) (*coreagent.Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if req == nil {
		req = &proto.CreateAgentProviderSessionRequest{}
	}
	if p.createSessionHook != nil {
		p.createSessionHook()
	}

	now := time.Now().UTC().Truncate(time.Second)
	session := &coreagent.Session{
		ID:                 req.GetSessionId(),
		ProviderName:       "managed",
		Model:              req.GetModel(),
		ClientRef:          req.GetClientRef(),
		State:              coreagent.SessionStateActive,
		Metadata:           mapFromStruct(req.GetMetadata()),
		CreatedBySubjectID: strings.TrimSpace(req.GetCreatedBySubjectId()),
		CreatedAt:          &now,
		UpdatedAt:          &now,
	}
	p.sessions[session.ID] = session
	return cloneSession(session), nil
}

func (p *memoryAgentProvider) GetSession(_ context.Context, req *proto.GetAgentProviderSessionRequest) (*coreagent.Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if req == nil {
		req = &proto.GetAgentProviderSessionRequest{}
	}

	p.getSessionRequests = append(p.getSessionRequests, cloneProto(req).(*proto.GetAgentProviderSessionRequest))
	session, ok := p.sessions[req.GetSessionId()]
	if !ok {
		return nil, core.ErrNotFound
	}
	return cloneSession(session), nil
}

func (p *memoryAgentProvider) ListSessions(_ context.Context, req *proto.ListAgentProviderSessionsRequest) ([]*coreagent.Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if req == nil {
		req = &proto.ListAgentProviderSessionsRequest{}
	}

	p.listSessionRequests = append(p.listSessionRequests, cloneListSessionsRequest(req))
	out := make([]*coreagent.Session, 0, len(p.sessions))
	for _, session := range p.sessions {
		if state := sessionStateFromProto(req.GetState()); state != "" && session.State != state {
			continue
		}
		out = append(out, cloneSession(session))
		if req.GetLimit() > 0 && len(out) >= int(req.GetLimit()) {
			break
		}
	}
	return out, nil
}

func (p *memoryAgentProvider) UpdateSession(_ context.Context, req *proto.UpdateAgentProviderSessionRequest) (*coreagent.Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if req == nil {
		req = &proto.UpdateAgentProviderSessionRequest{}
	}

	session, ok := p.sessions[req.GetSessionId()]
	if !ok {
		return nil, core.ErrNotFound
	}
	now := time.Now().UTC().Truncate(time.Second)
	if req.GetClientRef() != "" {
		session.ClientRef = req.GetClientRef()
	}
	if state := sessionStateFromProto(req.GetState()); state != "" {
		session.State = state
	}
	if req.GetMetadata() != nil {
		session.Metadata = mapFromStruct(req.GetMetadata())
	}
	session.UpdatedAt = &now
	return cloneSession(session), nil
}

func (p *memoryAgentProvider) CreateTurn(_ context.Context, req *proto.CreateAgentProviderTurnRequest) (*coreagent.Turn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if req == nil {
		req = &proto.CreateAgentProviderTurnRequest{}
	}

	p.turnRequests = append(p.turnRequests, cloneCreateTurnRequest(req))
	if p.createTurnErr != nil {
		return nil, p.createTurnErr
	}
	now := time.Now().UTC().Truncate(time.Second)
	metadata := mapFromStruct(req.GetMetadata())
	turn := &coreagent.Turn{
		ID:                 req.GetTurnId(),
		SessionID:          req.GetSessionId(),
		ProviderName:       "managed",
		Model:              req.GetModel(),
		Status:             coreagent.ExecutionStatusSucceeded,
		Messages:           messagesFromProto(req.GetMessages()),
		CreatedBySubjectID: strings.TrimSpace(req.GetCreatedBySubjectId()),
		CreatedAt:          &now,
		StartedAt:          &now,
		CompletedAt:        &now,
		ExecutionRef:       req.GetExecutionRef(),
		Output:             coreagent.TurnOutput{Text: &coreagent.TurnTextOutput{Text: "turn completed"}},
	}
	if req.GetOutput().GetStructured() != nil {
		turn.Output = coreagent.TurnOutput{
			Structured: &coreagent.TurnStructuredOutput{
				Text:  "turn completed",
				Value: map[string]any{"score": float64(1)},
			},
		}
	}
	if p.createTurnHook != nil {
		p.createTurnHook(turn)
	}
	p.turns[turn.ID] = turn
	p.appendTurnEventLocked(turn.ID, "turn.started", map[string]any{"session_id": req.GetSessionId()})
	if requireInteraction, _ := metadata["requireInteraction"].(bool); requireInteraction {
		turn.Status = coreagent.ExecutionStatusWaitingForInput
		turn.CompletedAt = nil
		turn.StatusMessage = "waiting for input"
		interactionID := "interaction-" + turn.ID
		p.interactions[interactionID] = &coreagent.Interaction{
			ID:        interactionID,
			TurnID:    turn.ID,
			SessionID: turn.SessionID,
			Type:      coreagent.InteractionTypeApproval,
			State:     coreagent.InteractionStatePending,
			Title:     "Approve action",
			Prompt:    "Continue the turn?",
			Request:   map[string]any{"ticket": "RD-42"},
			CreatedAt: &now,
		}
		p.appendTurnEventLocked(turn.ID, "interaction.requested", map[string]any{"interaction_id": interactionID})
	} else {
		if emitToolEvents, _ := metadata["emitToolEvents"].(bool); emitToolEvents {
			startedData := map[string]any{
				"toolName":   "lookup",
				"toolCallId": "call-1",
				"arguments": map[string]any{
					"query": "docs",
				},
			}
			completedData := map[string]any{
				"toolName":   "lookup",
				"toolCallId": "call-1",
				"statusCode": 200,
				"output": map[string]any{
					"hits": float64(2),
				},
			}
			var failedData map[string]any
			if nilDisplayAliases, _ := metadata["nilDisplayAliases"].(bool); nilDisplayAliases {
				startedData["display_input"] = nil
				completedData["display_output"] = nil
				failedData = map[string]any{
					"toolName":      "lookup",
					"toolCallId":    "call-2",
					"display_error": nil,
					"error":         "denied",
				}
			}
			p.appendTurnEventLocked(turn.ID, "tool.started", startedData)
			p.appendTurnEventLocked(turn.ID, "tool.completed", completedData)
			if failedData != nil {
				p.appendTurnEventLocked(turn.ID, "tool.failed", failedData)
			}
		}
		p.appendTurnEventLocked(turn.ID, "assistant.completed", map[string]any{"text": "turn completed"})
		p.appendTurnEventLocked(turn.ID, "turn.completed", map[string]any{"status": "succeeded"})
	}
	if session := p.sessions[req.GetSessionId()]; session != nil {
		session.LastTurnAt = &now
		session.UpdatedAt = &now
	}
	return cloneTurn(turn), nil
}

func (p *memoryAgentProvider) capturedGetSessionRequests() []*proto.GetAgentProviderSessionRequest {
	p.mu.Lock()
	defer p.mu.Unlock()

	return append([]*proto.GetAgentProviderSessionRequest(nil), p.getSessionRequests...)
}

func (p *memoryAgentProvider) capturedTurnRequests() []*proto.CreateAgentProviderTurnRequest {
	p.mu.Lock()
	defer p.mu.Unlock()

	out := make([]*proto.CreateAgentProviderTurnRequest, 0, len(p.turnRequests))
	for _, req := range p.turnRequests {
		out = append(out, cloneCreateTurnRequest(req))
	}
	return out
}

func (p *memoryAgentProvider) capturedGetTurnRequests() []*proto.GetAgentProviderTurnRequest {
	p.mu.Lock()
	defer p.mu.Unlock()

	return append([]*proto.GetAgentProviderTurnRequest(nil), p.getTurnRequests...)
}

func (p *memoryAgentProvider) capturedListSessionRequests() []*proto.ListAgentProviderSessionsRequest {
	p.mu.Lock()
	defer p.mu.Unlock()

	out := make([]*proto.ListAgentProviderSessionsRequest, 0, len(p.listSessionRequests))
	for _, req := range p.listSessionRequests {
		out = append(out, cloneListSessionsRequest(req))
	}
	return out
}

func (p *memoryAgentProvider) capturedListTurnRequests() []*proto.ListAgentProviderTurnsRequest {
	p.mu.Lock()
	defer p.mu.Unlock()

	out := make([]*proto.ListAgentProviderTurnsRequest, 0, len(p.listTurnRequests))
	for _, req := range p.listTurnRequests {
		out = append(out, cloneListTurnsRequest(req))
	}
	return out
}

func (p *memoryAgentProvider) GetTurn(_ context.Context, req *proto.GetAgentProviderTurnRequest) (*coreagent.Turn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if req == nil {
		req = &proto.GetAgentProviderTurnRequest{}
	}

	p.getTurnRequests = append(p.getTurnRequests, cloneProto(req).(*proto.GetAgentProviderTurnRequest))
	turn, ok := p.turns[req.GetTurnId()]
	if !ok {
		return nil, core.ErrNotFound
	}
	return cloneTurn(turn), nil
}

func (p *memoryAgentProvider) ListTurns(_ context.Context, req *proto.ListAgentProviderTurnsRequest) ([]*coreagent.Turn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if req == nil {
		req = &proto.ListAgentProviderTurnsRequest{}
	}

	out := make([]*coreagent.Turn, 0, len(p.turns))
	p.listTurnRequests = append(p.listTurnRequests, cloneListTurnsRequest(req))
	for _, turn := range p.turns {
		if req.GetSessionId() == "" || turn.SessionID == req.GetSessionId() {
			if status := executionStatusFromProto(req.GetStatus()); status != "" && turn.Status != status {
				continue
			}
			out = append(out, cloneTurn(turn))
			if req.GetLimit() > 0 && len(out) >= int(req.GetLimit()) {
				break
			}
		}
	}
	return out, nil
}

func (p *memoryAgentProvider) CancelTurn(_ context.Context, req *proto.CancelAgentProviderTurnRequest) (*coreagent.Turn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if req == nil {
		req = &proto.CancelAgentProviderTurnRequest{}
	}

	turn, ok := p.turns[req.GetTurnId()]
	if !ok {
		return nil, core.ErrNotFound
	}
	now := time.Now().UTC().Truncate(time.Second)
	turn.Status = coreagent.ExecutionStatusCanceled
	turn.StatusMessage = req.GetReason()
	turn.CompletedAt = &now
	p.appendTurnEventLocked(turn.ID, "turn.canceled", map[string]any{"reason": req.GetReason()})
	return cloneTurn(turn), nil
}

func (p *memoryAgentProvider) ListTurnEvents(_ context.Context, req *proto.ListAgentProviderTurnEventsRequest) ([]*coreagent.TurnEvent, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if req == nil {
		req = &proto.ListAgentProviderTurnEventsRequest{}
	}

	if p.listTurnEventsErr != nil {
		return nil, p.listTurnEventsErr
	}
	events := p.turnEvents[req.GetTurnId()]
	out := make([]*coreagent.TurnEvent, 0, len(events))
	for _, event := range events {
		if event.Seq <= req.GetAfterSeq() {
			continue
		}
		out = append(out, cloneTurnEvent(event))
		if req.GetLimit() > 0 && len(out) >= int(req.GetLimit()) {
			break
		}
	}
	return out, nil
}

func (p *memoryAgentProvider) GetInteraction(_ context.Context, req *proto.GetAgentProviderInteractionRequest) (*coreagent.Interaction, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if req == nil {
		req = &proto.GetAgentProviderInteractionRequest{}
	}

	interaction, ok := p.interactions[req.GetInteractionId()]
	if !ok {
		return nil, core.ErrNotFound
	}
	return cloneInteraction(interaction), nil
}

func (p *memoryAgentProvider) ListInteractions(_ context.Context, req *proto.ListAgentProviderInteractionsRequest) ([]*coreagent.Interaction, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if req == nil {
		req = &proto.ListAgentProviderInteractionsRequest{}
	}

	out := make([]*coreagent.Interaction, 0, len(p.interactions))
	for _, interaction := range p.interactions {
		if req.GetTurnId() == "" || interaction.TurnID == req.GetTurnId() {
			out = append(out, cloneInteraction(interaction))
		}
	}
	return out, nil
}

func (p *memoryAgentProvider) ResolveInteraction(_ context.Context, req *proto.ResolveAgentProviderInteractionRequest) (*coreagent.Interaction, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if req == nil {
		req = &proto.ResolveAgentProviderInteractionRequest{}
	}

	interaction, ok := p.interactions[req.GetInteractionId()]
	if !ok {
		return nil, core.ErrNotFound
	}
	now := time.Now().UTC().Truncate(time.Second)
	interaction.State = coreagent.InteractionStateResolved
	interaction.Resolution = mapFromStruct(req.GetResolution())
	interaction.ResolvedAt = &now
	if turn := p.turns[interaction.TurnID]; turn != nil {
		turn.Status = coreagent.ExecutionStatusSucceeded
		turn.CompletedAt = &now
		turn.StatusMessage = "resolved"
		p.appendTurnEventLocked(turn.ID, "interaction.resolved", map[string]any{"interaction_id": interaction.ID})
		p.appendTurnEventLocked(turn.ID, "turn.completed", map[string]any{"status": "succeeded"})
	}
	return cloneInteraction(interaction), nil
}

func (p *memoryAgentProvider) GetCapabilities(context.Context, *proto.GetAgentProviderCapabilitiesRequest) (*coreagent.ProviderCapabilities, error) {
	if p.capabilities != nil {
		caps := *p.capabilities
		caps.SupportedToolSources = append([]coreagent.ToolSourceMode(nil), p.capabilities.SupportedToolSources...)
		return &caps, nil
	}
	return &coreagent.ProviderCapabilities{
		StreamingText:        true,
		ToolCalls:            true,
		Interactions:         true,
		ResumableTurns:       true,
		BoundedListHydration: true,
		SupportedToolSources: []coreagent.ToolSourceMode{
			coreagent.ToolSourceModeCatalog,
		},
	}, nil
}

func (p *memoryAgentProvider) Ping(context.Context) error { return nil }
func (p *memoryAgentProvider) Close() error               { return nil }

func (p *memoryAgentProvider) appendTurnEventLocked(turnID, eventType string, data map[string]any) {
	events := p.turnEvents[turnID]
	now := time.Now().UTC().Truncate(time.Second)
	p.turnEvents[turnID] = append(events, &coreagent.TurnEvent{
		ID:         uuid.NewString(),
		TurnID:     turnID,
		Seq:        int64(len(events) + 1),
		Type:       eventType,
		Source:     "managed",
		Visibility: "private",
		Data:       cloneMap(data),
		CreatedAt:  &now,
	})
}

func cloneSession(src *coreagent.Session) *coreagent.Session {
	if src == nil {
		return nil
	}
	dst := *src
	dst.Metadata = cloneMap(src.Metadata)
	return &dst
}

func cloneTurn(src *coreagent.Turn) *coreagent.Turn {
	if src == nil {
		return nil
	}
	dst := *src
	dst.Messages = append([]coreagent.Message(nil), src.Messages...)
	if src.Output.Text != nil {
		dst.Output.Text = &coreagent.TurnTextOutput{Text: src.Output.Text.Text}
	}
	if src.Output.Structured != nil {
		dst.Output.Structured = &coreagent.TurnStructuredOutput{
			Text:  src.Output.Structured.Text,
			Value: cloneMap(src.Output.Structured.Value),
		}
	}
	return &dst
}

func cloneCreateTurnRequest(src *proto.CreateAgentProviderTurnRequest) *proto.CreateAgentProviderTurnRequest {
	if src == nil {
		return nil
	}
	return cloneProto(src).(*proto.CreateAgentProviderTurnRequest)
}

func cloneListSessionsRequest(src *proto.ListAgentProviderSessionsRequest) *proto.ListAgentProviderSessionsRequest {
	if src == nil {
		return nil
	}
	return cloneProto(src).(*proto.ListAgentProviderSessionsRequest)
}

func cloneListTurnsRequest(src *proto.ListAgentProviderTurnsRequest) *proto.ListAgentProviderTurnsRequest {
	if src == nil {
		return nil
	}
	return cloneProto(src).(*proto.ListAgentProviderTurnsRequest)
}

func cloneProto[T gproto.Message](msg T) gproto.Message {
	return gproto.Clone(msg)
}

func mapFromStruct(value *structpb.Struct) map[string]any {
	if value == nil {
		return nil
	}
	return value.AsMap()
}

func messagesFromProto(values []*proto.AgentMessage) []coreagent.Message {
	if len(values) == 0 {
		return nil
	}
	out := make([]coreagent.Message, 0, len(values))
	for _, value := range values {
		out = append(out, coreagent.Message{
			Role:     value.GetRole(),
			Text:     value.GetText(),
			Metadata: mapFromStruct(value.GetMetadata()),
		})
	}
	return out
}

func sessionStateFromProto(value proto.AgentSessionState) coreagent.SessionState {
	switch value {
	case proto.AgentSessionState_AGENT_SESSION_STATE_ACTIVE:
		return coreagent.SessionStateActive
	case proto.AgentSessionState_AGENT_SESSION_STATE_ARCHIVED:
		return coreagent.SessionStateArchived
	default:
		return ""
	}
}

func executionStatusFromProto(value proto.AgentExecutionStatus) coreagent.ExecutionStatus {
	switch value {
	case proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_PENDING:
		return coreagent.ExecutionStatusPending
	case proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_RUNNING:
		return coreagent.ExecutionStatusRunning
	case proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_SUCCEEDED:
		return coreagent.ExecutionStatusSucceeded
	case proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_FAILED:
		return coreagent.ExecutionStatusFailed
	case proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_CANCELED:
		return coreagent.ExecutionStatusCanceled
	case proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_WAITING_FOR_INPUT:
		return coreagent.ExecutionStatusWaitingForInput
	default:
		return ""
	}
}

func cloneTurnEvent(src *coreagent.TurnEvent) *coreagent.TurnEvent {
	if src == nil {
		return nil
	}
	dst := *src
	dst.Data = cloneMap(src.Data)
	if src.Display != nil {
		display := *src.Display
		dst.Display = &display
	}
	return &dst
}

func cloneInteraction(src *coreagent.Interaction) *coreagent.Interaction {
	if src == nil {
		return nil
	}
	dst := *src
	dst.Request = cloneMap(src.Request)
	dst.Resolution = cloneMap(src.Resolution)
	return &dst
}

func assertHTTPAgentEventDisplay(t *testing.T, event map[string]any, kind, phase, label, text string) {
	t.Helper()
	display, ok := event["display"].(map[string]any)
	if !ok {
		t.Fatalf("event display = %#v, want object", event["display"])
	}
	if display["kind"] != kind {
		t.Fatalf("display kind = %#v, want %q", display["kind"], kind)
	}
	if display["phase"] != phase {
		t.Fatalf("display phase = %#v, want %q", display["phase"], phase)
	}
	if label != "" && display["label"] != label {
		t.Fatalf("display label = %#v, want %q", display["label"], label)
	}
	if text != "" && display["text"] != text {
		t.Fatalf("display text = %#v, want %q", display["text"], text)
	}
}

func findHTTPAgentEvent(t *testing.T, events []map[string]any, eventType string) map[string]any {
	t.Helper()
	for _, event := range events {
		if event["type"] == eventType {
			return event
		}
	}
	t.Fatalf("events = %#v, want type %q", events, eventType)
	return nil
}

func TestAgentSessionsAndTurnsRoundTrip(t *testing.T) {
	t.Parallel()

	provider := newMemoryAgentProvider()
	ts := newTestServer(t, func(cfg *server.Config) {
		services := testutil.NewStubServices(t)
		agentControl := &stubAgentControl{defaultProviderName: "managed", provider: provider}
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "ada-session" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: "ada@example.com", DisplayName: "Ada"}, nil
			},
		}
		cfg.Services = services
		cfg.Agent = agentControl
		cfg.AgentManager = agentmanager.New(agentmanager.Config{
			Agent: agentControl,
			Providers: testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
				N:        "docs",
				ConnMode: core.ConnectionModeNone,
				CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
					ID:       "search",
					Title:    "Search",
					ReadOnly: true,
				}}},
			}),
			TurnScopes: newServerTestAgentTurnScopes(),
			ToolIDs:    newServerTestAgentToolIDs(t),
		})
	})
	testutil.CloseOnCleanup(t, ts)

	sessionReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/sessions", bytes.NewBufferString(`{"provider":"managed","model":"gpt-5.4","clientRef":"cli-1","metadata":{"project":"docs"},"tools":{"catalog":{"refs":[{"app":"docs","operation":"search"}]}}}`))
	sessionReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	sessionResp, err := http.DefaultClient.Do(sessionReq)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer func() { _ = sessionResp.Body.Close() }()
	if sessionResp.StatusCode != http.StatusCreated {
		t.Fatalf("create session status = %d", sessionResp.StatusCode)
	}
	var session map[string]any
	if err := json.NewDecoder(sessionResp.Body).Decode(&session); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	sessionID := session["id"].(string)

	listSessionsReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/agent/sessions", nil)
	listSessionsReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	listSessionsResp, err := http.DefaultClient.Do(listSessionsReq)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	defer func() { _ = listSessionsResp.Body.Close() }()
	if listSessionsResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(listSessionsResp.Body)
		t.Fatalf("list sessions status = %d body=%s", listSessionsResp.StatusCode, body)
	}
	var sessions []map[string]any
	if err := json.NewDecoder(listSessionsResp.Body).Decode(&sessions); err != nil {
		t.Fatalf("decode sessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0]["id"] != sessionID {
		t.Fatalf("sessions = %#v, want %q", sessions, sessionID)
	}

	providersReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/agent/providers", nil)
	providersReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	providersResp, err := http.DefaultClient.Do(providersReq)
	if err != nil {
		t.Fatalf("list providers: %v", err)
	}
	defer func() { _ = providersResp.Body.Close() }()
	if providersResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(providersResp.Body)
		t.Fatalf("list providers status = %d body=%s", providersResp.StatusCode, body)
	}
	var providers struct {
		Providers []struct {
			Name         string `json:"name"`
			Default      bool   `json:"default"`
			Capabilities struct {
				StreamingText        bool     `json:"streamingText"`
				BoundedListHydration bool     `json:"boundedListHydration"`
				SupportedToolSources []string `json:"supportedToolSources"`
			} `json:"capabilities"`
		} `json:"providers"`
	}
	if err := json.NewDecoder(providersResp.Body).Decode(&providers); err != nil {
		t.Fatalf("decode providers: %v", err)
	}
	if len(providers.Providers) != 1 {
		t.Fatalf("providers = %#v, want one provider", providers.Providers)
	}
	gotProvider := providers.Providers[0]
	if gotProvider.Name != "managed" || !gotProvider.Default {
		t.Fatalf("provider = %#v, want managed default", gotProvider)
	}
	if !gotProvider.Capabilities.StreamingText || !gotProvider.Capabilities.BoundedListHydration {
		t.Fatalf("provider capabilities = %#v, want streaming text and catalog listing", gotProvider.Capabilities)
	}
	turnReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/sessions/"+sessionID+"/turns", bytes.NewBufferString(`{"output":{"text":{}},"messages":[{"role":"user","text":"hello"}]}`))
	turnReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	turnResp, err := http.DefaultClient.Do(turnReq)
	if err != nil {
		t.Fatalf("create turn: %v", err)
	}
	defer func() { _ = turnResp.Body.Close() }()
	if turnResp.StatusCode != http.StatusCreated {
		t.Fatalf("create turn status = %d", turnResp.StatusCode)
	}
	var turn map[string]any
	if err := json.NewDecoder(turnResp.Body).Decode(&turn); err != nil {
		t.Fatalf("decode turn: %v", err)
	}
	turnID := turn["id"].(string)
	turnRequests := provider.capturedTurnRequests()
	if len(turnRequests) != 1 {
		t.Fatalf("provider turn requests len = %d, want 1", len(turnRequests))
	}
	if got := turnRequests[0].GetTools(); len(got) != 0 {
		t.Fatalf("provider turn tools = %#v, want no preloaded tools", got)
	}

	eventsReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/agent/turns/"+turnID+"/events?after=0&limit=10", nil)
	eventsReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	eventsResp, err := http.DefaultClient.Do(eventsReq)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	defer func() { _ = eventsResp.Body.Close() }()
	var events []map[string]any
	if err := json.NewDecoder(eventsResp.Body).Decode(&events); err != nil {
		t.Fatalf("decode events: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("events len = %d, want 3", len(events))
	}
	assertHTTPAgentEventDisplay(t, events[0], "status", "started", "turn", "")
	assertHTTPAgentEventDisplay(t, events[1], "text", "completed", "", "turn completed")
	assistantDisplay := events[1]["display"].(map[string]any)
	if assistantDisplay["format"] != "markdown" {
		t.Fatalf("assistant display format = %#v, want markdown", assistantDisplay["format"])
	}
	if _, ok := events[2]["display"]; ok {
		t.Fatalf("turn.completed display = %#v, want omitted", events[2]["display"])
	}

	listReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/agent/sessions/"+sessionID+"/turns", nil)
	listReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	listResp, err := http.DefaultClient.Do(listReq)
	if err != nil {
		t.Fatalf("list turns: %v", err)
	}
	defer func() { _ = listResp.Body.Close() }()
	var turns []map[string]any
	if err := json.NewDecoder(listResp.Body).Decode(&turns); err != nil {
		t.Fatalf("decode turns: %v", err)
	}
	var listedTurn map[string]any
	for _, candidate := range turns {
		if candidate["id"] == turnID {
			listedTurn = candidate
			break
		}
	}
	if listedTurn == nil {
		t.Fatalf("turns = %#v, want %q", turns, turnID)
	}
	output := listedTurn["output"].(map[string]any)
	textOutput := output["text"].(map[string]any)
	if len(listedTurn["messages"].([]any)) != 1 || textOutput["text"] != "turn completed" {
		t.Fatalf("full turn = %#v, want messages and output text", listedTurn)
	}

	summarySessionsReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/agent/sessions?view=summary&state=active", nil)
	summarySessionsReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	summarySessionsResp, err := http.DefaultClient.Do(summarySessionsReq)
	if err != nil {
		t.Fatalf("list summary sessions: %v", err)
	}
	defer func() { _ = summarySessionsResp.Body.Close() }()
	if summarySessionsResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(summarySessionsResp.Body)
		t.Fatalf("list summary sessions status = %d body=%s", summarySessionsResp.StatusCode, body)
	}
	var summarySessions []map[string]any
	if err := json.NewDecoder(summarySessionsResp.Body).Decode(&summarySessions); err != nil {
		t.Fatalf("decode summary sessions: %v", err)
	}
	if len(summarySessions) != 1 || summarySessions[0]["id"] != sessionID {
		t.Fatalf("summary sessions = %#v, want %q", summarySessions, sessionID)
	}
	if _, ok := summarySessions[0]["metadata"]; ok {
		t.Fatalf("summary session metadata = %#v, want omitted", summarySessions[0]["metadata"])
	}
	listSessionRequests := provider.capturedListSessionRequests()
	if got := listSessionRequests[len(listSessionRequests)-1]; got.GetState() != proto.AgentSessionState_AGENT_SESSION_STATE_ACTIVE || got.GetLimit() != 100 || !got.GetSummaryOnly() {
		t.Fatalf("provider list sessions request = %#v, want active summary default limit", got)
	} else if got.GetSubject().GetId() == "" {
		t.Fatalf("provider list sessions request subject = %#v, want caller subject", got.Subject)
	}

	summaryTurnsReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/agent/sessions/"+sessionID+"/turns?summary=true&limit=1&status=succeeded", nil)
	summaryTurnsReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	summaryTurnsResp, err := http.DefaultClient.Do(summaryTurnsReq)
	if err != nil {
		t.Fatalf("list summary turns: %v", err)
	}
	defer func() { _ = summaryTurnsResp.Body.Close() }()
	if summaryTurnsResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(summaryTurnsResp.Body)
		t.Fatalf("list summary turns status = %d body=%s", summaryTurnsResp.StatusCode, body)
	}
	var summaryTurns []map[string]any
	if err := json.NewDecoder(summaryTurnsResp.Body).Decode(&summaryTurns); err != nil {
		t.Fatalf("decode summary turns: %v", err)
	}
	if len(summaryTurns) != 1 || summaryTurns[0]["id"] != turnID {
		t.Fatalf("summary turns = %#v, want %q", summaryTurns, turnID)
	}
	if _, ok := summaryTurns[0]["messages"]; ok {
		t.Fatalf("summary turn messages = %#v, want omitted", summaryTurns[0]["messages"])
	}
	if _, ok := summaryTurns[0]["output"]; ok {
		t.Fatalf("summary turn output = %#v, want omitted", summaryTurns[0]["output"])
	}
	listTurnRequests := provider.capturedListTurnRequests()
	if got := listTurnRequests[len(listTurnRequests)-1]; got.GetStatus() != proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_SUCCEEDED || got.GetLimit() != 1 || !got.GetSummaryOnly() {
		t.Fatalf("provider list turns request = %#v, want succeeded summary limit 1", got)
	} else if got.GetSubject().GetId() == "" {
		t.Fatalf("provider list turns request subject = %#v, want caller subject", got.Subject)
	}

	cancelReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/turns/"+turnID+"/cancel", bytes.NewBufferString(`{"reason":"stop"}`))
	cancelReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	cancelResp, err := http.DefaultClient.Do(cancelReq)
	if err != nil {
		t.Fatalf("cancel turn: %v", err)
	}
	defer func() { _ = cancelResp.Body.Close() }()
	if cancelResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(cancelResp.Body)
		t.Fatalf("cancel turn status = %d body=%s", cancelResp.StatusCode, body)
	}
	for _, got := range provider.capturedGetSessionRequests() {
		if got.GetSubject().GetId() == "" {
			t.Fatalf("provider get session request subject = %#v, want caller subject", got.Subject)
		}
	}
	for _, got := range provider.capturedGetTurnRequests() {
		if got.GetSubject().GetId() == "" {
			t.Fatalf("provider get turn request subject = %#v, want caller subject", got.Subject)
		}
	}
}

func TestAgentHarnessResolveUsesDefaultAndNamedHarness(t *testing.T) {
	t.Parallel()

	provider := newMemoryAgentProvider()
	ts := newTestServer(t, func(cfg *server.Config) {
		services := testutil.NewStubServices(t)
		agentControl := &stubAgentControl{defaultProviderName: "managed", provider: provider}
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "ada-harness" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: "ada@example.com", DisplayName: "Ada"}, nil
			},
		}
		cfg.Services = services
		cfg.Agent = agentControl
		cfg.AgentDefs = map[string]*config.ProviderEntry{
			"managed": {
				DefaultHarness: "default",
				Harnesses: map[string]*config.ProviderEntryHarnessConfig{
					"default": {
						Command:          "hermes",
						Args:             []string{"acp"},
						Env:              map[string]string{"OPENAI_BASE_URL": "https://example.test"},
						WorkingDirectory: "/workspace/default",
						RequiredCommands: []string{"hermes"},
						Install: &config.ProviderEntryHarnessInstallConfig{
							Instructions: "Install Hermes before running this harness.",
							Commands: []config.ProviderEntryHarnessInstallCommand{{
								Command: "npm",
								Args:    []string{"install", "-g", "@example/hermes"},
							}},
						},
					},
					"no-tools": {
						Command: "hermes",
						Args:    []string{"acp", "--no-tools"},
					},
				},
			},
		}
		cfg.AgentManager = agentmanager.New(agentmanager.Config{
			Agent:      agentControl,
			TurnScopes: newServerTestAgentTurnScopes(),
			ToolIDs:    newServerTestAgentToolIDs(t),
		})
	})
	testutil.CloseOnCleanup(t, ts)

	defaultReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/harnesses/resolve", bytes.NewBufferString(`{}`))
	defaultReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-harness"})
	defaultResp, err := http.DefaultClient.Do(defaultReq)
	if err != nil {
		t.Fatalf("resolve default harness: %v", err)
	}
	defer func() { _ = defaultResp.Body.Close() }()
	if defaultResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(defaultResp.Body)
		t.Fatalf("resolve default harness status = %d body=%s, want 200", defaultResp.StatusCode, body)
	}
	var defaultPlan map[string]any
	if err := json.NewDecoder(defaultResp.Body).Decode(&defaultPlan); err != nil {
		t.Fatalf("decode default plan: %v", err)
	}
	if defaultPlan["provider"] != "managed" || defaultPlan["harness"] != "default" || defaultPlan["command"] != "hermes" {
		t.Fatalf("default plan = %#v", defaultPlan)
	}
	install, ok := defaultPlan["install"].(map[string]any)
	if !ok {
		t.Fatalf("default plan install = %#v, want object", defaultPlan["install"])
	}
	if install["instructions"] != "Install Hermes before running this harness." {
		t.Fatalf("default plan install = %#v", install)
	}
	commands, ok := install["commands"].([]any)
	if !ok || len(commands) != 1 {
		t.Fatalf("default plan install commands = %#v, want one command", install["commands"])
	}
	installCommand, ok := commands[0].(map[string]any)
	if !ok || installCommand["command"] != "npm" {
		t.Fatalf("default plan install command = %#v", commands[0])
	}

	namedReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/harnesses/resolve", bytes.NewBufferString(`{"provider":"managed","harness":"no-tools"}`))
	namedReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-harness"})
	namedResp, err := http.DefaultClient.Do(namedReq)
	if err != nil {
		t.Fatalf("resolve named harness: %v", err)
	}
	defer func() { _ = namedResp.Body.Close() }()
	if namedResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(namedResp.Body)
		t.Fatalf("resolve named harness status = %d body=%s, want 200", namedResp.StatusCode, body)
	}
	var namedPlan map[string]any
	if err := json.NewDecoder(namedResp.Body).Decode(&namedPlan); err != nil {
		t.Fatalf("decode named plan: %v", err)
	}
	if namedPlan["harness"] != "no-tools" {
		t.Fatalf("named plan = %#v, want no-tools harness", namedPlan)
	}
}

func TestAgentTurnOmittedToolsDefaultToNone(t *testing.T) {
	t.Parallel()

	provider := newMemoryAgentProvider()
	ts := newTestServer(t, func(cfg *server.Config) {
		services := testutil.NewStubServices(t)
		agentControl := &stubAgentControl{defaultProviderName: "managed", provider: provider}
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "ada-session" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: "ada@example.com", DisplayName: "Ada"}, nil
			},
		}
		cfg.Services = services
		cfg.Agent = agentControl
		cfg.AgentManager = agentmanager.New(agentmanager.Config{
			Agent: agentControl,
			Providers: testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
				N:        "docs",
				ConnMode: core.ConnectionModeNone,
				CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
					ID:       "search",
					Title:    "Search",
					ReadOnly: true,
				}}},
			}),
			TurnScopes: newServerTestAgentTurnScopes(),
			ToolIDs:    newServerTestAgentToolIDs(t),
		})
	})
	testutil.CloseOnCleanup(t, ts)

	sessionReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/sessions", bytes.NewBufferString(`{"provider":"managed","model":"gpt-5.4"}`))
	sessionReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	sessionResp, err := http.DefaultClient.Do(sessionReq)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer func() { _ = sessionResp.Body.Close() }()
	if sessionResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(sessionResp.Body)
		t.Fatalf("create session status = %d body=%s", sessionResp.StatusCode, body)
	}
	var session map[string]any
	if err := json.NewDecoder(sessionResp.Body).Decode(&session); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	sessionID := session["id"].(string)

	createTurn := func(name, body string, wantStatus int) string {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/sessions/"+sessionID+"/turns", bytes.NewBufferString(body))
		req.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s: create turn: %v", name, err)
		}
		defer func() { _ = resp.Body.Close() }()
		respBody, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != wantStatus {
			t.Fatalf("%s: create turn status = %d body=%s, want %d", name, resp.StatusCode, respBody, wantStatus)
		}
		return string(respBody)
	}

	createTurn("omitted", `{"output":{"text":{}},"messages":[{"role":"user","text":"hello"}]}`, http.StatusCreated)
	if body := createTurn("explicit empty", `{"output":{"text":{}},"messages":[{"role":"user","text":"hello"}],"toolRefs":[]}`, http.StatusBadRequest); !strings.Contains(body, "turn tools must be configured on the session") {
		t.Fatalf("explicit empty response body = %s", body)
	}
	if body := createTurn("tool source", `{"output":{"text":{}},"messages":[{"role":"user","text":"hello"}],"toolSource":"catalog"}`, http.StatusBadRequest); !strings.Contains(body, "turn tools must be configured on the session") {
		t.Fatalf("tool source response body = %s", body)
	}
	createTurn("plugin broad", `{"output":{"text":{}},"messages":[{"role":"user","text":"hello"}],"toolRefs":[{"app":"docs"}]}`, http.StatusBadRequest)
	createTurn("missing output", `{"messages":[{"role":"user","text":"hello"}]}`, http.StatusBadRequest)
	createTurn("null toolRefs", `{"output":{"text":{}},"messages":[{"role":"user","text":"hello"}],"toolRefs":null}`, http.StatusBadRequest)
	createTurn("global credential mode", `{"output":{"text":{}},"messages":[{"role":"user","text":"hello"}],"toolRefs":[{"app":"*","credentialMode":"none"}]}`, http.StatusBadRequest)
	createTurn("system title", `{"output":{"text":{}},"messages":[{"role":"user","text":"hello"}],"toolRefs":[{"system":"workflow","operation":"schedules.list","title":"Schedules"}]}`, http.StatusBadRequest)

	turnRequests := provider.capturedTurnRequests()
	if len(turnRequests) != 1 {
		t.Fatalf("provider turn requests len = %d, want 1", len(turnRequests))
	}
}

func TestAgentCreateTurnUsesNoToolsWithStructuredOutput(t *testing.T) {
	t.Parallel()

	provider := newMemoryAgentProvider()
	provider.capabilities = &coreagent.ProviderCapabilities{
		SupportedToolSources: []coreagent.ToolSourceMode{
			coreagent.ToolSourceModeNone,
		},
	}
	ts := newTestServer(t, func(cfg *server.Config) {
		services := testutil.NewStubServices(t)
		agentControl := &stubAgentControl{defaultProviderName: "managed", provider: provider}
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "ada-session" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: "ada@example.com", DisplayName: "Ada"}, nil
			},
		}
		cfg.Services = services
		cfg.Agent = agentControl
		cfg.AgentManager = agentmanager.New(agentmanager.Config{
			Agent:      agentControl,
			TurnScopes: newServerTestAgentTurnScopes(),
			ToolIDs:    newServerTestAgentToolIDs(t),
		})
	})
	testutil.CloseOnCleanup(t, ts)

	sessionReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/sessions", bytes.NewBufferString(`{"provider":"managed","model":"gpt-5.4"}`))
	sessionReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	sessionResp, err := http.DefaultClient.Do(sessionReq)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer func() { _ = sessionResp.Body.Close() }()
	if sessionResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(sessionResp.Body)
		t.Fatalf("create session status = %d body=%s", sessionResp.StatusCode, body)
	}
	var session map[string]any
	if err := json.NewDecoder(sessionResp.Body).Decode(&session); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	sessionID := session["id"].(string)

	createTurn := func(name, body string, wantStatus int) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/sessions/"+sessionID+"/turns", bytes.NewBufferString(body))
		req.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s: create turn: %v", name, err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != wantStatus {
			respBody, _ := io.ReadAll(resp.Body)
			t.Fatalf("%s: create turn status = %d body=%s, want %d", name, resp.StatusCode, respBody, wantStatus)
		}
	}

	createTurn("structured none", `{"messages":[{"role":"user","text":"grade"}],"output":{"structured":{"schema":{"type":"object","properties":{"score":{"type":"number"}}}}}}`, http.StatusCreated)
	createTurn("null output", `{"messages":[{"role":"user","text":"grade"}],"output":null}`, http.StatusBadRequest)
	createTurn("ambiguous output", `{"messages":[{"role":"user","text":"grade"}],"output":{"text":{},"structured":{"schema":{"type":"object"}}}}`, http.StatusBadRequest)
	createTurn("ambiguous null text output", `{"messages":[{"role":"user","text":"grade"}],"output":{"text":null,"structured":{"schema":{"type":"object"}}}}`, http.StatusBadRequest)
	createTurn("empty schema", `{"messages":[{"role":"user","text":"grade"}],"output":{"structured":{"schema":{}}}}`, http.StatusBadRequest)
	createTurn("none with tools", `{"messages":[{"role":"user","text":"grade"}],"toolSource":"none","toolRefs":[{"app":"docs"}],"output":{"text":{}}}`, http.StatusBadRequest)

	turnRequests := provider.capturedTurnRequests()
	if len(turnRequests) != 1 {
		t.Fatalf("provider turn requests len = %d, want 1", len(turnRequests))
	}
	req := turnRequests[0]
	if len(req.GetTools()) != 0 {
		t.Fatalf("provider turn tools = %#v, want none", req.GetTools())
	}
	if req.GetOutput().GetStructured() == nil {
		t.Fatal("provider turn output.structured = nil, want structured output request")
	}
	if req.GetOutput().GetStructured().GetSchema().AsMap()["type"] != "object" {
		t.Fatalf("provider turn response schema = %#v, want object schema", req.GetOutput().GetStructured().GetSchema())
	}
}

func TestAgentTurnOmittedToolsDoNotForceCatalogForUnsupportedProvider(t *testing.T) {
	t.Parallel()

	provider := newMemoryAgentProvider()
	provider.capabilities = &coreagent.ProviderCapabilities{
		BoundedListHydration: true,
	}
	ts := newTestServer(t, func(cfg *server.Config) {
		services := testutil.NewStubServices(t)
		cfg.Auth = nil
		cfg.Services = services
		cfg.AgentManager = agentmanager.New(agentmanager.Config{
			Agent:      &stubAgentControl{defaultProviderName: "managed", provider: provider},
			TurnScopes: newServerTestAgentTurnScopes(),
			ToolIDs:    newServerTestAgentToolIDs(t),
		})
	})
	testutil.CloseOnCleanup(t, ts)

	sessionReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/sessions/", bytes.NewBufferString(`{"provider":"managed","model":"gpt-5.4"}`))
	sessionResp, err := http.DefaultClient.Do(sessionReq)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer func() { _ = sessionResp.Body.Close() }()
	if sessionResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(sessionResp.Body)
		t.Fatalf("create session status = %d body=%s", sessionResp.StatusCode, body)
	}
	var session map[string]any
	if err := json.NewDecoder(sessionResp.Body).Decode(&session); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	sessionID := session["id"].(string)

	turnReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/sessions/"+sessionID+"/turns", bytes.NewBufferString(`{"output":{"text":{}},"messages":[{"role":"user","text":"hello"}]}`))
	turnResp, err := http.DefaultClient.Do(turnReq)
	if err != nil {
		t.Fatalf("create turn: %v", err)
	}
	defer func() { _ = turnResp.Body.Close() }()
	if turnResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(turnResp.Body)
		t.Fatalf("create turn status = %d body=%s", turnResp.StatusCode, body)
	}

	turnRequests := provider.capturedTurnRequests()
	if len(turnRequests) != 1 {
		t.Fatalf("provider turn requests len = %d, want 1", len(turnRequests))
	}
}

func TestAgentTurnEventsNormalizeToolPayloads(t *testing.T) {
	t.Parallel()

	provider := newMemoryAgentProvider()
	ts := newTestServer(t, func(cfg *server.Config) {
		services := testutil.NewStubServices(t)
		cfg.Auth = nil
		cfg.Services = services
		cfg.AgentManager = agentmanager.New(agentmanager.Config{
			Agent:      &stubAgentControl{defaultProviderName: "managed", provider: provider},
			TurnScopes: newServerTestAgentTurnScopes(),
			ToolIDs:    newServerTestAgentToolIDs(t),
		})
	})
	testutil.CloseOnCleanup(t, ts)

	sessionReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/sessions/", bytes.NewBufferString(`{"provider":"managed","model":"gpt-5.4"}`))
	sessionResp, err := http.DefaultClient.Do(sessionReq)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer func() { _ = sessionResp.Body.Close() }()
	var session map[string]any
	if err := json.NewDecoder(sessionResp.Body).Decode(&session); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	sessionID := session["id"].(string)

	turnReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/sessions/"+sessionID+"/turns", bytes.NewBufferString(`{"metadata":{"emitToolEvents":true,"nilDisplayAliases":true},"output":{"text":{}},"messages":[{"role":"user","text":"lookup"}]}`))
	turnResp, err := http.DefaultClient.Do(turnReq)
	if err != nil {
		t.Fatalf("create turn: %v", err)
	}
	defer func() { _ = turnResp.Body.Close() }()
	var turn map[string]any
	if err := json.NewDecoder(turnResp.Body).Decode(&turn); err != nil {
		t.Fatalf("decode turn: %v", err)
	}
	turnID := turn["id"].(string)

	eventsResp, err := http.Get(ts.URL + "/api/v1/agent/turns/" + turnID + "/events?after=0&limit=10")
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	defer func() { _ = eventsResp.Body.Close() }()
	var events []map[string]any
	if err := json.NewDecoder(eventsResp.Body).Decode(&events); err != nil {
		t.Fatalf("decode events: %v", err)
	}

	started := findHTTPAgentEvent(t, events, "tool.started")
	assertHTTPAgentEventDisplay(t, started, "tool", "started", "lookup", "")
	startedDisplay := started["display"].(map[string]any)
	if startedDisplay["action"] != "Running" {
		t.Fatalf("tool.started display action = %#v, want Running", startedDisplay["action"])
	}
	if startedDisplay["ref"] != "call-1" {
		t.Fatalf("tool.started display ref = %#v, want call-1", startedDisplay["ref"])
	}
	startedInput, ok := startedDisplay["input"].(map[string]any)
	if !ok || startedInput["query"] != "docs" {
		t.Fatalf("tool.started display input = %#v, want query docs", startedDisplay["input"])
	}

	completed := findHTTPAgentEvent(t, events, "tool.completed")
	assertHTTPAgentEventDisplay(t, completed, "tool", "completed", "lookup", "")
	completedDisplay := completed["display"].(map[string]any)
	if completedDisplay["action"] != "Ran" {
		t.Fatalf("tool.completed display action = %#v, want Ran", completedDisplay["action"])
	}
	completedOutput, ok := completedDisplay["output"].(map[string]any)
	if !ok || completedOutput["hits"] != float64(2) {
		t.Fatalf("tool.completed display output = %#v, want hits 2", completedDisplay["output"])
	}

	failed := findHTTPAgentEvent(t, events, "tool.failed")
	assertHTTPAgentEventDisplay(t, failed, "tool", "failed", "lookup", "denied")
	failedDisplay := failed["display"].(map[string]any)
	if failedDisplay["action"] != "Failed" {
		t.Fatalf("tool.failed display action = %#v, want Failed", failedDisplay["action"])
	}
	if failedDisplay["error"] != "denied" {
		t.Fatalf("tool.failed display error = %#v, want denied", failedDisplay["error"])
	}
}

func TestAgentSessionAndTurnMetrics(t *testing.T) {
	t.Parallel()

	provider := observability.InstrumentAgentProvider("managed", newMemoryAgentProvider())
	metrics := metrictest.NewManualMeterProvider(t)
	services := testutil.NewStubServices(t)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.MeterProvider = metrics.Provider
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "metric-session" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: "metrics@example.com", DisplayName: "Metrics"}, nil
			},
		}
		cfg.Services = services
		cfg.AgentManager = agentmanager.New(agentmanager.Config{
			Agent:      &stubAgentControl{defaultProviderName: "managed", provider: provider},
			Providers:  testutil.NewProviderRegistry(t),
			TurnScopes: newServerTestAgentTurnScopes(),
			ToolIDs:    newServerTestAgentToolIDs(t),
		})
	})
	testutil.CloseOnCleanup(t, ts)

	sessionReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/sessions", bytes.NewBufferString(`{"provider":"managed","model":"claude-sonnet"}`))
	sessionReq.AddCookie(&http.Cookie{Name: "session_token", Value: "metric-session"})
	sessionResp, err := http.DefaultClient.Do(sessionReq)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer func() { _ = sessionResp.Body.Close() }()
	if sessionResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(sessionResp.Body)
		t.Fatalf("create session status = %d body=%s", sessionResp.StatusCode, string(body))
	}
	var session map[string]any
	if err := json.NewDecoder(sessionResp.Body).Decode(&session); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	sessionID, _ := session["id"].(string)
	if sessionID == "" {
		t.Fatalf("session response missing id: %#v", session)
	}

	turnReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/sessions/"+sessionID+"/turns", bytes.NewBufferString(`{"output":{"text":{}},"messages":[{"role":"user","text":"hello"}]}`))
	turnReq.AddCookie(&http.Cookie{Name: "session_token", Value: "metric-session"})
	turnResp, err := http.DefaultClient.Do(turnReq)
	if err != nil {
		t.Fatalf("create turn: %v", err)
	}
	defer func() { _ = turnResp.Body.Close() }()
	if turnResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(turnResp.Body)
		t.Fatalf("create turn status = %d body=%s", turnResp.StatusCode, string(body))
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.agent.operation.count", 1, map[string]string{
		"gestalt.agent.operation": "create_session",
	})
	metrictest.RequireInt64Sum(t, rm, "gestaltd.agent.operation.count", 1, map[string]string{
		"gestalt.agent.operation": "create_turn",
	})
	metrictest.RequireInt64Sum(t, rm, "gestaltd.agent.provider.operation.count", 1, map[string]string{
		"gestalt.agent.provider":  "managed",
		"gestalt.agent.operation": "create_turn",
	})
}

func TestAgentSessionsAndTurnsRoundTripWithoutAuth(t *testing.T) {
	t.Parallel()

	provider := newMemoryAgentProvider()
	ts := newTestServer(t, func(cfg *server.Config) {
		services := testutil.NewStubServices(t)
		cfg.Auth = nil
		cfg.Services = services
		cfg.AgentManager = agentmanager.New(agentmanager.Config{
			Agent:      &stubAgentControl{defaultProviderName: "managed", provider: provider},
			TurnScopes: newServerTestAgentTurnScopes(),
			ToolIDs:    newServerTestAgentToolIDs(t),
		})
	})
	testutil.CloseOnCleanup(t, ts)

	sessionReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/sessions/", bytes.NewBufferString(`{"provider":"managed","model":"gpt-5.4","clientRef":"cli-1"}`))
	sessionResp, err := http.DefaultClient.Do(sessionReq)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer func() { _ = sessionResp.Body.Close() }()
	if sessionResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(sessionResp.Body)
		t.Fatalf("create session status = %d body=%s", sessionResp.StatusCode, body)
	}
	var session map[string]any
	if err := json.NewDecoder(sessionResp.Body).Decode(&session); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	sessionID := session["id"].(string)

	turnReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/sessions/"+sessionID+"/turns", bytes.NewBufferString(`{"output":{"text":{}},"messages":[{"role":"user","text":"hello"}]}`))
	turnResp, err := http.DefaultClient.Do(turnReq)
	if err != nil {
		t.Fatalf("create turn: %v", err)
	}
	defer func() { _ = turnResp.Body.Close() }()
	if turnResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(turnResp.Body)
		t.Fatalf("create turn status = %d body=%s", turnResp.StatusCode, body)
	}
	var turn map[string]any
	if err := json.NewDecoder(turnResp.Body).Decode(&turn); err != nil {
		t.Fatalf("decode turn: %v", err)
	}
	if turn["sessionId"] != sessionID {
		t.Fatalf("turn sessionId = %#v, want %q", turn["sessionId"], sessionID)
	}
}

func TestAgentTurnEventStreamSendsHeartbeatBeforeEvents(t *testing.T) {
	t.Parallel()

	provider := newMemoryAgentProvider()
	ts := newTestServer(t, func(cfg *server.Config) {
		services := testutil.NewStubServices(t)
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "ada-session" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: "ada@example.com", DisplayName: "Ada"}, nil
			},
		}
		cfg.Services = services
		cfg.AgentManager = agentmanager.New(agentmanager.Config{
			Agent:      &stubAgentControl{defaultProviderName: "managed", provider: provider},
			TurnScopes: newServerTestAgentTurnScopes(),
			ToolIDs:    newServerTestAgentToolIDs(t),
		})
		cfg.APIRouteTimeout = 25 * time.Millisecond
		cfg.AgentStreamHeartbeat = time.Millisecond
	})
	testutil.CloseOnCleanup(t, ts)

	sessionReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/sessions/", bytes.NewBufferString(`{"provider":"managed","model":"gpt-5.4"}`))
	sessionReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	sessionResp, err := http.DefaultClient.Do(sessionReq)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer func() { _ = sessionResp.Body.Close() }()
	if sessionResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(sessionResp.Body)
		t.Fatalf("create session status = %d body=%s", sessionResp.StatusCode, body)
	}
	var session map[string]any
	if err := json.NewDecoder(sessionResp.Body).Decode(&session); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	sessionID := session["id"].(string)

	turnReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/sessions/"+sessionID+"/turns", bytes.NewBufferString(`{"output":{"text":{}},"messages":[{"role":"user","text":"wait"}]}`))
	turnReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	turnResp, err := http.DefaultClient.Do(turnReq)
	if err != nil {
		t.Fatalf("create turn: %v", err)
	}
	defer func() { _ = turnResp.Body.Close() }()
	if turnResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(turnResp.Body)
		t.Fatalf("create turn status = %d body=%s", turnResp.StatusCode, body)
	}
	var turn map[string]any
	if err := json.NewDecoder(turnResp.Body).Decode(&turn); err != nil {
		t.Fatalf("decode turn: %v", err)
	}
	turnID := turn["id"].(string)

	provider.mu.Lock()
	provider.turns[turnID].Status = coreagent.ExecutionStatusRunning
	provider.turns[turnID].CompletedAt = nil
	provider.turnEvents[turnID] = nil
	provider.mu.Unlock()

	streamCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	streamReq, _ := http.NewRequestWithContext(streamCtx, http.MethodGet, ts.URL+"/api/v1/agent/turns/"+turnID+"/events/stream?after=0&limit=10", nil)
	streamReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	streamResp, err := http.DefaultClient.Do(streamReq)
	if err != nil {
		t.Fatalf("stream quiet events: %v", err)
	}
	defer func() { _ = streamResp.Body.Close() }()
	if streamResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(streamResp.Body)
		t.Fatalf("stream quiet events status = %d body=%s", streamResp.StatusCode, body)
	}
	if got := streamResp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("stream quiet events content-type = %q, want text/event-stream", got)
	}
	reader := bufio.NewReader(streamResp.Body)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read heartbeat: %v", err)
	}
	if got := strings.TrimRight(line, "\r\n"); got != ": stream-open" {
		t.Fatalf("first stream line = %q, want stream-open heartbeat", got)
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read post-timeout heartbeat: %v", err)
		}
		if got := strings.TrimRight(line, "\r\n"); got == ": keepalive" {
			break
		}
	}

	provider.mu.Lock()
	provider.listTurnEventsErr = core.ErrNotFound
	provider.mu.Unlock()

	var errorFrame string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read stream error: %v", err)
		}
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data: ") {
			errorFrame = strings.TrimPrefix(line, "data: ")
			break
		}
	}
	if !strings.Contains(errorFrame, `"type":"stream.error"`) {
		t.Fatalf("stream error frame = %s, want stream.error event", errorFrame)
	}
}

func TestAgentTurnEventStreamReportsProviderContextErrorWhileRequestOpen(t *testing.T) {
	t.Parallel()

	provider := newMemoryAgentProvider()
	ts := newTestServer(t, func(cfg *server.Config) {
		services := testutil.NewStubServices(t)
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "ada-session" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: "ada@example.com", DisplayName: "Ada"}, nil
			},
		}
		cfg.Services = services
		cfg.AgentManager = agentmanager.New(agentmanager.Config{
			Agent:      &stubAgentControl{defaultProviderName: "managed", provider: provider},
			TurnScopes: newServerTestAgentTurnScopes(),
			ToolIDs:    newServerTestAgentToolIDs(t),
		})
	})
	testutil.CloseOnCleanup(t, ts)

	sessionReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/sessions/", bytes.NewBufferString(`{"provider":"managed","model":"gpt-5.4"}`))
	sessionReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	sessionResp, err := http.DefaultClient.Do(sessionReq)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer func() { _ = sessionResp.Body.Close() }()
	if sessionResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(sessionResp.Body)
		t.Fatalf("create session status = %d body=%s", sessionResp.StatusCode, body)
	}
	var session map[string]any
	if err := json.NewDecoder(sessionResp.Body).Decode(&session); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	sessionID := session["id"].(string)

	turnReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/sessions/"+sessionID+"/turns", bytes.NewBufferString(`{"output":{"text":{}},"messages":[{"role":"user","text":"wait"}]}`))
	turnReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	turnResp, err := http.DefaultClient.Do(turnReq)
	if err != nil {
		t.Fatalf("create turn: %v", err)
	}
	defer func() { _ = turnResp.Body.Close() }()
	if turnResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(turnResp.Body)
		t.Fatalf("create turn status = %d body=%s", turnResp.StatusCode, body)
	}
	var turn map[string]any
	if err := json.NewDecoder(turnResp.Body).Decode(&turn); err != nil {
		t.Fatalf("decode turn: %v", err)
	}
	turnID := turn["id"].(string)

	provider.mu.Lock()
	provider.turns[turnID].Status = coreagent.ExecutionStatusRunning
	provider.turns[turnID].CompletedAt = nil
	provider.turnEvents[turnID] = nil
	provider.mu.Unlock()

	streamCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	streamReq, _ := http.NewRequestWithContext(streamCtx, http.MethodGet, ts.URL+"/api/v1/agent/turns/"+turnID+"/events/stream?after=0&limit=10", nil)
	streamReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	streamResp, err := http.DefaultClient.Do(streamReq)
	if err != nil {
		t.Fatalf("stream quiet events: %v", err)
	}
	defer func() { _ = streamResp.Body.Close() }()
	if streamResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(streamResp.Body)
		t.Fatalf("stream quiet events status = %d body=%s", streamResp.StatusCode, body)
	}

	reader := bufio.NewReader(streamResp.Body)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read heartbeat: %v", err)
	}
	if got := strings.TrimRight(line, "\r\n"); got != ": stream-open" {
		t.Fatalf("first stream line = %q, want stream-open heartbeat", got)
	}

	provider.mu.Lock()
	provider.listTurnEventsErr = context.Canceled
	provider.mu.Unlock()

	var errorFrame string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read stream body: %v", err)
		}
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data: ") {
			errorFrame = strings.TrimPrefix(line, "data: ")
			break
		}
	}
	if streamCtx.Err() != nil {
		t.Fatal("stream error frame did not arrive before context deadline")
	}
	if !strings.Contains(errorFrame, `"type":"stream.error"`) {
		t.Fatalf("stream error frame = %s, want stream.error event", errorFrame)
	}
}

func TestAgentInteractionResolutionAndEventStream(t *testing.T) {
	t.Parallel()

	provider := newMemoryAgentProvider()
	ts := newTestServer(t, func(cfg *server.Config) {
		services := testutil.NewStubServices(t)
		cfg.Auth = &coretesting.StubAuthProvider{
			N: "stub",
			ValidateTokenFn: func(_ context.Context, token string) (*core.UserIdentity, error) {
				if token != "ada-session" {
					return nil, core.ErrNotFound
				}
				return &core.UserIdentity{Email: "ada@example.com", DisplayName: "Ada"}, nil
			},
		}
		cfg.Services = services
		cfg.AgentManager = agentmanager.New(agentmanager.Config{
			Agent:      &stubAgentControl{defaultProviderName: "managed", provider: provider},
			TurnScopes: newServerTestAgentTurnScopes(),
			ToolIDs:    newServerTestAgentToolIDs(t),
		})
	})
	testutil.CloseOnCleanup(t, ts)

	sessionReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/sessions/", bytes.NewBufferString(`{"provider":"managed","model":"gpt-5.4"}`))
	sessionReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	sessionResp, _ := http.DefaultClient.Do(sessionReq)
	defer func() { _ = sessionResp.Body.Close() }()
	var session map[string]any
	_ = json.NewDecoder(sessionResp.Body).Decode(&session)
	sessionID := session["id"].(string)

	turnReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/sessions/"+sessionID+"/turns", bytes.NewBufferString(`{"metadata":{"requireInteraction":true},"output":{"text":{}},"messages":[{"role":"user","text":"proceed"}]}`))
	turnReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	turnResp, _ := http.DefaultClient.Do(turnReq)
	defer func() { _ = turnResp.Body.Close() }()
	var turn map[string]any
	_ = json.NewDecoder(turnResp.Body).Decode(&turn)
	turnID := turn["id"].(string)

	blockedCtx, blockedCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer blockedCancel()
	blockedReq, _ := http.NewRequestWithContext(blockedCtx, http.MethodGet, ts.URL+"/api/v1/agent/turns/"+turnID+"/events/stream?after=0&limit=1&until=blocked_or_terminal", nil)
	blockedReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	blockedResp, err := http.DefaultClient.Do(blockedReq)
	if err != nil {
		t.Fatalf("stream blocked events: %v", err)
	}
	defer func() { _ = blockedResp.Body.Close() }()
	if blockedResp.StatusCode != http.StatusOK {
		t.Fatalf("blocked stream status = %d", blockedResp.StatusCode)
	}
	blockedReader := bufio.NewReader(blockedResp.Body)
	var blocked []string
	for {
		line, err := blockedReader.ReadString('\n')
		if err != nil {
			break
		}
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data: ") {
			blocked = append(blocked, strings.TrimPrefix(line, "data: "))
		}
	}
	if blockedCtx.Err() != nil {
		t.Fatalf("blocked stream did not close before context deadline")
	}
	if len(blocked) == 0 {
		t.Fatal("expected blocked stream events")
	}
	if !strings.Contains(strings.Join(blocked, "\n"), "interaction.requested") {
		t.Fatalf("blocked stream events = %#v, want interaction.requested", blocked)
	}
	var blockedEvent map[string]any
	for _, rawEvent := range blocked {
		var candidate map[string]any
		if err := json.Unmarshal([]byte(rawEvent), &candidate); err != nil {
			t.Fatalf("decode blocked event: %v", err)
		}
		if candidate["type"] == "interaction.requested" {
			blockedEvent = candidate
			break
		}
	}
	if blockedEvent == nil {
		t.Fatalf("blocked stream events = %#v, want interaction.requested object", blocked)
	}
	assertHTTPAgentEventDisplay(t, blockedEvent, "interaction", "requested", "interaction", "")
	if strings.Contains(strings.Join(blocked, "\n"), "turn.completed") {
		t.Fatalf("blocked stream events = %#v, did not expect turn.completed", blocked)
	}

	streamCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	streamReq, _ := http.NewRequestWithContext(streamCtx, http.MethodGet, ts.URL+"/api/v1/agent/turns/"+turnID+"/events/stream?after=1&limit=10", nil)
	streamReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	streamResp, err := http.DefaultClient.Do(streamReq)
	if err != nil {
		t.Fatalf("stream events: %v", err)
	}
	defer func() { _ = streamResp.Body.Close() }()

	interactionsReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/agent/turns/"+turnID+"/interactions", nil)
	interactionsReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	interactionsResp, _ := http.DefaultClient.Do(interactionsReq)
	defer func() { _ = interactionsResp.Body.Close() }()
	var interactions []map[string]any
	_ = json.NewDecoder(interactionsResp.Body).Decode(&interactions)
	if len(interactions) != 1 {
		t.Fatalf("interactions len = %d, want 1", len(interactions))
	}
	interactionID := interactions[0]["id"].(string)

	resolveReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/turns/"+turnID+"/interactions/"+interactionID+"/resolve", bytes.NewBufferString(`{"resolution":{"approved":true}}`))
	resolveReq.AddCookie(&http.Cookie{Name: "session_token", Value: "ada-session"})
	resolveResp, err := http.DefaultClient.Do(resolveReq)
	if err != nil {
		t.Fatalf("resolve interaction: %v", err)
	}
	defer func() { _ = resolveResp.Body.Close() }()
	if resolveResp.StatusCode != http.StatusOK {
		t.Fatalf("resolve interaction status = %d", resolveResp.StatusCode)
	}

	reader := bufio.NewReader(streamResp.Body)
	var streamed []string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data: ") {
			streamed = append(streamed, strings.TrimPrefix(line, "data: "))
		}
		if len(streamed) >= 3 {
			break
		}
	}
	if len(streamed) == 0 {
		t.Fatal("expected streamed turn events")
	}
	if !strings.Contains(streamed[len(streamed)-1], "turn.completed") {
		t.Fatalf("streamed events = %#v, want final turn.completed", streamed)
	}
}
