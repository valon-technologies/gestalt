package providerhost

import (
	"fmt"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
)

func agentRunStatusFromProto(status proto.AgentRunStatus) (coreagent.RunStatus, error) {
	switch status {
	case proto.AgentRunStatus_AGENT_RUN_STATUS_UNSPECIFIED:
		return "", nil
	case proto.AgentRunStatus_AGENT_RUN_STATUS_PENDING:
		return coreagent.RunStatusPending, nil
	case proto.AgentRunStatus_AGENT_RUN_STATUS_RUNNING:
		return coreagent.RunStatusRunning, nil
	case proto.AgentRunStatus_AGENT_RUN_STATUS_SUCCEEDED:
		return coreagent.RunStatusSucceeded, nil
	case proto.AgentRunStatus_AGENT_RUN_STATUS_FAILED:
		return coreagent.RunStatusFailed, nil
	case proto.AgentRunStatus_AGENT_RUN_STATUS_CANCELED:
		return coreagent.RunStatusCanceled, nil
	case proto.AgentRunStatus_AGENT_RUN_STATUS_WAITING_FOR_INPUT:
		return coreagent.RunStatusWaitingForInput, nil
	default:
		return "", fmt.Errorf("unknown agent run status %v", status)
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
		PluginName: target.PluginName,
		Operation:  target.Operation,
		Connection: target.Connection,
		Instance:   target.Instance,
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
	for _, tool := range tools {
		value, err := agentToolToProto(tool)
		if err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, nil
}

func agentToolRefFromProto(ref *proto.AgentToolRef) coreagent.ToolRef {
	if ref == nil {
		return coreagent.ToolRef{}
	}
	return coreagent.ToolRef{
		PluginName:  ref.GetPluginName(),
		Operation:   ref.GetOperation(),
		Connection:  ref.GetConnection(),
		Instance:    ref.GetInstance(),
		Title:       ref.GetTitle(),
		Description: ref.GetDescription(),
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

func agentToolSourceModeFromProto(mode proto.AgentToolSourceMode) coreagent.ToolSourceMode {
	switch mode {
	case proto.AgentToolSourceMode_AGENT_TOOL_SOURCE_MODE_EXPLICIT:
		return coreagent.ToolSourceModeExplicit
	case proto.AgentToolSourceMode_AGENT_TOOL_SOURCE_MODE_INHERIT_INVOKES:
		return coreagent.ToolSourceModeInheritInvokes
	default:
		return coreagent.ToolSourceModeUnspecified
	}
}

func agentRunFromProto(run *proto.BoundAgentRun) (*coreagent.Run, error) {
	if run == nil {
		return nil, nil
	}
	status, err := agentRunStatusFromProto(run.GetStatus())
	if err != nil {
		return nil, err
	}
	return &coreagent.Run{
		ID:               run.GetId(),
		ProviderName:     run.GetProviderName(),
		Model:            run.GetModel(),
		Status:           status,
		Messages:         agentMessagesFromProto(run.GetMessages()),
		OutputText:       run.GetOutputText(),
		StructuredOutput: mapFromStruct(run.GetStructuredOutput()),
		StatusMessage:    run.GetStatusMessage(),
		SessionRef:       run.GetSessionRef(),
		CreatedBy:        agentActorFromProto(run.GetCreatedBy()),
		CreatedAt:        timeFromProto(run.GetCreatedAt()),
		StartedAt:        timeFromProto(run.GetStartedAt()),
		CompletedAt:      timeFromProto(run.GetCompletedAt()),
		ExecutionRef:     run.GetExecutionRef(),
	}, nil
}

func agentRunStatusToProto(status coreagent.RunStatus) proto.AgentRunStatus {
	switch status {
	case "":
		return proto.AgentRunStatus_AGENT_RUN_STATUS_UNSPECIFIED
	case coreagent.RunStatusPending:
		return proto.AgentRunStatus_AGENT_RUN_STATUS_PENDING
	case coreagent.RunStatusRunning:
		return proto.AgentRunStatus_AGENT_RUN_STATUS_RUNNING
	case coreagent.RunStatusSucceeded:
		return proto.AgentRunStatus_AGENT_RUN_STATUS_SUCCEEDED
	case coreagent.RunStatusFailed:
		return proto.AgentRunStatus_AGENT_RUN_STATUS_FAILED
	case coreagent.RunStatusCanceled:
		return proto.AgentRunStatus_AGENT_RUN_STATUS_CANCELED
	case coreagent.RunStatusWaitingForInput:
		return proto.AgentRunStatus_AGENT_RUN_STATUS_WAITING_FOR_INPUT
	default:
		return proto.AgentRunStatus_AGENT_RUN_STATUS_UNSPECIFIED
	}
}

func agentRunToProto(run *coreagent.Run) (*proto.BoundAgentRun, error) {
	if run == nil {
		return nil, nil
	}
	messages, err := agentMessagesToProto(run.Messages)
	if err != nil {
		return nil, fmt.Errorf("agent messages: %w", err)
	}
	structuredOutput, err := structFromMap(run.StructuredOutput)
	if err != nil {
		return nil, fmt.Errorf("agent structured output: %w", err)
	}
	return &proto.BoundAgentRun{
		Id:               run.ID,
		ProviderName:     run.ProviderName,
		Model:            run.Model,
		Status:           agentRunStatusToProto(run.Status),
		Messages:         messages,
		OutputText:       run.OutputText,
		StructuredOutput: structuredOutput,
		StatusMessage:    run.StatusMessage,
		SessionRef:       run.SessionRef,
		CreatedBy:        agentActorToProto(run.CreatedBy),
		CreatedAt:        timeToProto(run.CreatedAt),
		StartedAt:        timeToProto(run.StartedAt),
		CompletedAt:      timeToProto(run.CompletedAt),
		ExecutionRef:     run.ExecutionRef,
	}, nil
}

func managedAgentRunToProto(run *coreagent.ManagedRun) (*proto.ManagedAgentRun, error) {
	if run == nil {
		return nil, nil
	}
	encodedRun, err := agentRunToProto(run.Run)
	if err != nil {
		return nil, err
	}
	return &proto.ManagedAgentRun{
		ProviderName: run.ProviderName,
		Run:          encodedRun,
	}, nil
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
		StreamingText:       value.GetStreamingText(),
		ToolCalls:           value.GetToolCalls(),
		ParallelToolCalls:   value.GetParallelToolCalls(),
		StructuredOutput:    value.GetStructuredOutput(),
		SessionContinuation: value.GetSessionContinuation(),
		Approvals:           value.GetApprovals(),
		ResumableRuns:       value.GetResumableRuns(),
		ReasoningSummaries:  value.GetReasoningSummaries(),
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
		RunId:      value.RunID,
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
