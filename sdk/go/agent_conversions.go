package gestalt

import (
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/internal/gen/v1"
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

func agentMessagePtrsFromProto(values []*proto.AgentMessage) []*AgentMessage {
	if len(values) == 0 {
		return nil
	}
	out := make([]*AgentMessage, 0, len(values))
	for _, value := range values {
		native := agentMessageFromProto(value)
		out = append(out, &native)
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

func agentActorFromProto(value *proto.AgentActor) *AgentActor {
	if value == nil {
		return nil
	}
	return &AgentActor{
		SubjectID:   value.GetSubjectId(),
		SubjectKind: value.GetSubjectKind(),
		DisplayName: value.GetDisplayName(),
		AuthSource:  value.GetAuthSource(),
	}
}

func agentActorToProto(value *AgentActor) *proto.AgentActor {
	if value == nil {
		return nil
	}
	return &proto.AgentActor{
		SubjectId:   value.SubjectID,
		SubjectKind: value.SubjectKind,
		DisplayName: value.DisplayName,
		AuthSource:  value.AuthSource,
	}
}

func agentSubjectContextFromProto(value *proto.AgentSubjectContext) *AgentSubjectContext {
	if value == nil {
		return nil
	}
	return &AgentSubjectContext{
		SubjectID:           value.GetSubjectId(),
		SubjectKind:         value.GetSubjectKind(),
		CredentialSubjectID: value.GetCredentialSubjectId(),
		DisplayName:         value.GetDisplayName(),
		AuthSource:          value.GetAuthSource(),
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

func resolvedAgentToolFromProto(value *proto.ResolvedAgentTool) ResolvedAgentTool {
	if value == nil {
		return ResolvedAgentTool{}
	}
	return ResolvedAgentTool{
		ID:               value.GetId(),
		Name:             value.GetName(),
		Description:      value.GetDescription(),
		ParametersSchema: mapFromStruct(value.GetParametersSchema()),
	}
}

func resolvedAgentToolsFromProto(values []*proto.ResolvedAgentTool) []ResolvedAgentTool {
	if len(values) == 0 {
		return nil
	}
	out := make([]ResolvedAgentTool, 0, len(values))
	for _, value := range values {
		out = append(out, resolvedAgentToolFromProto(value))
	}
	return out
}

func agentToolRefFromProto(value *proto.AgentToolRef) AgentToolRef {
	if value == nil {
		return AgentToolRef{}
	}
	return AgentToolRef{
		Plugin:                value.GetPlugin(),
		Operation:             value.GetOperation(),
		Connection:            value.GetConnection(),
		Instance:              value.GetInstance(),
		Title:                 value.GetTitle(),
		Description:           value.GetDescription(),
		System:                value.GetSystem(),
		RunAs:                 agentRunAsSubjectFromProto(value.GetRunAs()),
		RunAsExternalIdentity: externalIdentityFromProto(value.GetRunAsExternalIdentity()),
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

func agentToolRefPtrsFromProto(values []*proto.AgentToolRef) []*AgentToolRef {
	if len(values) == 0 {
		return nil
	}
	out := make([]*AgentToolRef, 0, len(values))
	for _, value := range values {
		native := agentToolRefFromProto(value)
		out = append(out, &native)
	}
	return out
}

func agentToolRefToProto(value AgentToolRef) *proto.AgentToolRef {
	return &proto.AgentToolRef{
		Plugin:                value.Plugin,
		Operation:             value.Operation,
		Connection:            value.Connection,
		Instance:              value.Instance,
		Title:                 value.Title,
		Description:           value.Description,
		System:                value.System,
		RunAs:                 agentRunAsSubjectToProto(value.RunAs),
		RunAsExternalIdentity: externalIdentityToProto(value.RunAsExternalIdentity),
	}
}

func agentRunAsSubjectFromProto(value *proto.AgentRunAsSubject) *AgentRunAsSubject {
	if value == nil {
		return nil
	}
	return &AgentRunAsSubject{
		SubjectID:           value.GetSubjectId(),
		SubjectKind:         value.GetSubjectKind(),
		CredentialSubjectID: value.GetCredentialSubjectId(),
		DisplayName:         value.GetDisplayName(),
		AuthSource:          value.GetAuthSource(),
	}
}

func agentRunAsSubjectToProto(value *AgentRunAsSubject) *proto.AgentRunAsSubject {
	if value == nil {
		return nil
	}
	return &proto.AgentRunAsSubject{
		SubjectId:           value.SubjectID,
		SubjectKind:         value.SubjectKind,
		CredentialSubjectId: value.CredentialSubjectID,
		DisplayName:         value.DisplayName,
		AuthSource:          value.AuthSource,
	}
}

func externalIdentityFromProto(value *proto.ExternalIdentityContext) *ExternalIdentity {
	if value == nil {
		return nil
	}
	return &ExternalIdentity{
		Type: value.GetType(),
		ID:   value.GetId(),
	}
}

func externalIdentityToProto(value *ExternalIdentity) *proto.ExternalIdentityContext {
	if value == nil {
		return nil
	}
	return &proto.ExternalIdentityContext{
		Type: value.Type,
		Id:   value.ID,
	}
}

func agentToolRefPtrsToProto(values []*AgentToolRef) []*proto.AgentToolRef {
	if len(values) == 0 {
		return nil
	}
	out := make([]*proto.AgentToolRef, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		out = append(out, agentToolRefToProto(*value))
	}
	return out
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
		StructuredOutput:          value.StructuredOutput,
		Interactions:              value.Interactions,
		ResumableTurns:            value.ResumableTurns,
		ReasoningSummaries:        value.ReasoningSummaries,
		BoundedListHydration:      value.BoundedListHydration,
		SupportedToolSources:      sources,
		SupportsSessionStart:      value.SupportsSessionStart,
		SupportsPreparedWorkspace: value.SupportsPreparedWorkspace,
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
		Id:           value.ID,
		ProviderName: value.ProviderName,
		Model:        value.Model,
		ClientRef:    value.ClientRef,
		State:        proto.AgentSessionState(value.State),
		Metadata:     metadata,
		CreatedBy:    agentActorToProto(value.CreatedBy),
		CreatedAt:    timestampFromNonZeroTime(value.CreatedAt),
		UpdatedAt:    timestampFromNonZeroTime(value.UpdatedAt),
		LastTurnAt:   timestampFromOptionalTime(value.LastTurnAt),
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
		ID:           value.GetId(),
		ProviderName: value.GetProviderName(),
		Model:        value.GetModel(),
		ClientRef:    value.GetClientRef(),
		State:        AgentSessionState(value.GetState()),
		Metadata:     mapFromStruct(value.GetMetadata()),
		CreatedBy:    agentActorFromProto(value.GetCreatedBy()),
		CreatedAt:    timeFromTimestamp(value.GetCreatedAt()),
		UpdatedAt:    timeFromTimestamp(value.GetUpdatedAt()),
		LastTurnAt:   timePtrFromTimestampUnchecked(value.GetLastTurnAt()),
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

func listAgentManagerSessionsResponseFromProto(value *proto.AgentManagerListSessionsResponse) *ListAgentManagerSessionsResponse {
	if value == nil {
		return nil
	}
	return &ListAgentManagerSessionsResponse{Sessions: agentSessionsFromProto(value.GetSessions())}
}

func createAgentProviderSessionRequestFromProto(req *proto.CreateAgentProviderSessionRequest) *CreateAgentProviderSessionRequest {
	if req == nil {
		return &CreateAgentProviderSessionRequest{}
	}
	return &CreateAgentProviderSessionRequest{
		SessionID:         req.GetSessionId(),
		IdempotencyKey:    req.GetIdempotencyKey(),
		Model:             req.GetModel(),
		ClientRef:         req.GetClientRef(),
		Metadata:          mapFromStruct(req.GetMetadata()),
		CreatedBy:         agentActorFromProto(req.GetCreatedBy()),
		Subject:           agentSubjectContextFromProto(req.GetSubject()),
		SessionStart:      agentSessionStartConfigFromProto(req.GetSessionStart()),
		PreparedWorkspace: agentPreparedWorkspaceFromProto(req.GetPreparedWorkspace()),
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
		SessionID: req.GetSessionId(),
		Subject:   agentSubjectContextFromProto(req.GetSubject()),
	}
}

func listAgentProviderSessionsRequestFromProto(req *proto.ListAgentProviderSessionsRequest) *ListAgentProviderSessionsRequest {
	if req == nil {
		return &ListAgentProviderSessionsRequest{}
	}
	return &ListAgentProviderSessionsRequest{
		Subject:     agentSubjectContextFromProto(req.GetSubject()),
		SessionIDs:  append([]string(nil), req.GetSessionIds()...),
		State:       AgentSessionState(req.GetState()),
		Limit:       req.GetLimit(),
		SummaryOnly: req.GetSummaryOnly(),
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
		SessionID: req.GetSessionId(),
		ClientRef: req.GetClientRef(),
		State:     AgentSessionState(req.GetState()),
		Metadata:  mapFromStruct(req.GetMetadata()),
		Subject:   agentSubjectContextFromProto(req.GetSubject()),
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
	structuredOutput, err := structFromAny(value.StructuredOutput)
	if err != nil {
		return nil, err
	}
	return &proto.AgentTurn{
		Id:               value.ID,
		SessionId:        value.SessionID,
		ProviderName:     value.ProviderName,
		Model:            value.Model,
		Status:           proto.AgentExecutionStatus(value.Status),
		Messages:         messages,
		OutputText:       value.OutputText,
		StructuredOutput: structuredOutput,
		StatusMessage:    value.StatusMessage,
		CreatedBy:        agentActorToProto(value.CreatedBy),
		CreatedAt:        timestampFromNonZeroTime(value.CreatedAt),
		StartedAt:        timestampFromOptionalTime(value.StartedAt),
		CompletedAt:      timestampFromOptionalTime(value.CompletedAt),
		ExecutionRef:     value.ExecutionRef,
	}, nil
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
		ID:               value.GetId(),
		SessionID:        value.GetSessionId(),
		ProviderName:     value.GetProviderName(),
		Model:            value.GetModel(),
		Status:           AgentExecutionStatus(value.GetStatus()),
		Messages:         agentMessagesFromProto(value.GetMessages()),
		OutputText:       value.GetOutputText(),
		StructuredOutput: mapFromStruct(value.GetStructuredOutput()),
		StatusMessage:    value.GetStatusMessage(),
		CreatedBy:        agentActorFromProto(value.GetCreatedBy()),
		CreatedAt:        timeFromTimestamp(value.GetCreatedAt()),
		StartedAt:        timePtrFromTimestampUnchecked(value.GetStartedAt()),
		CompletedAt:      timePtrFromTimestampUnchecked(value.GetCompletedAt()),
		ExecutionRef:     value.GetExecutionRef(),
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

func listAgentManagerTurnsResponseFromProto(value *proto.AgentManagerListTurnsResponse) *ListAgentManagerTurnsResponse {
	if value == nil {
		return nil
	}
	return &ListAgentManagerTurnsResponse{Turns: agentTurnsFromProto(value.GetTurns())}
}

func createAgentProviderTurnRequestFromProto(req *proto.CreateAgentProviderTurnRequest) *CreateAgentProviderTurnRequest {
	if req == nil {
		return &CreateAgentProviderTurnRequest{}
	}
	return &CreateAgentProviderTurnRequest{
		TurnID:            req.GetTurnId(),
		SessionID:         req.GetSessionId(),
		IdempotencyKey:    req.GetIdempotencyKey(),
		Model:             req.GetModel(),
		Messages:          agentMessagesFromProto(req.GetMessages()),
		Tools:             resolvedAgentToolsFromProto(req.GetTools()),
		ResponseSchema:    mapFromStruct(req.GetResponseSchema()),
		ResponseSchemaSet: req.ResponseSchema != nil,
		Metadata:          mapFromStruct(req.GetMetadata()),
		CreatedBy:         agentActorFromProto(req.GetCreatedBy()),
		ExecutionRef:      req.GetExecutionRef(),
		ToolRefs:          agentToolRefsFromProto(req.GetToolRefs()),
		ToolSource:        AgentToolSourceMode(req.GetToolSource()),
		Subject:           agentSubjectContextFromProto(req.GetSubject()),
		ModelOptions:      mapFromStruct(req.GetModelOptions()),
		RunGrant:          req.GetRunGrant(),
	}
}

func getAgentProviderTurnRequestFromProto(req *proto.GetAgentProviderTurnRequest) *GetAgentProviderTurnRequest {
	if req == nil {
		return &GetAgentProviderTurnRequest{}
	}
	return &GetAgentProviderTurnRequest{
		TurnID:  req.GetTurnId(),
		Subject: agentSubjectContextFromProto(req.GetSubject()),
	}
}

func listAgentProviderTurnsRequestFromProto(req *proto.ListAgentProviderTurnsRequest) *ListAgentProviderTurnsRequest {
	if req == nil {
		return &ListAgentProviderTurnsRequest{}
	}
	return &ListAgentProviderTurnsRequest{
		SessionID:   req.GetSessionId(),
		Subject:     agentSubjectContextFromProto(req.GetSubject()),
		TurnIDs:     append([]string(nil), req.GetTurnIds()...),
		Status:      AgentExecutionStatus(req.GetStatus()),
		Limit:       req.GetLimit(),
		SummaryOnly: req.GetSummaryOnly(),
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
		TurnID:  req.GetTurnId(),
		Reason:  req.GetReason(),
		Subject: agentSubjectContextFromProto(req.GetSubject()),
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

func listAgentManagerTurnEventsResponseFromProto(value *proto.AgentManagerListTurnEventsResponse) *ListAgentManagerTurnEventsResponse {
	if value == nil {
		return nil
	}
	return &ListAgentManagerTurnEventsResponse{Events: agentTurnEventsFromProto(value.GetEvents())}
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
		TurnID:   req.GetTurnId(),
		AfterSeq: req.GetAfterSeq(),
		Limit:    req.GetLimit(),
		Subject:  agentSubjectContextFromProto(req.GetSubject()),
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
		Subject:       agentSubjectContextFromProto(req.GetSubject()),
	}
}

func listAgentProviderInteractionsRequestFromProto(req *proto.ListAgentProviderInteractionsRequest) *ListAgentProviderInteractionsRequest {
	if req == nil {
		return &ListAgentProviderInteractionsRequest{}
	}
	return &ListAgentProviderInteractionsRequest{
		TurnID:  req.GetTurnId(),
		Subject: agentSubjectContextFromProto(req.GetSubject()),
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

func listAgentManagerInteractionsResponseFromProto(value *proto.AgentManagerListInteractionsResponse) *ListAgentManagerInteractionsResponse {
	if value == nil {
		return nil
	}
	return &ListAgentManagerInteractionsResponse{Interactions: agentInteractionsFromProto(value.GetInteractions())}
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
		InteractionID: req.GetInteractionId(),
		Resolution:    mapFromStruct(req.GetResolution()),
		Subject:       agentSubjectContextFromProto(req.GetSubject()),
	}
}

func executeAgentToolResponseFromProto(value *proto.ExecuteAgentToolResponse) *ExecuteAgentToolResponse {
	if value == nil {
		return nil
	}
	return &ExecuteAgentToolResponse{
		Status: value.GetStatus(),
		Body:   value.GetBody(),
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

func listAgentToolsResponseFromProto(value *proto.ListAgentToolsResponse) *ListAgentToolsResponse {
	if value == nil {
		return nil
	}
	return &ListAgentToolsResponse{
		Tools:         listedAgentToolsFromProto(value.GetTools()),
		NextPageToken: value.GetNextPageToken(),
	}
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

func resolvedAgentConnectionFromProto(value *proto.ResolvedAgentConnection) *ResolvedAgentConnection {
	if value == nil {
		return nil
	}
	return &ResolvedAgentConnection{
		ConnectionID: value.GetConnectionId(),
		Connection:   value.GetConnection(),
		Instance:     value.GetInstance(),
		Mode:         value.GetMode(),
		Headers:      cloneAgentStringMap(value.GetHeaders()),
		Params:       cloneAgentStringMap(value.GetParams()),
		ExpiresAt:    timePtrFromTimestampUnchecked(value.GetExpiresAt()),
	}
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
