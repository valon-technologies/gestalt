package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coreintegration "github.com/valon-technologies/gestalt/server/core/integration"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/composite"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/egress"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	gestaltmcp "github.com/valon-technologies/gestalt/server/internal/mcp"
	"github.com/valon-technologies/gestalt/server/internal/mcpupstream"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/testutil"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

const (
	testAPIConnectionName   = "api"
	testPluginAccessToken   = "plugin-token"
	testNamedAPIAccessToken = "api-token"
)

type catalogProvider struct {
	coretesting.StubIntegration
	ops     []core.Operation
	catalog *catalog.Catalog
}

func (p *catalogProvider) Catalog() *catalog.Catalog { return p.catalog }

type flatProvider struct {
	coretesting.StubIntegration
	ops            []core.Operation
	catalog        *catalog.Catalog
	disableCatalog bool
}

func (p *flatProvider) Catalog() *catalog.Catalog {
	if p.disableCatalog {
		return nil
	}
	if p.catalog != nil {
		return p.catalog
	}
	return testCatalogFromOperations(p.N, p.ops)
}

func testCatalogFromOperations(name string, ops []core.Operation) *catalog.Catalog {
	cat := &catalog.Catalog{
		Name:       name,
		Operations: make([]catalog.CatalogOperation, 0, len(ops)),
	}
	for _, op := range ops {
		params := make([]catalog.CatalogParameter, 0, len(op.Parameters))
		for _, param := range op.Parameters {
			params = append(params, catalog.CatalogParameter{
				Name:        param.Name,
				Type:        param.Type,
				Description: param.Description,
				Required:    param.Required,
				Default:     param.Default,
			})
		}
		cat.Operations = append(cat.Operations, catalog.CatalogOperation{
			ID:          op.Name,
			Method:      op.Method,
			Path:        "/" + op.Name,
			Title:       op.Name,
			Description: op.Description,
			Parameters:  params,
			Transport:   catalog.TransportREST,
		})
	}
	coreintegration.CompileSchemas(cat)
	return cat
}

func newCatalogBackedProvider(stub coretesting.StubIntegration, ops []core.Operation) *catalogProvider {
	return &catalogProvider{
		StubIntegration: stub,
		catalog:         testCatalogFromOperations(stub.N, ops),
	}
}

