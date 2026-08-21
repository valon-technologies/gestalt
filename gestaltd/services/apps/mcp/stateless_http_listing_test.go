package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

type recordingOperationAccess struct {
	allowed      map[string]bool
	err          error
	calls        int
	lastBatch    int
	totalQueries int
}

func (r *recordingOperationAccess) CheckOperationAccessMany(
	_ context.Context, _ *principal.Principal, queries []invocation.OperationAccessQuery,
) ([]error, error) {
	r.calls++
	r.lastBatch = len(queries)
	r.totalQueries += len(queries)
	if r.err != nil {
		return nil, r.err
	}
	results := make([]error, len(queries))
	for i, query := range queries {
		if !r.allowed[query.Provider+"."+query.Operation] && !r.allowed[query.Operation] {
			results[i] = errors.New("denied")
		}
	}
	return results, nil
}

type panicCatalogProvider struct {
	coretesting.StubIntegration
}

func (p *panicCatalogProvider) Catalog() *catalog.Catalog {
	panic("catalog must not be read during tools/list")
}

func listingTestConfig(t *testing.T, access invocation.OperationAccessChecker) Config {
	t.Helper()
	return listingConfig(t, access, []catalog.CatalogOperation{
		{ID: "items.list", Title: "List items", Description: "List items", Method: "GET", Path: "/items", InputSchema: json.RawMessage(`{"type":"object","properties":{"limit":{"type":"integer"}}}`)},
		{ID: "items.create", Title: "Create item", Description: "Create an item", Method: "POST", Path: "/items"},
	})
}

func listingConfig(t *testing.T, access invocation.OperationAccessChecker, ops []catalog.CatalogOperation) Config {
	t.Helper()
	stub := &coretesting.StubIntegration{
		N:        "sampleApp",
		DN:       "Sample App",
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{
			Name:       "sampleApp",
			Operations: ops,
		},
		ExecuteFn: func(_ context.Context, op string, _ map[string]any, _ string) (*core.OperationResult, error) {
			return &core.OperationResult{Status: http.StatusOK, Body: []byte(`{"op":"` + op + `"}`)}, nil
		},
	}
	return Config{
		Invoker:          &listingInvoker{stub: stub},
		Providers:        testutil.NewProviderRegistry(t, stub),
		AllowedProviders: []string{"sampleApp"},
		IncludeREST:      map[string]bool{"sampleApp": true},
		OperationAccess:  access,
	}
}

type listingInvoker struct {
	stub *coretesting.StubIntegration
}

func (i *listingInvoker) Invoke(ctx context.Context, _ *principal.Principal, _, _, operation string, params map[string]any) (*core.OperationResult, error) {
	return i.stub.Execute(ctx, operation, params, "")
}

type recordingProviderInvoker struct {
	provider  string
	operation string
}

func (i *recordingProviderInvoker) Invoke(_ context.Context, _ *principal.Principal, provider, _, operation string, _ map[string]any) (*core.OperationResult, error) {
	i.provider = provider
	i.operation = operation
	return &core.OperationResult{Status: http.StatusOK, Body: []byte(`{}`)}, nil
}

func listingTestPrincipal() *principal.Principal {
	return &principal.Principal{SubjectID: "user:u-1", UserID: "u-1", Kind: principal.KindUser}
}

func listToolNames(t *testing.T, cfg Config, p *principal.Principal) ([]string, map[string]any) {
	t.Helper()
	status, envelope := callMCP(t, cfg, p, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/list", "params": map[string]any{},
	})
	return mcpToolNames(t, status, envelope)
}

func callMCP(t *testing.T, cfg Config, p *principal.Principal, payload map[string]any) (int, map[string]any) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal mcp payload: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if p != nil {
		req = req.WithContext(principal.WithPrincipal(req.Context(), p))
	}
	rec := httptest.NewRecorder()
	NewStatelessHTTPHandler(cfg).ServeHTTP(rec, req)
	var envelope map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("decode mcp response: %v body=%s", err, rec.Body.String())
		}
	}
	return rec.Code, envelope
}

