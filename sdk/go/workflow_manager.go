package gestalt

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
)

const EnvWorkflowManagerSocket = proto.EnvWorkflowManagerSocket
const EnvWorkflowManagerSocketToken = EnvWorkflowManagerSocket + "_TOKEN"

type WorkflowManagerClient struct {
	client          proto.WorkflowManagerHostClient
	invocationToken string
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
	return WorkflowManager(InvocationTokenFromContext(ctx))
}

func (c *WorkflowManagerClient) Close() error {
	return nil
}

func (c *WorkflowManagerClient) CreateSchedule(ctx context.Context, req *proto.WorkflowManagerCreateScheduleRequest) (*proto.ManagedWorkflowSchedule, error) {
	return managerUnary(ctx, "workflow manager", c != nil && c.client != nil, req, &proto.WorkflowManagerCreateScheduleRequest{}, c.invocationToken,
		func(value *proto.WorkflowManagerCreateScheduleRequest, token string) { value.InvocationToken = token },
		func(ctx context.Context, value *proto.WorkflowManagerCreateScheduleRequest) (*proto.ManagedWorkflowSchedule, error) {
			return c.client.CreateSchedule(ctx, value)
		},
	)
}

func (c *WorkflowManagerClient) GetSchedule(ctx context.Context, req *proto.WorkflowManagerGetScheduleRequest) (*proto.ManagedWorkflowSchedule, error) {
	return managerUnary(ctx, "workflow manager", c != nil && c.client != nil, req, &proto.WorkflowManagerGetScheduleRequest{}, c.invocationToken,
		func(value *proto.WorkflowManagerGetScheduleRequest, token string) { value.InvocationToken = token },
		func(ctx context.Context, value *proto.WorkflowManagerGetScheduleRequest) (*proto.ManagedWorkflowSchedule, error) {
			return c.client.GetSchedule(ctx, value)
		},
	)
}

func (c *WorkflowManagerClient) UpdateSchedule(ctx context.Context, req *proto.WorkflowManagerUpdateScheduleRequest) (*proto.ManagedWorkflowSchedule, error) {
	return managerUnary(ctx, "workflow manager", c != nil && c.client != nil, req, &proto.WorkflowManagerUpdateScheduleRequest{}, c.invocationToken,
		func(value *proto.WorkflowManagerUpdateScheduleRequest, token string) { value.InvocationToken = token },
		func(ctx context.Context, value *proto.WorkflowManagerUpdateScheduleRequest) (*proto.ManagedWorkflowSchedule, error) {
			return c.client.UpdateSchedule(ctx, value)
		},
	)
}

func (c *WorkflowManagerClient) DeleteSchedule(ctx context.Context, req *proto.WorkflowManagerDeleteScheduleRequest) error {
	return managerUnaryNoResponse(ctx, "workflow manager", c != nil && c.client != nil, req, &proto.WorkflowManagerDeleteScheduleRequest{}, c.invocationToken,
		func(value *proto.WorkflowManagerDeleteScheduleRequest, token string) { value.InvocationToken = token },
		func(ctx context.Context, value *proto.WorkflowManagerDeleteScheduleRequest) error {
			_, err := c.client.DeleteSchedule(ctx, value)
			return err
		},
	)
}

func (c *WorkflowManagerClient) PauseSchedule(ctx context.Context, req *proto.WorkflowManagerPauseScheduleRequest) (*proto.ManagedWorkflowSchedule, error) {
	return managerUnary(ctx, "workflow manager", c != nil && c.client != nil, req, &proto.WorkflowManagerPauseScheduleRequest{}, c.invocationToken,
		func(value *proto.WorkflowManagerPauseScheduleRequest, token string) { value.InvocationToken = token },
		func(ctx context.Context, value *proto.WorkflowManagerPauseScheduleRequest) (*proto.ManagedWorkflowSchedule, error) {
			return c.client.PauseSchedule(ctx, value)
		},
	)
}

func (c *WorkflowManagerClient) ResumeSchedule(ctx context.Context, req *proto.WorkflowManagerResumeScheduleRequest) (*proto.ManagedWorkflowSchedule, error) {
	return managerUnary(ctx, "workflow manager", c != nil && c.client != nil, req, &proto.WorkflowManagerResumeScheduleRequest{}, c.invocationToken,
		func(value *proto.WorkflowManagerResumeScheduleRequest, token string) { value.InvocationToken = token },
		func(ctx context.Context, value *proto.WorkflowManagerResumeScheduleRequest) (*proto.ManagedWorkflowSchedule, error) {
			return c.client.ResumeSchedule(ctx, value)
		},
	)
}

