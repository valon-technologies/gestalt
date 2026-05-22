package gestalt

import "context"

// StartWorkflowProviderRunRequest requests a new or idempotent workflow run.
type StartWorkflowProviderRunRequest struct {
	Target         *BoundWorkflowTarget
	IdempotencyKey string
	CreatedBy      *WorkflowActor
	ExecutionRef   string
	WorkflowKey    string
	DefinitionID   string
}

// GetWorkflowProviderRunRequest identifies one workflow run.
type GetWorkflowProviderRunRequest struct {
	RunID string
}

// ListWorkflowProviderRunsRequest requests workflow runs visible to the caller.
type ListWorkflowProviderRunsRequest struct {
	PageSize     int
	PageToken    string
	Status       WorkflowRunStatus
	TargetApp string
}

// ListWorkflowProviderRunsResponse contains workflow runs.
type ListWorkflowProviderRunsResponse struct {
	Runs          []BoundWorkflowRun
	NextPageToken string
}

// GetRuns returns workflow runs from the response.
func (r *ListWorkflowProviderRunsResponse) GetRuns() []BoundWorkflowRun {
	if r == nil {
		return nil
	}
	return r.Runs
}

// GetNextPageToken returns the token for the next page, if any.
func (r *ListWorkflowProviderRunsResponse) GetNextPageToken() string {
	if r == nil {
		return ""
	}
	return r.NextPageToken
}

// CancelWorkflowProviderRunRequest requests cancellation of one workflow run.
type CancelWorkflowProviderRunRequest struct {
	RunID  string
	Reason string
}

// SignalWorkflowProviderRunRequest requests signaling a run.
type SignalWorkflowProviderRunRequest struct {
	RunID  string
	Signal *WorkflowSignal
}

// SignalOrStartWorkflowProviderRunRequest requests signaling an existing
// workflow run or starting one when needed.
type SignalOrStartWorkflowProviderRunRequest struct {
	WorkflowKey    string
	Target         *BoundWorkflowTarget
	IdempotencyKey string
	CreatedBy      *WorkflowActor
	ExecutionRef   string
	Signal         *WorkflowSignal
	DefinitionID   string
}

// CreateWorkflowProviderDefinitionRequest requests creating a workflow
// definition in provider-owned storage.
type CreateWorkflowProviderDefinitionRequest struct {
	Target         *BoundWorkflowTarget
	IdempotencyKey string
}

// GetWorkflowProviderDefinitionRequest identifies one workflow definition.
type GetWorkflowProviderDefinitionRequest struct {
	DefinitionID string
}

// UpdateWorkflowProviderDefinitionRequest requests updating a workflow
// definition.
type UpdateWorkflowProviderDefinitionRequest struct {
	DefinitionID string
	Target       *BoundWorkflowTarget
}

// DeleteWorkflowProviderDefinitionRequest identifies a workflow definition to
// delete.
type DeleteWorkflowProviderDefinitionRequest struct {
	DefinitionID string
}

// SignalWorkflowRunResponse contains the run and signal affected by a signal
// operation.
type SignalWorkflowRunResponse struct {
	Run         *BoundWorkflowRun
	Signal      *WorkflowSignal
	StartedRun  bool
	WorkflowKey string
}

// GetStatus-compatible helpers are intentionally not provided here; provider
// code should read native fields directly.

// UpsertWorkflowProviderScheduleRequest requests creating or updating a
// workflow schedule.
type UpsertWorkflowProviderScheduleRequest struct {
	ScheduleID     string
	Cron           string
	Timezone       string
	Target         *BoundWorkflowTarget
	Paused         bool
	RequestedBy    *WorkflowActor
	ExecutionRef   string
	IdempotencyKey string
	DefinitionID   string
}

// GetWorkflowProviderScheduleRequest identifies one workflow schedule.
type GetWorkflowProviderScheduleRequest struct {
	ScheduleID string
}

// ListWorkflowProviderSchedulesRequest requests workflow schedules visible to
// the caller.
type ListWorkflowProviderSchedulesRequest struct{}

// ListWorkflowProviderSchedulesResponse contains workflow schedules.
type ListWorkflowProviderSchedulesResponse struct {
	Schedules []BoundWorkflowSchedule
}

