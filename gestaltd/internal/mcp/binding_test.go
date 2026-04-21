package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coreintegration "github.com/valon-technologies/gestalt/server/core/integration"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/authorization"
	"github.com/valon-technologies/gestalt/server/internal/composite"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	gestaltmcp "github.com/valon-technologies/gestalt/server/internal/mcp"
	"github.com/valon-technologies/gestalt/server/internal/mcpupstream"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/registry"
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

func stubServicesWithToken(t *testing.T, integrations ...string) (*coredata.Services, string) {
	t.Helper()
	svc := coretesting.NewStubServices(t)
	ctx := context.Background()
	u, err := svc.Users.FindOrCreateUser(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("FindOrCreateUser: %v", err)
	}
	if len(integrations) == 0 {
		integrations = []string{"test"}
	}
	for i, intg := range integrations {
		if err := svc.Tokens.StoreToken(ctx, &core.IntegrationToken{
			ID: fmt.Sprintf("tok%d", i+1), SubjectID: principal.UserSubjectID(u.ID), Integration: intg,
			Connection: "", Instance: "default",
			AccessToken: intg + "-token",
		}); err != nil {
			t.Fatalf("StoreToken: %v", err)
		}
	}
	return svc, u.ID
}

func ctxWithPrincipal(userID string) context.Context {
	p := &principal.Principal{
		Identity: &core.UserIdentity{Email: "test@example.com"},
		UserID:   userID,
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

func ctxWithWorkloadPrincipal(workloadID string) context.Context {
	p := &principal.Principal{
		Kind:      principal.KindWorkload,
		SubjectID: principal.WorkloadSubjectID(workloadID),
		Source:    principal.SourceWorkloadToken,
	}
	return principal.WithPrincipal(context.Background(), p)
}

func mustAuthorizer(t *testing.T, cfg config.AuthorizationConfig, providers *registry.ProviderMap[core.Provider]) *authorization.Authorizer {
	t.Helper()
	authz, err := authorization.New(cfg, nil, providers, map[string]string{})
	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}
	return authz
}

type testSessionWithTools struct {
	id            string
	initialized   bool
	notifications chan mcpgo.JSONRPCNotification
	tools         map[string]mcpserver.ServerTool
}

func newTestSessionWithTools() *testSessionWithTools {
	return &testSessionWithTools{
		id:            fmt.Sprintf("session-%d", time.Now().UnixNano()),
		initialized:   true,
		notifications: make(chan mcpgo.JSONRPCNotification, 1),
	}
}

func hydrationMarkerToolName(provider string) string {
	return "__gestalt_internal_hydrated__:" + provider
}

func sessionCatalogOperationMarkerToolName(provider, operation string) string {
	return "__gestalt_internal_catalog_op__:" + provider + ":" + operation
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
	ds, _ := stubServicesWithToken(t)
	broker := invocation.NewBroker(providers, ds.Users, ds.Tokens)

	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:       broker,
		TokenResolver: broker,
		Providers:     providers,
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
	ds, _ := stubServicesWithToken(t)
	broker := invocation.NewBroker(providers, ds.Users, ds.Tokens)

	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:       broker,
		TokenResolver: broker,
		Providers:     providers,
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
	ds, _ := stubServicesWithToken(t)
	broker := invocation.NewBroker(providers, ds.Users, ds.Tokens)

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
	ds, userID := stubServicesWithToken(t)
	broker := invocation.NewBroker(providers, ds.Users, ds.Tokens)

	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:       broker,
		TokenResolver: broker,
		Providers:     providers,
	})

	tool := srv.GetTool("test_do_thing")
	if tool == nil {
		t.Fatal("tool not found")
	}

	ctx := ctxWithPrincipal(userID)
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

	ctx := ctxWithPrincipal("stub-user-id")
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
	ds, userID := stubServicesWithToken(t)
	broker := invocation.NewBroker(providers, ds.Users, ds.Tokens)

	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:       broker,
		TokenResolver: broker,
		Providers:     providers,
	})

	tool := srv.GetTool("test_forbidden_op")
	ctx := ctxWithPrincipal(userID)
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
	ds, userID := stubServicesWithToken(t)
	broker := invocation.NewBroker(providers, ds.Users, ds.Tokens)

	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:       broker,
		TokenResolver: broker,
		Providers:     providers,
	})

	tool := srv.GetTool("test_flaky_op")
	ctx := ctxWithPrincipal(userID)
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
	ds, _ := stubServicesWithToken(t)
	broker := invocation.NewBroker(providers, ds.Users, ds.Tokens)

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
	ds, _ := stubServicesWithToken(t)
	broker := invocation.NewBroker(providers, ds.Users, ds.Tokens)

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
	ds, _ := stubServicesWithToken(t)
	broker := invocation.NewBroker(providers, ds.Users, ds.Tokens)

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
	resolveFn func(context.Context, *principal.Principal, string, string, string) (context.Context, string, error)
}

func (r *stubTokenResolver) ResolveToken(ctx context.Context, p *principal.Principal, providerName, connection, instance string) (context.Context, string, error) {
	if r.resolveFn != nil {
		return r.resolveFn(ctx, p, providerName, connection, instance)
	}
	return ctx, r.token, r.err
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
			calledName = name
			calledArgs = args
			return mcpgo.NewToolResultText("query result"), nil
		},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	ds, userID := stubServicesWithToken(t, "clickhouse")
	broker := invocation.NewBroker(providers, ds.Users, ds.Tokens)
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

	ctx := ctxWithPrincipal(userID)
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

func TestNewServer_DirectCallerPassthroughConnectionModeNoneSetsCredentialContext(t *testing.T) {
	t.Parallel()

	var seen invocation.CredentialContext
	prov := &directCallerProvider{
		StubIntegration: coretesting.StubIntegration{N: "clickhouse", ConnMode: core.ConnectionModeNone},
		cat: &catalog.Catalog{
			Name: "clickhouse",
			Operations: []catalog.CatalogOperation{
				{
					ID:        "run_query",
					Transport: catalog.TransportMCPPassthrough,
				},
			},
		},
		callFn: func(ctx context.Context, name string, args map[string]any) (*mcpgo.CallToolResult, error) {
			seen = invocation.CredentialContextFromContext(ctx)
			return mcpgo.NewToolResultText("query result"), nil
		},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	ds, userID := stubServicesWithToken(t, "clickhouse")
	broker := invocation.NewBroker(providers, ds.Users, ds.Tokens)

	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:   broker,
		Providers: providers,
	})

	tool := srv.GetTool("clickhouse_run_query")
	if tool == nil {
		t.Fatal("tool not found")
	}

	req := mcpgo.CallToolRequest{}
	req.Params.Name = "clickhouse_run_query"
	req.Params.Arguments = map[string]any{"sql": "SELECT 1"}

	result, err := tool.Handler(ctxWithPrincipal(userID), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %v", result.Content)
	}
	if seen.Mode != core.ConnectionModeNone {
		t.Fatalf("credential mode = %q, want %q", seen.Mode, core.ConnectionModeNone)
	}
	if seen.SubjectID != "" || seen.Connection != "" || seen.Instance != "" {
		t.Fatalf("unexpected credential context: %+v", seen)
	}
}

func TestNewServer_SessionCatalogConnectionModeNoneSetsCredentialContext(t *testing.T) {
	t.Parallel()

	var seen invocation.CredentialContext
	var seenPrincipal *principal.Principal
	prov := &directCallerProvider{
		StubIntegration: coretesting.StubIntegration{N: "clickhouse", ConnMode: core.ConnectionModeNone},
		cat:             &catalog.Catalog{Name: "clickhouse"},
		sessionCatalogFn: func(ctx context.Context, token string) (*catalog.Catalog, error) {
			if token != "" {
				t.Fatalf("session catalog token = %q, want empty", token)
			}
			seen = invocation.CredentialContextFromContext(ctx)
			seenPrincipal = principal.FromContext(ctx)
			return &catalog.Catalog{
				Name: "clickhouse",
				Operations: []catalog.CatalogOperation{
					{
						ID:          "run_query",
						Description: "Execute a SQL query",
						Transport:   catalog.TransportMCPPassthrough,
					},
				},
			}, nil
		},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	ds, userID := stubServicesWithToken(t, "clickhouse")
	broker := invocation.NewBroker(providers, ds.Users, ds.Tokens)

	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:       broker,
		TokenResolver: broker,
		Providers:     providers,
	})

	session := newTestSessionWithTools()
	result := listToolsForSession(t, srv, ctxWithIdentityPrincipal("test@example.com", ""), session)
	if len(result.Tools) != 1 || result.Tools[0].Name != "clickhouse_run_query" {
		t.Fatalf("unexpected tools = %+v", result.Tools)
	}
	if seen.Mode != core.ConnectionModeNone {
		t.Fatalf("credential mode = %q, want %q", seen.Mode, core.ConnectionModeNone)
	}
	if seen.SubjectID != "" || seen.Connection != "" || seen.Instance != "" {
		t.Fatalf("unexpected credential context: %+v", seen)
	}
	if seenPrincipal == nil || seenPrincipal.UserID != userID {
		t.Fatalf("principal userID = %+v, want %q", seenPrincipal, userID)
	}
	if seenPrincipal.SubjectID != principal.UserSubjectID(userID) {
		t.Fatalf("principal subjectID = %q, want %q", seenPrincipal.SubjectID, principal.UserSubjectID(userID))
	}
}

