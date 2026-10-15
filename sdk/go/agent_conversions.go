package gestalt

import (
	"fmt"
	"strings"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

func agentMessageFromProto(value *proto.AgentMessage) AgentMessage {
	if value == nil {
		return AgentMessage{}
	}
	return AgentMessage{
		Role:     value.GetRole(),
		Text:     value.GetText(),
		Parts:    agentMessagePartsFromProto(value.GetParts()),
		Metadata: mapFromStruct(value.GetMetadata()),
	}
}

func agentMessagesFromProto(values []*proto.AgentMessage) []AgentMessage {
	if len(values) == 0 {
		return nil
	}
	out := make([]AgentMessage, 0, len(values))
	for _, value := range values {
		out = append(out, agentMessageFromProto(value))
	}
	return out
}

func agentMessageToProto(value AgentMessage) (*proto.AgentMessage, error) {
	metadata, err := structFromAny(value.Metadata)
	if err != nil {
		return nil, err
	}
	parts, err := agentMessagePartsToProto(value.Parts)
	if err != nil {
		return nil, err
	}
	return &proto.AgentMessage{
		Role:     value.Role,
		Text:     value.Text,
		Parts:    parts,
		Metadata: metadata,
	}, nil
}

func agentMessagesToProto(values []AgentMessage) ([]*proto.AgentMessage, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]*proto.AgentMessage, 0, len(values))
	for _, value := range values {
		pbValue, err := agentMessageToProto(value)
		if err != nil {
			return nil, err
		}
		out = append(out, pbValue)
	}
	return out, nil
}

func agentMessagePartsFromProto(values []*proto.AgentMessagePart) []AgentMessagePart {
	if len(values) == 0 {
		return nil
	}
	out := make([]AgentMessagePart, 0, len(values))
	for _, value := range values {
		out = append(out, agentMessagePartFromProto(value))
	}
	return out
}

func agentMessagePartFromProto(value *proto.AgentMessagePart) AgentMessagePart {
	if value == nil {
		return AgentMessagePart{}
	}
	return AgentMessagePart{
		Type:       AgentMessagePartType(value.GetType()),
		Text:       value.GetText(),
		JSON:       mapFromStruct(value.GetJson()),
		ToolCall:   agentMessagePartToolCallFromProto(value.GetToolCall()),
		ToolResult: agentMessagePartToolResultFromProto(value.GetToolResult()),
		ImageRef:   agentMessagePartImageRefFromProto(value.GetImageRef()),
	}
}

func agentMessagePartsToProto(values []AgentMessagePart) ([]*proto.AgentMessagePart, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]*proto.AgentMessagePart, 0, len(values))
	for _, value := range values {
		pbValue, err := agentMessagePartToProto(value)
		if err != nil {
			return nil, err
		}
		out = append(out, pbValue)
	}
	return out, nil
}

func agentMessagePartToProto(value AgentMessagePart) (*proto.AgentMessagePart, error) {
	partType := value.Type
	if partType == AgentMessagePartTypeUnspecified {
		partType = inferAgentMessagePartType(value)
	}
	jsonValue, err := structFromAny(value.JSON)
	if err != nil {
		return nil, err
	}
	toolCall, err := agentMessagePartToolCallToProto(value.ToolCall)
	if err != nil {
		return nil, err
	}
	toolResult, err := agentMessagePartToolResultToProto(value.ToolResult)
	if err != nil {
		return nil, err
	}
	return &proto.AgentMessagePart{
		Type:       proto.AgentMessagePartType(partType),
		Text:       value.Text,
		Json:       jsonValue,
		ToolCall:   toolCall,
		ToolResult: toolResult,
		ImageRef:   agentMessagePartImageRefToProto(value.ImageRef),
	}, nil
}

func inferAgentMessagePartType(input AgentMessagePart) AgentMessagePartType {
	switch {
	case input.ToolCall != nil:
		return AgentMessagePartTypeToolCall
	case input.ToolResult != nil:
		return AgentMessagePartTypeToolResult
	case input.ImageRef != nil:
		return AgentMessagePartTypeImageRef
	case input.JSON != nil:
		return AgentMessagePartTypeJSON
	case strings.TrimSpace(input.Text) != "":
		return AgentMessagePartTypeText
	default:
		return AgentMessagePartTypeUnspecified
	}
}