func mcpToolNames(t *testing.T, status int, envelope map[string]any) ([]string, map[string]any) {
	t.Helper()
	if status != http.StatusOK {
		t.Fatalf("mcp status = %d: %v", status, envelope)
	}
	rpcErr, _ := envelope["error"].(map[string]any)
	result, _ := envelope["result"].(map[string]any)
	rawTools, _ := result["tools"].([]any)
	names := make([]string, 0, len(rawTools))
	for _, raw := range rawTools {
		tool, _ := raw.(map[string]any)
		if name, ok := tool["name"].(string); ok {
			names = append(names, name)
		}
	}
	return names, rpcErr
}

func callToolJSON(t *testing.T, cfg Config, p *principal.Principal, name string, args map[string]any) map[string]any {
	t.Helper()
	status, envelope := callMCP(t, cfg, p, map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": args},
	})
	if status != http.StatusOK {
		t.Fatalf("tools/call status = %d: %v", status, envelope)
	}
	if rpcErr, ok := envelope["error"]; ok {
		t.Fatalf("tools/call rpc error: %v", rpcErr)
	}
	result, _ := envelope["result"].(map[string]any)
	if result["isError"] == true {
		t.Fatalf("tools/call tool error: %v", result)
	}
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		return result
	}
	block, _ := content[0].(map[string]any)
	text, _ := block["text"].(string)
	if text == "" {
		return result
	}
	var parsed any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return result
	}
	if object, ok := parsed.(map[string]any); ok {
		return object
	}
	return result
}

func callSearch(t *testing.T, cfg Config, p *principal.Principal, args map[string]any) SearchResult {
	t.Helper()
	status, envelope := callMCP(t, cfg, p, map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{"name": SearchToolName, "arguments": args},
	})
	if status != http.StatusOK {
		t.Fatalf("gestalt_search status = %d: %v", status, envelope)
	}
	body, err := DecodeSearchToolResult(envelope)
	if err != nil {
		t.Fatalf("%v", err)
	}
	return body
}

func requireToolError(t *testing.T, cfg Config, p *principal.Principal, name string, args map[string]any) map[string]any {
	t.Helper()
	status, envelope := callMCP(t, cfg, p, map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": args},
	})
	if status != http.StatusOK {
		t.Fatalf("tools/call status = %d: %v", status, envelope)
	}
	if rpcErr, ok := envelope["error"]; ok {
		t.Fatalf("tools/call rpc error: %v", rpcErr)
	}
	result, _ := envelope["result"].(map[string]any)
	if result["isError"] != true {
		t.Fatalf("tools/call returned %v, want a tool error", envelope)
	}
	return result
}

func TestInitializeExplainsWorkspaceFrontDoor(t *testing.T) {
	t.Parallel()

	status, envelope := callMCP(t, listingTestConfig(t, nil), listingTestPrincipal(), map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "test", "version": "1.0"},
		},
	})
	if status != http.StatusOK {
		t.Fatalf("initialize status = %d: %v", status, envelope)
	}
	result, _ := envelope["result"].(map[string]any)
	instructions, _ := result["instructions"].(string)
	for _, name := range []string{SearchToolName, DescribeToolName, InvokeToolName} {
		if !strings.Contains(instructions, name) {
			t.Fatalf("initialize instructions %q missing %s", instructions, name)
		}
	}
}

func TestListToolsReturnsWorkspaceFrontDoor(t *testing.T) {
	t.Parallel()

	access := &recordingOperationAccess{allowed: map[string]bool{}}
	names, rpcErr := listToolNames(t, listingTestConfig(t, access), listingTestPrincipal())
	if rpcErr != nil {
		t.Fatalf("tools/list error: %v", rpcErr)
	}
	if got, want := names, WorkspaceFrontDoorToolNames(); !slices.Equal(got, want) {
		t.Fatalf("tools = %v, want front door %v", got, want)
	}
	if access.calls != 0 {
		t.Fatalf("tools/list consulted the evaluator %d times", access.calls)
	}
}

