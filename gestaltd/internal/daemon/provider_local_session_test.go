package daemon

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

func TestPrepareProviderLocalSessionSupportsDirectUITarget(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mountedUI := setupMountedUIDir(t, dir)
	setUIManifestSource(t, mountedUI.ManifestPath, "github.com/test/ui/roadmap.review")

	session, err := prepareProviderLocalSession(providerLocalCommandOptions{Path: mountedUI.ManifestPath})
	if err != nil {
		t.Fatalf("prepareProviderLocalSession: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(session.Dir) })

	if got, want := session.Kind, "ui"; got != want {
		t.Fatalf("session.Kind = %q, want %q", got, want)
	}
	if got, want := session.TargetKey, "roadmap_review"; got != want {
		t.Fatalf("session.TargetKey = %q, want %q", got, want)
	}
	if got, want := session.AutoMountedUIPath, "/roadmap.review"; got != want {
		t.Fatalf("session.AutoMountedUIPath = %q, want %q", got, want)
	}
	if !slices.Contains(session.PublicUIPaths, session.AutoMountedUIPath) {
		t.Fatalf("session.PublicUIPaths = %v, want %q", session.PublicUIPaths, session.AutoMountedUIPath)
	}

	cfg, err := config.LoadPaths(session.ConfigPaths)
	if err != nil {
		t.Fatalf("LoadPaths(session.ConfigPaths): %v", err)
	}
	ui := cfg.Providers.UI[session.TargetKey]
	if ui == nil {
		t.Fatalf("Providers.UI[%q] = nil", session.TargetKey)
		return
	}
	wantManifestPath, err := canonicalPath(mountedUI.ManifestPath)
	if err != nil {
		t.Fatalf("canonicalPath(%s): %v", mountedUI.ManifestPath, err)
	}
	if got := ui.SourcePath(); got != wantManifestPath {
		t.Fatalf("UI source path = %q, want %q", got, wantManifestPath)
	}
	if got := ui.Path; got != session.AutoMountedUIPath {
		t.Fatalf("UI path = %q, want %q", got, session.AutoMountedUIPath)
	}
}

func TestPrepareProviderLocalSessionAutoMountsSiblingUI(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	rootDir := filepath.Join(dir, "package")
	appDir := setupAppDir(t, filepath.Join(rootDir, "app"))
	setAppManifestSource(t, appDir, "github.com/test/apps/vm-style-guide")
	siblingUI := setupMountedUIDirAt(t, filepath.Join(rootDir, "ui"), nil)
	appManifest := componentProviderManifestPath(t, appDir)

	session, err := prepareProviderLocalSession(providerLocalCommandOptions{Path: appManifest})
	if err != nil {
		t.Fatalf("prepareProviderLocalSession: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(session.Dir) })

	if got, want := session.Kind, "app"; got != want {
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
		return
	}
	if got := app.UI; got != session.TargetKey {
		t.Fatalf("App UI = %q, want %q", got, session.TargetKey)
	}
	if got := app.MountPath; got != session.AutoMountedUIPath {
		t.Fatalf("App mount path = %q, want %q", got, session.AutoMountedUIPath)
	}
	ui := cfg.Providers.UI[session.TargetKey]
	if ui == nil {
		t.Fatalf("Providers.UI[%q] = nil", session.TargetKey)
		return
	}
	wantManifestPath, err := canonicalPath(siblingUI.ManifestPath)
	if err != nil {
		t.Fatalf("canonicalPath(%s): %v", siblingUI.ManifestPath, err)
	}
	if got := ui.SourcePath(); got != wantManifestPath {
		t.Fatalf("sibling UI source path = %q, want %q", got, wantManifestPath)
	}
}

func TestPrepareProviderLocalSessionLeavesAppWithoutUIUnmounted(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	appDir := setupAppDir(t, dir)
	setAppManifestSource(t, appDir, "github.com/test/apps/api-only")
	appManifest := componentProviderManifestPath(t, appDir)

	session, err := prepareProviderLocalSession(providerLocalCommandOptions{Path: appManifest})
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
		return
	}
	if got := app.UI; got != "" {
		t.Fatalf("App UI = %q, want empty", got)
	}
	if got := app.MountPath; got != "" {
		t.Fatalf("App mount path = %q, want empty", got)
	}
	if ui := cfg.Providers.UI[session.TargetKey]; ui != nil {
		t.Fatalf("Providers.UI[%q] = %#v, want nil", session.TargetKey, ui)
	}
}
