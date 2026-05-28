package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type agentProvider struct {
	gestalt.UnimplementedAgentProvider

	mu             sync.Mutex
	configuredName string
	sessions       map[string]*proto.AgentSession
	turns          map[string]*proto.AgentTurn
	turnEvents     map[string][]*proto.AgentTurnEvent
	interactions   map[string]*proto.AgentInteraction
}

func newAgentProvider() *agentProvider {
	return &agentProvider{
		sessions:     make(map[string]*proto.AgentSession),
		turns:        make(map[string]*proto.AgentTurn),
		turnEvents:   make(map[string][]*proto.AgentTurnEvent),
		interactions: make(map[string]*proto.AgentInteraction),
	}
}

func (p *agentProvider) Configure(_ context.Context, name string, _ map[string]any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.configuredName = strings.TrimSpace(name)
	return nil
}

func (p *agentProvider) CreateSession(_ context.Context, req *proto.CreateAgentProviderSessionRequest) (*proto.AgentSession, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	session := p.createOrUpdateSessionLocked(
		strings.TrimSpace(req.GetSessionId()),
		strings.TrimSpace(req.GetModel()),
		strings.TrimSpace(req.GetClientRef()),
		req.GetCreatedBy(),
		req.GetMetadata(),
	)
	return cloneSession(session), nil
}

func (p *agentProvider) GetSession(_ context.Context, req *proto.GetAgentProviderSessionRequest) (*proto.AgentSession, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	session, ok := p.sessions[strings.TrimSpace(req.GetSessionId())]
	if !ok {
		return nil, status.Error(codes.NotFound, "session not found")
	}
	return cloneSession(session), nil
}

func (p *agentProvider) ListSessions(context.Context, *proto.ListAgentProviderSessionsRequest) (*proto.ListAgentProviderSessionsResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return &proto.ListAgentProviderSessionsResponse{Sessions: sortedSessions(p.sessions)}, nil
}

func (p *agentProvider) UpdateSession(_ context.Context, req *proto.UpdateAgentProviderSessionRequest) (*proto.AgentSession, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	sessionID := strings.TrimSpace(req.GetSessionId())
	session, ok := p.sessions[sessionID]
	if !ok {
		return nil, status.Error(codes.NotFound, "session not found")
	}
	if clientRef := strings.TrimSpace(req.GetClientRef()); clientRef != "" {
		session.ClientRef = clientRef
	}
	if state := req.GetState(); state != proto.AgentSessionState_AGENT_SESSION_STATE_UNSPECIFIED {
		session.State = state
	}
	if req.GetMetadata() != nil {
		session.Metadata = cloneStruct(req.GetMetadata())
	}
	session.UpdatedAt = timestamppb.Now()
	return cloneSession(session), nil
}

func (p *agentProvider) CreateTurn(ctx context.Context, req *proto.CreateAgentProviderTurnRequest) (*proto.AgentTurn, error) {
	turn, _, err := p.startTurn(ctx, req)
	return turn, err
}

func (p *agentProvider) GetTurn(_ context.Context, req *proto.GetAgentProviderTurnRequest) (*proto.AgentTurn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	turn, ok := p.turns[strings.TrimSpace(req.GetTurnId())]
	if !ok {
		return nil, status.Error(codes.NotFound, "turn not found")
	}
	return cloneTurn(turn), nil
}

func (p *agentProvider) ListTurns(_ context.Context, req *proto.ListAgentProviderTurnsRequest) (*proto.ListAgentProviderTurnsResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	sessionID := strings.TrimSpace(req.GetSessionId())
	turns := make([]*proto.AgentTurn, 0, len(p.turns))
	for _, turn := range p.turns {
		if sessionID == "" || turn.GetSessionId() == sessionID {
			turns = append(turns, cloneTurn(turn))
		}
	}
	sort.Slice(turns, func(i, j int) bool { return turns[i].GetId() < turns[j].GetId() })
	return &proto.ListAgentProviderTurnsResponse{Turns: turns}, nil
}

