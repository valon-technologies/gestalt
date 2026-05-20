package gestalt

import "context"

// ApplyWorkflowDefinitionRequest asks a provider to create or update a
// workflow definition.
type ApplyWorkflowDefinitionRequest struct {
	Spec         *WorkflowDefinitionSpec
	Binding      *WorkflowDefinitionBinding
	ExecutionRef *WorkflowExecutionReference
	RequestID    string
}

// GetWorkflowDefinitionRequest identifies one workflow definition.
type GetWorkflowDefinitionRequest struct {
	DefinitionID string
}

// ListWorkflowDefinitionsRequest requests workflow definitions visible to the
// caller.
type ListWorkflowDefinitionsRequest struct {
	PageSize  int
	PageToken string
	Labels    map[string]string
}

// ListWorkflowDefinitionsResponse contains workflow definitions.
type ListWorkflowDefinitionsResponse struct {
	Definitions   []WorkflowDefinition
	NextPageToken string
}

// GetDefinitions returns definitions from the response.
func (r *ListWorkflowDefinitionsResponse) GetDefinitions() []WorkflowDefinition {
	if r == nil {
		return nil
	}
	return r.Definitions
}

// GetNextPageToken returns the token for the next page, if any.
func (r *ListWorkflowDefinitionsResponse) GetNextPageToken() string {
	if r == nil {
		return ""
	}
	return r.NextPageToken
}

// DeleteWorkflowDefinitionRequest identifies a workflow definition to delete.
type DeleteWorkflowDefinitionRequest struct {
	DefinitionID string
	Generation   int64
	RequestID    string
}

// SetWorkflowDefinitionPausedRequest pauses or resumes a workflow definition.
type SetWorkflowDefinitionPausedRequest struct {
	DefinitionID string
	Paused       bool
	RequestID    string
}

// SetWorkflowActivationPausedRequest pauses or resumes one workflow definition
// activation.
type SetWorkflowActivationPausedRequest struct {
	DefinitionID string
	ActivationID string
	Paused       bool
	RequestID    string
}

// StartWorkflowRunRequest requests a new or idempotent workflow run.
type StartWorkflowRunRequest struct {
	DefinitionID         string
	DefinitionGeneration int64
	ActivationID         string
	WorkflowKey          string
	Input                any
	IdempotencyKey       string
	CreatedBy            *WorkflowActor
}

// SignalWorkflowRunRequest requests signaling a run.
type SignalWorkflowRunRequest struct {
	RunID  string
	Signal *WorkflowSignal
}

// SignalOrStartWorkflowRunRequest requests signaling an existing workflow run
// or starting one when needed.
type SignalOrStartWorkflowRunRequest struct {
	DefinitionID         string
	DefinitionGeneration int64
	ActivationID         string
	WorkflowKey          string
	Input                any
	IdempotencyKey       string
	Signal               *WorkflowSignal
	CreatedBy            *WorkflowActor
}

// CancelWorkflowRunRequest requests cancellation of one workflow run.
type CancelWorkflowRunRequest struct {
	RunID  string
	Reason string
}

// GetWorkflowRunRequest identifies one workflow run.
type GetWorkflowRunRequest struct {
	RunID string
}

// ListWorkflowRunsRequest requests workflow runs visible to the caller.
type ListWorkflowRunsRequest struct {
	DefinitionID string
	PageSize     int
	PageToken    string
	Status       WorkflowRunStatus
}

// ListWorkflowRunsResponse contains workflow runs.
type ListWorkflowRunsResponse struct {
	Runs          []WorkflowRun
	NextPageToken string
}

// GetRuns returns workflow runs from the response.
func (r *ListWorkflowRunsResponse) GetRuns() []WorkflowRun {
	if r == nil {
		return nil
	}
	return r.Runs
}

// GetNextPageToken returns the token for the next page, if any.
func (r *ListWorkflowRunsResponse) GetNextPageToken() string {
	if r == nil {
		return ""
	}
	return r.NextPageToken
}

// DeliverWorkflowEventRequest requests delivering an event to matching
// workflow activations.
type DeliverWorkflowEventRequest struct {
	DeliveryID     string
	Event          *WorkflowEvent
	PublishedBy    *WorkflowActor
	IdempotencyKey string
}

// DeliverWorkflowEventResponse contains event delivery results.
type DeliverWorkflowEventResponse struct {
	Results []WorkflowEventDeliveryResult
}

