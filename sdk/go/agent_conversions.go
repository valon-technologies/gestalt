package gestalt

import (
	"fmt"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
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
	if value == nil {
		return nil
	}
	return &AgentMessagePartToolCall{
		ID:        value.GetId(),
		ToolID:    value.GetToolId(),
		Arguments: mapFromStruct(value.GetArguments()),
	}
}

func agentMessagePartToolCallToProto(value *AgentMessagePartToolCall) (*proto.AgentMessagePartToolCall, error) {
	if value == nil {
		return nil, nil
	}
	arguments, err := structFromAny(value.Arguments)
	if err != nil {
		return nil, err
	}
	return &proto.AgentMessagePartToolCall{
		Id:        value.ID,
		ToolId:    value.ToolID,
		Arguments: arguments,
	}, nil
}

func agentMessagePartToolResultFromProto(value *proto.AgentMessagePartToolResult) *AgentMessagePartToolResult {
	if value == nil {
		return nil
	}
	return &AgentMessagePartToolResult{
		ToolCallID: value.GetToolCallId(),
		Status:     value.GetStatus(),
		Content:    value.GetContent(),
		Output:     mapFromStruct(value.GetOutput()),
	}
}

func agentMessagePartToolResultToProto(value *AgentMessagePartToolResult) (*proto.AgentMessagePartToolResult, error) {
	if value == nil {
		return nil, nil
	}
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
}

func agentMessagePartImageRefFromProto(value *proto.AgentMessagePartImageRef) *AgentMessagePartImageRef {
	if value == nil {
		return nil
	}
	return &AgentMessagePartImageRef{
		URI:      value.GetUri(),
		MimeType: value.GetMimeType(),
	}
}

func agentMessagePartImageRefToProto(value *AgentMessagePartImageRef) *proto.AgentMessagePartImageRef {
	if value == nil {
		return nil
	}
	return &proto.AgentMessagePartImageRef{
		Uri:      value.URI,
		MimeType: value.MimeType,
	}
}

func subjectFromProto(value *proto.SubjectContext) *Subject {
	if value == nil {
		return nil
	}
	return &Subject{
		ID:                  value.GetId(),
		CredentialSubjectID: value.GetCredentialSubjectId(),
		Email:               value.GetEmail(),
		DisplayName:         value.GetDisplayName(),
		Scopes:              cloneStrings(value.GetScopes()),
		Permissions:         subjectPermissionsFromProto(value.GetPermissions()),
	}
}

func subjectToProto(value *Subject) *proto.SubjectContext {
	if value == nil {
		return nil
	}
	return &proto.SubjectContext{
		Id:                  value.ID,
		CredentialSubjectId: value.CredentialSubjectID,
		Email:               value.Email,
		DisplayName:         value.DisplayName,
		Scopes:              cloneStrings(value.Scopes),
		Permissions:         subjectPermissionsToProto(value.Permissions),
	}
}

