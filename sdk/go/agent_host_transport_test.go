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

type agentHostTransportHarness struct {
	proto.UnimplementedAgentHostServer

	mu           sync.Mutex
	toolRequests []*proto.ExecuteAgentToolRequest
	tokens       []string
}

func (h *agentHostTransportHarness) ExecuteTool(ctx context.Context, req *proto.ExecuteAgentToolRequest) (*proto.ExecuteAgentToolResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	h.mu.Lock()
	if values := md.Get("x-gestalt-host-service-relay-token"); len(values) > 0 {
		h.tokens = append(h.tokens, values...)
	}
	h.toolRequests = append(h.toolRequests, gproto.Clone(req).(*proto.ExecuteAgentToolRequest))
	h.mu.Unlock()
	return &proto.ExecuteAgentToolResponse{Status: 207, Body: `{"ok":true}`}, nil
}

func TestTransport_AgentHostUnixSocket(t *testing.T) {
	socketPath := newSocketPath(t, "agent-host.sock")
	lis, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	harness := &agentHostTransportHarness{}
	srv := grpc.NewServer()
	proto.RegisterAgentHostServer(srv, harness)
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(srv.Stop)

	t.Setenv(gestalt.EnvAgentHostSocket, socketPath)
	t.Setenv(gestalt.EnvAgentHostSocketToken, "relay-token-go")

	client, err := gestalt.AgentHost()
	if err != nil {
		t.Fatalf("AgentHost: %v", err)
	}
	defer func() { _ = client.Close() }()

	resp, err := client.ExecuteTool(context.Background(), &proto.ExecuteAgentToolRequest{
		SessionId:  "session-1",
		TurnId:     "turn-1",
		ToolCallId: "call-1",
		ToolId:     "tool-1",
	})
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	if resp.GetStatus() != 207 || resp.GetBody() != `{"ok":true}` {
		t.Fatalf("response = %#v", resp)
	}

	harness.mu.Lock()
	defer harness.mu.Unlock()
	if len(harness.toolRequests) != 1 {
		t.Fatalf("toolRequests len = %d, want 1", len(harness.toolRequests))
	}
	if harness.toolRequests[0].GetSessionId() != "session-1" || harness.toolRequests[0].GetTurnId() != "turn-1" || harness.toolRequests[0].GetToolCallId() != "call-1" || harness.toolRequests[0].GetToolId() != "tool-1" {
		t.Fatalf("tool request = %#v", harness.toolRequests[0])
	}
	if len(harness.tokens) != 1 || harness.tokens[0] != "relay-token-go" {
		t.Fatalf("relay tokens = %#v, want one relay-token-go value", harness.tokens)
	}
}
