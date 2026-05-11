package gestalt

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	gproto "google.golang.org/protobuf/proto"
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
func (c *WorkflowManagerClient) StartRun(ctx context.Context, req *proto.WorkflowManagerStartRunRequest) (*proto.ManagedWorkflowRun, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	value := &proto.WorkflowManagerStartRunRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.WorkflowManagerStartRunRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.StartRun(ctx, value)
}

// StartRunWithInput starts a workflow run.
func (c *WorkflowManagerClient) StartRunWithInput(ctx context.Context, input WorkflowManagerStartRunInput) (*proto.ManagedWorkflowRun, error) {
	req, err := NewWorkflowManagerStartRunRequest(input)
	if err != nil {
		return nil, err
	}
	return c.StartRun(ctx, req)
}

// SignalRun signals an existing workflow run.
func (c *WorkflowManagerClient) SignalRun(ctx context.Context, req *proto.WorkflowManagerSignalRunRequest) (*proto.ManagedWorkflowRunSignal, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	value := &proto.WorkflowManagerSignalRunRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.WorkflowManagerSignalRunRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.SignalRun(ctx, value)
}

// SignalRunWithInput signals an existing workflow run.
func (c *WorkflowManagerClient) SignalRunWithInput(ctx context.Context, input WorkflowManagerSignalRunInput) (*proto.ManagedWorkflowRunSignal, error) {
	req, err := NewWorkflowManagerSignalRunRequest(input)
	if err != nil {
		return nil, err
	}
	return c.SignalRun(ctx, req)
}

// SignalOrStartRun signals a run or starts it when no matching run exists.
func (c *WorkflowManagerClient) SignalOrStartRun(ctx context.Context, req *proto.WorkflowManagerSignalOrStartRunRequest) (*proto.ManagedWorkflowRunSignal, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	value := &proto.WorkflowManagerSignalOrStartRunRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.WorkflowManagerSignalOrStartRunRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.SignalOrStartRun(ctx, value)
}

// SignalOrStartRunWithInput signals or starts a workflow run.
func (c *WorkflowManagerClient) SignalOrStartRunWithInput(ctx context.Context, input WorkflowManagerSignalOrStartRunInput) (*proto.ManagedWorkflowRunSignal, error) {
	req, err := NewWorkflowManagerSignalOrStartRunRequest(input)
	if err != nil {
		return nil, err
	}
	return c.SignalOrStartRun(ctx, req)
}

// CreateDefinition creates a reusable workflow definition.
func (c *WorkflowManagerClient) CreateDefinition(ctx context.Context, req *proto.WorkflowManagerCreateDefinitionRequest) (*proto.ManagedWorkflowDefinition, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	value := &proto.WorkflowManagerCreateDefinitionRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.WorkflowManagerCreateDefinitionRequest)
	}
	value.InvocationToken = c.invocationToken
	if value.IdempotencyKey == "" {
		value.IdempotencyKey = c.idempotencyKey
	}
	return c.client.CreateDefinition(ctx, value)
}

// CreateDefinitionWithInput creates a reusable workflow definition.
func (c *WorkflowManagerClient) CreateDefinitionWithInput(ctx context.Context, input WorkflowManagerCreateDefinitionInput) (*proto.ManagedWorkflowDefinition, error) {
	req, err := NewWorkflowManagerCreateDefinitionRequest(input)
	if err != nil {
		return nil, err
	}
	return c.CreateDefinition(ctx, req)
}

// GetDefinition fetches one workflow definition.
func (c *WorkflowManagerClient) GetDefinition(ctx context.Context, req *proto.WorkflowManagerGetDefinitionRequest) (*proto.ManagedWorkflowDefinition, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	value := &proto.WorkflowManagerGetDefinitionRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.WorkflowManagerGetDefinitionRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.GetDefinition(ctx, value)
}

// GetDefinitionWithInput fetches one workflow definition.
func (c *WorkflowManagerClient) GetDefinitionWithInput(ctx context.Context, input WorkflowManagerGetDefinitionInput) (*proto.ManagedWorkflowDefinition, error) {
	return c.GetDefinition(ctx, NewWorkflowManagerGetDefinitionRequest(input))
}

