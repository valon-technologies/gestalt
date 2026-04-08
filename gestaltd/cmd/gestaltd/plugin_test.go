package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/session"
	"github.com/valon-technologies/gestalt/server/internal/pluginhost"
	"github.com/valon-technologies/gestalt/server/internal/pluginpkg"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

const (
	releaseTestPluginName      = "release-test"
	releaseTestSource          = "github.com/testowner/plugins/catalog/release-test"
	releaseTestModule          = "example.com/release-test"
	releaseTestIconPath        = "branding/icon.svg"
	releaseProviderSchemaPath  = "schemas/provider.schema.json"
	webUITestPluginName        = "webui-test"
	webUITestSource            = "github.com/testowner/plugins/catalog/webui-test"
	webUITestAssetRoot         = "out"
	prebuiltProviderPluginName = "prebuilt-provider"
	prebuiltProviderSource     = "github.com/testowner/plugins/prebuilt-provider"
	prebuiltProviderBinaryPath = "bin/provider"
	authReleasePluginName      = "auth-release"
	authReleaseSource          = "github.com/testowner/plugins/auth-release"
	authReleaseSchemaPath      = "schemas/auth.schema.json"
	datastoreReleasePluginName = "datastore-release"
	datastoreReleaseSource     = "github.com/testowner/plugins/datastore-release"
	datastoreReleaseSchemaPath = "schemas/datastore.schema.json"
	rustReleasePluginName      = "provider-rust"
	rustWrapperBinaryName      = "gestalt-provider-wrapper"
)

func TestRun_PluginHelpExitsCleanly(t *testing.T) {
	t.Parallel()

	out, err := runPluginCommandResult("", "--help")
	if err != nil {
		t.Fatalf("expected exit 0 for 'plugin --help', got error: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "gestaltd plugin <command> [flags]") {
		t.Fatalf("expected plugin usage output, got: %s", out)
	}
	if !strings.Contains(string(out), "release") {
		t.Fatalf("expected release in help output, got: %s", out)
	}
	for _, removed := range []string{"\n  install", "\n  inspect", "\n  list", "\n  init", "\n  package"} {
		if strings.Contains(string(out), removed) {
			t.Fatalf("expected %q absent from help, got: %s", removed, out)
		}
	}
}

func TestRun_PluginReleaseHelpExitsCleanly(t *testing.T) {
	t.Parallel()

	out, err := runPluginCommandResult("", "release", "--help")
	if err != nil {
		t.Fatalf("expected exit 0 for 'plugin release --help', got error: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "--version") {
		t.Fatalf("expected --version in release help, got: %s", out)
	}
}

func TestRun_PluginRootReturnsHelpWhenNoSubcommandProvided(t *testing.T) {
	t.Parallel()

	out, err := runPluginCommandResult("")
	if err != nil {
		t.Fatalf("expected exit 0 for 'plugin', got error: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "gestaltd plugin <command> [flags]") {
		t.Fatalf("expected plugin usage output, got: %s", out)
	}
}

func TestRun_PluginRejectsUnknownSubcommand(t *testing.T) {
	t.Parallel()

	out, err := runPluginCommandResult("", "bogus")
	if err == nil {
		t.Fatal("expected error for unknown plugin subcommand")
	}
	if !strings.Contains(string(out), "unknown plugin command") || !strings.Contains(string(out), "bogus") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestRun_PluginRejectsRemovedPackageSubcommand(t *testing.T) {
	t.Parallel()

	out, err := runPluginCommandResult("", "package")
	if err == nil {
		t.Fatal("expected error for removed plugin package subcommand")
	}
	if !strings.Contains(string(out), "unknown plugin command") || !strings.Contains(string(out), "package") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestRun_PluginReleaseRequiresVersion(t *testing.T) {
	t.Parallel()

	pluginDir := newSourceProviderReleaseFixture(t, t.TempDir())
	out, err := runPluginCommandResult(pluginDir, "release")
	if err == nil {
		t.Fatal("expected error when --version missing")
	}
	if !strings.Contains(string(out), "--version is required") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestRun_PluginReleaseRejectsInvalidManifest(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		manifestYAML string
		wantError    string
	}{
		{
			name: "rest surface requires base_url",
			manifestYAML: `
source: github.com/testowner/plugins/invalid
version: 0.0.1-alpha.1
kinds:
  - plugin
provider:
  surfaces:
    rest:
      operations:
        - name: list_items
          method: GET
          path: /items
`,
			wantError: "provider.base_url is required",
		},
		{
			name: "exec block requires artifact path",
			manifestYAML: `
source: github.com/testowner/plugins/invalid
version: 0.0.1-alpha.1
kinds:
  - plugin
provider:
  exec: {}
`,
			wantError: "entrypoints.provider.artifact_path is required",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			pluginDir := filepath.Join(t.TempDir(), "invalid-plugin")
			if err := os.MkdirAll(pluginDir, 0755); err != nil {
				t.Fatalf("MkdirAll(pluginDir): %v", err)
			}
			writeTestFile(t, pluginDir, "plugin.yaml", []byte(tc.manifestYAML), 0644)

			out, err := runPluginReleaseCommandResult(pluginDir, "--version", "0.0.1-test")
			if err == nil {
				t.Fatal("expected invalid manifest error")
			}
			if !strings.Contains(string(out), tc.wantError) {
				t.Fatalf("unexpected output: %s", out)
			}
		})
	}
}

func TestE2EPluginReleaseBigquery(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Join("..", "..", "..")
	bigqueryDir := filepath.Join(repoRoot, "plugins", "bigquery")
	if _, err := os.Stat(filepath.Join(bigqueryDir, "go.mod")); err != nil {
		t.Skipf("bigquery plugin not found: %v", err)
	}

	outputDir := t.TempDir()
	const testVersion = "0.0.1-test"
	const testPlatform = "linux/amd64"

	cmd := exec.Command(gestaltdBin, "plugin", "release",
		"--version", testVersion,
		"--platform", testPlatform,
		"--output", outputDir,
	)
	cmd.Dir = bigqueryDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("plugin release failed: %v\n%s", err, out)
	}

	archiveName := "gestalt-plugin-bigquery_v" + testVersion + "_linux_amd64.tar.gz"
	archivePath := filepath.Join(outputDir, archiveName)
	if _, err := os.Stat(archivePath); err != nil {
		t.Fatalf("expected archive %s to exist: %v", archiveName, err)
	}

	extractDir := filepath.Join(outputDir, "extracted")
	if err := pluginpkg.ExtractPackage(archivePath, extractDir); err != nil {
		t.Fatalf("extract archive: %v", err)
	}

	_, manifest := readManifestFromDir(t, extractDir)
	if manifest.Version != testVersion {
		t.Fatalf("manifest version = %q, want %q", manifest.Version, testVersion)
	}
	if len(manifest.Artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(manifest.Artifacts))
	}
	artifact := manifest.Artifacts[0]
	if artifact.OS != "linux" || artifact.Arch != "amd64" {
		t.Fatalf("artifact platform = %s/%s, want linux/amd64", artifact.OS, artifact.Arch)
	}
	if artifact.Path != "gestalt-plugin-bigquery" {
		t.Fatalf("artifact path = %q, want %q", artifact.Path, "gestalt-plugin-bigquery")
	}

	binaryPath := filepath.Join(extractDir, artifact.Path)
	if _, err := os.Stat(binaryPath); err != nil {
		t.Fatalf("binary not in archive: %v", err)
	}
	digest, err := pluginpkg.FileSHA256(binaryPath)
	if err != nil {
		t.Fatalf("hash binary: %v", err)
	}
	if digest != artifact.SHA256 {
		t.Fatalf("binary sha256 = %s, manifest says %s", digest, artifact.SHA256)
	}

	checksumPath := filepath.Join(outputDir, "checksums.txt")
	checksumData, err := os.ReadFile(checksumPath)
	if err != nil {
		t.Fatalf("read checksums.txt: %v", err)
	}
	if !strings.Contains(string(checksumData), archiveName) {
		t.Fatalf("checksums.txt does not reference %s: %s", archiveName, checksumData)
	}

	iconPath := filepath.Join(extractDir, "assets", "icon.svg")
	if _, err := os.Stat(iconPath); err != nil {
		t.Fatalf("expected assets/icon.svg in archive: %v", err)
	}
}

func TestRun_PluginReleaseBuildsPythonSourcePluginForCurrentPlatform(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake Python build fixture is POSIX-only")
	}

	t.Setenv("GESTALT_TEST_PYINSTALLER_BINARY", pluginBin)
	t.Setenv("PATH", pathWithoutGo(t))

	pluginDir := newPythonSourceReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.12-test"

	runPluginReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", runtime.GOOS+"/"+runtime.GOARCH,
		"--output", outputDir,
	)

	archiveName := expectedPythonArchiveName(testVersion, runtime.GOOS, runtime.GOARCH)
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifest := readReleasedManifest(t, outputDir, archiveName)

	binaryName := releaseBinaryName("python-release", runtime.GOOS)
	if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Path != binaryName {
		t.Fatalf("artifacts = %+v, want path %q", manifest.Artifacts, binaryName)
	}
	if manifest.Entrypoints.Provider == nil || manifest.Entrypoints.Provider.ArtifactPath != binaryName {
		t.Fatalf("provider entrypoint = %+v, want artifact path %q", manifest.Entrypoints.Provider, binaryName)
	}

	artifactPath := filepath.Join(extractDir, binaryName)
	if _, err := os.Stat(artifactPath); err != nil {
		t.Fatalf("expected %s in archive: %v", binaryName, err)
	}

	ctx := context.Background()
	prov, err := pluginhost.NewExecutableProvider(ctx, pluginhost.ExecConfig{
		Command: artifactPath,
		StaticSpec: pluginhost.StaticProviderSpec{
			Name: "python-release",
		},
		Config: map[string]any{"greeting": "Hi"},
	})
	if err != nil {
		t.Fatalf("NewExecutableProvider: %v", err)
	}
	defer func() {
		if closer, ok := prov.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}()

	result, err := prov.Execute(ctx, "greet", map[string]any{"name": "Ada"}, "")
	if err != nil {
		t.Fatalf("Execute(greet): %v", err)
	}
	if result.Status != 200 {
		t.Fatalf("status = %d, want 200", result.Status)
	}
	if !strings.Contains(result.Body, "Hi, Ada!") {
		t.Fatalf("body = %q, want greeting", result.Body)
	}
}

