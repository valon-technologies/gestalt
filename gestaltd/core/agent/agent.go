package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
)

type ExecutionStatus string

const (
	ExecutionStatusPending         ExecutionStatus = "pending"
	ExecutionStatusRunning         ExecutionStatus = "running"
	ExecutionStatusSucceeded       ExecutionStatus = "succeeded"
	ExecutionStatusFailed          ExecutionStatus = "failed"
	ExecutionStatusCanceled        ExecutionStatus = "canceled"
	ExecutionStatusWaitingForInput ExecutionStatus = "waiting_for_input"
)

func ExecutionStatusIsLive(status ExecutionStatus) bool {
	switch status {
	case ExecutionStatusPending, ExecutionStatusRunning, ExecutionStatusWaitingForInput:
		return true
	default:
		return false
	}
}

type SessionState string

const (
	SessionStateActive   SessionState = "active"
	SessionStateArchived SessionState = "archived"
)

type Actor struct {
	SubjectID   string
	SubjectKind string
	DisplayName string
	AuthSource  string
}

type SubjectContext struct {
	SubjectID           string
	SubjectKind         string
	CredentialSubjectID string
	DisplayName         string
	AuthSource          string
}

type Message struct {
	Role     string
	Text     string
	Parts    []MessagePart
	Metadata map[string]any
}

type MessagePartType string

const (
	MessagePartTypeText       MessagePartType = "text"
	MessagePartTypeJSON       MessagePartType = "json"
	MessagePartTypeToolCall   MessagePartType = "tool_call"
	MessagePartTypeToolResult MessagePartType = "tool_result"
	MessagePartTypeImageRef   MessagePartType = "image_ref"
)

type ToolCallPart struct {
	ID        string
	ToolID    string
	Arguments map[string]any
}

type ToolResultPart struct {
	ToolCallID string
	Status     int
	Content    string
	Output     map[string]any
}

type ImageRefPart struct {
	URI      string
	MIMEType string
}

const SystemToolWorkflow = "workflow"

type MessagePart struct {
	Type       MessagePartType
	Text       string
	JSON       map[string]any
	ToolCall   *ToolCallPart
	ToolResult *ToolResultPart
	ImageRef   *ImageRefPart
}

type ToolTarget struct {
	System         string `json:",omitempty"`
	Plugin         string
	Operation      string
	Connection     string
	Instance       string
	CredentialMode core.ConnectionMode
}

type Tool struct {
	ID               string
	Name             string
	Description      string
	ParametersSchema map[string]any
	Target           ToolTarget
	Hidden           bool
}

type ToolRef struct {
	System         string `json:",omitempty"`
	Plugin         string
	Operation      string
	Connection     string
	Instance       string
	CredentialMode core.ConnectionMode
	Title          string
	Description    string
}

type ResolveToolsRequest struct {
	ToolRefs         []ToolRef
	ToolSource       ToolSourceMode
	CallerPluginName string
}

type ToolSourceMode string

const (
	ToolSourceModeUnspecified ToolSourceMode = ""
	ToolSourceModeMCPCatalog  ToolSourceMode = "mcp_catalog"
)

func ValidateMCPCatalogToolRefs(refs []ToolRef, fieldName string) error {
	fieldName = strings.TrimSpace(fieldName)
	if fieldName == "" {
		fieldName = "toolRefs"
	}
	for i := range refs {
		ref := refs[i]
		system := strings.TrimSpace(ref.System)
		pluginName := strings.TrimSpace(ref.Plugin)
		operation := strings.TrimSpace(ref.Operation)
		if operation == "" {
			return fmt.Errorf("mcp catalog %s[%d].operation is required", fieldName, i)
		}
		if system == "" && (pluginName == "" || pluginName == "*") {
			return fmt.Errorf("mcp catalog %s[%d].plugin must be explicit", fieldName, i)
		}
		if system != "" && system != SystemToolWorkflow {
			return fmt.Errorf("mcp catalog %s[%d].system %q is not supported", fieldName, i, system)
		}
		if system != "" && pluginName != "" {
			return fmt.Errorf("mcp catalog %s[%d] must set exactly one of plugin or system", fieldName, i)
		}
	}
	return nil
}

type Session struct {
	ID           string
	ProviderName string
	Model        string
	ClientRef    string
	State        SessionState
	Metadata     map[string]any
	CreatedBy    Actor
	CreatedAt    *time.Time
	UpdatedAt    *time.Time
	LastTurnAt   *time.Time
}

type CreateSessionRequest struct {
	SessionID       string
	IdempotencyKey  string
	Model           string
	ClientRef       string
	Metadata        map[string]any
	ProviderOptions map[string]any
	CreatedBy       Actor
	Subject         SubjectContext
}

type GetSessionRequest struct {
	SessionID string
	Subject   SubjectContext
}