func TestListToolsDoesNotReadAppCatalogs(t *testing.T) {
	t.Parallel()

	stub := &panicCatalogProvider{StubIntegration: coretesting.StubIntegration{N: "sampleApp"}}
	cfg := Config{
		Providers:        testutil.NewProviderRegistry(t, stub),
		AllowedProviders: []string{"sampleApp"},
		IncludeREST:      map[string]bool{"sampleApp": true},
		OperationAccess:  &recordingOperationAccess{allowed: map[string]bool{}},
	}
	names, rpcErr := listToolNames(t, cfg, listingTestPrincipal())
	if rpcErr != nil {
		t.Fatalf("tools/list error: %v", rpcErr)
	}
	if len(names) != 3 {
		t.Fatalf("tools = %v, want 3 front-door tools", names)
	}
}

func TestListToolsHidesNothingFromUngrantedSubject(t *testing.T) {
	t.Parallel()

	access := &recordingOperationAccess{allowed: map[string]bool{}}
	names, rpcErr := listToolNames(t, listingTestConfig(t, access), listingTestPrincipal())
	if rpcErr != nil {
		t.Fatalf("tools/list error: %v", rpcErr)
	}
	if len(names) != 3 {
		t.Fatalf("ungranted subject saw tools %v, want the front door", names)
	}
}

func TestSearchReturnsGrantedOperations(t *testing.T) {
	t.Parallel()

	access := &recordingOperationAccess{allowed: map[string]bool{"items.list": true}}
	body := callSearch(t, listingTestConfig(t, access), listingTestPrincipal(), map[string]any{
		"query": "items",
	})
	if len(body.Results) != 1 {
		t.Fatalf("search results = %+v, want the granted list operation", body)
	}
	hit := body.Results[0]
	if hit.App != "sampleApp" || hit.Operation != "items.list" {
		t.Fatalf("hit = %+v", hit)
	}
	if access.calls != 1 {
		t.Fatalf("search evaluator calls = %d, want 1", access.calls)
	}
}

func TestSearchHidesUngrantedOperations(t *testing.T) {
	t.Parallel()

	access := &recordingOperationAccess{allowed: map[string]bool{}}
	body := callSearch(t, listingTestConfig(t, access), listingTestPrincipal(), map[string]any{
		"query": "items",
	})
	if len(body.Results) != 0 {
		t.Fatalf("ungranted search returned %+v", body)
	}
}

func TestSearchKeepsScopeAsAnAdditionalFilter(t *testing.T) {
	t.Parallel()

	access := &recordingOperationAccess{allowed: map[string]bool{"items.list": true, "items.create": true}}
	scoped := listingTestPrincipal()
	scoped.Scopes = []string{"otherApp"}
	body := callSearch(t, listingTestConfig(t, access), scoped, map[string]any{
		"query": "items",
	})
	if len(body.Results) != 0 {
		t.Fatalf("out-of-scope search returned %+v", body)
	}
	if access.calls != 0 {
		t.Fatalf("evaluator consulted for out-of-scope app: %d calls", access.calls)
	}
}

func TestSearchFailsLoudlyWhenEvaluatorUnavailable(t *testing.T) {
	t.Parallel()

	access := &recordingOperationAccess{allowed: map[string]bool{}, err: errors.New("evaluator unavailable")}
	status, envelope := callMCP(t, listingTestConfig(t, access), listingTestPrincipal(), map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{"name": SearchToolName, "arguments": map[string]any{"query": "items"}},
	})
	if status != http.StatusOK {
		t.Fatalf("status = %d: %v", status, envelope)
	}
	result, _ := envelope["result"].(map[string]any)
	if result["isError"] != true {
		t.Fatalf("evaluator failure returned %v, want a tool error", envelope)
	}
}

