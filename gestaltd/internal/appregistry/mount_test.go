package appregistry_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/appregistry/registrytest"
	"github.com/valon-technologies/gestalt/server/internal/config"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/packageio"
)

func TestBindInstalledApp_updates_provider_entry(t *testing.T) {
	t.Parallel()

	fixture := registrytest.NewInstallFixture(t)
	artifactsDir := t.TempDir()
	materializer := &appregistry.Materializer{
		Registries: map[string]config.AppRegistryConfig{
			"toolshed": fixture.Registry,
		},
		Reader:       fixture.Reader,
		ArtifactsDir: artifactsDir,
	}
	if _, err := materializer.Ensure(context.Background(), &core.AppInstallation{
		AppName:  "g-issues",
		Version:  fixture.Version,
		Registry: "toolshed",
	}); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	oldCommand := filepath.Join(t.TempDir(), "old-binary")
	entry := &config.ProviderEntry{
		Command: oldCommand,
		Args:    []string{"--legacy"},
		ResolvedManifest: &providermanifestv1.Manifest{
			Version: "0.0.0-old",
		},
	}
	destDir := appregistry.MaterializedPath(artifactsDir, "g-issues", fixture.Version)
	if err := appregistry.BindInstalledApp("g-issues", entry, destDir, fixture.Version); err != nil {
		t.Fatalf("BindInstalledApp: %v", err)
	}

	wantCommand := filepath.Join(destDir, filepath.FromSlash(packageio.InstalledExecutablePath("g-issues", runtime.GOOS)))
	if entry.Command != wantCommand {
		t.Fatalf("Command = %q, want %q", entry.Command, wantCommand)
	}
	if entry.ResolvedManifestPath != filepath.Join(destDir, "manifest.yaml") {
		t.Fatalf("ResolvedManifestPath = %q, want manifest under %q", entry.ResolvedManifestPath, destDir)
	}
	if entry.ResolvedManifest == nil || entry.ResolvedManifest.Version != fixture.Version {
		t.Fatalf("ResolvedManifest.Version = %q, want %q", entry.ResolvedManifest.Version, fixture.Version)
	}
	if _, err := os.Stat(entry.Command); err != nil {
		t.Fatalf("stat mounted executable: %v", err)
	}
}

func TestBindInstalledAppIfPresent_ignores_missing_registry_install(t *testing.T) {
	t.Parallel()

	oldCommand := filepath.Join(t.TempDir(), "old-binary")
	entry := &config.ProviderEntry{Command: oldCommand}
	if err := appregistry.BindInstalledAppIfPresent("g-issues", entry, t.TempDir(), "0.0.0-snapshot.gmissing"); err != nil {
		t.Fatalf("BindInstalledAppIfPresent: %v", err)
	}
	if entry.Command != oldCommand {
		t.Fatalf("Command = %q, want unchanged %q", entry.Command, oldCommand)
	}
}
