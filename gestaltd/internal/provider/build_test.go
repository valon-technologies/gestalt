package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func testDefinition(name string) *Definition {
	return &Definition{
		Provider:    name,
		DisplayName: name + " Test",
		Description: "Test integration for " + name,
		BaseURL:     "https://api.example.com",
		Auth: AuthDef{
			Type:             "oauth2",
			AuthorizationURL: "/oauth/authorize",
			TokenURL:         "/oauth/token",
		},
		Operations: map[string]OperationDef{
			"list_items": {Description: "List items", Method: http.MethodGet, Path: "/items"},
			"get_item":   {Description: "Get item", Method: http.MethodGet, Path: "/items/{id}"},
		},
	}
}

func testCreds() config.ConnectionDef {
	return config.ConnectionDef{Auth: config.ConnectionAuthDef{
		ClientID:     "test",
		ClientSecret: "test",
		RedirectURL:  "http://localhost/callback",
	}}
}

func TestBuild(t *testing.T) {
	t.Parallel()

	def := testDefinition("example")
	intg, err := Build(def, testCreds())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if intg.Name() != "example" {
		t.Errorf("Name() = %q", intg.Name())
	}
	if cat := intg.Catalog(); cat == nil || len(cat.Operations) != 2 {
		t.Errorf("got %+v, want 2 operations", cat)
	}
}