func (c *WorkflowManagerClient) CreateTrigger(ctx context.Context, req *proto.WorkflowManagerCreateEventTriggerRequest) (*proto.ManagedWorkflowEventTrigger, error) {
	return managerUnary(ctx, "workflow manager", c != nil && c.client != nil, req, &proto.WorkflowManagerCreateEventTriggerRequest{}, c.invocationToken,
		func(value *proto.WorkflowManagerCreateEventTriggerRequest, token string) {
			value.InvocationToken = token
		},
		func(ctx context.Context, value *proto.WorkflowManagerCreateEventTriggerRequest) (*proto.ManagedWorkflowEventTrigger, error) {
			return c.client.CreateEventTrigger(ctx, value)
		},
	)
}

func (c *WorkflowManagerClient) GetTrigger(ctx context.Context, req *proto.WorkflowManagerGetEventTriggerRequest) (*proto.ManagedWorkflowEventTrigger, error) {
	return managerUnary(ctx, "workflow manager", c != nil && c.client != nil, req, &proto.WorkflowManagerGetEventTriggerRequest{}, c.invocationToken,
		func(value *proto.WorkflowManagerGetEventTriggerRequest, token string) { value.InvocationToken = token },
		func(ctx context.Context, value *proto.WorkflowManagerGetEventTriggerRequest) (*proto.ManagedWorkflowEventTrigger, error) {
			return c.client.GetEventTrigger(ctx, value)
		},
	)
}

func (c *WorkflowManagerClient) UpdateTrigger(ctx context.Context, req *proto.WorkflowManagerUpdateEventTriggerRequest) (*proto.ManagedWorkflowEventTrigger, error) {
	return managerUnary(ctx, "workflow manager", c != nil && c.client != nil, req, &proto.WorkflowManagerUpdateEventTriggerRequest{}, c.invocationToken,
		func(value *proto.WorkflowManagerUpdateEventTriggerRequest, token string) {
			value.InvocationToken = token
		},
		func(ctx context.Context, value *proto.WorkflowManagerUpdateEventTriggerRequest) (*proto.ManagedWorkflowEventTrigger, error) {
			return c.client.UpdateEventTrigger(ctx, value)
		},
	)
}

func (c *WorkflowManagerClient) DeleteTrigger(ctx context.Context, req *proto.WorkflowManagerDeleteEventTriggerRequest) error {
	return managerUnaryNoResponse(ctx, "workflow manager", c != nil && c.client != nil, req, &proto.WorkflowManagerDeleteEventTriggerRequest{}, c.invocationToken,
		func(value *proto.WorkflowManagerDeleteEventTriggerRequest, token string) {
			value.InvocationToken = token
		},
		func(ctx context.Context, value *proto.WorkflowManagerDeleteEventTriggerRequest) error {
			_, err := c.client.DeleteEventTrigger(ctx, value)
			return err
		},
	)
}

func (c *WorkflowManagerClient) PauseTrigger(ctx context.Context, req *proto.WorkflowManagerPauseEventTriggerRequest) (*proto.ManagedWorkflowEventTrigger, error) {
	return managerUnary(ctx, "workflow manager", c != nil && c.client != nil, req, &proto.WorkflowManagerPauseEventTriggerRequest{}, c.invocationToken,
		func(value *proto.WorkflowManagerPauseEventTriggerRequest, token string) {
			value.InvocationToken = token
		},
		func(ctx context.Context, value *proto.WorkflowManagerPauseEventTriggerRequest) (*proto.ManagedWorkflowEventTrigger, error) {
			return c.client.PauseEventTrigger(ctx, value)
		},
	)
}

func (c *WorkflowManagerClient) ResumeTrigger(ctx context.Context, req *proto.WorkflowManagerResumeEventTriggerRequest) (*proto.ManagedWorkflowEventTrigger, error) {
	return managerUnary(ctx, "workflow manager", c != nil && c.client != nil, req, &proto.WorkflowManagerResumeEventTriggerRequest{}, c.invocationToken,
		func(value *proto.WorkflowManagerResumeEventTriggerRequest, token string) {
			value.InvocationToken = token
		},
		func(ctx context.Context, value *proto.WorkflowManagerResumeEventTriggerRequest) (*proto.ManagedWorkflowEventTrigger, error) {
			return c.client.ResumeEventTrigger(ctx, value)
		},
	)
}

func (c *WorkflowManagerClient) PublishEvent(ctx context.Context, req *proto.WorkflowManagerPublishEventRequest) (*proto.WorkflowEvent, error) {
	return managerUnary(ctx, "workflow manager", c != nil && c.client != nil, req, &proto.WorkflowManagerPublishEventRequest{}, c.invocationToken,
		func(value *proto.WorkflowManagerPublishEventRequest, token string) { value.InvocationToken = token },
		func(ctx context.Context, value *proto.WorkflowManagerPublishEventRequest) (*proto.WorkflowEvent, error) {
			return c.client.PublishEvent(ctx, value)
		},
	)
}
