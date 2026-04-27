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
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/agentmanager"
	"github.com/valon-technologies/gestalt/server/internal/observability"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/internal/testutil/metrictest"
)

type stubAgentControl struct {
	defaultProviderName string
	provider            coreagent.Provider
}

func (s *stubAgentControl) ResolveProviderSelection(name string) (string, coreagent.Provider, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = strings.TrimSpace(s.defaultProviderName)
	}
	if name == "" || s.provider == nil {
		return "", nil, agentmanager.ErrAgentProviderRequired
	}
	return name, s.provider, nil
}

func (s *stubAgentControl) ResolveProvider(name string) (coreagent.Provider, error) {
	if strings.TrimSpace(name) == "" || s.provider == nil {
		return nil, agentmanager.NewAgentProviderNotAvailableError(name)
	}
	return s.provider, nil
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
	mu           sync.Mutex
	sessions     map[string]*coreagent.Session
	turns        map[string]*coreagent.Turn
	turnEvents   map[string][]*coreagent.TurnEvent
	interactions map[string]*coreagent.Interaction
	turnRequests []coreagent.CreateTurnRequest
}

func newMemoryAgentProvider() *memoryAgentProvider {
	return &memoryAgentProvider{
		sessions:     map[string]*coreagent.Session{},
		turns:        map[string]*coreagent.Turn{},
		turnEvents:   map[string][]*coreagent.TurnEvent{},
		interactions: map[string]*coreagent.Interaction{},
	}
}

func (p *memoryAgentProvider) CreateSession(_ context.Context, req coreagent.CreateSessionRequest) (*coreagent.Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now().UTC().Truncate(time.Second)
	session := &coreagent.Session{
		ID:           req.SessionID,
		ProviderName: "managed",
		Model:        req.Model,
		ClientRef:    req.ClientRef,
		State:        coreagent.SessionStateActive,
		Metadata:     cloneMap(req.Metadata),
		CreatedBy:    req.CreatedBy,
		CreatedAt:    &now,
		UpdatedAt:    &now,
	}
	p.sessions[session.ID] = session
	return cloneSession(session), nil
}

func (p *memoryAgentProvider) GetSession(_ context.Context, req coreagent.GetSessionRequest) (*coreagent.Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	session, ok := p.sessions[req.SessionID]
	if !ok {
		return nil, core.ErrNotFound
	}
	return cloneSession(session), nil
}

func (p *memoryAgentProvider) ListSessions(context.Context, coreagent.ListSessionsRequest) ([]*coreagent.Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	out := make([]*coreagent.Session, 0, len(p.sessions))
	for _, session := range p.sessions {
		out = append(out, cloneSession(session))
	}
	return out, nil
}

func (p *memoryAgentProvider) UpdateSession(_ context.Context, req coreagent.UpdateSessionRequest) (*coreagent.Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	session, ok := p.sessions[req.SessionID]
	if !ok {
		return nil, core.ErrNotFound
	}
	now := time.Now().UTC().Truncate(time.Second)
	if req.ClientRef != "" {
		session.ClientRef = req.ClientRef
	}
	if req.State != "" {
		session.State = req.State
	}
	if req.Metadata != nil {
		session.Metadata = cloneMap(req.Metadata)
	}
	session.UpdatedAt = &now
	return cloneSession(session), nil
}

func (p *memoryAgentProvider) CreateTurn(_ context.Context, req coreagent.CreateTurnRequest) (*coreagent.Turn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.turnRequests = append(p.turnRequests, cloneCreateTurnRequest(req))
	now := time.Now().UTC().Truncate(time.Second)
	turn := &coreagent.Turn{
		ID:           req.TurnID,
		SessionID:    req.SessionID,
		ProviderName: "managed",
		Model:        req.Model,
		Status:       coreagent.ExecutionStatusSucceeded,
		Messages:     append([]coreagent.Message(nil), req.Messages...),
		CreatedBy:    req.CreatedBy,
		CreatedAt:    &now,
		StartedAt:    &now,
		CompletedAt:  &now,
		ExecutionRef: req.ExecutionRef,
		OutputText:   "turn completed",
	}
	p.turns[turn.ID] = turn
	p.appendTurnEventLocked(turn.ID, "turn.started", map[string]any{"session_id": req.SessionID})
	if requireInteraction, _ := req.Metadata["requireInteraction"].(bool); requireInteraction {
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
		p.appendTurnEventLocked(turn.ID, "assistant.completed", map[string]any{"text": "turn completed"})
		p.appendTurnEventLocked(turn.ID, "turn.completed", map[string]any{"status": "succeeded"})
	}
	if session := p.sessions[req.SessionID]; session != nil {
		session.LastTurnAt = &now
		session.UpdatedAt = &now
	}
	return cloneTurn(turn), nil
}

