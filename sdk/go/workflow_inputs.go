package gestalt

import proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"

type WorkflowStartRun struct {
	ProviderName   string
	Target         *BoundWorkflowTarget
	IdempotencyKey string
	WorkflowKey    string
	DefinitionID   string
	RunAs          *Subject
}

type WorkflowSignalRun struct {
	RunID  string
	Signal *WorkflowSignal
}

type WorkflowSignalOrStartRun struct {
	ProviderName   string
	WorkflowKey    string
	Target         *BoundWorkflowTarget
	IdempotencyKey string
	Signal         *WorkflowSignal
	DefinitionID   string
	RunAs          *Subject
}

type WorkflowCreateDefinition struct {
	ProviderName   string
	Target         *BoundWorkflowTarget
	IdempotencyKey string
}

type WorkflowGetDefinition struct {
	DefinitionID string
}

type WorkflowUpdateDefinition struct {
	DefinitionID string
	ProviderName string
	Target       *BoundWorkflowTarget
}

type WorkflowDeleteDefinition struct {
	DefinitionID string
}

type WorkflowCreateSchedule struct {
	ProviderName   string
	Cron           string
	Timezone       string
	Target         *BoundWorkflowTarget
	Paused         bool
	IdempotencyKey string
	DefinitionID   string
	RunAs          *Subject
}

type WorkflowGetSchedule struct {
	ScheduleID string
}

type WorkflowUpdateSchedule struct {
	ScheduleID   string
	ProviderName string
	Cron         string
	Timezone     string
	Target       *BoundWorkflowTarget
	Paused       bool
	DefinitionID string
	RunAs        *Subject
}

type WorkflowDeleteSchedule struct {
	ScheduleID string
}

type WorkflowPauseSchedule struct {
	ScheduleID string
}

type WorkflowResumeSchedule struct {
	ScheduleID string
}

type WorkflowCreateEventTrigger struct {
	ProviderName   string
	Match          *WorkflowEventMatch
	Target         *BoundWorkflowTarget
	Paused         bool
	IdempotencyKey string
	DefinitionID   string
	RunAs          *Subject
}

type WorkflowGetEventTrigger struct {
	TriggerID string
}

type WorkflowUpdateEventTrigger struct {
	TriggerID    string
	ProviderName string
	Match        *WorkflowEventMatch
	Target       *BoundWorkflowTarget
	Paused       bool
	DefinitionID string
	RunAs        *Subject
}

type WorkflowDeleteEventTrigger struct {
	TriggerID string
}

type WorkflowPauseEventTrigger struct {
	TriggerID string
}

type WorkflowResumeEventTrigger struct {
	TriggerID string
}

type WorkflowPublishEvent struct {
	ProviderName string
	Event        *WorkflowEvent
}

type WorkflowRun struct {
	ProviderName string
	Run          *BoundWorkflowRun
}

type WorkflowRunSignal struct {
	ProviderName string
	Run          *BoundWorkflowRun
	Signal       *WorkflowSignal
	StartedRun   bool
	WorkflowKey  string
}

type WorkflowDefinition struct {
	ProviderName string
	Definition   *BoundWorkflowDefinition
}

type WorkflowSchedule struct {
	ProviderName string
	Schedule     *BoundWorkflowSchedule
}

type WorkflowEventTrigger struct {
	ProviderName string
	Trigger      *BoundWorkflowEventTrigger
}

func newWorkflowStartRunRequest(input WorkflowStartRun) (*proto.StartWorkflowProviderRunRequest, error) {
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
		RunAs:          subjectToProto(input.RunAs),
	}, nil
}

func newWorkflowSignalRunRequest(input WorkflowSignalRun) (*proto.SignalWorkflowProviderRunRequest, error) {
	signal, err := newOptionalWorkflowSignal(input.Signal)
	if err != nil {
		return nil, err
	}
	return &proto.SignalWorkflowProviderRunRequest{
		RunId:  input.RunID,
		Signal: signal,
	}, nil
}

func newWorkflowSignalOrStartRunRequest(input WorkflowSignalOrStartRun) (*proto.SignalOrStartWorkflowProviderRunRequest, error) {
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
		RunAs:          subjectToProto(input.RunAs),
	}, nil
}

func newWorkflowCreateDefinitionRequest(input WorkflowCreateDefinition) (*proto.CreateWorkflowProviderDefinitionRequest, error) {
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

func newWorkflowGetDefinitionRequest(input WorkflowGetDefinition) *proto.GetWorkflowProviderDefinitionRequest {
	return &proto.GetWorkflowProviderDefinitionRequest{DefinitionId: input.DefinitionID}
}

func newWorkflowUpdateDefinitionRequest(input WorkflowUpdateDefinition) (*proto.UpdateWorkflowProviderDefinitionRequest, error) {
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

func newWorkflowDeleteDefinitionRequest(input WorkflowDeleteDefinition) *proto.DeleteWorkflowProviderDefinitionRequest {
	return &proto.DeleteWorkflowProviderDefinitionRequest{DefinitionId: input.DefinitionID}
}

func newWorkflowCreateScheduleRequest(input WorkflowCreateSchedule) (*proto.UpsertWorkflowProviderScheduleRequest, error) {
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
		RunAs:          subjectToProto(input.RunAs),
	}, nil
}

func newWorkflowGetScheduleRequest(input WorkflowGetSchedule) *proto.GetWorkflowProviderScheduleRequest {
	return &proto.GetWorkflowProviderScheduleRequest{ScheduleId: input.ScheduleID}
}

func newWorkflowUpdateScheduleRequest(input WorkflowUpdateSchedule) (*proto.UpsertWorkflowProviderScheduleRequest, error) {
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
		RunAs:        subjectToProto(input.RunAs),
	}, nil
}

