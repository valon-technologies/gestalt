package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
)

// AgentProvider is implemented by providers that serve the agent base
// primitive. The SDK owns the gRPC/protobuf transport adapter; provider code
// implements this typed interface instead of importing generated protobuf
// service bindings.
type AgentProvider interface {
	Provider
	// CreateSession creates or idempotently returns an agent session.
	CreateSession(ctx context.Context, req *CreateAgentProviderSessionRequest) (*AgentSession, error)
	// GetSession returns one agent session by ID.
	GetSession(ctx context.Context, req *GetAgentProviderSessionRequest) (*AgentSession, error)
	// ListSessions returns agent sessions visible to the request subject.
	ListSessions(ctx context.Context, req *ListAgentProviderSessionsRequest) (*ListAgentProviderSessionsResponse, error)
	// UpdateSession updates mutable agent session metadata or state.
	UpdateSession(ctx context.Context, req *UpdateAgentProviderSessionRequest) (*AgentSession, error)
	// CreateTurn starts or idempotently returns an agent turn.
	CreateTurn(ctx context.Context, req *CreateAgentProviderTurnRequest) (*AgentTurn, error)
	// GetTurn returns one agent turn by ID.
	GetTurn(ctx context.Context, req *GetAgentProviderTurnRequest) (*AgentTurn, error)
	// ListTurns returns turns for a session or query.
	ListTurns(ctx context.Context, req *ListAgentProviderTurnsRequest) (*ListAgentProviderTurnsResponse, error)
	// CancelTurn requests cancellation of a running or pending turn.
	CancelTurn(ctx context.Context, req *CancelAgentProviderTurnRequest) (*AgentTurn, error)
	// ListTurnEvents returns ordered events emitted by a turn.
	ListTurnEvents(ctx context.Context, req *ListAgentProviderTurnEventsRequest) (*ListAgentProviderTurnEventsResponse, error)
	// GetInteraction returns one pending or resolved interaction.
	GetInteraction(ctx context.Context, req *GetAgentProviderInteractionRequest) (*AgentInteraction, error)
	// ListInteractions returns interactions associated with a turn.
	ListInteractions(ctx context.Context, req *ListAgentProviderInteractionsRequest) (*ListAgentProviderInteractionsResponse, error)
	// ResolveInteraction records a response to a pending interaction.
	ResolveInteraction(ctx context.Context, req *ResolveAgentProviderInteractionRequest) (*AgentInteraction, error)
	// GetCapabilities returns the provider's supported agent features.
	GetCapabilities(ctx context.Context, req *GetAgentProviderCapabilitiesRequest) (*AgentProviderCapabilities, error)
}

// UnimplementedAgentProvider provides no-op lifecycle behavior and
// unimplemented agent operations. Embed it when a provider implements only part
// of the agent surface.
type UnimplementedAgentProvider struct{}

func (UnimplementedAgentProvider) Configure(context.Context, string, map[string]any) error {
	return nil
}

func (UnimplementedAgentProvider) CreateSession(context.Context, *CreateAgentProviderSessionRequest) (*AgentSession, error) {
	return nil, Unimplemented("agent create session is not implemented")
}

func (UnimplementedAgentProvider) GetSession(context.Context, *GetAgentProviderSessionRequest) (*AgentSession, error) {
	return nil, Unimplemented("agent get session is not implemented")
}

func (UnimplementedAgentProvider) ListSessions(context.Context, *ListAgentProviderSessionsRequest) (*ListAgentProviderSessionsResponse, error) {
	return nil, Unimplemented("agent list sessions is not implemented")
}

func (UnimplementedAgentProvider) UpdateSession(context.Context, *UpdateAgentProviderSessionRequest) (*AgentSession, error) {
	return nil, Unimplemented("agent update session is not implemented")
}

func (UnimplementedAgentProvider) CreateTurn(context.Context, *CreateAgentProviderTurnRequest) (*AgentTurn, error) {
	return nil, Unimplemented("agent create turn is not implemented")
}

func (UnimplementedAgentProvider) GetTurn(context.Context, *GetAgentProviderTurnRequest) (*AgentTurn, error) {
	return nil, Unimplemented("agent get turn is not implemented")
}

func (UnimplementedAgentProvider) ListTurns(context.Context, *ListAgentProviderTurnsRequest) (*ListAgentProviderTurnsResponse, error) {
	return nil, Unimplemented("agent list turns is not implemented")
}

func (UnimplementedAgentProvider) CancelTurn(context.Context, *CancelAgentProviderTurnRequest) (*AgentTurn, error) {
	return nil, Unimplemented("agent cancel turn is not implemented")
}

func (UnimplementedAgentProvider) ListTurnEvents(context.Context, *ListAgentProviderTurnEventsRequest) (*ListAgentProviderTurnEventsResponse, error) {
	return nil, Unimplemented("agent list turn events is not implemented")
}

