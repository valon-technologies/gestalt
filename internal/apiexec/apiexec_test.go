package apiexec

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/valon-technologies/gestalt/internal/testutil"
)

func TestDoGETWithQueryAndPathParams(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"path":  r.URL.Path,
			"query": r.URL.RawQuery,
			"auth":  r.Header.Get("Authorization"),
		})
	}))
	testutil.CloseOnCleanup(t, srv)

	result, err := Do(context.Background(), srv.Client(), Request{
		Method:  http.MethodGet,
		BaseURL: srv.URL,
		Path:    "/items/{item_id}",
		Params: map[string]any{
			"item_id": "abc123",
			"limit":   10,
		},
		Token: "test-token",
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(result.Body), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if resp["path"] != "/items/abc123" {
		t.Fatalf("path = %v, want /items/abc123", resp["path"])
	}
	if resp["auth"] != "Bearer test-token" {
		t.Fatalf("auth = %v, want Bearer test-token", resp["auth"])
	}
}

func TestDoPOSTWithJSONBody(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(body)
	}))
	testutil.CloseOnCleanup(t, srv)

	result, err := Do(context.Background(), srv.Client(), Request{
		Method:  http.MethodPost,
		BaseURL: srv.URL,
		Path:    "/search",
		Params: map[string]any{
			"query": "hello",
			"count": 5,
		},
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(result.Body), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if resp["query"] != "hello" {
		t.Fatalf("query = %v, want hello", resp["query"])
	}
}

func TestDoUsesCustomAuthHeaderAndBodyOverride(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"auth": r.Header.Get("Authorization"),
			"body": body,
		})
	}))
	testutil.CloseOnCleanup(t, srv)

	overrideBody, err := json.Marshal(map[string]any{
		"query": "{ viewer { id } }",
	})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	result, err := Do(context.Background(), srv.Client(), Request{
		Method:     http.MethodPost,
		BaseURL:    srv.URL,
		Path:       "/graphql",
		AuthHeader: "Token abc123",
		Body:       overrideBody,
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(result.Body), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if resp["auth"] != "Token abc123" {
		t.Fatalf("auth = %v, want Token abc123", resp["auth"])
	}
}
