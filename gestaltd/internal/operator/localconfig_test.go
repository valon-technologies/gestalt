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
		return
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
		return
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
		return
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
		return
	}
	wantExternalCredentialsPath := filepath.Join(providersDir, "externalcredentials", "default", "manifest.yaml")
	if got := externalCredentials.SourcePath(); got != wantExternalCredentialsPath {
		t.Fatalf(`Providers.ExternalCredentials["default"].Source.Path = %q, want %q`, got, wantExternalCredentialsPath)
	}
}

func TestResolveStartConfigPathsGeneratesDefaultLocalSourceConfig(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	workdir := filepath.Join(root, "work")
	providersDir := filepath.Join(root, "providers")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("MkdirAll home: %v", err)
	}
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatalf("MkdirAll workdir: %v", err)
	}

	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("GESTALT_CONFIG", "")
	t.Setenv("GESTALT_PROVIDERS_DIR", providersDir)

	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(workdir); err != nil {
		t.Fatalf("Chdir workdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	paths, err := ResolveStartConfigPaths(nil)
	if err != nil {
		t.Fatalf("ResolveStartConfigPaths: %v", err)
	}
	wantConfigPath := filepath.Join(home, localConfigDirName, "config.yaml")
	if len(paths) != 1 || paths[0] != wantConfigPath {
		t.Fatalf("ResolveStartConfigPaths = %v, want [%s]", paths, wantConfigPath)
	}

	cfg, err := config.Load(wantConfigPath)
	if err != nil {
		t.Fatalf("Load(%s): %v", wantConfigPath, err)
	}
	rootUI := cfg.Providers.UI["root"]
	if rootUI == nil {
		t.Fatal(`Providers.UI["root"] = nil`)
	}
	wantUIPath := config.DefaultLocalProviderManifestPath(providersDir, config.DefaultUIProvider)
	if got := rootUI.SourcePath(); got != wantUIPath {
		t.Fatalf(`Providers.UI["root"].Source.Path = %q, want %q`, got, wantUIPath)
	}
	if got := rootUI.Path; got != "/" {
		t.Fatalf(`Providers.UI["root"].Path = %q, want %q`, got, "/")
	}
	if len(cfg.Apps) != 0 {
		t.Fatalf("Apps = %#v, want empty", cfg.Apps)
	}
}
