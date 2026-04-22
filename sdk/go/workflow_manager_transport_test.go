package gestalt_test

import (
	"context"
	"net"
	"sync"
	"testing"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	gproto "google.golang.org/protobuf/proto"
)

type workflowManagerTransportHarness struct {
	proto.UnimplementedWorkflowManagerHostServer

	mu       sync.Mutex
	requests []*proto.WorkflowManagerCreateScheduleRequest
	tokens   []string
}

func (h *workflowManagerTransportHarness) CreateSchedule(ctx context.Context, req *proto.WorkflowManagerCreateScheduleRequest) (*proto.ManagedWorkflowSchedule, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	h.mu.Lock()
	if values := md.Get("x-gestalt-host-service-relay-token"); len(values) > 0 {
		h.tokens = append(h.tokens, values...)
	}
	h.requests = append(h.requests, gproto.Clone(req).(*proto.WorkflowManagerCreateScheduleRequest))
	h.mu.Unlock()

	return &proto.ManagedWorkflowSchedule{
		ProviderName: req.GetProviderName(),
		Schedule: &proto.BoundWorkflowSchedule{
			Id:   "sched-1",
			Cron: req.GetCron(),
		},
	}, nil
}

func TestTransport_WorkflowManagerTCPTargetTokenEnv(t *testing.T) {
	address := reserveTCPAddress()
	lis, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	harness := &workflowManagerTransportHarness{}
	srv := grpc.NewServer()
	proto.RegisterWorkflowManagerHostServer(srv, harness)
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(srv.Stop)

	t.Setenv(gestalt.EnvWorkflowManagerSocket, "tcp://"+address)
	t.Setenv(gestalt.EnvWorkflowManagerSocketToken, "relay-token-go")

	client, err := gestalt.WorkflowManager("parent-token")
	if err != nil {
		t.Fatalf("WorkflowManager: %v", err)
	}
	defer func() { _ = client.Close() }()

	created, err := client.CreateSchedule(context.Background(), &proto.WorkflowManagerCreateScheduleRequest{
		ProviderName: "managed",
		Cron:         "*/5 * * * *",
	})
	if err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}
	if created.GetProviderName() != "managed" {
		t.Fatalf("provider_name = %q, want %q", created.GetProviderName(), "managed")
	}
	if created.GetSchedule().GetId() != "sched-1" {
		t.Fatalf("schedule id = %q, want %q", created.GetSchedule().GetId(), "sched-1")
	}

	harness.mu.Lock()
	defer harness.mu.Unlock()
	if len(harness.tokens) != 1 || harness.tokens[0] != "relay-token-go" {
		t.Fatalf("relay tokens = %#v, want [relay-token-go]", harness.tokens)
	}
	if len(harness.requests) != 1 {
		t.Fatalf("create schedule requests len = %d, want 1", len(harness.requests))
	}
	if harness.requests[0].GetInvocationToken() != "parent-token" {
		t.Fatalf("invocation token = %q, want %q", harness.requests[0].GetInvocationToken(), "parent-token")
	}
	if harness.requests[0].GetProviderName() != "managed" || harness.requests[0].GetCron() != "*/5 * * * *" {
		t.Fatalf("create schedule request = %+v, want provider_name=managed cron=*/5 * * * *", harness.requests[0])
	}
}
