package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/valon-technologies/gestalt/core/catalog"
)

func TestPartitionParams_NilCatalogOp(t *testing.T) {
	t.Parallel()

	params := map[string]any{"name": "x", "page": 2}
	body, query, headers := partitionParams(nil, params)

	if body["name"] != "x" || body["page"] != 2 {
		t.Fatalf("body = %v, want all params", body)
	}
	if query != nil {
		t.Fatalf("query = %v, want nil", query)
	}
	if headers != nil {
		t.Fatalf("headers = %v, want nil", headers)
	}
}

func TestPartitionParams_NoLocations(t *testing.T) {
	t.Parallel()

	catOp := &catalog.CatalogOperation{
		ID: "op1",
		Parameters: []catalog.CatalogParameter{
			{Name: "name", Type: "string"},
			{Name: "count", Type: "integer"},
		},
	}
	params := map[string]any{"name": "x", "count": 5}
	body, query, headers := partitionParams(catOp, params)

	if body["name"] != "x" || body["count"] != 5 {
		t.Fatalf("body = %v, want all params", body)
	}
	if query != nil {
		t.Fatalf("query = %v, want nil", query)
	}
	if headers != nil {
		t.Fatalf("headers = %v, want nil", headers)
	}
}

func TestPartitionParams_MixedLocations(t *testing.T) {
	t.Parallel()

	catOp := &catalog.CatalogOperation{
		ID: "op1",
		Parameters: []catalog.CatalogParameter{
			{Name: "name", Type: "string", Location: "body"},
			{Name: "page", Type: "integer", Location: "query"},
			{Name: "x_api_key", Type: "string", Location: "header"},
			{Name: "item_id", Type: "string", Location: "path"},
		},
	}
	params := map[string]any{
		"name":      "widget",
		"page":      3,
		"x_api_key": "secret",
		"item_id":   "abc",
	}
	body, query, headers := partitionParams(catOp, params)

	if body["name"] != "widget" {
		t.Fatalf("body[name] = %v, want widget", body["name"])
	}
	if body["item_id"] != "abc" {
		t.Fatalf("body[item_id] = %v, want abc (path params stay in body for substitutePath)", body["item_id"])
	}
	if query["page"] != 3 {
		t.Fatalf("query[page] = %v, want 3", query["page"])
	}
	if headers["x_api_key"] != "secret" {
		t.Fatalf("headers[x_api_key] = %v, want secret", headers["x_api_key"])
	}
}

func TestPartitionParams_UnknownParam(t *testing.T) {
	t.Parallel()

	catOp := &catalog.CatalogOperation{
		ID: "op1",
		Parameters: []catalog.CatalogParameter{
			{Name: "page", Type: "integer", Location: "query"},
		},
	}
	params := map[string]any{
		"page":    1,
		"unknown": "extra",
	}
	body, query, _ := partitionParams(catOp, params)

	if query["page"] != 1 {
		t.Fatalf("query[page] = %v, want 1", query["page"])
	}
	if body["unknown"] != "extra" {
		t.Fatalf("body[unknown] = %v, want extra (unknown params default to body)", body["unknown"])
	}
}

func TestFindCatalogOp_Found(t *testing.T) {
	t.Parallel()

	cat := &catalog.Catalog{
		Name: "test",
		Operations: []catalog.CatalogOperation{
			{ID: "op_alpha", Method: "GET", Path: "/alpha"},
			{ID: "op_beta", Method: "POST", Path: "/beta"},
		},
	}
	got := findCatalogOp(cat, "op_beta")
	if got == nil || got.ID != "op_beta" {
		t.Fatalf("findCatalogOp = %v, want op_beta", got)
	}
}

func TestFindCatalogOp_NilCatalog(t *testing.T) {
	t.Parallel()

	got := findCatalogOp(nil, "anything")
	if got != nil {
		t.Fatalf("findCatalogOp(nil) = %v, want nil", got)
	}
}

func TestFindCatalogOp_NotFound(t *testing.T) {
	t.Parallel()

	cat := &catalog.Catalog{
		Name: "test",
		Operations: []catalog.CatalogOperation{
			{ID: "op_alpha", Method: "GET", Path: "/alpha"},
		},
	}
	got := findCatalogOp(cat, "nonexistent")
	if got != nil {
		t.Fatalf("findCatalogOp = %v, want nil", got)
	}
}

