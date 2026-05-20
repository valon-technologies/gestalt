package gestalt

import (
	"fmt"

	proto "github.com/valon-technologies/gestalt/sdk/go/internal/gen/v1"
)

type WorkflowManagerPlanDeployment struct {
	ProviderName   string
	Spec           *WorkflowDeploymentSpec
	IdempotencyKey string
}

type WorkflowManagerApplyDeployment struct {
	ProviderName   string
	Spec           *WorkflowDeploymentSpec
	IdempotencyKey string
}

type WorkflowManagerGetDeployment struct {
	DeploymentID string
}

type WorkflowManagerListDeployments struct {
	ProviderName string
}

type WorkflowManagerDeleteDeployment struct {
	DeploymentID string
	Generation   int64
}

type WorkflowManagerSetDeploymentPaused struct {
	DeploymentID string
	Paused       bool
}

type WorkflowManagerSetActivationPaused struct {
	DeploymentID string
	ActivationID string
	Paused       bool
}

type WorkflowManagerStartRun struct {
	ProviderName         string
	DeploymentID         string
	DeploymentGeneration int64
	ActivationID         string
	WorkflowKey          string
	Input                any
	IdempotencyKey       string
}

type WorkflowManagerSignalRun struct {
	RunID  string
	Signal *WorkflowSignal
}

type WorkflowManagerSignalOrStartRun struct {
	ProviderName         string
	DeploymentID         string
	DeploymentGeneration int64
	ActivationID         string
	WorkflowKey          string
	Input                any
	IdempotencyKey       string
	Signal               *WorkflowSignal
}

type WorkflowManagerCancelRun struct {
	RunID  string
	Reason string
}

type WorkflowManagerDeliverEvent struct {
	ProviderName   string
	Event          *WorkflowEvent
	IdempotencyKey string
}

type WorkflowManagerDeployment struct {
	ProviderName string
	Deployment   *WorkflowDeployment
}

type WorkflowManagerListDeploymentsResponse struct {
	Deployments []WorkflowManagerDeployment
}

func (r *WorkflowManagerListDeploymentsResponse) GetDeployments() []WorkflowManagerDeployment {
	if r == nil {
		return nil
	}
	return r.Deployments
}

type WorkflowManagerRun struct {
	ProviderName string
	Run          *WorkflowRun
}

type WorkflowManagerRunSignal struct {
	ProviderName string
	Run          *WorkflowRun
	Signal       *WorkflowSignal
	StartedRun   bool
	WorkflowKey  string
}

type WorkflowManagerDeliverEventResponse struct {
	Results []WorkflowEventDeliveryResult
}

func (r *WorkflowManagerDeliverEventResponse) GetResults() []WorkflowEventDeliveryResult {
	if r == nil {
		return nil
	}
	return r.Results
}

func newWorkflowManagerPlanDeploymentRequest(input WorkflowManagerPlanDeployment) (*proto.WorkflowManagerPlanDeploymentRequest, error) {
	spec, err := newOptionalWorkflowDeploymentSpec(input.Spec)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowManagerPlanDeploymentRequest{
		ProviderName:   input.ProviderName,
		Spec:           spec,
		IdempotencyKey: input.IdempotencyKey,
	}, nil
}

func newWorkflowManagerApplyDeploymentRequest(input WorkflowManagerApplyDeployment) (*proto.WorkflowManagerApplyDeploymentRequest, error) {
	spec, err := newOptionalWorkflowDeploymentSpec(input.Spec)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowManagerApplyDeploymentRequest{
		ProviderName:   input.ProviderName,
		Spec:           spec,
		IdempotencyKey: input.IdempotencyKey,
	}, nil
}

func newWorkflowManagerGetDeploymentRequest(input WorkflowManagerGetDeployment) *proto.WorkflowManagerGetDeploymentRequest {
	return &proto.WorkflowManagerGetDeploymentRequest{DeploymentId: input.DeploymentID}
}

func newWorkflowManagerListDeploymentsRequest(input WorkflowManagerListDeployments) *proto.WorkflowManagerListDeploymentsRequest {
	return &proto.WorkflowManagerListDeploymentsRequest{ProviderName: input.ProviderName}
}

func newWorkflowManagerDeleteDeploymentRequest(input WorkflowManagerDeleteDeployment) *proto.WorkflowManagerDeleteDeploymentRequest {
	return &proto.WorkflowManagerDeleteDeploymentRequest{
		DeploymentId: input.DeploymentID,
		Generation:   input.Generation,
	}
}

func newWorkflowManagerSetDeploymentPausedRequest(input WorkflowManagerSetDeploymentPaused) *proto.WorkflowManagerSetDeploymentPausedRequest {
	return &proto.WorkflowManagerSetDeploymentPausedRequest{
		DeploymentId: input.DeploymentID,
		Paused:       input.Paused,
	}
}