func agentMessagePartToolCallFromProto(value *proto.AgentMessagePartToolCall) *AgentMessagePartToolCall {
	return convertProtoMessage(value, func(value *proto.AgentMessagePartToolCall) *AgentMessagePartToolCall {
		return &AgentMessagePartToolCall{
			ID:        value.GetId(),
			ToolID:    value.GetToolId(),
			Arguments: mapFromStruct(value.GetArguments()),
		}
	})
}

func agentMessagePartToolCallToProto(value *AgentMessagePartToolCall) (*proto.AgentMessagePartToolCall, error) {
	return convertProtoMessageErr(value, func(value *AgentMessagePartToolCall) (*proto.AgentMessagePartToolCall, error) {
		arguments, err := structFromAny(value.Arguments)
		if err != nil {
			return nil, err
		}
		return &proto.AgentMessagePartToolCall{
			Id:        value.ID,
			ToolId:    value.ToolID,
			Arguments: arguments,
		}, nil
	})
}

func agentMessagePartToolResultFromProto(value *proto.AgentMessagePartToolResult) *AgentMessagePartToolResult {
	return convertProtoMessage(value, func(value *proto.AgentMessagePartToolResult) *AgentMessagePartToolResult {
		return &AgentMessagePartToolResult{
			ToolCallID: value.GetToolCallId(),
			Status:     value.GetStatus(),
			Content:    value.GetContent(),
			Output:     mapFromStruct(value.GetOutput()),
		}
	})
}

func agentMessagePartToolResultToProto(value *AgentMessagePartToolResult) (*proto.AgentMessagePartToolResult, error) {
	return convertProtoMessageErr(value, func(value *AgentMessagePartToolResult) (*proto.AgentMessagePartToolResult, error) {
		output, err := structFromAny(value.Output)
		if err != nil {
			return nil, err
		}
		return &proto.AgentMessagePartToolResult{
			ToolCallId: value.ToolCallID,
			Status:     value.Status,
			Content:    value.Content,
			Output:     output,
		}, nil
	})
}

func agentMessagePartImageRefFromProto(value *proto.AgentMessagePartImageRef) *AgentMessagePartImageRef {
	return convertProtoMessage(value, func(value *proto.AgentMessagePartImageRef) *AgentMessagePartImageRef {
		return &AgentMessagePartImageRef{
			URI:      value.GetUri(),
			MimeType: value.GetMimeType(),
		}
	})
}

func agentMessagePartImageRefToProto(value *AgentMessagePartImageRef) *proto.AgentMessagePartImageRef {
	return convertProtoMessage(value, func(value *AgentMessagePartImageRef) *proto.AgentMessagePartImageRef {
		return &proto.AgentMessagePartImageRef{
			Uri:      value.URI,
			MimeType: value.MimeType,
		}
	})
}

func subjectFromProto(value *proto.SubjectContext) *Subject {
	return convertProtoMessage(value, func(value *proto.SubjectContext) *Subject {
		return &Subject{
			ID:          value.GetId(),
			Email:       value.GetEmail(),
			DisplayName: value.GetDisplayName(),
			Scopes:      cloneStrings(value.GetScopes()),
			Permissions: subjectPermissionsFromProto(value.GetPermissions()),
		}
	})
}

func subjectToProto(value *Subject) *proto.SubjectContext {
	return convertProtoMessage(value, func(value *Subject) *proto.SubjectContext {
		return &proto.SubjectContext{
			Id:          value.ID,
			Email:       value.Email,
			DisplayName: value.DisplayName,
			Scopes:      cloneStrings(value.Scopes),
			Permissions: subjectPermissionsToProto(value.Permissions),
		}
	})
}

