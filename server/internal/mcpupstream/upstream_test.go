package mcpupstream

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/egress"
	"github.com/valon-technologies/gestalt/server/internal/egress/egresstest"

	mcpclient "github.com/mark3labs/mcp-go/client"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

func newTestServer() *mcpserver.MCPServer {
	srv := mcpserver.NewMCPServer("test-remote", "1.0.0")

	srv.AddTool(
		mcpgo.NewToolWithRawSchema("run_query", "Execute a SQL query",
			[]byte(`{"type":"object","properties":{"sql":{"type":"string","description":"SQL query"}}}`)),
		func(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			sql, _ := req.GetArguments()["sql"].(string)
			return mcpgo.NewToolResultText("result for: " + sql), nil
		},
	)
	srv.AddTool(
		mcpgo.NewTool("list_databases", mcpgo.WithDescription("List all databases")),
		func(_ context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			return mcpgo.NewToolResultText("db1, db2"), nil
		},
	)
	return srv
}

func newTestUpstream(t *testing.T) *Upstream {
	t.Helper()
	return newTestUpstreamFromServer(t, "clickhouse", newTestServer())
}

func newTestUpstreamFromServer(t *testing.T, name string, srv *mcpserver.MCPServer) *Upstream {
	t.Helper()

	client, err := mcpclient.NewInProcessClient(srv)
	if err != nil {
		t.Fatalf("creating in-process client: %v", err)
	}

	ctx := context.Background()
	if err := client.Start(ctx); err != nil {
		t.Fatalf("starting client: %v", err)
	}

	initReq := mcpgo.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcpgo.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcpgo.Implementation{Name: "test", Version: "0.0.1"}
	if _, err := client.Initialize(ctx, initReq); err != nil {
		t.Fatalf("initialize: %v", err)
	}

	toolsResult, err := client.ListTools(ctx, mcpgo.ListToolsRequest{})
	if err != nil {
		t.Fatalf("listing tools: %v", err)
	}

	return newFromClient(name, client, core.ConnectionModeUser, toolsResult.Tools)
}

func newAuthenticatedHTTPTestServer(t *testing.T, expectedAuth string) *httptest.Server {
	t.Helper()
	return newHeaderAuthenticatedHTTPTestServer(t, map[string]string{"Authorization": expectedAuth})
}

func newHeaderAuthenticatedHTTPTestServer(t *testing.T, expectedHeaders map[string]string) *httptest.Server {
	t.Helper()

	handler := mcpserver.NewStreamableHTTPServer(
		newTestServer(),
		mcpserver.WithStateLess(true),
	)

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for name, value := range expectedHeaders {
			if r.Header.Get(name) != value {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		handler.ServeHTTP(w, r)
	}))
}

func TestUpstream_DiscoverTools(t *testing.T) {
	t.Parallel()

	u := newTestUpstream(t)
	t.Cleanup(func() { _ = u.Close() })

	ops := u.ListOperations()
	if len(ops) != 2 {
		t.Fatalf("expected 2 operations, got %d", len(ops))
	}

	opNames := make(map[string]bool)
	for _, op := range ops {
		opNames[op.Name] = true
	}
	if !opNames["run_query"] || !opNames["list_databases"] {
		t.Fatalf("unexpected operations: %v", ops)
	}

	cat := u.Catalog()
	if cat == nil {
		t.Fatal("expected non-nil catalog")
	}
	if len(cat.Operations) != 2 {
		t.Fatalf("expected 2 catalog operations, got %d", len(cat.Operations))
	}

	for _, op := range cat.Operations {
		if op.ID == "run_query" && len(op.InputSchema) == 0 {
			t.Fatal("expected run_query to have an InputSchema")
		}
	}
}

func TestUpstream_CallToolPassthrough(t *testing.T) {
	t.Parallel()

	u := newTestUpstream(t)
	t.Cleanup(func() { _ = u.Close() })

	result, err := u.CallTool(context.Background(), "run_query", map[string]any{"sql": "SELECT 1"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatal("expected no error in result")
	}
	if len(result.Content) == 0 {
		t.Fatal("expected content in result")
	}

	text, ok := result.Content[0].(mcpgo.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	if text.Text != "result for: SELECT 1" {
		t.Fatalf("unexpected text: %q", text.Text)
	}
}

func TestUpstream_ExecuteReturnsError(t *testing.T) {
	t.Parallel()

	u := newTestUpstream(t)
	t.Cleanup(func() { _ = u.Close() })

	_, err := u.Execute(context.Background(), "run_query", nil, "token")
	if err != core.ErrMCPOnly {
		t.Fatalf("expected ErrMCPOnly, got %v", err)
	}
}

func TestUpstream_FilterOperations(t *testing.T) {
	t.Parallel()

	u := newTestUpstream(t)
	t.Cleanup(func() { _ = u.Close() })

	err := u.FilterOperations(map[string]*config.OperationOverride{"run_query": nil})
	if err != nil {
		t.Fatalf("FilterOperations: %v", err)
	}

	ops := u.ListOperations()
	if len(ops) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(ops))
	}
	if ops[0].Name != "run_query" {
		t.Fatalf("expected run_query, got %q", ops[0].Name)
	}
	if ops[0].Description != "Execute a SQL query" {
		t.Fatalf("expected spec description, got %q", ops[0].Description)
	}

	cat := u.Catalog()
	if len(cat.Operations) != 1 {
		t.Fatalf("expected 1 catalog operation, got %d", len(cat.Operations))
	}
	if cat.Operations[0].ID != "run_query" {
		t.Fatalf("expected run_query in catalog, got %q", cat.Operations[0].ID)
	}
}

