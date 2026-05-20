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

// WorkflowHostClient invokes workflow actions from provider code.
type WorkflowHostClient struct {
	client proto.WorkflowHostClient
}

var sharedWorkflowHostTransport sharedManagerTransport[proto.WorkflowHostClient]

// WorkflowHostActionSelector selects one canonical action in a host-owned
// execution reference. Providers must reuse the same selector and idempotency
// key until the host returns a terminal result.
type WorkflowHostActionSelector struct {
	ExecutionRef           string
	ExecutionRefGeneration int64
	ExecutionRefSeal       string
	RunID                  string
	StepID                 string
	ActionID               string
	AttemptNumber          int32
	IdempotencyKey         string
	TargetDigest           string
	ActionTableDigest      string
	ProviderPlanDigest     string
}

// WorkflowPluginActionPayload carries provider-evaluated input for a selected
// plugin or delivery action.
type WorkflowPluginActionPayload struct {
	Input any
}

// WorkflowAgentTurnPayload carries provider-evaluated prompt/messages for a
// selected agent-turn action.
type WorkflowAgentTurnPayload struct {
	Prompt   WorkflowText
	Messages []WorkflowAgentMessage
}

// InvokeWorkflowActionInput invokes the action selected by
// WorkflowHostActionSelector. Exactly one payload must be set.
type InvokeWorkflowActionInput struct {
	Selector  *WorkflowHostActionSelector
	Plugin    *WorkflowPluginActionPayload
	AgentTurn *WorkflowAgentTurnPayload
	Metadata  any
	Trigger   *WorkflowRunTrigger
	Signals   []WorkflowSignal
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
	return &WorkflowHostClient{client: client}, nil
}

// Close is a no-op compatibility method because this client uses shared transport.
func (c *WorkflowHostClient) Close() error {
	return nil
}

// InvokeWorkflowAction invokes a canonical workflow step action.
func (c *WorkflowHostClient) InvokeWorkflowAction(ctx context.Context, input InvokeWorkflowActionInput) (*WorkflowActionResult, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("workflow host: client is not initialized")
	}
	req, err := invokeWorkflowActionInputToProto(input)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.InvokeWorkflowAction(ctx, req)
	if err != nil {
		return nil, err
	}
	return workflowActionResultFromProto(resp), nil
}

func invokeWorkflowActionInputToProto(input InvokeWorkflowActionInput) (*proto.InvokeWorkflowActionRequest, error) {
	metadata, err := structFromAny(input.Metadata)
	if err != nil {
		return nil, err
	}
	trigger, err := newOptionalWorkflowRunTrigger(input.Trigger)
	if err != nil {
		return nil, err
	}
	signals, err := workflowSignalsToProto(input.Signals)
	if err != nil {
		return nil, err
	}
	req := &proto.InvokeWorkflowActionRequest{
		Selector: workflowHostActionSelectorToProto(input.Selector),
		Metadata: metadata,
		Trigger:  trigger,
		Signals:  signals,
	}
	switch {
	case input.Plugin != nil && input.AgentTurn != nil:
		return nil, fmt.Errorf("workflow action: exactly one payload must be set")
	case input.Plugin != nil:
		body, err := structFromAny(input.Plugin.Input)
		if err != nil {
			return nil, err
		}
		req.Action = &proto.InvokeWorkflowActionRequest_Plugin{
			Plugin: &proto.WorkflowPluginActionPayload{Input: body},
		}
	case input.AgentTurn != nil:
		messages, err := workflowAgentMessagesToProto(input.AgentTurn.Messages)
		if err != nil {
			return nil, err
		}
		req.Action = &proto.InvokeWorkflowActionRequest_AgentTurn{
			AgentTurn: &proto.WorkflowAgentTurnPayload{
				Prompt:   workflowTextToProto(input.AgentTurn.Prompt),
				Messages: messages,
			},
		}
	default:
		return nil, fmt.Errorf("workflow action: exactly one payload must be set")
	}
	return req, nil
}

func workflowHostActionSelectorToProto(input *WorkflowHostActionSelector) *proto.WorkflowHostActionSelector {
	if input == nil {
		return nil
	}
	return &proto.WorkflowHostActionSelector{
		ExecutionRef:           input.ExecutionRef,
		ExecutionRefGeneration: input.ExecutionRefGeneration,
		ExecutionRefSeal:       input.ExecutionRefSeal,
		RunId:                  input.RunID,
		StepId:                 input.StepID,
		ActionId:               input.ActionID,
		AttemptNumber:          input.AttemptNumber,
		IdempotencyKey:         input.IdempotencyKey,
		TargetDigest:           input.TargetDigest,
		ActionTableDigest:      input.ActionTableDigest,
		ProviderPlanDigest:     input.ProviderPlanDigest,
	}
}
