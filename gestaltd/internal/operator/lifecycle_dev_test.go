package operator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

func TestLoadConfigForLifecycleMarksDevActiveLocalSourceApp(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	appDir := filepath.Join(dir, "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifest := `kind: app
source: github.com/acme/apps/demo
version: "1.0.0"
run:
  command: [sh, ./provider.sh]
spec:
  connections:
    default:
      auth:
        type: none
`
	if err := os.WriteFile(filepath.Join(appDir, "manifest.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "provider.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile provider.sh: %v", err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	cfgYAML := `apiVersion: gestaltd.config/v8
apps:
  demo:
    source:
      path: ./app
`
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	cfg, err := NewLifecycle().WithDevServeEligible(true).loadConfigForLifecycle([]string{cfgPath}, false)
	if err != nil {
		t.Fatalf("loadConfigForLifecycle: %v", err)
	}
	entry := cfg.Apps["demo"]
	if entry == nil {
		t.Fatal("expected apps.demo entry")
	}
	if !entry.DevActive {
		t.Fatal("expected DevActive")
	}
	if entry.ResolvedDevWorkdir != appDir {
		t.Fatalf("ResolvedDevWorkdir = %q, want %q", entry.ResolvedDevWorkdir, appDir)
	}
	if entry.Command != "" {
		t.Fatalf("Command = %q, want empty for source-run app", entry.Command)
	}
	if entry.ResolvedManifestPath == "" {
		t.Fatal("expected ResolvedManifestPath")
	}
}

func TestLoadConfigForLifecycleLocalSourceAppWithoutRunErrors(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	appDir := filepath.Join(dir, "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifest := `kind: app
source: github.com/acme/apps/demo
version: "1.0.0"
build:
  command: [sh, -c, echo]
spec:
  connections:
    default:
      auth:
        type: none
`
	if err := os.WriteFile(filepath.Join(appDir, "manifest.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	cfgYAML := `apiVersion: gestaltd.config/v8
apps:
  demo:
    source:
      path: ./app
`
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	_, err := NewLifecycle().WithDevServeEligible(true).loadConfigForLifecycle([]string{cfgPath}, false)
	if err == nil || !strings.Contains(err.Error(), `local-source apps must declare run`) {
		t.Fatalf("loadConfigForLifecycle error = %v, want must declare run error", err)
	}
}

func TestLoadConfigForLifecycleLocalSourceAppWithoutPhasesErrors(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	appDir := filepath.Join(dir, "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifest := `kind: app
source: github.com/acme/apps/demo
version: "1.0.0"
spec:
  connections:
    default:
      auth:
        type: none
`
	if err := os.WriteFile(filepath.Join(appDir, "manifest.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	cfgYAML := `apiVersion: gestaltd.config/v8
apps:
  demo:
    source:
      path: ./app
`
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	_, err := NewLifecycle().WithDevServeEligible(true).loadConfigForLifecycle([]string{cfgPath}, false)
	if err == nil || !strings.Contains(err.Error(), `local-source apps must declare run`) {
		t.Fatalf("loadConfigForLifecycle error = %v, want must declare run error", err)
	}
}

func TestConfigHasLocalProviderSourcesIgnoresDevActiveApp(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"demo": {
				Source:    config.ProviderSource{Path: "/tmp/app"},
				DevActive: true,
			},
		},
	}
	if configHasLocalProviderSources(cfg) {
		t.Fatal("dev-active app should not trigger local provider auto-prepare")
	}
}

func TestForcedDevAppKeysActivatesUnderLocked(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	appDir := filepath.Join(dir, "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifest := `kind: app
source: github.com/acme/apps/demo
version: "1.0.0"
run:
  command: [sh, ./provider.sh]
spec:
  connections:
    default:
      auth:
        type: none
`
	if err := os.WriteFile(filepath.Join(appDir, "manifest.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "provider.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile provider.sh: %v", err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	cfgYAML := `apiVersion: gestaltd.config/v8
apps:
  demo:
    source:
      path: ./app
`
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	cfg, err := NewLifecycle().WithForcedDevAppKeys([]string{"demo"}).loadConfigForLifecycle([]string{cfgPath}, false)
	if err != nil {
		t.Fatalf("loadConfigForLifecycle: %v", err)
	}
	entry := cfg.Apps["demo"]
	if entry == nil {
		t.Fatal("expected apps.demo entry")
	}
	if !entry.DevActive {
		t.Fatal("forced dev key should mark app dev-active even when devServeEligible is false")
	}
}

func TestForcedDevAppKeyWithoutRunBlockErrors(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	appDir := filepath.Join(dir, "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifest := `kind: app
source: github.com/acme/apps/demo
version: "1.0.0"
build:
  command: [sh, -c, echo]
spec:
  connections:
    default:
      auth:
        type: none
`
	if err := os.WriteFile(filepath.Join(appDir, "manifest.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	cfgYAML := `apiVersion: gestaltd.config/v8
apps:
  demo:
    source:
      path: ./app
`
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	_, err := NewLifecycle().WithForcedDevAppKeys([]string{"demo"}).loadConfigForLifecycle([]string{cfgPath}, false)
	if err == nil || !strings.Contains(err.Error(), `no run: block`) {
		t.Fatalf("loadConfigForLifecycle error = %v, want no run: block error", err)
	}
}

func TestForcedDevAppKeyWithUnreadableSourceErrors(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfgYAML := `apiVersion: gestaltd.config/v8
apps:
  demo:
    source:
      path: ./missing-app
`
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	_, err := NewLifecycle().WithForcedDevAppKeys([]string{"demo"}).loadConfigForLifecycle([]string{cfgPath}, false)
	if err == nil || !strings.Contains(err.Error(), `--path target "demo"`) {
		t.Fatalf("loadConfigForLifecycle error = %v, want forced --path target error", err)
	}
}
