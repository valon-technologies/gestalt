package gestalt

import "context"

// PlanWorkflowRequest asks a provider to validate and plan a deployment spec.
type PlanWorkflowRequest struct {
	Spec                          *WorkflowDeploymentSpec
	SpecDigest                    string
	TargetDigest                  string
	ActionTableDigest             string
	TargetCanonicalizationVersion string
	WorkflowSemanticsVersion      string
}

// ApplyWorkflowDeploymentRequest asks a provider to create or update a
// deployment from a previously planned spec.
type ApplyWorkflowDeploymentRequest struct {
	Spec         *WorkflowDeploymentSpec
	Plan         *PlanWorkflowResponse
	Binding      *WorkflowDeploymentBinding
	RequestID    string
	ValidateOnly bool
}

// GetWorkflowDeploymentRequest identifies one workflow deployment.
type GetWorkflowDeploymentRequest struct {
	DeploymentID string
}

// ListWorkflowDeploymentsRequest requests workflow deployments visible to the
// caller.
type ListWorkflowDeploymentsRequest struct {
	PageSize  int
	PageToken string
	Labels    map[string]string
}

// ListWorkflowDeploymentsResponse contains workflow deployments.
type ListWorkflowDeploymentsResponse struct {
	Deployments   []WorkflowDeployment
	NextPageToken string
}

// GetDeployments returns deployments from the response.
func (r *ListWorkflowDeploymentsResponse) GetDeployments() []WorkflowDeployment {
	if r == nil {
		return nil
	}
	return r.Deployments
}

// GetNextPageToken returns the token for the next page, if any.
func (r *ListWorkflowDeploymentsResponse) GetNextPageToken() string {
	if r == nil {
		return ""
	}
	return r.NextPageToken
}

// DeleteWorkflowDeploymentRequest identifies a deployment to delete.
type DeleteWorkflowDeploymentRequest struct {
	DeploymentID string
	Generation   int64
	RequestID    string
}

// SetWorkflowDeploymentPausedRequest pauses or resumes a deployment.
type SetWorkflowDeploymentPausedRequest struct {
	DeploymentID string
	Paused       bool
	RequestID    string
}

// SetWorkflowActivationPausedRequest pauses or resumes one deployment
// activation.
type SetWorkflowActivationPausedRequest struct {
	DeploymentID string
	ActivationID string
	Paused       bool
	RequestID    string
}

// StartWorkflowRunRequest requests a new or idempotent workflow run.
type StartWorkflowRunRequest struct {
	DeploymentID         string
	DeploymentGeneration int64
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
	DeploymentID         string
	DeploymentGeneration int64
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
	DeploymentID string
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

// PutWorkflowExecutionReferenceRequest stores provider-owned host-callback
// authority.
type PutWorkflowExecutionReferenceRequest struct {
	ExecutionRef *WorkflowExecutionReference
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
	PlanWorkflow(ctx context.Context, req *PlanWorkflowRequest) (*PlanWorkflowResponse, error)
	ApplyWorkflowDeployment(ctx context.Context, req *ApplyWorkflowDeploymentRequest) (*WorkflowDeployment, error)
	GetWorkflowDeployment(ctx context.Context, req *GetWorkflowDeploymentRequest) (*WorkflowDeployment, error)
	ListWorkflowDeployments(ctx context.Context, req *ListWorkflowDeploymentsRequest) (*ListWorkflowDeploymentsResponse, error)
	DeleteWorkflowDeployment(ctx context.Context, req *DeleteWorkflowDeploymentRequest) error
	SetWorkflowDeploymentPaused(ctx context.Context, req *SetWorkflowDeploymentPausedRequest) (*WorkflowDeployment, error)
	SetWorkflowActivationPaused(ctx context.Context, req *SetWorkflowActivationPausedRequest) (*WorkflowDeployment, error)
	StartWorkflowRun(ctx context.Context, req *StartWorkflowRunRequest) (*WorkflowRun, error)
	SignalWorkflowRun(ctx context.Context, req *SignalWorkflowRunRequest) (*WorkflowRunSignal, error)
	SignalOrStartWorkflowRun(ctx context.Context, req *SignalOrStartWorkflowRunRequest) (*WorkflowRunSignal, error)
	CancelWorkflowRun(ctx context.Context, req *CancelWorkflowRunRequest) (*WorkflowRun, error)
	DeliverWorkflowEvent(ctx context.Context, req *DeliverWorkflowEventRequest) (*DeliverWorkflowEventResponse, error)
	GetWorkflowRun(ctx context.Context, req *GetWorkflowRunRequest) (*WorkflowRun, error)
	ListWorkflowRuns(ctx context.Context, req *ListWorkflowRunsRequest) (*ListWorkflowRunsResponse, error)
	GetWorkflowRunEvents(ctx context.Context, req *GetWorkflowRunEventsRequest) (*ListWorkflowRunEventsResponse, error)
	GetWorkflowRunOutput(ctx context.Context, req *GetWorkflowRunOutputRequest) (*WorkflowRunOutput, error)
	PutExecutionReference(ctx context.Context, req *PutWorkflowExecutionReferenceRequest) (*WorkflowExecutionReference, error)
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

func (UnimplementedWorkflowProvider) PlanWorkflow(context.Context, *PlanWorkflowRequest) (*PlanWorkflowResponse, error) {
	return nil, Unimplemented("workflow plan is not implemented")
}

func (UnimplementedWorkflowProvider) ApplyWorkflowDeployment(context.Context, *ApplyWorkflowDeploymentRequest) (*WorkflowDeployment, error) {
	return nil, Unimplemented("workflow apply deployment is not implemented")
}

func (UnimplementedWorkflowProvider) GetWorkflowDeployment(context.Context, *GetWorkflowDeploymentRequest) (*WorkflowDeployment, error) {
	return nil, Unimplemented("workflow get deployment is not implemented")
}

func (UnimplementedWorkflowProvider) ListWorkflowDeployments(context.Context, *ListWorkflowDeploymentsRequest) (*ListWorkflowDeploymentsResponse, error) {
	return nil, Unimplemented("workflow list deployments is not implemented")
}

func (UnimplementedWorkflowProvider) DeleteWorkflowDeployment(context.Context, *DeleteWorkflowDeploymentRequest) error {
	return Unimplemented("workflow delete deployment is not implemented")
}

func (UnimplementedWorkflowProvider) SetWorkflowDeploymentPaused(context.Context, *SetWorkflowDeploymentPausedRequest) (*WorkflowDeployment, error) {
	return nil, Unimplemented("workflow set deployment paused is not implemented")
}

func (UnimplementedWorkflowProvider) SetWorkflowActivationPaused(context.Context, *SetWorkflowActivationPausedRequest) (*WorkflowDeployment, error) {
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

func (UnimplementedWorkflowProvider) PutExecutionReference(context.Context, *PutWorkflowExecutionReferenceRequest) (*WorkflowExecutionReference, error) {
	return nil, Unimplemented("workflow put execution reference is not implemented")
}

func (UnimplementedWorkflowProvider) GetExecutionReference(context.Context, *GetWorkflowExecutionReferenceRequest) (*WorkflowExecutionReference, error) {
	return nil, Unimplemented("workflow get execution reference is not implemented")
}

func (UnimplementedWorkflowProvider) ListExecutionReferences(context.Context, *ListWorkflowExecutionReferencesRequest) (*ListWorkflowExecutionReferencesResponse, error) {
	return nil, Unimplemented("workflow list execution references is not implemented")
}