func TestNewServer_WorkloadListToolsFiltersStaticAndSessionTools(t *testing.T) {
	t.Parallel()

	staticCat := &catalog.Catalog{
		Name: "clickhouse",
		Operations: []catalog.CatalogOperation{
			{
				ID:          "run_query",
				Description: "static query",
				Transport:   catalog.TransportMCPPassthrough,
			},
			{
				ID:          "delete_table",
				Description: "delete a table",
				Transport:   catalog.TransportMCPPassthrough,
			},
		},
	}

	sessionCat := &catalog.Catalog{
		Name: "clickhouse",
		Operations: []catalog.CatalogOperation{
			{
				ID:          "run_query",
				Description: "session query",
				Transport:   catalog.TransportMCPPassthrough,
			},
			{
				ID:          "list_databases",
				Description: "list databases",
				Transport:   catalog.TransportMCPPassthrough,
			},
			{
				ID:          "drop_database",
				Description: "drop a database",
				Transport:   catalog.TransportMCPPassthrough,
			},
		},
	}

	prov := &directCallerProvider{
		StubIntegration: coretesting.StubIntegration{N: "clickhouse", ConnMode: core.ConnectionModeIdentity},
		cat:             staticCat,
		sessionCatalogFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
			if token != "identity-token" {
				t.Fatalf("session catalog token = %q, want %q", token, "identity-token")
			}
			return sessionCat, nil
		},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	authz := mustAuthorizer(t, config.AuthorizationConfig{
		Workloads: map[string]config.WorkloadDef{
			"triage-bot": {
				Token: "gst_wld_triage-bot-token",
				Providers: map[string]config.WorkloadProviderDef{
					"clickhouse": {
						Connection: "workspace",
						Instance:   "team-a",
						Allow:      []string{"run_query", "list_databases"},
					},
				},
			},
		},
	}, providers)

	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker: &testutil.StubInvoker{
			InvokeFn: func(context.Context, *principal.Principal, string, string, string, map[string]any) (*core.OperationResult, error) {
				return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
			},
		},
		TokenResolver: &stubTokenResolver{
			resolveFn: func(ctx context.Context, p *principal.Principal, providerName, connection, instance string) (context.Context, string, error) {
				if p == nil || p.Kind != principal.KindWorkload {
					t.Fatalf("expected workload principal, got %+v", p)
				}
				if providerName != "clickhouse" {
					t.Fatalf("providerName = %q, want %q", providerName, "clickhouse")
				}
				if connection != "workspace" || instance != "team-a" {
					t.Fatalf("unexpected session hydration selector inputs: connection=%q instance=%q", connection, instance)
				}
				return ctx, "identity-token", nil
			},
		},
		Providers:     providers,
		Authorizer:    authz,
		MCPConnection: map[string]string{"clickhouse": "default"},
	})

	session := newTestSessionWithTools()
	result := listToolsForSession(t, srv, ctxWithWorkloadPrincipal("triage-bot"), session)

	names := make([]string, 0, len(result.Tools))
	descriptions := map[string]string{}
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
		descriptions[tool.Name] = tool.Description
	}
	sort.Strings(names)

	if !reflect.DeepEqual(names, []string{"clickhouse_list_databases", "clickhouse_run_query"}) {
		t.Fatalf("tool names = %v, want %v", names, []string{"clickhouse_list_databases", "clickhouse_run_query"})
	}
	if descriptions["clickhouse_run_query"] != "static query" {
		t.Fatalf("run_query description = %q, want %q", descriptions["clickhouse_run_query"], "static query")
	}
	sessionTools := session.GetSessionTools()
	if _, ok := sessionTools["clickhouse_run_query"]; ok {
		t.Fatal("expected static tool collision to keep global tool and skip session override")
	}
	if _, ok := sessionTools["clickhouse_list_databases"]; !ok {
		t.Fatal("expected allowed session-only tool to remain registered for the session")
	}
	if _, ok := sessionTools["clickhouse_drop_database"]; !ok {
		t.Fatal("expected denied session-only tool to remain registered for call-time authorization")
	}
	if _, ok := sessionTools[hydrationMarkerToolName("clickhouse")]; !ok {
		t.Fatal("expected hydration marker to remain in session tool state")
	}
}

func TestNewServer_HumanListToolsFiltersRoleRestrictedTools(t *testing.T) {
	t.Parallel()

	staticCat := &catalog.Catalog{
		Name: "sampledb",
		Operations: []catalog.CatalogOperation{
			{
				ID:           "run_query",
				Description:  "run a query",
				Transport:    catalog.TransportMCPPassthrough,
				AllowedRoles: []string{"viewer", "admin"},
			},
			{
				ID:           "delete_table",
				Description:  "delete a table",
				Transport:    catalog.TransportMCPPassthrough,
				AllowedRoles: []string{"admin"},
			},
			{
				ID:          "export_results",
				Description: "export results",
				Transport:   catalog.TransportMCPPassthrough,
			},
		},
	}

	prov := &directCallerProvider{
		StubIntegration: coretesting.StubIntegration{N: "sampledb", ConnMode: core.ConnectionModeNone},
		cat:             staticCat,
	}

	providers := testutil.NewProviderRegistry(t, prov)
	_, userID := stubServicesWithToken(t, "sampledb")
	authz, err := authorization.New(config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.HumanPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(userID), Role: "viewer"},
				},
			},
		},
	}, map[string]*config.ProviderEntry{
		"sampledb": {AuthorizationPolicy: "sample_policy"},
	}, providers, map[string]string{})
	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}

	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker: &testutil.StubInvoker{
			InvokeFn: func(context.Context, *principal.Principal, string, string, string, map[string]any) (*core.OperationResult, error) {
				return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
			},
		},
		Providers:  providers,
		Authorizer: authz,
	})

	result := listToolsForSession(t, srv, ctxWithPrincipal(userID), newTestSessionWithTools())
	if len(result.Tools) != 1 || result.Tools[0].Name != "sampledb_run_query" {
		t.Fatalf("tool names = %+v, want only sampledb_run_query", result.Tools)
	}
}

func TestNewServer_HumanListToolsUsesSessionMetadataForStaticCollisions(t *testing.T) {
	t.Parallel()

	prov := &directCallerProvider{
		StubIntegration: coretesting.StubIntegration{N: "sampledb", ConnMode: core.ConnectionModeUser},
		cat: &catalog.Catalog{
			Name: "sampledb",
			Operations: []catalog.CatalogOperation{
				{
					ID:           "run_query",
					Description:  "static query",
					Transport:    catalog.TransportMCPPassthrough,
					AllowedRoles: []string{"viewer"},
				},
			},
		},
		sessionCatalogFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
			if token != "default-token" {
				t.Fatalf("session catalog token = %q, want %q", token, "default-token")
			}
			return &catalog.Catalog{
				Name: "sampledb",
				Operations: []catalog.CatalogOperation{
					{
						ID:           "run_query",
						Description:  "session query",
						Transport:    catalog.TransportMCPPassthrough,
						AllowedRoles: []string{"admin"},
					},
				},
			}, nil
		},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	ds := coretesting.NewStubServices(t)
	const userID = "viewer-user"
	if err := ds.Tokens.StoreToken(context.Background(), &core.IntegrationToken{
		ID:          "tok-default",
		SubjectID:   principal.UserSubjectID(userID),
		Integration: "sampledb",
		Connection:  "workspace",
		Instance:    "default",
		AccessToken: "default-token",
	}); err != nil {
		t.Fatalf("StoreToken: %v", err)
	}
	authz, err := authorization.New(config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.HumanPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(userID), Role: "viewer"},
				},
			},
		},
	}, map[string]*config.ProviderEntry{
		"sampledb": {AuthorizationPolicy: "sample_policy"},
	}, providers, map[string]string{})
	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}
	broker := invocation.NewBroker(providers, ds.Users, ds.Tokens, invocation.WithAuthorizer(authz))

	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:       broker,
		TokenResolver: broker,
		Providers:     providers,
		Authorizer:    authz,
		MCPConnection: map[string]string{"sampledb": "workspace"},
	})

	result := listToolsForSession(t, srv, ctxWithPrincipal(userID), newTestSessionWithTools())
	if len(result.Tools) != 0 {
		t.Fatalf("tool names = %+v, want no visible tools", result.Tools)
	}
}

