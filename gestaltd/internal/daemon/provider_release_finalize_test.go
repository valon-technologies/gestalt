package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	artifact, ok := metadata.Artifacts[providerpkg.CurrentPlatformString()]
	if !ok {
		t.Fatalf("release metadata artifacts missing current platform key %q: %+v", providerpkg.CurrentPlatformString(), metadata.Artifacts)
	}
	if artifact.Path != archiveName {
		t.Fatalf("release metadata artifact path = %q, want %q", artifact.Path, archiveName)
	}
}

func TestProviderReleaseRejectsDuplicateArchiveTargets(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	const testVersion = "0.0.4-duplicate.1"
	writeProviderReleaseArchiveForTest(t, outputDir, platformArchiveNameForTest(releaseTestAppName, testVersion, runtime.GOOS, runtime.GOARCH), providerReleaseManifestForTest(testVersion, "Release Test", runtime.GOOS, runtime.GOARCH))
	writeProviderReleaseArchiveForTest(t, outputDir, "gestalt-app-"+releaseTestAppName+"_v"+testVersion+"_duplicate.tar.gz", providerReleaseManifestForTest(testVersion, "Release Test", runtime.GOOS, runtime.GOARCH))

	_, _, _, err := collectReleaseArchives(outputDir, testVersion)
	if err == nil || !strings.Contains(err.Error(), "multiple release archives map to target") {
		t.Fatalf("collectReleaseArchives error = %v, want duplicate target failure", err)
	}
}

func TestProviderReleaseRejectsMismatchedArchiveManifests(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	const testVersion = "0.0.4-mismatch.1"
	alternatePlatform := releasePlatform{}
	for _, platform := range defaultReleasePlatformsForTest(t) {
		if platform.GOOS != runtime.GOOS || platform.GOARCH != runtime.GOARCH {
			alternatePlatform = platform
			break
		}
	}
	if alternatePlatform.GOOS == "" {
		t.Fatal("no alternate release platform available")
	}
	writeProviderReleaseArchiveForTest(t, outputDir, platformArchiveNameForTest(releaseTestAppName, testVersion, runtime.GOOS, runtime.GOARCH), providerReleaseManifestForTest(testVersion, "Release Test", runtime.GOOS, runtime.GOARCH))
	writeProviderReleaseArchiveForTest(t, outputDir, platformArchiveNameForTest(releaseTestAppName, testVersion, alternatePlatform.GOOS, alternatePlatform.GOARCH), providerReleaseManifestForTest(testVersion, "Different Release Test", alternatePlatform.GOOS, alternatePlatform.GOARCH))

	_, _, _, err := collectReleaseArchives(outputDir, testVersion)
	if err == nil || !strings.Contains(err.Error(), "manifest does not match other release archives") {
		t.Fatalf("collectReleaseArchives error = %v, want mismatched manifest failure", err)
	}
}

func TestProviderReleaseRejectsArchiveVersionMismatch(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	writeProviderReleaseArchiveForTest(t, outputDir, "gestalt-app-"+uiTestAppName+"_v1.0.0.tar.gz", uiReleaseManifestForTest("1.0.0"))

	_, _, _, err := collectReleaseArchives(outputDir, "1.0.1")
	if err == nil || !strings.Contains(err.Error(), "does not match --version") {
		t.Fatalf("collectReleaseArchives error = %v, want version mismatch failure", err)
	}
}

func TestProviderReleaseRejectsNoArchives(t *testing.T) {
	t.Parallel()

	_, _, _, err := collectReleaseArchives(t.TempDir(), "")
	if err == nil || !strings.Contains(err.Error(), "no .tar.gz release archives found") {
		t.Fatalf("collectReleaseArchives error = %v, want no archives failure", err)
	}
}

