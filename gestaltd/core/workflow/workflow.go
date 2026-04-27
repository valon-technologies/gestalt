package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
)

const ConfigManagedSchedulePrefix = "cfg_"

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

type Target struct {
	PluginName string
	Operation  string
	Connection string
	Instance   string
	Input      map[string]any
	Plugin     *PluginTarget
	Agent      *AgentTarget
}

type PluginTarget struct {
	PluginName string
	Operation  string
	Connection string
	Instance   string
	Input      map[string]any
}

type AgentTarget struct {
	ProviderName    string
	Model           string
	Prompt          string
	Messages        []coreagent.Message
	ToolRefs        []coreagent.ToolRef
	ToolSource      coreagent.ToolSourceMode
	ResponseSchema  map[string]any
	ProviderOptions map[string]any
	Metadata        map[string]any
	TimeoutSeconds  int
}

type ExecutionReference struct {
	ID                  string
	ProviderName        string
	Target              Target
	TargetFingerprint   string
	SubjectID           string
	CredentialSubjectID string
	Permissions         []core.AccessPermission
	CreatedAt           *time.Time
	RevokedAt           *time.Time
}

type ExecutionReferenceStore interface {
	PutExecutionReference(ctx context.Context, ref *ExecutionReference) (*ExecutionReference, error)
	GetExecutionReference(ctx context.Context, id string) (*ExecutionReference, error)
	ListExecutionReferences(ctx context.Context, subjectID string) ([]*ExecutionReference, error)
}

type Event struct {
	ID              string
	Source          string
	SpecVersion     string
	Type            string
	Subject         string
	Time            *time.Time
	DataContentType string
	Data            map[string]any
	Extensions      map[string]any
}

type EventMatch struct {
	Type    string
	Source  string
	Subject string
}

type ScheduleTrigger struct {
	ScheduleID   string
	ScheduledFor *time.Time
}

type EventTriggerInvocation struct {
	TriggerID string
	Event     Event
}

type RunTrigger struct {
	Manual   bool
	Schedule *ScheduleTrigger
	Event    *EventTriggerInvocation
}

type Run struct {
	ID            string
	Status        RunStatus
	Target        Target
	Trigger       RunTrigger
	ExecutionRef  string
	CreatedBy     Actor
	CreatedAt     *time.Time
	StartedAt     *time.Time
	CompletedAt   *time.Time
	StatusMessage string
	ResultBody    string
}

type Schedule struct {
	ID           string
	Cron         string
	Timezone     string
	Target       Target
	Paused       bool
	ExecutionRef string
	CreatedBy    Actor
	CreatedAt    *time.Time
	UpdatedAt    *time.Time
	NextRunAt    *time.Time
}

type EventTrigger struct {
	ID           string
	Match        EventMatch
	Target       Target
	Paused       bool
	ExecutionRef string
	CreatedBy    Actor
	CreatedAt    *time.Time
	UpdatedAt    *time.Time
}

type StartRunRequest struct {
	Target         Target
	IdempotencyKey string
	CreatedBy      Actor
	ExecutionRef   string
}

type GetRunRequest struct {
	RunID string
}

type ListRunsRequest struct{}

type CancelRunRequest struct {
	RunID  string
	Reason string
}

type UpsertScheduleRequest struct {
	ScheduleID   string
	Cron         string
	Timezone     string
	Target       Target
	Paused       bool
	RequestedBy  Actor
	ExecutionRef string
}

type ListSchedulesRequest struct{}

type GetScheduleRequest struct {
	ScheduleID string
}

type DeleteScheduleRequest struct {
	ScheduleID string
}

type PauseScheduleRequest struct {
	ScheduleID string
}

type ResumeScheduleRequest struct {
	ScheduleID string
}

type UpsertEventTriggerRequest struct {
	TriggerID    string
	Match        EventMatch
	Target       Target
	Paused       bool
	RequestedBy  Actor
	ExecutionRef string
}

type ListEventTriggersRequest struct{}

type GetEventTriggerRequest struct {
	TriggerID string
}

type DeleteEventTriggerRequest struct {
	TriggerID string
}

type PauseEventTriggerRequest struct {
	TriggerID string
}

type ResumeEventTriggerRequest struct {
	TriggerID string
}

type PublishEventRequest struct {
	PluginName string
	Event      Event
}

