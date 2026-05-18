package workflow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	Steps []Step
}

type Step struct {
	ID             string
	Inputs         map[string]Value
	Plugin         *PluginCall
	Agent          *AgentTurn
	When           *StepWhen
	TimeoutSeconds int
	OutputDelivery *StepDelivery
	Metadata       map[string]any
}

type PluginCall struct {
	Name           string
	Operation      string
	Connection     string
	Instance       string
	CredentialMode core.ConnectionMode `json:",omitempty"`
	Input          Value
}

type AgentTurn struct {
	ProviderName   string
	Model          string
	SessionKey     string
	Prompt         Text
	Messages       []AgentMessage
	ToolRefs       []coreagent.ToolRef
	ResponseSchema map[string]any
	ModelOptions   map[string]any
}

type AgentMessage struct {
	Role     string
	Text     Text
	Metadata map[string]any
}

type Text struct {
	Template string
}

type StepWhen struct {
	Value     Value
	EqualsSet bool
	Equals    any
}

type Value struct {
	Literal         any
	LiteralSet      bool
	Object          map[string]Value
	Array           []Value
	Template        *Text
	RunInput        string
	SignalPayload   string
	SignalMetadata  string
	WorkflowContext string
	StepOutput      *StepOutputSource
}

type StepOutputSource struct {
	StepID string
	Path   string
}

type StepDelivery struct {
	Plugin *PluginCall
}

func CloneValue(value Value) Value {
	out := value
	if value.Object != nil {
		out.Object = make(map[string]Value, len(value.Object))
		for key := range value.Object {
			out.Object[key] = CloneValue(value.Object[key])
		}
	}
	if value.Array != nil {
		out.Array = make([]Value, len(value.Array))
		for i := range value.Array {
			out.Array[i] = CloneValue(value.Array[i])
		}
	}
	if value.Template != nil {
		template := *value.Template
		out.Template = &template
	}
	if value.StepOutput != nil {
		source := *value.StepOutput
		out.StepOutput = &source
	}
	return out
}

func CloneStepDelivery(delivery *StepDelivery) *StepDelivery {
	if delivery == nil {
		return nil
	}
	out := *delivery
	if delivery.Plugin != nil {
		plugin := *delivery.Plugin
		plugin.Input = CloneValue(delivery.Plugin.Input)
		out.Plugin = &plugin
	}
	return &out
}

type ExecutionReference struct {
	ID                  string
	ProviderName        string
	Target              Target
	CallerPluginName    string
	SourceDefinitionID  string
	SubjectID           string
	SubjectKind         string
	DisplayName         string
	AuthSource          string
	CredentialSubjectID string
	RunAs               *core.RunAsSubject
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
	leftJSON, leftErr := json.Marshal(normalizedTargetComparisonPayload(left))
	if leftErr != nil {
		return false
	}
	rightJSON, rightErr := json.Marshal(normalizedTargetComparisonPayload(right))
	return rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}

func TargetFingerprint(target Target) (string, error) {
	data, err := json.Marshal(normalizedTargetComparisonPayload(target))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

type targetComparisonPayload struct {
	Steps []Step
}

func normalizedTargetComparisonPayload(target Target) targetComparisonPayload {
	out := targetComparisonPayload{Steps: append([]Step(nil), target.Steps...)}
	for i := range out.Steps {
		step := &out.Steps[i]
		if len(step.Inputs) == 0 {
			step.Inputs = nil
		}
		if step.Plugin != nil {
			plugin := *step.Plugin
			plugin.Input = normalizedWorkflowValue(plugin.Input)
			step.Plugin = &plugin
		}
		if step.Agent != nil {
			agent := *step.Agent
			if len(agent.Messages) == 0 {
				agent.Messages = nil
			}
			if len(agent.ToolRefs) == 0 {
				agent.ToolRefs = nil
			}
			if len(agent.ResponseSchema) == 0 {
				agent.ResponseSchema = nil
			}
			if len(agent.ModelOptions) == 0 {
				agent.ModelOptions = nil
			}
			step.Agent = &agent
		}
		if len(step.Metadata) == 0 {
			step.Metadata = nil
		}
		step.OutputDelivery = normalizedStepDelivery(step.OutputDelivery)
	}
	return out
}

func normalizedWorkflowValue(value Value) Value {
	if len(value.Object) == 0 {
		value.Object = nil
	}
	if len(value.Array) == 0 {
		value.Array = nil
	}
	return value
}

func normalizedStepDelivery(delivery *StepDelivery) *StepDelivery {
	if delivery == nil {
		return nil
	}
	out := *delivery
	if out.Plugin != nil {
		plugin := *out.Plugin
		plugin.Input = normalizedWorkflowValue(plugin.Input)
		out.Plugin = &plugin
	}
	return &out
}
