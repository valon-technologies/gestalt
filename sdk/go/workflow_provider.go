package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
)

// WorkflowProvider is implemented by providers that serve the workflow base
// primitive. The SDK owns the gRPC/protobuf transport adapter; provider code
// implements this typed interface instead of importing generated protobuf
// service bindings.
type WorkflowProvider interface {
	Provider
	StartRun(ctx context.Context, req *StartWorkflowProviderRunRequest) (*BoundWorkflowRun, error)
	GetRun(ctx context.Context, req *GetWorkflowProviderRunRequest) (*BoundWorkflowRun, error)
	ListRuns(ctx context.Context, req *ListWorkflowProviderRunsRequest) (*ListWorkflowProviderRunsResponse, error)
	CancelRun(ctx context.Context, req *CancelWorkflowProviderRunRequest) (*BoundWorkflowRun, error)
	SignalRun(ctx context.Context, req *SignalWorkflowProviderRunRequest) (*SignalWorkflowRunResponse, error)
	SignalOrStartRun(ctx context.Context, req *SignalOrStartWorkflowProviderRunRequest) (*SignalWorkflowRunResponse, error)
	UpsertSchedule(ctx context.Context, req *UpsertWorkflowProviderScheduleRequest) (*BoundWorkflowSchedule, error)
	GetSchedule(ctx context.Context, req *GetWorkflowProviderScheduleRequest) (*BoundWorkflowSchedule, error)
	ListSchedules(ctx context.Context, req *ListWorkflowProviderSchedulesRequest) (*ListWorkflowProviderSchedulesResponse, error)
	DeleteSchedule(ctx context.Context, req *DeleteWorkflowProviderScheduleRequest) error
	PauseSchedule(ctx context.Context, req *PauseWorkflowProviderScheduleRequest) (*BoundWorkflowSchedule, error)
	ResumeSchedule(ctx context.Context, req *ResumeWorkflowProviderScheduleRequest) (*BoundWorkflowSchedule, error)
	UpsertEventTrigger(ctx context.Context, req *UpsertWorkflowProviderEventTriggerRequest) (*BoundWorkflowEventTrigger, error)
	GetEventTrigger(ctx context.Context, req *GetWorkflowProviderEventTriggerRequest) (*BoundWorkflowEventTrigger, error)
	ListEventTriggers(ctx context.Context, req *ListWorkflowProviderEventTriggersRequest) (*ListWorkflowProviderEventTriggersResponse, error)
	DeleteEventTrigger(ctx context.Context, req *DeleteWorkflowProviderEventTriggerRequest) error
	PauseEventTrigger(ctx context.Context, req *PauseWorkflowProviderEventTriggerRequest) (*BoundWorkflowEventTrigger, error)
	ResumeEventTrigger(ctx context.Context, req *ResumeWorkflowProviderEventTriggerRequest) (*BoundWorkflowEventTrigger, error)
	PutExecutionReference(ctx context.Context, req *PutWorkflowExecutionReferenceRequest) (*WorkflowExecutionReference, error)
	GetExecutionReference(ctx context.Context, req *GetWorkflowExecutionReferenceRequest) (*WorkflowExecutionReference, error)
	ListExecutionReferences(ctx context.Context, req *ListWorkflowExecutionReferencesRequest) (*ListWorkflowExecutionReferencesResponse, error)
	PublishEvent(ctx context.Context, req *PublishWorkflowProviderEventRequest) error
}

// UnimplementedWorkflowProvider provides no-op lifecycle behavior and
// unimplemented workflow operations. Embed it when a provider implements only
// part of the workflow surface.
type UnimplementedWorkflowProvider struct{}

func (UnimplementedWorkflowProvider) Configure(context.Context, string, map[string]any) error {
	return nil
}

func (UnimplementedWorkflowProvider) StartRun(context.Context, *StartWorkflowProviderRunRequest) (*BoundWorkflowRun, error) {
	return nil, Unimplemented("workflow start run is not implemented")
}

func (UnimplementedWorkflowProvider) GetRun(context.Context, *GetWorkflowProviderRunRequest) (*BoundWorkflowRun, error) {
	return nil, Unimplemented("workflow get run is not implemented")
}