func (p *memoryAgentProvider) capturedTurnRequests() []coreagent.CreateTurnRequest {
	p.mu.Lock()
	defer p.mu.Unlock()

	out := make([]coreagent.CreateTurnRequest, 0, len(p.turnRequests))
	for _, req := range p.turnRequests {
		out = append(out, cloneCreateTurnRequest(req))
	}
	return out
}

func (p *memoryAgentProvider) GetTurn(_ context.Context, req coreagent.GetTurnRequest) (*coreagent.Turn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	turn, ok := p.turns[req.TurnID]
	if !ok {
		return nil, core.ErrNotFound
	}
	return cloneTurn(turn), nil
}

func (p *memoryAgentProvider) ListTurns(_ context.Context, req coreagent.ListTurnsRequest) ([]*coreagent.Turn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	out := make([]*coreagent.Turn, 0, len(p.turns))
	for _, turn := range p.turns {
		if req.SessionID == "" || turn.SessionID == req.SessionID {
			out = append(out, cloneTurn(turn))
		}
	}
	return out, nil
}

func (p *memoryAgentProvider) CancelTurn(_ context.Context, req coreagent.CancelTurnRequest) (*coreagent.Turn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	turn, ok := p.turns[req.TurnID]
	if !ok {
		return nil, core.ErrNotFound
	}
	now := time.Now().UTC().Truncate(time.Second)
	turn.Status = coreagent.ExecutionStatusCanceled
	turn.StatusMessage = req.Reason
	turn.CompletedAt = &now
	p.appendTurnEventLocked(turn.ID, "turn.canceled", map[string]any{"reason": req.Reason})
	return cloneTurn(turn), nil
}

func (p *memoryAgentProvider) ListTurnEvents(_ context.Context, req coreagent.ListTurnEventsRequest) ([]*coreagent.TurnEvent, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	events := p.turnEvents[req.TurnID]
	out := make([]*coreagent.TurnEvent, 0, len(events))
	for _, event := range events {
		if event.Seq <= req.AfterSeq {
			continue
		}
		out = append(out, cloneTurnEvent(event))
		if req.Limit > 0 && len(out) >= req.Limit {
			break
		}
	}
	return out, nil
}

func (p *memoryAgentProvider) GetInteraction(_ context.Context, req coreagent.GetInteractionRequest) (*coreagent.Interaction, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	interaction, ok := p.interactions[req.InteractionID]
	if !ok {
		return nil, core.ErrNotFound
	}
	return cloneInteraction(interaction), nil
}

func (p *memoryAgentProvider) ListInteractions(_ context.Context, req coreagent.ListInteractionsRequest) ([]*coreagent.Interaction, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	out := make([]*coreagent.Interaction, 0, len(p.interactions))
	for _, interaction := range p.interactions {
		if req.TurnID == "" || interaction.TurnID == req.TurnID {
			out = append(out, cloneInteraction(interaction))
		}
	}
	return out, nil
}

