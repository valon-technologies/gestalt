package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"testing"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/catalog"
	ci "github.com/valon-technologies/gestalt/core/integration"
	coretesting "github.com/valon-technologies/gestalt/core/testing"
	"github.com/valon-technologies/gestalt/internal/invocation"
	toolshedmcp "github.com/valon-technologies/gestalt/internal/mcp"
	"github.com/valon-technologies/gestalt/internal/principal"
	"github.com/valon-technologies/gestalt/internal/testutil"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

type catalogProvider struct {
	coretesting.StubIntegration
	ops     []core.Operation
	catalog *catalog.Catalog
}

func (p *catalogProvider) ListOperations() []core.Operation { return p.ops }
func (p *catalogProvider) Catalog() *catalog.Catalog        { return p.catalog }

type flatProvider struct {
	coretesting.StubIntegration
	ops []core.Operation
}

func (p *flatProvider) ListOperations() []core.Operation { return p.ops }

func stubDatastoreWithToken() *coretesting.StubDatastore {
	return &coretesting.StubDatastore{
		TokenFn: func(_ context.Context, _, _, _ string) (*core.IntegrationToken, error) {
			return &core.IntegrationToken{AccessToken: "test-token"}, nil
		},
	}
}

func ctxWithPrincipal() context.Context {
	p := &principal.Principal{
		Identity: &core.UserIdentity{Email: "test@example.com"},
		UserID:   "u1",
		Source:   principal.SourceAPIToken,
	}
	return principal.WithPrincipal(context.Background(), p)
}

