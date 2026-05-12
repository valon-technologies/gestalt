package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
)

// StartWorkflowProviderRunRequest contains native Go values for starting a
// workflow run.
type StartWorkflowProviderRunRequest struct {
	Target         *BoundWorkflowTargetInput
	IdempotencyKey string
	CreatedBy      *WorkflowActorInput
	ExecutionRef   string
	WorkflowKey    string
}

// GetWorkflowProviderRunRequest identifies one workflow run.
type GetWorkflowProviderRunRequest struct {
	RunID string
}

// ListWorkflowProviderRunsRequest requests workflow runs visible to the caller.
type ListWorkflowProviderRunsRequest struct{}

// ListWorkflowProviderRunsResponse contains native workflow runs.
type ListWorkflowProviderRunsResponse struct {
	Runs []BoundWorkflowRunInput
}

// GetRuns returns workflow runs from the response.
func (r *ListWorkflowProviderRunsResponse) GetRuns() []BoundWorkflowRunInput {
	if r == nil {
		return nil
	}
	return r.Runs
}

// CancelWorkflowProviderRunRequest requests cancellation of one workflow run.
type CancelWorkflowProviderRunRequest struct {
	RunID  string
	Reason string
}

// SignalWorkflowProviderRunRequest contains native values for signaling a run.
type SignalWorkflowProviderRunRequest struct {
	RunID  string
	Signal *WorkflowSignalInput
}

// SignalOrStartWorkflowProviderRunRequest contains native values for signaling
// an existing workflow run or starting one when needed.
type SignalOrStartWorkflowProviderRunRequest struct {
	WorkflowKey    string
	Target         *BoundWorkflowTargetInput
	IdempotencyKey string
	CreatedBy      *WorkflowActorInput
	ExecutionRef   string
	Signal         *WorkflowSignalInput
}

// SignalWorkflowRunResponse contains the run and signal affected by a signal
// operation.
type SignalWorkflowRunResponse struct {
	Run         *BoundWorkflowRunInput
	Signal      *WorkflowSignalInput
	StartedRun  bool
	WorkflowKey string
}

// GetStatus-compatible helpers are intentionally not provided here; provider
// code should read native fields directly.

// UpsertWorkflowProviderScheduleRequest contains native values for upserting a
// workflow schedule.
type UpsertWorkflowProviderScheduleRequest struct {
	ScheduleID   string
	Cron         string
	Timezone     string
	Target       *BoundWorkflowTargetInput
	Paused       bool
	RequestedBy  *WorkflowActorInput
	ExecutionRef string
}

// GetWorkflowProviderScheduleRequest identifies one workflow schedule.
type GetWorkflowProviderScheduleRequest struct {
	ScheduleID string
}

// ListWorkflowProviderSchedulesRequest requests workflow schedules visible to
// the caller.
type ListWorkflowProviderSchedulesRequest struct{}

// ListWorkflowProviderSchedulesResponse contains native workflow schedules.
type ListWorkflowProviderSchedulesResponse struct {
	Schedules []BoundWorkflowScheduleInput
}

// GetSchedules returns workflow schedules from the response.
func (r *ListWorkflowProviderSchedulesResponse) GetSchedules() []BoundWorkflowScheduleInput {
	if r == nil {
		return nil
	}
	return r.Schedules
}

// DeleteWorkflowProviderScheduleRequest identifies a workflow schedule to
// delete.
type DeleteWorkflowProviderScheduleRequest struct {
	ScheduleID string
}

// PauseWorkflowProviderScheduleRequest identifies a workflow schedule to pause.
type PauseWorkflowProviderScheduleRequest struct {
	ScheduleID string
}

// ResumeWorkflowProviderScheduleRequest identifies a workflow schedule to
// resume.
type ResumeWorkflowProviderScheduleRequest struct {
	ScheduleID string
}

// UpsertWorkflowProviderEventTriggerRequest contains native values for
// upserting a workflow event trigger.
type UpsertWorkflowProviderEventTriggerRequest struct {
	TriggerID    string
	Match        *WorkflowEventMatchInput
	Target       *BoundWorkflowTargetInput
	Paused       bool
	RequestedBy  *WorkflowActorInput
	ExecutionRef string
}

