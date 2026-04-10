package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coreintegration "github.com/valon-technologies/gestalt/server/core/integration"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/composite"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	gestaltmcp "github.com/valon-technologies/gestalt/server/internal/mcp"
	"github.com/valon-technologies/gestalt/server/internal/testutil"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

type stubMCPUpstream struct {
	cat    *catalog.Catalog
	callFn func(ctx context.Context, name string, args map[string]any) (*mcpgo.CallToolResult, error)
}

func (u *stubMCPUpstream) Name() string {
	if u.cat != nil {
		return u.cat.Name
	}
	return "stub-mcp"
}
func (u *stubMCPUpstream) DisplayName() string { return u.Name() }
func (u *stubMCPUpstream) Description() string { return "" }
func (u *stubMCPUpstream) ConnectionMode() core.ConnectionMode {
	return core.ConnectionModeNone
}
func (u *stubMCPUpstream) Catalog() *catalog.Catalog { return u.cat }
func (u *stubMCPUpstream) CatalogForRequest(_ context.Context, _ string) (*catalog.Catalog, error) {
	return u.cat, nil
}
func (u *stubMCPUpstream) SupportsManualAuth() bool { return true }
func (u *stubMCPUpstream) Close() error             { return nil }
func (u *stubMCPUpstream) Execute(context.Context, string, map[string]any, string) (*core.OperationResult, error) {
	return nil, core.ErrMCPOnly
}

func (u *stubMCPUpstream) CallTool(ctx context.Context, name string, args map[string]any) (*mcpgo.CallToolResult, error) {
	if u.callFn != nil {
		return u.callFn(ctx, name, args)
	}
	return mcpgo.NewToolResultText("direct:" + name), nil
}

func TestComposite_MCPPassthroughRouting(t *testing.T) {
	t.Parallel()

	apiCat := &catalog.Catalog{
		Name: "notion",
		Operations: []catalog.CatalogOperation{
			{ID: "list_pages", Method: http.MethodGet, Path: "/pages", Description: "List pages"},
		},
	}
	apiProv := &catalogProvider{
		StubIntegration: coretesting.StubIntegration{N: "notion"},
		ops:             coreintegration.OperationsList(apiCat),
		catalog:         apiCat,
	}

	mcpUp := &stubMCPUpstream{
		cat: &catalog.Catalog{
			Name: "notion",
			Operations: []catalog.CatalogOperation{
				{ID: "search", Description: "Search Notion", InputSchema: json.RawMessage(`{"type":"object"}`)},
			},
		},
	}

	comp := composite.New("notion", apiProv, mcpUp)
	providers := testutil.NewProviderRegistry(t, comp)
	invoker := &testutil.StubInvoker{
		Result: &core.OperationResult{Status: http.StatusOK, Body: `{"from":"invoker"}`},
	}
	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:   invoker,
		Providers: providers,
	})

	tools := srv.ListTools()
	if tools["notion_search"] == nil {
		t.Fatal("expected notion_search tool from MCP upstream")
	}
	if tools["notion_list_pages"] == nil {
		t.Fatal("expected notion_list_pages tool from API upstream")
	}

	tool := srv.GetTool("notion_search")
	ctx := ctxWithPrincipal()
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "notion_search"
	req.Params.Arguments = map[string]any{"query": "hello"}
	result, err := tool.Handler(ctx, req)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %v", result.Content)
	}
	if !invoker.Invoked {
		t.Fatal("expected invoker to be called for MCP passthrough tool")
	}
	if invoker.Provider != "notion" {
		t.Fatalf("provider = %q, want %q", invoker.Provider, "notion")
	}
	if invoker.Operation != "search" {
		t.Fatalf("operation = %q, want %q", invoker.Operation, "search")
	}
}