func agentPreparedWorkspaceFromProto(value *proto.PreparedAgentWorkspace) *AgentPreparedWorkspace {
	if value == nil {
		return nil
	}
	return &AgentPreparedWorkspace{
		Root: value.GetRoot(),
		Cwd:  value.GetCwd(),
	}
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

func agentToolRefPtrFromProto(value *proto.AgentToolRef) *AgentToolRef {
	if value == nil {
		return nil
	}
	out := agentToolRefFromProto(value)
	return &out
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

func agentToolConfigToProto(value AgentToolConfig) *proto.AgentToolConfig {
	if value == nil {
		return nil
	}
	switch toolConfig := value.(type) {
	case *AgentCatalogToolConfig:
		if toolConfig == nil {
			return nil
		}
		return &proto.AgentToolConfig{Source: &proto.AgentToolConfig_Catalog{
			Catalog: &proto.AgentCatalogToolConfig{
				Refs:  agentToolRefsToProto(toolConfig.Refs),
				Tools: listedAgentToolsToProto(toolConfig.Tools),
			},
		}}
	case *AgentNoTools:
		if toolConfig == nil {
			return nil
		}
		return &proto.AgentToolConfig{Source: &proto.AgentToolConfig_None{None: &proto.AgentNoTools{}}}
	default:
		return nil
	}
}

func agentProviderCapabilitiesToProto(value *AgentProviderCapabilities) *proto.AgentProviderCapabilities {
	if value == nil {
		return nil
	}
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
	if value == nil {
		return nil, nil
	}
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
}

func agentTurnOutputFromProto(value *proto.AgentTurn) *AgentTurnOutput {
	if value == nil || value.GetOutput() == nil {
		return nil
	}
	if text := value.GetText(); text != nil {
		return &AgentTurnOutput{Text: &AgentTurnTextOutput{Text: text.GetText()}}
	}
	if structured := value.GetStructured(); structured != nil {
		return &AgentTurnOutput{
			Structured: &AgentTurnStructuredOutput{
				Text:  structured.GetText(),
				Value: mapFromStruct(structured.GetValue()),
			},
		}
	}
	return nil
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
	if value == nil {
		return nil, nil
	}
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

func agentSessionFromProto(value *proto.AgentSession) *AgentSession {
	if value == nil {
		return nil
	}
	return &AgentSession{
		ID:                 value.GetId(),
		ProviderName:       value.GetProviderName(),
		Model:              value.GetModel(),
		ClientRef:          value.GetClientRef(),
		State:              AgentSessionState(value.GetState()),
		Metadata:           mapFromStruct(value.GetMetadata()),
		CreatedBySubjectID: strings.TrimSpace(value.GetCreatedBySubjectId()),
		CreatedAt:          timeFromTimestamp(value.GetCreatedAt()),
		UpdatedAt:          timeFromTimestamp(value.GetUpdatedAt()),
		LastTurnAt:         timePtrFromTimestampUnchecked(value.GetLastTurnAt()),
	}
}

func agentSessionsFromProto(values []*proto.AgentSession) []AgentSession {
	if len(values) == 0 {
		return nil
	}
	out := make([]AgentSession, 0, len(values))
	for _, value := range values {
		session := agentSessionFromProto(value)
		if session == nil {
			continue
		}
		out = append(out, *session)
	}
	return out
}

func listAgentSessionsResponseFromProto(value *proto.ListAgentProviderSessionsResponse) *ListAgentSessionsResponse {
	if value == nil {
		return nil
	}
	return &ListAgentSessionsResponse{Sessions: agentSessionsFromProto(value.GetSessions())}
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
		CreatedBySubjectID: strings.TrimSpace(req.GetCreatedBySubjectId()),
		Subject:            subjectFromProto(req.GetSubject()),
		Context:            cloneRequestContext(req.GetContext()),
		SessionStart:       agentSessionStartConfigFromProto(req.GetSessionStart()),
		PreparedWorkspace:  agentPreparedWorkspaceFromProto(req.GetPreparedWorkspace()),
		Workspace:          agentWorkspaceFromProto(req.GetWorkspace()),
		Tools:              agentToolConfigFromProto(req.GetTools()),
	}
}

func agentSessionStartConfigFromProto(value *proto.AgentSessionStartConfig) *AgentSessionStartConfig {
	if value == nil {
		return nil
	}
	hooks := make([]AgentSessionStartHook, 0, len(value.GetHooks()))
	for _, hook := range value.GetHooks() {
		hooks = append(hooks, agentSessionStartHookFromProto(hook))
	}
	return &AgentSessionStartConfig{Hooks: hooks}
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
		Env:     cloneAgentStringMap(value.GetEnv()),
		Output:  agentSessionStartHookOutputFromProto(value.GetOutput()),
	}
}

func agentSessionStartHookOutputFromProto(value *proto.AgentSessionStartHookOutput) *AgentSessionStartHookOutput {
	if value == nil {
		return nil
	}
	return &AgentSessionStartHookOutput{
		AdditionalContext: value.GetAdditionalContext(),
		Metadata:          value.GetMetadata(),
	}
}

func getAgentProviderSessionRequestFromProto(req *proto.GetAgentProviderSessionRequest) *GetAgentProviderSessionRequest {
	if req == nil {
		return &GetAgentProviderSessionRequest{}
	}
	return &GetAgentProviderSessionRequest{
		ProviderName: req.GetProviderName(),
		SessionID:    req.GetSessionId(),
		Subject:      subjectFromProto(req.GetSubject()),
		Context:      cloneRequestContext(req.GetContext()),
	}
}

func listAgentProviderSessionsRequestFromProto(req *proto.ListAgentProviderSessionsRequest) *ListAgentProviderSessionsRequest {
	if req == nil {
		return &ListAgentProviderSessionsRequest{}
	}
	return &ListAgentProviderSessionsRequest{
		ProviderName: req.GetProviderName(),
		Subject:      subjectFromProto(req.GetSubject()),
		Context:      cloneRequestContext(req.GetContext()),
		SessionIDs:   append([]string(nil), req.GetSessionIds()...),
		State:        AgentSessionState(req.GetState()),
		Limit:        req.GetLimit(),
		SummaryOnly:  req.GetSummaryOnly(),
	}
}

func listAgentProviderSessionsResponseToProto(value *ListAgentProviderSessionsResponse) (*proto.ListAgentProviderSessionsResponse, error) {
	if value == nil {
		return nil, nil
	}
	sessions, err := agentSessionsToProto(value.Sessions)
	if err != nil {
		return nil, err
	}
	return &proto.ListAgentProviderSessionsResponse{Sessions: sessions}, nil
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
		Subject:      subjectFromProto(req.GetSubject()),
		Context:      cloneRequestContext(req.GetContext()),
	}
}

