package workflows

import (
	"context"
	"testing"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	proto "github.com/valon-technologies/gestalt/server/internal/gen/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestRemoteWorkflowUsesDefinitionProviderRPCs(t *testing.T) {
	t.Parallel()

	client := &recordingWorkflowProviderClient{}
	provider := &remoteWorkflow{client: client, name: "temporal"}

	definition, err := provider.ApplyWorkflowDefinition(context.Background(), coreworkflow.ApplyDefinitionRequest{
		Spec: coreworkflow.DefinitionSpec{
			ID: "deploy-1",
			Target: coreworkflow.Target{Steps: []coreworkflow.Step{{
				ID: "notify",
				Plugin: &coreworkflow.PluginCall{
					Name:      "slack",
					Operation: "reply",
					Input:     coreworkflow.Value{RunInput: "message"},
				},
			}}},
		},
		Binding: &coreworkflow.DefinitionBinding{
			ExecutionRef:             "exec-1",
			ExecutionRefGeneration:   1,
			DefinitionID:             "deploy-1",
			DefinitionGeneration:     1,
			SpecDigest:               "sha256:spec",
			TargetDigest:             "sha256:target",
			ActionTableDigest:        "sha256:actions",
			PermissionsDigest:        "sha256:permissions",
			WorkflowSemanticsVersion: "workflow_steps_v1",
		},
		ExecutionRef: &coreworkflow.ExecutionReference{
			ID:           "exec-1",
			ProviderName: "temporal",
			Target: coreworkflow.Target{Steps: []coreworkflow.Step{{
				ID:     "notify",
				Plugin: &coreworkflow.PluginCall{Name: "slack", Operation: "reply"},
			}}},
			SubjectID:         "user:1",
			SubjectKind:       "user",
			TargetDigest:      "sha256:target",
			PermissionsDigest: "sha256:permissions",
			Generation:        1,
		},
	})
	if err != nil {
		t.Fatalf("ApplyWorkflowDefinition: %v", err)
	}
	if definition.TargetDigest != "sha256:target" {
		t.Fatalf("target digest = %q", definition.TargetDigest)
	}
	if client.applyReq == nil || client.applyReq.GetSpec().GetTarget().GetSteps()[0].GetPlugin().GetName() != "slack" {
		t.Fatalf("apply request = %#v", client.applyReq)
	}
	if client.applyReq.GetExecutionRef().GetId() != "exec-1" {
		t.Fatalf("apply execution ref = %#v, want exec-1", client.applyReq.GetExecutionRef())
	}

	run, err := provider.StartRun(context.Background(), coreworkflow.StartRunRequest{
		DefinitionID:         "deploy-1",
		DefinitionGeneration: 2,
		ActivationID:         "manual",
		WorkflowKey:          "customer-1",
		Input:                map[string]any{"message": "hello"},
		IdempotencyKey:       "idem-1",
	})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if run.ID != "run-1" || run.DefinitionID != "deploy-1" {
		t.Fatalf("run = %#v", run)
	}
	if client.startReq == nil || client.startReq.GetDefinitionId() != "deploy-1" || client.startReq.GetInput().AsMap()["message"] != "hello" {
		t.Fatalf("start request = %#v", client.startReq)
	}
}

func TestRemoteWorkflowPublishEventUsesDeliverWorkflowEvent(t *testing.T) {
	t.Parallel()

	client := &recordingWorkflowProviderClient{}
	provider := &remoteWorkflow{client: client, name: "temporal"}

	err := provider.PublishEvent(context.Background(), coreworkflow.PublishEventRequest{
		DeliveryID:     "delivery-1",
		IdempotencyKey: "idem-1",
		Event: coreworkflow.Event{
			ID:   "evt-1",
			Type: "com.example.created",
			Data: map[string]any{"id": "123"},
		},
	})
	if err != nil {
		t.Fatalf("PublishEvent: %v", err)
	}
	if client.deliverReq == nil || client.deliverReq.GetDeliveryId() != "delivery-1" || client.deliverReq.GetEvent().GetData().AsMap()["id"] != "123" {
		t.Fatalf("deliver request = %#v", client.deliverReq)
	}
}

