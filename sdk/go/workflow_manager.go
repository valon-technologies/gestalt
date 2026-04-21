package gestalt

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	gproto "google.golang.org/protobuf/proto"
)

const EnvWorkflowManagerSocket = proto.EnvWorkflowManagerSocket

type WorkflowManagerClient struct {
	client        proto.WorkflowManagerHostClient
	requestHandle string
}

var sharedWorkflowManagerTransport struct {
	mu         sync.Mutex
	socketPath string
	conn       *grpc.ClientConn
	client     proto.WorkflowManagerHostClient
}

func WorkflowManager(requestHandle string) (*WorkflowManagerClient, error) {
	if requestHandle == "" {
		return nil, fmt.Errorf("workflow manager: request handle is not available")
	}
	socketPath := os.Getenv(EnvWorkflowManagerSocket)
	if socketPath == "" {
		return nil, fmt.Errorf("workflow manager: %s is not set", EnvWorkflowManagerSocket)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := sharedWorkflowManagerClient(ctx, socketPath)
	if err != nil {
		return nil, err
	}

	return &WorkflowManagerClient{
		client:        client,
		requestHandle: requestHandle,
	}, nil
}

func sharedWorkflowManagerClient(ctx context.Context, socketPath string) (proto.WorkflowManagerHostClient, error) {
	sharedWorkflowManagerTransport.mu.Lock()
	if sharedWorkflowManagerTransport.conn != nil && sharedWorkflowManagerTransport.socketPath == socketPath {
		client := sharedWorkflowManagerTransport.client
		sharedWorkflowManagerTransport.mu.Unlock()
		return client, nil
	}
	sharedWorkflowManagerTransport.mu.Unlock()

	conn, err := grpc.DialContext(ctx, "unix:"+socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("workflow manager: connect to host: %w", err)
	}

	client := proto.NewWorkflowManagerHostClient(conn)

	sharedWorkflowManagerTransport.mu.Lock()
	defer sharedWorkflowManagerTransport.mu.Unlock()

	if sharedWorkflowManagerTransport.conn != nil && sharedWorkflowManagerTransport.socketPath == socketPath {
		_ = conn.Close()
		return sharedWorkflowManagerTransport.client, nil
	}
	if sharedWorkflowManagerTransport.conn != nil {
		_ = sharedWorkflowManagerTransport.conn.Close()
	}

	sharedWorkflowManagerTransport.socketPath = socketPath
	sharedWorkflowManagerTransport.conn = conn
	sharedWorkflowManagerTransport.client = client
	return client, nil
}

func WorkflowManagerFromContext(ctx context.Context) (*WorkflowManagerClient, error) {
	return WorkflowManager(RequestHandleFromContext(ctx))
}

func (c *WorkflowManagerClient) Close() error {
	return nil
}

func (c *WorkflowManagerClient) CreateSchedule(ctx context.Context, req *proto.WorkflowManagerCreateScheduleRequest) (*proto.ManagedWorkflowSchedule, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow manager: client is not initialized")
	}
	value := &proto.WorkflowManagerCreateScheduleRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.WorkflowManagerCreateScheduleRequest)
	}
	value.RequestHandle = c.requestHandle
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
	value.RequestHandle = c.requestHandle
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
	value.RequestHandle = c.requestHandle
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
	value.RequestHandle = c.requestHandle
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
	value.RequestHandle = c.requestHandle
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
	value.RequestHandle = c.requestHandle
	return c.client.ResumeSchedule(ctx, value)
}
