package operator

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/pluginpkg"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

func mustBuildTestPluginDir(t *testing.T, dir, source, version, content string) string {
	t.Helper()

	srcDir := filepath.Join(dir, "plugin-src")
	artifactRel := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "provider"))
	if err := os.MkdirAll(filepath.Join(srcDir, filepath.Dir(filepath.FromSlash(artifactRel))), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, filepath.FromSlash(artifactRel)), []byte(content), 0755); err != nil {
		t.Fatalf("WriteFile artifact: %v", err)
	}
	manifest := &pluginmanifestv1.Manifest{
		Source:   source,
		Version:  version,
		Kinds:    []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{},
		Artifacts: []pluginmanifestv1.Artifact{
			{
				OS:     runtime.GOOS,
				Arch:   runtime.GOARCH,
				Path:   artifactRel,
				SHA256: sha256hex(content),
			},
		},
		Entrypoints: pluginmanifestv1.Entrypoints{
			Provider: &pluginmanifestv1.Entrypoint{ArtifactPath: artifactRel},
		},
	}
	data, err := pluginpkg.EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("EncodeManifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, pluginpkg.ManifestFile), data, 0644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "catalog.yaml"), []byte("name: provider\noperations:\n  - id: echo\n    method: POST\n"), 0644); err != nil {
		t.Fatalf("WriteFile catalog: %v", err)
	}
	return srcDir
}

func writeTestConfig(t *testing.T, dir, packagePath string) string {
	t.Helper()

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := `providers:
  example:
    from:
      package: ` + packagePath + `
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}
	return cfgPath
}

func TestLoadForExecutionAtPath_ResolvesLocalManifestCommandPluginWithoutLockfile(t *testing.T) {
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
      command: /tmp/provider
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
	if intg.Plugin.IsDeclarative {
		t.Fatal("expected manifest-backed command plugin to remain executable")
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
	writeTestFile(filepath.ToSlash(filepath.Join("provider", "provider.go")), []byte(testutil.GeneratedProviderPackageSource()), 0o644)
	manifest, err := pluginpkg.EncodeSourceManifestFormat(&pluginmanifestv1.Manifest{
		Source:      "github.com/testowner/plugins/local-generated-provider",
		Version:     "0.1.0",
		DisplayName: "Generated Local Provider",
		Kinds:       []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			Auth:    &pluginmanifestv1.ProviderAuth{Type: pluginmanifestv1.AuthTypeNone},
			BaseURL: "https://example.com",
			Operations: []pluginmanifestv1.ProviderOperation{
				{Name: "ping", Method: "GET", Path: "/ping"},
			},
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
	if intg.Plugin.ResolvedManifest.Provider == nil || intg.Plugin.ResolvedManifest.Provider.BaseURL != "https://example.com" {
		t.Fatalf("provider base_url = %#v", intg.Plugin.ResolvedManifest.Provider)
	}
	if len(intg.Plugin.ResolvedManifest.Provider.Operations) != 1 || intg.Plugin.ResolvedManifest.Provider.Operations[0].Name != "ping" {
		t.Fatalf("provider operations = %+v", intg.Plugin.ResolvedManifest.Provider.Operations)
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
      command: /tmp/provider
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

func TestInitAtPath_WritesLockfileWithPluginEntry(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginDir := mustBuildTestPluginDir(t, dir, "github.com/testowner/plugins/provider", "0.1.0", "init-test-binary")
	cfgPath := writeTestConfig(t, dir, pluginDir)

	lc := NewLifecycle(nil)
	lock, err := lc.InitAtPath(cfgPath)
	if err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}

	key := LockPluginKey("integration", "example")
	entry, ok := lock.Plugins[key]
	if !ok {
		t.Fatalf("lockfile missing plugin entry %q: %+v", key, lock.Plugins)
	}
	wantPackage := filepath.ToSlash(filepath.Base(pluginDir))
	if entry.Package != wantPackage {
		t.Fatalf("entry.Package = %q, want %q", entry.Package, wantPackage)
	}
	if entry.Manifest == "" {
		t.Fatal("entry.Manifest is empty")
	}
	if entry.Executable == "" {
		t.Fatal("entry.Executable is empty")
	}
	if entry.Fingerprint == "" {
		t.Fatal("entry.Fingerprint is empty")
	}

	lockPath := filepath.Join(dir, InitLockfileName)
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("lockfile not written: %v", err)
	}
}

func TestLockMatchesConfig_TrueAfterInit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginDir := mustBuildTestPluginDir(t, dir, "github.com/testowner/plugins/provider", "0.1.0", "match-test-binary")
	cfgPath := writeTestConfig(t, dir, pluginDir)

	lc := NewLifecycle(nil)
	if _, err := lc.InitAtPath(cfgPath); err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load config: %v", err)
	}
	paths := initPathsForConfig(cfgPath)
	lock, err := ReadLockfile(paths.lockfilePath)
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}

	if !lockMatchesConfig(cfg, paths, lock) {
		t.Fatal("lockMatchesConfig returned false after init")
	}
}

func TestLockMatchesConfig_FalseWhenPackageChanged(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginDir := mustBuildTestPluginDir(t, dir, "github.com/testowner/plugins/provider", "0.1.0", "original-binary")
	cfgPath := writeTestConfig(t, dir, pluginDir)

	lc := NewLifecycle(nil)
	if _, err := lc.InitAtPath(cfgPath); err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}

	altDir := filepath.Join(dir, "alt-plugin")
	if err := os.MkdirAll(altDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	cfg := `providers:
  example:
    from:
      package: ` + altDir + `
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	loadedCfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load config: %v", err)
	}
	paths := initPathsForConfig(cfgPath)
	lock, err := ReadLockfile(paths.lockfilePath)
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}

	if lockMatchesConfig(loadedCfg, paths, lock) {
		t.Fatal("lockMatchesConfig returned true after package path changed")
	}
}

