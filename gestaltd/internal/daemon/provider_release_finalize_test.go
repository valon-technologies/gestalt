package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

func TestRun_ProviderReleaseFinalizesArchivesWithoutSourceTree(t *testing.T) {
	t.Parallel()

	pluginDir := newSourceProviderReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.4-finalize.1"
	runProviderPackageCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", runtime.GOOS+"/"+runtime.GOARCH,
		"--output", outputDir,
	)
	writeReleaseBuildScript(t, pluginDir, "build.sh", "echo should-not-run >&2\nexit 42\n")

	out, err := runProviderCommandResult(t.TempDir(), "release", "--dist-dir", outputDir, "--version", testVersion)
	if err != nil {
		t.Fatalf("provider release failed: %v\n%s", err, out)
	}

	archiveName := "gestalt-app-" + releaseTestAppName + "_v" + testVersion + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	metadata := readProviderReleaseMetadata(t, outputDir)
	artifact, ok := metadata.Artifacts[providerpkg.CurrentPlatformString()]
	if !ok {
		t.Fatalf("release metadata artifacts missing current platform key %q: %+v", providerpkg.CurrentPlatformString(), metadata.Artifacts)
	}
	if artifact.Path != archiveName {
		t.Fatalf("release metadata artifact path = %q, want %q", artifact.Path, archiveName)
	}
}

func TestRun_ProviderReleaseRejectsDuplicateArchiveTargets(t *testing.T) {
	t.Parallel()

	pluginDir := newSourceProviderReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.4-duplicate.1"
	runProviderPackageCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", runtime.GOOS+"/"+runtime.GOARCH,
		"--output", outputDir,
	)
	archiveName := "gestalt-app-" + releaseTestAppName + "_v" + testVersion + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	data, err := os.ReadFile(filepath.Join(outputDir, archiveName))
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	duplicateName := "gestalt-app-" + releaseTestAppName + "_v" + testVersion + "_duplicate.tar.gz"
	if err := os.WriteFile(filepath.Join(outputDir, duplicateName), data, 0o644); err != nil {
		t.Fatalf("write duplicate archive: %v", err)
	}

	out, err := runProviderCommandResult(pluginDir, "release", "--dist-dir", outputDir, "--version", testVersion)
	if err == nil {
		t.Fatalf("expected duplicate target failure, got output: %s", out)
	}
	if !strings.Contains(string(out), "multiple release archives map to target") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestRun_ProviderReleaseRejectsMismatchedArchiveManifests(t *testing.T) {
	t.Parallel()

	pluginDir := newSourceProviderReleaseFixture(t, t.TempDir())
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
	runProviderPackageCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", runtime.GOOS+"/"+runtime.GOARCH+","+providerpkg.PlatformString(alternatePlatform.GOOS, alternatePlatform.GOARCH),
		"--output", outputDir,
	)
	archiveName := platformArchiveNameForTest(releaseTestAppName, testVersion, alternatePlatform.GOOS, alternatePlatform.GOARCH)
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifestPath, manifest := readManifestFromDir(t, extractDir)
	manifest.DisplayName = "Different Release Test"
	data, err := providerpkg.EncodeManifestFormat(manifest, providerpkg.ManifestFormatFromPath(manifestPath))
	if err != nil {
		t.Fatalf("encode manifest: %v", err)
	}
	if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := providerpkg.CreatePackageFromDir(extractDir, filepath.Join(outputDir, archiveName)); err != nil {
		t.Fatalf("rewrite archive: %v", err)
	}

	out, err := runProviderCommandResult(pluginDir, "release", "--dist-dir", outputDir, "--version", testVersion)
	if err == nil {
		t.Fatalf("expected mismatched manifest failure, got output: %s", out)
	}
	if !strings.Contains(string(out), "manifest does not match other release archives") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestRun_ProviderReleaseRejectsArchiveVersionMismatch(t *testing.T) {
	t.Parallel()

	pluginDir := newUIReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	runProviderPackageCommand(t, pluginDir,
		"--version", "1.0.0",
		"--output", outputDir,
	)

	out, err := runProviderCommandResult(pluginDir, "release", "--dist-dir", outputDir, "--version", "1.0.1")
	if err == nil {
		t.Fatalf("expected version mismatch failure, got output: %s", out)
	}
	if !strings.Contains(string(out), "does not match --version") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestRun_ProviderReleaseRejectsNoArchives(t *testing.T) {
	t.Parallel()

	out, err := runProviderCommandResult(t.TempDir(), "release", "--dist-dir", t.TempDir())
	if err == nil {
		t.Fatalf("expected no archives failure, got output: %s", out)
	}
	if !strings.Contains(string(out), "no .tar.gz release archives found") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestRun_ProviderReleaseRejectsMultipleRootManifests(t *testing.T) {
	t.Parallel()

	pluginDir := newUIReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "1.0.0"
	runProviderPackageCommand(t, pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)
	archiveName := "gestalt-app-" + uiTestAppName + "_v" + testVersion + ".tar.gz"
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifestData, err := os.ReadFile(filepath.Join(extractDir, providerpkg.ManifestFile))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(extractDir, "manifest.yml"), manifestData, 0o644); err != nil {
		t.Fatalf("write second manifest: %v", err)
	}
	if err := providerpkg.CreatePackageFromDir(extractDir, filepath.Join(outputDir, archiveName)); err != nil {
		t.Fatalf("rewrite archive: %v", err)
	}

	out, err := runProviderCommandResult(pluginDir, "release", "--dist-dir", outputDir, "--version", testVersion)
	if err == nil {
		t.Fatalf("expected multiple manifest failure, got output: %s", out)
	}
	if !strings.Contains(string(out), "contains multiple root provider manifests") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestRun_ProviderReleaseRejectsCorruptArchive(t *testing.T) {
	t.Parallel()

	pluginDir := newUIReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "1.0.0"
	runProviderPackageCommand(t, pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)
	archiveName := "gestalt-app-" + uiTestAppName + "_v" + testVersion + ".tar.gz"
	if err := os.WriteFile(filepath.Join(outputDir, archiveName), []byte("not a gzip archive\n"), 0o644); err != nil {
		t.Fatalf("rewrite corrupt archive: %v", err)
	}

	out, err := runProviderCommandResult(pluginDir, "release", "--dist-dir", outputDir, "--version", testVersion)
	if err == nil {
		t.Fatalf("expected corrupt archive failure, got output: %s", out)
	}
}
