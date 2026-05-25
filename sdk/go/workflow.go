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
	idempotencyKey  string
}

// Workflow is the fakeable contract for starting workflow runs and managing definitions, schedules, and event triggers.
type Workflow interface {
	Close() error
	StartRun(context.Context, WorkflowStartRun) (*WorkflowRun, error)
	SignalRun(context.Context, WorkflowSignalRun) (*WorkflowRunSignal, error)
	SignalOrStartRun(context.Context, WorkflowSignalOrStartRun) (*WorkflowRunSignal, error)
	CreateDefinition(context.Context, WorkflowCreateDefinition) (*WorkflowDefinition, error)
	GetDefinition(context.Context, WorkflowGetDefinition) (*WorkflowDefinition, error)
	UpdateDefinition(context.Context, WorkflowUpdateDefinition) (*WorkflowDefinition, error)
	DeleteDefinition(context.Context, WorkflowDeleteDefinition) error
	CreateSchedule(context.Context, WorkflowCreateSchedule) (*WorkflowSchedule, error)
	GetSchedule(context.Context, WorkflowGetSchedule) (*WorkflowSchedule, error)
	UpdateSchedule(context.Context, WorkflowUpdateSchedule) (*WorkflowSchedule, error)
	DeleteSchedule(context.Context, WorkflowDeleteSchedule) error
	PauseSchedule(context.Context, WorkflowPauseSchedule) (*WorkflowSchedule, error)
	ResumeSchedule(context.Context, WorkflowResumeSchedule) (*WorkflowSchedule, error)
	CreateTrigger(context.Context, WorkflowCreateEventTrigger) (*WorkflowEventTrigger, error)
	GetTrigger(context.Context, WorkflowGetEventTrigger) (*WorkflowEventTrigger, error)
	UpdateTrigger(context.Context, WorkflowUpdateEventTrigger) (*WorkflowEventTrigger, error)
	DeleteTrigger(context.Context, WorkflowDeleteEventTrigger) error
	PauseTrigger(context.Context, WorkflowPauseEventTrigger) (*WorkflowEventTrigger, error)
	ResumeTrigger(context.Context, WorkflowResumeEventTrigger) (*WorkflowEventTrigger, error)
	PublishEvent(context.Context, WorkflowPublishEvent) (*WorkflowEvent, error)
}

var sharedWorkflowTransport sharedManagerTransport[proto.WorkflowProviderClient]

// NewWorkflow returns a capability that attaches invocationToken to every request.
func NewWorkflow(invocationToken string) (Workflow, error) {
	return newWorkflow(invocationToken)
}

func newWorkflow(invocationToken string) (*workflow, error) {
	if strings.TrimSpace(invocationToken) == "" {
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

	return &workflow{client: client, invocationToken: strings.TrimSpace(invocationToken)}, nil
}

// WorkflowFromContext returns a Workflow using context metadata.
func WorkflowFromContext(ctx context.Context) (Workflow, error) {
	client, err := newWorkflow(InvocationTokenFromContext(ctx))
	if err != nil {
		return nil, err
	}
	client.idempotencyKey = IdempotencyKeyFromContext(ctx)
	return client, nil
}

// Close is a no-op because this capability uses shared transport.
func (c *workflow) Close() error {
	return nil
}

// StartRun starts a workflow run.
func (c *workflow) StartRun(ctx context.Context, input WorkflowStartRun) (*WorkflowRun, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow: client is not initialized")
	}
	req, err := newWorkflowStartRunRequest(input)
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
	return workflowRunFromProto(resp)
}

// SignalRun signals an existing workflow run.
func (c *workflow) SignalRun(ctx context.Context, input WorkflowSignalRun) (*WorkflowRunSignal, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow: client is not initialized")
	}
	req, err := newWorkflowSignalRunRequest(input)
	if err != nil {
		return nil, err
	}
	req.InvocationToken = c.invocationToken
	resp, err := c.client.SignalRun(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowRunSignalFromProto(resp)
}

