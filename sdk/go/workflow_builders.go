package gestalt

import (
	"fmt"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/internal/gen/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// BoundWorkflowPluginTarget contains fields for constructing a
// BoundWorkflowPluginTarget.
type BoundWorkflowPluginTarget struct {
	PluginName     string
	Operation      string
	Input          any
	Connection     string
	Instance       string
	CredentialMode string
}

// NewBoundWorkflowPluginTarget creates a plugin workflow target.
func NewBoundWorkflowPluginTarget(input BoundWorkflowPluginTarget) (*proto.BoundWorkflowPluginTarget, error) {
	value, err := StructFromAny(input.Input)
	if err != nil {
		return nil, err
	}
	return &proto.BoundWorkflowPluginTarget{
		PluginName:     input.PluginName,
		Operation:      input.Operation,
		Input:          value,
		Connection:     input.Connection,
		Instance:       input.Instance,
		CredentialMode: input.CredentialMode,
	}, nil
}

// BoundWorkflowPluginTargetFromTarget converts an existing protocol target
// into builder input.
func BoundWorkflowPluginTargetFromTarget(value *proto.BoundWorkflowPluginTarget) BoundWorkflowPluginTarget {
	if value == nil {
		return BoundWorkflowPluginTarget{}
	}
	return BoundWorkflowPluginTarget{
		PluginName:     value.GetPluginName(),
		Operation:      value.GetOperation(),
		Input:          MapFromStruct(value.GetInput()),
		Connection:     value.GetConnection(),
		Instance:       value.GetInstance(),
		CredentialMode: value.GetCredentialMode(),
	}
}

// WorkflowOutputDelivery contains fields for constructing a
// WorkflowOutputDelivery.
type WorkflowOutputDelivery struct {
	Target         *BoundWorkflowPluginTarget
	InputBindings  []WorkflowOutputBinding
	CredentialMode string
}

// WorkflowOutputValueSource contains fields for constructing a
// workflow output value source. Set at most one source field.
type WorkflowOutputValueSource struct {
	AgentOutput    string
	SignalPayload  string
	SignalMetadata string
	Literal        any
	AgentSession   string
}

// WorkflowOutputBinding contains fields for one workflow output
// binding.
type WorkflowOutputBinding struct {
	InputField string
	Value      *WorkflowOutputValueSource
}

// NewWorkflowOutputDelivery creates a workflow output delivery.
func NewWorkflowOutputDelivery(input WorkflowOutputDelivery) (*proto.WorkflowOutputDelivery, error) {
	var target *proto.BoundWorkflowPluginTarget
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
	return &proto.WorkflowOutputDelivery{
		Target:         target,
		InputBindings:  bindings,
		CredentialMode: input.CredentialMode,
	}, nil
}

// WorkflowOutputDeliveryFromDelivery converts an existing protocol delivery
// into builder input.
func WorkflowOutputDeliveryFromDelivery(value *proto.WorkflowOutputDelivery) *WorkflowOutputDelivery {
	if value == nil {
		return nil
	}
	var target *BoundWorkflowPluginTarget
	if value.GetTarget() != nil {
		input := BoundWorkflowPluginTargetFromTarget(value.GetTarget())
		target = &input
	}
	return &WorkflowOutputDelivery{
		Target:         target,
		InputBindings:  workflowOutputBindingInputsFromBindings(value.GetInputBindings()),
		CredentialMode: value.GetCredentialMode(),
	}
}

// BoundWorkflowAgentTarget contains fields for constructing a
// BoundWorkflowAgentTarget.
type BoundWorkflowAgentTarget struct {
	ProviderName         string
	Model                string
	Prompt               string
	Messages             []AgentMessage
	ToolRefs             []AgentToolRef
	ResponseSchema       any
	Metadata             any
	TimeoutSeconds       int32
	OutputDelivery       *WorkflowOutputDelivery
	ModelOptions         any
	SessionReadyDelivery *WorkflowOutputDelivery
}

// NewBoundWorkflowAgentTarget creates an agent workflow target.
func NewBoundWorkflowAgentTarget(input BoundWorkflowAgentTarget) (*proto.BoundWorkflowAgentTarget, error) {
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
	messages, err := agentMessagesFromInputs(input.Messages)
	if err != nil {
		return nil, err
	}
	messageProtos, err := agentMessagePtrsToProto(messages)
	if err != nil {
		return nil, err
	}
	toolRefs := agentToolRefPtrsToProto(agentToolRefsFromInputs(input.ToolRefs))
	return &proto.BoundWorkflowAgentTarget{
		ProviderName:         input.ProviderName,
		Model:                input.Model,
		Prompt:               input.Prompt,
		Messages:             messageProtos,
		ToolRefs:             toolRefs,
		ResponseSchema:       responseSchema,
		Metadata:             metadata,
		TimeoutSeconds:       input.TimeoutSeconds,
		OutputDelivery:       outputDelivery,
		ModelOptions:         modelOptions,
		SessionReadyDelivery: sessionReadyDelivery,
	}, nil
}

// BoundWorkflowAgentTargetFromTarget converts an existing protocol target
// into builder input.
func BoundWorkflowAgentTargetFromTarget(value *proto.BoundWorkflowAgentTarget) BoundWorkflowAgentTarget {
	if value == nil {
		return BoundWorkflowAgentTarget{}
	}
	return BoundWorkflowAgentTarget{
		ProviderName:         value.GetProviderName(),
		Model:                value.GetModel(),
		Prompt:               value.GetPrompt(),
		Messages:             agentMessagesFromPtrs(agentMessagePtrsFromProto(value.GetMessages())),
		ToolRefs:             agentToolRefsFromPtrs(agentToolRefPtrsFromProto(value.GetToolRefs())),
		ResponseSchema:       MapFromStruct(value.GetResponseSchema()),
		Metadata:             MapFromStruct(value.GetMetadata()),
		TimeoutSeconds:       value.GetTimeoutSeconds(),
		OutputDelivery:       WorkflowOutputDeliveryFromDelivery(value.GetOutputDelivery()),
		ModelOptions:         MapFromStruct(value.GetModelOptions()),
		SessionReadyDelivery: WorkflowOutputDeliveryFromDelivery(value.GetSessionReadyDelivery()),
	}
}

// BoundWorkflowTarget contains fields for constructing a
// BoundWorkflowTarget. Exactly one of Plugin or Agent should be set.
type BoundWorkflowTarget struct {
	Plugin *BoundWorkflowPluginTarget
	Agent  *BoundWorkflowAgentTarget
}

// WorkflowActor contains fields for constructing workflow actor
// metadata.
type WorkflowActor struct {
	SubjectID   string
	SubjectKind string
	DisplayName string
	AuthSource  string
}

// NewWorkflowActor creates workflow actor metadata.
func NewWorkflowActor(input WorkflowActor) *proto.WorkflowActor {
	return &proto.WorkflowActor{
		SubjectId:   input.SubjectID,
		SubjectKind: input.SubjectKind,
		DisplayName: input.DisplayName,
		AuthSource:  input.AuthSource,
	}
}

// WorkflowActorFromActor converts existing workflow actor metadata into
// builder input.
func WorkflowActorFromActor(value *proto.WorkflowActor) WorkflowActor {
	if value == nil {
		return WorkflowActor{}
	}
	return WorkflowActor{
		SubjectID:   value.GetSubjectId(),
		SubjectKind: value.GetSubjectKind(),
		DisplayName: value.GetDisplayName(),
		AuthSource:  value.GetAuthSource(),
	}
}

// NewBoundWorkflowTarget creates a workflow target.
func NewBoundWorkflowTarget(input BoundWorkflowTarget) (*proto.BoundWorkflowTarget, error) {
	switch {
	case input.Plugin != nil:
		plugin, err := NewBoundWorkflowPluginTarget(*input.Plugin)
		if err != nil {
			return nil, err
		}
		return &proto.BoundWorkflowTarget{Kind: &proto.BoundWorkflowTarget_Plugin{Plugin: plugin}}, nil
	case input.Agent != nil:
		agent, err := NewBoundWorkflowAgentTarget(*input.Agent)
		if err != nil {
			return nil, err
		}
		return &proto.BoundWorkflowTarget{Kind: &proto.BoundWorkflowTarget_Agent{Agent: agent}}, nil
	default:
		return &proto.BoundWorkflowTarget{}, nil
	}
}

// BoundWorkflowTargetFromTarget converts an existing protocol target into
// builder input.
func BoundWorkflowTargetFromTarget(value *proto.BoundWorkflowTarget) BoundWorkflowTarget {
	if value == nil {
		return BoundWorkflowTarget{}
	}
	if plugin := value.GetPlugin(); plugin != nil {
		input := BoundWorkflowPluginTargetFromTarget(plugin)
		return BoundWorkflowTarget{Plugin: &input}
	}
	if agent := value.GetAgent(); agent != nil {
		input := BoundWorkflowAgentTargetFromTarget(agent)
		return BoundWorkflowTarget{Agent: &input}
	}
	return BoundWorkflowTarget{}
}

// NewBoundWorkflowTargetFromTarget creates a copy of an existing workflow target
// through the target input builder.
func NewBoundWorkflowTargetFromTarget(value *proto.BoundWorkflowTarget) (*proto.BoundWorkflowTarget, error) {
	if value == nil {
		return nil, nil
	}
	return NewBoundWorkflowTarget(BoundWorkflowTargetFromTarget(value))
}

// WorkflowEvent contains fields for constructing a WorkflowEvent.
type WorkflowEvent struct {
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

// NewWorkflowEvent creates a workflow event.
func NewWorkflowEvent(input WorkflowEvent) (*proto.WorkflowEvent, error) {
	data, err := StructFromAny(input.Data)
	if err != nil {
		return nil, err
	}
	extensions, err := ValuesFromMap(input.Extensions)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowEvent{
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

// WorkflowEventFromEvent converts an existing protocol event into builder input.
func WorkflowEventFromEvent(value *proto.WorkflowEvent) WorkflowEvent {
	if value == nil {
		return WorkflowEvent{}
	}
	return WorkflowEvent{
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
// the event input builder.
func NewWorkflowEventFromEvent(value *proto.WorkflowEvent) (*proto.WorkflowEvent, error) {
	if value == nil {
		return nil, nil
	}
	return NewWorkflowEvent(WorkflowEventFromEvent(value))
}

// WorkflowSignal contains fields for constructing a
// WorkflowSignal.
type WorkflowSignal struct {
	ID             string
	Name           string
	Payload        any
	Metadata       any
	CreatedBy      *WorkflowActor
	CreatedAt      time.Time
	IdempotencyKey string
	Sequence       int64
}

// NewWorkflowSignal creates a workflow signal.
func NewWorkflowSignal(input WorkflowSignal) (*proto.WorkflowSignal, error) {
	payload, err := StructFromAny(input.Payload)
	if err != nil {
		return nil, err
	}
	metadata, err := StructFromAny(input.Metadata)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowSignal{
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

// WorkflowSignalFromSignal converts an existing protocol signal into builder input.
func WorkflowSignalFromSignal(value *proto.WorkflowSignal) WorkflowSignal {
	if value == nil {
		return WorkflowSignal{}
	}
	return WorkflowSignal{
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
// through the signal input builder.
func NewWorkflowSignalFromSignal(value *proto.WorkflowSignal) (*proto.WorkflowSignal, error) {
	if value == nil {
		return nil, nil
	}
	return NewWorkflowSignal(WorkflowSignalFromSignal(value))
}

// WorkflowScheduleTrigger contains fields for constructing a
// schedule-triggered workflow run trigger.
type WorkflowScheduleTrigger struct {
	ScheduleID   string
	ScheduledFor *time.Time
}

// WorkflowEventTriggerInvocation contains fields for
// constructing an event-triggered workflow run trigger.
type WorkflowEventTriggerInvocation struct {
	TriggerID string
	Event     *WorkflowEvent
}

// WorkflowRunTrigger contains fields for constructing a
// workflow run trigger. Exactly one trigger kind should be set.
type WorkflowRunTrigger struct {
	Manual   bool
	Schedule *WorkflowScheduleTrigger
	Event    *WorkflowEventTriggerInvocation
}

// NewWorkflowScheduleTrigger creates a schedule-trigger run trigger.
func NewWorkflowScheduleTrigger(scheduleID string, scheduledFor time.Time) *proto.WorkflowRunTrigger {
	return &proto.WorkflowRunTrigger{Kind: &proto.WorkflowRunTrigger_Schedule{Schedule: &proto.WorkflowScheduleTrigger{
		ScheduleId:   scheduleID,
		ScheduledFor: timestampFromNonZeroTime(scheduledFor),
	}}}
}

// NewWorkflowRunTrigger creates a workflow run trigger.
func NewWorkflowRunTrigger(input WorkflowRunTrigger) (*proto.WorkflowRunTrigger, error) {
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
		return &proto.WorkflowRunTrigger{}, nil
	}
	if selected > 1 {
		return nil, fmt.Errorf("workflow run trigger must set exactly one trigger kind")
	}
	if input.Manual {
		return &proto.WorkflowRunTrigger{Kind: &proto.WorkflowRunTrigger_Manual{Manual: &proto.WorkflowManualTrigger{}}}, nil
	}
	if input.Schedule != nil {
		return &proto.WorkflowRunTrigger{Kind: &proto.WorkflowRunTrigger_Schedule{Schedule: &proto.WorkflowScheduleTrigger{
			ScheduleId:   input.Schedule.ScheduleID,
			ScheduledFor: timestampFromOptionalTime(input.Schedule.ScheduledFor),
		}}}, nil
	}

	var event *proto.WorkflowEvent
	if input.Event.Event != nil {
		value, err := NewWorkflowEvent(*input.Event.Event)
		if err != nil {
			return nil, err
		}
		event = value
	}
	return &proto.WorkflowRunTrigger{Kind: &proto.WorkflowRunTrigger_Event{Event: &proto.WorkflowEventTriggerInvocation{
		TriggerId: input.Event.TriggerID,
		Event:     event,
	}}}, nil
}

// WorkflowRunTriggerFromTrigger converts an existing protocol trigger into
// builder input.
func WorkflowRunTriggerFromTrigger(value *proto.WorkflowRunTrigger) (WorkflowRunTrigger, error) {
	if value == nil {
		return WorkflowRunTrigger{}, nil
	}
	switch kind := value.GetKind().(type) {
	case *proto.WorkflowRunTrigger_Manual:
		return WorkflowRunTrigger{Manual: true}, nil
	case *proto.WorkflowRunTrigger_Schedule:
		if kind.Schedule == nil {
			return WorkflowRunTrigger{}, nil
		}
		scheduledFor, err := timePtrFromTimestamp(kind.Schedule.GetScheduledFor())
		if err != nil {
			return WorkflowRunTrigger{}, err
		}
		return WorkflowRunTrigger{Schedule: &WorkflowScheduleTrigger{
			ScheduleID:   kind.Schedule.GetScheduleId(),
			ScheduledFor: scheduledFor,
		}}, nil
	case *proto.WorkflowRunTrigger_Event:
		if kind.Event == nil {
			return WorkflowRunTrigger{}, nil
		}
		var event *WorkflowEvent
		if kind.Event.GetEvent() != nil {
			input := WorkflowEventFromEvent(kind.Event.GetEvent())
			event = &input
		}
		return WorkflowRunTrigger{Event: &WorkflowEventTriggerInvocation{
			TriggerID: kind.Event.GetTriggerId(),
			Event:     event,
		}}, nil
	default:
		return WorkflowRunTrigger{}, nil
	}
}

// NewWorkflowRunTriggerFromTrigger creates a copy of an existing workflow run
// trigger.
func NewWorkflowRunTriggerFromTrigger(value *proto.WorkflowRunTrigger) (*proto.WorkflowRunTrigger, error) {
	input, err := WorkflowRunTriggerFromTrigger(value)
	if err != nil || value == nil {
		return nil, err
	}
	return NewWorkflowRunTrigger(input)
}

// BoundWorkflowRun contains fields for constructing a
// BoundWorkflowRun.
type BoundWorkflowRun struct {
	ID            string
	Status        WorkflowRunStatus
	Target        *BoundWorkflowTarget
	Trigger       *WorkflowRunTrigger
	CreatedAt     time.Time
	StartedAt     *time.Time
	CompletedAt   *time.Time
	StatusMessage string
	ResultBody    string
	CreatedBy     *WorkflowActor
	ExecutionRef  string
	WorkflowKey   string
}

// NewBoundWorkflowRun creates a bound workflow run.
func NewBoundWorkflowRun(input BoundWorkflowRun) (*proto.BoundWorkflowRun, error) {
	target, err := newOptionalBoundWorkflowTarget(input.Target)
	if err != nil {
		return nil, err
	}
	trigger, err := newOptionalWorkflowRunTrigger(input.Trigger)
	if err != nil {
		return nil, err
	}
	return &proto.BoundWorkflowRun{
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

// BoundWorkflowRunFromRun converts an existing protocol run into builder input.
func BoundWorkflowRunFromRun(value *proto.BoundWorkflowRun) (BoundWorkflowRun, error) {
	if value == nil {
		return BoundWorkflowRun{}, nil
	}
	startedAt, err := timePtrFromTimestamp(value.GetStartedAt())
	if err != nil {
		return BoundWorkflowRun{}, err
	}
	completedAt, err := timePtrFromTimestamp(value.GetCompletedAt())
	if err != nil {
		return BoundWorkflowRun{}, err
	}
	trigger, err := WorkflowRunTriggerFromTrigger(value.GetTrigger())
	if err != nil {
		return BoundWorkflowRun{}, err
	}
	return BoundWorkflowRun{
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
// through the run input builder.
func NewBoundWorkflowRunFromRun(value *proto.BoundWorkflowRun) (*proto.BoundWorkflowRun, error) {
	input, err := BoundWorkflowRunFromRun(value)
	if err != nil || value == nil {
		return nil, err
	}
	return NewBoundWorkflowRun(input)
}

// BoundWorkflowSchedule contains fields for constructing a
// BoundWorkflowSchedule.
type BoundWorkflowSchedule struct {
	ID           string
	Cron         string
	Timezone     string
	Target       *BoundWorkflowTarget
	Paused       bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
	NextRunAt    *time.Time
	CreatedBy    *WorkflowActor
	ExecutionRef string
}

// NewBoundWorkflowSchedule creates a bound workflow schedule.
func NewBoundWorkflowSchedule(input BoundWorkflowSchedule) (*proto.BoundWorkflowSchedule, error) {
	target, err := newOptionalBoundWorkflowTarget(input.Target)
	if err != nil {
		return nil, err
	}
	return &proto.BoundWorkflowSchedule{
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

// BoundWorkflowScheduleFromSchedule converts an existing protocol schedule
// into builder input.
func BoundWorkflowScheduleFromSchedule(value *proto.BoundWorkflowSchedule) (BoundWorkflowSchedule, error) {
	if value == nil {
		return BoundWorkflowSchedule{}, nil
	}
	nextRunAt, err := timePtrFromTimestamp(value.GetNextRunAt())
	if err != nil {
		return BoundWorkflowSchedule{}, err
	}
	return BoundWorkflowSchedule{
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
// through the schedule input builder.
func NewBoundWorkflowScheduleFromSchedule(value *proto.BoundWorkflowSchedule) (*proto.BoundWorkflowSchedule, error) {
	input, err := BoundWorkflowScheduleFromSchedule(value)
	if err != nil || value == nil {
		return nil, err
	}
	return NewBoundWorkflowSchedule(input)
}

// BoundWorkflowEventTrigger contains fields for constructing a
// BoundWorkflowEventTrigger.
type BoundWorkflowEventTrigger struct {
	ID           string
	Match        *WorkflowEventMatch
	Target       *BoundWorkflowTarget
	Paused       bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
	CreatedBy    *WorkflowActor
	ExecutionRef string
}

// WorkflowEventMatch contains fields for matching workflow
// events.
type WorkflowEventMatch struct {
	Type    string
	Source  string
	Subject string
}

// NewBoundWorkflowEventTrigger creates a bound workflow event trigger.
func NewBoundWorkflowEventTrigger(input BoundWorkflowEventTrigger) (*proto.BoundWorkflowEventTrigger, error) {
	target, err := newOptionalBoundWorkflowTarget(input.Target)
	if err != nil {
		return nil, err
	}
	return &proto.BoundWorkflowEventTrigger{
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

// BoundWorkflowEventTriggerFromTrigger converts an existing protocol event
// trigger into builder input.
func BoundWorkflowEventTriggerFromTrigger(value *proto.BoundWorkflowEventTrigger) (BoundWorkflowEventTrigger, error) {
	if value == nil {
		return BoundWorkflowEventTrigger{}, nil
	}
	return BoundWorkflowEventTrigger{
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
// trigger through the event trigger input builder.
func NewBoundWorkflowEventTriggerFromTrigger(value *proto.BoundWorkflowEventTrigger) (*proto.BoundWorkflowEventTrigger, error) {
	input, err := BoundWorkflowEventTriggerFromTrigger(value)
	if err != nil || value == nil {
		return nil, err
	}
	return NewBoundWorkflowEventTrigger(input)
}

// WorkflowExecutionReference contains fields for constructing a
// WorkflowExecutionReference.
type WorkflowExecutionReference struct {
	ID                  string
	ProviderName        string
	Target              *BoundWorkflowTarget
	SubjectID           string
	CredentialSubjectID string
	Permissions         []WorkflowAccessPermission
	CreatedAt           time.Time
	RevokedAt           *time.Time
	SubjectKind         string
	DisplayName         string
	AuthSource          string
	CallerPluginName    string
	RunAs               *WorkflowRunAsSubject
	SourceDefinitionID  string
}

// WorkflowAccessPermission contains fields for an execution
// reference permission.
type WorkflowAccessPermission struct {
	Plugin     string
	Operations []string
}

// WorkflowRunAsSubject contains fields for workflow run-as
// metadata.
type WorkflowRunAsSubject struct {
	SubjectID   string
	SubjectKind string
	DisplayName string
	AuthSource  string
}

// NewWorkflowExecutionReference creates a workflow execution reference.
func NewWorkflowExecutionReference(input WorkflowExecutionReference) (*proto.WorkflowExecutionReference, error) {
	target, err := newOptionalBoundWorkflowTarget(input.Target)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowExecutionReference{
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

// WorkflowExecutionReferenceFromReference converts an existing protocol
// execution reference into builder input.
func WorkflowExecutionReferenceFromReference(value *proto.WorkflowExecutionReference) (WorkflowExecutionReference, error) {
	if value == nil {
		return WorkflowExecutionReference{}, nil
	}
	revokedAt, err := timePtrFromTimestamp(value.GetRevokedAt())
	if err != nil {
		return WorkflowExecutionReference{}, err
	}
	return WorkflowExecutionReference{
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
// execution reference through the execution reference input builder.
func NewWorkflowExecutionReferenceFromReference(value *proto.WorkflowExecutionReference) (*proto.WorkflowExecutionReference, error) {
	input, err := WorkflowExecutionReferenceFromReference(value)
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

func newOptionalWorkflowOutputDelivery(input *WorkflowOutputDelivery) (*proto.WorkflowOutputDelivery, error) {
	if input == nil {
		return nil, nil
	}
	return NewWorkflowOutputDelivery(*input)
}

func newOptionalBoundWorkflowTarget(input *BoundWorkflowTarget) (*proto.BoundWorkflowTarget, error) {
	if input == nil {
		return nil, nil
	}
	return NewBoundWorkflowTarget(*input)
}

func newOptionalWorkflowRunTrigger(input *WorkflowRunTrigger) (*proto.WorkflowRunTrigger, error) {
	if input == nil {
		return nil, nil
	}
	return NewWorkflowRunTrigger(*input)
}

func workflowTargetInputPtrFromTarget(value *proto.BoundWorkflowTarget) *BoundWorkflowTarget {
	if value == nil {
		return nil
	}
	input := BoundWorkflowTargetFromTarget(value)
	return &input
}

func workflowActorFromInput(input *WorkflowActor) *proto.WorkflowActor {
	if input == nil {
		return nil
	}
	return NewWorkflowActor(*input)
}

func workflowActorInputPtrFromActor(value *proto.WorkflowActor) *WorkflowActor {
	if value == nil {
		return nil
	}
	input := WorkflowActorFromActor(value)
	return &input
}

func workflowEventMatchFromInput(input *WorkflowEventMatch) *proto.WorkflowEventMatch {
	if input == nil {
		return nil
	}
	return &proto.WorkflowEventMatch{
		Type:    input.Type,
		Source:  input.Source,
		Subject: input.Subject,
	}
}

func workflowEventMatchInputPtrFromMatch(value *proto.WorkflowEventMatch) *WorkflowEventMatch {
	if value == nil {
		return nil
	}
	return &WorkflowEventMatch{
		Type:    value.GetType(),
		Source:  value.GetSource(),
		Subject: value.GetSubject(),
	}
}

func workflowRunAsSubjectFromInput(input *WorkflowRunAsSubject) *proto.WorkflowRunAsSubject {
	if input == nil {
		return nil
	}
	return &proto.WorkflowRunAsSubject{
		SubjectId:   input.SubjectID,
		SubjectKind: input.SubjectKind,
		DisplayName: input.DisplayName,
		AuthSource:  input.AuthSource,
	}
}

func workflowRunAsSubjectInputPtrFromSubject(value *proto.WorkflowRunAsSubject) *WorkflowRunAsSubject {
	if value == nil {
		return nil
	}
	return &WorkflowRunAsSubject{
		SubjectID:   value.GetSubjectId(),
		SubjectKind: value.GetSubjectKind(),
		DisplayName: value.GetDisplayName(),
		AuthSource:  value.GetAuthSource(),
	}
}

func workflowAccessPermissionsFromInputs(values []WorkflowAccessPermission) []*proto.WorkflowAccessPermission {
	if len(values) == 0 {
		return nil
	}
	out := make([]*proto.WorkflowAccessPermission, 0, len(values))
	for _, value := range values {
		out = append(out, &proto.WorkflowAccessPermission{
			Plugin:     value.Plugin,
			Operations: append([]string(nil), value.Operations...),
		})
	}
	return out
}

func workflowAccessPermissionInputsFromPermissions(values []*proto.WorkflowAccessPermission) []WorkflowAccessPermission {
	if len(values) == 0 {
		return nil
	}
	out := make([]WorkflowAccessPermission, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		out = append(out, WorkflowAccessPermission{
			Plugin:     value.GetPlugin(),
			Operations: append([]string(nil), value.GetOperations()...),
		})
	}
	return out
}

func newWorkflowOutputBindings(values []WorkflowOutputBinding) ([]*proto.WorkflowOutputBinding, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]*proto.WorkflowOutputBinding, 0, len(values))
	for _, value := range values {
		source, err := workflowOutputValueSourceFromInput(value.Value)
		if err != nil {
			return nil, err
		}
		out = append(out, &proto.WorkflowOutputBinding{
			InputField: value.InputField,
			Value:      source,
		})
	}
	return out, nil
}

func workflowOutputBindingInputsFromBindings(values []*proto.WorkflowOutputBinding) []WorkflowOutputBinding {
	if len(values) == 0 {
		return nil
	}
	out := make([]WorkflowOutputBinding, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		out = append(out, WorkflowOutputBinding{
			InputField: value.GetInputField(),
			Value:      workflowOutputValueSourceInputPtrFromSource(value.GetValue()),
		})
	}
	return out
}

func workflowOutputValueSourceFromInput(input *WorkflowOutputValueSource) (*proto.WorkflowOutputValueSource, error) {
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
		return &proto.WorkflowOutputValueSource{}, nil
	}
	if selected > 1 {
		return nil, fmt.Errorf("workflow output value source must set exactly one source")
	}
	switch {
	case input.AgentOutput != "":
		return &proto.WorkflowOutputValueSource{Kind: &proto.WorkflowOutputValueSource_AgentOutput{AgentOutput: input.AgentOutput}}, nil
	case input.SignalPayload != "":
		return &proto.WorkflowOutputValueSource{Kind: &proto.WorkflowOutputValueSource_SignalPayload{SignalPayload: input.SignalPayload}}, nil
	case input.SignalMetadata != "":
		return &proto.WorkflowOutputValueSource{Kind: &proto.WorkflowOutputValueSource_SignalMetadata{SignalMetadata: input.SignalMetadata}}, nil
	case input.AgentSession != "":
		return &proto.WorkflowOutputValueSource{Kind: &proto.WorkflowOutputValueSource_AgentSession{AgentSession: input.AgentSession}}, nil
	default:
		literal, err := ValueFromAny(input.Literal)
		if err != nil {
			return nil, err
		}
		return &proto.WorkflowOutputValueSource{Kind: &proto.WorkflowOutputValueSource_Literal{Literal: literal}}, nil
	}
}

func workflowOutputValueSourceInputPtrFromSource(value *proto.WorkflowOutputValueSource) *WorkflowOutputValueSource {
	if value == nil {
		return nil
	}
	switch kind := value.GetKind().(type) {
	case *proto.WorkflowOutputValueSource_AgentOutput:
		return &WorkflowOutputValueSource{AgentOutput: kind.AgentOutput}
	case *proto.WorkflowOutputValueSource_SignalPayload:
		return &WorkflowOutputValueSource{SignalPayload: kind.SignalPayload}
	case *proto.WorkflowOutputValueSource_SignalMetadata:
		return &WorkflowOutputValueSource{SignalMetadata: kind.SignalMetadata}
	case *proto.WorkflowOutputValueSource_AgentSession:
		return &WorkflowOutputValueSource{AgentSession: kind.AgentSession}
	case *proto.WorkflowOutputValueSource_Literal:
		return &WorkflowOutputValueSource{Literal: AnyFromValue(kind.Literal)}
	default:
		return &WorkflowOutputValueSource{}
	}
}