func TestSearchWithoutCheckerReturnsMatchingOperations(t *testing.T) {
	t.Parallel()

	body := callSearch(t, listingTestConfig(t, nil), listingTestPrincipal(), map[string]any{
		"query": "create",
	})
	if len(body.Results) != 1 {
		t.Fatalf("search results = %+v, want create", body)
	}
}

func TestSearchWithoutQueryOrAppReturnsHint(t *testing.T) {
	t.Parallel()

	body := callSearch(t, listingTestConfig(t, nil), listingTestPrincipal(), map[string]any{})
	if len(body.Results) != 0 {
		t.Fatalf("empty search returned %+v", body)
	}
	if body.Hint == "" {
		t.Fatalf("empty search missing hint: %+v", body)
	}
}

func TestSearchOrdersAndLimitsResults(t *testing.T) {
	t.Parallel()

	access := &recordingOperationAccess{allowed: map[string]bool{"items.list": true, "items.create": true}}
	body := callSearch(t, listingTestConfig(t, access), listingTestPrincipal(), map[string]any{
		"query": "items",
		"limit": 1,
	})
	if len(body.Results) != 1 {
		t.Fatalf("search results = %+v, want one hit", body)
	}
	if body.Results[0].Operation != "items.create" {
		t.Fatalf("first limited hit = %+v, want items.create", body.Results[0])
	}
	if !body.Truncated {
		t.Fatalf("limited search missing truncated: %+v", body)
	}
}

func TestSearchAuthorizesAPageNotTheWholeMatchSet(t *testing.T) {
	t.Parallel()

	ops := make([]catalog.CatalogOperation, 0, 40)
	allowed := map[string]bool{}
	for i := 0; i < 40; i++ {
		id := "item_" + strconv.Itoa(i)
		ops = append(ops, catalog.CatalogOperation{ID: id, Title: id, Description: "item op", Method: "GET", Path: "/" + id})
		allowed[id] = true
	}
	access := &recordingOperationAccess{allowed: allowed}
	body := callSearch(t, listingConfig(t, access, ops), listingTestPrincipal(), map[string]any{
		"query": "item",
		"limit": 1,
	})
	if len(body.Results) != 1 || body.Results[0].Operation != "item_0" {
		t.Fatalf("paged search = %+v, want item_0", body)
	}
	if !body.Truncated {
		t.Fatalf("paged search missing truncated: %+v", body)
	}
	if access.totalQueries != defaultSearchLimit {
		t.Fatalf("search authorized %d operations, want a page of %d", access.totalQueries, defaultSearchLimit)
	}
}

func TestSearchWalksDeniedOperationsToFillThePage(t *testing.T) {
	t.Parallel()

	access := &recordingOperationAccess{allowed: map[string]bool{"items.list": true}}
	body := callSearch(t, listingTestConfig(t, access), listingTestPrincipal(), map[string]any{
		"query": "items",
		"limit": 1,
	})
	if len(body.Results) != 1 || body.Results[0].Operation != "items.list" {
		t.Fatalf("search skipped the granted operation: %+v", body)
	}
}

func TestSearchOmitsFlattenedToolNames(t *testing.T) {
	t.Parallel()

	raw := callToolJSON(t, listingTestConfig(t, nil), listingTestPrincipal(), SearchToolName, map[string]any{
		"query": "items.list",
	})
	results, _ := raw["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("search results = %v, want items.list", raw)
	}
	hit, _ := results[0].(map[string]any)
	if _, ok := hit["mcpName"]; ok {
		t.Fatalf("search advertised flattened mcpName: %v", hit)
	}
	if hit["app"] != "sampleApp" || hit["operation"] != "items.list" {
		t.Fatalf("hit = %v, want app and operation", hit)
	}
}

