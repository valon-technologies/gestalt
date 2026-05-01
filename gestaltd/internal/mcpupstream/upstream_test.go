package mcpupstream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/egress"

	mcpclient "github.com/mark3labs/mcp-go/client"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

func newTestServer(opts ...mcpserver.ServerOption) *mcpserver.MCPServer {
	srv := mcpserver.NewMCPServer("test-remote", "1.0.0", opts...)

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

func newCountingHTTPTestServer(t *testing.T, listToolsCount *atomic.Int32, opts ...mcpserver.ServerOption) *httptest.Server {
	t.Helper()

	hooks := &mcpserver.Hooks{}
	hooks.AddBeforeListTools(func(context.Context, any, *mcpgo.ListToolsRequest) {
		listToolsCount.Add(1)
	})
	opts = append(opts, mcpserver.WithHooks(hooks))
	handler := mcpserver.NewStreamableHTTPServer(
		newTestServer(opts...),
		mcpserver.WithStateLess(true),
	)
	return httptest.NewServer(handler)
}

func TestUpstream_DiscoverTools(t *testing.T) {
	t.Parallel()

	u := newTestUpstream(t)
	t.Cleanup(func() { _ = u.Close() })

	cat := u.Catalog()
	if cat == nil {
		t.Fatal("expected non-nil catalog")
		return
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

	cat := u.Catalog()
	if len(cat.Operations) != 1 {
		t.Fatalf("expected 1 catalog operation, got %d", len(cat.Operations))
	}
	if cat.Operations[0].ID != "run_query" {
		t.Fatalf("expected run_query in catalog, got %q", cat.Operations[0].ID)
	}
	if cat.Operations[0].Description != "Execute a SQL query" {
		t.Fatalf("expected spec description, got %q", cat.Operations[0].Description)
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

	cat := u.Catalog()
	if len(cat.Operations) != 2 {
		t.Fatalf("expected 2 operations, got %d", len(cat.Operations))
	}

	for _, op := range cat.Operations {
		switch op.ID {
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
		"run_query": {Alias: "query", Tags: []string{"sql"}},
	})
	if err != nil {
		t.Fatalf("FilterOperations: %v", err)
	}

	cat, err := u.discover(context.Background(), "")
	if err != nil {
		t.Fatalf("discover after filter with alias: %v", err)
	}
	if len(cat.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(cat.Operations))
	}
	if cat.Operations[0].ID != "query" {
		t.Errorf("expected aliased ID %q, got %q", "query", cat.Operations[0].ID)
	}
	if got, want := cat.Operations[0].Tags, []string{"sql"}; !slices.Equal(got, want) {
		t.Errorf("expected tags %#v, got %#v", want, got)
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
	if got := u.AuthTypes(); len(got) != 0 {
		t.Fatalf("AuthTypes = %#v, want nil/empty", got)
	}
}

func TestUpstream_MetadataOverridesDecorateCatalogs(t *testing.T) {
	t.Parallel()

	ts := newAuthenticatedHTTPTestServer(t, "Bearer secret-token")
	t.Cleanup(ts.Close)

	u, err := New(
		context.Background(),
		"clickhouse",
		ts.URL,
		core.ConnectionModeUser,
		nil,
		nil,
		WithMetadataOverrides("Override", "Override description", "<svg/>"),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	staticCat := u.Catalog()
	if staticCat == nil {
		t.Fatal("expected static catalog")
		return
	}
	if staticCat.DisplayName != "Override" {
		t.Fatalf("DisplayName = %q, want %q", staticCat.DisplayName, "Override")
	}
	if staticCat.Description != "Override description" {
		t.Fatalf("Description = %q, want %q", staticCat.Description, "Override description")
	}
	if staticCat.IconSVG != "<svg/>" {
		t.Fatalf("IconSVG = %q, want %q", staticCat.IconSVG, "<svg/>")
	}

	sessionCat, err := u.CatalogForRequest(context.Background(), "secret-token")
	if err != nil {
		t.Fatalf("CatalogForRequest: %v", err)
	}
	if sessionCat.DisplayName != "Override" {
		t.Fatalf("session DisplayName = %q, want %q", sessionCat.DisplayName, "Override")
	}
	if sessionCat.Description != "Override description" {
		t.Fatalf("session Description = %q, want %q", sessionCat.Description, "Override description")
	}
	if sessionCat.IconSVG != "<svg/>" {
		t.Fatalf("session IconSVG = %q, want %q", sessionCat.IconSVG, "<svg/>")
	}
}

func TestUpstream_SetIconSVGDecoratesCatalogWithoutStaticCatalog(t *testing.T) {
	t.Parallel()

	u, err := New(context.Background(), "clickhouse", "https://example.com/mcp", core.ConnectionModeUser, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	u.SetIconSVG("<svg/>")

	cat := u.Catalog()
	if cat == nil {
		t.Fatal("expected catalog")
		return
	}
	if cat.IconSVG != "<svg/>" {
		t.Fatalf("IconSVG = %q, want %q", cat.IconSVG, "<svg/>")
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

func TestUpstream_CatalogForRequestCachesByToken(t *testing.T) {
	t.Parallel()

	var listToolsCount atomic.Int32
	ts := newCountingHTTPTestServer(t, &listToolsCount)
	t.Cleanup(ts.Close)

	u, err := New(context.Background(), "clickhouse", ts.URL, core.ConnectionModeUser, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	first, err := u.CatalogForRequest(context.Background(), "secret-token")
	if err != nil {
		t.Fatalf("first CatalogForRequest: %v", err)
	}
	first.Operations[0].ID = "mutated-by-caller"

	second, err := u.CatalogForRequest(context.Background(), "secret-token")
	if err != nil {
		t.Fatalf("second CatalogForRequest: %v", err)
	}
	if got := listToolsCount.Load(); got != 1 {
		t.Fatalf("ListTools count = %d, want 1", got)
	}
	if second.Operations[0].ID == "mutated-by-caller" {
		t.Fatalf("cached catalog was mutated by caller: %#v", second.Operations)
	}
}

func TestUpstream_CatalogForRequestCacheExpires(t *testing.T) {
	t.Parallel()

	var listToolsCount atomic.Int32
	ts := newCountingHTTPTestServer(t, &listToolsCount)
	t.Cleanup(ts.Close)

	u, err := New(context.Background(), "clickhouse", ts.URL, core.ConnectionModeUser, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	u.catalogCacheTTL = 10 * time.Millisecond

	if _, err := u.CatalogForRequest(context.Background(), "secret-token"); err != nil {
		t.Fatalf("first CatalogForRequest: %v", err)
	}
	time.Sleep(25 * time.Millisecond)
	if _, err := u.CatalogForRequest(context.Background(), "secret-token"); err != nil {
		t.Fatalf("second CatalogForRequest: %v", err)
	}
	if got := listToolsCount.Load(); got != 2 {
		t.Fatalf("ListTools count = %d, want 2 after expiry", got)
	}
}

func TestUpstream_CatalogForRequestCacheSeparatesTokens(t *testing.T) {
	t.Parallel()

	var listToolsCount atomic.Int32
	ts := newCountingHTTPTestServer(t, &listToolsCount)
	t.Cleanup(ts.Close)

	u, err := New(context.Background(), "clickhouse", ts.URL, core.ConnectionModeUser, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for _, token := range []string{"token-a", "token-a", "token-b", "token-a"} {
		if _, err := u.CatalogForRequest(context.Background(), token); err != nil {
			t.Fatalf("CatalogForRequest(%q): %v", token, err)
		}
	}
	if got := listToolsCount.Load(); got != 2 {
		t.Fatalf("ListTools count = %d, want 2 for two distinct tokens", got)
	}
}

func TestUpstream_CatalogForRequestSingleflightsConcurrentDiscovery(t *testing.T) {
	t.Parallel()

	var listToolsCount atomic.Int32
	firstListStarted := make(chan struct{})
	releaseFirstList := make(chan struct{})
	hooks := &mcpserver.Hooks{}
	hooks.AddBeforeListTools(func(context.Context, any, *mcpgo.ListToolsRequest) {
		if listToolsCount.Add(1) == 1 {
			close(firstListStarted)
			<-releaseFirstList
		}
	})
	handler := mcpserver.NewStreamableHTTPServer(
		newTestServer(mcpserver.WithHooks(hooks)),
		mcpserver.WithStateLess(true),
	)
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	u, err := New(context.Background(), "clickhouse", ts.URL, core.ConnectionModeUser, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := u.CatalogForRequest(context.Background(), "secret-token")
		errs <- err
	}()

	<-firstListStarted
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := u.CatalogForRequest(context.Background(), "secret-token")
		errs <- err
	}()
	time.Sleep(25 * time.Millisecond)
	close(releaseFirstList)
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("CatalogForRequest: %v", err)
		}
	}
	if got := listToolsCount.Load(); got != 1 {
		t.Fatalf("ListTools count = %d, want 1 for concurrent same-token discovery", got)
	}
}

func TestUpstream_CatalogForRequestSingleflightIgnoresLeaderCancellation(t *testing.T) {
	t.Parallel()

	var listToolsCount atomic.Int32
	firstListStarted := make(chan struct{})
	releaseFirstList := make(chan struct{})
	hooks := &mcpserver.Hooks{}
	hooks.AddBeforeListTools(func(context.Context, any, *mcpgo.ListToolsRequest) {
		if listToolsCount.Add(1) == 1 {
			close(firstListStarted)
			<-releaseFirstList
		}
	})
	handler := mcpserver.NewStreamableHTTPServer(
		newTestServer(mcpserver.WithHooks(hooks)),
		mcpserver.WithStateLess(true),
	)
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	u, err := New(context.Background(), "clickhouse", ts.URL, core.ConnectionModeUser, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	leaderErr := make(chan error, 1)
	go func() {
		_, err := u.CatalogForRequest(leaderCtx, "secret-token")
		leaderErr <- err
	}()

	<-firstListStarted
	cancelLeader()
	if err := <-leaderErr; !errors.Is(err, context.Canceled) {
		t.Fatalf("leader error = %v, want context.Canceled", err)
	}

	waiterErr := make(chan error, 1)
	go func() {
		_, err := u.CatalogForRequest(context.Background(), "secret-token")
		waiterErr <- err
	}()
	time.Sleep(25 * time.Millisecond)
	close(releaseFirstList)
	if err := <-waiterErr; err != nil {
		t.Fatalf("waiter CatalogForRequest: %v", err)
	}
	if got := listToolsCount.Load(); got != 1 {
		t.Fatalf("ListTools count = %d, want 1 despite leader cancellation", got)
	}
}

func TestUpstream_FilterOperationsClearsDynamicCatalogCache(t *testing.T) {
	t.Parallel()

	var listToolsCount atomic.Int32
	ts := newCountingHTTPTestServer(t, &listToolsCount)
	t.Cleanup(ts.Close)

	u, err := New(context.Background(), "clickhouse", ts.URL, core.ConnectionModeUser, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	full, err := u.CatalogForRequest(context.Background(), "secret-token")
	if err != nil {
		t.Fatalf("initial CatalogForRequest: %v", err)
	}
	if len(full.Operations) != 2 {
		t.Fatalf("initial operations len = %d, want 2", len(full.Operations))
	}

	if err := u.FilterOperations(map[string]*config.OperationOverride{
		"run_query": {Alias: "query"},
	}); err != nil {
		t.Fatalf("FilterOperations: %v", err)
	}

	filtered, err := u.CatalogForRequest(context.Background(), "secret-token")
	if err != nil {
		t.Fatalf("filtered CatalogForRequest: %v", err)
	}
	if got := listToolsCount.Load(); got != 2 {
		t.Fatalf("ListTools count = %d, want 2 after filter invalidation", got)
	}
	if len(filtered.Operations) != 1 || filtered.Operations[0].ID != "query" {
		t.Fatalf("filtered operations = %#v, want only aliased query", filtered.Operations)
	}
}

func TestUpstream_FilterOperationsConcurrentWithCatalogAndCallTool(t *testing.T) {
	t.Parallel()

	var listToolsCount atomic.Int32
	ts := newCountingHTTPTestServer(t, &listToolsCount)
	t.Cleanup(ts.Close)

	u, err := New(context.Background(), "clickhouse", ts.URL, core.ConnectionModeUser, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 90)
	for i := 0; i < 30; i++ {
		wg.Add(3)
		go func(i int) {
			defer wg.Done()
			allowed := map[string]*config.OperationOverride{"run_query": nil}
			if i%2 == 0 {
				allowed = map[string]*config.OperationOverride{"list_databases": nil}
			}
			if err := u.FilterOperations(allowed); err != nil {
				errs <- err
			}
		}(i)
		go func() {
			defer wg.Done()
			if _, err := u.CatalogForRequest(context.Background(), "secret-token"); err != nil {
				errs <- err
			}
		}()
		go func() {
			defer wg.Done()
			if _, err := u.CallTool(context.Background(), "run_query", map[string]any{"sql": "SELECT 1"}); err != nil && !strings.Contains(err.Error(), "not allowed") {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent operation failed: %v", err)
	}
}

func TestUpstream_CallToolRemainsLiveAfterCatalogCache(t *testing.T) {
	t.Parallel()

	var (
		listToolsCount atomic.Int32
		callToolCount  atomic.Int32
	)
	hooks := &mcpserver.Hooks{}
	hooks.AddBeforeListTools(func(context.Context, any, *mcpgo.ListToolsRequest) {
		listToolsCount.Add(1)
	})
	srv := mcpserver.NewMCPServer("live-tool-test", "1.0.0", mcpserver.WithHooks(hooks))
	srv.AddTool(
		mcpgo.NewTool("run_query", mcpgo.WithDescription("Execute a SQL query")),
		func(context.Context, mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			return mcpgo.NewToolResultText(fmt.Sprintf("call-%d", callToolCount.Add(1))), nil
		},
	)
	handler := mcpserver.NewStreamableHTTPServer(srv, mcpserver.WithStateLess(true))
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	u, err := New(context.Background(), "clickhouse", ts.URL, core.ConnectionModeUser, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := u.CatalogForRequest(context.Background(), "secret-token"); err != nil {
		t.Fatalf("first CatalogForRequest: %v", err)
	}
	if _, err := u.CatalogForRequest(context.Background(), "secret-token"); err != nil {
		t.Fatalf("second CatalogForRequest: %v", err)
	}
	if got := listToolsCount.Load(); got != 1 {
		t.Fatalf("ListTools count = %d, want cached catalog discovery", got)
	}

	for i, want := range []string{"call-1", "call-2"} {
		result, err := u.CallTool(context.Background(), "run_query", nil)
		if err != nil {
			t.Fatalf("CallTool #%d: %v", i+1, err)
		}
		if result.IsError {
			t.Fatalf("CallTool #%d returned tool error: %v", i+1, result.Content)
		}
		text, ok := result.Content[0].(mcpgo.TextContent)
		if !ok {
			t.Fatalf("CallTool #%d content = %T, want TextContent", i+1, result.Content[0])
		}
		if text.Text != want {
			t.Fatalf("CallTool #%d text = %q, want %q", i+1, text.Text, want)
		}
	}
	if got := callToolCount.Load(); got != 2 {
		t.Fatalf("CallTool count = %d, want 2 live calls", got)
	}
}

func TestUpstream_StaticHeadersReachUpstream(t *testing.T) {
	t.Parallel()

	const headerName = "X-Static-Version"
	const headerValue = "2026-02-09"

	ts := newHeaderAuthenticatedHTTPTestServer(t, map[string]string{
		headerName: headerValue,
	})
	t.Cleanup(ts.Close)

	u, err := New(context.Background(), "clickhouse", ts.URL, core.ConnectionModeNone, map[string]string{
		headerName: headerValue,
	}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		t.Fatalf("http.DefaultTransport = %T, want *http.Transport", http.DefaultTransport)
	}

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
				defaultTransport.CloseIdleConnections()
			}
		}
	}()
	t.Cleanup(func() {
		close(stop)
		<-done
	})

	for i := range 5 {
		cat, err := u.CatalogForRequest(context.Background(), "")
		if err != nil {
			t.Fatalf("CatalogForRequest #%d: %v", i+1, err)
		}
		if len(cat.Operations) != 2 {
			t.Fatalf("expected 2 operations, got %d", len(cat.Operations))
		}

		result, err := u.CallTool(context.Background(), "run_query", map[string]any{"sql": "SELECT 1"})
		if err != nil {
			t.Fatalf("CallTool #%d: %v", i+1, err)
		}
		if result.IsError {
			t.Fatalf("unexpected tool error on iteration %d: %v", i+1, result.Content)
		}
	}
}

func TestUpstream_RequestTokenOverridesStaticAuthorizationHeader(t *testing.T) {
	t.Parallel()

	const (
		headerName  = "X-Static-Version"
		headerValue = "2026-02-09"
	)

	ts := newHeaderAuthenticatedHTTPTestServer(t, map[string]string{
		"Authorization": "Bearer secret-token",
		headerName:      headerValue,
	})
	t.Cleanup(ts.Close)

	u, err := New(context.Background(), "clickhouse", ts.URL, core.ConnectionModeUser, map[string]string{
		"Authorization": "Bearer wrong-token",
		headerName:      headerValue,
	}, nil)
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

func TestUpstream_EgressCheckBlocksDeniedHost(t *testing.T) {
	t.Parallel()

	ts := newAuthenticatedHTTPTestServer(t, "Bearer tok")
	t.Cleanup(ts.Close)

	u, err := New(context.Background(), "clickhouse", ts.URL, core.ConnectionModeUser, nil,
		func(_ string) error {
			return fmt.Errorf("%w: denied", egress.ErrEgressDenied)
		},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = u.CatalogForRequest(context.Background(), "tok")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, egress.ErrEgressDenied) {
		t.Fatalf("expected ErrEgressDenied, got %v", err)
	}
}

func TestUpstream_EgressCheckAllowsPermittedHost(t *testing.T) {
	t.Parallel()

	var checkedHost string
	ts := newAuthenticatedHTTPTestServer(t, "Bearer secret-token")
	t.Cleanup(ts.Close)

	u, err := New(context.Background(), "clickhouse", ts.URL, core.ConnectionModeUser, nil,
		func(host string) error {
			checkedHost = host
			return nil
		},
	)
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
	if checkedHost == "" {
		t.Fatal("egress check was not called")
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
		return
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