func (p *memoryAgentProvider) ResolveInteraction(_ context.Context, req coreagent.ResolveInteractionRequest) (*coreagent.Interaction, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	interaction, ok := p.interactions[req.InteractionID]
	if !ok {
		return nil, core.ErrNotFound
	}
	now := time.Now().UTC().Truncate(time.Second)
	interaction.State = coreagent.InteractionStateResolved
	interaction.Resolution = cloneMap(req.Resolution)
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

func (p *memoryAgentProvider) GetCapabilities(context.Context, coreagent.GetCapabilitiesRequest) (*coreagent.ProviderCapabilities, error) {
	return &coreagent.ProviderCapabilities{
		StreamingText:    true,
		ToolCalls:        true,
		Interactions:     true,
		ResumableTurns:   true,
		StructuredOutput: true,
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
	dst.StructuredOutput = cloneMap(src.StructuredOutput)
	return &dst
}

func cloneCreateTurnRequest(src coreagent.CreateTurnRequest) coreagent.CreateTurnRequest {
	dst := src
	dst.Messages = append([]coreagent.Message(nil), src.Messages...)
	dst.Tools = append([]coreagent.Tool(nil), src.Tools...)
	dst.ResponseSchema = cloneMap(src.ResponseSchema)
	dst.Metadata = cloneMap(src.Metadata)
	dst.ProviderOptions = cloneMap(src.ProviderOptions)
	return dst
}

func cloneTurnEvent(src *coreagent.TurnEvent) *coreagent.TurnEvent {
	if src == nil {
		return nil
	}
	dst := *src
	dst.Data = cloneMap(src.Data)
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

func TestAgentSessionsAndTurnsRoundTrip(t *testing.T) {
	t.Parallel()

	provider := newMemoryAgentProvider()
	ts := newTestServer(t, func(cfg *server.Config) {
		services := coretesting.NewStubServices(t)
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
			Agent: &stubAgentControl{defaultProviderName: "managed", provider: provider},
			Providers: testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
				N:        "docs",
				ConnMode: core.ConnectionModeNone,
				CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
					ID:       "search",
					Title:    "Search",
					ReadOnly: true,
				}}},
			}),
			SessionMetadata: services.AgentSessions,
			RunMetadata:     services.AgentRunMetadata,
		})
	})
	testutil.CloseOnCleanup(t, ts)

	sessionReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/sessions", bytes.NewBufferString(`{"provider":"managed","model":"gpt-5.4","clientRef":"cli-1"}`))
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

	turnReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/sessions/"+sessionID+"/turns", bytes.NewBufferString(`{"messages":[{"role":"user","text":"hello"}]}`))
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
	if len(turnRequests[0].Tools) != 0 {
		t.Fatalf("provider turn tools len = %d, want 0", len(turnRequests[0].Tools))
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
	if len(turns) != 1 || turns[0]["id"] != turnID {
		t.Fatalf("turns = %#v, want %q", turns, turnID)
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
}

func TestAgentSessionAndTurnMetrics(t *testing.T) {
	t.Parallel()

	provider := observability.InstrumentAgentProvider("managed", newMemoryAgentProvider())
	metrics := metrictest.NewManualMeterProvider(t)
	services := coretesting.NewStubServices(t)

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
			Agent:           &stubAgentControl{defaultProviderName: "managed", provider: provider},
			Providers:       testutil.NewProviderRegistry(t),
			SessionMetadata: services.AgentSessions,
			RunMetadata:     services.AgentRunMetadata,
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

	turnReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/sessions/"+sessionID+"/turns", bytes.NewBufferString(`{"messages":[{"role":"user","text":"hello"}]}`))
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
	metrictest.RequireInt64Sum(t, rm, "gestaltd.agent.tool.resolve.count", 1, map[string]string{
		"gestalt.agent.tool.source": "explicit",
	})
	metrictest.RequireInt64Sum(t, rm, "gestaltd.agent.run_metadata.write.count", 1, map[string]string{
		"gestalt.agent.provider":  "managed",
		"gestalt.agent.operation": "create_turn",
	})
}

func TestAgentSessionsAndTurnsRoundTripWithoutAuth(t *testing.T) {
	t.Parallel()

	provider := newMemoryAgentProvider()
	ts := newTestServer(t, func(cfg *server.Config) {
		services := coretesting.NewStubServices(t)
		cfg.Auth = nil
		cfg.Services = services
		cfg.AgentManager = agentmanager.New(agentmanager.Config{
			Agent:           &stubAgentControl{defaultProviderName: "managed", provider: provider},
			SessionMetadata: services.AgentSessions,
			RunMetadata:     services.AgentRunMetadata,
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

	turnReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/sessions/"+sessionID+"/turns", bytes.NewBufferString(`{"messages":[{"role":"user","text":"hello"}]}`))
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

func TestAgentInteractionResolutionAndEventStream(t *testing.T) {
	t.Parallel()

	provider := newMemoryAgentProvider()
	ts := newTestServer(t, func(cfg *server.Config) {
		services := coretesting.NewStubServices(t)
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
			Agent:           &stubAgentControl{defaultProviderName: "managed", provider: provider},
			SessionMetadata: services.AgentSessions,
			RunMetadata:     services.AgentRunMetadata,
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

	turnReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/agent/sessions/"+sessionID+"/turns", bytes.NewBufferString(`{"metadata":{"requireInteraction":true},"messages":[{"role":"user","text":"proceed"}]}`))
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
