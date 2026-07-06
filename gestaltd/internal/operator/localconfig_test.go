package operator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

func TestDefaultManagedConfigIncludesRootStaticApp(t *testing.T) {
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

	rootApp := cfg.Apps["root"]
	if rootApp == nil {
		t.Fatal(`Apps["root"] = nil`)
		return
	}
	wantURL := config.DefaultProviderMetadataURL(config.DefaultRootAppProvider, config.DefaultRootAppVersion)
	if got := rootApp.SourceMetadataURL(); got != wantURL {
		t.Fatalf(`Apps["root"].SourceMetadataURL() = %q, want %q`, got, wantURL)
	}
	if rootApp.Static == nil {
		t.Fatal(`Apps["root"].Static = nil`)
		return
	}
	if got := rootApp.Static.Mount; got != "/" {
		t.Fatalf(`Apps["root"].Static.Mount = %q, want %q`, got, "/")
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

func TestDefaultLocalSourceConfigIncludesRootStaticApp(t *testing.T) {
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

	rootApp := cfg.Apps["root"]
	if rootApp == nil {
		t.Fatal(`Apps["root"] = nil`)
		return
	}
	wantPath := filepath.Join(providersDir, "app", "default", "manifest.yaml")
	if got := rootApp.SourcePath(); got != wantPath {
		t.Fatalf(`Apps["root"].Source.Path = %q, want %q`, got, wantPath)
	}
	if rootApp.Static == nil {
		t.Fatal(`Apps["root"].Static = nil`)
		return
	}
	if got := rootApp.Static.Mount; got != "/" {
		t.Fatalf(`Apps["root"].Static.Mount = %q, want %q`, got, "/")
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
	rootApp := cfg.Apps["root"]
	if rootApp == nil {
		t.Fatal(`Apps["root"] = nil`)
		return
	}
	wantRootAppPath := config.DefaultLocalProviderManifestPath(providersDir, config.DefaultRootAppProvider)
	if got := rootApp.SourcePath(); got != wantRootAppPath {
		t.Fatalf(`Apps["root"].Source.Path = %q, want %q`, got, wantRootAppPath)
	}
	if rootApp.Static == nil {
		t.Fatal(`Apps["root"].Static = nil`)
		return
	}
	if got := rootApp.Static.Mount; got != "/" {
		t.Fatalf(`Apps["root"].Static.Mount = %q, want %q`, got, "/")
	}
	if len(cfg.Apps) != 1 {
		t.Fatalf("Apps = %#v, want only root static app", cfg.Apps)
	}
}
