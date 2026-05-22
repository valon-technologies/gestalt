package gestalt

import proto "github.com/valon-technologies/gestalt/sdk/go/internal/gen/v1"

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

type WorkflowManagerBoundRun = BoundWorkflowRun

type WorkflowManagerBoundDefinition = BoundWorkflowDefinition

type WorkflowManagerBoundSchedule = BoundWorkflowSchedule

type WorkflowManagerBoundEventTrigger = BoundWorkflowEventTrigger

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

func newWorkflowManagerStartRunRequest(input WorkflowManagerStartRun) (*proto.StartWorkflowProviderRunRequest, error) {
	target, err := newOptionalBoundWorkflowTarget(input.Target)
	if err != nil {
		return nil, err
	}
	return &proto.StartWorkflowProviderRunRequest{
		ProviderName:   input.ProviderName,
		Target:         target,
		IdempotencyKey: input.IdempotencyKey,
		WorkflowKey:    input.WorkflowKey,
		DefinitionId:   input.DefinitionID,
	}, nil
}

func newWorkflowManagerSignalRunRequest(input WorkflowManagerSignalRun) (*proto.SignalWorkflowProviderRunRequest, error) {
	signal, err := newOptionalWorkflowSignal(input.Signal)
	if err != nil {
		return nil, err
	}
	return &proto.SignalWorkflowProviderRunRequest{
		RunId:  input.RunID,
		Signal: signal,
	}, nil
}

func newWorkflowManagerSignalOrStartRunRequest(input WorkflowManagerSignalOrStartRun) (*proto.SignalOrStartWorkflowProviderRunRequest, error) {
	target, err := newOptionalBoundWorkflowTarget(input.Target)
	if err != nil {
		return nil, err
	}
	signal, err := newOptionalWorkflowSignal(input.Signal)
	if err != nil {
		return nil, err
	}
	return &proto.SignalOrStartWorkflowProviderRunRequest{
		ProviderName:   input.ProviderName,
		WorkflowKey:    input.WorkflowKey,
		Target:         target,
		IdempotencyKey: input.IdempotencyKey,
		Signal:         signal,
		DefinitionId:   input.DefinitionID,
	}, nil
}

func newWorkflowManagerCreateDefinitionRequest(input WorkflowManagerCreateDefinition) (*proto.CreateWorkflowProviderDefinitionRequest, error) {
	target, err := newOptionalBoundWorkflowTarget(input.Target)
	if err != nil {
		return nil, err
	}
	return &proto.CreateWorkflowProviderDefinitionRequest{
		ProviderName:   input.ProviderName,
		Target:         target,
		IdempotencyKey: input.IdempotencyKey,
	}, nil
}

func newWorkflowManagerGetDefinitionRequest(input WorkflowManagerGetDefinition) *proto.GetWorkflowProviderDefinitionRequest {
	return &proto.GetWorkflowProviderDefinitionRequest{DefinitionId: input.DefinitionID}
}

func newWorkflowManagerUpdateDefinitionRequest(input WorkflowManagerUpdateDefinition) (*proto.UpdateWorkflowProviderDefinitionRequest, error) {
	target, err := newOptionalBoundWorkflowTarget(input.Target)
	if err != nil {
		return nil, err
	}
	return &proto.UpdateWorkflowProviderDefinitionRequest{
		DefinitionId: input.DefinitionID,
		ProviderName: input.ProviderName,
		Target:       target,
	}, nil
}

func newWorkflowManagerDeleteDefinitionRequest(input WorkflowManagerDeleteDefinition) *proto.DeleteWorkflowProviderDefinitionRequest {
	return &proto.DeleteWorkflowProviderDefinitionRequest{DefinitionId: input.DefinitionID}
}

func newWorkflowManagerCreateScheduleRequest(input WorkflowManagerCreateSchedule) (*proto.UpsertWorkflowProviderScheduleRequest, error) {
	target, err := newOptionalBoundWorkflowTarget(input.Target)
	if err != nil {
		return nil, err
	}
	return &proto.UpsertWorkflowProviderScheduleRequest{
		ProviderName:   input.ProviderName,
		Cron:           input.Cron,
		Timezone:       input.Timezone,
		Target:         target,
		Paused:         input.Paused,
		IdempotencyKey: input.IdempotencyKey,
		DefinitionId:   input.DefinitionID,
	}, nil
}

func newWorkflowManagerGetScheduleRequest(input WorkflowManagerGetSchedule) *proto.GetWorkflowProviderScheduleRequest {
	return &proto.GetWorkflowProviderScheduleRequest{ScheduleId: input.ScheduleID}
}

func newWorkflowManagerUpdateScheduleRequest(input WorkflowManagerUpdateSchedule) (*proto.UpsertWorkflowProviderScheduleRequest, error) {
	target, err := newOptionalBoundWorkflowTarget(input.Target)
	if err != nil {
		return nil, err
	}
	return &proto.UpsertWorkflowProviderScheduleRequest{
		ScheduleId:   input.ScheduleID,
		ProviderName: input.ProviderName,
		Cron:         input.Cron,
		Timezone:     input.Timezone,
		Target:       target,
		Paused:       input.Paused,
		DefinitionId: input.DefinitionID,
	}, nil
}

func newWorkflowManagerDeleteScheduleRequest(input WorkflowManagerDeleteSchedule) *proto.DeleteWorkflowProviderScheduleRequest {
	return &proto.DeleteWorkflowProviderScheduleRequest{ScheduleId: input.ScheduleID}
}

func newWorkflowManagerPauseScheduleRequest(input WorkflowManagerPauseSchedule) *proto.PauseWorkflowProviderScheduleRequest {
	return &proto.PauseWorkflowProviderScheduleRequest{ScheduleId: input.ScheduleID}
}

