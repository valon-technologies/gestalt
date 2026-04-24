package agent

import (
	"context"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
)

type RunStatus string

const (
	RunStatusPending         RunStatus = "pending"
	RunStatusRunning         RunStatus = "running"
	RunStatusSucceeded       RunStatus = "succeeded"
	RunStatusFailed          RunStatus = "failed"
	RunStatusCanceled        RunStatus = "canceled"
	RunStatusWaitingForInput RunStatus = "waiting_for_input"
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

type MessagePart struct {
	Type       MessagePartType
	Text       string
	JSON       map[string]any
	ToolCall   *ToolCallPart
	ToolResult *ToolResultPart
	ImageRef   *ImageRefPart
}

type ToolTarget struct {
	PluginName string
	Operation  string
	Connection string
	Instance   string
}

type Tool struct {
	ID               string
	Name             string
	Description      string
	ParametersSchema map[string]any
	Target           ToolTarget
}

type ToolRef struct {
	PluginName  string
	Operation   string
	Connection  string
	Instance    string
	Title       string
	Description string
}

type ToolSourceMode string

const (
	ToolSourceModeUnspecified    ToolSourceMode = ""
	ToolSourceModeExplicit       ToolSourceMode = "explicit"
	ToolSourceModeInheritInvokes ToolSourceMode = "inherit_invokes"
)

type Run struct {
	ID               string
	ProviderName     string
	Model            string
	Status           RunStatus
	SessionRef       string
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

type StartRunRequest struct {
	RunID           string
	IdempotencyKey  string
	ProviderName    string
	Model           string
	Messages        []Message
	Tools           []Tool
	ResponseSchema  map[string]any
	SessionRef      string
	Metadata        map[string]any
	ProviderOptions map[string]any
	CreatedBy       Actor
	ExecutionRef    string
}

type GetRunRequest struct {
	RunID string
}

type ListRunsRequest struct{}

type CancelRunRequest struct {
	RunID  string
	Reason string
}

type GetCapabilitiesRequest struct{}

type ProviderCapabilities struct {
	StreamingText       bool
	ToolCalls           bool
	ParallelToolCalls   bool
	StructuredOutput    bool
	SessionContinuation bool
	Approvals           bool
	ResumableRuns       bool
	ReasoningSummaries  bool
}

type ResumeRunRequest struct {
	RunID         string
	InteractionID string
	Resolution    map[string]any
}

type ExecuteToolRequest struct {
	ProviderName string
	RunID        string
	ToolCallID   string
	ToolID       string
	Arguments    map[string]any
}

type ExecuteToolResponse struct {
	Status int
	Body   string
}

type RunEvent struct {
	ID         string
	RunID      string
	Seq        int64
	Type       string
	Source     string
	Visibility string
	Data       map[string]any
	CreatedAt  *time.Time
}

type EmitEventRequest struct {
	ProviderName string
	RunID        string
	Type         string
	Visibility   string
	Data         map[string]any
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
	RunID      string
	Type       InteractionType
	State      InteractionState
	Title      string
	Prompt     string
	Request    map[string]any
	Resolution map[string]any
	CreatedAt  *time.Time
	ResolvedAt *time.Time
}

type RequestInteractionRequest struct {
	ProviderName string
	RunID        string
	Type         InteractionType
	Title        string
	Prompt       string
	Request      map[string]any
}

type ManagedRun struct {
	ProviderName string
	Run          *Run
}

type ManagerRunRequest struct {
	CallerPluginName string
	IdempotencyKey   string
	ProviderName     string
	Model            string
	Messages         []Message
	ToolRefs         []ToolRef
	ToolSource       ToolSourceMode
	ResponseSchema   map[string]any
	SessionRef       string
	Metadata         map[string]any
	ProviderOptions  map[string]any
}

type ManagerGetRunRequest struct {
	RunID string
}

type ManagerListRunsRequest struct{}

type ManagerCancelRunRequest struct {
	RunID  string
	Reason string
}

type ExecutionReference struct {
	ID                  string
	ProviderName        string
	SubjectID           string
	CredentialSubjectID string
	IdempotencyKey      string
	Permissions         []core.AccessPermission
	Tools               []Tool
	CreatedAt           *time.Time
	RevokedAt           *time.Time
}

type Provider interface {
	StartRun(ctx context.Context, req StartRunRequest) (*Run, error)
	GetRun(ctx context.Context, req GetRunRequest) (*Run, error)
	ListRuns(ctx context.Context, req ListRunsRequest) ([]*Run, error)
	CancelRun(ctx context.Context, req CancelRunRequest) (*Run, error)
	GetCapabilities(ctx context.Context, req GetCapabilitiesRequest) (*ProviderCapabilities, error)
	ResumeRun(ctx context.Context, req ResumeRunRequest) (*Run, error)
	Ping(ctx context.Context) error
	Close() error
}

type Host interface {
	ExecuteTool(ctx context.Context, req ExecuteToolRequest) (*ExecuteToolResponse, error)
	EmitEvent(ctx context.Context, req EmitEventRequest) (*RunEvent, error)
	RequestInteraction(ctx context.Context, req RequestInteractionRequest) (*Interaction, error)
}
