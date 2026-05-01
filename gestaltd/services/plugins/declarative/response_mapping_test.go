package declarative

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/valon-technologies/gestalt/server/services/plugins/apiexec"
)

func TestResponseMappingExtractsDataAndPagination(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success":           true,
			"results":           []any{map[string]any{"id": "1", "name": "Alice"}, map[string]any{"id": "2", "name": "Bob"}},
			"moreDataAvailable": true,
			"nextCursor":        "cursor-abc",
		})
	}))
	t.Cleanup(func() { srv.Close() })

	b := &Base{
		Auth:    mockAuth{},
		BaseURL: srv.URL,
		ResponseMapping: &ResponseMappingConfig{
			DataPath: "results",
			Pagination: &PaginationProjectionConfig{
				HasMore: &apiexec.ValueSelector{
					Source: apiexec.ValueSelectorSourceBody,
					Path:   "moreDataAvailable",
				},
				Cursor: &apiexec.ValueSelector{
					Source: apiexec.ValueSelectorSourceBody,
					Path:   "nextCursor",
				},
			},
		},
	}
	setTestCatalog(b, restCatalogOp("list", http.MethodPost, "/list"))

	result, err := b.Execute(context.Background(), "list", nil, "test-token")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result.Body), &parsed); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	data, ok := parsed["data"].([]any)
	if !ok {
		t.Fatalf("data is not array: %v", parsed["data"])
	}
	if len(data) != 2 {
		t.Fatalf("data length = %d, want 2", len(data))
	}
	if data[0].(map[string]any)["name"] != "Alice" {
		t.Fatalf("first item name = %v, want Alice", data[0])
	}

	pgn := parsed["pagination"].(map[string]any)
	if pgn["hasMore"] != true {
		t.Fatalf("hasMore = %v, want true", pgn["hasMore"])
	}
	if pgn["cursor"] != "cursor-abc" {
		t.Fatalf("cursor = %v, want cursor-abc", pgn["cursor"])
	}

	if _, exists := parsed["success"]; exists {
		t.Fatal("original envelope fields should be stripped")
	}
}

func TestResponseMappingNestedDataPath(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"response": map[string]any{
				"items": []any{map[string]any{"id": "1"}},
			},
		})
	}))
	t.Cleanup(func() { srv.Close() })

	b := &Base{
		Auth:            mockAuth{},
		BaseURL:         srv.URL,
		ResponseMapping: &ResponseMappingConfig{DataPath: "response.items"},
	}
	setTestCatalog(b, restCatalogOp("list", http.MethodGet, "/list"))

	result, err := b.Execute(context.Background(), "list", nil, "tok")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result.Body), &parsed); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	data := parsed["data"].([]any)
	if len(data) != 1 {
		t.Fatalf("data length = %d, want 1", len(data))
	}
}

func TestResponseMappingPassesThroughErrors(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad request"}`))
	}))
	t.Cleanup(func() { srv.Close() })

	b := &Base{
		Auth:    mockAuth{},
		BaseURL: srv.URL,
		CheckResponse: func(status int, body []byte) error {
			return nil
		},
		ResponseMapping: &ResponseMappingConfig{DataPath: "results"},
	}
	setTestCatalog(b, restCatalogOp("op", http.MethodGet, "/op"))

	result, err := b.Execute(context.Background(), "op", nil, "tok")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", result.Status)
	}
	if result.Body != `{"error":"bad request"}` {
		t.Fatalf("error body should pass through unchanged, got %s", result.Body)
	}
}

func TestResponseMappingPassesThroughWhenDataPathMissing(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"not_abc123","title":"Meeting notes","hasMore":true,"cursor":"next"}`))
	}))
	t.Cleanup(func() { srv.Close() })

	b := &Base{
		Auth:    mockAuth{},
		BaseURL: srv.URL,
		ResponseMapping: &ResponseMappingConfig{
			Pagination: &PaginationProjectionConfig{
				HasMore: &apiexec.ValueSelector{
					Source: apiexec.ValueSelectorSourceBody,
					Path:   "hasMore",
				},
				Cursor: &apiexec.ValueSelector{
					Source: apiexec.ValueSelectorSourceBody,
					Path:   "cursor",
				},
			},
		},
	}
	setTestCatalog(b, restCatalogOp("get", http.MethodGet, "/get"))

	result, err := b.Execute(context.Background(), "get", nil, "tok")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if result.Body != `{"id":"not_abc123","title":"Meeting notes","hasMore":true,"cursor":"next"}` {
		t.Fatalf("body = %s, want original response", result.Body)
	}
}

func TestResponseMappingWithoutConfig(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"raw":"passthrough"}`))
	}))
	t.Cleanup(func() { srv.Close() })

	b := &Base{
		Auth:    mockAuth{},
		BaseURL: srv.URL,
	}
	setTestCatalog(b, restCatalogOp("op", http.MethodGet, "/op"))

	result, err := b.Execute(context.Background(), "op", nil, "tok")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result.Body), &parsed); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if parsed["raw"] != "passthrough" {
		t.Fatalf("without config, response should pass through unchanged")
	}
}
