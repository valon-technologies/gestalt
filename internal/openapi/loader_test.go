package openapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/testutil"
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
	testutil.CloseOnCleanup(t, srv)

	allowed := map[string]*config.OperationOverride{
		"list_items": {Description: "List items with pagination"},
		"get_item":   nil,
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
	testutil.CloseOnCleanup(t, srv)

	def, err := LoadDefinition(context.Background(), "test", srv.URL, map[string]*config.OperationOverride{"op_a": nil, "op_c": nil})
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
	testutil.CloseOnCleanup(t, srv)

	def, err := LoadDefinition(context.Background(), "test", srv.URL, nil)
	if err != nil {
		t.Fatalf("LoadDefinition: %v", err)
	}

	if len(def.Operations) != 2 {
		t.Fatalf("got %d operations, want 2", len(def.Operations))
	}
}

func TestExtractAuthScopes(t *testing.T) {
	t.Parallel()

	spec := map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]string{"title": "Scoped API"},
		"servers": []any{map[string]string{"url": "https://api.example.com"}},
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"oauth2": map[string]any{
					"type": "oauth2",
					"flows": map[string]any{
						"authorizationCode": map[string]any{
							"authorizationUrl": "https://auth.example.com/authorize",
							"tokenUrl":         "https://auth.example.com/token",
							"scopes": map[string]string{
								"read:data":  "Read data",
								"write:data": "Write data",
							},
						},
					},
				},
			},
		},
		"paths": map[string]any{},
	}

	srv := serveJSON(t, spec)
	testutil.CloseOnCleanup(t, srv)

	def, err := LoadDefinition(context.Background(), "test", srv.URL, nil)
	if err != nil {
		t.Fatalf("LoadDefinition: %v", err)
	}

	if len(def.Auth.Scopes) != 2 {
		t.Fatalf("got %d scopes, want 2: %v", len(def.Auth.Scopes), def.Auth.Scopes)
	}

	scopeSet := make(map[string]bool)
	for _, s := range def.Auth.Scopes {
		scopeSet[s] = true
	}
	if !scopeSet["read:data"] {
		t.Error("missing read:data scope")
	}
	if !scopeSet["write:data"] {
		t.Error("missing write:data scope")
	}
}

func TestExtractAuthNoScopes(t *testing.T) {
	t.Parallel()

	srv := serveJSON(t, testSpec())
	testutil.CloseOnCleanup(t, srv)

	def, err := LoadDefinition(context.Background(), "test", srv.URL, nil)
	if err != nil {
		t.Fatalf("LoadDefinition: %v", err)
	}

	if len(def.Auth.Scopes) != 0 {
		t.Errorf("expected no scopes, got %v", def.Auth.Scopes)
	}
}

func TestCollectScopesFromOperationSecurity(t *testing.T) {
	t.Parallel()

	spec := map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]string{"title": "No Scheme API"},
		"servers": []any{map[string]string{"url": "https://api.example.com"}},
		"paths": map[string]any{
			"/items": map[string]any{
				"get": map[string]any{
					"operationId": "list_items",
					"summary":     "List items",
					"security": []any{
						map[string]any{
							"Oauth2": []string{"read:data", "read:meta"},
						},
					},
				},
			},
			"/items/{id}": map[string]any{
				"post": map[string]any{
					"operationId": "create_item",
					"summary":     "Create item",
					"security": []any{
						map[string]any{
							"Oauth2": []string{"read:data", "write:data"},
						},
					},
				},
			},
		},
	}

	srv := serveJSON(t, spec)
	testutil.CloseOnCleanup(t, srv)

	def, err := LoadDefinition(context.Background(), "test", srv.URL, nil)
	if err != nil {
		t.Fatalf("LoadDefinition: %v", err)
	}

	scopeSet := make(map[string]bool)
	for _, s := range def.Auth.Scopes {
		scopeSet[s] = true
	}
	if len(scopeSet) != 3 {
		t.Fatalf("got %d unique scopes, want 3: %v", len(scopeSet), def.Auth.Scopes)
	}
	for _, want := range []string{"read:data", "read:meta", "write:data"} {
		if !scopeSet[want] {
			t.Errorf("missing scope %q", want)
		}
	}
}