// GetWorkflowProviderEventTriggerRequest identifies one event trigger.
type GetWorkflowProviderEventTriggerRequest struct {
	TriggerID string
}

// ListWorkflowProviderEventTriggersRequest requests event triggers visible to
// the caller.
type ListWorkflowProviderEventTriggersRequest struct{}

// ListWorkflowProviderEventTriggersResponse contains native event triggers.
type ListWorkflowProviderEventTriggersResponse struct {
	Triggers []BoundWorkflowEventTriggerInput
}

// GetTriggers returns workflow event triggers from the response.
func (r *ListWorkflowProviderEventTriggersResponse) GetTriggers() []BoundWorkflowEventTriggerInput {
	if r == nil {
		return nil
	}
	return r.Triggers
}

// DeleteWorkflowProviderEventTriggerRequest identifies an event trigger to
// delete.
type DeleteWorkflowProviderEventTriggerRequest struct {
	TriggerID string
}

// PauseWorkflowProviderEventTriggerRequest identifies an event trigger to
// pause.
type PauseWorkflowProviderEventTriggerRequest struct {
	TriggerID string
}

// ResumeWorkflowProviderEventTriggerRequest identifies an event trigger to
// resume.
type ResumeWorkflowProviderEventTriggerRequest struct {
	TriggerID string
}

// PutWorkflowExecutionReferenceRequest contains a native execution reference to
// store.
type PutWorkflowExecutionReferenceRequest struct {
	Reference *WorkflowExecutionReferenceInput
}

// GetWorkflowExecutionReferenceRequest identifies one execution reference.
type GetWorkflowExecutionReferenceRequest struct {
	ID string
}

// ListWorkflowExecutionReferencesRequest requests execution references for a
// subject.
type ListWorkflowExecutionReferencesRequest struct {
	SubjectID string
}

// ListWorkflowExecutionReferencesResponse contains native execution references.
type ListWorkflowExecutionReferencesResponse struct {
	References []WorkflowExecutionReferenceInput
}

// GetReferences returns workflow execution references from the response.
func (r *ListWorkflowExecutionReferencesResponse) GetReferences() []WorkflowExecutionReferenceInput {
	if r == nil {
		return nil
	}
	return r.References
}

// PublishWorkflowProviderEventRequest contains native values for publishing a
// workflow event.
type PublishWorkflowProviderEventRequest struct {
	PluginName  string
	Event       *WorkflowEventInput
	PublishedBy *WorkflowActorInput
}

