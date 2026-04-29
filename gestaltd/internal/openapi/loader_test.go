package openapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/provider"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
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
					"tags":        []any{"inventory"},
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
		"list_items": {Description: "List items with pagination", Tags: []string{"pagination"}},
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
	if got, want := listOp.Tags, []string{"inventory", "pagination"}; !slices.Equal(got, want) {
		t.Errorf("list_items tags = %#v, want %#v", got, want)
	}
	if len(listOp.Parameters) != 1 {
		t.Fatalf("list_items params = %d, want 1", len(listOp.Parameters))
	}
	if listOp.Parameters[0].Location != "query" {
		t.Errorf("list_items param location = %q, want query", listOp.Parameters[0].Location)
	}

	getOp := def.Operations["get_item"]
	if getOp.Description != "Get an item by ID" {
		t.Errorf("get_item description = %q, want spec default", getOp.Description)
	}
	if len(getOp.Parameters) != 1 {
		t.Fatalf("get_item params = %d, want 1", len(getOp.Parameters))
	}
	if getOp.Parameters[0].Location != "path" {
		t.Errorf("get_item param location = %q, want path", getOp.Parameters[0].Location)
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

func TestExtractAuth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		schemeName string
		scheme     map[string]any
		wantStyle  string
		wantHeader string
	}{
		{
			name:       "api key header",
			schemeName: "apiKey",
			scheme: map[string]any{
				"type": "apiKey",
				"in":   "header",
				"name": "X-API-Key",
			},
			wantStyle:  "raw",
			wantHeader: "X-API-Key",
		},
		{
			name:       "api key query",
			schemeName: "apiKey",
			scheme: map[string]any{
				"type": "apiKey",
				"in":   "query",
				"name": "api_key",
			},
			wantStyle: "raw",
		},
		{
			name:       "http bearer",
			schemeName: "bearerAuth",
			scheme: map[string]any{
				"type":   "http",
				"scheme": "bearer",
			},
		},
		{
			name:       "http basic",
			schemeName: "basicAuth",
			scheme: map[string]any{
				"type":   "http",
				"scheme": "basic",
			},
			wantStyle: "basic",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			spec := map[string]any{
				"openapi": "3.0.0",
				"info":    map[string]string{"title": tc.name},
				"servers": []any{map[string]string{"url": "https://api.example.com"}},
				"components": map[string]any{
					"securitySchemes": map[string]any{tc.schemeName: tc.scheme},
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
			if def.AuthStyle != tc.wantStyle {
				t.Errorf("AuthStyle = %q, want %q", def.AuthStyle, tc.wantStyle)
			}
			if def.AuthHeader != tc.wantHeader {
				t.Errorf("AuthHeader = %q, want %q", def.AuthHeader, tc.wantHeader)
			}
		})
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

func TestLoadDefinitionBodyParamDedup(t *testing.T) {
	t.Parallel()

	spec := map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]string{"title": "Test"},
		"servers": []any{map[string]string{"url": "https://api.example.com"}},
		"paths": map[string]any{
			"/variables/{name}": map[string]any{
				"patch": map[string]any{
					"operationId": "update_variable",
					"summary":     "Update a variable",
					"parameters": []any{
						map[string]any{
							"name": "name", "in": "path", "required": true,
							"schema": map[string]any{"type": "string"},
						},
					},
					"requestBody": map[string]any{
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{
									"type":     "object",
									"required": []string{"name", "value"},
									"properties": map[string]any{
										"name":  map[string]any{"type": "string", "description": "Variable name"},
										"value": map[string]any{"type": "string", "description": "Variable value"},
									},
								},
							},
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

	op, ok := def.Operations["update_variable"]
	if !ok {
		t.Fatal("missing update_variable operation")
	}

	nameCount := 0
	hasValue := false
	for _, p := range op.Parameters {
		if p.Name == "name" {
			nameCount++
		}
		if p.Name == "value" {
			hasValue = true
		}
	}
	if nameCount != 1 {
		t.Errorf("expected 1 'name' parameter, got %d", nameCount)
	}
	if !hasValue {
		t.Error("expected 'value' body property to be included")
	}

	byName := make(map[string]provider.ParameterDef, len(op.Parameters))
	for _, p := range op.Parameters {
		byName[p.Name] = p
	}
	if byName["name"].Location != "path" {
		t.Errorf("name location = %q, want path", byName["name"].Location)
	}
	if byName["value"].Location != "body" {
		t.Errorf("value location = %q, want body", byName["value"].Location)
	}
}

func TestLoadDefinitionNormalizesModelUnsafeParamNames(t *testing.T) {
	t.Parallel()

	spec := map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]string{"title": "Unsafe Names API"},
		"servers": []any{map[string]string{"url": "https://api.example.com"}},
		"paths": map[string]any{
			"/items": map[string]any{
				"post": map[string]any{
					"operationId": "create_item",
					"summary":     "Create item",
					"parameters": []any{
						map[string]any{
							"name": "$select", "in": "query",
							"schema": map[string]any{"type": "string"},
						},
						map[string]any{
							"name": "page[size]", "in": "query",
							"schema": map[string]any{"type": "integer"},
						},
						map[string]any{
							"name": "'x-Cwd'", "in": "header",
							"schema": map[string]any{"type": "string"},
						},
					},
					"requestBody": map[string]any{
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{
									"type":     "object",
									"required": []string{"@odata.type"},
									"properties": map[string]any{
										"@odata.type":   map[string]any{"type": "string"},
										"dollar_select": map[string]any{"type": "string"},
									},
								},
							},
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

	op := def.Operations["create_item"]
	byName := make(map[string]provider.ParameterDef, len(op.Parameters))
	for _, p := range op.Parameters {
		byName[p.Name] = p
	}

	assertParam := func(name, wireName, location string, required bool) {
		t.Helper()
		param, ok := byName[name]
		if !ok {
			t.Fatalf("missing parameter %q; got %#v", name, byName)
		}
		if param.WireName != wireName {
			t.Errorf("%s wireName = %q, want %q", name, param.WireName, wireName)
		}
		if param.Location != location {
			t.Errorf("%s location = %q, want %q", name, param.Location, location)
		}
		if param.Required != required {
			t.Errorf("%s required = %v, want %v", name, param.Required, required)
		}
	}

	assertParam("dollar_select", "$select", "query", false)
	assertParam("page_size", "page[size]", "query", false)
	assertParam("x-Cwd", "'x-Cwd'", "header", false)
	assertParam("at_odata.type", "@odata.type", "body", true)
	assertParam("dollar_select_2", "dollar_select", "body", false)
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