func TestRun_PluginReleaseDefaultsGoSourcePluginToHostPlatform(t *testing.T) {
	t.Parallel()

	pluginDir := newGoSourceReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.12-go-default"

	runPluginReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)

	archiveName := "gestalt-plugin-release-test_v" + testVersion + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	manifest := readReleasedManifest(t, outputDir, archiveName)
	if len(manifest.Artifacts) != 1 {
		t.Fatalf("artifacts = %+v, want exactly one host-platform artifact", manifest.Artifacts)
	}
	if manifest.Artifacts[0].OS != runtime.GOOS || manifest.Artifacts[0].Arch != runtime.GOARCH {
		t.Fatalf("artifact platform = %s/%s, want %s/%s", manifest.Artifacts[0].OS, manifest.Artifacts[0].Arch, runtime.GOOS, runtime.GOARCH)
	}
}

func TestRun_PluginReleaseBuildsGoSourcePluginForAllPlatforms(t *testing.T) {
	t.Parallel()

	pluginDir := newGoSourceReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.12-go-all"

	runPluginReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", allPlatformsValue,
		"--output", outputDir,
	)

	for _, platform := range defaultReleasePlatformsForTest(t) {
		archiveName := "gestalt-plugin-release-test_v" + testVersion + "_" + platform.GOOS + "_" + platform.GOARCH + ".tar.gz"
		manifest := readReleasedManifest(t, outputDir, archiveName)
		if len(manifest.Artifacts) != 1 {
			t.Fatalf("artifacts for %s/%s = %+v, want one artifact", platform.GOOS, platform.GOARCH, manifest.Artifacts)
		}
		if manifest.Artifacts[0].OS != platform.GOOS || manifest.Artifacts[0].Arch != platform.GOARCH {
			t.Fatalf("artifact platform = %s/%s, want %s/%s", manifest.Artifacts[0].OS, manifest.Artifacts[0].Arch, platform.GOOS, platform.GOARCH)
		}
	}
}

func TestRun_PluginReleaseDefaultsPythonSourcePluginToHostPlatform(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake Python build fixture is POSIX-only")
	}

	t.Setenv("GESTALT_TEST_PYINSTALLER_BINARY", pluginBin)
	t.Setenv("PATH", pathWithoutGo(t))

	pluginDir := newPythonSourceReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.12-default"

	runPluginReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)

	archiveName := expectedPythonArchiveName(testVersion, runtime.GOOS, runtime.GOARCH)
	manifest := readReleasedManifest(t, outputDir, archiveName)
	if len(manifest.Artifacts) != 1 {
		t.Fatalf("artifacts = %+v, want exactly one host-platform artifact", manifest.Artifacts)
	}
	assertExpectedPythonArtifactPlatform(t, manifest.Artifacts[0], runtime.GOOS, runtime.GOARCH)
}

func TestRun_PluginReleaseBuildsPythonSourcePluginForAllPlatforms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake Python build fixture is POSIX-only")
	}

	t.Setenv("GESTALT_TEST_PYINSTALLER_BINARY", pluginBin)
	t.Setenv("PATH", pathWithoutGo(t))

	pluginDir := newPythonSourceReleaseFixture(t, t.TempDir())
	configurePythonReleaseInterpretersForAllPlatforms(t, pluginDir)

	outputDir := t.TempDir()
	const testVersion = "0.0.12-python-all"

	runPluginReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", allPlatformsValue,
		"--output", outputDir,
	)

	for _, platform := range defaultReleasePlatformsForTest(t) {
		archiveName := expectedPythonArchiveName(testVersion, platform.GOOS, platform.GOARCH)
		manifest := readReleasedManifest(t, outputDir, archiveName)
		if len(manifest.Artifacts) != 1 {
			t.Fatalf("artifacts for %s/%s = %+v, want one artifact", platform.GOOS, platform.GOARCH, manifest.Artifacts)
		}
		assertExpectedPythonArtifactPlatform(t, manifest.Artifacts[0], platform.GOOS, platform.GOARCH)
	}
}

func TestRun_PluginReleaseBuildsPythonSourcePluginForRequestedPlatforms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake Python build fixture is POSIX-only")
	}

	t.Setenv("GESTALT_TEST_PYINSTALLER_BINARY", pluginBin)
	t.Setenv("PATH", pathWithoutGo(t))

	pluginDir := newPythonSourceReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	otherGOOS, otherGOARCH := pythonReleaseOtherPlatform()
	otherPlatform := otherGOOS + "/" + otherGOARCH
	writeFakePythonReleaseInterpreter(t, filepath.Join(pluginDir, "cross-python"), otherGOOS, otherGOARCH)
	t.Setenv(pluginpkgPythonEnvVar(otherGOOS, otherGOARCH), filepath.Join(pluginDir, "cross-python"))

	runPluginReleaseCommand(t, pluginDir,
		"--version", "0.0.13-test",
		"--platform", runtime.GOOS+"/"+runtime.GOARCH+","+otherPlatform,
		"--output", outputDir,
	)

	currentArchive := expectedPythonArchiveName("0.0.13-test", runtime.GOOS, runtime.GOARCH)
	otherArchive := expectedPythonArchiveName("0.0.13-test", otherGOOS, otherGOARCH)
	for _, archiveName := range []string{currentArchive, otherArchive} {
		extractDir := extractReleasedArchive(t, outputDir, archiveName)
		manifest := readReleasedManifest(t, outputDir, archiveName)
		binaryName := releaseBinaryName("python-release", manifest.Artifacts[0].OS)
		if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Path != binaryName {
			t.Fatalf("artifacts = %+v, want path %q", manifest.Artifacts, binaryName)
		}
		if _, err := os.Stat(filepath.Join(extractDir, binaryName)); err != nil {
			t.Fatalf("expected %s in archive: %v", binaryName, err)
		}
	}
}

func TestRun_PluginReleaseBuildsRustSourcePluginForCurrentPlatform(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake cargo test fixture is POSIX-only")
	}

	hostTarget, _, err := pluginpkgRustTargetTriple(runtime.GOOS, runtime.GOARCH, "")
	if err != nil {
		t.Fatalf("pluginpkgRustTargetTriple(host): %v", err)
	}

	fakeCargoDir := t.TempDir()
	writeFakeRustReleaseCargo(t, filepath.Join(fakeCargoDir, "cargo"), fakeRustCargoConfig{
		ExpectedPluginName:   rustReleasePluginName,
		ExpectedServeExport:  "__gestalt_serve",
		ExpectedCatalogWrite: true,
		GeneratedCatalog:     rustReleasePluginName,
		DelegateBinary:       pluginBin,
		AllowedTargets:       []string{hostTarget},
	})
	t.Setenv("PATH", fakeCargoDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	pluginDir := newRustSourceReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.12-rust-current"

	runPluginReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", runtime.GOOS+"/"+runtime.GOARCH,
		"--output", outputDir,
	)

	archiveName := expectedRustArchiveName(testVersion, runtime.GOOS, runtime.GOARCH, "")
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifest := readReleasedManifest(t, outputDir, archiveName)
	binaryName := releaseBinaryName(rustReleasePluginName, runtime.GOOS)

	if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Path != binaryName {
		t.Fatalf("artifacts = %+v, want path %q", manifest.Artifacts, binaryName)
	}
	assertExpectedRustArtifactPlatform(t, manifest.Artifacts[0], runtime.GOOS, runtime.GOARCH, "")
	if manifest.Entrypoints.Provider == nil || manifest.Entrypoints.Provider.ArtifactPath != binaryName {
		t.Fatalf("provider entrypoint = %+v, want artifact path %q", manifest.Entrypoints.Provider, binaryName)
	}

	artifactPath := filepath.Join(extractDir, binaryName)
	if _, err := os.Stat(artifactPath); err != nil {
		t.Fatalf("expected %s in archive: %v", binaryName, err)
	}
	catalogPath := filepath.Join(extractDir, pluginpkg.StaticCatalogFile)
	catalogData, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatalf("read generated catalog: %v", err)
	}
	if !strings.Contains(string(catalogData), "id: greet") {
		t.Fatalf("unexpected generated catalog: %s", catalogData)
	}

	ctx := context.Background()
	prov, err := pluginhost.NewExecutableProvider(ctx, pluginhost.ExecConfig{
		Command: artifactPath,
		StaticSpec: pluginhost.StaticProviderSpec{
			Name: rustReleasePluginName,
		},
		Config: map[string]any{"greeting": "Hi"},
	})
	if err != nil {
		t.Fatalf("NewExecutableProvider: %v", err)
	}
	defer func() {
		if closer, ok := prov.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}()

	result, err := prov.Execute(ctx, "greet", map[string]any{"name": "Ada"}, "")
	if err != nil {
		t.Fatalf("Execute(greet): %v", err)
	}
	if result.Status != 200 {
		t.Fatalf("status = %d, want 200", result.Status)
	}
	if !strings.Contains(result.Body, "Hi, Ada!") {
		t.Fatalf("body = %q, want greeting", result.Body)
	}
}

func TestRun_PluginReleaseBuildsRustSourcePluginForExplicitLinuxTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake cargo test fixture is POSIX-only")
	}

	hostTarget, _, err := pluginpkgRustTargetTriple(runtime.GOOS, runtime.GOARCH, "")
	if err != nil {
		t.Fatalf("pluginpkgRustTargetTriple(host): %v", err)
	}
	explicitTarget, _, err := pluginpkgRustTargetTriple("linux", "amd64", pluginpkg.LinuxLibCMusl)
	if err != nil {
		t.Fatalf("pluginpkgRustTargetTriple(linux/amd64/musl): %v", err)
	}

	fakeCargoDir := t.TempDir()
	writeFakeRustReleaseCargo(t, filepath.Join(fakeCargoDir, "cargo"), fakeRustCargoConfig{
		ExpectedPluginName:   rustReleasePluginName,
		ExpectedServeExport:  "__gestalt_serve",
		ExpectedCatalogWrite: true,
		GeneratedCatalog:     rustReleasePluginName,
		DelegateBinary:       pluginBin,
		AllowedTargets:       []string{hostTarget, explicitTarget},
	})
	t.Setenv("PATH", fakeCargoDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	pluginDir := newRustSourceReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.12-rust-musl"

	runPluginReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", "linux/amd64/musl",
		"--output", outputDir,
	)

	archiveName := expectedRustArchiveName(testVersion, "linux", "amd64", pluginpkg.LinuxLibCMusl)
	manifest := readReleasedManifest(t, outputDir, archiveName)
	binaryName := releaseBinaryName(rustReleasePluginName, "linux")

	if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Path != binaryName {
		t.Fatalf("artifacts = %+v, want path %q", manifest.Artifacts, binaryName)
	}
	assertExpectedRustArtifactPlatform(t, manifest.Artifacts[0], "linux", "amd64", pluginpkg.LinuxLibCMusl)
}

