package gestalt

import (
	"context"
	"fmt"
	"os"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/internal/gen/v1"
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

// InvokeWorkflowOperationInput requests invoking a workflow operation through
// the host service.
type InvokeWorkflowOperationInput struct {
	Target       *BoundWorkflowTargetInput
	RunID        string
	Trigger      *WorkflowRunTriggerInput
	Input        any
	Metadata     any
	CreatedBy    *WorkflowActorInput
	ExecutionRef string
	Signals      []WorkflowSignalInput
}

// InvokeWorkflowOperationResponse is returned by WorkflowHostClient.InvokeOperation.
type InvokeWorkflowOperationResponse struct {
	Status int32
	Body   string
}

// GetStatus returns the HTTP-style operation status code.
func (r *InvokeWorkflowOperationResponse) GetStatus() int32 {
	if r == nil {
		return 0
	}
	return r.Status
}

// GetBody returns the operation response body.
func (r *InvokeWorkflowOperationResponse) GetBody() string {
	if r == nil {
		return ""
	}
	return r.Body
}

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
func (c *WorkflowHostClient) InvokeOperation(ctx context.Context, input InvokeWorkflowOperationInput) (*InvokeWorkflowOperationResponse, error) {
	req, err := invokeWorkflowOperationInputToProto(input)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.InvokeOperation(ctx, req)
	if err != nil {
		return nil, err
	}
	return invokeWorkflowOperationResponseFromProto(resp), nil
}

func invokeWorkflowOperationInputToProto(input InvokeWorkflowOperationInput) (*proto.InvokeWorkflowOperationRequest, error) {
	var target *proto.BoundWorkflowTarget
	if input.Target != nil {
		value, err := NewBoundWorkflowTarget(*input.Target)
		if err != nil {
			return nil, err
		}
		target = value
	}
	var trigger *proto.WorkflowRunTrigger
	if input.Trigger != nil {
		value, err := NewWorkflowRunTrigger(*input.Trigger)
		if err != nil {
			return nil, err
		}
		trigger = value
	}
	body, err := StructFromAny(input.Input)
	if err != nil {
		return nil, err
	}
	metadata, err := StructFromAny(input.Metadata)
	if err != nil {
		return nil, err
	}
	signals := make([]*proto.WorkflowSignal, 0, len(input.Signals))
	for _, signalInput := range input.Signals {
		signal, err := NewWorkflowSignal(signalInput)
		if err != nil {
			return nil, err
		}
		signals = append(signals, signal)
	}
	return &proto.InvokeWorkflowOperationRequest{
		Target:       target,
		RunId:        input.RunID,
		Trigger:      trigger,
		Input:        body,
		Metadata:     metadata,
		CreatedBy:    workflowActorFromInput(input.CreatedBy),
		ExecutionRef: input.ExecutionRef,
		Signals:      signals,
	}, nil
}

func invokeWorkflowOperationResponseFromProto(resp *proto.InvokeWorkflowOperationResponse) *InvokeWorkflowOperationResponse {
	if resp == nil {
		return nil
	}
	return &InvokeWorkflowOperationResponse{
		Status: resp.GetStatus(),
		Body:   resp.GetBody(),
	}
}
