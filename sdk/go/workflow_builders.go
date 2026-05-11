package gestalt

import (
	"fmt"
	"time"

	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// BoundWorkflowPluginTargetInput contains native Go values for constructing a
// BoundWorkflowPluginTarget.
type BoundWorkflowPluginTargetInput struct {
	PluginName     string
	Operation      string
	Input          any
	Connection     string
	Instance       string
	CredentialMode string
}

// NewBoundWorkflowPluginTarget creates a plugin workflow target from native Go
// values.
func NewBoundWorkflowPluginTarget(input BoundWorkflowPluginTargetInput) (*BoundWorkflowPluginTarget, error) {
	value, err := StructFromAny(input.Input)
	if err != nil {
		return nil, err
	}
	return &BoundWorkflowPluginTarget{
		PluginName:     input.PluginName,
		Operation:      input.Operation,
		Input:          value,
		Connection:     input.Connection,
		Instance:       input.Instance,
		CredentialMode: input.CredentialMode,
	}, nil
}

// BoundWorkflowPluginTargetInputFromTarget converts an existing protocol target
// into native builder input.
func BoundWorkflowPluginTargetInputFromTarget(value *BoundWorkflowPluginTarget) BoundWorkflowPluginTargetInput {
	if value == nil {
		return BoundWorkflowPluginTargetInput{}
	}
	return BoundWorkflowPluginTargetInput{
		PluginName:     value.GetPluginName(),
		Operation:      value.GetOperation(),
		Input:          MapFromStruct(value.GetInput()),
		Connection:     value.GetConnection(),
		Instance:       value.GetInstance(),
		CredentialMode: value.GetCredentialMode(),
	}
}

// WorkflowOutputDeliveryInput contains native Go values for constructing a
// WorkflowOutputDelivery.
type WorkflowOutputDeliveryInput struct {
	Target         *BoundWorkflowPluginTargetInput
	InputBindings  []WorkflowOutputBindingInput
	CredentialMode string
}

// WorkflowOutputValueSourceInput contains native Go values for constructing a
// workflow output value source. Set at most one source field.
type WorkflowOutputValueSourceInput struct {
	AgentOutput    string
	SignalPayload  string
	SignalMetadata string
	Literal        any
	AgentSession   string
}

// WorkflowOutputBindingInput contains native Go values for one workflow output
// binding.
type WorkflowOutputBindingInput struct {
	InputField string
	Value      *WorkflowOutputValueSourceInput
}

// NewWorkflowOutputDelivery creates a workflow output delivery from native Go
// values.
func NewWorkflowOutputDelivery(input WorkflowOutputDeliveryInput) (*WorkflowOutputDelivery, error) {
	var target *BoundWorkflowPluginTarget
	if input.Target != nil {
		value, err := NewBoundWorkflowPluginTarget(*input.Target)
		if err != nil {
			return nil, err
		}
		target = value
	}
	bindings, err := newWorkflowOutputBindings(input.InputBindings)
	if err != nil {
		return nil, err
	}
	return &WorkflowOutputDelivery{
		Target:         target,
		InputBindings:  bindings,
		CredentialMode: input.CredentialMode,
	}, nil
}

// WorkflowOutputDeliveryInputFromDelivery converts an existing protocol delivery
// into native builder input.
func WorkflowOutputDeliveryInputFromDelivery(value *WorkflowOutputDelivery) *WorkflowOutputDeliveryInput {
	if value == nil {
		return nil
	}
	var target *BoundWorkflowPluginTargetInput
	if value.GetTarget() != nil {
		input := BoundWorkflowPluginTargetInputFromTarget(value.GetTarget())
		target = &input
	}
	return &WorkflowOutputDeliveryInput{
		Target:         target,
		InputBindings:  workflowOutputBindingInputsFromBindings(value.GetInputBindings()),
		CredentialMode: value.GetCredentialMode(),
	}
}

// BoundWorkflowAgentTargetInput contains native Go values for constructing a
// BoundWorkflowAgentTarget.
type BoundWorkflowAgentTargetInput struct {
	ProviderName         string
	Model                string
	Prompt               string
	Messages             []*AgentMessage
	ToolRefs             []*AgentToolRef
	ResponseSchema       any
	Metadata             any
	TimeoutSeconds       int32
	OutputDelivery       *WorkflowOutputDeliveryInput
	ModelOptions         any
	SessionReadyDelivery *WorkflowOutputDeliveryInput
}

// NewBoundWorkflowAgentTarget creates an agent workflow target from native Go
// values.
func NewBoundWorkflowAgentTarget(input BoundWorkflowAgentTargetInput) (*BoundWorkflowAgentTarget, error) {
	responseSchema, err := StructFromAny(input.ResponseSchema)
	if err != nil {
		return nil, err
	}
	metadata, err := StructFromAny(input.Metadata)
	if err != nil {
		return nil, err
	}
	modelOptions, err := StructFromAny(input.ModelOptions)
	if err != nil {
		return nil, err
	}
	outputDelivery, err := newOptionalWorkflowOutputDelivery(input.OutputDelivery)
	if err != nil {
		return nil, err
	}
	sessionReadyDelivery, err := newOptionalWorkflowOutputDelivery(input.SessionReadyDelivery)
	if err != nil {
		return nil, err
	}
	return &BoundWorkflowAgentTarget{
		ProviderName:         input.ProviderName,
		Model:                input.Model,
		Prompt:               input.Prompt,
		Messages:             copyAgentMessages(input.Messages),
		ToolRefs:             copyAgentToolRefs(input.ToolRefs),
		ResponseSchema:       responseSchema,
		Metadata:             metadata,
		TimeoutSeconds:       input.TimeoutSeconds,
		OutputDelivery:       outputDelivery,
		ModelOptions:         modelOptions,
		SessionReadyDelivery: sessionReadyDelivery,
	}, nil
}

// BoundWorkflowAgentTargetInputFromTarget converts an existing protocol target
// into native builder input.
func BoundWorkflowAgentTargetInputFromTarget(value *BoundWorkflowAgentTarget) BoundWorkflowAgentTargetInput {
	if value == nil {
		return BoundWorkflowAgentTargetInput{}
	}
	return BoundWorkflowAgentTargetInput{
		ProviderName:         value.GetProviderName(),
		Model:                value.GetModel(),
		Prompt:               value.GetPrompt(),
		Messages:             value.GetMessages(),
		ToolRefs:             value.GetToolRefs(),
		ResponseSchema:       MapFromStruct(value.GetResponseSchema()),
		Metadata:             MapFromStruct(value.GetMetadata()),
		TimeoutSeconds:       value.GetTimeoutSeconds(),
		OutputDelivery:       WorkflowOutputDeliveryInputFromDelivery(value.GetOutputDelivery()),
		ModelOptions:         MapFromStruct(value.GetModelOptions()),
		SessionReadyDelivery: WorkflowOutputDeliveryInputFromDelivery(value.GetSessionReadyDelivery()),
	}
}

// BoundWorkflowTargetInput contains native Go values for constructing a
// BoundWorkflowTarget. Exactly one of Plugin or Agent should be set.
type BoundWorkflowTargetInput struct {
	Plugin *BoundWorkflowPluginTargetInput
	Agent  *BoundWorkflowAgentTargetInput
}

// WorkflowActorInput contains native Go values for constructing workflow actor
// metadata.
type WorkflowActorInput struct {
	SubjectID   string
	SubjectKind string
	DisplayName string
	AuthSource  string
}

// NewWorkflowActor creates workflow actor metadata from native Go values.
func NewWorkflowActor(input WorkflowActorInput) *WorkflowActor {
	return &WorkflowActor{
		SubjectId:   input.SubjectID,
		SubjectKind: input.SubjectKind,
		DisplayName: input.DisplayName,
		AuthSource:  input.AuthSource,
	}
}

// WorkflowActorInputFromActor converts existing workflow actor metadata into
// native builder input.
func WorkflowActorInputFromActor(value *WorkflowActor) WorkflowActorInput {
	if value == nil {
		return WorkflowActorInput{}
	}
	return WorkflowActorInput{
		SubjectID:   value.GetSubjectId(),
		SubjectKind: value.GetSubjectKind(),
		DisplayName: value.GetDisplayName(),
		AuthSource:  value.GetAuthSource(),
	}
}

// NewBoundWorkflowTarget creates a workflow target from native Go values.
func NewBoundWorkflowTarget(input BoundWorkflowTargetInput) (*BoundWorkflowTarget, error) {
	switch {
	case input.Plugin != nil:
		plugin, err := NewBoundWorkflowPluginTarget(*input.Plugin)
		if err != nil {
			return nil, err
		}
		return &BoundWorkflowTarget{Kind: &BoundWorkflowTargetPlugin{Plugin: plugin}}, nil
	case input.Agent != nil:
		agent, err := NewBoundWorkflowAgentTarget(*input.Agent)
		if err != nil {
			return nil, err
		}
		return &BoundWorkflowTarget{Kind: &BoundWorkflowTargetAgent{Agent: agent}}, nil
	default:
		return &BoundWorkflowTarget{}, nil
	}
}

// BoundWorkflowTargetInputFromTarget converts an existing protocol target into
// native builder input.
func BoundWorkflowTargetInputFromTarget(value *BoundWorkflowTarget) BoundWorkflowTargetInput {
	if value == nil {
		return BoundWorkflowTargetInput{}
	}
	if plugin := value.GetPlugin(); plugin != nil {
		input := BoundWorkflowPluginTargetInputFromTarget(plugin)
		return BoundWorkflowTargetInput{Plugin: &input}
	}
	if agent := value.GetAgent(); agent != nil {
		input := BoundWorkflowAgentTargetInputFromTarget(agent)
		return BoundWorkflowTargetInput{Agent: &input}
	}
	return BoundWorkflowTargetInput{}
}

// NewBoundWorkflowTargetFromTarget creates a copy of an existing workflow target
// through the native target input builder.
func NewBoundWorkflowTargetFromTarget(value *BoundWorkflowTarget) (*BoundWorkflowTarget, error) {
	if value == nil {
		return nil, nil
	}
	return NewBoundWorkflowTarget(BoundWorkflowTargetInputFromTarget(value))
}

// WorkflowEventInput contains native Go values for constructing a WorkflowEvent.
type WorkflowEventInput struct {
	ID              string
	Source          string
	SpecVersion     string
	Type            string
	Subject         string
	Time            time.Time
	DataContentType string
	Data            any
	Extensions      map[string]any
}

// NewWorkflowEvent creates a workflow event from native Go values.
func NewWorkflowEvent(input WorkflowEventInput) (*WorkflowEvent, error) {
	data, err := StructFromAny(input.Data)
	if err != nil {
		return nil, err
	}
	extensions, err := ValuesFromMap(input.Extensions)
	if err != nil {
		return nil, err
	}
	return &WorkflowEvent{
		Id:              input.ID,
		Source:          input.Source,
		SpecVersion:     input.SpecVersion,
		Type:            input.Type,
		Subject:         input.Subject,
		Time:            timestampFromNonZeroTime(input.Time),
		Datacontenttype: input.DataContentType,
		Data:            data,
		Extensions:      extensions,
	}, nil
}

// WorkflowEventInputFromEvent converts an existing protocol event into native
// builder input.
func WorkflowEventInputFromEvent(value *WorkflowEvent) WorkflowEventInput {
	if value == nil {
		return WorkflowEventInput{}
	}
	return WorkflowEventInput{
		ID:              value.GetId(),
		Source:          value.GetSource(),
		SpecVersion:     value.GetSpecVersion(),
		Type:            value.GetType(),
		Subject:         value.GetSubject(),
		Time:            timeFromTimestamp(value.GetTime()),
		DataContentType: value.GetDatacontenttype(),
		Data:            MapFromStruct(value.GetData()),
		Extensions:      MapFromValues(value.GetExtensions()),
	}
}

// NewWorkflowEventFromEvent creates a copy of an existing workflow event through
// the native event input builder.
func NewWorkflowEventFromEvent(value *WorkflowEvent) (*WorkflowEvent, error) {
	if value == nil {
		return nil, nil
	}
	return NewWorkflowEvent(WorkflowEventInputFromEvent(value))
}

// WorkflowSignalInput contains native Go values for constructing a
// WorkflowSignal.
type WorkflowSignalInput struct {
	ID             string
	Name           string
	Payload        any
	Metadata       any
	CreatedBy      *WorkflowActorInput
	CreatedAt      time.Time
	IdempotencyKey string
	Sequence       int64
}

// NewWorkflowSignal creates a workflow signal from native Go values.
func NewWorkflowSignal(input WorkflowSignalInput) (*WorkflowSignal, error) {
	payload, err := StructFromAny(input.Payload)
	if err != nil {
		return nil, err
	}
	metadata, err := StructFromAny(input.Metadata)
	if err != nil {
		return nil, err
	}
	return &WorkflowSignal{
		Id:             input.ID,
		Name:           input.Name,
		Payload:        payload,
		Metadata:       metadata,
		CreatedBy:      workflowActorFromInput(input.CreatedBy),
		CreatedAt:      timestampFromNonZeroTime(input.CreatedAt),
		IdempotencyKey: input.IdempotencyKey,
		Sequence:       input.Sequence,
	}, nil
}

// WorkflowSignalInputFromSignal converts an existing protocol signal into native
// builder input.
func WorkflowSignalInputFromSignal(value *WorkflowSignal) WorkflowSignalInput {
	if value == nil {
		return WorkflowSignalInput{}
	}
	return WorkflowSignalInput{
		ID:             value.GetId(),
		Name:           value.GetName(),
		Payload:        MapFromStruct(value.GetPayload()),
		Metadata:       MapFromStruct(value.GetMetadata()),
		CreatedBy:      workflowActorInputPtrFromActor(value.GetCreatedBy()),
		CreatedAt:      timeFromTimestamp(value.GetCreatedAt()),
		IdempotencyKey: value.GetIdempotencyKey(),
		Sequence:       value.GetSequence(),
	}
}

// NewWorkflowSignalFromSignal creates a copy of an existing workflow signal
// through the native signal input builder.
func NewWorkflowSignalFromSignal(value *WorkflowSignal) (*WorkflowSignal, error) {
	if value == nil {
		return nil, nil
	}
	return NewWorkflowSignal(WorkflowSignalInputFromSignal(value))
}

// WorkflowScheduleTriggerInput contains native Go values for constructing a
// schedule-triggered workflow run trigger.
type WorkflowScheduleTriggerInput struct {
	ScheduleID   string
	ScheduledFor *time.Time
}

// WorkflowEventTriggerInvocationInput contains native Go values for
// constructing an event-triggered workflow run trigger.
type WorkflowEventTriggerInvocationInput struct {
	TriggerID string
	Event     *WorkflowEventInput
}

// WorkflowRunTriggerInput contains native Go values for constructing a
// workflow run trigger. Exactly one trigger kind should be set.
type WorkflowRunTriggerInput struct {
	Manual   bool
	Schedule *WorkflowScheduleTriggerInput
	Event    *WorkflowEventTriggerInvocationInput
}

// NewWorkflowScheduleTrigger creates a schedule-trigger run trigger from native
// Go values.
func NewWorkflowScheduleTrigger(scheduleID string, scheduledFor time.Time) *WorkflowRunTrigger {
	return &WorkflowRunTrigger{Kind: &WorkflowRunTriggerSchedule{Schedule: &WorkflowScheduleTrigger{
		ScheduleId:   scheduleID,
		ScheduledFor: timestampFromNonZeroTime(scheduledFor),
	}}}
}

// NewWorkflowRunTrigger creates a workflow run trigger from native Go values.
func NewWorkflowRunTrigger(input WorkflowRunTriggerInput) (*WorkflowRunTrigger, error) {
	selected := 0
	if input.Manual {
		selected++
	}
	if input.Schedule != nil {
		selected++
	}
	if input.Event != nil {
		selected++
	}
	if selected == 0 {
		return &WorkflowRunTrigger{}, nil
	}
	if selected > 1 {
		return nil, fmt.Errorf("workflow run trigger must set exactly one trigger kind")
	}
	if input.Manual {
		return &WorkflowRunTrigger{Kind: &WorkflowRunTriggerManual{Manual: &WorkflowManualTrigger{}}}, nil
	}
	if input.Schedule != nil {
		return &WorkflowRunTrigger{Kind: &WorkflowRunTriggerSchedule{Schedule: &WorkflowScheduleTrigger{
			ScheduleId:   input.Schedule.ScheduleID,
			ScheduledFor: timestampFromOptionalTime(input.Schedule.ScheduledFor),
		}}}, nil
	}

	var event *WorkflowEvent
	if input.Event.Event != nil {
		value, err := NewWorkflowEvent(*input.Event.Event)
		if err != nil {
			return nil, err
		}
		event = value
	}
	return &WorkflowRunTrigger{Kind: &WorkflowRunTriggerEvent{Event: &WorkflowEventTriggerInvocation{
		TriggerId: input.Event.TriggerID,
		Event:     event,
	}}}, nil
}

// WorkflowRunTriggerInputFromTrigger converts an existing protocol trigger into
// native builder input.
func WorkflowRunTriggerInputFromTrigger(value *WorkflowRunTrigger) (WorkflowRunTriggerInput, error) {
	if value == nil {
		return WorkflowRunTriggerInput{}, nil
	}
	switch kind := value.GetKind().(type) {
	case *WorkflowRunTriggerManual:
		return WorkflowRunTriggerInput{Manual: true}, nil
	case *WorkflowRunTriggerSchedule:
		if kind.Schedule == nil {
			return WorkflowRunTriggerInput{}, nil
		}
		scheduledFor, err := timePtrFromTimestamp(kind.Schedule.GetScheduledFor())
		if err != nil {
			return WorkflowRunTriggerInput{}, err
		}
		return WorkflowRunTriggerInput{Schedule: &WorkflowScheduleTriggerInput{
			ScheduleID:   kind.Schedule.GetScheduleId(),
			ScheduledFor: scheduledFor,
		}}, nil
	case *WorkflowRunTriggerEvent:
		if kind.Event == nil {
			return WorkflowRunTriggerInput{}, nil
		}
		var event *WorkflowEventInput
		if kind.Event.GetEvent() != nil {
			input := WorkflowEventInputFromEvent(kind.Event.GetEvent())
			event = &input
		}
		return WorkflowRunTriggerInput{Event: &WorkflowEventTriggerInvocationInput{
			TriggerID: kind.Event.GetTriggerId(),
			Event:     event,
		}}, nil
	default:
		return WorkflowRunTriggerInput{}, nil
	}
}

// NewWorkflowRunTriggerFromTrigger creates a copy of an existing workflow run
// trigger.
func NewWorkflowRunTriggerFromTrigger(value *WorkflowRunTrigger) (*WorkflowRunTrigger, error) {
	input, err := WorkflowRunTriggerInputFromTrigger(value)
	if err != nil || value == nil {
		return nil, err
	}
	return NewWorkflowRunTrigger(input)
}

// BoundWorkflowRunInput contains native Go values for constructing a
// BoundWorkflowRun.
type BoundWorkflowRunInput struct {
	ID            string
	Status        WorkflowRunStatus
	Target        *BoundWorkflowTargetInput
	Trigger       *WorkflowRunTriggerInput
	CreatedAt     time.Time
	StartedAt     *time.Time
	CompletedAt   *time.Time
	StatusMessage string
	ResultBody    string
	CreatedBy     *WorkflowActorInput
	ExecutionRef  string
	WorkflowKey   string
}

// NewBoundWorkflowRun creates a bound workflow run from native Go values.
func NewBoundWorkflowRun(input BoundWorkflowRunInput) (*BoundWorkflowRun, error) {
	target, err := newOptionalBoundWorkflowTarget(input.Target)
	if err != nil {
		return nil, err
	}
	trigger, err := newOptionalWorkflowRunTrigger(input.Trigger)
	if err != nil {
		return nil, err
	}
	return &BoundWorkflowRun{
		Id:            input.ID,
		Status:        input.Status,
		Target:        target,
		Trigger:       trigger,
		CreatedAt:     timestampFromNonZeroTime(input.CreatedAt),
		StartedAt:     timestampFromOptionalTime(input.StartedAt),
		CompletedAt:   timestampFromOptionalTime(input.CompletedAt),
		StatusMessage: input.StatusMessage,
		ResultBody:    input.ResultBody,
		CreatedBy:     workflowActorFromInput(input.CreatedBy),
		ExecutionRef:  input.ExecutionRef,
		WorkflowKey:   input.WorkflowKey,
	}, nil
}

// BoundWorkflowRunInputFromRun converts an existing protocol run into native
// builder input.
func BoundWorkflowRunInputFromRun(value *BoundWorkflowRun) (BoundWorkflowRunInput, error) {
	if value == nil {
		return BoundWorkflowRunInput{}, nil
	}
	startedAt, err := timePtrFromTimestamp(value.GetStartedAt())
	if err != nil {
		return BoundWorkflowRunInput{}, err
	}
	completedAt, err := timePtrFromTimestamp(value.GetCompletedAt())
	if err != nil {
		return BoundWorkflowRunInput{}, err
	}
	trigger, err := WorkflowRunTriggerInputFromTrigger(value.GetTrigger())
	if err != nil {
		return BoundWorkflowRunInput{}, err
	}
	return BoundWorkflowRunInput{
		ID:            value.GetId(),
		Status:        value.GetStatus(),
		Target:        workflowTargetInputPtrFromTarget(value.GetTarget()),
		Trigger:       &trigger,
		CreatedAt:     timeFromTimestamp(value.GetCreatedAt()),
		StartedAt:     startedAt,
		CompletedAt:   completedAt,
		StatusMessage: value.GetStatusMessage(),
		ResultBody:    value.GetResultBody(),
		CreatedBy:     workflowActorInputPtrFromActor(value.GetCreatedBy()),
		ExecutionRef:  value.GetExecutionRef(),
		WorkflowKey:   value.GetWorkflowKey(),
	}, nil
}

// NewBoundWorkflowRunFromRun creates a copy of an existing bound workflow run
// through the native run input builder.
func NewBoundWorkflowRunFromRun(value *BoundWorkflowRun) (*BoundWorkflowRun, error) {
	input, err := BoundWorkflowRunInputFromRun(value)
	if err != nil || value == nil {
		return nil, err
	}
	return NewBoundWorkflowRun(input)
}

// BoundWorkflowScheduleInput contains native Go values for constructing a
// BoundWorkflowSchedule.
type BoundWorkflowScheduleInput struct {
	ID           string
	Cron         string
	Timezone     string
	Target       *BoundWorkflowTargetInput
	Paused       bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
	NextRunAt    *time.Time
	CreatedBy    *WorkflowActorInput
	ExecutionRef string
}

// NewBoundWorkflowSchedule creates a bound workflow schedule from native Go
// values.
func NewBoundWorkflowSchedule(input BoundWorkflowScheduleInput) (*BoundWorkflowSchedule, error) {
	target, err := newOptionalBoundWorkflowTarget(input.Target)
	if err != nil {
		return nil, err
	}
	return &BoundWorkflowSchedule{
		Id:           input.ID,
		Cron:         input.Cron,
		Timezone:     input.Timezone,
		Target:       target,
		Paused:       input.Paused,
		CreatedAt:    timestampFromNonZeroTime(input.CreatedAt),
		UpdatedAt:    timestampFromNonZeroTime(input.UpdatedAt),
		NextRunAt:    timestampFromOptionalTime(input.NextRunAt),
		CreatedBy:    workflowActorFromInput(input.CreatedBy),
		ExecutionRef: input.ExecutionRef,
	}, nil
}

// BoundWorkflowScheduleInputFromSchedule converts an existing protocol schedule
// into native builder input.
func BoundWorkflowScheduleInputFromSchedule(value *BoundWorkflowSchedule) (BoundWorkflowScheduleInput, error) {
	if value == nil {
		return BoundWorkflowScheduleInput{}, nil
	}
	nextRunAt, err := timePtrFromTimestamp(value.GetNextRunAt())
	if err != nil {
		return BoundWorkflowScheduleInput{}, err
	}
	return BoundWorkflowScheduleInput{
		ID:           value.GetId(),
		Cron:         value.GetCron(),
		Timezone:     value.GetTimezone(),
		Target:       workflowTargetInputPtrFromTarget(value.GetTarget()),
		Paused:       value.GetPaused(),
		CreatedAt:    timeFromTimestamp(value.GetCreatedAt()),
		UpdatedAt:    timeFromTimestamp(value.GetUpdatedAt()),
		NextRunAt:    nextRunAt,
		CreatedBy:    workflowActorInputPtrFromActor(value.GetCreatedBy()),
		ExecutionRef: value.GetExecutionRef(),
	}, nil
}

// NewBoundWorkflowScheduleFromSchedule creates a copy of an existing schedule
// through the native schedule input builder.
func NewBoundWorkflowScheduleFromSchedule(value *BoundWorkflowSchedule) (*BoundWorkflowSchedule, error) {
	input, err := BoundWorkflowScheduleInputFromSchedule(value)
	if err != nil || value == nil {
		return nil, err
	}
	return NewBoundWorkflowSchedule(input)
}

// BoundWorkflowEventTriggerInput contains native Go values for constructing a
// BoundWorkflowEventTrigger.
type BoundWorkflowEventTriggerInput struct {
	ID           string
	Match        *WorkflowEventMatchInput
	Target       *BoundWorkflowTargetInput
	Paused       bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
	CreatedBy    *WorkflowActorInput
	ExecutionRef string
}

// WorkflowEventMatchInput contains native Go values for matching workflow
// events.
type WorkflowEventMatchInput struct {
	Type    string
	Source  string
	Subject string
}

// NewBoundWorkflowEventTrigger creates a bound workflow event trigger from
// native Go values.
func NewBoundWorkflowEventTrigger(input BoundWorkflowEventTriggerInput) (*BoundWorkflowEventTrigger, error) {
	target, err := newOptionalBoundWorkflowTarget(input.Target)
	if err != nil {
		return nil, err
	}
	return &BoundWorkflowEventTrigger{
		Id:           input.ID,
		Match:        workflowEventMatchFromInput(input.Match),
		Target:       target,
		Paused:       input.Paused,
		CreatedAt:    timestampFromNonZeroTime(input.CreatedAt),
		UpdatedAt:    timestampFromNonZeroTime(input.UpdatedAt),
		CreatedBy:    workflowActorFromInput(input.CreatedBy),
		ExecutionRef: input.ExecutionRef,
	}, nil
}

// BoundWorkflowEventTriggerInputFromTrigger converts an existing protocol event
// trigger into native builder input.
func BoundWorkflowEventTriggerInputFromTrigger(value *BoundWorkflowEventTrigger) (BoundWorkflowEventTriggerInput, error) {
	if value == nil {
		return BoundWorkflowEventTriggerInput{}, nil
	}
	return BoundWorkflowEventTriggerInput{
		ID:           value.GetId(),
		Match:        workflowEventMatchInputPtrFromMatch(value.GetMatch()),
		Target:       workflowTargetInputPtrFromTarget(value.GetTarget()),
		Paused:       value.GetPaused(),
		CreatedAt:    timeFromTimestamp(value.GetCreatedAt()),
		UpdatedAt:    timeFromTimestamp(value.GetUpdatedAt()),
		CreatedBy:    workflowActorInputPtrFromActor(value.GetCreatedBy()),
		ExecutionRef: value.GetExecutionRef(),
	}, nil
}

// NewBoundWorkflowEventTriggerFromTrigger creates a copy of an existing event
// trigger through the native event trigger input builder.
func NewBoundWorkflowEventTriggerFromTrigger(value *BoundWorkflowEventTrigger) (*BoundWorkflowEventTrigger, error) {
	input, err := BoundWorkflowEventTriggerInputFromTrigger(value)
	if err != nil || value == nil {
		return nil, err
	}
	return NewBoundWorkflowEventTrigger(input)
}

// WorkflowExecutionReferenceInput contains native Go values for constructing a
// WorkflowExecutionReference.
type WorkflowExecutionReferenceInput struct {
	ID                  string
	ProviderName        string
	Target              *BoundWorkflowTargetInput
	SubjectID           string
	CredentialSubjectID string
	Permissions         []WorkflowAccessPermissionInput
	CreatedAt           time.Time
	RevokedAt           *time.Time
	SubjectKind         string
	DisplayName         string
	AuthSource          string
	CallerPluginName    string
	RunAs               *WorkflowRunAsSubjectInput
	SourceDefinitionID  string
}

// WorkflowAccessPermissionInput contains native Go values for an execution
// reference permission.
type WorkflowAccessPermissionInput struct {
	Plugin     string
	Operations []string
}

// WorkflowRunAsSubjectInput contains native Go values for workflow run-as
// metadata.
type WorkflowRunAsSubjectInput struct {
	SubjectID   string
	SubjectKind string
	DisplayName string
	AuthSource  string
}

// NewWorkflowExecutionReference creates a workflow execution reference from
// native Go values.
func NewWorkflowExecutionReference(input WorkflowExecutionReferenceInput) (*WorkflowExecutionReference, error) {
	target, err := newOptionalBoundWorkflowTarget(input.Target)
	if err != nil {
		return nil, err
	}
	return &WorkflowExecutionReference{
		Id:                  input.ID,
		ProviderName:        input.ProviderName,
		Target:              target,
		SubjectId:           input.SubjectID,
		CredentialSubjectId: input.CredentialSubjectID,
		Permissions:         workflowAccessPermissionsFromInputs(input.Permissions),
		CreatedAt:           timestampFromNonZeroTime(input.CreatedAt),
		RevokedAt:           timestampFromOptionalTime(input.RevokedAt),
		SubjectKind:         input.SubjectKind,
		DisplayName:         input.DisplayName,
		AuthSource:          input.AuthSource,
		CallerPluginName:    input.CallerPluginName,
		RunAs:               workflowRunAsSubjectFromInput(input.RunAs),
		SourceDefinitionId:  input.SourceDefinitionID,
	}, nil
}

// WorkflowExecutionReferenceInputFromReference converts an existing protocol
// execution reference into native builder input.
func WorkflowExecutionReferenceInputFromReference(value *WorkflowExecutionReference) (WorkflowExecutionReferenceInput, error) {
	if value == nil {
		return WorkflowExecutionReferenceInput{}, nil
	}
	revokedAt, err := timePtrFromTimestamp(value.GetRevokedAt())
	if err != nil {
		return WorkflowExecutionReferenceInput{}, err
	}
	return WorkflowExecutionReferenceInput{
		ID:                  value.GetId(),
		ProviderName:        value.GetProviderName(),
		Target:              workflowTargetInputPtrFromTarget(value.GetTarget()),
		SubjectID:           value.GetSubjectId(),
		CredentialSubjectID: value.GetCredentialSubjectId(),
		Permissions:         workflowAccessPermissionInputsFromPermissions(value.GetPermissions()),
		CreatedAt:           timeFromTimestamp(value.GetCreatedAt()),
		RevokedAt:           revokedAt,
		SubjectKind:         value.GetSubjectKind(),
		DisplayName:         value.GetDisplayName(),
		AuthSource:          value.GetAuthSource(),
		CallerPluginName:    value.GetCallerPluginName(),
		RunAs:               workflowRunAsSubjectInputPtrFromSubject(value.GetRunAs()),
		SourceDefinitionID:  value.GetSourceDefinitionId(),
	}, nil
}

// NewWorkflowExecutionReferenceFromReference creates a copy of an existing
// execution reference through the native execution reference input builder.
func NewWorkflowExecutionReferenceFromReference(value *WorkflowExecutionReference) (*WorkflowExecutionReference, error) {
	input, err := WorkflowExecutionReferenceInputFromReference(value)
	if err != nil || value == nil {
		return nil, err
	}
	return NewWorkflowExecutionReference(input)
}

func timestampFromNonZeroTime(value time.Time) *timestamppb.Timestamp {
	if value.IsZero() {
		return nil
	}
	return timestamppb.New(value)
}

func timestampFromOptionalTime(value *time.Time) *timestamppb.Timestamp {
	if value == nil || value.IsZero() {
		return nil
	}
	return timestamppb.New(*value)
}

func newOptionalWorkflowOutputDelivery(input *WorkflowOutputDeliveryInput) (*WorkflowOutputDelivery, error) {
	if input == nil {
		return nil, nil
	}
	return NewWorkflowOutputDelivery(*input)
}

func newOptionalBoundWorkflowTarget(input *BoundWorkflowTargetInput) (*BoundWorkflowTarget, error) {
	if input == nil {
		return nil, nil
	}
	return NewBoundWorkflowTarget(*input)
}

func newOptionalWorkflowRunTrigger(input *WorkflowRunTriggerInput) (*WorkflowRunTrigger, error) {
	if input == nil {
		return nil, nil
	}
	return NewWorkflowRunTrigger(*input)
}

func workflowTargetInputPtrFromTarget(value *BoundWorkflowTarget) *BoundWorkflowTargetInput {
	if value == nil {
		return nil
	}
	input := BoundWorkflowTargetInputFromTarget(value)
	return &input
}

func copyAgentMessages(values []*AgentMessage) []*AgentMessage {
	if len(values) == 0 {
		return nil
	}
	out := make([]*AgentMessage, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		out = append(out, gproto.Clone(value).(*AgentMessage))
	}
	return out
}

func copyAgentToolRefs(values []*AgentToolRef) []*AgentToolRef {
	if len(values) == 0 {
		return nil
	}
	out := make([]*AgentToolRef, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		out = append(out, gproto.Clone(value).(*AgentToolRef))
	}
	return out
}

func workflowActorFromInput(input *WorkflowActorInput) *WorkflowActor {
	if input == nil {
		return nil
	}
	return NewWorkflowActor(*input)
}

func workflowActorInputPtrFromActor(value *WorkflowActor) *WorkflowActorInput {
	if value == nil {
		return nil
	}
	input := WorkflowActorInputFromActor(value)
	return &input
}

func workflowEventMatchFromInput(input *WorkflowEventMatchInput) *WorkflowEventMatch {
	if input == nil {
		return nil
	}
	return &WorkflowEventMatch{
		Type:    input.Type,
		Source:  input.Source,
		Subject: input.Subject,
	}
}

func workflowEventMatchInputPtrFromMatch(value *WorkflowEventMatch) *WorkflowEventMatchInput {
	if value == nil {
		return nil
	}
	return &WorkflowEventMatchInput{
		Type:    value.GetType(),
		Source:  value.GetSource(),
		Subject: value.GetSubject(),
	}
}

func workflowRunAsSubjectFromInput(input *WorkflowRunAsSubjectInput) *WorkflowRunAsSubject {
	if input == nil {
		return nil
	}
	return &WorkflowRunAsSubject{
		SubjectId:   input.SubjectID,
		SubjectKind: input.SubjectKind,
		DisplayName: input.DisplayName,
		AuthSource:  input.AuthSource,
	}
}

func workflowRunAsSubjectInputPtrFromSubject(value *WorkflowRunAsSubject) *WorkflowRunAsSubjectInput {
	if value == nil {
		return nil
	}
	return &WorkflowRunAsSubjectInput{
		SubjectID:   value.GetSubjectId(),
		SubjectKind: value.GetSubjectKind(),
		DisplayName: value.GetDisplayName(),
		AuthSource:  value.GetAuthSource(),
	}
}

func workflowAccessPermissionsFromInputs(values []WorkflowAccessPermissionInput) []*WorkflowAccessPermission {
	if len(values) == 0 {
		return nil
	}
	out := make([]*WorkflowAccessPermission, 0, len(values))
	for _, value := range values {
		out = append(out, &WorkflowAccessPermission{
			Plugin:     value.Plugin,
			Operations: append([]string(nil), value.Operations...),
		})
	}
	return out
}

func workflowAccessPermissionInputsFromPermissions(values []*WorkflowAccessPermission) []WorkflowAccessPermissionInput {
	if len(values) == 0 {
		return nil
	}
	out := make([]WorkflowAccessPermissionInput, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		out = append(out, WorkflowAccessPermissionInput{
			Plugin:     value.GetPlugin(),
			Operations: append([]string(nil), value.GetOperations()...),
		})
	}
	return out
}

func newWorkflowOutputBindings(values []WorkflowOutputBindingInput) ([]*WorkflowOutputBinding, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]*WorkflowOutputBinding, 0, len(values))
	for _, value := range values {
		source, err := workflowOutputValueSourceFromInput(value.Value)
		if err != nil {
			return nil, err
		}
		out = append(out, &WorkflowOutputBinding{
			InputField: value.InputField,
			Value:      source,
		})
	}
	return out, nil
}