func TestRun_PluginReleaseRejectsMissingCrossTargetInterpreterForPythonSourcePlugin(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("fake Python build fixture is POSIX-only")
	}

	pluginDir := newPythonSourceReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	otherGOOS, otherGOARCH := pythonReleaseOtherPlatform()
	otherPlatform := otherGOOS + "/" + otherGOARCH

	out, err := runPluginReleaseCommandResult(pluginDir,
		"--version", "0.0.13-test",
		"--platform", otherPlatform,
		"--output", outputDir,
	)
	if err == nil {
		t.Fatalf("expected error for non-current platform, got output: %s", out)
	}
	if !strings.Contains(string(out), pluginpkgPythonEnvVar(otherGOOS, otherGOARCH)) {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestRun_PluginReleaseRejectsInvalidPythonProviderTarget(t *testing.T) {
	t.Parallel()

	pluginDir := filepath.Join(t.TempDir(), "invalid-python-release")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(pluginDir): %v", err)
	}
	writeTestFile(t, pluginDir, "pyproject.toml", []byte(`[build-system]
requires = ["setuptools==82.0.1"]
build-backend = "setuptools.build_meta"

[project]
name = "invalid-python-release"
version = "0.0.1-alpha.1"
dependencies = ["gestalt"]

[tool.gestalt]
plugin = "os import path\nimport os;os.system('cmd')#:attr"
`), 0o644)
	writeTestFile(t, pluginDir, "provider.py", []byte("plugin = None\n"), 0o644)
	manifestData, err := pluginpkg.EncodeSourceManifestFormat(&pluginmanifestv1.Manifest{
		Source:  "github.com/testowner/plugins/invalid-python-release",
		Version: "0.0.1",
		Kinds:   []string{pluginmanifestv1.KindPlugin},
		Plugin: &pluginmanifestv1.Plugin{
			Auth: &pluginmanifestv1.ProviderAuth{Type: pluginmanifestv1.AuthTypeNone},
		},
	}, pluginpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat: %v", err)
	}
	writeTestFile(t, pluginDir, "plugin.yaml", manifestData, 0o644)

	out, err := runPluginReleaseCommandResult(pluginDir, "--version", "0.0.14-test", "--output", t.TempDir())
	if err == nil {
		t.Fatalf("expected invalid target error, got output: %s", out)
	}
	if !strings.Contains(string(out), "module must be a dot-separated Python identifier path") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestRun_PluginReleaseBuildsGoSourceAuthPlugin(t *testing.T) {
	t.Parallel()

	pluginDir := newSourceAuthReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.15-test"

	runPluginReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", runtime.GOOS+"/"+runtime.GOARCH,
		"--output", outputDir,
	)

	archiveName := platformArchiveName(authReleasePluginName, testVersion, expectedGoReleasePlatform(runtime.GOOS, runtime.GOARCH, ""))
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifest := readReleasedManifest(t, outputDir, archiveName)
	binaryName := releaseBinaryName(authReleasePluginName, runtime.GOOS)

	if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Path != binaryName {
		t.Fatalf("artifacts = %+v, want path %q", manifest.Artifacts, binaryName)
	}
	assertExpectedGoArtifactPlatform(t, manifest.Artifacts[0], runtime.GOOS, runtime.GOARCH, "")
	if manifest.Entrypoints.Auth == nil || manifest.Entrypoints.Auth.ArtifactPath != binaryName {
		t.Fatalf("auth entrypoint = %+v, want artifact path %q", manifest.Entrypoints.Auth, binaryName)
	}
	if _, err := os.Stat(filepath.Join(extractDir, authReleaseSchemaPath)); err != nil {
		t.Fatalf("expected %s in archive: %v", authReleaseSchemaPath, err)
	}

	auth, err := pluginhost.NewExecutableAuthProvider(context.Background(), pluginhost.AuthExecConfig{
		Command:     filepath.Join(extractDir, binaryName),
		Name:        "auth-release",
		CallbackURL: "https://gestalt.example.test/api/v1/auth/login/callback",
		SessionKey:  []byte("0123456789abcdef0123456789abcdef"),
	})
	if err != nil {
		t.Fatalf("NewExecutableAuthProvider: %v", err)
	}
	defer func() {
		if closer, ok := auth.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}()

	loginURL, err := auth.LoginURL("host-state")
	if err != nil {
		t.Fatalf("LoginURL: %v", err)
	}
	parsed, err := url.Parse(loginURL)
	if err != nil {
		t.Fatalf("url.Parse(loginURL): %v", err)
	}
	state := parsed.Query().Get("state")
	if state == "" {
		t.Fatal("login URL did not include state")
	}

	callbackHandler, ok := auth.(interface {
		HandleCallbackRequest(context.Context, url.Values) (*core.UserIdentity, string, error)
	})
	if !ok {
		t.Fatal("auth provider did not expose HandleCallbackRequest")
	}
	identity, originalState, err := callbackHandler.HandleCallbackRequest(context.Background(), url.Values{
		"code":   {"callback-code"},
		"state":  {state},
		"prompt": {parsed.Query().Get("prompt")},
	})
	if err != nil {
		t.Fatalf("HandleCallbackRequest: %v", err)
	}
	if originalState != "host-state" {
		t.Fatalf("original state = %q, want %q", originalState, "host-state")
	}
	if identity == nil || identity.Email != "generated-auth@example.com" {
		t.Fatalf("identity = %+v", identity)
	}
	if ttlProvider, ok := auth.(interface{ SessionTokenTTL() time.Duration }); !ok || ttlProvider.SessionTokenTTL() != 90*time.Minute {
		t.Fatalf("SessionTokenTTL = %v", ttlProvider)
	}

	externalJWT, err := session.IssueToken(&core.UserIdentity{Email: "jwt@example.com"}, []byte("abcdef0123456789abcdef0123456789"), 24*time.Hour)
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	validated, err := auth.ValidateToken(context.Background(), externalJWT)
	if err != nil {
		t.Fatalf("ValidateToken(external jwt): %v", err)
	}
	if validated == nil || validated.Email != "jwt@example.com" {
		t.Fatalf("validated = %+v", validated)
	}
}

func TestRun_PluginReleaseBuildsGoSourceAuthPluginForExplicitLinuxLibC(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "linux" {
		t.Skip("explicit linux libc packaging only applies on linux builders")
	}
	libc := pluginpkg.CurrentRuntimeLibC()
	if libc == "" {
		t.Skip("current linux runtime libc is unknown")
	}

	pluginDir := newSourceAuthReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.15-linux-libc"

	runPluginReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", runtime.GOOS+"/"+runtime.GOARCH+"/"+libc,
		"--output", outputDir,
	)

	archiveName := "gestalt-plugin-" + authReleasePluginName + "_v" + testVersion + "_" + runtime.GOOS + "_" + runtime.GOARCH + "_" + libc + ".tar.gz"
	manifest := readReleasedManifest(t, outputDir, archiveName)
	if len(manifest.Artifacts) != 1 {
		t.Fatalf("artifacts = %+v, want one artifact", manifest.Artifacts)
	}
	if manifest.Artifacts[0].LibC != libc {
		t.Fatalf("artifact libc = %q, want %q", manifest.Artifacts[0].LibC, libc)
	}
}

