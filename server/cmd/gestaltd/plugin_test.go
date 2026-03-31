package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/pluginpkg"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

var stdoutMu sync.Mutex

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
)

func TestRun_PluginHelpExitsCleanly(t *testing.T) {
	t.Parallel()

	cmd := exec.Command("go", "run", ".", "plugin", "--help")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected exit 0 for 'plugin --help', got error: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "gestaltd plugin <command> [flags]") {
		t.Fatalf("expected plugin usage output, got: %s", out)
	}
	if !strings.Contains(string(out), "release") {
		t.Fatalf("expected release in help output, got: %s", out)
	}
	for _, removed := range []string{"install", "inspect", "list", "init"} {
		if strings.Contains(string(out), removed) {
			t.Fatalf("expected %q absent from help, got: %s", removed, out)
		}
	}
}

func TestRun_PluginPackageHelpExitsCleanly(t *testing.T) {
	t.Parallel()

	cmd := exec.Command("go", "run", ".", "plugin", "package", "--help")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected exit 0 for 'plugin package --help', got error: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "gestaltd plugin package --input PATH --output PATH") {
		t.Fatalf("expected package usage output, got: %s", out)
	}
	if strings.Contains(string(out), "--binary") {
		t.Fatalf("expected --binary removed from help, got: %s", out)
	}
}

func TestRun_PluginReleaseHelpExitsCleanly(t *testing.T) {
	t.Parallel()

	cmd := exec.Command("go", "run", ".", "plugin", "release", "--help")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected exit 0 for 'plugin release --help', got error: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "--version") {
		t.Fatalf("expected --version in release help, got: %s", out)
	}
}

func TestRun_PluginRootReturnsHelpWhenNoSubcommandProvided(t *testing.T) {
	t.Parallel()

	err := run([]string{"plugin"})
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("expected flag.ErrHelp, got: %v", err)
	}
}

//nolint:paralleltest // Swaps os.Stdout via captureStdout.
func TestRun_PluginPackageCreatesArchive(t *testing.T) {
	dir := t.TempDir()
	src := newPluginPackageFixture(t, dir)
	outPath := filepath.Join(dir, "testowner-provider-0.1.0.tar.gz")

	output := captureStdout(t, func() error {
		return run([]string{"plugin", "package", "--input", src, "--output", outPath})
	})

	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("expected package archive to exist: %v", err)
	}
	if !strings.Contains(output, "packaged") {
		t.Fatalf("expected package output, got: %q", output)
	}
}

//nolint:paralleltest // Swaps os.Stdout via captureStdout.
func TestRun_PluginPackageCreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	src := newPluginPackageFixture(t, dir)
	outPath := filepath.Join(dir, "output-dir")

	output := captureStdout(t, func() error {
		return run([]string{"plugin", "package", "--input", src, "--output", outPath})
	})

	info, err := os.Stat(outPath)
	if err != nil {
		t.Fatalf("expected output directory to exist: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("expected output to be a directory")
	}
	manifestPath := filepath.Join(outPath, "plugin.json")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("expected manifest in output directory: %v", err)
	}
	artifactPath := filepath.Join(outPath, "artifacts", runtime.GOOS, runtime.GOARCH, "provider")
	data, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatalf("expected artifact in output directory: %v", err)
	}
	if string(data) != "provider" {
		t.Fatalf("unexpected artifact content: %q", data)
	}
	if !strings.Contains(output, "packaged") {
		t.Fatalf("expected packaged output, got: %q", output)
	}
}

