package gestalt

import (
	"context"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/protov1/v1"
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

// AgentMessage is one provider-visible conversation message.
type AgentMessage struct {
	Role     string
	Text     string
	Parts    []AgentMessagePart
	Metadata map[string]any
}

type AgentMessagePartToolCall struct {
	ID        string
	ToolID    string
	Arguments map[string]any
}

type AgentMessagePartToolResult struct {
	ToolCallID string
	Status     int32
	Content    string
	Output     map[string]any
}

type AgentMessagePartImageRef struct {
	URI      string
	MimeType string
}

type AgentMessagePart struct {
	Type       AgentMessagePartType
	Text       string
	JSON       map[string]any
	ToolCall   *AgentMessagePartToolCall
	ToolResult *AgentMessagePartToolResult
	ImageRef   *AgentMessagePartImageRef
}

type AgentActor struct {
	SubjectID   string
	SubjectKind string
	DisplayName string
	AuthSource  string
}

type AgentPreparedWorkspace struct {
	Root string
	Cwd  string
}

type ResolvedAgentTool struct {
	ID               string
	Name             string
	Description      string
	ParametersSchema map[string]any
}

type AgentToolRef struct {
	App                string
	Operation             string
	Connection            string
	Instance              string
	Title                 string
	Description           string
	System                string
	RunAs                 *Subject
	RunAsExternalIdentity *ExternalIdentity
}

type AgentProviderCapabilities struct {
	StreamingText             bool
	ToolCalls                 bool
	ParallelToolCalls         bool
	StructuredOutput          bool
	Interactions              bool
	ResumableTurns            bool
	ReasoningSummaries        bool
	BoundedListHydration      bool
	SupportedToolSources      []AgentToolSourceMode
	SupportsSessionStart      bool
	SupportsPreparedWorkspace bool
}

type GetAgentProviderCapabilitiesRequest struct{}

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

type AgentSession struct {
	ID           string
	ProviderName string
	Model        string
	ClientRef    string
	State        AgentSessionState
	Metadata     map[string]any
	CreatedBy    *AgentActor
	CreatedAt    time.Time
	UpdatedAt    time.Time
	LastTurnAt   *time.Time
}

type CreateAgentProviderSessionRequest struct {
	SessionID         string
	IdempotencyKey    string
	Model             string
	ClientRef         string
	Metadata          map[string]any
	CreatedBy         *AgentActor
	Subject           *Subject
	SessionStart      *AgentSessionStartConfig
	PreparedWorkspace *AgentPreparedWorkspace
}

type AgentSessionStartConfig struct {
	Hooks []AgentSessionStartHook
}

type AgentSessionStartHook struct {
	ID      string
	Type    string
	Command []string
	Cwd     string
	Timeout string
	Env     map[string]string
	Output  *AgentSessionStartHookOutput
}

type AgentSessionStartHookOutput struct {
	AdditionalContext bool
	Metadata          bool
}

type GetAgentProviderSessionRequest struct {
	SessionID string
	Subject   *Subject
}

type ListAgentProviderSessionsRequest struct {
	Subject     *Subject
	SessionIDs  []string
	State       AgentSessionState
	Limit       int32
	SummaryOnly bool
}

type ListAgentProviderSessionsResponse struct {
	Sessions []AgentSession
}

type UpdateAgentProviderSessionRequest struct {
	SessionID string
	ClientRef string
	State     AgentSessionState
	Metadata  map[string]any
	Subject   *Subject
}

type AgentTurn struct {
	ID               string
	SessionID        string
	ProviderName     string
	Model            string
	Status           AgentExecutionStatus
	Messages         []AgentMessage
	OutputText       string
	StructuredOutput map[string]any
	StatusMessage    string
	CreatedBy        *AgentActor
	CreatedAt        time.Time
	StartedAt        *time.Time
	CompletedAt      *time.Time
	ExecutionRef     string
}

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

type CreateAgentProviderTurnRequest struct {
	TurnID            string
	SessionID         string
	IdempotencyKey    string
	Model             string
	Messages          []AgentMessage
	Tools             []ResolvedAgentTool
	ResponseSchema    map[string]any
	ResponseSchemaSet bool
	Metadata          map[string]any
	CreatedBy         *AgentActor
	ExecutionRef      string
	ToolRefs          []AgentToolRef
	ToolSource        AgentToolSourceMode
	Subject           *Subject
	ModelOptions      map[string]any
	RunGrant          string
	TimeoutSeconds    int32
}

type GetAgentProviderTurnRequest struct {
	TurnID  string
	Subject *Subject
}

type ListAgentProviderTurnsRequest struct {
	SessionID   string
	Subject     *Subject
	TurnIDs     []string
	Status      AgentExecutionStatus
	Limit       int32
	SummaryOnly bool
}

type ListAgentProviderTurnsResponse struct {
	Turns []AgentTurn
}

type CancelAgentProviderTurnRequest struct {
	TurnID  string
	Reason  string
	Subject *Subject
}

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

type ListAgentProviderTurnEventsRequest struct {
	TurnID   string
	AfterSeq int64
	Limit    int32
	Subject  *Subject
}

type ListAgentProviderTurnEventsResponse struct {
	Events []AgentTurnEvent
}

type GetAgentProviderInteractionRequest struct {
	InteractionID string
	Subject       *Subject
}

type ListAgentProviderInteractionsRequest struct {
	TurnID  string
	Subject *Subject
}

type ListAgentProviderInteractionsResponse struct {
	Interactions []AgentInteraction
}

type ResolveAgentProviderInteractionRequest struct {
	InteractionID string
	Resolution    map[string]any
	Subject       *Subject
}

type ExecuteAgentToolRequest struct {
	SessionID      string
	TurnID         string
	ToolCallID     string
	ToolID         string
	Arguments      map[string]any
	IdempotencyKey string
	RunGrant       string
}

type ExecuteAgentToolResponse struct {
	Status int32
	Body   string
}

type AgentToolAnnotations struct {
	ReadOnlyHint    *bool
	IdempotentHint  *bool
	DestructiveHint *bool
	OpenWorldHint   *bool
}

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

type ListAgentToolsRequest struct {
	SessionID string
	TurnID    string
	PageSize  int32
	PageToken string
	RunGrant  string
	Query     string
}

type ListAgentToolsResponse struct {
	Tools         []ListedAgentTool
	NextPageToken string
}

type ResolveAgentConnectionRequest struct {
	SessionID  string
	TurnID     string
	Connection string
	Instance   string
	RunGrant   string
}

type ResolvedAgentConnection struct {
	ConnectionID string
	Connection   string
	Instance     string
	Mode         string
	Headers      map[string]string
	Params       map[string]string
	ExpiresAt    *time.Time
}

type (
	// AgentMessagePartType identifies the payload kind in an agent message part.
	AgentMessagePartType  int32
	AgentToolSourceMode   int32
	AgentExecutionStatus  int32
	AgentSessionState     int32
	AgentInteractionType  int32
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
	AgentToolSourceModeMCPCatalog  AgentToolSourceMode = AgentToolSourceMode(proto.AgentToolSourceMode_AGENT_TOOL_SOURCE_MODE_MCP_CATALOG)
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
