package gestalt

import (
	"context"
	"fmt"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

type workflowManager struct {
	client          proto.WorkflowProviderClient
	invocationToken string
	idempotencyKey  string
}

// WorkflowManager is the fakeable contract for starting workflow runs and managing definitions, schedules, and event triggers.
type WorkflowManagerAPI interface {
	Close() error
	StartRun(context.Context, WorkflowManagerStartRun) (*WorkflowManagerRun, error)
	SignalRun(context.Context, WorkflowManagerSignalRun) (*WorkflowManagerRunSignal, error)
	SignalOrStartRun(context.Context, WorkflowManagerSignalOrStartRun) (*WorkflowManagerRunSignal, error)
	CreateDefinition(context.Context, WorkflowManagerCreateDefinition) (*WorkflowManagerDefinition, error)
	GetDefinition(context.Context, WorkflowManagerGetDefinition) (*WorkflowManagerDefinition, error)
	UpdateDefinition(context.Context, WorkflowManagerUpdateDefinition) (*WorkflowManagerDefinition, error)
	DeleteDefinition(context.Context, WorkflowManagerDeleteDefinition) error
	CreateSchedule(context.Context, WorkflowManagerCreateSchedule) (*WorkflowManagerSchedule, error)
	GetSchedule(context.Context, WorkflowManagerGetSchedule) (*WorkflowManagerSchedule, error)
	UpdateSchedule(context.Context, WorkflowManagerUpdateSchedule) (*WorkflowManagerSchedule, error)
	DeleteSchedule(context.Context, WorkflowManagerDeleteSchedule) error
	PauseSchedule(context.Context, WorkflowManagerPauseSchedule) (*WorkflowManagerSchedule, error)
	ResumeSchedule(context.Context, WorkflowManagerResumeSchedule) (*WorkflowManagerSchedule, error)
	CreateTrigger(context.Context, WorkflowManagerCreateEventTrigger) (*WorkflowManagerEventTrigger, error)
	GetTrigger(context.Context, WorkflowManagerGetEventTrigger) (*WorkflowManagerEventTrigger, error)
	UpdateTrigger(context.Context, WorkflowManagerUpdateEventTrigger) (*WorkflowManagerEventTrigger, error)
	DeleteTrigger(context.Context, WorkflowManagerDeleteEventTrigger) error
	PauseTrigger(context.Context, WorkflowManagerPauseEventTrigger) (*WorkflowManagerEventTrigger, error)
	ResumeTrigger(context.Context, WorkflowManagerResumeEventTrigger) (*WorkflowManagerEventTrigger, error)
	PublishEvent(context.Context, WorkflowManagerPublishEvent) (*WorkflowEvent, error)
}

var sharedWorkflowManagerTransport sharedManagerTransport[proto.WorkflowProviderClient]

// WorkflowManager returns a capability that attaches invocationToken to every request.
func WorkflowManager(invocationToken string) (WorkflowManagerAPI, error) {
	return newWorkflowManager(invocationToken)
}

func newWorkflowManager(invocationToken string) (*workflowManager, error) {
	if strings.TrimSpace(invocationToken) == "" {
		return nil, fmt.Errorf("workflow manager: invocation token is not available")
	}
	target, token, err := hostServiceTarget("workflow manager")
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := managerTransportClient(ctx, "workflow manager", target, token, &sharedWorkflowManagerTransport, proto.NewWorkflowProviderClient)
	if err != nil {
		return nil, err
	}

	return &workflowManager{client: client, invocationToken: strings.TrimSpace(invocationToken)}, nil
}

// WorkflowManagerFromContext returns a WorkflowManager using context metadata.
func WorkflowManagerFromContext(ctx context.Context) (WorkflowManagerAPI, error) {
	client, err := newWorkflowManager(InvocationTokenFromContext(ctx))
	if err != nil {
		return nil, err
	}
	client.idempotencyKey = IdempotencyKeyFromContext(ctx)
	return client, nil
}

// Close is a no-op because this capability uses shared transport.
func (c *workflowManager) Close() error {
	return nil
}

// StartRun starts a workflow run.
func (c *workflowManager) StartRun(ctx context.Context, input WorkflowManagerStartRun) (*WorkflowManagerRun, error) {
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
func (c *workflowManager) SignalRun(ctx context.Context, input WorkflowManagerSignalRun) (*WorkflowManagerRunSignal, error) {
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
func (c *workflowManager) SignalOrStartRun(ctx context.Context, input WorkflowManagerSignalOrStartRun) (*WorkflowManagerRunSignal, error) {
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

// CreateDefinition creates a reusable workflow definition.
func (c *workflowManager) CreateDefinition(ctx context.Context, input WorkflowManagerCreateDefinition) (*WorkflowManagerDefinition, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req, err := newWorkflowManagerCreateDefinitionRequest(input)
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
	return workflowManagerDefinitionFromProto(resp)
}

// GetDefinition fetches one workflow definition.
func (c *workflowManager) GetDefinition(ctx context.Context, input WorkflowManagerGetDefinition) (*WorkflowManagerDefinition, error) {
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

// UpdateDefinition updates a workflow definition.
func (c *workflowManager) UpdateDefinition(ctx context.Context, input WorkflowManagerUpdateDefinition) (*WorkflowManagerDefinition, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req, err := newWorkflowManagerUpdateDefinitionRequest(input)
	if err != nil {
		return nil, err
	}
	req.InvocationToken = c.invocationToken
	resp, err := c.client.UpdateDefinition(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowManagerDefinitionFromProto(resp)
}

// DeleteDefinition deletes a workflow definition.
func (c *workflowManager) DeleteDefinition(ctx context.Context, input WorkflowManagerDeleteDefinition) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("workflow manager: client is not initialized")
	}
	req := newWorkflowManagerDeleteDefinitionRequest(input)
	req.InvocationToken = c.invocationToken
	_, err := c.client.DeleteDefinition(ctx, req)
	return err
}

// CreateSchedule creates a workflow schedule.
func (c *workflowManager) CreateSchedule(ctx context.Context, input WorkflowManagerCreateSchedule) (*WorkflowManagerSchedule, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req, err := newWorkflowManagerCreateScheduleRequest(input)
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
	return workflowManagerScheduleFromProto(resp)
}

// GetSchedule fetches one workflow schedule.
func (c *workflowManager) GetSchedule(ctx context.Context, input WorkflowManagerGetSchedule) (*WorkflowManagerSchedule, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req := newWorkflowManagerGetScheduleRequest(input)
	req.InvocationToken = c.invocationToken
	resp, err := c.client.GetSchedule(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowManagerScheduleFromProto(resp)
}

// UpdateSchedule updates a workflow schedule.
func (c *workflowManager) UpdateSchedule(ctx context.Context, input WorkflowManagerUpdateSchedule) (*WorkflowManagerSchedule, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req, err := newWorkflowManagerUpdateScheduleRequest(input)
	if err != nil {
		return nil, err
	}
	req.InvocationToken = c.invocationToken
	resp, err := c.client.UpsertSchedule(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowManagerScheduleFromProto(resp)
}

// DeleteSchedule deletes a workflow schedule.
func (c *workflowManager) DeleteSchedule(ctx context.Context, input WorkflowManagerDeleteSchedule) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("workflow manager: client is not initialized")
	}
	req := newWorkflowManagerDeleteScheduleRequest(input)
	req.InvocationToken = c.invocationToken
	_, err := c.client.DeleteSchedule(ctx, req)
	return err
}

// PauseSchedule pauses a workflow schedule.
func (c *workflowManager) PauseSchedule(ctx context.Context, input WorkflowManagerPauseSchedule) (*WorkflowManagerSchedule, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req := newWorkflowManagerPauseScheduleRequest(input)
	req.InvocationToken = c.invocationToken
	resp, err := c.client.PauseSchedule(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowManagerScheduleFromProto(resp)
}

// ResumeSchedule resumes a workflow schedule.
func (c *workflowManager) ResumeSchedule(ctx context.Context, input WorkflowManagerResumeSchedule) (*WorkflowManagerSchedule, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req := newWorkflowManagerResumeScheduleRequest(input)
	req.InvocationToken = c.invocationToken
	resp, err := c.client.ResumeSchedule(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowManagerScheduleFromProto(resp)
}

// CreateTrigger creates an event trigger.
func (c *workflowManager) CreateTrigger(ctx context.Context, input WorkflowManagerCreateEventTrigger) (*WorkflowManagerEventTrigger, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req, err := newWorkflowManagerCreateEventTriggerRequest(input)
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
	return workflowManagerEventTriggerFromProto(resp)
}

// GetTrigger fetches one event trigger.
func (c *workflowManager) GetTrigger(ctx context.Context, input WorkflowManagerGetEventTrigger) (*WorkflowManagerEventTrigger, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req := newWorkflowManagerGetEventTriggerRequest(input)
	req.InvocationToken = c.invocationToken
	resp, err := c.client.GetEventTrigger(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowManagerEventTriggerFromProto(resp)
}

// UpdateTrigger updates an event trigger.
func (c *workflowManager) UpdateTrigger(ctx context.Context, input WorkflowManagerUpdateEventTrigger) (*WorkflowManagerEventTrigger, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req, err := newWorkflowManagerUpdateEventTriggerRequest(input)
	if err != nil {
		return nil, err
	}
	req.InvocationToken = c.invocationToken
	resp, err := c.client.UpsertEventTrigger(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowManagerEventTriggerFromProto(resp)
}

// DeleteTrigger deletes an event trigger.
func (c *workflowManager) DeleteTrigger(ctx context.Context, input WorkflowManagerDeleteEventTrigger) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("workflow manager: client is not initialized")
	}
	req := newWorkflowManagerDeleteEventTriggerRequest(input)
	req.InvocationToken = c.invocationToken
	_, err := c.client.DeleteEventTrigger(ctx, req)
	return err
}

// PauseTrigger pauses an event trigger.
func (c *workflowManager) PauseTrigger(ctx context.Context, input WorkflowManagerPauseEventTrigger) (*WorkflowManagerEventTrigger, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req := newWorkflowManagerPauseEventTriggerRequest(input)
	req.InvocationToken = c.invocationToken
	resp, err := c.client.PauseEventTrigger(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowManagerEventTriggerFromProto(resp)
}

// ResumeTrigger resumes an event trigger.
func (c *workflowManager) ResumeTrigger(ctx context.Context, input WorkflowManagerResumeEventTrigger) (*WorkflowManagerEventTrigger, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req := newWorkflowManagerResumeEventTriggerRequest(input)
	req.InvocationToken = c.invocationToken
	resp, err := c.client.ResumeEventTrigger(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowManagerEventTriggerFromProto(resp)
}

// PublishEvent publishes an event into the workflow manager.
func (c *workflowManager) PublishEvent(ctx context.Context, input WorkflowManagerPublishEvent) (*WorkflowEvent, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req, err := newWorkflowManagerPublishEventRequest(input)
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