func TestComposite_MCPFromAPIExposesBothToolSets(t *testing.T) {
	t.Parallel()

	var directCalled string
	var invokerCalled string

	apiCat := &catalog.Catalog{
		Name: "notion",
		Operations: []catalog.CatalogOperation{
			{ID: "list_pages", Method: http.MethodGet, Path: "/pages", Description: "List pages"},
		},
	}
	apiProv := &catalogProvider{
		StubIntegration: coretesting.StubIntegration{
			N: "notion",
			ExecuteFn: func(_ context.Context, op string, _ map[string]any, _ string) (*core.OperationResult, error) {
				invokerCalled = op
				return &core.OperationResult{Status: http.StatusOK, Body: `{"from":"api"}`}, nil
			},
		},
		ops:     coreintegration.OperationsList(apiCat),
		catalog: apiCat,
	}

	mcpUp := &stubMCPUpstream{
		cat: &catalog.Catalog{
			Name: "notion",
			Operations: []catalog.CatalogOperation{
				{ID: "search", Description: "Search Notion", InputSchema: json.RawMessage(`{"type":"object"}`)},
			},
		},
		callFn: func(_ context.Context, name string, _ map[string]any) (*mcpgo.CallToolResult, error) {
			directCalled = name
			return mcpgo.NewToolResultText("from-mcp"), nil
		},
	}

	comp := composite.New("notion", apiProv, mcpUp)
	providers := testutil.NewProviderRegistry(t, comp)
	ds := stubServicesWithToken(t)
	broker := invocation.NewBroker(providers, ds.Users, ds.Tokens)
	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:       broker,
		TokenResolver: &stubTokenResolver{token: "t"},
		Providers:     providers,
	})

	tools := srv.ListTools()
	if tools["notion_search"] == nil {
		t.Fatal("expected notion_search from MCP upstream")
	}
	if tools["notion_list_pages"] == nil {
		t.Fatal("expected notion_list_pages from API (mcpFromAPI=true)")
	}

	ctx := ctxWithPrincipal()

	mcpTool := srv.GetTool("notion_search")
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "notion_search"
	if _, err := mcpTool.Handler(ctx, req); err != nil {
		t.Fatal(err)
	}
	if directCalled != "search" {
		t.Fatalf("expected direct call to 'search', got %q", directCalled)
	}

	apiTool := srv.GetTool("notion_list_pages")
	req2 := mcpgo.CallToolRequest{}
	req2.Params.Name = "notion_list_pages"
	result, err := apiTool.Handler(ctx, req2)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Content)
	}
	if invokerCalled != "list_pages" {
		t.Fatalf("expected invoker to call 'list_pages', got %q", invokerCalled)
	}
}

func TestComposite_ExecuteDelegatesToAPI(t *testing.T) {
	t.Parallel()

	apiCat := &catalog.Catalog{
		Name: "notion",
		Operations: []catalog.CatalogOperation{
			{ID: "get_page", Method: http.MethodGet, Path: "/pages/{id}", Description: "Get page"},
		},
	}
	var executedOp string
	apiProv := &catalogProvider{
		StubIntegration: coretesting.StubIntegration{
			N: "notion",
			ExecuteFn: func(_ context.Context, op string, _ map[string]any, _ string) (*core.OperationResult, error) {
				executedOp = op
				return &core.OperationResult{Status: http.StatusOK, Body: `{"id":"page1"}`}, nil
			},
		},
		ops:     coreintegration.OperationsList(apiCat),
		catalog: apiCat,
	}

	mcpUp := &stubMCPUpstream{
		cat: &catalog.Catalog{
			Name:       "notion",
			Operations: []catalog.CatalogOperation{{ID: "search", Description: "Search"}},
		},
	}

	comp := composite.New("notion", apiProv, mcpUp)

	result, err := comp.Execute(context.Background(), "get_page", map[string]any{"id": "page1"}, "token")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("status=%d", result.Status)
	}
	if executedOp != "get_page" {
		t.Fatalf("expected execute on 'get_page', got %q", executedOp)
	}
}

func TestComposite_IncludeRESTFalseExcludesAPITools(t *testing.T) {
	t.Parallel()

	apiCat := &catalog.Catalog{
		Name: "alpha",
		Operations: []catalog.CatalogOperation{
			{ID: "list_items", Method: http.MethodGet, Path: "/items", Description: "List items"},
		},
	}
	apiProv := &catalogProvider{
		StubIntegration: coretesting.StubIntegration{N: "alpha"},
		ops:             coreintegration.OperationsList(apiCat),
		catalog:         apiCat,
	}

	mcpUp := &stubMCPUpstream{
		cat: &catalog.Catalog{
			Name: "alpha",
			Operations: []catalog.CatalogOperation{
				{ID: "search", Description: "Search items", InputSchema: json.RawMessage(`{"type":"object"}`)},
			},
		},
	}

	comp := composite.New("alpha", apiProv, mcpUp)
	providers := testutil.NewProviderRegistry(t, comp)
	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:       &testutil.StubInvoker{},
		TokenResolver: &stubTokenResolver{token: "t"},
		Providers:     providers,
		IncludeREST:   map[string]bool{"alpha": false},
	})

	tools := srv.ListTools()
	if tools["alpha_search"] == nil {
		t.Fatal("expected alpha_search from MCP upstream")
	}
	if tools["alpha_list_items"] != nil {
		t.Fatal("expected alpha_list_items to be excluded when IncludeREST=false")
	}
	if len(tools) != 1 {
		names := make([]string, 0, len(tools))
		for n := range tools {
			names = append(names, n)
		}
		t.Fatalf("expected 1 tool, got %d: %v", len(tools), names)
	}
}
