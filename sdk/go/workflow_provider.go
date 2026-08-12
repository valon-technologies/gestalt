package gestalt

import "context"

// ApplyWorkflowProviderDefinitionRequest atomically stores one workflow
// definition specification and its activations.
type ApplyWorkflowProviderDefinitionRequest struct {
	Spec           *WorkflowDefinitionSpec
	IdempotencyKey string
}

// GetWorkflowProviderDefinitionRequest identifies one workflow definition.
type GetWorkflowProviderDefinitionRequest struct {
	DefinitionID string
}

// ListWorkflowProviderDefinitionsRequest requests workflow definitions.
type ListWorkflowProviderDefinitionsRequest struct{}

// ListWorkflowProviderDefinitionsResponse contains workflow definitions.
type ListWorkflowProviderDefinitionsResponse struct {
	Definitions []WorkflowDefinition
}

// GetDefinitions returns the definitions field; it is safe to call on a nil receiver.
func (r *ListWorkflowProviderDefinitionsResponse) GetDefinitions() []WorkflowDefinition {
	if r == nil {
		return nil
	}
	return r.Definitions
}

// SetWorkflowProviderDefinitionPausedRequest changes definition-wide pause
// state. Paused definitions do not start new manual, schedule, or event runs.
type SetWorkflowProviderDefinitionPausedRequest struct {
	DefinitionID string
	Paused       bool
}

// SetWorkflowProviderActivationPausedRequest changes one activation pause
// state without affecting other activations on the same definition.
type SetWorkflowProviderActivationPausedRequest struct {
	DefinitionID string
	ActivationID string
	Paused       bool
}

// DeleteWorkflowProviderDefinitionRequest identifies a workflow definition to
// delete from the authoring/activation surface. Providers keep historical
// generation snapshots required by already-started runs.
type DeleteWorkflowProviderDefinitionRequest struct {
	DefinitionID string
}

// StartWorkflowProviderRunRequest requests a new or idempotent workflow run.
type StartWorkflowProviderRunRequest struct {
	DefinitionID                 string
	ExpectedDefinitionGeneration int64
	Input                        map[string]any
	IdempotencyKey               string
	WorkflowKey                  string
}

// GetWorkflowProviderRunRequest identifies one workflow run.
type GetWorkflowProviderRunRequest struct {
	RunID string
}

// ListWorkflowProviderRunsRequest requests workflow runs visible to the caller.
type ListWorkflowProviderRunsRequest struct {
	PageSize  int
	PageToken string
	Status    WorkflowRunStatus
	TargetApp string
	// KnownApps is filled by gestaltd for provider calls; public callers must omit it
	// filters when target steps are empty. Providers must apply the same rules
	// to returned runs and to TotalCount / StatusCounts.
	KnownApps []string
	// DefinitionID optionally restricts the list to one workflow definition.
	DefinitionID string
}

// ListWorkflowProviderRunsResponse contains workflow runs.
type ListWorkflowProviderRunsResponse struct {
	Runs          []WorkflowRun
	NextPageToken string
	// TotalCount is the visibility total for this request's filters (including
	// KnownApps ownership). Nil when the provider cannot compute it. Distinct
	// from len(Runs).
	TotalCount *int64
	// StatusCounts is the provider/target_app/known_apps/definition_id status histogram with
	// status filter cleared. Nil when unknown.
	StatusCounts *WorkflowRunStatusCounts
}

// WorkflowRunStatusCounts is a visibility status histogram for list aggregates.
type WorkflowRunStatusCounts struct {
	Pending   int64
	Running   int64
	Succeeded int64
	Failed    int64
	Canceled  int64
}

// GetRuns returns the runs field; it is safe to call on a nil receiver.
func (r *ListWorkflowProviderRunsResponse) GetRuns() []WorkflowRun {
	if r == nil {
		return nil
	}
	return r.Runs
}

// GetNextPageToken returns the next page token field; it is safe to call on a nil receiver.
func (r *ListWorkflowProviderRunsResponse) GetNextPageToken() string {
	if r == nil {
		return ""
	}
	return r.NextPageToken
}

// GetTotalCount returns the visibility total when present.
func (r *ListWorkflowProviderRunsResponse) GetTotalCount() (int64, bool) {
	if r == nil || r.TotalCount == nil {
		return 0, false
	}
	return *r.TotalCount, true
}

// GetStatusCounts returns the status histogram; it is safe to call on a nil receiver.
func (r *ListWorkflowProviderRunsResponse) GetStatusCounts() *WorkflowRunStatusCounts {
	if r == nil {
		return nil
	}
	return r.StatusCounts
}

// GetWorkflowProviderRunEventsRequest identifies one run event stream.
type GetWorkflowProviderRunEventsRequest struct {
	RunID string
}

// GetWorkflowProviderRunEventsResponse contains persisted run events.
type GetWorkflowProviderRunEventsResponse struct {
	Events []WorkflowRunEvent
}

// GetEvents returns the events field; it is safe to call on a nil receiver.
func (r *GetWorkflowProviderRunEventsResponse) GetEvents() []WorkflowRunEvent {
	if r == nil {
		return nil
	}
	return r.Events
}

