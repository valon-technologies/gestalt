package operator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/pluginpkg"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

func TestLoadForExecutionAtPath_ResolvesLocalManifestPluginWithoutLockfile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "plugin.yaml")
	manifest, err := pluginpkg.EncodeManifest(&pluginmanifestv1.Manifest{
		Source:      "github.com/testowner/plugins/local-provider",
		Version:     "0.1.0",
		DisplayName: "Local Provider",
		Description: "Local manifest-backed provider",
		Kinds:       []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			Auth:    &pluginmanifestv1.ProviderAuth{Type: pluginmanifestv1.AuthTypeNone},
			BaseURL: "https://example.com",
			Operations: []pluginmanifestv1.ProviderOperation{
				{
					Name:   "ping",
					Method: "GET",
					Path:   "/ping",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("EncodeManifest: %v", err)
	}
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := `auth:
  provider: none
datastore:
  provider: sqlite
  config:
    path: ./gestalt.db
providers:
  example:
    from:
      source:
        path: ./plugin.yaml
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	lc := NewLifecycle(nil)
	loaded, _, err := lc.LoadForExecutionAtPath(cfgPath, false)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath: %v", err)
	}

	intg := loaded.Integrations["example"]
	if intg.DisplayName != "Local Provider" {
		t.Fatalf("DisplayName = %q", intg.DisplayName)
	}
	if intg.Description != "Local manifest-backed provider" {
		t.Fatalf("Description = %q", intg.Description)
	}
	if intg.Plugin == nil || intg.Plugin.ResolvedManifest == nil {
		t.Fatalf("ResolvedManifest = %+v", intg.Plugin)
	}
	if intg.Plugin.ResolvedManifestPath != manifestPath {
		t.Fatalf("ResolvedManifestPath = %q, want %q", intg.Plugin.ResolvedManifestPath, manifestPath)
	}
	if !intg.Plugin.IsDeclarative {
		t.Fatal("expected manifest-backed source plugin to resolve as declarative")
	}
	if _, err := os.Stat(filepath.Join(dir, InitLockfileName)); !os.IsNotExist(err) {
		t.Fatalf("lockfile should not be created, got err=%v", err)
	}
}

func TestLoadForExecutionAtPath_GeneratesStaticCatalogForLocalSourceHybridPlugin(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeTestFile := func(rel string, data []byte, mode os.FileMode) {
		t.Helper()
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", rel, err)
		}
		if err := os.WriteFile(path, data, mode); err != nil {
			t.Fatalf("WriteFile(%s): %v", rel, err)
		}
	}

	writeTestFile("go.mod", []byte(testutil.GeneratedProviderModuleSource(t, "example.com/local-generated-provider")), 0o644)
	writeTestFile("go.sum", testutil.GeneratedProviderModuleSum(t), 0o644)
	writeTestFile("provider.go", []byte(testutil.GeneratedProviderPackageSource()), 0o644)
	manifest, err := pluginpkg.EncodeSourceManifestFormat(&pluginmanifestv1.Manifest{
		Source:      "github.com/testowner/plugins/local-generated-provider",
		Version:     "0.1.0",
		DisplayName: "Generated Local Provider",
		Kinds:       []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			Auth: &pluginmanifestv1.ProviderAuth{Type: pluginmanifestv1.AuthTypeNone},
		},
	}, pluginpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeManifestFormat: %v", err)
	}
	writeTestFile("plugin.yaml", manifest, 0o644)

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := `auth:
  provider: none
datastore:
  provider: sqlite
  config:
    path: ./gestalt.db
providers:
  example:
    from:
      source:
        path: ./plugin.yaml
`
	writeTestFile("config.yaml", []byte(cfg), 0o644)

	lc := NewLifecycle(nil)
	loaded, _, err := lc.LoadForExecutionAtPath(cfgPath, false)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPath: %v", err)
	}

	intg := loaded.Integrations["example"]
	if intg.Plugin == nil || intg.Plugin.ResolvedManifest == nil {
		t.Fatalf("ResolvedManifest = %+v", intg.Plugin)
	}
	if intg.Plugin.IsDeclarative {
		t.Fatal("expected executable manifest-backed plugin to remain non-declarative")
	}
	catalogData, err := os.ReadFile(filepath.Join(dir, "catalog.yaml"))
	if err != nil {
		t.Fatalf("ReadFile(catalog.yaml): %v", err)
	}
	if !strings.Contains(string(catalogData), "generated_op") {
		t.Fatalf("unexpected catalog contents: %s", catalogData)
	}
	if _, err := os.Stat(filepath.Join(dir, InitLockfileName)); !os.IsNotExist(err) {
		t.Fatalf("lockfile should not be created, got err=%v", err)
	}
}

func TestApplyLockedPlugins_SkipsNilIntegrationPlugins(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "plugin.yaml")
	manifest, err := pluginpkg.EncodeManifest(&pluginmanifestv1.Manifest{
		Source:      "github.com/testowner/plugins/local-provider",
		Version:     "0.1.0",
		DisplayName: "Local Provider",
		Kinds:       []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			Auth:    &pluginmanifestv1.ProviderAuth{Type: pluginmanifestv1.AuthTypeNone},
			BaseURL: "https://example.com",
			Operations: []pluginmanifestv1.ProviderOperation{
				{Name: "ping", Method: "GET", Path: "/ping"},
			},
		},
	})
	if err != nil {
		t.Fatalf("EncodeManifest: %v", err)
	}
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := `auth:
  provider: none
datastore:
  provider: sqlite
  config:
    path: ./gestalt.db
providers:
  example:
    from:
      source:
        path: ./plugin.yaml
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load config: %v", err)
	}
	loaded.Integrations["missing"] = config.IntegrationDef{}

	lc := NewLifecycle(nil)
	if err := lc.applyLockedPlugins(cfgPath, "", loaded, false); err != nil {
		t.Fatalf("applyLockedPlugins: %v", err)
	}
	if loaded.Integrations["example"].Plugin == nil || loaded.Integrations["example"].Plugin.ResolvedManifest == nil {
		t.Fatalf("ResolvedManifest = %+v", loaded.Integrations["example"].Plugin)
	}
}

func TestLockMatchesConfig_FalseWithNilLock(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("server:\n  port: 8080\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	paths := initPathsForConfig(cfgPath)

	if lockMatchesConfig(cfg, paths, nil) {
		t.Fatal("lockMatchesConfig returned true for nil lock")
	}
}

func TestPluginFingerprint_Stable(t *testing.T) {
	t.Parallel()

	plugin := &config.PluginDef{
		Source: &config.PluginSourceDef{Ref: "github.com/test-org/test-repo/test-plugin", Version: "1.0.0"},
	}
	first, err := PluginFingerprint("example", plugin, ".")
	if err != nil {
		t.Fatalf("PluginFingerprint: %v", err)
	}
	second, err := PluginFingerprint("example", plugin, ".")
	if err != nil {
		t.Fatalf("PluginFingerprint: %v", err)
	}
	if first != second {
		t.Fatalf("fingerprint not stable: %q != %q", first, second)
	}
}

func TestPluginFingerprint_ChangesWithName(t *testing.T) {
	t.Parallel()

	plugin := &config.PluginDef{
		Source: &config.PluginSourceDef{Ref: "github.com/test-org/test-repo/test-plugin", Version: "1.0.0"},
	}
	first, err := PluginFingerprint("alpha", plugin, ".")
	if err != nil {
		t.Fatalf("PluginFingerprint: %v", err)
	}
	second, err := PluginFingerprint("beta", plugin, ".")
	if err != nil {
		t.Fatalf("PluginFingerprint: %v", err)
	}
	if first == second {
		t.Fatal("fingerprint should differ with different name")
	}
}

func TestLockPluginKey(t *testing.T) {
	t.Parallel()

	if got := LockPluginKey("integration", "example"); got != "integration:example" {
		t.Fatalf("LockPluginKey = %q", got)
	}
}

func TestReadLockfile_RejectsUnsupportedVersion(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	lockPath := filepath.Join(dir, InitLockfileName)
	lock := &Lockfile{
		Version:   999,
		Providers: make(map[string]LockProviderEntry),
		Plugins:   make(map[string]LockPluginEntry),
	}
	if err := WriteLockfile(lockPath, lock); err != nil {
		t.Fatalf("WriteLockfile: %v", err)
	}

	_, err := ReadLockfile(lockPath)
	if err == nil {
		t.Fatal("expected error for unsupported lockfile version")
	}
	if !strings.Contains(err.Error(), "unsupported lockfile version") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadWriteLockfile_RoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	lockPath := filepath.Join(dir, InitLockfileName)
	want := &Lockfile{
		Version: LockVersion,
		Providers: map[string]LockProviderEntry{
			"test-provider": {
				Fingerprint: "test-fp",
				Provider:    ".gestaltd/providers/test.json",
			},
		},
		Plugins: map[string]LockPluginEntry{
			"integration:example": {
				Fingerprint: "plugin-fp",
				Source:      "github.com/test-org/test-repo/test-plugin",
				Version:     "1.0.0",
				Manifest:    ".gestaltd/plugins/integration_example/plugin.json",
				Executable:  ".gestaltd/plugins/integration_example/artifacts/darwin/arm64/provider",
			},
		},
	}
	if err := WriteLockfile(lockPath, want); err != nil {
		t.Fatalf("WriteLockfile: %v", err)
	}

	got, err := ReadLockfile(lockPath)
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}
	if got.Version != want.Version {
		t.Fatalf("Version = %d, want %d", got.Version, want.Version)
	}
	if got.Providers["test-provider"].Fingerprint != want.Providers["test-provider"].Fingerprint {
		t.Fatal("provider fingerprint mismatch")
	}
	if got.Plugins["integration:example"].Source != want.Plugins["integration:example"].Source || got.Plugins["integration:example"].Version != want.Plugins["integration:example"].Version {
		t.Fatal("plugin source mismatch")
	}
}
