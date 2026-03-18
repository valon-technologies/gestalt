package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/valon-technologies/toolshed/core"
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
	defer srv.Close()

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
	defer srv.Close()

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
