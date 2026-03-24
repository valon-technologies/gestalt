package mcpupstream

import (
	"context"
	"sync"
	"testing"

	"github.com/valon-technologies/gestalt/core"

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

	srv := newTestServer()
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

	return newFromClient("clickhouse", client, core.ConnectionModeUser, toolsResult.Tools)
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

	err := u.FilterOperations(map[string]string{"run_query": ""})
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

	err := u.FilterOperations(map[string]string{
		"run_query":      "Custom query description",
		"list_databases": "",
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

	err := u.FilterOperations(map[string]string{"nonexistent": ""})
	if err == nil {
		t.Fatal("expected error for unknown operation")
	}
}

func TestUpstream_FilterOperationsEmpty(t *testing.T) {
	t.Parallel()

	u := newTestUpstream(t)
	t.Cleanup(func() { _ = u.Close() })

	err := u.FilterOperations(map[string]string{})
	if err == nil {
		t.Fatal("expected error for empty allowed_operations")
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

const testUpstreamName = "test-deferred"

func newTestHTTPServerURL(t *testing.T) string {
	t.Helper()
	srv := newTestServer()
	ts := mcpserver.NewTestStreamableHTTPServer(srv, mcpserver.WithStateLess(true))
	t.Cleanup(ts.Close)
	return ts.URL + "/mcp"
}

func TestNewDeferred_EmptyCatalog(t *testing.T) {
	t.Parallel()

	u := NewDeferred(testUpstreamName, "http://localhost:9999/mcp", core.ConnectionModeUser)

	if u.client != nil {
		t.Fatal("expected nil client on deferred upstream")
	}

	ops := u.ListOperations()
	if len(ops) != 0 {
		t.Fatalf("expected 0 operations, got %d", len(ops))
	}

	cat := u.Catalog()
	if cat == nil {
		t.Fatal("expected non-nil catalog")
	}
	if len(cat.Operations) != 0 {
		t.Fatalf("expected 0 catalog operations, got %d", len(cat.Operations))
	}
}

func TestDeferred_EnsureInitialized(t *testing.T) {
	t.Parallel()

	url := newTestHTTPServerURL(t)
	u := NewDeferred(testUpstreamName, url, core.ConnectionModeUser)
	t.Cleanup(func() { _ = u.Close() })

	if !u.IsDeferred() {
		t.Fatal("expected IsDeferred true before init")
	}

	ctx := context.Background()
	if _, err := u.EnsureInitialized(ctx); err != nil {
		t.Fatalf("EnsureInitialized: %v", err)
	}

	if u.IsDeferred() {
		t.Fatal("expected IsDeferred false after init")
	}

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

	if _, err := u.EnsureInitialized(ctx); err != nil {
		t.Fatalf("second EnsureInitialized should be no-op: %v", err)
	}
}

func TestDeferred_ConcurrentInit(t *testing.T) {
	t.Parallel()

	url := newTestHTTPServerURL(t)
	u := NewDeferred(testUpstreamName, url, core.ConnectionModeUser)
	t.Cleanup(func() { _ = u.Close() })

	const goroutines = 10
	ctx := context.Background()
	errs := make(chan error, goroutines)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			_, err := u.EnsureInitialized(ctx)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("EnsureInitialized from goroutine: %v", err)
		}
	}

	if u.IsDeferred() {
		t.Fatal("expected IsDeferred false after concurrent init")
	}

	ops := u.ListOperations()
	if len(ops) != 2 {
		t.Fatalf("expected 2 operations, got %d", len(ops))
	}
}

func TestDeferred_CallToolTriggersInit(t *testing.T) {
	t.Parallel()

	url := newTestHTTPServerURL(t)
	u := NewDeferred(testUpstreamName, url, core.ConnectionModeUser)
	t.Cleanup(func() { _ = u.Close() })

	if !u.IsDeferred() {
		t.Fatal("expected IsDeferred true before CallTool")
	}

	result, err := u.CallTool(context.Background(), "run_query", map[string]any{"sql": "SELECT 42"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	if u.IsDeferred() {
		t.Fatal("expected IsDeferred false after CallTool")
	}

	text, ok := result.Content[0].(mcpgo.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	if text.Text != "result for: SELECT 42" {
		t.Fatalf("unexpected text: %q", text.Text)
	}
}

func TestDeferred_AllowedOpsApplied(t *testing.T) {
	t.Parallel()

	url := newTestHTTPServerURL(t)
	u := NewDeferred(testUpstreamName, url, core.ConnectionModeUser)
	t.Cleanup(func() { _ = u.Close() })

	u.SetAllowedOperations(map[string]string{
		"run_query": "Custom description",
	})

	ctx := context.Background()
	if _, err := u.EnsureInitialized(ctx); err != nil {
		t.Fatalf("EnsureInitialized: %v", err)
	}

	ops := u.ListOperations()
	if len(ops) != 1 {
		t.Fatalf("expected 1 operation after filtering, got %d", len(ops))
	}
	if ops[0].Name != "run_query" {
		t.Fatalf("expected run_query, got %q", ops[0].Name)
	}
	if ops[0].Description != "Custom description" {
		t.Fatalf("expected overridden description, got %q", ops[0].Description)
	}

	cat := u.Catalog()
	if len(cat.Operations) != 1 {
		t.Fatalf("expected 1 catalog operation, got %d", len(cat.Operations))
	}
	if cat.Operations[0].ID != "run_query" {
		t.Fatalf("expected run_query in catalog, got %q", cat.Operations[0].ID)
	}
}

func TestDeferred_CloseBeforeInit(t *testing.T) {
	t.Parallel()

	u := NewDeferred(testUpstreamName, "http://localhost:9999/mcp", core.ConnectionModeUser)
	if err := u.Close(); err != nil {
		t.Fatalf("Close on uninitialized deferred upstream: %v", err)
	}
}

func TestDeferred_IsDeferred(t *testing.T) {
	t.Parallel()

	url := newTestHTTPServerURL(t)
	u := NewDeferred(testUpstreamName, url, core.ConnectionModeUser)
	t.Cleanup(func() { _ = u.Close() })

	if !u.IsDeferred() {
		t.Fatal("expected IsDeferred true before init")
	}

	ctx := context.Background()
	if _, err := u.EnsureInitialized(ctx); err != nil {
		t.Fatalf("EnsureInitialized: %v", err)
	}

	if u.IsDeferred() {
		t.Fatal("expected IsDeferred false after init")
	}
}