func TestUpstream_FilterOperationsWithOverride(t *testing.T) {
	t.Parallel()

	u := newTestUpstream(t)
	t.Cleanup(func() { _ = u.Close() })

	err := u.FilterOperations(map[string]*config.OperationOverride{
		"run_query":      {Description: "Custom query description"},
		"list_databases": nil,
	})
	if err != nil {
		t.Fatalf("FilterOperations: %v", err)
	}

	ops := u.ListOperations()
	if len(ops) != 2 {
		t.Fatalf("expected 2 operations, got %d", len(ops))
	}

	for _, op := range ops {
		switch op.Name {
		case "run_query":
			if op.Description != "Custom query description" {
				t.Errorf("run_query description: got %q, want override", op.Description)
			}
		case "list_databases":
			if op.Description != "List all databases" {
				t.Errorf("list_databases description: got %q, want spec default", op.Description)
			}
		}
	}
}

func TestUpstream_FilterOperationsUnknown(t *testing.T) {
	t.Parallel()

	u := newTestUpstream(t)
	t.Cleanup(func() { _ = u.Close() })

	err := u.FilterOperations(map[string]*config.OperationOverride{"nonexistent": nil})
	if err == nil {
		t.Fatal("expected error for unknown operation")
	}
}

func TestUpstream_FilterOperationsEmpty(t *testing.T) {
	t.Parallel()

	u := newTestUpstream(t)
	t.Cleanup(func() { _ = u.Close() })

	err := u.FilterOperations(map[string]*config.OperationOverride{})
	if err == nil {
		t.Fatal("expected error for empty allowed_operations")
	}
}

func TestUpstream_DiscoverAfterFilterWithAlias(t *testing.T) {
	t.Parallel()

	u := newTestUpstream(t)
	t.Cleanup(func() { _ = u.Close() })

	err := u.FilterOperations(map[string]*config.OperationOverride{
		"run_query": {Alias: "query"},
	})
	if err != nil {
		t.Fatalf("FilterOperations: %v", err)
	}

	cat, _, err := u.discover(context.Background(), "")
	if err != nil {
		t.Fatalf("discover after filter with alias: %v", err)
	}
	if len(cat.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(cat.Operations))
	}
	if cat.Operations[0].ID != "query" {
		t.Errorf("expected aliased ID %q, got %q", "query", cat.Operations[0].ID)
	}
}

func TestUpstream_ProviderMetadata(t *testing.T) {
	t.Parallel()

	u := newTestUpstream(t)
	t.Cleanup(func() { _ = u.Close() })

	if u.Name() != "clickhouse" {
		t.Fatalf("Name = %q", u.Name())
	}
	if u.ConnectionMode() != core.ConnectionModeUser {
		t.Fatalf("ConnectionMode = %q", u.ConnectionMode())
	}
	if !u.SupportsManualAuth() {
		t.Fatal("expected SupportsManualAuth to be true")
	}
}

