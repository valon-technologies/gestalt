package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/core"
)

type mockAuth struct{}

func (m mockAuth) AuthorizationURL(state string, _ []string) string {
	return "https://example.com/auth?state=" + state
}

func (m mockAuth) ExchangeCode(_ context.Context, code string) (*core.TokenResponse, error) {
	return &core.TokenResponse{AccessToken: "access-" + code, TokenType: "Bearer"}, nil
}

func (m mockAuth) RefreshToken(_ context.Context, refreshToken string) (*core.TokenResponse, error) {
	return &core.TokenResponse{AccessToken: "refresh-" + refreshToken, TokenType: "Bearer"}, nil
}

func TestBaseExecuteDispatchesToEndpoint(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"path": r.URL.Path,
			"auth": r.Header.Get("Authorization"),
		})
	}))
	t.Cleanup(func() { srv.Close() })

	b := &Base{
		Auth:    mockAuth{},
		BaseURL: srv.URL,
		Endpoints: map[string]Endpoint{
			"list_items": {Method: http.MethodGet, Path: "/api/items"},
		},
	}

	result, err := b.Execute(context.Background(), "list_items", nil, "test-token")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(result.Body), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if resp["path"] != "/api/items" {
		t.Fatalf("path = %v, want /api/items", resp["path"])
	}
	if resp["auth"] != "Bearer test-token" {
		t.Fatalf("auth = %v, want Bearer test-token", resp["auth"])
	}
}

func TestBaseTokenParserOverridesAuthorization(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"auth":   r.Header.Get("Authorization"),
			"custom": r.Header.Get("X-Custom"),
		})
	}))
	t.Cleanup(func() { srv.Close() })

	b := &Base{
		Auth:    mockAuth{},
		BaseURL: srv.URL,
		Endpoints: map[string]Endpoint{
			"op": {Method: http.MethodGet, Path: "/test"},
		},
		TokenParser: func(token string) (string, map[string]string, error) {
			return "Token " + token, map[string]string{"X-Custom": "value"}, nil
		},
	}

	result, err := b.Execute(context.Background(), "op", nil, "abc123")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(result.Body), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if resp["auth"] != "Token abc123" {
		t.Fatalf("auth = %v, want Token abc123", resp["auth"])
	}
	if resp["custom"] != "value" {
		t.Fatalf("custom = %v, want value", resp["custom"])
	}
}

func TestBaseExecuteFuncOverridesDefaultExecution(t *testing.T) {
	t.Parallel()

	b := &Base{
		Auth: mockAuth{},
		ExecuteFunc: func(_ context.Context, operation string, _ map[string]any, _ string) (*core.OperationResult, error) {
			return &core.OperationResult{Status: 299, Body: "custom-" + operation}, nil
		},
	}

	result, err := b.Execute(context.Background(), "anything", nil, "tok")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != 299 {
		t.Fatalf("status = %d, want 299", result.Status)
	}
	if result.Body != "custom-anything" {
		t.Fatalf("body = %q, want %q", result.Body, "custom-anything")
	}
}

func TestBaseExecuteRoutesGraphQLOperations(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)

		query, _ := body["query"].(string)
		auth := r.Header.Get("Authorization")

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"viewer":{"login":"test","query":"` + query + `","auth":"` + auth + `"}}}`))
	}))
	t.Cleanup(func() { srv.Close() })

	b := &Base{
		Auth:    mockAuth{},
		BaseURL: srv.URL,
		Queries: map[string]string{
			"get_viewer": "{ viewer { login } }",
		},
		Endpoints: map[string]Endpoint{
			"list_items": {Method: http.MethodGet, Path: "/api/items"},
		},
	}

	result, err := b.Execute(context.Background(), "get_viewer", nil, "gql-token")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("status = %d, want 200", result.Status)
	}

	var data map[string]any
	if err := json.Unmarshal([]byte(result.Body), &data); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	viewer := data["viewer"].(map[string]any)
	if viewer["auth"] != "Bearer gql-token" {
		t.Fatalf("auth = %v, want Bearer gql-token", viewer["auth"])
	}
}

func TestBaseExecuteGraphQLWithVariables(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		vars, _ := body["variables"].(map[string]any)
		first := vars["first"]

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"repos": map[string]any{"count": first},
			},
		})
	}))
	t.Cleanup(func() { srv.Close() })

	b := &Base{
		Auth:    mockAuth{},
		BaseURL: srv.URL,
		Queries: map[string]string{
			"list_repos": "query($first: Int) { viewer { repositories(first: $first) { nodes { name } } } }",
		},
	}

	result, err := b.Execute(context.Background(), "list_repos", map[string]any{"first": 5}, "tok")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var data map[string]any
	if err := json.Unmarshal([]byte(result.Body), &data); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	repos := data["repos"].(map[string]any)
	if repos["count"] != float64(5) {
		t.Fatalf("count = %v, want 5", repos["count"])
	}
}

func TestBaseExecuteGraphQLWithTokenParser(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		extra := r.Header.Get("X-Org")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"auth":"` + auth + `","org":"` + extra + `"}}`))
	}))
	t.Cleanup(func() { srv.Close() })

	b := &Base{
		Auth:    mockAuth{},
		BaseURL: srv.URL,
		Queries: map[string]string{
			"get_viewer": "{ viewer { login } }",
		},
		TokenParser: func(token string) (string, map[string]string, error) {
			return "Token " + token, map[string]string{"X-Org": "acme"}, nil
		},
	}

	result, err := b.Execute(context.Background(), "get_viewer", nil, "my-token")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var data map[string]any
	if err := json.Unmarshal([]byte(result.Body), &data); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if data["auth"] != "Token my-token" {
		t.Fatalf("auth = %v, want Token my-token", data["auth"])
	}
	if data["org"] != "acme" {
		t.Fatalf("org = %v, want acme", data["org"])
	}
}

func TestBaseExecuteGraphQLErrorsReturned(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errors":[{"message":"rate limited"}]}`))
	}))
	t.Cleanup(func() { srv.Close() })

	b := &Base{
		Auth:    mockAuth{},
		BaseURL: srv.URL,
		Queries: map[string]string{
			"get_viewer": "{ viewer { login } }",
		},
	}

	_, err := b.Execute(context.Background(), "get_viewer", nil, "tok")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("error = %v, want to contain 'rate limited'", err)
	}
}

func TestBaseExecuteUnknownOperationFallsThrough(t *testing.T) {
	t.Parallel()

	b := &Base{
		Auth: mockAuth{},
		Queries: map[string]string{
			"get_viewer": "{ viewer { login } }",
		},
	}

	_, err := b.Execute(context.Background(), "nonexistent", nil, "tok")
	if err == nil {
		t.Fatal("expected error for unknown operation, got nil")
	}
	if !strings.Contains(err.Error(), "unknown operation") {
		t.Fatalf("error = %v, want to contain 'unknown operation'", err)
	}
}