func TestRemoteWorkflowExecutionReferencesUseProviderReadRPCs(t *testing.T) {
	t.Parallel()

	client := &recordingWorkflowProviderClient{}
	provider := &remoteWorkflow{client: client, name: "temporal"}
	client.executionRef = &proto.WorkflowExecutionReference{
		Id:                "ref-1",
		ProviderName:      "temporal",
		SubjectId:         "user:1",
		TargetDigest:      "sha256:target",
		PermissionsDigest: "sha256:permissions",
		Generation:        2,
	}

	loaded, err := provider.GetExecutionReference(context.Background(), "ref-1")
	if err != nil {
		t.Fatalf("GetExecutionReference: %v", err)
	}
	if loaded.ID != "ref-1" || client.getExecutionRefReq.GetId() != "ref-1" {
		t.Fatalf("get execution ref request = %#v loaded=%#v", client.getExecutionRefReq, loaded)
	}

	refs, err := provider.ListExecutionReferences(context.Background(), "user:1")
	if err != nil {
		t.Fatalf("ListExecutionReferences: %v", err)
	}
	if len(refs) != 1 || refs[0].ID != "ref-1" || client.listExecutionRefsReq.GetSubjectId() != "user:1" {
		t.Fatalf("list execution refs request = %#v refs=%#v", client.listExecutionRefsReq, refs)
	}
}

func TestHostServerInvokeWorkflowActionReturnsActionResult(t *testing.T) {
	t.Parallel()

	input, err := structpb.NewStruct(map[string]any{"text": "hello"})
	if err != nil {
		t.Fatalf("NewStruct: %v", err)
	}
	server := NewHostServerWithActions("temporal", nil, func(_ context.Context, req coreworkflow.InvokeActionRequest) (*coreworkflow.HostActionResponse, error) {
		if req.ProviderName != "temporal" || req.Selector.DefinitionID != "deploy-1" {
			t.Fatalf("invoke request = %#v", req)
		}
		return &coreworkflow.HostActionResponse{
			ActionEventID: "event-1",
			Status:        200,
			Body:          `{"ok":true}`,
			OutputRef:     "output-1",
		}, nil
	}, nil)

	resp, err := server.InvokeWorkflowAction(context.Background(), &proto.InvokeWorkflowActionRequest{
		Selector: &proto.WorkflowHostActionSelector{
			DefinitionId: "deploy-1",
			RunId:        "run-1",
			StepId:       "notify",
			ActionId:     "step/notify/plugin",
		},
		Action: &proto.InvokeWorkflowActionRequest_Plugin{
			Plugin: &proto.WorkflowPluginActionPayload{Input: input},
		},
	})
	if err != nil {
		t.Fatalf("InvokeWorkflowAction: %v", err)
	}
	if resp.GetActionEventId() != "event-1" || resp.GetOutputRef() != "output-1" {
		t.Fatalf("response = %#v", resp)
	}
}

type recordingWorkflowProviderClient struct {
	applyReq             *proto.ApplyWorkflowDefinitionRequest
	startReq             *proto.StartWorkflowRunRequest
	deliverReq           *proto.DeliverWorkflowEventRequest
	executionRef         *proto.WorkflowExecutionReference
	getExecutionRefReq   *proto.GetWorkflowExecutionReferenceRequest
	listExecutionRefsReq *proto.ListWorkflowExecutionReferencesRequest
}

func (c *recordingWorkflowProviderClient) ApplyWorkflowDefinition(_ context.Context, req *proto.ApplyWorkflowDefinitionRequest, _ ...grpc.CallOption) (*proto.WorkflowDefinition, error) {
	c.applyReq = req
	return &proto.WorkflowDefinition{
		Spec:              req.GetSpec(),
		Status:            proto.WorkflowDefinitionStatus_WORKFLOW_DEFINITION_STATUS_ACTIVE,
		SpecDigest:        req.GetBinding().GetSpecDigest(),
		TargetDigest:      req.GetBinding().GetTargetDigest(),
		ActionTableDigest: req.GetBinding().GetActionTableDigest(),
		Binding:           req.GetBinding(),
	}, nil
}

func (c *recordingWorkflowProviderClient) GetWorkflowDefinition(context.Context, *proto.GetWorkflowDefinitionRequest, ...grpc.CallOption) (*proto.WorkflowDefinition, error) {
	return nil, unimplementedTestRPC()
}

func (c *recordingWorkflowProviderClient) ListWorkflowDefinitions(context.Context, *proto.ListWorkflowDefinitionsRequest, ...grpc.CallOption) (*proto.ListWorkflowDefinitionsResponse, error) {
	return nil, unimplementedTestRPC()
}

func (c *recordingWorkflowProviderClient) DeleteWorkflowDefinition(context.Context, *proto.DeleteWorkflowDefinitionRequest, ...grpc.CallOption) (*emptypb.Empty, error) {
	return nil, unimplementedTestRPC()
}

