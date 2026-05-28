package agent

import (
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

type Actor = core.Actor

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
	System                string `json:",omitempty"`
	App                   string
	Operation             string
	Connection            string
	Instance              string
	CredentialMode        core.ConnectionMode
	Unavailable           *UnavailableToolTarget `json:",omitempty"`
	RunAs                 *core.RunAsSubject
	RunAsExternalIdentity *core.ExternalIdentityRef
}

type UnavailableToolTarget struct {
	Reason  string
	Message string
}

const (
	ToolUnavailableReasonReconnectRequired = "reconnect_required"
	ToolUnavailableReasonNotAuthenticated  = "not_authenticated"
	ToolUnavailableReasonNoCredential      = "no_credential"
	ToolUnavailableReasonScopeDenied       = "scope_denied"
	ToolUnavailableReasonInstanceRequired  = "instance_required"
)

type Tool struct {
	ID               string
	Name             string
	Description      string
	ParametersSchema map[string]any
	Target           ToolTarget
	Hidden           bool
}

type ToolRef struct {
	System                string `json:",omitempty"`
	App                   string
	Operation             string
	Connection            string
	Instance              string
	CredentialMode        core.ConnectionMode
	RunAs                 *core.RunAsSubject
	RunAsExternalIdentity *core.ExternalIdentityRef
	Title                 string
	Description           string
}

type ToolSourceMode string

const (
	ToolSourceModeUnspecified ToolSourceMode = ""
	ToolSourceModeMCPCatalog  ToolSourceMode = "mcp_catalog"
	ToolSourceModeNone        ToolSourceMode = "none"
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

type Workspace struct {
	Checkouts []WorkspaceGitCheckout
	CWD       string
}

type WorkspaceGitCheckout struct {
	URL  string
	Ref  string
	Path string
}

type PreparedWorkspace struct {
	Root string
	CWD  string
}

type SessionStartConfig struct {
	Hooks []SessionStartHook
}

type SessionStartHook struct {
	ID      string
	Type    string
	Command []string
	CWD     string
	Timeout string
	Env     map[string]string
	Output  SessionStartHookOutput
}

type SessionStartHookOutput struct {
	AdditionalContext bool
	Metadata          bool
}

type Turn struct {
	ID            string
	SessionID     string
	ProviderName  string
	Model         string
	Status        ExecutionStatus
	Messages      []Message
	Output        TurnOutput
	StatusMessage string
	CreatedBy     Actor
	CreatedAt     *time.Time
	StartedAt     *time.Time
	CompletedAt   *time.Time
	ExecutionRef  string
}

type Output struct {
	Text       *TextOutput
	Structured *StructuredOutput
}

type TextOutput struct{}

type StructuredOutput struct {
	Schema map[string]any
}

type TurnOutput struct {
	Text       *TurnTextOutput
	Structured *TurnStructuredOutput
}

type TurnTextOutput struct {
	Text string
}

type TurnStructuredOutput struct {
	Text  string
	Value map[string]any
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

type ProviderCapabilities struct {
	StreamingText             bool
	ToolCalls                 bool
	ParallelToolCalls         bool
	Interactions              bool
	ResumableTurns            bool
	ReasoningSummaries        bool
	SupportsSessionStart      bool
	SupportsPreparedWorkspace bool
	// BoundedListHydration means provider list APIs can apply Limit and SummaryOnly
	// without hydrating every source record, while preserving the list ordering
	// contract before applying Limit.
	BoundedListHydration bool
	SupportedToolSources []ToolSourceMode
}

type ListedTool struct {
	ToolID           string
	MCPName          string
	Title            string
	Description      string
	Tags             []string
	SearchText       string
	InputSchemaJSON  string
	OutputSchemaJSON string
	Annotations      core.CapabilityAnnotations
	Ref              ToolRef
	Target           ToolTarget
	Hidden           bool
}

type ResolvedConnection struct {
	ConnectionID string
	Connection   string
	Instance     string
	Mode         core.ConnectionMode
	Headers      map[string]string
	Params       map[string]string
	ExpiresAt    *time.Time
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
