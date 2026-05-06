package gestalt

import (
	"context"
	"fmt"
	"os"
	"time"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
)

// EnvWorkflowHostSocket names the environment variable containing the workflow-host
// service target.
const EnvWorkflowHostSocket = "GESTALT_WORKFLOW_HOST_SOCKET"

// EnvWorkflowHostSocketToken names the optional workflow-host relay-token variable.
const EnvWorkflowHostSocketToken = EnvWorkflowHostSocket + "_TOKEN"

// WorkflowHostClient invokes operations from workflow provider code.
type WorkflowHostClient struct {
	client proto.WorkflowHostClient
}

var sharedWorkflowHostTransport sharedManagerTransport[proto.WorkflowHostClient]

// WorkflowHost returns a shared client for the workflow host service.
func WorkflowHost() (*WorkflowHostClient, error) {
	target := os.Getenv(EnvWorkflowHostSocket)
	if target == "" {
		return nil, fmt.Errorf("workflow host: %s is not set", EnvWorkflowHostSocket)
	}
	token := os.Getenv(EnvWorkflowHostSocketToken)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := managerTransportClient(ctx, "workflow host", target, token, &sharedWorkflowHostTransport, proto.NewWorkflowHostClient)
	if err != nil {
		return nil, err
	}
	return &WorkflowHostClient{
		client: client,
	}, nil
}

// Close is a no-op compatibility method because this client uses shared transport.
func (c *WorkflowHostClient) Close() error {
	return nil
}

// InvokeOperation invokes an operation through the workflow host service.
func (c *WorkflowHostClient) InvokeOperation(ctx context.Context, req *proto.InvokeWorkflowOperationRequest) (*proto.InvokeWorkflowOperationResponse, error) {
	return c.client.InvokeOperation(ctx, req)
}