func TestBuildManualAuth(t *testing.T) {
	t.Parallel()

	def := &Definition{
		Provider:    "manual_api",
		DisplayName: "Manual API",
		BaseURL:     "https://api.example.com",
		Auth:        AuthDef{Type: "manual"},
		Operations: map[string]OperationDef{
			"list": {Description: "List", Method: http.MethodGet, Path: "/list"},
		},
	}
	intg, err := Build(def, config.ConnectionDef{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if intg.Name() != "manual_api" {
		t.Errorf("Name() = %q", intg.Name())
	}
}

func TestBuildDoesNotMutateDefinition(t *testing.T) {
	t.Parallel()

	def := testDefinition("original")
	_, err := Build(def, config.ConnectionDef{Auth: config.ConnectionAuthDef{
		ClientID: "test",
		TokenURL: "https://override.example.com/token",
	}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if def.Auth.TokenURL != "/oauth/token" {
		t.Errorf("Build mutated original definition: TokenURL = %q", def.Auth.TokenURL)
	}
}

func TestBuildUnknownAuthStyle(t *testing.T) {
	t.Parallel()

	def := testDefinition("bad")
	def.AuthStyle = "bogus"
	_, err := Build(def, testCreds())
	if err == nil {
		t.Fatal("expected error for unknown auth_style")
	}
}

func TestBuildBasicAuthStyle(t *testing.T) {
	t.Parallel()

	def := &Definition{
		Provider:    "basic_api",
		DisplayName: "Basic API",
		BaseURL:     "https://api.example.com",
		Auth:        AuthDef{Type: "manual"},
		AuthStyle:   "basic",
		Operations: map[string]OperationDef{
			"list": {Description: "List", Method: http.MethodGet, Path: "/list"},
		},
	}
	intg, err := Build(def, config.ConnectionDef{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if intg.Name() != "basic_api" {
		t.Errorf("Name() = %q", intg.Name())
	}
}

func TestBuildExposesCatalog(t *testing.T) {
	t.Parallel()

	def := &Definition{
		Provider:    "catprov",
		DisplayName: "Catalog Provider",
		IconSVG:     `<svg viewBox="0 0 24 24"><path d="M12 2L2 22h20z"/></svg>`,
		BaseURL:     "https://api.example.com",
		Auth:        AuthDef{Type: "manual"},
		Operations: map[string]OperationDef{
			"list": {
				Description: "List things",
				Method:      http.MethodGet,
				Path:        "/things",
				Parameters: []ParameterDef{
					{Name: "limit", Type: "integer", Location: "query", Description: "Max results", Default: 25},
					{Name: "page_size", WireName: "page[size]", Type: "integer", Location: "query", Description: "Nested pagination size"},
					{Name: "cursor", Type: "string", Description: "Pagination cursor"},
				},
			},
			"create": {
				Description: "Create a thing",
				Method:      http.MethodPost,
				Path:        "/things",
				Parameters: []ParameterDef{
					{Name: "name", Type: "string", Required: true},
				},
			},
		},
	}

	provider, err := Build(def, config.ConnectionDef{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	cat := provider.Catalog()
	if cat == nil {
		t.Fatal("Catalog() should return *catalog.Catalog")
	}

	if cat.Name != "catprov" {
		t.Errorf("catalog Name = %q", cat.Name)
	}
	if cat.IconSVG != `<svg viewBox="0 0 24 24"><path d="M12 2L2 22h20z"/></svg>` {
		t.Errorf("catalog IconSVG = %q", cat.IconSVG)
	}
	if len(cat.Operations) != 2 {
		t.Fatalf("got %d catalog operations, want 2", len(cat.Operations))
	}

	for _, op := range cat.Operations {
		if op.ID == "list" {
			if len(op.Parameters) != 3 {
				t.Fatalf("list params = %d, want 3", len(op.Parameters))
			}
			if op.Parameters[0].Location != "query" {
				t.Errorf("list limit location = %q, want query", op.Parameters[0].Location)
			}
			if op.Parameters[1].Name != "page_size" {
				t.Errorf("page param name = %q, want page_size", op.Parameters[1].Name)
			}
			if op.Parameters[1].WireName != "page[size]" {
				t.Errorf("page param wire name = %q, want page[size]", op.Parameters[1].WireName)
			}
			if op.Parameters[1].Location != "query" {
				t.Errorf("page param location = %q, want query", op.Parameters[1].Location)
			}
		}
		if op.InputSchema == nil {
			t.Errorf("operation %q should have synthesized InputSchema", op.ID)
			continue
		}
		var schema map[string]any
		if err := json.Unmarshal(op.InputSchema, &schema); err != nil {
			t.Errorf("operation %q InputSchema unmarshal: %v", op.ID, err)
			continue
		}
		if schema["type"] != "object" {
			t.Errorf("operation %q schema type = %v", op.ID, schema["type"])
		}
	}
}

func TestBuildExecuteRoutesQueryParamsUsingCatalogMetadata(t *testing.T) {
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
	t.Cleanup(srv.Close)

	def := &Definition{
		Provider:    "routing",
		DisplayName: "Routing API",
		BaseURL:     srv.URL,
		Auth:        AuthDef{Type: "manual"},
		Operations: map[string]OperationDef{
			"create": {
				Description: "Create a thing",
				Method:      http.MethodPost,
				Path:        "/things",
				Parameters: []ParameterDef{
					{Name: "name", Type: "string", Location: "body", Required: true},
					{Name: "page_size", WireName: "page[size]", Type: "integer", Location: "query"},
				},
			},
		},
	}

	prov, err := Build(def, config.ConnectionDef{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	result, err := prov.Execute(context.Background(), "create", map[string]any{
		"name":      "widget",
		"page_size": 25,
	}, "")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(result.Body), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	query, err := url.ParseQuery(resp["query"].(string))
	if err != nil {
		t.Fatalf("url.ParseQuery: %v", err)
	}
	if query.Get("page[size]") != "25" {
		t.Fatalf("query page[size] = %q, want 25", query.Get("page[size]"))
	}

	body := resp["body"].(map[string]any)
	if body["name"] != "widget" {
		t.Fatalf("body[name] = %v, want widget", body["name"])
	}
	if _, ok := body["page_size"]; ok {
		t.Fatalf("body should not contain page_size when parameter is routed to query")
	}
}

func TestBuildAppliesIconFile(t *testing.T) {
	t.Parallel()

	const svg = `<svg viewBox="0 0 24 24"><rect width="24" height="24"/></svg>`
	iconPath := filepath.Join(t.TempDir(), "test.svg")
	if err := os.WriteFile(iconPath, []byte(svg+"\n"), 0644); err != nil {
		t.Fatalf("writing icon file: %v", err)
	}

	def := &Definition{
		Provider:    "fileicon",
		DisplayName: "File Icon Test",
		BaseURL:     "https://api.example.com",
		Auth:        AuthDef{Type: "manual"},
		Operations: map[string]OperationDef{
			"op": {Description: "An op", Method: http.MethodGet, Path: "/op"},
		},
	}

	iconSVG, err := ReadIconFile(iconPath)
	if err != nil {
		t.Fatalf("ReadIconFile: %v", err)
	}
	def.IconSVG = iconSVG
	prov, err := Build(def, config.ConnectionDef{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	cat := prov.Catalog()
	if cat == nil {
		t.Fatal("expected non-nil Catalog")
	}
	if cat.IconSVG != svg {
		t.Fatalf("expected icon from file, got %q", cat.IconSVG)
	}
}

func TestBuildIconFileMissing(t *testing.T) {
	t.Parallel()

	def := &Definition{
		Provider:    "badicon",
		DisplayName: "Bad Icon Test",
		BaseURL:     "https://api.example.com",
		Auth:        AuthDef{Type: "manual"},
		Operations: map[string]OperationDef{
			"op": {Description: "An op", Method: http.MethodGet, Path: "/op"},
		},
	}

	if _, err := ReadIconFile("/nonexistent/icon.svg"); err == nil {
		t.Fatal("expected ReadIconFile to fail for missing icon")
	}
	prov, err := Build(def, config.ConnectionDef{})
	if err != nil {
		t.Fatalf("Build should succeed with missing icon: %v", err)
	}
	if cat := prov.Catalog(); cat != nil && cat.IconSVG != "" {
		t.Errorf("expected empty IconSVG, got %q", cat.IconSVG)
	}
}
func TestBuildAuthHeader(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"auth":      r.Header.Get("Authorization"),
			"x_api_key": r.Header.Get("X-API-Key"),
		})
	}))
	t.Cleanup(srv.Close)

	def := &Definition{
		Provider:    "custom_header",
		DisplayName: "Custom Header API",
		BaseURL:     srv.URL,
		Auth:        AuthDef{Type: "manual"},
		AuthHeader:  "X-API-Key",
		Operations: map[string]OperationDef{
			"list": {Description: "List items", Method: http.MethodGet, Path: "/items"},
		},
	}

	prov, err := Build(def, config.ConnectionDef{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	result, err := prov.Execute(context.Background(), "list", nil, "my-secret-key")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(result.Body), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if resp["x_api_key"] != "my-secret-key" {
		t.Errorf("X-API-Key = %v, want my-secret-key", resp["x_api_key"])
	}
	if resp["auth"] != "" {
		t.Errorf("Authorization should be empty, got %v", resp["auth"])
	}
}

func TestBuildOAuthConnectionOverrideClearsOpenAPIManualAuth(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"authorization": r.Header.Get("Authorization"),
		})
	}))
	t.Cleanup(srv.Close)

	def := &Definition{
		Provider:    "oauth_override",
		DisplayName: "OAuth Override API",
		BaseURL:     srv.URL,
		Auth:        AuthDef{Type: "manual"},
		AuthStyle:   "raw",
		AuthHeader:  "Authorization",
		CredentialFields: []CredentialFieldDef{
			{Name: "api_key", Label: "API Key"},
		},
		Operations: map[string]OperationDef{
			"list": {Description: "List items", Method: http.MethodGet, Path: "/items"},
		},
	}

	prov, err := Build(def, config.ConnectionDef{
		Auth: config.ConnectionAuthDef{
			Type:             providermanifestv1.AuthTypeOAuth2,
			AuthorizationURL: "https://identity.example.com/oauth/authorize",
			TokenURL:         "https://identity.example.com/oauth/token",
			ClientID:         "client-id",
			ClientSecret:     "client-secret",
			RedirectURL:      "https://gestalt.example.com/api/v1/auth/callback",
		},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	result, err := prov.Execute(context.Background(), "list", nil, "oauth-access-token")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(result.Body), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if resp["authorization"] != "Bearer oauth-access-token" {
		t.Errorf("Authorization = %v, want Bearer oauth-access-token", resp["authorization"])
	}
	if fields := prov.CredentialFields(); len(fields) != 0 {
		t.Fatalf("CredentialFields len = %d, want 0: %+v", len(fields), fields)
	}
}

func TestBuildAuthMapping(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"auth":    r.Header.Get("Authorization"),
			"api_key": r.Header.Get("DD-API-KEY"),
			"app_key": r.Header.Get("DD-APPLICATION-KEY"),
		})
	}))
	t.Cleanup(srv.Close)

	def := &Definition{
		Provider:    "multi_header",
		DisplayName: "Multi Header API",
		BaseURL:     srv.URL,
		Auth:        AuthDef{Type: "manual"},
		AuthMapping: &AuthMappingDef{
			Headers: map[string]AuthValueDef{
				"DD-API-KEY": {
					ValueFrom: &AuthValueFromDef{
						CredentialFieldRef: &CredentialFieldRefDef{Name: "api_key"},
					},
				},
				"DD-APPLICATION-KEY": {
					ValueFrom: &AuthValueFromDef{
						CredentialFieldRef: &CredentialFieldRefDef{Name: "app_key"},
					},
				},
			},
		},
		Operations: map[string]OperationDef{
			"list": {Description: "List items", Method: http.MethodGet, Path: "/items"},
		},
	}

	prov, err := Build(def, config.ConnectionDef{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	token := `{"api_key":"k1","app_key":"k2"}`
	result, err := prov.Execute(context.Background(), "list", nil, token)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(result.Body), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if resp["api_key"] != "k1" {
		t.Errorf("DD-API-KEY = %v, want k1", resp["api_key"])
	}
	if resp["app_key"] != "k2" {
		t.Errorf("DD-APPLICATION-KEY = %v, want k2", resp["app_key"])
	}
	if resp["auth"] != "" {
		t.Errorf("Authorization should be empty, got %v", resp["auth"])
	}
}

func TestBuildAuthMappingMissingField(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	def := &Definition{
		Provider:    "bad_mapping",
		DisplayName: "Bad Mapping API",
		BaseURL:     srv.URL,
		Auth:        AuthDef{Type: "manual"},
		AuthMapping: &AuthMappingDef{
			Headers: map[string]AuthValueDef{
				"X-Key": {
					ValueFrom: &AuthValueFromDef{
						CredentialFieldRef: &CredentialFieldRefDef{Name: "missing_field"},
					},
				},
			},
		},
		Operations: map[string]OperationDef{
			"op": {Description: "Op", Method: http.MethodGet, Path: "/op"},
		},
	}

	prov, err := Build(def, config.ConnectionDef{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	_, err = prov.Execute(context.Background(), "op", nil, `{"other":"val"}`)
	if err == nil {
		t.Fatal("expected error for missing JSON field in auth_mapping")
	}
	if !strings.Contains(err.Error(), "missing_field") {
		t.Errorf("error should mention missing field, got: %v", err)
	}
}

func TestBuildErrorMessagePath(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"message": "invalid parameter: limit",
			},
		})
	}))
	t.Cleanup(srv.Close)

	def := &Definition{
		Provider:         "error_path",
		DisplayName:      "Error Path API",
		BaseURL:          srv.URL,
		Auth:             AuthDef{Type: "manual"},
		ErrorMessagePath: "error.message",
		Operations: map[string]OperationDef{
			"list": {Description: "List", Method: http.MethodGet, Path: "/list"},
		},
	}

	prov, err := Build(def, config.ConnectionDef{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	_, err = prov.Execute(context.Background(), "list", nil, "tok")
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	if !strings.Contains(err.Error(), "invalid parameter: limit") {
		t.Errorf("error should contain extracted message, got: %v", err)
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error should contain status code, got: %v", err)
	}
}

func TestBuildErrorMessagePathSuccessPassthrough(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	t.Cleanup(srv.Close)

	def := &Definition{
		Provider:         "error_path_ok",
		DisplayName:      "Error Path OK API",
		BaseURL:          srv.URL,
		Auth:             AuthDef{Type: "manual"},
		ErrorMessagePath: "error.message",
		Operations: map[string]OperationDef{
			"op": {Description: "Op", Method: http.MethodGet, Path: "/op"},
		},
	}

	prov, err := Build(def, config.ConnectionDef{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	result, err := prov.Execute(context.Background(), "op", nil, "tok")
	if err != nil {
		t.Fatalf("Execute should succeed for 200: %v", err)
	}
	if result.Status != http.StatusOK {
		t.Errorf("status = %d, want 200", result.Status)
	}
}

func TestBuildConfigOverridesAuthHeader(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"custom_header": r.Header.Get("X-Override-Key"),
			"def_header":    r.Header.Get("X-Original-Key"),
		})
	}))
	t.Cleanup(srv.Close)

	def := &Definition{
		Provider:    "override_test",
		DisplayName: "Override Test API",
		BaseURL:     srv.URL,
		Auth:        AuthDef{Type: "manual"},
		AuthHeader:  "X-Original-Key",
		Operations: map[string]OperationDef{
			"op": {Description: "Op", Method: http.MethodGet, Path: "/op"},
		},
	}

	def.AuthHeader = "X-Override-Key"

	prov, err := Build(def, config.ConnectionDef{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	result, err := prov.Execute(context.Background(), "op", nil, "secret")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(result.Body), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if resp["custom_header"] != "secret" {
		t.Errorf("X-Override-Key = %v, want secret", resp["custom_header"])
	}
	if resp["def_header"] != "" {
		t.Errorf("X-Original-Key should be empty, got %v", resp["def_header"])
	}
}

func TestBuildConnectionParams(t *testing.T) {
	t.Parallel()

	def := &Definition{
		Provider:    "shopify_test",
		DisplayName: "Shopify Test",
		BaseURL:     "https://{subdomain}.myshopify.com",
		Auth:        AuthDef{Type: "manual"},
		Connection: map[string]ConnectionParamDef{
			"subdomain": {Required: true, Description: "Store subdomain"},
			"instance_url": {
				From:  "token_response",
				Field: "instance_url",
			},
		},
		Operations: map[string]OperationDef{
			"list_products": {Description: "List products", Method: http.MethodGet, Path: "/products"},
		},
	}

	prov, err := Build(def, config.ConnectionDef{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	defs := prov.ConnectionParamDefs()
	if len(defs) != 2 {
		t.Fatalf("expected 2 connection params, got %d", len(defs))
	}
	if !defs["subdomain"].Required {
		t.Error("subdomain should be required")
	}
	if defs["instance_url"].From != "token_response" {
		t.Errorf("instance_url.From = %q, want token_response", defs["instance_url"].From)
	}
}

func TestBuildConnectionParamsBaseURLInterpolation(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"host": r.Host, "path": r.URL.Path})
	}))
	t.Cleanup(srv.Close)

	def := &Definition{
		Provider:    "dynamic_url_test",
		DisplayName: "Dynamic URL Test",
		BaseURL:     srv.URL,
		Auth:        AuthDef{Type: "manual"},
		Connection: map[string]ConnectionParamDef{
			"subdomain": {Required: true},
		},
		Operations: map[string]OperationDef{
			"op": {Description: "Op", Method: http.MethodGet, Path: "/items"},
		},
	}

	prov, err := Build(def, config.ConnectionDef{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	ctx := core.WithConnectionParams(context.Background(), map[string]string{"subdomain": "test-store"})
	result, err := prov.Execute(ctx, "op", nil, "tok")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != 200 {
		t.Fatalf("unexpected status %d, body: %s", result.Status, result.Body)
	}
}

func TestBuildResponseCheck_SuccessMatch(t *testing.T) {
	t.Parallel()

	const successKey = "ok"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{successKey: true, "data": "hello"})
	}))
	t.Cleanup(srv.Close)

	def := &Definition{
		Provider:    "check_ok",
		DisplayName: "Check OK API",
		BaseURL:     srv.URL,
		Auth:        AuthDef{Type: "manual"},
		ResponseCheck: &ResponseCheckDef{
			SuccessBodyMatch: map[string]any{successKey: true},
			ErrorMessagePath: "error",
		},
		Operations: map[string]OperationDef{
			"op": {Description: "Op", Method: http.MethodGet, Path: "/op"},
		},
	}

	prov, err := Build(def, config.ConnectionDef{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	result, err := prov.Execute(context.Background(), "op", nil, "tok")
	if err != nil {
		t.Fatalf("Execute should succeed: %v", err)
	}
	if result.Status != http.StatusOK {
		t.Errorf("status = %d, want 200", result.Status)
	}
}

func TestBuildResponseCheck_FailureMatch(t *testing.T) {
	t.Parallel()

	const (
		successKey = "ok"
		errorKey   = "error"
		errorValue = "channel_not_found"
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{successKey: false, errorKey: errorValue})
	}))
	t.Cleanup(srv.Close)

	def := &Definition{
		Provider:    "check_fail",
		DisplayName: "Check Fail API",
		BaseURL:     srv.URL,
		Auth:        AuthDef{Type: "manual"},
		ResponseCheck: &ResponseCheckDef{
			SuccessBodyMatch: map[string]any{successKey: true},
			ErrorMessagePath: errorKey,
		},
		Operations: map[string]OperationDef{
			"op": {Description: "Op", Method: http.MethodGet, Path: "/op"},
		},
	}

	prov, err := Build(def, config.ConnectionDef{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	_, err = prov.Execute(context.Background(), "op", nil, "tok")
	if err == nil {
		t.Fatal("expected error for failed response check")
	}
	if !strings.Contains(err.Error(), errorValue) {
		t.Errorf("error should contain %q, got: %v", errorValue, err)
	}
}

func TestBuildResponseCheck_NonJSON200(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("plain text response"))
	}))
	t.Cleanup(srv.Close)

	def := &Definition{
		Provider:    "check_text_ok",
		DisplayName: "Check Text OK API",
		BaseURL:     srv.URL,
		Auth:        AuthDef{Type: "manual"},
		ResponseCheck: &ResponseCheckDef{
			SuccessBodyMatch: map[string]any{"ok": true},
			ErrorMessagePath: "error",
		},
		Operations: map[string]OperationDef{
			"op": {Description: "Op", Method: http.MethodGet, Path: "/op"},
		},
	}

	prov, err := Build(def, config.ConnectionDef{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	_, err = prov.Execute(context.Background(), "op", nil, "tok")
	if err != nil {
		t.Fatalf("Execute should succeed for non-JSON 200: %v", err)
	}
}

func TestBuildResponseCheck_NonJSON500(t *testing.T) {
	t.Parallel()

	const serverErrorStatus = http.StatusInternalServerError

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(serverErrorStatus)
		_, _ = w.Write([]byte("internal server error"))
	}))
	t.Cleanup(srv.Close)

	def := &Definition{
		Provider:    "check_text_err",
		DisplayName: "Check Text Err API",
		BaseURL:     srv.URL,
		Auth:        AuthDef{Type: "manual"},
		ResponseCheck: &ResponseCheckDef{
			SuccessBodyMatch: map[string]any{"ok": true},
			ErrorMessagePath: "error",
		},
		Operations: map[string]OperationDef{
			"op": {Description: "Op", Method: http.MethodGet, Path: "/op"},
		},
	}

	prov, err := Build(def, config.ConnectionDef{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	_, err = prov.Execute(context.Background(), "op", nil, "tok")
	if err == nil {
		t.Fatal("expected error for non-JSON 500")
	}
}

func TestBuildResponseCheck_SuccessMatchOnly(t *testing.T) {
	t.Parallel()

	def := &Definition{
		Provider:    "check_match_only",
		DisplayName: "Check Match Only API",
		BaseURL:     "https://api.example.com",
		Auth:        AuthDef{Type: "manual"},
		ResponseCheck: &ResponseCheckDef{
			SuccessBodyMatch: map[string]any{"ok": true},
		},
		Operations: map[string]OperationDef{
			"op": {Description: "Op", Method: http.MethodGet, Path: "/op"},
		},
	}

	_, err := Build(def, config.ConnectionDef{})
	if err != nil {
		t.Fatalf("Build should succeed with structured response check: %v", err)
	}
}

func TestBuildResponseCheck_ErrorMessagePathOnly(t *testing.T) {
	t.Parallel()

	const (
		msgKey   = "message"
		msgValue = "bad request"
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{msgKey: msgValue})
	}))
	t.Cleanup(srv.Close)

	def := &Definition{
		Provider:    "check_errpath",
		DisplayName: "Check ErrPath API",
		BaseURL:     srv.URL,
		Auth:        AuthDef{Type: "manual"},
		ResponseCheck: &ResponseCheckDef{
			ErrorMessagePath: msgKey,
		},
		Operations: map[string]OperationDef{
			"op": {Description: "Op", Method: http.MethodGet, Path: "/op"},
		},
	}

	prov, err := Build(def, config.ConnectionDef{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	_, err = prov.Execute(context.Background(), "op", nil, "tok")
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	if !strings.Contains(err.Error(), msgValue) {
		t.Errorf("error should contain %q, got: %v", msgValue, err)
	}
}

func TestBuildCredentialFields(t *testing.T) {
	t.Parallel()

	def := &Definition{
		Provider:    "cred_test",
		DisplayName: "Credential Test",
		BaseURL:     "https://api.example.com",
		Auth:        AuthDef{Type: "manual"},
		CredentialFields: []CredentialFieldDef{
			{Name: "api_key", Label: "API Key"},
			{Name: "app_key", Label: "App Key", Description: "Your application key"},
		},
		Operations: map[string]OperationDef{
			"list": {Description: "List", Method: http.MethodGet, Path: "/items"},
		},
	}

	prov, err := Build(def, config.ConnectionDef{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	fields := prov.CredentialFields()
	if len(fields) != 2 {
		t.Fatalf("got %d credential fields, want 2", len(fields))
	}
	if fields[0].Name != "api_key" || fields[0].Label != "API Key" {
		t.Errorf("field[0] = %+v", fields[0])
	}
	if fields[1].Name != "app_key" || fields[1].Description != "Your application key" {
		t.Errorf("field[1] = %+v", fields[1])
	}
}

func TestBuildCredentialFieldsFromConfig(t *testing.T) {
	t.Parallel()

	def := &Definition{
		Provider:    "cred_cfg_test",
		DisplayName: "Credential Config Test",
		BaseURL:     "https://api.example.com",
		Auth:        AuthDef{Type: "manual"},
		Operations: map[string]OperationDef{
			"list": {Description: "List", Method: http.MethodGet, Path: "/items"},
		},
	}

	conn := config.ConnectionDef{
		Auth: config.ConnectionAuthDef{
			Credentials: []config.CredentialFieldDef{
				{Name: "token", Label: "Access Token"},
			},
		},
	}

	prov, err := Build(def, conn)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	fields := prov.CredentialFields()
	if len(fields) != 1 {
		t.Fatalf("got %d credential fields, want 1", len(fields))
	}
	if fields[0].Name != "token" || fields[0].Label != "Access Token" {
		t.Errorf("field = %+v", fields[0])
	}
}

func TestBuildAuthMappingFromConfig(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"api_key": r.Header.Get("X-Api-Key"),
			"app_key": r.Header.Get("X-App-Key"),
		})
	}))
	t.Cleanup(srv.Close)

	def := &Definition{
		Provider:    "cfg_mapping_test",
		DisplayName: "Config Mapping Test",
		BaseURL:     srv.URL,
		Auth:        AuthDef{Type: "manual"},
		Operations: map[string]OperationDef{
			"list": {Description: "List", Method: http.MethodGet, Path: "/items"},
		},
	}

	conn := config.ConnectionDef{
		Auth: config.ConnectionAuthDef{
			AuthMapping: &config.AuthMappingDef{
				Headers: map[string]config.AuthValueDef{
					"X-Api-Key": {
						ValueFrom: &config.AuthValueFromDef{
							CredentialFieldRef: &config.CredentialFieldRefDef{Name: "api_key"},
						},
					},
					"X-App-Key": {
						ValueFrom: &config.AuthValueFromDef{
							CredentialFieldRef: &config.CredentialFieldRefDef{Name: "app_key"},
						},
					},
				},
			},
		},
	}

	prov, err := Build(def, conn)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	token := `{"api_key":"k1","app_key":"k2"}`
	result, err := prov.Execute(context.Background(), "list", nil, token)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(result.Body), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if resp["api_key"] != "k1" {
		t.Errorf("X-Api-Key = %v, want k1", resp["api_key"])
	}
	if resp["app_key"] != "k2" {
		t.Errorf("X-App-Key = %v, want k2", resp["app_key"])
	}
}
