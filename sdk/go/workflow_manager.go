package gestalt

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/internal/gen/v1"
)

// EnvWorkflowManagerSocket names the environment variable containing the
// workflow-manager service target.
const EnvWorkflowManagerSocket = proto.EnvWorkflowManagerSocket

// EnvWorkflowManagerSocketToken names the optional workflow-manager relay-token
// variable.
const EnvWorkflowManagerSocketToken = EnvWorkflowManagerSocket + "_TOKEN"

// WorkflowManagerClient manages workflow deployments and runs.
type WorkflowManagerClient struct {
	client          proto.WorkflowManagerHostClient
	invocationToken string
	idempotencyKey  string
}

var sharedWorkflowManagerTransport sharedManagerTransport[proto.WorkflowManagerHostClient]

// WorkflowManager returns a client that attaches invocationToken to every request.
func WorkflowManager(invocationToken string) (*WorkflowManagerClient, error) {
	if strings.TrimSpace(invocationToken) == "" {
		return nil, fmt.Errorf("workflow manager: invocation token is not available")
	}
	target := os.Getenv(EnvWorkflowManagerSocket)
	if target == "" {
		return nil, fmt.Errorf("workflow manager: %s is not set", EnvWorkflowManagerSocket)
	}
	token := os.Getenv(EnvWorkflowManagerSocketToken)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := managerTransportClient(ctx, "workflow manager", target, token, &sharedWorkflowManagerTransport, proto.NewWorkflowManagerHostClient)
	if err != nil {
		return nil, err
	}

	return &WorkflowManagerClient{client: client, invocationToken: strings.TrimSpace(invocationToken)}, nil
}

// WorkflowManagerFromContext returns a WorkflowManager using context metadata.
func WorkflowManagerFromContext(ctx context.Context) (*WorkflowManagerClient, error) {
	client, err := WorkflowManager(InvocationTokenFromContext(ctx))
	if err != nil {
		return nil, err
	}
	client.idempotencyKey = IdempotencyKeyFromContext(ctx)
	return client, nil
}

// Close is a no-op compatibility method because this client uses shared transport.
func (c *WorkflowManagerClient) Close() error {
	return nil
}

// PlanDeployment validates and plans a workflow deployment.
func (c *WorkflowManagerClient) PlanDeployment(ctx context.Context, input WorkflowManagerPlanDeployment) (*PlanWorkflowResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req, err := newWorkflowManagerPlanDeploymentRequest(input)
	if err != nil {
		return nil, err
	}
	req.InvocationToken = c.invocationToken
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = c.idempotencyKey
	}
	resp, err := c.client.PlanDeployment(ctx, req)
	if err != nil {
		return nil, err
	}
	return planWorkflowResponseFromProto(resp), nil
}

// ApplyDeployment creates or updates a workflow deployment.
func (c *WorkflowManagerClient) ApplyDeployment(ctx context.Context, input WorkflowManagerApplyDeployment) (*WorkflowManagerDeployment, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req, err := newWorkflowManagerApplyDeploymentRequest(input)
	if err != nil {
		return nil, err
	}
	req.InvocationToken = c.invocationToken
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = c.idempotencyKey
	}
	resp, err := c.client.ApplyDeployment(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowManagerDeploymentFromProto(resp)
}