func stubDatastoreWithToken() *coretesting.StubDatastore {
	return &coretesting.StubDatastore{
		TokenFn: func(_ context.Context, _, _, _, _ string) (*core.IntegrationToken, error) {
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

func ctxWithIdentityPrincipal(email, userID string) context.Context {
	p := &principal.Principal{
		Identity: &core.UserIdentity{Email: email},
		UserID:   userID,
		Source:   principal.SourceAPIToken,
	}
	return principal.WithPrincipal(context.Background(), p)
}

type testSessionWithTools struct {
	id            string
	initialized   bool
	notifications chan mcpgo.JSONRPCNotification
	tools         map[string]mcpserver.ServerTool
}

func newTestSessionWithTools() *testSessionWithTools {
	return &testSessionWithTools{
		id:            "session-1",
		initialized:   true,
		notifications: make(chan mcpgo.JSONRPCNotification, 1),
	}
}

func (s *testSessionWithTools) Initialize() { s.initialized = true }
func (s *testSessionWithTools) Initialized() bool {
	return s.initialized
}
func (s *testSessionWithTools) NotificationChannel() chan<- mcpgo.JSONRPCNotification {
	return s.notifications
}
func (s *testSessionWithTools) SessionID() string { return s.id }
func (s *testSessionWithTools) GetSessionTools() map[string]mcpserver.ServerTool {
	if s.tools == nil {
		return nil
	}
	out := make(map[string]mcpserver.ServerTool, len(s.tools))
	for name, tool := range s.tools {
		out[name] = tool
	}
	return out
}
func (s *testSessionWithTools) SetSessionTools(tools map[string]mcpserver.ServerTool) {
	s.tools = make(map[string]mcpserver.ServerTool, len(tools))
	for name, tool := range tools {
		s.tools[name] = tool
	}
}

func initializeSession(t *testing.T, srv *mcpserver.MCPServer, ctx context.Context, session *testSessionWithTools) {
	t.Helper()

	resp := srv.HandleMessage(srv.WithContext(ctx, session), []byte(`{
		"jsonrpc":"2.0",
		"id":1,
		"method":"initialize",
		"params":{
			"protocolVersion":"2025-03-26",
			"capabilities":{},
			"clientInfo":{"name":"test","version":"1.0"}
		}
	}`))
	if _, ok := resp.(mcpgo.JSONRPCResponse); !ok {
		t.Fatalf("expected initialize response, got %T", resp)
	}
}

func listToolsForSession(t *testing.T, srv *mcpserver.MCPServer, ctx context.Context, session *testSessionWithTools) mcpgo.ListToolsResult {
	t.Helper()
	initializeSession(t, srv, ctx, session)

	resp := srv.HandleMessage(srv.WithContext(ctx, session), []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	rpcResp, ok := resp.(mcpgo.JSONRPCResponse)
	if !ok {
		if rpcErr, ok := resp.(mcpgo.JSONRPCError); ok {
			t.Fatalf("expected JSONRPCResponse, got JSONRPCError: %+v", rpcErr.Error)
		}
		t.Fatalf("expected JSONRPCResponse, got %T", resp)
	}
	result, ok := rpcResp.Result.(mcpgo.ListToolsResult)
	if !ok {
		t.Fatalf("expected ListToolsResult, got %T", rpcResp.Result)
	}
	return result
}

func callToolForSession(t *testing.T, srv *mcpserver.MCPServer, ctx context.Context, session *testSessionWithTools, name string, args map[string]any) mcpgo.CallToolResult {
	t.Helper()
	initializeSession(t, srv, ctx, session)

	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      name,
			"arguments": args,
		},
	}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	resp := srv.HandleMessage(srv.WithContext(ctx, session), raw)
	rpcResp, ok := resp.(mcpgo.JSONRPCResponse)
	if !ok {
		if rpcErr, ok := resp.(mcpgo.JSONRPCError); ok {
			t.Fatalf("expected JSONRPCResponse, got JSONRPCError: %+v", rpcErr.Error)
		}
		t.Fatalf("expected JSONRPCResponse, got %T", resp)
	}
	if result, ok := rpcResp.Result.(mcpgo.CallToolResult); ok {
		return result
	}
	if result, ok := rpcResp.Result.(*mcpgo.CallToolResult); ok {
		return *result
	}
	t.Fatalf("expected CallToolResult, got %T", rpcResp.Result)
	return mcpgo.CallToolResult{}
}

func TestNewServer_ListsToolsFromCatalogProvider(t *testing.T) {
	t.Parallel()

	cat := &catalog.Catalog{
		Name: "linear",
		Operations: []catalog.CatalogOperation{
			{
				ID:          "search_issues",
				Method:      http.MethodGet,
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
				Method:      http.MethodPost,
				Path:        "/issues",
				Title:       "Create Issue",
				Description: "Create a new issue",
			},
		},
	}

	prov := &catalogProvider{
		StubIntegration: coretesting.StubIntegration{N: "linear"},
		ops:             coreintegration.OperationsList(cat),
		catalog:         cat,
	}

	providers := testutil.NewProviderRegistry(t, prov)
	ds := stubDatastoreWithToken()
	broker := invocation.NewBroker(providers, ds)

	srv := gestaltmcp.NewServer(gestaltmcp.Config{
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

func TestNewServer_SkipsFlatOnlyProvider(t *testing.T) {
	t.Parallel()

	prov := &flatProvider{
		StubIntegration: coretesting.StubIntegration{N: "github"},
		disableCatalog:  true,
		ops: []core.Operation{
			{Name: "list_repos", Description: "List repositories", Method: http.MethodGet, Parameters: []core.Parameter{
				{Name: "org", Type: "string", Description: "Organization name", Required: true},
			}},
			{Name: "delete_repo", Description: "Delete a repository", Method: http.MethodDelete},
		},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	ds := stubDatastoreWithToken()
	broker := invocation.NewBroker(providers, ds)

	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:   broker,
		Providers: providers,
	})

	tools := srv.ListTools()
	if len(tools) != 0 {
		t.Fatalf("expected 0 tools for flat-only provider, got %d", len(tools))
	}
}

func TestNewServer_ToolNameConvention(t *testing.T) {
	t.Parallel()

	prov := newCatalogBackedProvider(
		coretesting.StubIntegration{N: "slack"},
		[]core.Operation{{Name: "send_message", Method: http.MethodPost}},
	)

	providers := testutil.NewProviderRegistry(t, prov)
	ds := stubDatastoreWithToken()
	broker := invocation.NewBroker(providers, ds)

	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:      broker,
		Providers:    providers,
		ToolPrefixes: map[string]string{"slack": "ts_"},
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

	prov := newCatalogBackedProvider(
		coretesting.StubIntegration{
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
		[]core.Operation{{Name: "do_thing", Method: http.MethodPost}},
	)

	providers := testutil.NewProviderRegistry(t, prov)
	ds := stubDatastoreWithToken()
	broker := invocation.NewBroker(providers, ds)

	srv := gestaltmcp.NewServer(gestaltmcp.Config{
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

	prov := newCatalogBackedProvider(
		coretesting.StubIntegration{N: "test"},
		[]core.Operation{{Name: "op", Method: http.MethodGet}},
	)

	providers := testutil.NewProviderRegistry(t, prov)
	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker: &testutil.StubInvoker{
			InvokeFn: func(_ context.Context, p *principal.Principal, providerName, _, operation string, params map[string]any) (*core.OperationResult, error) {
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

	prov := newCatalogBackedProvider(
		coretesting.StubIntegration{
			N: "test",
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return &core.OperationResult{
					Status: http.StatusForbidden,
					Body:   "access denied",
				}, nil
			},
		},
		[]core.Operation{{Name: "forbidden_op", Method: http.MethodGet}},
	)

	providers := testutil.NewProviderRegistry(t, prov)
	ds := stubDatastoreWithToken()
	broker := invocation.NewBroker(providers, ds)

	srv := gestaltmcp.NewServer(gestaltmcp.Config{
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

	prov := newCatalogBackedProvider(
		coretesting.StubIntegration{
			N: "test",
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return nil, fmt.Errorf("connection timeout")
			},
		},
		[]core.Operation{{Name: "flaky_op", Method: http.MethodGet}},
	)

	providers := testutil.NewProviderRegistry(t, prov)
	ds := stubDatastoreWithToken()
	broker := invocation.NewBroker(providers, ds)

	srv := gestaltmcp.NewServer(gestaltmcp.Config{
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

	prov := newCatalogBackedProvider(
		coretesting.StubIntegration{N: "test"},
		[]core.Operation{{Name: "op", Method: http.MethodGet}},
	)

	providers := testutil.NewProviderRegistry(t, prov)
	ds := stubDatastoreWithToken()
	broker := invocation.NewBroker(providers, ds)

	srv := gestaltmcp.NewServer(gestaltmcp.Config{
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

	prov1 := newCatalogBackedProvider(
		coretesting.StubIntegration{N: "allowed"},
		[]core.Operation{{Name: "op1", Method: http.MethodGet}},
	)
	prov2 := newCatalogBackedProvider(
		coretesting.StubIntegration{N: "excluded"},
		[]core.Operation{{Name: "op2", Method: http.MethodGet}},
	)

	providers := testutil.NewProviderRegistry(t, prov1, prov2)
	ds := stubDatastoreWithToken()
	broker := invocation.NewBroker(providers, ds)

	srv := gestaltmcp.NewServer(gestaltmcp.Config{
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
			{ID: "visible_op", Method: http.MethodGet, Path: "/v"},
			{ID: "hidden_op", Method: http.MethodGet, Path: "/h", Visible: &hidden},
		},
	}

	prov := &catalogProvider{
		StubIntegration: coretesting.StubIntegration{N: "test"},
		ops:             coreintegration.OperationsList(cat),
		catalog:         cat,
	}

	providers := testutil.NewProviderRegistry(t, prov)
	ds := stubDatastoreWithToken()
	broker := invocation.NewBroker(providers, ds)

	srv := gestaltmcp.NewServer(gestaltmcp.Config{
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
	ops              []core.Operation
	cat              *catalog.Catalog
	sessionCatalogFn func(ctx context.Context, token string) (*catalog.Catalog, error)
	callFn           func(ctx context.Context, name string, args map[string]any) (*mcpgo.CallToolResult, error)
}

func (p *directCallerProvider) Catalog() *catalog.Catalog { return p.cat }
func (p *directCallerProvider) CatalogForRequest(ctx context.Context, token string) (*catalog.Catalog, error) {
	if p.sessionCatalogFn != nil {
		return p.sessionCatalogFn(ctx, token)
	}
	return p.cat, nil
}

func (p *directCallerProvider) CallTool(ctx context.Context, name string, args map[string]any) (*mcpgo.CallToolResult, error) {
	if p.callFn != nil {
		return p.callFn(ctx, name, args)
	}
	return mcpgo.NewToolResultText("direct:" + name), nil
}

type stubTokenResolver struct {
	token     string
	err       error
	resolveFn func(context.Context, *principal.Principal, string, string, string) (string, error)
}

func (r *stubTokenResolver) ResolveToken(ctx context.Context, p *principal.Principal, providerName, connection, instance string) (string, error) {
	if r.resolveFn != nil {
		return r.resolveFn(ctx, p, providerName, connection, instance)
	}
	return r.token, r.err
}

func TestNewServer_DirectCallerPassthrough(t *testing.T) {
	t.Parallel()

	var calledName string
	var calledArgs map[string]any
	var gotSubject egress.Subject

	cat := &catalog.Catalog{
		Name: "clickhouse",
		Operations: []catalog.CatalogOperation{
			{
				ID:          "run_query",
				Description: "Execute a SQL query",
				Transport:   catalog.TransportMCPPassthrough,
				InputSchema: json.RawMessage(`{"type":"object","properties":{"sql":{"type":"string"}}}`),
			},
		},
	}

	prov := &directCallerProvider{
		StubIntegration: coretesting.StubIntegration{N: "clickhouse"},
		ops:             []core.Operation{{Name: "run_query", Description: "Execute a SQL query"}},
		cat:             cat,
		callFn: func(ctx context.Context, name string, args map[string]any) (*mcpgo.CallToolResult, error) {
			subject, ok := egress.SubjectFromContext(ctx)
			if !ok {
				t.Fatal("expected egress subject in direct caller context")
			}
			gotSubject = subject
			calledName = name
			calledArgs = args
			return mcpgo.NewToolResultText("query result"), nil
		},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	broker := invocation.NewBroker(providers, stubDatastoreWithToken())
	caps := broker.ListCapabilities()
	if len(caps) != 1 {
		t.Fatalf("expected 1 capability, got %d", len(caps))
	}
	if caps[0].Operation != "run_query" {
		t.Fatalf("expected run_query capability, got %q", caps[0].Operation)
	}
	if caps[0].Transport != catalog.TransportMCPPassthrough {
		t.Fatalf("capability transport = %q, want %q", caps[0].Transport, catalog.TransportMCPPassthrough)
	}

	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:       broker,
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
	if gotSubject != (egress.Subject{Kind: egress.SubjectUser, ID: "u1"}) {
		t.Fatalf("subject = %+v, want user u1", gotSubject)
	}

	text, ok := result.Content[0].(mcpgo.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	if text.Text != "query result" {
		t.Fatalf("expected direct passthrough result, got %q", text.Text)
	}
}

func TestNewServer_RESTCatalogToolsUseOperationConnections(t *testing.T) {
	t.Parallel()

	pluginProv := &flatProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "plugin",
			ConnMode: core.ConnectionModeUser,
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, token string) (*core.OperationResult, error) {
				return &core.OperationResult{Status: http.StatusOK, Body: token}, nil
			},
		},
		ops: []core.Operation{{Name: "plugin_echo", Description: "Plugin-backed echo", Method: http.MethodGet}},
	}
	apiProv := &flatProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "api",
			ConnMode: core.ConnectionModeUser,
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, token string) (*core.OperationResult, error) {
				return &core.OperationResult{Status: http.StatusOK, Body: token}, nil
			},
		},
		ops: []core.Operation{{Name: "api_echo", Description: "API-backed echo", Method: http.MethodGet}},
	}

	merged, err := composite.NewMergedWithConnections(
		"hybrid",
		"Hybrid",
		"Hybrid provider",
		"",
		composite.BoundProvider{Provider: pluginProv, Connection: config.PluginConnectionName},
		composite.BoundProvider{Provider: apiProv, Connection: testAPIConnectionName},
	)
	if err != nil {
		t.Fatalf("NewMergedWithConnections: %v", err)
	}

	providers := testutil.NewProviderRegistry(t, merged)
	datastore := &coretesting.StubDatastore{
		TokenFn: func(_ context.Context, _, integration, connection, _ string) (*core.IntegrationToken, error) {
			if integration != "hybrid" {
				return nil, fmt.Errorf("unexpected integration %q", integration)
			}
			switch connection {
			case config.PluginConnectionName:
				return &core.IntegrationToken{AccessToken: testPluginAccessToken}, nil
			case testAPIConnectionName:
				return &core.IntegrationToken{AccessToken: testNamedAPIAccessToken}, nil
			default:
				return nil, fmt.Errorf("unexpected connection %q", connection)
			}
		},
	}
	broker := invocation.NewBroker(
		providers,
		datastore,
		invocation.WithConnectionMapper(invocation.ConnectionMap{"hybrid": testAPIConnectionName}),
	)

	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:       broker,
		Providers:     providers,
		APIConnection: map[string]string{"hybrid": testAPIConnectionName},
	})

	callTool := func(name string) string {
		t.Helper()
		tool := srv.GetTool(name)
		if tool == nil {
			t.Fatalf("tool %q not found", name)
		}
		req := mcpgo.CallToolRequest{}
		req.Params.Name = name

		result, err := tool.Handler(ctxWithPrincipal(), req)
		if err != nil {
			t.Fatalf("tool %q: %v", name, err)
		}
		if result.IsError {
			t.Fatalf("tool %q returned error: %v", name, result.Content)
		}
		text, ok := result.Content[0].(mcpgo.TextContent)
		if !ok {
			t.Fatalf("tool %q content type = %T", name, result.Content[0])
		}
		return text.Text
	}

	if got := callTool("hybrid_plugin_echo"); got != testPluginAccessToken {
		t.Fatalf("plugin tool token = %q, want %q", got, testPluginAccessToken)
	}
	if got := callTool("hybrid_api_echo"); got != testNamedAPIAccessToken {
		t.Fatalf("api tool token = %q, want %q", got, testNamedAPIAccessToken)
	}
}

func TestNewServer_DirectCallerUsesAPIConnectionForNonPassthroughTools(t *testing.T) {
	t.Parallel()

	var gotConnection string
	prov := &directCallerProvider{
		StubIntegration: coretesting.StubIntegration{N: "notion"},
		ops:             []core.Operation{{Name: "search", Description: "Search workspace"}},
		cat: &catalog.Catalog{
			Name: "notion",
			Operations: []catalog.CatalogOperation{
				{
					ID:          "search",
					Description: "Search workspace",
					InputSchema: json.RawMessage(`{"type":"object"}`),
				},
			},
		},
		callFn: func(_ context.Context, name string, _ map[string]any) (*mcpgo.CallToolResult, error) {
			if name != "search" {
				t.Fatalf("CallTool name = %q, want %q", name, "search")
			}
			return mcpgo.NewToolResultText("ok"), nil
		},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker: &testutil.StubInvoker{},
		TokenResolver: &stubTokenResolver{resolveFn: func(_ context.Context, _ *principal.Principal, providerName, connection, instance string) (string, error) {
			if providerName != "notion" {
				t.Fatalf("provider = %q, want %q", providerName, "notion")
			}
			if instance != "" {
				t.Fatalf("instance = %q, want empty", instance)
			}
			gotConnection = connection
			return "upstream-token", nil
		}},
		Providers:     providers,
		APIConnection: map[string]string{"notion": testAPIConnectionName},
	})

	tool := srv.GetTool("notion_search")
	if tool == nil {
		t.Fatal("tool not found")
	}

	req := mcpgo.CallToolRequest{}
	req.Params.Name = "notion_search"
	result, err := tool.Handler(ctxWithPrincipal(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %v", result.Content)
	}
	if gotConnection != testAPIConnectionName {
		t.Fatalf("connection = %q, want %q", gotConnection, testAPIConnectionName)
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
	srv := gestaltmcp.NewServer(gestaltmcp.Config{
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

func TestNewServer_DirectCallerUsesIdentitySubjectEmail(t *testing.T) {
	t.Parallel()

	var gotSubject egress.Subject

	cat := &catalog.Catalog{
		Name: "sample",
		Operations: []catalog.CatalogOperation{
			{ID: "perform", Description: "Perform action"},
		},
	}
	prov := &directCallerProvider{
		StubIntegration: coretesting.StubIntegration{N: "sample"},
		ops:             []core.Operation{{Name: "perform", Description: "Perform action"}},
		cat:             cat,
		callFn: func(ctx context.Context, _ string, _ map[string]any) (*mcpgo.CallToolResult, error) {
			var ok bool
			gotSubject, ok = egress.SubjectFromContext(ctx)
			if !ok {
				t.Fatal("expected egress subject in direct caller context")
			}
			return mcpgo.NewToolResultText("ok"), nil
		},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:       &testutil.StubInvoker{},
		TokenResolver: &stubTokenResolver{token: "test-token"},
		Providers:     providers,
	})

	tool := srv.GetTool("sample_perform")
	if tool == nil {
		t.Fatal("tool not found")
	}

	req := mcpgo.CallToolRequest{}
	req.Params.Name = "sample_perform"

	result, err := tool.Handler(ctxWithIdentityPrincipal("identity@example.invalid", principal.IdentityPrincipal), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %v", result.Content)
	}
	if gotSubject != (egress.Subject{Kind: egress.SubjectIdentity, ID: "identity@example.invalid"}) {
		t.Fatalf("subject = %+v, want identity email subject", gotSubject)
	}
}

func TestNewServer_DirectCallerUsesIdentitySubjectSentinel(t *testing.T) {
	t.Parallel()

	var gotSubject egress.Subject

	cat := &catalog.Catalog{
		Name: "sample",
		Operations: []catalog.CatalogOperation{
			{ID: "perform", Description: "Perform action"},
		},
	}
	prov := &directCallerProvider{
		StubIntegration: coretesting.StubIntegration{N: "sample"},
		ops:             []core.Operation{{Name: "perform", Description: "Perform action"}},
		cat:             cat,
		callFn: func(ctx context.Context, _ string, _ map[string]any) (*mcpgo.CallToolResult, error) {
			var ok bool
			gotSubject, ok = egress.SubjectFromContext(ctx)
			if !ok {
				t.Fatal("expected egress subject in direct caller context")
			}
			return mcpgo.NewToolResultText("ok"), nil
		},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:       &testutil.StubInvoker{},
		TokenResolver: &stubTokenResolver{token: "test-token"},
		Providers:     providers,
	})

	tool := srv.GetTool("sample_perform")
	if tool == nil {
		t.Fatal("tool not found")
	}

	req := mcpgo.CallToolRequest{}
	req.Params.Name = "sample_perform"

	result, err := tool.Handler(ctxWithIdentityPrincipal("", principal.IdentityPrincipal), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %v", result.Content)
	}
	if gotSubject != (egress.Subject{Kind: egress.SubjectIdentity, ID: principal.IdentityPrincipal}) {
		t.Fatalf("subject = %+v, want identity sentinel subject", gotSubject)
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
	srv := gestaltmcp.NewServer(gestaltmcp.Config{
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

func TestNewServer_DynamicCatalogProviderListsSessionTools(t *testing.T) {
	t.Parallel()

	var gotToken string
	var gotSubject egress.Subject
	prov := &directCallerProvider{
		StubIntegration: coretesting.StubIntegration{N: "clickhouse"},
		sessionCatalogFn: func(ctx context.Context, token string) (*catalog.Catalog, error) {
			subject, ok := egress.SubjectFromContext(ctx)
			if !ok {
				t.Fatal("expected egress subject in session catalog context")
			}
			gotSubject = subject
			gotToken = token
			return &catalog.Catalog{
				Name: "clickhouse",
				Operations: []catalog.CatalogOperation{
					{
						ID:          "run_query",
						Description: "Execute a SQL query",
						InputSchema: json.RawMessage(`{"type":"object","properties":{"sql":{"type":"string"}}}`),
					},
				},
			}, nil
		},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:       &testutil.StubInvoker{},
		TokenResolver: &stubTokenResolver{token: "upstream-token"},
		Providers:     providers,
	})

	if tools := srv.ListTools(); len(tools) != 0 {
		t.Fatalf("expected no global tools before session hydration, got %d", len(tools))
	}

	result := listToolsForSession(t, srv, ctxWithPrincipal(), newTestSessionWithTools())
	if gotToken != "upstream-token" {
		t.Fatalf("expected token upstream-token, got %q", gotToken)
	}
	if gotSubject != (egress.Subject{Kind: egress.SubjectUser, ID: "u1"}) {
		t.Fatalf("subject = %+v, want user u1", gotSubject)
	}
	if len(result.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result.Tools))
	}
	if result.Tools[0].Name != "clickhouse_run_query" {
		t.Fatalf("expected clickhouse_run_query, got %q", result.Tools[0].Name)
	}
}

func TestNewServer_DynamicCatalogProviderCallsSessionTool(t *testing.T) {
	t.Parallel()

	var calledName string
	var gotToken string
	var gotSubject egress.Subject

	prov := &directCallerProvider{
		StubIntegration: coretesting.StubIntegration{N: "clickhouse"},
		sessionCatalogFn: func(ctx context.Context, _ string) (*catalog.Catalog, error) {
			subject, ok := egress.SubjectFromContext(ctx)
			if !ok {
				t.Fatal("expected egress subject in session catalog context")
			}
			gotSubject = subject
			return &catalog.Catalog{
				Name: "clickhouse",
				Operations: []catalog.CatalogOperation{
					{
						ID:          "run_query",
						Description: "Execute a SQL query",
						InputSchema: json.RawMessage(`{"type":"object","properties":{"sql":{"type":"string"}}}`),
					},
				},
			}, nil
		},
		callFn: func(ctx context.Context, name string, args map[string]any) (*mcpgo.CallToolResult, error) {
			calledName = name
			gotToken = mcpupstream.UpstreamTokenFromContext(ctx)
			if args["sql"] != "SELECT 1" {
				t.Fatalf("expected sql argument, got %v", args)
			}
			return mcpgo.NewToolResultText("query result"), nil
		},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:       &testutil.StubInvoker{},
		TokenResolver: &stubTokenResolver{token: "upstream-token"},
		Providers:     providers,
	})

	result := callToolForSession(t, srv, ctxWithPrincipal(), newTestSessionWithTools(), "clickhouse_run_query", map[string]any{"sql": "SELECT 1"})
	if result.IsError {
		t.Fatalf("unexpected tool error: %v", result.Content)
	}
	if calledName != "run_query" {
		t.Fatalf("expected run_query, got %q", calledName)
	}
	if gotToken != "upstream-token" {
		t.Fatalf("expected upstream-token, got %q", gotToken)
	}
	if gotSubject != (egress.Subject{Kind: egress.SubjectUser, ID: "u1"}) {
		t.Fatalf("subject = %+v, want user u1", gotSubject)
	}
}

func TestNewServer_IncludeRESTFiltering(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		includeREST bool
		wantCount   int
	}{
		{"excluded", false, 1},
		{"included", true, 2},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cat := &catalog.Catalog{
				Name: "acme",
				Operations: []catalog.CatalogOperation{
					{ID: "api_op", Method: http.MethodGet, Path: "/api", Transport: catalog.TransportREST},
					{ID: "mcp_op", Description: "passthrough", Transport: catalog.TransportMCPPassthrough},
				},
			}

			prov := &catalogProvider{
				StubIntegration: coretesting.StubIntegration{N: "acme"},
				ops:             coreintegration.OperationsList(cat),
				catalog:         cat,
			}

			providers := testutil.NewProviderRegistry(t, prov)
			ds := stubDatastoreWithToken()
			broker := invocation.NewBroker(providers, ds)

			srv := gestaltmcp.NewServer(gestaltmcp.Config{
				Invoker:     broker,
				Providers:   providers,
				IncludeREST: map[string]bool{"acme": tc.includeREST},
			})

			tools := srv.ListTools()
			if len(tools) != tc.wantCount {
				t.Fatalf("expected %d tools, got %d", tc.wantCount, len(tools))
			}
			if tools["acme_mcp_op"] == nil {
				t.Fatal("expected acme_mcp_op to always be present")
			}
		})
	}
}

func TestNewServer_MCPPassthroughContract(t *testing.T) {
	t.Parallel()

	const (
		providerName  = "svc"
		operationName = "do_thing"
		toolName      = providerName + "_" + operationName
		tokenValue    = "test-token-abc"
	)

	var gotName string
	var gotArgs map[string]any
	var gotMeta *mcpgo.Meta
	var gotToken string

	inputSchema := json.RawMessage(`{"type":"object","properties":{"input":{"type":"string"}},"required":["input"]}`)
	outputSchema := json.RawMessage(`{"type":"object","properties":{"items":{"type":"array"}}}`)

	cat := &catalog.Catalog{
		Name: providerName,
		Operations: []catalog.CatalogOperation{
			{
				ID:           operationName,
				Description:  "A passthrough operation",
				Transport:    catalog.TransportMCPPassthrough,
				InputSchema:  inputSchema,
				OutputSchema: outputSchema,
			},
		},
	}

	prov := &directCallerProvider{
		StubIntegration: coretesting.StubIntegration{N: providerName},
		ops:             []core.Operation{{Name: operationName, Description: "A passthrough operation"}},
		cat:             cat,
		callFn: func(ctx context.Context, name string, args map[string]any) (*mcpgo.CallToolResult, error) {
			gotName = name
			gotArgs = args
			gotMeta = mcpupstream.CallToolMetaFromContext(ctx)
			gotToken = mcpupstream.UpstreamTokenFromContext(ctx)
			return &mcpgo.CallToolResult{
				Content:           []mcpgo.Content{mcpgo.NewTextContent(`{"ok":true}`)},
				StructuredContent: map[string]any{"ok": true},
			}, nil
		},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:       &testutil.StubInvoker{},
		TokenResolver: &stubTokenResolver{token: tokenValue},
		Providers:     providers,
	})

	tools := srv.ListTools()
	st := tools[toolName]
	if st == nil {
		t.Fatalf("tool %q not in list", toolName)
	}

	raw, err := json.Marshal(st.Tool)
	if err != nil {
		t.Fatalf("marshal tool: %v", err)
	}
	var toolJSON map[string]any
	if err := json.Unmarshal(raw, &toolJSON); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if toolJSON["inputSchema"] == nil {
		t.Fatal("inputSchema missing from tool definition")
	}
	if toolJSON["outputSchema"] == nil {
		t.Fatal("outputSchema missing from tool definition")
	}

	tool := srv.GetTool(toolName)
	if tool == nil {
		t.Fatalf("tool %q not found", toolName)
	}

	ctx := ctxWithPrincipal()
	req := mcpgo.CallToolRequest{}
	req.Params.Name = toolName
	req.Params.Arguments = map[string]any{"input": "hello"}
	req.Params.Meta = &mcpgo.Meta{
		ProgressToken:    mcpgo.ProgressToken("pt-1"),
		AdditionalFields: map[string]any{"custom_field": "custom_value"},
	}

	result, err := tool.Handler(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotName != operationName {
		t.Fatalf("upstream received name %q, want %q", gotName, operationName)
	}
	if gotArgs["input"] != "hello" {
		t.Fatalf("upstream received args %v, want input=hello", gotArgs)
	}
	if gotToken != tokenValue {
		t.Fatalf("upstream received token %q, want %q", gotToken, tokenValue)
	}
	if gotMeta == nil {
		t.Fatal("upstream did not receive _meta")
	}
	if gotMeta.ProgressToken != mcpgo.ProgressToken("pt-1") {
		t.Fatalf("upstream received progressToken %v, want pt-1", gotMeta.ProgressToken)
	}
	if gotMeta.AdditionalFields["custom_field"] != "custom_value" {
		t.Fatal("upstream did not receive _meta additional fields")
	}

	if result.IsError {
		t.Fatalf("unexpected tool error: %v", result.Content)
	}
	if result.StructuredContent == nil {
		t.Fatal("structuredContent not passed through from upstream")
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}
}

func boolPtr(v bool) *bool { return &v }