func TestInvokeFrontDoorRunsGrantedOperation(t *testing.T) {
	t.Parallel()

	access := &recordingOperationAccess{allowed: map[string]bool{"items.list": true}}
	body := callToolJSON(t, listingTestConfig(t, access), listingTestPrincipal(), InvokeToolName, map[string]any{
		"app":       "sampleApp",
		"operation": "items.list",
		"arguments": map[string]any{},
	})
	if body["op"] != "items.list" {
		t.Fatalf("invoke result = %v", body)
	}
}

func TestInvokeFrontDoorUsesExplicitAppIdentity(t *testing.T) {
	t.Parallel()

	foo := &coretesting.StubIntegration{
		N:        "foo",
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
			ID: "bar.items", Method: "GET", Path: "/bar-items",
		}}},
	}
	fooBar := &coretesting.StubIntegration{
		N:        "foo_bar",
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{{
			ID: "items", Method: "GET", Path: "/items",
		}}},
	}
	invoker := &recordingProviderInvoker{}
	cfg := Config{
		Invoker:          invoker,
		Providers:        testutil.NewProviderRegistry(t, foo, fooBar),
		AllowedProviders: []string{"foo", "foo_bar"},
		IncludeREST:      map[string]bool{"foo": true, "foo_bar": true},
	}

	callToolJSON(t, cfg, listingTestPrincipal(), InvokeToolName, map[string]any{
		"app":       "foo",
		"operation": "bar.items",
	})
	if invoker.provider != "foo" || invoker.operation != "bar.items" {
		t.Fatalf("invoke target = %s.%s, want foo.bar.items", invoker.provider, invoker.operation)
	}
}

func TestInvokeRejectsOutOfScopeAppBeforeCatalogLookup(t *testing.T) {
	t.Parallel()

	stub := &panicCatalogProvider{StubIntegration: coretesting.StubIntegration{
		N:        "sampleApp",
		ConnMode: core.ConnectionModeNone,
	}}
	cfg := Config{
		Providers:        testutil.NewProviderRegistry(t, stub),
		AllowedProviders: []string{"sampleApp"},
		IncludeREST:      map[string]bool{"sampleApp": true},
	}
	scoped := listingTestPrincipal()
	scoped.Scopes = []string{"otherApp"}

	requireToolError(t, cfg, scoped, InvokeToolName, map[string]any{
		"app":       "sampleApp",
		"operation": "items.list",
	})
}

func TestFlattenedToolNamesReserveFrontDoorNames(t *testing.T) {
	t.Parallel()

	stub := &coretesting.StubIntegration{
		N:        "gestalt",
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{
			{ID: "search", Method: "GET", Path: "/search"},
			{ID: "describe", Method: "GET", Path: "/describe"},
			{ID: "invoke", Method: "GET", Path: "/invoke"},
		}},
		ExecuteFn: func(_ context.Context, op string, _ map[string]any, _ string) (*core.OperationResult, error) {
			return &core.OperationResult{Status: http.StatusOK, Body: []byte(`{"op":"` + op + `"}`)}, nil
		},
	}
	cfg := Config{
		Invoker:          &listingInvoker{stub: stub},
		Providers:        testutil.NewProviderRegistry(t, stub),
		AllowedProviders: []string{"gestalt"},
		IncludeREST:      map[string]bool{"gestalt": true},
	}

	for _, operation := range []string{"search", "describe", "invoke"} {
		name := toolName(nil, "gestalt", operation)
		if name == SearchToolName || name == DescribeToolName || name == InvokeToolName {
			t.Fatalf("flattened %s tool still collides with front door: %q", operation, name)
		}
		body := callToolJSON(t, cfg, listingTestPrincipal(), name, map[string]any{})
		if body["op"] != operation {
			t.Fatalf("flattened %s invoke = %v", operation, body)
		}
	}
}

