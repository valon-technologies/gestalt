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
	// StartRun starts or idempotently returns a workflow run.
	StartRun(ctx context.Context, req *StartWorkflowProviderRunRequest) (*BoundWorkflowRun, error)
	// GetRun returns one workflow run by ID.
	GetRun(ctx context.Context, req *GetWorkflowProviderRunRequest) (*BoundWorkflowRun, error)
	// ListRuns returns workflow runs visible to the request subject.
	ListRuns(ctx context.Context, req *ListWorkflowProviderRunsRequest) (*ListWorkflowProviderRunsResponse, error)
	// CancelRun requests cancellation of a pending or running workflow run.
	CancelRun(ctx context.Context, req *CancelWorkflowProviderRunRequest) (*BoundWorkflowRun, error)
	// SignalRun delivers a signal to an existing workflow run.
	SignalRun(ctx context.Context, req *SignalWorkflowProviderRunRequest) (*SignalWorkflowRunResponse, error)
	// SignalOrStartRun delivers a signal or starts a run when no target run exists.
	SignalOrStartRun(ctx context.Context, req *SignalOrStartWorkflowProviderRunRequest) (*SignalWorkflowRunResponse, error)
	// UpsertSchedule creates or updates a workflow schedule.
	UpsertSchedule(ctx context.Context, req *UpsertWorkflowProviderScheduleRequest) (*BoundWorkflowSchedule, error)
	// GetSchedule returns one workflow schedule by ID.
	GetSchedule(ctx context.Context, req *GetWorkflowProviderScheduleRequest) (*BoundWorkflowSchedule, error)
	// ListSchedules returns workflow schedules visible to the request subject.
	ListSchedules(ctx context.Context, req *ListWorkflowProviderSchedulesRequest) (*ListWorkflowProviderSchedulesResponse, error)
	// DeleteSchedule deletes a workflow schedule.
	DeleteSchedule(ctx context.Context, req *DeleteWorkflowProviderScheduleRequest) error
	// PauseSchedule pauses a workflow schedule without deleting it.
	PauseSchedule(ctx context.Context, req *PauseWorkflowProviderScheduleRequest) (*BoundWorkflowSchedule, error)
	// ResumeSchedule resumes a paused workflow schedule.
	ResumeSchedule(ctx context.Context, req *ResumeWorkflowProviderScheduleRequest) (*BoundWorkflowSchedule, error)
	// UpsertEventTrigger creates or updates a workflow event trigger.
	UpsertEventTrigger(ctx context.Context, req *UpsertWorkflowProviderEventTriggerRequest) (*BoundWorkflowEventTrigger, error)
	// GetEventTrigger returns one workflow event trigger by ID.
	GetEventTrigger(ctx context.Context, req *GetWorkflowProviderEventTriggerRequest) (*BoundWorkflowEventTrigger, error)
	// ListEventTriggers returns workflow event triggers visible to the request subject.
	ListEventTriggers(ctx context.Context, req *ListWorkflowProviderEventTriggersRequest) (*ListWorkflowProviderEventTriggersResponse, error)
	// DeleteEventTrigger deletes a workflow event trigger.
	DeleteEventTrigger(ctx context.Context, req *DeleteWorkflowProviderEventTriggerRequest) error
	// PauseEventTrigger pauses a workflow event trigger without deleting it.
	PauseEventTrigger(ctx context.Context, req *PauseWorkflowProviderEventTriggerRequest) (*BoundWorkflowEventTrigger, error)
	// ResumeEventTrigger resumes a paused workflow event trigger.
	ResumeEventTrigger(ctx context.Context, req *ResumeWorkflowProviderEventTriggerRequest) (*BoundWorkflowEventTrigger, error)
	// PutExecutionReference stores or updates a workflow execution reference.
	PutExecutionReference(ctx context.Context, req *PutWorkflowExecutionReferenceRequest) (*WorkflowExecutionReference, error)
	// GetExecutionReference returns one workflow execution reference.
	GetExecutionReference(ctx context.Context, req *GetWorkflowExecutionReferenceRequest) (*WorkflowExecutionReference, error)
	// ListExecutionReferences returns workflow execution references for a scope.
	ListExecutionReferences(ctx context.Context, req *ListWorkflowExecutionReferencesRequest) (*ListWorkflowExecutionReferencesResponse, error)
	// PublishEvent publishes a workflow event for trigger matching.
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

// Workflow protocol type aliases expose request, response, and model messages
// through the SDK package so provider implementations do not need to import
// generated protobuf packages directly.
type (
	// BoundWorkflowTarget is a protocol alias exposed for provider implementations.
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
	WorkflowOutputValueSourceAgentSession     = proto.WorkflowOutputValueSource_AgentSession
	WorkflowActor                             = proto.WorkflowActor
	WorkflowRunAsSubject                      = proto.WorkflowRunAsSubject
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

// WorkflowRunStatus identifies the lifecycle state of a workflow run.
type WorkflowRunStatus = proto.WorkflowRunStatus

// Workflow run status value constants provide stable SDK names for common
// generated enum values without colliding with workflow telemetry dimensions.
const (
	WorkflowRunStatusValueUnspecified = proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_UNSPECIFIED
	WorkflowRunStatusValuePending     = proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_PENDING
	WorkflowRunStatusValueRunning     = proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_RUNNING
	WorkflowRunStatusValueSucceeded   = proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_SUCCEEDED
	WorkflowRunStatusValueFailed      = proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_FAILED
	WorkflowRunStatusValueCanceled    = proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_CANCELED
)
