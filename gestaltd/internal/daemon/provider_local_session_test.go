package daemon

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func TestPrepareProviderLocalSessionRejectsDirectUITarget(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	uiDir := filepath.Join(dir, "ui")
	if err := os.MkdirAll(uiDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifest := `kind: ui
source: github.com/test/ui/roadmap.review
version: "0.0.1-alpha.1"
build:
  command: [sh, -c, "mkdir -p dist && echo ok > dist/index.html"]
spec:
  assetRoot: dist
`
	manifestPath := filepath.Join(uiDir, "manifest.yaml")
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	_, err := prepareProviderLocalSession(providerLocalCommandOptions{Paths: []string{manifestPath}})
	if err == nil || (!strings.Contains(err.Error(), "apps.roadmap_review.static") && !strings.Contains(err.Error(), `manifest kind "ui" is not valid`)) {
		t.Fatalf("error = %v, want ui kind rejection", err)
	}
}

func TestPrepareProviderLocalSessionAutoMountsAppStatic(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	appDir := setupAppDir(t, dir)
	setAppManifestSource(t, appDir, "github.com/test/apps/vm-style-guide")
	appManifest := componentProviderManifestPath(t, appDir)

	baseCfg := filepath.Join(dir, "base.yaml")
	baseYAML := `apiVersion: gestaltd.config/v8
apps:
  vm_style_guide:
    static:
      mount: /vm-style-guide
    source: https://example.invalid/apps/vm-style-guide
`
	if err := os.WriteFile(baseCfg, []byte(baseYAML), 0o644); err != nil {
		t.Fatalf("WriteFile base config: %v", err)
	}

	session, err := prepareProviderLocalSession(providerLocalCommandOptions{
		Paths:        []string{appManifest},
		ConfigPaths:  []string{baseCfg},
		FleetOverlay: true,
	})
	if err != nil {
		t.Fatalf("prepareProviderLocalSession: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(session.Dir) })

	if got, want := session.Kind, providermanifestv1.KindApp; got != want {
		t.Fatalf("session.Kind = %q, want %q", got, want)
	}
	if got, want := session.TargetKey, "vm_style_guide"; got != want {
		t.Fatalf("session.TargetKey = %q, want %q", got, want)
	}
	if got, want := session.AutoMountedUIPath, "/vm-style-guide"; got != want {
		t.Fatalf("session.AutoMountedUIPath = %q, want %q", got, want)
	}
	if !slices.Contains(session.PublicUIPaths, session.AutoMountedUIPath) {
		t.Fatalf("session.PublicUIPaths = %v, want %q", session.PublicUIPaths, session.AutoMountedUIPath)
	}

	cfg, err := config.LoadPaths(session.ConfigPaths)
	if err != nil {
		t.Fatalf("LoadPaths(session.ConfigPaths): %v", err)
	}
	app := cfg.Apps[session.TargetKey]
	if app == nil {
		t.Fatalf("Apps[%q] = nil", session.TargetKey)
	}
	if app.Static == nil {
		t.Fatalf("Apps[%q].Static = nil, want static mount overlay", session.TargetKey)
	}
	if got := app.Static.Mount; got != session.AutoMountedUIPath {
		t.Fatalf("App static mount = %q, want %q", got, session.AutoMountedUIPath)
	}
	wantManifestPath, err := canonicalPath(appManifest)
	if err != nil {
		t.Fatalf("canonicalPath(%s): %v", appManifest, err)
	}
	if got := app.SourcePath(); got != wantManifestPath {
		t.Fatalf("App source path = %q, want %q", got, wantManifestPath)
	}
}

func TestPrepareProviderLocalSessionUsesConfiguredGitAppKey(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	appDir := filepath.Join(dir, "valon-tools", "apps", "ci-cd")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifest := `kind: app
source: github.com/valon-technologies/valon-tools/apps/ci-cd
version: 1.0.0
displayName: CI/CD
spec: {}
run:
  command: [sh, -c, echo]
`
	manifestPath := filepath.Join(appDir, "manifest.yaml")
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	baseCfg := filepath.Join(dir, "base.yaml")
	baseYAML := `apiVersion: gestaltd.config/v8
apps:
  ciCd:
    source:
      git:
        repo: https://github.com/valon-technologies/toolshed.git
        ref: abcdef0123456789abcdef0123456789abcdef01
        path: valon-tools/apps/ci-cd/manifest.yaml
    static:
      mount: /ci-cd
`
	if err := os.WriteFile(baseCfg, []byte(baseYAML), 0o644); err != nil {
		t.Fatalf("WriteFile base config: %v", err)
	}

	session, err := prepareProviderLocalSession(providerLocalCommandOptions{
		Paths:        []string{manifestPath},
		ConfigPaths:  []string{baseCfg},
		FleetOverlay: true,
	})
	if err != nil {
		t.Fatalf("prepareProviderLocalSession: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(session.Dir) })

	if got, want := session.Kind, providermanifestv1.KindApp; got != want {
		t.Fatalf("session.Kind = %q, want %q", got, want)
	}
	if got, want := session.TargetKey, "ciCd"; got != want {
		t.Fatalf("session.TargetKey = %q, want %q", got, want)
	}
	if got, want := session.AutoMountedUIPath, "/ci-cd"; got != want {
		t.Fatalf("session.AutoMountedUIPath = %q, want %q", got, want)
	}

	cfg, err := config.LoadPaths(session.ConfigPaths)
	if err != nil {
		t.Fatalf("LoadPaths(session.ConfigPaths): %v", err)
	}
	app := cfg.Apps[session.TargetKey]
	if app == nil {
		t.Fatalf("Apps[%q] = nil", session.TargetKey)
	}
	wantManifestPath, err := canonicalPath(manifestPath)
	if err != nil {
		t.Fatalf("canonicalPath(%s): %v", manifestPath, err)
	}
	if got := app.SourcePath(); got != wantManifestPath {
		t.Fatalf("App source path = %q, want %q", got, wantManifestPath)
	}
}

func TestPrepareProviderLocalSessionLeavesAppWithoutUIUnmounted(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	appDir := setupAppDir(t, dir)
	setAppManifestSource(t, appDir, "github.com/test/apps/api-only")
	appManifest := componentProviderManifestPath(t, appDir)

	session, err := prepareProviderLocalSession(providerLocalCommandOptions{Paths: []string{appManifest}})
	if err != nil {
		t.Fatalf("prepareProviderLocalSession: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(session.Dir) })

	if got := session.AutoMountedUIPath; got != "" {
		t.Fatalf("session.AutoMountedUIPath = %q, want empty", got)
	}
	if len(session.PublicUIPaths) != 0 {
		t.Fatalf("session.PublicUIPaths = %v, want empty", session.PublicUIPaths)
	}

	cfg, err := config.LoadPaths(session.ConfigPaths)
	if err != nil {
		t.Fatalf("LoadPaths(session.ConfigPaths): %v", err)
	}
	app := cfg.Apps[session.TargetKey]
	if app == nil {
		t.Fatalf("Apps[%q] = nil", session.TargetKey)
	}
	if app.Static != nil {
		t.Fatalf("Apps[%q].Static = %#v, want nil", session.TargetKey, app.Static)
	}
}

func TestWriteProviderLocalBaseConfigMarksIndexedDBLocal(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "base.yaml")
	dbPath := filepath.Join(dir, "provider.db")
	if err := writeProviderLocalBaseConfig(cfgPath, dbPath); err != nil {
		t.Fatalf("writeProviderLocalBaseConfig: %v", err)
	}

	cfg, err := config.LoadPaths([]string{cfgPath})
	if err != nil {
		t.Fatalf("LoadPaths: %v", err)
	}

	name, entry, err := cfg.SelectedIndexedDBProvider()
	if err != nil {
		t.Fatalf("SelectedIndexedDBProvider: %v", err)
	}
	if entry == nil {
		t.Fatalf("IndexedDB entry %q is nil", name)
	}
	if !entry.Local {
		t.Fatalf("IndexedDB entry %q: Local = false, want true (prevents remote stamping in dev mode)", name)
	}
}
