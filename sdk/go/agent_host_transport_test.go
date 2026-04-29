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

	mu             sync.Mutex
	toolRequests   []*proto.ExecuteAgentToolRequest
	searchRequests []*proto.SearchAgentToolsRequest
	tokens         []string
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

func (h *agentHostTransportHarness) SearchTools(ctx context.Context, req *proto.SearchAgentToolsRequest) (*proto.SearchAgentToolsResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	h.mu.Lock()
	if values := md.Get("x-gestalt-host-service-relay-token"); len(values) > 0 {
		h.tokens = append(h.tokens, values...)
	}
	h.searchRequests = append(h.searchRequests, gproto.Clone(req).(*proto.SearchAgentToolsRequest))
	h.mu.Unlock()
	return &proto.SearchAgentToolsResponse{
		Tools: []*proto.ResolvedAgentTool{{
			Id:          "slack.send_message",
			Name:        "Send Slack message",
			Description: "Send a direct message",
		}},
		Candidates: []*proto.AgentToolCandidate{{
			Ref: &proto.AgentToolRef{
				Plugin:     "slack",
				Operation:  "search_messages",
				Connection: "workspace",
				Instance:   "primary",
			},
			Id:          "slack/search_messages/workspace/primary",
			Name:        "Search Slack messages",
			Description: "Search messages",
			Parameters:  []string{"query", "channel"},
			Score:       12.5,
		}},
		HasMore: true,
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
	searchResp, err := client.SearchTools(context.Background(), &proto.SearchAgentToolsRequest{
		SessionId:      "session-1",
		TurnId:         "turn-1",
		Query:          "send slack dm",
		MaxResults:     3,
		CandidateLimit: 12,
		LoadRefs: []*proto.AgentToolRef{{
			Plugin:     "slack",
			Operation:  "search_messages",
			Connection: "workspace",
			Instance:   "primary",
		}},
	})
	if err != nil {
		t.Fatalf("SearchTools: %v", err)
	}
	if len(searchResp.GetTools()) != 1 || searchResp.GetTools()[0].GetId() != "slack.send_message" || searchResp.GetTools()[0].GetName() != "Send Slack message" {
		t.Fatalf("search response = %#v", searchResp)
	}
	if len(searchResp.GetCandidates()) != 1 || searchResp.GetCandidates()[0].GetRef().GetOperation() != "search_messages" || !searchResp.GetHasMore() {
		t.Fatalf("search candidates response = %#v", searchResp)
	}

	harness.mu.Lock()
	defer harness.mu.Unlock()
	if len(harness.toolRequests) != 1 {
		t.Fatalf("toolRequests len = %d, want 1", len(harness.toolRequests))
	}
	if harness.toolRequests[0].GetSessionId() != "session-1" || harness.toolRequests[0].GetTurnId() != "turn-1" || harness.toolRequests[0].GetToolCallId() != "call-1" || harness.toolRequests[0].GetToolId() != "tool-1" {
		t.Fatalf("tool request = %#v", harness.toolRequests[0])
	}
	if len(harness.searchRequests) != 1 {
		t.Fatalf("searchRequests len = %d, want 1", len(harness.searchRequests))
	}
	if harness.searchRequests[0].GetSessionId() != "session-1" || harness.searchRequests[0].GetTurnId() != "turn-1" || harness.searchRequests[0].GetQuery() != "send slack dm" || harness.searchRequests[0].GetMaxResults() != 3 || harness.searchRequests[0].GetCandidateLimit() != 12 {
		t.Fatalf("search request = %#v", harness.searchRequests[0])
	}
	if len(harness.searchRequests[0].GetLoadRefs()) != 1 || harness.searchRequests[0].GetLoadRefs()[0].GetOperation() != "search_messages" {
		t.Fatalf("search load refs = %#v, want search_messages", harness.searchRequests[0].GetLoadRefs())
	}
	if len(harness.tokens) != 2 || harness.tokens[0] != "relay-token-go" || harness.tokens[1] != "relay-token-go" {
		t.Fatalf("relay tokens = %#v, want two relay-token-go values", harness.tokens)
	}
}