func agentTurnToProto(value *AgentTurn) (*proto.AgentTurn, error) {
	if value == nil {
		return nil, nil
	}
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

func agentTurnFromProto(value *proto.AgentTurn) *AgentTurn {
	if value == nil {
		return nil
	}
	return &AgentTurn{
		ID:                 value.GetId(),
		SessionID:          value.GetSessionId(),
		ProviderName:       value.GetProviderName(),
		Model:              value.GetModel(),
		Status:             AgentExecutionStatus(value.GetStatus()),
		Messages:           agentMessagesFromProto(value.GetMessages()),
		Output:             agentTurnOutputFromProto(value),
		StatusMessage:      value.GetStatusMessage(),
		CreatedBySubjectID: strings.TrimSpace(value.GetCreatedBySubjectId()),
		CreatedAt:          timeFromTimestamp(value.GetCreatedAt()),
		StartedAt:          timePtrFromTimestampUnchecked(value.GetStartedAt()),
		CompletedAt:        timePtrFromTimestampUnchecked(value.GetCompletedAt()),
		ExecutionRef:       value.GetExecutionRef(),
	}
}

func agentTurnsFromProto(values []*proto.AgentTurn) []AgentTurn {
	if len(values) == 0 {
		return nil
	}
	out := make([]AgentTurn, 0, len(values))
	for _, value := range values {
		turn := agentTurnFromProto(value)
		if turn == nil {
			continue
		}
		out = append(out, *turn)
	}
	return out
}

func listAgentTurnsResponseFromProto(value *proto.ListAgentProviderTurnsResponse) *ListAgentTurnsResponse {
	if value == nil {
		return nil
	}
	return &ListAgentTurnsResponse{Turns: agentTurnsFromProto(value.GetTurns())}
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
		CreatedBySubjectID: strings.TrimSpace(req.GetCreatedBySubjectId()),
		ExecutionRef:       req.GetExecutionRef(),
		Subject:            subjectFromProto(req.GetSubject()),
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
		Subject:      subjectFromProto(req.GetSubject()),
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
		Subject:      subjectFromProto(req.GetSubject()),
		Context:      cloneRequestContext(req.GetContext()),
		TurnIDs:      append([]string(nil), req.GetTurnIds()...),
		Status:       AgentExecutionStatus(req.GetStatus()),
		Limit:        req.GetLimit(),
		SummaryOnly:  req.GetSummaryOnly(),
	}
}

func listAgentProviderTurnsResponseToProto(value *ListAgentProviderTurnsResponse) (*proto.ListAgentProviderTurnsResponse, error) {
	if value == nil {
		return nil, nil
	}
	turns, err := agentTurnsToProto(value.Turns)
	if err != nil {
		return nil, err
	}
	return &proto.ListAgentProviderTurnsResponse{Turns: turns}, nil
}