// GetDeployment fetches one workflow deployment.
func (c *WorkflowManagerClient) GetDeployment(ctx context.Context, input WorkflowManagerGetDeployment) (*WorkflowManagerDeployment, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req := newWorkflowManagerGetDeploymentRequest(input)
	req.InvocationToken = c.invocationToken
	resp, err := c.client.GetDeployment(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowManagerDeploymentFromProto(resp)
}

// ListDeployments lists workflow deployments.
func (c *WorkflowManagerClient) ListDeployments(ctx context.Context, input WorkflowManagerListDeployments) (*WorkflowManagerListDeploymentsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req := newWorkflowManagerListDeploymentsRequest(input)
	req.InvocationToken = c.invocationToken
	resp, err := c.client.ListDeployments(ctx, req)
	if err != nil {
		return nil, err
	}
	deployments, err := workflowManagerDeploymentsFromProto(resp.GetDeployments())
	if err != nil {
		return nil, err
	}
	return &WorkflowManagerListDeploymentsResponse{Deployments: deployments}, nil
}

// DeleteDeployment deletes a workflow deployment.
func (c *WorkflowManagerClient) DeleteDeployment(ctx context.Context, input WorkflowManagerDeleteDeployment) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("workflow manager: client is not initialized")
	}
	req := newWorkflowManagerDeleteDeploymentRequest(input)
	req.InvocationToken = c.invocationToken
	_, err := c.client.DeleteDeployment(ctx, req)
	return err
}

// SetDeploymentPaused pauses or resumes a workflow deployment.
func (c *WorkflowManagerClient) SetDeploymentPaused(ctx context.Context, input WorkflowManagerSetDeploymentPaused) (*WorkflowManagerDeployment, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req := newWorkflowManagerSetDeploymentPausedRequest(input)
	req.InvocationToken = c.invocationToken
	resp, err := c.client.SetDeploymentPaused(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowManagerDeploymentFromProto(resp)
}

// SetActivationPaused pauses or resumes one workflow activation.
func (c *WorkflowManagerClient) SetActivationPaused(ctx context.Context, input WorkflowManagerSetActivationPaused) (*WorkflowManagerDeployment, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req := newWorkflowManagerSetActivationPausedRequest(input)
	req.InvocationToken = c.invocationToken
	resp, err := c.client.SetActivationPaused(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowManagerDeploymentFromProto(resp)
}

// StartRun starts a workflow run.
func (c *WorkflowManagerClient) StartRun(ctx context.Context, input WorkflowManagerStartRun) (*WorkflowManagerRun, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req, err := newWorkflowManagerStartRunRequest(input)
	if err != nil {
		return nil, err
	}
	req.InvocationToken = c.invocationToken
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = c.idempotencyKey
	}
	resp, err := c.client.StartRun(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowManagerRunFromProto(resp)
}

// SignalRun signals an existing workflow run.
func (c *WorkflowManagerClient) SignalRun(ctx context.Context, input WorkflowManagerSignalRun) (*WorkflowManagerRunSignal, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req, err := newWorkflowManagerSignalRunRequest(input)
	if err != nil {
		return nil, err
	}
	req.InvocationToken = c.invocationToken
	resp, err := c.client.SignalRun(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowManagerRunSignalFromProto(resp)
}

// SignalOrStartRun signals a run or starts it when no matching run exists.
func (c *WorkflowManagerClient) SignalOrStartRun(ctx context.Context, input WorkflowManagerSignalOrStartRun) (*WorkflowManagerRunSignal, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req, err := newWorkflowManagerSignalOrStartRunRequest(input)
	if err != nil {
		return nil, err
	}
	req.InvocationToken = c.invocationToken
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = c.idempotencyKey
	}
	resp, err := c.client.SignalOrStartRun(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowManagerRunSignalFromProto(resp)
}

// CancelRun cancels a workflow run.
func (c *WorkflowManagerClient) CancelRun(ctx context.Context, input WorkflowManagerCancelRun) (*WorkflowManagerRun, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req := newWorkflowManagerCancelRunRequest(input)
	req.InvocationToken = c.invocationToken
	resp, err := c.client.CancelRun(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowManagerRunFromProto(resp)
}

// DeliverEvent delivers an event to matching workflow activations.
func (c *WorkflowManagerClient) DeliverEvent(ctx context.Context, input WorkflowManagerDeliverEvent) (*WorkflowManagerDeliverEventResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req, err := newWorkflowManagerDeliverEventRequest(input)
	if err != nil {
		return nil, err
	}
	req.InvocationToken = c.invocationToken
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = c.idempotencyKey
	}
	resp, err := c.client.DeliverEvent(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowManagerDeliverEventResponseFromProto(resp)
}