// SignalOrStartRun signals a run or starts it when no matching run exists.
func (c *workflow) SignalOrStartRun(ctx context.Context, input WorkflowSignalOrStartRun) (*WorkflowRunSignal, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow: client is not initialized")
	}
	req, err := newWorkflowSignalOrStartRunRequest(input)
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
	return workflowRunSignalFromProto(resp)
}

// CreateDefinition creates a reusable workflow definition.
func (c *workflow) CreateDefinition(ctx context.Context, input WorkflowCreateDefinition) (*WorkflowDefinition, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow: client is not initialized")
	}
	req, err := newWorkflowCreateDefinitionRequest(input)
	if err != nil {
		return nil, err
	}
	req.InvocationToken = c.invocationToken
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = c.idempotencyKey
	}
	resp, err := c.client.CreateDefinition(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowDefinitionFromProto(resp)
}

// GetDefinition fetches one workflow definition.
func (c *workflow) GetDefinition(ctx context.Context, input WorkflowGetDefinition) (*WorkflowDefinition, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow: client is not initialized")
	}
	req := newWorkflowGetDefinitionRequest(input)
	req.InvocationToken = c.invocationToken
	resp, err := c.client.GetDefinition(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowDefinitionFromProto(resp)
}

// UpdateDefinition updates a workflow definition.
func (c *workflow) UpdateDefinition(ctx context.Context, input WorkflowUpdateDefinition) (*WorkflowDefinition, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow: client is not initialized")
	}
	req, err := newWorkflowUpdateDefinitionRequest(input)
	if err != nil {
		return nil, err
	}
	req.InvocationToken = c.invocationToken
	resp, err := c.client.UpdateDefinition(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowDefinitionFromProto(resp)
}

// DeleteDefinition deletes a workflow definition.
func (c *workflow) DeleteDefinition(ctx context.Context, input WorkflowDeleteDefinition) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("workflow: client is not initialized")
	}
	req := newWorkflowDeleteDefinitionRequest(input)
	req.InvocationToken = c.invocationToken
	_, err := c.client.DeleteDefinition(ctx, req)
	return err
}

// CreateSchedule creates a workflow schedule.
func (c *workflow) CreateSchedule(ctx context.Context, input WorkflowCreateSchedule) (*WorkflowSchedule, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow: client is not initialized")
	}
	req, err := newWorkflowCreateScheduleRequest(input)
	if err != nil {
		return nil, err
	}
	req.InvocationToken = c.invocationToken
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = c.idempotencyKey
	}
	resp, err := c.client.UpsertSchedule(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowScheduleFromProto(resp)
}

// GetSchedule fetches one workflow schedule.
func (c *workflow) GetSchedule(ctx context.Context, input WorkflowGetSchedule) (*WorkflowSchedule, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow: client is not initialized")
	}
	req := newWorkflowGetScheduleRequest(input)
	req.InvocationToken = c.invocationToken
	resp, err := c.client.GetSchedule(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowScheduleFromProto(resp)
}

// UpdateSchedule updates a workflow schedule.
func (c *workflow) UpdateSchedule(ctx context.Context, input WorkflowUpdateSchedule) (*WorkflowSchedule, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow: client is not initialized")
	}
	req, err := newWorkflowUpdateScheduleRequest(input)
	if err != nil {
		return nil, err
	}
	req.InvocationToken = c.invocationToken
	resp, err := c.client.UpsertSchedule(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowScheduleFromProto(resp)
}

// DeleteSchedule deletes a workflow schedule.
func (c *workflow) DeleteSchedule(ctx context.Context, input WorkflowDeleteSchedule) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("workflow: client is not initialized")
	}
	req := newWorkflowDeleteScheduleRequest(input)
	req.InvocationToken = c.invocationToken
	_, err := c.client.DeleteSchedule(ctx, req)
	return err
}

