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

// WorkflowManagerClient starts runs and manages workflow schedules or triggers.
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

// StartRun starts a workflow run.
func (c *WorkflowManagerClient) StartRun(ctx context.Context, input WorkflowManagerStartRunInput) (*proto.ManagedWorkflowRun, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req, err := NewWorkflowManagerStartRunRequest(input)
	if err != nil {
		return nil, err
	}
	req.InvocationToken = c.invocationToken
	return c.client.StartRun(ctx, req)
}

// SignalRun signals an existing workflow run.
func (c *WorkflowManagerClient) SignalRun(ctx context.Context, input WorkflowManagerSignalRunInput) (*proto.ManagedWorkflowRunSignal, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req, err := NewWorkflowManagerSignalRunRequest(input)
	if err != nil {
		return nil, err
	}
	req.InvocationToken = c.invocationToken
	return c.client.SignalRun(ctx, req)
}

// SignalOrStartRun signals a run or starts it when no matching run exists.
func (c *WorkflowManagerClient) SignalOrStartRun(ctx context.Context, input WorkflowManagerSignalOrStartRunInput) (*proto.ManagedWorkflowRunSignal, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req, err := NewWorkflowManagerSignalOrStartRunRequest(input)
	if err != nil {
		return nil, err
	}
	req.InvocationToken = c.invocationToken
	return c.client.SignalOrStartRun(ctx, req)
}

// CreateDefinition creates a reusable workflow definition.
func (c *WorkflowManagerClient) CreateDefinition(ctx context.Context, input WorkflowManagerCreateDefinitionInput) (*proto.ManagedWorkflowDefinition, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req, err := NewWorkflowManagerCreateDefinitionRequest(input)
	if err != nil {
		return nil, err
	}
	req.InvocationToken = c.invocationToken
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = c.idempotencyKey
	}
	return c.client.CreateDefinition(ctx, req)
}

// GetDefinition fetches one workflow definition.
func (c *WorkflowManagerClient) GetDefinition(ctx context.Context, input WorkflowManagerGetDefinitionInput) (*proto.ManagedWorkflowDefinition, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req := NewWorkflowManagerGetDefinitionRequest(input)
	req.InvocationToken = c.invocationToken
	return c.client.GetDefinition(ctx, req)
}

// UpdateDefinition updates a workflow definition.
func (c *WorkflowManagerClient) UpdateDefinition(ctx context.Context, input WorkflowManagerUpdateDefinitionInput) (*proto.ManagedWorkflowDefinition, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req, err := NewWorkflowManagerUpdateDefinitionRequest(input)
	if err != nil {
		return nil, err
	}
	req.InvocationToken = c.invocationToken
	return c.client.UpdateDefinition(ctx, req)
}

// DeleteDefinition deletes a workflow definition.
func (c *WorkflowManagerClient) DeleteDefinition(ctx context.Context, input WorkflowManagerDeleteDefinitionInput) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("workflow manager: client is not initialized")
	}
	req := NewWorkflowManagerDeleteDefinitionRequest(input)
	req.InvocationToken = c.invocationToken
	_, err := c.client.DeleteDefinition(ctx, req)
	return err
}

// CreateSchedule creates a workflow schedule.
func (c *WorkflowManagerClient) CreateSchedule(ctx context.Context, input WorkflowManagerCreateScheduleInput) (*proto.ManagedWorkflowSchedule, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req, err := NewWorkflowManagerCreateScheduleRequest(input)
	if err != nil {
		return nil, err
	}
	req.InvocationToken = c.invocationToken
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = c.idempotencyKey
	}
	return c.client.CreateSchedule(ctx, req)
}

// GetSchedule fetches one workflow schedule.
func (c *WorkflowManagerClient) GetSchedule(ctx context.Context, input WorkflowManagerGetScheduleInput) (*proto.ManagedWorkflowSchedule, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req := NewWorkflowManagerGetScheduleRequest(input)
	req.InvocationToken = c.invocationToken
	return c.client.GetSchedule(ctx, req)
}

// UpdateSchedule updates a workflow schedule.
func (c *WorkflowManagerClient) UpdateSchedule(ctx context.Context, input WorkflowManagerUpdateScheduleInput) (*proto.ManagedWorkflowSchedule, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req, err := NewWorkflowManagerUpdateScheduleRequest(input)
	if err != nil {
		return nil, err
	}
	req.InvocationToken = c.invocationToken
	return c.client.UpdateSchedule(ctx, req)
}

