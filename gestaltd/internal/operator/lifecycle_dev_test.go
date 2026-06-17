package operator

import (
	"os"
	"path/filepath"
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
	cfgYAML := `apiVersion: gestaltd.config/v6
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
	cfgYAML := `apiVersion: gestaltd.config/v6
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
	cfgYAML := `apiVersion: gestaltd.config/v6
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
