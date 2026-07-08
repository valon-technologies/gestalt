package workflows

import (
	"context"
	"errors"
	"testing"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type fakeGestaltWorkflowClient struct {
	startRun        func(context.Context, *proto.StartWorkflowProviderRunRequest) (*proto.WorkflowRun, error)
	listDefinitions func(context.Context, *proto.ListWorkflowProviderDefinitionsRequest) (*proto.ListWorkflowProviderDefinitionsResponse, error)
}

func (f *fakeGestaltWorkflowClient) ApplyDefinition(context.Context, *proto.ApplyWorkflowProviderDefinitionRequest, ...grpc.CallOption) (*proto.WorkflowDefinition, error) {
	return nil, errors.New("unexpected ApplyDefinition")
}

func (f *fakeGestaltWorkflowClient) GetDefinition(context.Context, *proto.GetWorkflowProviderDefinitionRequest, ...grpc.CallOption) (*proto.WorkflowDefinition, error) {
	return nil, errors.New("unexpected GetDefinition")
}

func (f *fakeGestaltWorkflowClient) ListDefinitions(ctx context.Context, req *proto.ListWorkflowProviderDefinitionsRequest, _ ...grpc.CallOption) (*proto.ListWorkflowProviderDefinitionsResponse, error) {
	if f.listDefinitions != nil {
		return f.listDefinitions(ctx, req)
	}
	return &proto.ListWorkflowProviderDefinitionsResponse{}, nil
}

func (f *fakeGestaltWorkflowClient) SetDefinitionPaused(context.Context, *proto.SetWorkflowProviderDefinitionPausedRequest, ...grpc.CallOption) (*proto.WorkflowDefinition, error) {
	return nil, errors.New("unexpected SetDefinitionPaused")
}

func (f *fakeGestaltWorkflowClient) SetActivationPaused(context.Context, *proto.SetWorkflowProviderActivationPausedRequest, ...grpc.CallOption) (*proto.WorkflowDefinition, error) {
	return nil, errors.New("unexpected SetActivationPaused")
}

func (f *fakeGestaltWorkflowClient) DeleteDefinition(context.Context, *proto.DeleteWorkflowProviderDefinitionRequest, ...grpc.CallOption) (*emptypb.Empty, error) {
	return nil, errors.New("unexpected DeleteDefinition")
}

func (f *fakeGestaltWorkflowClient) StartRun(ctx context.Context, req *proto.StartWorkflowProviderRunRequest, _ ...grpc.CallOption) (*proto.WorkflowRun, error) {
	if f.startRun != nil {
		return f.startRun(ctx, req)
	}
	return nil, errors.New("unexpected StartRun")
}

func (f *fakeGestaltWorkflowClient) GetRun(context.Context, *proto.GetWorkflowProviderRunRequest, ...grpc.CallOption) (*proto.WorkflowRun, error) {
	return nil, errors.New("unexpected GetRun")
}

func (f *fakeGestaltWorkflowClient) ListRuns(context.Context, *proto.ListWorkflowProviderRunsRequest, ...grpc.CallOption) (*proto.ListWorkflowProviderRunsResponse, error) {
	return nil, errors.New("unexpected ListRuns")
}

func (f *fakeGestaltWorkflowClient) GetRunEvents(context.Context, *proto.GetWorkflowProviderRunEventsRequest, ...grpc.CallOption) (*proto.GetWorkflowProviderRunEventsResponse, error) {
	return nil, errors.New("unexpected GetRunEvents")
}

func (f *fakeGestaltWorkflowClient) GetRunOutput(context.Context, *proto.GetWorkflowProviderRunOutputRequest, ...grpc.CallOption) (*proto.GetWorkflowProviderRunOutputResponse, error) {
	return nil, errors.New("unexpected GetRunOutput")
}

func (f *fakeGestaltWorkflowClient) CancelRun(context.Context, *proto.CancelWorkflowProviderRunRequest, ...grpc.CallOption) (*proto.WorkflowRun, error) {
	return nil, errors.New("unexpected CancelRun")
}

func (f *fakeGestaltWorkflowClient) SignalRun(context.Context, *proto.SignalWorkflowProviderRunRequest, ...grpc.CallOption) (*proto.SignalWorkflowRunResponse, error) {
	return nil, errors.New("unexpected SignalRun")
}

func (f *fakeGestaltWorkflowClient) SignalOrStartRun(context.Context, *proto.SignalOrStartWorkflowProviderRunRequest, ...grpc.CallOption) (*proto.SignalWorkflowRunResponse, error) {
	return nil, errors.New("unexpected SignalOrStartRun")
}

func (f *fakeGestaltWorkflowClient) DeliverEvent(context.Context, *proto.DeliverWorkflowProviderEventRequest, ...grpc.CallOption) (*proto.WorkflowEvent, error) {
	return nil, errors.New("unexpected DeliverEvent")
}

func TestGestaltRemoteWorkflowSetsProviderName(t *testing.T) {
	t.Parallel()

	var gotProvider string
	client := &fakeGestaltWorkflowClient{
		startRun: func(_ context.Context, req *proto.StartWorkflowProviderRunRequest) (*proto.WorkflowRun, error) {
			gotProvider = req.GetProviderName()
			return &proto.WorkflowRun{Id: "run-1"}, nil
		},
	}
	provider := NewGestaltRemoteProvider(client, "temporal")
	run, err := provider.StartRun(context.Background(), &proto.StartWorkflowProviderRunRequest{DefinitionId: "def-1"})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if gotProvider != "temporal" {
		t.Fatalf("provider_name = %q, want temporal", gotProvider)
	}
	if run.GetId() != "run-1" {
		t.Fatalf("run id = %q", run.GetId())
	}
}

func TestGestaltRemoteWorkflowMapsAuthErrors(t *testing.T) {
	t.Parallel()

	client := &fakeGestaltWorkflowClient{
		startRun: func(context.Context, *proto.StartWorkflowProviderRunRequest) (*proto.WorkflowRun, error) {
			return nil, status.Error(codes.Unauthenticated, "bad token")
		},
	}
	provider := NewGestaltRemoteProvider(client, "temporal")
	_, err := provider.StartRun(context.Background(), &proto.StartWorkflowProviderRunRequest{DefinitionId: "def-1"})
	if !errors.Is(err, invocation.ErrNotAuthenticated) {
		t.Fatalf("StartRun err = %v, want ErrNotAuthenticated", err)
	}
}

var _ coreworkflow.Provider = NewGestaltRemoteProvider(&fakeGestaltWorkflowClient{}, "temporal")