// UpdateDefinition updates a workflow definition.
func (c *WorkflowManagerClient) UpdateDefinition(ctx context.Context, req *proto.WorkflowManagerUpdateDefinitionRequest) (*proto.ManagedWorkflowDefinition, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	value := &proto.WorkflowManagerUpdateDefinitionRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.WorkflowManagerUpdateDefinitionRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.UpdateDefinition(ctx, value)
}

// UpdateDefinitionWithInput updates a workflow definition.
func (c *WorkflowManagerClient) UpdateDefinitionWithInput(ctx context.Context, input WorkflowManagerUpdateDefinitionInput) (*proto.ManagedWorkflowDefinition, error) {
	req, err := NewWorkflowManagerUpdateDefinitionRequest(input)
	if err != nil {
		return nil, err
	}
	return c.UpdateDefinition(ctx, req)
}

// DeleteDefinition deletes a workflow definition.
func (c *WorkflowManagerClient) DeleteDefinition(ctx context.Context, req *proto.WorkflowManagerDeleteDefinitionRequest) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("workflow manager: client is not initialized")
	}
	value := &proto.WorkflowManagerDeleteDefinitionRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.WorkflowManagerDeleteDefinitionRequest)
	}
	value.InvocationToken = c.invocationToken
	_, err := c.client.DeleteDefinition(ctx, value)
	return err
}

// DeleteDefinitionWithInput deletes a workflow definition.
func (c *WorkflowManagerClient) DeleteDefinitionWithInput(ctx context.Context, input WorkflowManagerDeleteDefinitionInput) error {
	return c.DeleteDefinition(ctx, NewWorkflowManagerDeleteDefinitionRequest(input))
}

// CreateSchedule creates a workflow schedule.
func (c *WorkflowManagerClient) CreateSchedule(ctx context.Context, req *proto.WorkflowManagerCreateScheduleRequest) (*proto.ManagedWorkflowSchedule, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	value := &proto.WorkflowManagerCreateScheduleRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.WorkflowManagerCreateScheduleRequest)
	}
	value.InvocationToken = c.invocationToken
	if value.IdempotencyKey == "" {
		value.IdempotencyKey = c.idempotencyKey
	}
	return c.client.CreateSchedule(ctx, value)
}

// CreateScheduleWithInput creates a workflow schedule.
func (c *WorkflowManagerClient) CreateScheduleWithInput(ctx context.Context, input WorkflowManagerCreateScheduleInput) (*proto.ManagedWorkflowSchedule, error) {
	req, err := NewWorkflowManagerCreateScheduleRequest(input)
	if err != nil {
		return nil, err
	}
	return c.CreateSchedule(ctx, req)
}

// GetSchedule fetches one workflow schedule.
func (c *WorkflowManagerClient) GetSchedule(ctx context.Context, req *proto.WorkflowManagerGetScheduleRequest) (*proto.ManagedWorkflowSchedule, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	value := &proto.WorkflowManagerGetScheduleRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.WorkflowManagerGetScheduleRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.GetSchedule(ctx, value)
}

// GetScheduleWithInput fetches one workflow schedule.
func (c *WorkflowManagerClient) GetScheduleWithInput(ctx context.Context, input WorkflowManagerGetScheduleInput) (*proto.ManagedWorkflowSchedule, error) {
	return c.GetSchedule(ctx, NewWorkflowManagerGetScheduleRequest(input))
}

// UpdateSchedule updates a workflow schedule.
func (c *WorkflowManagerClient) UpdateSchedule(ctx context.Context, req *proto.WorkflowManagerUpdateScheduleRequest) (*proto.ManagedWorkflowSchedule, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	value := &proto.WorkflowManagerUpdateScheduleRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.WorkflowManagerUpdateScheduleRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.UpdateSchedule(ctx, value)
}

// UpdateScheduleWithInput updates a workflow schedule.
func (c *WorkflowManagerClient) UpdateScheduleWithInput(ctx context.Context, input WorkflowManagerUpdateScheduleInput) (*proto.ManagedWorkflowSchedule, error) {
	req, err := NewWorkflowManagerUpdateScheduleRequest(input)
	if err != nil {
		return nil, err
	}
	return c.UpdateSchedule(ctx, req)
}

