package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/pluginpkg"
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
	for _, removed := range []string{"install", "inspect", "list", "init"} {
		if strings.Contains(string(out), removed) {
			t.Fatalf("expected %q absent from help, got: %s", removed, out)
		}
	}
}

func TestRun_PluginPackageHelpExitsCleanly(t *testing.T) {
	t.Parallel()

	out, err := runPluginCommandResult("", "package", "--help")
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

func TestRun_PluginPackageCreatesArchive(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src := newPluginPackageFixture(t, dir)
	outPath := filepath.Join(dir, "testowner-provider-0.1.0.tar.gz")

	output, err := runPluginCommandResult("", "package", "--input", src, "--output", outPath)
	if err != nil {
		t.Fatalf("plugin package failed: %v\n%s", err, output)
	}

	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("expected package archive to exist: %v", err)
	}
	if !strings.Contains(string(output), "packaged") {
		t.Fatalf("expected package output, got: %q", output)
	}
}

func TestRun_PluginPackageCreatesDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src := newPluginPackageFixture(t, dir)
	outPath := filepath.Join(dir, "output-dir")

	output, err := runPluginCommandResult("", "package", "--input", src, "--output", outPath)
	if err != nil {
		t.Fatalf("plugin package failed: %v\n%s", err, output)
	}

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
	if _, err := os.Stat(filepath.Join(outPath, "openapi.yaml")); err != nil {
		t.Fatalf("expected support file in output directory: %v", err)
	}
	artifactPath := filepath.Join(outPath, "artifacts", runtime.GOOS, runtime.GOARCH, "provider")
	data, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatalf("expected artifact in output directory: %v", err)
	}
	if string(data) != "provider" {
		t.Fatalf("unexpected artifact content: %q", data)
	}
	if !strings.Contains(string(output), "packaged") {
		t.Fatalf("expected packaged output, got: %q", output)
	}
}

func TestRun_PluginPackageAcceptsYAMLManifestInput(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src := newPluginPackageFixture(t, dir)
	manifestPath, manifest := readManifestFromDir(t, src)
	if err := os.Remove(manifestPath); err != nil {
		t.Fatalf("remove %s: %v", manifestPath, err)
	}
	writeReleaseTestManifestFormat(t, src, "plugin.yaml", manifest)

	inputPath := filepath.Join(src, "plugin.yaml")
	outPath := filepath.Join(dir, "output-dir")

	output, err := runPluginCommandResult("", "package", "--input", inputPath, "--output", outPath)
	if err != nil {
		t.Fatalf("plugin package failed: %v\n%s", err, output)
	}

	packagedManifestPath, _ := readManifestFromDir(t, outPath)
	if filepath.Base(packagedManifestPath) != "plugin.yaml" {
		t.Fatalf("packaged manifest = %q, want plugin.yaml", filepath.Base(packagedManifestPath))
	}
	if !strings.Contains(string(output), "packaged") {
		t.Fatalf("expected packaged output, got: %q", output)
	}
}

func TestRun_PluginPackageRejectsOutputInsideInput(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src := newPluginPackageFixture(t, dir)
	outPath := filepath.Join(src, "dist")

	out, err := runPluginCommandResult("", "package", "--input", src, "--output", outPath)
	if err == nil {
		t.Fatal("expected error when output is inside input")
	}
	if !strings.Contains(string(out), "must not be inside source") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestRun_PluginPackageRejectsArchiveOutputInsideInput(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src := newPluginPackageFixture(t, dir)
	outPath := filepath.Join(src, "testowner-provider-0.1.0.tar.gz")

	out, err := runPluginCommandResult("", "package", "--input", src, "--output", outPath)
	if err == nil {
		t.Fatal("expected error when archive output is inside input")
	}
	if !strings.Contains(string(out), "must not be inside source") {
		t.Fatalf("unexpected output: %s", out)
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
	digest, err := fileSHA256(binaryPath)
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

	_, manifest := readManifestFromDir(t, extractDir)
	if manifest.WebUI == nil || manifest.WebUI.AssetRoot != "out" {
		t.Fatalf("manifest webui = %#v, want asset_root %q", manifest.WebUI, "out")
	}

	if _, err := os.Stat(filepath.Join(extractDir, "out", "index.html")); err != nil {
		t.Fatalf("expected webui asset in archive: %v", err)
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

	pluginDir := newNestedCmdReleaseFixture(t, t.TempDir())
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
	if err := os.WriteFile(filepath.Join(src, "openapi.yaml"), []byte("openapi: 3.0.0\ninfo:\n  title: Test\n  version: 1.0.0\npaths: {}\n"), 0644); err != nil {
		t.Fatalf("WriteFile(openapi.yaml): %v", err)
	}

	manifest := &pluginmanifestv1.Manifest{
		Source:  "github.com/testowner/plugins/provider",
		Version: "0.1.0",
		Kinds:   []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			ConfigSchemaPath: "schemas/config.schema.json",
		},
		Artifacts: []pluginmanifestv1.Artifact{
			{
				OS:     runtime.GOOS,
				Arch:   runtime.GOARCH,
				Path:   filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "provider")),
				SHA256: sha256HexForTest("provider"),
			},
		},
		Entrypoints: pluginmanifestv1.Entrypoints{
			Provider: &pluginmanifestv1.Entrypoint{
				ArtifactPath: filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "provider")),
			},
		},
	}
	manifestData, err := pluginpkg.EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("EncodeManifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "plugin.json"), manifestData, 0644); err != nil {
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
	return pluginpkg.EncodeManifestFormat(manifest, format)
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
