package gestalt

import (
	"context"
	"time"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

// AgentProvider is implemented by providers that serve the agent base
// primitive. The SDK owns the gRPC/protobuf transport adapter; provider code
// implements this typed interface instead of importing generated protobuf
// service bindings. Read methods for sessions, turns, turn events, and
// interactions must be served from provider-owned control-plane state. They
// must not require a live execution sandbox, pod IP, cached tunnel, or other
// transport attachment.
type AgentProvider interface {
	Provider
	// CreateSession mints the session id returned on the AgentSession.
	// Creation must be idempotent on IdempotencyKey scoped per subject
	// (CreatedBySubjectID); an empty key always creates a new session.
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

// Configure returns Unimplemented; embed UnimplementedAgentProvider to default
// unimplemented surface methods.
func (UnimplementedAgentProvider) Configure(context.Context, string, map[string]any) error {
	return nil
}

// CreateSession returns Unimplemented; embed UnimplementedAgentProvider to default
// unimplemented surface methods.
func (UnimplementedAgentProvider) CreateSession(context.Context, *CreateAgentProviderSessionRequest) (*AgentSession, error) {
	return nil, Unimplemented("agent create session is not implemented")
}

// GetSession returns Unimplemented; embed UnimplementedAgentProvider to default
// unimplemented surface methods.
func (UnimplementedAgentProvider) GetSession(context.Context, *GetAgentProviderSessionRequest) (*AgentSession, error) {
	return nil, Unimplemented("agent get session is not implemented")
}

// ListSessions returns Unimplemented; embed UnimplementedAgentProvider to default
// unimplemented surface methods.
func (UnimplementedAgentProvider) ListSessions(context.Context, *ListAgentProviderSessionsRequest) (*ListAgentProviderSessionsResponse, error) {
	return nil, Unimplemented("agent list sessions is not implemented")
}

// UpdateSession returns Unimplemented; embed UnimplementedAgentProvider to default
// unimplemented surface methods.
func (UnimplementedAgentProvider) UpdateSession(context.Context, *UpdateAgentProviderSessionRequest) (*AgentSession, error) {
	return nil, Unimplemented("agent update session is not implemented")
}

// CreateTurn returns Unimplemented; embed UnimplementedAgentProvider to default
// unimplemented surface methods.
func (UnimplementedAgentProvider) CreateTurn(context.Context, *CreateAgentProviderTurnRequest) (*AgentTurn, error) {
	return nil, Unimplemented("agent create turn is not implemented")
}

// GetTurn returns Unimplemented; embed UnimplementedAgentProvider to default
// unimplemented surface methods.
func (UnimplementedAgentProvider) GetTurn(context.Context, *GetAgentProviderTurnRequest) (*AgentTurn, error) {
	return nil, Unimplemented("agent get turn is not implemented")
}

// ListTurns returns Unimplemented; embed UnimplementedAgentProvider to default
// unimplemented surface methods.
func (UnimplementedAgentProvider) ListTurns(context.Context, *ListAgentProviderTurnsRequest) (*ListAgentProviderTurnsResponse, error) {
	return nil, Unimplemented("agent list turns is not implemented")
}

// CancelTurn returns Unimplemented; embed UnimplementedAgentProvider to default
// unimplemented surface methods.
func (UnimplementedAgentProvider) CancelTurn(context.Context, *CancelAgentProviderTurnRequest) (*AgentTurn, error) {
	return nil, Unimplemented("agent cancel turn is not implemented")
}

// ListTurnEvents returns Unimplemented; embed UnimplementedAgentProvider to default
// unimplemented surface methods.
func (UnimplementedAgentProvider) ListTurnEvents(context.Context, *ListAgentProviderTurnEventsRequest) (*ListAgentProviderTurnEventsResponse, error) {
	return nil, Unimplemented("agent list turn events is not implemented")
}

// GetInteraction returns Unimplemented; embed UnimplementedAgentProvider to default
// unimplemented surface methods.
func (UnimplementedAgentProvider) GetInteraction(context.Context, *GetAgentProviderInteractionRequest) (*AgentInteraction, error) {
	return nil, Unimplemented("agent get interaction is not implemented")
}

// ListInteractions returns Unimplemented; embed UnimplementedAgentProvider to default
// unimplemented surface methods.
func (UnimplementedAgentProvider) ListInteractions(context.Context, *ListAgentProviderInteractionsRequest) (*ListAgentProviderInteractionsResponse, error) {
	return nil, Unimplemented("agent list interactions is not implemented")
}

// ResolveInteraction returns Unimplemented; embed UnimplementedAgentProvider to default
// unimplemented surface methods.
func (UnimplementedAgentProvider) ResolveInteraction(context.Context, *ResolveAgentProviderInteractionRequest) (*AgentInteraction, error) {
	return nil, Unimplemented("agent resolve interaction is not implemented")
}

// GetCapabilities returns Unimplemented; embed UnimplementedAgentProvider to default
// unimplemented surface methods.
func (UnimplementedAgentProvider) GetCapabilities(context.Context, *GetAgentProviderCapabilitiesRequest) (*AgentProviderCapabilities, error) {
	return nil, Unimplemented("agent get capabilities is not implemented")
}

// AgentMessage is one provider-visible conversation message.
type AgentMessage struct {
	Role     string
	Text     string
	Parts    []AgentMessagePart
	Metadata map[string]any
}

// AgentMessagePartToolCall is the native message type for gestalt.provider.v1.AgentMessagePartToolCall.
type AgentMessagePartToolCall struct {
	ID        string
	ToolID    string
	Arguments map[string]any
}

// AgentMessagePartToolResult is the native message type for gestalt.provider.v1.AgentMessagePartToolResult.
type AgentMessagePartToolResult struct {
	ToolCallID string
	Status     int32
	Content    string
	Output     map[string]any
}

// AgentMessagePartImageRef is the native message type for gestalt.provider.v1.AgentMessagePartImageRef.
type AgentMessagePartImageRef struct {
	URI      string
	MimeType string
}

// AgentMessagePart is the native message type for gestalt.provider.v1.AgentMessagePart.
type AgentMessagePart struct {
	Type       AgentMessagePartType
	Text       string
	JSON       map[string]any
	ToolCall   *AgentMessagePartToolCall
	ToolResult *AgentMessagePartToolResult
	ImageRef   *AgentMessagePartImageRef
}

// AgentPreparedWorkspace describes the workspace a provider prepared for a session.
type AgentPreparedWorkspace struct {
	Root string
	Cwd  string
}

// AgentToolRef is the native message type for gestalt.provider.v1.AgentToolRef.
type AgentToolRef struct {
	App            string
	Operation      string
	Connection     string
	Instance       string
	Title          string
	Description    string
	CredentialMode string
	System         string
	RunAs          *Subject
}

// AgentProviderCapabilities is the native message type for gestalt.provider.v1.AgentProviderCapabilities.
type AgentProviderCapabilities struct {
	StreamingText             bool
	ToolCalls                 bool
	ParallelToolCalls         bool
	Interactions              bool
	ResumableTurns            bool
	ReasoningSummaries        bool
	BoundedListHydration      bool
	SupportedToolSources      []AgentToolSourceMode
	SupportsSessionStart      bool
	SupportsPreparedWorkspace bool
}

// GetAgentProviderCapabilitiesRequest is the native message type for gestalt.provider.v1.GetAgentProviderCapabilitiesRequest.
type GetAgentProviderCapabilitiesRequest struct{}

// AgentInteraction is the native message type for gestalt.provider.v1.AgentInteraction.
type AgentInteraction struct {
	ID         string
	Type       AgentInteractionType
	State      AgentInteractionState
	Title      string
	Prompt     string
	Request    map[string]any
	Resolution map[string]any
	CreatedAt  time.Time
	ResolvedAt *time.Time
	TurnID     string
	SessionID  string
}

// AgentSession is the native message type for gestalt.provider.v1.AgentSession.
type AgentSession struct {
	ID                 string
	ProviderName       string
	Model              string
	ClientRef          string
	State              AgentSessionState
	Metadata           map[string]any
	CreatedBySubjectID string
	CreatedAt          time.Time
	UpdatedAt          time.Time
	LastTurnAt         *time.Time
}

// CreateAgentProviderSessionRequest is the native message type for gestalt.provider.v1.CreateAgentProviderSessionRequest.
type CreateAgentProviderSessionRequest struct {
	ProviderName       string
	IdempotencyKey     string
	Model              string
	ClientRef          string
	Metadata           map[string]any
	CreatedBySubjectID string
	Subject            *Subject
	Context            *proto.RequestContext
	SessionStart       *AgentSessionStartConfig
	PreparedWorkspace  *AgentPreparedWorkspace
	Workspace          *AgentWorkspace
	Tools              AgentToolConfig
}

// AgentToolConfig is the native message type for gestalt.provider.v1.AgentToolConfig.
type AgentToolConfig interface {
	agentToolConfig()
}

// AgentNoTools is the native message type for gestalt.provider.v1.AgentNoTools.
type AgentNoTools struct{}

func (*AgentNoTools) agentToolConfig() {}

// AgentCatalogToolConfig is the native message type for gestalt.provider.v1.AgentCatalogToolConfig.
type AgentCatalogToolConfig struct {
	Refs  []AgentToolRef
	Tools []ListedAgentTool
}

func (*AgentCatalogToolConfig) agentToolConfig() {}

// AgentSessionStartConfig is the native message type for gestalt.provider.v1.AgentSessionStartConfig.
type AgentSessionStartConfig struct {
	Hooks []AgentSessionStartHook
}

// AgentSessionStartHook is the native message type for gestalt.provider.v1.AgentSessionStartHook.
type AgentSessionStartHook struct {
	ID      string
	Type    string
	Command []string
	Cwd     string
	Timeout string
	Env     map[string]string
	Output  *AgentSessionStartHookOutput
}

// AgentSessionStartHookOutput is the native message type for gestalt.provider.v1.AgentSessionStartHookOutput.
type AgentSessionStartHookOutput struct {
	AdditionalContext bool
	Metadata          bool
}

// GetAgentProviderSessionRequest is the native message type for gestalt.provider.v1.GetAgentProviderSessionRequest.
type GetAgentProviderSessionRequest struct {
	ProviderName string
	SessionID    string
	Subject      *Subject
	Context      *proto.RequestContext
}

// ListAgentProviderSessionsRequest is the native message type for gestalt.provider.v1.ListAgentProviderSessionsRequest.
type ListAgentProviderSessionsRequest struct {
	ProviderName string
	Subject      *Subject
	Context      *proto.RequestContext
	SessionIDs   []string
	State        AgentSessionState
	Limit        int32
	SummaryOnly  bool
}

// ListAgentProviderSessionsResponse is the native message type for gestalt.provider.v1.ListAgentProviderSessionsResponse.
type ListAgentProviderSessionsResponse struct {
	Sessions []AgentSession
}

// UpdateAgentProviderSessionRequest is the native message type for gestalt.provider.v1.UpdateAgentProviderSessionRequest.
type UpdateAgentProviderSessionRequest struct {
	ProviderName string
	SessionID    string
	ClientRef    string
	State        AgentSessionState
	Metadata     map[string]any
	Subject      *Subject
	Context      *proto.RequestContext
}

// AgentTurn is the native message type for gestalt.provider.v1.AgentTurn.
type AgentTurn struct {
	ID                 string
	SessionID          string
	ProviderName       string
	Model              string
	Status             AgentExecutionStatus
	Messages           []AgentMessage
	Output             *AgentTurnOutput
	StatusMessage      string
	CreatedBySubjectID string
	CreatedAt          time.Time
	StartedAt          *time.Time
	CompletedAt        *time.Time
	ExecutionRef       string
}

// AgentTurnOutput is the structured-or-text output of a finished turn.
type AgentTurnOutput struct {
	Text       *AgentTurnTextOutput
	Structured *AgentTurnStructuredOutput
}

// AgentTurnTextOutput is the native message type for gestalt.provider.v1.AgentTurnTextOutput.
type AgentTurnTextOutput struct {
	Text string
}

// AgentTurnStructuredOutput is the native message type for gestalt.provider.v1.AgentTurnStructuredOutput.
type AgentTurnStructuredOutput struct {
	Text  string
	Value map[string]any
}

// AgentTurnDisplay is the native message type for gestalt.provider.v1.AgentTurnDisplay.
type AgentTurnDisplay struct {
	Kind      string
	Phase     string
	Text      string
	Label     string
	Ref       string
	ParentRef string
	Input     any
	Output    any
	Error     any
	Action    string
	Format    string
	Language  string
}

// CreateAgentProviderTurnRequest is the native message type for gestalt.provider.v1.CreateAgentProviderTurnRequest.
type CreateAgentProviderTurnRequest struct {
	ProviderName       string
	TurnID             string
	SessionID          string
	IdempotencyKey     string
	Model              string
	Messages           []AgentMessage
	Output             *AgentOutput
	Metadata           map[string]any
	CreatedBySubjectID string
	ExecutionRef       string
	Subject            *Subject
	ModelOptions       map[string]any
	Context            *proto.RequestContext
	TimeoutSeconds     int32
}

// AgentOutput is the native message type for gestalt.provider.v1.AgentOutput.
type AgentOutput struct {
	Text       *AgentTextOutput
	Structured *AgentStructuredOutput
}

// AgentTextOutput is the native message type for gestalt.provider.v1.AgentTextOutput.
type AgentTextOutput struct{}

// AgentStructuredOutput is the native message type for gestalt.provider.v1.AgentStructuredOutput.
type AgentStructuredOutput struct {
	Schema map[string]any
}

// GetAgentProviderTurnRequest is the native message type for gestalt.provider.v1.GetAgentProviderTurnRequest.
type GetAgentProviderTurnRequest struct {
	ProviderName string
	TurnID       string
	Subject      *Subject
	Context      *proto.RequestContext
}

// ListAgentProviderTurnsRequest is the native message type for gestalt.provider.v1.ListAgentProviderTurnsRequest.
type ListAgentProviderTurnsRequest struct {
	ProviderName string
	SessionID    string
	Subject      *Subject
	Context      *proto.RequestContext
	TurnIDs      []string
	Status       AgentExecutionStatus
	Limit        int32
	SummaryOnly  bool
}

// ListAgentProviderTurnsResponse is the native message type for gestalt.provider.v1.ListAgentProviderTurnsResponse.
type ListAgentProviderTurnsResponse struct {
	Turns []AgentTurn
}

// CancelAgentProviderTurnRequest is the native message type for gestalt.provider.v1.CancelAgentProviderTurnRequest.
type CancelAgentProviderTurnRequest struct {
	ProviderName string
	TurnID       string
	Reason       string
	Subject      *Subject
	Context      *proto.RequestContext
}

// AgentTurnEvent is the native message type for gestalt.provider.v1.AgentTurnEvent.
type AgentTurnEvent struct {
	ID         string
	TurnID     string
	Seq        int64
	Type       string
	Source     string
	Visibility string
	Data       map[string]any
	CreatedAt  time.Time
	Display    *AgentTurnDisplay
}

// ListAgentProviderTurnEventsRequest is the native message type for gestalt.provider.v1.ListAgentProviderTurnEventsRequest.
type ListAgentProviderTurnEventsRequest struct {
	ProviderName string
	TurnID       string
	AfterSeq     int64
	Limit        int32
	Subject      *Subject
	Context      *proto.RequestContext
}

// ListAgentProviderTurnEventsResponse is the native message type for gestalt.provider.v1.ListAgentProviderTurnEventsResponse.
type ListAgentProviderTurnEventsResponse struct {
	Events []AgentTurnEvent
}

// GetAgentProviderInteractionRequest is the native message type for gestalt.provider.v1.GetAgentProviderInteractionRequest.
type GetAgentProviderInteractionRequest struct {
	InteractionID string
	Subject       *Subject
	Context       *proto.RequestContext
}

// ListAgentProviderInteractionsRequest is the native message type for gestalt.provider.v1.ListAgentProviderInteractionsRequest.
type ListAgentProviderInteractionsRequest struct {
	ProviderName string
	TurnID       string
	Subject      *Subject
	Context      *proto.RequestContext
}

// ListAgentProviderInteractionsResponse is the native message type for gestalt.provider.v1.ListAgentProviderInteractionsResponse.
type ListAgentProviderInteractionsResponse struct {
	Interactions []AgentInteraction
}

// ResolveAgentProviderInteractionRequest is the native message type for gestalt.provider.v1.ResolveAgentProviderInteractionRequest.
type ResolveAgentProviderInteractionRequest struct {
	ProviderName  string
	TurnID        string
	InteractionID string
	Resolution    map[string]any
	Subject       *Subject
	Context       *proto.RequestContext
}

// AgentToolAnnotations carries the MCP-style behavior hints of a tool.
type AgentToolAnnotations struct {
	ReadOnlyHint    *bool
	IdempotentHint  *bool
	DestructiveHint *bool
	OpenWorldHint   *bool
}

// ListedAgentTool is the native message type for gestalt.provider.v1.ListedAgentTool.
type ListedAgentTool struct {
	ID           string
	MCPName      string
	Title        string
	Description  string
	InputSchema  string
	OutputSchema string
	Annotations  *AgentToolAnnotations
	Ref          *AgentToolRef
	Tags         []string
	SearchText   string
}

type (
	// AgentMessagePartType identifies the payload kind in an agent message part.
	AgentMessagePartType int32
	// AgentToolSourceMode is the native message type for gestalt.provider.v1.AgentToolSourceMode.
	AgentToolSourceMode int32
	// AgentExecutionStatus is the native message type for gestalt.provider.v1.AgentExecutionStatus.
	AgentExecutionStatus int32
	// AgentSessionState is the native message type for gestalt.provider.v1.AgentSessionState.
	AgentSessionState int32
	// AgentInteractionType is the native message type for gestalt.provider.v1.AgentInteractionType.
	AgentInteractionType int32
	// AgentInteractionState is the native message type for gestalt.provider.v1.AgentInteractionState.
	AgentInteractionState int32
)

// Agent protocol enum constants provide stable SDK names for common generated
// enum values.
const (
	AgentMessagePartTypeUnspecified AgentMessagePartType = AgentMessagePartType(proto.AgentMessagePartType_AGENT_MESSAGE_PART_TYPE_UNSPECIFIED)
	AgentMessagePartTypeText        AgentMessagePartType = AgentMessagePartType(proto.AgentMessagePartType_AGENT_MESSAGE_PART_TYPE_TEXT)
	AgentMessagePartTypeJSON        AgentMessagePartType = AgentMessagePartType(proto.AgentMessagePartType_AGENT_MESSAGE_PART_TYPE_JSON)
	AgentMessagePartTypeToolCall    AgentMessagePartType = AgentMessagePartType(proto.AgentMessagePartType_AGENT_MESSAGE_PART_TYPE_TOOL_CALL)
	AgentMessagePartTypeToolResult  AgentMessagePartType = AgentMessagePartType(proto.AgentMessagePartType_AGENT_MESSAGE_PART_TYPE_TOOL_RESULT)
	AgentMessagePartTypeImageRef    AgentMessagePartType = AgentMessagePartType(proto.AgentMessagePartType_AGENT_MESSAGE_PART_TYPE_IMAGE_REF)

	AgentToolSourceModeUnspecified AgentToolSourceMode = AgentToolSourceMode(proto.AgentToolSourceMode_AGENT_TOOL_SOURCE_MODE_UNSPECIFIED)
	AgentToolSourceModeCatalog     AgentToolSourceMode = AgentToolSourceMode(proto.AgentToolSourceMode_AGENT_TOOL_SOURCE_MODE_CATALOG)
	AgentToolSourceModeNone        AgentToolSourceMode = AgentToolSourceMode(proto.AgentToolSourceMode_AGENT_TOOL_SOURCE_MODE_NONE)

	AgentExecutionStatusUnspecified     AgentExecutionStatus = AgentExecutionStatus(proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_UNSPECIFIED)
	AgentExecutionStatusPending         AgentExecutionStatus = AgentExecutionStatus(proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_PENDING)
	AgentExecutionStatusRunning         AgentExecutionStatus = AgentExecutionStatus(proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_RUNNING)
	AgentExecutionStatusSucceeded       AgentExecutionStatus = AgentExecutionStatus(proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_SUCCEEDED)
	AgentExecutionStatusFailed          AgentExecutionStatus = AgentExecutionStatus(proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_FAILED)
	AgentExecutionStatusCanceled        AgentExecutionStatus = AgentExecutionStatus(proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_CANCELED)
	AgentExecutionStatusWaitingForInput AgentExecutionStatus = AgentExecutionStatus(proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_WAITING_FOR_INPUT)

	AgentSessionStateUnspecified AgentSessionState = AgentSessionState(proto.AgentSessionState_AGENT_SESSION_STATE_UNSPECIFIED)
	AgentSessionStateActive      AgentSessionState = AgentSessionState(proto.AgentSessionState_AGENT_SESSION_STATE_ACTIVE)
	AgentSessionStateArchived    AgentSessionState = AgentSessionState(proto.AgentSessionState_AGENT_SESSION_STATE_ARCHIVED)

	AgentInteractionTypeUnspecified   AgentInteractionType = AgentInteractionType(proto.AgentInteractionType_AGENT_INTERACTION_TYPE_UNSPECIFIED)
	AgentInteractionTypeInput         AgentInteractionType = AgentInteractionType(proto.AgentInteractionType_AGENT_INTERACTION_TYPE_INPUT)
	AgentInteractionTypeApproval      AgentInteractionType = AgentInteractionType(proto.AgentInteractionType_AGENT_INTERACTION_TYPE_APPROVAL)
	AgentInteractionTypeClarification AgentInteractionType = AgentInteractionType(proto.AgentInteractionType_AGENT_INTERACTION_TYPE_CLARIFICATION)

	AgentInteractionStateUnspecified AgentInteractionState = AgentInteractionState(proto.AgentInteractionState_AGENT_INTERACTION_STATE_UNSPECIFIED)
	AgentInteractionStatePending     AgentInteractionState = AgentInteractionState(proto.AgentInteractionState_AGENT_INTERACTION_STATE_PENDING)
	AgentInteractionStateResolved    AgentInteractionState = AgentInteractionState(proto.AgentInteractionState_AGENT_INTERACTION_STATE_RESOLVED)
	AgentInteractionStateCanceled    AgentInteractionState = AgentInteractionState(proto.AgentInteractionState_AGENT_INTERACTION_STATE_CANCELED)
)