func workflowOutputBindingInputsFromBindings(values []*WorkflowOutputBinding) []WorkflowOutputBindingInput {
	if len(values) == 0 {
		return nil
	}
	out := make([]WorkflowOutputBindingInput, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		out = append(out, WorkflowOutputBindingInput{
			InputField: value.GetInputField(),
			Value:      workflowOutputValueSourceInputPtrFromSource(value.GetValue()),
		})
	}
	return out
}

func workflowOutputValueSourceFromInput(input *WorkflowOutputValueSourceInput) (*WorkflowOutputValueSource, error) {
	if input == nil {
		return nil, nil
	}
	selected := 0
	if input.AgentOutput != "" {
		selected++
	}
	if input.SignalPayload != "" {
		selected++
	}
	if input.SignalMetadata != "" {
		selected++
	}
	if input.Literal != nil {
		selected++
	}
	if input.AgentSession != "" {
		selected++
	}
	if selected == 0 {
		return &WorkflowOutputValueSource{}, nil
	}
	if selected > 1 {
		return nil, fmt.Errorf("workflow output value source must set exactly one source")
	}
	switch {
	case input.AgentOutput != "":
		return &WorkflowOutputValueSource{Kind: &WorkflowOutputValueSourceAgentOutput{AgentOutput: input.AgentOutput}}, nil
	case input.SignalPayload != "":
		return &WorkflowOutputValueSource{Kind: &WorkflowOutputValueSourceSignalPayload{SignalPayload: input.SignalPayload}}, nil
	case input.SignalMetadata != "":
		return &WorkflowOutputValueSource{Kind: &WorkflowOutputValueSourceSignalMetadata{SignalMetadata: input.SignalMetadata}}, nil
	case input.AgentSession != "":
		return &WorkflowOutputValueSource{Kind: &WorkflowOutputValueSourceAgentSession{AgentSession: input.AgentSession}}, nil
	default:
		literal, err := ValueFromAny(input.Literal)
		if err != nil {
			return nil, err
		}
		return &WorkflowOutputValueSource{Kind: &WorkflowOutputValueSourceLiteral{Literal: literal}}, nil
	}
}

func workflowOutputValueSourceInputPtrFromSource(value *WorkflowOutputValueSource) *WorkflowOutputValueSourceInput {
	if value == nil {
		return nil
	}
	switch kind := value.GetKind().(type) {
	case *WorkflowOutputValueSourceAgentOutput:
		return &WorkflowOutputValueSourceInput{AgentOutput: kind.AgentOutput}
	case *WorkflowOutputValueSourceSignalPayload:
		return &WorkflowOutputValueSourceInput{SignalPayload: kind.SignalPayload}
	case *WorkflowOutputValueSourceSignalMetadata:
		return &WorkflowOutputValueSourceInput{SignalMetadata: kind.SignalMetadata}
	case *WorkflowOutputValueSourceAgentSession:
		return &WorkflowOutputValueSourceInput{AgentSession: kind.AgentSession}
	case *WorkflowOutputValueSourceLiteral:
		return &WorkflowOutputValueSourceInput{Literal: AnyFromValue(kind.Literal)}
	default:
		return &WorkflowOutputValueSourceInput{}
	}
}
