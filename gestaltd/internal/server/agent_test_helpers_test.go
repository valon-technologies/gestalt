package server_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	appaccessservice "github.com/valon-technologies/gestalt/server/services/appaccess"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

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
		ID:                 fmt.Sprintf("managed-session-%d", len(p.sessions)+1),
		ProviderName:       "managed",
		Model:              req.GetModel(),
		ClientRef:          req.GetClientRef(),
		State:              coreagent.SessionStateActive,
		Metadata:           mapFromStruct(req.GetMetadata()),
		CreatedBySubjectID: appaccessservice.SubjectIDFromRequestContext(req.GetContext()),
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
		CreatedBySubjectID: appaccessservice.SubjectIDFromRequestContext(req.GetContext()),
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

func (p *memoryAgentProvider) Close() error { return nil }

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
