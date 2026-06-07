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
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type agentProvider struct {
	gestalt.UnimplementedAgentProvider

	mu             sync.Mutex
	configuredName string
	sessions       map[string]*gestalt.AgentSession
	turns          map[string]*gestalt.AgentTurn
	turnEvents     map[string][]*gestalt.AgentTurnEvent
	interactions   map[string]*gestalt.AgentInteraction
}

func newAgentProvider() *agentProvider {
	return &agentProvider{
		sessions:     make(map[string]*gestalt.AgentSession),
		turns:        make(map[string]*gestalt.AgentTurn),
		turnEvents:   make(map[string][]*gestalt.AgentTurnEvent),
		interactions: make(map[string]*gestalt.AgentInteraction),
	}
}

func (p *agentProvider) Configure(_ context.Context, name string, _ map[string]any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.configuredName = strings.TrimSpace(name)
	return nil
}

func (p *agentProvider) CreateSession(_ context.Context, req *gestalt.CreateAgentProviderSessionRequest) (*gestalt.AgentSession, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	session := p.createOrUpdateSessionLocked(
		strings.TrimSpace(req.SessionID),
		strings.TrimSpace(req.Model),
		strings.TrimSpace(req.ClientRef),
		req.CreatedBySubjectID,
		req.Metadata,
	)
	return cloneSession(session), nil
}

func (p *agentProvider) GetSession(_ context.Context, req *gestalt.GetAgentProviderSessionRequest) (*gestalt.AgentSession, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	session, ok := p.sessions[strings.TrimSpace(req.SessionID)]
	if !ok {
		return nil, status.Error(codes.NotFound, "session not found")
	}
	return cloneSession(session), nil
}

func (p *agentProvider) ListSessions(context.Context, *gestalt.ListAgentProviderSessionsRequest) (*gestalt.ListAgentProviderSessionsResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return &gestalt.ListAgentProviderSessionsResponse{Sessions: sortedSessions(p.sessions)}, nil
}

func (p *agentProvider) UpdateSession(_ context.Context, req *gestalt.UpdateAgentProviderSessionRequest) (*gestalt.AgentSession, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	sessionID := strings.TrimSpace(req.SessionID)
	session, ok := p.sessions[sessionID]
	if !ok {
		return nil, status.Error(codes.NotFound, "session not found")
	}
	if clientRef := strings.TrimSpace(req.ClientRef); clientRef != "" {
		session.ClientRef = clientRef
	}
	if state := req.State; state != gestalt.AgentSessionStateUnspecified {
		session.State = state
	}
	if req.Metadata != nil {
		session.Metadata = cloneMap(req.Metadata)
	}
	session.UpdatedAt = time.Now()
	return cloneSession(session), nil
}

func (p *agentProvider) CreateTurn(ctx context.Context, req *gestalt.CreateAgentProviderTurnRequest) (*gestalt.AgentTurn, error) {
	turn, _, err := p.startTurn(
		ctx,
		strings.TrimSpace(req.TurnID),
		strings.TrimSpace(req.SessionID),
		strings.TrimSpace(req.Model),
		req.Messages,
		req.Tools,
		req.Metadata,
		req.Output,
		req.CreatedBySubjectID,
		strings.TrimSpace(req.ExecutionRef),
	)
	return turn, err
}

func (p *agentProvider) GetTurn(_ context.Context, req *gestalt.GetAgentProviderTurnRequest) (*gestalt.AgentTurn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	turn, ok := p.turns[strings.TrimSpace(req.TurnID)]
	if !ok {
		return nil, status.Error(codes.NotFound, "turn not found")
	}
	return cloneTurn(turn), nil
}

