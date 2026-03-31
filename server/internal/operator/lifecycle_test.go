package operator

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/pluginpkg"
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
	return srcDir
}

func writeTestConfig(t *testing.T, dir, packagePath string) string {
	t.Helper()

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := `integrations:
  example:
    plugin:
      package: ` + packagePath + `
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}
	return cfgPath
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
	if entry.Package != pluginDir {
		t.Fatalf("entry.Package = %q, want %q", entry.Package, pluginDir)
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
	cfg := `integrations:
  example:
    plugin:
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
	first, err := PluginFingerprint("example", plugin, nil)
	if err != nil {
		t.Fatalf("PluginFingerprint: %v", err)
	}
	second, err := PluginFingerprint("example", plugin, nil)
	if err != nil {
		t.Fatalf("PluginFingerprint: %v", err)
	}
	if first != second {
		t.Fatalf("fingerprint not stable: %q != %q", first, second)
	}
}

func TestPluginFingerprint_ChangesWithConfig(t *testing.T) {
	t.Parallel()

	plugin := &config.PluginDef{
		Package: "./test-plugin-dir",
	}
	first, err := PluginFingerprint("example", plugin, map[string]any{"key": "one"})
	if err != nil {
		t.Fatalf("PluginFingerprint: %v", err)
	}
	second, err := PluginFingerprint("example", plugin, map[string]any{"key": "two"})
	if err != nil {
		t.Fatalf("PluginFingerprint: %v", err)
	}
	if first == second {
		t.Fatal("fingerprint should differ with different config")
	}
}

func TestPluginFingerprint_ChangesWithName(t *testing.T) {
	t.Parallel()

	plugin := &config.PluginDef{
		Package: "./test-plugin-dir",
	}
	first, err := PluginFingerprint("alpha", plugin, nil)
	if err != nil {
		t.Fatalf("PluginFingerprint: %v", err)
	}
	second, err := PluginFingerprint("beta", plugin, nil)
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
	if got := LockPluginKey("runtime", "worker"); got != "runtime:worker" {
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
				Provider:    ".gestalt/providers/test.json",
			},
		},
		Plugins: map[string]LockPluginEntry{
			"integration:example": {
				Fingerprint: "plugin-fp",
				Package:     "./test-plugin",
				Manifest:    ".gestalt/plugins/integration_example/plugin.json",
				Executable:  ".gestalt/plugins/integration_example/artifacts/darwin/arm64/provider",
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

func TestInitAtPath_RuntimePlugin(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	srcDir := filepath.Join(dir, "runtime-src")
	artifactRel := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "runtime"))
	if err := os.MkdirAll(filepath.Join(srcDir, filepath.Dir(filepath.FromSlash(artifactRel))), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, filepath.FromSlash(artifactRel)), []byte("runtime-binary"), 0755); err != nil {
		t.Fatalf("WriteFile artifact: %v", err)
	}
	manifest := &pluginmanifestv1.Manifest{
		Source:  "github.com/testowner/plugins/runtime",
		Version: "0.1.0",
		Kinds:   []string{pluginmanifestv1.KindRuntime},
		Artifacts: []pluginmanifestv1.Artifact{
			{
				OS:     runtime.GOOS,
				Arch:   runtime.GOARCH,
				Path:   artifactRel,
				SHA256: sha256hex("runtime-binary"),
			},
		},
		Entrypoints: pluginmanifestv1.Entrypoints{
			Runtime: &pluginmanifestv1.Entrypoint{ArtifactPath: artifactRel},
		},
	}
	data, err := pluginpkg.EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("EncodeManifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, pluginpkg.ManifestFile), data, 0644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := `runtimes:
  worker:
    plugin:
      package: ` + srcDir + `
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	lc := NewLifecycle(nil)
	lock, err := lc.InitAtPath(cfgPath)
	if err != nil {
		t.Fatalf("InitAtPath: %v", err)
	}

	key := LockPluginKey("runtime", "worker")
	entry, ok := lock.Plugins[key]
	if !ok {
		t.Fatalf("lockfile missing runtime plugin entry %q", key)
	}
	if entry.Package != srcDir {
		t.Fatalf("entry.Package = %q, want %q", entry.Package, srcDir)
	}
	if entry.Manifest == "" {
		t.Fatal("entry.Manifest is empty")
	}
}
