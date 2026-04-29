package providerhost

import (
	"fmt"
	"log/slog"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
)

func agentExecutionStatusFromProto(status proto.AgentExecutionStatus) (coreagent.ExecutionStatus, error) {
	switch status {
	case proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_UNSPECIFIED:
		return "", nil
	case proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_PENDING:
		return coreagent.ExecutionStatusPending, nil
	case proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_RUNNING:
		return coreagent.ExecutionStatusRunning, nil
	case proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_SUCCEEDED:
		return coreagent.ExecutionStatusSucceeded, nil
	case proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_FAILED:
		return coreagent.ExecutionStatusFailed, nil
	case proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_CANCELED:
		return coreagent.ExecutionStatusCanceled, nil
	case proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_WAITING_FOR_INPUT:
		return coreagent.ExecutionStatusWaitingForInput, nil
	default:
		return "", fmt.Errorf("unknown agent execution status %v", status)
	}
}

func agentMessageToProto(message coreagent.Message) (*proto.AgentMessage, error) {
	parts, err := agentMessagePartsToProto(message.Parts)
	if err != nil {
		return nil, err
	}
	metadata, err := structFromMap(message.Metadata)
	if err != nil {
		return nil, err
	}
	return &proto.AgentMessage{
		Role:     message.Role,
		Text:     message.Text,
		Parts:    parts,
		Metadata: metadata,
	}, nil
}

func agentMessagesToProto(messages []coreagent.Message) ([]*proto.AgentMessage, error) {
	if len(messages) == 0 {
		return nil, nil
	}
	out := make([]*proto.AgentMessage, 0, len(messages))
	for _, message := range messages {
		encoded, err := agentMessageToProto(message)
		if err != nil {
			return nil, err
		}
		out = append(out, encoded)
	}
	return out, nil
}

func agentMessageFromProto(message *proto.AgentMessage) coreagent.Message {
	if message == nil {
		return coreagent.Message{}
	}
	return coreagent.Message{
		Role:     message.GetRole(),
		Text:     message.GetText(),
		Parts:    agentMessagePartsFromProto(message.GetParts()),
		Metadata: mapFromStruct(message.GetMetadata()),
	}
}

func agentMessagesFromProto(messages []*proto.AgentMessage) []coreagent.Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]coreagent.Message, 0, len(messages))
	for _, message := range messages {
		out = append(out, agentMessageFromProto(message))
	}
	return out
}

func agentActorToProto(actor coreagent.Actor) *proto.AgentActor {
	if actor == (coreagent.Actor{}) {
		return nil
	}
	return &proto.AgentActor{
		SubjectId:   actor.SubjectID,
		SubjectKind: actor.SubjectKind,
		DisplayName: actor.DisplayName,
		AuthSource:  actor.AuthSource,
	}
}

func agentActorFromProto(actor *proto.AgentActor) coreagent.Actor {
	if actor == nil {
		return coreagent.Actor{}
	}
	return coreagent.Actor{
		SubjectID:   actor.GetSubjectId(),
		SubjectKind: actor.GetSubjectKind(),
		DisplayName: actor.GetDisplayName(),
		AuthSource:  actor.GetAuthSource(),
	}
}

func agentToolTargetToProto(target coreagent.ToolTarget) *proto.BoundAgentToolTarget {
	if target == (coreagent.ToolTarget{}) {
		return nil
	}
	return &proto.BoundAgentToolTarget{
		System:         target.System,
		Plugin:         target.Plugin,
		Operation:      target.Operation,
		Connection:     target.Connection,
		Instance:       target.Instance,
		CredentialMode: string(target.CredentialMode),
	}
}

func agentToolToProto(tool coreagent.Tool) (*proto.ResolvedAgentTool, error) {
	schema, err := structFromMap(tool.ParametersSchema)
	if err != nil {
		return nil, fmt.Errorf("agent tool parameters schema: %w", err)
	}
	return &proto.ResolvedAgentTool{
		Id:               tool.ID,
		Name:             tool.Name,
		Description:      tool.Description,
		Target:           agentToolTargetToProto(tool.Target),
		ParametersSchema: schema,
	}, nil
}