func TestNewServer_HumanListToolsUsesHydratedSessionCatalogSnapshot(t *testing.T) {
	t.Parallel()

	var sessionCatalogCalls int
	sessionCatalog := &catalog.Catalog{
		Name: "sampledb",
		Operations: []catalog.CatalogOperation{
			{
				ID:           "run_query",
				Description:  "run a query",
				Transport:    catalog.TransportMCPPassthrough,
				AllowedRoles: []string{"viewer"},
			},
		},
	}
	prov := &directCallerProvider{
		StubIntegration: coretesting.StubIntegration{N: "sampledb", ConnMode: core.ConnectionModeNone},
		sessionCatalogFn: func(_ context.Context, _ string) (*catalog.Catalog, error) {
			sessionCatalogCalls++
			if sessionCatalogCalls > 1 {
				return nil, fmt.Errorf("catalog unavailable")
			}
			return sessionCatalog, nil
		},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	ds, userID := stubServicesWithToken(t, "sampledb")
	authz, err := authorization.New(config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.HumanPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(userID), Role: "viewer"},
				},
			},
		},
	}, map[string]*config.ProviderEntry{
		"sampledb": {AuthorizationPolicy: "sample_policy"},
	}, providers, map[string]string{})
	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}
	broker := invocation.NewBroker(providers, ds.Users, ds.Tokens, invocation.WithAuthorizer(authz))

	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:       broker,
		TokenResolver: broker,
		Providers:     providers,
		Authorizer:    authz,
	})

	session := newTestSessionWithTools()
	result := listToolsForSession(t, srv, ctxWithPrincipal(userID), session)
	if len(result.Tools) != 1 || result.Tools[0].Name != "sampledb_run_query" {
		t.Fatalf("tool names = %+v, want only sampledb_run_query", result.Tools)
	}
	if _, ok := session.GetSessionTools()["sampledb_run_query"]; !ok {
		t.Fatal("expected session hydration to cache the session tool")
	}
	if sessionCatalogCalls != 1 {
		t.Fatalf("sessionCatalogCalls = %d, want 1", sessionCatalogCalls)
	}

	sessionCatalog.Operations[0].AllowedRoles = []string{"admin"}
	result = listToolsForSession(t, srv, ctxWithPrincipal(userID), session)
	if len(result.Tools) != 1 || result.Tools[0].Name != "sampledb_run_query" {
		t.Fatalf("tool names after mutation = %+v, want only sampledb_run_query", result.Tools)
	}
	callResult := callToolForSession(t, srv, ctxWithPrincipal(userID), session, "sampledb_run_query", map[string]any{
		"sql": "select 1",
	})
	if callResult.IsError {
		t.Fatalf("call result = %+v, want success", callResult)
	}
	if sessionCatalogCalls != 1 {
		t.Fatalf("sessionCatalogCalls after call = %d, want 1", sessionCatalogCalls)
	}
}

func TestNewServer_HumanListToolsIgnoresHiddenStaticCollisions(t *testing.T) {
	t.Parallel()

	hidden := false
	prov := &directCallerProvider{
		StubIntegration: coretesting.StubIntegration{N: "sampledb", ConnMode: core.ConnectionModeNone},
		cat: &catalog.Catalog{
			Name: "sampledb",
			Operations: []catalog.CatalogOperation{
				{
					ID:           "run_query",
					Description:  "hidden static query",
					Transport:    catalog.TransportMCPPassthrough,
					AllowedRoles: []string{"admin"},
					Visible:      &hidden,
				},
			},
		},
		sessionCatalogFn: func(_ context.Context, _ string) (*catalog.Catalog, error) {
			return &catalog.Catalog{
				Name: "sampledb",
				Operations: []catalog.CatalogOperation{
					{
						ID:           "run_query",
						Description:  "viewer session query",
						Transport:    catalog.TransportMCPPassthrough,
						AllowedRoles: []string{"viewer"},
					},
				},
			}, nil
		},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	ds, userID := stubServicesWithToken(t, "sampledb")
	authz, err := authorization.New(config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.HumanPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(userID), Role: "viewer"},
				},
			},
		},
	}, map[string]*config.ProviderEntry{
		"sampledb": {AuthorizationPolicy: "sample_policy"},
	}, providers, map[string]string{})
	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}
	broker := invocation.NewBroker(providers, ds.Users, ds.Tokens, invocation.WithAuthorizer(authz))

	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:       broker,
		TokenResolver: broker,
		Providers:     providers,
		Authorizer:    authz,
	})

	result := listToolsForSession(t, srv, ctxWithPrincipal(userID), newTestSessionWithTools())
	if len(result.Tools) != 1 || result.Tools[0].Name != "sampledb_run_query" {
		t.Fatalf("tool names = %+v, want only sampledb_run_query", result.Tools)
	}
}

func TestNewServer_SessionCatalogSuppressionHidesStaticMCPTool(t *testing.T) {
	t.Parallel()

	visible := true
	hidden := false
	prov := &directCallerProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "sampledb",
			ConnMode: core.ConnectionModeUser,
		},
		cat: &catalog.Catalog{
			Name: "sampledb",
			Operations: []catalog.CatalogOperation{
				{
					ID:          "run_query",
					Description: "static query",
					Transport:   catalog.TransportMCPPassthrough,
					Visible:     &visible,
				},
			},
		},
		sessionCatalogFn: func(_ context.Context, _ string) (*catalog.Catalog, error) {
			return &catalog.Catalog{
				Name: "sampledb",
				Operations: []catalog.CatalogOperation{
					{
						ID:          "run_query",
						Description: "hidden session query",
						Transport:   catalog.TransportMCPPassthrough,
						Visible:     &hidden,
					},
				},
			}, nil
		},
		callFn: func(context.Context, string, map[string]any) (*mcpgo.CallToolResult, error) {
			return mcpgo.NewToolResultText("unexpected provider call"), nil
		},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	ds, userID := stubServicesWithToken(t, "sampledb")
	broker := invocation.NewBroker(providers, ds.Users, ds.Tokens)
	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:       broker,
		TokenResolver: broker,
		Providers:     providers,
	})

	result := listToolsForSession(t, srv, ctxWithPrincipal(userID), newTestSessionWithTools())
	if len(result.Tools) != 0 {
		t.Fatalf("tool names = %+v, want no visible tools", result.Tools)
	}

	callResult := callToolForSession(t, srv, ctxWithPrincipal(userID), newTestSessionWithTools(), "sampledb_run_query", map[string]any{
		"sql": "select 1",
	})
	if !callResult.IsError {
		t.Fatalf("expected error result, got %+v", callResult)
	}
	text, ok := callResult.Content[0].(mcpgo.TextContent)
	if !ok || text.Text != "requested instance is unavailable for this tool" {
		t.Fatalf("unexpected call error content: %+v", callResult.Content)
	}
}

func TestNewServer_HumanListToolsDoesNotLeakAcrossStatelessSessions(t *testing.T) {
	t.Parallel()

	prov := &directCallerProvider{
		StubIntegration: coretesting.StubIntegration{N: "sampledb", ConnMode: core.ConnectionModeNone},
		sessionCatalogFn: func(ctx context.Context, _ string) (*catalog.Catalog, error) {
			switch invocation.AccessContextFromContext(ctx).Role {
			case "viewer":
				return &catalog.Catalog{
					Name: "sampledb",
					Operations: []catalog.CatalogOperation{
						{
							ID:           "viewer_query",
							Description:  "viewer query",
							Transport:    catalog.TransportMCPPassthrough,
							AllowedRoles: []string{"viewer"},
						},
					},
				}, nil
			case "admin":
				return &catalog.Catalog{
					Name: "sampledb",
					Operations: []catalog.CatalogOperation{
						{
							ID:           "admin_query",
							Description:  "admin query",
							Transport:    catalog.TransportMCPPassthrough,
							AllowedRoles: []string{"admin"},
						},
					},
				}, nil
			default:
				return &catalog.Catalog{Name: "sampledb"}, nil
			}
		},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	ds := coretesting.NewStubServices(t)
	viewer, err := ds.Users.FindOrCreateUser(context.Background(), "viewer@example.test")
	if err != nil {
		t.Fatalf("FindOrCreateUser viewer: %v", err)
	}
	admin, err := ds.Users.FindOrCreateUser(context.Background(), "admin@example.test")
	if err != nil {
		t.Fatalf("FindOrCreateUser admin: %v", err)
	}
	authz, err := authorization.New(config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.HumanPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(viewer.ID), Role: "viewer"},
					{SubjectID: principal.UserSubjectID(admin.ID), Role: "admin"},
				},
			},
		},
	}, map[string]*config.ProviderEntry{
		"sampledb": {AuthorizationPolicy: "sample_policy"},
	}, providers, map[string]string{})
	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}
	broker := invocation.NewBroker(providers, ds.Users, ds.Tokens, invocation.WithAuthorizer(authz))

	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:       broker,
		TokenResolver: broker,
		Providers:     providers,
		Authorizer:    authz,
	})

	viewerSession := newTestSessionWithTools()
	viewerSession.id = ""
	viewerResult := listToolsForSession(t, srv, ctxWithPrincipal(viewer.ID), viewerSession)
	if len(viewerResult.Tools) != 1 || viewerResult.Tools[0].Name != "sampledb_viewer_query" {
		t.Fatalf("viewer tool names = %+v, want only sampledb_viewer_query", viewerResult.Tools)
	}

	adminSession := newTestSessionWithTools()
	adminSession.id = ""
	adminResult := listToolsForSession(t, srv, ctxWithPrincipal(admin.ID), adminSession)
	if len(adminResult.Tools) != 1 || adminResult.Tools[0].Name != "sampledb_admin_query" {
		t.Fatalf("admin tool names = %+v, want only sampledb_admin_query", adminResult.Tools)
	}
}

func TestNewServer_HumanListToolsHidesInternalInstanceMarkers(t *testing.T) {
	t.Parallel()

	prov := &directCallerProvider{
		StubIntegration: coretesting.StubIntegration{N: "sampledb", ConnMode: core.ConnectionModeNone},
		cat: &catalog.Catalog{
			Name: "sampledb",
			Operations: []catalog.CatalogOperation{
				{
					ID:          "run_query",
					Description: "run a query",
					Transport:   catalog.TransportMCPPassthrough,
				},
			},
		},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker: &testutil.StubInvoker{
			InvokeFn: func(context.Context, *principal.Principal, string, string, string, map[string]any) (*core.OperationResult, error) {
				return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
			},
		},
		Providers: providers,
	})

	session := newTestSessionWithTools()
	session.SetSessionTools(map[string]mcpserver.ServerTool{
		"__gestalt_internal_instance_tool__:sampledb:cnVuX3F1ZXJ5:dGVhbS1i": {
			Tool: mcpgo.NewTool("__gestalt_internal_instance_tool__:sampledb:cnVuX3F1ZXJ5:dGVhbS1i"),
		},
	})

	result := listToolsForSession(t, srv, ctxWithPrincipal("viewer-user"), session)
	if len(result.Tools) != 1 || result.Tools[0].Name != "sampledb_run_query" {
		t.Fatalf("tool names = %+v, want only sampledb_run_query", result.Tools)
	}
}