// GetWorkflowProviderRunOutputRequest identifies one run output.
type GetWorkflowProviderRunOutputRequest struct {
	RunID string
}

// GetWorkflowProviderRunOutputResponse contains the terminal run output.
type GetWorkflowProviderRunOutputResponse struct {
	Output any
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
// workflow-key run or starting one from a pinned definition generation.
type SignalOrStartWorkflowProviderRunRequest struct {
	WorkflowKey                  string
	DefinitionID                 string
	ExpectedDefinitionGeneration int64
	Input                        map[string]any
	IdempotencyKey               string
	Signal                       *WorkflowSignal
}

// SignalWorkflowRunResponse contains the run and signal affected by a signal
// operation.
type SignalWorkflowRunResponse struct {
	Run         *WorkflowRun
	Signal      *WorkflowSignal
	StartedRun  bool
	WorkflowKey string
}

// DeliverWorkflowProviderEventRequest requests delivery of a workflow event to
// provider-owned activation matching.
type DeliverWorkflowProviderEventRequest struct {
	Event *WorkflowEvent
}

// WorkflowProvider is implemented by providers that serve the workflow base
// primitive. The SDK owns the transport adapter; provider code implements this
// typed interface instead of generated service bindings.
type WorkflowProvider interface {
	Provider
	ApplyDefinition(ctx context.Context, req *ApplyWorkflowProviderDefinitionRequest) (*WorkflowDefinition, error)
	GetDefinition(ctx context.Context, req *GetWorkflowProviderDefinitionRequest) (*WorkflowDefinition, error)
	ListDefinitions(ctx context.Context, req *ListWorkflowProviderDefinitionsRequest) (*ListWorkflowProviderDefinitionsResponse, error)
	SetDefinitionPaused(ctx context.Context, req *SetWorkflowProviderDefinitionPausedRequest) (*WorkflowDefinition, error)
	SetActivationPaused(ctx context.Context, req *SetWorkflowProviderActivationPausedRequest) (*WorkflowDefinition, error)
	DeleteDefinition(ctx context.Context, req *DeleteWorkflowProviderDefinitionRequest) error
	StartRun(ctx context.Context, req *StartWorkflowProviderRunRequest) (*WorkflowRun, error)
	GetRun(ctx context.Context, req *GetWorkflowProviderRunRequest) (*WorkflowRun, error)
	ListRuns(ctx context.Context, req *ListWorkflowProviderRunsRequest) (*ListWorkflowProviderRunsResponse, error)
	GetRunEvents(ctx context.Context, req *GetWorkflowProviderRunEventsRequest) (*GetWorkflowProviderRunEventsResponse, error)
	GetRunOutput(ctx context.Context, req *GetWorkflowProviderRunOutputRequest) (*GetWorkflowProviderRunOutputResponse, error)
	CancelRun(ctx context.Context, req *CancelWorkflowProviderRunRequest) (*WorkflowRun, error)
	SignalRun(ctx context.Context, req *SignalWorkflowProviderRunRequest) (*SignalWorkflowRunResponse, error)
	SignalOrStartRun(ctx context.Context, req *SignalOrStartWorkflowProviderRunRequest) (*SignalWorkflowRunResponse, error)
	DeliverEvent(ctx context.Context, req *DeliverWorkflowProviderEventRequest) (*WorkflowEvent, error)
}

// UnimplementedWorkflowProvider provides no-op lifecycle behavior and
// unimplemented workflow operations. Embed it when a provider implements only
// part of the workflow surface.
type UnimplementedWorkflowProvider struct{}

// Configure returns Unimplemented; embed UnimplementedWorkflowProvider to default
// unimplemented surface methods.
func (UnimplementedWorkflowProvider) Configure(context.Context, string, map[string]any) error {
	return nil
}

// ApplyDefinition returns Unimplemented; embed UnimplementedWorkflowProvider to default
// unimplemented surface methods.
func (UnimplementedWorkflowProvider) ApplyDefinition(context.Context, *ApplyWorkflowProviderDefinitionRequest) (*WorkflowDefinition, error) {
	return nil, Unimplemented("workflow apply definition is not implemented")
}

// GetDefinition returns Unimplemented; embed UnimplementedWorkflowProvider to default
// unimplemented surface methods.
func (UnimplementedWorkflowProvider) GetDefinition(context.Context, *GetWorkflowProviderDefinitionRequest) (*WorkflowDefinition, error) {
	return nil, Unimplemented("workflow get definition is not implemented")
}

// ListDefinitions returns Unimplemented; embed UnimplementedWorkflowProvider to default
// unimplemented surface methods.
func (UnimplementedWorkflowProvider) ListDefinitions(context.Context, *ListWorkflowProviderDefinitionsRequest) (*ListWorkflowProviderDefinitionsResponse, error) {
	return nil, Unimplemented("workflow list definitions is not implemented")
}

// SetDefinitionPaused returns Unimplemented; embed UnimplementedWorkflowProvider to default
// unimplemented surface methods.
func (UnimplementedWorkflowProvider) SetDefinitionPaused(context.Context, *SetWorkflowProviderDefinitionPausedRequest) (*WorkflowDefinition, error) {
	return nil, Unimplemented("workflow set definition paused is not implemented")
}

// SetActivationPaused returns Unimplemented; embed UnimplementedWorkflowProvider to default
// unimplemented surface methods.
func (UnimplementedWorkflowProvider) SetActivationPaused(context.Context, *SetWorkflowProviderActivationPausedRequest) (*WorkflowDefinition, error) {
	return nil, Unimplemented("workflow set activation paused is not implemented")
}

// DeleteDefinition returns Unimplemented; embed UnimplementedWorkflowProvider to default
// unimplemented surface methods.
func (UnimplementedWorkflowProvider) DeleteDefinition(context.Context, *DeleteWorkflowProviderDefinitionRequest) error {
	return Unimplemented("workflow delete definition is not implemented")
}

// StartRun returns Unimplemented; embed UnimplementedWorkflowProvider to default
// unimplemented surface methods.
func (UnimplementedWorkflowProvider) StartRun(context.Context, *StartWorkflowProviderRunRequest) (*WorkflowRun, error) {
	return nil, Unimplemented("workflow start run is not implemented")
}

// GetRun returns Unimplemented; embed UnimplementedWorkflowProvider to default
// unimplemented surface methods.
func (UnimplementedWorkflowProvider) GetRun(context.Context, *GetWorkflowProviderRunRequest) (*WorkflowRun, error) {
	return nil, Unimplemented("workflow get run is not implemented")
}

// ListRuns returns Unimplemented; embed UnimplementedWorkflowProvider to default
// unimplemented surface methods.
func (UnimplementedWorkflowProvider) ListRuns(context.Context, *ListWorkflowProviderRunsRequest) (*ListWorkflowProviderRunsResponse, error) {
	return nil, Unimplemented("workflow list runs is not implemented")
}

// GetRunEvents returns Unimplemented; embed UnimplementedWorkflowProvider to default
// unimplemented surface methods.
func (UnimplementedWorkflowProvider) GetRunEvents(context.Context, *GetWorkflowProviderRunEventsRequest) (*GetWorkflowProviderRunEventsResponse, error) {
	return nil, Unimplemented("workflow get run events is not implemented")
}

// GetRunOutput returns Unimplemented; embed UnimplementedWorkflowProvider to default
// unimplemented surface methods.
func (UnimplementedWorkflowProvider) GetRunOutput(context.Context, *GetWorkflowProviderRunOutputRequest) (*GetWorkflowProviderRunOutputResponse, error) {
	return nil, Unimplemented("workflow get run output is not implemented")
}

// CancelRun returns Unimplemented; embed UnimplementedWorkflowProvider to default
// unimplemented surface methods.
func (UnimplementedWorkflowProvider) CancelRun(context.Context, *CancelWorkflowProviderRunRequest) (*WorkflowRun, error) {
	return nil, Unimplemented("workflow cancel run is not implemented")
}

// SignalRun returns Unimplemented; embed UnimplementedWorkflowProvider to default
// unimplemented surface methods.
func (UnimplementedWorkflowProvider) SignalRun(context.Context, *SignalWorkflowProviderRunRequest) (*SignalWorkflowRunResponse, error) {
	return nil, Unimplemented("workflow signal run is not implemented")
}

// SignalOrStartRun returns Unimplemented; embed UnimplementedWorkflowProvider to default
// unimplemented surface methods.
func (UnimplementedWorkflowProvider) SignalOrStartRun(context.Context, *SignalOrStartWorkflowProviderRunRequest) (*SignalWorkflowRunResponse, error) {
	return nil, Unimplemented("workflow signal or start run is not implemented")
}

// DeliverEvent returns Unimplemented; embed UnimplementedWorkflowProvider to default
// unimplemented surface methods.
func (UnimplementedWorkflowProvider) DeliverEvent(context.Context, *DeliverWorkflowProviderEventRequest) (*WorkflowEvent, error) {
	return nil, Unimplemented("workflow deliver event is not implemented")
}

// WorkflowRunStatus identifies the lifecycle state of a workflow run.
type WorkflowRunStatus int32

// The workflow run statuses.
const (
	WorkflowRunStatusValueUnspecified WorkflowRunStatus = iota
	WorkflowRunStatusValuePending
	WorkflowRunStatusValueRunning
	WorkflowRunStatusValueSucceeded
	WorkflowRunStatusValueFailed
	WorkflowRunStatusValueCanceled
)

// WorkflowStepStatus identifies the lifecycle state of one workflow step.
type WorkflowStepStatus int32

// The workflow step statuses.
const (
	WorkflowStepStatusValueUnspecified WorkflowStepStatus = iota
	WorkflowStepStatusValuePending
	WorkflowStepStatusValueRunning
	WorkflowStepStatusValueSkipped
	WorkflowStepStatusValueSucceeded
	WorkflowStepStatusValueFailed
	WorkflowStepStatusValueUnknown
)