func agentToolsToProto(tools []coreagent.Tool) ([]*proto.ResolvedAgentTool, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	out := make([]*proto.ResolvedAgentTool, 0, len(tools))
	for i := range tools {
		value, err := agentToolToProto(tools[i])
		if err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, nil
}

func agentToolCandidateToProto(candidate coreagent.ToolCandidate) *proto.AgentToolCandidate {
	return &proto.AgentToolCandidate{
		Ref:         agentToolRefToProto(candidate.Ref),
		Id:          candidate.ID,
		Name:        candidate.Name,
		Description: candidate.Description,
		Parameters:  append([]string(nil), candidate.Parameters...),
		Score:       candidate.Score,
	}
}

func agentToolCandidatesToProto(candidates []coreagent.ToolCandidate) []*proto.AgentToolCandidate {
	if len(candidates) == 0 {
		return nil
	}
	out := make([]*proto.AgentToolCandidate, 0, len(candidates))
	for i := range candidates {
		out = append(out, agentToolCandidateToProto(candidates[i]))
	}
	return out
}

func agentToolRefFromProto(ref *proto.AgentToolRef) coreagent.ToolRef {
	if ref == nil {
		return coreagent.ToolRef{}
	}
	return coreagent.ToolRef{
		System:         ref.GetSystem(),
		Plugin:         ref.GetPlugin(),
		Operation:      ref.GetOperation(),
		Connection:     ref.GetConnection(),
		Instance:       ref.GetInstance(),
		Title:          ref.GetTitle(),
		Description:    ref.GetDescription(),
		CredentialMode: core.ConnectionMode(ref.GetCredentialMode()),
	}
}

func agentToolRefToProto(ref coreagent.ToolRef) *proto.AgentToolRef {
	return &proto.AgentToolRef{
		System:         ref.System,
		Plugin:         ref.Plugin,
		Operation:      ref.Operation,
		Connection:     ref.Connection,
		Instance:       ref.Instance,
		Title:          ref.Title,
		Description:    ref.Description,
		CredentialMode: string(ref.CredentialMode),
	}
}

func agentToolRefsFromProto(refs []*proto.AgentToolRef) []coreagent.ToolRef {
	if len(refs) == 0 {
		return nil
	}
	out := make([]coreagent.ToolRef, 0, len(refs))
	for _, ref := range refs {
		out = append(out, agentToolRefFromProto(ref))
	}
	return out
}

func agentToolRefsToProto(refs []coreagent.ToolRef) []*proto.AgentToolRef {
	if len(refs) == 0 {
		return nil
	}
	out := make([]*proto.AgentToolRef, 0, len(refs))
	for i := range refs {
		out = append(out, agentToolRefToProto(refs[i]))
	}
	return out
}

func agentToolSourceModeFromProto(mode proto.AgentToolSourceMode) coreagent.ToolSourceMode {
	switch mode {
	case proto.AgentToolSourceMode_AGENT_TOOL_SOURCE_MODE_NATIVE_SEARCH:
		return coreagent.ToolSourceModeNativeSearch
	default:
		return coreagent.ToolSourceModeUnspecified
	}
}

func agentToolSourceModeToProto(mode coreagent.ToolSourceMode) proto.AgentToolSourceMode {
	switch mode {
	case coreagent.ToolSourceModeNativeSearch:
		return proto.AgentToolSourceMode_AGENT_TOOL_SOURCE_MODE_NATIVE_SEARCH
	default:
		return proto.AgentToolSourceMode_AGENT_TOOL_SOURCE_MODE_UNSPECIFIED
	}
}

func agentExecutionStatusToProto(status coreagent.ExecutionStatus) proto.AgentExecutionStatus {
	switch status {
	case "":
		return proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_UNSPECIFIED
	case coreagent.ExecutionStatusPending:
		return proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_PENDING
	case coreagent.ExecutionStatusRunning:
		return proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_RUNNING
	case coreagent.ExecutionStatusSucceeded:
		return proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_SUCCEEDED
	case coreagent.ExecutionStatusFailed:
		return proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_FAILED
	case coreagent.ExecutionStatusCanceled:
		return proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_CANCELED
	case coreagent.ExecutionStatusWaitingForInput:
		return proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_WAITING_FOR_INPUT
	default:
		return proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_UNSPECIFIED
	}
}

func agentSessionStateFromProto(state proto.AgentSessionState) (coreagent.SessionState, error) {
	switch state {
	case proto.AgentSessionState_AGENT_SESSION_STATE_UNSPECIFIED:
		return "", nil
	case proto.AgentSessionState_AGENT_SESSION_STATE_ACTIVE:
		return coreagent.SessionStateActive, nil
	case proto.AgentSessionState_AGENT_SESSION_STATE_ARCHIVED:
		return coreagent.SessionStateArchived, nil
	default:
		return "", fmt.Errorf("unknown agent session state %v", state)
	}
}

func agentSessionStateToProto(state coreagent.SessionState) proto.AgentSessionState {
	switch state {
	case "":
		return proto.AgentSessionState_AGENT_SESSION_STATE_UNSPECIFIED
	case coreagent.SessionStateActive:
		return proto.AgentSessionState_AGENT_SESSION_STATE_ACTIVE
	case coreagent.SessionStateArchived:
		return proto.AgentSessionState_AGENT_SESSION_STATE_ARCHIVED
	default:
		return proto.AgentSessionState_AGENT_SESSION_STATE_UNSPECIFIED
	}
}

func agentSessionFromProto(session *proto.AgentSession) (*coreagent.Session, error) {
	if session == nil {
		return nil, nil
	}
	state, err := agentSessionStateFromProto(session.GetState())
	if err != nil {
		return nil, err
	}
	return &coreagent.Session{
		ID:           session.GetId(),
		ProviderName: session.GetProviderName(),
		Model:        session.GetModel(),
		ClientRef:    session.GetClientRef(),
		State:        state,
		Metadata:     mapFromStruct(session.GetMetadata()),
		CreatedBy:    agentActorFromProto(session.GetCreatedBy()),
		CreatedAt:    timeFromProto(session.GetCreatedAt()),
		UpdatedAt:    timeFromProto(session.GetUpdatedAt()),
		LastTurnAt:   timeFromProto(session.GetLastTurnAt()),
	}, nil
}

func agentTurnFromProto(turn *proto.AgentTurn) (*coreagent.Turn, error) {
	if turn == nil {
		return nil, nil
	}
	status, err := agentExecutionStatusFromProto(turn.GetStatus())
	if err != nil {
		return nil, err
	}
	return &coreagent.Turn{
		ID:               turn.GetId(),
		SessionID:        turn.GetSessionId(),
		ProviderName:     turn.GetProviderName(),
		Model:            turn.GetModel(),
		Status:           status,
		Messages:         agentMessagesFromProto(turn.GetMessages()),
		OutputText:       turn.GetOutputText(),
		StructuredOutput: mapFromStruct(turn.GetStructuredOutput()),
		StatusMessage:    turn.GetStatusMessage(),
		CreatedBy:        agentActorFromProto(turn.GetCreatedBy()),
		CreatedAt:        timeFromProto(turn.GetCreatedAt()),
		StartedAt:        timeFromProto(turn.GetStartedAt()),
		CompletedAt:      timeFromProto(turn.GetCompletedAt()),
		ExecutionRef:     turn.GetExecutionRef(),
	}, nil
}

func agentTurnEventFromProto(event *proto.AgentTurnEvent) *coreagent.TurnEvent {
	if event == nil {
		return nil
	}
	return &coreagent.TurnEvent{
		ID:         event.GetId(),
		TurnID:     event.GetTurnId(),
		Seq:        event.GetSeq(),
		Type:       event.GetType(),
		Source:     event.GetSource(),
		Visibility: event.GetVisibility(),
		Data:       mapFromStruct(event.GetData()),
		CreatedAt:  timeFromProto(event.GetCreatedAt()),
		Display:    agentTurnDisplayFromProto(event.GetDisplay()),
	}
}

func agentTurnEventsFromProto(events []*proto.AgentTurnEvent) []*coreagent.TurnEvent {
	if len(events) == 0 {
		return nil
	}
	out := make([]*coreagent.TurnEvent, 0, len(events))
	for _, event := range events {
		out = append(out, agentTurnEventFromProto(event))
	}
	return out
}

func agentTurnDisplayFromProto(display *proto.AgentTurnDisplay) *coreagent.TurnDisplay {
	if display == nil {
		return nil
	}
	return &coreagent.TurnDisplay{
		Kind:      display.GetKind(),
		Phase:     display.GetPhase(),
		Text:      display.GetText(),
		Label:     display.GetLabel(),
		Ref:       display.GetRef(),
		ParentRef: display.GetParentRef(),
		Input:     protoValueToAny(display.GetInput()),
		Output:    protoValueToAny(display.GetOutput()),
		Error:     protoValueToAny(display.GetError()),
	}
}

func agentTurnDisplayToProto(display *coreagent.TurnDisplay) (*proto.AgentTurnDisplay, error) {
	if display == nil {
		return nil, nil
	}
	input, err := protoValueFromAny(display.Input)
	if err != nil {
		return nil, fmt.Errorf("agent turn display input: %w", err)
	}
	output, err := protoValueFromAny(display.Output)
	if err != nil {
		return nil, fmt.Errorf("agent turn display output: %w", err)
	}
	displayErr, err := protoValueFromAny(display.Error)
	if err != nil {
		return nil, fmt.Errorf("agent turn display error: %w", err)
	}
	return &proto.AgentTurnDisplay{
		Kind:      display.Kind,
		Phase:     display.Phase,
		Text:      display.Text,
		Label:     display.Label,
		Ref:       display.Ref,
		ParentRef: display.ParentRef,
		Input:     input,
		Output:    output,
		Error:     displayErr,
	}, nil
}

func agentSessionToProto(session *coreagent.Session) (*proto.AgentSession, error) {
	if session == nil {
		return nil, nil
	}
	metadata, err := structFromMap(session.Metadata)
	if err != nil {
		return nil, fmt.Errorf("agent session metadata: %w", err)
	}
	return &proto.AgentSession{
		Id:           session.ID,
		ProviderName: session.ProviderName,
		Model:        session.Model,
		ClientRef:    session.ClientRef,
		State:        agentSessionStateToProto(session.State),
		Metadata:     metadata,
		CreatedBy:    agentActorToProto(session.CreatedBy),
		CreatedAt:    timeToProto(session.CreatedAt),
		UpdatedAt:    timeToProto(session.UpdatedAt),
		LastTurnAt:   timeToProto(session.LastTurnAt),
	}, nil
}

func agentTurnToProto(turn *coreagent.Turn) (*proto.AgentTurn, error) {
	if turn == nil {
		return nil, nil
	}
	messages, err := agentMessagesToProto(turn.Messages)
	if err != nil {
		return nil, fmt.Errorf("agent turn messages: %w", err)
	}
	structuredOutput, err := structFromMap(turn.StructuredOutput)
	if err != nil {
		return nil, fmt.Errorf("agent turn structured output: %w", err)
	}
	return &proto.AgentTurn{
		Id:               turn.ID,
		SessionId:        turn.SessionID,
		ProviderName:     turn.ProviderName,
		Model:            turn.Model,
		Status:           agentExecutionStatusToProto(turn.Status),
		Messages:         messages,
		OutputText:       turn.OutputText,
		StructuredOutput: structuredOutput,
		StatusMessage:    turn.StatusMessage,
		CreatedBy:        agentActorToProto(turn.CreatedBy),
		CreatedAt:        timeToProto(turn.CreatedAt),
		StartedAt:        timeToProto(turn.StartedAt),
		CompletedAt:      timeToProto(turn.CompletedAt),
		ExecutionRef:     turn.ExecutionRef,
	}, nil
}

func turnEventsToProto(values []*coreagent.TurnEvent) []*proto.AgentTurnEvent {
	if len(values) == 0 {
		return nil
	}
	out := make([]*proto.AgentTurnEvent, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		data, err := structFromMap(value.Data)
		if err != nil {
			slog.Warn("omit invalid agent turn event data during proto conversion", "turn_id", value.TurnID, "event_id", value.ID, "type", value.Type, "error", err)
			data = nil
		}
		display, err := agentTurnDisplayToProto(value.Display)
		if err != nil {
			slog.Warn("omit invalid agent turn event display during proto conversion", "turn_id", value.TurnID, "event_id", value.ID, "type", value.Type, "error", err)
			display = nil
		}
		out = append(out, &proto.AgentTurnEvent{
			Id:         value.ID,
			TurnId:     value.TurnID,
			Seq:        value.Seq,
			Type:       value.Type,
			Source:     value.Source,
			Visibility: value.Visibility,
			Data:       data,
			CreatedAt:  timeToProto(value.CreatedAt),
			Display:    display,
		})
	}
	return out
}

func agentMessagePartTypeFromProto(partType proto.AgentMessagePartType) (coreagent.MessagePartType, error) {
	switch partType {
	case proto.AgentMessagePartType_AGENT_MESSAGE_PART_TYPE_UNSPECIFIED:
		return "", nil
	case proto.AgentMessagePartType_AGENT_MESSAGE_PART_TYPE_TEXT:
		return coreagent.MessagePartTypeText, nil
	case proto.AgentMessagePartType_AGENT_MESSAGE_PART_TYPE_JSON:
		return coreagent.MessagePartTypeJSON, nil
	case proto.AgentMessagePartType_AGENT_MESSAGE_PART_TYPE_TOOL_CALL:
		return coreagent.MessagePartTypeToolCall, nil
	case proto.AgentMessagePartType_AGENT_MESSAGE_PART_TYPE_TOOL_RESULT:
		return coreagent.MessagePartTypeToolResult, nil
	case proto.AgentMessagePartType_AGENT_MESSAGE_PART_TYPE_IMAGE_REF:
		return coreagent.MessagePartTypeImageRef, nil
	default:
		return "", fmt.Errorf("unknown agent message part type %v", partType)
	}
}

func agentMessagePartTypeToProto(partType coreagent.MessagePartType) proto.AgentMessagePartType {
	switch partType {
	case "":
		return proto.AgentMessagePartType_AGENT_MESSAGE_PART_TYPE_UNSPECIFIED
	case coreagent.MessagePartTypeText:
		return proto.AgentMessagePartType_AGENT_MESSAGE_PART_TYPE_TEXT
	case coreagent.MessagePartTypeJSON:
		return proto.AgentMessagePartType_AGENT_MESSAGE_PART_TYPE_JSON
	case coreagent.MessagePartTypeToolCall:
		return proto.AgentMessagePartType_AGENT_MESSAGE_PART_TYPE_TOOL_CALL
	case coreagent.MessagePartTypeToolResult:
		return proto.AgentMessagePartType_AGENT_MESSAGE_PART_TYPE_TOOL_RESULT
	case coreagent.MessagePartTypeImageRef:
		return proto.AgentMessagePartType_AGENT_MESSAGE_PART_TYPE_IMAGE_REF
	default:
		return proto.AgentMessagePartType_AGENT_MESSAGE_PART_TYPE_UNSPECIFIED
	}
}

func agentMessagePartToProto(part coreagent.MessagePart) (*proto.AgentMessagePart, error) {
	jsonValue, err := structFromMap(part.JSON)
	if err != nil {
		return nil, fmt.Errorf("agent message part json: %w", err)
	}
	var toolCall *proto.AgentMessagePartToolCall
	if part.ToolCall != nil {
		args, err := structFromMap(part.ToolCall.Arguments)
		if err != nil {
			return nil, fmt.Errorf("agent message tool call arguments: %w", err)
		}
		toolCall = &proto.AgentMessagePartToolCall{
			Id:        part.ToolCall.ID,
			ToolId:    part.ToolCall.ToolID,
			Arguments: args,
		}
	}
	var toolResult *proto.AgentMessagePartToolResult
	if part.ToolResult != nil {
		output, err := structFromMap(part.ToolResult.Output)
		if err != nil {
			return nil, fmt.Errorf("agent message tool result output: %w", err)
		}
		toolResult = &proto.AgentMessagePartToolResult{
			ToolCallId: part.ToolResult.ToolCallID,
			Status:     int32(part.ToolResult.Status),
			Content:    part.ToolResult.Content,
			Output:     output,
		}
	}
	var imageRef *proto.AgentMessagePartImageRef
	if part.ImageRef != nil {
		imageRef = &proto.AgentMessagePartImageRef{
			Uri:      part.ImageRef.URI,
			MimeType: part.ImageRef.MIMEType,
		}
	}
	return &proto.AgentMessagePart{
		Type:       agentMessagePartTypeToProto(part.Type),
		Text:       part.Text,
		Json:       jsonValue,
		ToolCall:   toolCall,
		ToolResult: toolResult,
		ImageRef:   imageRef,
	}, nil
}

func agentMessagePartsToProto(parts []coreagent.MessagePart) ([]*proto.AgentMessagePart, error) {
	if len(parts) == 0 {
		return nil, nil
	}
	out := make([]*proto.AgentMessagePart, 0, len(parts))
	for _, part := range parts {
		encoded, err := agentMessagePartToProto(part)
		if err != nil {
			return nil, err
		}
		out = append(out, encoded)
	}
	return out, nil
}

func agentMessagePartFromProto(part *proto.AgentMessagePart) coreagent.MessagePart {
	if part == nil {
		return coreagent.MessagePart{}
	}
	partType, _ := agentMessagePartTypeFromProto(part.GetType())
	var toolCall *coreagent.ToolCallPart
	if value := part.GetToolCall(); value != nil {
		toolCall = &coreagent.ToolCallPart{
			ID:        value.GetId(),
			ToolID:    value.GetToolId(),
			Arguments: mapFromStruct(value.GetArguments()),
		}
	}
	var toolResult *coreagent.ToolResultPart
	if value := part.GetToolResult(); value != nil {
		toolResult = &coreagent.ToolResultPart{
			ToolCallID: value.GetToolCallId(),
			Status:     int(value.GetStatus()),
			Content:    value.GetContent(),
			Output:     mapFromStruct(value.GetOutput()),
		}
	}
	var imageRef *coreagent.ImageRefPart
	if value := part.GetImageRef(); value != nil {
		imageRef = &coreagent.ImageRefPart{
			URI:      value.GetUri(),
			MIMEType: value.GetMimeType(),
		}
	}
	return coreagent.MessagePart{
		Type:       partType,
		Text:       part.GetText(),
		JSON:       mapFromStruct(part.GetJson()),
		ToolCall:   toolCall,
		ToolResult: toolResult,
		ImageRef:   imageRef,
	}
}

func agentMessagePartsFromProto(parts []*proto.AgentMessagePart) []coreagent.MessagePart {
	if len(parts) == 0 {
		return nil
	}
	out := make([]coreagent.MessagePart, 0, len(parts))
	for _, part := range parts {
		out = append(out, agentMessagePartFromProto(part))
	}
	return out
}

func agentProviderCapabilitiesFromProto(value *proto.AgentProviderCapabilities) *coreagent.ProviderCapabilities {
	if value == nil {
		return nil
	}
	return &coreagent.ProviderCapabilities{
		StreamingText:      value.GetStreamingText(),
		ToolCalls:          value.GetToolCalls(),
		ParallelToolCalls:  value.GetParallelToolCalls(),
		StructuredOutput:   value.GetStructuredOutput(),
		Interactions:       value.GetInteractions(),
		ResumableTurns:     value.GetResumableTurns(),
		ReasoningSummaries: value.GetReasoningSummaries(),
		NativeToolSearch:   value.GetNativeToolSearch(),
	}
}

func agentInteractionTypeFromProto(value proto.AgentInteractionType) (coreagent.InteractionType, error) {
	switch value {
	case proto.AgentInteractionType_AGENT_INTERACTION_TYPE_UNSPECIFIED:
		return "", nil
	case proto.AgentInteractionType_AGENT_INTERACTION_TYPE_APPROVAL:
		return coreagent.InteractionTypeApproval, nil
	case proto.AgentInteractionType_AGENT_INTERACTION_TYPE_CLARIFICATION:
		return coreagent.InteractionTypeClarification, nil
	case proto.AgentInteractionType_AGENT_INTERACTION_TYPE_INPUT:
		return coreagent.InteractionTypeInput, nil
	default:
		return "", fmt.Errorf("unknown agent interaction type %v", value)
	}
}

func agentInteractionTypeToProto(value coreagent.InteractionType) proto.AgentInteractionType {
	switch value {
	case "":
		return proto.AgentInteractionType_AGENT_INTERACTION_TYPE_UNSPECIFIED
	case coreagent.InteractionTypeApproval:
		return proto.AgentInteractionType_AGENT_INTERACTION_TYPE_APPROVAL
	case coreagent.InteractionTypeClarification:
		return proto.AgentInteractionType_AGENT_INTERACTION_TYPE_CLARIFICATION
	case coreagent.InteractionTypeInput:
		return proto.AgentInteractionType_AGENT_INTERACTION_TYPE_INPUT
	default:
		return proto.AgentInteractionType_AGENT_INTERACTION_TYPE_UNSPECIFIED
	}
}

func agentInteractionStateToProto(value coreagent.InteractionState) proto.AgentInteractionState {
	switch value {
	case "":
		return proto.AgentInteractionState_AGENT_INTERACTION_STATE_UNSPECIFIED
	case coreagent.InteractionStatePending:
		return proto.AgentInteractionState_AGENT_INTERACTION_STATE_PENDING
	case coreagent.InteractionStateResolved:
		return proto.AgentInteractionState_AGENT_INTERACTION_STATE_RESOLVED
	case coreagent.InteractionStateCanceled:
		return proto.AgentInteractionState_AGENT_INTERACTION_STATE_CANCELED
	default:
		return proto.AgentInteractionState_AGENT_INTERACTION_STATE_UNSPECIFIED
	}
}

func agentInteractionStateFromProto(value proto.AgentInteractionState) (coreagent.InteractionState, error) {
	switch value {
	case proto.AgentInteractionState_AGENT_INTERACTION_STATE_UNSPECIFIED:
		return "", nil
	case proto.AgentInteractionState_AGENT_INTERACTION_STATE_PENDING:
		return coreagent.InteractionStatePending, nil
	case proto.AgentInteractionState_AGENT_INTERACTION_STATE_RESOLVED:
		return coreagent.InteractionStateResolved, nil
	case proto.AgentInteractionState_AGENT_INTERACTION_STATE_CANCELED:
		return coreagent.InteractionStateCanceled, nil
	default:
		return "", fmt.Errorf("unknown agent interaction state %v", value)
	}
}

func agentInteractionFromProto(value *proto.AgentInteraction) (*coreagent.Interaction, error) {
	if value == nil {
		return nil, nil
	}
	interactionType, err := agentInteractionTypeFromProto(value.GetType())
	if err != nil {
		return nil, err
	}
	state, err := agentInteractionStateFromProto(value.GetState())
	if err != nil {
		return nil, err
	}
	return &coreagent.Interaction{
		ID:         value.GetId(),
		TurnID:     value.GetTurnId(),
		SessionID:  value.GetSessionId(),
		Type:       interactionType,
		State:      state,
		Title:      value.GetTitle(),
		Prompt:     value.GetPrompt(),
		Request:    mapFromStruct(value.GetRequest()),
		Resolution: mapFromStruct(value.GetResolution()),
		CreatedAt:  timeFromProto(value.GetCreatedAt()),
		ResolvedAt: timeFromProto(value.GetResolvedAt()),
	}, nil
}

func agentInteractionsFromProto(values []*proto.AgentInteraction) ([]*coreagent.Interaction, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]*coreagent.Interaction, 0, len(values))
	for _, value := range values {
		interaction, err := agentInteractionFromProto(value)
		if err != nil {
			return nil, err
		}
		out = append(out, interaction)
	}
	return out, nil
}