func TestNewServer_HumanSessionCatalogReceivesAccessContext(t *testing.T) {
	t.Parallel()

	var seen []invocation.AccessContext
	prov := &directCallerProvider{
		StubIntegration: coretesting.StubIntegration{N: "sampledb", ConnMode: core.ConnectionModeNone},
		sessionCatalogFn: func(ctx context.Context, _ string) (*catalog.Catalog, error) {
			seen = append(seen, invocation.AccessContextFromContext(ctx))
			return &catalog.Catalog{
				Name: "sampledb",
				Operations: []catalog.CatalogOperation{
					{
						ID:           "run_query",
						Description:  "run a query",
						Transport:    catalog.TransportMCPPassthrough,
						AllowedRoles: []string{"viewer"},
					},
				},
			}, nil
		},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	ds, userID := stubServicesWithToken(t, "sampledb")
	authz, err := authorization.New(config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.HumanPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(userID), Role: "viewer"},
				},
			},
		},
	}, map[string]*config.ProviderEntry{
		"sampledb": {AuthorizationPolicy: "sample_policy"},
	}, providers, map[string]string{})
	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}
	broker := invocation.NewBroker(providers, ds.Users, ds.Tokens, invocation.WithAuthorizer(authz))

	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:       broker,
		TokenResolver: broker,
		Providers:     providers,
		Authorizer:    authz,
	})

	result := listToolsForSession(t, srv, ctxWithPrincipal(userID), newTestSessionWithTools())
	if len(result.Tools) != 1 || result.Tools[0].Name != "sampledb_run_query" {
		t.Fatalf("tool names = %+v, want only sampledb_run_query", result.Tools)
	}
	if len(seen) == 0 {
		t.Fatal("expected session catalog to receive access context")
	}
	for _, access := range seen {
		if access.Policy != "sample_policy" || access.Role != "viewer" {
			t.Fatalf("access context = %+v, want policy=sample_policy role=viewer", access)
		}
	}
}

func TestNewServer_HumanCallToolDeniedWhenAllowedRolesAreMissing(t *testing.T) {
	t.Parallel()

	var called bool
	prov := &directCallerProvider{
		StubIntegration: coretesting.StubIntegration{N: "sampledb", ConnMode: core.ConnectionModeNone},
		cat: &catalog.Catalog{
			Name: "sampledb",
			Operations: []catalog.CatalogOperation{
				{
					ID:          "export_results",
					Description: "export results",
					Transport:   catalog.TransportMCPPassthrough,
				},
			},
		},
		callFn: func(context.Context, string, map[string]any) (*mcpgo.CallToolResult, error) {
			called = true
			return mcpgo.NewToolResultText("should not run"), nil
		},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	ds, userID := stubServicesWithToken(t, "sampledb")
	authz, err := authorization.New(config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.HumanPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(userID), Role: "viewer"},
				},
			},
		},
	}, map[string]*config.ProviderEntry{
		"sampledb": {AuthorizationPolicy: "sample_policy"},
	}, providers, map[string]string{})
	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}
	broker := invocation.NewBroker(providers, ds.Users, ds.Tokens, invocation.WithAuthorizer(authz))

	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:       broker,
		TokenResolver: broker,
		Providers:     providers,
		Authorizer:    authz,
	})

	result := callToolForSession(t, srv, ctxWithPrincipal(userID), newTestSessionWithTools(), "sampledb_export_results", map[string]any{})
	if called {
		t.Fatal("expected export_results to be denied before provider execution")
	}
	if !result.IsError {
		t.Fatalf("expected error result, got %+v", result)
	}
}

func TestNewServer_WorkloadCallToolDeniedReturnsErrorResult(t *testing.T) {
	t.Parallel()

	var sessionCatalogCalls int
	var called bool
	prov := &directCallerProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "clickhouse",
			ConnMode: core.ConnectionModeNone,
		},
		sessionCatalogFn: func(_ context.Context, _ string) (*catalog.Catalog, error) {
			sessionCatalogCalls++
			return &catalog.Catalog{
				Name: "clickhouse",
				Operations: []catalog.CatalogOperation{
					{
						ID:          "run_query",
						Description: "run a query",
						Transport:   catalog.TransportMCPPassthrough,
					},
					{
						ID:          "delete_table",
						Description: "delete a table",
						Transport:   catalog.TransportMCPPassthrough,
					},
				},
			}, nil
		},
		callFn: func(_ context.Context, _ string, _ map[string]any) (*mcpgo.CallToolResult, error) {
			called = true
			return mcpgo.NewToolResultText("unexpected provider call"), nil
		},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	ds := coretesting.NewStubServices(t)
	authz := mustAuthorizer(t, config.AuthorizationConfig{
		Workloads: map[string]config.WorkloadDef{
			"triage-bot": {
				Token: "gst_wld_triage-bot-token",
				Providers: map[string]config.WorkloadProviderDef{
					"clickhouse": {
						Allow: []string{"run_query"},
					},
				},
			},
		},
	}, providers)

	broker := invocation.NewBroker(providers, ds.Users, ds.Tokens, invocation.WithAuthorizer(authz))
	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:    broker,
		Providers:  providers,
		Authorizer: authz,
	})

	result := callToolForSession(t, srv, ctxWithWorkloadPrincipal("triage-bot"), newTestSessionWithTools(), "clickhouse_delete_table", map[string]any{"table": "users"})
	if !result.IsError {
		t.Fatalf("expected MCP error result, got %+v", result)
	}
	text, ok := result.Content[0].(mcpgo.TextContent)
	if !ok || text.Text != "operation access denied" {
		t.Fatalf("unexpected MCP error content: %+v", result.Content)
	}
	if called {
		t.Fatal("expected denied tool call to stop before provider execution")
	}
	if sessionCatalogCalls != 1 {
		t.Fatalf("session catalog calls = %d, want %d", sessionCatalogCalls, 1)
	}
}

func TestNewServer_WorkloadCallToolDeniedForUnboundSessionOnlyProvider(t *testing.T) {
	t.Parallel()

	var sessionCatalogCalls int
	var called bool
	prov := &directCallerProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "clickhouse",
			ConnMode: core.ConnectionModeNone,
		},
		sessionCatalogFn: func(_ context.Context, _ string) (*catalog.Catalog, error) {
			sessionCatalogCalls++
			return &catalog.Catalog{
				Name: "clickhouse",
				Operations: []catalog.CatalogOperation{
					{
						ID:          "run_query",
						Description: "run a query",
						Transport:   catalog.TransportMCPPassthrough,
					},
				},
			}, nil
		},
		callFn: func(_ context.Context, _ string, _ map[string]any) (*mcpgo.CallToolResult, error) {
			called = true
			return mcpgo.NewToolResultText("unexpected provider call"), nil
		},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	ds := coretesting.NewStubServices(t)
	authz := mustAuthorizer(t, config.AuthorizationConfig{
		Workloads: map[string]config.WorkloadDef{
			"triage-bot": {
				Token: "gst_wld_triage-bot-token",
			},
		},
	}, providers)

	broker := invocation.NewBroker(providers, ds.Users, ds.Tokens, invocation.WithAuthorizer(authz))
	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:    broker,
		Providers:  providers,
		Authorizer: authz,
	})

	session := newTestSessionWithTools()
	result := callToolForSession(t, srv, ctxWithWorkloadPrincipal("triage-bot"), session, "clickhouse_run_query", map[string]any{"sql": "select 1"})
	if !result.IsError {
		t.Fatalf("expected MCP error result, got %+v", result)
	}
	text, ok := result.Content[0].(mcpgo.TextContent)
	if !ok || text.Text != "operation access denied" {
		t.Fatalf("unexpected MCP error content: %+v", result.Content)
	}
	if called {
		t.Fatal("expected denied tool call to stop before provider execution")
	}
	if sessionCatalogCalls != 1 {
		t.Fatalf("session catalog calls = %d, want %d", sessionCatalogCalls, 1)
	}
	if _, ok := session.GetSessionTools()["clickhouse_run_query"]; !ok {
		t.Fatal("expected hidden session-only tool to remain registered for call-time authorization")
	}
}

