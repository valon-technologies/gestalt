package agents

import (
	"context"
	"testing"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/internal/testutil/metrictest"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestHostServerExecuteToolPropagatesIdempotencyKey(t *testing.T) {
	t.Parallel()

	var captured coreagent.ExecuteToolRequest
	server := NewHostServer("agent-provider", nil, nil, func(_ context.Context, req coreagent.ExecuteToolRequest) (*coreagent.ExecuteToolResponse, error) {
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

func TestHostServerExecuteToolRequiresReplayKey(t *testing.T) {
	t.Parallel()

	server := NewHostServer("agent-provider", nil, nil, func(context.Context, coreagent.ExecuteToolRequest) (*coreagent.ExecuteToolResponse, error) {
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

func TestHostServerExecuteToolRecordsGenAITelemetry(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	ctx := metricutil.WithMeterProvider(context.Background(), metrics.Provider)
	server := NewHostServer("agent-provider", nil, nil, func(context.Context, coreagent.ExecuteToolRequest) (*coreagent.ExecuteToolResponse, error) {
		return &coreagent.ExecuteToolResponse{Status: 200, Body: `{"ok":true}`}, nil
	})

	_, err := server.ExecuteTool(ctx, &proto.ExecuteAgentToolRequest{
		SessionId:  "session-1",
		TurnId:     "turn-1",
		ToolCallId: "call-1",
		ToolId:     "roadmap.sync",
	})
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	attrs := map[string]string{
		"gen_ai.operation.name": "execute_tool",
		"gen_ai.provider.name":  "gestalt",
		"gen_ai.agent.name":     "agent-provider",
		"gen_ai.tool.name":      "roadmap.sync",
		"gen_ai.tool.type":      "extension",
	}
	metrictest.RequireFloat64Histogram(t, rm, "gen_ai.client.operation.duration", attrs)
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "gen_ai.client.operation.duration", attrs, "gen_ai.tool.call.id")
}

func TestAgentHostServerListToolsForwardsCatalogRequest(t *testing.T) {
	t.Parallel()

	var captured coreagent.ListToolsRequest
	server := NewHostServer("agent-provider", nil, func(_ context.Context, req coreagent.ListToolsRequest) (*coreagent.ListToolsResponse, error) {
		captured = req
		readOnly := true
		return &coreagent.ListToolsResponse{
			Tools: []coreagent.ListedTool{{
				ToolID:          "tool-1",
				MCPName:         "linear__issues_create",
				Title:           "Create issue",
				Description:     "Create a Linear issue",
				InputSchemaJSON: `{"type":"object"}`,
				Annotations:     core.CapabilityAnnotations{ReadOnlyHint: &readOnly},
				Ref: coreagent.ToolRef{
					Plugin:    "linear",
					Operation: "issues.create",
				},
			}},
			NextPageToken: "10",
		}, nil
	}, nil)
	conn := newBufconnConn(t, func(srv *grpc.Server) {
		proto.RegisterAgentHostServer(srv, server)
	})
	client := proto.NewAgentHostClient(conn)

	resp, err := client.ListTools(context.Background(), &proto.ListAgentToolsRequest{
		SessionId: "session-1",
		TurnId:    "turn-1",
		PageSize:  10,
		PageToken: " 0 ",
		ToolGrant: " grant-token ",
	})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if captured.ProviderName != "agent-provider" || captured.SessionID != "session-1" || captured.TurnID != "turn-1" {
		t.Fatalf("captured request = %#v", captured)
	}
	if captured.PageSize != 10 || captured.PageToken != "0" || captured.ToolGrant != "grant-token" {
		t.Fatalf("captured paging/grant = %#v", captured)
	}
	if resp.GetNextPageToken() != "10" || len(resp.GetTools()) != 1 {
		t.Fatalf("ListTools response = %#v", resp)
	}
	tool := resp.GetTools()[0]
	if tool.GetMcpName() != "linear__issues_create" || tool.GetInputSchema() != `{"type":"object"}` {
		t.Fatalf("listed tool = %#v", tool)
	}
	if tool.GetAnnotations() == nil || !tool.GetAnnotations().GetReadOnlyHint() {
		t.Fatalf("annotations = %#v, want read_only_hint", tool.GetAnnotations())
	}
}

func TestGenAIToolExecutionAttrsAvoidDuplicateAgentIdentity(t *testing.T) {
	t.Parallel()

	spanAttrs, metricAttrs := genAIToolExecutionAttrs("agent-provider", "session-1", "turn-1", "roadmap.sync", "call-1")

	requireAttr(t, spanAttrs, "gen_ai.agent.name", "agent-provider")
	requireAttr(t, metricAttrs, "gen_ai.agent.name", "agent-provider")
	requireMissingAttr(t, spanAttrs, "gestalt.agent.provider")
	requireMissingAttr(t, metricAttrs, "gestalt.agent.provider")
}

func TestGenAIErrorAttrOmitsNil(t *testing.T) {
	t.Parallel()

	if attr, ok := genAIErrorAttr(nil); ok {
		t.Fatalf("genAIErrorAttr(nil) = %v, true; want no attribute", attr)
	}
	attr, ok := genAIErrorAttr(status.Error(codes.NotFound, "missing"))
	if !ok {
		t.Fatal("genAIErrorAttr(non-nil) returned no attribute")
	}
	if string(attr.Key) != "error.type" || attr.Value.AsString() != codes.NotFound.String() {
		t.Fatalf("genAIErrorAttr(non-nil) = %v, want error.type=%s", attr, codes.NotFound.String())
	}
}

func requireAttr(t *testing.T, attrs []attribute.KeyValue, key, value string) {
	t.Helper()
	for _, attr := range attrs {
		if string(attr.Key) == key {
			if attr.Value.AsString() != value {
				t.Fatalf("%s = %q, want %q", key, attr.Value.AsString(), value)
			}
			return
		}
	}
	t.Fatalf("missing attribute %q in %v", key, attrs)
}

func requireMissingAttr(t *testing.T, attrs []attribute.KeyValue, key string) {
	t.Helper()
	for _, attr := range attrs {
		if string(attr.Key) == key {
			t.Fatalf("unexpected attribute %q in %v", key, attrs)
		}
	}
}