// GetResults returns event delivery results.
func (r *DeliverWorkflowEventResponse) GetResults() []WorkflowEventDeliveryResult {
	if r == nil {
		return nil
	}
	return r.Results
}

// GetWorkflowRunEventsRequest requests events for one run.
type GetWorkflowRunEventsRequest struct {
	RunID     string
	PageSize  int
	PageToken string
}

// ListWorkflowRunEventsResponse contains workflow run events.
type ListWorkflowRunEventsResponse struct {
	Events        []WorkflowRunEvent
	NextPageToken string
}

// GetEvents returns run events from the response.
func (r *ListWorkflowRunEventsResponse) GetEvents() []WorkflowRunEvent {
	if r == nil {
		return nil
	}
	return r.Events
}

// GetNextPageToken returns the token for the next page, if any.
func (r *ListWorkflowRunEventsResponse) GetNextPageToken() string {
	if r == nil {
		return ""
	}
	return r.NextPageToken
}

// GetWorkflowRunOutputRequest identifies a run or step output.
type GetWorkflowRunOutputRequest struct {
	RunID     string
	OutputRef string
	StepID    string
}

// GetWorkflowExecutionReferenceRequest identifies one execution reference.
type GetWorkflowExecutionReferenceRequest struct {
	ID string
}

// ListWorkflowExecutionReferencesRequest requests execution references for one
// subject.
type ListWorkflowExecutionReferencesRequest struct {
	SubjectID string
}

// ListWorkflowExecutionReferencesResponse contains execution references.
type ListWorkflowExecutionReferencesResponse struct {
	ExecutionRefs []WorkflowExecutionReference
}

// GetExecutionRefs returns execution references from the response.
func (r *ListWorkflowExecutionReferencesResponse) GetExecutionRefs() []WorkflowExecutionReference {
	if r == nil {
		return nil
	}
	return r.ExecutionRefs
}

// WorkflowProvider is implemented by providers that serve the workflow base
// primitive. The SDK owns the transport adapter; provider code implements this
// typed interface instead of generated service bindings.
type WorkflowProvider interface {
	Provider
	ApplyWorkflowDefinition(ctx context.Context, req *ApplyWorkflowDefinitionRequest) (*WorkflowDefinition, error)
	GetWorkflowDefinition(ctx context.Context, req *GetWorkflowDefinitionRequest) (*WorkflowDefinition, error)
	ListWorkflowDefinitions(ctx context.Context, req *ListWorkflowDefinitionsRequest) (*ListWorkflowDefinitionsResponse, error)
	DeleteWorkflowDefinition(ctx context.Context, req *DeleteWorkflowDefinitionRequest) error
	SetWorkflowDefinitionPaused(ctx context.Context, req *SetWorkflowDefinitionPausedRequest) (*WorkflowDefinition, error)
	SetWorkflowActivationPaused(ctx context.Context, req *SetWorkflowActivationPausedRequest) (*WorkflowDefinition, error)
	StartWorkflowRun(ctx context.Context, req *StartWorkflowRunRequest) (*WorkflowRun, error)
	SignalWorkflowRun(ctx context.Context, req *SignalWorkflowRunRequest) (*WorkflowRunSignal, error)
	SignalOrStartWorkflowRun(ctx context.Context, req *SignalOrStartWorkflowRunRequest) (*WorkflowRunSignal, error)
	CancelWorkflowRun(ctx context.Context, req *CancelWorkflowRunRequest) (*WorkflowRun, error)
	DeliverWorkflowEvent(ctx context.Context, req *DeliverWorkflowEventRequest) (*DeliverWorkflowEventResponse, error)
	GetWorkflowRun(ctx context.Context, req *GetWorkflowRunRequest) (*WorkflowRun, error)
	ListWorkflowRuns(ctx context.Context, req *ListWorkflowRunsRequest) (*ListWorkflowRunsResponse, error)
	GetWorkflowRunEvents(ctx context.Context, req *GetWorkflowRunEventsRequest) (*ListWorkflowRunEventsResponse, error)
	GetWorkflowRunOutput(ctx context.Context, req *GetWorkflowRunOutputRequest) (*WorkflowRunOutput, error)
	GetExecutionReference(ctx context.Context, req *GetWorkflowExecutionReferenceRequest) (*WorkflowExecutionReference, error)
	ListExecutionReferences(ctx context.Context, req *ListWorkflowExecutionReferencesRequest) (*ListWorkflowExecutionReferencesResponse, error)
}