func TestNewServer_WorkloadCallToolUsesBoundConnectionForSessionOnlyProvider(t *testing.T) {
	t.Parallel()

	var sessionCatalogCalls int
	var called bool
	prov := &directCallerProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "clickhouse",
			ConnMode: core.ConnectionModeIdentity,
		},
		sessionCatalogFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
			sessionCatalogCalls++
			if token != "identity-token" {
				t.Fatalf("session catalog token = %q, want %q", token, "identity-token")
			}
			return &catalog.Catalog{
				Name: "clickhouse",
				Operations: []catalog.CatalogOperation{
					{
						ID:          "run_query",
						Description: "run a query",
						Transport:   catalog.TransportMCPPassthrough,
					},
				},
			}, nil
		},
		callFn: func(ctx context.Context, name string, args map[string]any) (*mcpgo.CallToolResult, error) {
			called = true
			if name != "run_query" {
				t.Fatalf("name = %q, want %q", name, "run_query")
			}
			if token := mcpupstream.UpstreamTokenFromContext(ctx); token != "identity-token" {
				t.Fatalf("upstream token = %q, want %q", token, "identity-token")
			}
			if sql, _ := args["sql"].(string); sql != "select 1" {
				t.Fatalf("sql = %q, want %q", sql, "select 1")
			}
			return mcpgo.NewToolResultText("ok"), nil
		},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	ds := coretesting.NewStubServices(t)
	ctx := context.Background()
	if err := ds.Tokens.StoreToken(ctx, &core.IntegrationToken{
		ID:          "tok-identity",
		SubjectID:   principal.IdentitySubjectID(),
		Integration: "clickhouse",
		Connection:  "workspace",
		Instance:    "team-a",
		AccessToken: "identity-token",
	}); err != nil {
		t.Fatalf("StoreToken: %v", err)
	}

	authz := mustAuthorizer(t, config.AuthorizationConfig{
		Workloads: map[string]config.WorkloadDef{
			"triage-bot": {
				Token: "gst_wld_triage-bot-token",
				Providers: map[string]config.WorkloadProviderDef{
					"clickhouse": {
						Connection: "workspace",
						Instance:   "team-a",
						Allow:      []string{"run_query"},
					},
				},
			},
		},
	}, providers)

	broker := invocation.NewBroker(providers, ds.Users, ds.Tokens, invocation.WithAuthorizer(authz))
	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:       broker,
		TokenResolver: broker,
		Providers:     providers,
		Authorizer:    authz,
		MCPConnection: map[string]string{"clickhouse": "default"},
	})

	result := callToolForSession(t, srv, ctxWithWorkloadPrincipal("triage-bot"), newTestSessionWithTools(), "clickhouse_run_query", map[string]any{"sql": "select 1"})
	if result.IsError {
		t.Fatalf("expected success result, got %+v", result)
	}
	text, ok := result.Content[0].(mcpgo.TextContent)
	if !ok || text.Text != "ok" {
		t.Fatalf("unexpected MCP success content: %+v", result.Content)
	}
	if !called {
		t.Fatal("expected workload tool call to reach provider")
	}
	if sessionCatalogCalls != 1 {
		t.Fatalf("session catalog calls = %d, want %d", sessionCatalogCalls, 1)
	}
}

func TestNewServer_WorkloadCallToolRejectsInstanceOverride(t *testing.T) {
	t.Parallel()

	var sessionCatalogCalls int
	var called bool
	prov := &directCallerProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "sampledb",
			ConnMode: core.ConnectionModeIdentity,
		},
		cat: &catalog.Catalog{
			Name: "sampledb",
			Operations: []catalog.CatalogOperation{
				{
					ID:          "run_query",
					Description: "run a query",
					Transport:   catalog.TransportMCPPassthrough,
				},
			},
		},
		sessionCatalogFn: func(_ context.Context, _ string) (*catalog.Catalog, error) {
			sessionCatalogCalls++
			return &catalog.Catalog{
				Name: "sampledb",
				Operations: []catalog.CatalogOperation{
					{
						ID:          "run_query",
						Description: "run a query",
						Transport:   catalog.TransportMCPPassthrough,
					},
				},
			}, nil
		},
		callFn: func(context.Context, string, map[string]any) (*mcpgo.CallToolResult, error) {
			called = true
			return mcpgo.NewToolResultText("unexpected provider call"), nil
		},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	ds := coretesting.NewStubServices(t)
	ctx := context.Background()
	if err := ds.Tokens.StoreToken(ctx, &core.IntegrationToken{
		ID:          "tok-identity",
		SubjectID:   principal.IdentitySubjectID(),
		Integration: "sampledb",
		Connection:  "workspace",
		Instance:    "team-a",
		AccessToken: "identity-token",
	}); err != nil {
		t.Fatalf("StoreToken: %v", err)
	}

	authz := mustAuthorizer(t, config.AuthorizationConfig{
		Workloads: map[string]config.WorkloadDef{
			"triage-bot": {
				Token: "gst_wld_triage-bot-token",
				Providers: map[string]config.WorkloadProviderDef{
					"sampledb": {
						Connection: "workspace",
						Instance:   "team-a",
						Allow:      []string{"run_query"},
					},
				},
			},
		},
	}, providers)

	broker := invocation.NewBroker(providers, ds.Users, ds.Tokens, invocation.WithAuthorizer(authz))
	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:       broker,
		TokenResolver: broker,
		Providers:     providers,
		Authorizer:    authz,
		MCPConnection: map[string]string{"sampledb": "default"},
	})

	result := callToolForSession(t, srv, ctxWithWorkloadPrincipal("triage-bot"), newTestSessionWithTools(), "sampledb_run_query", map[string]any{
		"sql":       "select 1",
		"_instance": "team-b",
	})
	if !result.IsError {
		t.Fatalf("expected MCP error result, got %+v", result)
	}
	text, ok := result.Content[0].(mcpgo.TextContent)
	if !ok || text.Text != "workload callers may not override connection or instance bindings" {
		t.Fatalf("unexpected MCP error content: %+v", result.Content)
	}
	if called {
		t.Fatal("expected workload override to stop before provider execution")
	}
	if sessionCatalogCalls != 0 {
		t.Fatalf("session catalog calls = %d, want %d", sessionCatalogCalls, 0)
	}
}

func TestNewServer_HumanCallToolUsesInstanceMetadataForStaticCollisions(t *testing.T) {
	t.Parallel()

	var sessionCatalogCalls int
	var called bool
	prov := &directCallerProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "sampledb",
			ConnMode: core.ConnectionModeUser,
		},
		cat: &catalog.Catalog{
			Name: "sampledb",
			Operations: []catalog.CatalogOperation{
				{
					ID:           "run_query",
					Description:  "static query",
					Transport:    catalog.TransportMCPPassthrough,
					AllowedRoles: []string{"viewer"},
				},
			},
		},
		sessionCatalogFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
			sessionCatalogCalls++
			if token != "team-b-token" {
				t.Fatalf("session catalog token = %q, want %q", token, "team-b-token")
			}
			return &catalog.Catalog{
				Name: "sampledb",
				Operations: []catalog.CatalogOperation{
					{
						ID:           "run_query",
						Description:  "instance query",
						Transport:    catalog.TransportMCPPassthrough,
						AllowedRoles: []string{"admin"},
					},
				},
			}, nil
		},
		callFn: func(context.Context, string, map[string]any) (*mcpgo.CallToolResult, error) {
			called = true
			return mcpgo.NewToolResultText("unexpected provider call"), nil
		},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	ds := coretesting.NewStubServices(t)
	const userID = "viewer-user"
	if err := ds.Tokens.StoreToken(context.Background(), &core.IntegrationToken{
		ID:          "tok-team-b",
		SubjectID:   principal.UserSubjectID(userID),
		Integration: "sampledb",
		Connection:  "workspace",
		Instance:    "team-b",
		AccessToken: "team-b-token",
	}); err != nil {
		t.Fatalf("StoreToken: %v", err)
	}
	authz, err := authorization.New(config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.HumanPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(userID), Role: "viewer"},
				},
			},
		},
	}, map[string]*config.ProviderEntry{
		"sampledb": {AuthorizationPolicy: "sample_policy"},
	}, providers, map[string]string{})
	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}
	broker := invocation.NewBroker(providers, ds.Users, ds.Tokens, invocation.WithAuthorizer(authz))

	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:       broker,
		TokenResolver: broker,
		Providers:     providers,
		Authorizer:    authz,
		MCPConnection: map[string]string{"sampledb": "workspace"},
	})

	result := callToolForSession(t, srv, ctxWithPrincipal(userID), newTestSessionWithTools(), "sampledb_run_query", map[string]any{
		"sql":       "select 1",
		"_instance": "team-b",
	})
	if !result.IsError {
		t.Fatalf("expected MCP error result, got %+v", result)
	}
	text, ok := result.Content[0].(mcpgo.TextContent)
	if !ok || text.Text != "operation access denied" {
		t.Fatalf("unexpected MCP error content: %+v", result.Content)
	}
	if called {
		t.Fatal("expected stricter instance metadata to stop provider execution")
	}
	if sessionCatalogCalls != 1 {
		t.Fatalf("session catalog calls = %d, want %d", sessionCatalogCalls, 1)
	}
}

func TestNewServer_NoneModeInstanceHydrationUsesRequestedInstance(t *testing.T) {
	t.Parallel()

	var sessionCatalogCalls int
	var called bool
	prov := &directCallerProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "sampledb",
			ConnMode: core.ConnectionModeNone,
		},
		cat: &catalog.Catalog{
			Name: "sampledb",
			Operations: []catalog.CatalogOperation{
				{
					ID:          "run_query",
					Description: "static query",
					Transport:   catalog.TransportMCPPassthrough,
				},
			},
		},
		sessionCatalogFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
			sessionCatalogCalls++
			if token != "team-b-token" {
				t.Fatalf("session catalog token = %q, want %q", token, "team-b-token")
			}
			return &catalog.Catalog{
				Name: "sampledb",
				Operations: []catalog.CatalogOperation{
					{
						ID:          "run_query",
						Description: "instance query",
						Transport:   catalog.TransportMCPPassthrough,
					},
				},
			}, nil
		},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker: &testutil.StubInvoker{
			InvokeFn: func(context.Context, *principal.Principal, string, string, string, map[string]any) (*core.OperationResult, error) {
				called = true
				return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
			},
		},
		TokenResolver: &stubTokenResolver{
			resolveFn: func(ctx context.Context, p *principal.Principal, providerName, connection, instance string) (context.Context, string, error) {
				if providerName != "sampledb" {
					t.Fatalf("providerName = %q, want %q", providerName, "sampledb")
				}
				if connection != "" {
					t.Fatalf("connection = %q, want empty connection", connection)
				}
				if instance != "team-b" {
					t.Fatalf("instance = %q, want %q", instance, "team-b")
				}
				return ctx, "team-b-token", nil
			},
		},
		Providers: providers,
	})

	result := callToolForSession(t, srv, ctxWithPrincipal("viewer-user"), newTestSessionWithTools(), "sampledb_run_query", map[string]any{
		"sql":       "select 1",
		"_instance": "team-b",
	})
	if result.IsError {
		t.Fatalf("expected success result, got %+v", result)
	}
	if !called {
		t.Fatal("expected instance-specific none-mode call to reach invoker")
	}
	if sessionCatalogCalls != 1 {
		t.Fatalf("session catalog calls = %d, want %d", sessionCatalogCalls, 1)
	}
}