func TestRun_PluginReleaseBuildsGoSourceDatastorePlugin(t *testing.T) {
	t.Parallel()

	pluginDir := newSourceDatastoreReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.16-test"

	runPluginReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", runtime.GOOS+"/"+runtime.GOARCH,
		"--output", outputDir,
	)

	archiveName := platformArchiveName(datastoreReleasePluginName, testVersion, expectedGoReleasePlatform(runtime.GOOS, runtime.GOARCH, ""))
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifest := readReleasedManifest(t, outputDir, archiveName)
	binaryName := releaseBinaryName(datastoreReleasePluginName, runtime.GOOS)

	if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Path != binaryName {
		t.Fatalf("artifacts = %+v, want path %q", manifest.Artifacts, binaryName)
	}
	assertExpectedGoArtifactPlatform(t, manifest.Artifacts[0], runtime.GOOS, runtime.GOARCH, "")
	if manifest.Entrypoints.Datastore == nil || manifest.Entrypoints.Datastore.ArtifactPath != binaryName {
		t.Fatalf("datastore entrypoint = %+v, want artifact path %q", manifest.Entrypoints.Datastore, binaryName)
	}
	if _, err := os.Stat(filepath.Join(extractDir, datastoreReleaseSchemaPath)); err != nil {
		t.Fatalf("expected %s in archive: %v", datastoreReleaseSchemaPath, err)
	}

	store, err := pluginhost.NewExecutableDatastore(context.Background(), pluginhost.DatastoreExecConfig{
		Command:       filepath.Join(extractDir, binaryName),
		Name:          "datastore-release",
		EncryptionKey: []byte("0123456789abcdef0123456789abcdef"),
	})
	if err != nil {
		t.Fatalf("NewExecutableDatastore: %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	user, err := store.FindOrCreateUser(context.Background(), "datastore-user@example.com")
	if err != nil {
		t.Fatalf("FindOrCreateUser: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	token := &core.IntegrationToken{
		ID:           "tok-1",
		UserID:       user.ID,
		Integration:  "github",
		Connection:   "default",
		Instance:     "prod",
		AccessToken:  "plain-access",
		RefreshToken: "plain-refresh",
		MetadataJSON: `{"tenant":"acme"}`,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := store.StoreToken(context.Background(), token); err != nil {
		t.Fatalf("StoreToken: %v", err)
	}
	got, err := store.Token(context.Background(), user.ID, "github", "default", "prod")
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if got == nil || got.AccessToken != "plain-access" || got.RefreshToken != "plain-refresh" {
		t.Fatalf("token = %+v", got)
	}
	if warnings, ok := store.(interface{ Warnings() []string }); !ok {
		t.Fatal("datastore did not expose Warnings()")
	} else if gotWarnings := warnings.Warnings(); len(gotWarnings) != 1 || gotWarnings[0] != "generated datastore warning" {
		t.Fatalf("Warnings() = %v", gotWarnings)
	}
}

func TestRun_PluginReleaseBuildsRustSourceAuthPlugin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake cargo test fixture is POSIX-only")
	}

	hostTarget, _, err := pluginpkgRustTargetTriple(runtime.GOOS, runtime.GOARCH, "")
	if err != nil {
		t.Fatalf("pluginpkgRustTargetTriple(host): %v", err)
	}

	fakeCargoDir := t.TempDir()
	writeFakeRustReleaseCargo(t, filepath.Join(fakeCargoDir, "cargo"), fakeRustCargoConfig{
		ExpectedPluginName:   authReleasePluginName,
		ExpectedServeExport:  "__gestalt_serve_auth",
		ExpectedCatalogWrite: false,
		DelegateBinary:       buildGoSourceAuthBinary(t),
		AllowedTargets:       []string{hostTarget},
	})
	t.Setenv("PATH", fakeCargoDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	pluginDir := newRustSourceAuthReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.17-rust-auth"

	runPluginReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", runtime.GOOS+"/"+runtime.GOARCH,
		"--output", outputDir,
	)

	archiveName := platformArchiveName(authReleasePluginName, testVersion, expectedRustReleasePlatform(runtime.GOOS, runtime.GOARCH, ""))
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifest := readReleasedManifest(t, outputDir, archiveName)
	binaryName := releaseBinaryName(authReleasePluginName, runtime.GOOS)

	if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Path != binaryName {
		t.Fatalf("artifacts = %+v, want path %q", manifest.Artifacts, binaryName)
	}
	assertExpectedRustArtifactPlatform(t, manifest.Artifacts[0], runtime.GOOS, runtime.GOARCH, "")
	if manifest.Entrypoints.Auth == nil || manifest.Entrypoints.Auth.ArtifactPath != binaryName {
		t.Fatalf("auth entrypoint = %+v, want artifact path %q", manifest.Entrypoints.Auth, binaryName)
	}
	if _, err := os.Stat(filepath.Join(extractDir, authReleaseSchemaPath)); err != nil {
		t.Fatalf("expected %s in archive: %v", authReleaseSchemaPath, err)
	}

	auth, err := pluginhost.NewExecutableAuthProvider(context.Background(), pluginhost.AuthExecConfig{
		Command:     filepath.Join(extractDir, binaryName),
		Name:        "auth-release",
		CallbackURL: "https://gestalt.example.test/api/v1/auth/login/callback",
		SessionKey:  []byte("0123456789abcdef0123456789abcdef"),
	})
	if err != nil {
		t.Fatalf("NewExecutableAuthProvider: %v", err)
	}
	defer func() {
		if closer, ok := auth.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}()

	loginURL, err := auth.LoginURL("host-state")
	if err != nil {
		t.Fatalf("LoginURL: %v", err)
	}
	parsed, err := url.Parse(loginURL)
	if err != nil {
		t.Fatalf("url.Parse(loginURL): %v", err)
	}
	state := parsed.Query().Get("state")
	if state == "" {
		t.Fatal("login URL did not include state")
	}

	callbackHandler, ok := auth.(interface {
		HandleCallbackRequest(context.Context, url.Values) (*core.UserIdentity, string, error)
	})
	if !ok {
		t.Fatal("auth provider did not expose HandleCallbackRequest")
	}
	identity, originalState, err := callbackHandler.HandleCallbackRequest(context.Background(), url.Values{
		"code":   {"callback-code"},
		"state":  {state},
		"prompt": {parsed.Query().Get("prompt")},
	})
	if err != nil {
		t.Fatalf("HandleCallbackRequest: %v", err)
	}
	if originalState != "host-state" {
		t.Fatalf("original state = %q, want %q", originalState, "host-state")
	}
	if identity == nil || identity.Email != "generated-auth@example.com" {
		t.Fatalf("identity = %+v", identity)
	}
	if ttlProvider, ok := auth.(interface{ SessionTokenTTL() time.Duration }); !ok || ttlProvider.SessionTokenTTL() != 90*time.Minute {
		t.Fatalf("SessionTokenTTL = %v", ttlProvider)
	}
}

func TestRun_PluginReleaseBuildsRustSourceDatastorePlugin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake cargo test fixture is POSIX-only")
	}

	hostTarget, _, err := pluginpkgRustTargetTriple(runtime.GOOS, runtime.GOARCH, "")
	if err != nil {
		t.Fatalf("pluginpkgRustTargetTriple(host): %v", err)
	}

	fakeCargoDir := t.TempDir()
	writeFakeRustReleaseCargo(t, filepath.Join(fakeCargoDir, "cargo"), fakeRustCargoConfig{
		ExpectedPluginName:   datastoreReleasePluginName,
		ExpectedServeExport:  "__gestalt_serve_datastore",
		ExpectedCatalogWrite: false,
		DelegateBinary:       buildGoSourceDatastoreBinary(t),
		AllowedTargets:       []string{hostTarget},
	})
	t.Setenv("PATH", fakeCargoDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	pluginDir := newRustSourceDatastoreReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.18-rust-datastore"

	runPluginReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", runtime.GOOS+"/"+runtime.GOARCH,
		"--output", outputDir,
	)

	archiveName := platformArchiveName(datastoreReleasePluginName, testVersion, expectedRustReleasePlatform(runtime.GOOS, runtime.GOARCH, ""))
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifest := readReleasedManifest(t, outputDir, archiveName)
	binaryName := releaseBinaryName(datastoreReleasePluginName, runtime.GOOS)

	if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Path != binaryName {
		t.Fatalf("artifacts = %+v, want path %q", manifest.Artifacts, binaryName)
	}
	assertExpectedRustArtifactPlatform(t, manifest.Artifacts[0], runtime.GOOS, runtime.GOARCH, "")
	if manifest.Entrypoints.Datastore == nil || manifest.Entrypoints.Datastore.ArtifactPath != binaryName {
		t.Fatalf("datastore entrypoint = %+v, want artifact path %q", manifest.Entrypoints.Datastore, binaryName)
	}
	if _, err := os.Stat(filepath.Join(extractDir, datastoreReleaseSchemaPath)); err != nil {
		t.Fatalf("expected %s in archive: %v", datastoreReleaseSchemaPath, err)
	}

	store, err := pluginhost.NewExecutableDatastore(context.Background(), pluginhost.DatastoreExecConfig{
		Command:       filepath.Join(extractDir, binaryName),
		Name:          "datastore-release",
		EncryptionKey: []byte("0123456789abcdef0123456789abcdef"),
	})
	if err != nil {
		t.Fatalf("NewExecutableDatastore: %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	user, err := store.FindOrCreateUser(context.Background(), "datastore-user@example.com")
	if err != nil {
		t.Fatalf("FindOrCreateUser: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	token := &core.IntegrationToken{
		ID:           "tok-1",
		UserID:       user.ID,
		Integration:  "github",
		Connection:   "default",
		Instance:     "prod",
		AccessToken:  "plain-access",
		RefreshToken: "plain-refresh",
		MetadataJSON: `{"tenant":"acme"}`,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := store.StoreToken(context.Background(), token); err != nil {
		t.Fatalf("StoreToken: %v", err)
	}
	got, err := store.Token(context.Background(), user.ID, "github", "default", "prod")
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if got == nil || got.AccessToken != "plain-access" || got.RefreshToken != "plain-refresh" {
		t.Fatalf("token = %+v", got)
	}
	if warnings, ok := store.(interface{ Warnings() []string }); !ok {
		t.Fatal("datastore did not expose Warnings()")
	} else if gotWarnings := warnings.Warnings(); len(gotWarnings) != 1 || gotWarnings[0] != "generated datastore warning" {
		t.Fatalf("Warnings() = %v", gotWarnings)
	}
}

func TestRun_PluginReleaseCopiesCompiledSupportFiles(t *testing.T) {
	t.Parallel()

	pluginDir := newSourceProviderReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	testVersion := "0.0.2-test"

	runPluginReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", runtime.GOOS+"/"+runtime.GOARCH,
		"--output", outputDir,
	)

	archiveName := "gestalt-plugin-release-test_v" + testVersion + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	extractDir := extractReleasedArchive(t, outputDir, archiveName)

	if _, err := pluginpkg.ValidatePackageDir(extractDir); err != nil {
		t.Fatalf("validate extracted package: %v", err)
	}
	for _, rel := range []string{
		"branding/icon.svg",
		"schemas/provider.schema.json",
		"gestalt-plugin-release-test",
	} {
		if _, err := os.Stat(filepath.Join(extractDir, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("expected %s in archive: %v", rel, err)
		}
	}
}

func TestRun_PluginReleaseCopiesWebUISupportFiles(t *testing.T) {
	t.Parallel()

	pluginDir := newWebUIReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	testVersion := "0.0.3-test"

	runPluginReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)

	archiveName := "gestalt-plugin-webui-test_v" + testVersion + ".tar.gz"
	extractDir := extractReleasedArchive(t, outputDir, archiveName)

	for _, rel := range []string{
		"branding/icon.svg",
		"out/index.html",
		"out/static/app.js",
	} {
		if _, err := os.Stat(filepath.Join(extractDir, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("expected %s in archive: %v", rel, err)
		}
	}
}

func TestRun_PluginReleaseAllowsOverlappingSupportPaths(t *testing.T) {
	t.Parallel()

	pluginDir := filepath.Join(t.TempDir(), "webui-overlap")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(pluginDir): %v", err)
	}
	writeReleaseTestManifest(t, pluginDir, &pluginmanifestv1.Manifest{
		Source:      "github.com/testowner/plugins/webui-overlap",
		Version:     "0.0.1",
		DisplayName: "WebUI Overlap",
		IconFile:    "out/icon.svg",
		Kinds:       []string{pluginmanifestv1.KindWebUI},
		WebUI: &pluginmanifestv1.WebUIMetadata{
			AssetRoot: "out",
		},
	})
	writeTestFile(t, pluginDir, "out/icon.svg", []byte("<svg></svg>\n"), 0o644)
	writeTestFile(t, pluginDir, "out/index.html", []byte("<html></html>\n"), 0o644)

	outputDir := t.TempDir()
	const testVersion = "0.0.3-overlap.1"

	runPluginReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)

	archiveName := "gestalt-plugin-webui-overlap_v" + testVersion + ".tar.gz"
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	for _, rel := range []string{"out/icon.svg", "out/index.html"} {
		if _, err := os.Stat(filepath.Join(extractDir, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("expected %s in archive: %v", rel, err)
		}
	}
}

func TestRun_PluginReleaseTreatsGoModWithoutProviderPackageAsDeclarative(t *testing.T) {
	t.Parallel()

	pluginDir := newWebUIReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	testVersion := "0.0.4-test"

	writeTestFile(t, pluginDir, "go.mod", []byte("module example.com/webui-test\n\ngo 1.22\n"), 0644)

	runPluginReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)

	archiveName := "gestalt-plugin-webui-test_v" + testVersion + ".tar.gz"
	if _, err := os.Stat(filepath.Join(outputDir, archiveName)); err != nil {
		t.Fatalf("expected declarative archive %s to exist: %v", archiveName, err)
	}

	compiledArchiveName := "gestalt-plugin-webui-test_v" + testVersion + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	if _, err := os.Stat(filepath.Join(outputDir, compiledArchiveName)); !os.IsNotExist(err) {
		t.Fatalf("unexpected compiled archive %s: %v", compiledArchiveName, err)
	}
}

func TestRun_PluginReleasePreservesYAMLManifestFormatAndConnectionDefaults(t *testing.T) {
	t.Parallel()

	pluginDir := newSourceProviderReleaseFixture(t, t.TempDir())
	writeReleaseTestManifestFormat(t, pluginDir, "plugin.yaml", &pluginmanifestv1.Manifest{
		Source:      "github.com/testowner/plugins/provider-yaml",
		Version:     "0.0.1",
		DisplayName: "Provider YAML",
		Kinds:       []string{pluginmanifestv1.KindPlugin},
		Plugin: &pluginmanifestv1.Plugin{
			ConfigSchemaPath: releaseProviderSchemaPath,
			MCP:              true,
			ConnectionMode:   "identity",
			ConnectionParams: map[string]pluginmanifestv1.ProviderConnectionParam{
				"tenant": {Required: true},
			},
		},
	})
	if err := os.Remove(filepath.Join(pluginDir, pluginpkg.ManifestFile)); err != nil {
		t.Fatalf("remove plugin.json: %v", err)
	}

	outputDir := t.TempDir()
	const testVersion = "0.0.4-yaml.1"

	runPluginReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)

	archiveName := "gestalt-plugin-provider-yaml_v" + testVersion + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifestPath, manifest := readManifestFromDir(t, extractDir)
	if filepath.Base(manifestPath) != "plugin.yaml" {
		t.Fatalf("released manifest = %q, want plugin.yaml", filepath.Base(manifestPath))
	}
	if manifest.Plugin == nil || len(manifest.Plugin.ConnectionParams) != 1 || !manifest.Plugin.ConnectionParams["tenant"].Required {
		t.Fatalf("provider connection_params = %+v", manifest.Plugin)
	}
	if manifest.Plugin.ConnectionMode != "identity" {
		t.Fatalf("provider connection_mode = %q, want %q", manifest.Plugin.ConnectionMode, "identity")
	}

	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read released manifest: %v", err)
	}
	for _, expected := range []string{
		"exec:",
		"connections:",
		"default:",
		"mode: identity",
		"params:",
		"mcp:",
		"enabled: true",
	} {
		if !strings.Contains(string(manifestData), expected) {
			t.Fatalf("expected released manifest to preserve provider wire field %q, got: %s", expected, manifestData)
		}
	}
}

