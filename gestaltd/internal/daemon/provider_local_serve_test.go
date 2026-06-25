package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaybeRunServeProviderLocalRejectsLockedWithoutConfig(t *testing.T) {
	t.Parallel()

	_, err := maybeRunServeProviderLocal(serveProviderLocalOptions{
		Paths:         []string{"./ui"},
		Locked:        true,
		LockedAllowed: true,
	})
	if err == nil || !strings.Contains(err.Error(), "--locked requires --config") {
		t.Fatalf("error = %v, want --locked requires --config", err)
	}
}

func TestMaybeRunServeProviderLocalRejectsNameWithMultiplePaths(t *testing.T) {
	t.Parallel()

	_, err := maybeRunServeProviderLocal(serveProviderLocalOptions{
		Paths:       []string{"./ui/a", "./ui/b"},
		ConfigPaths: []string{filepath.Join(t.TempDir(), "config.yaml")},
		Name:        "demo",
	})
	if err == nil || !strings.Contains(err.Error(), "multiple --path") {
		t.Fatalf("error = %v, want multiple --path name error", err)
	}
}

func TestMaybeRunServeProviderLocalRejectsStatePathsWithoutConfig(t *testing.T) {
	t.Parallel()

	_, err := maybeRunServeProviderLocal(serveProviderLocalOptions{
		Paths:        []string{"./ui"},
		ArtifactsDir: t.TempDir(),
	})
	if err == nil || !strings.Contains(err.Error(), "--artifacts-dir and --lockfile require --config") {
		t.Fatalf("error = %v, want --artifacts-dir/--lockfile require --config", err)
	}
}

func TestPrepareProviderLocalOverlaySessionCollectsDevUIKeys(t *testing.T) {
	t.Parallel()

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

	baseCfg := filepath.Join(dir, "base.yaml")
	baseYAML := `apiVersion: gestaltd.config/v8
providers:
  ui:
    demo:
      path: /demo
      source: https://example.invalid/ui/demo
`
	if err := os.WriteFile(baseCfg, []byte(baseYAML), 0o644); err != nil {
		t.Fatalf("WriteFile base config: %v", err)
	}

	session, err := prepareProviderLocalSession(providerLocalCommandOptions{
		Paths:        []string{uiDir, uiDir},
		ConfigPaths:  []string{baseCfg},
		Locked:       true,
		FleetOverlay: true,
	})
	if err != nil {
		t.Fatalf("prepareProviderLocalSession: %v", err)
	}
	defer func() { _ = os.RemoveAll(session.Dir) }()

	if len(session.DevUIKeys) != 2 {
		t.Fatalf("DevUIKeys = %#v, want two entries", session.DevUIKeys)
	}
	if session.DevUIKeys[0] != "demo_ui" || session.DevUIKeys[1] != "demo_ui" {
		t.Fatalf("DevUIKeys = %#v, want demo_ui twice", session.DevUIKeys)
	}
	if len(session.ConfigPaths) != 3 {
		t.Fatalf("ConfigPaths len = %d, want base + 2 overlays", len(session.ConfigPaths))
	}
}