// GetSchedules returns workflow schedules from the response.
func (r *ListWorkflowProviderSchedulesResponse) GetSchedules() []BoundWorkflowSchedule {
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

// UpsertWorkflowProviderEventTriggerRequest requests creating or updating a
// workflow event trigger.
type UpsertWorkflowProviderEventTriggerRequest struct {
	TriggerID      string
	Match          *WorkflowEventMatch
	Target         *BoundWorkflowTarget
	Paused         bool
	RequestedBy    *WorkflowActor
	ExecutionRef   string
	IdempotencyKey string
	DefinitionID   string
}

// GetWorkflowProviderEventTriggerRequest identifies one event trigger.
type GetWorkflowProviderEventTriggerRequest struct {
	TriggerID string
}

// ListWorkflowProviderEventTriggersRequest requests event triggers visible to
// the caller.
type ListWorkflowProviderEventTriggersRequest struct{}

// ListWorkflowProviderEventTriggersResponse contains workflow event triggers.
type ListWorkflowProviderEventTriggersResponse struct {
	Triggers []BoundWorkflowEventTrigger
}

// GetTriggers returns workflow event triggers from the response.
func (r *ListWorkflowProviderEventTriggersResponse) GetTriggers() []BoundWorkflowEventTrigger {
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
	Reference *WorkflowExecutionReference
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

// ListWorkflowExecutionReferencesResponse contains workflow execution references.
type ListWorkflowExecutionReferencesResponse struct {
	References []WorkflowExecutionReference
}

// GetReferences returns workflow execution references from the response.
func (r *ListWorkflowExecutionReferencesResponse) GetReferences() []WorkflowExecutionReference {
	if r == nil {
		return nil
	}
	return r.References
}

// PublishWorkflowProviderEventRequest requests publishing a workflow event.
type PublishWorkflowProviderEventRequest struct {
	AppName  string
	Event       *WorkflowEvent
	PublishedBy *WorkflowActor
}

// WorkflowProvider is implemented by providers that serve the workflow base
// primitive. The SDK owns the transport adapter; provider code implements this
// typed interface instead of generated service bindings.
type WorkflowProvider interface {
	Provider
	// CreateDefinition creates a reusable workflow definition.
	CreateDefinition(ctx context.Context, req *CreateWorkflowProviderDefinitionRequest) (*BoundWorkflowDefinition, error)
	// GetDefinition returns one workflow definition by ID.
	GetDefinition(ctx context.Context, req *GetWorkflowProviderDefinitionRequest) (*BoundWorkflowDefinition, error)
	// UpdateDefinition updates a reusable workflow definition.
	UpdateDefinition(ctx context.Context, req *UpdateWorkflowProviderDefinitionRequest) (*BoundWorkflowDefinition, error)
	// DeleteDefinition deletes a reusable workflow definition.
	DeleteDefinition(ctx context.Context, req *DeleteWorkflowProviderDefinitionRequest) error
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
	PublishEvent(ctx context.Context, req *PublishWorkflowProviderEventRequest) (*WorkflowEvent, error)
}

// UnimplementedWorkflowProvider provides no-op lifecycle behavior and
// unimplemented workflow operations. Embed it when a provider implements only
// part of the workflow surface.
type UnimplementedWorkflowProvider struct{}

func (UnimplementedWorkflowProvider) Configure(context.Context, string, map[string]any) error {
	return nil
}

func (UnimplementedWorkflowProvider) CreateDefinition(context.Context, *CreateWorkflowProviderDefinitionRequest) (*BoundWorkflowDefinition, error) {
	return nil, Unimplemented("workflow create definition is not implemented")
}

func (UnimplementedWorkflowProvider) GetDefinition(context.Context, *GetWorkflowProviderDefinitionRequest) (*BoundWorkflowDefinition, error) {
	return nil, Unimplemented("workflow get definition is not implemented")
}

func (UnimplementedWorkflowProvider) UpdateDefinition(context.Context, *UpdateWorkflowProviderDefinitionRequest) (*BoundWorkflowDefinition, error) {
	return nil, Unimplemented("workflow update definition is not implemented")
}

func (UnimplementedWorkflowProvider) DeleteDefinition(context.Context, *DeleteWorkflowProviderDefinitionRequest) error {
	return Unimplemented("workflow delete definition is not implemented")
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

func (UnimplementedWorkflowProvider) PublishEvent(context.Context, *PublishWorkflowProviderEventRequest) (*WorkflowEvent, error) {
	return nil, Unimplemented("workflow publish event is not implemented")
}

// WorkflowRunStatus identifies the lifecycle state of a workflow run.
type WorkflowRunStatus int32

// Workflow run status value constants provide stable SDK names for common
// generated enum values without colliding with workflow telemetry dimensions.
const (
	WorkflowRunStatusValueUnspecified WorkflowRunStatus = iota
	WorkflowRunStatusValuePending
	WorkflowRunStatusValueRunning
	WorkflowRunStatusValueSucceeded
	WorkflowRunStatusValueFailed
	WorkflowRunStatusValueCanceled
)