func agentPreparedWorkspaceFromProto(value *proto.PreparedAgentWorkspace) *AgentPreparedWorkspace {
	return convertProtoMessage(value, func(value *proto.PreparedAgentWorkspace) *AgentPreparedWorkspace {
		return &AgentPreparedWorkspace{
			Root: value.GetRoot(),
			Cwd:  value.GetCwd(),
		}
	})
}

func agentToolRefFromProto(value *proto.AgentToolRef) AgentToolRef {
	if value == nil {
		return AgentToolRef{}
	}
	return AgentToolRef{
		App:            value.GetApp(),
		Operation:      value.GetOperation(),
		Connection:     value.GetConnection(),
		Instance:       value.GetInstance(),
		Title:          value.GetTitle(),
		Description:    value.GetDescription(),
		CredentialMode: value.GetCredentialMode(),
		System:         value.GetSystem(),
		RunAs:          subjectFromProto(value.GetRunAs()),
	}
}

func agentToolRefsFromProto(values []*proto.AgentToolRef) []AgentToolRef {
	if len(values) == 0 {
		return nil
	}
	out := make([]AgentToolRef, 0, len(values))
	for _, value := range values {
		out = append(out, agentToolRefFromProto(value))
	}
	return out
}

func agentToolRefToProto(value AgentToolRef) *proto.AgentToolRef {
	return &proto.AgentToolRef{
		App:            value.App,
		Operation:      value.Operation,
		Connection:     value.Connection,
		Instance:       value.Instance,
		Title:          value.Title,
		Description:    value.Description,
		CredentialMode: value.CredentialMode,
		System:         value.System,
		RunAs:          subjectToProto(value.RunAs),
	}
}

func agentToolRefsToProto(values []AgentToolRef) []*proto.AgentToolRef {
	if len(values) == 0 {
		return nil
	}
	out := make([]*proto.AgentToolRef, 0, len(values))
	for _, value := range values {
		out = append(out, agentToolRefToProto(value))
	}
	return out
}

func agentToolConfigFromProto(value *proto.AgentToolConfig) AgentToolConfig {
	if value == nil {
		return nil
	}
	switch source := value.GetSource().(type) {
	case *proto.AgentToolConfig_None:
		return &AgentNoTools{}
	case *proto.AgentToolConfig_Catalog:
		catalog := source.Catalog
		if catalog == nil {
			catalog = &proto.AgentCatalogToolConfig{}
		}
		return &AgentCatalogToolConfig{
			Refs:  agentToolRefsFromProto(catalog.GetRefs()),
			Tools: listedAgentToolsFromProto(catalog.GetTools()),
		}
	default:
		return nil
	}
}

func agentProviderCapabilitiesToProto(value *AgentProviderCapabilities) *proto.AgentProviderCapabilities {
	return convertProtoMessage(value, func(value *AgentProviderCapabilities) *proto.AgentProviderCapabilities {
		sources := make([]proto.AgentToolSourceMode, 0, len(value.SupportedToolSources))
		for _, source := range value.SupportedToolSources {
			sources = append(sources, proto.AgentToolSourceMode(source))
		}
		return &proto.AgentProviderCapabilities{
			StreamingText:             value.StreamingText,
			ToolCalls:                 value.ToolCalls,
			ParallelToolCalls:         value.ParallelToolCalls,
			Interactions:              value.Interactions,
			ResumableTurns:            value.ResumableTurns,
			ReasoningSummaries:        value.ReasoningSummaries,
			BoundedListHydration:      value.BoundedListHydration,
			SupportedToolSources:      sources,
			SupportsSessionStart:      value.SupportsSessionStart,
			SupportsPreparedWorkspace: value.SupportsPreparedWorkspace,
		}
	})
}

func agentOutputFromProto(value *proto.AgentOutput) *AgentOutput {
	if value == nil || value.GetKind() == nil {
		return nil
	}
	if value.GetText() != nil {
		return &AgentOutput{Text: &AgentTextOutput{}}
	}
	if structured := value.GetStructured(); structured != nil {
		return &AgentOutput{
			Structured: &AgentStructuredOutput{
				Schema: mapFromStruct(structured.GetSchema()),
			},
		}
	}
	return nil
}

