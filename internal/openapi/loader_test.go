package openapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func serveJSON(t *testing.T, spec any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(spec)
	}))
}

func serveYAML(t *testing.T, yaml string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/yaml")
		_, _ = w.Write([]byte(yaml))
	}))
}

func testSpec() map[string]any {
	return map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]string{"title": "Example API", "description": "Test API"},
		"servers": []any{map[string]string{"url": "https://api.example.com/v1"}},
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"oauth2": map[string]any{
					"type": "oauth2",
					"flows": map[string]any{
						"authorizationCode": map[string]any{
							"authorizationUrl": "https://auth.example.com/authorize",
							"tokenUrl":         "https://auth.example.com/token",
						},
					},
				},
			},
		},
		"paths": map[string]any{
			"/items": map[string]any{
				"get": map[string]any{
					"operationId": "list_items",
					"summary":     "List all items",
					"parameters": []any{
						map[string]any{
							"name": "limit", "in": "query",
							"schema": map[string]any{"type": "integer"},
						},
					},
				},
			},
			"/items/{id}": map[string]any{
				"get": map[string]any{
					"operationId": "get_item",
					"summary":     "Get an item by ID",
					"parameters": []any{
						map[string]any{
							"name": "id", "in": "path", "required": true,
							"schema": map[string]any{"type": "string"},
						},
					},
				},
			},
		},
	}
}

func TestLoadDefinition(t *testing.T) {
	t.Parallel()

	srv := serveJSON(t, testSpec())
	defer srv.Close()

	allowed := map[string]string{
		"list_items": "List items with pagination",
		"get_item":   "",
	}

	def, err := LoadDefinition(context.Background(), "example", srv.URL, allowed)
	if err != nil {
		t.Fatalf("LoadDefinition: %v", err)
	}

	if def.Provider != "example" {
		t.Errorf("Provider = %q, want example", def.Provider)
	}
	if def.DisplayName != "Example API" {
		t.Errorf("DisplayName = %q", def.DisplayName)
	}
	if def.BaseURL != "https://api.example.com/v1" {
		t.Errorf("BaseURL = %q", def.BaseURL)
	}
	if def.Auth.AuthorizationURL != "https://auth.example.com/authorize" {
		t.Errorf("Auth.AuthorizationURL = %q", def.Auth.AuthorizationURL)
	}
	if def.Auth.TokenURL != "https://auth.example.com/token" {
		t.Errorf("Auth.TokenURL = %q", def.Auth.TokenURL)
	}
	if len(def.Operations) != 2 {
		t.Fatalf("got %d operations, want 2", len(def.Operations))
	}

	listOp := def.Operations["list_items"]
	if listOp.Description != "List items with pagination" {
		t.Errorf("list_items description = %q, want override", listOp.Description)
	}

	getOp := def.Operations["get_item"]
	if getOp.Description != "Get an item by ID" {
		t.Errorf("get_item description = %q, want spec default", getOp.Description)
	}
}

func TestLoadDefinitionFiltersOperations(t *testing.T) {
	t.Parallel()

	spec := map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]string{"title": "Test"},
		"servers": []any{map[string]string{"url": "https://api.example.com"}},
		"paths": map[string]any{
			"/a": map[string]any{"get": map[string]any{"operationId": "op_a", "summary": "A"}},
			"/b": map[string]any{"get": map[string]any{"operationId": "op_b", "summary": "B"}},
			"/c": map[string]any{"get": map[string]any{"operationId": "op_c", "summary": "C"}},
		},
	}

	srv := serveJSON(t, spec)
	defer srv.Close()

	def, err := LoadDefinition(context.Background(), "test", srv.URL, map[string]string{"op_a": "", "op_c": ""})
	if err != nil {
		t.Fatalf("LoadDefinition: %v", err)
	}

	if len(def.Operations) != 2 {
		t.Fatalf("got %d operations, want 2", len(def.Operations))
	}
	if _, ok := def.Operations["op_b"]; ok {
		t.Error("op_b should have been filtered out")
	}
}

func TestLoadDefinitionNilAllowedOpsExposesAll(t *testing.T) {
	t.Parallel()

	spec := map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]string{"title": "Test"},
		"servers": []any{map[string]string{"url": "https://api.example.com"}},
		"paths": map[string]any{
			"/a": map[string]any{"get": map[string]any{"operationId": "op_a", "summary": "A"}},
			"/b": map[string]any{"get": map[string]any{"operationId": "op_b", "summary": "B"}},
		},
	}

	srv := serveJSON(t, spec)
	defer srv.Close()

	def, err := LoadDefinition(context.Background(), "test", srv.URL, nil)
	if err != nil {
		t.Fatalf("LoadDefinition: %v", err)
	}

	if len(def.Operations) != 2 {
		t.Fatalf("got %d operations, want 2", len(def.Operations))
	}
}

func TestLoadDefinitionYAML(t *testing.T) {
	t.Parallel()

	srv := serveYAML(t, `
openapi: "3.0.0"
info:
  title: YAML API
servers:
  - url: https://api.yaml.example.com
paths:
  /ping:
    get:
      operationId: ping
      summary: Ping
`)
	defer srv.Close()

	def, err := LoadDefinition(context.Background(), "yamltest", srv.URL, nil)
	if err != nil {
		t.Fatalf("LoadDefinition YAML: %v", err)
	}
	if def.DisplayName != "YAML API" {
		t.Errorf("DisplayName = %q", def.DisplayName)
	}
	if len(def.Operations) != 1 {
		t.Fatalf("got %d operations, want 1", len(def.Operations))
	}
}
