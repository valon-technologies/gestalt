package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/catalog"
	ci "github.com/valon-technologies/gestalt/core/integration"
	coretesting "github.com/valon-technologies/gestalt/core/testing"
	"github.com/valon-technologies/gestalt/internal/composite"
	gestaltmcp "github.com/valon-technologies/gestalt/internal/mcp"
	"github.com/valon-technologies/gestalt/internal/principal"
	"github.com/valon-technologies/gestalt/internal/testutil"

	mcpclient "github.com/mark3labs/mcp-go/client"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

type stubDeferredUpstream struct {
	coretesting.StubIntegration
	mu          sync.Mutex
	deferred    bool
	cat         *catalog.Catalog
	postInitCat *catalog.Catalog
	callFn      func(ctx context.Context, name string, args map[string]any) (*mcpgo.CallToolResult, error)
}

func newStubDeferredUpstream(name string, postInitCat *catalog.Catalog) *stubDeferredUpstream {
	return &stubDeferredUpstream{
		StubIntegration: coretesting.StubIntegration{N: name},
		deferred:        true,
		cat:             &catalog.Catalog{Name: name},
		postInitCat:     postInitCat,
	}
}

func (u *stubDeferredUpstream) Catalog() *catalog.Catalog {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.cat
}

func (u *stubDeferredUpstream) SupportsManualAuth() bool { return true }
func (u *stubDeferredUpstream) Close() error             { return nil }

func (u *stubDeferredUpstream) CallTool(ctx context.Context, name string, args map[string]any) (*mcpgo.CallToolResult, error) {
	if u.callFn != nil {
		return u.callFn(ctx, name, args)
	}
	return mcpgo.NewToolResultText("direct:" + name), nil
}

func (u *stubDeferredUpstream) IsDeferred() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.deferred
}

func (u *stubDeferredUpstream) EnsureInitialized(_ context.Context) (bool, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if !u.deferred {
		return false, nil
	}
	u.cat = u.postInitCat
	u.deferred = false
	return true, nil
}

func (u *stubDeferredUpstream) ListOperations() []core.Operation {
	return ci.OperationsList(u.Catalog())
}

func listToolsViaClient(t *testing.T, srv *mcpserver.MCPServer) []mcpgo.Tool {
	t.Helper()

	client, err := mcpclient.NewInProcessClient(srv)
	if err != nil {
		t.Fatalf("NewInProcessClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	ctx := ctxWithPrincipal()
	if err := client.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	initReq := mcpgo.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcpgo.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcpgo.Implementation{Name: "test", Version: "0.0.1"}
	if _, err := client.Initialize(ctx, initReq); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	result, err := client.ListTools(ctx, mcpgo.ListToolsRequest{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	return result.Tools
}

type principalTokenResolver struct {
	token string
}

func (r *principalTokenResolver) ResolveToken(ctx context.Context, p *principal.Principal, _ string) (string, error) {
	if p == nil {
		return "", nil
	}
	return r.token, nil
}

func TestNewServer_DeferredProviderSkipsMCPTools(t *testing.T) {
	t.Parallel()

	mcpUp := newStubDeferredUpstream("ch", &catalog.Catalog{
		Name: "ch",
		Operations: []catalog.CatalogOperation{
			{ID: "run_query", Description: "Execute SQL", InputSchema: json.RawMessage(`{"type":"object"}`)},
		},
	})

	apiCat := &catalog.Catalog{
		Name: "ch",
		Operations: []catalog.CatalogOperation{
			{ID: "api_op", Method: "GET", Path: "/api", Description: "API op"},
		},
	}
	comp := composite.New("ch", &catalogProvider{
		StubIntegration: coretesting.StubIntegration{N: "ch"},
		ops:             ci.OperationsList(apiCat),
		catalog:         apiCat,
	}, mcpUp)

	providers := testutil.NewProviderRegistry(t, comp)
	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:       &testutil.StubInvoker{},
		TokenResolver: &principalTokenResolver{token: "t"},
		Providers:     providers,
		IncludeHTTP:   map[string]bool{"ch": true},
	})

	tools := srv.ListTools()
	if tools["ch_api_op"] == nil {
		t.Fatal("expected ch_api_op (API tool) to be present")
	}
	if tools["ch_run_query"] != nil {
		t.Fatal("expected ch_run_query (deferred MCP tool) to be absent before init")
	}
}

func TestNewServer_DeferredToolsAppearAfterListTools(t *testing.T) {
	t.Parallel()

	mcpUp := newStubDeferredUpstream("ch", &catalog.Catalog{
		Name: "ch",
		Operations: []catalog.CatalogOperation{
			{ID: "run_query", Description: "Execute SQL", InputSchema: json.RawMessage(`{"type":"object"}`)},
		},
	})

	providers := testutil.NewProviderRegistry(t, mcpUp)
	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:       &testutil.StubInvoker{},
		TokenResolver: &principalTokenResolver{token: "t"},
		Providers:     providers,
	})

	// Before authenticated ListTools: no tools
	tools := srv.ListTools()
	if len(tools) != 0 {
		t.Fatalf("expected 0 tools before authenticated ListTools, got %d", len(tools))
	}

	// ListTools via in-process client triggers the OnBeforeListTools hook
	resultTools := listToolsViaClient(t, srv)
	if len(resultTools) != 1 {
		t.Fatalf("expected 1 tool after deferred init, got %d", len(resultTools))
	}
	if resultTools[0].Name != "ch_run_query" {
		t.Fatalf("unexpected tool name: %q", resultTools[0].Name)
	}
}