func TestFlattenedToolNamesStillInvoke(t *testing.T) {
	t.Parallel()

	body := callToolJSON(t, listingTestConfig(t, nil), listingTestPrincipal(), "sampleApp_items_list", map[string]any{})
	if body["op"] != "items.list" {
		t.Fatalf("flattened invoke result = %v", body)
	}
}

func TestDescribeReturnsGrantedOperationSchema(t *testing.T) {
	t.Parallel()

	access := &recordingOperationAccess{allowed: map[string]bool{"items.list": true}}
	body := callToolJSON(t, listingTestConfig(t, access), listingTestPrincipal(), DescribeToolName, map[string]any{
		"app":       "sampleApp",
		"operation": "items.list",
	})
	if body["app"] != "sampleApp" || body["operation"] != "items.list" {
		t.Fatalf("describe = %v", body)
	}
	schema, _ := body["inputSchema"].(map[string]any)
	if schema["type"] != "object" {
		t.Fatalf("describe schema = %v, want the list input schema", body["inputSchema"])
	}
	if _, ok := body["mcpName"]; ok {
		t.Fatalf("describe advertised flattened mcpName: %v", body)
	}
}

func TestFrontDoorToolsAdvertiseCatalogInstance(t *testing.T) {
	t.Parallel()

	status, envelope := callMCP(t, listingTestConfig(t, nil), listingTestPrincipal(), map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/list", "params": map[string]any{},
	})
	if status != http.StatusOK {
		t.Fatalf("tools/list status = %d: %v", status, envelope)
	}
	result, _ := envelope["result"].(map[string]any)
	tools, _ := result["tools"].([]any)
	found := map[string]bool{}
	for _, raw := range tools {
		tool, _ := raw.(map[string]any)
		name, _ := tool["name"].(string)
		schema, _ := tool["inputSchema"].(map[string]any)
		props, _ := schema["properties"].(map[string]any)
		if _, ok := props["_instance"]; !ok {
			t.Fatalf("%s schema missing _instance: %v", name, schema)
		}
		found[name] = true
	}
	for _, name := range WorkspaceFrontDoorToolNames() {
		if !found[name] {
			t.Fatalf("tools/list missing %s", name)
		}
	}
}

type recordingTokenResolver struct {
	lastInstance string
}

func (r *recordingTokenResolver) ResolveToken(ctx context.Context, _ *principal.Principal, _, _, instance string) (context.Context, string, error) {
	r.lastInstance = instance
	return ctx, instance, nil
}

type instanceCatalogProvider struct {
	coretesting.StubIntegration
}

func (p *instanceCatalogProvider) CatalogForRequest(_ context.Context, token string) (*catalog.Catalog, error) {
	id := "op_default"
	if token != "" {
		id = "op_" + token
	}
	return &catalog.Catalog{
		Name: p.N,
		Operations: []catalog.CatalogOperation{{
			ID: id, Title: id, Description: "instance op", Method: "GET", Path: "/" + id,
		}},
	}, nil
}

func instanceListingConfig(t *testing.T, resolver invocation.TokenResolver) Config {
	t.Helper()
	stub := &instanceCatalogProvider{StubIntegration: coretesting.StubIntegration{
		N:        "sampleApp",
		DN:       "Sample App",
		ConnMode: core.ConnectionModeSubject,
		ExecuteFn: func(_ context.Context, op string, _ map[string]any, _ string) (*core.OperationResult, error) {
			return &core.OperationResult{Status: http.StatusOK, Body: []byte(`{"op":"` + op + `"}`)}, nil
		},
	}}
	return Config{
		Invoker:          &listingInvoker{stub: &stub.StubIntegration},
		Providers:        testutil.NewProviderRegistry(t, stub),
		AllowedProviders: []string{"sampleApp"},
		IncludeREST:      map[string]bool{"sampleApp": true},
		TokenResolver:    resolver,
	}
}