func TestRun_PluginPackageRejectsOutputInsideInput(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src := newPluginPackageFixture(t, dir)
	outPath := filepath.Join(src, "dist")

	err := run([]string{"plugin", "package", "--input", src, "--output", outPath})
	if err == nil {
		t.Fatal("expected error when output is inside input")
	}
	if !strings.Contains(err.Error(), "must not be inside source") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRun_PluginRejectsUnknownSubcommand(t *testing.T) {
	t.Parallel()

	err := run([]string{"plugin", "bogus"})
	if err == nil {
		t.Fatal("expected error for unknown plugin subcommand")
	}
	if !strings.Contains(err.Error(), `unknown plugin command "bogus"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRun_PluginReleaseRequiresVersion(t *testing.T) {
	t.Parallel()

	err := run([]string{"plugin", "release"})
	if err == nil {
		t.Fatal("expected error when --version missing")
	}
	if !strings.Contains(err.Error(), "--version is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRun_PluginReleaseRejectsInvalidManifest(t *testing.T) {
	t.Parallel()

	pluginDir := filepath.Join(t.TempDir(), "invalid-plugin")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("MkdirAll(pluginDir): %v", err)
	}
	writeTestFile(t, pluginDir, "plugin.yaml", []byte(`
source: github.com/testowner/plugins/invalid
version: 0.1.0
kinds:
  - provider
provider:
  operations:
    - name: list_items
      method: GET
      path: /items
`), 0644)

	out, err := runPluginReleaseCommandResult(pluginDir, "--version", "0.0.1-test")
	if err == nil {
		t.Fatal("expected invalid manifest error")
	}
	if !strings.Contains(string(out), "base_url") {
		t.Fatalf("unexpected output: %s", out)
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

	manifestData, err := os.ReadFile(filepath.Join(extractDir, pluginpkg.ManifestFile))
	if err != nil {
		t.Fatalf("read plugin.json: %v", err)
	}
	var manifest pluginmanifestv1.Manifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
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
	digest, err := fileSHA256(binaryPath)
	if err != nil {
		t.Fatalf("hash binary: %v", err)
	}
	if digest != artifact.SHA256 {
		t.Fatalf("binary sha256 = %s, manifest says %s", digest, artifact.SHA256)
	}

	checksumPath := filepath.Join(outputDir, "gestalt-plugin-bigquery_v"+testVersion+"_checksums.txt")
	checksumData, err := os.ReadFile(checksumPath)
	if err != nil {
		t.Fatalf("read checksums file: %v", err)
	}
	if !strings.Contains(string(checksumData), archiveName) {
		t.Fatalf("checksums file does not reference %s: %s", archiveName, checksumData)
	}

	iconPath := filepath.Join(extractDir, "assets", "icon.svg")
	if _, err := os.Stat(iconPath); err != nil {
		t.Fatalf("expected assets/icon.svg in archive: %v", err)
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
		"schemas/config.schema.json",
		"gestalt-plugin-release-test",
	} {
		if _, err := os.Stat(filepath.Join(extractDir, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("expected %s in archive: %v", rel, err)
		}
	}
}

func TestRun_PluginReleasePreservesCompiledWebUIMetadata(t *testing.T) {
	t.Parallel()

	pluginDir := newCompiledWebUIReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	testVersion := "0.0.21-test"

	runPluginReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", runtime.GOOS+"/"+runtime.GOARCH,
		"--output", outputDir,
	)

	archiveName := "gestalt-plugin-compiled-webui-test_v" + testVersion + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	extractDir := extractReleasedArchive(t, outputDir, archiveName)

	manifestData, err := os.ReadFile(filepath.Join(extractDir, pluginpkg.ManifestFile))
	if err != nil {
		t.Fatalf("read plugin.json: %v", err)
	}

	var manifest pluginmanifestv1.Manifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if manifest.WebUI == nil || manifest.WebUI.AssetRoot != "out" {
		t.Fatalf("manifest webui = %#v, want asset_root %q", manifest.WebUI, "out")
	}

	if _, err := os.Stat(filepath.Join(extractDir, "out", "index.html")); err != nil {
		t.Fatalf("expected webui asset in archive: %v", err)
	}
}

func TestEnvWithPWDPreservesWorkspaceEnv(t *testing.T) {
	t.Parallel()

	got := envWithPWD([]string{
		"PWD=/tmp/old",
		"GOWORK=/tmp/workspace/go.work",
		"GOMOD=/tmp/module/go.mod",
		"HOME=/tmp/home",
	}, "/tmp/new")

	for _, want := range []string{
		"PWD=/tmp/new",
		"GOWORK=/tmp/workspace/go.work",
		"GOMOD=/tmp/module/go.mod",
		"HOME=/tmp/home",
	} {
		if !containsEnvEntry(got, want) {
			t.Fatalf("envWithPWD() missing %q in %v", want, got)
		}
	}
	if containsEnvEntry(got, "PWD=/tmp/old") {
		t.Fatalf("envWithPWD() preserved old PWD: %v", got)
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

func TestRun_PluginReleaseTreatsGoModWithoutCmdAsDeclarative(t *testing.T) {
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

func TestRun_PluginReleaseTreatsDeclarativePluginWithHelperMainAsSource(t *testing.T) {
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

	firstChecksumPath := filepath.Join(outputDir, "gestalt-plugin-webui-test_v1.0.0_checksums.txt")
	firstChecksumData, err := os.ReadFile(firstChecksumPath)
	if err != nil {
		t.Fatalf("read first checksums file: %v", err)
	}
	if got := string(firstChecksumData); !strings.Contains(got, "gestalt-plugin-webui-test_v1.0.0.tar.gz") {
		t.Fatalf("first checksums file missing initial archive: %s", got)
	}

	secondChecksumPath := filepath.Join(outputDir, "gestalt-plugin-webui-test_v1.0.1_checksums.txt")
	secondChecksumData, err := os.ReadFile(secondChecksumPath)
	if err != nil {
		t.Fatalf("read second checksums file: %v", err)
	}
	if got := string(secondChecksumData); strings.Contains(got, "gestalt-plugin-webui-test_v1.0.0.tar.gz") {
		t.Fatalf("second checksums file unexpectedly included old archive: %s", got)
	} else if !strings.Contains(got, "gestalt-plugin-webui-test_v1.0.1.tar.gz") {
		t.Fatalf("second checksums file missing current archive: %s", got)
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

func TestRun_PluginReleasePackagesRootMainBuildTarget(t *testing.T) {
	t.Parallel()

	pluginDir := newRootMainReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.7-test"

	runPluginReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", runtime.GOOS+"/"+runtime.GOARCH,
		"--output", outputDir,
	)

	archiveName := "gestalt-plugin-" + releaseTestPluginName + "_v" + testVersion + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	manifest := readReleasedManifest(t, outputDir, archiveName)

	if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Path != releaseBinaryName(releaseTestPluginName, runtime.GOOS) {
		t.Fatalf("artifacts = %+v", manifest.Artifacts)
	}
}

func TestRun_PluginReleasePrefersCmdBuildTargetOverRootMain(t *testing.T) {
	t.Parallel()

	pluginDir := newCompiledReleaseFixture(t, t.TempDir())
	writeTestFile(t, pluginDir, "main.go", []byte("package main\n\nfunc main() { missingSymbol() }\n"), 0644)

	outputDir := t.TempDir()
	const testVersion = "0.0.7-rc.1"

	runPluginReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", runtime.GOOS+"/"+runtime.GOARCH,
		"--output", outputDir,
	)

	archiveName := "gestalt-plugin-" + releaseTestPluginName + "_v" + testVersion + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	manifest := readReleasedManifest(t, outputDir, archiveName)

	if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Path != releaseBinaryName(releaseTestPluginName, runtime.GOOS) {
		t.Fatalf("artifacts = %+v", manifest.Artifacts)
	}
}

func TestRun_PluginReleaseBuildsNestedCmdMainPackage(t *testing.T) {
	t.Parallel()

	pluginDir := newNestedCmdReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.7-alpha.1"

	runPluginReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", runtime.GOOS+"/"+runtime.GOARCH,
		"--output", outputDir,
	)

	archiveName := "gestalt-plugin-" + releaseTestPluginName + "_v" + testVersion + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	manifest := readReleasedManifest(t, outputDir, archiveName)

	if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Path != releaseBinaryName(releaseTestPluginName, runtime.GOOS) {
		t.Fatalf("artifacts = %+v", manifest.Artifacts)
	}
}

func TestRun_PluginReleaseDetectsPlatformTaggedMainPackage(t *testing.T) {
	t.Parallel()

	targetOS := "linux"
	switch runtime.GOOS {
	case "linux":
		targetOS = "darwin"
	case "darwin":
		targetOS = "linux"
	default:
		t.Skipf("platform-tagged release test expects darwin or linux host, got %s", runtime.GOOS)
	}

	pluginDir := newRootMainReleaseFixture(t, t.TempDir())
	writeTestFile(t, pluginDir, "main.go", []byte("//go:build "+targetOS+"\n\npackage main\n\nfunc main() {}\n"), 0644)

	outputDir := t.TempDir()
	const testVersion = "0.0.7-beta.1"

	runPluginReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", targetOS+"/amd64",
		"--output", outputDir,
	)

	archiveName := "gestalt-plugin-" + releaseTestPluginName + "_v" + testVersion + "_" + targetOS + "_amd64.tar.gz"
	manifest := readReleasedManifest(t, outputDir, archiveName)

	if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].OS != targetOS || manifest.Artifacts[0].Arch != "amd64" {
		t.Fatalf("artifacts = %+v", manifest.Artifacts)
	}
}

func TestRun_PluginReleaseRejectsStaleSourceArtifactDigest(t *testing.T) {
	t.Parallel()

	pluginDir := newPrebuiltHybridReleaseFixture(t, t.TempDir())

	_, manifest, err := pluginpkg.ReadManifestFile(filepath.Join(pluginDir, pluginpkg.ManifestFile))
	if err != nil {
		t.Fatalf("ReadManifestFile(plugin.json): %v", err)
	}
	manifest.Artifacts[0].SHA256 = sha256HexForTest("different-content")
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
	if manifest.Entrypoints.Runtime == nil || manifest.Entrypoints.Runtime.ArtifactPath != binaryName {
		t.Fatalf("runtime entrypoint = %+v, want artifact path %q", manifest.Entrypoints.Runtime, binaryName)
	}
	if _, err := os.Stat(filepath.Join(extractDir, binaryName)); err != nil {
		t.Fatalf("expected %s in archive: %v", binaryName, err)
	}
}

func TestRun_PluginReleaseCopiesDeclarativeProviderSupportFiles(t *testing.T) {
	t.Parallel()

	pluginDir := newDeclarativeProviderReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.10-test"

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

func newPluginPackageFixture(t *testing.T, dir string) string {
	t.Helper()

	src := filepath.Join(dir, "src")
	artifactDir := filepath.Join(src, "artifacts", runtime.GOOS, runtime.GOARCH)
	if err := os.MkdirAll(artifactDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(artifactDir, "provider"), []byte("provider"), 0755); err != nil {
		t.Fatalf("WriteFile(provider): %v", err)
	}
	if err := os.MkdirAll(filepath.Join(src, "schemas"), 0755); err != nil {
		t.Fatalf("MkdirAll(schemas): %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "schemas", "config.schema.json"), []byte(`{"type":"object"}`), 0644); err != nil {
		t.Fatalf("WriteFile(schema): %v", err)
	}

	manifest := `{
  "source": "github.com/testowner/plugins/provider",
  "version": "0.1.0",
  "kinds": ["provider"],
  "provider": {
    "config_schema_path": "schemas/config.schema.json"
  },
  "artifacts": [
    {
      "os": "` + runtime.GOOS + `",
      "arch": "` + runtime.GOARCH + `",
      "path": "artifacts/` + runtime.GOOS + `/` + runtime.GOARCH + `/provider",
      "sha256": "` + sha256HexForTest("provider") + `"
    }
  ],
  "entrypoints": {
    "provider": {
      "artifact_path": "artifacts/` + runtime.GOOS + `/` + runtime.GOARCH + `/provider"
    }
  }
}`
	if err := os.WriteFile(filepath.Join(src, "plugin.json"), []byte(manifest), 0644); err != nil {
		t.Fatalf("WriteFile(plugin.json): %v", err)
	}
	return src
}

func newCompiledReleaseFixture(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := filepath.Join(dir, releaseTestPluginName)
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("MkdirAll(pluginDir): %v", err)
	}
	writeTestFile(t, pluginDir, "go.mod", []byte("module "+releaseTestModule+"\n\ngo 1.22\n"), 0644)
	writeTestFile(t, pluginDir, "cmd/main.go", []byte("package main\n\nfunc main() {}\n"), 0644)
	writeReleaseTestManifest(t, pluginDir, &pluginmanifestv1.Manifest{
		Source:      releaseTestSource,
		Version:     "0.0.1",
		DisplayName: "Release Test",
		IconFile:    releaseTestIconPath,
		Kinds:       []string{pluginmanifestv1.KindProvider, pluginmanifestv1.KindRuntime},
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
				OS:     runtime.GOOS,
				Arch:   runtime.GOARCH,
				Path:   releaseSourceArtifactPath,
				SHA256: sha256HexForTest("source-plugin"),
			},
		},
		Entrypoints: pluginmanifestv1.Entrypoints{
			Provider: &pluginmanifestv1.Entrypoint{ArtifactPath: releaseSourceArtifactPath},
			Runtime:  &pluginmanifestv1.Entrypoint{ArtifactPath: releaseSourceArtifactPath},
		},
	})
	writeTestFile(t, pluginDir, releaseTestIconPath, []byte("<svg></svg>\n"), 0644)
	writeTestFile(t, pluginDir, releaseProviderSchemaPath, []byte(`{"type":"object"}`), 0644)
	writeTestFile(t, pluginDir, "schemas/config.schema.json", []byte(`{"type":"object"}`), 0644)
	return pluginDir
}

func newRootMainReleaseFixture(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := newCompiledReleaseFixture(t, dir)
	if err := os.RemoveAll(filepath.Join(pluginDir, "cmd")); err != nil {
		t.Fatalf("RemoveAll(cmd): %v", err)
	}
	writeTestFile(t, pluginDir, "main.go", []byte("package main\n\nfunc main() {}\n"), 0644)
	return pluginDir
}

func newNestedCmdReleaseFixture(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := newCompiledReleaseFixture(t, dir)
	if err := os.Remove(filepath.Join(pluginDir, "cmd", "main.go")); err != nil {
		t.Fatalf("Remove(cmd/main.go): %v", err)
	}
	writeTestFile(t, pluginDir, filepath.ToSlash(filepath.Join("cmd", releaseTestPluginName, "main.go")), []byte("package main\n\nfunc main() {}\n"), 0644)
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
				OS:     runtime.GOOS,
				Arch:   runtime.GOARCH,
				Path:   releaseSourceArtifactPath,
				SHA256: sha256HexForTest("source-plugin"),
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

func newCompiledWebUIReleaseFixture(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := filepath.Join(dir, "compiled-webui-test")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("MkdirAll(pluginDir): %v", err)
	}
	writeTestFile(t, pluginDir, "go.mod", []byte("module example.com/compiled-webui-test\n\ngo 1.22\n"), 0644)
	writeTestFile(t, pluginDir, "cmd/main.go", []byte("package main\n\nfunc main() {}\n"), 0644)
	writeReleaseTestManifest(t, pluginDir, &pluginmanifestv1.Manifest{
		Source:      "github.com/testowner/plugins/compiled-webui-test",
		Version:     "0.0.1",
		DisplayName: "Compiled WebUI Test",
		IconFile:    "branding/icon.svg",
		Kinds:       []string{pluginmanifestv1.KindWebUI},
		WebUI: &pluginmanifestv1.WebUIMetadata{
			AssetRoot: "out",
		},
	})
	writeTestFile(t, pluginDir, "branding/icon.svg", []byte("<svg></svg>\n"), 0644)
	writeTestFile(t, pluginDir, "out/index.html", []byte("<html></html>\n"), 0644)
	return pluginDir
}

func newPrebuiltHybridReleaseFixture(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := filepath.Join(dir, prebuiltHybridPluginName)
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("MkdirAll(pluginDir): %v", err)
	}
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
				OS:     runtime.GOOS,
				Arch:   runtime.GOARCH,
				Path:   prebuiltHybridArtifactPath,
				SHA256: sha256HexForTest("prebuilt-provider"),
			},
		},
		Entrypoints: pluginmanifestv1.Entrypoints{
			Provider: &pluginmanifestv1.Entrypoint{
				ArtifactPath: prebuiltHybridArtifactPath,
				Args:         []string{releaseHybridArg},
			},
		},
	})
	writeTestFile(t, pluginDir, releaseTestIconPath, []byte("<svg></svg>\n"), 0644)
	writeTestFile(t, pluginDir, prebuiltHybridArtifactPath, []byte("prebuilt-provider"), 0755)
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

func containsEnvEntry(env []string, want string) bool {
	for _, entry := range env {
		if entry == want {
			return true
		}
	}
	return false
}

func runPluginReleaseCommandResult(pluginDir string, args ...string) ([]byte, error) {
	cmdArgs := append([]string{"plugin", "release"}, args...)
	cmd := exec.Command(gestaltdBin, cmdArgs...)
	cmd.Dir = pluginDir
	return cmd.CombinedOutput()
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
	_, manifest, err := pluginpkg.ReadManifestFile(filepath.Join(extractDir, pluginpkg.ManifestFile))
	if err != nil {
		t.Fatalf("read released manifest: %v", err)
	}
	return manifest
}

func writeReleaseTestManifest(t *testing.T, dir string, manifest *pluginmanifestv1.Manifest) {
	t.Helper()

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent(plugin.json): %v", err)
	}
	writeTestFile(t, dir, "plugin.json", append(data, '\n'), 0644)
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

func captureStdout(t *testing.T, fn func() error) string {
	t.Helper()

	stdoutMu.Lock()
	defer stdoutMu.Unlock()

	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w

	runErr := fn()

	_ = w.Close()
	os.Stdout = orig

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("copy stdout: %v", err)
	}
	_ = r.Close()

	if runErr != nil {
		t.Fatalf("run: %v", runErr)
	}
	return buf.String()
}

func sha256HexForTest(value string) string {
	return sha256HexForPrepareTest(value)
}