// DeleteSchedule deletes a workflow schedule.
func (c *WorkflowManagerClient) DeleteSchedule(ctx context.Context, req *proto.WorkflowManagerDeleteScheduleRequest) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("workflow manager: client is not initialized")
	}
	value := &proto.WorkflowManagerDeleteScheduleRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.WorkflowManagerDeleteScheduleRequest)
	}
	value.InvocationToken = c.invocationToken
	_, err := c.client.DeleteSchedule(ctx, value)
	return err
}

// DeleteScheduleWithInput deletes a workflow schedule.
func (c *WorkflowManagerClient) DeleteScheduleWithInput(ctx context.Context, input WorkflowManagerDeleteScheduleInput) error {
	return c.DeleteSchedule(ctx, NewWorkflowManagerDeleteScheduleRequest(input))
}

// PauseSchedule pauses a workflow schedule.
func (c *WorkflowManagerClient) PauseSchedule(ctx context.Context, req *proto.WorkflowManagerPauseScheduleRequest) (*proto.ManagedWorkflowSchedule, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	value := &proto.WorkflowManagerPauseScheduleRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.WorkflowManagerPauseScheduleRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.PauseSchedule(ctx, value)
}

// PauseScheduleWithInput pauses a workflow schedule.
func (c *WorkflowManagerClient) PauseScheduleWithInput(ctx context.Context, input WorkflowManagerPauseScheduleInput) (*proto.ManagedWorkflowSchedule, error) {
	return c.PauseSchedule(ctx, NewWorkflowManagerPauseScheduleRequest(input))
}

// ResumeSchedule resumes a workflow schedule.
func (c *WorkflowManagerClient) ResumeSchedule(ctx context.Context, req *proto.WorkflowManagerResumeScheduleRequest) (*proto.ManagedWorkflowSchedule, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	value := &proto.WorkflowManagerResumeScheduleRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.WorkflowManagerResumeScheduleRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.ResumeSchedule(ctx, value)
}

// ResumeScheduleWithInput resumes a workflow schedule.
func (c *WorkflowManagerClient) ResumeScheduleWithInput(ctx context.Context, input WorkflowManagerResumeScheduleInput) (*proto.ManagedWorkflowSchedule, error) {
	return c.ResumeSchedule(ctx, NewWorkflowManagerResumeScheduleRequest(input))
}

// CreateTrigger creates an event trigger.
func (c *WorkflowManagerClient) CreateTrigger(ctx context.Context, req *proto.WorkflowManagerCreateEventTriggerRequest) (*proto.ManagedWorkflowEventTrigger, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	value := &proto.WorkflowManagerCreateEventTriggerRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.WorkflowManagerCreateEventTriggerRequest)
	}
	value.InvocationToken = c.invocationToken
	if value.IdempotencyKey == "" {
		value.IdempotencyKey = c.idempotencyKey
	}
	return c.client.CreateEventTrigger(ctx, value)
}

// CreateTriggerWithInput creates an event trigger.
func (c *WorkflowManagerClient) CreateTriggerWithInput(ctx context.Context, input WorkflowManagerCreateEventTriggerInput) (*proto.ManagedWorkflowEventTrigger, error) {
	req, err := NewWorkflowManagerCreateEventTriggerRequest(input)
	if err != nil {
		return nil, err
	}
	return c.CreateTrigger(ctx, req)
}

// GetTrigger fetches one event trigger.
func (c *WorkflowManagerClient) GetTrigger(ctx context.Context, req *proto.WorkflowManagerGetEventTriggerRequest) (*proto.ManagedWorkflowEventTrigger, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	value := &proto.WorkflowManagerGetEventTriggerRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.WorkflowManagerGetEventTriggerRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.GetEventTrigger(ctx, value)
}

// GetTriggerWithInput fetches one event trigger.
func (c *WorkflowManagerClient) GetTriggerWithInput(ctx context.Context, input WorkflowManagerGetEventTriggerInput) (*proto.ManagedWorkflowEventTrigger, error) {
	return c.GetTrigger(ctx, NewWorkflowManagerGetEventTriggerRequest(input))
}