func (p *agentProvider) ListTurns(_ context.Context, req *gestalt.ListAgentProviderTurnsRequest) (*gestalt.ListAgentProviderTurnsResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	sessionID := strings.TrimSpace(req.SessionID)
	turns := make([]gestalt.AgentTurn, 0, len(p.turns))
	for _, turn := range p.turns {
		if sessionID == "" || turn.SessionID == sessionID {
			if cloned := cloneTurn(turn); cloned != nil {
				turns = append(turns, *cloned)
			}
		}
	}
	sort.Slice(turns, func(i, j int) bool { return turns[i].ID < turns[j].ID })
	return &gestalt.ListAgentProviderTurnsResponse{Turns: turns}, nil
}

func (p *agentProvider) CancelTurn(_ context.Context, req *gestalt.CancelAgentProviderTurnRequest) (*gestalt.AgentTurn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	turnID := strings.TrimSpace(req.TurnID)
	turn, ok := p.turns[turnID]
	if !ok {
		return nil, status.Error(codes.NotFound, "turn not found")
	}
	now := time.Now()
	turn.Status = gestalt.AgentExecutionStatusCanceled
	turn.StatusMessage = strings.TrimSpace(req.Reason)
	turn.CompletedAt = &now
	p.appendTurnEventLocked(turnID, "turn.canceled", map[string]any{
		"reason": turn.StatusMessage,
	})
	return cloneTurn(turn), nil
}