func TestCollectScopesRespectsAllowedOps(t *testing.T) {
	t.Parallel()

	spec := map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]string{"title": "Filtered API"},
		"servers": []any{map[string]string{"url": "https://api.example.com"}},
		"paths": map[string]any{
			"/read": map[string]any{
				"get": map[string]any{
					"operationId": "read_op",
					"summary":     "Read",
					"security": []any{
						map[string]any{"Oauth2": []string{"read:data"}},
					},
				},
			},
			"/admin": map[string]any{
				"post": map[string]any{
					"operationId": "admin_op",
					"summary":     "Admin",
					"security": []any{
						map[string]any{"Oauth2": []string{"admin:all"}},
					},
				},
			},
		},
	}

	srv := serveJSON(t, spec)
	testutil.CloseOnCleanup(t, srv)

	def, err := LoadDefinition(context.Background(), "test", srv.URL, map[string]*config.OperationOverride{"read_op": nil})
	if err != nil {
		t.Fatalf("LoadDefinition: %v", err)
	}

	if len(def.Auth.Scopes) != 1 {
		t.Fatalf("got %d scopes, want 1: %v", len(def.Auth.Scopes), def.Auth.Scopes)
	}
	if def.Auth.Scopes[0] != "read:data" {
		t.Errorf("scope = %q, want %q", def.Auth.Scopes[0], "read:data")
	}
}

func TestExtractAuthAPIKeyHeader(t *testing.T) {
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

	def, err := LoadDefinition(context.Background(), "test", srv.URL, nil)
	if err != nil {
		t.Fatalf("LoadDefinition: %v", err)
	}
	if def.Auth.Type != "manual" {
		t.Errorf("Auth.Type = %q, want manual", def.Auth.Type)
	}
	if def.AuthStyle != "raw" {
		t.Errorf("AuthStyle = %q, want raw", def.AuthStyle)
	}
	if def.AuthHeader != "X-API-Key" {
		t.Errorf("AuthHeader = %q, want X-API-Key", def.AuthHeader)
	}
}

func TestExtractAuthAPIKeyQuery(t *testing.T) {
	t.Parallel()

	spec := map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]string{"title": "Query Key API"},
		"servers": []any{map[string]string{"url": "https://api.example.com"}},
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"apiKey": map[string]any{
					"type": "apiKey",
					"in":   "query",
					"name": "api_key",
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

	def, err := LoadDefinition(context.Background(), "test", srv.URL, nil)
	if err != nil {
		t.Fatalf("LoadDefinition: %v", err)
	}
	if def.Auth.Type != "manual" {
		t.Errorf("Auth.Type = %q, want manual", def.Auth.Type)
	}
	if def.AuthStyle != "raw" {
		t.Errorf("AuthStyle = %q, want raw", def.AuthStyle)
	}
	if def.AuthHeader != "" {
		t.Errorf("AuthHeader = %q, want empty for query auth", def.AuthHeader)
	}
}

func TestExtractAuthHTTPBearer(t *testing.T) {
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

	def, err := LoadDefinition(context.Background(), "test", srv.URL, nil)
	if err != nil {
		t.Fatalf("LoadDefinition: %v", err)
	}
	if def.Auth.Type != "manual" {
		t.Errorf("Auth.Type = %q, want manual", def.Auth.Type)
	}
	if def.AuthStyle != "" {
		t.Errorf("AuthStyle = %q, want empty (bearer is default)", def.AuthStyle)
	}
}

func TestExtractAuthHTTPBasic(t *testing.T) {
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

	def, err := LoadDefinition(context.Background(), "test", srv.URL, nil)
	if err != nil {
		t.Fatalf("LoadDefinition: %v", err)
	}
	if def.Auth.Type != "manual" {
		t.Errorf("Auth.Type = %q, want manual", def.Auth.Type)
	}
	if def.AuthStyle != "" {
		t.Errorf("AuthStyle = %q, want empty (basic deferred to Phase 2)", def.AuthStyle)
	}
}

func TestLoadDefinitionRelativeServerURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		servers []any
		want    string
	}{
		{
			name:    "absolute URL unchanged",
			servers: []any{map[string]string{"url": "https://api.example.com/v1"}},
			want:    "https://api.example.com/v1",
		},
		{
			name:    "relative path resolved against spec URL",
			servers: []any{map[string]string{"url": "/v1"}},
		},
		{
			name:    "no servers leaves BaseURL empty",
			servers: nil,
			want:    "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			spec := map[string]any{
				"openapi": "3.0.0",
				"info":    map[string]string{"title": "Test"},
				"paths":   map[string]any{},
			}
			if tc.servers != nil {
				spec["servers"] = tc.servers
			}

			srv := serveJSON(t, spec)
			testutil.CloseOnCleanup(t, srv)

			want := tc.want
			if tc.name == "relative path resolved against spec URL" {
				want = srv.URL + "/v1"
			}

			def, err := LoadDefinition(context.Background(), "test", srv.URL, nil)
			if err != nil {
				t.Fatalf("LoadDefinition: %v", err)
			}
			if def.BaseURL != want {
				t.Errorf("BaseURL = %q, want %q", def.BaseURL, want)
			}
		})
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
	testutil.CloseOnCleanup(t, srv)

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
