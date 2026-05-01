package apiexec

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestDoPaginatedCursor(t *testing.T) {
	t.Parallel()

	pages := []map[string]any{
		{"data": []any{"a", "b"}, "next_cursor": "page2"},
		{"data": []any{"c", "d"}, "next_cursor": ""},
	}
	callCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := callCount
		if idx >= len(pages) {
			idx = len(pages) - 1
		}
		callCount++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(pages[idx])
	}))
	testutil.CloseOnCleanup(t, srv)

	result, err := DoPaginated(context.Background(), srv.Client(), Request{
		Method:  http.MethodGet,
		BaseURL: srv.URL,
		Path:    "/items",
	}, PaginationConfig{
		Style:       PaginationStyleCursor,
		CursorParam: "cursor",
		Cursor: &ValueSelector{
			Source: ValueSelectorSourceBody,
			Path:   "next_cursor",
		},
		ResultsPath: "data",
	})
	if err != nil {
		t.Fatalf("DoPaginated: %v", err)
	}

	var items []any
	if err := json.Unmarshal([]byte(result.Body), &items); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(items) != 4 {
		t.Fatalf("got %d items, want 4", len(items))
	}
	if callCount != 2 {
		t.Fatalf("server called %d times, want 2", callCount)
	}
}

func TestDoPaginatedOffset(t *testing.T) {
	t.Parallel()

	allItems := []any{"a", "b", "c", "d", "e"}
	const pageSize = 2

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset := 0
		if v := r.URL.Query().Get("offset"); v != "" {
			offset, _ = strconv.Atoi(v)
		}
		end := offset + pageSize
		if end > len(allItems) {
			end = len(allItems)
		}
		var page []any
		if offset < len(allItems) {
			page = allItems[offset:end]
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"results": page})
	}))
	testutil.CloseOnCleanup(t, srv)

	result, err := DoPaginated(context.Background(), srv.Client(), Request{
		Method:  http.MethodGet,
		BaseURL: srv.URL,
		Path:    "/items",
	}, PaginationConfig{
		Style:        PaginationStyleOffset,
		CursorParam:  "offset",
		LimitParam:   "limit",
		DefaultLimit: pageSize,
		ResultsPath:  "results",
	})
	if err != nil {
		t.Fatalf("DoPaginated: %v", err)
	}

	var items []any
	if err := json.Unmarshal([]byte(result.Body), &items); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(items) != 5 {
		t.Fatalf("got %d items, want 5", len(items))
	}
}

func TestDoPaginatedMaxPages(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items":       []any{"x"},
			"next_cursor": "always-more",
		})
	}))
	testutil.CloseOnCleanup(t, srv)

	const maxPages = 3
	result, err := DoPaginated(context.Background(), srv.Client(), Request{
		Method:  http.MethodGet,
		BaseURL: srv.URL,
		Path:    "/items",
	}, PaginationConfig{
		Style:       PaginationStyleCursor,
		CursorParam: "cursor",
		Cursor: &ValueSelector{
			Source: ValueSelectorSourceBody,
			Path:   "next_cursor",
		},
		ResultsPath: "items",
		MaxPages:    maxPages,
	})
	if err != nil {
		t.Fatalf("DoPaginated: %v", err)
	}

	var items []any
	if err := json.Unmarshal([]byte(result.Body), &items); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(items) != maxPages {
		t.Fatalf("got %d items, want %d (max_pages)", len(items), maxPages)
	}
}

func TestDoPaginatedEmptyResults(t *testing.T) {
	t.Parallel()

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var page []any
		if callCount == 1 {
			page = []any{"a", "b"}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":        page,
			"next_cursor": fmt.Sprintf("page%d", callCount+1),
		})
	}))
	testutil.CloseOnCleanup(t, srv)

	result, err := DoPaginated(context.Background(), srv.Client(), Request{
		Method:  http.MethodGet,
		BaseURL: srv.URL,
		Path:    "/items",
	}, PaginationConfig{
		Style:       PaginationStyleCursor,
		CursorParam: "cursor",
		Cursor: &ValueSelector{
			Source: ValueSelectorSourceBody,
			Path:   "next_cursor",
		},
		ResultsPath: "data",
	})
	if err != nil {
		t.Fatalf("DoPaginated: %v", err)
	}

	var items []any
	if err := json.Unmarshal([]byte(result.Body), &items); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	if callCount != 2 {
		t.Fatalf("server called %d times, want 2", callCount)
	}
}

func TestDoPaginatedCallerProvidedLimit(t *testing.T) {
	t.Parallel()

	var receivedLimit string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedLimit = r.URL.Query().Get("per_page")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":        []any{"a"},
			"next_cursor": "",
		})
	}))
	testutil.CloseOnCleanup(t, srv)

	callerLimit := 50
	_, err := DoPaginated(context.Background(), srv.Client(), Request{
		Method:  http.MethodGet,
		BaseURL: srv.URL,
		Path:    "/items",
		Params:  map[string]any{"per_page": callerLimit},
	}, PaginationConfig{
		Style:       PaginationStyleCursor,
		CursorParam: "cursor",
		Cursor: &ValueSelector{
			Source: ValueSelectorSourceBody,
			Path:   "next_cursor",
		},
		LimitParam:   "per_page",
		DefaultLimit: 25,
		ResultsPath:  "data",
	})
	if err != nil {
		t.Fatalf("DoPaginated: %v", err)
	}

	if receivedLimit != "50" {
		t.Fatalf("limit sent = %q, want %q", receivedLimit, "50")
	}
}