func TestRun_PluginReleaseChecksumsOnlyCurrentArchives(t *testing.T) {
	t.Parallel()

	pluginDir := newWebUIReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()

	runPluginReleaseCommand(t, pluginDir,
		"--version", "1.0.0",
		"--output", outputDir,
	)
	runPluginReleaseCommand(t, pluginDir,
		"--version", "1.0.1",
		"--output", outputDir,
	)

	checksumPath := filepath.Join(outputDir, "checksums.txt")
	checksumData, err := os.ReadFile(checksumPath)
	if err != nil {
		t.Fatalf("read checksums.txt: %v", err)
	}
	if got := string(checksumData); strings.Contains(got, "gestalt-plugin-webui-test_v1.0.0.tar.gz") {
		t.Fatalf("checksums.txt unexpectedly included stale archive: %s", got)
	} else if !strings.Contains(got, "gestalt-plugin-webui-test_v1.0.1.tar.gz") {
		t.Fatalf("checksums.txt missing current archive: %s", got)
	}
}

func TestRun_PluginReleaseRejectsOutputInsideWebUIAssetRoot(t *testing.T) {
	t.Parallel()

	pluginDir := newWebUIReleaseFixtureWithAssetRoot(t, t.TempDir(), "release-output")
	outputDir := filepath.Join(pluginDir, "release-output", "nested")

	out, err := runPluginReleaseCommandResult(pluginDir, "--version", "1.0.0", "--output", outputDir)
	if err == nil {
		t.Fatalf("expected plugin release to fail, got output: %s", out)
	}
	if !strings.Contains(string(out), "must not be inside webui.asset_root") {
		t.Fatalf("expected overlap error, got: %s", out)
	}
}

func TestRun_PluginReleaseCompilesProviderWithoutSourceArtifacts(t *testing.T) {
	t.Parallel()

	pluginDir := newSourceProviderReleaseFixtureWithoutCatalog(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.4-source.1"

	runPluginReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", runtime.GOOS+"/"+runtime.GOARCH,
		"--output", outputDir,
	)

	archiveName := "gestalt-plugin-" + releaseTestPluginName + "_v" + testVersion + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifest := readReleasedManifest(t, outputDir, archiveName)

	if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Path != releaseBinaryName(releaseTestPluginName, runtime.GOOS) {
		t.Fatalf("artifacts = %+v", manifest.Artifacts)
	}
	if manifest.Entrypoints.Provider == nil || manifest.Entrypoints.Provider.ArtifactPath != releaseBinaryName(releaseTestPluginName, runtime.GOOS) {
		t.Fatalf("provider entrypoint = %+v", manifest.Entrypoints.Provider)
	}
	if manifest.Plugin == nil || manifest.Plugin.ConfigSchemaPath != releaseProviderSchemaPath {
		t.Fatalf("provider metadata = %#v, want config schema path %q", manifest.Plugin, releaseProviderSchemaPath)
	}
	data, err := os.ReadFile(filepath.Join(extractDir, pluginpkg.StaticCatalogFile))
	if err != nil {
		t.Fatalf("read generated catalog: %v", err)
	}
	if !strings.Contains(string(data), "generated_op") {
		t.Fatalf("unexpected generated catalog: %s", data)
	}
}

func TestRun_PluginReleaseRejectsRequiredExecutableKindsWithoutSourceOrEntrypoint(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		manifest  *pluginmanifestv1.Manifest
		wantError string
	}{
		{
			name: "provider",
			manifest: &pluginmanifestv1.Manifest{
				Source:      "github.com/testowner/plugins/missing-provider",
				Version:     "0.0.1",
				DisplayName: "Missing Provider",
				Kinds:       []string{pluginmanifestv1.KindPlugin},
				Plugin:      &pluginmanifestv1.Plugin{},
			},
			wantError: "no Go, Rust, or Python provider package found",
		},
		{
			name: "auth",
			manifest: &pluginmanifestv1.Manifest{
				Source:      "github.com/testowner/plugins/missing-auth",
				Version:     "0.0.1",
				DisplayName: "Missing Auth",
				Kinds:       []string{pluginmanifestv1.KindAuth},
				Auth:        &pluginmanifestv1.AuthMetadata{},
			},
			wantError: "no Go or Rust auth source package found",
		},
		{
			name: "datastore",
			manifest: &pluginmanifestv1.Manifest{
				Source:      "github.com/testowner/plugins/missing-datastore",
				Version:     "0.0.1",
				DisplayName: "Missing Datastore",
				Kinds:       []string{pluginmanifestv1.KindDatastore},
				Datastore:   &pluginmanifestv1.DatastoreMetadata{},
			},
			wantError: "no Go or Rust datastore source package found",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			pluginDir := filepath.Join(t.TempDir(), tc.name)
			if err := os.MkdirAll(pluginDir, 0o755); err != nil {
				t.Fatalf("MkdirAll(pluginDir): %v", err)
			}
			writeReleaseTestManifest(t, pluginDir, tc.manifest)

			out, err := runPluginReleaseCommandResult(pluginDir, "--version", "0.0.1-test", "--output", t.TempDir())
			if err == nil {
				t.Fatalf("expected missing source error, got output: %s", out)
			}
			if !strings.Contains(string(out), tc.wantError) {
				t.Fatalf("unexpected output: %s", out)
			}
		})
	}
}

func TestRun_PluginReleasePreservesPrebuiltProvider(t *testing.T) {
	t.Parallel()

	pluginDir := newPrebuiltProviderReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.5-test"

	runPluginReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)

	archiveName := "gestalt-plugin-" + prebuiltProviderPluginName + "_v" + testVersion + ".tar.gz"
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifest := readReleasedManifest(t, outputDir, archiveName)

	if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Path != prebuiltProviderBinaryPath {
		t.Fatalf("artifacts = %+v", manifest.Artifacts)
	}
	if manifest.Entrypoints.Provider == nil {
		t.Fatal("expected provider entrypoint")
	}
	if manifest.Entrypoints.Provider.ArtifactPath != prebuiltProviderBinaryPath {
		t.Fatalf("provider artifact path = %q", manifest.Entrypoints.Provider.ArtifactPath)
	}
	if manifest.Plugin == nil || manifest.Plugin.ConfigSchemaPath != releaseProviderSchemaPath {
		t.Fatalf("provider metadata = %#v, want config schema path %q", manifest.Plugin, releaseProviderSchemaPath)
	}
	if _, err := os.Stat(filepath.Join(extractDir, filepath.FromSlash(prebuiltProviderBinaryPath))); err != nil {
		t.Fatalf("expected prebuilt artifact in archive: %v", err)
	}
}

func TestRun_PluginReleasePackagesGoModuleWithoutCmdAsSource(t *testing.T) {
	t.Parallel()

	pluginDir := newPrebuiltProviderReleaseFixture(t, t.TempDir())
	writeTestFile(t, pluginDir, "go.mod", []byte("module example.com/prebuilt-provider\n\ngo 1.22\n"), 0644)

	outputDir := t.TempDir()
	const testVersion = "0.0.6-test"

	runPluginReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)

	archiveName := "gestalt-plugin-" + prebuiltProviderPluginName + "_v" + testVersion + ".tar.gz"
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifest := readReleasedManifest(t, outputDir, archiveName)

	if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Path != prebuiltProviderBinaryPath {
		t.Fatalf("artifacts = %+v", manifest.Artifacts)
	}
	if manifest.Entrypoints.Provider == nil || manifest.Entrypoints.Provider.ArtifactPath != prebuiltProviderBinaryPath {
		t.Fatalf("provider entrypoint = %+v", manifest.Entrypoints.Provider)
	}
	if _, err := os.Stat(filepath.Join(extractDir, filepath.FromSlash(prebuiltProviderBinaryPath))); err != nil {
		t.Fatalf("expected prebuilt artifact in archive: %v", err)
	}
}

