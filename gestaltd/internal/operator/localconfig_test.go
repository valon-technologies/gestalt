package operator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

func TestDefaultManagedConfigIncludesRootWebUI(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "gestalt.db")
	configPath := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(configPath, []byte(defaultManagedConfig(dbPath, "server-key")), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	rootUI := cfg.Providers.UI["root"]
	if rootUI == nil {
		t.Fatal(`Providers.UI["root"] = nil`)
	}
	if got := rootUI.SourceRef(); got != config.DefaultWebUIProvider {
		t.Fatalf(`Providers.UI["root"].Source.Ref = %q, want %q`, got, config.DefaultWebUIProvider)
	}
	if got := rootUI.SourceVersion(); got != config.DefaultWebUIVersion {
		t.Fatalf(`Providers.UI["root"].Source.Version = %q, want %q`, got, config.DefaultWebUIVersion)
	}
	if got := rootUI.Path; got != "/" {
		t.Fatalf(`Providers.UI["root"].Path = %q, want %q`, got, "/")
	}
}

func TestDefaultLocalSourceConfigIncludesRootWebUI(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	providersDir := filepath.Join(dir, "providers")
	dbPath := filepath.Join(dir, "gestalt.db")
	configPath := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(configPath, []byte(defaultLocalSourceConfig(providersDir, dbPath, "server-key")), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	rootUI := cfg.Providers.UI["root"]
	if rootUI == nil {
		t.Fatal(`Providers.UI["root"] = nil`)
	}
	wantPath := filepath.Join(providersDir, "web", "default", "manifest.yaml")
	if got := rootUI.SourcePath(); got != wantPath {
		t.Fatalf(`Providers.UI["root"].Source.Path = %q, want %q`, got, wantPath)
	}
	if got := rootUI.Path; got != "/" {
		t.Fatalf(`Providers.UI["root"].Path = %q, want %q`, got, "/")
	}
}
