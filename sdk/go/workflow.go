package gestalt

import (
	"context"
	"fmt"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

type workflow struct {
	client          proto.WorkflowProviderClient
	invocationToken string
	context         *proto.RequestContext
	idempotencyKey  string
}

// Workflow is the fakeable contract for applying definitions and starting or
// inspecting definition-backed workflow runs.
type Workflow interface {
	Close() error
	ApplyDefinition(context.Context, WorkflowApplyDefinition) (*WorkflowDefinition, error)
	GetDefinition(context.Context, WorkflowGetDefinition) (*WorkflowDefinition, error)
	ListDefinitions(context.Context, WorkflowListDefinitions) ([]WorkflowDefinition, error)
	SetDefinitionPaused(context.Context, WorkflowSetDefinitionPaused) (*WorkflowDefinition, error)
	SetActivationPaused(context.Context, WorkflowSetActivationPaused) (*WorkflowDefinition, error)
	DeleteDefinition(context.Context, WorkflowDeleteDefinition) error
	StartRun(context.Context, WorkflowStartRun) (*WorkflowRun, error)
	GetRun(context.Context, WorkflowGetRun) (*WorkflowRun, error)
	GetRunEvents(context.Context, WorkflowGetRunEvents) ([]WorkflowRunEvent, error)
	GetRunOutput(context.Context, WorkflowGetRunOutput) (any, error)
	SignalRun(context.Context, WorkflowSignalRun) (*WorkflowRunSignal, error)
	SignalOrStartRun(context.Context, WorkflowSignalOrStartRun) (*WorkflowRunSignal, error)
	DeliverEvent(context.Context, WorkflowDeliverEvent) (*WorkflowEvent, error)
}

var sharedWorkflowTransport sharedManagerTransport[proto.WorkflowProviderClient]

// NewWorkflow returns a capability that attaches invocationToken to every request.
func NewWorkflow(invocationToken string) (Workflow, error) {
	return newWorkflow(nil, strings.TrimSpace(invocationToken), true)
}

// NewWorkflowFromRequest returns a workflow capability for the current
// provider request.
func NewWorkflowFromRequest(req Request) (Workflow, error) {
	return newWorkflow(requestContextForRequest(req), req.InvocationToken(), false)
}

func newWorkflow(reqCtx *proto.RequestContext, invocationToken string, requireToken bool) (*workflow, error) {
	invocationToken = strings.TrimSpace(invocationToken)
	if requireToken && invocationToken == "" {
		return nil, fmt.Errorf("workflow: invocation token is not available")
	}
	target, token, err := hostServiceTarget("workflow")
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := managerTransportClient(ctx, "workflow", target, token, &sharedWorkflowTransport, proto.NewWorkflowProviderClient)
	if err != nil {
		return nil, err
	}

	return &workflow{
		client:          client,
		invocationToken: invocationToken,
		context:         cloneRequestContext(reqCtx),
	}, nil
}

// WorkflowFromContext returns a Workflow using context metadata.
func WorkflowFromContext(ctx context.Context) (Workflow, error) {
	client, err := newWorkflow(requestContextFromContext(ctx), InvocationTokenFromContext(ctx), false)
	if err != nil {
		return nil, err
	}
	client.idempotencyKey = IdempotencyKeyFromContext(ctx)
	return client, nil
}

func (c *workflow) Close() error {
	return nil
}

func (c *workflow) attachAuth(req protoMessage) {
	attachHostServiceAuth(req, c.context, c.invocationToken)
}

func (c *workflow) ApplyDefinition(ctx context.Context, input WorkflowApplyDefinition) (*WorkflowDefinition, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow: client is not initialized")
	}
	req, err := newWorkflowApplyDefinitionRequest(input)
	if err != nil {
		return nil, err
	}
	c.attachAuth(req)
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = c.idempotencyKey
	}
	resp, err := c.client.ApplyDefinition(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowDefinitionPtrFromProto(resp)
}

func (c *workflow) GetDefinition(ctx context.Context, input WorkflowGetDefinition) (*WorkflowDefinition, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow: client is not initialized")
	}
	req := newWorkflowGetDefinitionRequest(input)
	c.attachAuth(req)
	resp, err := c.client.GetDefinition(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowDefinitionPtrFromProto(resp)
}

