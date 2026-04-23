package agent

import (
	"context"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
)

type RunStatus string

const (
	RunStatusPending   RunStatus = "pending"
	RunStatusRunning   RunStatus = "running"
	RunStatusSucceeded RunStatus = "succeeded"
	RunStatusFailed    RunStatus = "failed"
	RunStatusCanceled  RunStatus = "canceled"
)

type Actor struct {
	SubjectID   string
	SubjectKind string
	DisplayName string
	AuthSource  string
}

type Message struct {
	Role string
	Text string
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
	Ping(ctx context.Context) error
	Close() error
}

type Host interface {
	ExecuteTool(ctx context.Context, req ExecuteToolRequest) (*ExecuteToolResponse, error)
}