func TestRun_PluginReleaseRejectsStaleSourceArtifactDigest(t *testing.T) {
	t.Parallel()

	pluginDir := newPrebuiltProviderReleaseFixture(t, t.TempDir())

	_, manifest, err := pluginpkg.ReadSourceManifestFile(filepath.Join(pluginDir, pluginpkg.ManifestFile))
	if err != nil {
		t.Fatalf("ReadSourceManifestFile(plugin.json): %v", err)
	}
	manifest.Artifacts = []pluginmanifestv1.Artifact{
		{
			OS:     runtime.GOOS,
			Arch:   runtime.GOARCH,
			Path:   prebuiltProviderBinaryPath,
			SHA256: sha256HexForTest("different-content"),
		},
	}
	writeReleaseTestManifest(t, pluginDir, manifest)

	out, err := runPluginReleaseCommandResult(pluginDir, "--version", "0.0.8-test")
	if err == nil {
		t.Fatal("expected stale digest error")
	}
	if !strings.Contains(string(out), "sha256 mismatch") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestRun_PluginReleaseWindowsArtifactUsesExe(t *testing.T) {
	t.Parallel()

	pluginDir := newSourceProviderReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.9-test"
	const windowsPlatform = "windows/amd64"

	runPluginReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", windowsPlatform,
		"--output", outputDir,
	)

	archiveName := "gestalt-plugin-" + releaseTestPluginName + "_v" + testVersion + "_windows_amd64.tar.gz"
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifest := readReleasedManifest(t, outputDir, archiveName)
	binaryName := releaseBinaryName(releaseTestPluginName, "windows")

	if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Path != binaryName {
		t.Fatalf("artifacts = %+v, want path %q", manifest.Artifacts, binaryName)
	}
	if manifest.Entrypoints.Provider == nil || manifest.Entrypoints.Provider.ArtifactPath != binaryName {
		t.Fatalf("provider entrypoint = %+v, want artifact path %q", manifest.Entrypoints.Provider, binaryName)
	}
	if _, err := os.Stat(filepath.Join(extractDir, binaryName)); err != nil {
		t.Fatalf("expected %s in archive: %v", binaryName, err)
	}
}

func newPythonSourceReleaseFixture(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := filepath.Join(dir, "python-release")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(pluginDir): %v", err)
	}
	writeTestFile(t, pluginDir, "pyproject.toml", []byte(`[build-system]
requires = ["setuptools==82.0.1"]
build-backend = "setuptools.build_meta"

[project]
name = "python-release"
version = "0.0.1-alpha.1"
dependencies = ["gestalt"]

[tool.gestalt]
plugin = "provider"
`), 0o644)
	writeTestFile(t, pluginDir, "provider.py", []byte(`import gestalt


class GreetInput(gestalt.Model):
    name: str = gestalt.field(default="World")


class GreetOutput(gestalt.Model):
    message: str


@gestalt.operation(method="GET", read_only=True)
def greet(input: GreetInput, _req: gestalt.Request) -> GreetOutput:
    return GreetOutput(message=f"Hello, {input.name}!")


@gestalt.session_catalog
def dynamic_catalog(request: gestalt.Request) -> gestalt.Catalog:
    return gestalt.Catalog(
        name="python-release-session",
        display_name=request.token,
        operations=[
            gestalt.CatalogOperation(
                id="session_greet",
                method="GET",
            )
        ],
    )
`), 0o644)
	manifestData, err := pluginpkg.EncodeSourceManifestFormat(&pluginmanifestv1.Manifest{
		Source:  "github.com/testowner/plugins/python-release",
		Version: "0.0.1",
		Kinds:   []string{pluginmanifestv1.KindPlugin},
		Plugin: &pluginmanifestv1.Plugin{
			Auth: &pluginmanifestv1.ProviderAuth{Type: pluginmanifestv1.AuthTypeNone},
		},
	}, pluginpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat: %v", err)
	}
	writeTestFile(t, pluginDir, "plugin.yaml", manifestData, 0o644)
	writeFakePythonReleaseInterpreter(t, filepath.Join(pluginDir, ".venv", "bin", "python"), runtime.GOOS, runtime.GOARCH)
	return pluginDir
}

func writeFakePythonReleaseInterpreter(t *testing.T, path, expectedGOOS, expectedGOARCH string) {
	t.Helper()

	script := `#!/bin/sh
set -eu

if [ "$#" -ge 2 ] && [ "$1" = "-m" ] && [ "$2" = "gestalt._build" ]; then
  if [ -z "${GESTALT_TEST_PYINSTALLER_BINARY:-}" ]; then
    echo "missing GESTALT_TEST_PYINSTALLER_BINARY" >&2
    exit 1
  fi
  if [ "$#" -ne 8 ]; then
    echo "unexpected gestalt._build args: $*" >&2
    exit 1
  fi
  root="$3"
  target="$4"
  output="$5"
  name="$6"
  goos="$7"
  goarch="$8"
  if [ "$target" != "provider" ]; then
    echo "unexpected provider target: $target" >&2
    exit 1
  fi
  if [ "$name" != "python-release" ]; then
    echo "unexpected plugin name: $name" >&2
    exit 1
  fi
  if [ "$goos" != "` + expectedGOOS + `" ] || [ "$goarch" != "` + expectedGOARCH + `" ]; then
    echo "unexpected target platform: $goos/$goarch" >&2
    exit 1
  fi
  output_dir="${output%/*}"
  if [ "$output_dir" = "$output" ]; then
    output_dir="."
  fi
  mkdir -p "$output_dir"
  cp "$GESTALT_TEST_PYINSTALLER_BINARY" "$output"
  chmod +x "$output"
  exit 0
fi

if [ "$#" -ge 2 ] && [ "$1" = "-m" ] && [ "$2" = "gestalt._runtime" ]; then
  if [ -z "${GESTALT_PLUGIN_WRITE_CATALOG:-}" ]; then
    echo "missing GESTALT_PLUGIN_WRITE_CATALOG" >&2
    exit 1
  fi
  cat > "$GESTALT_PLUGIN_WRITE_CATALOG" <<'EOF'
name: python-release
operations:
  - id: greet
    method: GET
EOF
  exit 0
fi

echo "unexpected fake python invocation: $*" >&2
exit 1
`
	writeTestFile(t, filepath.Dir(path), filepath.Base(path), []byte(script), 0o755)
}

func pythonReleaseOtherPlatform() (string, string) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		return "darwin", "arm64"
	}
	return "linux", "amd64"
}

func defaultReleasePlatformsForTest(t *testing.T) []releasePlatform {
	t.Helper()

	platforms, err := parseReleasePlatforms(defaultPlatforms)
	if err != nil {
		t.Fatalf("parseReleasePlatforms(defaultPlatforms): %v", err)
	}
	return platforms
}

func expectedPythonReleasePlatform(goos, goarch string) releasePlatform {
	plat := releasePlatform{GOOS: goos, GOARCH: goarch}
	if runtime.GOOS == "linux" && goos == "linux" {
		plat.LibC = pluginpkg.CurrentRuntimeLibC()
	}
	return plat
}

func expectedPythonArchiveName(version, goos, goarch string) string {
	return platformArchiveName("python-release", version, expectedPythonReleasePlatform(goos, goarch))
}

func expectedGoReleasePlatform(goos, goarch, libc string) releasePlatform {
	return releasePlatform{GOOS: goos, GOARCH: goarch, LibC: libc}
}

func expectedRustReleasePlatform(goos, goarch, libc string) releasePlatform {
	plat := releasePlatform{GOOS: goos, GOARCH: goarch, LibC: libc}
	if plat.GOOS != "linux" || plat.LibC != "" {
		return plat
	}
	if runtime.GOOS == "linux" && goos == "linux" {
		plat.LibC = pluginpkg.CurrentRuntimeLibC()
	}
	if plat.LibC == "" {
		plat.LibC = pluginpkg.LinuxLibCGLibC
	}
	return plat
}

func expectedRustArchiveName(version, goos, goarch, libc string) string {
	return platformArchiveName(rustReleasePluginName, version, expectedRustReleasePlatform(goos, goarch, libc))
}

func assertExpectedPythonArtifactPlatform(t *testing.T, artifact pluginmanifestv1.Artifact, goos, goarch string) {
	t.Helper()

	want := expectedPythonReleasePlatform(goos, goarch)
	if artifact.OS != want.GOOS || artifact.Arch != want.GOARCH || artifact.LibC != want.LibC {
		t.Fatalf(
			"artifact platform = %s/%s/%s, want %s/%s/%s",
			artifact.OS, artifact.Arch, artifact.LibC,
			want.GOOS, want.GOARCH, want.LibC,
		)
	}
}

func assertExpectedGoArtifactPlatform(t *testing.T, artifact pluginmanifestv1.Artifact, goos, goarch, libc string) {
	t.Helper()

	want := expectedGoReleasePlatform(goos, goarch, libc)
	if artifact.OS != want.GOOS || artifact.Arch != want.GOARCH || artifact.LibC != want.LibC {
		t.Fatalf(
			"artifact platform = %s/%s/%s, want %s/%s/%s",
			artifact.OS, artifact.Arch, artifact.LibC,
			want.GOOS, want.GOARCH, want.LibC,
		)
	}
}

func assertExpectedRustArtifactPlatform(t *testing.T, artifact pluginmanifestv1.Artifact, goos, goarch, libc string) {
	t.Helper()

	want := expectedRustReleasePlatform(goos, goarch, libc)
	if artifact.OS != want.GOOS || artifact.Arch != want.GOARCH || artifact.LibC != want.LibC {
		t.Fatalf(
			"artifact platform = %s/%s/%s, want %s/%s/%s",
			artifact.OS, artifact.Arch, artifact.LibC,
			want.GOOS, want.GOARCH, want.LibC,
		)
	}
}

func configurePythonReleaseInterpretersForAllPlatforms(t *testing.T, pluginDir string) {
	t.Helper()

	replacer := strings.NewReplacer("/", "-", "\\", "-")
	for _, platform := range defaultReleasePlatformsForTest(t) {
		if platform.GOOS == runtime.GOOS && platform.GOARCH == runtime.GOARCH {
			continue
		}
		interpreterPath := filepath.Join(pluginDir, "python-"+replacer.Replace(platform.GOOS+"-"+platform.GOARCH))
		writeFakePythonReleaseInterpreter(t, interpreterPath, platform.GOOS, platform.GOARCH)
		t.Setenv(pluginpkgPythonEnvVar(platform.GOOS, platform.GOARCH), interpreterPath)
	}
}

func pluginpkgPythonEnvVar(goos, goarch string) string {
	replacer := strings.NewReplacer("-", "_", ".", "_", "/", "_")
	return "GESTALT_PYTHON_" + strings.ToUpper(replacer.Replace(goos)) + "_" + strings.ToUpper(replacer.Replace(goarch))
}

