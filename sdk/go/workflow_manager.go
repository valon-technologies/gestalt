package gestalt

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	gproto "google.golang.org/protobuf/proto"
)

const EnvWorkflowManagerSocket = proto.EnvWorkflowManagerSocket
const EnvWorkflowManagerSocketToken = EnvWorkflowManagerSocket + "_TOKEN"

type WorkflowManagerClient struct {
	client          proto.WorkflowManagerHostClient
	invocationToken string
	idempotencyKey  string
}

var sharedWorkflowManagerTransport sharedManagerTransport[proto.WorkflowManagerHostClient]

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

func WorkflowManagerFromContext(ctx context.Context) (*WorkflowManagerClient, error) {
	client, err := WorkflowManager(InvocationTokenFromContext(ctx))
	if err != nil {
		return nil, err
	}
	client.idempotencyKey = IdempotencyKeyFromContext(ctx)
	return client, nil
}

func (c *WorkflowManagerClient) Close() error {
	return nil
}

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