func (p *agentProvider) CancelTurn(_ context.Context, req *proto.CancelAgentProviderTurnRequest) (*proto.AgentTurn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	turnID := strings.TrimSpace(req.GetTurnId())
	turn, ok := p.turns[turnID]
	if !ok {
		return nil, status.Error(codes.NotFound, "turn not found")
	}
	turn.Status = proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_CANCELED
	turn.StatusMessage = strings.TrimSpace(req.GetReason())
	turn.CompletedAt = timestamppb.Now()
	p.appendTurnEventLocked(turnID, "turn.canceled", map[string]any{
		"reason": turn.GetStatusMessage(),
	})
	return cloneTurn(turn), nil
}

func (p *agentProvider) ListTurnEvents(_ context.Context, req *proto.ListAgentProviderTurnEventsRequest) (*proto.ListAgentProviderTurnEventsResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	events := p.turnEvents[strings.TrimSpace(req.GetTurnId())]
	out := make([]*proto.AgentTurnEvent, 0, len(events))
	afterSeq := req.GetAfterSeq()
	limit := int(req.GetLimit())
	for _, event := range events {
		if event.GetSeq() <= afterSeq {
			continue
		}
		out = append(out, cloneTurnEvent(event))
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return &proto.ListAgentProviderTurnEventsResponse{Events: out}, nil
}

func (p *agentProvider) GetInteraction(_ context.Context, req *proto.GetAgentProviderInteractionRequest) (*proto.AgentInteraction, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	interaction, ok := p.interactions[strings.TrimSpace(req.GetInteractionId())]
	if !ok {
		return nil, status.Error(codes.NotFound, "interaction not found")
	}
	return cloneInteraction(interaction), nil
}

func (p *agentProvider) ListInteractions(_ context.Context, req *proto.ListAgentProviderInteractionsRequest) (*proto.ListAgentProviderInteractionsResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	turnID := strings.TrimSpace(req.GetTurnId())
	interactions := make([]*proto.AgentInteraction, 0, len(p.interactions))
	for _, interaction := range p.interactions {
		if turnID == "" || interaction.GetTurnId() == turnID {
			interactions = append(interactions, cloneInteraction(interaction))
		}
	}
	sort.Slice(interactions, func(i, j int) bool { return interactions[i].GetId() < interactions[j].GetId() })
	return &proto.ListAgentProviderInteractionsResponse{Interactions: interactions}, nil
}

func (p *agentProvider) ResolveInteraction(_ context.Context, req *proto.ResolveAgentProviderInteractionRequest) (*proto.AgentInteraction, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	interactionID := strings.TrimSpace(req.GetInteractionId())
	interaction, ok := p.interactions[interactionID]
	if !ok {
		return nil, status.Error(codes.NotFound, "interaction not found")
	}
	if interaction.GetState() != proto.AgentInteractionState_AGENT_INTERACTION_STATE_PENDING {
		return nil, status.Error(codes.FailedPrecondition, "interaction is not pending")
	}
	now := timestamppb.Now()
	interaction.State = proto.AgentInteractionState_AGENT_INTERACTION_STATE_RESOLVED
	interaction.Resolution = cloneStruct(req.GetResolution())
	interaction.ResolvedAt = now
	turn, ok := p.turns[interaction.GetTurnId()]
	if !ok {
		return nil, status.Error(codes.NotFound, "turn not found")
	}
	turn.Status = proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_SUCCEEDED
	turn.StatusMessage = interactionID
	turn.CompletedAt = now
	p.appendTurnEventLocked(interaction.GetTurnId(), "interaction.resolved", map[string]any{
		"interaction_id": interactionID,
	})
	p.appendTurnEventLocked(interaction.GetTurnId(), "assistant.completed", map[string]any{
		"interaction_id": interactionID,
	})
	p.appendTurnEventLocked(interaction.GetTurnId(), "turn.completed", map[string]any{
		"interaction_id": interactionID,
	})
	return cloneInteraction(interaction), nil
}

func (p *agentProvider) GetCapabilities(context.Context, *proto.GetAgentProviderCapabilitiesRequest) (*proto.AgentProviderCapabilities, error) {
	return &proto.AgentProviderCapabilities{
		StreamingText:      true,
		ToolCalls:          true,
		ParallelToolCalls:  true,
		Interactions:       true,
		ResumableTurns:     true,
		ReasoningSummaries: false,
	}, nil
}

func (p *agentProvider) startTurn(ctx context.Context, req *proto.CreateAgentProviderTurnRequest) (*proto.AgentTurn, *proto.AgentInteraction, error) {
	turnID := strings.TrimSpace(req.GetTurnId())
	if turnID == "" {
		turnID = "agent-turn-1"
	}
	sessionID := strings.TrimSpace(req.GetSessionId())
	if sessionID == "" {
		sessionID = "session-" + turnID
	}
	model := strings.TrimSpace(req.GetModel())
	toolID := firstAgentToolID(req.GetTools())
	metadata := structMap(req.GetMetadata())
	executionRef := strings.TrimSpace(req.GetExecutionRef())
	runGrant := strings.TrimSpace(req.GetRunGrant())

	providerName := p.providerName()
	output := map[string]any{
		"provider_name": providerName,
	}

	requireInteraction := false
	if metadata != nil {
		requireInteraction, _ = metadata["requireInteraction"].(bool)
	}

	now := timestamppb.Now()
	p.mu.Lock()
	session := p.createOrUpdateSessionLocked(sessionID, model, "", req.GetCreatedBy(), nil)
	session.LastTurnAt = now
	session.UpdatedAt = now
	turn := &proto.AgentTurn{
		Id:           turnID,
		SessionId:    sessionID,
		ProviderName: providerName,
		Model:        model,
		Status:       proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_RUNNING,
		Messages:     cloneMessages(req.GetMessages()),
		CreatedBy:    cloneActor(req.GetCreatedBy()),
		CreatedAt:    now,
		StartedAt:    now,
		ExecutionRef: executionRef,
	}
	p.turns[turnID] = turn
	p.appendTurnEventLocked(turnID, "turn.started", map[string]any{
		"session_id": sessionID,
	})
	p.mu.Unlock()

	if toolID != "" {
		host, err := gestalt.AgentHost()
		if err != nil {
			output["host_error"] = err.Error()
		} else {
			defer func() {
				if closeErr := host.Close(); closeErr != nil && output["host_error"] == nil {
					output["host_error"] = closeErr.Error()
				}
			}()

			arguments, err := structpb.NewStruct(map[string]any{"taskId": "task-123"})
			if err != nil {
				output["tool_error"] = err.Error()
			} else if resp, err := host.ExecuteTool(ctx, &proto.ExecuteAgentToolRequest{
				SessionId:      sessionID,
				TurnId:         turnID,
				ToolCallId:     "call-1",
				ToolId:         toolID,
				Arguments:      arguments,
				RunGrant:       runGrant,
				IdempotencyKey: " tool-call-key-1 ",
			}); err != nil {
				output["tool_error"] = err.Error()
			} else {
				output["tool_status"] = resp.GetStatus()
				output["tool_body"] = resp.GetBody()
			}
			output["event_emitted"] = true
		}
	}

	var interactionRequest map[string]any
	if requireInteraction {
		output["interaction_requested"] = true
		output["interaction_id"] = "interaction-" + turnID
		interactionRequest = map[string]any{"provider_name": providerName}
	}

	if !requireInteraction && len(req.GetMessages()) > 0 && output["tool_status"] == nil {
		last := req.GetMessages()[len(req.GetMessages())-1]
		if text := strings.TrimSpace(last.GetText()); text != "" {
			output["echo"] = text
		}
	}

	body, err := json.Marshal(output)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal output: %w", err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	session = p.createOrUpdateSessionLocked(sessionID, model, "", req.GetCreatedBy(), nil)
	session.LastTurnAt = now
	session.UpdatedAt = now

	turn = p.turns[turnID]
	if turn == nil {
		turn = &proto.AgentTurn{
			Id:           turnID,
			SessionId:    sessionID,
			ProviderName: providerName,
			Model:        model,
			Messages:     cloneMessages(req.GetMessages()),
			CreatedBy:    cloneActor(req.GetCreatedBy()),
			CreatedAt:    now,
			StartedAt:    now,
			ExecutionRef: executionRef,
		}
	}
	if req.GetOutput().GetStructured() != nil {
		turn.Output = &proto.AgentTurn_Structured{
			Structured: &proto.AgentTurnStructuredOutput{
				Text:  string(body),
				Value: structFromMap(output),
			},
		}
	} else {
		turn.Output = &proto.AgentTurn_Text{Text: &proto.AgentTurnTextOutput{Text: string(body)}}
	}
	turn.Status = proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_SUCCEEDED
	if requireInteraction {
		turn.Status = proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_WAITING_FOR_INPUT
		turn.StatusMessage = "waiting for input"
	} else {
		turn.CompletedAt = now
	}

	p.turns[turnID] = turn
	if output["event_emitted"] == true {
		p.appendTurnEventLocked(turnID, "agent.test", map[string]any{"provider_name": providerName})
	}

	var interaction *proto.AgentInteraction
	if requireInteraction {
		interaction = &proto.AgentInteraction{
			Id:        "interaction-" + turnID,
			TurnId:    turnID,
			SessionId: sessionID,
			Type:      proto.AgentInteractionType_AGENT_INTERACTION_TYPE_APPROVAL,
			State:     proto.AgentInteractionState_AGENT_INTERACTION_STATE_PENDING,
			Title:     "Approve action",
			Prompt:    "Continue the agent turn?",
			Request:   structFromMap(interactionRequest),
			CreatedAt: now,
		}
		p.interactions[interaction.GetId()] = cloneInteraction(interaction)
		p.appendTurnEventLocked(turnID, "interaction.requested", map[string]any{
			"interaction_id": interaction.GetId(),
			"session_id":     sessionID,
		})
	} else {
		p.appendTurnEventLocked(turnID, "assistant.completed", map[string]any{
			"session_id": sessionID,
		})
		p.appendTurnEventLocked(turnID, "turn.completed", map[string]any{
			"session_id": sessionID,
		})
	}

	return cloneTurn(turn), cloneInteraction(interaction), nil
}

func (p *agentProvider) createOrUpdateSessionLocked(
	sessionID string,
	model string,
	clientRef string,
	createdBy *proto.AgentActor,
	metadata *structpb.Struct,
) *proto.AgentSession {
	if sessionID == "" {
		sessionID = "agent-session-1"
	}
	if existing, ok := p.sessions[sessionID]; ok {
		if model != "" {
			existing.Model = model
		}
		if clientRef != "" {
			existing.ClientRef = clientRef
		}
		if metadata != nil {
			existing.Metadata = cloneStruct(metadata)
		}
		existing.UpdatedAt = timestamppb.Now()
		return existing
	}
	now := timestamppb.Now()
	session := &proto.AgentSession{
		Id:           sessionID,
		ProviderName: p.providerNameLocked(""),
		Model:        model,
		ClientRef:    clientRef,
		State:        proto.AgentSessionState_AGENT_SESSION_STATE_ACTIVE,
		Metadata:     cloneStruct(metadata),
		CreatedBy:    cloneActor(createdBy),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	p.sessions[sessionID] = session
	return session
}

func (p *agentProvider) appendTurnEventLocked(turnID, eventType string, data map[string]any) {
	events := p.turnEvents[turnID]
	event := &proto.AgentTurnEvent{
		Id:         fmt.Sprintf("%s-event-%d", turnID, len(events)+1),
		TurnId:     turnID,
		Seq:        int64(len(events) + 1),
		Type:       eventType,
		Source:     p.providerNameLocked(""),
		Visibility: "private",
		Data:       structFromMap(data),
		Display:    turnEventDisplay(eventType, data),
		CreatedAt:  timestamppb.Now(),
	}
	p.turnEvents[turnID] = append(events, event)
}

func turnEventDisplay(eventType string, data map[string]any) *proto.AgentTurnDisplay {
	switch eventType {
	case "turn.started":
		return &proto.AgentTurnDisplay{
			Kind:  "status",
			Phase: "started",
			Label: "turn",
			Text:  "provider turn started",
		}
	case "agent.test":
		return &proto.AgentTurnDisplay{
			Kind:  "status",
			Phase: "completed",
			Label: "provider event",
			Text:  displayString(data, "provider_name"),
		}
	case "interaction.requested":
		return &proto.AgentTurnDisplay{
			Kind:  "interaction",
			Phase: "requested",
			Label: "approval",
			Ref:   displayString(data, "interaction_id"),
			Input: valueFromAny(data),
		}
	case "interaction.resolved":
		return &proto.AgentTurnDisplay{
			Kind:   "interaction",
			Phase:  "resolved",
			Label:  "approval",
			Ref:    displayString(data, "interaction_id"),
			Output: valueFromAny(data),
		}
	case "assistant.completed":
		return &proto.AgentTurnDisplay{
			Kind:   "text",
			Phase:  "completed",
			Text:   "provider assistant completed",
			Format: "markdown",
		}
	case "turn.completed":
		return &proto.AgentTurnDisplay{
			Kind:   "status",
			Phase:  "completed",
			Label:  "turn",
			Text:   "provider turn completed",
			Output: valueFromAny(data),
		}
	case "turn.canceled":
		return &proto.AgentTurnDisplay{
			Kind:  "status",
			Phase: "canceled",
			Label: "turn",
			Text:  displayString(data, "reason"),
			Error: valueFromAny(data),
		}
	default:
		return nil
	}
}

func displayString(data map[string]any, key string) string {
	if data == nil {
		return ""
	}
	value, _ := data[key].(string)
	return strings.TrimSpace(value)
}

func (p *agentProvider) providerName() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.providerNameLocked("")
}

func (p *agentProvider) providerNameLocked(fallback string) string {
	if name := strings.TrimSpace(p.configuredName); name != "" {
		return name
	}
	if name := strings.TrimSpace(fallback); name != "" {
		return name
	}
	return "agent-provider"
}

func sortedSessions(input map[string]*proto.AgentSession) []*proto.AgentSession {
	ids := make([]string, 0, len(input))
	for id := range input {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]*proto.AgentSession, 0, len(ids))
	for _, id := range ids {
		out = append(out, cloneSession(input[id]))
	}
	return out
}