// UpdateTrigger updates an event trigger.
func (c *WorkflowManagerClient) UpdateTrigger(ctx context.Context, req *proto.WorkflowManagerUpdateEventTriggerRequest) (*proto.ManagedWorkflowEventTrigger, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	value := &proto.WorkflowManagerUpdateEventTriggerRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.WorkflowManagerUpdateEventTriggerRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.UpdateEventTrigger(ctx, value)
}

// UpdateTriggerWithInput updates an event trigger.
func (c *WorkflowManagerClient) UpdateTriggerWithInput(ctx context.Context, input WorkflowManagerUpdateEventTriggerInput) (*proto.ManagedWorkflowEventTrigger, error) {
	req, err := NewWorkflowManagerUpdateEventTriggerRequest(input)
	if err != nil {
		return nil, err
	}
	return c.UpdateTrigger(ctx, req)
}

// DeleteTrigger deletes an event trigger.
func (c *WorkflowManagerClient) DeleteTrigger(ctx context.Context, req *proto.WorkflowManagerDeleteEventTriggerRequest) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("workflow manager: client is not initialized")
	}
	value := &proto.WorkflowManagerDeleteEventTriggerRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.WorkflowManagerDeleteEventTriggerRequest)
	}
	value.InvocationToken = c.invocationToken
	_, err := c.client.DeleteEventTrigger(ctx, value)
	return err
}

// DeleteTriggerWithInput deletes an event trigger.
func (c *WorkflowManagerClient) DeleteTriggerWithInput(ctx context.Context, input WorkflowManagerDeleteEventTriggerInput) error {
	return c.DeleteTrigger(ctx, NewWorkflowManagerDeleteEventTriggerRequest(input))
}

// PauseTrigger pauses an event trigger.
func (c *WorkflowManagerClient) PauseTrigger(ctx context.Context, req *proto.WorkflowManagerPauseEventTriggerRequest) (*proto.ManagedWorkflowEventTrigger, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	value := &proto.WorkflowManagerPauseEventTriggerRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.WorkflowManagerPauseEventTriggerRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.PauseEventTrigger(ctx, value)
}

// PauseTriggerWithInput pauses an event trigger.
func (c *WorkflowManagerClient) PauseTriggerWithInput(ctx context.Context, input WorkflowManagerPauseEventTriggerInput) (*proto.ManagedWorkflowEventTrigger, error) {
	return c.PauseTrigger(ctx, NewWorkflowManagerPauseEventTriggerRequest(input))
}

// ResumeTrigger resumes an event trigger.
func (c *WorkflowManagerClient) ResumeTrigger(ctx context.Context, req *proto.WorkflowManagerResumeEventTriggerRequest) (*proto.ManagedWorkflowEventTrigger, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	value := &proto.WorkflowManagerResumeEventTriggerRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.WorkflowManagerResumeEventTriggerRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.ResumeEventTrigger(ctx, value)
}

// ResumeTriggerWithInput resumes an event trigger.
func (c *WorkflowManagerClient) ResumeTriggerWithInput(ctx context.Context, input WorkflowManagerResumeEventTriggerInput) (*proto.ManagedWorkflowEventTrigger, error) {
	return c.ResumeTrigger(ctx, NewWorkflowManagerResumeEventTriggerRequest(input))
}

// PublishEvent publishes an event into the workflow manager.
func (c *WorkflowManagerClient) PublishEvent(ctx context.Context, req *proto.WorkflowManagerPublishEventRequest) (*proto.WorkflowEvent, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	value := &proto.WorkflowManagerPublishEventRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.WorkflowManagerPublishEventRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.PublishEvent(ctx, value)
}

// PublishEventWithInput publishes an event.
func (c *WorkflowManagerClient) PublishEventWithInput(ctx context.Context, input WorkflowManagerPublishEventInput) (*proto.WorkflowEvent, error) {
	req, err := NewWorkflowManagerPublishEventRequest(input)
	if err != nil {
		return nil, err
	}
	return c.PublishEvent(ctx, req)
}