func TestNewServer_InstanceCallFailsClosedWhenHydrationFailsOnStaticCollision(t *testing.T) {
	t.Parallel()

	var called bool
	prov := &directCallerProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "sampledb",
			ConnMode: core.ConnectionModeNone,
		},
		cat: &catalog.Catalog{
			Name: "sampledb",
			Operations: []catalog.CatalogOperation{
				{
					ID:          "run_query",
					Description: "static query",
					Transport:   catalog.TransportMCPPassthrough,
				},
			},
		},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker: &testutil.StubInvoker{
			InvokeFn: func(context.Context, *principal.Principal, string, string, string, map[string]any) (*core.OperationResult, error) {
				called = true
				return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
			},
		},
		TokenResolver: &stubTokenResolver{
			resolveFn: func(context.Context, *principal.Principal, string, string, string) (context.Context, string, error) {
				return context.Background(), "", fmt.Errorf("token unavailable")
			},
		},
		Providers: providers,
	})

	result := callToolForSession(t, srv, ctxWithPrincipal("viewer-user"), newTestSessionWithTools(), "sampledb_run_query", map[string]any{
		"sql":       "select 1",
		"_instance": "team-b",
	})
	if !result.IsError {
		t.Fatalf("expected error result, got %+v", result)
	}
	text, ok := result.Content[0].(mcpgo.TextContent)
	if !ok || text.Text != "requested instance is unavailable for this tool" {
		t.Fatalf("unexpected error content: %+v", result.Content)
	}
	if called {
		t.Fatal("expected failed hydration to stop before invoker execution")
	}
}

func TestNewServer_DefaultInstanceCallFailsClosedWhenHydrationFailsOnStaticCollision(t *testing.T) {
	t.Parallel()

	var called bool
	prov := &directCallerProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "sampledb",
			ConnMode: core.ConnectionModeNone,
		},
		cat: &catalog.Catalog{
			Name: "sampledb",
			Operations: []catalog.CatalogOperation{
				{
					ID:          "run_query",
					Description: "static query",
					Transport:   catalog.TransportMCPPassthrough,
				},
			},
		},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker: &testutil.StubInvoker{
			InvokeFn: func(context.Context, *principal.Principal, string, string, string, map[string]any) (*core.OperationResult, error) {
				called = true
				return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
			},
		},
		TokenResolver: &stubTokenResolver{
			resolveFn: func(context.Context, *principal.Principal, string, string, string) (context.Context, string, error) {
				return context.Background(), "", fmt.Errorf("token unavailable")
			},
		},
		Providers: providers,
	})

	result := callToolForSession(t, srv, ctxWithPrincipal("viewer-user"), newTestSessionWithTools(), "sampledb_run_query", map[string]any{
		"sql": "select 1",
	})
	if !result.IsError {
		t.Fatalf("expected error result, got %+v", result)
	}
	text, ok := result.Content[0].(mcpgo.TextContent)
	if !ok || text.Text != "requested instance is unavailable for this tool" {
		t.Fatalf("unexpected error content: %+v", result.Content)
	}
	if called {
		t.Fatal("expected failed default hydration to stop before invoker execution")
	}
}

func TestNewServer_DefaultHydrationFailureHidesStaticCollisionWithoutAuthorizer(t *testing.T) {
	t.Parallel()

	prov := &directCallerProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "sampledb",
			ConnMode: core.ConnectionModeUser,
		},
		cat: &catalog.Catalog{
			Name: "sampledb",
			Operations: []catalog.CatalogOperation{
				{
					ID:          "run_query",
					Description: "static query",
					Transport:   catalog.TransportMCPPassthrough,
				},
			},
		},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker: &testutil.StubInvoker{},
		TokenResolver: &stubTokenResolver{
			resolveFn: func(context.Context, *principal.Principal, string, string, string) (context.Context, string, error) {
				return context.Background(), "", fmt.Errorf("token unavailable")
			},
		},
		Providers: providers,
	})

	result := listToolsForSession(t, srv, ctxWithPrincipal("viewer-user"), newTestSessionWithTools())
	if len(result.Tools) != 0 {
		t.Fatalf("tool names = %+v, want no visible tools", result.Tools)
	}
}

func TestNewServer_SessionHydratedRESTToolUsesHydrationConnection(t *testing.T) {
	t.Parallel()

	var seenToken string
	prov := &directCallerProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "sampledb",
			ConnMode: core.ConnectionModeUser,
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, token string) (*core.OperationResult, error) {
				seenToken = token
				return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
			},
		},
		sessionCatalogFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
			switch token {
			case "catalog-token":
				return &catalog.Catalog{
					Name: "sampledb",
					Operations: []catalog.CatalogOperation{
						{ID: "run_query", Description: "run a query", Method: http.MethodGet, Transport: catalog.TransportREST},
					},
				}, nil
			case "rest-token":
				return &catalog.Catalog{Name: "sampledb"}, nil
			default:
				return nil, fmt.Errorf("unexpected token %q", token)
			}
		},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	ds := coretesting.NewStubServices(t)
	const userID = "viewer-user"
	if err := ds.Tokens.StoreToken(context.Background(), &core.IntegrationToken{
		ID:          "tok-catalog",
		SubjectID:   principal.UserSubjectID(userID),
		Integration: "sampledb",
		Connection:  "catalog-conn",
		Instance:    "default",
		AccessToken: "catalog-token",
	}); err != nil {
		t.Fatalf("StoreToken catalog: %v", err)
	}
	if err := ds.Tokens.StoreToken(context.Background(), &core.IntegrationToken{
		ID:          "tok-rest",
		SubjectID:   principal.UserSubjectID(userID),
		Integration: "sampledb",
		Connection:  "rest-conn",
		Instance:    "default",
		AccessToken: "rest-token",
	}); err != nil {
		t.Fatalf("StoreToken rest: %v", err)
	}

	broker := invocation.NewBroker(
		providers,
		ds.Users,
		ds.Tokens,
		invocation.WithConnectionMapper(invocation.ConnectionMap(map[string]string{"sampledb": "rest-conn"})),
		invocation.WithMCPConnectionMapper(invocation.ConnectionMap(map[string]string{"sampledb": "catalog-conn"})),
	)
	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:       broker,
		TokenResolver: broker,
		Providers:     providers,
		MCPConnection: map[string]string{"sampledb": "catalog-conn"},
	})

	result := callToolForSession(t, srv, ctxWithPrincipal(userID), newTestSessionWithTools(), "sampledb_run_query", map[string]any{
		"sql": "select 1",
	})
	if result.IsError {
		t.Fatalf("expected success result, got %+v", result)
	}
	if seenToken != "catalog-token" {
		t.Fatalf("execute token = %q, want %q", seenToken, "catalog-token")
	}

	t.Run("broker resolution uses session metadata before static collision", func(t *testing.T) {
		t.Parallel()

		seenToken = ""
		usedDirectTool := false
		collisionProv := &directCallerProvider{
			StubIntegration: coretesting.StubIntegration{
				N:        "sampledb",
				ConnMode: core.ConnectionModeUser,
				ExecuteFn: func(_ context.Context, _ string, _ map[string]any, token string) (*core.OperationResult, error) {
					seenToken = token
					return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
				},
			},
			cat: &catalog.Catalog{
				Name: "sampledb",
				Operations: []catalog.CatalogOperation{
					{ID: "run_query", Description: "static query", Transport: catalog.TransportMCPPassthrough},
				},
			},
			sessionCatalogFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
				switch token {
				case "catalog-token":
					return &catalog.Catalog{
						Name: "sampledb",
						Operations: []catalog.CatalogOperation{
							{ID: "run_query", Description: "session query", Method: http.MethodGet, Transport: catalog.TransportREST},
						},
					}, nil
				case "rest-token":
					return &catalog.Catalog{Name: "sampledb"}, nil
				default:
					return nil, fmt.Errorf("unexpected token %q", token)
				}
			},
			callFn: func(_ context.Context, _ string, _ map[string]any) (*mcpgo.CallToolResult, error) {
				usedDirectTool = true
				return mcpgo.NewToolResultText("static"), nil
			},
		}

		collisionProviders := testutil.NewProviderRegistry(t, collisionProv)
		collisionBroker := invocation.NewBroker(
			collisionProviders,
			ds.Users,
			ds.Tokens,
			invocation.WithConnectionMapper(invocation.ConnectionMap(map[string]string{"sampledb": "rest-conn"})),
			invocation.WithMCPConnectionMapper(invocation.ConnectionMap(map[string]string{"sampledb": "catalog-conn"})),
		)
		collisionServer := gestaltmcp.NewServer(gestaltmcp.Config{
			Invoker:       collisionBroker,
			TokenResolver: collisionBroker,
			Providers:     collisionProviders,
			MCPConnection: map[string]string{"sampledb": "catalog-conn"},
		})

		result := callToolForSession(t, collisionServer, ctxWithPrincipal(userID), newTestSessionWithTools(), "sampledb_run_query", map[string]any{
			"sql": "select 1",
		})
		if result.IsError {
			t.Fatalf("expected success result, got %+v", result)
		}
		if usedDirectTool {
			t.Fatal("expected session REST metadata to beat static MCP collision")
		}
		if seenToken != "catalog-token" {
			t.Fatalf("execute token = %q, want %q", seenToken, "catalog-token")
		}
	})
}