func TestUpstream_LazyDiscoveryUsesRequestToken(t *testing.T) {
	t.Parallel()

	ts := newAuthenticatedHTTPTestServer(t, "Bearer secret-token")
	t.Cleanup(ts.Close)

	u, err := New(context.Background(), "clickhouse", ts.URL, core.ConnectionModeUser, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	cat, err := u.CatalogForRequest(context.Background(), "secret-token")
	if err != nil {
		t.Fatalf("CatalogForRequest: %v", err)
	}
	if len(cat.Operations) != 2 {
		t.Fatalf("expected 2 operations, got %d", len(cat.Operations))
	}

	ctx := WithUpstreamToken(context.Background(), "secret-token")
	result, err := u.CallTool(ctx, "run_query", map[string]any{"sql": "SELECT 1"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %v", result.Content)
	}
}

func TestUpstream_StaticHeadersReachLazyDiscoveryAndToolCalls(t *testing.T) {
	t.Parallel()

	const headerName = "X-Static-Version"
	const headerValue = "2026-02-09"

	ts := newHeaderAuthenticatedHTTPTestServer(t, map[string]string{headerName: headerValue})
	t.Cleanup(ts.Close)

	u, err := New(context.Background(), "clickhouse", ts.URL, core.ConnectionModeNone, map[string]string{
		"x-static-version": headerValue,
	}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	cat, err := u.CatalogForRequest(context.Background(), "")
	if err != nil {
		t.Fatalf("CatalogForRequest: %v", err)
	}
	if len(cat.Operations) != 2 {
		t.Fatalf("expected 2 operations, got %d", len(cat.Operations))
	}

	result, err := u.CallTool(context.Background(), "run_query", map[string]any{"sql": "SELECT 1"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %v", result.Content)
	}
}

func TestUpstream_UsesSharedEgressResolver(t *testing.T) {
	t.Parallel()

	var gotPolicy egress.PolicyInput
	ts := newAuthenticatedHTTPTestServer(t, "Bearer secret-token")
	t.Cleanup(ts.Close)

	u, err := New(context.Background(), "clickhouse", ts.URL, core.ConnectionModeUser, nil, &egress.Resolver{
		Subjects: egress.ContextSubjectResolver{},
		Policy: egresstest.PolicyFunc(func(_ context.Context, input egress.PolicyInput) error {
			gotPolicy = input
			return nil
		}),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := egress.WithSubject(context.Background(), egress.Subject{Kind: egress.SubjectUser, ID: "u1"})
	cat, err := u.CatalogForRequest(ctx, "secret-token")
	if err != nil {
		t.Fatalf("CatalogForRequest: %v", err)
	}
	if len(cat.Operations) != 2 {
		t.Fatalf("expected 2 operations, got %d", len(cat.Operations))
	}
	if gotPolicy.Subject != (egress.Subject{Kind: egress.SubjectUser, ID: "u1"}) {
		t.Fatalf("subject = %+v, want user u1", gotPolicy.Subject)
	}
	if gotPolicy.Target.Method == "" {
		t.Fatal("expected target method to be populated")
	}

	callCtx := WithUpstreamToken(
		egress.WithSubject(context.Background(), egress.Subject{Kind: egress.SubjectUser, ID: "u1"}),
		"secret-token",
	)
	result, err := u.CallTool(callCtx, "run_query", map[string]any{"sql": "SELECT 1"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %v", result.Content)
	}
}

func TestUpstream_CallToolForwardsMeta(t *testing.T) {
	t.Parallel()

	srv := mcpserver.NewMCPServer("meta-test", "1.0.0")
	srv.AddTool(
		mcpgo.NewTool("check_meta", mcpgo.WithDescription("checks meta")),
		func(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			if req.Params.Meta == nil {
				return nil, fmt.Errorf("expected _meta to be forwarded")
			}
			return mcpgo.NewToolResultText("ok"), nil
		},
	)

	u := newTestUpstreamFromServer(t, "meta-upstream", srv)
	t.Cleanup(func() { _ = u.Close() })

	ctx := WithCallToolMeta(context.Background(), &mcpgo.Meta{ProgressToken: mcpgo.ProgressToken("tok-1")})
	result, err := u.CallTool(ctx, "check_meta", nil)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %v", result.Content)
	}
}

func TestUpstream_DiscoverToolsPreservesOutputSchema(t *testing.T) {
	t.Parallel()

	outputSchema := json.RawMessage(`{"type":"object","properties":{"count":{"type":"integer"}}}`)

	srv := mcpserver.NewMCPServer("output-test", "1.0.0")
	srv.AddTool(
		mcpgo.NewTool("typed_op", mcpgo.WithDescription("has output schema"), mcpgo.WithRawOutputSchema(outputSchema)),
		func(_ context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			return mcpgo.NewToolResultText("ok"), nil
		},
	)

	u := newTestUpstreamFromServer(t, "output-upstream", srv)
	t.Cleanup(func() { _ = u.Close() })

	cat := u.Catalog()
	if cat == nil {
		t.Fatal("expected non-nil catalog")
	}
	if len(cat.Operations) != 1 {
		t.Fatalf("expected 1 catalog operation, got %d", len(cat.Operations))
	}
	if len(cat.Operations[0].OutputSchema) == 0 {
		t.Fatal("expected OutputSchema to be preserved")
	}

	var parsed map[string]any
	if err := json.Unmarshal(cat.Operations[0].OutputSchema, &parsed); err != nil {
		t.Fatalf("unmarshal OutputSchema: %v", err)
	}
	if parsed["type"] != "object" {
		t.Fatalf("expected type=object, got %v", parsed["type"])
	}
}