func (c *recordingWorkflowProviderClient) SetWorkflowDefinitionPaused(context.Context, *proto.SetWorkflowDefinitionPausedRequest, ...grpc.CallOption) (*proto.WorkflowDefinition, error) {
	return nil, unimplementedTestRPC()
}

func (c *recordingWorkflowProviderClient) SetWorkflowActivationPaused(context.Context, *proto.SetWorkflowActivationPausedRequest, ...grpc.CallOption) (*proto.WorkflowDefinition, error) {
	return nil, unimplementedTestRPC()
}

func (c *recordingWorkflowProviderClient) StartWorkflowRun(_ context.Context, req *proto.StartWorkflowRunRequest, _ ...grpc.CallOption) (*proto.WorkflowRun, error) {
	c.startReq = req
	return &proto.WorkflowRun{
		Id:                   "run-1",
		DefinitionId:         req.GetDefinitionId(),
		DefinitionGeneration: req.GetDefinitionGeneration(),
		WorkflowKey:          req.GetWorkflowKey(),
		Status:               proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_RUNNING,
		Input:                req.GetInput(),
	}, nil
}

func (c *recordingWorkflowProviderClient) SignalWorkflowRun(context.Context, *proto.SignalWorkflowRunRequest, ...grpc.CallOption) (*proto.WorkflowRunSignal, error) {
	return nil, unimplementedTestRPC()
}

func (c *recordingWorkflowProviderClient) SignalOrStartWorkflowRun(context.Context, *proto.SignalOrStartWorkflowRunRequest, ...grpc.CallOption) (*proto.WorkflowRunSignal, error) {
	return nil, unimplementedTestRPC()
}

func (c *recordingWorkflowProviderClient) CancelWorkflowRun(context.Context, *proto.CancelWorkflowRunRequest, ...grpc.CallOption) (*proto.WorkflowRun, error) {
	return nil, unimplementedTestRPC()
}

func (c *recordingWorkflowProviderClient) DeliverWorkflowEvent(_ context.Context, req *proto.DeliverWorkflowEventRequest, _ ...grpc.CallOption) (*proto.DeliverWorkflowEventResponse, error) {
	c.deliverReq = req
	return &proto.DeliverWorkflowEventResponse{}, nil
}

func (c *recordingWorkflowProviderClient) GetWorkflowRun(context.Context, *proto.GetWorkflowRunRequest, ...grpc.CallOption) (*proto.WorkflowRun, error) {
	return nil, unimplementedTestRPC()
}

func (c *recordingWorkflowProviderClient) ListWorkflowRuns(context.Context, *proto.ListWorkflowRunsRequest, ...grpc.CallOption) (*proto.ListWorkflowRunsResponse, error) {
	return nil, unimplementedTestRPC()
}

func (c *recordingWorkflowProviderClient) GetWorkflowRunEvents(context.Context, *proto.GetWorkflowRunEventsRequest, ...grpc.CallOption) (*proto.ListWorkflowRunEventsResponse, error) {
	return nil, unimplementedTestRPC()
}

func (c *recordingWorkflowProviderClient) GetWorkflowRunOutput(context.Context, *proto.GetWorkflowRunOutputRequest, ...grpc.CallOption) (*proto.WorkflowRunOutput, error) {
	return nil, unimplementedTestRPC()
}

func (c *recordingWorkflowProviderClient) GetExecutionReference(_ context.Context, req *proto.GetWorkflowExecutionReferenceRequest, _ ...grpc.CallOption) (*proto.WorkflowExecutionReference, error) {
	c.getExecutionRefReq = req
	if c.executionRef != nil && c.executionRef.GetId() == req.GetId() {
		return c.executionRef, nil
	}
	return nil, status.Error(codes.NotFound, "not found")
}

func (c *recordingWorkflowProviderClient) ListExecutionReferences(_ context.Context, req *proto.ListWorkflowExecutionReferencesRequest, _ ...grpc.CallOption) (*proto.ListWorkflowExecutionReferencesResponse, error) {
	c.listExecutionRefsReq = req
	if c.executionRef == nil || c.executionRef.GetSubjectId() != req.GetSubjectId() {
		return &proto.ListWorkflowExecutionReferencesResponse{}, nil
	}
	return &proto.ListWorkflowExecutionReferencesResponse{ExecutionRefs: []*proto.WorkflowExecutionReference{c.executionRef}}, nil
}

func unimplementedTestRPC() error {
	return status.Error(codes.Unimplemented, "not implemented in test client")
}
