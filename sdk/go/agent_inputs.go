package gestalt

import proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"

type AgentCreateSession struct {
	ProviderName   string
	Model          string
	ClientRef      string
	Metadata       any
	IdempotencyKey string
	Workspace      *AgentWorkspace
}

type AgentGetSession struct {
	SessionID string
}

type AgentListSessions struct {
	ProviderName string
	State        AgentSessionState
	Limit        int32
	SummaryOnly  bool
}

type AgentUpdateSession struct {
	SessionID string
	ClientRef string
	State     AgentSessionState
	Metadata  any
}

type AgentCreateTurn struct {
	SessionID      string
	Model          string
	Messages       []AgentMessage
	ToolRefs       []AgentToolRef
	ToolRefsSet    bool
	ToolSource     AgentToolSourceMode
	ResponseSchema any
	Metadata       any
	IdempotencyKey string
	ModelOptions   any
	TimeoutSeconds int32
}

type AgentGetTurn struct {
	TurnID string
}

type AgentListTurns struct {
	SessionID   string
	Status      AgentExecutionStatus
	Limit       int32
	SummaryOnly bool
}

type AgentCancelTurn struct {
	TurnID string
	Reason string
}

type AgentListTurnEvents struct {
	TurnID   string
	AfterSeq int64
	Limit    int32
}

type AgentListInteractions struct {
	TurnID string
}

type AgentResolveInteraction struct {
	TurnID        string
	InteractionID string
	Resolution    any
}

type ListAgentSessionsResponse struct {
	Sessions []AgentSession
}

type ListAgentTurnsResponse struct {
	Turns []AgentTurn
}

type ListAgentTurnEventsResponse struct {
	Events []AgentTurnEvent
}

type ListAgentInteractionsResponse struct {
	Interactions []AgentInteraction
}

func newAgentCreateSessionRequest(input AgentCreateSession) (*proto.CreateAgentProviderSessionRequest, error) {
	metadata, err := structFromAny(input.Metadata)
	if err != nil {
		return nil, err
	}
	var workspace *proto.AgentWorkspace
	if input.Workspace != nil {
		workspace = agentWorkspaceToProto(input.Workspace)
	}
	return &proto.CreateAgentProviderSessionRequest{
		ProviderName:   input.ProviderName,
		Model:          input.Model,
		ClientRef:      input.ClientRef,
		Metadata:       metadata,
		IdempotencyKey: input.IdempotencyKey,
		Workspace:      workspace,
	}, nil
}

func newAgentGetSessionRequest(input AgentGetSession) *proto.GetAgentProviderSessionRequest {
	return &proto.GetAgentProviderSessionRequest{SessionId: input.SessionID}
}

func newAgentListSessionsRequest(input AgentListSessions) *proto.ListAgentProviderSessionsRequest {
	return &proto.ListAgentProviderSessionsRequest{
		ProviderName: input.ProviderName,
		State:        proto.AgentSessionState(input.State),
		Limit:        input.Limit,
		SummaryOnly:  input.SummaryOnly,
	}
}

func newAgentUpdateSessionRequest(input AgentUpdateSession) (*proto.UpdateAgentProviderSessionRequest, error) {
	metadata, err := structFromAny(input.Metadata)
	if err != nil {
		return nil, err
	}
	return &proto.UpdateAgentProviderSessionRequest{
		SessionId: input.SessionID,
		ClientRef: input.ClientRef,
		State:     proto.AgentSessionState(input.State),
		Metadata:  metadata,
	}, nil
}

func newAgentCreateTurnRequest(input AgentCreateTurn) (*proto.CreateAgentProviderTurnRequest, error) {
	messages, err := agentMessagesToProto(input.Messages)
	if err != nil {
		return nil, err
	}
	responseSchema, err := structFromAny(input.ResponseSchema)
	if err != nil {
		return nil, err
	}
	metadata, err := structFromAny(input.Metadata)
	if err != nil {
		return nil, err
	}
	modelOptions, err := structFromAny(input.ModelOptions)
	if err != nil {
		return nil, err
	}
	return &proto.CreateAgentProviderTurnRequest{
		SessionId:      input.SessionID,
		Model:          input.Model,
		Messages:       messages,
		ToolRefs:       agentToolRefPtrsToProto(agentToolRefsFromInputs(input.ToolRefs)),
		ToolRefsSet:    input.ToolRefsSet || len(input.ToolRefs) > 0,
		ToolSource:     proto.AgentToolSourceMode(input.ToolSource),
		ResponseSchema: responseSchema,
		Metadata:       metadata,
		IdempotencyKey: input.IdempotencyKey,
		ModelOptions:   modelOptions,
		TimeoutSeconds: input.TimeoutSeconds,
	}, nil
}

func newAgentGetTurnRequest(input AgentGetTurn) *proto.GetAgentProviderTurnRequest {
	return &proto.GetAgentProviderTurnRequest{TurnId: input.TurnID}
}

func newAgentListTurnsRequest(input AgentListTurns) *proto.ListAgentProviderTurnsRequest {
	return &proto.ListAgentProviderTurnsRequest{
		SessionId:   input.SessionID,
		Status:      proto.AgentExecutionStatus(input.Status),
		Limit:       input.Limit,
		SummaryOnly: input.SummaryOnly,
	}
}

func newAgentCancelTurnRequest(input AgentCancelTurn) *proto.CancelAgentProviderTurnRequest {
	return &proto.CancelAgentProviderTurnRequest{
		TurnId: input.TurnID,
		Reason: input.Reason,
	}
}

func newAgentListTurnEventsRequest(input AgentListTurnEvents) *proto.ListAgentProviderTurnEventsRequest {
	return &proto.ListAgentProviderTurnEventsRequest{
		TurnId:   input.TurnID,
		AfterSeq: input.AfterSeq,
		Limit:    input.Limit,
	}
}

func newAgentListInteractionsRequest(input AgentListInteractions) *proto.ListAgentProviderInteractionsRequest {
	return &proto.ListAgentProviderInteractionsRequest{TurnId: input.TurnID}
}

func newAgentResolveInteractionRequest(input AgentResolveInteraction) (*proto.ResolveAgentProviderInteractionRequest, error) {
	resolution, err := structFromAny(input.Resolution)
	if err != nil {
		return nil, err
	}
	return &proto.ResolveAgentProviderInteractionRequest{
		TurnId:        input.TurnID,
		InteractionId: input.InteractionID,
		Resolution:    resolution,
	}, nil
}