func newSourceAuthReleaseFixture(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := filepath.Join(dir, authReleasePluginName)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(pluginDir): %v", err)
	}
	writeTestFile(t, pluginDir, "go.mod", []byte(testutil.GeneratedProviderModuleSource(t, "example.com/"+authReleasePluginName)), 0o644)
	writeTestFile(t, pluginDir, "go.sum", testutil.GeneratedProviderModuleSum(t), 0o644)
	writeTestFile(t, pluginDir, "auth.go", []byte(testutil.GeneratedAuthPackageSource()), 0o644)
	writeReleaseTestManifest(t, pluginDir, &pluginmanifestv1.Manifest{
		Source:      authReleaseSource,
		Version:     "0.0.1",
		DisplayName: "Auth Release",
		Kinds:       []string{pluginmanifestv1.KindAuth},
		Auth: &pluginmanifestv1.AuthMetadata{
			ConfigSchemaPath: authReleaseSchemaPath,
		},
	})
	writeTestFile(t, pluginDir, authReleaseSchemaPath, []byte(`{"type":"object"}`), 0o644)
	return pluginDir
}

func newRustSourceAuthReleaseFixture(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := filepath.Join(dir, authReleasePluginName)
	copyFixtureTree(t, rustAuthProviderFixturePath(t), pluginDir)
	writeReleaseTestManifest(t, pluginDir, &pluginmanifestv1.Manifest{
		Source:      authReleaseSource,
		Version:     "0.0.1",
		DisplayName: "Auth Release",
		Kinds:       []string{pluginmanifestv1.KindAuth},
		Auth: &pluginmanifestv1.AuthMetadata{
			ConfigSchemaPath: authReleaseSchemaPath,
		},
	})
	writeTestFile(t, pluginDir, authReleaseSchemaPath, []byte(`{"type":"object"}`), 0o644)
	return pluginDir
}

func newSourceDatastoreReleaseFixture(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := filepath.Join(dir, datastoreReleasePluginName)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(pluginDir): %v", err)
	}
	writeTestFile(t, pluginDir, "go.mod", []byte(testutil.GeneratedProviderModuleSource(t, "example.com/"+datastoreReleasePluginName)), 0o644)
	writeTestFile(t, pluginDir, "go.sum", testutil.GeneratedProviderModuleSum(t), 0o644)
	writeTestFile(t, pluginDir, "datastore.go", []byte(testutil.GeneratedDatastorePackageSource()), 0o644)
	writeReleaseTestManifest(t, pluginDir, &pluginmanifestv1.Manifest{
		Source:      datastoreReleaseSource,
		Version:     "0.0.1",
		DisplayName: "Datastore Release",
		Kinds:       []string{pluginmanifestv1.KindDatastore},
		Datastore: &pluginmanifestv1.DatastoreMetadata{
			ConfigSchemaPath: datastoreReleaseSchemaPath,
		},
	})
	writeTestFile(t, pluginDir, datastoreReleaseSchemaPath, []byte(`{"type":"object"}`), 0o644)
	return pluginDir
}

func newRustSourceDatastoreReleaseFixture(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := filepath.Join(dir, datastoreReleasePluginName)
	copyFixtureTree(t, rustDatastoreProviderFixturePath(t), pluginDir)
	writeReleaseTestManifest(t, pluginDir, &pluginmanifestv1.Manifest{
		Source:      datastoreReleaseSource,
		Version:     "0.0.1",
		DisplayName: "Datastore Release",
		Kinds:       []string{pluginmanifestv1.KindDatastore},
		Datastore: &pluginmanifestv1.DatastoreMetadata{
			ConfigSchemaPath: datastoreReleaseSchemaPath,
		},
	})
	writeTestFile(t, pluginDir, datastoreReleaseSchemaPath, []byte(`{"type":"object"}`), 0o644)
	return pluginDir
}

func pathWithoutGo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	for _, name := range []string{"cat", "chmod", "cp", "mkdir"} {
		target, err := exec.LookPath(name)
		if err != nil {
			t.Skipf("%s not found: %v", name, err)
		}
		if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
			t.Fatalf("Symlink(%s): %v", name, err)
		}
	}
	return dir
}

func newRustSourceReleaseFixture(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := filepath.Join(dir, rustReleasePluginName)
	copyFixtureTree(t, rustProviderFixturePath(t), pluginDir)
	return pluginDir
}

func newSourceProviderReleaseFixture(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := filepath.Join(dir, releaseTestPluginName)
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("MkdirAll(pluginDir): %v", err)
	}
	writeTestFile(t, pluginDir, "go.mod", []byte(testutil.GeneratedProviderModuleSource(t, releaseTestModule)), 0644)
	writeTestFile(t, pluginDir, "go.sum", testutil.GeneratedProviderModuleSum(t), 0644)
	writeStaticCatalogProviderMain(t, pluginDir)
	writeReleaseTestManifest(t, pluginDir, &pluginmanifestv1.Manifest{
		Source:      releaseTestSource,
		Version:     "0.0.1",
		DisplayName: "Release Test",
		IconFile:    releaseTestIconPath,
		Kinds:       []string{pluginmanifestv1.KindPlugin},
		Plugin: &pluginmanifestv1.Plugin{
			ConfigSchemaPath: releaseProviderSchemaPath,
		},
	})
	writeTestFile(t, pluginDir, releaseTestIconPath, []byte("<svg></svg>\n"), 0644)
	writeTestFile(t, pluginDir, releaseProviderSchemaPath, []byte(`{"type":"object"}`), 0644)
	return pluginDir
}

func newGoSourceReleaseFixture(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := filepath.Join(dir, releaseTestPluginName)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(pluginDir): %v", err)
	}
	writeTestFile(t, pluginDir, "go.mod", []byte(testutil.GeneratedProviderModuleSource(t, releaseTestModule)), 0o644)
	writeTestFile(t, pluginDir, "go.sum", testutil.GeneratedProviderModuleSum(t), 0o644)
	writeStaticCatalogProviderMain(t, pluginDir)
	writeReleaseTestManifest(t, pluginDir, &pluginmanifestv1.Manifest{
		Source:      releaseTestSource,
		Version:     "0.0.1",
		DisplayName: "Release Test",
		Kinds:       []string{pluginmanifestv1.KindPlugin},
		Plugin: &pluginmanifestv1.Plugin{
			Auth: &pluginmanifestv1.ProviderAuth{Type: pluginmanifestv1.AuthTypeNone},
		},
	})
	return pluginDir
}

func newSourceProviderReleaseFixtureWithoutCatalog(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := newSourceProviderReleaseFixture(t, dir)
	_ = os.Remove(filepath.Join(pluginDir, pluginpkg.StaticCatalogFile))

	return pluginDir
}

func writeStaticCatalogProviderMain(t *testing.T, dir string) {
	t.Helper()
	writeStaticCatalogProviderMainAt(t, dir, "provider.go")
}

func writeStaticCatalogProviderMainAt(t *testing.T, dir, rel string) {
	t.Helper()
	writeTestFile(t, dir, rel, []byte(testutil.GeneratedProviderPackageSource()), 0644)
}

func rustProviderFixturePath(t *testing.T) string {
	t.Helper()

	root, ok := pluginTestRepoRoot()
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(root, "gestaltd", "internal", "testutil", "testdata", "provider-rust")
}

func rustAuthProviderFixturePath(t *testing.T) string {
	t.Helper()

	root, ok := pluginTestRepoRoot()
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(root, "gestaltd", "internal", "testutil", "testdata", "provider-rust-auth")
}

func rustDatastoreProviderFixturePath(t *testing.T) string {
	t.Helper()

	root, ok := pluginTestRepoRoot()
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(root, "gestaltd", "internal", "testutil", "testdata", "provider-rust-datastore")
}

func buildGoSourceAuthBinary(t *testing.T) string {
	t.Helper()

	providerDir := filepath.Join(t.TempDir(), "go-auth")
	if err := os.MkdirAll(providerDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(providerDir): %v", err)
	}
	writeTestFile(t, providerDir, "go.mod", []byte(testutil.GeneratedProviderModuleSource(t, "example.com/test-go-auth")), 0o644)
	writeTestFile(t, providerDir, "go.sum", testutil.GeneratedProviderModuleSum(t), 0o644)
	writeTestFile(t, providerDir, "auth.go", []byte(testutil.GeneratedAuthPackageSource()), 0o644)
	outputPath := filepath.Join(t.TempDir(), "auth-provider")
	if err := pluginpkg.BuildGoComponentBinary(providerDir, outputPath, pluginmanifestv1.KindAuth, runtime.GOOS, runtime.GOARCH); err != nil {
		t.Fatalf("BuildGoComponentBinary(auth): %v", err)
	}
	return outputPath
}

func buildGoSourceDatastoreBinary(t *testing.T) string {
	t.Helper()

	providerDir := filepath.Join(t.TempDir(), "go-datastore")
	if err := os.MkdirAll(providerDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(providerDir): %v", err)
	}
	writeTestFile(t, providerDir, "go.mod", []byte(testutil.GeneratedProviderModuleSource(t, "example.com/test-go-datastore")), 0o644)
	writeTestFile(t, providerDir, "go.sum", testutil.GeneratedProviderModuleSum(t), 0o644)
	writeTestFile(t, providerDir, "datastore.go", []byte(testutil.GeneratedDatastorePackageSource()), 0o644)
	outputPath := filepath.Join(t.TempDir(), "datastore-provider")
	if err := pluginpkg.BuildGoComponentBinary(providerDir, outputPath, pluginmanifestv1.KindDatastore, runtime.GOOS, runtime.GOARCH); err != nil {
		t.Fatalf("BuildGoComponentBinary(datastore): %v", err)
	}
	return outputPath
}

func copyFixtureTree(t *testing.T, src, dst string) {
	t.Helper()

	if err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	}); err != nil {
		t.Fatalf("copy fixture tree: %v", err)
	}
}

type fakeRustCargoConfig struct {
	ExpectedPluginName   string
	ExpectedServeExport  string
	ExpectedCatalogWrite bool
	GeneratedCatalog     string
	DelegateBinary       string
	AllowedTargets       []string
}