func agentInteractionToProto(value *coreagent.Interaction) (*proto.AgentInteraction, error) {
	if value == nil {
		return nil, nil
	}
	request, err := structFromMap(value.Request)
	if err != nil {
		return nil, fmt.Errorf("agent interaction request: %w", err)
	}
	resolution, err := structFromMap(value.Resolution)
	if err != nil {
		return nil, fmt.Errorf("agent interaction resolution: %w", err)
	}
	return &proto.AgentInteraction{
		Id:         value.ID,
		TurnId:     value.TurnID,
		SessionId:  value.SessionID,
		Type:       agentInteractionTypeToProto(value.Type),
		State:      agentInteractionStateToProto(value.State),
		Title:      value.Title,
		Prompt:     value.Prompt,
		Request:    request,
		Resolution: resolution,
		CreatedAt:  timeToProto(value.CreatedAt),
		ResolvedAt: timeToProto(value.ResolvedAt),
	}, nil
}

func interactionsToProto(values []*coreagent.Interaction) []*proto.AgentInteraction {
	if len(values) == 0 {
		return nil
	}
	out := make([]*proto.AgentInteraction, 0, len(values))
	for _, value := range values {
		encoded, err := agentInteractionToProto(value)
		if err != nil {
			continue
		}
		out = append(out, encoded)
	}
	return out
}
