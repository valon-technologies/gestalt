package bigquery

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/valon-technologies/gestalt/core"
)

type stubProvider struct {
	ops      []core.Operation
	execOp   string
	execResp *core.OperationResult
}

func (s *stubProvider) Name() string                        { return "bigquery" }
func (s *stubProvider) DisplayName() string                 { return "BigQuery" }
func (s *stubProvider) Description() string                 { return "stub" }
func (s *stubProvider) ConnectionMode() core.ConnectionMode { return core.ConnectionModeUser }
func (s *stubProvider) ListOperations() []core.Operation    { return s.ops }

func (s *stubProvider) Execute(_ context.Context, operation string, _ map[string]any, _ string) (*core.OperationResult, error) {
	s.execOp = operation
	return s.execResp, nil
}

type stubOAuthProvider struct {
	stubProvider
}

func (s *stubOAuthProvider) AuthorizationURL(string, []string) string { return "https://auth" }
func (s *stubOAuthProvider) ExchangeCode(context.Context, string) (*core.TokenResponse, error) {
	return nil, nil
}
func (s *stubOAuthProvider) RefreshToken(context.Context, string) (*core.TokenResponse, error) {
	return nil, nil
}

func TestWrapProvider_AddsQueryOperation(t *testing.T) {
	t.Parallel()
	inner := &stubProvider{
		ops: []core.Operation{
			{Name: "bigquery.datasets.list", Description: "List datasets"},
		},
	}

	wrapped := wrapProvider(inner, true)
	ops := wrapped.ListOperations()

	if len(ops) != 2 {
		t.Fatalf("expected 2 operations, got %d", len(ops))
	}
	if ops[0].Name != "bigquery.datasets.list" {
		t.Errorf("expected first op to be bigquery.datasets.list, got %s", ops[0].Name)
	}
	if ops[1].Name != operationQuery {
		t.Errorf("expected second op to be %s, got %s", operationQuery, ops[1].Name)
	}
}

func TestWrapProvider_DelegatesToInnerForNonQuery(t *testing.T) {
	t.Parallel()
	inner := &stubProvider{
		execResp: &core.OperationResult{Status: http.StatusOK, Body: `{"datasets":[]}`},
	}

	wrapped := wrapProvider(inner, true)
	result, err := wrapped.Execute(context.Background(), "bigquery.datasets.list", nil, "token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inner.execOp != "bigquery.datasets.list" {
		t.Errorf("inner provider should have received operation %q, got %q", "bigquery.datasets.list", inner.execOp)
	}
	if result.Status != http.StatusOK {
		t.Errorf("expected status 200, got %d", result.Status)
	}
}

func TestWrapProvider_PreservesOAuth(t *testing.T) {
	t.Parallel()
	inner := &stubOAuthProvider{}
	wrapped := wrapProvider(inner, true)

	if _, ok := wrapped.(core.OAuthProvider); !ok {
		t.Error("wrapped provider should implement OAuthProvider when inner does")
	}
}

func TestWrapProvider_NoOAuthWhenInnerLacks(t *testing.T) {
	t.Parallel()
	inner := &stubProvider{}
	wrapped := wrapProvider(inner, true)

	if _, ok := wrapped.(core.OAuthProvider); ok {
		t.Error("wrapped provider should not implement OAuthProvider when inner doesn't")
	}
}

func TestWrapProvider_DelegatesMetadata(t *testing.T) {
	t.Parallel()
	inner := &stubProvider{}
	wrapped := wrapProvider(inner, true)

	if wrapped.Name() != "bigquery" {
		t.Errorf("expected name bigquery, got %s", wrapped.Name())
	}
	if wrapped.DisplayName() != "BigQuery" {
		t.Errorf("expected display name BigQuery, got %s", wrapped.DisplayName())
	}
}

func TestWrapProvider_QueryGatedByFlag(t *testing.T) {
	t.Parallel()
	inner := &stubProvider{
		ops: []core.Operation{
			{Name: "bigquery.datasets.list"},
		},
	}

	withQuery := wrapProvider(inner, true)
	if len(withQuery.ListOperations()) != 2 {
		t.Errorf("expected 2 ops with addQuery=true, got %d", len(withQuery.ListOperations()))
	}

	withoutQuery := wrapProvider(inner, false)
	if len(withoutQuery.ListOperations()) != 1 {
		t.Errorf("expected 1 op with addQuery=false, got %d", len(withoutQuery.ListOperations()))
	}
}

func TestQueryOperation_HasRequiredParams(t *testing.T) {
	t.Parallel()
	op := queryOperation()

	required := make(map[string]bool)
	for _, p := range op.Parameters {
		if p.Required {
			required[p.Name] = true
		}
	}

	if !required[paramProjectID] {
		t.Error("project_id should be required")
	}
	if !required[paramQuery] {
		t.Error("query should be required")
	}
	if required[paramMaxResults] {
		t.Error("max_results should not be required")
	}
}

func TestExecuteQuery_MissingProjectID(t *testing.T) {
	t.Parallel()
	_, err := executeQuery(context.Background(), map[string]any{paramQuery: "SELECT 1"}, "token")
	if err == nil {
		t.Fatal("expected error for missing project_id")
	}
}

func TestExecuteQuery_MissingQuery(t *testing.T) {
	t.Parallel()
	_, err := executeQuery(context.Background(), map[string]any{paramProjectID: "proj"}, "token")
	if err == nil {
		t.Fatal("expected error for missing query")
	}
}

func TestIntParam(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		val      any
		fallback int
		want     int
	}{
		{"float64", float64(100), 0, 100},
		{"int", 200, 0, 200},
		{"int64", int64(300), 0, 300},
		{"string_fallback", "bad", 42, 42},
		{"missing", nil, 42, 42},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			params := map[string]any{}
			if tt.val != nil {
				params["key"] = tt.val
			}
			got := intParam(params, "key", tt.fallback)
			if got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestQueryResult_JSONShape(t *testing.T) {
	t.Parallel()
	r := queryResult{
		Schema: []schemaField{
			{Name: "col1", Type: "STRING", Mode: fieldModeNullable},
		},
		Rows:      []map[string]any{{"col1": "hello"}},
		TotalRows: 1,
		Complete:  true,
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if parsed["job_complete"] != true {
		t.Error("expected job_complete=true")
	}
	if parsed["total_rows"].(float64) != 1 {
		t.Error("expected total_rows=1")
	}
	schema := parsed["schema"].([]any)
	if len(schema) != 1 {
		t.Errorf("expected 1 schema field, got %d", len(schema))
	}
}
