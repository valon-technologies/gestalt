package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDefaultParamsAppliedWhenMissing(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"path": r.URL.Path,
		})
	}))
	t.Cleanup(func() { srv.Close() })

	b := &Base{
		Auth:    mockAuth{},
		BaseURL: srv.URL,
		Endpoints: map[string]Endpoint{
			"list_messages": {Method: http.MethodGet, Path: "/v1/users/{userId}/messages"},
		},
		DefaultParams: map[string]any{
			"userId": "me",
		},
	}

	result, err := b.Execute(context.Background(), "list_messages", nil, "tok")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(result.Body), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if resp["path"] != "/v1/users/me/messages" {
		t.Fatalf("path = %v, want /v1/users/me/messages", resp["path"])
	}
}

func TestDefaultParamsNotOverrideExplicit(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"path": r.URL.Path,
		})
	}))
	t.Cleanup(func() { srv.Close() })

	b := &Base{
		Auth:    mockAuth{},
		BaseURL: srv.URL,
		Endpoints: map[string]Endpoint{
			"list_messages": {Method: http.MethodGet, Path: "/v1/users/{userId}/messages"},
		},
		DefaultParams: map[string]any{
			"userId": "me",
		},
	}

	result, err := b.Execute(context.Background(), "list_messages", map[string]any{
		"userId": "someone-else",
	}, "tok")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(result.Body), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if resp["path"] != "/v1/users/someone-else/messages" {
		t.Fatalf("path = %v, want /v1/users/someone-else/messages", resp["path"])
	}
}

func TestNoDefaultParamsDoesNotInterfere(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"path": r.URL.Path,
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

	result, err := b.Execute(context.Background(), "list_items", nil, "tok")
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
}