func agentOutputToProto(value *AgentOutput) (*proto.AgentOutput, error) {
	return convertProtoMessageErr(value, func(value *AgentOutput) (*proto.AgentOutput, error) {
		switch {
		case value.Text != nil && value.Structured != nil:
			return nil, fmt.Errorf("agent output cannot set both text and structured")
		case value.Text != nil:
			return &proto.AgentOutput{Kind: &proto.AgentOutput_Text{Text: &proto.AgentTextOutput{}}}, nil
		case value.Structured != nil:
			schema, err := structFromAny(value.Structured.Schema)
			if err != nil {
				return nil, err
			}
			return &proto.AgentOutput{
				Kind: &proto.AgentOutput_Structured{
					Structured: &proto.AgentStructuredOutput{Schema: schema},
				},
			}, nil
		default:
			return nil, nil
		}
	})
}

func applyAgentTurnOutputToProto(out *proto.AgentTurn, value *AgentTurnOutput) error {
	if value == nil {
		return nil
	}
	switch {
	case value.Text != nil && value.Structured != nil:
		return fmt.Errorf("agent turn output cannot set both text and structured")
	case value.Text != nil:
		out.Output = &proto.AgentTurn_Text{Text: &proto.AgentTurnTextOutput{Text: value.Text.Text}}
		return nil
	case value.Structured != nil:
		output, err := structFromAny(value.Structured.Value)
		if err != nil {
			return err
		}
		out.Output = &proto.AgentTurn_Structured{
			Structured: &proto.AgentTurnStructuredOutput{
				Text:  value.Structured.Text,
				Value: output,
			},
		}
		return nil
	default:
		return nil
	}
}

func agentSessionToProto(value *AgentSession) (*proto.AgentSession, error) {
	return convertProtoMessageErr(value, func(value *AgentSession) (*proto.AgentSession, error) {
		metadata, err := structFromAny(value.Metadata)
		if err != nil {
			return nil, err
		}
		return &proto.AgentSession{
			Id:                 value.ID,
			ProviderName:       value.ProviderName,
			Model:              value.Model,
			ClientRef:          value.ClientRef,
			State:              proto.AgentSessionState(value.State),
			Metadata:           metadata,
			CreatedBySubjectId: strings.TrimSpace(value.CreatedBySubjectID),
			CreatedAt:          timestampFromNonZeroTime(value.CreatedAt),
			UpdatedAt:          timestampFromNonZeroTime(value.UpdatedAt),
			LastTurnAt:         timestampFromOptionalTime(value.LastTurnAt),
		}, nil
	})
}

func agentSessionsToProto(values []AgentSession) ([]*proto.AgentSession, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]*proto.AgentSession, 0, len(values))
	for index := range values {
		pbValue, err := agentSessionToProto(&values[index])
		if err != nil {
			return nil, err
		}
		out = append(out, pbValue)
	}
	return out, nil
}

func createAgentProviderSessionRequestFromProto(req *proto.CreateAgentProviderSessionRequest) *CreateAgentProviderSessionRequest {
	if req == nil {
		return &CreateAgentProviderSessionRequest{}
	}
	return &CreateAgentProviderSessionRequest{
		ProviderName:       req.GetProviderName(),
		IdempotencyKey:     req.GetIdempotencyKey(),
		Model:              req.GetModel(),
		ClientRef:          req.GetClientRef(),
		Metadata:           mapFromStruct(req.GetMetadata()),
		Context:            cloneRequestContext(req.GetContext()),
		SessionStart:       agentSessionStartConfigFromProto(req.GetSessionStart()),
		PreparedWorkspace:  agentPreparedWorkspaceFromProto(req.GetPreparedWorkspace()),
		Workspace:          agentWorkspaceFromProto(req.GetWorkspace()),
		Tools:              agentToolConfigFromProto(req.GetTools()),
	}
}

