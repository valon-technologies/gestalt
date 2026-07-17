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
	"github.com/valon-technologies/gestalt/server/services/apps/packageio"
)

func TestMaterializer_downloads_and_extracts_artifact(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := registrytest.NewInstallFixture(t)
	artifactsDir := t.TempDir()

	materializer := &appregistry.Materializer{
		Registries: map[string]config.AppRegistryConfig{
			"toolshed": fixture.Registry,
		},
		Reader:       fixture.Reader,
		ArtifactsDir: artifactsDir,
	}

	result, err := materializer.Ensure(ctx, &core.AppInstallation{
		AppName:  "g-issues",
		Version:  fixture.Version,
		Registry: "toolshed",
	})
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	destDir := result.Path

	want := appregistry.MaterializedPath(artifactsDir, "g-issues", fixture.Version)
	if destDir != want {
		t.Fatalf("destDir = %q, want %q", destDir, want)
	}
	if _, err := os.Stat(filepath.Join(destDir, "manifest.yaml")); err != nil {
		t.Fatalf("stat manifest.yaml: %v", err)
	}
}

func TestMaterializer_skips_when_already_materialized(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := registrytest.NewInstallFixture(t)
	artifactsDir := t.TempDir()

	materializer := &appregistry.Materializer{
		Registries: map[string]config.AppRegistryConfig{
			"toolshed": fixture.Registry,
		},
		Reader:       fixture.Reader,
		ArtifactsDir: artifactsDir,
	}
	installation := &core.AppInstallation{
		AppName:  "g-issues",
		Version:  fixture.Version,
		Registry: "toolshed",
	}

	first, err := materializer.Ensure(ctx, installation)
	if err != nil {
		t.Fatalf("first Ensure: %v", err)
	}
	if !first.Changed {
		t.Fatal("first Ensure did not report a changed artifact")
	}

	badReader := fixture.NewDigestMismatchReader(t, fixture.Version)
	materializer.Reader = badReader

	second, err := materializer.Ensure(ctx, installation)
	if err != nil {
		t.Fatalf("second Ensure: %v", err)
	}
	if second.Path != first.Path {
		t.Fatalf("second destDir = %q, want %q", second.Path, first.Path)
	}
	if second.Changed {
		t.Fatal("second Ensure reported an unchanged artifact as changed")
	}
}

func TestMaterializer_retries_after_partial_install(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := registrytest.NewInstallFixture(t)
	artifactsDir := t.TempDir()
	destDir := appregistry.MaterializedPath(artifactsDir, "g-issues", fixture.Version)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(destDir, "manifest.yaml"), []byte("kind: app\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	materializer := &appregistry.Materializer{
		Registries: map[string]config.AppRegistryConfig{
			"toolshed": fixture.Registry,
		},
		Reader:       fixture.Reader,
		ArtifactsDir: artifactsDir,
	}

	result, err := materializer.Ensure(ctx, &core.AppInstallation{
		AppName:  "g-issues",
		Version:  fixture.Version,
		Registry: "toolshed",
	})
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if result.Path != destDir {
		t.Fatalf("destDir = %q, want %q", result.Path, destDir)
	}
	if _, err := os.Stat(filepath.Join(destDir, filepath.FromSlash(packageio.InstalledExecutablePath("g-issues", runtime.GOOS)))); err != nil {
		t.Fatalf("stat installed executable: %v", err)
	}
}

func TestMaterializer_retries_when_manifest_version_mismatches(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := registrytest.NewInstallFixture(t)
	artifactsDir := t.TempDir()

	materializer := &appregistry.Materializer{
		Registries: map[string]config.AppRegistryConfig{
			"toolshed": fixture.Registry,
		},
		Reader:       fixture.Reader,
		ArtifactsDir: artifactsDir,
	}
	installation := &core.AppInstallation{
		AppName:  "g-issues",
		Version:  fixture.Version,
		Registry: "toolshed",
	}

	result, err := materializer.Ensure(ctx, installation)
	if err != nil {
		t.Fatalf("first Ensure: %v", err)
	}
	destDir := result.Path

	manifestPath := filepath.Join(destDir, "manifest.yaml")
	_, manifest, err := packageio.ReadManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadManifestFile: %v", err)
	}
	manifest.Version = "0.0.0-snapshot.gwrong"
	updated, err := packageio.EncodeManifestFormat(manifest, packageio.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeManifestFormat: %v", err)
	}
	if err := os.WriteFile(manifestPath, updated, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := materializer.Ensure(ctx, installation); err != nil {
		t.Fatalf("second Ensure: %v", err)
	}
	_, manifest, err = packageio.ReadManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadManifestFile after rematerialize: %v", err)
	}
	if manifest.Version != fixture.Version {
		t.Fatalf("manifest.Version = %q, want %q", manifest.Version, fixture.Version)
	}
}

func TestMaterializer_rejects_digest_mismatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fixture := registrytest.NewInstallFixture(t)
	artifactsDir := t.TempDir()

	materializer := &appregistry.Materializer{
		Registries: map[string]config.AppRegistryConfig{
			"toolshed": fixture.Registry,
		},
		Reader:       fixture.NewDigestMismatchReader(t, fixture.Version),
		ArtifactsDir: artifactsDir,
	}

	_, err := materializer.Ensure(ctx, &core.AppInstallation{
		AppName:  "g-issues",
		Version:  fixture.Version,
		Registry: "toolshed",
	})
	if err == nil {
		t.Fatal("expected digest mismatch error")
	}
}
