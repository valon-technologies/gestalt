package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

func TestServeProviderLocalAppliesRemoteTokenOverrides(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	appDir := setupAppDir(t, dir)
	setAppManifestSource(t, appDir, "github.com/acme/apps/demo")

	baseCfg := filepath.Join(dir, "base.yaml")
	baseYAML := `apiVersion: gestaltd.config/v8
server:
  remote: https://valon.tools/
apps:
  demo:
    source: https://example.invalid/apps/demo
`
	if err := os.WriteFile(baseCfg, []byte(baseYAML), 0o644); err != nil {
		t.Fatalf("WriteFile base config: %v", err)
	}

	appManifest := componentProviderManifestPath(t, appDir)
	session, err := prepareProviderLocalSession(providerLocalCommandOptions{
		Paths:        []string{appManifest},
		ConfigPaths:  []string{baseCfg},
		Locked:       true,
		FleetOverlay: true,
		RemoteToken:  "test-token",
	})
	if err != nil {
		t.Fatalf("prepareProviderLocalSession: %v", err)
	}
	defer func() { _ = os.RemoveAll(session.Dir) }()

	cfg, err := config.LoadPaths(session.ConfigPaths)
	if err != nil {
		t.Fatalf("LoadPaths: %v", err)
	}
	if err := config.ApplyServeRemoteOverrides(cfg, "", "test-token"); err != nil {
		t.Fatalf("ApplyServeRemoteOverrides: %v", err)
	}
	if got := cfg.Server.Remote; got != "https://valon.tools" {
		t.Fatalf("Server.Remote = %q, want https://valon.tools", got)
	}
	if got := cfg.Server.RemoteToken; got != "test-token" {
		t.Fatalf("Server.RemoteToken = %q, want test-token", got)
	}
}

func TestServeProviderLocalRequiresRemoteTokenWhenRemoteSet(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	appDir := setupAppDir(t, dir)
	setAppManifestSource(t, appDir, "github.com/acme/apps/demo")

	baseCfg := filepath.Join(dir, "base.yaml")
	baseYAML := `apiVersion: gestaltd.config/v8
server:
  remote: https://valon.tools/
apps:
  demo:
    source: https://example.invalid/apps/demo
`
	if err := os.WriteFile(baseCfg, []byte(baseYAML), 0o644); err != nil {
		t.Fatalf("WriteFile base config: %v", err)
	}

	appManifest := componentProviderManifestPath(t, appDir)
	session, err := prepareProviderLocalSession(providerLocalCommandOptions{
		Paths:        []string{appManifest},
		ConfigPaths:  []string{baseCfg},
		Locked:       true,
		FleetOverlay: true,
	})
	if err != nil {
		t.Fatalf("prepareProviderLocalSession: %v", err)
	}
	defer func() { _ = os.RemoveAll(session.Dir) }()

	cfg, err := config.LoadPaths(session.ConfigPaths)
	if err != nil {
		t.Fatalf("LoadPaths: %v", err)
	}
	err = config.ApplyServeRemoteOverrides(cfg, "", "")
	if err == nil || !strings.Contains(err.Error(), "server.remoteToken is required") {
		t.Fatalf("error = %v, want server.remoteToken is required", err)
	}
}

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

func TestMaybeRunServeProviderLocalRejectsLockfileAndArtifactsDirWithoutConfig(t *testing.T) {
	t.Parallel()

	_, err := maybeRunServeProviderLocal(serveProviderLocalOptions{
		Paths:        []string{"./ui"},
		ArtifactsDir: t.TempDir(),
	})
	if err == nil || !strings.Contains(err.Error(), "--lockfile and --artifacts-dir require --config") {
		t.Fatalf("error = %v, want --lockfile and --artifacts-dir require --config", err)
	}
}

func TestPrepareProviderLocalOverlaySessionCollectsDevAppKeys(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	appDir := setupAppDir(t, dir)
	setAppManifestSource(t, appDir, "github.com/acme/apps/demo")

	baseCfg := filepath.Join(dir, "base.yaml")
	baseYAML := `apiVersion: gestaltd.config/v8
apps:
  demo:
    source: https://example.invalid/apps/demo
`
	if err := os.WriteFile(baseCfg, []byte(baseYAML), 0o644); err != nil {
		t.Fatalf("WriteFile base config: %v", err)
	}

	appManifest := componentProviderManifestPath(t, appDir)
	session, err := prepareProviderLocalSession(providerLocalCommandOptions{
		Paths:        []string{appManifest, appManifest},
		ConfigPaths:  []string{baseCfg},
		Locked:       true,
		FleetOverlay: true,
	})
	if err != nil {
		t.Fatalf("prepareProviderLocalSession: %v", err)
	}
	defer func() { _ = os.RemoveAll(session.Dir) }()

	if len(session.DevAppKeys) != 2 {
		t.Fatalf("DevAppKeys = %#v, want two entries", session.DevAppKeys)
	}
	if session.DevAppKeys[0] != "demo" || session.DevAppKeys[1] != "demo" {
		t.Fatalf("DevAppKeys = %#v, want demo twice", session.DevAppKeys)
	}
	if len(session.ConfigPaths) != 3 {
		t.Fatalf("ConfigPaths len = %d, want base + 2 overlays", len(session.ConfigPaths))
	}
}

func TestPrepareProviderLocalOverlaySessionForwardsNoSync(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	appDir := setupAppDir(t, dir)
	setAppManifestSource(t, appDir, "github.com/acme/apps/demo")

	baseCfg := filepath.Join(dir, "base.yaml")
	baseYAML := `apiVersion: gestaltd.config/v8
apps:
  demo:
    source: https://example.invalid/apps/demo
`
	if err := os.WriteFile(baseCfg, []byte(baseYAML), 0o644); err != nil {
		t.Fatalf("WriteFile base config: %v", err)
	}

	appManifest := componentProviderManifestPath(t, appDir)
	session, err := prepareProviderLocalSession(providerLocalCommandOptions{
		Paths:        []string{appManifest},
		ConfigPaths:  []string{baseCfg},
		Locked:       true,
		NoSync:       true,
		FleetOverlay: true,
	})
	if err != nil {
		t.Fatalf("prepareProviderLocalSession: %v", err)
	}
	defer func() { _ = os.RemoveAll(session.Dir) }()

	if !session.Locked {
		t.Fatalf("session.Locked = false, want true under fleet overlay")
	}
	if !session.NoSync {
		t.Fatalf("session.NoSync = false, want true so --locked --no-sync binds pinned artifacts as-is")
	}
}

func TestPrepareProviderLocalOverlaySessionRejectsUIKind(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	uiDir := filepath.Join(dir, "ui", "demo")
	if err := os.MkdirAll(uiDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifest := `kind: ui
source: github.com/acme/apps/demo-ui
version: "1.0.0"
run:
  command: [sh, -c, echo]
build:
  command: [sh, -c, "mkdir -p out && echo ok > out/index.html"]
spec:
  assetRoot: out
`
	if err := os.WriteFile(filepath.Join(uiDir, "manifest.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	_, err := prepareProviderLocalSession(providerLocalCommandOptions{
		Paths: []string{uiDir},
	})
	if err == nil || (!strings.Contains(err.Error(), "apps.demo_ui.static") && !strings.Contains(err.Error(), `manifest kind "ui" is not valid`)) {
		t.Fatalf("error = %v, want ui kind rejection", err)
	}
}