// DeleteSchedule deletes a workflow schedule.
func (c *WorkflowManagerClient) DeleteSchedule(ctx context.Context, input WorkflowManagerDeleteScheduleInput) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("workflow manager: client is not initialized")
	}
	req := NewWorkflowManagerDeleteScheduleRequest(input)
	req.InvocationToken = c.invocationToken
	_, err := c.client.DeleteSchedule(ctx, req)
	return err
}

// PauseSchedule pauses a workflow schedule.
func (c *WorkflowManagerClient) PauseSchedule(ctx context.Context, input WorkflowManagerPauseScheduleInput) (*proto.ManagedWorkflowSchedule, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req := NewWorkflowManagerPauseScheduleRequest(input)
	req.InvocationToken = c.invocationToken
	return c.client.PauseSchedule(ctx, req)
}

// ResumeSchedule resumes a workflow schedule.
func (c *WorkflowManagerClient) ResumeSchedule(ctx context.Context, input WorkflowManagerResumeScheduleInput) (*proto.ManagedWorkflowSchedule, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req := NewWorkflowManagerResumeScheduleRequest(input)
	req.InvocationToken = c.invocationToken
	return c.client.ResumeSchedule(ctx, req)
}

// CreateTrigger creates an event trigger.
func (c *WorkflowManagerClient) CreateTrigger(ctx context.Context, input WorkflowManagerCreateEventTriggerInput) (*proto.ManagedWorkflowEventTrigger, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req, err := NewWorkflowManagerCreateEventTriggerRequest(input)
	if err != nil {
		return nil, err
	}
	req.InvocationToken = c.invocationToken
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = c.idempotencyKey
	}
	return c.client.CreateEventTrigger(ctx, req)
}

// GetTrigger fetches one event trigger.
func (c *WorkflowManagerClient) GetTrigger(ctx context.Context, input WorkflowManagerGetEventTriggerInput) (*proto.ManagedWorkflowEventTrigger, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req := NewWorkflowManagerGetEventTriggerRequest(input)
	req.InvocationToken = c.invocationToken
	return c.client.GetEventTrigger(ctx, req)
}

// UpdateTrigger updates an event trigger.
func (c *WorkflowManagerClient) UpdateTrigger(ctx context.Context, input WorkflowManagerUpdateEventTriggerInput) (*proto.ManagedWorkflowEventTrigger, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req, err := NewWorkflowManagerUpdateEventTriggerRequest(input)
	if err != nil {
		return nil, err
	}
	req.InvocationToken = c.invocationToken
	return c.client.UpdateEventTrigger(ctx, req)
}

// DeleteTrigger deletes an event trigger.
func (c *WorkflowManagerClient) DeleteTrigger(ctx context.Context, input WorkflowManagerDeleteEventTriggerInput) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("workflow manager: client is not initialized")
	}
	req := NewWorkflowManagerDeleteEventTriggerRequest(input)
	req.InvocationToken = c.invocationToken
	_, err := c.client.DeleteEventTrigger(ctx, req)
	return err
}

// PauseTrigger pauses an event trigger.
func (c *WorkflowManagerClient) PauseTrigger(ctx context.Context, input WorkflowManagerPauseEventTriggerInput) (*proto.ManagedWorkflowEventTrigger, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req := NewWorkflowManagerPauseEventTriggerRequest(input)
	req.InvocationToken = c.invocationToken
	return c.client.PauseEventTrigger(ctx, req)
}

// ResumeTrigger resumes an event trigger.
func (c *WorkflowManagerClient) ResumeTrigger(ctx context.Context, input WorkflowManagerResumeEventTriggerInput) (*proto.ManagedWorkflowEventTrigger, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req := NewWorkflowManagerResumeEventTriggerRequest(input)
	req.InvocationToken = c.invocationToken
	return c.client.ResumeEventTrigger(ctx, req)
}

// PublishEvent publishes an event into the workflow manager.
func (c *WorkflowManagerClient) PublishEvent(ctx context.Context, input WorkflowManagerPublishEventInput) (*proto.WorkflowEvent, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	req, err := NewWorkflowManagerPublishEventRequest(input)
	if err != nil {
		return nil, err
	}
	req.InvocationToken = c.invocationToken
	return c.client.PublishEvent(ctx, req)
}