func (UnimplementedAgentProvider) GetInteraction(context.Context, *GetAgentProviderInteractionRequest) (*AgentInteraction, error) {
	return nil, Unimplemented("agent get interaction is not implemented")
}

func (UnimplementedAgentProvider) ListInteractions(context.Context, *ListAgentProviderInteractionsRequest) (*ListAgentProviderInteractionsResponse, error) {
	return nil, Unimplemented("agent list interactions is not implemented")
}

func (UnimplementedAgentProvider) ResolveInteraction(context.Context, *ResolveAgentProviderInteractionRequest) (*AgentInteraction, error) {
	return nil, Unimplemented("agent resolve interaction is not implemented")
}

func (UnimplementedAgentProvider) GetCapabilities(context.Context, *GetAgentProviderCapabilitiesRequest) (*AgentProviderCapabilities, error) {
	return nil, Unimplemented("agent get capabilities is not implemented")
}

// Agent protocol type aliases expose request, response, and model messages
// through the SDK package so provider implementations do not need to import
// generated protobuf packages directly.
type (
	// AgentMessage is a protocol alias exposed for provider implementations.
	AgentMessage                           = proto.AgentMessage
	AgentMessagePartToolCall               = proto.AgentMessagePartToolCall
	AgentMessagePartToolResult             = proto.AgentMessagePartToolResult
	AgentMessagePartImageRef               = proto.AgentMessagePartImageRef
	AgentMessagePart                       = proto.AgentMessagePart
	AgentActor                             = proto.AgentActor
	AgentSubjectContext                    = proto.AgentSubjectContext
	AgentProtocolWorkspace                 = proto.AgentWorkspace
	AgentProtocolWorkspaceGitCheckout      = proto.AgentWorkspaceGitCheckout
	AgentPreparedWorkspace                 = proto.PreparedAgentWorkspace
	ResolvedAgentTool                      = proto.ResolvedAgentTool
	AgentToolRef                           = proto.AgentToolRef
	AgentProviderCapabilities              = proto.AgentProviderCapabilities
	GetAgentProviderCapabilitiesRequest    = proto.GetAgentProviderCapabilitiesRequest
	AgentInteraction                       = proto.AgentInteraction
	AgentSession                           = proto.AgentSession
	CreateAgentProviderSessionRequest      = proto.CreateAgentProviderSessionRequest
	AgentSessionStartConfig                = proto.AgentSessionStartConfig
	AgentSessionStartHook                  = proto.AgentSessionStartHook
	AgentSessionStartHookOutput            = proto.AgentSessionStartHookOutput
	GetAgentProviderSessionRequest         = proto.GetAgentProviderSessionRequest
	ListAgentProviderSessionsRequest       = proto.ListAgentProviderSessionsRequest
	ListAgentProviderSessionsResponse      = proto.ListAgentProviderSessionsResponse
	UpdateAgentProviderSessionRequest      = proto.UpdateAgentProviderSessionRequest
	AgentTurn                              = proto.AgentTurn
	AgentTurnDisplay                       = proto.AgentTurnDisplay
	CreateAgentProviderTurnRequest         = proto.CreateAgentProviderTurnRequest
	GetAgentProviderTurnRequest            = proto.GetAgentProviderTurnRequest
	ListAgentProviderTurnsRequest          = proto.ListAgentProviderTurnsRequest
	ListAgentProviderTurnsResponse         = proto.ListAgentProviderTurnsResponse
	CancelAgentProviderTurnRequest         = proto.CancelAgentProviderTurnRequest
	AgentTurnEvent                         = proto.AgentTurnEvent
	ListAgentProviderTurnEventsRequest     = proto.ListAgentProviderTurnEventsRequest
	ListAgentProviderTurnEventsResponse    = proto.ListAgentProviderTurnEventsResponse
	GetAgentProviderInteractionRequest     = proto.GetAgentProviderInteractionRequest
	ListAgentProviderInteractionsRequest   = proto.ListAgentProviderInteractionsRequest
	ListAgentProviderInteractionsResponse  = proto.ListAgentProviderInteractionsResponse
	ResolveAgentProviderInteractionRequest = proto.ResolveAgentProviderInteractionRequest
	ExecuteAgentToolRequest                = proto.ExecuteAgentToolRequest
	ExecuteAgentToolResponse               = proto.ExecuteAgentToolResponse
	ListedAgentTool                        = proto.ListedAgentTool
	ListAgentToolsRequest                  = proto.ListAgentToolsRequest
	ListAgentToolsResponse                 = proto.ListAgentToolsResponse
	ResolveAgentConnectionRequest          = proto.ResolveAgentConnectionRequest
	ResolvedAgentConnection                = proto.ResolvedAgentConnection
	AgentManagerCreateSessionRequest       = proto.AgentManagerCreateSessionRequest
	AgentManagerGetSessionRequest          = proto.AgentManagerGetSessionRequest
	AgentManagerListSessionsRequest        = proto.AgentManagerListSessionsRequest
	AgentManagerListSessionsResponse       = proto.AgentManagerListSessionsResponse
	AgentManagerUpdateSessionRequest       = proto.AgentManagerUpdateSessionRequest
	AgentManagerCreateTurnRequest          = proto.AgentManagerCreateTurnRequest
	AgentManagerGetTurnRequest             = proto.AgentManagerGetTurnRequest
	AgentManagerListTurnsRequest           = proto.AgentManagerListTurnsRequest
	AgentManagerListTurnsResponse          = proto.AgentManagerListTurnsResponse
	AgentManagerCancelTurnRequest          = proto.AgentManagerCancelTurnRequest
	AgentManagerListTurnEventsRequest      = proto.AgentManagerListTurnEventsRequest
	AgentManagerListTurnEventsResponse     = proto.AgentManagerListTurnEventsResponse
	AgentManagerListInteractionsRequest    = proto.AgentManagerListInteractionsRequest
	AgentManagerListInteractionsResponse   = proto.AgentManagerListInteractionsResponse
	AgentManagerResolveInteractionRequest  = proto.AgentManagerResolveInteractionRequest
)

