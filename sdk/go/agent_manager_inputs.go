package gestalt

import proto "github.com/valon-technologies/gestalt/sdk/go/protov1/v1"

type AgentManagerCreateSession struct {
	ProviderName   string
	Model          string
	ClientRef      string
	Metadata       any
	IdempotencyKey string
	Workspace      *AgentWorkspace
}

type AgentManagerGetSession struct {
	SessionID string
}

type AgentManagerListSessions struct {
	ProviderName string
	State        AgentSessionState
	Limit        int32
	SummaryOnly  bool
}

type AgentManagerUpdateSession struct {
	SessionID string
	ClientRef string
	State     AgentSessionState
	Metadata  any
}

type AgentManagerCreateTurn struct {
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

type AgentManagerGetTurn struct {
	TurnID string
}

type AgentManagerListTurns struct {
	SessionID   string
	Status      AgentExecutionStatus
	Limit       int32
	SummaryOnly bool
}

type AgentManagerCancelTurn struct {
	TurnID string
	Reason string
}

type AgentManagerListTurnEvents struct {
	TurnID   string
	AfterSeq int64
	Limit    int32
}

type AgentManagerListInteractions struct {
	TurnID string
}

type AgentManagerResolveInteraction struct {
	TurnID        string
	InteractionID string
	Resolution    any
}

type ListAgentManagerSessionsResponse struct {
	Sessions []AgentSession
}

type ListAgentManagerTurnsResponse struct {
	Turns []AgentTurn
}

type ListAgentManagerTurnEventsResponse struct {
	Events []AgentTurnEvent
}

type ListAgentManagerInteractionsResponse struct {
	Interactions []AgentInteraction
}

func newAgentManagerCreateSessionRequest(input AgentManagerCreateSession) (*proto.CreateAgentProviderSessionRequest, error) {
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

func newAgentManagerGetSessionRequest(input AgentManagerGetSession) *proto.GetAgentProviderSessionRequest {
	return &proto.GetAgentProviderSessionRequest{SessionId: input.SessionID}
}

func newAgentManagerListSessionsRequest(input AgentManagerListSessions) *proto.ListAgentProviderSessionsRequest {
	return &proto.ListAgentProviderSessionsRequest{
		ProviderName: input.ProviderName,
		State:        proto.AgentSessionState(input.State),
		Limit:        input.Limit,
		SummaryOnly:  input.SummaryOnly,
	}
}

func newAgentManagerUpdateSessionRequest(input AgentManagerUpdateSession) (*proto.UpdateAgentProviderSessionRequest, error) {
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

func newAgentManagerCreateTurnRequest(input AgentManagerCreateTurn) (*proto.CreateAgentProviderTurnRequest, error) {
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

func newAgentManagerGetTurnRequest(input AgentManagerGetTurn) *proto.GetAgentProviderTurnRequest {
	return &proto.GetAgentProviderTurnRequest{TurnId: input.TurnID}
}

func newAgentManagerListTurnsRequest(input AgentManagerListTurns) *proto.ListAgentProviderTurnsRequest {
	return &proto.ListAgentProviderTurnsRequest{
		SessionId:   input.SessionID,
		Status:      proto.AgentExecutionStatus(input.Status),
		Limit:       input.Limit,
		SummaryOnly: input.SummaryOnly,
	}
}

func newAgentManagerCancelTurnRequest(input AgentManagerCancelTurn) *proto.CancelAgentProviderTurnRequest {
	return &proto.CancelAgentProviderTurnRequest{
		TurnId: input.TurnID,
		Reason: input.Reason,
	}
}

func newAgentManagerListTurnEventsRequest(input AgentManagerListTurnEvents) *proto.ListAgentProviderTurnEventsRequest {
	return &proto.ListAgentProviderTurnEventsRequest{
		TurnId:   input.TurnID,
		AfterSeq: input.AfterSeq,
		Limit:    input.Limit,
	}
}

func newAgentManagerListInteractionsRequest(input AgentManagerListInteractions) *proto.ListAgentProviderInteractionsRequest {
	return &proto.ListAgentProviderInteractionsRequest{TurnId: input.TurnID}
}

func newAgentManagerResolveInteractionRequest(input AgentManagerResolveInteraction) (*proto.ResolveAgentProviderInteractionRequest, error) {
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
