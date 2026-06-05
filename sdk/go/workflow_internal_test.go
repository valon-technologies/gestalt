package gestalt

import (
	"context"
	"net"
	"sync"
	"testing"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	gproto "google.golang.org/protobuf/proto"
)

type workflowIdempotencyHarness struct {
	proto.UnimplementedWorkflowProviderServer

	mu      sync.Mutex
	starts  []*proto.StartWorkflowProviderRunRequest
	signals []*proto.SignalOrStartWorkflowProviderRunRequest
}

func (h *workflowIdempotencyHarness) StartRun(_ context.Context, req *proto.StartWorkflowProviderRunRequest) (*proto.WorkflowRun, error) {
	h.mu.Lock()
	h.starts = append(h.starts, gproto.Clone(req).(*proto.StartWorkflowProviderRunRequest))
	h.mu.Unlock()

	return &proto.WorkflowRun{
		ProviderName:         req.GetProviderName(),
		Id:                   "run-1",
		Status:               proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_PENDING,
		WorkflowKey:          req.GetWorkflowKey(),
		DefinitionId:         req.GetDefinitionId(),
		DefinitionGeneration: req.GetExpectedDefinitionGeneration(),
	}, nil
}

func (h *workflowIdempotencyHarness) SignalOrStartRun(_ context.Context, req *proto.SignalOrStartWorkflowProviderRunRequest) (*proto.SignalWorkflowRunResponse, error) {
	h.mu.Lock()
	h.signals = append(h.signals, gproto.Clone(req).(*proto.SignalOrStartWorkflowProviderRunRequest))
	h.mu.Unlock()

	return &proto.SignalWorkflowRunResponse{
		Run: &proto.WorkflowRun{
			ProviderName:         req.GetProviderName(),
			Id:                   "run-2",
			Status:               proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_PENDING,
			WorkflowKey:          req.GetWorkflowKey(),
			DefinitionId:         req.GetDefinitionId(),
			DefinitionGeneration: req.GetExpectedDefinitionGeneration(),
		},
		StartedRun:  true,
		WorkflowKey: req.GetWorkflowKey(),
	}, nil
}

func TestWorkflowFromContextDefaultsRunIdempotencyKey(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	harness := &workflowIdempotencyHarness{}
	srv := grpc.NewServer()
	proto.RegisterWorkflowProviderServer(srv, harness)
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(srv.Stop)

	t.Setenv(EnvHostServiceSocket, "tcp://"+lis.Addr().String())

	ctx := WithIdempotencyKey(context.Background(), "request-key")
	client, err := WorkflowFromContext(ctx)
	if err != nil {
		t.Fatalf("WorkflowFromContext: %v", err)
	}
	defer func() { _ = client.Close() }()

	started, err := client.StartRun(context.Background(), WorkflowStartRun{
		ProviderName:                 "basic",
		DefinitionID:                 "definition-1",
		ExpectedDefinitionGeneration: 7,
		WorkflowKey:                  "workflow-key-1",
	})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	signaled, err := client.SignalOrStartRun(context.Background(), WorkflowSignalOrStartRun{
		ProviderName:                 "basic",
		DefinitionID:                 "definition-1",
		ExpectedDefinitionGeneration: 7,
		WorkflowKey:                  "workflow-key-1",
	})
	if err != nil {
		t.Fatalf("SignalOrStartRun: %v", err)
	}

	if started == nil || started.Status != WorkflowRunStatusValuePending {
		t.Fatalf("started run status = %#v, want pending", started)
	}
	if signaled.Run == nil || signaled.Run.Status != WorkflowRunStatusValuePending {
		t.Fatalf("signaled run status = %#v, want pending", signaled.Run)
	}

	harness.mu.Lock()
	defer harness.mu.Unlock()
	if len(harness.starts) != 1 {
		t.Fatalf("starts len = %d, want 1", len(harness.starts))
	}
	if got := harness.starts[0].GetIdempotencyKey(); got != "request-key" {
		t.Fatalf("start idempotency key = %q, want request-key", got)
	}
	if got := harness.starts[0].GetDefinitionId(); got != "definition-1" {
		t.Fatalf("start definition id = %q, want definition-1", got)
	}
	if len(harness.signals) != 1 {
		t.Fatalf("signals len = %d, want 1", len(harness.signals))
	}
	if got := harness.signals[0].GetIdempotencyKey(); got != "request-key" {
		t.Fatalf("signal-or-start idempotency key = %q, want request-key", got)
	}
}
