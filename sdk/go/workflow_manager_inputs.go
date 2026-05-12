package gestalt

import (
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/internal/gen/v1"
)

type WorkflowManagerStartRunInput struct {
	ProviderName   string
	Target         *BoundWorkflowTargetInput
	IdempotencyKey string
	WorkflowKey    string
	DefinitionID   string
}

type WorkflowManagerSignalRunInput struct {
	RunID  string
	Signal *WorkflowSignalInput
}

type WorkflowManagerSignalOrStartRunInput struct {
	ProviderName   string
	WorkflowKey    string
	Target         *BoundWorkflowTargetInput
	IdempotencyKey string
	Signal         *WorkflowSignalInput
	DefinitionID   string
}

type WorkflowManagerCreateDefinitionInput struct {
	ProviderName   string
	Target         *BoundWorkflowTargetInput
	IdempotencyKey string
}

type WorkflowManagerGetDefinitionInput struct {
	DefinitionID string
}

type WorkflowManagerUpdateDefinitionInput struct {
	DefinitionID string
	ProviderName string
	Target       *BoundWorkflowTargetInput
}

type WorkflowManagerDeleteDefinitionInput struct {
	DefinitionID string
}

type WorkflowManagerCreateScheduleInput struct {
	ProviderName   string
	Cron           string
	Timezone       string
	Target         *BoundWorkflowTargetInput
	Paused         bool
	IdempotencyKey string
	DefinitionID   string
}

type WorkflowManagerGetScheduleInput struct {
	ScheduleID string
}

type WorkflowManagerUpdateScheduleInput struct {
	ScheduleID   string
	ProviderName string
	Cron         string
	Timezone     string
	Target       *BoundWorkflowTargetInput
	Paused       bool
	DefinitionID string
}

type WorkflowManagerDeleteScheduleInput struct {
	ScheduleID string
}

type WorkflowManagerPauseScheduleInput struct {
	ScheduleID string
}

type WorkflowManagerResumeScheduleInput struct {
	ScheduleID string
}

type WorkflowManagerCreateEventTriggerInput struct {
	ProviderName   string
	Match          *WorkflowEventMatchInput
	Target         *BoundWorkflowTargetInput
	Paused         bool
	IdempotencyKey string
	DefinitionID   string
}

type WorkflowManagerGetEventTriggerInput struct {
	TriggerID string
}

type WorkflowManagerUpdateEventTriggerInput struct {
	TriggerID    string
	ProviderName string
	Match        *WorkflowEventMatchInput
	Target       *BoundWorkflowTargetInput
	Paused       bool
	DefinitionID string
}

type WorkflowManagerDeleteEventTriggerInput struct {
	TriggerID string
}

type WorkflowManagerPauseEventTriggerInput struct {
	TriggerID string
}

type WorkflowManagerResumeEventTriggerInput struct {
	TriggerID string
}

type WorkflowManagerPublishEventInput struct {
	ProviderName string
	Event        *WorkflowEventInput
}

