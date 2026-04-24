package operator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

func TestDefaultManagedConfigIncludesRootUI(t *testing.T) {
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
	wantURL := config.DefaultProviderMetadataURL(config.DefaultUIProvider, config.DefaultUIVersion)
	if got := rootUI.SourceMetadataURL(); got != wantURL {
		t.Fatalf(`Providers.UI["root"].SourceMetadataURL() = %q, want %q`, got, wantURL)
	}
	if got := rootUI.Path; got != "/" {
		t.Fatalf(`Providers.UI["root"].Path = %q, want %q`, got, "/")
	}

	indexedDB := cfg.Providers.IndexedDB["main"]
	if indexedDB == nil {
		t.Fatal(`Providers.IndexedDB["main"] = nil`)
	}
	wantIndexedDBURL := config.DefaultProviderMetadataURL(config.DefaultIndexedDBProvider, config.DefaultIndexedDBVersion)
	if got := indexedDB.SourceMetadataURL(); got != wantIndexedDBURL {
		t.Fatalf(`Providers.IndexedDB["main"].SourceMetadataURL() = %q, want %q`, got, wantIndexedDBURL)
	}
}

func TestDefaultLocalSourceConfigIncludesRootUI(t *testing.T) {
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
	wantPath := filepath.Join(providersDir, "ui", "default", "manifest.yaml")
	if got := rootUI.SourcePath(); got != wantPath {
		t.Fatalf(`Providers.UI["root"].Source.Path = %q, want %q`, got, wantPath)
	}
	if got := rootUI.Path; got != "/" {
		t.Fatalf(`Providers.UI["root"].Path = %q, want %q`, got, "/")
	}

	externalCredentials := cfg.Providers.ExternalCredentials[config.DefaultProviderInstance]
	if externalCredentials == nil {
		t.Fatal(`Providers.ExternalCredentials["default"] = nil`)
	}
	wantExternalCredentialsPath := filepath.Join(providersDir, "external_credentials", "default", "manifest.yaml")
	if got := externalCredentials.SourcePath(); got != wantExternalCredentialsPath {
		t.Fatalf(`Providers.ExternalCredentials["default"].Source.Path = %q, want %q`, got, wantExternalCredentialsPath)
	}
}