func TestProviderReleaseRejectsMultipleRootManifests(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	const testVersion = "1.0.0"
	packageDir := t.TempDir()
	writeProviderReleaseManifestSupportFilesForTest(t, packageDir, uiReleaseManifestForTest(testVersion))
	writeReleasedManifestForArchiveTest(t, packageDir, uiReleaseManifestForTest(testVersion))
	archiveName := "gestalt-app-" + uiTestAppName + "_v" + testVersion + ".tar.gz"
	manifestData, err := os.ReadFile(filepath.Join(packageDir, providerpkg.ManifestFile))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(packageDir, "manifest.yml"), manifestData, 0o644); err != nil {
		t.Fatalf("write second manifest: %v", err)
	}
	if err := providerpkg.CreatePackageFromDir(packageDir, filepath.Join(outputDir, archiveName)); err != nil {
		t.Fatalf("CreatePackageFromDir(%s): %v", archiveName, err)
	}

	_, _, _, err = collectReleaseArchives(outputDir, testVersion)
	if err == nil || !strings.Contains(err.Error(), "contains multiple root provider manifests") {
		t.Fatalf("collectReleaseArchives error = %v, want multiple manifest failure", err)
	}
}

func TestProviderReleaseRejectsCorruptArchive(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	const testVersion = "1.0.0"
	archiveName := "gestalt-app-" + uiTestAppName + "_v" + testVersion + ".tar.gz"
	if err := os.WriteFile(filepath.Join(outputDir, archiveName), []byte("not a gzip archive\n"), 0o644); err != nil {
		t.Fatalf("rewrite corrupt archive: %v", err)
	}

	_, _, _, err := collectReleaseArchives(outputDir, testVersion)
	if err == nil {
		t.Fatal("expected corrupt archive failure")
	}
}

func TestProviderReleaseMetadataIncludesPlatformArchives(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	linuxArchive := writeProviderReleaseArchiveForTest(t, outputDir, "gestalt-app-release-test_v1.0.0_linux_amd64.tar.gz", providerReleaseManifestForTest("1.0.0", "Release Test", "linux", "amd64"))
	darwinArchive := writeProviderReleaseArchiveForTest(t, outputDir, "gestalt-app-release-test_v1.0.0_darwin_arm64.tar.gz", providerReleaseManifestForTest("1.0.0", "Release Test", "darwin", "arm64"))
	linuxSHA, err := providerpkg.ArchiveDigest(linuxArchive)
	if err != nil {
		t.Fatalf("ArchiveDigest(linux): %v", err)
	}
	darwinSHA, err := providerpkg.ArchiveDigest(darwinArchive)
	if err != nil {
		t.Fatalf("ArchiveDigest(darwin): %v", err)
	}
	metadata, err := buildProviderReleaseMetadata(
		providerReleaseManifestForTest("1.0.0", "Release Test", "linux", "amd64"),
		"1.0.0",
		[]releaseArchive{
			{Path: linuxArchive, SHA256: linuxSHA, Target: "linux/amd64"},
			{Path: darwinArchive, SHA256: darwinSHA, Target: "darwin/arm64"},
		},
	)
	if err != nil {
		t.Fatalf("buildProviderReleaseMetadata: %v", err)
	}
	for target, wantSHA := range map[string]string{
		"linux/amd64":  linuxSHA,
		"darwin/arm64": darwinSHA,
	} {
		artifact, ok := metadata.Artifacts[target]
		if !ok {
			t.Fatalf("metadata artifacts missing %s: %+v", target, metadata.Artifacts)
		}
		if artifact.SHA256 != wantSHA {
			t.Fatalf("metadata artifact %s sha = %q, want %q", target, artifact.SHA256, wantSHA)
		}
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

func uiReleaseManifestForTest(version string) *providermanifestv1.Manifest {
	return &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindUI,
		Source:      uiTestSource,
		Version:     version,
		DisplayName: "UI Test",
		IconFile:    releaseTestIconPath,
		Spec:        &providermanifestv1.Spec{AssetRoot: uiTestAssetRoot},
	}
}
