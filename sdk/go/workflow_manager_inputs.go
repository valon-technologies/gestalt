package gestalt

import (
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/internal/gen/v1"
)

type WorkflowManagerStartRun struct {
	ProviderName   string
	Target         *BoundWorkflowTarget
	IdempotencyKey string
	WorkflowKey    string
	DefinitionID   string
}

type WorkflowManagerSignalRun struct {
	RunID  string
	Signal *WorkflowSignal
}

type WorkflowManagerSignalOrStartRun struct {
	ProviderName   string
	WorkflowKey    string
	Target         *BoundWorkflowTarget
	IdempotencyKey string
	Signal         *WorkflowSignal
	DefinitionID   string
}

type WorkflowManagerCreateDefinition struct {
	ProviderName   string
	Target         *BoundWorkflowTarget
	IdempotencyKey string
}

type WorkflowManagerGetDefinition struct {
	DefinitionID string
}

type WorkflowManagerUpdateDefinition struct {
	DefinitionID string
	ProviderName string
	Target       *BoundWorkflowTarget
}

type WorkflowManagerDeleteDefinition struct {
	DefinitionID string
}

type WorkflowManagerCreateSchedule struct {
	ProviderName   string
	Cron           string
	Timezone       string
	Target         *BoundWorkflowTarget
	Paused         bool
	IdempotencyKey string
	DefinitionID   string
}

type WorkflowManagerGetSchedule struct {
	ScheduleID string
}

type WorkflowManagerUpdateSchedule struct {
	ScheduleID   string
	ProviderName string
	Cron         string
	Timezone     string
	Target       *BoundWorkflowTarget
	Paused       bool
	DefinitionID string
}

type WorkflowManagerDeleteSchedule struct {
	ScheduleID string
}

type WorkflowManagerPauseSchedule struct {
	ScheduleID string
}

type WorkflowManagerResumeSchedule struct {
	ScheduleID string
}

type WorkflowManagerCreateEventTrigger struct {
	ProviderName   string
	Match          *WorkflowEventMatch
	Target         *BoundWorkflowTarget
	Paused         bool
	IdempotencyKey string
	DefinitionID   string
}

type WorkflowManagerGetEventTrigger struct {
	TriggerID string
}

type WorkflowManagerUpdateEventTrigger struct {
	TriggerID    string
	ProviderName string
	Match        *WorkflowEventMatch
	Target       *BoundWorkflowTarget
	Paused       bool
	DefinitionID string
}

type WorkflowManagerDeleteEventTrigger struct {
	TriggerID string
}

type WorkflowManagerPauseEventTrigger struct {
	TriggerID string
}

type WorkflowManagerResumeEventTrigger struct {
	TriggerID string
}

type WorkflowManagerPublishEvent struct {
	ProviderName string
	Event        *WorkflowEvent
}