func cancelAgentProviderTurnRequestFromProto(req *proto.CancelAgentProviderTurnRequest) *CancelAgentProviderTurnRequest {
	if req == nil {
		return &CancelAgentProviderTurnRequest{}
	}
	return &CancelAgentProviderTurnRequest{
		ProviderName: req.GetProviderName(),
		TurnID:       req.GetTurnId(),
		Reason:       req.GetReason(),
		Subject:      subjectFromProto(req.GetSubject()),
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

func agentTurnDisplayFromProto(value *proto.AgentTurnDisplay) *AgentTurnDisplay {
	if value == nil {
		return nil
	}
	return &AgentTurnDisplay{
		Kind:      value.GetKind(),
		Phase:     value.GetPhase(),
		Text:      value.GetText(),
		Label:     value.GetLabel(),
		Ref:       value.GetRef(),
		ParentRef: value.GetParentRef(),
		Input:     anyFromValue(value.GetInput()),
		Output:    anyFromValue(value.GetOutput()),
		Error:     anyFromValue(value.GetError()),
		Action:    value.GetAction(),
		Format:    value.GetFormat(),
		Language:  value.GetLanguage(),
	}
}

func agentTurnEventFromProto(value *proto.AgentTurnEvent) AgentTurnEvent {
	if value == nil {
		return AgentTurnEvent{}
	}
	return AgentTurnEvent{
		ID:         value.GetId(),
		TurnID:     value.GetTurnId(),
		Seq:        value.GetSeq(),
		Type:       value.GetType(),
		Source:     value.GetSource(),
		Visibility: value.GetVisibility(),
		Data:       mapFromStruct(value.GetData()),
		CreatedAt:  timeFromTimestamp(value.GetCreatedAt()),
		Display:    agentTurnDisplayFromProto(value.GetDisplay()),
	}
}

func agentTurnEventsFromProto(values []*proto.AgentTurnEvent) []AgentTurnEvent {
	if len(values) == 0 {
		return nil
	}
	out := make([]AgentTurnEvent, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		out = append(out, agentTurnEventFromProto(value))
	}
	return out
}

func listAgentTurnEventsResponseFromProto(value *proto.ListAgentProviderTurnEventsResponse) *ListAgentTurnEventsResponse {
	if value == nil {
		return nil
	}
	return &ListAgentTurnEventsResponse{Events: agentTurnEventsFromProto(value.GetEvents())}
}

func agentTurnDisplayToProto(value *AgentTurnDisplay) (*proto.AgentTurnDisplay, error) {
	if value == nil {
		return nil, nil
	}
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
		Subject:      subjectFromProto(req.GetSubject()),
		Context:      cloneRequestContext(req.GetContext()),
	}
}

func listAgentProviderTurnEventsResponseToProto(value *ListAgentProviderTurnEventsResponse) (*proto.ListAgentProviderTurnEventsResponse, error) {
	if value == nil {
		return nil, nil
	}
	events, err := agentTurnEventsToProto(value.Events)
	if err != nil {
		return nil, err
	}
	return &proto.ListAgentProviderTurnEventsResponse{Events: events}, nil
}

func getAgentProviderInteractionRequestFromProto(req *proto.GetAgentProviderInteractionRequest) *GetAgentProviderInteractionRequest {
	if req == nil {
		return &GetAgentProviderInteractionRequest{}
	}
	return &GetAgentProviderInteractionRequest{
		InteractionID: req.GetInteractionId(),
		Subject:       subjectFromProto(req.GetSubject()),
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
		Subject:      subjectFromProto(req.GetSubject()),
		Context:      cloneRequestContext(req.GetContext()),
	}
}

func agentInteractionToProto(value *AgentInteraction) (*proto.AgentInteraction, error) {
	if value == nil {
		return nil, nil
	}
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

func agentInteractionFromProto(value *proto.AgentInteraction) *AgentInteraction {
	if value == nil {
		return nil
	}
	return &AgentInteraction{
		ID:         value.GetId(),
		Type:       AgentInteractionType(value.GetType()),
		State:      AgentInteractionState(value.GetState()),
		Title:      value.GetTitle(),
		Prompt:     value.GetPrompt(),
		Request:    mapFromStruct(value.GetRequest()),
		Resolution: mapFromStruct(value.GetResolution()),
		CreatedAt:  timeFromTimestamp(value.GetCreatedAt()),
		ResolvedAt: timePtrFromTimestampUnchecked(value.GetResolvedAt()),
		TurnID:     value.GetTurnId(),
		SessionID:  value.GetSessionId(),
	}
}

func agentInteractionsFromProto(values []*proto.AgentInteraction) []AgentInteraction {
	if len(values) == 0 {
		return nil
	}
	out := make([]AgentInteraction, 0, len(values))
	for _, value := range values {
		interaction := agentInteractionFromProto(value)
		if interaction == nil {
			continue
		}
		out = append(out, *interaction)
	}
	return out
}

func listAgentInteractionsResponseFromProto(value *proto.ListAgentProviderInteractionsResponse) *ListAgentInteractionsResponse {
	if value == nil {
		return nil
	}
	return &ListAgentInteractionsResponse{Interactions: agentInteractionsFromProto(value.GetInteractions())}
}

func listAgentProviderInteractionsResponseToProto(value *ListAgentProviderInteractionsResponse) (*proto.ListAgentProviderInteractionsResponse, error) {
	if value == nil {
		return nil, nil
	}
	interactions, err := agentInteractionsToProto(value.Interactions)
	if err != nil {
		return nil, err
	}
	return &proto.ListAgentProviderInteractionsResponse{Interactions: interactions}, nil
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
		Subject:       subjectFromProto(req.GetSubject()),
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

func listedAgentToolToProto(value ListedAgentTool) *proto.ListedAgentTool {
	return &proto.ListedAgentTool{
		Id:           value.ID,
		McpName:      value.MCPName,
		Title:        value.Title,
		Description:  value.Description,
		InputSchema:  value.InputSchema,
		OutputSchema: value.OutputSchema,
		Annotations:  agentToolAnnotationsToProto(value.Annotations),
		Ref:          agentToolRefPtrToProto(value.Ref),
		Tags:         append([]string(nil), value.Tags...),
		SearchText:   value.SearchText,
	}
}

func listedAgentToolsToProto(values []ListedAgentTool) []*proto.ListedAgentTool {
	if len(values) == 0 {
		return nil
	}
	out := make([]*proto.ListedAgentTool, 0, len(values))
	for _, value := range values {
		out = append(out, listedAgentToolToProto(value))
	}
	return out
}

func agentToolAnnotationsFromProto(value *proto.OperationAnnotations) *AgentToolAnnotations {
	if value == nil {
		return nil
	}
	return &AgentToolAnnotations{
		ReadOnlyHint:    value.ReadOnlyHint,
		IdempotentHint:  value.IdempotentHint,
		DestructiveHint: value.DestructiveHint,
		OpenWorldHint:   value.OpenWorldHint,
	}
}

func agentToolAnnotationsToProto(value *AgentToolAnnotations) *proto.OperationAnnotations {
	if value == nil {
		return nil
	}
	return &proto.OperationAnnotations{
		ReadOnlyHint:    value.ReadOnlyHint,
		IdempotentHint:  value.IdempotentHint,
		DestructiveHint: value.DestructiveHint,
		OpenWorldHint:   value.OpenWorldHint,
	}
}

func agentToolRefPtrToProto(value *AgentToolRef) *proto.AgentToolRef {
	if value == nil {
		return nil
	}
	return agentToolRefToProto(*value)
}

func timePtrFromTimestampUnchecked(value *timestamppb.Timestamp) *time.Time {
	if value == nil {
		return nil
	}
	out := value.AsTime()
	return &out
}

func optionalValueFromAny(value any) (*structpb.Value, error) {
	if value == nil {
		return nil, nil
	}
	return valueFromAny(value)
}

func cloneAgentStringMap(value map[string]string) map[string]string {
	if len(value) == 0 {
		return nil
	}
	out := make(map[string]string, len(value))
	for key, item := range value {
		out[key] = item
	}
	return out
}