type (
	// AgentMessagePartType identifies the payload kind in an agent message part.
	AgentMessagePartType  = proto.AgentMessagePartType
	AgentToolSourceMode   = proto.AgentToolSourceMode
	AgentExecutionStatus  = proto.AgentExecutionStatus
	AgentSessionState     = proto.AgentSessionState
	AgentInteractionType  = proto.AgentInteractionType
	AgentInteractionState = proto.AgentInteractionState
)

// Agent protocol enum constants provide stable SDK names for common generated
// enum values.
const (
	AgentMessagePartTypeUnspecified = proto.AgentMessagePartType_AGENT_MESSAGE_PART_TYPE_UNSPECIFIED
	AgentMessagePartTypeText        = proto.AgentMessagePartType_AGENT_MESSAGE_PART_TYPE_TEXT
	AgentMessagePartTypeJSON        = proto.AgentMessagePartType_AGENT_MESSAGE_PART_TYPE_JSON
	AgentMessagePartTypeToolCall    = proto.AgentMessagePartType_AGENT_MESSAGE_PART_TYPE_TOOL_CALL
	AgentMessagePartTypeToolResult  = proto.AgentMessagePartType_AGENT_MESSAGE_PART_TYPE_TOOL_RESULT
	AgentMessagePartTypeImageRef    = proto.AgentMessagePartType_AGENT_MESSAGE_PART_TYPE_IMAGE_REF

	AgentToolSourceModeUnspecified = proto.AgentToolSourceMode_AGENT_TOOL_SOURCE_MODE_UNSPECIFIED
	AgentToolSourceModeMCPCatalog  = proto.AgentToolSourceMode_AGENT_TOOL_SOURCE_MODE_MCP_CATALOG

	AgentExecutionStatusUnspecified     = proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_UNSPECIFIED
	AgentExecutionStatusPending         = proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_PENDING
	AgentExecutionStatusRunning         = proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_RUNNING
	AgentExecutionStatusSucceeded       = proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_SUCCEEDED
	AgentExecutionStatusFailed          = proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_FAILED
	AgentExecutionStatusCanceled        = proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_CANCELED
	AgentExecutionStatusWaitingForInput = proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_WAITING_FOR_INPUT

	AgentSessionStateUnspecified = proto.AgentSessionState_AGENT_SESSION_STATE_UNSPECIFIED
	AgentSessionStateActive      = proto.AgentSessionState_AGENT_SESSION_STATE_ACTIVE
	AgentSessionStateArchived    = proto.AgentSessionState_AGENT_SESSION_STATE_ARCHIVED

	AgentInteractionTypeUnspecified   = proto.AgentInteractionType_AGENT_INTERACTION_TYPE_UNSPECIFIED
	AgentInteractionTypeInput         = proto.AgentInteractionType_AGENT_INTERACTION_TYPE_INPUT
	AgentInteractionTypeApproval      = proto.AgentInteractionType_AGENT_INTERACTION_TYPE_APPROVAL
	AgentInteractionTypeClarification = proto.AgentInteractionType_AGENT_INTERACTION_TYPE_CLARIFICATION

	AgentInteractionStateUnspecified = proto.AgentInteractionState_AGENT_INTERACTION_STATE_UNSPECIFIED
	AgentInteractionStatePending     = proto.AgentInteractionState_AGENT_INTERACTION_STATE_PENDING
	AgentInteractionStateResolved    = proto.AgentInteractionState_AGENT_INTERACTION_STATE_RESOLVED
	AgentInteractionStateCanceled    = proto.AgentInteractionState_AGENT_INTERACTION_STATE_CANCELED
)