type WorkflowManagerBoundRun struct {
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

type WorkflowManagerBoundDefinition struct {
	ID        string
	Target    *BoundWorkflowTarget
	CreatedBy *WorkflowActor
	CreatedAt time.Time
}

type WorkflowManagerBoundSchedule struct {
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

type WorkflowManagerBoundEventTrigger struct {
	ID           string
	Match        *WorkflowEventMatch
	Target       *BoundWorkflowTarget
	Paused       bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
	CreatedBy    *WorkflowActor
	ExecutionRef string
}

type WorkflowManagerRun struct {
	ProviderName string
	Run          *WorkflowManagerBoundRun
}

type WorkflowManagerRunSignal struct {
	ProviderName string
	Run          *WorkflowManagerBoundRun
	Signal       *WorkflowSignal
	StartedRun   bool
	WorkflowKey  string
}

type WorkflowManagerDefinition struct {
	ProviderName string
	Definition   *WorkflowManagerBoundDefinition
}

type WorkflowManagerSchedule struct {
	ProviderName string
	Schedule     *WorkflowManagerBoundSchedule
}

type WorkflowManagerEventTrigger struct {
	ProviderName string
	Trigger      *WorkflowManagerBoundEventTrigger
}

type WorkflowManagerPublishedEvent = WorkflowEvent

func newWorkflowManagerStartRunRequest(input WorkflowManagerStartRun) (*proto.WorkflowManagerStartRunRequest, error) {
	target, err := newOptionalBoundWorkflowTarget(input.Target)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowManagerStartRunRequest{
		ProviderName:   input.ProviderName,
		Target:         target,
		IdempotencyKey: input.IdempotencyKey,
		WorkflowKey:    input.WorkflowKey,
		DefinitionId:   input.DefinitionID,
	}, nil
}

func newWorkflowManagerSignalRunRequest(input WorkflowManagerSignalRun) (*proto.WorkflowManagerSignalRunRequest, error) {
	signal, err := newOptionalWorkflowSignal(input.Signal)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowManagerSignalRunRequest{
		RunId:  input.RunID,
		Signal: signal,
	}, nil
}

func newWorkflowManagerSignalOrStartRunRequest(input WorkflowManagerSignalOrStartRun) (*proto.WorkflowManagerSignalOrStartRunRequest, error) {
	target, err := newOptionalBoundWorkflowTarget(input.Target)
	if err != nil {
		return nil, err
	}
	signal, err := newOptionalWorkflowSignal(input.Signal)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowManagerSignalOrStartRunRequest{
		ProviderName:   input.ProviderName,
		WorkflowKey:    input.WorkflowKey,
		Target:         target,
		IdempotencyKey: input.IdempotencyKey,
		Signal:         signal,
		DefinitionId:   input.DefinitionID,
	}, nil
}

func newWorkflowManagerCreateDefinitionRequest(input WorkflowManagerCreateDefinition) (*proto.WorkflowManagerCreateDefinitionRequest, error) {
	target, err := newOptionalBoundWorkflowTarget(input.Target)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowManagerCreateDefinitionRequest{
		ProviderName:   input.ProviderName,
		Target:         target,
		IdempotencyKey: input.IdempotencyKey,
	}, nil
}

func newWorkflowManagerGetDefinitionRequest(input WorkflowManagerGetDefinition) *proto.WorkflowManagerGetDefinitionRequest {
	return &proto.WorkflowManagerGetDefinitionRequest{DefinitionId: input.DefinitionID}
}

func newWorkflowManagerUpdateDefinitionRequest(input WorkflowManagerUpdateDefinition) (*proto.WorkflowManagerUpdateDefinitionRequest, error) {
	target, err := newOptionalBoundWorkflowTarget(input.Target)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowManagerUpdateDefinitionRequest{
		DefinitionId: input.DefinitionID,
		ProviderName: input.ProviderName,
		Target:       target,
	}, nil
}

func newWorkflowManagerDeleteDefinitionRequest(input WorkflowManagerDeleteDefinition) *proto.WorkflowManagerDeleteDefinitionRequest {
	return &proto.WorkflowManagerDeleteDefinitionRequest{DefinitionId: input.DefinitionID}
}

func newWorkflowManagerCreateScheduleRequest(input WorkflowManagerCreateSchedule) (*proto.WorkflowManagerCreateScheduleRequest, error) {
	target, err := newOptionalBoundWorkflowTarget(input.Target)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowManagerCreateScheduleRequest{
		ProviderName:   input.ProviderName,
		Cron:           input.Cron,
		Timezone:       input.Timezone,
		Target:         target,
		Paused:         input.Paused,
		IdempotencyKey: input.IdempotencyKey,
		DefinitionId:   input.DefinitionID,
	}, nil
}

func newWorkflowManagerGetScheduleRequest(input WorkflowManagerGetSchedule) *proto.WorkflowManagerGetScheduleRequest {
	return &proto.WorkflowManagerGetScheduleRequest{ScheduleId: input.ScheduleID}
}

func newWorkflowManagerUpdateScheduleRequest(input WorkflowManagerUpdateSchedule) (*proto.WorkflowManagerUpdateScheduleRequest, error) {
	target, err := newOptionalBoundWorkflowTarget(input.Target)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowManagerUpdateScheduleRequest{
		ScheduleId:   input.ScheduleID,
		ProviderName: input.ProviderName,
		Cron:         input.Cron,
		Timezone:     input.Timezone,
		Target:       target,
		Paused:       input.Paused,
		DefinitionId: input.DefinitionID,
	}, nil
}

func newWorkflowManagerDeleteScheduleRequest(input WorkflowManagerDeleteSchedule) *proto.WorkflowManagerDeleteScheduleRequest {
	return &proto.WorkflowManagerDeleteScheduleRequest{ScheduleId: input.ScheduleID}
}

func newWorkflowManagerPauseScheduleRequest(input WorkflowManagerPauseSchedule) *proto.WorkflowManagerPauseScheduleRequest {
	return &proto.WorkflowManagerPauseScheduleRequest{ScheduleId: input.ScheduleID}
}

func newWorkflowManagerResumeScheduleRequest(input WorkflowManagerResumeSchedule) *proto.WorkflowManagerResumeScheduleRequest {
	return &proto.WorkflowManagerResumeScheduleRequest{ScheduleId: input.ScheduleID}
}

func newWorkflowManagerCreateEventTriggerRequest(input WorkflowManagerCreateEventTrigger) (*proto.WorkflowManagerCreateEventTriggerRequest, error) {
	target, err := newOptionalBoundWorkflowTarget(input.Target)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowManagerCreateEventTriggerRequest{
		ProviderName:   input.ProviderName,
		Match:          workflowEventMatchFromInput(input.Match),
		Target:         target,
		Paused:         input.Paused,
		IdempotencyKey: input.IdempotencyKey,
		DefinitionId:   input.DefinitionID,
	}, nil
}

func newWorkflowManagerGetEventTriggerRequest(input WorkflowManagerGetEventTrigger) *proto.WorkflowManagerGetEventTriggerRequest {
	return &proto.WorkflowManagerGetEventTriggerRequest{TriggerId: input.TriggerID}
}

func newWorkflowManagerUpdateEventTriggerRequest(input WorkflowManagerUpdateEventTrigger) (*proto.WorkflowManagerUpdateEventTriggerRequest, error) {
	target, err := newOptionalBoundWorkflowTarget(input.Target)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowManagerUpdateEventTriggerRequest{
		TriggerId:    input.TriggerID,
		ProviderName: input.ProviderName,
		Match:        workflowEventMatchFromInput(input.Match),
		Target:       target,
		Paused:       input.Paused,
		DefinitionId: input.DefinitionID,
	}, nil
}

func newWorkflowManagerDeleteEventTriggerRequest(input WorkflowManagerDeleteEventTrigger) *proto.WorkflowManagerDeleteEventTriggerRequest {
	return &proto.WorkflowManagerDeleteEventTriggerRequest{TriggerId: input.TriggerID}
}

func newWorkflowManagerPauseEventTriggerRequest(input WorkflowManagerPauseEventTrigger) *proto.WorkflowManagerPauseEventTriggerRequest {
	return &proto.WorkflowManagerPauseEventTriggerRequest{TriggerId: input.TriggerID}
}

func newWorkflowManagerResumeEventTriggerRequest(input WorkflowManagerResumeEventTrigger) *proto.WorkflowManagerResumeEventTriggerRequest {
	return &proto.WorkflowManagerResumeEventTriggerRequest{TriggerId: input.TriggerID}
}

func newWorkflowManagerPublishEventRequest(input WorkflowManagerPublishEvent) (*proto.WorkflowManagerPublishEventRequest, error) {
	event, err := newOptionalWorkflowEvent(input.Event)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowManagerPublishEventRequest{
		Event:        event,
		ProviderName: input.ProviderName,
	}, nil
}

func newOptionalWorkflowSignal(input *WorkflowSignal) (*proto.WorkflowSignal, error) {
	if input == nil {
		return nil, nil
	}
	return workflowSignalToProto(*input)
}

func newOptionalWorkflowEvent(input *WorkflowEvent) (*proto.WorkflowEvent, error) {
	if input == nil {
		return nil, nil
	}
	return workflowEventToProto(*input)
}

func workflowManagerBoundRunFromProto(value *proto.BoundWorkflowRun) (*WorkflowManagerBoundRun, error) {
	if value == nil {
		return nil, nil
	}
	startedAt, err := timePtrFromTimestamp(value.GetStartedAt())
	if err != nil {
		return nil, err
	}
	completedAt, err := timePtrFromTimestamp(value.GetCompletedAt())
	if err != nil {
		return nil, err
	}
	var trigger *WorkflowRunTrigger
	if value.GetTrigger() != nil {
		input, err := workflowRunTriggerFromProto(value.GetTrigger())
		if err != nil {
			return nil, err
		}
		trigger = &input
	}
	return &WorkflowManagerBoundRun{
		ID:            value.GetId(),
		Status:        WorkflowRunStatus(value.GetStatus()),
		Target:        workflowTargetInputPtrFromTarget(value.GetTarget()),
		Trigger:       trigger,
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

func workflowManagerBoundDefinitionFromProto(value *proto.BoundWorkflowDefinition) *WorkflowManagerBoundDefinition {
	if value == nil {
		return nil
	}
	return &WorkflowManagerBoundDefinition{
		ID:        value.GetId(),
		Target:    workflowTargetInputPtrFromTarget(value.GetTarget()),
		CreatedBy: workflowActorInputPtrFromActor(value.GetCreatedBy()),
		CreatedAt: timeFromTimestamp(value.GetCreatedAt()),
	}
}

func workflowManagerBoundScheduleFromProto(value *proto.BoundWorkflowSchedule) (*WorkflowManagerBoundSchedule, error) {
	if value == nil {
		return nil, nil
	}
	nextRunAt, err := timePtrFromTimestamp(value.GetNextRunAt())
	if err != nil {
		return nil, err
	}
	return &WorkflowManagerBoundSchedule{
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

func workflowManagerBoundEventTriggerFromProto(value *proto.BoundWorkflowEventTrigger) *WorkflowManagerBoundEventTrigger {
	if value == nil {
		return nil
	}
	return &WorkflowManagerBoundEventTrigger{
		ID:           value.GetId(),
		Match:        workflowEventMatchInputPtrFromMatch(value.GetMatch()),
		Target:       workflowTargetInputPtrFromTarget(value.GetTarget()),
		Paused:       value.GetPaused(),
		CreatedAt:    timeFromTimestamp(value.GetCreatedAt()),
		UpdatedAt:    timeFromTimestamp(value.GetUpdatedAt()),
		CreatedBy:    workflowActorInputPtrFromActor(value.GetCreatedBy()),
		ExecutionRef: value.GetExecutionRef(),
	}
}

func workflowManagerRunFromProto(value *proto.ManagedWorkflowRun) (*WorkflowManagerRun, error) {
	if value == nil {
		return nil, nil
	}
	run, err := workflowManagerBoundRunFromProto(value.GetRun())
	if err != nil {
		return nil, err
	}
	return &WorkflowManagerRun{ProviderName: value.GetProviderName(), Run: run}, nil
}

func workflowManagerRunSignalFromProto(value *proto.ManagedWorkflowRunSignal) (*WorkflowManagerRunSignal, error) {
	if value == nil {
		return nil, nil
	}
	run, err := workflowManagerBoundRunFromProto(value.GetRun())
	if err != nil {
		return nil, err
	}
	var signal *WorkflowSignal
	if value.GetSignal() != nil {
		input := workflowSignalFromProto(value.GetSignal())
		signal = &input
	}
	return &WorkflowManagerRunSignal{
		ProviderName: value.GetProviderName(),
		Run:          run,
		Signal:       signal,
		StartedRun:   value.GetStartedRun(),
		WorkflowKey:  value.GetWorkflowKey(),
	}, nil
}

func workflowManagerDefinitionFromProto(value *proto.ManagedWorkflowDefinition) *WorkflowManagerDefinition {
	if value == nil {
		return nil
	}
	return &WorkflowManagerDefinition{
		ProviderName: value.GetProviderName(),
		Definition:   workflowManagerBoundDefinitionFromProto(value.GetDefinition()),
	}
}

func workflowManagerScheduleFromProto(value *proto.ManagedWorkflowSchedule) (*WorkflowManagerSchedule, error) {
	if value == nil {
		return nil, nil
	}
	schedule, err := workflowManagerBoundScheduleFromProto(value.GetSchedule())
	if err != nil {
		return nil, err
	}
	return &WorkflowManagerSchedule{ProviderName: value.GetProviderName(), Schedule: schedule}, nil
}

func workflowManagerEventTriggerFromProto(value *proto.ManagedWorkflowEventTrigger) *WorkflowManagerEventTrigger {
	if value == nil {
		return nil
	}
	return &WorkflowManagerEventTrigger{
		ProviderName: value.GetProviderName(),
		Trigger:      workflowManagerBoundEventTriggerFromProto(value.GetTrigger()),
	}
}
