package provider

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/valon-technologies/toolshed/internal/config"
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
	intg, err := Build(def, testCreds())
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
	intg, err := Build(def, config.IntegrationDef{})
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

	intg, err := Build(def, testCreds())
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

	_, err := Build(def, testCreds())
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
	})
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
		ClientID:          "test",
		ClientSecret:      "test",
		RedirectURL:       "http://localhost/callback",
		AllowedOperations: map[string]string{"list_items": ""},
	})
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
	_, err := Build(def, config.IntegrationDef{
		AllowedOperations: map[string]string{"nonexistent": ""},
	})
	if err == nil {
		t.Fatal("expected error for unknown allowed operation")
	}
}

func TestBuildAllowedOperationsEmpty(t *testing.T) {
	t.Parallel()

	def := testDefinition("bad")
	_, err := Build(def, config.IntegrationDef{
		AllowedOperations: map[string]string{},
	})
	if err == nil {
		t.Fatal("expected error for empty allowed_operations")
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

func writeProviderYAML(t *testing.T, dir, name, displayName string) {
	t.Helper()
	content := fmt.Sprintf(minimalProviderYAML, name, displayName, name)
	if err := os.WriteFile(filepath.Join(dir, name+".yaml"), []byte(content), 0644); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
}
