package gestalt_test

import (
	"context"
	"net"
	"sync"
	"testing"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
)

type agentHostTransportHarness struct {
	proto.UnimplementedAgentHostServer

	mu            sync.Mutex
	toolRequests  []*proto.ExecuteAgentToolRequest
	eventRequests []*proto.EmitAgentEventRequest
}

func (h *agentHostTransportHarness) ExecuteTool(ctx context.Context, req *proto.ExecuteAgentToolRequest) (*proto.ExecuteAgentToolResponse, error) {
	h.mu.Lock()
	h.toolRequests = append(h.toolRequests, gproto.Clone(req).(*proto.ExecuteAgentToolRequest))
	h.mu.Unlock()
	return &proto.ExecuteAgentToolResponse{Status: 207, Body: `{"ok":true}`}, nil
}

func (h *agentHostTransportHarness) EmitEvent(ctx context.Context, req *proto.EmitAgentEventRequest) (*emptypb.Empty, error) {
	h.mu.Lock()
	h.eventRequests = append(h.eventRequests, gproto.Clone(req).(*proto.EmitAgentEventRequest))
	h.mu.Unlock()
	return &emptypb.Empty{}, nil
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

	client, err := gestalt.AgentHost()
	if err != nil {
		t.Fatalf("AgentHost: %v", err)
	}
	defer func() { _ = client.Close() }()

	resp, err := client.ExecuteTool(context.Background(), &proto.ExecuteAgentToolRequest{
		RunId:      "run-1",
		ToolCallId: "call-1",
		ToolId:     "tool-1",
	})
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	if resp.GetStatus() != 207 || resp.GetBody() != `{"ok":true}` {
		t.Fatalf("response = %#v", resp)
	}
	data, err := structpb.NewStruct(map[string]any{"phase": "tool_call", "attempt": float64(1)})
	if err != nil {
		t.Fatalf("NewStruct: %v", err)
	}
	if err := client.EmitEvent(context.Background(), &proto.EmitAgentEventRequest{
		RunId:      "run-1",
		Type:       "agent.tool_call.started",
		Visibility: "public",
		Data:       data,
	}); err != nil {
		t.Fatalf("EmitEvent: %v", err)
	}

	harness.mu.Lock()
	defer harness.mu.Unlock()
	if len(harness.toolRequests) != 1 {
		t.Fatalf("toolRequests len = %d, want 1", len(harness.toolRequests))
	}
	if harness.toolRequests[0].GetRunId() != "run-1" || harness.toolRequests[0].GetToolCallId() != "call-1" || harness.toolRequests[0].GetToolId() != "tool-1" {
		t.Fatalf("tool request = %#v", harness.toolRequests[0])
	}
	if len(harness.eventRequests) != 1 {
		t.Fatalf("eventRequests len = %d, want 1", len(harness.eventRequests))
	}
	if harness.eventRequests[0].GetRunId() != "run-1" || harness.eventRequests[0].GetType() != "agent.tool_call.started" || harness.eventRequests[0].GetVisibility() != "public" {
		t.Fatalf("event request = %#v", harness.eventRequests[0])
	}
	if got := harness.eventRequests[0].GetData().GetFields()["phase"].GetStringValue(); got != "tool_call" {
		t.Fatalf("event phase = %q, want tool_call", got)
	}
}
