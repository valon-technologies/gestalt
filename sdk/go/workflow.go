package gestalt

import (
	"context"
	"fmt"
	"os"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const EnvWorkflowSocket = "GESTALT_WORKFLOW_SOCKET"

type WorkflowClient struct {
	client proto.WorkflowClient
	conn   *grpc.ClientConn
}

func Workflow() (*WorkflowClient, error) {
	socketPath := os.Getenv(EnvWorkflowSocket)
	if socketPath == "" {
		return nil, fmt.Errorf("workflow: %s is not set", EnvWorkflowSocket)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, "unix:"+socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("workflow: connect to host: %w", err)
	}
	return &WorkflowClient{
		client: proto.NewWorkflowClient(conn),
		conn:   conn,
	}, nil
}

func (c *WorkflowClient) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *WorkflowClient) StartRun(ctx context.Context, req *proto.StartWorkflowRunRequest) (*proto.WorkflowRun, error) {
	return c.client.StartRun(ctx, req)
}

func (c *WorkflowClient) GetRun(ctx context.Context, runID string) (*proto.WorkflowRun, error) {
	return c.client.GetRun(ctx, &proto.GetWorkflowRunRequest{RunId: runID})
}

func (c *WorkflowClient) ListRuns(ctx context.Context) ([]*proto.WorkflowRun, error) {
	resp, err := c.client.ListRuns(ctx, &proto.ListWorkflowRunsRequest{})
	if err != nil {
		return nil, err
	}
	return resp.GetRuns(), nil
}

func (c *WorkflowClient) CancelRun(ctx context.Context, runID, reason string) (*proto.WorkflowRun, error) {
	return c.client.CancelRun(ctx, &proto.CancelWorkflowRunRequest{
		RunId:  runID,
		Reason: reason,
	})
}

func (c *WorkflowClient) UpsertSchedule(ctx context.Context, req *proto.UpsertWorkflowScheduleRequest) (*proto.WorkflowSchedule, error) {
	return c.client.UpsertSchedule(ctx, req)
}

func (c *WorkflowClient) GetSchedule(ctx context.Context, scheduleID string) (*proto.WorkflowSchedule, error) {
	return c.client.GetSchedule(ctx, &proto.GetWorkflowScheduleRequest{ScheduleId: scheduleID})
}

func (c *WorkflowClient) ListSchedules(ctx context.Context) ([]*proto.WorkflowSchedule, error) {
	resp, err := c.client.ListSchedules(ctx, &proto.ListWorkflowSchedulesRequest{})
	if err != nil {
		return nil, err
	}
	return resp.GetSchedules(), nil
}

func (c *WorkflowClient) DeleteSchedule(ctx context.Context, scheduleID string) error {
	_, err := c.client.DeleteSchedule(ctx, &proto.DeleteWorkflowScheduleRequest{ScheduleId: scheduleID})
	return err
}

func (c *WorkflowClient) PauseSchedule(ctx context.Context, scheduleID string) (*proto.WorkflowSchedule, error) {
	return c.client.PauseSchedule(ctx, &proto.PauseWorkflowScheduleRequest{ScheduleId: scheduleID})
}

func (c *WorkflowClient) ResumeSchedule(ctx context.Context, scheduleID string) (*proto.WorkflowSchedule, error) {
	return c.client.ResumeSchedule(ctx, &proto.ResumeWorkflowScheduleRequest{ScheduleId: scheduleID})
}

func (c *WorkflowClient) UpsertEventTrigger(ctx context.Context, req *proto.UpsertWorkflowEventTriggerRequest) (*proto.WorkflowEventTrigger, error) {
	return c.client.UpsertEventTrigger(ctx, req)
}

func (c *WorkflowClient) GetEventTrigger(ctx context.Context, triggerID string) (*proto.WorkflowEventTrigger, error) {
	return c.client.GetEventTrigger(ctx, &proto.GetWorkflowEventTriggerRequest{TriggerId: triggerID})
}

func (c *WorkflowClient) ListEventTriggers(ctx context.Context) ([]*proto.WorkflowEventTrigger, error) {
	resp, err := c.client.ListEventTriggers(ctx, &proto.ListWorkflowEventTriggersRequest{})
	if err != nil {
		return nil, err
	}
	return resp.GetTriggers(), nil
}

func (c *WorkflowClient) DeleteEventTrigger(ctx context.Context, triggerID string) error {
	_, err := c.client.DeleteEventTrigger(ctx, &proto.DeleteWorkflowEventTriggerRequest{TriggerId: triggerID})
	return err
}

func (c *WorkflowClient) PauseEventTrigger(ctx context.Context, triggerID string) (*proto.WorkflowEventTrigger, error) {
	return c.client.PauseEventTrigger(ctx, &proto.PauseWorkflowEventTriggerRequest{TriggerId: triggerID})
}

func (c *WorkflowClient) ResumeEventTrigger(ctx context.Context, triggerID string) (*proto.WorkflowEventTrigger, error) {
	return c.client.ResumeEventTrigger(ctx, &proto.ResumeWorkflowEventTriggerRequest{TriggerId: triggerID})
}

func (c *WorkflowClient) PublishEvent(ctx context.Context, event *proto.WorkflowEvent) error {
	_, err := c.client.PublishEvent(ctx, &proto.PublishWorkflowEventRequest{Event: event})
	return err
}