func agentSessionStartConfigFromProto(value *proto.AgentSessionStartConfig) *AgentSessionStartConfig {
	return convertProtoMessage(value, func(value *proto.AgentSessionStartConfig) *AgentSessionStartConfig {
		hooks := make([]AgentSessionStartHook, 0, len(value.GetHooks()))
		for _, hook := range value.GetHooks() {
			hooks = append(hooks, agentSessionStartHookFromProto(hook))
		}
		return &AgentSessionStartConfig{Hooks: hooks}
	})
}

func agentSessionStartHookFromProto(value *proto.AgentSessionStartHook) AgentSessionStartHook {
	if value == nil {
		return AgentSessionStartHook{}
	}
	return AgentSessionStartHook{
		ID:      value.GetId(),
		Type:    value.GetType(),
		Command: append([]string(nil), value.GetCommand()...),
		Cwd:     value.GetCwd(),
		Timeout: value.GetTimeout(),
		Env:     cloneMap(value.GetEnv()),
		Output:  agentSessionStartHookOutputFromProto(value.GetOutput()),
	}
}

func agentSessionStartHookOutputFromProto(value *proto.AgentSessionStartHookOutput) *AgentSessionStartHookOutput {
	return convertProtoMessage(value, func(value *proto.AgentSessionStartHookOutput) *AgentSessionStartHookOutput {
		return &AgentSessionStartHookOutput{
			AdditionalContext: value.GetAdditionalContext(),
			Metadata:          value.GetMetadata(),
		}
	})
}

func getAgentProviderSessionRequestFromProto(req *proto.GetAgentProviderSessionRequest) *GetAgentProviderSessionRequest {
	if req == nil {
		return &GetAgentProviderSessionRequest{}
	}
	return &GetAgentProviderSessionRequest{
		ProviderName: req.GetProviderName(),
		SessionID:    req.GetSessionId(),
		Context:      cloneRequestContext(req.GetContext()),
	}
}

func listAgentProviderSessionsRequestFromProto(req *proto.ListAgentProviderSessionsRequest) *ListAgentProviderSessionsRequest {
	if req == nil {
		return &ListAgentProviderSessionsRequest{}
	}
	return &ListAgentProviderSessionsRequest{
		ProviderName: req.GetProviderName(),
		Context:      cloneRequestContext(req.GetContext()),
		SessionIDs:   append([]string(nil), req.GetSessionIds()...),
		State:        AgentSessionState(req.GetState()),
		Limit:        req.GetLimit(),
		SummaryOnly:  req.GetSummaryOnly(),
	}
}

func listAgentProviderSessionsResponseToProto(value *ListAgentProviderSessionsResponse) (*proto.ListAgentProviderSessionsResponse, error) {
	return convertProtoMessageErr(value, func(value *ListAgentProviderSessionsResponse) (*proto.ListAgentProviderSessionsResponse, error) {
		sessions, err := agentSessionsToProto(value.Sessions)
		if err != nil {
			return nil, err
		}
		return &proto.ListAgentProviderSessionsResponse{Sessions: sessions}, nil
	})
}

func updateAgentProviderSessionRequestFromProto(req *proto.UpdateAgentProviderSessionRequest) *UpdateAgentProviderSessionRequest {
	if req == nil {
		return &UpdateAgentProviderSessionRequest{}
	}
	return &UpdateAgentProviderSessionRequest{
		ProviderName: req.GetProviderName(),
		SessionID:    req.GetSessionId(),
		ClientRef:    req.GetClientRef(),
		State:        AgentSessionState(req.GetState()),
		Metadata:     mapFromStruct(req.GetMetadata()),
		Context:      cloneRequestContext(req.GetContext()),
	}
}

func agentTurnToProto(value *AgentTurn) (*proto.AgentTurn, error) {
	return convertProtoMessageErr(value, func(value *AgentTurn) (*proto.AgentTurn, error) {
		messages, err := agentMessagesToProto(value.Messages)
		if err != nil {
			return nil, err
		}
		out := &proto.AgentTurn{
			Id:                 value.ID,
			SessionId:          value.SessionID,
			ProviderName:       value.ProviderName,
			Model:              value.Model,
			Status:             proto.AgentExecutionStatus(value.Status),
			Messages:           messages,
			StatusMessage:      value.StatusMessage,
			CreatedBySubjectId: strings.TrimSpace(value.CreatedBySubjectID),
			CreatedAt:          timestampFromNonZeroTime(value.CreatedAt),
			StartedAt:          timestampFromOptionalTime(value.StartedAt),
			CompletedAt:        timestampFromOptionalTime(value.CompletedAt),
			ExecutionRef:       value.ExecutionRef,
		}
		if err := applyAgentTurnOutputToProto(out, value.Output); err != nil {
			return nil, err
		}
		return out, nil
	})
}