func TestDoPaginatedPage(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		var items []any
		switch page {
		case "1":
			items = []any{"a", "b"}
		case "2":
			items = []any{"c"}
		default:
			items = []any{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"results": items})
	}))
	testutil.CloseOnCleanup(t, srv)

	result, err := DoPaginated(context.Background(), srv.Client(), Request{
		Method:  http.MethodGet,
		BaseURL: srv.URL,
		Path:    "/items",
	}, PaginationConfig{
		Style:       PaginationStylePage,
		CursorParam: "page",
		ResultsPath: "results",
	})
	if err != nil {
		t.Fatalf("DoPaginated: %v", err)
	}

	var items []any
	if err := json.Unmarshal([]byte(result.Body), &items); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3", len(items))
	}
}

func TestDoPaginatedNumericCursor(t *testing.T) {
	t.Parallel()

	pages := []map[string]any{
		{"data": []any{"a", "b"}, "next_cursor": 12345},
		{"data": []any{"c"}, "next_cursor": 0},
	}
	callCount := 0
	var secondCursor string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := callCount
		if idx >= len(pages) {
			idx = len(pages) - 1
		}
		if callCount > 0 {
			secondCursor = r.URL.Query().Get("cursor")
		}
		callCount++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(pages[idx])
	}))
	testutil.CloseOnCleanup(t, srv)

	result, err := DoPaginated(context.Background(), srv.Client(), Request{
		Method:  http.MethodGet,
		BaseURL: srv.URL,
		Path:    "/items",
	}, PaginationConfig{
		Style:       PaginationStyleCursor,
		CursorParam: "cursor",
		Cursor: &ValueSelector{
			Source: ValueSelectorSourceBody,
			Path:   "next_cursor",
		},
		ResultsPath: "data",
	})
	if err != nil {
		t.Fatalf("DoPaginated: %v", err)
	}

	var items []any
	if err := json.Unmarshal([]byte(result.Body), &items); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3", len(items))
	}
	if callCount != 2 {
		t.Fatalf("server called %d times, want 2", callCount)
	}
	if secondCursor != "12345" {
		t.Fatalf("second request cursor = %q, want %q", secondCursor, "12345")
	}
}

func TestDoPaginatedDoesNotMutateCallerParams(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":        []any{"a"},
			"next_cursor": "",
		})
	}))
	testutil.CloseOnCleanup(t, srv)

	callerParams := map[string]any{"foo": "bar"}
	_, err := DoPaginated(context.Background(), srv.Client(), Request{
		Method:  http.MethodGet,
		BaseURL: srv.URL,
		Path:    "/items",
		Params:  callerParams,
	}, PaginationConfig{
		Style:       PaginationStyleCursor,
		CursorParam: "cursor",
		Cursor: &ValueSelector{
			Source: ValueSelectorSourceBody,
			Path:   "next_cursor",
		},
		LimitParam:   "per_page",
		DefaultLimit: 25,
		ResultsPath:  "data",
	})
	if err != nil {
		t.Fatalf("DoPaginated: %v", err)
	}

	if len(callerParams) != 1 {
		t.Fatalf("caller params mutated: got %d keys, want 1: %v", len(callerParams), callerParams)
	}
	if callerParams["foo"] != "bar" {
		t.Fatalf("caller params key modified: foo = %v", callerParams["foo"])
	}
}

func TestDoPaginatedNestedResultsPath(t *testing.T) {
	t.Parallel()

	pages := []map[string]any{
		{
			"response": map[string]any{
				"items": []any{"a", "b"},
			},
			"meta": map[string]any{"next": "p2"},
		},
		{
			"response": map[string]any{
				"items": []any{"c"},
			},
			"meta": map[string]any{"next": ""},
		},
	}
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := callCount
		if idx >= len(pages) {
			idx = len(pages) - 1
		}
		callCount++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(pages[idx])
	}))
	testutil.CloseOnCleanup(t, srv)

	result, err := DoPaginated(context.Background(), srv.Client(), Request{
		Method:  http.MethodGet,
		BaseURL: srv.URL,
		Path:    "/items",
	}, PaginationConfig{
		Style:       PaginationStyleCursor,
		CursorParam: "cursor",
		Cursor: &ValueSelector{
			Source: ValueSelectorSourceBody,
			Path:   "meta.next",
		},
		ResultsPath: "response.items",
	})
	if err != nil {
		t.Fatalf("DoPaginated: %v", err)
	}

	var items []any
	if err := json.Unmarshal([]byte(result.Body), &items); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3", len(items))
	}
}
