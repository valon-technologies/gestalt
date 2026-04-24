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

	mu              sync.Mutex
	sessionRequests []*proto.AgentManagerCreateSessionRequest
	turnRequests    []*proto.AgentManagerCreateTurnRequest
	tokens          []string
}

func (h *agentManagerTransportHarness) CreateSession(ctx context.Context, req *proto.AgentManagerCreateSessionRequest) (*proto.AgentSession, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	h.mu.Lock()
	if values := md.Get("x-gestalt-host-service-relay-token"); len(values) > 0 {
		h.tokens = append(h.tokens, values...)
	}
	h.sessionRequests = append(h.sessionRequests, gproto.Clone(req).(*proto.AgentManagerCreateSessionRequest))
	h.mu.Unlock()

	return &proto.AgentSession{
		Id:           "session-1",
		ProviderName: req.GetProviderName(),
		Model:        req.GetModel(),
		ClientRef:    req.GetClientRef(),
		State:        proto.AgentSessionState_AGENT_SESSION_STATE_ACTIVE,
	}, nil
}

func (h *agentManagerTransportHarness) CreateTurn(ctx context.Context, req *proto.AgentManagerCreateTurnRequest) (*proto.AgentTurn, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	h.mu.Lock()
	if values := md.Get("x-gestalt-host-service-relay-token"); len(values) > 0 {
		h.tokens = append(h.tokens, values...)
	}
	h.turnRequests = append(h.turnRequests, gproto.Clone(req).(*proto.AgentManagerCreateTurnRequest))
	h.mu.Unlock()

	return &proto.AgentTurn{
		Id:        "turn-1",
		SessionId: req.GetSessionId(),
		Model:     req.GetModel(),
		Status:    proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_RUNNING,
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

	session, err := client.CreateSession(context.Background(), &proto.AgentManagerCreateSessionRequest{
		ProviderName: "managed",
		Model:        "gpt-test",
		ClientRef:    "cli-session-1",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if session.GetProviderName() != "managed" {
		t.Fatalf("provider_name = %q, want %q", session.GetProviderName(), "managed")
	}
	if session.GetId() != "session-1" {
		t.Fatalf("session id = %q, want %q", session.GetId(), "session-1")
	}

	turn, err := client.CreateTurn(context.Background(), &proto.AgentManagerCreateTurnRequest{
		SessionId: "session-1",
		Model:     "gpt-test",
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if turn.GetId() != "turn-1" {
		t.Fatalf("turn id = %q, want %q", turn.GetId(), "turn-1")
	}

	harness.mu.Lock()
	defer harness.mu.Unlock()
	if len(harness.tokens) != 2 || harness.tokens[0] != "relay-token-go" || harness.tokens[1] != "relay-token-go" {
		t.Fatalf("relay tokens = %#v, want [relay-token-go relay-token-go]", harness.tokens)
	}
	if len(harness.sessionRequests) != 1 {
		t.Fatalf("session requests len = %d, want 1", len(harness.sessionRequests))
	}
	if harness.sessionRequests[0].GetInvocationToken() != "parent-token" {
		t.Fatalf("session invocation token = %q, want %q", harness.sessionRequests[0].GetInvocationToken(), "parent-token")
	}
	if harness.sessionRequests[0].GetProviderName() != "managed" || harness.sessionRequests[0].GetModel() != "gpt-test" {
		t.Fatalf("session request = %+v, want provider_name=managed model=gpt-test", harness.sessionRequests[0])
	}
	if len(harness.turnRequests) != 1 {
		t.Fatalf("turn requests len = %d, want 1", len(harness.turnRequests))
	}
	if harness.turnRequests[0].GetInvocationToken() != "parent-token" {
		t.Fatalf("turn invocation token = %q, want %q", harness.turnRequests[0].GetInvocationToken(), "parent-token")
	}
	if harness.turnRequests[0].GetSessionId() != "session-1" || harness.turnRequests[0].GetModel() != "gpt-test" {
		t.Fatalf("turn request = %+v, want session_id=session-1 model=gpt-test", harness.turnRequests[0])
	}
}