// PauseSchedule pauses a workflow schedule.
func (c *workflow) PauseSchedule(ctx context.Context, input WorkflowPauseSchedule) (*WorkflowSchedule, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow: client is not initialized")
	}
	req := newWorkflowPauseScheduleRequest(input)
	req.InvocationToken = c.invocationToken
	resp, err := c.client.PauseSchedule(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowScheduleFromProto(resp)
}

// ResumeSchedule resumes a workflow schedule.
func (c *workflow) ResumeSchedule(ctx context.Context, input WorkflowResumeSchedule) (*WorkflowSchedule, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow: client is not initialized")
	}
	req := newWorkflowResumeScheduleRequest(input)
	req.InvocationToken = c.invocationToken
	resp, err := c.client.ResumeSchedule(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowScheduleFromProto(resp)
}

// CreateTrigger creates an event trigger.
func (c *workflow) CreateTrigger(ctx context.Context, input WorkflowCreateEventTrigger) (*WorkflowEventTrigger, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow: client is not initialized")
	}
	req, err := newWorkflowCreateEventTriggerRequest(input)
	if err != nil {
		return nil, err
	}
	req.InvocationToken = c.invocationToken
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = c.idempotencyKey
	}
	resp, err := c.client.UpsertEventTrigger(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowEventTriggerFromProto(resp)
}

// GetTrigger fetches one event trigger.
func (c *workflow) GetTrigger(ctx context.Context, input WorkflowGetEventTrigger) (*WorkflowEventTrigger, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow: client is not initialized")
	}
	req := newWorkflowGetEventTriggerRequest(input)
	req.InvocationToken = c.invocationToken
	resp, err := c.client.GetEventTrigger(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowEventTriggerFromProto(resp)
}

// UpdateTrigger updates an event trigger.
func (c *workflow) UpdateTrigger(ctx context.Context, input WorkflowUpdateEventTrigger) (*WorkflowEventTrigger, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow: client is not initialized")
	}
	req, err := newWorkflowUpdateEventTriggerRequest(input)
	if err != nil {
		return nil, err
	}
	req.InvocationToken = c.invocationToken
	resp, err := c.client.UpsertEventTrigger(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowEventTriggerFromProto(resp)
}

// DeleteTrigger deletes an event trigger.
func (c *workflow) DeleteTrigger(ctx context.Context, input WorkflowDeleteEventTrigger) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("workflow: client is not initialized")
	}
	req := newWorkflowDeleteEventTriggerRequest(input)
	req.InvocationToken = c.invocationToken
	_, err := c.client.DeleteEventTrigger(ctx, req)
	return err
}

// PauseTrigger pauses an event trigger.
func (c *workflow) PauseTrigger(ctx context.Context, input WorkflowPauseEventTrigger) (*WorkflowEventTrigger, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow: client is not initialized")
	}
	req := newWorkflowPauseEventTriggerRequest(input)
	req.InvocationToken = c.invocationToken
	resp, err := c.client.PauseEventTrigger(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowEventTriggerFromProto(resp)
}

// ResumeTrigger resumes an event trigger.
func (c *workflow) ResumeTrigger(ctx context.Context, input WorkflowResumeEventTrigger) (*WorkflowEventTrigger, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow: client is not initialized")
	}
	req := newWorkflowResumeEventTriggerRequest(input)
	req.InvocationToken = c.invocationToken
	resp, err := c.client.ResumeEventTrigger(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowEventTriggerFromProto(resp)
}

// PublishEvent publishes an event into the workflow.
func (c *workflow) PublishEvent(ctx context.Context, input WorkflowPublishEvent) (*WorkflowEvent, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow: client is not initialized")
	}
	req, err := newWorkflowPublishEventRequest(input)
	if err != nil {
		return nil, err
	}
	req.InvocationToken = c.invocationToken
	resp, err := c.client.PublishEvent(ctx, req)
	if err != nil {
		return nil, err
	}
	event := workflowEventFromProto(resp)
	return &event, nil
}
