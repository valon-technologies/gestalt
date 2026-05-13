package gestalt

import proto "github.com/valon-technologies/gestalt/sdk/go/internal/gen/v1"

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
	ToolSource     AgentToolSourceMode
	ResponseSchema any
	Metadata       any
	IdempotencyKey string
	ModelOptions   any
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

func newAgentManagerCreateSessionRequest(input AgentManagerCreateSession) (*proto.AgentManagerCreateSessionRequest, error) {
	metadata, err := structFromAny(input.Metadata)
	if err != nil {
		return nil, err
	}
	var workspace *proto.AgentWorkspace
	if input.Workspace != nil {
		workspace = agentWorkspaceToProto(input.Workspace)
	}
	return &proto.AgentManagerCreateSessionRequest{
		ProviderName:   input.ProviderName,
		Model:          input.Model,
		ClientRef:      input.ClientRef,
		Metadata:       metadata,
		IdempotencyKey: input.IdempotencyKey,
		Workspace:      workspace,
	}, nil
}

func newAgentManagerGetSessionRequest(input AgentManagerGetSession) *proto.AgentManagerGetSessionRequest {
	return &proto.AgentManagerGetSessionRequest{SessionId: input.SessionID}
}

func newAgentManagerListSessionsRequest(input AgentManagerListSessions) *proto.AgentManagerListSessionsRequest {
	return &proto.AgentManagerListSessionsRequest{
		ProviderName: input.ProviderName,
		State:        proto.AgentSessionState(input.State),
		Limit:        input.Limit,
		SummaryOnly:  input.SummaryOnly,
	}
}

func newAgentManagerUpdateSessionRequest(input AgentManagerUpdateSession) (*proto.AgentManagerUpdateSessionRequest, error) {
	metadata, err := structFromAny(input.Metadata)
	if err != nil {
		return nil, err
	}
	return &proto.AgentManagerUpdateSessionRequest{
		SessionId: input.SessionID,
		ClientRef: input.ClientRef,
		State:     proto.AgentSessionState(input.State),
		Metadata:  metadata,
	}, nil
}

func newAgentManagerCreateTurnRequest(input AgentManagerCreateTurn) (*proto.AgentManagerCreateTurnRequest, error) {
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
	return &proto.AgentManagerCreateTurnRequest{
		SessionId:      input.SessionID,
		Model:          input.Model,
		Messages:       messages,
		ToolRefs:       agentToolRefPtrsToProto(agentToolRefsFromInputs(input.ToolRefs)),
		ToolSource:     proto.AgentToolSourceMode(input.ToolSource),
		ResponseSchema: responseSchema,
		Metadata:       metadata,
		IdempotencyKey: input.IdempotencyKey,
		ModelOptions:   modelOptions,
	}, nil
}

func newAgentManagerGetTurnRequest(input AgentManagerGetTurn) *proto.AgentManagerGetTurnRequest {
	return &proto.AgentManagerGetTurnRequest{TurnId: input.TurnID}
}

func newAgentManagerListTurnsRequest(input AgentManagerListTurns) *proto.AgentManagerListTurnsRequest {
	return &proto.AgentManagerListTurnsRequest{
		SessionId:   input.SessionID,
		Status:      proto.AgentExecutionStatus(input.Status),
		Limit:       input.Limit,
		SummaryOnly: input.SummaryOnly,
	}
}

func newAgentManagerCancelTurnRequest(input AgentManagerCancelTurn) *proto.AgentManagerCancelTurnRequest {
	return &proto.AgentManagerCancelTurnRequest{
		TurnId: input.TurnID,
		Reason: input.Reason,
	}
}

func newAgentManagerListTurnEventsRequest(input AgentManagerListTurnEvents) *proto.AgentManagerListTurnEventsRequest {
	return &proto.AgentManagerListTurnEventsRequest{
		TurnId:   input.TurnID,
		AfterSeq: input.AfterSeq,
		Limit:    input.Limit,
	}
}

func newAgentManagerListInteractionsRequest(input AgentManagerListInteractions) *proto.AgentManagerListInteractionsRequest {
	return &proto.AgentManagerListInteractionsRequest{TurnId: input.TurnID}
}

func newAgentManagerResolveInteractionRequest(input AgentManagerResolveInteraction) (*proto.AgentManagerResolveInteractionRequest, error) {
	resolution, err := structFromAny(input.Resolution)
	if err != nil {
		return nil, err
	}
	return &proto.AgentManagerResolveInteractionRequest{
		TurnId:        input.TurnID,
		InteractionId: input.InteractionID,
		Resolution:    resolution,
	}, nil
}
