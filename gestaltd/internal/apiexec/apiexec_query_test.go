package apiexec

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestAddQueryValue_Scalar(t *testing.T) {
	t.Parallel()

	q := url.Values{}
	addQueryValue(q, "count", 42)
	addQueryValue(q, "name", "alice")

	if got := q.Get("count"); got != "42" {
		t.Fatalf("count = %q, want 42", got)
	}
	if got := q.Get("name"); got != "alice" {
		t.Fatalf("name = %q, want alice", got)
	}
}

func TestAddQueryValue_SliceAny(t *testing.T) {
	t.Parallel()

	q := url.Values{}
	addQueryValue(q, "tag", []any{"a", "b"})

	got := q["tag"]
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("tag = %v, want [a b]", got)
	}
}

func TestAddQueryValue_SliceString(t *testing.T) {
	t.Parallel()

	q := url.Values{}
	addQueryValue(q, "color", []string{"red", "blue"})

	got := q["color"]
	if len(got) != 2 || got[0] != "red" || got[1] != "blue" {
		t.Fatalf("color = %v, want [red blue]", got)
	}
}

func TestDo_POSTWithQueryParams(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"query":  r.URL.RawQuery,
			"body":   body,
			"method": r.Method,
		})
	}))
	testutil.CloseOnCleanup(t, srv)

	result, err := Do(context.Background(), srv.Client(), Request{
		Method:  http.MethodPost,
		BaseURL: srv.URL,
		Path:    "/items",
		Params:  map[string]any{"name": "widget"},
		QueryParams: map[string]any{
			"page":  2,
			"limit": 10,
		},
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(result.Body), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	query := resp["query"].(string)
	parsed, err := url.ParseQuery(query)
	if err != nil {
		t.Fatalf("url.ParseQuery: %v", err)
	}
	if parsed.Get("page") != "2" {
		t.Fatalf("query page = %q, want 2", parsed.Get("page"))
	}
	if parsed.Get("limit") != "10" {
		t.Fatalf("query limit = %q, want 10", parsed.Get("limit"))
	}

	body := resp["body"].(map[string]any)
	if body["name"] != "widget" {
		t.Fatalf("body name = %v, want widget", body["name"])
	}
}

func TestDo_GETWithArrayQueryFallback(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"query": r.URL.RawQuery,
		})
	}))
	testutil.CloseOnCleanup(t, srv)

	result, err := Do(context.Background(), srv.Client(), Request{
		Method:  http.MethodGet,
		BaseURL: srv.URL,
		Path:    "/search",
		Params: map[string]any{
			"tag": []any{"alpha", "beta"},
		},
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(result.Body), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	query := resp["query"].(string)
	parsed, err := url.ParseQuery(query)
	if err != nil {
		t.Fatalf("url.ParseQuery: %v", err)
	}
	tags := parsed["tag"]
	if len(tags) != 2 || tags[0] != "alpha" || tags[1] != "beta" {
		t.Fatalf("tag = %v, want [alpha beta]", tags)
	}
}

func TestDo_QueryParamsNil_PreservesLegacy(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"query": r.URL.RawQuery,
			"body":  body,
		})
	}))
	testutil.CloseOnCleanup(t, srv)

	result, err := Do(context.Background(), srv.Client(), Request{
		Method:  http.MethodPost,
		BaseURL: srv.URL,
		Path:    "/items",
		Params:  map[string]any{"name": "widget", "count": 5},
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(result.Body), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if resp["query"] != "" {
		t.Fatalf("query = %q, want empty (legacy POST should have no query string)", resp["query"])
	}

	body := resp["body"].(map[string]any)
	if body["name"] != "widget" {
		t.Fatalf("body name = %v, want widget", body["name"])
	}
	if body["count"] != float64(5) {
		t.Fatalf("body count = %v, want 5", body["count"])
	}
}

func TestExpandedPathWithQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		method      string
		path        string
		params      map[string]any
		queryParams map[string]any
		wantPrefix  string
		wantQuery   url.Values
	}{
		{
			name:        "POST with query params",
			method:      http.MethodPost,
			path:        "/items",
			params:      map[string]any{"name": "x"},
			queryParams: map[string]any{"page": 2},
			wantPrefix:  "/items",
			wantQuery:   url.Values{"page": {"2"}},
		},
		{
			name:        "GET merges both",
			method:      http.MethodGet,
			path:        "/items/{id}",
			params:      map[string]any{"id": "abc", "limit": 10},
			queryParams: map[string]any{"page": 1},
			wantPrefix:  "/items/abc",
			wantQuery:   url.Values{"page": {"1"}, "limit": {"10"}},
		},
		{
			name:       "nil query params delegates to legacy",
			method:     http.MethodGet,
			path:       "/items",
			params:     map[string]any{"limit": 5},
			wantPrefix: "/items",
			wantQuery:  url.Values{"limit": {"5"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ExpandedPathWithQuery(tt.method, tt.path, tt.params, tt.queryParams)
			parts := strings.SplitN(got, "?", 2)
			if parts[0] != tt.wantPrefix {
				t.Fatalf("path prefix = %q, want %q", parts[0], tt.wantPrefix)
			}
			if tt.wantQuery != nil {
				if len(parts) < 2 {
					t.Fatalf("expected query string, got none in %q", got)
				}
				parsed, err := url.ParseQuery(parts[1])
				if err != nil {
					t.Fatalf("url.ParseQuery: %v", err)
				}
				for k, want := range tt.wantQuery {
					gotVals := parsed[k]
					if len(gotVals) != len(want) {
						t.Fatalf("query[%s] = %v, want %v", k, gotVals, want)
					}
					for i := range want {
						if gotVals[i] != want[i] {
							t.Fatalf("query[%s][%d] = %q, want %q", k, i, gotVals[i], want[i])
						}
					}
				}
			}
		})
	}
}
