package gestalt

import proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"

type AgentManagerCreateSessionInput struct {
	ProviderName   string
	Model          string
	ClientRef      string
	Metadata       any
	IdempotencyKey string
	Workspace      *AgentWorkspaceInput
}

type AgentManagerGetSessionInput struct {
	SessionID string
}

type AgentManagerListSessionsInput struct {
	ProviderName string
	State        AgentSessionState
	Limit        int32
	SummaryOnly  bool
}

type AgentManagerUpdateSessionInput struct {
	SessionID string
	ClientRef string
	State     AgentSessionState
	Metadata  any
}

type AgentManagerCreateTurnInput struct {
	SessionID      string
	Model          string
	Messages       []AgentMessageInput
	ToolRefs       []AgentToolRefInput
	ToolSource     AgentToolSourceMode
	ResponseSchema any
	Metadata       any
	IdempotencyKey string
	ModelOptions   any
}

type AgentManagerGetTurnInput struct {
	TurnID string
}

type AgentManagerListTurnsInput struct {
	SessionID   string
	Status      AgentExecutionStatus
	Limit       int32
	SummaryOnly bool
}

type AgentManagerCancelTurnInput struct {
	TurnID string
	Reason string
}

type AgentManagerListTurnEventsInput struct {
	TurnID   string
	AfterSeq int64
	Limit    int32
}

type AgentManagerListInteractionsInput struct {
	TurnID string
}

type AgentManagerResolveInteractionInput struct {
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

func NewAgentManagerCreateSessionRequest(input AgentManagerCreateSessionInput) (*proto.AgentManagerCreateSessionRequest, error) {
	metadata, err := StructFromAny(input.Metadata)
	if err != nil {
		return nil, err
	}
	var workspace *proto.AgentWorkspace
	if input.Workspace != nil {
		workspace = agentProtocolWorkspaceToProto(NewAgentWorkspace(*input.Workspace))
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

func NewAgentManagerGetSessionRequest(input AgentManagerGetSessionInput) *proto.AgentManagerGetSessionRequest {
	return &proto.AgentManagerGetSessionRequest{SessionId: input.SessionID}
}

func NewAgentManagerListSessionsRequest(input AgentManagerListSessionsInput) *proto.AgentManagerListSessionsRequest {
	return &proto.AgentManagerListSessionsRequest{
		ProviderName: input.ProviderName,
		State:        proto.AgentSessionState(input.State),
		Limit:        input.Limit,
		SummaryOnly:  input.SummaryOnly,
	}
}

func NewAgentManagerUpdateSessionRequest(input AgentManagerUpdateSessionInput) (*proto.AgentManagerUpdateSessionRequest, error) {
	metadata, err := StructFromAny(input.Metadata)
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

func NewAgentManagerCreateTurnRequest(input AgentManagerCreateTurnInput) (*proto.AgentManagerCreateTurnRequest, error) {
	nativeMessages, err := agentMessagesFromInputs(input.Messages)
	if err != nil {
		return nil, err
	}
	messages, err := agentMessagePtrsToProto(nativeMessages)
	if err != nil {
		return nil, err
	}
	responseSchema, err := StructFromAny(input.ResponseSchema)
	if err != nil {
		return nil, err
	}
	metadata, err := StructFromAny(input.Metadata)
	if err != nil {
		return nil, err
	}
	modelOptions, err := StructFromAny(input.ModelOptions)
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

func NewAgentManagerGetTurnRequest(input AgentManagerGetTurnInput) *proto.AgentManagerGetTurnRequest {
	return &proto.AgentManagerGetTurnRequest{TurnId: input.TurnID}
}

func NewAgentManagerListTurnsRequest(input AgentManagerListTurnsInput) *proto.AgentManagerListTurnsRequest {
	return &proto.AgentManagerListTurnsRequest{
		SessionId:   input.SessionID,
		Status:      proto.AgentExecutionStatus(input.Status),
		Limit:       input.Limit,
		SummaryOnly: input.SummaryOnly,
	}
}

func NewAgentManagerCancelTurnRequest(input AgentManagerCancelTurnInput) *proto.AgentManagerCancelTurnRequest {
	return &proto.AgentManagerCancelTurnRequest{
		TurnId: input.TurnID,
		Reason: input.Reason,
	}
}

func NewAgentManagerListTurnEventsRequest(input AgentManagerListTurnEventsInput) *proto.AgentManagerListTurnEventsRequest {
	return &proto.AgentManagerListTurnEventsRequest{
		TurnId:   input.TurnID,
		AfterSeq: input.AfterSeq,
		Limit:    input.Limit,
	}
}

func NewAgentManagerListInteractionsRequest(input AgentManagerListInteractionsInput) *proto.AgentManagerListInteractionsRequest {
	return &proto.AgentManagerListInteractionsRequest{TurnId: input.TurnID}
}

func NewAgentManagerResolveInteractionRequest(input AgentManagerResolveInteractionInput) (*proto.AgentManagerResolveInteractionRequest, error) {
	resolution, err := StructFromAny(input.Resolution)
	if err != nil {
		return nil, err
	}
	return &proto.AgentManagerResolveInteractionRequest{
		TurnId:        input.TurnID,
		InteractionId: input.InteractionID,
		Resolution:    resolution,
	}, nil
}