type ListSessionsRequest struct {
	Subject     SubjectContext
	SessionIDs  []string
	State       SessionState
	Limit       int
	SummaryOnly bool
}

type UpdateSessionRequest struct {
	SessionID string
	ClientRef string
	State     SessionState
	Metadata  map[string]any
	Subject   SubjectContext
}

type Turn struct {
	ID               string
	SessionID        string
	ProviderName     string
	Model            string
	Status           ExecutionStatus
	Messages         []Message
	OutputText       string
	StructuredOutput map[string]any
	StatusMessage    string
	CreatedBy        Actor
	CreatedAt        *time.Time
	StartedAt        *time.Time
	CompletedAt      *time.Time
	ExecutionRef     string
}

type CreateTurnRequest struct {
	TurnID          string
	SessionID       string
	IdempotencyKey  string
	Model           string
	Messages        []Message
	ToolRefs        []ToolRef
	ToolSource      ToolSourceMode
	Tools           []Tool
	ResponseSchema  map[string]any
	Metadata        map[string]any
	ProviderOptions map[string]any
	CreatedBy       Actor
	ExecutionRef    string
	Subject         SubjectContext
	ToolGrant       string
}

type GetTurnRequest struct {
	TurnID  string
	Subject SubjectContext
}

type ListTurnsRequest struct {
	SessionID   string
	Subject     SubjectContext
	TurnIDs     []string
	Status      ExecutionStatus
	Limit       int
	SummaryOnly bool
}

type CancelTurnRequest struct {
	TurnID  string
	Reason  string
	Subject SubjectContext
}

type TurnEvent struct {
	ID         string
	TurnID     string
	Seq        int64
	Type       string
	Source     string
	Visibility string
	Data       map[string]any
	CreatedAt  *time.Time
	Display    *TurnDisplay
}