func (p *agentProvider) ListTurnEvents(_ context.Context, req *gestalt.ListAgentProviderTurnEventsRequest) (*gestalt.ListAgentProviderTurnEventsResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	events := p.turnEvents[strings.TrimSpace(req.TurnID)]
	out := make([]gestalt.AgentTurnEvent, 0, len(events))
	afterSeq := req.AfterSeq
	limit := int(req.Limit)
	for _, event := range events {
		if event.Seq <= afterSeq {
			continue
		}
		if cloned := cloneTurnEvent(event); cloned != nil {
			out = append(out, *cloned)
		}
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return &gestalt.ListAgentProviderTurnEventsResponse{Events: out}, nil
}

func (p *agentProvider) GetInteraction(_ context.Context, req *gestalt.GetAgentProviderInteractionRequest) (*gestalt.AgentInteraction, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	interaction, ok := p.interactions[strings.TrimSpace(req.InteractionID)]
	if !ok {
		return nil, status.Error(codes.NotFound, "interaction not found")
	}
	return cloneInteraction(interaction), nil
}

func (p *agentProvider) ListInteractions(_ context.Context, req *gestalt.ListAgentProviderInteractionsRequest) (*gestalt.ListAgentProviderInteractionsResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	turnID := strings.TrimSpace(req.TurnID)
	interactions := make([]gestalt.AgentInteraction, 0, len(p.interactions))
	for _, interaction := range p.interactions {
		if turnID == "" || interaction.TurnID == turnID {
			if cloned := cloneInteraction(interaction); cloned != nil {
				interactions = append(interactions, *cloned)
			}
		}
	}
	sort.Slice(interactions, func(i, j int) bool { return interactions[i].ID < interactions[j].ID })
	return &gestalt.ListAgentProviderInteractionsResponse{Interactions: interactions}, nil
}

func (p *agentProvider) ResolveInteraction(_ context.Context, req *gestalt.ResolveAgentProviderInteractionRequest) (*gestalt.AgentInteraction, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	interactionID := strings.TrimSpace(req.InteractionID)
	interaction, ok := p.interactions[interactionID]
	if !ok {
		return nil, status.Error(codes.NotFound, "interaction not found")
	}
	if interaction.State != gestalt.AgentInteractionStatePending {
		return nil, status.Error(codes.FailedPrecondition, "interaction is not pending")
	}
	now := time.Now()
	interaction.State = gestalt.AgentInteractionStateResolved
	interaction.Resolution = cloneMap(req.Resolution)
	interaction.ResolvedAt = &now
	turn, ok := p.turns[interaction.TurnID]
	if !ok {
		return nil, status.Error(codes.NotFound, "turn not found")
	}
	turn.Status = gestalt.AgentExecutionStatusSucceeded
	turn.StatusMessage = interactionID
	turn.CompletedAt = &now
	p.appendTurnEventLocked(interaction.TurnID, "interaction.resolved", map[string]any{
		"interaction_id": interactionID,
	})
	p.appendTurnEventLocked(interaction.TurnID, "assistant.completed", map[string]any{
		"interaction_id": interactionID,
	})
	p.appendTurnEventLocked(interaction.TurnID, "turn.completed", map[string]any{
		"interaction_id": interactionID,
	})
	return cloneInteraction(interaction), nil
}

func (p *agentProvider) GetCapabilities(context.Context, *gestalt.GetAgentProviderCapabilitiesRequest) (*gestalt.AgentProviderCapabilities, error) {
	return &gestalt.AgentProviderCapabilities{
		StreamingText:      true,
		ToolCalls:          true,
		ParallelToolCalls:  true,
		Interactions:       true,
		ResumableTurns:     true,
		ReasoningSummaries: false,
	}, nil
}

func (p *agentProvider) startTurn(
	ctx context.Context,
	turnID string,
	sessionID string,
	model string,
	messages []gestalt.AgentMessage,
	tools []gestalt.ResolvedAgentTool,
	metadata map[string]any,
	requestedOutput *gestalt.AgentOutput,
	createdBySubjectID string,
	executionRef string,
) (*gestalt.AgentTurn, *gestalt.AgentInteraction, error) {
	if turnID == "" {
		turnID = "agent-turn-1"
	}
	if sessionID == "" {
		sessionID = "session-" + turnID
	}

	providerName := p.providerName()
	output := map[string]any{
		"provider_name": providerName,
	}

	requireInteraction := false
	if metadata != nil {
		requireInteraction, _ = metadata["requireInteraction"].(bool)
	}

	now := time.Now()
	p.mu.Lock()
	session := p.createOrUpdateSessionLocked(sessionID, model, "", createdBySubjectID, nil)
	session.LastTurnAt = &now
	session.UpdatedAt = now
	turn := &gestalt.AgentTurn{
		ID:                 turnID,
		SessionID:          sessionID,
		ProviderName:       providerName,
		Model:              model,
		Status:             gestalt.AgentExecutionStatusRunning,
		Messages:           cloneMessages(messages),
		CreatedBySubjectID: strings.TrimSpace(createdBySubjectID),
		CreatedAt:          now,
		StartedAt:          &now,
		ExecutionRef:       executionRef,
	}
	p.turns[turnID] = turn
	p.appendTurnEventLocked(turnID, "turn.started", map[string]any{
		"session_id": sessionID,
	})
	p.mu.Unlock()

	if len(tools) > 0 {
		app, err := gestalt.AppFromContext(ctx)
		if err != nil {
			output["app_error"] = err.Error()
		} else {
			resp, err := app.Invoke(ctx, "roadmap", "sync", map[string]any{"taskId": "task-123"}, &gestalt.InvokeOptions{
				IdempotencyKey: " tool-call-key-1 ",
			})
			if err != nil {
				output["tool_error"] = err.Error()
			} else {
				output["tool_status"] = resp.Status
				output["tool_body"] = resp.Body
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

	if !requireInteraction && len(messages) > 0 && output["tool_status"] == nil {
		last := messages[len(messages)-1]
		if text := strings.TrimSpace(last.Text); text != "" {
			output["echo"] = text
		}
	}

	body, err := json.Marshal(output)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal output: %w", err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	session = p.createOrUpdateSessionLocked(sessionID, model, "", createdBySubjectID, nil)
	session.LastTurnAt = &now
	session.UpdatedAt = now

	turn = p.turns[turnID]
	if turn == nil {
		turn = &gestalt.AgentTurn{
			ID:                 turnID,
			SessionID:          sessionID,
			ProviderName:       providerName,
			Model:              model,
			Messages:           cloneMessages(messages),
			CreatedBySubjectID: strings.TrimSpace(createdBySubjectID),
			CreatedAt:          now,
			StartedAt:          &now,
			ExecutionRef:       executionRef,
		}
	}
	if requestedOutput != nil && requestedOutput.Structured != nil {
		turn.Output = &gestalt.AgentTurnOutput{
			Structured: &gestalt.AgentTurnStructuredOutput{
				Text:  string(body),
				Value: output,
			},
		}
	} else {
		turn.Output = &gestalt.AgentTurnOutput{Text: &gestalt.AgentTurnTextOutput{Text: string(body)}}
	}
	turn.Status = gestalt.AgentExecutionStatusSucceeded
	if requireInteraction {
		turn.Status = gestalt.AgentExecutionStatusWaitingForInput
		turn.StatusMessage = "waiting for input"
	} else {
		turn.CompletedAt = &now
	}

	p.turns[turnID] = turn
	if output["event_emitted"] == true {
		p.appendTurnEventLocked(turnID, "agent.test", map[string]any{"provider_name": providerName})
	}

	var interaction *gestalt.AgentInteraction
	if requireInteraction {
		interaction = &gestalt.AgentInteraction{
			ID:        "interaction-" + turnID,
			TurnID:    turnID,
			SessionID: sessionID,
			Type:      gestalt.AgentInteractionTypeApproval,
			State:     gestalt.AgentInteractionStatePending,
			Title:     "Approve action",
			Prompt:    "Continue the agent turn?",
			Request:   cloneMap(interactionRequest),
			CreatedAt: now,
		}
		p.interactions[interaction.ID] = cloneInteraction(interaction)
		p.appendTurnEventLocked(turnID, "interaction.requested", map[string]any{
			"interaction_id": interaction.ID,
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
	createdBySubjectID string,
	metadata map[string]any,
) *gestalt.AgentSession {
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
			existing.Metadata = cloneMap(metadata)
		}
		existing.UpdatedAt = time.Now()
		return existing
	}
	now := time.Now()
	session := &gestalt.AgentSession{
		ID:                 sessionID,
		ProviderName:       p.providerNameLocked(""),
		Model:              model,
		ClientRef:          clientRef,
		State:              gestalt.AgentSessionStateActive,
		Metadata:           cloneMap(metadata),
		CreatedBySubjectID: strings.TrimSpace(createdBySubjectID),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	p.sessions[sessionID] = session
	return session
}

func (p *agentProvider) appendTurnEventLocked(turnID, eventType string, data map[string]any) {
	events := p.turnEvents[turnID]
	event := &gestalt.AgentTurnEvent{
		ID:         fmt.Sprintf("%s-event-%d", turnID, len(events)+1),
		TurnID:     turnID,
		Seq:        int64(len(events) + 1),
		Type:       eventType,
		Source:     p.providerNameLocked(""),
		Visibility: "private",
		Data:       cloneMap(data),
		Display:    turnEventDisplay(eventType, data),
		CreatedAt:  time.Now(),
	}
	p.turnEvents[turnID] = append(events, event)
}

func turnEventDisplay(eventType string, data map[string]any) *gestalt.AgentTurnDisplay {
	switch eventType {
	case "turn.started":
		return &gestalt.AgentTurnDisplay{
			Kind:  "status",
			Phase: "started",
			Label: "turn",
			Text:  "provider turn started",
		}
	case "agent.test":
		return &gestalt.AgentTurnDisplay{
			Kind:  "status",
			Phase: "completed",
			Label: "provider event",
			Text:  displayString(data, "provider_name"),
		}
	case "interaction.requested":
		return &gestalt.AgentTurnDisplay{
			Kind:  "interaction",
			Phase: "requested",
			Label: "approval",
			Ref:   displayString(data, "interaction_id"),
			Input: displayValue(data),
		}
	case "interaction.resolved":
		return &gestalt.AgentTurnDisplay{
			Kind:   "interaction",
			Phase:  "resolved",
			Label:  "approval",
			Ref:    displayString(data, "interaction_id"),
			Output: displayValue(data),
		}
	case "assistant.completed":
		return &gestalt.AgentTurnDisplay{
			Kind:   "text",
			Phase:  "completed",
			Text:   "provider assistant completed",
			Format: "markdown",
		}
	case "turn.completed":
		return &gestalt.AgentTurnDisplay{
			Kind:   "status",
			Phase:  "completed",
			Label:  "turn",
			Text:   "provider turn completed",
			Output: displayValue(data),
		}
	case "turn.canceled":
		return &gestalt.AgentTurnDisplay{
			Kind:  "status",
			Phase: "canceled",
			Label: "turn",
			Text:  displayString(data, "reason"),
			Error: displayValue(data),
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

func displayValue(value any) any {
	if data, ok := value.(map[string]any); ok {
		return cloneMap(data)
	}
	return value
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

func sortedSessions(input map[string]*gestalt.AgentSession) []gestalt.AgentSession {
	ids := make([]string, 0, len(input))
	for id := range input {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]gestalt.AgentSession, 0, len(ids))
	for _, id := range ids {
		if session := cloneSession(input[id]); session != nil {
			out = append(out, *session)
		}
	}
	return out
}

func cloneMessages(input []gestalt.AgentMessage) []gestalt.AgentMessage {
	if len(input) == 0 {
		return nil
	}
	out := make([]gestalt.AgentMessage, 0, len(input))
	for _, message := range input {
		out = append(out, cloneMessage(message))
	}
	return out
}

func cloneMessage(input gestalt.AgentMessage) gestalt.AgentMessage {
	return gestalt.AgentMessage{
		Role:     input.Role,
		Text:     input.Text,
		Parts:    cloneMessageParts(input.Parts),
		Metadata: cloneMap(input.Metadata),
	}
}

func cloneMessageParts(input []gestalt.AgentMessagePart) []gestalt.AgentMessagePart {
	if len(input) == 0 {
		return nil
	}
	out := make([]gestalt.AgentMessagePart, 0, len(input))
	for _, part := range input {
		out = append(out, gestalt.AgentMessagePart{
			Type:       part.Type,
			Text:       part.Text,
			JSON:       cloneMap(part.JSON),
			ToolCall:   cloneToolCall(part.ToolCall),
			ToolResult: cloneToolResult(part.ToolResult),
			ImageRef:   cloneImageRef(part.ImageRef),
		})
	}
	return out
}

func cloneToolCall(input *gestalt.AgentMessagePartToolCall) *gestalt.AgentMessagePartToolCall {
	if input == nil {
		return nil
	}
	return &gestalt.AgentMessagePartToolCall{
		ID:        input.ID,
		ToolID:    input.ToolID,
		Arguments: cloneMap(input.Arguments),
	}
}

func cloneToolResult(input *gestalt.AgentMessagePartToolResult) *gestalt.AgentMessagePartToolResult {
	if input == nil {
		return nil
	}
	return &gestalt.AgentMessagePartToolResult{
		ToolCallID: input.ToolCallID,
		Status:     input.Status,
		Content:    input.Content,
		Output:     cloneMap(input.Output),
	}
}

func cloneImageRef(input *gestalt.AgentMessagePartImageRef) *gestalt.AgentMessagePartImageRef {
	if input == nil {
		return nil
	}
	return &gestalt.AgentMessagePartImageRef{
		URI:      input.URI,
		MimeType: input.MimeType,
	}
}

func cloneSession(input *gestalt.AgentSession) *gestalt.AgentSession {
	if input == nil {
		return nil
	}
	return &gestalt.AgentSession{
		ID:                 input.ID,
		ProviderName:       input.ProviderName,
		Model:              input.Model,
		ClientRef:          input.ClientRef,
		State:              input.State,
		Metadata:           cloneMap(input.Metadata),
		CreatedBySubjectID: input.CreatedBySubjectID,
		CreatedAt:          input.CreatedAt,
		UpdatedAt:          input.UpdatedAt,
		LastTurnAt:         cloneTime(input.LastTurnAt),
	}
}

func cloneTurn(input *gestalt.AgentTurn) *gestalt.AgentTurn {
	if input == nil {
		return nil
	}
	return &gestalt.AgentTurn{
		ID:                 input.ID,
		SessionID:          input.SessionID,
		ProviderName:       input.ProviderName,
		Model:              input.Model,
		Status:             input.Status,
		Messages:           cloneMessages(input.Messages),
		Output:             cloneTurnOutput(input.Output),
		StatusMessage:      input.StatusMessage,
		CreatedBySubjectID: input.CreatedBySubjectID,
		CreatedAt:          input.CreatedAt,
		StartedAt:          cloneTime(input.StartedAt),
		CompletedAt:        cloneTime(input.CompletedAt),
		ExecutionRef:       input.ExecutionRef,
	}
}

func cloneTurnOutput(input *gestalt.AgentTurnOutput) *gestalt.AgentTurnOutput {
	if input == nil {
		return nil
	}
	if input.Structured != nil {
		return &gestalt.AgentTurnOutput{
			Structured: &gestalt.AgentTurnStructuredOutput{
				Text:  input.Structured.Text,
				Value: cloneMap(input.Structured.Value),
			},
		}
	}
	if input.Text != nil {
		return &gestalt.AgentTurnOutput{Text: &gestalt.AgentTurnTextOutput{Text: input.Text.Text}}
	}
	return &gestalt.AgentTurnOutput{}
}

func cloneTurnEvent(input *gestalt.AgentTurnEvent) *gestalt.AgentTurnEvent {
	if input == nil {
		return nil
	}
	return &gestalt.AgentTurnEvent{
		ID:         input.ID,
		TurnID:     input.TurnID,
		Seq:        input.Seq,
		Type:       input.Type,
		Source:     input.Source,
		Visibility: input.Visibility,
		Data:       cloneMap(input.Data),
		CreatedAt:  input.CreatedAt,
		Display:    cloneDisplay(input.Display),
	}
}

func cloneInteraction(input *gestalt.AgentInteraction) *gestalt.AgentInteraction {
	if input == nil {
		return nil
	}
	return &gestalt.AgentInteraction{
		ID:         input.ID,
		Type:       input.Type,
		State:      input.State,
		Title:      input.Title,
		Prompt:     input.Prompt,
		Request:    cloneMap(input.Request),
		Resolution: cloneMap(input.Resolution),
		CreatedAt:  input.CreatedAt,
		ResolvedAt: cloneTime(input.ResolvedAt),
		TurnID:     input.TurnID,
		SessionID:  input.SessionID,
	}
}

func cloneDisplay(input *gestalt.AgentTurnDisplay) *gestalt.AgentTurnDisplay {
	if input == nil {
		return nil
	}
	return &gestalt.AgentTurnDisplay{
		Kind:      input.Kind,
		Phase:     input.Phase,
		Text:      input.Text,
		Label:     input.Label,
		Ref:       input.Ref,
		ParentRef: input.ParentRef,
		Input:     cloneAny(input.Input),
		Output:    cloneAny(input.Output),
		Error:     cloneAny(input.Error),
		Action:    input.Action,
		Format:    input.Format,
		Language:  input.Language,
	}
}

func cloneAny(input any) any {
	switch data := input.(type) {
	case map[string]any:
		return cloneMap(data)
	case []any:
		if len(data) == 0 {
			return nil
		}
		out := make([]any, 0, len(data))
		for _, value := range data {
			out = append(out, cloneAny(value))
		}
		return out
	default:
		return input
	}
}

func cloneMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = cloneAny(value)
	}
	return out
}

func cloneTime(input *time.Time) *time.Time {
	if input == nil {
		return nil
	}
	value := *input
	return &value
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := gestalt.ServeAgentProvider(ctx, newAgentProvider()); err != nil {
		panic(err)
	}
}