func newWorkflowManagerResumeScheduleRequest(input WorkflowManagerResumeSchedule) *proto.ResumeWorkflowProviderScheduleRequest {
	return &proto.ResumeWorkflowProviderScheduleRequest{ScheduleId: input.ScheduleID}
}

func newWorkflowManagerCreateEventTriggerRequest(input WorkflowManagerCreateEventTrigger) (*proto.UpsertWorkflowProviderEventTriggerRequest, error) {
	target, err := newOptionalBoundWorkflowTarget(input.Target)
	if err != nil {
		return nil, err
	}
	return &proto.UpsertWorkflowProviderEventTriggerRequest{
		ProviderName:   input.ProviderName,
		Match:          workflowEventMatchFromInput(input.Match),
		Target:         target,
		Paused:         input.Paused,
		IdempotencyKey: input.IdempotencyKey,
		DefinitionId:   input.DefinitionID,
	}, nil
}

func newWorkflowManagerGetEventTriggerRequest(input WorkflowManagerGetEventTrigger) *proto.GetWorkflowProviderEventTriggerRequest {
	return &proto.GetWorkflowProviderEventTriggerRequest{TriggerId: input.TriggerID}
}

func newWorkflowManagerUpdateEventTriggerRequest(input WorkflowManagerUpdateEventTrigger) (*proto.UpsertWorkflowProviderEventTriggerRequest, error) {
	target, err := newOptionalBoundWorkflowTarget(input.Target)
	if err != nil {
		return nil, err
	}
	return &proto.UpsertWorkflowProviderEventTriggerRequest{
		TriggerId:    input.TriggerID,
		ProviderName: input.ProviderName,
		Match:        workflowEventMatchFromInput(input.Match),
		Target:       target,
		Paused:       input.Paused,
		DefinitionId: input.DefinitionID,
	}, nil
}

func newWorkflowManagerDeleteEventTriggerRequest(input WorkflowManagerDeleteEventTrigger) *proto.DeleteWorkflowProviderEventTriggerRequest {
	return &proto.DeleteWorkflowProviderEventTriggerRequest{TriggerId: input.TriggerID}
}

func newWorkflowManagerPauseEventTriggerRequest(input WorkflowManagerPauseEventTrigger) *proto.PauseWorkflowProviderEventTriggerRequest {
	return &proto.PauseWorkflowProviderEventTriggerRequest{TriggerId: input.TriggerID}
}

func newWorkflowManagerResumeEventTriggerRequest(input WorkflowManagerResumeEventTrigger) *proto.ResumeWorkflowProviderEventTriggerRequest {
	return &proto.ResumeWorkflowProviderEventTriggerRequest{TriggerId: input.TriggerID}
}

func newWorkflowManagerPublishEventRequest(input WorkflowManagerPublishEvent) (*proto.PublishWorkflowProviderEventRequest, error) {
	event, err := newOptionalWorkflowEvent(input.Event)
	if err != nil {
		return nil, err
	}
	return &proto.PublishWorkflowProviderEventRequest{
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

func workflowManagerRunFromProto(value *proto.BoundWorkflowRun) (*WorkflowManagerRun, error) {
	if value == nil {
		return nil, nil
	}
	run, err := boundWorkflowRunFromProto(value)
	if err != nil {
		return nil, err
	}
	return &WorkflowManagerRun{ProviderName: value.GetProviderName(), Run: &run}, nil
}

func workflowManagerRunSignalFromProto(value *proto.SignalWorkflowRunResponse) (*WorkflowManagerRunSignal, error) {
	if value == nil {
		return nil, nil
	}
	var run *WorkflowManagerBoundRun
	if value.GetRun() != nil {
		input, err := boundWorkflowRunFromProto(value.GetRun())
		if err != nil {
			return nil, err
		}
		run = &input
	}
	var signal *WorkflowSignal
	if value.GetSignal() != nil {
		input := workflowSignalFromProto(value.GetSignal())
		signal = &input
	}
	return &WorkflowManagerRunSignal{
		ProviderName: value.GetRun().GetProviderName(),
		Run:          run,
		Signal:       signal,
		StartedRun:   value.GetStartedRun(),
		WorkflowKey:  value.GetWorkflowKey(),
	}, nil
}

func workflowManagerDefinitionFromProto(value *proto.BoundWorkflowDefinition) (*WorkflowManagerDefinition, error) {
	if value == nil {
		return nil, nil
	}
	definition, err := boundWorkflowDefinitionFromProto(value)
	if err != nil {
		return nil, err
	}
	return &WorkflowManagerDefinition{
		ProviderName: value.GetProviderName(),
		Definition:   &definition,
	}, nil
}

func workflowManagerScheduleFromProto(value *proto.BoundWorkflowSchedule) (*WorkflowManagerSchedule, error) {
	if value == nil {
		return nil, nil
	}
	schedule, err := boundWorkflowScheduleFromProto(value)
	if err != nil {
		return nil, err
	}
	return &WorkflowManagerSchedule{ProviderName: value.GetProviderName(), Schedule: &schedule}, nil
}

func workflowManagerEventTriggerFromProto(value *proto.BoundWorkflowEventTrigger) (*WorkflowManagerEventTrigger, error) {
	if value == nil {
		return nil, nil
	}
	trigger, err := boundWorkflowEventTriggerFromProto(value)
	if err != nil {
		return nil, err
	}
	return &WorkflowManagerEventTrigger{
		ProviderName: value.GetProviderName(),
		Trigger:      &trigger,
	}, nil
}