func TestNewServer_HumanCallToolUsesNormalizedRequestedInstanceWithoutOverwritingDefaultTools(t *testing.T) {
	t.Parallel()

	var sessionCatalogCalls int
	prov := &directCallerProvider{
		StubIntegration: coretesting.StubIntegration{
			N:        "sampledb",
			ConnMode: core.ConnectionModeUser,
		},
		sessionCatalogFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
			sessionCatalogCalls++
			description := "default query"
			switch token {
			case "default-token":
			case "team-b-token":
				description = "team-b query"
			default:
				t.Fatalf("session catalog token = %q, want one of default-token or team-b-token", token)
			}
			return &catalog.Catalog{
				Name: "sampledb",
				Operations: []catalog.CatalogOperation{
					{
						ID:           "run_query",
						Description:  description,
						Transport:    catalog.TransportMCPPassthrough,
						AllowedRoles: []string{"viewer"},
					},
				},
			}, nil
		},
		callFn: func(_ context.Context, _ string, _ map[string]any) (*mcpgo.CallToolResult, error) {
			return mcpgo.NewToolResultText("ok"), nil
		},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	const userID = "viewer-user"
	authz, err := authorization.New(config.AuthorizationConfig{
		Policies: map[string]config.HumanPolicyDef{
			"sample_policy": {
				Default: "deny",
				Members: []config.HumanPolicyMemberDef{
					{SubjectID: principal.UserSubjectID(userID), Role: "viewer"},
				},
			},
		},
	}, map[string]*config.ProviderEntry{
		"sampledb": {AuthorizationPolicy: "sample_policy"},
	}, providers, map[string]string{})
	if err != nil {
		t.Fatalf("authorization.New: %v", err)
	}

	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker: &testutil.StubInvoker{
			InvokeFn: func(_ context.Context, _ *principal.Principal, providerName, instance, operation string, args map[string]any) (*core.OperationResult, error) {
				if providerName != "sampledb" {
					t.Fatalf("providerName = %q, want %q", providerName, "sampledb")
				}
				if instance != "team-b" {
					t.Fatalf("instance = %q, want %q", instance, "team-b")
				}
				if operation != "run_query" {
					t.Fatalf("operation = %q, want %q", operation, "run_query")
				}
				if _, ok := args["_instance"]; ok {
					t.Fatalf("unexpected _instance in args: %#v", args)
				}
				return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
			},
		},
		TokenResolver: &stubTokenResolver{
			resolveFn: func(ctx context.Context, p *principal.Principal, providerName, connection, instance string) (context.Context, string, error) {
				if providerName != "sampledb" {
					t.Fatalf("providerName = %q, want %q", providerName, "sampledb")
				}
				if connection != "workspace" {
					t.Fatalf("connection = %q, want %q", connection, "workspace")
				}
				if p == nil || p.UserID != userID {
					t.Fatalf("principal = %+v, want userID %q", p, userID)
				}
				switch instance {
				case "":
					return ctx, "default-token", nil
				case "team-b":
					return ctx, "team-b-token", nil
				default:
					return ctx, "", fmt.Errorf("unexpected instance %q", instance)
				}
			},
		},
		Providers:     providers,
		Authorizer:    authz,
		MCPConnection: map[string]string{"sampledb": "workspace"},
	})

	session := newTestSessionWithTools()
	first := listToolsForSession(t, srv, ctxWithPrincipal(userID), session)
	if len(first.Tools) != 1 || first.Tools[0].Name != "sampledb_run_query" {
		t.Fatalf("default tools = %+v, want only sampledb_run_query", first.Tools)
	}
	if first.Tools[0].Description != "default query" {
		t.Fatalf("default tool description = %q, want %q", first.Tools[0].Description, "default query")
	}

	result := callToolForSession(t, srv, ctxWithPrincipal(userID), session, "sampledb_run_query", map[string]any{
		"sql":       "select 1",
		"_instance": " team-b ",
	})
	if result.IsError {
		t.Fatalf("expected success result, got %+v", result)
	}
	second := listToolsForSession(t, srv, ctxWithPrincipal(userID), session)
	if len(second.Tools) != 1 || second.Tools[0].Name != "sampledb_run_query" {
		t.Fatalf("default tools after instance call = %+v, want only sampledb_run_query", second.Tools)
	}
	if second.Tools[0].Description != "default query" {
		t.Fatalf("default tool description after instance call = %q, want %q", second.Tools[0].Description, "default query")
	}
	third := callToolForSession(t, srv, ctxWithPrincipal(userID), session, "sampledb_run_query", map[string]any{
		"sql":       "select 1",
		"_instance": "team-b",
	})
	if third.IsError {
		t.Fatalf("expected second instance call to succeed, got %+v", third)
	}
	if sessionCatalogCalls != 3 {
		t.Fatalf("session catalog calls = %d, want %d", sessionCatalogCalls, 3)
	}
}

func TestNewServer_DynamicCatalogProviderDoesNotRehydrateAfterExactCollisionOnly(t *testing.T) {
	t.Parallel()

	var sessionCatalogCalls int
	prov := &directCallerProvider{
		StubIntegration: coretesting.StubIntegration{N: "clickhouse", ConnMode: core.ConnectionModeNone},
		cat: &catalog.Catalog{
			Name: "clickhouse",
			Operations: []catalog.CatalogOperation{
				{
					ID:          "run_query",
					Description: "static query",
					Transport:   catalog.TransportMCPPassthrough,
				},
			},
		},
		sessionCatalogFn: func(_ context.Context, _ string) (*catalog.Catalog, error) {
			sessionCatalogCalls++
			return &catalog.Catalog{
				Name: "clickhouse",
				Operations: []catalog.CatalogOperation{
					{
						ID:          "run_query",
						Description: "session query",
						Transport:   catalog.TransportMCPPassthrough,
					},
				},
			}, nil
		},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:   &testutil.StubInvoker{},
		Providers: providers,
	})

	session := newTestSessionWithTools()
	ctx := ctxWithPrincipal("stub-user-id")

	first := listToolsForSession(t, srv, ctx, session)
	second := listToolsForSession(t, srv, ctx, session)

	if sessionCatalogCalls != 1 {
		t.Fatalf("session catalog calls = %d, want %d", sessionCatalogCalls, 1)
	}
	if len(first.Tools) != 1 || first.Tools[0].Name != "clickhouse_run_query" {
		t.Fatalf("first tools = %+v, want only clickhouse_run_query", first.Tools)
	}
	if len(second.Tools) != 1 || second.Tools[0].Name != "clickhouse_run_query" {
		t.Fatalf("second tools = %+v, want only clickhouse_run_query", second.Tools)
	}
	tools := session.GetSessionTools()
	if len(tools) != 2 {
		t.Fatalf("session tools = %+v, want hydration and metadata markers after exact collision hydration", tools)
	}
	if _, ok := tools[hydrationMarkerToolName("clickhouse")]; !ok {
		t.Fatalf("session tools = %+v, want hydration marker after exact collision hydration", tools)
	}
	if _, ok := tools[sessionCatalogOperationMarkerToolName("clickhouse", "run_query")]; !ok {
		t.Fatalf("session tools = %+v, want session catalog marker after exact collision hydration", tools)
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
	ds, userID := stubServicesWithToken(t, "hybrid")
	ctx := context.Background()
	_ = ds.Tokens.StoreToken(ctx, &core.IntegrationToken{
		ID: "tok-plugin", SubjectID: principal.UserSubjectID(userID), Integration: "hybrid", Connection: config.PluginConnectionName, Instance: "default",
		AccessToken: testPluginAccessToken,
	})
	_ = ds.Tokens.StoreToken(ctx, &core.IntegrationToken{
		ID: "tok-api", SubjectID: principal.UserSubjectID(userID), Integration: "hybrid", Connection: testAPIConnectionName, Instance: "default",
		AccessToken: testNamedAPIAccessToken,
	})
	broker := invocation.NewBroker(
		providers,
		ds.Users, ds.Tokens,
		invocation.WithConnectionMapper(invocation.ConnectionMap{"hybrid": testAPIConnectionName}),
	)

	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:   broker,
		Providers: providers,
	})

	callTool := func(name string) string {
		t.Helper()
		tool := srv.GetTool(name)
		if tool == nil {
			t.Fatalf("tool %q not found", name)
		}
		req := mcpgo.CallToolRequest{}
		req.Params.Name = name

		result, err := tool.Handler(ctxWithPrincipal(userID), req)
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

func TestNewServer_DirectCallerRoutedThroughInvoker(t *testing.T) {
	t.Parallel()

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
	}

	invoker := &testutil.StubInvoker{
		Result: &core.OperationResult{Status: 200, Body: `{"ok":true}`},
	}
	providers := testutil.NewProviderRegistry(t, prov)
	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:   invoker,
		Providers: providers,
	})

	tool := srv.GetTool("notion_search")
	if tool == nil {
		t.Fatal("tool not found")
	}

	req := mcpgo.CallToolRequest{}
	req.Params.Name = "notion_search"
	result, err := tool.Handler(ctxWithPrincipal("stub-user-id"), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %v", result.Content)
	}
	if !invoker.Invoked {
		t.Fatal("expected invoker to be called")
	}
	if invoker.Provider != "notion" {
		t.Fatalf("provider = %q, want %q", invoker.Provider, "notion")
	}
	if invoker.Operation != "search" {
		t.Fatalf("operation = %q, want %q", invoker.Operation, "search")
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

func TestNewServer_DirectCallerInvokerReceivesPrincipal(t *testing.T) {
	t.Parallel()

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
	}

	invoker := &testutil.StubInvoker{
		Result: &core.OperationResult{Status: 200, Body: `{"ok":true}`},
	}
	providers := testutil.NewProviderRegistry(t, prov)
	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:   invoker,
		Providers: providers,
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
	if !invoker.Invoked {
		t.Fatal("expected invoker to be called")
	}
	if invoker.LastP == nil {
		t.Fatal("expected principal to be passed to invoker")
	}
}