type InvokeOperationRequest struct {
	ProviderName string
	RunID        string
	Trigger      RunTrigger
	Target       Target
	Input        map[string]any
	Metadata     map[string]any
	CreatedBy    Actor
	ExecutionRef string
}

type InvokeOperationResponse struct {
	Status int
	Body   string
}

type Provider interface {
	StartRun(ctx context.Context, req StartRunRequest) (*Run, error)
	GetRun(ctx context.Context, req GetRunRequest) (*Run, error)
	ListRuns(ctx context.Context, req ListRunsRequest) ([]*Run, error)
	CancelRun(ctx context.Context, req CancelRunRequest) (*Run, error)
	UpsertSchedule(ctx context.Context, req UpsertScheduleRequest) (*Schedule, error)
	GetSchedule(ctx context.Context, req GetScheduleRequest) (*Schedule, error)
	ListSchedules(ctx context.Context, req ListSchedulesRequest) ([]*Schedule, error)
	DeleteSchedule(ctx context.Context, req DeleteScheduleRequest) error
	PauseSchedule(ctx context.Context, req PauseScheduleRequest) (*Schedule, error)
	ResumeSchedule(ctx context.Context, req ResumeScheduleRequest) (*Schedule, error)
	UpsertEventTrigger(ctx context.Context, req UpsertEventTriggerRequest) (*EventTrigger, error)
	GetEventTrigger(ctx context.Context, req GetEventTriggerRequest) (*EventTrigger, error)
	ListEventTriggers(ctx context.Context, req ListEventTriggersRequest) ([]*EventTrigger, error)
	DeleteEventTrigger(ctx context.Context, req DeleteEventTriggerRequest) error
	PauseEventTrigger(ctx context.Context, req PauseEventTriggerRequest) (*EventTrigger, error)
	ResumeEventTrigger(ctx context.Context, req ResumeEventTriggerRequest) (*EventTrigger, error)
	PublishEvent(ctx context.Context, req PublishEventRequest) error
	Ping(ctx context.Context) error
	Close() error
}

type Host interface {
	InvokeOperation(ctx context.Context, req InvokeOperationRequest) (*InvokeOperationResponse, error)
}

func (t Target) PluginTarget() PluginTarget {
	if t.Plugin != nil {
		return *t.Plugin
	}
	return PluginTarget{
		PluginName: t.PluginName,
		Operation:  t.Operation,
		Connection: t.Connection,
		Instance:   t.Instance,
		Input:      t.Input,
	}
}

func (t Target) AgentTarget() AgentTarget {
	if t.Agent != nil {
		return *t.Agent
	}
	return AgentTarget{}
}

func TargetFingerprint(target Target) (string, error) {
	if target.Agent != nil && PluginTargetSet(target.PluginTarget()) {
		return "", fmt.Errorf("target cannot include both agent and plugin fields")
	}
	payload, err := json.Marshal(normalizedTargetFingerprintPayload(target))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func normalizedTargetFingerprintPayload(target Target) Target {
	out := Target{}
	if target.Agent != nil {
		agentTarget := target.AgentTarget()
		if len(agentTarget.Messages) == 0 {
			agentTarget.Messages = nil
		}
		if len(agentTarget.ToolRefs) == 0 {
			agentTarget.ToolRefs = nil
		}
		if len(agentTarget.ResponseSchema) == 0 {
			agentTarget.ResponseSchema = nil
		}
		if len(agentTarget.ProviderOptions) == 0 {
			agentTarget.ProviderOptions = nil
		}
		if len(agentTarget.Metadata) == 0 {
			agentTarget.Metadata = nil
		}
		out.Agent = &agentTarget
		return out
	}
	pluginTarget := target.PluginTarget()
	if PluginTargetSet(pluginTarget) {
		if len(pluginTarget.Input) == 0 {
			pluginTarget.Input = nil
		}
		out.Plugin = &pluginTarget
	}
	return out
}

// PluginTargetSet reports whether a workflow target contains any plugin target field.
func PluginTargetSet(target PluginTarget) bool {
	return !PluginTargetEmpty(target)
}

// PluginTargetEmpty reports whether a workflow target has no plugin target fields.
func PluginTargetEmpty(target PluginTarget) bool {
	return strings.TrimSpace(target.PluginName) == "" &&
		strings.TrimSpace(target.Operation) == "" &&
		strings.TrimSpace(target.Connection) == "" &&
		strings.TrimSpace(target.Instance) == "" &&
		len(target.Input) == 0
}
