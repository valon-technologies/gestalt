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
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

const ConfigManagedDefinitionPrefix = "cfg_"

type RunStatus string

const (
	RunStatusPending   RunStatus = "pending"
	RunStatusRunning   RunStatus = "running"
	RunStatusSucceeded RunStatus = "succeeded"
	RunStatusFailed    RunStatus = "failed"
	RunStatusCanceled  RunStatus = "canceled"
)

type StepStatus string

const (
	StepStatusPending   StepStatus = "pending"
	StepStatusRunning   StepStatus = "running"
	StepStatusSkipped   StepStatus = "skipped"
	StepStatusSucceeded StepStatus = "succeeded"
	StepStatusFailed    StepStatus = "failed"
	StepStatusUnknown   StepStatus = "unknown"
)

type Target struct {
	Steps []Step
}

type Step struct {
	ID             string
	Inputs         map[string]Value
	App            *AppCall
	Agent          *AgentTurn
	When           *StepWhen
	TimeoutSeconds int
	Metadata       map[string]any
}

type AppCall struct {
	Name           string
	Operation      string
	Connection     string
	Instance       string
	CredentialMode core.ConnectionMode `json:",omitempty"`
	Input          Value
}

type AgentTurn struct {
	ProviderName string
	Model        string
	SessionKey   string
	Prompt       Text
	Messages     []AgentMessage
	ToolRefs     []coreagent.ToolRef
	Output       coreagent.Output
	ModelOptions map[string]any
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
	Literal    any
	LiteralSet bool
	Object     map[string]Value
	Array      []Value
	Template   *Text
	Input      string
	Signal     string
	StepOutput *StepOutputSource
	StepInput  *StepInputSource
}

type StepOutputSource struct {
	StepID string
	Path   string
}

type StepInputSource struct {
	StepID string
	Path   string
}

func CloneValue(value Value) Value {
	out := value
	out.Literal = cloneLiteral(value.Literal)
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
	if value.StepInput != nil {
		source := *value.StepInput
		out.StepInput = &source
	}
	return out
}

func cloneLiteral(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = cloneLiteral(item)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i := range typed {
			out[i] = cloneLiteral(typed[i])
		}
		return out
	default:
		return value
	}
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
	ActivationID string
	ScheduledFor *time.Time
}

type EventTriggerInvocation struct {
	ActivationID string
	Event        Event
}

type RunTrigger struct {
	Manual   bool
	Schedule *ScheduleTrigger
	Event    *EventTriggerInvocation
}

type Run struct {
	ID                   string
	Status               RunStatus
	WorkflowKey          string
	Target               Target
	DefinitionID         string
	DefinitionGeneration int64
	Input                map[string]any
	CurrentStepID        string
	Steps                []StepExecution
	Trigger              RunTrigger
	CreatedBy            string
	RunAs                string
	CreatedAt            *time.Time
	StartedAt            *time.Time
	CompletedAt          *time.Time
	StatusMessage        string
	Output               any
}

type StepAttempt struct {
	ID             string
	Status         StepStatus
	IdempotencyKey string
	Input          any
	Output         any
	StatusMessage  string
	StartedAt      *time.Time
	CompletedAt    *time.Time
}

type StepExecution struct {
	StepID        string
	Status        StepStatus
	Attempts      []StepAttempt
	Input         any
	Output        any
	StatusMessage string
	SkipReason    string
	StartedAt     *time.Time
	CompletedAt   *time.Time
}

type Schedule struct {
	ID           string
	Cron         string
	Timezone     string
	Target       Target
	DefinitionID string
	Paused       bool
	CreatedBy    string
	RunAs        string
	CreatedAt    *time.Time
	UpdatedAt    *time.Time
	NextRunAt    *time.Time
}

type EventTrigger struct {
	ID           string
	Match        EventMatch
	Target       Target
	DefinitionID string
	Paused       bool
	CreatedBy    string
	RunAs        string
	CreatedAt    *time.Time
	UpdatedAt    *time.Time
}

type Definition struct {
	ID           string
	Generation   int64
	Target       Target
	Activations  []Activation
	Paused       bool
	CreatedBy    string
	CreatedAt    *time.Time
	UpdatedAt    *time.Time
	ProviderName string
	RunAs        string
}

type DefinitionSpec struct {
	ID          string
	Target      Target
	Activations []Activation
	Paused      bool
	RunAs       string
}

type ScheduleActivation struct {
	Cron     string
	Timezone string
}

type EventActivation struct {
	Match EventMatch
}

type Activation struct {
	ID       string
	Input    Value
	Paused   bool
	Schedule *ScheduleActivation
	Event    *EventActivation
}

