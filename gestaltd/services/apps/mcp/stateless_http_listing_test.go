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

// recordingOperationAccess allows a named set of operations and counts the
// batched calls, so a test can prove tools/list asks once for every tool rather
// than once per tool.
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
		if !r.allowed[query.Operation] {
			results[i] = errors.New("denied")
		}
	}
	return results, nil
}

func listingTestConfig(t *testing.T, access invocation.OperationAccessChecker) Config {
	t.Helper()
	stub := &coretesting.StubIntegration{
		N:        "sampleApp",
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{
			Name: "sampleApp",
			Operations: []catalog.CatalogOperation{
				{ID: "items.list", Method: "GET", Path: "/items"},
				{ID: "items.create", Method: "POST", Path: "/items"},
			},
		},
	}
	return Config{
		Providers:        testutil.NewProviderRegistry(t, stub),
		AllowedProviders: []string{"sampleApp"},
		IncludeREST:      map[string]bool{"sampleApp": true},
		OperationAccess:  access,
	}
}

func listToolNames(t *testing.T, cfg Config, p *principal.Principal) ([]string, map[string]any) {
	t.Helper()
	handler := NewStatelessHTTPHandler(cfg)
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	if p != nil {
		req = req.WithContext(principal.WithPrincipal(req.Context(), p))
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("tools/list status = %d: %s", rec.Code, rec.Body.String())
	}

	var envelope struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
		Error map[string]any `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode tools/list: %v", err)
	}
	names := make([]string, 0, len(envelope.Result.Tools))
	for _, tool := range envelope.Result.Tools {
		names = append(names, tool.Name)
	}
	return names, envelope.Error
}

func listingTestPrincipal() *principal.Principal {
	return &principal.Principal{SubjectID: "user:u-1", UserID: "u-1", Kind: principal.KindUser}
}

// TestListToolsFiltersOnAuthorizationWithOneBatchedCall proves tools/list now
// consults the evaluator - a tool the caller cannot call is not listed - and
// that it does so in one batched question for the whole tool set.
func TestListToolsFiltersOnAuthorizationWithOneBatchedCall(t *testing.T) {
	t.Parallel()

	access := &recordingOperationAccess{allowed: map[string]bool{"items.list": true}}
	names, rpcErr := listToolNames(t, listingTestConfig(t, access), listingTestPrincipal())
	if rpcErr != nil {
		t.Fatalf("tools/list error: %v", rpcErr)
	}
	if len(names) != 1 {
		t.Fatalf("tools = %v, want only the allowed tool", names)
	}
	if access.calls != 1 {
		t.Fatalf("batched calls = %d, want 1", access.calls)
	}
	if len(access.queries) != 2 {
		t.Fatalf("batched queries = %d, want 2 (one per tool)", len(access.queries))
	}
}

// TestListToolsListsGrantedToolsForGrantedSubject is the positive half: a
// subject the evaluator allows keeps every tool.
func TestListToolsListsGrantedToolsForGrantedSubject(t *testing.T) {
	t.Parallel()

	access := &recordingOperationAccess{allowed: map[string]bool{"items.list": true, "items.create": true}}
	names, rpcErr := listToolNames(t, listingTestConfig(t, access), listingTestPrincipal())
	if rpcErr != nil {
		t.Fatalf("tools/list error: %v", rpcErr)
	}
	if len(names) != 2 {
		t.Fatalf("tools = %v, want both tools", names)
	}
}

// TestListToolsHidesEverythingForUngrantedSubject covers the ungranted case.
func TestListToolsHidesEverythingForUngrantedSubject(t *testing.T) {
	t.Parallel()

	access := &recordingOperationAccess{allowed: map[string]bool{}}
	names, rpcErr := listToolNames(t, listingTestConfig(t, access), listingTestPrincipal())
	if rpcErr != nil {
		t.Fatalf("tools/list error: %v", rpcErr)
	}
	if len(names) != 0 {
		t.Fatalf("ungranted subject sees tools %v", names)
	}
}

// TestListToolsKeepsScopeAsAnAdditionalFilter proves scope AND authorization:
// an authorization allow does not override a token scope that excludes the app.
func TestListToolsKeepsScopeAsAnAdditionalFilter(t *testing.T) {
	t.Parallel()

	access := &recordingOperationAccess{allowed: map[string]bool{"items.list": true, "items.create": true}}
	scoped := listingTestPrincipal()
	scoped.Scopes = []string{"otherApp"}
	names, rpcErr := listToolNames(t, listingTestConfig(t, access), scoped)
	if rpcErr != nil {
		t.Fatalf("tools/list error: %v", rpcErr)
	}
	if len(names) != 0 {
		t.Fatalf("out-of-scope tools listed: %v", names)
	}
	if access.calls != 0 {
		t.Fatalf("evaluator consulted for out-of-scope app: %d calls", access.calls)
	}
}

// TestListToolsFailsLoudlyWhenEvaluatorUnavailable keeps an unreachable
// evaluator from being reported to the client as "you have no tools".
func TestListToolsFailsLoudlyWhenEvaluatorUnavailable(t *testing.T) {
	t.Parallel()

	access := &recordingOperationAccess{allowed: map[string]bool{}, err: errors.New("evaluator unavailable")}
	names, rpcErr := listToolNames(t, listingTestConfig(t, access), listingTestPrincipal())
	if rpcErr == nil {
		t.Fatalf("evaluator failure returned tools %v instead of an error", names)
	}
	if len(names) != 0 {
		t.Fatalf("tools returned alongside error: %v", names)
	}
}

// TestListToolsWithoutCheckerKeepsCurrentBehavior keeps a deployment with no
// authorization evaluator listing exactly what it listed before.
func TestListToolsWithoutCheckerKeepsCurrentBehavior(t *testing.T) {
	t.Parallel()

	names, rpcErr := listToolNames(t, listingTestConfig(t, nil), listingTestPrincipal())
	if rpcErr != nil {
		t.Fatalf("tools/list error: %v", rpcErr)
	}
	if len(names) != 2 {
		t.Fatalf("tools = %v, want both tools", names)
	}
}
