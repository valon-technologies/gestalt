package e2e

import (
	"path/filepath"
	"runtime"
	"testing"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

func TestRun_ProviderReleaseFinalizesArchivesWithoutSourceTree(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	const testVersion = "0.0.4-finalize.1"
	archiveName := platformArchiveNameForTest(releaseTestAppName, testVersion, runtime.GOOS, runtime.GOARCH)
	writeProviderReleaseArchiveForTest(t, outputDir, archiveName, providerReleaseManifestForTest(testVersion, "Release Test", runtime.GOOS, runtime.GOARCH))

	out, err := runProviderCommandResult(t.TempDir(), "release", "--dist-dir", outputDir, "--version", testVersion)
	if err != nil {
		t.Fatalf("provider release failed: %v\n%s", err, out)
	}

	metadata := readProviderReleaseMetadata(t, outputDir)
	artifact := providerReleaseArtifactForTarget(t, metadata, providerpkg.CurrentPlatformString())
	if artifact.Path != archiveName {
		t.Fatalf("release metadata artifact path = %q, want %q", artifact.Path, archiveName)
	}
}

func providerReleaseManifestForTest(version, displayName, goos, goarch string) *providermanifestv1.Manifest {
	artifactPath := filepath.ToSlash(filepath.Join("bin", "provider-"+goos+"-"+goarch))
	return &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindApp,
		Source:      releaseTestSource,
		Version:     version,
		DisplayName: displayName,
		IconFile:    releaseTestIconPath,
		Spec:        &providermanifestv1.Spec{},
		Artifacts: []providermanifestv1.Artifact{{
			OS:   goos,
			Arch: goarch,
			Path: artifactPath,
		}},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: artifactPath},
	}
}