func (UnimplementedWorkflowProvider) ListRuns(context.Context, *ListWorkflowProviderRunsRequest) (*ListWorkflowProviderRunsResponse, error) {
	return nil, Unimplemented("workflow list runs is not implemented")
}

func (UnimplementedWorkflowProvider) CancelRun(context.Context, *CancelWorkflowProviderRunRequest) (*BoundWorkflowRun, error) {
	return nil, Unimplemented("workflow cancel run is not implemented")
}

func (UnimplementedWorkflowProvider) SignalRun(context.Context, *SignalWorkflowProviderRunRequest) (*SignalWorkflowRunResponse, error) {
	return nil, Unimplemented("workflow signal run is not implemented")
}

func (UnimplementedWorkflowProvider) SignalOrStartRun(context.Context, *SignalOrStartWorkflowProviderRunRequest) (*SignalWorkflowRunResponse, error) {
	return nil, Unimplemented("workflow signal or start run is not implemented")
}

func (UnimplementedWorkflowProvider) UpsertSchedule(context.Context, *UpsertWorkflowProviderScheduleRequest) (*BoundWorkflowSchedule, error) {
	return nil, Unimplemented("workflow upsert schedule is not implemented")
}

func (UnimplementedWorkflowProvider) GetSchedule(context.Context, *GetWorkflowProviderScheduleRequest) (*BoundWorkflowSchedule, error) {
	return nil, Unimplemented("workflow get schedule is not implemented")
}

func (UnimplementedWorkflowProvider) ListSchedules(context.Context, *ListWorkflowProviderSchedulesRequest) (*ListWorkflowProviderSchedulesResponse, error) {
	return nil, Unimplemented("workflow list schedules is not implemented")
}

func (UnimplementedWorkflowProvider) DeleteSchedule(context.Context, *DeleteWorkflowProviderScheduleRequest) error {
	return Unimplemented("workflow delete schedule is not implemented")
}

func (UnimplementedWorkflowProvider) PauseSchedule(context.Context, *PauseWorkflowProviderScheduleRequest) (*BoundWorkflowSchedule, error) {
	return nil, Unimplemented("workflow pause schedule is not implemented")
}

func (UnimplementedWorkflowProvider) ResumeSchedule(context.Context, *ResumeWorkflowProviderScheduleRequest) (*BoundWorkflowSchedule, error) {
	return nil, Unimplemented("workflow resume schedule is not implemented")
}

func (UnimplementedWorkflowProvider) UpsertEventTrigger(context.Context, *UpsertWorkflowProviderEventTriggerRequest) (*BoundWorkflowEventTrigger, error) {
	return nil, Unimplemented("workflow upsert event trigger is not implemented")
}

func (UnimplementedWorkflowProvider) GetEventTrigger(context.Context, *GetWorkflowProviderEventTriggerRequest) (*BoundWorkflowEventTrigger, error) {
	return nil, Unimplemented("workflow get event trigger is not implemented")
}

func (UnimplementedWorkflowProvider) ListEventTriggers(context.Context, *ListWorkflowProviderEventTriggersRequest) (*ListWorkflowProviderEventTriggersResponse, error) {
	return nil, Unimplemented("workflow list event triggers is not implemented")
}

func (UnimplementedWorkflowProvider) DeleteEventTrigger(context.Context, *DeleteWorkflowProviderEventTriggerRequest) error {
	return Unimplemented("workflow delete event trigger is not implemented")
}

func (UnimplementedWorkflowProvider) PauseEventTrigger(context.Context, *PauseWorkflowProviderEventTriggerRequest) (*BoundWorkflowEventTrigger, error) {
	return nil, Unimplemented("workflow pause event trigger is not implemented")
}

func (UnimplementedWorkflowProvider) ResumeEventTrigger(context.Context, *ResumeWorkflowProviderEventTriggerRequest) (*BoundWorkflowEventTrigger, error) {
	return nil, Unimplemented("workflow resume event trigger is not implemented")
}

func (UnimplementedWorkflowProvider) PutExecutionReference(context.Context, *PutWorkflowExecutionReferenceRequest) (*WorkflowExecutionReference, error) {
	return nil, Unimplemented("workflow put execution reference is not implemented")
}

func (UnimplementedWorkflowProvider) GetExecutionReference(context.Context, *GetWorkflowExecutionReferenceRequest) (*WorkflowExecutionReference, error) {
	return nil, Unimplemented("workflow get execution reference is not implemented")
}

