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

func TestResolveInstalledApp_returns_isolated_provider_entry(t *testing.T) {
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
	resolved, err := appregistry.ResolveInstalledApp("g-issues", entry, destDir, fixture.Version)
	if err != nil {
		t.Fatalf("ResolveInstalledApp: %v", err)
	}

	wantCommand := filepath.Join(destDir, filepath.FromSlash(packageio.InstalledExecutablePath("g-issues", runtime.GOOS)))
	if resolved.Command != wantCommand {
		t.Fatalf("Command = %q, want %q", resolved.Command, wantCommand)
	}
	if resolved.ResolvedManifestPath != filepath.Join(destDir, "manifest.yaml") {
		t.Fatalf("ResolvedManifestPath = %q, want manifest under %q", resolved.ResolvedManifestPath, destDir)
	}
	if resolved.ResolvedManifest == nil || resolved.ResolvedManifest.Version != fixture.Version {
		t.Fatalf("ResolvedManifest.Version = %q, want %q", resolved.ResolvedManifest.Version, fixture.Version)
	}
	if _, err := os.Stat(resolved.Command); err != nil {
		t.Fatalf("stat mounted executable: %v", err)
	}
	if entry.Command != oldCommand {
		t.Fatalf("original Command = %q, want unchanged %q", entry.Command, oldCommand)
	}
	if entry.ResolvedManifest == nil || entry.ResolvedManifest.Version != "0.0.0-old" {
		t.Fatalf("original manifest was mutated: %#v", entry.ResolvedManifest)
	}
}

func TestResolveInstalledAppIfPresent_uses_deploy_entry_when_install_missing(t *testing.T) {
	t.Parallel()

	oldCommand := filepath.Join(t.TempDir(), "old-binary")
	entry := &config.ProviderEntry{Command: oldCommand}
	resolved, err := appregistry.ResolveInstalledAppIfPresent("g-issues", entry, t.TempDir(), "0.0.0-snapshot.gmissing")
	if err != nil {
		t.Fatalf("ResolveInstalledAppIfPresent: %v", err)
	}
	if resolved != entry {
		t.Fatal("missing install should return the deploy-time entry")
	}
}

func TestResolveInstalledAppIfPresent_rejects_incomplete_existing_install(t *testing.T) {
	t.Parallel()

	artifactsDir := t.TempDir()
	destDir := appregistry.MaterializedPath(artifactsDir, "g-issues", "1.0.0")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	_, err := appregistry.ResolveInstalledAppIfPresent(
		"g-issues",
		&config.ProviderEntry{},
		artifactsDir,
		"1.0.0",
	)
	if err == nil {
		t.Fatal("ResolveInstalledAppIfPresent: expected incomplete install error")
	}
}

func TestResolveInstalledApp_rejects_non_app_manifest(t *testing.T) {
	t.Parallel()

	fixture := registrytest.NewInstallFixture(t)
	artifactsDir := t.TempDir()
	materializer := &appregistry.Materializer{
		Registries:   map[string]config.AppRegistryConfig{"toolshed": fixture.Registry},
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

	destDir := appregistry.MaterializedPath(artifactsDir, "g-issues", fixture.Version)
	manifestPath := filepath.Join(destDir, "manifest.yaml")
	_, manifest, err := packageio.ReadManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadManifestFile: %v", err)
	}
	manifest.Kind = providermanifestv1.KindWorkflow
	data, err := packageio.EncodeManifestFormat(manifest, packageio.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeManifestFormat: %v", err)
	}
	if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := appregistry.ResolveInstalledApp(
		"g-issues",
		&config.ProviderEntry{},
		destDir,
		fixture.Version,
	); err == nil {
		t.Fatal("ResolveInstalledApp: expected manifest kind error")
	}
}
