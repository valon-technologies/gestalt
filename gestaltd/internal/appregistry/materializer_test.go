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

	destDir, err := materializer.Materialize(ctx, &core.AppInstallation{
		AppName:  "g-issues",
		Version:  fixture.Version,
		Registry: "toolshed",
	})
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}

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

	first, err := materializer.Materialize(ctx, installation)
	if err != nil {
		t.Fatalf("first Materialize: %v", err)
	}

	badReader := fixture.NewDigestMismatchReader(t, fixture.Version)
	materializer.Reader = badReader

	second, err := materializer.Materialize(ctx, installation)
	if err != nil {
		t.Fatalf("second Materialize: %v", err)
	}
	if second != first {
		t.Fatalf("second destDir = %q, want %q", second, first)
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

	got, err := materializer.Materialize(ctx, &core.AppInstallation{
		AppName:  "g-issues",
		Version:  fixture.Version,
		Registry: "toolshed",
	})
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if got != destDir {
		t.Fatalf("destDir = %q, want %q", got, destDir)
	}
	if _, err := os.Stat(filepath.Join(destDir, filepath.FromSlash(packageio.InstalledExecutablePath("g-issues", runtime.GOOS)))); err != nil {
		t.Fatalf("stat installed executable: %v", err)
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

	_, err := materializer.Materialize(ctx, &core.AppInstallation{
		AppName:  "g-issues",
		Version:  fixture.Version,
		Registry: "toolshed",
	})
	if err == nil {
		t.Fatal("expected digest mismatch error")
	}
}