func (c *workflow) ListDefinitions(ctx context.Context, input WorkflowListDefinitions) ([]WorkflowDefinition, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow: client is not initialized")
	}
	req := newWorkflowListDefinitionsRequest(input)
	c.attachAuth(req)
	resp, err := c.client.ListDefinitions(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowDefinitionsFromProto(resp.GetDefinitions())
}

func (c *workflow) SetDefinitionPaused(ctx context.Context, input WorkflowSetDefinitionPaused) (*WorkflowDefinition, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow: client is not initialized")
	}
	req := newWorkflowSetDefinitionPausedRequest(input)
	c.attachAuth(req)
	resp, err := c.client.SetDefinitionPaused(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowDefinitionPtrFromProto(resp)
}

func (c *workflow) SetActivationPaused(ctx context.Context, input WorkflowSetActivationPaused) (*WorkflowDefinition, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow: client is not initialized")
	}
	req := newWorkflowSetActivationPausedRequest(input)
	c.attachAuth(req)
	resp, err := c.client.SetActivationPaused(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowDefinitionPtrFromProto(resp)
}

func (c *workflow) DeleteDefinition(ctx context.Context, input WorkflowDeleteDefinition) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("workflow: client is not initialized")
	}
	req := newWorkflowDeleteDefinitionRequest(input)
	c.attachAuth(req)
	_, err := c.client.DeleteDefinition(ctx, req)
	return err
}

func (c *workflow) StartRun(ctx context.Context, input WorkflowStartRun) (*WorkflowRun, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow: client is not initialized")
	}
	req, err := newWorkflowStartRunRequest(input)
	if err != nil {
		return nil, err
	}
	c.attachAuth(req)
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = c.idempotencyKey
	}
	resp, err := c.client.StartRun(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowRunPtrFromProto(resp)
}

func (c *workflow) GetRun(ctx context.Context, input WorkflowGetRun) (*WorkflowRun, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow: client is not initialized")
	}
	req := newWorkflowGetRunRequest(input)
	c.attachAuth(req)
	resp, err := c.client.GetRun(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowRunPtrFromProto(resp)
}

func (c *workflow) GetRunEvents(ctx context.Context, input WorkflowGetRunEvents) ([]WorkflowRunEvent, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow: client is not initialized")
	}
	req := newWorkflowGetRunEventsRequest(input)
	c.attachAuth(req)
	resp, err := c.client.GetRunEvents(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowRunEventsFromProto(resp.GetEvents()), nil
}

func (c *workflow) GetRunOutput(ctx context.Context, input WorkflowGetRunOutput) (any, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow: client is not initialized")
	}
	req := newWorkflowGetRunOutputRequest(input)
	c.attachAuth(req)
	resp, err := c.client.GetRunOutput(ctx, req)
	if err != nil {
		return nil, err
	}
	return anyFromValue(resp.GetOutput()), nil
}

func (c *workflow) SignalRun(ctx context.Context, input WorkflowSignalRun) (*WorkflowRunSignal, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow: client is not initialized")
	}
	req, err := newWorkflowSignalRunRequest(input)
	if err != nil {
		return nil, err
	}
	c.attachAuth(req)
	resp, err := c.client.SignalRun(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowRunSignalFromProto(resp)
}

func (c *workflow) SignalOrStartRun(ctx context.Context, input WorkflowSignalOrStartRun) (*WorkflowRunSignal, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow: client is not initialized")
	}
	req, err := newWorkflowSignalOrStartRunRequest(input)
	if err != nil {
		return nil, err
	}
	c.attachAuth(req)
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = c.idempotencyKey
	}
	resp, err := c.client.SignalOrStartRun(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowRunSignalFromProto(resp)
}

func (c *workflow) DeliverEvent(ctx context.Context, input WorkflowDeliverEvent) (*WorkflowEvent, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow: client is not initialized")
	}
	req, err := newWorkflowDeliverEventRequest(input)
	if err != nil {
		return nil, err
	}
	c.attachAuth(req)
	resp, err := c.client.DeliverEvent(ctx, req)
	if err != nil {
		return nil, err
	}
	event := workflowEventFromProto(resp)
	return &event, nil
}