// WorkflowProvider is implemented by providers that serve the workflow base
// primitive. The SDK owns the gRPC/protobuf transport adapter; provider code
// implements this typed interface instead of importing generated protobuf
// service bindings.
type WorkflowProvider interface {
	Provider
	// StartRun starts or idempotently returns a workflow run.
	StartRun(ctx context.Context, req *StartWorkflowProviderRunRequest) (*BoundWorkflowRunInput, error)
	// GetRun returns one workflow run by ID.
	GetRun(ctx context.Context, req *GetWorkflowProviderRunRequest) (*BoundWorkflowRunInput, error)
	// ListRuns returns workflow runs visible to the request subject.
	ListRuns(ctx context.Context, req *ListWorkflowProviderRunsRequest) (*ListWorkflowProviderRunsResponse, error)
	// CancelRun requests cancellation of a pending or running workflow run.
	CancelRun(ctx context.Context, req *CancelWorkflowProviderRunRequest) (*BoundWorkflowRunInput, error)
	// SignalRun delivers a signal to an existing workflow run.
	SignalRun(ctx context.Context, req *SignalWorkflowProviderRunRequest) (*SignalWorkflowRunResponse, error)
	// SignalOrStartRun delivers a signal or starts a run when no target run exists.
	SignalOrStartRun(ctx context.Context, req *SignalOrStartWorkflowProviderRunRequest) (*SignalWorkflowRunResponse, error)
	// UpsertSchedule creates or updates a workflow schedule.
	UpsertSchedule(ctx context.Context, req *UpsertWorkflowProviderScheduleRequest) (*BoundWorkflowScheduleInput, error)
	// GetSchedule returns one workflow schedule by ID.
	GetSchedule(ctx context.Context, req *GetWorkflowProviderScheduleRequest) (*BoundWorkflowScheduleInput, error)
	// ListSchedules returns workflow schedules visible to the request subject.
	ListSchedules(ctx context.Context, req *ListWorkflowProviderSchedulesRequest) (*ListWorkflowProviderSchedulesResponse, error)
	// DeleteSchedule deletes a workflow schedule.
	DeleteSchedule(ctx context.Context, req *DeleteWorkflowProviderScheduleRequest) error
	// PauseSchedule pauses a workflow schedule without deleting it.
	PauseSchedule(ctx context.Context, req *PauseWorkflowProviderScheduleRequest) (*BoundWorkflowScheduleInput, error)
	// ResumeSchedule resumes a paused workflow schedule.
	ResumeSchedule(ctx context.Context, req *ResumeWorkflowProviderScheduleRequest) (*BoundWorkflowScheduleInput, error)
	// UpsertEventTrigger creates or updates a workflow event trigger.
	UpsertEventTrigger(ctx context.Context, req *UpsertWorkflowProviderEventTriggerRequest) (*BoundWorkflowEventTriggerInput, error)
	// GetEventTrigger returns one workflow event trigger by ID.
	GetEventTrigger(ctx context.Context, req *GetWorkflowProviderEventTriggerRequest) (*BoundWorkflowEventTriggerInput, error)
	// ListEventTriggers returns workflow event triggers visible to the request subject.
	ListEventTriggers(ctx context.Context, req *ListWorkflowProviderEventTriggersRequest) (*ListWorkflowProviderEventTriggersResponse, error)
	// DeleteEventTrigger deletes a workflow event trigger.
	DeleteEventTrigger(ctx context.Context, req *DeleteWorkflowProviderEventTriggerRequest) error
	// PauseEventTrigger pauses a workflow event trigger without deleting it.
	PauseEventTrigger(ctx context.Context, req *PauseWorkflowProviderEventTriggerRequest) (*BoundWorkflowEventTriggerInput, error)
	// ResumeEventTrigger resumes a paused workflow event trigger.
	ResumeEventTrigger(ctx context.Context, req *ResumeWorkflowProviderEventTriggerRequest) (*BoundWorkflowEventTriggerInput, error)
	// PutExecutionReference stores or updates a workflow execution reference.
	PutExecutionReference(ctx context.Context, req *PutWorkflowExecutionReferenceRequest) (*WorkflowExecutionReferenceInput, error)
	// GetExecutionReference returns one workflow execution reference.
	GetExecutionReference(ctx context.Context, req *GetWorkflowExecutionReferenceRequest) (*WorkflowExecutionReferenceInput, error)
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

func (UnimplementedWorkflowProvider) StartRun(context.Context, *StartWorkflowProviderRunRequest) (*BoundWorkflowRunInput, error) {
	return nil, Unimplemented("workflow start run is not implemented")
}

func (UnimplementedWorkflowProvider) GetRun(context.Context, *GetWorkflowProviderRunRequest) (*BoundWorkflowRunInput, error) {
	return nil, Unimplemented("workflow get run is not implemented")
}

func (UnimplementedWorkflowProvider) ListRuns(context.Context, *ListWorkflowProviderRunsRequest) (*ListWorkflowProviderRunsResponse, error) {
	return nil, Unimplemented("workflow list runs is not implemented")
}

func (UnimplementedWorkflowProvider) CancelRun(context.Context, *CancelWorkflowProviderRunRequest) (*BoundWorkflowRunInput, error) {
	return nil, Unimplemented("workflow cancel run is not implemented")
}

func (UnimplementedWorkflowProvider) SignalRun(context.Context, *SignalWorkflowProviderRunRequest) (*SignalWorkflowRunResponse, error) {
	return nil, Unimplemented("workflow signal run is not implemented")
}

func (UnimplementedWorkflowProvider) SignalOrStartRun(context.Context, *SignalOrStartWorkflowProviderRunRequest) (*SignalWorkflowRunResponse, error) {
	return nil, Unimplemented("workflow signal or start run is not implemented")
}

func (UnimplementedWorkflowProvider) UpsertSchedule(context.Context, *UpsertWorkflowProviderScheduleRequest) (*BoundWorkflowScheduleInput, error) {
	return nil, Unimplemented("workflow upsert schedule is not implemented")
}

func (UnimplementedWorkflowProvider) GetSchedule(context.Context, *GetWorkflowProviderScheduleRequest) (*BoundWorkflowScheduleInput, error) {
	return nil, Unimplemented("workflow get schedule is not implemented")
}

func (UnimplementedWorkflowProvider) ListSchedules(context.Context, *ListWorkflowProviderSchedulesRequest) (*ListWorkflowProviderSchedulesResponse, error) {
	return nil, Unimplemented("workflow list schedules is not implemented")
}

func (UnimplementedWorkflowProvider) DeleteSchedule(context.Context, *DeleteWorkflowProviderScheduleRequest) error {
	return Unimplemented("workflow delete schedule is not implemented")
}

func (UnimplementedWorkflowProvider) PauseSchedule(context.Context, *PauseWorkflowProviderScheduleRequest) (*BoundWorkflowScheduleInput, error) {
	return nil, Unimplemented("workflow pause schedule is not implemented")
}

func (UnimplementedWorkflowProvider) ResumeSchedule(context.Context, *ResumeWorkflowProviderScheduleRequest) (*BoundWorkflowScheduleInput, error) {
	return nil, Unimplemented("workflow resume schedule is not implemented")
}

func (UnimplementedWorkflowProvider) UpsertEventTrigger(context.Context, *UpsertWorkflowProviderEventTriggerRequest) (*BoundWorkflowEventTriggerInput, error) {
	return nil, Unimplemented("workflow upsert event trigger is not implemented")
}

func (UnimplementedWorkflowProvider) GetEventTrigger(context.Context, *GetWorkflowProviderEventTriggerRequest) (*BoundWorkflowEventTriggerInput, error) {
	return nil, Unimplemented("workflow get event trigger is not implemented")
}

func (UnimplementedWorkflowProvider) ListEventTriggers(context.Context, *ListWorkflowProviderEventTriggersRequest) (*ListWorkflowProviderEventTriggersResponse, error) {
	return nil, Unimplemented("workflow list event triggers is not implemented")
}

func (UnimplementedWorkflowProvider) DeleteEventTrigger(context.Context, *DeleteWorkflowProviderEventTriggerRequest) error {
	return Unimplemented("workflow delete event trigger is not implemented")
}

func (UnimplementedWorkflowProvider) PauseEventTrigger(context.Context, *PauseWorkflowProviderEventTriggerRequest) (*BoundWorkflowEventTriggerInput, error) {
	return nil, Unimplemented("workflow pause event trigger is not implemented")
}

func (UnimplementedWorkflowProvider) ResumeEventTrigger(context.Context, *ResumeWorkflowProviderEventTriggerRequest) (*BoundWorkflowEventTriggerInput, error) {
	return nil, Unimplemented("workflow resume event trigger is not implemented")
}

func (UnimplementedWorkflowProvider) PutExecutionReference(context.Context, *PutWorkflowExecutionReferenceRequest) (*WorkflowExecutionReferenceInput, error) {
	return nil, Unimplemented("workflow put execution reference is not implemented")
}

func (UnimplementedWorkflowProvider) GetExecutionReference(context.Context, *GetWorkflowExecutionReferenceRequest) (*WorkflowExecutionReferenceInput, error) {
	return nil, Unimplemented("workflow get execution reference is not implemented")
}

func (UnimplementedWorkflowProvider) ListExecutionReferences(context.Context, *ListWorkflowExecutionReferencesRequest) (*ListWorkflowExecutionReferencesResponse, error) {
	return nil, Unimplemented("workflow list execution references is not implemented")
}

func (UnimplementedWorkflowProvider) PublishEvent(context.Context, *PublishWorkflowProviderEventRequest) error {
	return Unimplemented("workflow publish event is not implemented")
}

// Workflow protocol type aliases expose low-level model messages for protocol
// escape hatches. Workflow provider methods use the native request and response
// structs above; generated protobuf requests stay inside the SDK transport
// adapter.
type (
	// BoundWorkflowTarget is a protocol alias exposed for provider implementations.
	BoundWorkflowTarget                      = proto.BoundWorkflowTarget
	BoundWorkflowTargetPlugin                = proto.BoundWorkflowTarget_Plugin
	BoundWorkflowTargetAgent                 = proto.BoundWorkflowTarget_Agent
	BoundWorkflowPluginTarget                = proto.BoundWorkflowPluginTarget
	BoundWorkflowAgentTarget                 = proto.BoundWorkflowAgentTarget
	WorkflowOutputDelivery                   = proto.WorkflowOutputDelivery
	WorkflowOutputBinding                    = proto.WorkflowOutputBinding
	WorkflowOutputValueSource                = proto.WorkflowOutputValueSource
	WorkflowOutputValueSourceAgentOutput     = proto.WorkflowOutputValueSource_AgentOutput
	WorkflowOutputValueSourceSignalPayload   = proto.WorkflowOutputValueSource_SignalPayload
	WorkflowOutputValueSourceSignalMetadata  = proto.WorkflowOutputValueSource_SignalMetadata
	WorkflowOutputValueSourceLiteral         = proto.WorkflowOutputValueSource_Literal
	WorkflowOutputValueSourceAgentSession    = proto.WorkflowOutputValueSource_AgentSession
	WorkflowActor                            = proto.WorkflowActor
	WorkflowRunAsSubject                     = proto.WorkflowRunAsSubject
	WorkflowEvent                            = proto.WorkflowEvent
	WorkflowEventMatch                       = proto.WorkflowEventMatch
	WorkflowManualTrigger                    = proto.WorkflowManualTrigger
	WorkflowScheduleTrigger                  = proto.WorkflowScheduleTrigger
	WorkflowEventTriggerInvocation           = proto.WorkflowEventTriggerInvocation
	WorkflowRunTrigger                       = proto.WorkflowRunTrigger
	WorkflowRunTriggerManual                 = proto.WorkflowRunTrigger_Manual
	WorkflowRunTriggerSchedule               = proto.WorkflowRunTrigger_Schedule
	WorkflowRunTriggerEvent                  = proto.WorkflowRunTrigger_Event
	BoundWorkflowRun                         = proto.BoundWorkflowRun
	BoundWorkflowSchedule                    = proto.BoundWorkflowSchedule
	BoundWorkflowEventTrigger                = proto.BoundWorkflowEventTrigger
	WorkflowAccessPermission                 = proto.WorkflowAccessPermission
	WorkflowExecutionReference               = proto.WorkflowExecutionReference
	WorkflowSignal                           = proto.WorkflowSignal
	ManagedWorkflowSchedule                  = proto.ManagedWorkflowSchedule
	ManagedWorkflowEventTrigger              = proto.ManagedWorkflowEventTrigger
	ManagedWorkflowRun                       = proto.ManagedWorkflowRun
	ManagedWorkflowRunSignal                 = proto.ManagedWorkflowRunSignal
	WorkflowManagerCreateScheduleRequest     = proto.WorkflowManagerCreateScheduleRequest
	WorkflowManagerGetScheduleRequest        = proto.WorkflowManagerGetScheduleRequest
	WorkflowManagerUpdateScheduleRequest     = proto.WorkflowManagerUpdateScheduleRequest
	WorkflowManagerDeleteScheduleRequest     = proto.WorkflowManagerDeleteScheduleRequest
	WorkflowManagerPauseScheduleRequest      = proto.WorkflowManagerPauseScheduleRequest
	WorkflowManagerResumeScheduleRequest     = proto.WorkflowManagerResumeScheduleRequest
	WorkflowManagerCreateEventTriggerRequest = proto.WorkflowManagerCreateEventTriggerRequest
	WorkflowManagerGetEventTriggerRequest    = proto.WorkflowManagerGetEventTriggerRequest
	WorkflowManagerUpdateEventTriggerRequest = proto.WorkflowManagerUpdateEventTriggerRequest
	WorkflowManagerDeleteEventTriggerRequest = proto.WorkflowManagerDeleteEventTriggerRequest
	WorkflowManagerPauseEventTriggerRequest  = proto.WorkflowManagerPauseEventTriggerRequest
	WorkflowManagerResumeEventTriggerRequest = proto.WorkflowManagerResumeEventTriggerRequest
	WorkflowManagerPublishEventRequest       = proto.WorkflowManagerPublishEventRequest
	WorkflowManagerStartRunRequest           = proto.WorkflowManagerStartRunRequest
	WorkflowManagerSignalRunRequest          = proto.WorkflowManagerSignalRunRequest
	WorkflowManagerSignalOrStartRunRequest   = proto.WorkflowManagerSignalOrStartRunRequest
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
