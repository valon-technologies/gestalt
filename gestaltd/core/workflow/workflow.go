package workflow

import (
	"bytes"
	"context"
	"encoding/json"
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
	Plugin *PluginTarget
	Agent  *AgentTarget
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
	ResponseSchema  map[string]any
	ProviderOptions map[string]any
	Metadata        map[string]any
	TimeoutSeconds  int
	OutputDelivery  *OutputDelivery
}

type OutputDelivery struct {
	Target         PluginTarget
	InputBindings  []OutputBinding
	CredentialMode core.ConnectionMode
}

type OutputBinding struct {
	InputField string
	Value      OutputValueSource
}

type OutputValueSource struct {
	AgentOutput    string
	SignalPayload  string
	SignalMetadata string
	Literal        any
}

type ExecutionReference struct {
	ID                  string
	ProviderName        string
	Target              Target
	CallerPluginName    string
	SubjectID           string
	SubjectKind         string
	DisplayName         string
	AuthSource          string
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
	WorkflowKey   string
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
	WorkflowKey    string
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

type Signal struct {
	ID             string
	Name           string
	Payload        map[string]any
	Metadata       map[string]any
	CreatedBy      Actor
	CreatedAt      *time.Time
	IdempotencyKey string
	Sequence       int64
}

type SignalRunRequest struct {
	RunID  string
	Signal Signal
}

type SignalOrStartRunRequest struct {
	WorkflowKey    string
	Target         Target
	IdempotencyKey string
	CreatedBy      Actor
	ExecutionRef   string
	Signal         Signal
}

type SignalRunResponse struct {
	Run         *Run
	Signal      Signal
	StartedRun  bool
	WorkflowKey string
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
	PluginName  string
	Event       Event
	PublishedBy Actor
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
	Signals      []Signal
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
	SignalRun(ctx context.Context, req SignalRunRequest) (*SignalRunResponse, error)
	SignalOrStartRun(ctx context.Context, req SignalOrStartRunRequest) (*SignalRunResponse, error)
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

func TargetsEqual(left, right Target) bool {
	if (left.Agent != nil && left.Plugin != nil) || (right.Agent != nil && right.Plugin != nil) {
		return false
	}
	leftJSON, leftErr := json.Marshal(normalizedTargetComparisonPayload(left))
	if leftErr != nil {
		return false
	}
	rightJSON, rightErr := json.Marshal(normalizedTargetComparisonPayload(right))
	return rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}

type targetComparisonPayload struct {
	Plugin *PluginTarget
	Agent  *AgentTarget
}

func normalizedTargetComparisonPayload(target Target) targetComparisonPayload {
	out := targetComparisonPayload{}
	if target.Agent != nil {
		agentTarget := *target.Agent
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
		if agentTarget.OutputDelivery != nil {
			outputDelivery := *agentTarget.OutputDelivery
			if len(outputDelivery.Target.Input) == 0 {
				outputDelivery.Target.Input = nil
			}
			if len(outputDelivery.InputBindings) == 0 {
				outputDelivery.InputBindings = nil
			}
			agentTarget.OutputDelivery = &outputDelivery
		}
		out.Agent = &agentTarget
		return out
	}
	if target.Plugin != nil {
		pluginTarget := *target.Plugin
		if len(pluginTarget.Input) == 0 {
			pluginTarget.Input = nil
		}
		out.Plugin = &pluginTarget
	}
	return out
}
