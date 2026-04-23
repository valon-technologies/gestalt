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

type agentManagerTransportHarness struct {
	proto.UnimplementedAgentManagerHostServer

	mu       sync.Mutex
	requests []*proto.AgentManagerRunRequest
	tokens   []string
}

func (h *agentManagerTransportHarness) Run(ctx context.Context, req *proto.AgentManagerRunRequest) (*proto.ManagedAgentRun, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	h.mu.Lock()
	if values := md.Get("x-gestalt-host-service-relay-token"); len(values) > 0 {
		h.tokens = append(h.tokens, values...)
	}
	h.requests = append(h.requests, gproto.Clone(req).(*proto.AgentManagerRunRequest))
	h.mu.Unlock()

	return &proto.ManagedAgentRun{
		ProviderName: req.GetProviderName(),
		Run: &proto.BoundAgentRun{
			Id: "run-1",
		},
	}, nil
}

func TestTransport_AgentManagerTCPTargetTokenEnv(t *testing.T) {
	address := reserveTCPAddress()
	lis, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	harness := &agentManagerTransportHarness{}
	srv := grpc.NewServer()
	proto.RegisterAgentManagerHostServer(srv, harness)
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(srv.Stop)

	t.Setenv(gestalt.EnvAgentManagerSocket, "tcp://"+address)
	t.Setenv(gestalt.EnvAgentManagerSocketToken, "relay-token-go")

	client, err := gestalt.AgentManager("parent-token")
	if err != nil {
		t.Fatalf("AgentManager: %v", err)
	}
	defer func() { _ = client.Close() }()

	created, err := client.Run(context.Background(), &proto.AgentManagerRunRequest{
		ProviderName: "managed",
		Model:        "gpt-test",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if created.GetProviderName() != "managed" {
		t.Fatalf("provider_name = %q, want %q", created.GetProviderName(), "managed")
	}
	if created.GetRun().GetId() != "run-1" {
		t.Fatalf("run id = %q, want %q", created.GetRun().GetId(), "run-1")
	}

	harness.mu.Lock()
	defer harness.mu.Unlock()
	if len(harness.tokens) != 1 || harness.tokens[0] != "relay-token-go" {
		t.Fatalf("relay tokens = %#v, want [relay-token-go]", harness.tokens)
	}
	if len(harness.requests) != 1 {
		t.Fatalf("run requests len = %d, want 1", len(harness.requests))
	}
	if harness.requests[0].GetInvocationToken() != "parent-token" {
		t.Fatalf("invocation token = %q, want %q", harness.requests[0].GetInvocationToken(), "parent-token")
	}
	if harness.requests[0].GetProviderName() != "managed" || harness.requests[0].GetModel() != "gpt-test" {
		t.Fatalf("run request = %+v, want provider_name=managed model=gpt-test", harness.requests[0])
	}
}