func TestNewServer_DeferredCompositeToolsAppearAfterInit(t *testing.T) {
	t.Parallel()

	mcpUp := newStubDeferredUpstream("notion", &catalog.Catalog{
		Name: "notion",
		Operations: []catalog.CatalogOperation{
			{ID: "search", Description: "Search Notion", InputSchema: json.RawMessage(`{"type":"object"}`)},
		},
	})

	apiCat := &catalog.Catalog{
		Name: "notion",
		Operations: []catalog.CatalogOperation{
			{ID: "list_pages", Method: "GET", Path: "/pages", Description: "List pages"},
		},
	}
	apiProv := &catalogProvider{
		StubIntegration: coretesting.StubIntegration{N: "notion"},
		ops:             ci.OperationsList(apiCat),
		catalog:         apiCat,
	}

	comp := composite.New("notion", apiProv, mcpUp)
	providers := testutil.NewProviderRegistry(t, comp)
	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:       &testutil.StubInvoker{},
		TokenResolver: &principalTokenResolver{token: "t"},
		Providers:     providers,
		IncludeHTTP:   map[string]bool{"notion": true},
	})

	// Before: only API tools
	tools := srv.ListTools()
	if tools["notion_list_pages"] == nil {
		t.Fatal("expected notion_list_pages (API tool)")
	}
	if tools["notion_search"] != nil {
		t.Fatal("expected notion_search (deferred MCP) to be absent")
	}

	// After hook fires via authenticated ListTools: MCP tools appear too
	resultTools := listToolsViaClient(t, srv)
	toolNames := make(map[string]bool)
	for _, tool := range resultTools {
		toolNames[tool.Name] = true
	}
	if !toolNames["notion_list_pages"] {
		t.Fatal("expected notion_list_pages after init")
	}
	if !toolNames["notion_search"] {
		t.Fatal("expected notion_search after deferred init")
	}
}

func TestNewServer_DeferredTokenErrorSkipsInit(t *testing.T) {
	t.Parallel()

	mcpUp := newStubDeferredUpstream("ch", &catalog.Catalog{
		Name: "ch",
		Operations: []catalog.CatalogOperation{
			{ID: "run_query", Description: "Execute SQL"},
		},
	})

	providers := testutil.NewProviderRegistry(t, mcpUp)
	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:       &testutil.StubInvoker{},
		TokenResolver: &stubTokenResolver{err: fmt.Errorf("no token stored")},
		Providers:     providers,
	})

	resultTools := listToolsViaClient(t, srv)
	if len(resultTools) != 0 {
		t.Fatalf("expected 0 tools when token resolution fails, got %d", len(resultTools))
	}
	if !mcpUp.IsDeferred() {
		t.Fatal("expected upstream to remain deferred")
	}
}
