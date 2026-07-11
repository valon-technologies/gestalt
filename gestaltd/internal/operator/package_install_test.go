package operator

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/packageio"
)

func TestInstallPackageAsPreservesEntrypointArgsWhenRenamingExecutable(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	packageRoot := filepath.Join(root, "package")
	artifactRel := packageio.PackageExecutablePath("source-name", runtime.GOOS)
	artifactPath := filepath.Join(packageRoot, filepath.FromSlash(artifactRel))
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(artifactPath), err)
	}
	if err := os.WriteFile(artifactPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(%q): %v", artifactPath, err)
	}
	digest, err := packageio.FileSHA256(artifactPath)
	if err != nil {
		t.Fatalf("FileSHA256(%q): %v", artifactPath, err)
	}

	wantArgs := []string{"--serve", "--config", "provider.yaml"}
	manifest := &providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindIdentity,
		Source:  "github.com/test/auth/source-name",
		Version: "0.0.1",
		Spec:    &providermanifestv1.Spec{},
		Entrypoint: &providermanifestv1.Entrypoint{
			ArtifactPath: artifactRel,
			Args:         append([]string(nil), wantArgs...),
		},
		Artifacts: []providermanifestv1.Artifact{
			{
				OS:     runtime.GOOS,
				Arch:   runtime.GOARCH,
				Path:   artifactRel,
				SHA256: digest,
			},
		},
	}
	manifestData, err := packageio.EncodeManifestFormat(manifest, packageio.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeManifestFormat: %v", err)
	}
	manifestPath := filepath.Join(packageRoot, "manifest.yaml")
	if err := os.WriteFile(manifestPath, manifestData, 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", manifestPath, err)
	}

	packagePath := filepath.Join(root, "provider.tar.gz")
	if err := packageio.CreatePackageFromDir(packageRoot, packagePath); err != nil {
		t.Fatalf("CreatePackageFromDir: %v", err)
	}

	installRoot := filepath.Join(root, "install")
	installed, err := installPackageAs(context.Background(), packagePath, installRoot, "configuredName")
	if err != nil {
		t.Fatalf("installPackageAs: %v", err)
	}

	wantArtifactRel := packageio.InstalledExecutablePath("configuredName", runtime.GOOS)
	if installed.Manifest.Entrypoint == nil {
		t.Fatal("installed manifest entrypoint is nil")
	}
	if installed.Manifest.Entrypoint.ArtifactPath != wantArtifactRel {
		t.Fatalf("installed entrypoint artifactPath = %q, want %q", installed.Manifest.Entrypoint.ArtifactPath, wantArtifactRel)
	}
	if !slices.Equal(installed.Manifest.Entrypoint.Args, wantArgs) {
		t.Fatalf("installed entrypoint args = %v, want %v", installed.Manifest.Entrypoint.Args, wantArgs)
	}
	if installed.ExecutablePath != filepath.Join(installRoot, filepath.FromSlash(wantArtifactRel)) {
		t.Fatalf("installed executable = %q, want %q", installed.ExecutablePath, filepath.Join(installRoot, filepath.FromSlash(wantArtifactRel)))
	}
	if _, err := os.Stat(filepath.Join(installRoot, filepath.FromSlash(wantArtifactRel))); err != nil {
		t.Fatalf("stat renamed executable: %v", err)
	}
	if _, err := os.Stat(filepath.Join(installRoot, filepath.FromSlash(artifactRel))); err == nil {
		t.Fatalf("original executable path %q still exists after install rename", artifactRel)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat original executable path %q: %v", artifactRel, err)
	}

	_, diskManifest, err := packageio.ReadManifestFile(installed.ManifestPath)
	if err != nil {
		t.Fatalf("ReadManifestFile(%q): %v", installed.ManifestPath, err)
	}
	if diskManifest.Entrypoint == nil {
		t.Fatal("disk manifest entrypoint is nil")
	}
	if diskManifest.Entrypoint.ArtifactPath != wantArtifactRel {
		t.Fatalf("disk entrypoint artifactPath = %q, want %q", diskManifest.Entrypoint.ArtifactPath, wantArtifactRel)
	}
	if !slices.Equal(diskManifest.Entrypoint.Args, wantArgs) {
		t.Fatalf("disk entrypoint args = %v, want %v", diskManifest.Entrypoint.Args, wantArgs)
	}
}
