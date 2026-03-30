package openapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func nestedBodySpec() map[string]any {
	return map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]string{"title": "Nested API", "description": "API with nested body"},
		"servers": []any{map[string]string{"url": "https://api.nested.example.com/v1"}},
		"paths": map[string]any{
			"/items": map[string]any{
				"get": map[string]any{
					"operationId": "list_items",
					"summary":     "List items",
					"description": "List all items with filtering",
					"parameters": []any{
						map[string]any{
							"name": "limit", "in": "query",
							"schema": map[string]any{"type": "integer"},
						},
					},
				},
				"post": map[string]any{
					"operationId": "create_item",
					"summary":     "Create item",
					"description": "Create a new item",
					"requestBody": map[string]any{
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{
									"type": "object",
									"properties": map[string]any{
										"name": map[string]any{
											"type":        "string",
											"description": "Item name",
										},
										"filter": map[string]any{
											"type":        "object",
											"description": "Filter criteria",
											"properties": map[string]any{
												"status": map[string]any{
													"type":        "string",
													"description": "Status filter",
												},
												"priority": map[string]any{
													"type": "integer",
												},
											},
											"required": []any{"status"},
										},
									},
									"required": []any{"name"},
								},
							},
						},
					},
				},
			},
			"/items/{id}": map[string]any{
				"delete": map[string]any{
					"operationId": "delete_item",
					"summary":     "Delete item",
					"parameters": []any{
						map[string]any{
							"name": "id", "in": "path", "required": true,
							"schema": map[string]any{"type": "string"},
						},
					},
				},
				"put": map[string]any{
					"operationId": "update_item",
					"summary":     "Update item",
				},
			},
		},
	}
}

func TestLoadCatalogPreservesNestedSchema(t *testing.T) {
	t.Parallel()

	srv := serveJSON(t, nestedBodySpec())
	testutil.CloseOnCleanup(t, srv)

	cat, err := LoadCatalog(context.Background(), "nested", srv.URL, nil)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}

	if cat.Name != "nested" {
		t.Errorf("Name = %q", cat.Name)
	}
	if cat.DisplayName != "Nested API" {
		t.Errorf("DisplayName = %q", cat.DisplayName)
	}
	if cat.BaseURL != "https://api.nested.example.com/v1" {
		t.Errorf("BaseURL = %q", cat.BaseURL)
	}

	if len(cat.Operations) != 4 {
		t.Fatalf("got %d operations, want 4", len(cat.Operations))
	}

	var createOp, listOp, deleteOp, updateOp bool
	for _, op := range cat.Operations {
		switch op.ID {
		case "create_item":
			createOp = true
			if op.InputSchema == nil {
				t.Fatal("create_item should have InputSchema for request body")
			}
			var schema map[string]any
			if err := json.Unmarshal(op.InputSchema, &schema); err != nil {
				t.Fatalf("unmarshal create_item InputSchema: %v", err)
			}
			props, ok := schema["properties"].(map[string]any)
			if !ok {
				t.Fatalf("create_item schema missing properties")
			}
			filterProp, ok := props["filter"].(map[string]any)
			if !ok {
				t.Fatal("create_item schema missing nested filter property")
			}
			filterProps, ok := filterProp["properties"].(map[string]any)
			if !ok {
				t.Fatal("filter property missing nested properties (schema not preserved)")
			}
			if _, ok := filterProps["status"]; !ok {
				t.Error("filter.status not preserved in nested schema")
			}
			if _, ok := filterProps["priority"]; !ok {
				t.Error("filter.priority not preserved in nested schema")
			}

		case "list_items":
			listOp = true
			if op.InputSchema != nil {
				t.Error("list_items should not have InputSchema (no request body)")
			}
			if len(op.Parameters) != 1 || op.Parameters[0].Name != "limit" {
				t.Errorf("list_items parameters = %v", op.Parameters)
			}

		case "delete_item":
			deleteOp = true

		case "update_item":
			updateOp = true
		}
	}

	if !createOp || !listOp || !deleteOp || !updateOp {
		t.Errorf("missing operations: create=%v list=%v delete=%v update=%v", createOp, listOp, deleteOp, updateOp)
	}
}

