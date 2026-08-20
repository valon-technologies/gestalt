package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

type recordingOperationAccess struct {
	allowed map[string]bool
	err     error
	calls   int
	queries []invocation.OperationAccessQuery
}

func (r *recordingOperationAccess) CheckOperationAccessMany(
	_ context.Context, _ *principal.Principal, queries []invocation.OperationAccessQuery,
) ([]error, error) {
	r.calls++
	r.queries = append(r.queries, queries...)
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
	stub := &coretesting.StubIntegration{
		N:        "sampleApp",
		DN:       "Sample App",
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{
			Name: "sampleApp",
			Operations: []catalog.CatalogOperation{
				{ID: "items.list", Title: "List items", Description: "List items", Method: "GET", Path: "/items"},
				{ID: "items.create", Title: "Create item", Description: "Create an item", Method: "POST", Path: "/items"},
			},
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

func TestListToolsReturnsWorkspaceFrontDoor(t *testing.T) {
	t.Parallel()

	access := &recordingOperationAccess{allowed: map[string]bool{}}
	names, rpcErr := listToolNames(t, listingTestConfig(t, access), listingTestPrincipal())
	if rpcErr != nil {
		t.Fatalf("tools/list error: %v", rpcErr)
	}
	if got, want := names, []string{DescribeToolName, InvokeToolName, SearchToolName}; !equalStrings(got, want) {
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
	body := callToolJSON(t, listingTestConfig(t, access), listingTestPrincipal(), SearchToolName, map[string]any{
		"query": "items",
	})
	results, _ := body["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("search results = %v, want the granted list operation", body)
	}
	hit, _ := results[0].(map[string]any)
	if hit["app"] != "sampleApp" || hit["operation"] != "items.list" {
		t.Fatalf("hit = %v", hit)
	}
	if access.calls != 1 {
		t.Fatalf("search evaluator calls = %d, want 1", access.calls)
	}
}

func TestSearchHidesUngrantedOperations(t *testing.T) {
	t.Parallel()

	access := &recordingOperationAccess{allowed: map[string]bool{}}
	body := callToolJSON(t, listingTestConfig(t, access), listingTestPrincipal(), SearchToolName, map[string]any{
		"query": "items",
	})
	results, _ := body["results"].([]any)
	if len(results) != 0 {
		t.Fatalf("ungranted search returned %v", body)
	}
}

func TestSearchKeepsScopeAsAnAdditionalFilter(t *testing.T) {
	t.Parallel()

	access := &recordingOperationAccess{allowed: map[string]bool{"items.list": true, "items.create": true}}
	scoped := listingTestPrincipal()
	scoped.Scopes = []string{"otherApp"}
	body := callToolJSON(t, listingTestConfig(t, access), scoped, SearchToolName, map[string]any{
		"query": "items",
	})
	results, _ := body["results"].([]any)
	if len(results) != 0 {
		t.Fatalf("out-of-scope search returned %v", body)
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

	body := callToolJSON(t, listingTestConfig(t, nil), listingTestPrincipal(), SearchToolName, map[string]any{
		"query": "create",
	})
	results, _ := body["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("search results = %v, want create", body)
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
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