type WorkflowManagerBoundRun struct {
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

type WorkflowManagerBoundDefinition struct {
	ID        string
	Target    *BoundWorkflowTargetInput
	CreatedBy *WorkflowActorInput
	CreatedAt time.Time
}

type WorkflowManagerBoundSchedule struct {
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

type WorkflowManagerBoundEventTrigger struct {
	ID           string
	Match        *WorkflowEventMatchInput
	Target       *BoundWorkflowTargetInput
	Paused       bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
	CreatedBy    *WorkflowActorInput
	ExecutionRef string
}

type WorkflowManagerRun struct {
	ProviderName string
	Run          *WorkflowManagerBoundRun
}

type WorkflowManagerRunSignal struct {
	ProviderName string
	Run          *WorkflowManagerBoundRun
	Signal       *WorkflowSignalInput
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

type WorkflowManagerPublishedEvent = WorkflowEventInput

func NewWorkflowManagerStartRunRequest(input WorkflowManagerStartRunInput) (*proto.WorkflowManagerStartRunRequest, error) {
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

func NewWorkflowManagerSignalRunRequest(input WorkflowManagerSignalRunInput) (*proto.WorkflowManagerSignalRunRequest, error) {
	signal, err := newOptionalWorkflowSignal(input.Signal)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowManagerSignalRunRequest{
		RunId:  input.RunID,
		Signal: signal,
	}, nil
}

func NewWorkflowManagerSignalOrStartRunRequest(input WorkflowManagerSignalOrStartRunInput) (*proto.WorkflowManagerSignalOrStartRunRequest, error) {
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

func NewWorkflowManagerCreateDefinitionRequest(input WorkflowManagerCreateDefinitionInput) (*proto.WorkflowManagerCreateDefinitionRequest, error) {
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

func NewWorkflowManagerGetDefinitionRequest(input WorkflowManagerGetDefinitionInput) *proto.WorkflowManagerGetDefinitionRequest {
	return &proto.WorkflowManagerGetDefinitionRequest{DefinitionId: input.DefinitionID}
}

func NewWorkflowManagerUpdateDefinitionRequest(input WorkflowManagerUpdateDefinitionInput) (*proto.WorkflowManagerUpdateDefinitionRequest, error) {
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

func NewWorkflowManagerDeleteDefinitionRequest(input WorkflowManagerDeleteDefinitionInput) *proto.WorkflowManagerDeleteDefinitionRequest {
	return &proto.WorkflowManagerDeleteDefinitionRequest{DefinitionId: input.DefinitionID}
}

func NewWorkflowManagerCreateScheduleRequest(input WorkflowManagerCreateScheduleInput) (*proto.WorkflowManagerCreateScheduleRequest, error) {
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

func NewWorkflowManagerGetScheduleRequest(input WorkflowManagerGetScheduleInput) *proto.WorkflowManagerGetScheduleRequest {
	return &proto.WorkflowManagerGetScheduleRequest{ScheduleId: input.ScheduleID}
}

func NewWorkflowManagerUpdateScheduleRequest(input WorkflowManagerUpdateScheduleInput) (*proto.WorkflowManagerUpdateScheduleRequest, error) {
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

func NewWorkflowManagerDeleteScheduleRequest(input WorkflowManagerDeleteScheduleInput) *proto.WorkflowManagerDeleteScheduleRequest {
	return &proto.WorkflowManagerDeleteScheduleRequest{ScheduleId: input.ScheduleID}
}

func NewWorkflowManagerPauseScheduleRequest(input WorkflowManagerPauseScheduleInput) *proto.WorkflowManagerPauseScheduleRequest {
	return &proto.WorkflowManagerPauseScheduleRequest{ScheduleId: input.ScheduleID}
}

func NewWorkflowManagerResumeScheduleRequest(input WorkflowManagerResumeScheduleInput) *proto.WorkflowManagerResumeScheduleRequest {
	return &proto.WorkflowManagerResumeScheduleRequest{ScheduleId: input.ScheduleID}
}

func NewWorkflowManagerCreateEventTriggerRequest(input WorkflowManagerCreateEventTriggerInput) (*proto.WorkflowManagerCreateEventTriggerRequest, error) {
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

func NewWorkflowManagerGetEventTriggerRequest(input WorkflowManagerGetEventTriggerInput) *proto.WorkflowManagerGetEventTriggerRequest {
	return &proto.WorkflowManagerGetEventTriggerRequest{TriggerId: input.TriggerID}
}

func NewWorkflowManagerUpdateEventTriggerRequest(input WorkflowManagerUpdateEventTriggerInput) (*proto.WorkflowManagerUpdateEventTriggerRequest, error) {
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

func NewWorkflowManagerDeleteEventTriggerRequest(input WorkflowManagerDeleteEventTriggerInput) *proto.WorkflowManagerDeleteEventTriggerRequest {
	return &proto.WorkflowManagerDeleteEventTriggerRequest{TriggerId: input.TriggerID}
}

func NewWorkflowManagerPauseEventTriggerRequest(input WorkflowManagerPauseEventTriggerInput) *proto.WorkflowManagerPauseEventTriggerRequest {
	return &proto.WorkflowManagerPauseEventTriggerRequest{TriggerId: input.TriggerID}
}

func NewWorkflowManagerResumeEventTriggerRequest(input WorkflowManagerResumeEventTriggerInput) *proto.WorkflowManagerResumeEventTriggerRequest {
	return &proto.WorkflowManagerResumeEventTriggerRequest{TriggerId: input.TriggerID}
}

func NewWorkflowManagerPublishEventRequest(input WorkflowManagerPublishEventInput) (*proto.WorkflowManagerPublishEventRequest, error) {
	event, err := newOptionalWorkflowEvent(input.Event)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowManagerPublishEventRequest{
		Event:        event,
		ProviderName: input.ProviderName,
	}, nil
}

func newOptionalWorkflowSignal(input *WorkflowSignalInput) (*WorkflowSignal, error) {
	if input == nil {
		return nil, nil
	}
	return NewWorkflowSignal(*input)
}

func newOptionalWorkflowEvent(input *WorkflowEventInput) (*WorkflowEvent, error) {
	if input == nil {
		return nil, nil
	}
	return NewWorkflowEvent(*input)
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
	var trigger *WorkflowRunTriggerInput
	if value.GetTrigger() != nil {
		input, err := WorkflowRunTriggerInputFromTrigger(value.GetTrigger())
		if err != nil {
			return nil, err
		}
		trigger = &input
	}
	return &WorkflowManagerBoundRun{
		ID:            value.GetId(),
		Status:        value.GetStatus(),
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
	var signal *WorkflowSignalInput
	if value.GetSignal() != nil {
		input := WorkflowSignalInputFromSignal(value.GetSignal())
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