func TestNewServer_ListsToolsFromCatalogProvider(t *testing.T) {
	t.Parallel()

	cat := &catalog.Catalog{
		Name: "linear",
		Operations: []catalog.CatalogOperation{
			{
				ID:          "search_issues",
				Method:      "GET",
				Path:        "/issues",
				Title:       "Search Issues",
				Description: "Search for issues",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`),
				Annotations: catalog.OperationAnnotations{
					ReadOnlyHint: boolPtr(true),
				},
			},
			{
				ID:          "create_issue",
				Method:      "POST",
				Path:        "/issues",
				Title:       "Create Issue",
				Description: "Create a new issue",
			},
		},
	}

	prov := &catalogProvider{
		StubIntegration: coretesting.StubIntegration{N: "linear"},
		ops:             ci.OperationsList(cat),
		catalog:         cat,
	}

	providers := testutil.NewProviderRegistry(t, prov)
	ds := stubDatastoreWithToken()
	broker := invocation.NewBroker(providers, ds)

	srv := toolshedmcp.NewServer(toolshedmcp.Config{
		Invoker:   broker,
		Providers: providers,
	})

	tools := srv.ListTools()
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}

	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}
	sort.Strings(names)

	if names[0] != "linear_create_issue" || names[1] != "linear_search_issues" {
		t.Fatalf("unexpected tool names: %v", names)
	}

	searchTool := tools["linear_search_issues"]
	if searchTool.Tool.Description != "Search for issues" {
		t.Fatalf("unexpected description: %q", searchTool.Tool.Description)
	}
	if searchTool.Tool.Annotations.Title != "Search Issues" {
		t.Fatalf("unexpected title annotation: %q", searchTool.Tool.Annotations.Title)
	}
	if searchTool.Tool.Annotations.ReadOnlyHint == nil || !*searchTool.Tool.Annotations.ReadOnlyHint {
		t.Fatal("expected ReadOnlyHint to be true")
	}
}

func TestNewServer_ListsToolsFromFlatProvider(t *testing.T) {
	t.Parallel()

	prov := &flatProvider{
		StubIntegration: coretesting.StubIntegration{N: "github"},
		ops: []core.Operation{
			{Name: "list_repos", Description: "List repositories", Method: "GET", Parameters: []core.Parameter{
				{Name: "org", Type: "string", Description: "Organization name", Required: true},
			}},
			{Name: "delete_repo", Description: "Delete a repository", Method: "DELETE"},
		},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	ds := stubDatastoreWithToken()
	broker := invocation.NewBroker(providers, ds)

	srv := toolshedmcp.NewServer(toolshedmcp.Config{
		Invoker:   broker,
		Providers: providers,
	})

	tools := srv.ListTools()
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}

	listTool := tools["github_list_repos"]
	if listTool == nil {
		t.Fatal("expected github_list_repos tool")
	}
	if listTool.Tool.Annotations.ReadOnlyHint == nil || !*listTool.Tool.Annotations.ReadOnlyHint {
		t.Fatal("expected ReadOnlyHint for GET method")
	}

	deleteTool := tools["github_delete_repo"]
	if deleteTool == nil {
		t.Fatal("expected github_delete_repo tool")
	}
	if deleteTool.Tool.Annotations.DestructiveHint == nil || !*deleteTool.Tool.Annotations.DestructiveHint {
		t.Fatal("expected DestructiveHint for DELETE method")
	}
}

func TestNewServer_ToolNameConvention(t *testing.T) {
	t.Parallel()

	prov := &flatProvider{
		StubIntegration: coretesting.StubIntegration{N: "slack"},
		ops:             []core.Operation{{Name: "send_message", Method: "POST"}},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	ds := stubDatastoreWithToken()
	broker := invocation.NewBroker(providers, ds)

	srv := toolshedmcp.NewServer(toolshedmcp.Config{
		Invoker:        broker,
		Providers:      providers,
		ToolNamePrefix: "ts_",
	})

	tools := srv.ListTools()
	if tools["ts_slack_send_message"] == nil {
		names := make([]string, 0, len(tools))
		for n := range tools {
			names = append(names, n)
		}
		t.Fatalf("expected ts_slack_send_message, got %v", names)
	}
}

func TestNewServer_ToolCallRoutesThrough(t *testing.T) {
	t.Parallel()

	var invokedOp string
	var invokedParams map[string]any

	prov := &flatProvider{
		StubIntegration: coretesting.StubIntegration{
			N: "test",
			ExecuteFn: func(_ context.Context, op string, params map[string]any, _ string) (*core.OperationResult, error) {
				invokedOp = op
				invokedParams = params
				return &core.OperationResult{
					Status: http.StatusOK,
					Body:   `{"result":"ok"}`,
				}, nil
			},
		},
		ops: []core.Operation{{Name: "do_thing", Method: "POST"}},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	ds := stubDatastoreWithToken()
	broker := invocation.NewBroker(providers, ds)

	srv := toolshedmcp.NewServer(toolshedmcp.Config{
		Invoker:   broker,
		Providers: providers,
	})

	tool := srv.GetTool("test_do_thing")
	if tool == nil {
		t.Fatal("tool not found")
	}

	ctx := ctxWithPrincipal()
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "test_do_thing"
	req.Params.Arguments = map[string]any{"key": "value"}

	result, err := tool.Handler(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %v", result.Content)
	}
	if invokedOp != "do_thing" {
		t.Fatalf("expected operation do_thing, got %q", invokedOp)
	}
	if invokedParams["key"] != "value" {
		t.Fatalf("expected params to contain key=value, got %v", invokedParams)
	}
}

func TestNewServer_ToolCallUsesInjectedInvoker(t *testing.T) {
	t.Parallel()

	var called bool
	var gotProvider string
	var gotOperation string
	var gotParams map[string]any

	prov := &flatProvider{
		StubIntegration: coretesting.StubIntegration{N: "test"},
		ops:             []core.Operation{{Name: "op", Method: "GET"}},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	srv := toolshedmcp.NewServer(toolshedmcp.Config{
		Invoker: &testutil.StubInvoker{
			InvokeFn: func(_ context.Context, p *principal.Principal, providerName, operation string, params map[string]any) (*core.OperationResult, error) {
				called = true
				gotProvider = providerName
				gotOperation = operation
				gotParams = params
				if p == nil || p.UserID == "" {
					t.Fatal("expected authenticated principal")
				}
				return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
			},
		},
		Providers: providers,
	})

	tool := srv.GetTool("test_op")
	if tool == nil {
		t.Fatal("tool not found")
	}

	ctx := ctxWithPrincipal()
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "test_op"
	req.Params.Arguments = map[string]any{"foo": "bar"}

	result, err := tool.Handler(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected injected invoker to be called")
	}
	if gotProvider != "test" {
		t.Fatalf("expected provider test, got %q", gotProvider)
	}
	if gotOperation != "op" {
		t.Fatalf("expected operation op, got %q", gotOperation)
	}
	if gotParams["foo"] != "bar" {
		t.Fatalf("expected params to include foo=bar, got %v", gotParams)
	}
	if result.IsError {
		t.Fatalf("expected tool call to succeed, got error result: %v", result.Content)
	}
}

func TestNewServer_ErrorResultSetsIsError(t *testing.T) {
	t.Parallel()

	prov := &flatProvider{
		StubIntegration: coretesting.StubIntegration{
			N: "test",
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return &core.OperationResult{
					Status: http.StatusForbidden,
					Body:   "access denied",
				}, nil
			},
		},
		ops: []core.Operation{{Name: "forbidden_op", Method: "GET"}},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	ds := stubDatastoreWithToken()
	broker := invocation.NewBroker(providers, ds)

	srv := toolshedmcp.NewServer(toolshedmcp.Config{
		Invoker:   broker,
		Providers: providers,
	})

	tool := srv.GetTool("test_forbidden_op")
	ctx := ctxWithPrincipal()
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "test_forbidden_op"

	result, err := tool.Handler(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError to be true for 403 response")
	}
}

func TestNewServer_BrokerErrorReturnsToolError(t *testing.T) {
	t.Parallel()

	prov := &flatProvider{
		StubIntegration: coretesting.StubIntegration{
			N: "test",
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return nil, fmt.Errorf("connection timeout")
			},
		},
		ops: []core.Operation{{Name: "flaky_op", Method: "GET"}},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	ds := stubDatastoreWithToken()
	broker := invocation.NewBroker(providers, ds)

	srv := toolshedmcp.NewServer(toolshedmcp.Config{
		Invoker:   broker,
		Providers: providers,
	})

	tool := srv.GetTool("test_flaky_op")
	ctx := ctxWithPrincipal()
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "test_flaky_op"

	result, err := tool.Handler(ctx, req)
	if err != nil {
		t.Fatalf("unexpected protocol-level error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for broker error")
	}
}

func TestNewServer_NoPrincipalReturnsToolError(t *testing.T) {
	t.Parallel()

	prov := &flatProvider{
		StubIntegration: coretesting.StubIntegration{N: "test"},
		ops:             []core.Operation{{Name: "op", Method: "GET"}},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	ds := stubDatastoreWithToken()
	broker := invocation.NewBroker(providers, ds)

	srv := toolshedmcp.NewServer(toolshedmcp.Config{
		Invoker:   broker,
		Providers: providers,
	})

	tool := srv.GetTool("test_op")
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "test_op"

	result, err := tool.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError when no principal")
	}
}

func TestNewServer_AllowedProvidersFilter(t *testing.T) {
	t.Parallel()

	prov1 := &flatProvider{
		StubIntegration: coretesting.StubIntegration{N: "allowed"},
		ops:             []core.Operation{{Name: "op1", Method: "GET"}},
	}
	prov2 := &flatProvider{
		StubIntegration: coretesting.StubIntegration{N: "excluded"},
		ops:             []core.Operation{{Name: "op2", Method: "GET"}},
	}

	providers := testutil.NewProviderRegistry(t, prov1, prov2)
	ds := stubDatastoreWithToken()
	broker := invocation.NewBroker(providers, ds)

	srv := toolshedmcp.NewServer(toolshedmcp.Config{
		Invoker:          broker,
		Providers:        providers,
		AllowedProviders: []string{"allowed"},
	})

	tools := srv.ListTools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools["allowed_op1"] == nil {
		t.Fatal("expected allowed_op1 to be present")
	}
}

func TestNewServer_HiddenOperationsFiltered(t *testing.T) {
	t.Parallel()

	hidden := false
	cat := &catalog.Catalog{
		Name: "test",
		Operations: []catalog.CatalogOperation{
			{ID: "visible_op", Method: "GET", Path: "/v"},
			{ID: "hidden_op", Method: "GET", Path: "/h", Visible: &hidden},
		},
	}

	prov := &catalogProvider{
		StubIntegration: coretesting.StubIntegration{N: "test"},
		ops:             ci.OperationsList(cat),
		catalog:         cat,
	}

	providers := testutil.NewProviderRegistry(t, prov)
	ds := stubDatastoreWithToken()
	broker := invocation.NewBroker(providers, ds)

	srv := toolshedmcp.NewServer(toolshedmcp.Config{
		Invoker:   broker,
		Providers: providers,
	})

	tools := srv.ListTools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools["test_visible_op"] == nil {
		t.Fatal("expected test_visible_op to be present")
	}
}

type directCallerProvider struct {
	coretesting.StubIntegration
	ops    []core.Operation
	cat    *catalog.Catalog
	callFn func(ctx context.Context, name string, args map[string]any) (*mcpgo.CallToolResult, error)
}

func (p *directCallerProvider) ListOperations() []core.Operation { return p.ops }
func (p *directCallerProvider) Catalog() *catalog.Catalog        { return p.cat }

func (p *directCallerProvider) CallTool(ctx context.Context, name string, args map[string]any) (*mcpgo.CallToolResult, error) {
	if p.callFn != nil {
		return p.callFn(ctx, name, args)
	}
	return mcpgo.NewToolResultText("direct:" + name), nil
}

type stubTokenResolver struct {
	token string
	err   error
}

func (r *stubTokenResolver) ResolveToken(_ context.Context, _ *principal.Principal, _ string) (string, error) {
	return r.token, r.err
}

func TestNewServer_DirectCallerPassthrough(t *testing.T) {
	t.Parallel()

	var calledName string
	var calledArgs map[string]any

	cat := &catalog.Catalog{
		Name: "clickhouse",
		Operations: []catalog.CatalogOperation{
			{
				ID:          "run_query",
				Description: "Execute a SQL query",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"sql":{"type":"string"}}}`),
			},
		},
	}

	prov := &directCallerProvider{
		StubIntegration: coretesting.StubIntegration{N: "clickhouse"},
		ops:             []core.Operation{{Name: "run_query", Description: "Execute a SQL query"}},
		cat:             cat,
		callFn: func(_ context.Context, name string, args map[string]any) (*mcpgo.CallToolResult, error) {
			calledName = name
			calledArgs = args
			return mcpgo.NewToolResultText("query result"), nil
		},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	srv := toolshedmcp.NewServer(toolshedmcp.Config{
		Invoker:       &testutil.StubInvoker{},
		TokenResolver: &stubTokenResolver{token: "upstream-token"},
		Providers:     providers,
	})

	tool := srv.GetTool("clickhouse_run_query")
	if tool == nil {
		t.Fatal("tool not found")
	}

	ctx := ctxWithPrincipal()
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "clickhouse_run_query"
	req.Params.Arguments = map[string]any{"sql": "SELECT 1"}

	result, err := tool.Handler(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %v", result.Content)
	}
	if calledName != "run_query" {
		t.Fatalf("expected CallTool with name run_query, got %q", calledName)
	}
	if calledArgs["sql"] != "SELECT 1" {
		t.Fatalf("expected sql=SELECT 1, got %v", calledArgs)
	}

	text, ok := result.Content[0].(mcpgo.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	if text.Text != "query result" {
		t.Fatalf("expected direct passthrough result, got %q", text.Text)
	}
}

func TestNewServer_DirectCallerNoPrincipal(t *testing.T) {
	t.Parallel()

	cat := &catalog.Catalog{
		Name: "ch",
		Operations: []catalog.CatalogOperation{
			{ID: "op", Description: "op"},
		},
	}
	prov := &directCallerProvider{
		StubIntegration: coretesting.StubIntegration{N: "ch"},
		ops:             []core.Operation{{Name: "op"}},
		cat:             cat,
	}

	providers := testutil.NewProviderRegistry(t, prov)
	srv := toolshedmcp.NewServer(toolshedmcp.Config{
		Invoker:       &testutil.StubInvoker{},
		TokenResolver: &stubTokenResolver{token: "t"},
		Providers:     providers,
	})

	tool := srv.GetTool("ch_op")
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "ch_op"

	result, err := tool.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError when no principal")
	}
}

func TestNewServer_DirectCallerTokenResolveError(t *testing.T) {
	t.Parallel()

	cat := &catalog.Catalog{
		Name: "ch",
		Operations: []catalog.CatalogOperation{
			{ID: "op", Description: "op"},
		},
	}
	prov := &directCallerProvider{
		StubIntegration: coretesting.StubIntegration{N: "ch"},
		ops:             []core.Operation{{Name: "op"}},
		cat:             cat,
	}

	providers := testutil.NewProviderRegistry(t, prov)
	srv := toolshedmcp.NewServer(toolshedmcp.Config{
		Invoker:       &testutil.StubInvoker{},
		TokenResolver: &stubTokenResolver{err: fmt.Errorf("no token stored")},
		Providers:     providers,
	})

	tool := srv.GetTool("ch_op")
	ctx := ctxWithPrincipal()
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "ch_op"

	result, err := tool.Handler(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for token resolve failure")
	}
}

func boolPtr(v bool) *bool { return &v }