func TestLoadCatalogAnnotationsFromMethod(t *testing.T) {
	t.Parallel()

	srv := serveJSON(t, nestedBodySpec())
	testutil.CloseOnCleanup(t, srv)

	cat, err := LoadCatalog(context.Background(), "test", srv.URL, nil)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}

	for _, op := range cat.Operations {
		if op.Annotations.OpenWorldHint == nil || !*op.Annotations.OpenWorldHint {
			t.Errorf("%s: openWorldHint should be true", op.ID)
		}

		switch op.Method {
		case http.MethodGet:
			if op.Annotations.ReadOnlyHint == nil || !*op.Annotations.ReadOnlyHint {
				t.Errorf("%s: GET should have readOnlyHint=true", op.ID)
			}
		case http.MethodDelete:
			if op.Annotations.DestructiveHint == nil || !*op.Annotations.DestructiveHint {
				t.Errorf("%s: DELETE should have destructiveHint=true", op.ID)
			}
		case http.MethodPut:
			if op.Annotations.IdempotentHint == nil || !*op.Annotations.IdempotentHint {
				t.Errorf("%s: PUT should have idempotentHint=true", op.ID)
			}
		}
	}
}

func TestLoadCatalogAllowedOpsFiltering(t *testing.T) {
	t.Parallel()

	srv := serveJSON(t, nestedBodySpec())
	testutil.CloseOnCleanup(t, srv)

	allowed := map[string]*config.OperationOverride{
		"list_items":  {Description: "Custom list description"},
		"create_item": nil,
	}
	cat, err := LoadCatalog(context.Background(), "filtered", srv.URL, allowed)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}

	if len(cat.Operations) != 2 {
		t.Fatalf("got %d operations, want 2", len(cat.Operations))
	}

	for _, op := range cat.Operations {
		if op.ID == "list_items" && op.Description != "Custom list description" {
			t.Errorf("list_items description = %q, want override", op.Description)
		}
		if op.ID == "delete_item" || op.ID == "update_item" {
			t.Errorf("operation %q should have been filtered out", op.ID)
		}
	}
}

func TestCatalogExtractAuthAPIKeyHeader(t *testing.T) {
	t.Parallel()

	spec := map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]string{"title": "API Key API"},
		"servers": []any{map[string]string{"url": "https://api.example.com"}},
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"apiKey": map[string]any{
					"type": "apiKey",
					"in":   "header",
					"name": "X-API-Key",
				},
			},
		},
		"paths": map[string]any{
			"/items": map[string]any{
				"get": map[string]any{"operationId": "list_items", "summary": "List items"},
			},
		},
	}

	srv := serveJSON(t, spec)
	testutil.CloseOnCleanup(t, srv)

	cat, err := LoadCatalog(context.Background(), "test", srv.URL, nil)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	if cat.AuthStyle != "raw" {
		t.Errorf("AuthStyle = %q, want raw", cat.AuthStyle)
	}
}

func TestCatalogExtractAuthHTTPBearer(t *testing.T) {
	t.Parallel()

	spec := map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]string{"title": "Bearer API"},
		"servers": []any{map[string]string{"url": "https://api.example.com"}},
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"bearerAuth": map[string]any{
					"type":   "http",
					"scheme": "bearer",
				},
			},
		},
		"paths": map[string]any{
			"/items": map[string]any{
				"get": map[string]any{"operationId": "list_items", "summary": "List items"},
			},
		},
	}

	srv := serveJSON(t, spec)
	testutil.CloseOnCleanup(t, srv)

	cat, err := LoadCatalog(context.Background(), "test", srv.URL, nil)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	if cat.AuthStyle != "bearer" {
		t.Errorf("AuthStyle = %q, want bearer", cat.AuthStyle)
	}
}

func TestCatalogExtractAuthHTTPBasic(t *testing.T) {
	t.Parallel()

	spec := map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]string{"title": "Basic API"},
		"servers": []any{map[string]string{"url": "https://api.example.com"}},
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"basicAuth": map[string]any{
					"type":   "http",
					"scheme": "basic",
				},
			},
		},
		"paths": map[string]any{
			"/items": map[string]any{
				"get": map[string]any{"operationId": "list_items", "summary": "List items"},
			},
		},
	}

	srv := serveJSON(t, spec)
	testutil.CloseOnCleanup(t, srv)

	cat, err := LoadCatalog(context.Background(), "test", srv.URL, nil)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	if cat.AuthStyle != "bearer" {
		t.Errorf("AuthStyle = %q, want bearer (basic deferred to Phase 2)", cat.AuthStyle)
	}
}