func agentTurnsToProto(values []AgentTurn) ([]*proto.AgentTurn, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]*proto.AgentTurn, 0, len(values))
	for index := range values {
		pbValue, err := agentTurnToProto(&values[index])
		if err != nil {
			return nil, err
		}
		out = append(out, pbValue)
	}
	return out, nil
}

func createAgentProviderTurnRequestFromProto(req *proto.CreateAgentProviderTurnRequest) (*CreateAgentProviderTurnRequest, error) {
	if req == nil {
		return nil, InvalidArgument("agent create turn request is required")
	}
	if req.GetTimeoutSeconds() < 0 {
		return nil, InvalidArgument("agent create turn timeout_seconds must not be negative")
	}
	return &CreateAgentProviderTurnRequest{
		ProviderName:       req.GetProviderName(),
		TurnID:             req.GetTurnId(),
		SessionID:          req.GetSessionId(),
		IdempotencyKey:     req.GetIdempotencyKey(),
		Model:              req.GetModel(),
		Messages:           agentMessagesFromProto(req.GetMessages()),
		Output:             agentOutputFromProto(req.GetOutput()),
		Metadata:           mapFromStruct(req.GetMetadata()),
		ExecutionRef:       req.GetExecutionRef(),
		ModelOptions:       mapFromStruct(req.GetModelOptions()),
		Context:            cloneRequestContext(req.GetContext()),
		TimeoutSeconds:     req.GetTimeoutSeconds(),
	}, nil
}

func getAgentProviderTurnRequestFromProto(req *proto.GetAgentProviderTurnRequest) *GetAgentProviderTurnRequest {
	if req == nil {
		return &GetAgentProviderTurnRequest{}
	}
	return &GetAgentProviderTurnRequest{
		ProviderName: req.GetProviderName(),
		TurnID:       req.GetTurnId(),
		Context:      cloneRequestContext(req.GetContext()),
	}
}

func listAgentProviderTurnsRequestFromProto(req *proto.ListAgentProviderTurnsRequest) *ListAgentProviderTurnsRequest {
	if req == nil {
		return &ListAgentProviderTurnsRequest{}
	}
	return &ListAgentProviderTurnsRequest{
		ProviderName: req.GetProviderName(),
		SessionID:    req.GetSessionId(),
		Context:      cloneRequestContext(req.GetContext()),
		TurnIDs:      append([]string(nil), req.GetTurnIds()...),
		Status:       AgentExecutionStatus(req.GetStatus()),
		Limit:        req.GetLimit(),
		SummaryOnly:  req.GetSummaryOnly(),
	}
}

func listAgentProviderTurnsResponseToProto(value *ListAgentProviderTurnsResponse) (*proto.ListAgentProviderTurnsResponse, error) {
	return convertProtoMessageErr(value, func(value *ListAgentProviderTurnsResponse) (*proto.ListAgentProviderTurnsResponse, error) {
		turns, err := agentTurnsToProto(value.Turns)
		if err != nil {
			return nil, err
		}
		return &proto.ListAgentProviderTurnsResponse{Turns: turns}, nil
	})
}

func cancelAgentProviderTurnRequestFromProto(req *proto.CancelAgentProviderTurnRequest) *CancelAgentProviderTurnRequest {
	if req == nil {
		return &CancelAgentProviderTurnRequest{}
	}
	return &CancelAgentProviderTurnRequest{
		ProviderName: req.GetProviderName(),
		TurnID:       req.GetTurnId(),
		Reason:       req.GetReason(),
		Context:      cloneRequestContext(req.GetContext()),
	}
}

