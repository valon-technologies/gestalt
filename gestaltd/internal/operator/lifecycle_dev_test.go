package operator

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

func TestLoadConfigForLifecycleMarksDevActiveUI(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	uiDir := filepath.Join(dir, "ui")
	if err := os.MkdirAll(uiDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifest := `kind: ui
source: github.com/acme/apps/demo-ui
version: "1.0.0"
dev:
  command: [sh, -c, echo]
build:
  command: [sh, -c, "mkdir -p out && echo ok > out/index.html"]
spec:
  assetRoot: out
  routes:
    - path: /
      allowedRoles: [viewer]
`
	if err := os.WriteFile(filepath.Join(uiDir, "manifest.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	cfgYAML := `apiVersion: gestaltd.config/v8
providers:
  ui:
    demo:
      path: /demo
      source:
        path: ./ui
`
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	cfg, err := NewLifecycle().WithDevServeEligible(true).loadConfigForLifecycle([]string{cfgPath}, StatePaths{}, false)
	if err != nil {
		t.Fatalf("loadConfigForLifecycle: %v", err)
	}
	entry := cfg.Providers.UI["demo"]
	if entry == nil {
		t.Fatal("expected ui.demo entry")
	}
	if !entry.DevActive {
		t.Fatal("expected DevActive")
	}
	if entry.ResolvedDevWorkdir != uiDir {
		t.Fatalf("ResolvedDevWorkdir = %q, want %q", entry.ResolvedDevWorkdir, uiDir)
	}
	if entry.ResolvedManifest == nil || entry.ResolvedManifest.Dev == nil {
		t.Fatal("expected resolved manifest with dev block")
	}
}

func TestLoadConfigForLifecycleDevServeIneligibleSkipsDevActive(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	uiDir := filepath.Join(dir, "ui")
	if err := os.MkdirAll(uiDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifest := `kind: ui
source: github.com/acme/apps/demo-ui
version: "1.0.0"
dev:
  command: [sh, -c, echo]
build:
  command: [sh, -c, "mkdir -p out && echo ok > out/index.html"]
spec:
  assetRoot: out
  routes:
    - path: /
      allowedRoles: [viewer]
`
	if err := os.WriteFile(filepath.Join(uiDir, "manifest.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	cfgYAML := `apiVersion: gestaltd.config/v8
providers:
  ui:
    demo:
      path: /demo
      source:
        path: ./ui
`
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	cfg, err := NewLifecycle().loadConfigForLifecycle([]string{cfgPath}, StatePaths{}, false)
	if err != nil {
		t.Fatalf("loadConfigForLifecycle: %v", err)
	}
	entry := cfg.Providers.UI["demo"]
	if entry == nil {
		t.Fatal("expected ui.demo entry")
	}
	if entry.DevActive {
		t.Fatal("lock/sync must not mark dev-active UI; it should be built and pinned normally")
	}
}

func TestLoadConfigForLifecycleGitUISkipsDevActive(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfgYAML := `apiVersion: gestaltd.config/v8
providers:
  ui:
    demo:
      path: /demo
      source: https://example.invalid/ui/demo
`
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	cfg, err := NewLifecycle().WithDevServeEligible(true).loadConfigForLifecycle([]string{cfgPath}, StatePaths{}, true)
	if err != nil {
		t.Fatalf("loadConfigForLifecycle: %v", err)
	}
	entry := cfg.Providers.UI["demo"]
	if entry == nil {
		t.Fatal("expected ui.demo entry")
	}
	if entry.DevActive {
		t.Fatal("git-backed UI must not be dev-active")
	}
}

func TestConfigHasLocalProviderSourcesIgnoresDevActiveUI(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Providers: config.ProvidersConfig{
			UI: map[string]*config.UIEntry{
				"demo": {
					ProviderEntry: config.ProviderEntry{
						Source: config.ProviderSource{Path: "/tmp/ui"},
					},
					DevActive: true,
				},
			},
		},
	}
	if configHasLocalProviderSources(cfg) {
		t.Fatal("dev-active UI should not trigger local provider auto-prepare")
	}
}

func TestLockSyncBuildsLocalDevUI(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("source UI build fixture uses POSIX shell")
	}

	dir := t.TempDir()
	uiDir := filepath.Join(dir, "ui", "demo")
	if err := os.MkdirAll(uiDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifest := `kind: ui
source: github.com/acme/apps/demo-ui
version: "1.0.0"
dev:
  command: [sh, -c, echo]
build:
  command: [sh, ./build.sh]
  inputs: [build.sh]
spec:
  assetRoot: dist
  routes:
    - path: /
      allowedRoles: [viewer]
`
	if err := os.WriteFile(filepath.Join(uiDir, "manifest.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uiDir, "build.sh"), []byte("mkdir -p dist\nprintf '<html>demo</html>\\n' > dist/index.html\n"), 0o755); err != nil {
		t.Fatalf("WriteFile build.sh: %v", err)
	}

	artifactsDir := filepath.Join(dir, "artifacts")
	configPath := filepath.Join(dir, "gestaltd.yaml")
	configYAML := `apiVersion: gestaltd.config/v8
` + requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")) + `  ui:
    demo:
      source:
        path: ui/demo/manifest.yaml
      path: /demo
server:
  providers:
    indexeddb: sqlite
  artifactsDir: ` + filepath.ToSlash(artifactsDir) + `
  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	lc := NewLifecycle()
	if _, err := lc.LockAtPathsWithStatePaths([]string{configPath}, StatePaths{}); err != nil {
		t.Fatalf("LockAtPathsWithStatePaths: %v", err)
	}
	if err := lc.SyncAtPathsWithStatePaths([]string{configPath}, StatePaths{}); err != nil {
		t.Fatalf("SyncAtPathsWithStatePaths: %v", err)
	}
	preparedUI := filepath.Join(artifactsDir, "ui", "demo", "dist", "index.html")
	if _, err := os.Stat(preparedUI); err != nil {
		t.Fatalf("sync should build local dev UI during lock/sync (not skip as dev-active): %v", err)
	}
}

func TestUnlockedServeResolvesDevUITheme(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	themeDir := filepath.Join(dir, "theme")
	if err := os.MkdirAll(themeDir, 0o755); err != nil {
		t.Fatalf("MkdirAll theme: %v", err)
	}
	if err := os.WriteFile(filepath.Join(themeDir, "tenant.css"), []byte("body{}"), 0o644); err != nil {
		t.Fatalf("WriteFile tenant.css: %v", err)
	}

	uiDir := filepath.Join(dir, "ui")
	if err := os.MkdirAll(uiDir, 0o755); err != nil {
		t.Fatalf("MkdirAll ui: %v", err)
	}
	manifest := `kind: ui
source: github.com/acme/apps/demo-ui
version: "1.0.0"
dev:
  command: [sh, -c, echo]
build:
  command: [sh, -c, "mkdir -p out && echo ok > out/index.html"]
spec:
  assetRoot: out
  routes:
    - path: /
      allowedRoles: [viewer]
`
	if err := os.WriteFile(filepath.Join(uiDir, "manifest.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	cfgPath := filepath.Join(dir, "gestaltd.yaml")
	cfgYAML := `apiVersion: gestaltd.config/v8
` + requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")) + `  ui:
    demo:
      path: /demo
      source:
        path: ./ui
      config:
        theme:
          stylesheet: ./theme/tenant.css
server:
  providers:
    indexeddb: sqlite
  artifactsDir: ` + filepath.ToSlash(filepath.Join(dir, "artifacts")) + `
  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	loaded, _, err := NewLifecycle().WithDevServeEligible(true).LoadForExecutionAtPathsWithStatePaths([]string{cfgPath}, StatePaths{}, false)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPathsWithStatePaths: %v", err)
	}
	entry := loaded.Providers.UI["demo"]
	if entry == nil {
		t.Fatal("expected ui.demo entry")
	}
	if !entry.DevActive {
		t.Fatal("expected DevActive for unlocked serve")
	}
	wantStylesheet := filepath.Join(dir, "theme", "tenant.css")
	if entry.ResolvedThemeStylesheet != wantStylesheet {
		t.Fatalf("ResolvedThemeStylesheet = %q, want %q", entry.ResolvedThemeStylesheet, wantStylesheet)
	}
}

func TestForcedDevUIKeysActivatesUnderLocked(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	uiDir := filepath.Join(dir, "ui")
	if err := os.MkdirAll(uiDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifest := `kind: ui
source: github.com/acme/apps/demo-ui
version: "1.0.0"
dev:
  command: [sh, -c, echo]
build:
  command: [sh, -c, "mkdir -p out && echo ok > out/index.html"]
spec:
  assetRoot: out
  routes:
    - path: /
      allowedRoles: [viewer]
`
	if err := os.WriteFile(filepath.Join(uiDir, "manifest.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	cfgYAML := `apiVersion: gestaltd.config/v8
providers:
  ui:
    demo:
      path: /demo
      source:
        path: ./ui
`
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	cfg, err := NewLifecycle().WithForcedDevUIKeys([]string{"demo"}).loadConfigForLifecycle([]string{cfgPath}, StatePaths{}, false)
	if err != nil {
		t.Fatalf("loadConfigForLifecycle: %v", err)
	}
	entry := cfg.Providers.UI["demo"]
	if entry == nil {
		t.Fatal("expected ui.demo entry")
	}
	if !entry.DevActive {
		t.Fatal("forced dev key should mark UI dev-active even when devServeEligible is false")
	}
}

func TestForcedDevUIKeyWithoutDevBlockErrors(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	uiDir := filepath.Join(dir, "ui")
	if err := os.MkdirAll(uiDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifest := `kind: ui
source: github.com/acme/apps/demo-ui
version: "1.0.0"
build:
  command: [sh, -c, "mkdir -p out && echo ok > out/index.html"]
spec:
  assetRoot: out
  routes:
    - path: /
      allowedRoles: [viewer]
`
	if err := os.WriteFile(filepath.Join(uiDir, "manifest.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	cfgYAML := `apiVersion: gestaltd.config/v8
providers:
  ui:
    demo:
      path: /demo
      source:
        path: ./ui
`
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	_, err := NewLifecycle().WithForcedDevUIKeys([]string{"demo"}).loadConfigForLifecycle([]string{cfgPath}, StatePaths{}, false)
	if err == nil || !strings.Contains(err.Error(), `no dev: block`) {
		t.Fatalf("loadConfigForLifecycle error = %v, want no dev: block error", err)
	}
}

func TestForcedDevUIKeyWithUnreadableSourceErrors(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfgYAML := `apiVersion: gestaltd.config/v8
providers:
  ui:
    demo:
      path: /demo
      source:
        path: ./missing-ui
`
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	_, err := NewLifecycle().WithForcedDevUIKeys([]string{"demo"}).loadConfigForLifecycle([]string{cfgPath}, StatePaths{}, false)
	if err == nil || !strings.Contains(err.Error(), `--path target "demo"`) {
		t.Fatalf("loadConfigForLifecycle error = %v, want forced --path target error", err)
	}
}