// UnimplementedWorkflowProvider provides no-op lifecycle behavior and
// unimplemented workflow operations. Embed it when a provider implements only
// part of the workflow surface.
type UnimplementedWorkflowProvider struct{}

func (UnimplementedWorkflowProvider) Configure(context.Context, string, map[string]any) error {
	return nil
}

func (UnimplementedWorkflowProvider) ApplyWorkflowDefinition(context.Context, *ApplyWorkflowDefinitionRequest) (*WorkflowDefinition, error) {
	return nil, Unimplemented("workflow apply definition is not implemented")
}

func (UnimplementedWorkflowProvider) GetWorkflowDefinition(context.Context, *GetWorkflowDefinitionRequest) (*WorkflowDefinition, error) {
	return nil, Unimplemented("workflow get definition is not implemented")
}

func (UnimplementedWorkflowProvider) ListWorkflowDefinitions(context.Context, *ListWorkflowDefinitionsRequest) (*ListWorkflowDefinitionsResponse, error) {
	return nil, Unimplemented("workflow list definitions is not implemented")
}

func (UnimplementedWorkflowProvider) DeleteWorkflowDefinition(context.Context, *DeleteWorkflowDefinitionRequest) error {
	return Unimplemented("workflow delete definition is not implemented")
}

func (UnimplementedWorkflowProvider) SetWorkflowDefinitionPaused(context.Context, *SetWorkflowDefinitionPausedRequest) (*WorkflowDefinition, error) {
	return nil, Unimplemented("workflow set deployment paused is not implemented")
}

func (UnimplementedWorkflowProvider) SetWorkflowActivationPaused(context.Context, *SetWorkflowActivationPausedRequest) (*WorkflowDefinition, error) {
	return nil, Unimplemented("workflow set activation paused is not implemented")
}

func (UnimplementedWorkflowProvider) StartWorkflowRun(context.Context, *StartWorkflowRunRequest) (*WorkflowRun, error) {
	return nil, Unimplemented("workflow start run is not implemented")
}

func (UnimplementedWorkflowProvider) SignalWorkflowRun(context.Context, *SignalWorkflowRunRequest) (*WorkflowRunSignal, error) {
	return nil, Unimplemented("workflow signal run is not implemented")
}

func (UnimplementedWorkflowProvider) SignalOrStartWorkflowRun(context.Context, *SignalOrStartWorkflowRunRequest) (*WorkflowRunSignal, error) {
	return nil, Unimplemented("workflow signal or start run is not implemented")
}

func (UnimplementedWorkflowProvider) CancelWorkflowRun(context.Context, *CancelWorkflowRunRequest) (*WorkflowRun, error) {
	return nil, Unimplemented("workflow cancel run is not implemented")
}

func (UnimplementedWorkflowProvider) DeliverWorkflowEvent(context.Context, *DeliverWorkflowEventRequest) (*DeliverWorkflowEventResponse, error) {
	return nil, Unimplemented("workflow deliver event is not implemented")
}

func (UnimplementedWorkflowProvider) GetWorkflowRun(context.Context, *GetWorkflowRunRequest) (*WorkflowRun, error) {
	return nil, Unimplemented("workflow get run is not implemented")
}

func (UnimplementedWorkflowProvider) ListWorkflowRuns(context.Context, *ListWorkflowRunsRequest) (*ListWorkflowRunsResponse, error) {
	return nil, Unimplemented("workflow list runs is not implemented")
}

func (UnimplementedWorkflowProvider) GetWorkflowRunEvents(context.Context, *GetWorkflowRunEventsRequest) (*ListWorkflowRunEventsResponse, error) {
	return nil, Unimplemented("workflow get run events is not implemented")
}

func (UnimplementedWorkflowProvider) GetWorkflowRunOutput(context.Context, *GetWorkflowRunOutputRequest) (*WorkflowRunOutput, error) {
	return nil, Unimplemented("workflow get run output is not implemented")
}

func (UnimplementedWorkflowProvider) GetExecutionReference(context.Context, *GetWorkflowExecutionReferenceRequest) (*WorkflowExecutionReference, error) {
	return nil, Unimplemented("workflow get execution reference is not implemented")
}

func (UnimplementedWorkflowProvider) ListExecutionReferences(context.Context, *ListWorkflowExecutionReferencesRequest) (*ListWorkflowExecutionReferencesResponse, error) {
	return nil, Unimplemented("workflow list execution references is not implemented")
}