func (UnimplementedWorkflowProvider) ListExecutionReferences(context.Context, *ListWorkflowExecutionReferencesRequest) (*ListWorkflowExecutionReferencesResponse, error) {
	return nil, Unimplemented("workflow list execution references is not implemented")
}

func (UnimplementedWorkflowProvider) PublishEvent(context.Context, *PublishWorkflowProviderEventRequest) error {
	return Unimplemented("workflow publish event is not implemented")
}

type (
	BoundWorkflowTarget                       = proto.BoundWorkflowTarget
	BoundWorkflowTargetPlugin                 = proto.BoundWorkflowTarget_Plugin
	BoundWorkflowTargetAgent                  = proto.BoundWorkflowTarget_Agent
	BoundWorkflowPluginTarget                 = proto.BoundWorkflowPluginTarget
	BoundWorkflowAgentTarget                  = proto.BoundWorkflowAgentTarget
	WorkflowOutputDelivery                    = proto.WorkflowOutputDelivery
	WorkflowOutputBinding                     = proto.WorkflowOutputBinding
	WorkflowOutputValueSource                 = proto.WorkflowOutputValueSource
	WorkflowOutputValueSourceAgentOutput      = proto.WorkflowOutputValueSource_AgentOutput
	WorkflowOutputValueSourceSignalPayload    = proto.WorkflowOutputValueSource_SignalPayload
	WorkflowOutputValueSourceSignalMetadata   = proto.WorkflowOutputValueSource_SignalMetadata
	WorkflowOutputValueSourceLiteral          = proto.WorkflowOutputValueSource_Literal
	WorkflowActor                             = proto.WorkflowActor
	WorkflowEvent                             = proto.WorkflowEvent
	WorkflowEventMatch                        = proto.WorkflowEventMatch
	WorkflowManualTrigger                     = proto.WorkflowManualTrigger
	WorkflowScheduleTrigger                   = proto.WorkflowScheduleTrigger
	WorkflowEventTriggerInvocation            = proto.WorkflowEventTriggerInvocation
	WorkflowRunTrigger                        = proto.WorkflowRunTrigger
	WorkflowRunTriggerManual                  = proto.WorkflowRunTrigger_Manual
	WorkflowRunTriggerSchedule                = proto.WorkflowRunTrigger_Schedule
	WorkflowRunTriggerEvent                   = proto.WorkflowRunTrigger_Event
	BoundWorkflowRun                          = proto.BoundWorkflowRun
	BoundWorkflowSchedule                     = proto.BoundWorkflowSchedule
	BoundWorkflowEventTrigger                 = proto.BoundWorkflowEventTrigger
	WorkflowAccessPermission                  = proto.WorkflowAccessPermission
	WorkflowExecutionReference                = proto.WorkflowExecutionReference
	WorkflowSignal                            = proto.WorkflowSignal
	StartWorkflowProviderRunRequest           = proto.StartWorkflowProviderRunRequest
	GetWorkflowProviderRunRequest             = proto.GetWorkflowProviderRunRequest
	ListWorkflowProviderRunsRequest           = proto.ListWorkflowProviderRunsRequest
	ListWorkflowProviderRunsResponse          = proto.ListWorkflowProviderRunsResponse
	CancelWorkflowProviderRunRequest          = proto.CancelWorkflowProviderRunRequest
	SignalWorkflowProviderRunRequest          = proto.SignalWorkflowProviderRunRequest
	SignalOrStartWorkflowProviderRunRequest   = proto.SignalOrStartWorkflowProviderRunRequest
	SignalWorkflowRunResponse                 = proto.SignalWorkflowRunResponse
	UpsertWorkflowProviderScheduleRequest     = proto.UpsertWorkflowProviderScheduleRequest
	GetWorkflowProviderScheduleRequest        = proto.GetWorkflowProviderScheduleRequest
	ListWorkflowProviderSchedulesRequest      = proto.ListWorkflowProviderSchedulesRequest
	ListWorkflowProviderSchedulesResponse     = proto.ListWorkflowProviderSchedulesResponse
	DeleteWorkflowProviderScheduleRequest     = proto.DeleteWorkflowProviderScheduleRequest
	PauseWorkflowProviderScheduleRequest      = proto.PauseWorkflowProviderScheduleRequest
	ResumeWorkflowProviderScheduleRequest     = proto.ResumeWorkflowProviderScheduleRequest
	UpsertWorkflowProviderEventTriggerRequest = proto.UpsertWorkflowProviderEventTriggerRequest
	GetWorkflowProviderEventTriggerRequest    = proto.GetWorkflowProviderEventTriggerRequest
	ListWorkflowProviderEventTriggersRequest  = proto.ListWorkflowProviderEventTriggersRequest
	ListWorkflowProviderEventTriggersResponse = proto.ListWorkflowProviderEventTriggersResponse
	PutWorkflowExecutionReferenceRequest      = proto.PutWorkflowExecutionReferenceRequest
	GetWorkflowExecutionReferenceRequest      = proto.GetWorkflowExecutionReferenceRequest
	ListWorkflowExecutionReferencesRequest    = proto.ListWorkflowExecutionReferencesRequest
	ListWorkflowExecutionReferencesResponse   = proto.ListWorkflowExecutionReferencesResponse
	DeleteWorkflowProviderEventTriggerRequest = proto.DeleteWorkflowProviderEventTriggerRequest
	PauseWorkflowProviderEventTriggerRequest  = proto.PauseWorkflowProviderEventTriggerRequest
	ResumeWorkflowProviderEventTriggerRequest = proto.ResumeWorkflowProviderEventTriggerRequest
	PublishWorkflowProviderEventRequest       = proto.PublishWorkflowProviderEventRequest
	InvokeWorkflowOperationRequest            = proto.InvokeWorkflowOperationRequest
	InvokeWorkflowOperationResponse           = proto.InvokeWorkflowOperationResponse
	ManagedWorkflowSchedule                   = proto.ManagedWorkflowSchedule
	ManagedWorkflowEventTrigger               = proto.ManagedWorkflowEventTrigger
	ManagedWorkflowRun                        = proto.ManagedWorkflowRun
	ManagedWorkflowRunSignal                  = proto.ManagedWorkflowRunSignal
	WorkflowManagerCreateScheduleRequest      = proto.WorkflowManagerCreateScheduleRequest
	WorkflowManagerGetScheduleRequest         = proto.WorkflowManagerGetScheduleRequest
	WorkflowManagerUpdateScheduleRequest      = proto.WorkflowManagerUpdateScheduleRequest
	WorkflowManagerDeleteScheduleRequest      = proto.WorkflowManagerDeleteScheduleRequest
	WorkflowManagerPauseScheduleRequest       = proto.WorkflowManagerPauseScheduleRequest
	WorkflowManagerResumeScheduleRequest      = proto.WorkflowManagerResumeScheduleRequest
	WorkflowManagerCreateEventTriggerRequest  = proto.WorkflowManagerCreateEventTriggerRequest
	WorkflowManagerGetEventTriggerRequest     = proto.WorkflowManagerGetEventTriggerRequest
	WorkflowManagerUpdateEventTriggerRequest  = proto.WorkflowManagerUpdateEventTriggerRequest
	WorkflowManagerDeleteEventTriggerRequest  = proto.WorkflowManagerDeleteEventTriggerRequest
	WorkflowManagerPauseEventTriggerRequest   = proto.WorkflowManagerPauseEventTriggerRequest
	WorkflowManagerResumeEventTriggerRequest  = proto.WorkflowManagerResumeEventTriggerRequest
	WorkflowManagerPublishEventRequest        = proto.WorkflowManagerPublishEventRequest
	WorkflowManagerStartRunRequest            = proto.WorkflowManagerStartRunRequest
	WorkflowManagerSignalRunRequest           = proto.WorkflowManagerSignalRunRequest
	WorkflowManagerSignalOrStartRunRequest    = proto.WorkflowManagerSignalOrStartRunRequest
)

type WorkflowRunStatus = proto.WorkflowRunStatus

const (
	WorkflowRunStatusUnspecified = proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_UNSPECIFIED
	WorkflowRunStatusPending     = proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_PENDING
	WorkflowRunStatusRunning     = proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_RUNNING
	WorkflowRunStatusSucceeded   = proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_SUCCEEDED
	WorkflowRunStatusFailed      = proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_FAILED
	WorkflowRunStatusCanceled    = proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_CANCELED
)
