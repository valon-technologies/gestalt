package agent

import (
	"context"
	"fmt"
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

type ToolCandidate struct {
	Ref         ToolRef
	ID          string
	Name        string
	Description string
	Parameters  []string
	Score       float64
}

type ResolveToolsRequest struct {
	ToolRefs         []ToolRef
	ToolSource       ToolSourceMode
	CallerPluginName string
}

type ToolSourceMode string

const (
	ToolSourceModeUnspecified  ToolSourceMode = ""
	ToolSourceModeNativeSearch ToolSourceMode = "native_search"
)

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
}

type GetSessionRequest struct {
	SessionID string
}

type ListSessionsRequest struct{}

type UpdateSessionRequest struct {
	SessionID string
	ClientRef string
	State     SessionState
	Metadata  map[string]any
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
}

type GetTurnRequest struct {
	TurnID string
}

type ListTurnsRequest struct {
	SessionID string
}

type CancelTurnRequest struct {
	TurnID string
	Reason string
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
}

type ListTurnEventsRequest struct {
	TurnID   string
	AfterSeq int64
	Limit    int
}

type GetCapabilitiesRequest struct{}

type ProviderCapabilities struct {
	StreamingText      bool
	ToolCalls          bool
	ParallelToolCalls  bool
	StructuredOutput   bool
	Interactions       bool
	ResumableTurns     bool
	ReasoningSummaries bool
	NativeToolSearch   bool
}

type GetInteractionRequest struct {
	InteractionID string
}

type ListInteractionsRequest struct {
	TurnID string
}

type ResolveInteractionRequest struct {
	InteractionID string
	Resolution    map[string]any
}

type ExecuteToolRequest struct {
	ProviderName string
	SessionID    string
	TurnID       string
	ToolCallID   string
	ToolID       string
	Arguments    map[string]any
}

type ExecuteToolResponse struct {
	Status int
	Body   string
}

type SearchToolsRequest struct {
	ProviderName   string
	SessionID      string
	TurnID         string
	Query          string
	MaxResults     int
	CandidateLimit int
	LoadRefs       []ToolRef
	ToolRefs       []ToolRef
	ToolSource     ToolSourceMode
}

type SearchToolsResponse struct {
	Tools      []Tool
	Candidates []ToolCandidate
	HasMore    bool
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
}

type ManagerListTurnsRequest struct {
	SessionID string
}

type ManagerCancelTurnRequest struct {
	TurnID string
	Reason string
}

type SessionReference struct {
	ID                  string
	ProviderName        string
	SubjectID           string
	CredentialSubjectID string
	IdempotencyKey      string
	CreatedAt           *time.Time
	ArchivedAt          *time.Time
}

type ExecutionReference struct {
	ID                  string
	SessionID           string
	ProviderName        string
	SubjectID           string
	CredentialSubjectID string
	IdempotencyKey      string
	Permissions         []core.AccessPermission
	ToolRefs            []ToolRef
	ToolSource          ToolSourceMode
	Tools               []Tool
	CreatedAt           *time.Time
	RevokedAt           *time.Time
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
	SearchTools(ctx context.Context, req SearchToolsRequest) (*SearchToolsResponse, error)
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
