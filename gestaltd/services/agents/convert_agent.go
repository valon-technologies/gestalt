package agents

import (
	"fmt"
	"log/slog"
	"maps"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/services/internal/agentwire"
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

func agentMessagesToProto(messages []coreagent.Message) ([]*proto.AgentMessage, error) {
	return agentwire.MessagesToProto(messages)
}

func agentMessagesFromProto(messages []*proto.AgentMessage) []coreagent.Message {
	return agentwire.MessagesFromProto(messages)
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

func agentSubjectContextToProto(subject coreagent.SubjectContext) *proto.AgentSubjectContext {
	if subject == (coreagent.SubjectContext{}) {
		return nil
	}
	return &proto.AgentSubjectContext{
		SubjectId:           subject.SubjectID,
		SubjectKind:         subject.SubjectKind,
		CredentialSubjectId: subject.CredentialSubjectID,
		DisplayName:         subject.DisplayName,
		AuthSource:          subject.AuthSource,
	}
}

func agentWorkspaceFromProto(workspace *proto.AgentWorkspace) *coreagent.Workspace {
	if workspace == nil {
		return nil
	}
	out := &coreagent.Workspace{
		Checkouts: make([]coreagent.WorkspaceGitCheckout, 0, len(workspace.GetCheckouts())),
		CWD:       workspace.GetCwd(),
	}
	for _, checkout := range workspace.GetCheckouts() {
		if checkout == nil {
			continue
		}
		out.Checkouts = append(out.Checkouts, coreagent.WorkspaceGitCheckout{
			URL:  checkout.GetUrl(),
			Ref:  checkout.GetRef(),
			Path: checkout.GetPath(),
		})
	}
	return out
}

func preparedAgentWorkspaceToProto(workspace *coreagent.PreparedWorkspace) *proto.PreparedAgentWorkspace {
	if workspace == nil {
		return nil
	}
	return &proto.PreparedAgentWorkspace{
		Root: workspace.Root,
		Cwd:  workspace.CWD,
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

func listedAgentToolToProto(tool coreagent.ListedTool) *proto.ListedAgentTool {
	return &proto.ListedAgentTool{
		Id:           tool.ToolID,
		McpName:      tool.MCPName,
		Title:        tool.Title,
		Description:  tool.Description,
		Tags:         append([]string(nil), tool.Tags...),
		SearchText:   tool.SearchText,
		InputSchema:  tool.InputSchemaJSON,
		OutputSchema: tool.OutputSchemaJSON,
		Annotations:  operationAnnotationsToProto(tool.Annotations),
		Ref:          agentToolRefToProto(tool.Ref),
	}
}

func listedAgentToolsToProto(tools []coreagent.ListedTool) []*proto.ListedAgentTool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]*proto.ListedAgentTool, 0, len(tools))
	for i := range tools {
		out = append(out, listedAgentToolToProto(tools[i]))
	}
	return out
}

func agentToolRefToProto(ref coreagent.ToolRef) *proto.AgentToolRef {
	return agentwire.ToolRefToProto(ref)
}

func agentToolRefsFromProto(refs []*proto.AgentToolRef) []coreagent.ToolRef {
	return agentwire.ToolRefsFromProto(refs)
}

func agentToolRefsToProto(refs []coreagent.ToolRef) []*proto.AgentToolRef {
	return agentwire.ToolRefsToProto(refs)
}

func agentToolSourceModeFromProto(mode proto.AgentToolSourceMode) coreagent.ToolSourceMode {
	return agentwire.ToolSourceModeFromProto(mode)
}

func agentToolSourceModeToProto(mode coreagent.ToolSourceMode) proto.AgentToolSourceMode {
	return agentwire.ToolSourceModeToProto(mode)
}

func agentToolSourceModesFromProto(modes []proto.AgentToolSourceMode) []coreagent.ToolSourceMode {
	if len(modes) == 0 {
		return nil
	}
	out := make([]coreagent.ToolSourceMode, 0, len(modes))
	for _, mode := range modes {
		if converted := agentToolSourceModeFromProto(mode); converted != coreagent.ToolSourceModeUnspecified {
			out = append(out, converted)
		}
	}
	return out
}

func operationAnnotationsToProto(value core.CapabilityAnnotations) *proto.OperationAnnotations {
	if value.ReadOnlyHint == nil && value.IdempotentHint == nil && value.DestructiveHint == nil && value.OpenWorldHint == nil {
		return nil
	}
	return &proto.OperationAnnotations{
		ReadOnlyHint:    value.ReadOnlyHint,
		IdempotentHint:  value.IdempotentHint,
		DestructiveHint: value.DestructiveHint,
		OpenWorldHint:   value.OpenWorldHint,
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
		Action:    display.GetAction(),
		Format:    display.GetFormat(),
		Language:  display.GetLanguage(),
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
		Action:    display.Action,
		Format:    display.Format,
		Language:  display.Language,
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

func agentProviderCapabilitiesFromProto(value *proto.AgentProviderCapabilities) *coreagent.ProviderCapabilities {
	if value == nil {
		return nil
	}
	return &coreagent.ProviderCapabilities{
		StreamingText:             value.GetStreamingText(),
		ToolCalls:                 value.GetToolCalls(),
		ParallelToolCalls:         value.GetParallelToolCalls(),
		StructuredOutput:          value.GetStructuredOutput(),
		Interactions:              value.GetInteractions(),
		ResumableTurns:            value.GetResumableTurns(),
		ReasoningSummaries:        value.GetReasoningSummaries(),
		SupportsSessionStart:      value.GetSupportsSessionStart(),
		SupportsPreparedWorkspace: value.GetSupportsPreparedWorkspace(),
		BoundedListHydration:      value.GetBoundedListHydration(),
		SupportedToolSources:      agentToolSourceModesFromProto(value.GetSupportedToolSources()),
	}
}

func sessionStartConfigToProto(value *coreagent.SessionStartConfig) *proto.AgentSessionStartConfig {
	if value == nil || len(value.Hooks) == 0 {
		return nil
	}
	out := &proto.AgentSessionStartConfig{
		Hooks: make([]*proto.AgentSessionStartHook, 0, len(value.Hooks)),
	}
	for i := range value.Hooks {
		hook := value.Hooks[i]
		out.Hooks = append(out.Hooks, &proto.AgentSessionStartHook{
			Id:      hook.ID,
			Type:    hook.Type,
			Command: append([]string(nil), hook.Command...),
			Cwd:     hook.CWD,
			Timeout: hook.Timeout,
			Env:     maps.Clone(hook.Env),
			Output: &proto.AgentSessionStartHookOutput{
				AdditionalContext: hook.Output.AdditionalContext,
				Metadata:          hook.Output.Metadata,
			},
		})
	}
	return out
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
