package workflows

import (
	"context"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	proto "github.com/valon-technologies/gestalt/server/internal/gen/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestRemoteWorkflowUsesDeploymentProviderRPCs(t *testing.T) {
	t.Parallel()

	client := &recordingWorkflowProviderClient{}
	provider := &remoteWorkflow{client: client, name: "temporal"}

	plan, err := provider.PlanWorkflow(context.Background(), coreworkflow.PlanWorkflowRequest{
		Spec: coreworkflow.DeploymentSpec{
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
		SpecDigest:        "sha256:spec",
		TargetDigest:      "sha256:target",
		ActionTableDigest: "sha256:actions",
	})
	if err != nil {
		t.Fatalf("PlanWorkflow: %v", err)
	}
	if plan.ProviderPlanDigest != "sha256:plan" {
		t.Fatalf("provider plan digest = %q", plan.ProviderPlanDigest)
	}
	if client.planReq == nil || client.planReq.GetSpec().GetTarget().GetSteps()[0].GetPlugin().GetName() != "slack" {
		t.Fatalf("plan request = %#v", client.planReq)
	}

	run, err := provider.StartRun(context.Background(), coreworkflow.StartRunRequest{
		DeploymentID:         "deploy-1",
		DeploymentGeneration: 2,
		ActivationID:         "manual",
		WorkflowKey:          "customer-1",
		Input:                map[string]any{"message": "hello"},
		IdempotencyKey:       "idem-1",
	})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if run.ID != "run-1" || run.DeploymentID != "deploy-1" {
		t.Fatalf("run = %#v", run)
	}
	if client.startReq == nil || client.startReq.GetDeploymentId() != "deploy-1" || client.startReq.GetInput().AsMap()["message"] != "hello" {
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

func TestRemoteWorkflowExecutionReferencesUseProviderRPCs(t *testing.T) {
	t.Parallel()

	client := &recordingWorkflowProviderClient{}
	provider := &remoteWorkflow{client: client, name: "temporal"}
	ref := &coreworkflow.ExecutionReference{
		ID:           "ref-1",
		ProviderName: "temporal",
		Target: coreworkflow.Target{Steps: []coreworkflow.Step{{
			ID:     "notify",
			Plugin: &coreworkflow.PluginCall{Name: "slack", Operation: "reply"},
		}}},
		SubjectID:         "user:1",
		Permissions:       []core.AccessPermission{{Plugin: "slack", Operations: []string{"reply"}}},
		TargetDigest:      "sha256:target",
		PermissionsDigest: "sha256:permissions",
		Generation:        2,
	}

	stored, err := provider.PutExecutionReference(context.Background(), ref)
	if err != nil {
		t.Fatalf("PutExecutionReference: %v", err)
	}
	if stored.ID != "ref-1" || client.putExecutionRefReq.GetExecutionRef().GetId() != "ref-1" {
		t.Fatalf("put execution ref request = %#v stored=%#v", client.putExecutionRefReq, stored)
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
		if req.ProviderName != "temporal" || req.Selector.RunID != "run-1" {
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
			RunId:    "run-1",
			StepId:   "notify",
			ActionId: "step/notify/plugin",
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
	planReq              *proto.PlanWorkflowRequest
	startReq             *proto.StartWorkflowRunRequest
	deliverReq           *proto.DeliverWorkflowEventRequest
	putExecutionRefReq   *proto.PutWorkflowExecutionReferenceRequest
	getExecutionRefReq   *proto.GetWorkflowExecutionReferenceRequest
	listExecutionRefsReq *proto.ListWorkflowExecutionReferencesRequest
}

func (c *recordingWorkflowProviderClient) PlanWorkflow(_ context.Context, req *proto.PlanWorkflowRequest, _ ...grpc.CallOption) (*proto.PlanWorkflowResponse, error) {
	c.planReq = req
	return &proto.PlanWorkflowResponse{
		AcceptedSpecDigest:    req.GetSpecDigest(),
		ProviderPlanId:        "plan-1",
		ProviderPlanDigest:    "sha256:plan",
		SupportedFeatureFlags: []string{"steps"},
	}, nil
}

func (c *recordingWorkflowProviderClient) ApplyWorkflowDeployment(context.Context, *proto.ApplyWorkflowDeploymentRequest, ...grpc.CallOption) (*proto.WorkflowDeployment, error) {
	return nil, unimplementedTestRPC()
}

func (c *recordingWorkflowProviderClient) GetWorkflowDeployment(context.Context, *proto.GetWorkflowDeploymentRequest, ...grpc.CallOption) (*proto.WorkflowDeployment, error) {
	return nil, unimplementedTestRPC()
}

func (c *recordingWorkflowProviderClient) ListWorkflowDeployments(context.Context, *proto.ListWorkflowDeploymentsRequest, ...grpc.CallOption) (*proto.ListWorkflowDeploymentsResponse, error) {
	return nil, unimplementedTestRPC()
}

func (c *recordingWorkflowProviderClient) DeleteWorkflowDeployment(context.Context, *proto.DeleteWorkflowDeploymentRequest, ...grpc.CallOption) (*emptypb.Empty, error) {
	return nil, unimplementedTestRPC()
}

func (c *recordingWorkflowProviderClient) SetWorkflowDeploymentPaused(context.Context, *proto.SetWorkflowDeploymentPausedRequest, ...grpc.CallOption) (*proto.WorkflowDeployment, error) {
	return nil, unimplementedTestRPC()
}

func (c *recordingWorkflowProviderClient) SetWorkflowActivationPaused(context.Context, *proto.SetWorkflowActivationPausedRequest, ...grpc.CallOption) (*proto.WorkflowDeployment, error) {
	return nil, unimplementedTestRPC()
}

func (c *recordingWorkflowProviderClient) StartWorkflowRun(_ context.Context, req *proto.StartWorkflowRunRequest, _ ...grpc.CallOption) (*proto.WorkflowRun, error) {
	c.startReq = req
	return &proto.WorkflowRun{
		Id:                   "run-1",
		DeploymentId:         req.GetDeploymentId(),
		DeploymentGeneration: req.GetDeploymentGeneration(),
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

func (c *recordingWorkflowProviderClient) PutExecutionReference(_ context.Context, req *proto.PutWorkflowExecutionReferenceRequest, _ ...grpc.CallOption) (*proto.WorkflowExecutionReference, error) {
	c.putExecutionRefReq = req
	return req.GetExecutionRef(), nil
}

func (c *recordingWorkflowProviderClient) GetExecutionReference(_ context.Context, req *proto.GetWorkflowExecutionReferenceRequest, _ ...grpc.CallOption) (*proto.WorkflowExecutionReference, error) {
	c.getExecutionRefReq = req
	if c.putExecutionRefReq != nil && c.putExecutionRefReq.GetExecutionRef().GetId() == req.GetId() {
		return c.putExecutionRefReq.GetExecutionRef(), nil
	}
	return nil, status.Error(codes.NotFound, "not found")
}

func (c *recordingWorkflowProviderClient) ListExecutionReferences(_ context.Context, req *proto.ListWorkflowExecutionReferencesRequest, _ ...grpc.CallOption) (*proto.ListWorkflowExecutionReferencesResponse, error) {
	c.listExecutionRefsReq = req
	if c.putExecutionRefReq == nil || c.putExecutionRefReq.GetExecutionRef().GetSubjectId() != req.GetSubjectId() {
		return &proto.ListWorkflowExecutionReferencesResponse{}, nil
	}
	return &proto.ListWorkflowExecutionReferencesResponse{ExecutionRefs: []*proto.WorkflowExecutionReference{c.putExecutionRefReq.GetExecutionRef()}}, nil
}

func unimplementedTestRPC() error {
	return status.Error(codes.Unimplemented, "not implemented in test client")
}