type ListRunsRequest struct {
	PageSize  int
	PageToken string
	TargetApp string
	// KnownApps is the installed app name set used to disambiguate
	// app_<app>_… definition ownership when Target.steps is empty.
	KnownApps []string
	Status    RunStatus
}

type ListRunsResponse struct {
	Runs          []*Run
	NextPageToken string
}

type Signal struct {
	ID             string
	Name           string
	Payload        map[string]any
	Metadata       map[string]any
	CreatedBy      string
	CreatedAt      *time.Time
	IdempotencyKey string
	Sequence       int64
}

type SignalRunResponse struct {
	Run         *Run
	Signal      Signal
	StartedRun  bool
	WorkflowKey string
}

type Provider interface {
	ApplyDefinition(ctx context.Context, req *proto.ApplyWorkflowProviderDefinitionRequest) (*proto.WorkflowDefinition, error)
	GetDefinition(ctx context.Context, req *proto.GetWorkflowProviderDefinitionRequest) (*proto.WorkflowDefinition, error)
	ListDefinitions(ctx context.Context, req *proto.ListWorkflowProviderDefinitionsRequest) (*proto.ListWorkflowProviderDefinitionsResponse, error)
	SetDefinitionPaused(ctx context.Context, req *proto.SetWorkflowProviderDefinitionPausedRequest) (*proto.WorkflowDefinition, error)
	SetActivationPaused(ctx context.Context, req *proto.SetWorkflowProviderActivationPausedRequest) (*proto.WorkflowDefinition, error)
	DeleteDefinition(ctx context.Context, req *proto.DeleteWorkflowProviderDefinitionRequest) error
	StartRun(ctx context.Context, req *proto.StartWorkflowProviderRunRequest) (*proto.WorkflowRun, error)
	GetRun(ctx context.Context, req *proto.GetWorkflowProviderRunRequest) (*proto.WorkflowRun, error)
	ListRuns(ctx context.Context, req *proto.ListWorkflowProviderRunsRequest) (*proto.ListWorkflowProviderRunsResponse, error)
	GetRunEvents(ctx context.Context, req *proto.GetWorkflowProviderRunEventsRequest) (*proto.GetWorkflowProviderRunEventsResponse, error)
	GetRunOutput(ctx context.Context, req *proto.GetWorkflowProviderRunOutputRequest) (*proto.GetWorkflowProviderRunOutputResponse, error)
	CancelRun(ctx context.Context, req *proto.CancelWorkflowProviderRunRequest) (*proto.WorkflowRun, error)
	SignalRun(ctx context.Context, req *proto.SignalWorkflowProviderRunRequest) (*proto.SignalWorkflowRunResponse, error)
	SignalOrStartRun(ctx context.Context, req *proto.SignalOrStartWorkflowProviderRunRequest) (*proto.SignalWorkflowRunResponse, error)
	DeliverEvent(ctx context.Context, req *proto.DeliverWorkflowProviderEventRequest) (*proto.WorkflowEvent, error)
	Ping(ctx context.Context) error
	Close() error
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
		} else {
			inputs := make(map[string]Value, len(step.Inputs))
			for key, value := range step.Inputs {
				inputs[key] = normalizedWorkflowValue(value)
			}
			step.Inputs = inputs
		}
		if step.When != nil {
			when := *step.When
			when.Value = normalizedWorkflowValue(when.Value)
			step.When = &when
		}
		if step.App != nil {
			app := *step.App
			app.Input = normalizedWorkflowValue(app.Input)
			step.App = &app
		}
		if step.Agent != nil {
			agent := *step.Agent
			if len(agent.Messages) == 0 {
				agent.Messages = nil
			}
			if len(agent.ToolRefs) == 0 {
				agent.ToolRefs = nil
			}
			if agent.Output.Structured != nil {
				structured := *agent.Output.Structured
				if len(structured.Schema) == 0 {
					structured.Schema = nil
				} else {
					structured.Schema = cloneLiteral(structured.Schema).(map[string]any)
				}
				agent.Output.Structured = &structured
			}
			if len(agent.ModelOptions) == 0 {
				agent.ModelOptions = nil
			}
			step.Agent = &agent
		}
		if len(step.Metadata) == 0 {
			step.Metadata = nil
		}
	}
	return out
}

func normalizedWorkflowValue(value Value) Value {
	if value.Object != nil {
		if len(value.Object) == 0 {
			value.Object = nil
		} else {
			object := make(map[string]Value, len(value.Object))
			for key, item := range value.Object {
				object[key] = normalizedWorkflowValue(item)
			}
			value.Object = object
		}
	}
	if value.Array != nil {
		if len(value.Array) == 0 {
			value.Array = nil
		} else {
			array := make([]Value, len(value.Array))
			for i, item := range value.Array {
				array[i] = normalizedWorkflowValue(item)
			}
			value.Array = array
		}
	}
	return value
}
