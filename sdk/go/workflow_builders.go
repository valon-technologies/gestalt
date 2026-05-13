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

// boundWorkflowPluginTargetToProto creates a plugin workflow target.
func boundWorkflowPluginTargetToProto(input BoundWorkflowPluginTarget) (*proto.BoundWorkflowPluginTarget, error) {
	value, err := structFromAny(input.Input)
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

// boundWorkflowPluginTargetFromProto converts an existing protocol target
// into builder input.
func boundWorkflowPluginTargetFromProto(value *proto.BoundWorkflowPluginTarget) BoundWorkflowPluginTarget {
	if value == nil {
		return BoundWorkflowPluginTarget{}
	}
	return BoundWorkflowPluginTarget{
		PluginName:     value.GetPluginName(),
		Operation:      value.GetOperation(),
		Input:          mapFromStruct(value.GetInput()),
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

// workflowOutputDeliveryToProto creates a workflow output delivery.
func workflowOutputDeliveryToProto(input WorkflowOutputDelivery) (*proto.WorkflowOutputDelivery, error) {
	var target *proto.BoundWorkflowPluginTarget
	if input.Target != nil {
		value, err := boundWorkflowPluginTargetToProto(*input.Target)
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

// workflowOutputDeliveryFromProto converts an existing protocol delivery
// into builder input.
func workflowOutputDeliveryFromProto(value *proto.WorkflowOutputDelivery) *WorkflowOutputDelivery {
	if value == nil {
		return nil
	}
	var target *BoundWorkflowPluginTarget
	if value.GetTarget() != nil {
		input := boundWorkflowPluginTargetFromProto(value.GetTarget())
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

// boundWorkflowAgentTargetToProto creates an agent workflow target.
func boundWorkflowAgentTargetToProto(input BoundWorkflowAgentTarget) (*proto.BoundWorkflowAgentTarget, error) {
	responseSchema, err := structFromAny(input.ResponseSchema)
	if err != nil {
		return nil, err
	}
	metadata, err := structFromAny(input.Metadata)
	if err != nil {
		return nil, err
	}
	modelOptions, err := structFromAny(input.ModelOptions)
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
	messageProtos, err := agentMessagesToProto(input.Messages)
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

// boundWorkflowAgentTargetFromProto converts an existing protocol target
// into builder input.
func boundWorkflowAgentTargetFromProto(value *proto.BoundWorkflowAgentTarget) BoundWorkflowAgentTarget {
	if value == nil {
		return BoundWorkflowAgentTarget{}
	}
	return BoundWorkflowAgentTarget{
		ProviderName:         value.GetProviderName(),
		Model:                value.GetModel(),
		Prompt:               value.GetPrompt(),
		Messages:             agentMessagesFromPtrs(agentMessagePtrsFromProto(value.GetMessages())),
		ToolRefs:             agentToolRefsFromPtrs(agentToolRefPtrsFromProto(value.GetToolRefs())),
		ResponseSchema:       mapFromStruct(value.GetResponseSchema()),
		Metadata:             mapFromStruct(value.GetMetadata()),
		TimeoutSeconds:       value.GetTimeoutSeconds(),
		OutputDelivery:       workflowOutputDeliveryFromProto(value.GetOutputDelivery()),
		ModelOptions:         mapFromStruct(value.GetModelOptions()),
		SessionReadyDelivery: workflowOutputDeliveryFromProto(value.GetSessionReadyDelivery()),
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

// workflowActorToProto creates workflow actor metadata.
func workflowActorToProto(input WorkflowActor) *proto.WorkflowActor {
	return &proto.WorkflowActor{
		SubjectId:   input.SubjectID,
		SubjectKind: input.SubjectKind,
		DisplayName: input.DisplayName,
		AuthSource:  input.AuthSource,
	}
}

// workflowActorFromProto converts existing workflow actor metadata into
// builder input.
func workflowActorFromProto(value *proto.WorkflowActor) WorkflowActor {
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

// boundWorkflowTargetToProto creates a workflow target.
func boundWorkflowTargetToProto(input BoundWorkflowTarget) (*proto.BoundWorkflowTarget, error) {
	switch {
	case input.Plugin != nil:
		plugin, err := boundWorkflowPluginTargetToProto(*input.Plugin)
		if err != nil {
			return nil, err
		}
		return &proto.BoundWorkflowTarget{Kind: &proto.BoundWorkflowTarget_Plugin{Plugin: plugin}}, nil
	case input.Agent != nil:
		agent, err := boundWorkflowAgentTargetToProto(*input.Agent)
		if err != nil {
			return nil, err
		}
		return &proto.BoundWorkflowTarget{Kind: &proto.BoundWorkflowTarget_Agent{Agent: agent}}, nil
	default:
		return &proto.BoundWorkflowTarget{}, nil
	}
}

// boundWorkflowTargetFromProto converts an existing protocol target into
// builder input.
func boundWorkflowTargetFromProto(value *proto.BoundWorkflowTarget) BoundWorkflowTarget {
	if value == nil {
		return BoundWorkflowTarget{}
	}
	if plugin := value.GetPlugin(); plugin != nil {
		input := boundWorkflowPluginTargetFromProto(plugin)
		return BoundWorkflowTarget{Plugin: &input}
	}
	if agent := value.GetAgent(); agent != nil {
		input := boundWorkflowAgentTargetFromProto(agent)
		return BoundWorkflowTarget{Agent: &input}
	}
	return BoundWorkflowTarget{}
}

// cloneBoundWorkflowTargetProto creates a copy of an existing workflow target
// through the target input builder.
func cloneBoundWorkflowTargetProto(value *proto.BoundWorkflowTarget) (*proto.BoundWorkflowTarget, error) {
	if value == nil {
		return nil, nil
	}
	return boundWorkflowTargetToProto(boundWorkflowTargetFromProto(value))
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

// workflowEventToProto creates a workflow event.
func workflowEventToProto(input WorkflowEvent) (*proto.WorkflowEvent, error) {
	data, err := structFromAny(input.Data)
	if err != nil {
		return nil, err
	}
	extensions, err := valuesFromMap(input.Extensions)
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

// workflowEventFromProto converts an existing protocol event into builder input.
func workflowEventFromProto(value *proto.WorkflowEvent) WorkflowEvent {
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
		Data:            mapFromStruct(value.GetData()),
		Extensions:      mapFromValues(value.GetExtensions()),
	}
}

// cloneWorkflowEventProto creates a copy of an existing workflow event through
// the event input builder.
func cloneWorkflowEventProto(value *proto.WorkflowEvent) (*proto.WorkflowEvent, error) {
	if value == nil {
		return nil, nil
	}
	return workflowEventToProto(workflowEventFromProto(value))
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

// workflowSignalToProto creates a workflow signal.
func workflowSignalToProto(input WorkflowSignal) (*proto.WorkflowSignal, error) {
	payload, err := structFromAny(input.Payload)
	if err != nil {
		return nil, err
	}
	metadata, err := structFromAny(input.Metadata)
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

// workflowSignalFromProto converts an existing protocol signal into builder input.
func workflowSignalFromProto(value *proto.WorkflowSignal) WorkflowSignal {
	if value == nil {
		return WorkflowSignal{}
	}
	return WorkflowSignal{
		ID:             value.GetId(),
		Name:           value.GetName(),
		Payload:        mapFromStruct(value.GetPayload()),
		Metadata:       mapFromStruct(value.GetMetadata()),
		CreatedBy:      workflowActorInputPtrFromActor(value.GetCreatedBy()),
		CreatedAt:      timeFromTimestamp(value.GetCreatedAt()),
		IdempotencyKey: value.GetIdempotencyKey(),
		Sequence:       value.GetSequence(),
	}
}

// cloneWorkflowSignalProto creates a copy of an existing workflow signal
// through the signal input builder.
func cloneWorkflowSignalProto(value *proto.WorkflowSignal) (*proto.WorkflowSignal, error) {
	if value == nil {
		return nil, nil
	}
	return workflowSignalToProto(workflowSignalFromProto(value))
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

// workflowScheduleTriggerToProto creates a schedule-trigger run trigger.
func workflowScheduleTriggerToProto(scheduleID string, scheduledFor time.Time) *proto.WorkflowRunTrigger {
	return &proto.WorkflowRunTrigger{Kind: &proto.WorkflowRunTrigger_Schedule{Schedule: &proto.WorkflowScheduleTrigger{
		ScheduleId:   scheduleID,
		ScheduledFor: timestampFromNonZeroTime(scheduledFor),
	}}}
}

// workflowRunTriggerToProto creates a workflow run trigger.
func workflowRunTriggerToProto(input WorkflowRunTrigger) (*proto.WorkflowRunTrigger, error) {
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
		value, err := workflowEventToProto(*input.Event.Event)
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

// workflowRunTriggerFromProto converts an existing protocol trigger into
// builder input.
func workflowRunTriggerFromProto(value *proto.WorkflowRunTrigger) (WorkflowRunTrigger, error) {
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
			input := workflowEventFromProto(kind.Event.GetEvent())
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

// cloneWorkflowRunTriggerProto creates a copy of an existing workflow run
// trigger.
func cloneWorkflowRunTriggerProto(value *proto.WorkflowRunTrigger) (*proto.WorkflowRunTrigger, error) {
	input, err := workflowRunTriggerFromProto(value)
	if err != nil || value == nil {
		return nil, err
	}
	return workflowRunTriggerToProto(input)
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

// boundWorkflowRunToProto creates a bound workflow run.
func boundWorkflowRunToProto(input BoundWorkflowRun) (*proto.BoundWorkflowRun, error) {
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
		Status:        proto.WorkflowRunStatus(input.Status),
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

// boundWorkflowRunFromProto converts an existing protocol run into builder input.
func boundWorkflowRunFromProto(value *proto.BoundWorkflowRun) (BoundWorkflowRun, error) {
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
	trigger, err := workflowRunTriggerFromProto(value.GetTrigger())
	if err != nil {
		return BoundWorkflowRun{}, err
	}
	return BoundWorkflowRun{
		ID:            value.GetId(),
		Status:        WorkflowRunStatus(value.GetStatus()),
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

// cloneBoundWorkflowRunProto creates a copy of an existing bound workflow run
// through the run input builder.
func cloneBoundWorkflowRunProto(value *proto.BoundWorkflowRun) (*proto.BoundWorkflowRun, error) {
	input, err := boundWorkflowRunFromProto(value)
	if err != nil || value == nil {
		return nil, err
	}
	return boundWorkflowRunToProto(input)
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

// boundWorkflowScheduleToProto creates a bound workflow schedule.
func boundWorkflowScheduleToProto(input BoundWorkflowSchedule) (*proto.BoundWorkflowSchedule, error) {
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

// boundWorkflowScheduleFromProto converts an existing protocol schedule
// into builder input.
func boundWorkflowScheduleFromProto(value *proto.BoundWorkflowSchedule) (BoundWorkflowSchedule, error) {
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

// cloneBoundWorkflowScheduleProto creates a copy of an existing schedule
// through the schedule input builder.
func cloneBoundWorkflowScheduleProto(value *proto.BoundWorkflowSchedule) (*proto.BoundWorkflowSchedule, error) {
	input, err := boundWorkflowScheduleFromProto(value)
	if err != nil || value == nil {
		return nil, err
	}
	return boundWorkflowScheduleToProto(input)
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

// boundWorkflowEventTriggerToProto creates a bound workflow event trigger.
func boundWorkflowEventTriggerToProto(input BoundWorkflowEventTrigger) (*proto.BoundWorkflowEventTrigger, error) {
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

// boundWorkflowEventTriggerFromProto converts an existing protocol event
// trigger into builder input.
func boundWorkflowEventTriggerFromProto(value *proto.BoundWorkflowEventTrigger) (BoundWorkflowEventTrigger, error) {
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

// cloneBoundWorkflowEventTriggerProto creates a copy of an existing event
// trigger through the event trigger input builder.
func cloneBoundWorkflowEventTriggerProto(value *proto.BoundWorkflowEventTrigger) (*proto.BoundWorkflowEventTrigger, error) {
	input, err := boundWorkflowEventTriggerFromProto(value)
	if err != nil || value == nil {
		return nil, err
	}
	return boundWorkflowEventTriggerToProto(input)
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

// workflowExecutionReferenceToProto creates a workflow execution reference.
func workflowExecutionReferenceToProto(input WorkflowExecutionReference) (*proto.WorkflowExecutionReference, error) {
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

// workflowExecutionReferenceFromProto converts an existing protocol
// execution reference into builder input.
func workflowExecutionReferenceFromProto(value *proto.WorkflowExecutionReference) (WorkflowExecutionReference, error) {
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

// cloneWorkflowExecutionReferenceProto creates a copy of an existing
// execution reference through the execution reference input builder.
func cloneWorkflowExecutionReferenceProto(value *proto.WorkflowExecutionReference) (*proto.WorkflowExecutionReference, error) {
	input, err := workflowExecutionReferenceFromProto(value)
	if err != nil || value == nil {
		return nil, err
	}
	return workflowExecutionReferenceToProto(input)
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
	return workflowOutputDeliveryToProto(*input)
}

func newOptionalBoundWorkflowTarget(input *BoundWorkflowTarget) (*proto.BoundWorkflowTarget, error) {
	if input == nil {
		return nil, nil
	}
	return boundWorkflowTargetToProto(*input)
}

func newOptionalWorkflowRunTrigger(input *WorkflowRunTrigger) (*proto.WorkflowRunTrigger, error) {
	if input == nil {
		return nil, nil
	}
	return workflowRunTriggerToProto(*input)
}

func workflowTargetInputPtrFromTarget(value *proto.BoundWorkflowTarget) *BoundWorkflowTarget {
	if value == nil {
		return nil
	}
	input := boundWorkflowTargetFromProto(value)
	return &input
}

func workflowActorFromInput(input *WorkflowActor) *proto.WorkflowActor {
	if input == nil {
		return nil
	}
	return workflowActorToProto(*input)
}

func workflowActorInputPtrFromActor(value *proto.WorkflowActor) *WorkflowActor {
	if value == nil {
		return nil
	}
	input := workflowActorFromProto(value)
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
		literal, err := valueFromAny(input.Literal)
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
		return &WorkflowOutputValueSource{Literal: anyFromValue(kind.Literal)}
	default:
		return &WorkflowOutputValueSource{}
	}
}