func writeFakeRustReleaseCargo(t *testing.T, path string, cfg fakeRustCargoConfig) {
	t.Helper()

	allowedTargets := make([]string, 0, len(cfg.AllowedTargets))
	for _, target := range cfg.AllowedTargets {
		if target == "" {
			continue
		}
		allowedTargets = append(allowedTargets, shellSingleQuoted(target))
	}
	script := `#!/bin/sh
set -eu

manifest=""
target=""
target_dir=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --manifest-path)
      manifest="$2"
      shift 2
      ;;
    --target)
      target="$2"
      shift 2
      ;;
    --target-dir)
      target_dir="$2"
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done

if [ -z "$manifest" ] || [ -z "$target" ] || [ -z "$target_dir" ]; then
  echo "missing cargo wrapper args" >&2
  exit 1
fi

allowed=false
for candidate in ` + strings.Join(allowedTargets, " ") + `; do
  if [ "$target" = "$candidate" ]; then
    allowed=true
    break
  fi
done
if [ "$allowed" != "true" ]; then
  echo "unexpected target triple: $target" >&2
  exit 1
fi

main_rs="$(dirname "$manifest")/src/main.rs"
if ! grep -q 'const PLUGIN_NAME: &str = "` + cfg.ExpectedPluginName + `";' "$main_rs"; then
  echo "missing plugin name in wrapper source" >&2
  exit 1
fi
if ! grep -Fq 'provider_plugin::` + cfg.ExpectedServeExport + `(PLUGIN_NAME)?' "$main_rs"; then
  echo "missing serve export in wrapper source" >&2
  exit 1
fi
` + fakeRustReleaseCatalogCheck(cfg.ExpectedCatalogWrite) + `
if ! grep -Fq 'Ok(())' "$main_rs"; then
  echo "missing explicit Ok return in wrapper source" >&2
  exit 1
fi

binary="$target_dir/$target/release/` + rustWrapperBinaryName + `"
mkdir -p "$(dirname "$binary")"
cat > "$binary" <<'EOF'
#!/bin/sh
set -eu
if [ -n "${GESTALT_PLUGIN_WRITE_CATALOG:-}" ]; then
  cat > "$GESTALT_PLUGIN_WRITE_CATALOG" <<'YAML'
name: ` + cfg.GeneratedCatalog + `
operations:
  - id: greet
    method: GET
YAML
  exit 0
fi
exec ` + shellSingleQuoted(cfg.DelegateBinary) + ` "$@"
EOF
chmod +x "$binary"
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func fakeRustReleaseCatalogCheck(expectCatalog bool) string {
	if expectCatalog {
		return `if ! grep -Fq 'provider_plugin::__gestalt_write_catalog(PLUGIN_NAME, &path)?' "$main_rs"; then
  echo "missing write-catalog export in wrapper source" >&2
  exit 1
fi`
	}
	return `if grep -Fq 'provider_plugin::__gestalt_write_catalog(PLUGIN_NAME, &path)?' "$main_rs"; then
  echo "unexpected write-catalog export in wrapper source" >&2
  exit 1
fi`
}

func pluginpkgRustTargetTriple(goos, goarch, libc string) (string, string, error) {
	switch goos {
	case "darwin":
		switch goarch {
		case "amd64":
			return "x86_64-apple-darwin", "", nil
		case "arm64":
			return "aarch64-apple-darwin", "", nil
		}
	case "linux":
		normalizedLibC, err := pluginpkg.NormalizeArtifactLibC(goos, libc)
		if err != nil {
			return "", "", err
		}
		if normalizedLibC == "" {
			normalizedLibC = expectedRustReleasePlatform(goos, goarch, "").LibC
		}
		switch goarch {
		case "amd64":
			if normalizedLibC == pluginpkg.LinuxLibCMusl {
				return "x86_64-unknown-linux-musl", normalizedLibC, nil
			}
			return "x86_64-unknown-linux-gnu", normalizedLibC, nil
		case "arm64":
			if normalizedLibC == pluginpkg.LinuxLibCMusl {
				return "aarch64-unknown-linux-musl", normalizedLibC, nil
			}
			return "aarch64-unknown-linux-gnu", normalizedLibC, nil
		}
	case "windows":
		switch goarch {
		case "amd64":
			return "x86_64-pc-windows-gnu", "", nil
		}
	}
	return "", "", fmt.Errorf("unsupported Rust target platform %s/%s", goos, goarch)
}

func shellSingleQuoted(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func pluginTestRepoRoot() (string, bool) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", false
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..")), true
}

func newPrebuiltProviderReleaseFixture(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := filepath.Join(dir, prebuiltProviderPluginName)
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("MkdirAll(pluginDir): %v", err)
	}
	writeTestFile(t, pluginDir, releaseTestIconPath, []byte("<svg></svg>\n"), 0644)
	writeTestFile(t, pluginDir, prebuiltProviderBinaryPath, []byte("prebuilt-provider"), 0755)
	writeReleaseTestManifest(t, pluginDir, &pluginmanifestv1.Manifest{
		Source:      prebuiltProviderSource,
		Version:     "0.0.1",
		DisplayName: "Prebuilt Provider",
		IconFile:    releaseTestIconPath,
		Kinds:       []string{pluginmanifestv1.KindPlugin},
		Plugin: &pluginmanifestv1.Plugin{
			ConfigSchemaPath: releaseProviderSchemaPath,
		},
		Artifacts: []pluginmanifestv1.Artifact{
			{
				OS:   runtime.GOOS,
				Arch: runtime.GOARCH,
				Path: prebuiltProviderBinaryPath,
			},
		},
		Entrypoints: pluginmanifestv1.Entrypoints{
			Provider: &pluginmanifestv1.Entrypoint{
				ArtifactPath: prebuiltProviderBinaryPath,
			},
		},
	})
	writeTestFile(t, pluginDir, releaseProviderSchemaPath, []byte(`{"type":"object"}`), 0o644)
	return pluginDir
}

func newWebUIReleaseFixture(t *testing.T, dir string) string {
	return newWebUIReleaseFixtureWithAssetRoot(t, dir, webUITestAssetRoot)
}

func newWebUIReleaseFixtureWithAssetRoot(t *testing.T, dir, assetRoot string) string {
	t.Helper()

	pluginDir := filepath.Join(dir, webUITestPluginName)
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("MkdirAll(pluginDir): %v", err)
	}
	writeReleaseTestManifest(t, pluginDir, &pluginmanifestv1.Manifest{
		Source:      webUITestSource,
		Version:     "0.0.1",
		DisplayName: "WebUI Test",
		IconFile:    releaseTestIconPath,
		Kinds:       []string{pluginmanifestv1.KindWebUI},
		WebUI: &pluginmanifestv1.WebUIMetadata{
			AssetRoot: assetRoot,
		},
	})
	writeTestFile(t, pluginDir, releaseTestIconPath, []byte("<svg></svg>\n"), 0644)
	writeTestFile(t, pluginDir, assetRoot+"/index.html", []byte("<html></html>\n"), 0644)
	writeTestFile(t, pluginDir, assetRoot+"/static/app.js", []byte("console.log('ok')\n"), 0644)
	return pluginDir
}

func runPluginReleaseCommand(t *testing.T, pluginDir string, args ...string) string {
	t.Helper()

	out, err := runPluginReleaseCommandResult(pluginDir, args...)
	if err != nil {
		t.Fatalf("plugin release failed: %v\n%s", err, out)
	}
	return string(out)
}

func runPluginCommandResult(pluginDir string, args ...string) ([]byte, error) {
	cmdArgs := append([]string{"plugin"}, args...)
	cmd := exec.Command(gestaltdBin, cmdArgs...)
	cmd.Dir = pluginDir
	return cmd.CombinedOutput()
}

func runPluginReleaseCommandResult(pluginDir string, args ...string) ([]byte, error) {
	return runPluginCommandResult(pluginDir, append([]string{"release"}, args...)...)
}

func extractReleasedArchive(t *testing.T, outputDir, archiveName string) string {
	t.Helper()

	archivePath := filepath.Join(outputDir, archiveName)
	if _, err := os.Stat(archivePath); err != nil {
		t.Fatalf("expected archive %s to exist: %v", archiveName, err)
	}
	extractDir := filepath.Join(outputDir, strings.TrimSuffix(archiveName, ".tar.gz"))
	if err := pluginpkg.ExtractPackage(archivePath, extractDir); err != nil {
		t.Fatalf("extract archive: %v", err)
	}
	return extractDir
}

func readReleasedManifest(t *testing.T, outputDir, archiveName string) *pluginmanifestv1.Manifest {
	t.Helper()

	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifestPath, err := pluginpkg.FindManifestFile(extractDir)
	if err != nil {
		t.Fatalf("find released manifest: %v", err)
	}
	_, manifest, err := pluginpkg.ReadManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("read released manifest: %v", err)
	}
	return manifest
}

func readManifestFromDir(t *testing.T, dir string) (string, *pluginmanifestv1.Manifest) {
	t.Helper()

	manifestPath, err := pluginpkg.FindManifestFile(dir)
	if err != nil {
		t.Fatalf("find manifest: %v", err)
	}
	_, manifest, err := pluginpkg.ReadManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	return manifestPath, manifest
}

func writeReleaseTestManifest(t *testing.T, dir string, manifest *pluginmanifestv1.Manifest) {
	t.Helper()
	writeReleaseTestManifestFormat(t, dir, pluginpkg.ManifestFile, manifest)
}

func writeReleaseTestManifestFormat(t *testing.T, dir, manifestFile string, manifest *pluginmanifestv1.Manifest) {
	t.Helper()
	populateMissingArtifactDigests(t, dir, manifest)
	data, err := encodeTestManifestFormat(manifest, pluginpkg.ManifestFormatFromPath(manifestFile))
	if err != nil {
		t.Fatalf("encodeTestManifestFormat(%s): %v", manifestFile, err)
	}
	writeTestFile(t, dir, manifestFile, data, 0644)
	if manifest.Plugin != nil {
		writeTestFile(t, dir, pluginpkg.StaticCatalogFile, []byte("name: provider\noperations:\n  - id: echo\n    method: POST\n"), 0644)
	}
}

func populateMissingArtifactDigests(t *testing.T, dir string, manifest *pluginmanifestv1.Manifest) {
	t.Helper()

	for i := range manifest.Artifacts {
		if manifest.Artifacts[i].SHA256 != "" {
			continue
		}

		path := filepath.Join(dir, filepath.FromSlash(manifest.Artifacts[i].Path))
		data, err := os.ReadFile(path)
		if err == nil {
			manifest.Artifacts[i].SHA256 = sha256HexForTest(string(data))
			continue
		}

		manifest.Artifacts[i].SHA256 = sha256HexForTest(manifest.Artifacts[i].Path)
	}
}

func encodeTestManifestFormat(manifest *pluginmanifestv1.Manifest, format string) ([]byte, error) {
	return pluginpkg.EncodeSourceManifestFormat(manifest, format)
}

func writeTestFile(t *testing.T, dir, rel string, data []byte, mode os.FileMode) {
	t.Helper()

	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", rel, err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatalf("WriteFile(%s): %v", rel, err)
	}
}

func sha256HexForTest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