func TestLockMatchesConfig_FalseWhenManifestMissing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginDir := mustBuildTestPluginDir(t, dir, "github.com/testowner/plugins/provider", "0.1.0", "manifest-test-binary")
	cfgPath := writeTestConfig(t, dir, pluginDir)

	lc := NewLifecycle(nil)
	if _, err := lc.InitAtPath(cfgPath); err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load config: %v", err)
	}
	paths := initPathsForConfig(cfgPath)
	lock, err := ReadLockfile(paths.lockfilePath)
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}

	key := LockPluginKey("integration", "example")
	entry := lock.Plugins[key]
	manifestPath := resolveLockPath(paths.configDir, entry.Manifest)
	if err := os.Remove(manifestPath); err != nil {
		t.Fatalf("Remove manifest: %v", err)
	}

	if lockMatchesConfig(cfg, paths, lock) {
		t.Fatal("lockMatchesConfig returned true when manifest file is missing")
	}
}

func TestLockMatchesConfig_FalseWhenExecutableMissing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginDir := mustBuildTestPluginDir(t, dir, "github.com/testowner/plugins/provider", "0.1.0", "exec-test-binary")
	cfgPath := writeTestConfig(t, dir, pluginDir)

	lc := NewLifecycle(nil)
	if _, err := lc.InitAtPath(cfgPath); err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load config: %v", err)
	}
	paths := initPathsForConfig(cfgPath)
	lock, err := ReadLockfile(paths.lockfilePath)
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}

	key := LockPluginKey("integration", "example")
	entry := lock.Plugins[key]
	execPath := resolveLockPath(paths.configDir, entry.Executable)
	if err := os.Remove(execPath); err != nil {
		t.Fatalf("Remove executable: %v", err)
	}

	if lockMatchesConfig(cfg, paths, lock) {
		t.Fatal("lockMatchesConfig returned true when executable is missing")
	}
}

func TestLockMatchesConfig_FalseWhenFingerprintChanged(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginDir := mustBuildTestPluginDir(t, dir, "github.com/testowner/plugins/provider", "0.1.0", "fp-test-binary")
	cfgPath := writeTestConfig(t, dir, pluginDir)

	lc := NewLifecycle(nil)
	if _, err := lc.InitAtPath(cfgPath); err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load config: %v", err)
	}
	paths := initPathsForConfig(cfgPath)
	lock, err := ReadLockfile(paths.lockfilePath)
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}

	key := LockPluginKey("integration", "example")
	entry := lock.Plugins[key]
	entry.Fingerprint = "0000000000000000000000000000000000000000000000000000000000000000"
	lock.Plugins[key] = entry
	if err := WriteLockfile(paths.lockfilePath, lock); err != nil {
		t.Fatalf("WriteLockfile: %v", err)
	}

	lock2, err := ReadLockfile(paths.lockfilePath)
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}
	if lockMatchesConfig(cfg, paths, lock2) {
		t.Fatal("lockMatchesConfig returned true after fingerprint was corrupted")
	}
}

func TestLockMatchesConfig_FalseWhenStaticCatalogChanged(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginDir := mustBuildTestPluginDir(t, dir, "github.com/testowner/plugins/provider", "0.1.0", "catalog-test-binary")
	cfgPath := writeTestConfig(t, dir, pluginDir)

	lc := NewLifecycle(nil)
	if _, err := lc.InitAtPath(cfgPath); err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}

	if err := os.WriteFile(filepath.Join(pluginDir, "catalog.yaml"), []byte("name: provider\noperations:\n  - id: greet\n    method: GET\n"), 0644); err != nil {
		t.Fatalf("WriteFile catalog: %v", err)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load config: %v", err)
	}
	paths := initPathsForConfig(cfgPath)
	lock, err := ReadLockfile(paths.lockfilePath)
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}

	if lockMatchesConfig(cfg, paths, lock) {
		t.Fatal("lockMatchesConfig returned true after static catalog changed")
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
		Package: "./test-plugin-dir",
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
		Package: "./test-plugin-dir",
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
				Package:     "./test-plugin",
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
	if got.Plugins["integration:example"].Package != want.Plugins["integration:example"].Package {
		t.Fatal("plugin package mismatch")
	}
}
