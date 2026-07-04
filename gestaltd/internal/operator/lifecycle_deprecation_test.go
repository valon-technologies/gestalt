package operator

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

func TestLockAtPathsSurfacesProvidersUIDeprecationWarning(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("source UI build fixture uses POSIX shell")
	}

	dir := t.TempDir()
	uiDir := filepath.Join(dir, "ui", "legacy")
	if err := os.MkdirAll(uiDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifest := `kind: ui
source: github.com/acme/apps/legacy-ui
version: "1.0.0"
run:
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
	if err := os.WriteFile(filepath.Join(uiDir, "build.sh"), []byte("mkdir -p dist\nprintf '<html>legacy</html>\\n' > dist/index.html\n"), 0o755); err != nil {
		t.Fatalf("WriteFile build.sh: %v", err)
	}

	artifactsDir := filepath.Join(dir, "artifacts")
	lockfilePath := filepath.Join(dir, LockfileName)
	configPath := filepath.Join(dir, "gestaltd.yaml")
	configYAML := `apiVersion: gestaltd.config/v8
` + requiredComponentConfigYAML(t, dir, filepath.Join(dir, "data.db")) + `  ui:
    legacy:
      source:
        path: ui/legacy/manifest.yaml
      path: /legacy
server:
  providers:
    indexeddb: sqlite
  artifactsDir: ` + filepath.ToSlash(artifactsDir) + `
  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	var warnings []string
	lc := NewLifecycle().WithDeprecationLogger(func(msg string) { warnings = append(warnings, msg) })
	if _, err := lc.LockAtPaths([]string{configPath}, lockfilePath, artifactsDir); err != nil {
		t.Fatalf("LockAtPaths: %v", err)
	}
	assertDeprecationWarnings(t, warnings,
		`providers.ui.legacy is deprecated; migrate to apps.legacy.static`,
	)
}

func TestLoadConfigForLifecycleSurfacesKindUIManifestDeprecationWarning(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	uiDir := filepath.Join(dir, "ui")
	if err := os.MkdirAll(uiDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifest := `kind: ui
source: github.com/acme/apps/legacy-ui
version: "1.0.0"
run:
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
    legacy:
      path: /legacy
      source:
        path: ./ui
`
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	var warnings []string
	lc := NewLifecycle().WithDevServeEligible(true).WithDeprecationLogger(func(msg string) { warnings = append(warnings, msg) })
	if _, err := lc.loadConfigForLifecycle([]string{cfgPath}, false); err != nil {
		t.Fatalf("loadConfigForLifecycle: %v", err)
	}
	assertDeprecationWarnings(t, warnings,
		`providers.ui.legacy is deprecated; migrate to apps.legacy.static`,
		`kind: ui manifest "legacy" is deprecated; migrate to apps.legacy.static`,
	)
}

func TestLoadConfigForLifecycleSurfacesSpecUIDeprecationWarning(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.yaml")
	buildOutput := filepath.Join(dir, "bin", "owned")
	manifest, err := encodeSourceManifestForTest(&providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindApp,
		Source:  "github.com/acme/apps/owned",
		Version: "1.0.0",
		Run:     localAppSourceRunCommand(buildOutput),
		Spec: withNoAuthDefaultConnection(&providermanifestv1.Spec{
			UI: &providermanifestv1.OwnedUI{Path: "ui"},
		}),
	}, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("encodeSourceManifestForTest: %v", err)
	}
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	cfgYAML := `apiVersion: gestaltd.config/v8
apps:
  owned:
    source:
      path: ./manifest.yaml
`
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	var warnings []string
	lc := NewLifecycle().WithDeprecationLogger(func(msg string) { warnings = append(warnings, msg) })
	if _, err := lc.loadConfigForLifecycle([]string{cfgPath}, true); err != nil {
		t.Fatalf("loadConfigForLifecycle: %v", err)
	}
	assertDeprecationWarnings(t, warnings,
		`apps.owned manifest spec.ui is deprecated; migrate to apps.owned.static`,
	)
}

func TestLoadForExecutionOmitsDeprecationWarningsForAppStaticOnlyConfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	lockfilePath, artifactsDir := configDirPaths(dir)
	manifestPath := filepath.Join(dir, "manifest.yaml")
	buildOutput := filepath.Join(dir, "bin", "modern")
	manifest, err := encodeSourceManifestForTest(&providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindApp,
		Source:      "github.com/acme/apps/modern",
		Version:     "1.0.0",
		DisplayName: "Modern",
		Spec:        withNoAuthDefaultConnection(&providermanifestv1.Spec{}),
		Run:         localAppSourceRunCommand(buildOutput),
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"build.sh"},
		},
	}, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("encodeSourceManifestForTest: %v", err)
	}
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "build.sh"), []byte("#!/bin/sh\nmkdir -p bin\ntouch bin/modern\n"), 0o755); err != nil {
		t.Fatalf("WriteFile build.sh: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "catalog.yaml"), []byte("name: modern\noperations:\n  - id: ping\n    method: GET\n"), 0o644); err != nil {
		t.Fatalf("WriteFile catalog: %v", err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := requiredComponentConfigWithAPIVersionYAML(t, dir, filepath.Join(dir, "gestalt.db")) + `apps:
  modern:
    source:
      path: ./manifest.yaml
    static:
      mount: /modern
` + `server:
` + requiredServerIndexedDBYAML() + `  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	var warnings []string
	lc := NewLifecycle().WithDeprecationLogger(func(msg string) { warnings = append(warnings, msg) })
	if _, _, err := lc.LoadForExecutionAtPaths([]string{cfgPath}, lockfilePath, artifactsDir, false, false); err != nil {
		t.Fatalf("LoadForExecutionAtPaths: %v", err)
	}
	for _, warning := range warnings {
		if strings.Contains(warning, "deprecated") {
			t.Fatalf("warnings = %q, want none", warnings)
		}
	}
}

func assertDeprecationWarnings(t *testing.T, got []string, want ...string) {
	t.Helper()
	for _, msg := range want {
		found := false
		for _, warning := range got {
			if warning == msg {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("warnings = %q, missing %q", got, msg)
		}
	}
}
