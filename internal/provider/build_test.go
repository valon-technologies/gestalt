package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/config"
)

const minimalProviderYAML = `
provider: %s
display_name: %s
base_url: https://%s.example.com
auth:
  type: manual
operations:
  op:
    description: An operation
    method: GET
    path: /op
`

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
			"list_items": {Description: "List items", Method: "GET", Path: "/items"},
			"get_item":   {Description: "Get item", Method: "GET", Path: "/items/{id}"},
		},
	}
}

func testCreds() config.IntegrationDef {
	return config.IntegrationDef{
		ClientID:     "test",
		ClientSecret: "test",
		RedirectURL:  "http://localhost/callback",
	}
}

func TestBuild(t *testing.T) {
	t.Parallel()

	def := testDefinition("example")
	intg, err := Build(def, testCreds(), nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if intg.Name() != "example" {
		t.Errorf("Name() = %q", intg.Name())
	}
	if len(intg.ListOperations()) != 2 {
		t.Errorf("got %d operations, want 2", len(intg.ListOperations()))
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
			"list": {Description: "List", Method: "GET", Path: "/list"},
		},
	}
	intg, err := Build(def, config.IntegrationDef{}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if intg.Name() != "manual_api" {
		t.Errorf("Name() = %q", intg.Name())
	}
}

func TestBuildWithHooks(t *testing.T) {
	t.Parallel()

	RegisterResponseChecker("test_checker", func(int, []byte) error { return nil })
	RegisterResponseHook("test_hook", func([]byte) error { return nil })

	def := testDefinition("hooked")
	def.ResponseCheck = "test_checker"
	def.Auth.ResponseHook = "test_hook"

	intg, err := Build(def, testCreds(), nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if intg.Name() != "hooked" {
		t.Errorf("Name() = %q", intg.Name())
	}
}

func TestBuildUnknownHook(t *testing.T) {
	t.Parallel()

	def := testDefinition("bad")
	def.ResponseCheck = "nonexistent_hook"

	_, err := Build(def, testCreds(), nil)
	if err == nil {
		t.Fatal("expected error for unknown hook")
	}
}

func TestBuildDoesNotMutateDefinition(t *testing.T) {
	t.Parallel()

	def := testDefinition("original")
	_, err := Build(def, config.IntegrationDef{
		ClientID: "test",
		Auth:     config.AuthOverrides{TokenURL: "https://override.example.com/token"},
	}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if def.Auth.TokenURL != "/oauth/token" {
		t.Errorf("Build mutated original definition: TokenURL = %q", def.Auth.TokenURL)
	}
}

func TestBuildAllowedOperations(t *testing.T) {
	t.Parallel()

	def := testDefinition("filtered")
	intg, err := Build(def, config.IntegrationDef{
		ClientID:     "test",
		ClientSecret: "test",
		RedirectURL:  "http://localhost/callback",
	}, map[string]string{"list_items": ""})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(intg.ListOperations()) != 1 {
		t.Fatalf("got %d operations, want 1", len(intg.ListOperations()))
	}
}

func TestBuildAllowedOperationsUnknown(t *testing.T) {
	t.Parallel()

	def := testDefinition("bad")
	_, err := Build(def, config.IntegrationDef{}, map[string]string{"nonexistent": ""})
	if err == nil {
		t.Fatal("expected error for unknown allowed operation")
	}
}

func TestBuildAllowedOperationsEmpty(t *testing.T) {
	t.Parallel()

	def := testDefinition("bad")
	_, err := Build(def, config.IntegrationDef{}, map[string]string{})
	if err == nil {
		t.Fatal("expected error for empty allowed_operations")
	}
}

func TestBuildUnknownAuthStyle(t *testing.T) {
	t.Parallel()

	def := testDefinition("bad")
	def.AuthStyle = "bogus"
	_, err := Build(def, testCreds(), nil)
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
			"list": {Description: "List", Method: "GET", Path: "/list"},
		},
	}
	intg, err := Build(def, config.IntegrationDef{}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if intg.Name() != "basic_api" {
		t.Errorf("Name() = %q", intg.Name())
	}
}

func TestLoadFromDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeProviderYAML(t, dir, "myapi", "My API")

	def, err := LoadFromDir("myapi", []string{dir})
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	if def.Provider != "myapi" {
		t.Errorf("Provider = %q", def.Provider)
	}
}

func TestLoadFromDir_NotFound(t *testing.T) {
	t.Parallel()

	_, err := LoadFromDir("missing", []string{t.TempDir()})
	if err == nil {
		t.Fatal("expected error for missing provider")
	}
}

func TestLoadFromDir_NilDirs(t *testing.T) {
	t.Parallel()

	_, err := LoadFromDir("anything", nil)
	if err == nil {
		t.Fatal("expected error for nil dirs")
	}
}

func TestBuildSatisfiesCatalogProvider(t *testing.T) {
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
				Method:      "GET",
				Path:        "/things",
				Parameters: []ParameterDef{
					{Name: "limit", Type: "integer", Description: "Max results", Default: 25},
					{Name: "cursor", Type: "string", Description: "Pagination cursor"},
				},
			},
			"create": {
				Description: "Create a thing",
				Method:      "POST",
				Path:        "/things",
				Parameters: []ParameterDef{
					{Name: "name", Type: "string", Required: true},
				},
			},
		},
	}

	provider, err := Build(def, config.IntegrationDef{}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	cp, ok := provider.(core.CatalogProvider)
	if !ok {
		t.Fatal("Build result should satisfy CatalogProvider")
	}

	cat := cp.Catalog()
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
			"op": {Description: "An op", Method: "GET", Path: "/op"},
		},
	}

	prov, err := Build(def, config.IntegrationDef{IconFile: iconPath}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	cp, ok := prov.(core.CatalogProvider)
	if !ok {
		t.Fatal("expected CatalogProvider")
	}
	cat := cp.Catalog()
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
			"op": {Description: "An op", Method: "GET", Path: "/op"},
		},
	}

	prov, err := Build(def, config.IntegrationDef{IconFile: "/nonexistent/icon.svg"}, nil)
	if err != nil {
		t.Fatalf("Build should succeed with missing icon: %v", err)
	}
	cp, ok := prov.(core.CatalogProvider)
	if !ok {
		t.Fatal("expected CatalogProvider")
	}
	if cat := cp.Catalog(); cat != nil && cat.IconSVG != "" {
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
			"list": {Description: "List items", Method: "GET", Path: "/items"},
		},
	}

	prov, err := Build(def, config.IntegrationDef{}, nil)
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
			Headers: map[string]string{
				"DD-API-KEY":         "api_key",
				"DD-APPLICATION-KEY": "app_key",
			},
		},
		Operations: map[string]OperationDef{
			"list": {Description: "List items", Method: "GET", Path: "/items"},
		},
	}

	prov, err := Build(def, config.IntegrationDef{}, nil)
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
			Headers: map[string]string{"X-Key": "missing_field"},
		},
		Operations: map[string]OperationDef{
			"op": {Description: "Op", Method: "GET", Path: "/op"},
		},
	}

	prov, err := Build(def, config.IntegrationDef{}, nil)
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
			"list": {Description: "List", Method: "GET", Path: "/list"},
		},
	}

	prov, err := Build(def, config.IntegrationDef{}, nil)
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
			"op": {Description: "Op", Method: "GET", Path: "/op"},
		},
	}

	prov, err := Build(def, config.IntegrationDef{}, nil)
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
			"op": {Description: "Op", Method: "GET", Path: "/op"},
		},
	}

	intg := config.IntegrationDef{
		AuthHeader: "X-Override-Key",
	}

	prov, err := Build(def, intg, nil)
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

func writeProviderYAML(t *testing.T, dir, name, displayName string) {
	t.Helper()
	content := fmt.Sprintf(minimalProviderYAML, name, displayName, name)
	if err := os.WriteFile(filepath.Join(dir, name+".yaml"), []byte(content), 0644); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
}