func newWorkflowDeleteScheduleRequest(input WorkflowDeleteSchedule) *proto.DeleteWorkflowProviderScheduleRequest {
	return &proto.DeleteWorkflowProviderScheduleRequest{ScheduleId: input.ScheduleID}
}

func newWorkflowPauseScheduleRequest(input WorkflowPauseSchedule) *proto.PauseWorkflowProviderScheduleRequest {
	return &proto.PauseWorkflowProviderScheduleRequest{ScheduleId: input.ScheduleID}
}

func newWorkflowResumeScheduleRequest(input WorkflowResumeSchedule) *proto.ResumeWorkflowProviderScheduleRequest {
	return &proto.ResumeWorkflowProviderScheduleRequest{ScheduleId: input.ScheduleID}
}

func newWorkflowCreateEventTriggerRequest(input WorkflowCreateEventTrigger) (*proto.UpsertWorkflowProviderEventTriggerRequest, error) {
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
		RunAs:          subjectToProto(input.RunAs),
	}, nil
}

func newWorkflowGetEventTriggerRequest(input WorkflowGetEventTrigger) *proto.GetWorkflowProviderEventTriggerRequest {
	return &proto.GetWorkflowProviderEventTriggerRequest{TriggerId: input.TriggerID}
}

func newWorkflowUpdateEventTriggerRequest(input WorkflowUpdateEventTrigger) (*proto.UpsertWorkflowProviderEventTriggerRequest, error) {
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
		RunAs:        subjectToProto(input.RunAs),
	}, nil
}

func newWorkflowDeleteEventTriggerRequest(input WorkflowDeleteEventTrigger) *proto.DeleteWorkflowProviderEventTriggerRequest {
	return &proto.DeleteWorkflowProviderEventTriggerRequest{TriggerId: input.TriggerID}
}

func newWorkflowPauseEventTriggerRequest(input WorkflowPauseEventTrigger) *proto.PauseWorkflowProviderEventTriggerRequest {
	return &proto.PauseWorkflowProviderEventTriggerRequest{TriggerId: input.TriggerID}
}

func newWorkflowResumeEventTriggerRequest(input WorkflowResumeEventTrigger) *proto.ResumeWorkflowProviderEventTriggerRequest {
	return &proto.ResumeWorkflowProviderEventTriggerRequest{TriggerId: input.TriggerID}
}

func newWorkflowPublishEventRequest(input WorkflowPublishEvent) (*proto.PublishWorkflowProviderEventRequest, error) {
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

func workflowRunFromProto(value *proto.BoundWorkflowRun) (*WorkflowRun, error) {
	if value == nil {
		return nil, nil
	}
	run, err := boundWorkflowRunFromProto(value)
	if err != nil {
		return nil, err
	}
	return &WorkflowRun{ProviderName: value.GetProviderName(), Run: &run}, nil
}

func workflowRunSignalFromProto(value *proto.SignalWorkflowRunResponse) (*WorkflowRunSignal, error) {
	if value == nil {
		return nil, nil
	}
	var run *BoundWorkflowRun
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
	return &WorkflowRunSignal{
		ProviderName: value.GetRun().GetProviderName(),
		Run:          run,
		Signal:       signal,
		StartedRun:   value.GetStartedRun(),
		WorkflowKey:  value.GetWorkflowKey(),
	}, nil
}

func workflowDefinitionFromProto(value *proto.BoundWorkflowDefinition) (*WorkflowDefinition, error) {
	if value == nil {
		return nil, nil
	}
	definition, err := boundWorkflowDefinitionFromProto(value)
	if err != nil {
		return nil, err
	}
	return &WorkflowDefinition{
		ProviderName: value.GetProviderName(),
		Definition:   &definition,
	}, nil
}

func workflowScheduleFromProto(value *proto.BoundWorkflowSchedule) (*WorkflowSchedule, error) {
	if value == nil {
		return nil, nil
	}
	schedule, err := boundWorkflowScheduleFromProto(value)
	if err != nil {
		return nil, err
	}
	return &WorkflowSchedule{ProviderName: value.GetProviderName(), Schedule: &schedule}, nil
}

func workflowEventTriggerFromProto(value *proto.BoundWorkflowEventTrigger) (*WorkflowEventTrigger, error) {
	if value == nil {
		return nil, nil
	}
	trigger, err := boundWorkflowEventTriggerFromProto(value)
	if err != nil {
		return nil, err
	}
	return &WorkflowEventTrigger{
		ProviderName: value.GetProviderName(),
		Trigger:      &trigger,
	}, nil
}