func newWorkflowManagerSetActivationPausedRequest(input WorkflowManagerSetActivationPaused) *proto.WorkflowManagerSetActivationPausedRequest {
	return &proto.WorkflowManagerSetActivationPausedRequest{
		DeploymentId: input.DeploymentID,
		ActivationId: input.ActivationID,
		Paused:       input.Paused,
	}
}

func newWorkflowManagerStartRunRequest(input WorkflowManagerStartRun) (*proto.WorkflowManagerStartRunRequest, error) {
	body, err := structFromAny(input.Input)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowManagerStartRunRequest{
		ProviderName:         input.ProviderName,
		DeploymentId:         input.DeploymentID,
		DeploymentGeneration: input.DeploymentGeneration,
		ActivationId:         input.ActivationID,
		WorkflowKey:          input.WorkflowKey,
		Input:                body,
		IdempotencyKey:       input.IdempotencyKey,
	}, nil
}

func newWorkflowManagerSignalRunRequest(input WorkflowManagerSignalRun) (*proto.WorkflowManagerSignalRunRequest, error) {
	signal, err := newOptionalWorkflowSignal(input.Signal)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowManagerSignalRunRequest{
		RunId:  input.RunID,
		Signal: signal,
	}, nil
}

func newWorkflowManagerSignalOrStartRunRequest(input WorkflowManagerSignalOrStartRun) (*proto.WorkflowManagerSignalOrStartRunRequest, error) {
	body, err := structFromAny(input.Input)
	if err != nil {
		return nil, err
	}
	signal, err := newOptionalWorkflowSignal(input.Signal)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowManagerSignalOrStartRunRequest{
		ProviderName:         input.ProviderName,
		DeploymentId:         input.DeploymentID,
		DeploymentGeneration: input.DeploymentGeneration,
		ActivationId:         input.ActivationID,
		WorkflowKey:          input.WorkflowKey,
		Input:                body,
		IdempotencyKey:       input.IdempotencyKey,
		Signal:               signal,
	}, nil
}

func newWorkflowManagerCancelRunRequest(input WorkflowManagerCancelRun) *proto.WorkflowManagerCancelRunRequest {
	return &proto.WorkflowManagerCancelRunRequest{
		RunId:  input.RunID,
		Reason: input.Reason,
	}
}

func newWorkflowManagerDeliverEventRequest(input WorkflowManagerDeliverEvent) (*proto.WorkflowManagerDeliverEventRequest, error) {
	event, err := newOptionalWorkflowEvent(input.Event)
	if err != nil {
		return nil, err
	}
	return &proto.WorkflowManagerDeliverEventRequest{
		ProviderName:   input.ProviderName,
		Event:          event,
		IdempotencyKey: input.IdempotencyKey,
	}, nil
}

func workflowManagerDeploymentFromProto(value *proto.ManagedWorkflowDeployment) (*WorkflowManagerDeployment, error) {
	if value == nil {
		return nil, nil
	}
	deployment, err := workflowDeploymentFromProto(value.GetDeployment())
	if err != nil {
		return nil, err
	}
	return &WorkflowManagerDeployment{ProviderName: value.GetProviderName(), Deployment: deployment}, nil
}

func workflowManagerDeploymentsFromProto(values []*proto.ManagedWorkflowDeployment) ([]WorkflowManagerDeployment, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]WorkflowManagerDeployment, 0, len(values))
	for i, value := range values {
		deployment, err := workflowManagerDeploymentFromProto(value)
		if err != nil {
			return nil, fmt.Errorf("deployments[%d]: %w", i, err)
		}
		if deployment != nil {
			out = append(out, *deployment)
		}
	}
	return out, nil
}

func workflowManagerRunFromProto(value *proto.ManagedWorkflowRun) (*WorkflowManagerRun, error) {
	if value == nil {
		return nil, nil
	}
	run, err := workflowRunFromProto(value.GetRun())
	if err != nil {
		return nil, err
	}
	return &WorkflowManagerRun{ProviderName: value.GetProviderName(), Run: run}, nil
}

func workflowManagerRunSignalFromProto(value *proto.ManagedWorkflowRunSignal) (*WorkflowManagerRunSignal, error) {
	if value == nil {
		return nil, nil
	}
	run, err := workflowRunFromProto(value.GetRun())
	if err != nil {
		return nil, err
	}
	var signal *WorkflowSignal
	if value.GetSignal() != nil {
		input := workflowSignalFromProto(value.GetSignal())
		signal = &input
	}
	return &WorkflowManagerRunSignal{
		ProviderName: value.GetProviderName(),
		Run:          run,
		Signal:       signal,
		StartedRun:   value.GetStartedRun(),
		WorkflowKey:  value.GetWorkflowKey(),
	}, nil
}

func workflowManagerDeliverEventResponseFromProto(value *proto.WorkflowManagerDeliverEventResponse) (*WorkflowManagerDeliverEventResponse, error) {
	if value == nil {
		return nil, nil
	}
	results, err := workflowEventDeliveryResultsFromProto(value.GetResults())
	if err != nil {
		return nil, err
	}
	return &WorkflowManagerDeliverEventResponse{Results: results}, nil
}
