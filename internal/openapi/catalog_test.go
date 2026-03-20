package openapi

import (
	"context"
	"encoding/json"
	"testing"
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
	t.Cleanup(func() { srv.Close() })

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
	t.Cleanup(func() { srv.Close() })

	cat, err := LoadCatalog(context.Background(), "test", srv.URL, nil)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}

	for _, op := range cat.Operations {
		if op.Annotations.OpenWorldHint == nil || !*op.Annotations.OpenWorldHint {
			t.Errorf("%s: openWorldHint should be true", op.ID)
		}

		switch op.Method {
		case "GET":
			if op.Annotations.ReadOnlyHint == nil || !*op.Annotations.ReadOnlyHint {
				t.Errorf("%s: GET should have readOnlyHint=true", op.ID)
			}
		case "DELETE":
			if op.Annotations.DestructiveHint == nil || !*op.Annotations.DestructiveHint {
				t.Errorf("%s: DELETE should have destructiveHint=true", op.ID)
			}
		case "PUT":
			if op.Annotations.IdempotentHint == nil || !*op.Annotations.IdempotentHint {
				t.Errorf("%s: PUT should have idempotentHint=true", op.ID)
			}
		}
	}
}

func TestLoadCatalogAllowedOpsFiltering(t *testing.T) {
	t.Parallel()

	srv := serveJSON(t, nestedBodySpec())
	t.Cleanup(func() { srv.Close() })

	allowed := map[string]string{
		"list_items":  "Custom list description",
		"create_item": "",
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
	t.Cleanup(func() { srv.Close() })

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