func structMap(input *structpb.Struct) map[string]any {
	if input == nil {
		return nil
	}
	return input.AsMap()
}

func structFromMap(input map[string]any) *structpb.Struct {
	if len(input) == 0 {
		return nil
	}
	out, err := structpb.NewStruct(input)
	if err != nil {
		panic(err)
	}
	return out
}

func valueFromAny(input any) *structpb.Value {
	if input == nil {
		return nil
	}
	out, err := structpb.NewValue(input)
	if err != nil {
		panic(err)
	}
	return out
}

func firstAgentToolID(input []*proto.ResolvedAgentTool) string {
	for _, tool := range input {
		if id := strings.TrimSpace(tool.GetId()); id != "" {
			return id
		}
	}
	return ""
}

func cloneSession(input *proto.AgentSession) *proto.AgentSession {
	if input == nil {
		return nil
	}
	return gproto.Clone(input).(*proto.AgentSession)
}

func cloneTurn(input *proto.AgentTurn) *proto.AgentTurn {
	if input == nil {
		return nil
	}
	return gproto.Clone(input).(*proto.AgentTurn)
}

func cloneTurnEvent(input *proto.AgentTurnEvent) *proto.AgentTurnEvent {
	if input == nil {
		return nil
	}
	return gproto.Clone(input).(*proto.AgentTurnEvent)
}

func cloneInteraction(input *proto.AgentInteraction) *proto.AgentInteraction {
	if input == nil {
		return nil
	}
	return gproto.Clone(input).(*proto.AgentInteraction)
}

func cloneStruct(input *structpb.Struct) *structpb.Struct {
	if input == nil {
		return nil
	}
	return gproto.Clone(input).(*structpb.Struct)
}

func cloneActor(input *proto.AgentActor) *proto.AgentActor {
	if input == nil {
		return nil
	}
	return gproto.Clone(input).(*proto.AgentActor)
}

func cloneMessages(input []*proto.AgentMessage) []*proto.AgentMessage {
	if len(input) == 0 {
		return nil
	}
	out := make([]*proto.AgentMessage, 0, len(input))
	for _, message := range input {
		if message != nil {
			out = append(out, gproto.Clone(message).(*proto.AgentMessage))
		}
	}
	return out
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := gestalt.ServeAgentProvider(ctx, newAgentProvider()); err != nil {
		panic(err)
	}
}
