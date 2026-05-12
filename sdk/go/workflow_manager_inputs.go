package gestalt

import proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"

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
