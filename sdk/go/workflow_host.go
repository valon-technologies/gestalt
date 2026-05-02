package gestalt

import (
	"context"
	"fmt"
	"os"
	"time"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// EnvWorkflowHostSocket names the environment variable containing the
// workflow-host service socket path.
const EnvWorkflowHostSocket = "GESTALT_WORKFLOW_HOST_SOCKET"

// WorkflowHostClient invokes operations from workflow provider code.
type WorkflowHostClient struct {
	client proto.WorkflowHostClient
	conn   *grpc.ClientConn
}

// WorkflowHost returns a client for the workflow host service.
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

// Close closes the underlying workflow-host connection.
func (c *WorkflowHostClient) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// InvokeOperation invokes an operation through the workflow host service.
func (c *WorkflowHostClient) InvokeOperation(ctx context.Context, req *proto.InvokeWorkflowOperationRequest) (*proto.InvokeWorkflowOperationResponse, error) {
	return c.client.InvokeOperation(ctx, req)
}
