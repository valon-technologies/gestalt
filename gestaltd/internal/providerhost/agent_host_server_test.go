package providerhost

import (
	"context"
	"testing"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestAgentHostServerExecuteToolPropagatesIdempotencyKey(t *testing.T) {
	t.Parallel()

	var captured coreagent.ExecuteToolRequest
	server := NewAgentHostServer("agent-provider", nil, func(_ context.Context, req coreagent.ExecuteToolRequest) (*coreagent.ExecuteToolResponse, error) {
		captured = req
		return &coreagent.ExecuteToolResponse{Status: 207, Body: `{"ok":true}`}, nil
	})
	conn := newBufconnConn(t, func(srv *grpc.Server) {
		proto.RegisterAgentHostServer(srv, server)
	})
	client := proto.NewAgentHostClient(conn)
	arguments, err := structpb.NewStruct(map[string]any{"taskId": "task-123"})
	if err != nil {
		t.Fatalf("structpb.NewStruct: %v", err)
	}

	resp, err := client.ExecuteTool(context.Background(), &proto.ExecuteAgentToolRequest{
		SessionId:      "session-1",
		TurnId:         "turn-1",
		ToolCallId:     "call-1",
		ToolId:         "roadmap.sync",
		Arguments:      arguments,
		IdempotencyKey: " tool-call-key-1 ",
	})
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	if resp.GetStatus() != 207 || resp.GetBody() != `{"ok":true}` {
		t.Fatalf("response = %#v", resp)
	}
	if captured.ProviderName != "agent-provider" {
		t.Fatalf("provider name = %q, want agent-provider", captured.ProviderName)
	}
	if captured.IdempotencyKey != "tool-call-key-1" {
		t.Fatalf("idempotency key = %q, want tool-call-key-1", captured.IdempotencyKey)
	}
	if captured.Arguments["taskId"] != "task-123" {
		t.Fatalf("arguments = %#v, want taskId", captured.Arguments)
	}
}

func TestAgentHostServerExecuteToolRequiresReplayKey(t *testing.T) {
	t.Parallel()

	server := NewAgentHostServer("agent-provider", nil, func(context.Context, coreagent.ExecuteToolRequest) (*coreagent.ExecuteToolResponse, error) {
		t.Fatal("executeTool should not be called")
		return nil, nil
	})
	conn := newBufconnConn(t, func(srv *grpc.Server) {
		proto.RegisterAgentHostServer(srv, server)
	})
	client := proto.NewAgentHostClient(conn)

	_, err := client.ExecuteTool(context.Background(), &proto.ExecuteAgentToolRequest{
		SessionId: "session-1",
		TurnId:    "turn-1",
		ToolId:    "roadmap.sync",
	})
	if err == nil {
		t.Fatal("ExecuteTool succeeded, want invalid argument")
	}
	if code := status.Code(err); code != codes.InvalidArgument {
		t.Fatalf("ExecuteTool code = %v, want %v", code, codes.InvalidArgument)
	}
}