func TestDescribeUsesCatalogInstance(t *testing.T) {
	t.Parallel()

	resolver := &recordingTokenResolver{}
	cfg := instanceListingConfig(t, resolver)
	body := callToolJSON(t, cfg, listingTestPrincipal(), DescribeToolName, map[string]any{
		"app":       "sampleApp",
		"operation": "op_team-a",
		"_instance": "team-a",
	})
	if body["operation"] != "op_team-a" {
		t.Fatalf("describe = %v, want op_team-a from instance team-a", body)
	}
	if resolver.lastInstance != "team-a" {
		t.Fatalf("describe resolved instance %q, want team-a", resolver.lastInstance)
	}
	if body["_instance"] != "team-a" {
		t.Fatalf("describe instance = %v, want team-a", body["_instance"])
	}
	requireToolError(t, cfg, listingTestPrincipal(), DescribeToolName, map[string]any{
		"app":       "sampleApp",
		"operation": "op_team-a",
	})
}

func TestSearchUsesCatalogInstance(t *testing.T) {
	t.Parallel()

	resolver := &recordingTokenResolver{}
	cfg := instanceListingConfig(t, resolver)
	body := callSearch(t, cfg, listingTestPrincipal(), map[string]any{
		"app":       "sampleApp",
		"_instance": "team-a",
	})
	if len(body.Results) != 1 || body.Results[0].Operation != "op_team-a" {
		t.Fatalf("search = %+v, want op_team-a", body)
	}
	if resolver.lastInstance != "team-a" {
		t.Fatalf("search resolved instance %q, want team-a", resolver.lastInstance)
	}
	if body.Results[0].CatalogInstance != "team-a" {
		t.Fatalf("search result instance = %q, want team-a", body.Results[0].CatalogInstance)
	}
}

func TestSearchResultCarriesCatalogInstanceIntoInvoke(t *testing.T) {
	t.Parallel()

	resolver := &recordingTokenResolver{}
	cfg := instanceListingConfig(t, resolver)
	search := callSearch(t, cfg, listingTestPrincipal(), map[string]any{
		"app":       "sampleApp",
		"_instance": "team-a",
	})
	if len(search.Results) != 1 {
		t.Fatalf("search = %+v, want one result", search)
	}
	hit := search.Results[0]
	body := callToolJSON(t, cfg, listingTestPrincipal(), InvokeToolName, map[string]any{
		"app":       hit.App,
		"operation": hit.Operation,
		"_instance": hit.CatalogInstance,
	})
	if body["op"] != hit.Operation {
		t.Fatalf("invoke result = %v, want operation %q", body, hit.Operation)
	}
	if resolver.lastInstance != "team-a" {
		t.Fatalf("invoke resolved instance %q, want team-a", resolver.lastInstance)
	}
}

func TestSearchRequiresAppWhenInstanceIsSet(t *testing.T) {
	t.Parallel()

	requireToolError(t, listingTestConfig(t, nil), listingTestPrincipal(), SearchToolName, map[string]any{
		"_instance": "team-a",
	})
}

func TestDescribeDeniesUngrantedOperation(t *testing.T) {
	t.Parallel()

	access := &recordingOperationAccess{allowed: map[string]bool{}}
	requireToolError(t, listingTestConfig(t, access), listingTestPrincipal(), DescribeToolName, map[string]any{
		"app":       "sampleApp",
		"operation": "items.list",
	})
}

func TestDescribeFailsLoudlyWhenEvaluatorUnavailable(t *testing.T) {
	t.Parallel()

	access := &recordingOperationAccess{allowed: map[string]bool{}, err: errors.New("evaluator unavailable")}
	requireToolError(t, listingTestConfig(t, access), listingTestPrincipal(), DescribeToolName, map[string]any{
		"app":       "sampleApp",
		"operation": "items.list",
	})
}