func TestExecuteREST_CatalogQueryParam(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(bodyBytes, &body)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"query":  r.URL.RawQuery,
			"body":   body,
			"method": r.Method,
			"path":   r.URL.Path,
		})
	}))
	t.Cleanup(func() { srv.Close() })

	cat := &catalog.Catalog{
		Name: "test-svc",
		Operations: []catalog.CatalogOperation{
			{
				ID:     "create_item",
				Method: "POST",
				Path:   "/items",
				Parameters: []catalog.CatalogParameter{
					{Name: "name", Type: "string", Location: "body"},
					{Name: "page", Type: "integer", Location: "query"},
				},
			},
		},
	}

	b := &Base{
		Auth:    mockAuth{},
		BaseURL: srv.URL,
		Endpoints: map[string]Endpoint{
			"create_item": {Method: http.MethodPost, Path: "/items"},
		},
	}
	b.SetCatalog(cat)

	result, err := b.Execute(context.Background(), "create_item", map[string]any{
		"name": "widget",
		"page": 2,
	}, "test-token")
	if err != nil {
		t.Fatalf("Execute: %v", err)
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

	body := resp["body"].(map[string]any)
	if body["name"] != "widget" {
		t.Fatalf("body[name] = %v, want widget", body["name"])
	}
	if _, hasPage := body["page"]; hasPage {
		t.Fatalf("body should not contain page (it should be in query)")
	}
}

func TestExecuteREST_NoCatalog_PreservesLegacy(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(bodyBytes, &body)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"query": r.URL.RawQuery,
			"body":  body,
		})
	}))
	t.Cleanup(func() { srv.Close() })

	b := &Base{
		Auth:    mockAuth{},
		BaseURL: srv.URL,
		Endpoints: map[string]Endpoint{
			"create_item": {Method: http.MethodPost, Path: "/items"},
		},
	}

	result, err := b.Execute(context.Background(), "create_item", map[string]any{
		"name": "widget",
		"page": 2,
	}, "test-token")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(result.Body), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if resp["query"] != "" {
		t.Fatalf("query = %q, want empty (legacy POST puts everything in body)", resp["query"])
	}

	body := resp["body"].(map[string]any)
	if body["name"] != "widget" {
		t.Fatalf("body[name] = %v, want widget", body["name"])
	}
	if body["page"] != float64(2) {
		t.Fatalf("body[page] = %v, want 2", body["page"])
	}
}

func TestExecuteREST_CatalogHeaderParam(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(bodyBytes, &body)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"body":     body,
			"x_org_id": r.Header.Get("X-Org-Id"),
			"query":    r.URL.RawQuery,
		})
	}))
	t.Cleanup(func() { srv.Close() })

	cat := &catalog.Catalog{
		Name: "test-svc",
		Operations: []catalog.CatalogOperation{
			{
				ID:     "create_item",
				Method: "POST",
				Path:   "/items",
				Parameters: []catalog.CatalogParameter{
					{Name: "name", Type: "string", Location: "body"},
					{Name: "X-Org-Id", Type: "string", Location: "header"},
				},
			},
		},
	}

	b := &Base{
		Auth:    mockAuth{},
		BaseURL: srv.URL,
		Endpoints: map[string]Endpoint{
			"create_item": {Method: http.MethodPost, Path: "/items"},
		},
	}
	b.SetCatalog(cat)

	result, err := b.Execute(context.Background(), "create_item", map[string]any{
		"name":     "widget",
		"X-Org-Id": "org-123",
	}, "test-token")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(result.Body), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if resp["x_org_id"] != "org-123" {
		t.Fatalf("X-Org-Id header = %v, want org-123", resp["x_org_id"])
	}

	body := resp["body"].(map[string]any)
	if body["name"] != "widget" {
		t.Fatalf("body[name] = %v, want widget", body["name"])
	}
	if _, hasHeader := body["X-Org-Id"]; hasHeader {
		t.Fatalf("body should not contain X-Org-Id (it should be in headers)")
	}
}
