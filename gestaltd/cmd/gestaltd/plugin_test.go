package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/pluginhost"
	"github.com/valon-technologies/gestalt/server/internal/pluginpkg"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

const (
	releaseTestPluginName      = "release-test"
	releaseTestSource          = "github.com/testowner/plugins/release-test"
	releaseTestModule          = "example.com/release-test"
	releaseTestIconPath        = "branding/icon.svg"
	releaseProviderSchemaPath  = "schemas/provider.schema.json"
	releaseHybridArg           = "--serve-provider"
	releaseHybridBaseURL       = "https://api.example.com"
	releaseHybridOperationName = "list_items"
	releaseSourceArtifactPath  = "artifacts/source-plugin"
	webUITestPluginName        = "webui-test"
	webUITestSource            = "github.com/testowner/plugins/webui-test"
	webUITestAssetRoot         = "out"
	prebuiltHybridPluginName   = "prebuilt-hybrid"
	prebuiltHybridSource       = "github.com/testowner/plugins/prebuilt-hybrid"
	prebuiltHybridArtifactPath = "bin/provider"
	specLoadedHybridPluginName = "spec-loaded-hybrid"
	specLoadedHybridSource     = "github.com/testowner/plugins/spec-loaded-hybrid"
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

	pluginDir := newDeclarativeProviderReleaseFixture(t, t.TempDir())
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
			name: "rest surface requires operations",
			manifestYAML: `
source: github.com/testowner/plugins/invalid
version: 0.1.0
provider:
  surfaces:
    rest:
      base_url: https://api.example.com
`,
			wantError: "missing property 'operations'",
		},
		{
			name: "exec requires artifact path",
			manifestYAML: `
source: github.com/testowner/plugins/invalid
version: 0.1.0
provider:
  exec: {}
  surfaces: {}
`,
			wantError: "missing property 'artifact_path'",
		},
		{
			name: "mcp block requires enabled",
			manifestYAML: `
source: github.com/testowner/plugins/invalid
version: 0.1.0
provider:
  mcp: {}
  surfaces:
    rest:
      base_url: https://api.example.com
      operations:
        - name: list_items
          method: GET
          path: /items
`,
			wantError: "missing property 'enabled'",
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
version = "0.1.0"
dependencies = ["gestalt"]

[tool.gestalt]
plugin = "os import path\nimport os;os.system('cmd')#:attr"
`), 0o644)
	writeTestFile(t, pluginDir, "provider.py", []byte("plugin = None\n"), 0o644)
	manifestData, err := pluginpkg.EncodeSourceManifestFormat(&pluginmanifestv1.Manifest{
		Source:  "github.com/testowner/plugins/invalid-python-release",
		Version: "0.0.1",
		Kinds:   []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
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

func TestRun_PluginReleaseCopiesCompiledSupportFiles(t *testing.T) {
	t.Parallel()

	pluginDir := newCompiledReleaseFixture(t, t.TempDir())
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

func TestRun_PluginReleaseIgnoresHelperMainPackagesWithoutProviderPackage(t *testing.T) {
	t.Parallel()

	pluginDir := newDeclarativeProviderReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.4-helper.1"

	writeTestFile(t, pluginDir, "go.mod", []byte("module example.com/declarative-provider\n\ngo 1.22\n"), 0644)
	writeTestFile(t, pluginDir, "cmd/helper/main.go", []byte("package main\n\nfunc main() {}\n"), 0644)

	runPluginReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", runtime.GOOS+"/"+runtime.GOARCH,
		"--output", outputDir,
	)

	archiveName := "gestalt-plugin-declarative-provider_v" + testVersion + ".tar.gz"
	manifest := readReleasedManifest(t, outputDir, archiveName)

	if len(manifest.Artifacts) != 0 {
		t.Fatalf("expected declarative release to omit artifacts, got %+v", manifest.Artifacts)
	}
	if manifest.Entrypoints.Provider != nil {
		t.Fatalf("expected declarative release to omit provider entrypoint, got %+v", manifest.Entrypoints.Provider)
	}

	compiledArchiveName := "gestalt-plugin-declarative-provider_v" + testVersion + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	if _, err := os.Stat(filepath.Join(outputDir, compiledArchiveName)); !os.IsNotExist(err) {
		t.Fatalf("unexpected compiled archive %s: %v", compiledArchiveName, err)
	}
}

func TestRun_PluginReleasePreservesYAMLManifestFormatAndDefaultConnectionParams(t *testing.T) {
	t.Parallel()

	pluginDir := filepath.Join(t.TempDir(), "declarative-provider")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(pluginDir): %v", err)
	}
	writeTestFile(t, pluginDir, "plugin.yaml", []byte(`
source: github.com/testowner/plugins/declarative-provider
version: 0.0.1
provider:
  mcp:
    enabled: true
  connections:
    default:
      mode: identity
      params:
        tenant:
          required: true
  surfaces:
    rest:
      base_url: https://api.example.com
      operations:
        - name: list_items
          method: GET
          path: /items
`), 0o644)

	outputDir := t.TempDir()
	const testVersion = "0.0.4-yaml.1"

	runPluginReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)

	archiveName := "gestalt-plugin-declarative-provider_v" + testVersion + ".tar.gz"
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifestPath, manifest := readManifestFromDir(t, extractDir)
	if filepath.Base(manifestPath) != "plugin.yaml" {
		t.Fatalf("released manifest = %q, want plugin.yaml", filepath.Base(manifestPath))
	}
	if manifest.Provider == nil || len(manifest.Provider.ConnectionParams) != 1 || !manifest.Provider.ConnectionParams["tenant"].Required {
		t.Fatalf("provider connection_params = %+v", manifest.Provider)
	}
	if manifest.Provider.ConnectionMode != "identity" {
		t.Fatalf("provider connection_mode = %q, want %q", manifest.Provider.ConnectionMode, "identity")
	}

	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read released manifest: %v", err)
	}
	if !strings.Contains(string(manifestData), "mcp:") || !strings.Contains(string(manifestData), "enabled: true") || !strings.Contains(string(manifestData), "connections:") || !strings.Contains(string(manifestData), "params:") || !strings.Contains(string(manifestData), "mode: identity") {
		t.Fatalf("expected released manifest to use connections.default.params, got: %s", manifestData)
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

func TestRun_PluginReleasePreservesHybridProviderManifest(t *testing.T) {
	t.Parallel()

	pluginDir := newHybridReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.4-test"

	runPluginReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", runtime.GOOS+"/"+runtime.GOARCH,
		"--output", outputDir,
	)

	archiveName := "gestalt-plugin-" + releaseTestPluginName + "_v" + testVersion + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	manifest := readReleasedManifest(t, outputDir, archiveName)

	if manifest.IsDeclarativeOnlyProvider() {
		t.Fatal("expected released manifest to remain executable for hybrid provider")
	}
	if manifest.Entrypoints.Provider == nil {
		t.Fatal("expected provider entrypoint")
	}
	if manifest.Entrypoints.Provider.ArtifactPath != "gestalt-plugin-"+releaseTestPluginName {
		t.Fatalf("provider artifact path = %q", manifest.Entrypoints.Provider.ArtifactPath)
	}
	if len(manifest.Entrypoints.Provider.Args) != 1 || manifest.Entrypoints.Provider.Args[0] != releaseHybridArg {
		t.Fatalf("provider entrypoint args = %v, want [%q]", manifest.Entrypoints.Provider.Args, releaseHybridArg)
	}
	if manifest.Provider == nil {
		t.Fatal("expected provider metadata")
	}
	if manifest.Provider.BaseURL != releaseHybridBaseURL {
		t.Fatalf("provider base_url = %q, want %q", manifest.Provider.BaseURL, releaseHybridBaseURL)
	}
	if len(manifest.Provider.Operations) != 1 || manifest.Provider.Operations[0].Name != releaseHybridOperationName {
		t.Fatalf("provider operations = %+v", manifest.Provider.Operations)
	}
}

func TestRun_PluginReleaseCompilesManifestBackedProviderWithoutSourceArtifacts(t *testing.T) {
	t.Parallel()

	pluginDir := newCompiledManifestBackedProviderReleaseFixture(t, t.TempDir())
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

	if manifest.IsDeclarativeOnlyProvider() {
		t.Fatal("expected released manifest to remain executable for compiled manifest-backed provider")
	}
	if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Path != releaseBinaryName(releaseTestPluginName, runtime.GOOS) {
		t.Fatalf("artifacts = %+v", manifest.Artifacts)
	}
	if manifest.Entrypoints.Provider == nil || manifest.Entrypoints.Provider.ArtifactPath != releaseBinaryName(releaseTestPluginName, runtime.GOOS) {
		t.Fatalf("provider entrypoint = %+v", manifest.Entrypoints.Provider)
	}
	if manifest.Provider == nil || manifest.Provider.BaseURL != releaseHybridBaseURL {
		t.Fatalf("provider base_url = %#v, want %q", manifest.Provider, releaseHybridBaseURL)
	}
	data, err := os.ReadFile(filepath.Join(extractDir, pluginpkg.StaticCatalogFile))
	if err != nil {
		t.Fatalf("read generated catalog: %v", err)
	}
	if !strings.Contains(string(data), "generated_op") {
		t.Fatalf("unexpected generated catalog: %s", data)
	}
}

func TestRun_PluginReleasePreservesPrebuiltHybridProvider(t *testing.T) {
	t.Parallel()

	pluginDir := newPrebuiltHybridReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.5-test"

	runPluginReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)

	archiveName := "gestalt-plugin-" + prebuiltHybridPluginName + "_v" + testVersion + ".tar.gz"
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifest := readReleasedManifest(t, outputDir, archiveName)

	if manifest.IsDeclarativeOnlyProvider() {
		t.Fatal("expected prebuilt hybrid provider to remain executable")
	}
	if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Path != prebuiltHybridArtifactPath {
		t.Fatalf("artifacts = %+v", manifest.Artifacts)
	}
	if manifest.Entrypoints.Provider == nil {
		t.Fatal("expected provider entrypoint")
	}
	if manifest.Entrypoints.Provider.ArtifactPath != prebuiltHybridArtifactPath {
		t.Fatalf("provider artifact path = %q", manifest.Entrypoints.Provider.ArtifactPath)
	}
	if len(manifest.Entrypoints.Provider.Args) != 1 || manifest.Entrypoints.Provider.Args[0] != releaseHybridArg {
		t.Fatalf("provider entrypoint args = %v", manifest.Entrypoints.Provider.Args)
	}
	if _, err := os.Stat(filepath.Join(extractDir, filepath.FromSlash(prebuiltHybridArtifactPath))); err != nil {
		t.Fatalf("expected prebuilt artifact in archive: %v", err)
	}
}

func TestRun_PluginReleasePreservesSpecLoadedHybridProvider(t *testing.T) {
	t.Parallel()

	pluginDir := newSpecLoadedHybridReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.5-test"

	runPluginReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)

	archiveName := "gestalt-plugin-" + specLoadedHybridPluginName + "_v" + testVersion + ".tar.gz"
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifest := readReleasedManifest(t, outputDir, archiveName)

	if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Path != prebuiltHybridArtifactPath {
		t.Fatalf("artifacts = %+v", manifest.Artifacts)
	}
	if manifest.Entrypoints.Provider == nil {
		t.Fatal("expected provider entrypoint")
	}
	if manifest.Entrypoints.Provider.ArtifactPath != prebuiltHybridArtifactPath {
		t.Fatalf("provider artifact path = %q", manifest.Entrypoints.Provider.ArtifactPath)
	}
	if manifest.Provider == nil || manifest.Provider.OpenAPI != "specs/openapi.yaml" {
		t.Fatalf("provider openapi = %#v, want specs/openapi.yaml", manifest.Provider)
	}
	if len(manifest.Provider.AllowedOperations) != 2 {
		t.Fatalf("allowed operations = %+v", manifest.Provider.AllowedOperations)
	}
	if _, err := os.Stat(filepath.Join(extractDir, filepath.FromSlash(prebuiltHybridArtifactPath))); err != nil {
		t.Fatalf("expected prebuilt artifact in archive: %v", err)
	}
	if _, err := os.Stat(filepath.Join(extractDir, "specs", "openapi.yaml")); err != nil {
		t.Fatalf("expected spec file in archive: %v", err)
	}
}

func TestRun_PluginReleasePackagesGoModuleWithoutCmdAsSource(t *testing.T) {
	t.Parallel()

	pluginDir := newPrebuiltHybridReleaseFixture(t, t.TempDir())
	writeTestFile(t, pluginDir, "go.mod", []byte("module example.com/prebuilt-hybrid\n\ngo 1.22\n"), 0644)

	outputDir := t.TempDir()
	const testVersion = "0.0.6-test"

	runPluginReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)

	archiveName := "gestalt-plugin-" + prebuiltHybridPluginName + "_v" + testVersion + ".tar.gz"
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifest := readReleasedManifest(t, outputDir, archiveName)

	if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Path != prebuiltHybridArtifactPath {
		t.Fatalf("artifacts = %+v", manifest.Artifacts)
	}
	if manifest.Entrypoints.Provider == nil || manifest.Entrypoints.Provider.ArtifactPath != prebuiltHybridArtifactPath {
		t.Fatalf("provider entrypoint = %+v", manifest.Entrypoints.Provider)
	}
	if _, err := os.Stat(filepath.Join(extractDir, filepath.FromSlash(prebuiltHybridArtifactPath))); err != nil {
		t.Fatalf("expected prebuilt artifact in archive: %v", err)
	}
}

func TestRun_PluginReleaseRejectsStaleSourceArtifactDigest(t *testing.T) {
	t.Parallel()

	pluginDir := newPrebuiltHybridReleaseFixture(t, t.TempDir())

	_, manifest, err := pluginpkg.ReadSourceManifestFile(filepath.Join(pluginDir, pluginpkg.ManifestFile))
	if err != nil {
		t.Fatalf("ReadSourceManifestFile(plugin.json): %v", err)
	}
	manifest.Artifacts = []pluginmanifestv1.Artifact{
		{
			OS:     runtime.GOOS,
			Arch:   runtime.GOARCH,
			Path:   prebuiltHybridArtifactPath,
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

	pluginDir := newCompiledReleaseFixture(t, t.TempDir())
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

func TestRun_PluginReleaseCopiesDeclarativeProviderSupportFiles(t *testing.T) {
	pluginDir := newDeclarativeProviderReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.10-test"
	t.Setenv("PATH", pathWithoutGo(t))

	runPluginReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)

	archiveName := "gestalt-plugin-declarative-provider_v" + testVersion + ".tar.gz"
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifest := readReleasedManifest(t, outputDir, archiveName)

	if len(manifest.Artifacts) != 0 {
		t.Fatalf("expected declarative release to omit artifacts, got %+v", manifest.Artifacts)
	}
	for _, rel := range []string{releaseTestIconPath, releaseProviderSchemaPath} {
		if _, err := os.Stat(filepath.Join(extractDir, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("expected %s in archive: %v", rel, err)
		}
	}
}

func TestRun_PluginReleaseCopiesSpecLoadedProviderSupportFiles(t *testing.T) {
	t.Parallel()

	pluginDir := newSpecLoadedProviderReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.11-test"

	runPluginReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)

	archiveName := "gestalt-plugin-spec-loaded-provider_v" + testVersion + ".tar.gz"
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifest := readReleasedManifest(t, outputDir, archiveName)

	if manifest.Version != testVersion {
		t.Fatalf("manifest version = %q, want %q", manifest.Version, testVersion)
	}
	if len(manifest.Artifacts) != 0 {
		t.Fatalf("expected spec-loaded release to omit artifacts, got %+v", manifest.Artifacts)
	}
	if manifest.Provider == nil || manifest.Provider.OpenAPI != "specs/openapi.yaml" {
		t.Fatalf("provider openapi = %#v, want specs/openapi.yaml", manifest.Provider)
	}
	for _, rel := range []string{releaseTestIconPath, "specs/openapi.yaml"} {
		if _, err := os.Stat(filepath.Join(extractDir, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("expected %s in archive: %v", rel, err)
		}
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
version = "0.1.0"
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
		Kinds:   []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
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

func newCompiledReleaseFixture(t *testing.T, dir string) string {
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
		Kinds:       []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			BaseURL:          releaseHybridBaseURL,
			ConfigSchemaPath: releaseProviderSchemaPath,
			Operations: []pluginmanifestv1.ProviderOperation{
				{
					Name:   releaseHybridOperationName,
					Method: "GET",
					Path:   "/items",
				},
			},
		},
		Artifacts: []pluginmanifestv1.Artifact{
			{
				OS:   runtime.GOOS,
				Arch: runtime.GOARCH,
				Path: releaseSourceArtifactPath,
			},
		},
		Entrypoints: pluginmanifestv1.Entrypoints{
			Provider: &pluginmanifestv1.Entrypoint{ArtifactPath: releaseSourceArtifactPath},
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
		Kinds:       []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			Auth: &pluginmanifestv1.ProviderAuth{Type: pluginmanifestv1.AuthTypeNone},
		},
	})
	return pluginDir
}

func newDeclarativeProviderReleaseFixture(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := filepath.Join(dir, "declarative-provider")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("MkdirAll(pluginDir): %v", err)
	}
	writeReleaseTestManifest(t, pluginDir, &pluginmanifestv1.Manifest{
		Source:      "github.com/testowner/plugins/declarative-provider",
		Version:     "0.0.1",
		DisplayName: "Declarative Provider",
		IconFile:    releaseTestIconPath,
		Kinds:       []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			BaseURL:          releaseHybridBaseURL,
			ConfigSchemaPath: releaseProviderSchemaPath,
			Operations: []pluginmanifestv1.ProviderOperation{
				{Name: releaseHybridOperationName, Method: "GET", Path: "/items"},
			},
		},
	})
	writeTestFile(t, pluginDir, releaseTestIconPath, []byte("<svg></svg>\n"), 0644)
	writeTestFile(t, pluginDir, releaseProviderSchemaPath, []byte(`{"type":"object"}`), 0644)
	return pluginDir
}

func newSpecLoadedProviderReleaseFixture(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := filepath.Join(dir, "spec-loaded-provider")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("MkdirAll(pluginDir): %v", err)
	}
	writeReleaseTestManifest(t, pluginDir, &pluginmanifestv1.Manifest{
		Source:      "github.com/testowner/plugins/spec-loaded-provider",
		Version:     "0.0.1",
		DisplayName: "Spec Loaded Provider",
		IconFile:    releaseTestIconPath,
		Kinds:       []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			OpenAPI: "specs/openapi.yaml",
		},
	})
	writeTestFile(t, pluginDir, releaseTestIconPath, []byte("<svg></svg>\n"), 0644)
	writeTestFile(t, pluginDir, "specs/openapi.yaml", []byte("openapi: 3.0.0\ninfo:\n  title: Test\n  version: 1.0.0\npaths: {}\n"), 0644)
	return pluginDir
}

func newHybridReleaseFixture(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := newCompiledReleaseFixture(t, dir)
	writeReleaseTestManifest(t, pluginDir, &pluginmanifestv1.Manifest{
		Source:      releaseTestSource,
		Version:     "0.0.1",
		DisplayName: "Release Test",
		IconFile:    releaseTestIconPath,
		Kinds:       []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			BaseURL: releaseHybridBaseURL,
			Operations: []pluginmanifestv1.ProviderOperation{
				{
					Name:   releaseHybridOperationName,
					Method: "GET",
					Path:   "/items",
				},
			},
		},
		Artifacts: []pluginmanifestv1.Artifact{
			{
				OS:   runtime.GOOS,
				Arch: runtime.GOARCH,
				Path: releaseSourceArtifactPath,
			},
		},
		Entrypoints: pluginmanifestv1.Entrypoints{
			Provider: &pluginmanifestv1.Entrypoint{
				ArtifactPath: releaseSourceArtifactPath,
				Args:         []string{releaseHybridArg},
			},
		},
	})

	return pluginDir
}

func newCompiledManifestBackedProviderReleaseFixture(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := newCompiledReleaseFixture(t, dir)
	writeReleaseTestManifest(t, pluginDir, &pluginmanifestv1.Manifest{
		Source:      releaseTestSource,
		Version:     "0.0.1",
		DisplayName: "Release Test",
		IconFile:    releaseTestIconPath,
		Kinds:       []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			BaseURL: releaseHybridBaseURL,
			Operations: []pluginmanifestv1.ProviderOperation{
				{
					Name:   releaseHybridOperationName,
					Method: "GET",
					Path:   "/items",
				},
			},
		},
	})
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

func newPrebuiltHybridReleaseFixture(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := filepath.Join(dir, prebuiltHybridPluginName)
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("MkdirAll(pluginDir): %v", err)
	}
	writeTestFile(t, pluginDir, releaseTestIconPath, []byte("<svg></svg>\n"), 0644)
	writeTestFile(t, pluginDir, prebuiltHybridArtifactPath, []byte("prebuilt-provider"), 0755)
	writeReleaseTestManifest(t, pluginDir, &pluginmanifestv1.Manifest{
		Source:      prebuiltHybridSource,
		Version:     "0.0.1",
		DisplayName: "Prebuilt Hybrid",
		IconFile:    releaseTestIconPath,
		Kinds:       []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			BaseURL: releaseHybridBaseURL,
			Operations: []pluginmanifestv1.ProviderOperation{
				{
					Name:   releaseHybridOperationName,
					Method: "GET",
					Path:   "/items",
				},
			},
		},
		Artifacts: []pluginmanifestv1.Artifact{
			{
				OS:   runtime.GOOS,
				Arch: runtime.GOARCH,
				Path: prebuiltHybridArtifactPath,
			},
		},
		Entrypoints: pluginmanifestv1.Entrypoints{
			Provider: &pluginmanifestv1.Entrypoint{
				ArtifactPath: prebuiltHybridArtifactPath,
				Args:         []string{releaseHybridArg},
			},
		},
	})
	return pluginDir
}

func newSpecLoadedHybridReleaseFixture(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := filepath.Join(dir, specLoadedHybridPluginName)
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("MkdirAll(pluginDir): %v", err)
	}
	writeTestFile(t, pluginDir, releaseTestIconPath, []byte("<svg></svg>\n"), 0644)
	writeTestFile(t, pluginDir, prebuiltHybridArtifactPath, []byte("spec-loaded-hybrid-provider"), 0755)
	writeTestFile(t, pluginDir, "specs/openapi.yaml", []byte("openapi: 3.0.0\ninfo:\n  title: Test\n  version: 1.0.0\npaths: {}\n"), 0644)
	writeReleaseTestManifest(t, pluginDir, &pluginmanifestv1.Manifest{
		Source:      specLoadedHybridSource,
		Version:     "0.0.1",
		DisplayName: "Spec Loaded Hybrid",
		IconFile:    releaseTestIconPath,
		Kinds:       []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			OpenAPI: "specs/openapi.yaml",
			AllowedOperations: map[string]*pluginmanifestv1.ManifestOperationOverride{
				"gmail.users.messages.list": {Alias: "messages.list"},
				"gmail.users.getProfile":    {Alias: "getProfile"},
			},
		},
		Artifacts: []pluginmanifestv1.Artifact{
			{
				OS:   runtime.GOOS,
				Arch: runtime.GOARCH,
				Path: prebuiltHybridArtifactPath,
			},
		},
		Entrypoints: pluginmanifestv1.Entrypoints{
			Provider: &pluginmanifestv1.Entrypoint{
				ArtifactPath: prebuiltHybridArtifactPath,
				Args:         []string{releaseHybridArg},
			},
		},
	})
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
	if manifest.Provider != nil {
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