type TurnDisplay struct {
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

type ListTurnEventsRequest struct {
	TurnID   string
	AfterSeq int64
	Limit    int
	Subject  SubjectContext
}

type GetCapabilitiesRequest struct{}

type ProviderCapabilities struct {
	StreamingText        bool
	ToolCalls            bool
	ParallelToolCalls    bool
	StructuredOutput     bool
	Interactions         bool
	ResumableTurns       bool
	ReasoningSummaries   bool
	BoundedListHydration bool
	SupportedToolSources []ToolSourceMode
}

type GetInteractionRequest struct {
	InteractionID string
	Subject       SubjectContext
}

type ListInteractionsRequest struct {
	TurnID  string
	Subject SubjectContext
}

type ResolveInteractionRequest struct {
	InteractionID string
	Resolution    map[string]any
	Subject       SubjectContext
}

type ExecuteToolRequest struct {
	ProviderName   string
	SessionID      string
	TurnID         string
	ToolCallID     string
	ToolID         string
	Arguments      map[string]any
	ToolGrant      string
	IdempotencyKey string
}

type ExecuteToolResponse struct {
	Status int
	Body   string
}

type ListedTool struct {
	ToolID           string
	MCPName          string
	Title            string
	Description      string
	InputSchemaJSON  string
	OutputSchemaJSON string
	Annotations      core.CapabilityAnnotations
	Ref              ToolRef
	Target           ToolTarget
	Hidden           bool
}

type ListToolsRequest struct {
	ProviderName string
	SessionID    string
	TurnID       string
	PageSize     int
	PageToken    string
	ToolRefs     []ToolRef
	ToolSource   ToolSourceMode
	ToolGrant    string
}

type ListToolsResponse struct {
	Tools         []ListedTool
	NextPageToken string
}

type InteractionType string

const (
	InteractionTypeApproval      InteractionType = "approval"
	InteractionTypeClarification InteractionType = "clarification"
	InteractionTypeInput         InteractionType = "input"
)

type InteractionState string

const (
	InteractionStatePending  InteractionState = "pending"
	InteractionStateResolved InteractionState = "resolved"
	InteractionStateCanceled InteractionState = "canceled"
)

type Interaction struct {
	ID         string
	TurnID     string
	SessionID  string
	Type       InteractionType
	State      InteractionState
	Title      string
	Prompt     string
	Request    map[string]any
	Resolution map[string]any
	CreatedAt  *time.Time
	ResolvedAt *time.Time
}

type ManagerCreateSessionRequest struct {
	IdempotencyKey  string
	ProviderName    string
	Model           string
	ClientRef       string
	Metadata        map[string]any
	ProviderOptions map[string]any
}

type ManagerUpdateSessionRequest struct {
	SessionID string
	ClientRef string
	State     SessionState
	Metadata  map[string]any
}

type ManagerCreateTurnRequest struct {
	CallerPluginName string
	IdempotencyKey   string
	Model            string
	SessionID        string
	Messages         []Message
	ToolRefs         []ToolRef
	ToolSource       ToolSourceMode
	ResponseSchema   map[string]any
	Metadata         map[string]any
	ProviderOptions  map[string]any
}

type ManagerListSessionsRequest struct {
	ProviderName string
	State        SessionState
	Limit        int
	SummaryOnly  bool
}

type ManagerListTurnsRequest struct {
	SessionID   string
	Status      ExecutionStatus
	Limit       int
	SummaryOnly bool
}

type ManagerCancelTurnRequest struct {
	TurnID string
	Reason string
}

type Provider interface {
	CreateSession(ctx context.Context, req CreateSessionRequest) (*Session, error)
	GetSession(ctx context.Context, req GetSessionRequest) (*Session, error)
	ListSessions(ctx context.Context, req ListSessionsRequest) ([]*Session, error)
	UpdateSession(ctx context.Context, req UpdateSessionRequest) (*Session, error)
	CreateTurn(ctx context.Context, req CreateTurnRequest) (*Turn, error)
	GetTurn(ctx context.Context, req GetTurnRequest) (*Turn, error)
	ListTurns(ctx context.Context, req ListTurnsRequest) ([]*Turn, error)
	CancelTurn(ctx context.Context, req CancelTurnRequest) (*Turn, error)
	ListTurnEvents(ctx context.Context, req ListTurnEventsRequest) ([]*TurnEvent, error)
	GetInteraction(ctx context.Context, req GetInteractionRequest) (*Interaction, error)
	ListInteractions(ctx context.Context, req ListInteractionsRequest) ([]*Interaction, error)
	ResolveInteraction(ctx context.Context, req ResolveInteractionRequest) (*Interaction, error)
	GetCapabilities(ctx context.Context, req GetCapabilitiesRequest) (*ProviderCapabilities, error)
	Ping(ctx context.Context) error
	Close() error
}

type Host interface {
	ListTools(ctx context.Context, req ListToolsRequest) (*ListToolsResponse, error)
	ExecuteTool(ctx context.Context, req ExecuteToolRequest) (*ExecuteToolResponse, error)
}

type UnimplementedProvider struct{}

func (UnimplementedProvider) CreateSession(context.Context, CreateSessionRequest) (*Session, error) {
	return nil, fmt.Errorf("agent provider create session is not implemented")
}

func (UnimplementedProvider) GetSession(context.Context, GetSessionRequest) (*Session, error) {
	return nil, fmt.Errorf("agent provider get session is not implemented")
}

func (UnimplementedProvider) ListSessions(context.Context, ListSessionsRequest) ([]*Session, error) {
	return nil, fmt.Errorf("agent provider list sessions is not implemented")
}

func (UnimplementedProvider) UpdateSession(context.Context, UpdateSessionRequest) (*Session, error) {
	return nil, fmt.Errorf("agent provider update session is not implemented")
}

func (UnimplementedProvider) CreateTurn(context.Context, CreateTurnRequest) (*Turn, error) {
	return nil, fmt.Errorf("agent provider create turn is not implemented")
}

func (UnimplementedProvider) GetTurn(context.Context, GetTurnRequest) (*Turn, error) {
	return nil, fmt.Errorf("agent provider get turn is not implemented")
}

func (UnimplementedProvider) ListTurns(context.Context, ListTurnsRequest) ([]*Turn, error) {
	return nil, fmt.Errorf("agent provider list turns is not implemented")
}

func (UnimplementedProvider) CancelTurn(context.Context, CancelTurnRequest) (*Turn, error) {
	return nil, fmt.Errorf("agent provider cancel turn is not implemented")
}

func (UnimplementedProvider) ListTurnEvents(context.Context, ListTurnEventsRequest) ([]*TurnEvent, error) {
	return nil, fmt.Errorf("agent provider list turn events is not implemented")
}

func (UnimplementedProvider) GetInteraction(context.Context, GetInteractionRequest) (*Interaction, error) {
	return nil, fmt.Errorf("agent provider get interaction is not implemented")
}

func (UnimplementedProvider) ListInteractions(context.Context, ListInteractionsRequest) ([]*Interaction, error) {
	return nil, fmt.Errorf("agent provider list interactions is not implemented")
}

func (UnimplementedProvider) ResolveInteraction(context.Context, ResolveInteractionRequest) (*Interaction, error) {
	return nil, fmt.Errorf("agent provider resolve interaction is not implemented")
}

func (UnimplementedProvider) GetCapabilities(context.Context, GetCapabilitiesRequest) (*ProviderCapabilities, error) {
	return nil, fmt.Errorf("agent provider get capabilities is not implemented")
}

func (UnimplementedProvider) Ping(context.Context) error {
	return fmt.Errorf("agent provider ping is not implemented")
}

func (UnimplementedProvider) Close() error {
	return nil
}