func agentTurnEventToProto(value AgentTurnEvent) (*proto.AgentTurnEvent, error) {
	data, err := structFromAny(value.Data)
	if err != nil {
		return nil, err
	}
	display, err := agentTurnDisplayToProto(value.Display)
	if err != nil {
		return nil, err
	}
	return &proto.AgentTurnEvent{
		Id:         value.ID,
		TurnId:     value.TurnID,
		Seq:        value.Seq,
		Type:       value.Type,
		Source:     value.Source,
		Visibility: value.Visibility,
		Data:       data,
		CreatedAt:  timestampFromNonZeroTime(value.CreatedAt),
		Display:    display,
	}, nil
}

func agentTurnEventsToProto(values []AgentTurnEvent) ([]*proto.AgentTurnEvent, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]*proto.AgentTurnEvent, 0, len(values))
	for _, value := range values {
		pbValue, err := agentTurnEventToProto(value)
		if err != nil {
			return nil, err
		}
		out = append(out, pbValue)
	}
	return out, nil
}

func agentTurnDisplayToProto(value *AgentTurnDisplay) (*proto.AgentTurnDisplay, error) {
	return convertProtoMessageErr(value, func(value *AgentTurnDisplay) (*proto.AgentTurnDisplay, error) {
		input, err := optionalValueFromAny(value.Input)
		if err != nil {
			return nil, err
		}
		output, err := optionalValueFromAny(value.Output)
		if err != nil {
			return nil, err
		}
		errorValue, err := optionalValueFromAny(value.Error)
		if err != nil {
			return nil, err
		}
		return &proto.AgentTurnDisplay{
			Kind:      value.Kind,
			Phase:     value.Phase,
			Text:      value.Text,
			Label:     value.Label,
			Ref:       value.Ref,
			ParentRef: value.ParentRef,
			Input:     input,
			Output:    output,
			Error:     errorValue,
			Action:    value.Action,
			Format:    value.Format,
			Language:  value.Language,
		}, nil
	})
}

func listAgentProviderTurnEventsRequestFromProto(req *proto.ListAgentProviderTurnEventsRequest) *ListAgentProviderTurnEventsRequest {
	if req == nil {
		return &ListAgentProviderTurnEventsRequest{}
	}
	return &ListAgentProviderTurnEventsRequest{
		ProviderName: req.GetProviderName(),
		TurnID:       req.GetTurnId(),
		AfterSeq:     req.GetAfterSeq(),
		Limit:        req.GetLimit(),
		Context:      cloneRequestContext(req.GetContext()),
	}
}

func listAgentProviderTurnEventsResponseToProto(value *ListAgentProviderTurnEventsResponse) (*proto.ListAgentProviderTurnEventsResponse, error) {
	return convertProtoMessageErr(value, func(value *ListAgentProviderTurnEventsResponse) (*proto.ListAgentProviderTurnEventsResponse, error) {
		events, err := agentTurnEventsToProto(value.Events)
		if err != nil {
			return nil, err
		}
		return &proto.ListAgentProviderTurnEventsResponse{Events: events}, nil
	})
}

func getAgentProviderInteractionRequestFromProto(req *proto.GetAgentProviderInteractionRequest) *GetAgentProviderInteractionRequest {
	if req == nil {
		return &GetAgentProviderInteractionRequest{}
	}
	return &GetAgentProviderInteractionRequest{
		InteractionID: req.GetInteractionId(),
		Context:       cloneRequestContext(req.GetContext()),
	}
}

func listAgentProviderInteractionsRequestFromProto(req *proto.ListAgentProviderInteractionsRequest) *ListAgentProviderInteractionsRequest {
	if req == nil {
		return &ListAgentProviderInteractionsRequest{}
	}
	return &ListAgentProviderInteractionsRequest{
		ProviderName: req.GetProviderName(),
		TurnID:       req.GetTurnId(),
		Context:      cloneRequestContext(req.GetContext()),
	}
}

