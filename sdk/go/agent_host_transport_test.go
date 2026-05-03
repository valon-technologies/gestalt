package gestalt_test

import (
	"context"
	"net"
	"sync"
	"testing"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	gproto "google.golang.org/protobuf/proto"
)

type agentHostTransportHarness struct {
	proto.UnimplementedAgentHostServer

	mu           sync.Mutex
	toolRequests []*proto.ExecuteAgentToolRequest
	listRequests []*proto.ListAgentToolsRequest
	connRequests []*proto.ResolveAgentConnectionRequest
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

func (h *agentHostTransportHarness) ListTools(ctx context.Context, req *proto.ListAgentToolsRequest) (*proto.ListAgentToolsResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	h.mu.Lock()
	if values := md.Get("x-gestalt-host-service-relay-token"); len(values) > 0 {
		h.tokens = append(h.tokens, values...)
	}
	h.listRequests = append(h.listRequests, gproto.Clone(req).(*proto.ListAgentToolsRequest))
	h.mu.Unlock()
	return &proto.ListAgentToolsResponse{
		Tools: []*proto.ListedAgentTool{{
			Id:          "tool-2",
			McpName:     "slack__send_message",
			Title:       "Send Slack message",
			Description: "Send a direct message",
		}},
		NextPageToken: "next-page",
	}, nil
}

func (h *agentHostTransportHarness) ResolveConnection(ctx context.Context, req *proto.ResolveAgentConnectionRequest) (*proto.ResolvedAgentConnection, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	h.mu.Lock()
	if values := md.Get("x-gestalt-host-service-relay-token"); len(values) > 0 {
		h.tokens = append(h.tokens, values...)
	}
	h.connRequests = append(h.connRequests, gproto.Clone(req).(*proto.ResolveAgentConnectionRequest))
	h.mu.Unlock()
	return &proto.ResolvedAgentConnection{
		ConnectionId: "vertex-ai",
		Connection:   req.GetConnection(),
		Instance:     req.GetInstance(),
		Mode:         "platform",
		Headers:      map[string]string{"authorization": "Bearer token"},
		Params:       map[string]string{"endpoint": "vertex-endpoint"},
	}, nil
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
		SessionId:      "session-1",
		TurnId:         "turn-1",
		ToolCallId:     "call-1",
		ToolId:         "tool-1",
		IdempotencyKey: "tool-call-key-1",
	})
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	if resp.GetStatus() != 207 || resp.GetBody() != `{"ok":true}` {
		t.Fatalf("response = %#v", resp)
	}
	listResp, err := client.ListTools(context.Background(), &proto.ListAgentToolsRequest{
		SessionId: "session-1",
		TurnId:    "turn-1",
		PageSize:  25,
		PageToken: "page-1",
	})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(listResp.GetTools()) != 1 || listResp.GetTools()[0].GetId() != "tool-2" || listResp.GetTools()[0].GetMcpName() != "slack__send_message" || listResp.GetNextPageToken() != "next-page" {
		t.Fatalf("list response = %#v", listResp)
	}
	connResp, err := client.ResolveConnection(context.Background(), &proto.ResolveAgentConnectionRequest{
		SessionId:  "session-1",
		TurnId:     "turn-1",
		Connection: "model",
		Instance:   "default",
		RunGrant:   "run-grant-1",
	})
	if err != nil {
		t.Fatalf("ResolveConnection: %v", err)
	}
	if connResp.GetConnectionId() != "vertex-ai" || connResp.GetHeaders()["authorization"] != "Bearer token" || connResp.GetParams()["endpoint"] != "vertex-endpoint" {
		t.Fatalf("connection response = %#v", connResp)
	}

	harness.mu.Lock()
	defer harness.mu.Unlock()
	if len(harness.toolRequests) != 1 {
		t.Fatalf("toolRequests len = %d, want 1", len(harness.toolRequests))
	}
	if harness.toolRequests[0].GetSessionId() != "session-1" || harness.toolRequests[0].GetTurnId() != "turn-1" || harness.toolRequests[0].GetToolCallId() != "call-1" || harness.toolRequests[0].GetToolId() != "tool-1" || harness.toolRequests[0].GetIdempotencyKey() != "tool-call-key-1" {
		t.Fatalf("tool request = %#v", harness.toolRequests[0])
	}
	if len(harness.listRequests) != 1 {
		t.Fatalf("listRequests len = %d, want 1", len(harness.listRequests))
	}
	if harness.listRequests[0].GetSessionId() != "session-1" || harness.listRequests[0].GetTurnId() != "turn-1" || harness.listRequests[0].GetPageSize() != 25 || harness.listRequests[0].GetPageToken() != "page-1" {
		t.Fatalf("list request = %#v", harness.listRequests[0])
	}
	if len(harness.connRequests) != 1 {
		t.Fatalf("connRequests len = %d, want 1", len(harness.connRequests))
	}
	if harness.connRequests[0].GetSessionId() != "session-1" || harness.connRequests[0].GetTurnId() != "turn-1" || harness.connRequests[0].GetConnection() != "model" || harness.connRequests[0].GetInstance() != "default" || harness.connRequests[0].GetRunGrant() != "run-grant-1" {
		t.Fatalf("connection request = %#v", harness.connRequests[0])
	}
	if len(harness.tokens) != 3 || harness.tokens[0] != "relay-token-go" || harness.tokens[1] != "relay-token-go" || harness.tokens[2] != "relay-token-go" {
		t.Fatalf("relay tokens = %#v, want three relay-token-go values", harness.tokens)
	}
}
