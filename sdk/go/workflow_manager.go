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

// WorkflowManagerClient manages workflow definitions and runs.
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

// ApplyDefinition creates or updates a workflow definition.
func (c *WorkflowManagerClient) ApplyDefinition(ctx context.Context, input WorkflowManagerApplyDefinition) (*WorkflowManagerDefinition, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req, err := newWorkflowManagerApplyDefinitionRequest(input)
	if err != nil {
		return nil, err
	}
	req.InvocationToken = c.invocationToken
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = c.idempotencyKey
	}
	resp, err := c.client.ApplyDefinition(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowManagerDefinitionFromProto(resp)
}

// GetDefinition fetches one workflow definition.
func (c *WorkflowManagerClient) GetDefinition(ctx context.Context, input WorkflowManagerGetDefinition) (*WorkflowManagerDefinition, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req := newWorkflowManagerGetDefinitionRequest(input)
	req.InvocationToken = c.invocationToken
	resp, err := c.client.GetDefinition(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowManagerDefinitionFromProto(resp)
}

// ListDefinitions lists workflow definitions.
func (c *WorkflowManagerClient) ListDefinitions(ctx context.Context, input WorkflowManagerListDefinitions) (*WorkflowManagerListDefinitionsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req := newWorkflowManagerListDefinitionsRequest(input)
	req.InvocationToken = c.invocationToken
	resp, err := c.client.ListDefinitions(ctx, req)
	if err != nil {
		return nil, err
	}
	definitions, err := workflowManagerDefinitionsFromProto(resp.GetDefinitions())
	if err != nil {
		return nil, err
	}
	return &WorkflowManagerListDefinitionsResponse{Definitions: definitions}, nil
}

// DeleteDefinition deletes a workflow definition.
func (c *WorkflowManagerClient) DeleteDefinition(ctx context.Context, input WorkflowManagerDeleteDefinition) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("workflow manager: client is not initialized")
	}
	req := newWorkflowManagerDeleteDefinitionRequest(input)
	req.InvocationToken = c.invocationToken
	_, err := c.client.DeleteDefinition(ctx, req)
	return err
}

// SetDefinitionPaused pauses or resumes a workflow definition.
func (c *WorkflowManagerClient) SetDefinitionPaused(ctx context.Context, input WorkflowManagerSetDefinitionPaused) (*WorkflowManagerDefinition, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req := newWorkflowManagerSetDefinitionPausedRequest(input)
	req.InvocationToken = c.invocationToken
	resp, err := c.client.SetDefinitionPaused(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowManagerDefinitionFromProto(resp)
}

// SetActivationPaused pauses or resumes one workflow activation.
func (c *WorkflowManagerClient) SetActivationPaused(ctx context.Context, input WorkflowManagerSetActivationPaused) (*WorkflowManagerDefinition, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req := newWorkflowManagerSetActivationPausedRequest(input)
	req.InvocationToken = c.invocationToken
	resp, err := c.client.SetActivationPaused(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowManagerDefinitionFromProto(resp)
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
