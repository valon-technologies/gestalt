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
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
)

type agentHostTransportHarness struct {
	proto.UnimplementedAgentHostServer

	mu            sync.Mutex
	toolRequests  []*proto.ExecuteAgentToolRequest
	eventRequests []*proto.EmitAgentEventRequest
	interactions  []*proto.RequestAgentInteractionRequest
	tokens        []string
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

func (h *agentHostTransportHarness) EmitEvent(ctx context.Context, req *proto.EmitAgentEventRequest) (*emptypb.Empty, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	h.mu.Lock()
	if values := md.Get("x-gestalt-host-service-relay-token"); len(values) > 0 {
		h.tokens = append(h.tokens, values...)
	}
	h.eventRequests = append(h.eventRequests, gproto.Clone(req).(*proto.EmitAgentEventRequest))
	h.mu.Unlock()
	return &emptypb.Empty{}, nil
}

func (h *agentHostTransportHarness) RequestInteraction(ctx context.Context, req *proto.RequestAgentInteractionRequest) (*proto.AgentInteraction, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	h.mu.Lock()
	if values := md.Get("x-gestalt-host-service-relay-token"); len(values) > 0 {
		h.tokens = append(h.tokens, values...)
	}
	h.interactions = append(h.interactions, gproto.Clone(req).(*proto.RequestAgentInteractionRequest))
	h.mu.Unlock()

	return &proto.AgentInteraction{
		Id:      "interaction-1",
		RunId:   req.GetRunId(),
		Type:    req.GetType(),
		State:   proto.AgentInteractionState_AGENT_INTERACTION_STATE_PENDING,
		Title:   req.GetTitle(),
		Prompt:  req.GetPrompt(),
		Request: req.GetRequest(),
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
	interaction, err := client.RequestInteraction(context.Background(), &proto.RequestAgentInteractionRequest{
		RunId:  "run-1",
		Type:   proto.AgentInteractionType_AGENT_INTERACTION_TYPE_APPROVAL,
		Title:  "Approve command",
		Prompt: "Run git status?",
		Request: func() *structpb.Struct {
			value, err := structpb.NewStruct(map[string]any{"command": []any{"git", "status"}})
			if err != nil {
				t.Fatalf("NewStruct interaction request: %v", err)
			}
			return value
		}(),
	})
	if err != nil {
		t.Fatalf("RequestInteraction: %v", err)
	}
	if interaction.GetId() != "interaction-1" || interaction.GetRunId() != "run-1" {
		t.Fatalf("interaction = %#v", interaction)
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
	if len(harness.interactions) != 1 {
		t.Fatalf("interactions len = %d, want 1", len(harness.interactions))
	}
	if len(harness.tokens) != 3 || harness.tokens[0] != "relay-token-go" || harness.tokens[1] != "relay-token-go" || harness.tokens[2] != "relay-token-go" {
		t.Fatalf("relay tokens = %#v, want three relay-token-go values", harness.tokens)
	}
	if harness.eventRequests[0].GetRunId() != "run-1" || harness.eventRequests[0].GetType() != "agent.tool_call.started" || harness.eventRequests[0].GetVisibility() != "public" {
		t.Fatalf("event request = %#v", harness.eventRequests[0])
	}
	if got := harness.eventRequests[0].GetData().GetFields()["phase"].GetStringValue(); got != "tool_call" {
		t.Fatalf("event phase = %q, want tool_call", got)
	}
	if harness.interactions[0].GetType() != proto.AgentInteractionType_AGENT_INTERACTION_TYPE_APPROVAL || harness.interactions[0].GetTitle() != "Approve command" {
		t.Fatalf("interaction request = %#v", harness.interactions[0])
	}
}
