package gestalt

import (
	"context"
	"net"
	"sync"
	"testing"

	proto "github.com/valon-technologies/gestalt/sdk/go/internal/gen/v1"
	"google.golang.org/grpc"
	gproto "google.golang.org/protobuf/proto"
)

type workflowManagerIdempotencyHarness struct {
	proto.UnimplementedWorkflowManagerHostServer

	mu      sync.Mutex
	starts  []*proto.WorkflowManagerStartRunRequest
	signals []*proto.WorkflowManagerSignalOrStartRunRequest
}

func (h *workflowManagerIdempotencyHarness) StartRun(_ context.Context, req *proto.WorkflowManagerStartRunRequest) (*proto.ManagedWorkflowRun, error) {
	h.mu.Lock()
	h.starts = append(h.starts, gproto.Clone(req).(*proto.WorkflowManagerStartRunRequest))
	h.mu.Unlock()

	return &proto.ManagedWorkflowRun{
		ProviderName: req.GetProviderName(),
		Run: &proto.BoundWorkflowRun{
			Id:          "run-1",
			Status:      proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_PENDING,
			WorkflowKey: req.GetWorkflowKey(),
		},
	}, nil
}

func (h *workflowManagerIdempotencyHarness) SignalOrStartRun(_ context.Context, req *proto.WorkflowManagerSignalOrStartRunRequest) (*proto.ManagedWorkflowRunSignal, error) {
	h.mu.Lock()
	h.signals = append(h.signals, gproto.Clone(req).(*proto.WorkflowManagerSignalOrStartRunRequest))
	h.mu.Unlock()

	return &proto.ManagedWorkflowRunSignal{
		ProviderName: req.GetProviderName(),
		Run: &proto.BoundWorkflowRun{
			Id:          "run-2",
			Status:      proto.WorkflowRunStatus_WORKFLOW_RUN_STATUS_PENDING,
			WorkflowKey: req.GetWorkflowKey(),
		},
		StartedRun:  true,
		WorkflowKey: req.GetWorkflowKey(),
	}, nil
}

func TestWorkflowManagerFromContextDefaultsRunIdempotencyKey(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	harness := &workflowManagerIdempotencyHarness{}
	srv := grpc.NewServer()
	proto.RegisterWorkflowManagerHostServer(srv, harness)
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(srv.Stop)

	t.Setenv(EnvWorkflowManagerSocket, "tcp://"+lis.Addr().String())

	ctx := WithIdempotencyKey(context.Background(), "request-key")
	ctx = withInvocationToken(ctx, "parent-token")
	client, err := WorkflowManagerFromContext(ctx)
	if err != nil {
		t.Fatalf("WorkflowManagerFromContext: %v", err)
	}
	defer func() { _ = client.Close() }()

	started, err := client.StartRun(context.Background(), WorkflowManagerStartRunInput{
		ProviderName: "basic",
		WorkflowKey:  "workflow-key-1",
	})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	signaled, err := client.SignalOrStartRun(context.Background(), WorkflowManagerSignalOrStartRunInput{
		ProviderName: "basic",
		WorkflowKey:  "workflow-key-1",
	})
	if err != nil {
		t.Fatalf("SignalOrStartRun: %v", err)
	}

	if started.Run == nil || started.Run.Status != WorkflowRunStatusValuePending {
		t.Fatalf("started run status = %#v, want pending", started.Run)
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
	if len(harness.signals) != 1 {
		t.Fatalf("signals len = %d, want 1", len(harness.signals))
	}
	if got := harness.signals[0].GetIdempotencyKey(); got != "request-key" {
		t.Fatalf("signal-or-start idempotency key = %q, want request-key", got)
	}
}