func agentInteractionToProto(value *AgentInteraction) (*proto.AgentInteraction, error) {
	return convertProtoMessageErr(value, func(value *AgentInteraction) (*proto.AgentInteraction, error) {
		request, err := structFromAny(value.Request)
		if err != nil {
			return nil, err
		}
		resolution, err := structFromAny(value.Resolution)
		if err != nil {
			return nil, err
		}
		return &proto.AgentInteraction{
			Id:         value.ID,
			Type:       proto.AgentInteractionType(value.Type),
			State:      proto.AgentInteractionState(value.State),
			Title:      value.Title,
			Prompt:     value.Prompt,
			Request:    request,
			Resolution: resolution,
			CreatedAt:  timestampFromNonZeroTime(value.CreatedAt),
			ResolvedAt: timestampFromOptionalTime(value.ResolvedAt),
			TurnId:     value.TurnID,
			SessionId:  value.SessionID,
		}, nil
	})
}

func agentInteractionsToProto(values []AgentInteraction) ([]*proto.AgentInteraction, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]*proto.AgentInteraction, 0, len(values))
	for index := range values {
		pbValue, err := agentInteractionToProto(&values[index])
		if err != nil {
			return nil, err
		}
		out = append(out, pbValue)
	}
	return out, nil
}

func listAgentProviderInteractionsResponseToProto(value *ListAgentProviderInteractionsResponse) (*proto.ListAgentProviderInteractionsResponse, error) {
	return convertProtoMessageErr(value, func(value *ListAgentProviderInteractionsResponse) (*proto.ListAgentProviderInteractionsResponse, error) {
		interactions, err := agentInteractionsToProto(value.Interactions)
		if err != nil {
			return nil, err
		}
		return &proto.ListAgentProviderInteractionsResponse{Interactions: interactions}, nil
	})
}

func resolveAgentProviderInteractionRequestFromProto(req *proto.ResolveAgentProviderInteractionRequest) *ResolveAgentProviderInteractionRequest {
	if req == nil {
		return &ResolveAgentProviderInteractionRequest{}
	}
	return &ResolveAgentProviderInteractionRequest{
		ProviderName:  req.GetProviderName(),
		TurnID:        req.GetTurnId(),
		InteractionID: req.GetInteractionId(),
		Resolution:    mapFromStruct(req.GetResolution()),
		Context:       cloneRequestContext(req.GetContext()),
	}
}

func listedAgentToolFromProto(value *proto.ListedAgentTool) ListedAgentTool {
	if value == nil {
		return ListedAgentTool{}
	}
	ref := agentToolRefFromProto(value.GetRef())
	var refPtr *AgentToolRef
	if value.GetRef() != nil {
		refPtr = &ref
	}
	return ListedAgentTool{
		ID:           value.GetId(),
		MCPName:      value.GetMcpName(),
		Title:        value.GetTitle(),
		Description:  value.GetDescription(),
		InputSchema:  value.GetInputSchema(),
		OutputSchema: value.GetOutputSchema(),
		Annotations:  agentToolAnnotationsFromProto(value.GetAnnotations()),
		Ref:          refPtr,
		Tags:         append([]string(nil), value.GetTags()...),
		SearchText:   value.GetSearchText(),
	}
}

func listedAgentToolsFromProto(values []*proto.ListedAgentTool) []ListedAgentTool {
	if len(values) == 0 {
		return nil
	}
	out := make([]ListedAgentTool, 0, len(values))
	for _, value := range values {
		out = append(out, listedAgentToolFromProto(value))
	}
	return out
}

func agentToolAnnotationsFromProto(value *proto.OperationAnnotations) *AgentToolAnnotations {
	return convertProtoMessage(value, func(value *proto.OperationAnnotations) *AgentToolAnnotations {
		return &AgentToolAnnotations{
			ReadOnlyHint:    value.ReadOnlyHint,
			IdempotentHint:  value.IdempotentHint,
			DestructiveHint: value.DestructiveHint,
			OpenWorldHint:   value.OpenWorldHint,
		}
	})
}

func optionalValueFromAny(value any) (*structpb.Value, error) {
	if value == nil {
		return nil, nil
	}
	return valueFromAny(value)
}