func TestLoadCatalogNormalizesBracketParams(t *testing.T) {
	t.Parallel()

	type paramExpectation struct {
		rawName        string
		normalizedName string
		hasWireName    bool
	}

	expectations := []paramExpectation{
		{rawName: "page[size]", normalizedName: "page_size", hasWireName: true},
		{rawName: "filter[from]", normalizedName: "filter_from", hasWireName: true},
		{rawName: "status", normalizedName: "status", hasWireName: false},
	}

	params := make([]any, len(expectations))
	for i, e := range expectations {
		params[i] = map[string]any{
			"name": e.rawName, "in": "query",
			"schema": map[string]any{"type": "string"},
		}
	}

	spec := map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]string{"title": "Bracket API"},
		"servers": []any{map[string]string{"url": "https://api.example.com"}},
		"paths": map[string]any{
			"/records": map[string]any{
				"get": map[string]any{
					"operationId": "list_records",
					"summary":     "List records",
					"parameters":  params,
				},
			},
		},
	}

	srv := serveJSON(t, spec)
	testutil.CloseOnCleanup(t, srv)

	cat, err := LoadCatalog(context.Background(), "test", srv.URL, nil)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}

	op := cat.Operations[0]
	if len(op.Parameters) != len(expectations) {
		t.Fatalf("got %d params, want %d", len(op.Parameters), len(expectations))
	}

	type paramResult struct {
		Name, WireName string
	}
	byWire := make(map[string]paramResult, len(op.Parameters))
	for _, p := range op.Parameters {
		key := p.WireName
		if key == "" {
			key = p.Name
		}
		byWire[key] = paramResult{Name: p.Name, WireName: p.WireName}
	}

	for _, e := range expectations {
		p, ok := byWire[e.rawName]
		if !ok {
			t.Errorf("missing param for raw name %q", e.rawName)
			continue
		}
		if p.Name != e.normalizedName {
			t.Errorf("%q normalized to %q, want %q", e.rawName, p.Name, e.normalizedName)
		}
		if e.hasWireName && p.WireName != e.rawName {
			t.Errorf("%q wire name = %q, want %q", e.rawName, p.WireName, e.rawName)
		}
		if !e.hasWireName && p.WireName != "" {
			t.Errorf("%q should have no wire name, got %q", e.rawName, p.WireName)
		}
	}
}

func TestValidateMCPCompatRejectsDuplicateNormalizedParams(t *testing.T) {
	t.Parallel()

	const (
		bracketName = "page[size]"
		plainName   = "page_size"
	)

	spec := map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]string{"title": "Collision API"},
		"servers": []any{map[string]string{"url": "https://api.example.com"}},
		"paths": map[string]any{
			"/records": map[string]any{
				"get": map[string]any{
					"operationId": "list_records",
					"summary":     "List records",
					"parameters": []any{
						map[string]any{
							"name": bracketName, "in": "query",
							"schema": map[string]any{"type": "integer"},
						},
						map[string]any{
							"name": plainName, "in": "query",
							"schema": map[string]any{"type": "integer"},
						},
					},
				},
			},
		},
	}

	srv := serveJSON(t, spec)
	testutil.CloseOnCleanup(t, srv)

	cat, err := LoadCatalog(context.Background(), "test", srv.URL, nil)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}

	if err := cat.Validate(); err != nil {
		t.Fatalf("Validate() should pass (duplicates are an MCP concern, not a general one): %v", err)
	}

	if err := cat.ValidateMCPCompat(); err == nil {
		t.Fatal("ValidateMCPCompat() should reject duplicate normalized param names")
	}
}

func TestLoadCatalogYAMLSpec(t *testing.T) {
	t.Parallel()

	srv := serveYAML(t, `
openapi: "3.0.0"
info:
  title: YAML Catalog API
  description: Test YAML catalog
servers:
  - url: https://api.yaml.example.com
paths:
  /ping:
    get:
      operationId: ping
      summary: Ping
      description: Health check
`)
	testutil.CloseOnCleanup(t, srv)

	cat, err := LoadCatalog(context.Background(), "yamlcat", srv.URL, nil)
	if err != nil {
		t.Fatalf("LoadCatalog YAML: %v", err)
	}
	if cat.DisplayName != "YAML Catalog API" {
		t.Errorf("DisplayName = %q", cat.DisplayName)
	}
	if len(cat.Operations) != 1 {
		t.Fatalf("got %d operations, want 1", len(cat.Operations))
	}
	if cat.Operations[0].Title != "Ping" {
		t.Errorf("Title = %q, want Ping", cat.Operations[0].Title)
	}
}