func TestNewServer_DirectCallerInvokerError(t *testing.T) {
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
		Invoker:   &testutil.StubInvoker{Err: fmt.Errorf("invoke failed")},
		Providers: providers,
	})

	tool := srv.GetTool("ch_op")
	ctx := ctxWithPrincipal("stub-user-id")
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "ch_op"

	result, err := tool.Handler(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError for invoker failure")
	}
}

func TestNewServer_DynamicCatalogProviderListsSessionTools(t *testing.T) {
	t.Parallel()

	var gotToken string
	prov := &directCallerProvider{
		StubIntegration: coretesting.StubIntegration{N: "clickhouse"},
		sessionCatalogFn: func(ctx context.Context, token string) (*catalog.Catalog, error) {
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

	result := listToolsForSession(t, srv, ctxWithPrincipal("stub-user-id"), newTestSessionWithTools())
	if gotToken != "upstream-token" {
		t.Fatalf("expected token upstream-token, got %q", gotToken)
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

	prov := &directCallerProvider{
		StubIntegration: coretesting.StubIntegration{N: "clickhouse"},
		sessionCatalogFn: func(_ context.Context, _ string) (*catalog.Catalog, error) {
			return &catalog.Catalog{
				Name: "clickhouse",
				Operations: []catalog.CatalogOperation{
					{
						ID:          "run_query",
						Description: "Execute a SQL query",
						Transport:   catalog.TransportMCPPassthrough,
						InputSchema: json.RawMessage(`{"type":"object","properties":{"sql":{"type":"string"}}}`),
					},
				},
			}, nil
		},
		callFn: func(_ context.Context, name string, args map[string]any) (*mcpgo.CallToolResult, error) {
			calledName = name
			if args["sql"] != "SELECT 1" {
				t.Fatalf("expected sql argument, got %v", args)
			}
			return mcpgo.NewToolResultText("query result"), nil
		},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	ds, userID := stubServicesWithToken(t, "clickhouse")
	broker := invocation.NewBroker(providers, ds.Users, ds.Tokens)
	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:       broker,
		TokenResolver: broker,
		Providers:     providers,
	})

	result := callToolForSession(t, srv, ctxWithPrincipal(userID), newTestSessionWithTools(), "clickhouse_run_query", map[string]any{"sql": "SELECT 1"})
	if result.IsError {
		t.Fatalf("unexpected tool error: %v", result.Content)
	}
	if calledName != "run_query" {
		t.Fatalf("expected run_query, got %q", calledName)
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
		{"included", true, 3},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cat := &catalog.Catalog{
				Name: "acme",
				Operations: []catalog.CatalogOperation{
					{ID: "api_op", Method: http.MethodGet, Path: "/api", Transport: catalog.TransportREST},
					{ID: "graphql_op", Description: "graphql-backed", Transport: "graphql"},
					{ID: "mcp_op", Description: "passthrough", Transport: catalog.TransportMCPPassthrough},
				},
			}

			prov := &catalogProvider{
				StubIntegration: coretesting.StubIntegration{N: "acme"},
				ops:             coreintegration.OperationsList(cat),
				catalog:         cat,
			}

			providers := testutil.NewProviderRegistry(t, prov)
			ds, _ := stubServicesWithToken(t)
			broker := invocation.NewBroker(providers, ds.Users, ds.Tokens)

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
			if !tc.includeREST && tools["acme_graphql_op"] != nil {
				t.Fatal("expected acme_graphql_op to be excluded when IncludeREST=false")
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
	)

	var gotName string
	var gotArgs map[string]any
	var gotMeta *mcpgo.Meta

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
			return &mcpgo.CallToolResult{
				Content:           []mcpgo.Content{mcpgo.NewTextContent(`{"ok":true}`)},
				StructuredContent: map[string]any{"ok": true},
			}, nil
		},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	dsSvc, userID := stubServicesWithToken(t, providerName)
	broker := invocation.NewBroker(providers, dsSvc.Users, dsSvc.Tokens)
	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:       broker,
		TokenResolver: broker,
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

	ctx := ctxWithPrincipal(userID)
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
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}
	structured, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected structured content map, got %T", result.StructuredContent)
	}
	if structured["ok"] != true {
		t.Fatalf("structured content = %v, want ok=true", structured)
	}
}

func TestNewServer_PassthroughToolPreservesErrorResultStructure(t *testing.T) {
	t.Parallel()

	const (
		providerName  = "svc"
		operationName = "do_thing"
		toolName      = providerName + "_" + operationName
	)

	prov := &directCallerProvider{
		StubIntegration: coretesting.StubIntegration{N: providerName},
		ops:             []core.Operation{{Name: operationName, Description: "A passthrough operation"}},
		cat: &catalog.Catalog{
			Name: providerName,
			Operations: []catalog.CatalogOperation{
				{
					ID:          operationName,
					Description: "A passthrough operation",
					Transport:   catalog.TransportMCPPassthrough,
				},
			},
		},
		callFn: func(_ context.Context, _ string, _ map[string]any) (*mcpgo.CallToolResult, error) {
			return &mcpgo.CallToolResult{
				IsError:           true,
				Content:           []mcpgo.Content{mcpgo.NewTextContent("query failed"), mcpgo.NewTextContent("try again")},
				StructuredContent: map[string]any{"code": "bad_query"},
			}, nil
		},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	dsSvc, userID := stubServicesWithToken(t, providerName)
	broker := invocation.NewBroker(providers, dsSvc.Users, dsSvc.Tokens)
	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:       broker,
		TokenResolver: broker,
		Providers:     providers,
	})

	tool := srv.GetTool(toolName)
	if tool == nil {
		t.Fatalf("tool %q not found", toolName)
	}

	req := mcpgo.CallToolRequest{}
	req.Params.Name = toolName
	result, err := tool.Handler(ctxWithPrincipal(userID), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected MCP error result")
	}
	if len(result.Content) != 2 {
		t.Fatalf("expected 2 content items, got %d", len(result.Content))
	}
	first, ok := mcpgo.AsTextContent(result.Content[0])
	if !ok {
		t.Fatalf("expected first content item to be text, got %T", result.Content[0])
	}
	if first.Text != "query failed" {
		t.Fatalf("first error text = %q, want %q", first.Text, "query failed")
	}
	structured, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected structured content map, got %T", result.StructuredContent)
	}
	if structured["code"] != "bad_query" {
		t.Fatalf("structured content = %v, want code=bad_query", structured)
	}
}

func TestNewServer_PassthroughToolTreatsNilResultAsEmptyJSON(t *testing.T) {
	t.Parallel()

	const (
		providerName  = "svc"
		operationName = "do_thing"
		toolName      = providerName + "_" + operationName
	)

	prov := &directCallerProvider{
		StubIntegration: coretesting.StubIntegration{N: providerName},
		ops:             []core.Operation{{Name: operationName, Description: "A passthrough operation"}},
		cat: &catalog.Catalog{
			Name: providerName,
			Operations: []catalog.CatalogOperation{
				{
					ID:          operationName,
					Description: "A passthrough operation",
					Transport:   catalog.TransportMCPPassthrough,
				},
			},
		},
		callFn: func(_ context.Context, _ string, _ map[string]any) (*mcpgo.CallToolResult, error) {
			return nil, nil
		},
	}

	providers := testutil.NewProviderRegistry(t, prov)
	dsSvc, userID := stubServicesWithToken(t, providerName)
	broker := invocation.NewBroker(providers, dsSvc.Users, dsSvc.Tokens)
	srv := gestaltmcp.NewServer(gestaltmcp.Config{
		Invoker:       broker,
		TokenResolver: broker,
		Providers:     providers,
	})

	tool := srv.GetTool(toolName)
	if tool == nil {
		t.Fatalf("tool %q not found", toolName)
	}

	req := mcpgo.CallToolRequest{}
	req.Params.Name = toolName
	result, err := tool.Handler(ctxWithPrincipal(userID), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil MCP result")
	}
	if result.IsError {
		t.Fatalf("unexpected MCP error result: %v", result.Content)
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}
	text, ok := mcpgo.AsTextContent(result.Content[0])
	if !ok {
		t.Fatalf("expected text content, got %T", result.Content[0])
	}
	if text.Text != "{}" {
		t.Fatalf("text content = %q, want %q", text.Text, "{}")
	}
}

func boolPtr(v bool) *bool { return &v }
