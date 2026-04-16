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

const EnvWorkflowHostSocket = "GESTALT_WORKFLOW_HOST_SOCKET"

type WorkflowHostClient struct {
	client proto.WorkflowHostClient
	conn   *grpc.ClientConn
}

func WorkflowHost() (*WorkflowHostClient, error) {
	socketPath := os.Getenv(EnvWorkflowHostSocket)
	if socketPath == "" {
		return nil, fmt.Errorf("workflow host: %s is not set", EnvWorkflowHostSocket)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, "unix:"+socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("workflow host: connect to host: %w", err)
	}
	return &WorkflowHostClient{
		client: proto.NewWorkflowHostClient(conn),
		conn:   conn,
	}, nil
}

func (c *WorkflowHostClient) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *WorkflowHostClient) InvokeOperation(ctx context.Context, req *proto.InvokeWorkflowOperationRequest) (*proto.InvokeWorkflowOperationResponse, error) {
	return c.client.InvokeOperation(ctx, req)
}
