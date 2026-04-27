package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/session"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/operator"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
	"github.com/valon-technologies/gestalt/server/internal/providerpkg"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/internal/testutil/fakebun"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"google.golang.org/grpc"
	"gopkg.in/yaml.v3"
)

const (
	releaseTestPluginName          = "release-test"
	releaseTestSource              = "github.com/testowner/plugins/catalog/release-test"
	releaseTestModule              = "example.com/release-test"
	releaseTestIconPath            = "branding/icon.svg"
	releaseProviderSchemaPath      = "schemas/provider.schema.json"
	declarativeReleasePluginName   = "declarative-release"
	declarativeReleaseSource       = "github.com/testowner/plugins/catalog/declarative-release"
	uiTestPluginName               = "ui-test"
	uiTestSource                   = "github.com/testowner/plugins/catalog/ui-test"
	uiTestAssetRoot                = "out"
	prebuiltProviderPluginName     = "prebuilt-provider"
	prebuiltProviderSource         = "github.com/testowner/plugins/prebuilt-provider"
	prebuiltProviderBinaryPath     = "bin/provider"
	authReleasePluginName          = "auth-release"
	authReleaseSource              = "github.com/testowner/plugins/auth-release"
	authReleaseSchemaPath          = "schemas/auth.schema.json"
	authorizationReleasePluginName = "authorization-release"
	authorizationReleaseSource     = "github.com/testowner/plugins/authorization-release"
	authorizationReleaseSchemaPath = "schemas/authorization.schema.json"
	secretsReleasePluginName       = "secrets-release"
	secretsReleaseSource           = "github.com/testowner/plugins/secrets-release"
	secretsReleaseSchemaPath       = "schemas/secrets.schema.json"
	rustReleasePluginName          = "provider-rust"
	rustWrapperBinaryName          = "gestalt-provider-wrapper"
	pythonAuthReleasePluginName    = "python-auth-release"
	pythonAuthReleaseSource        = "github.com/testowner/plugins/python-auth-release"
	typeScriptReleasePluginName    = "ts-release"
	typeScriptReleaseSource        = "github.com/testowner/plugins/ts-release"
	typeScriptReleaseModule        = "./provider.ts#provider"
	typeScriptReleaseTarget        = "plugin:./provider.ts#provider"
	authReleaseTypeScriptModule    = "./auth.ts#auth"
	authReleaseTypeScriptTarget    = "authentication:./auth.ts#auth"
)

func TestRun_ProviderCLIUsageAndErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		args      []string
		wantErr   bool
		wantParts []string
		notWant   []string
	}{
		{
			name:      "root help",
			args:      []string{"--help"},
			wantParts: []string{"gestaltd provider <command> [flags]", "release"},
			notWant:   []string{"\n  install", "\n  inspect", "\n  list", "\n  init", "\n  package"},
		},
		{
			name:      "release help",
			args:      []string{"release", "--help"},
			wantParts: []string{"--version"},
		},
		{
			name:      "root defaults to help",
			args:      nil,
			wantParts: []string{"gestaltd provider <command> [flags]"},
		},
		{
			name:      "unknown subcommand",
			args:      []string{"bogus"},
			wantErr:   true,
			wantParts: []string{"unknown provider command", "bogus"},
		},
		{
			name:      "removed package subcommand",
			args:      []string{"package"},
			wantErr:   true,
			wantParts: []string{"unknown provider command", "package"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			out, err := runProviderCommandResult("", tc.args...)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for provider %v, got output: %s", tc.args, out)
				}
			} else if err != nil {
				t.Fatalf("expected success for provider %v, got error: %v\noutput: %s", tc.args, err, out)
			}
			for _, want := range tc.wantParts {
				if !strings.Contains(string(out), want) {
					t.Fatalf("expected output to contain %q, got: %s", want, out)
				}
			}
			for _, notWant := range tc.notWant {
				if strings.Contains(string(out), notWant) {
					t.Fatalf("expected %q absent from output, got: %s", notWant, out)
				}
			}
		})
	}
}

func TestRun_ProviderReleaseRequiresVersion(t *testing.T) {
	t.Parallel()

	pluginDir := newSourceProviderReleaseFixture(t, t.TempDir())
	out, err := runProviderCommandResult(pluginDir, "release")
	if err == nil {
		t.Fatal("expected error when --version missing")
	}
	if !strings.Contains(string(out), "--version is required") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestRun_ProviderReleaseRejectsInvalidManifest(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		manifestYAML string
		wantError    string
	}{
		{
			name: "rest surface requires baseUrl",
			manifestYAML: `
kind: plugin
source: github.com/testowner/plugins/invalid
version: 0.0.1-alpha.1
spec:
  surfaces:
    rest:
      operations:
        - name: list_items
          method: GET
          path: /items
`,
			wantError: "provider.baseUrl is required",
		},
		{
			name: "exec block requires artifact path",
			manifestYAML: `
kind: plugin
source: github.com/testowner/plugins/invalid
version: 0.0.1-alpha.1
spec: {}
entrypoint:
  artifactPath: ""
`,
			wantError: "entrypoint.artifactPath is required",
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
			writeTestFile(t, pluginDir, "manifest.yaml", []byte(tc.manifestYAML), 0644)

			out, err := runProviderReleaseCommandResult(pluginDir, "--version", "0.0.1-test")
			if err == nil {
				t.Fatal("expected invalid manifest error")
			}
			if !strings.Contains(string(out), tc.wantError) {
				t.Fatalf("unexpected output: %s", out)
			}
		})
	}
}

func TestE2EProviderReleaseBigquery(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Join("..", "..", "..")
	bigqueryDir := filepath.Join(repoRoot, "plugins", "bigquery")
	if _, err := os.Stat(filepath.Join(bigqueryDir, "go.mod")); err != nil {
		t.Skipf("bigquery plugin not found: %v", err)
	}

	outputDir := t.TempDir()
	const testVersion = "0.0.1-test"
	const testPlatform = "linux/amd64"

	cmd := exec.Command(gestaltdBin, "provider", "release",
		"--version", testVersion,
		"--platform", testPlatform,
		"--output", outputDir,
	)
	cmd.Dir = bigqueryDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("provider release failed: %v\n%s", err, out)
	}

	archiveName := "gestalt-plugin-bigquery_v" + testVersion + "_linux_amd64.tar.gz"
	archivePath := filepath.Join(outputDir, archiveName)
	if _, err := os.Stat(archivePath); err != nil {
		t.Fatalf("expected archive %s to exist: %v", archiveName, err)
	}

	extractDir := filepath.Join(outputDir, "extracted")
	if err := providerpkg.ExtractPackage(archivePath, extractDir); err != nil {
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
	digest, err := providerpkg.FileSHA256(binaryPath)
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

func TestRun_ProviderReleaseBuildsPythonSourcePluginForCurrentPlatform(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake Python build fixture is POSIX-only")
	}

	t.Setenv("GESTALT_TEST_PYINSTALLER_BINARY", pluginBin)
	t.Setenv("PATH", pathWithoutGo(t))

	pluginDir := newPythonSourceReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.12-test"

	runProviderReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)
	if _, err := os.Stat(filepath.Join(pluginDir, providerpkg.StaticCatalogFile)); !os.IsNotExist(err) {
		t.Fatalf("source catalog should not persist after release, got err=%v", err)
	}

	archiveName := expectedPythonArchiveName(testVersion, runtime.GOOS, runtime.GOARCH)
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifest := readReleasedManifest(t, outputDir, archiveName)
	assertReleasedManifestHasHostedHTTPMetadata(t, manifest, "greet")

	binaryName := releaseBinaryName("python-release", runtime.GOOS)
	if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Path != binaryName {
		t.Fatalf("artifacts = %+v, want path %q", manifest.Artifacts, binaryName)
	}
	if manifest.Entrypoint == nil || manifest.Entrypoint.ArtifactPath != binaryName {
		t.Fatalf("provider entrypoint = %+v, want artifact path %q", manifest.Entrypoint, binaryName)
	}

	artifactPath := filepath.Join(extractDir, binaryName)
	if _, err := os.Stat(artifactPath); err != nil {
		t.Fatalf("expected %s in archive: %v", binaryName, err)
	}

	ctx := context.Background()
	prov, err := providerhost.NewExecutableProvider(ctx, providerhost.ExecConfig{
		Command: artifactPath,
		StaticSpec: providerhost.StaticProviderSpec{
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

func TestRun_ProviderReleaseWritesExecutableMetadataForManifestBackedPythonSourcePlugin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake Python build fixture is POSIX-only")
	}

	t.Setenv("GESTALT_TEST_PYINSTALLER_BINARY", pluginBin)
	t.Setenv("PATH", pathWithoutGo(t))

	pluginDir := newManifestBackedPythonSourceReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.12-manifest.1"

	runProviderReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", runtime.GOOS+"/"+runtime.GOARCH,
		"--output", outputDir,
	)

	archiveName := expectedPythonArchiveName(testVersion, runtime.GOOS, runtime.GOARCH)
	manifest := readReleasedManifest(t, outputDir, archiveName)
	assertReleasedManifestHasHostedHTTPMetadata(t, manifest, "greet")
	if manifest.Entrypoint == nil {
		t.Fatal("expected provider entrypoint")
	}

	metadata := readProviderReleaseMetadata(t, outputDir)
	if metadata.Runtime != providerReleaseRuntimeKindExecutable {
		t.Fatalf("release metadata runtime = %q, want %q", metadata.Runtime, providerReleaseRuntimeKindExecutable)
	}
	if _, ok := metadata.Artifacts[providerpkg.CurrentPlatformString()]; !ok {
		t.Fatalf("release metadata artifacts missing current platform key %q: %+v", providerpkg.CurrentPlatformString(), metadata.Artifacts)
	}
}

func TestRun_ProviderReleaseBuildsTypeScriptSourcePluginForCurrentPlatform(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake Bun build fixture is POSIX-only")
	}

	pluginDir := newTypeScriptSourceReleaseFixture(t, t.TempDir())
	sdkDir := writeFakeTypeScriptReleaseSDKDir(t, t.TempDir())
	t.Setenv("GESTALT_TYPESCRIPT_SDK_DIR", sdkDir)
	bunPath := writeFakeTypeScriptProviderReleaseBun(
		t,
		typeScriptReleasePluginName,
		typeScriptReleaseTarget,
		runtime.GOOS,
		runtime.GOARCH,
	)
	t.Setenv("PATH", pathWithoutGo(t))
	t.Setenv("GESTALT_BUN", bunPath)

	outputDir := t.TempDir()
	const testVersion = "0.0.12-ts"

	runProviderReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)
	if _, err := os.Stat(filepath.Join(pluginDir, providerpkg.StaticCatalogFile)); !os.IsNotExist(err) {
		t.Fatalf("source catalog should not persist after release, got err=%v", err)
	}

	archiveName := platformArchiveNameForTest(typeScriptReleasePluginName, testVersion, runtime.GOOS, runtime.GOARCH)
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifest := readReleasedManifest(t, outputDir, archiveName)
	assertReleasedManifestHasHostedHTTPMetadata(t, manifest, "greet")

	binaryName := releaseBinaryName(typeScriptReleasePluginName, runtime.GOOS)
	if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Path != binaryName {
		t.Fatalf("artifacts = %+v, want path %q", manifest.Artifacts, binaryName)
	}
	if manifest.Entrypoint == nil || manifest.Entrypoint.ArtifactPath != binaryName {
		t.Fatalf("provider entrypoint = %+v, want artifact path %q", manifest.Entrypoint, binaryName)
	}
	catalogData, err := os.ReadFile(filepath.Join(extractDir, providerpkg.StaticCatalogFile))
	if err != nil {
		t.Fatalf("read generated catalog: %v", err)
	}
	if !strings.Contains(string(catalogData), "id: greet") {
		t.Fatalf("unexpected generated catalog: %s", catalogData)
	}
}

func TestRun_ProviderReleaseDefaultsSourcePluginToHostPlatform(t *testing.T) {
	t.Run("go", func(t *testing.T) {
		t.Parallel()

		pluginDir := newGoSourceReleaseFixture(t, t.TempDir())
		outputDir := t.TempDir()
		const testVersion = "0.0.12-go-default"

		runProviderReleaseCommand(t, pluginDir,
			"--version", testVersion,
			"--output", outputDir,
		)

		archiveName := "gestalt-plugin-release-test_v" + testVersion + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
		manifest := readReleasedManifest(t, outputDir, archiveName)
		assertReleaseDefaultsToHostPlatform(t, manifest, func(t *testing.T, artifact providermanifestv1.Artifact) {
			assertExpectedGoArtifactPlatform(t, artifact, runtime.GOOS, runtime.GOARCH, "")
		})
		assertReleasedManifestHasHostedHTTPMetadata(t, manifest, "echo")
	})

	t.Run("python", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("fake Python build fixture is POSIX-only")
		}

		t.Setenv("GESTALT_TEST_PYINSTALLER_BINARY", pluginBin)
		t.Setenv("PATH", pathWithoutGo(t))

		pluginDir := newPythonSourceReleaseFixture(t, t.TempDir())
		outputDir := t.TempDir()
		const testVersion = "0.0.12-default"

		runProviderReleaseCommand(t, pluginDir,
			"--version", testVersion,
			"--output", outputDir,
		)

		archiveName := expectedPythonArchiveName(testVersion, runtime.GOOS, runtime.GOARCH)
		assertReleaseDefaultsToHostPlatform(t, readReleasedManifest(t, outputDir, archiveName), func(t *testing.T, artifact providermanifestv1.Artifact) {
			assertExpectedScriptArtifactPlatform(t, artifact, runtime.GOOS, runtime.GOARCH)
		})
	})
}

func TestRun_ProviderReleaseBuildsRequestedPlatformSets(t *testing.T) {
	t.Run("go all", func(t *testing.T) {
		t.Parallel()

		pluginDir := newGoSourceReleaseFixture(t, t.TempDir())
		outputDir := t.TempDir()
		const testVersion = "0.0.12-go-all"

		runProviderReleaseCommand(t, pluginDir,
			"--version", testVersion,
			"--platform", allPlatformsValue,
			"--output", outputDir,
		)

		assertReleasePlatforms(t, outputDir, defaultReleasePlatformsForTest(t), func(platform releasePlatform) string {
			return "gestalt-plugin-release-test_v" + testVersion + "_" + platform.GOOS + "_" + platform.GOARCH + ".tar.gz"
		}, func(t *testing.T, artifact providermanifestv1.Artifact, platform releasePlatform) {
			assertExpectedGoArtifactPlatform(t, artifact, platform.GOOS, platform.GOARCH, "")
		})
	})

	t.Run("python all", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("fake Python build fixture is POSIX-only")
		}

		t.Setenv("GESTALT_TEST_PYINSTALLER_BINARY", pluginBin)
		t.Setenv("PATH", pathWithoutGo(t))

		pluginDir := newPythonSourceReleaseFixture(t, t.TempDir())
		configurePythonReleaseInterpretersForAllPlatforms(t, pluginDir)

		outputDir := t.TempDir()
		const testVersion = "0.0.12-python-all"

		runProviderReleaseCommand(t, pluginDir,
			"--version", testVersion,
			"--platform", allPlatformsValue,
			"--output", outputDir,
		)

		assertReleasePlatforms(t, outputDir, defaultReleasePlatformsForTest(t), func(platform releasePlatform) string {
			return expectedPythonArchiveName(testVersion, platform.GOOS, platform.GOARCH)
		}, func(t *testing.T, artifact providermanifestv1.Artifact, platform releasePlatform) {
			assertExpectedScriptArtifactPlatform(t, artifact, platform.GOOS, platform.GOARCH)
		})
	})

	t.Run("python subset", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("fake Python build fixture is POSIX-only")
		}

		t.Setenv("GESTALT_TEST_PYINSTALLER_BINARY", pluginBin)
		t.Setenv("PATH", pathWithoutGo(t))

		pluginDir := newPythonSourceReleaseFixture(t, t.TempDir())
		outputDir := t.TempDir()
		otherGOOS, otherGOARCH := pythonReleaseOtherPlatform()
		otherPlatform := otherGOOS + "/" + otherGOARCH
		crossPythonPath := filepath.Join(pluginDir, "cross-python")
		writeFakePythonReleaseInterpreter(t, crossPythonPath, otherGOOS, otherGOARCH)
		t.Setenv(providerpkgPythonEnvVar(otherGOOS, otherGOARCH), crossPythonPath)

		runProviderReleaseCommand(t, pluginDir,
			"--version", "0.0.13-test",
			"--platform", runtime.GOOS+"/"+runtime.GOARCH+","+otherPlatform,
			"--output", outputDir,
		)

		assertReleasePlatforms(t, outputDir, []releasePlatform{
			{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH},
			{GOOS: otherGOOS, GOARCH: otherGOARCH},
		}, func(platform releasePlatform) string {
			return expectedPythonArchiveName("0.0.13-test", platform.GOOS, platform.GOARCH)
		}, func(t *testing.T, artifact providermanifestv1.Artifact, platform releasePlatform) {
			assertExpectedScriptArtifactPlatform(t, artifact, platform.GOOS, platform.GOARCH)
			extractDir := extractReleasedArchive(t, outputDir, expectedPythonArchiveName("0.0.13-test", platform.GOOS, platform.GOARCH))
			binaryName := releaseBinaryName("python-release", artifact.OS)
			if artifact.Path != binaryName {
				t.Fatalf("artifacts = %+v, want path %q", artifact, binaryName)
			}
			if _, err := os.Stat(filepath.Join(extractDir, binaryName)); err != nil {
				t.Fatalf("expected %s in archive: %v", binaryName, err)
			}
		})
	})

	t.Run("typescript auth all", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("fake Bun build fixture is POSIX-only")
		}

		builtBinary := buildGoSourceAuthBinary(t)
		pluginDir := newTypeScriptSourceAuthReleaseFixture(t, t.TempDir())
		sdkDir := writeFakeTypeScriptReleaseSDKDir(t, t.TempDir())
		t.Setenv("GESTALT_TYPESCRIPT_SDK_DIR", sdkDir)
		defaultPlatforms := defaultReleasePlatformsForTest(t)
		bunPath := writeFakeTypeScriptComponentReleaseBunForPlatforms(
			t,
			authReleaseTypeScriptTarget,
			authReleasePluginName,
			defaultPlatforms,
			builtBinary,
		)
		t.Setenv("PATH", pathWithoutGo(t))
		t.Setenv("GESTALT_BUN", bunPath)

		outputDir := t.TempDir()
		const testVersion = "0.0.12-ts-auth-all"

		runProviderReleaseCommand(t, pluginDir,
			"--version", testVersion,
			"--platform", allPlatformsValue,
			"--output", outputDir,
		)

		assertReleasePlatforms(t, outputDir, defaultPlatforms, func(platform releasePlatform) string {
			return platformArchiveNameForTest(authReleasePluginName, testVersion, platform.GOOS, platform.GOARCH)
		}, func(t *testing.T, artifact providermanifestv1.Artifact, platform releasePlatform) {
			assertExpectedScriptArtifactPlatform(t, artifact, platform.GOOS, platform.GOARCH)
			if artifact.Path != releaseBinaryName(authReleasePluginName, artifact.OS) {
				t.Fatalf("artifacts = %+v, want path %q", artifact, releaseBinaryName(authReleasePluginName, artifact.OS))
			}
		})
	})
}

func TestRun_ProviderReleaseBuildsRustSourcePluginForCurrentPlatform(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake cargo test fixture is POSIX-only")
	}

	hostTarget, _, err := providerpkgRustTargetTriple(runtime.GOOS, runtime.GOARCH, "")
	if err != nil {
		t.Fatalf("providerpkgRustTargetTriple(host): %v", err)
	}

	fakeCargoDir := t.TempDir()
	writeFakeRustReleaseCargo(t, filepath.Join(fakeCargoDir, "cargo"), fakeRustCargoConfig{
		ExpectedPluginName:   rustReleasePluginName,
		ExpectedServeExport:  "__gestalt_serve",
		ExpectedCatalogWrite: true,
		GeneratedCatalog:     rustReleasePluginName,
		GeneratedManifestMetadata: `securitySchemes:
  signed:
    type: hmac
    secret:
      env: REQUEST_SIGNING_SECRET
    signatureHeader: X-Request-Signature
    signaturePrefix: v0=
    payloadTemplate: "v0:{header:X-Request-Timestamp}:{raw_body}"
    timestampHeader: X-Request-Timestamp
    maxAgeSeconds: 300
http:
  command:
    path: /command
    method: POST
    security: signed
    target: greet
    requestBody:
      required: true
      content:
        application/x-www-form-urlencoded: {}
    ack:
      status: 200
      body:
        status: accepted`,
		DelegateBinary: pluginBin,
		AllowedTargets: []string{hostTarget},
	})
	t.Setenv("PATH", fakeCargoDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	pluginDir := newRustSourceReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.12-rust-current"

	runProviderReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", runtime.GOOS+"/"+runtime.GOARCH,
		"--output", outputDir,
	)

	archiveName := expectedRustArchiveName(testVersion, runtime.GOOS, runtime.GOARCH, "")
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifest := readReleasedManifest(t, outputDir, archiveName)
	assertReleasedManifestHasHostedHTTPMetadata(t, manifest, "greet")
	binaryName := releaseBinaryName(rustReleasePluginName, runtime.GOOS)

	if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Path != binaryName {
		t.Fatalf("artifacts = %+v, want path %q", manifest.Artifacts, binaryName)
	}
	assertExpectedRustArtifactPlatform(t, manifest.Artifacts[0], runtime.GOOS, runtime.GOARCH, "")
	if manifest.Entrypoint == nil || manifest.Entrypoint.ArtifactPath != binaryName {
		t.Fatalf("provider entrypoint = %+v, want artifact path %q", manifest.Entrypoint, binaryName)
	}

	artifactPath := filepath.Join(extractDir, binaryName)
	if _, err := os.Stat(artifactPath); err != nil {
		t.Fatalf("expected %s in archive: %v", binaryName, err)
	}
	catalogPath := filepath.Join(extractDir, providerpkg.StaticCatalogFile)
	catalogData, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatalf("read generated catalog: %v", err)
	}
	if !strings.Contains(string(catalogData), "id: greet") {
		t.Fatalf("unexpected generated catalog: %s", catalogData)
	}

	ctx := context.Background()
	prov, err := providerhost.NewExecutableProvider(ctx, providerhost.ExecConfig{
		Command: artifactPath,
		StaticSpec: providerhost.StaticProviderSpec{
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

func TestRun_ProviderReleaseBuildsRustSourcePluginForExplicitLinuxTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake cargo test fixture is POSIX-only")
	}

	hostTarget, _, err := providerpkgRustTargetTriple(runtime.GOOS, runtime.GOARCH, "")
	if err != nil {
		t.Fatalf("providerpkgRustTargetTriple(host): %v", err)
	}
	explicitTarget, _, err := providerpkgRustTargetTriple("linux", "amd64", "musl")
	if err != nil {
		t.Fatalf("providerpkgRustTargetTriple(linux/amd64/musl): %v", err)
	}

	fakeCargoDir := t.TempDir()
	writeFakeRustReleaseCargo(t, filepath.Join(fakeCargoDir, "cargo"), fakeRustCargoConfig{
		ExpectedPluginName:   rustReleasePluginName,
		ExpectedServeExport:  "__gestalt_serve",
		ExpectedCatalogWrite: true,
		GeneratedCatalog:     rustReleasePluginName,
		GeneratedManifestMetadata: `securitySchemes:
  signed:
    type: hmac
    secret:
      env: REQUEST_SIGNING_SECRET
    signatureHeader: X-Request-Signature
    signaturePrefix: v0=
    payloadTemplate: "v0:{header:X-Request-Timestamp}:{raw_body}"
    timestampHeader: X-Request-Timestamp
    maxAgeSeconds: 300
http:
  command:
    path: /command
    method: POST
    security: signed
    target: greet
    requestBody:
      required: true
      content:
        application/x-www-form-urlencoded: {}
    ack:
      status: 200
      body:
        status: accepted`,
		DelegateBinary: pluginBin,
		AllowedTargets: []string{hostTarget, explicitTarget},
	})
	t.Setenv("PATH", fakeCargoDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	pluginDir := newRustSourceReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.12-rust-musl"

	runProviderReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", "linux/amd64/musl",
		"--output", outputDir,
	)

	archiveName := expectedRustArchiveName(testVersion, "linux", "amd64", "musl")
	manifest := readReleasedManifest(t, outputDir, archiveName)
	binaryName := releaseBinaryName(rustReleasePluginName, "linux")

	if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Path != binaryName {
		t.Fatalf("artifacts = %+v, want path %q", manifest.Artifacts, binaryName)
	}
	assertExpectedRustArtifactPlatform(t, manifest.Artifacts[0], "linux", "amd64", "musl")
}

func TestRun_ProviderReleaseRejectsMissingCrossTargetInterpreterForPythonSourcePlugin(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("fake Python build fixture is POSIX-only")
	}

	pluginDir := newPythonSourceReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	otherGOOS, otherGOARCH := pythonReleaseOtherPlatform()
	otherPlatform := otherGOOS + "/" + otherGOARCH

	out, err := runProviderReleaseCommandResult(pluginDir,
		"--version", "0.0.13-test",
		"--platform", otherPlatform,
		"--output", outputDir,
	)
	if err == nil {
		t.Fatalf("expected error for non-current platform, got output: %s", out)
	}
	if !strings.Contains(string(out), providerpkgPythonEnvVar(otherGOOS, otherGOARCH)) {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestRun_ProviderReleaseRejectsInvalidPythonProviderTarget(t *testing.T) {
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
	manifestData, err := providerpkg.EncodeSourceManifestFormat(&providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindPlugin,
		Source:  "github.com/testowner/plugins/invalid-python-release",
		Version: "0.0.1",
		Spec: &providermanifestv1.Spec{
			Connections: map[string]*providermanifestv1.ManifestConnectionDef{
				"default": {
					Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeNone},
				},
			},
		},
	}, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat: %v", err)
	}
	writeTestFile(t, pluginDir, "manifest.yaml", manifestData, 0o644)

	out, err := runProviderReleaseCommandResult(pluginDir, "--version", "0.0.14-test", "--output", t.TempDir())
	if err == nil {
		t.Fatalf("expected invalid target error, got output: %s", out)
	}
	if !strings.Contains(string(out), "module must be a dot-separated Python identifier path") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestRun_ProviderReleaseBuildsGoSourceAuthPlugin(t *testing.T) {
	t.Parallel()

	pluginDir := newSourceComponentReleaseFixture(t, t.TempDir(), sourceComponentReleaseFixtureParams{
		pluginName: authReleasePluginName,
		schemaPath: authReleaseSchemaPath,
		sourceFile: "auth.go",
		sourceCode: testutil.GeneratedAuthPackageSource(),
		manifest: &providermanifestv1.Manifest{
			Kind:   providermanifestv1.KindAuthentication,
			Source: authReleaseSource, Version: "0.0.1", DisplayName: "Auth Release",
			Spec: &providermanifestv1.Spec{ConfigSchemaPath: authReleaseSchemaPath},
		},
	})
	outputDir := t.TempDir()
	const testVersion = "0.0.15-test"

	runProviderReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", runtime.GOOS+"/"+runtime.GOARCH,
		"--output", outputDir,
	)

	archiveName := platformArchiveNameForTest(authReleasePluginName, testVersion, runtime.GOOS, runtime.GOARCH)
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifest := readReleasedManifest(t, outputDir, archiveName)
	binaryName := releaseBinaryName(authReleasePluginName, runtime.GOOS)

	if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Path != binaryName {
		t.Fatalf("artifacts = %+v, want path %q", manifest.Artifacts, binaryName)
	}
	assertExpectedGoArtifactPlatform(t, manifest.Artifacts[0], runtime.GOOS, runtime.GOARCH, "")
	if manifest.Entrypoint == nil || manifest.Entrypoint.ArtifactPath != binaryName {
		t.Fatalf("auth entrypoint = %+v, want artifact path %q", manifest.Entrypoint, binaryName)
	}
	if _, err := os.Stat(filepath.Join(extractDir, authReleaseSchemaPath)); err != nil {
		t.Fatalf("expected %s in archive: %v", authReleaseSchemaPath, err)
	}
	metadata := readProviderReleaseMetadata(t, outputDir)
	if metadata.Package != authReleaseSource {
		t.Fatalf("release metadata package = %q, want %q", metadata.Package, authReleaseSource)
	}
	if metadata.Kind != providermanifestv1.KindAuthentication {
		t.Fatalf("release metadata kind = %q, want %q", metadata.Kind, providermanifestv1.KindAuthentication)
	}
	if metadata.Runtime != providerReleaseRuntimeKindExecutable {
		t.Fatalf("release metadata runtime = %q, want %q", metadata.Runtime, providerReleaseRuntimeKindExecutable)
	}
	authArtifact, ok := metadata.Artifacts[providerpkg.CurrentPlatformString()]
	if !ok {
		t.Fatalf("release metadata artifacts missing current platform key %q: %+v", providerpkg.CurrentPlatformString(), metadata.Artifacts)
	}
	authDigest, err := providerpkg.ArchiveDigest(filepath.Join(outputDir, archiveName))
	if err != nil {
		t.Fatalf("hash auth archive: %v", err)
	}
	if authArtifact.Path != archiveName || authArtifact.SHA256 != authDigest {
		t.Fatalf("release metadata auth artifact = %+v, want path %q sha %q", authArtifact, archiveName, authDigest)
	}

	auth, err := providerhost.NewExecutableAuthenticationProvider(context.Background(), providerhost.AuthenticationExecConfig{
		Command:     filepath.Join(extractDir, binaryName),
		Name:        "auth-release",
		CallbackURL: "https://gestalt.example.test/api/v1/auth/login/callback",
		SessionKey:  []byte("0123456789abcdef0123456789abcdef"),
	})
	if err != nil {
		t.Fatalf("NewExecutableAuthenticationProvider: %v", err)
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

func TestRun_ProviderReleaseBuildsGoSourceAuthorizationProvider(t *testing.T) {
	t.Parallel()

	pluginDir := newSourceComponentReleaseFixture(t, t.TempDir(), sourceComponentReleaseFixtureParams{
		pluginName: authorizationReleasePluginName,
		schemaPath: authorizationReleaseSchemaPath,
		sourceFile: "authorization.go",
		sourceCode: testutil.GeneratedAuthorizationPackageSource(),
		manifest: &providermanifestv1.Manifest{
			Kind:   providermanifestv1.KindAuthorization,
			Source: authorizationReleaseSource, Version: "0.0.1", DisplayName: "Authorization Release",
			Spec: &providermanifestv1.Spec{ConfigSchemaPath: authorizationReleaseSchemaPath},
		},
	})
	outputDir := t.TempDir()
	const testVersion = "0.0.18-test"

	runProviderReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", runtime.GOOS+"/"+runtime.GOARCH,
		"--output", outputDir,
	)

	archiveName := platformArchiveNameForTest(authorizationReleasePluginName, testVersion, runtime.GOOS, runtime.GOARCH)
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifest := readReleasedManifest(t, outputDir, archiveName)
	binaryName := releaseBinaryName(authorizationReleasePluginName, runtime.GOOS)

	if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Path != binaryName {
		t.Fatalf("artifacts = %+v, want path %q", manifest.Artifacts, binaryName)
	}
	assertExpectedGoArtifactPlatform(t, manifest.Artifacts[0], runtime.GOOS, runtime.GOARCH, "")
	if manifest.Entrypoint == nil || manifest.Entrypoint.ArtifactPath != binaryName {
		t.Fatalf("authorization entrypoint = %+v, want artifact path %q", manifest.Entrypoint, binaryName)
	}
	if _, err := os.Stat(filepath.Join(extractDir, authorizationReleaseSchemaPath)); err != nil {
		t.Fatalf("expected %s in archive: %v", authorizationReleaseSchemaPath, err)
	}

	metadata := readProviderReleaseMetadata(t, outputDir)
	if metadata.Package != authorizationReleaseSource {
		t.Fatalf("release metadata package = %q, want %q", metadata.Package, authorizationReleaseSource)
	}
	if metadata.Kind != providermanifestv1.KindAuthorization {
		t.Fatalf("release metadata kind = %q, want %q", metadata.Kind, providermanifestv1.KindAuthorization)
	}
	if metadata.Runtime != providerReleaseRuntimeKindExecutable {
		t.Fatalf("release metadata runtime = %q, want %q", metadata.Runtime, providerReleaseRuntimeKindExecutable)
	}

	authz, err := providerhost.NewExecutableAuthorizationProvider(context.Background(), providerhost.AuthorizationExecConfig{
		Command: filepath.Join(extractDir, binaryName),
		Name:    "authorization-release",
	})
	if err != nil {
		t.Fatalf("NewExecutableAuthorizationProvider: %v", err)
	}
	defer func() {
		if closer, ok := authz.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}()

	decision, err := authz.Evaluate(context.Background(), &core.AccessEvaluationRequest{
		Subject:  &core.SubjectRef{Type: "user", Id: "generated-user"},
		Action:   &core.ActionRef{Name: "invoke"},
		Resource: &core.ResourceRef{Type: "plugin", Id: "github"},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if decision == nil || !decision.Allowed || decision.ModelId != "model-v1" {
		t.Fatalf("decision = %+v", decision)
	}

	providerMetadata, err := authz.GetMetadata(context.Background())
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	if providerMetadata == nil || providerMetadata.ActiveModelId != "model-v1" {
		t.Fatalf("metadata = %+v", providerMetadata)
	}

	activeModel, err := authz.GetActiveModel(context.Background())
	if err != nil {
		t.Fatalf("GetActiveModel: %v", err)
	}
	if activeModel == nil || activeModel.Model == nil || activeModel.Model.Id != "model-v1" {
		t.Fatalf("active model = %+v", activeModel)
	}

	relationships, err := authz.ReadRelationships(context.Background(), &core.ReadRelationshipsRequest{})
	if err != nil {
		t.Fatalf("ReadRelationships: %v", err)
	}
	if relationships == nil || len(relationships.Relationships) != 1 {
		t.Fatalf("relationships = %+v", relationships)
	}
}

func TestRun_ProviderReleaseBuildsGoSourceSecretsPlugin(t *testing.T) {
	t.Parallel()

	pluginDir := newSourceComponentReleaseFixture(t, t.TempDir(), sourceComponentReleaseFixtureParams{
		pluginName: secretsReleasePluginName,
		schemaPath: secretsReleaseSchemaPath,
		sourceFile: "secrets.go",
		sourceCode: testutil.GeneratedSecretsPackageSource(),
		manifest: &providermanifestv1.Manifest{
			Kind:   providermanifestv1.KindSecrets,
			Source: secretsReleaseSource, Version: "0.0.1", DisplayName: "Secrets Release",
			Spec: &providermanifestv1.Spec{ConfigSchemaPath: secretsReleaseSchemaPath},
		},
	})
	outputDir := t.TempDir()
	const testVersion = "0.0.19-test"

	runProviderReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", runtime.GOOS+"/"+runtime.GOARCH,
		"--output", outputDir,
	)

	archiveName := platformArchiveNameForTest(secretsReleasePluginName, testVersion, runtime.GOOS, runtime.GOARCH)
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifest := readReleasedManifest(t, outputDir, archiveName)
	binaryName := releaseBinaryName(secretsReleasePluginName, runtime.GOOS)

	if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Path != binaryName {
		t.Fatalf("artifacts = %+v, want path %q", manifest.Artifacts, binaryName)
	}
	assertExpectedGoArtifactPlatform(t, manifest.Artifacts[0], runtime.GOOS, runtime.GOARCH, "")
	if manifest.Entrypoint == nil || manifest.Entrypoint.ArtifactPath != binaryName {
		t.Fatalf("secrets entrypoint = %+v, want artifact path %q", manifest.Entrypoint, binaryName)
	}
	if _, err := os.Stat(filepath.Join(extractDir, secretsReleaseSchemaPath)); err != nil {
		t.Fatalf("expected %s in archive: %v", secretsReleaseSchemaPath, err)
	}

	sm, err := providerhost.NewExecutableSecretManager(context.Background(), providerhost.SecretsExecConfig{
		Command: filepath.Join(extractDir, binaryName),
		Name:    secretsReleasePluginName,
	})
	if err != nil {
		t.Fatalf("NewExecutableSecretManager: %v", err)
	}
	defer func() {
		if closer, ok := sm.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}()

	value, err := sm.GetSecret(context.Background(), "generated-secret")
	if err != nil {
		t.Fatalf("GetSecret(generated-secret): %v", err)
	}
	if value != "generated-secret-value" {
		t.Fatalf("GetSecret(generated-secret) = %q, want %q", value, "generated-secret-value")
	}
}

func TestRun_ProviderReleaseBuildsGoSourceWorkflowPlugin(t *testing.T) {
	t.Parallel()

	const workflowReleasePluginName = "workflow-release"
	const workflowReleaseSource = "github.com/testowner/providers/workflow-release"
	const workflowReleaseSchemaPath = "workflow.schema.json"

	pluginDir := newSourceComponentReleaseFixture(t, t.TempDir(), sourceComponentReleaseFixtureParams{
		pluginName: workflowReleasePluginName,
		schemaPath: workflowReleaseSchemaPath,
		sourceFile: "workflow.go",
		sourceCode: testutil.GeneratedWorkflowPackageSource(),
		manifest: &providermanifestv1.Manifest{
			Kind:   providermanifestv1.KindWorkflow,
			Source: workflowReleaseSource, Version: "0.0.1", DisplayName: "Workflow Release",
			Spec: &providermanifestv1.Spec{ConfigSchemaPath: workflowReleaseSchemaPath},
		},
	})
	outputDir := t.TempDir()
	const testVersion = "0.0.20-test"

	runProviderReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", runtime.GOOS+"/"+runtime.GOARCH,
		"--output", outputDir,
	)

	archiveName := platformArchiveNameForTest(workflowReleasePluginName, testVersion, runtime.GOOS, runtime.GOARCH)
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifest := readReleasedManifest(t, outputDir, archiveName)
	binaryName := releaseBinaryName(workflowReleasePluginName, runtime.GOOS)

	if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Path != binaryName {
		t.Fatalf("artifacts = %+v, want path %q", manifest.Artifacts, binaryName)
	}
	assertExpectedGoArtifactPlatform(t, manifest.Artifacts[0], runtime.GOOS, runtime.GOARCH, "")
	if manifest.Entrypoint == nil || manifest.Entrypoint.ArtifactPath != binaryName {
		t.Fatalf("workflow entrypoint = %+v, want artifact path %q", manifest.Entrypoint, binaryName)
	}
	if _, err := os.Stat(filepath.Join(extractDir, workflowReleaseSchemaPath)); err != nil {
		t.Fatalf("expected %s in archive: %v", workflowReleaseSchemaPath, err)
	}

	metadata := readProviderReleaseMetadata(t, outputDir)
	if metadata.Package != workflowReleaseSource {
		t.Fatalf("release metadata package = %q, want %q", metadata.Package, workflowReleaseSource)
	}
	if metadata.Kind != providermanifestv1.KindWorkflow {
		t.Fatalf("release metadata kind = %q, want %q", metadata.Kind, providermanifestv1.KindWorkflow)
	}
	if metadata.Runtime != providerReleaseRuntimeKindExecutable {
		t.Fatalf("release metadata runtime = %q, want %q", metadata.Runtime, providerReleaseRuntimeKindExecutable)
	}
	workflowArtifact, ok := metadata.Artifacts[providerpkg.CurrentPlatformString()]
	if !ok {
		t.Fatalf("release metadata artifacts missing current platform key %q: %+v", providerpkg.CurrentPlatformString(), metadata.Artifacts)
	}
	workflowDigest, err := providerpkg.ArchiveDigest(filepath.Join(outputDir, archiveName))
	if err != nil {
		t.Fatalf("hash workflow archive: %v", err)
	}
	if workflowArtifact.Path != archiveName || workflowArtifact.SHA256 != workflowDigest {
		t.Fatalf("release metadata workflow artifact = %+v, want path %q sha %q", workflowArtifact, archiveName, workflowDigest)
	}
}

func TestRun_ProviderReleaseBuildsGoSourceExternalCredentialsPlugin(t *testing.T) {
	t.Parallel()

	const externalCredentialReleasePluginName = "external-credentials-release"
	const externalCredentialReleaseSource = "github.com/testowner/providers/external-credentials-release"
	const externalCredentialReleaseSchemaPath = "external-credentials.schema.json"

	pluginDir := newSourceComponentReleaseFixture(t, t.TempDir(), sourceComponentReleaseFixtureParams{
		pluginName: externalCredentialReleasePluginName,
		schemaPath: externalCredentialReleaseSchemaPath,
		sourceFile: "external_credentials.go",
		sourceCode: testutil.GeneratedExternalCredentialPackageSource(),
		manifest: &providermanifestv1.Manifest{
			Kind:   providermanifestv1.KindExternalCredentials,
			Source: externalCredentialReleaseSource, Version: "0.0.1", DisplayName: "External Credentials Release",
			Spec: &providermanifestv1.Spec{ConfigSchemaPath: externalCredentialReleaseSchemaPath},
		},
	})
	outputDir := t.TempDir()
	const testVersion = "0.0.21-test"

	runProviderReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", runtime.GOOS+"/"+runtime.GOARCH,
		"--output", outputDir,
	)

	archiveName := platformArchiveNameForTest(externalCredentialReleasePluginName, testVersion, runtime.GOOS, runtime.GOARCH)
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifest := readReleasedManifest(t, outputDir, archiveName)
	binaryName := releaseBinaryName(externalCredentialReleasePluginName, runtime.GOOS)

	if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Path != binaryName {
		t.Fatalf("artifacts = %+v, want path %q", manifest.Artifacts, binaryName)
	}
	assertExpectedGoArtifactPlatform(t, manifest.Artifacts[0], runtime.GOOS, runtime.GOARCH, "")
	if manifest.Entrypoint == nil || manifest.Entrypoint.ArtifactPath != binaryName {
		t.Fatalf("external credentials entrypoint = %+v, want artifact path %q", manifest.Entrypoint, binaryName)
	}
	if _, err := os.Stat(filepath.Join(extractDir, externalCredentialReleaseSchemaPath)); err != nil {
		t.Fatalf("expected %s in archive: %v", externalCredentialReleaseSchemaPath, err)
	}

	services, err := coredata.New(&coretesting.StubIndexedDB{})
	if err != nil {
		t.Fatalf("coredata.New: %v", err)
	}
	coretesting.AttachStubExternalCredentials(services)

	provider, err := providerhost.NewExecutableExternalCredentialProvider(context.Background(), providerhost.ExternalCredentialsExecConfig{
		Command: filepath.Join(extractDir, binaryName),
		Name:    externalCredentialReleasePluginName,
		HostServices: []providerhost.HostService{{
			Name:   "external-credentials",
			EnvVar: providerhost.DefaultExternalCredentialSocketEnv,
			Register: func(srv *grpc.Server) {
				proto.RegisterExternalCredentialProviderServer(srv, providerhost.NewExternalCredentialProviderServer(services.ExternalCredentials))
			},
		}},
	})
	if err != nil {
		t.Fatalf("NewExecutableExternalCredentialProvider: %v", err)
	}
	defer func() {
		if closer, ok := provider.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}()

	credential := &core.ExternalCredential{
		SubjectID:    "user:user-123",
		Integration:  "slack",
		Connection:   "default",
		Instance:     "workspace-1",
		AccessToken:  "xoxb-123",
		RefreshToken: "refresh-123",
		Scopes:       "channels:read chat:write",
	}
	if err := provider.PutCredential(context.Background(), credential); err != nil {
		t.Fatalf("PutCredential: %v", err)
	}
	if credential.ID == "" {
		t.Fatal("PutCredential returned empty credential id")
	}
	if credential.CreatedAt.IsZero() || credential.UpdatedAt.IsZero() {
		t.Fatalf("credential timestamps = created_at:%v updated_at:%v", credential.CreatedAt, credential.UpdatedAt)
	}

	got, err := provider.GetCredential(context.Background(), credential.SubjectID, credential.Integration, credential.Connection, credential.Instance)
	if err != nil {
		t.Fatalf("GetCredential: %v", err)
	}
	if got.AccessToken != credential.AccessToken || got.RefreshToken != credential.RefreshToken {
		t.Fatalf("credential tokens = access:%q refresh:%q", got.AccessToken, got.RefreshToken)
	}

	listed, err := provider.ListCredentialsForConnection(context.Background(), credential.SubjectID, credential.Integration, credential.Connection)
	if err != nil {
		t.Fatalf("ListCredentialsForConnection: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != credential.ID {
		t.Fatalf("listed credentials = %+v", listed)
	}

	if err := provider.DeleteCredential(context.Background(), credential.ID); err != nil {
		t.Fatalf("DeleteCredential: %v", err)
	}

	_, err = provider.GetCredential(context.Background(), credential.SubjectID, credential.Integration, credential.Connection, credential.Instance)
	if !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("GetCredential after delete error = %v, want core.ErrNotFound", err)
	}
}

//nolint:paralleltest // Uses t.Setenv in table-driven subtests, which cannot run under parallel ancestors.
func TestRun_ProviderReleaseBuildsExecutableAuthProviders(t *testing.T) {
	goAuthFixture := func(t *testing.T) sourceComponentReleaseFixtureParams {
		t.Helper()
		return sourceComponentReleaseFixtureParams{
			pluginName: authReleasePluginName,
			schemaPath: authReleaseSchemaPath,
			sourceFile: "auth.go",
			sourceCode: testutil.GeneratedAuthPackageSource(),
			manifest: &providermanifestv1.Manifest{
				Kind:   providermanifestv1.KindAuthentication,
				Source: authReleaseSource, Version: "0.0.1", DisplayName: "Auth Release",
				Spec: &providermanifestv1.Spec{ConfigSchemaPath: authReleaseSchemaPath},
			},
		}
	}

	cases := []struct {
		name                string
		pluginName          string
		version             string
		skipOnWindowsReason string
		prepare             func(t *testing.T) string
		archiveName         func(version string) string
		assertArtifact      func(t *testing.T, artifact providermanifestv1.Artifact)
		assertSessionTTL    bool
		assertExternalJWT   bool
	}{
		{
			name:       "go_source",
			pluginName: authReleasePluginName,
			version:    "0.0.15-test",
			prepare: func(t *testing.T) string {
				t.Helper()
				return newSourceComponentReleaseFixture(t, t.TempDir(), goAuthFixture(t))
			},
			archiveName: func(version string) string {
				return platformArchiveNameForTest(authReleasePluginName, version, runtime.GOOS, runtime.GOARCH)
			},
			assertArtifact: func(t *testing.T, artifact providermanifestv1.Artifact) {
				t.Helper()
				assertExpectedGoArtifactPlatform(t, artifact, runtime.GOOS, runtime.GOARCH, "")
			},
			assertSessionTTL:  true,
			assertExternalJWT: true,
		},
		{
			name:                "rust_source",
			pluginName:          authReleasePluginName,
			version:             "0.0.17-rust-auth",
			skipOnWindowsReason: "fake cargo test fixture is POSIX-only",
			prepare: func(t *testing.T) string {
				t.Helper()

				hostTarget, _, err := providerpkgRustTargetTriple(runtime.GOOS, runtime.GOARCH, "")
				if err != nil {
					t.Fatalf("providerpkgRustTargetTriple(host): %v", err)
				}
				fakeCargoDir := t.TempDir()
				writeFakeRustReleaseCargo(t, filepath.Join(fakeCargoDir, "cargo"), fakeRustCargoConfig{
					ExpectedPluginName:   authReleasePluginName,
					ExpectedServeExport:  "__gestalt_serve_authentication",
					ExpectedCatalogWrite: false,
					DelegateBinary:       buildGoSourceAuthBinary(t),
					AllowedTargets:       []string{hostTarget},
				})
				t.Setenv("PATH", fakeCargoDir+string(os.PathListSeparator)+os.Getenv("PATH"))
				return newRustSourceAuthReleaseFixture(t, t.TempDir())
			},
			archiveName: func(version string) string {
				return platformArchiveNameForTest(authReleasePluginName, version, runtime.GOOS, runtime.GOARCH)
			},
			assertArtifact: func(t *testing.T, artifact providermanifestv1.Artifact) {
				t.Helper()
				assertExpectedRustArtifactPlatform(t, artifact, runtime.GOOS, runtime.GOARCH, "")
			},
			assertSessionTTL: true,
		},
		{
			name:                "python_source",
			pluginName:          pythonAuthReleasePluginName,
			version:             "0.0.16-python-auth",
			skipOnWindowsReason: "fake Python build fixture is POSIX-only",
			prepare: func(t *testing.T) string {
				t.Helper()

				goFixtureDir := newSourceComponentReleaseFixture(t, t.TempDir(), goAuthFixture(t))
				t.Setenv("GESTALT_TEST_PYINSTALLER_BINARY", buildGoSourceComponentBinaryForTest(t, goFixtureDir, providermanifestv1.KindAuthentication))
				t.Setenv("PATH", pathWithoutGo(t))
				return newPythonSourceAuthReleaseFixture(t, t.TempDir())
			},
			archiveName: func(version string) string {
				return expectedPythonArchiveNameFor(pythonAuthReleasePluginName, version, runtime.GOOS, runtime.GOARCH)
			},
			assertArtifact: func(t *testing.T, artifact providermanifestv1.Artifact) {
				t.Helper()
				assertExpectedScriptArtifactPlatform(t, artifact, runtime.GOOS, runtime.GOARCH)
			},
		},
		{
			name:                "typescript_source",
			pluginName:          authReleasePluginName,
			version:             "0.0.15-ts-auth",
			skipOnWindowsReason: "fake Bun build fixture is POSIX-only",
			prepare: func(t *testing.T) string {
				t.Helper()

				builtBinary := buildGoSourceAuthBinary(t)
				pluginDir := newTypeScriptSourceAuthReleaseFixture(t, t.TempDir())
				sdkDir := writeFakeTypeScriptReleaseSDKDir(t, t.TempDir())
				t.Setenv("GESTALT_TYPESCRIPT_SDK_DIR", sdkDir)
				bunPath := writeFakeTypeScriptComponentReleaseBun(t, authReleaseTypeScriptTarget, authReleasePluginName, runtime.GOOS, runtime.GOARCH, builtBinary)
				t.Setenv("PATH", pathWithoutGo(t))
				t.Setenv("GESTALT_BUN", bunPath)
				return pluginDir
			},
			archiveName: func(version string) string {
				return platformArchiveNameForTest(authReleasePluginName, version, runtime.GOOS, runtime.GOARCH)
			},
			assertArtifact: func(t *testing.T, artifact providermanifestv1.Artifact) {
				t.Helper()
				assertExpectedScriptArtifactPlatform(t, artifact, runtime.GOOS, runtime.GOARCH)
				if runtime.GOOS == "linux" && artifact.LibC != "" {
					t.Fatalf("artifact libc = %q, want %q", artifact.LibC, "")
				}
			},
		},
	}

	//nolint:paralleltest // The subtests share process-wide env mutation through t.Setenv in selected cases.
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if runtime.GOOS == "windows" && tc.skipOnWindowsReason != "" {
				t.Skip(tc.skipOnWindowsReason)
			}

			pluginDir := tc.prepare(t)
			outputDir := t.TempDir()
			runProviderReleaseCommand(t, pluginDir,
				"--version", tc.version,
				"--platform", runtime.GOOS+"/"+runtime.GOARCH,
				"--output", outputDir,
			)

			archiveName := tc.archiveName(tc.version)
			extractDir := extractReleasedArchive(t, outputDir, archiveName)
			_, manifest := readManifestFromDir(t, extractDir)
			binaryName := releaseBinaryName(tc.pluginName, runtime.GOOS)

			if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Path != binaryName {
				t.Fatalf("artifacts = %+v, want path %q", manifest.Artifacts, binaryName)
			}
			tc.assertArtifact(t, manifest.Artifacts[0])
			if manifest.Entrypoint == nil || manifest.Entrypoint.ArtifactPath != binaryName {
				t.Fatalf("auth entrypoint = %+v, want artifact path %q", manifest.Entrypoint, binaryName)
			}
			if _, err := os.Stat(filepath.Join(extractDir, authReleaseSchemaPath)); err != nil {
				t.Fatalf("expected %s in archive: %v", authReleaseSchemaPath, err)
			}

			assertExecutableAuthProviderWorks(t, filepath.Join(extractDir, binaryName), tc.pluginName, tc.assertSessionTTL, tc.assertExternalJWT)
		})
	}
}

func TestRun_ProviderReleaseCopiesCompiledSupportFiles(t *testing.T) {
	t.Parallel()

	pluginDir := newSourceProviderReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	testVersion := "0.0.2-test"

	runProviderReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", runtime.GOOS+"/"+runtime.GOARCH,
		"--output", outputDir,
	)

	archiveName := "gestalt-plugin-release-test_v" + testVersion + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	extractDir := extractReleasedArchive(t, outputDir, archiveName)

	if _, err := providerpkg.ValidatePackageDir(extractDir); err != nil {
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

func TestRun_ProviderReleaseCopiesUISupportFiles(t *testing.T) {
	t.Parallel()

	pluginDir := newUIReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	testVersion := "0.0.3-test"

	runProviderReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)

	archiveName := "gestalt-plugin-ui-test_v" + testVersion + ".tar.gz"
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

	metadata := readProviderReleaseMetadata(t, outputDir)
	if metadata.Package != uiTestSource {
		t.Fatalf("release metadata package = %q, want %q", metadata.Package, uiTestSource)
	}
	if metadata.Kind != providermanifestv1.KindUI {
		t.Fatalf("release metadata kind = %q, want %q", metadata.Kind, providermanifestv1.KindUI)
	}
	if metadata.Runtime != providerReleaseRuntimeKindUI {
		t.Fatalf("release metadata runtime = %q, want %q", metadata.Runtime, providerReleaseRuntimeKindUI)
	}
	uiArtifact, ok := metadata.Artifacts[providerReleaseGenericTarget]
	if !ok {
		t.Fatalf("release metadata artifacts missing generic key: %+v", metadata.Artifacts)
	}
	uiDigest, err := providerpkg.ArchiveDigest(filepath.Join(outputDir, archiveName))
	if err != nil {
		t.Fatalf("hash ui archive: %v", err)
	}
	if uiArtifact.Path != archiveName || uiArtifact.SHA256 != uiDigest {
		t.Fatalf("release metadata ui artifact = %+v, want path %q sha %q", uiArtifact, archiveName, uiDigest)
	}
}

func TestRun_ProviderReleaseStagesOwnedUIPackage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		fixture       func(*testing.T, string) string
		wantFiles     []string
		wantAssetRoot string
		skipOnWin     bool
	}{
		{
			name:          "prebuilt owned ui assets",
			fixture:       newSourceProviderReleaseFixtureWithOwnedUI,
			wantFiles:     []string{"_owned_ui/roadmap-ui/branding/icon.svg", "_owned_ui/roadmap-ui/dist/index.html", "_owned_ui/roadmap-ui/dist/static/app.js"},
			wantAssetRoot: filepath.Join("_owned_ui", "roadmap-ui", "dist"),
		},
		{
			name:          "built owned ui assets",
			fixture:       newSourceProviderReleaseFixtureWithBuiltOwnedUI,
			wantFiles:     []string{"_owned_ui/roadmap-ui/branding/icon.svg", "_owned_ui/roadmap-ui/ui/dist/index.html", "_owned_ui/roadmap-ui/ui/dist/static/app.js"},
			wantAssetRoot: filepath.Join("_owned_ui", "roadmap-ui", "ui", "dist"),
			skipOnWin:     true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.skipOnWin && runtime.GOOS == "windows" {
				t.Skip("owned ui release-build fixture uses POSIX shell")
			}

			pluginDir := tc.fixture(t, t.TempDir())
			outputDir := t.TempDir()
			testVersion := "0.0.3-owned-ui"

			runProviderReleaseCommand(t, pluginDir,
				"--version", testVersion,
				"--platform", runtime.GOOS+"/"+runtime.GOARCH,
				"--output", outputDir,
			)

			archiveName := "gestalt-plugin-release-test_v" + testVersion + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
			extractDir := extractReleasedArchive(t, outputDir, archiveName)
			manifest := readReleasedManifest(t, outputDir, archiveName)
			if manifest.Spec == nil || manifest.Spec.UI == nil {
				t.Fatalf("released manifest spec.ui = %+v", manifest.Spec)
			}
			const wantOwnedUIPath = "_owned_ui/roadmap-ui/manifest.json"
			if got := manifest.Spec.UI.Path; got != wantOwnedUIPath {
				t.Fatalf("spec.ui.path = %q, want %q", got, wantOwnedUIPath)
			}
			for _, rel := range append([]string{wantOwnedUIPath}, tc.wantFiles...) {
				if _, err := os.Stat(filepath.Join(extractDir, filepath.FromSlash(rel))); err != nil {
					t.Fatalf("expected %s in archive: %v", rel, err)
				}
			}
			_, ownedUIManifest, err := providerpkg.ReadManifestFile(filepath.Join(extractDir, filepath.FromSlash(wantOwnedUIPath)))
			if err != nil {
				t.Fatalf("read owned ui manifest: %v", err)
			}
			if ownedUIManifest.Release != nil {
				t.Fatalf("owned ui manifest unexpectedly retained release metadata: %+v", ownedUIManifest.Release)
			}
			metadata := readProviderReleaseMetadata(t, outputDir)
			if metadata.Schema != providerReleaseSchemaName {
				t.Fatalf("release metadata schema = %q, want %q", metadata.Schema, providerReleaseSchemaName)
			}
			if metadata.SchemaVersion != providerReleaseSchemaVersion {
				t.Fatalf("release metadata schemaVersion = %d, want %d", metadata.SchemaVersion, providerReleaseSchemaVersion)
			}
			if metadata.Package != releaseTestSource {
				t.Fatalf("release metadata package = %q, want %q", metadata.Package, releaseTestSource)
			}
			if metadata.Kind != providermanifestv1.KindPlugin {
				t.Fatalf("release metadata kind = %q, want %q", metadata.Kind, providermanifestv1.KindPlugin)
			}
			if metadata.Version != testVersion {
				t.Fatalf("release metadata version = %q, want %q", metadata.Version, testVersion)
			}
			if metadata.Runtime != providerReleaseRuntimeKindExecutable {
				t.Fatalf("release metadata runtime = %q, want %q", metadata.Runtime, providerReleaseRuntimeKindExecutable)
			}
			if len(metadata.Artifacts) != 1 {
				t.Fatalf("release metadata artifacts = %+v, want 1 entry", metadata.Artifacts)
			}
			artifact, ok := metadata.Artifacts[providerpkg.CurrentPlatformString()]
			if !ok {
				t.Fatalf("release metadata artifacts missing current platform key %q: %+v", providerpkg.CurrentPlatformString(), metadata.Artifacts)
			}
			if got := artifact.Path; got != archiveName {
				t.Fatalf("release metadata artifact path = %q, want %q", got, archiveName)
			}
			digest, err := providerpkg.ArchiveDigest(filepath.Join(outputDir, archiveName))
			if err != nil {
				t.Fatalf("hash archive: %v", err)
			}
			if got := artifact.SHA256; got != digest {
				t.Fatalf("release metadata artifact sha256 = %q, want %q", got, digest)
			}

			releaseServer := httptest.NewServer(http.FileServer(http.Dir(outputDir)))
			defer releaseServer.Close()

			configDir := t.TempDir()
			configPath := writeManagedPluginConfigForTest(t, configDir, "roadmap", releaseServer.URL+"/provider-release.yaml", "/create-customer-roadmap-review")
			lc := operator.NewLifecycle().WithHTTPClient(releaseServer.Client())
			if _, err := lc.InitAtPath(configPath); err != nil {
				t.Fatalf("InitAtPath: %v", err)
			}

			loaded, _, err := lc.LoadForExecutionAtPath(configPath, true)
			if err != nil {
				t.Fatalf("LoadForExecutionAtPath(locked=true): %v", err)
			}
			plugin := loaded.Plugins["roadmap"]
			if plugin == nil || plugin.ResolvedManifest == nil {
				t.Fatalf("ResolvedManifest = %+v", plugin)
			}
			if plugin.Command == "" {
				t.Fatalf("plugin.Command = %q, want packaged executable path", plugin.Command)
			}
			if got := plugin.ResolvedManifest.Version; got != testVersion {
				t.Fatalf("ResolvedManifest.Version = %q, want %q", got, testVersion)
			}

			uiEntry := loaded.Providers.UI["roadmap"]
			if uiEntry == nil || uiEntry.ResolvedManifest == nil {
				t.Fatalf("Resolved plugin-owned UI = %+v", uiEntry)
			}
			if uiEntry.Path != "/create-customer-roadmap-review" {
				t.Fatalf("uiEntry.Path = %q, want %q", uiEntry.Path, "/create-customer-roadmap-review")
			}
			if got := filepath.ToSlash(uiEntry.ResolvedManifestPath); !strings.HasSuffix(got, filepath.ToSlash(filepath.Join("_owned_ui", "roadmap-ui", providerpkg.ManifestFile))) {
				t.Fatalf("ResolvedManifestPath = %q, want owned-ui manifest suffix", got)
			}
			if got := filepath.ToSlash(uiEntry.ResolvedAssetRoot); !strings.HasSuffix(got, filepath.ToSlash(tc.wantAssetRoot)) {
				t.Fatalf("ResolvedAssetRoot = %q, want owned-ui asset root suffix %q", got, tc.wantAssetRoot)
			}

			lock, err := operator.ReadLockfile(filepath.Join(configDir, operator.InitLockfileName))
			if err != nil {
				t.Fatalf("ReadLockfile: %v", err)
			}
			if len(lock.UIs) != 0 {
				t.Fatalf("lock.UIs = %#v, want no separate UI entries for packaged owned UI", lock.UIs)
			}
		})
	}
}

func TestRun_ProviderReleaseBuildsProviderSupportFilesBeforePackaging(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("release build fixture uses POSIX shell")
	}

	pluginDir := newBuiltSourceProviderReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.3-build-provider"

	runProviderReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", runtime.GOOS+"/"+runtime.GOARCH,
		"--output", outputDir,
	)

	archiveName := "gestalt-plugin-release-test_v" + testVersion + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifest := readReleasedManifest(t, outputDir, archiveName)
	if manifest.Release != nil {
		t.Fatalf("released manifest unexpectedly retained release metadata: %+v", manifest.Release)
	}
	if _, err := os.Stat(filepath.Join(extractDir, releaseProviderSchemaPath)); err != nil {
		t.Fatalf("expected %s in archive: %v", releaseProviderSchemaPath, err)
	}
}

func TestRun_ProviderReleaseBuildsUIAssetsBeforePackaging(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("release build fixture uses POSIX shell")
	}

	pluginDir := newBuiltUIReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.3-build-ui"

	runProviderReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)

	archiveName := "gestalt-plugin-ui-test_v" + testVersion + ".tar.gz"
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifest := readReleasedManifest(t, outputDir, archiveName)
	if manifest.Release != nil {
		t.Fatalf("released manifest unexpectedly retained release metadata: %+v", manifest.Release)
	}
	for _, rel := range []string{
		"branding/icon.svg",
		"ui/out/index.html",
		"ui/out/static/app.js",
	} {
		if _, err := os.Stat(filepath.Join(extractDir, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("expected %s in archive: %v", rel, err)
		}
	}
}

func TestRun_ProviderReleaseAllowsOverlappingSupportPaths(t *testing.T) {
	t.Parallel()

	pluginDir := filepath.Join(t.TempDir(), "ui-overlap")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(pluginDir): %v", err)
	}
	writeReleaseTestManifest(t, pluginDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindUI,
		Source:      "github.com/testowner/plugins/ui-overlap",
		Version:     "0.0.1",
		DisplayName: "UI Overlap",
		IconFile:    "out/icon.svg",
		Spec: &providermanifestv1.Spec{
			AssetRoot: "out",
		},
	})
	writeTestFile(t, pluginDir, "out/icon.svg", []byte("<svg></svg>\n"), 0o644)
	writeTestFile(t, pluginDir, "out/index.html", []byte("<html></html>\n"), 0o644)

	outputDir := t.TempDir()
	const testVersion = "0.0.3-overlap.1"

	runProviderReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)

	archiveName := "gestalt-plugin-ui-overlap_v" + testVersion + ".tar.gz"
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	for _, rel := range []string{"out/icon.svg", "out/index.html"} {
		if _, err := os.Stat(filepath.Join(extractDir, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("expected %s in archive: %v", rel, err)
		}
	}
}

func TestRun_ProviderReleaseTreatsGoModWithoutProviderPackageAsDeclarative(t *testing.T) {
	t.Parallel()

	pluginDir := newUIReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	testVersion := "0.0.4-test"

	writeTestFile(t, pluginDir, "go.mod", []byte("module example.com/ui-test\n\ngo 1.22\n"), 0644)

	runProviderReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)

	archiveName := "gestalt-plugin-ui-test_v" + testVersion + ".tar.gz"
	if _, err := os.Stat(filepath.Join(outputDir, archiveName)); err != nil {
		t.Fatalf("expected declarative archive %s to exist: %v", archiveName, err)
	}

	compiledArchiveName := "gestalt-plugin-ui-test_v" + testVersion + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	if _, err := os.Stat(filepath.Join(outputDir, compiledArchiveName)); !os.IsNotExist(err) {
		t.Fatalf("unexpected compiled archive %s: %v", compiledArchiveName, err)
	}
}

func TestRun_ProviderReleaseWritesProviderReleaseMetadataForDeclarativePlugin(t *testing.T) {
	t.Parallel()

	pluginDir := newDeclarativeProviderReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.4-declarative.1"

	runProviderReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)

	archiveName := "gestalt-plugin-" + declarativeReleasePluginName + "_v" + testVersion + ".tar.gz"
	if _, err := os.Stat(filepath.Join(outputDir, archiveName)); err != nil {
		t.Fatalf("expected archive %s to exist: %v", archiveName, err)
	}

	metadata := readProviderReleaseMetadata(t, outputDir)
	if metadata.Package != declarativeReleaseSource {
		t.Fatalf("release metadata package = %q, want %q", metadata.Package, declarativeReleaseSource)
	}
	if metadata.Kind != providermanifestv1.KindPlugin {
		t.Fatalf("release metadata kind = %q, want %q", metadata.Kind, providermanifestv1.KindPlugin)
	}
	if metadata.Version != testVersion {
		t.Fatalf("release metadata version = %q, want %q", metadata.Version, testVersion)
	}
	if metadata.Runtime != providerReleaseRuntimeKindDeclarative {
		t.Fatalf("release metadata runtime = %q, want %q", metadata.Runtime, providerReleaseRuntimeKindDeclarative)
	}
	if len(metadata.Artifacts) != 1 {
		t.Fatalf("release metadata artifacts = %+v, want 1 entry", metadata.Artifacts)
	}
	artifact, ok := metadata.Artifacts[providerReleaseGenericTarget]
	if !ok {
		t.Fatalf("release metadata artifacts missing generic key: %+v", metadata.Artifacts)
	}
	if got := artifact.Path; got != archiveName {
		t.Fatalf("release metadata artifact path = %q, want %q", got, archiveName)
	}
	digest, err := providerpkg.ArchiveDigest(filepath.Join(outputDir, archiveName))
	if err != nil {
		t.Fatalf("hash archive: %v", err)
	}
	if got := artifact.SHA256; got != digest {
		t.Fatalf("release metadata artifact sha256 = %q, want %q", got, digest)
	}
}

func TestRun_ProviderReleasePreservesYAMLManifestFormatAndConnectionDefaults(t *testing.T) {
	t.Parallel()

	pluginDir := newSourceProviderReleaseFixture(t, t.TempDir())
	writeReleaseTestManifestFormat(t, pluginDir, "manifest.yaml", &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindPlugin,
		Source:      "github.com/testowner/plugins/provider-yaml",
		Version:     "0.0.1",
		DisplayName: "Provider YAML",
		Spec: &providermanifestv1.Spec{
			ConfigSchemaPath: releaseProviderSchemaPath,
			MCP:              true,
			Connections: map[string]*providermanifestv1.ManifestConnectionDef{
				"default": {
					Mode: providermanifestv1.ConnectionModeUser,
					Params: map[string]providermanifestv1.ProviderConnectionParam{
						"tenant": {Required: true},
					},
				},
			},
		},
	})
	if err := os.Remove(filepath.Join(pluginDir, providerpkg.ManifestFile)); err != nil {
		t.Fatalf("remove manifest.json: %v", err)
	}

	outputDir := t.TempDir()
	const testVersion = "0.0.4-yaml.1"

	runProviderReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)

	archiveName := "gestalt-plugin-provider-yaml_v" + testVersion + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifestPath, manifest := readManifestFromDir(t, extractDir)
	if filepath.Base(manifestPath) != "manifest.yaml" {
		t.Fatalf("released manifest = %q, want manifest.yaml", filepath.Base(manifestPath))
	}
	if manifest.Spec == nil || manifest.Spec.Connections["default"] == nil || len(manifest.Spec.Connections["default"].Params) != 1 || !manifest.Spec.Connections["default"].Params["tenant"].Required {
		t.Fatalf("provider connection_params = %+v", manifest.Spec)
	}
	if manifest.Spec.Connections["default"].Mode != "user" {
		t.Fatalf("provider default connection mode = %q, want %q", manifest.Spec.Connections["default"].Mode, "user")
	}

	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read released manifest: %v", err)
	}
	for _, expected := range []string{
		"spec:",
		"connections:",
		"default:",
		"mode: user",
		"params:",
		"mcp: true",
		"entrypoint:",
		"artifactPath:",
	} {
		if !strings.Contains(string(manifestData), expected) {
			t.Fatalf("expected released manifest to contain canonical field %q, got: %s", expected, manifestData)
		}
	}
	for _, legacy := range []string{
		"connectionMode:",
		"connectionParams:",
	} {
		if strings.Contains(string(manifestData), legacy) {
			t.Fatalf("expected released manifest to omit legacy field %q, got: %s", legacy, manifestData)
		}
	}
}

func TestRun_ProviderReleaseSupportsSourcePackageManifestFile(t *testing.T) {
	t.Parallel()

	pluginDir := newSourceProviderReleaseFixture(t, t.TempDir())
	if err := os.Remove(filepath.Join(pluginDir, providerpkg.ManifestFile)); err != nil {
		t.Fatalf("remove %s: %v", providerpkg.ManifestFile, err)
	}
	writeReleaseTestManifestFormat(t, pluginDir, "manifest.yaml", &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindPlugin,
		Source:      "github.com/testowner/plugins/source-manifest",
		Version:     "0.0.1",
		DisplayName: "Source Manifest",
		Spec: &providermanifestv1.Spec{
			ConfigSchemaPath: releaseProviderSchemaPath,
			MCP:              true,
		},
	})

	outputDir := t.TempDir()
	const testVersion = "0.0.4-source.1"

	runProviderReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)

	archiveName := "gestalt-plugin-source-manifest_v" + testVersion + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifestPath, manifest := readManifestFromDir(t, extractDir)
	if filepath.Base(manifestPath) != "manifest.yaml" {
		t.Fatalf("released manifest = %q, want manifest.yaml", filepath.Base(manifestPath))
	}
	if manifest.Source != "github.com/testowner/plugins/source-manifest" {
		t.Fatalf("manifest source = %q, want %q", manifest.Source, "github.com/testowner/plugins/source-manifest")
	}
}

func TestRun_ProviderReleaseChecksumsOnlyCurrentArchives(t *testing.T) {
	t.Parallel()

	pluginDir := newUIReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()

	runProviderReleaseCommand(t, pluginDir,
		"--version", "1.0.0",
		"--output", outputDir,
	)
	runProviderReleaseCommand(t, pluginDir,
		"--version", "1.0.1",
		"--output", outputDir,
	)

	checksumPath := filepath.Join(outputDir, "checksums.txt")
	checksumData, err := os.ReadFile(checksumPath)
	if err != nil {
		t.Fatalf("read checksums.txt: %v", err)
	}
	if got := string(checksumData); strings.Contains(got, "gestalt-plugin-ui-test_v1.0.0.tar.gz") {
		t.Fatalf("checksums.txt unexpectedly included stale archive: %s", got)
	} else if !strings.Contains(got, "gestalt-plugin-ui-test_v1.0.1.tar.gz") {
		t.Fatalf("checksums.txt missing current archive: %s", got)
	}
}

func TestRun_ProviderReleaseRejectsOutputInsideUIAssetRoot(t *testing.T) {
	t.Parallel()

	pluginDir := newUIReleaseFixtureWithAssetRoot(t, t.TempDir(), "release-output")
	outputDir := filepath.Join(pluginDir, "release-output", "nested")

	out, err := runProviderReleaseCommandResult(pluginDir, "--version", "1.0.0", "--output", outputDir)
	if err == nil {
		t.Fatalf("expected provider release to fail, got output: %s", out)
	}
	if !strings.Contains(string(out), "must not be inside ui.asset_root") {
		t.Fatalf("expected overlap error, got: %s", out)
	}
}

func TestRun_ProviderReleaseRejectsHybridExecutableDuplicateEffectiveOperation(t *testing.T) {
	t.Parallel()

	pluginDir := newSourceProviderReleaseFixture(t, t.TempDir())
	manifestPath := filepath.Join(pluginDir, providerpkg.ManifestFile)
	_, manifest, err := providerpkg.ReadSourceManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadSourceManifestFile(%s): %v", providerpkg.ManifestFile, err)
	}
	if manifest.Spec == nil {
		manifest.Spec = &providermanifestv1.Spec{}
	}
	manifest.Spec.Surfaces = &providermanifestv1.ProviderSurfaces{
		OpenAPI: &providermanifestv1.OpenAPISurface{Document: "openapi.yaml"},
	}
	manifest.Spec.AllowedOperations = map[string]*providermanifestv1.ManifestOperationOverride{
		"external_op": {Alias: "generated_op"},
	}
	manifestData, err := providerpkg.EncodeSourceManifestFormat(manifest, providerpkg.ManifestFormatFromPath(manifestPath))
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat: %v", err)
	}
	if err := os.WriteFile(manifestPath, manifestData, 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "openapi.yaml"), []byte(`openapi: "3.1.0"
info:
  title: Hybrid Duplicate
  version: "1.0.0"
paths:
  /external-op:
    get:
      operationId: external_op
      responses:
        "200":
          description: OK
`), 0o644); err != nil {
		t.Fatalf("WriteFile openapi.yaml: %v", err)
	}

	out, err := runProviderReleaseCommandResult(pluginDir, "--version", "0.0.4-source.1", "--platform", runtime.GOOS+"/"+runtime.GOARCH, "--output", t.TempDir())
	if err == nil {
		t.Fatalf("expected provider release to fail, got output: %s", out)
	}
	if !strings.Contains(string(out), `duplicate operation \"generated_op\" across merged catalogs`) {
		t.Fatalf("expected duplicate effective operation error, got: %s", out)
	}
}

func TestRun_ProviderReleaseCompilesProviderWithoutSourceArtifacts(t *testing.T) {
	t.Parallel()

	pluginDir := newSourceProviderReleaseFixtureWithoutCatalog(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.4-source.1"

	runProviderReleaseCommand(t, pluginDir,
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
	if manifest.Entrypoint == nil || manifest.Entrypoint.ArtifactPath != releaseBinaryName(releaseTestPluginName, runtime.GOOS) {
		t.Fatalf("provider entrypoint = %+v", manifest.Entrypoint)
	}
	if manifest.Spec == nil || manifest.Spec.ConfigSchemaPath != releaseProviderSchemaPath {
		t.Fatalf("provider metadata = %#v, want config schema path %q", manifest.Spec, releaseProviderSchemaPath)
	}
	data, err := os.ReadFile(filepath.Join(extractDir, providerpkg.StaticCatalogFile))
	if err != nil {
		t.Fatalf("read generated catalog: %v", err)
	}
	if !strings.Contains(string(data), "generated_op") {
		t.Fatalf("unexpected generated catalog: %s", data)
	}
}

func TestRun_ProviderReleaseRejectsRequiredExecutableKindsWithoutSourceOrEntrypoint(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		manifest  *providermanifestv1.Manifest
		wantError string
	}{
		{
			name: "provider",
			manifest: &providermanifestv1.Manifest{
				Kind:        providermanifestv1.KindPlugin,
				Source:      "github.com/testowner/plugins/missing-provider",
				Version:     "0.0.1",
				DisplayName: "Missing Provider",
				Spec:        &providermanifestv1.Spec{},
			},
			wantError: "no Go, Rust, Python, or TypeScript provider package found",
		},
		{
			name: "auth",
			manifest: &providermanifestv1.Manifest{
				Kind:        providermanifestv1.KindAuthentication,
				Source:      "github.com/testowner/plugins/missing-auth",
				Version:     "0.0.1",
				DisplayName: "Missing Auth",
				Spec:        &providermanifestv1.Spec{},
			},
			wantError: "no Go, Rust, Python, or TypeScript authentication source package found",
		},
		{
			name: "authorization",
			manifest: &providermanifestv1.Manifest{
				Kind:        providermanifestv1.KindAuthorization,
				Source:      "github.com/testowner/plugins/missing-authorization",
				Version:     "0.0.1",
				DisplayName: "Missing Authorization",
				Spec:        &providermanifestv1.Spec{},
			},
			wantError: "no Go authorization source package found",
		},
		{
			name: "secrets",
			manifest: &providermanifestv1.Manifest{
				Kind:        providermanifestv1.KindSecrets,
				Source:      "github.com/testowner/plugins/missing-secrets",
				Version:     "0.0.1",
				DisplayName: "Missing Secrets",
				Spec:        &providermanifestv1.Spec{},
			},
			wantError: "no Go, Rust, Python, or TypeScript secrets source package found",
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

			out, err := runProviderReleaseCommandResult(pluginDir, "--version", "0.0.1-test", "--output", t.TempDir())
			if err == nil {
				t.Fatalf("expected missing source error, got output: %s", out)
			}
			if !strings.Contains(string(out), tc.wantError) {
				t.Fatalf("unexpected output: %s", out)
			}
		})
	}
}

func TestRun_ProviderReleasePreservesPrebuiltProvider(t *testing.T) {
	t.Parallel()

	pluginDir := newPrebuiltProviderReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.5-test"

	runProviderReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)

	archiveName := "gestalt-plugin-" + prebuiltProviderPluginName + "_v" + testVersion + ".tar.gz"
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifest := readReleasedManifest(t, outputDir, archiveName)

	if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Path != prebuiltProviderBinaryPath {
		t.Fatalf("artifacts = %+v", manifest.Artifacts)
	}
	if manifest.Entrypoint == nil {
		t.Fatal("expected provider entrypoint")
	}
	if manifest.Entrypoint.ArtifactPath != prebuiltProviderBinaryPath {
		t.Fatalf("provider artifact path = %q", manifest.Entrypoint.ArtifactPath)
	}
	if manifest.Spec == nil || manifest.Spec.ConfigSchemaPath != releaseProviderSchemaPath {
		t.Fatalf("provider metadata = %#v, want config schema path %q", manifest.Spec, releaseProviderSchemaPath)
	}
	if _, err := os.Stat(filepath.Join(extractDir, filepath.FromSlash(prebuiltProviderBinaryPath))); err != nil {
		t.Fatalf("expected prebuilt artifact in archive: %v", err)
	}

	metadata := readProviderReleaseMetadata(t, outputDir)
	if metadata.Package != prebuiltProviderSource {
		t.Fatalf("release metadata package = %q, want %q", metadata.Package, prebuiltProviderSource)
	}
	if metadata.Kind != providermanifestv1.KindPlugin {
		t.Fatalf("release metadata kind = %q, want %q", metadata.Kind, providermanifestv1.KindPlugin)
	}
	if metadata.Runtime != providerReleaseRuntimeKindExecutable {
		t.Fatalf("release metadata runtime = %q, want %q", metadata.Runtime, providerReleaseRuntimeKindExecutable)
	}
	prebuiltArtifact, ok := metadata.Artifacts[providerpkg.CurrentPlatformString()]
	if !ok {
		t.Fatalf("release metadata artifacts missing current platform key %q: %+v", providerpkg.CurrentPlatformString(), metadata.Artifacts)
	}
	prebuiltDigest, err := providerpkg.ArchiveDigest(filepath.Join(outputDir, archiveName))
	if err != nil {
		t.Fatalf("hash prebuilt archive: %v", err)
	}
	if prebuiltArtifact.Path != archiveName || prebuiltArtifact.SHA256 != prebuiltDigest {
		t.Fatalf("release metadata prebuilt artifact = %+v, want path %q sha %q", prebuiltArtifact, archiveName, prebuiltDigest)
	}
}

func TestRun_ProviderReleasePackagesGoModuleWithoutCmdAsSource(t *testing.T) {
	t.Parallel()

	pluginDir := newPrebuiltProviderReleaseFixture(t, t.TempDir())
	writeTestFile(t, pluginDir, "go.mod", []byte("module example.com/prebuilt-provider\n\ngo 1.22\n"), 0644)

	outputDir := t.TempDir()
	const testVersion = "0.0.6-test"

	runProviderReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)

	archiveName := "gestalt-plugin-" + prebuiltProviderPluginName + "_v" + testVersion + ".tar.gz"
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifest := readReleasedManifest(t, outputDir, archiveName)

	if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Path != prebuiltProviderBinaryPath {
		t.Fatalf("artifacts = %+v", manifest.Artifacts)
	}
	if manifest.Entrypoint == nil || manifest.Entrypoint.ArtifactPath != prebuiltProviderBinaryPath {
		t.Fatalf("provider entrypoint = %+v", manifest.Entrypoint)
	}
	if _, err := os.Stat(filepath.Join(extractDir, filepath.FromSlash(prebuiltProviderBinaryPath))); err != nil {
		t.Fatalf("expected prebuilt artifact in archive: %v", err)
	}
}

func TestRun_ProviderReleaseRejectsStaleSourceArtifactDigest(t *testing.T) {
	t.Parallel()

	pluginDir := newPrebuiltProviderReleaseFixture(t, t.TempDir())

	_, manifest, err := providerpkg.ReadSourceManifestFile(filepath.Join(pluginDir, providerpkg.ManifestFile))
	if err != nil {
		t.Fatalf("ReadSourceManifestFile(manifest.json): %v", err)
	}
	manifest.Artifacts = []providermanifestv1.Artifact{
		{
			OS:     runtime.GOOS,
			Arch:   runtime.GOARCH,
			Path:   prebuiltProviderBinaryPath,
			SHA256: sha256HexForTest("different-content"),
		},
	}
	writeReleaseTestManifest(t, pluginDir, manifest)

	out, err := runProviderReleaseCommandResult(pluginDir, "--version", "0.0.8-test")
	if err == nil {
		t.Fatal("expected stale digest error")
	}
	if !strings.Contains(string(out), "sha256 mismatch") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestRun_ProviderReleaseWindowsArtifactUsesExe(t *testing.T) {
	t.Parallel()

	pluginDir := newSourceProviderReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.9-test"
	const windowsPlatform = "windows/amd64"

	runProviderReleaseCommand(t, pluginDir,
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
	if manifest.Entrypoint == nil || manifest.Entrypoint.ArtifactPath != binaryName {
		t.Fatalf("provider entrypoint = %+v, want artifact path %q", manifest.Entrypoint, binaryName)
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


plugin = gestalt.Plugin(
    "python-release",
    securitySchemes={
        "signed": {
            "type": "hmac",
            "secret": {"env": "REQUEST_SIGNING_SECRET"},
            "signatureHeader": "X-Request-Signature",
            "signaturePrefix": "v0=",
            "payloadTemplate": "v0:{header:X-Request-Timestamp}:{raw_body}",
            "timestampHeader": "X-Request-Timestamp",
            "maxAgeSeconds": 300,
        }
    },
    http={
        "command": {
            "path": "/command",
            "method": "POST",
            "security": "signed",
            "target": "greet",
            "requestBody": {
                "required": True,
                "content": {
                    "application/x-www-form-urlencoded": {},
                },
            },
            "ack": {
                "status": 200,
                "body": {
                    "status": "accepted",
                },
            },
        }
    },
)


@plugin.operation(method="GET", read_only=True)
def greet(input: GreetInput, _req: gestalt.Request) -> GreetOutput:
    return GreetOutput(message=f"Hello, {input.name}!")


@plugin.session_catalog
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
	manifestData, err := providerpkg.EncodeSourceManifestFormat(&providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindPlugin,
		Source:  "github.com/testowner/plugins/python-release",
		Version: "0.0.1",
		Spec: &providermanifestv1.Spec{
			Connections: map[string]*providermanifestv1.ManifestConnectionDef{
				"default": {
					Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeNone},
				},
			},
		},
	}, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat: %v", err)
	}
	writeTestFile(t, pluginDir, "manifest.yaml", manifestData, 0o644)
	writeFakePythonReleaseInterpreter(t, filepath.Join(pluginDir, ".venv", "bin", "python"), runtime.GOOS, runtime.GOARCH)
	return pluginDir
}

func newManifestBackedPythonSourceReleaseFixture(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := newPythonSourceReleaseFixture(t, dir)
	writeReleaseTestManifest(t, pluginDir, &providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindPlugin,
		Source:  "github.com/testowner/plugins/python-release",
		Version: "0.0.1",
		Spec: &providermanifestv1.Spec{
			Surfaces: &providermanifestv1.ProviderSurfaces{
				REST: &providermanifestv1.RESTSurface{
					BaseURL: "https://api.example.test",
					Operations: []providermanifestv1.ProviderOperation{
						{
							Name:   "list_widgets",
							Method: "GET",
							Path:   "/widgets",
						},
					},
				},
			},
			Connections: map[string]*providermanifestv1.ManifestConnectionDef{
				"default": {
					Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeNone},
				},
			},
		},
	})
	return pluginDir
}

func newTypeScriptSourceAuthReleaseFixture(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := filepath.Join(dir, authReleasePluginName)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(pluginDir): %v", err)
	}
	writeTestFile(t, pluginDir, "package.json", []byte(`{
  "name": "`+authReleasePluginName+`",
  "version": "0.0.1",
  "gestalt": {
    "provider": {
      "kind": "authentication",
      "target": "`+authReleaseTypeScriptModule+`"
    }
  }
}
`), 0o644)
	writeTestFile(t, pluginDir, "auth.ts", []byte("export const auth = {};\n"), 0o644)
	writeReleaseTestManifest(t, pluginDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindAuthentication,
		Source:      authReleaseSource,
		Version:     "0.0.1",
		DisplayName: "Auth Release",
		Spec: &providermanifestv1.Spec{
			ConfigSchemaPath: authReleaseSchemaPath,
		},
	})
	writeTestFile(t, pluginDir, authReleaseSchemaPath, []byte(`{"type":"object"}`), 0o644)
	return pluginDir
}

func newPythonSourceAuthReleaseFixture(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := filepath.Join(dir, pythonAuthReleasePluginName)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(pluginDir): %v", err)
	}
	writeTestFile(t, pluginDir, "pyproject.toml", []byte(`[build-system]
requires = ["setuptools==82.0.1"]
build-backend = "setuptools.build_meta"

[project]
name = "`+pythonAuthReleasePluginName+`"
version = "0.0.1-alpha.1"
dependencies = ["gestalt"]

[tool.gestalt]
authentication = "provider:auth_provider"
`), 0o644)
	writeTestFile(t, pluginDir, "provider.py", []byte("auth_provider = object()\n"), 0o644)
	writeReleaseTestManifest(t, pluginDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindAuthentication,
		Source:      pythonAuthReleaseSource,
		Version:     "0.0.1",
		DisplayName: "Python Auth Release",
		Spec: &providermanifestv1.Spec{
			ConfigSchemaPath: authReleaseSchemaPath,
		},
	})
	writeTestFile(t, pluginDir, authReleaseSchemaPath, []byte(`{"type":"object"}`), 0o644)
	writeFakePythonReleaseInterpreterForKind(
		t,
		filepath.Join(pluginDir, ".venv", "bin", "python"),
		"provider:auth_provider",
		"authentication",
		pythonAuthReleasePluginName,
		runtime.GOOS,
		runtime.GOARCH,
	)
	return pluginDir
}

func writeFakeTypeScriptComponentReleaseBun(t *testing.T, expectedTarget, expectedPluginName, expectedGOOS, expectedGOARCH, builtBinaryPath string) string {
	t.Helper()
	return writeFakeTypeScriptComponentReleaseBunForPlatforms(t, expectedTarget, expectedPluginName, []releasePlatform{{
		GOOS:   expectedGOOS,
		GOARCH: expectedGOARCH,
	}}, builtBinaryPath)
}

func writeFakeTypeScriptComponentReleaseBunForPlatforms(t *testing.T, expectedTarget, expectedPluginName string, expectedPlatforms []releasePlatform, builtBinaryPath string) string {
	t.Helper()

	allowedPlatforms := make([]fakebun.Platform, 0, len(expectedPlatforms))
	for _, platform := range expectedPlatforms {
		allowedPlatforms = append(allowedPlatforms, fakebun.Platform{
			GOOS:   platform.GOOS,
			GOARCH: platform.GOARCH,
		})
	}

	sdkPath := strings.TrimSpace(os.Getenv("GESTALT_TYPESCRIPT_SDK_DIR"))
	if sdkPath == "" {
		t.Fatal("GESTALT_TYPESCRIPT_SDK_DIR must be set for TypeScript release tests")
	}

	sourceDir := "."
	return fakebun.NewExecutable(t, fakebun.Config{
		Install: &fakebun.InstallConfig{
			ExpectedCwd:           sdkPath,
			RequireFrozenLockfile: true,
		},
		Build: &fakebun.BuildConfig{
			ExpectedCwd:        sdkPath,
			ExpectedEntry:      filepath.Join(sdkPath, "src", "build.ts"),
			ExpectedSourceDir:  sourceDir,
			ExpectedTarget:     expectedTarget,
			ExpectedPluginName: expectedPluginName,
			AllowedPlatforms:   allowedPlatforms,
			CopyBinaryFrom:     builtBinaryPath,
		},
	})
}

func writeFakeTypeScriptReleaseSDKDir(t *testing.T, root string) string {
	t.Helper()
	writeTestFile(t, root, "package.json", []byte(`{
  "name": "@valon-technologies/gestalt",
  "version": "0.0.1-alpha.test"
}
`), 0o644)
	writeTestFile(t, root, "bun.lock", []byte("{}\n"), 0o644)
	writeTestFile(t, filepath.Join(root, "src"), "runtime.ts", []byte("export {};\n"), 0o644)
	writeTestFile(t, filepath.Join(root, "src"), "build.ts", []byte("export {};\n"), 0o644)
	return root
}

func writeFakeTypeScriptProviderReleaseBun(t *testing.T, expectedPluginName, expectedTarget, expectedGOOS, expectedGOARCH string) string {
	t.Helper()

	sdkPath := strings.TrimSpace(os.Getenv("GESTALT_TYPESCRIPT_SDK_DIR"))
	if sdkPath == "" {
		t.Fatal("GESTALT_TYPESCRIPT_SDK_DIR must be set for TypeScript release tests")
	}

	sourceDir := "."
	return fakebun.NewExecutable(t, fakebun.Config{
		Install: &fakebun.InstallConfig{
			ExpectedCwd:           sdkPath,
			RequireFrozenLockfile: true,
		},
		Runtime: &fakebun.RuntimeConfig{
			ExpectedCwd:      sdkPath,
			ExpectedEntry:    filepath.Join(sdkPath, "src", "runtime.ts"),
			ExpectedRoot:     sourceDir,
			ExpectedTarget:   expectedTarget,
			RequireAnyOutput: true,
			Catalog: `name: ` + typeScriptReleasePluginName + `
operations:
  - id: greet
    method: GET
`,
			ManifestMetadata: `securitySchemes:
  signed:
    type: hmac
    secret:
      env: REQUEST_SIGNING_SECRET
    signatureHeader: X-Request-Signature
    signaturePrefix: v0=
    payloadTemplate: "v0:{header:X-Request-Timestamp}:{raw_body}"
    timestampHeader: X-Request-Timestamp
    maxAgeSeconds: 300
http:
  command:
    path: /command
    method: POST
    security: signed
    target: greet
    requestBody:
      required: true
      content:
        application/x-www-form-urlencoded: {}
    ack:
      status: 200
      body:
        status: accepted
`,
		},
		Build: &fakebun.BuildConfig{
			ExpectedCwd:        sdkPath,
			ExpectedEntry:      filepath.Join(sdkPath, "src", "build.ts"),
			ExpectedSourceDir:  sourceDir,
			ExpectedTarget:     expectedTarget,
			ExpectedPluginName: expectedPluginName,
			AllowedPlatforms: []fakebun.Platform{{
				GOOS:   expectedGOOS,
				GOARCH: expectedGOARCH,
			}},
			BinaryContent: "#!/bin/sh\n# fake ts release binary\nexit 0\n",
		},
	})
}

func writeFakePythonReleaseInterpreter(t *testing.T, path, expectedGOOS, expectedGOARCH string) {
	t.Helper()
	writeFakePythonReleaseInterpreterForKind(
		t,
		path,
		"provider",
		"integration",
		"python-release",
		expectedGOOS,
		expectedGOARCH,
	)
}

func writeFakePythonReleaseInterpreterForKind(
	t *testing.T,
	path string,
	expectedTarget string,
	expectedRuntimeKind string,
	expectedName string,
	expectedGOOS string,
	expectedGOARCH string,
) {
	t.Helper()

	script := `#!/bin/sh
set -eu

if [ "$#" -ge 2 ] && [ "$1" = "-m" ] && [ "$2" = "gestalt._build" ]; then
  if [ -z "${GESTALT_TEST_PYINSTALLER_BINARY:-}" ]; then
    echo "missing GESTALT_TEST_PYINSTALLER_BINARY" >&2
    exit 1
  fi
  if [ "$#" -ne 9 ]; then
    echo "unexpected gestalt._build args: $*" >&2
    exit 1
  fi
  root="$3"
  target="$4"
  output="$5"
  name="$6"
  runtime_kind="$7"
  goos="$8"
  goarch="$9"
  if [ "$target" != "` + expectedTarget + `" ]; then
    echo "unexpected provider target: $target" >&2
    exit 1
  fi
  if [ "$name" != "` + expectedName + `" ]; then
    echo "unexpected plugin name: $name" >&2
    exit 1
  fi
  if [ "$runtime_kind" != "` + expectedRuntimeKind + `" ]; then
    echo "unexpected runtime kind: $runtime_kind" >&2
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
  if [ "$#" -ne 5 ]; then
    echo "unexpected gestalt._runtime args: $*" >&2
    exit 1
  fi
  target="$4"
  runtime_kind="$5"
  if [ "$target" != "` + expectedTarget + `" ]; then
    echo "unexpected runtime target: $target" >&2
    exit 1
  fi
  if [ "$runtime_kind" != "` + expectedRuntimeKind + `" ]; then
    echo "unexpected runtime kind: $runtime_kind" >&2
    exit 1
  fi
  if [ -z "${GESTALT_PLUGIN_WRITE_CATALOG:-}" ] && [ -z "${GESTALT_PLUGIN_WRITE_MANIFEST_METADATA:-}" ]; then
    echo "missing catalog or manifest metadata export path" >&2
    exit 1
  fi
  if [ -n "${GESTALT_PLUGIN_WRITE_CATALOG:-}" ]; then
    cat > "$GESTALT_PLUGIN_WRITE_CATALOG" <<'EOF'
name: python-release
operations:
  - id: greet
    method: GET
EOF
  fi
  if [ -n "${GESTALT_PLUGIN_WRITE_MANIFEST_METADATA:-}" ]; then
    cat > "$GESTALT_PLUGIN_WRITE_MANIFEST_METADATA" <<'EOF'
securitySchemes:
  signed:
    type: hmac
    secret:
      env: REQUEST_SIGNING_SECRET
    signatureHeader: X-Request-Signature
    signaturePrefix: v0=
    payloadTemplate: "v0:{header:X-Request-Timestamp}:{raw_body}"
    timestampHeader: X-Request-Timestamp
    maxAgeSeconds: 300
http:
  command:
    path: /command
    method: POST
    security: signed
    target: greet
    requestBody:
      required: true
      content:
        application/x-www-form-urlencoded: {}
    ack:
      status: 200
      body:
        status: accepted
EOF
  fi
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

func platformArchiveNameForTest(pluginName, version, goos, goarch string) string {
	return fmt.Sprintf("gestalt-plugin-%s_v%s_%s_%s.tar.gz", pluginName, version, goos, goarch)
}

func expectedPythonArchiveNameFor(pluginName, version, goos, goarch string) string {
	return platformArchiveNameForTest(pluginName, version, goos, goarch)
}

func expectedPythonArchiveName(version, goos, goarch string) string {
	return expectedPythonArchiveNameFor("python-release", version, goos, goarch)
}

func expectedRustArchiveName(version, goos, goarch, _ string) string {
	return platformArchiveNameForTest(rustReleasePluginName, version, goos, goarch)
}

func assertExpectedScriptArtifactPlatform(t *testing.T, artifact providermanifestv1.Artifact, goos, goarch string) {
	t.Helper()
	assertArtifactPlatform(t, artifact, goos, goarch)
}

func assertExpectedGoArtifactPlatform(t *testing.T, artifact providermanifestv1.Artifact, goos, goarch, _ string) {
	t.Helper()
	assertArtifactPlatform(t, artifact, goos, goarch)
}

func assertExpectedRustArtifactPlatform(t *testing.T, artifact providermanifestv1.Artifact, goos, goarch, _ string) {
	t.Helper()
	assertArtifactPlatform(t, artifact, goos, goarch)
}

func assertArtifactPlatform(t *testing.T, artifact providermanifestv1.Artifact, goos, goarch string) {
	t.Helper()
	if artifact.OS != goos || artifact.Arch != goarch {
		t.Fatalf("artifact platform = %s/%s, want %s/%s", artifact.OS, artifact.Arch, goos, goarch)
	}
}

func assertExecutableAuthProviderWorks(t *testing.T, command, providerName string, assertSessionTTL, assertExternalJWT bool) {
	t.Helper()

	auth, err := providerhost.NewExecutableAuthenticationProvider(context.Background(), providerhost.AuthenticationExecConfig{
		Command:     command,
		Name:        providerName,
		CallbackURL: "https://gestalt.example.test/api/v1/auth/login/callback",
		SessionKey:  []byte("0123456789abcdef0123456789abcdef"),
	})
	if err != nil {
		t.Fatalf("NewExecutableAuthenticationProvider: %v", err)
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
	if assertSessionTTL {
		if ttlProvider, ok := auth.(interface{ SessionTokenTTL() time.Duration }); !ok || ttlProvider.SessionTokenTTL() != 90*time.Minute {
			t.Fatalf("SessionTokenTTL = %v", ttlProvider)
		}
	}
	if assertExternalJWT {
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
}

func assertReleaseDefaultsToHostPlatform(t *testing.T, manifest *providermanifestv1.Manifest, assertPlatform func(*testing.T, providermanifestv1.Artifact)) {
	t.Helper()

	if len(manifest.Artifacts) != 1 {
		t.Fatalf("artifacts = %+v, want exactly one host-platform artifact", manifest.Artifacts)
	}
	assertPlatform(t, manifest.Artifacts[0])
}

func assertReleasedManifestHasHostedHTTPMetadata(t *testing.T, manifest *providermanifestv1.Manifest, target string) {
	t.Helper()

	if manifest == nil || manifest.Spec == nil {
		t.Fatalf("manifest = %+v, want populated spec", manifest)
		return
	}

	scheme := manifest.Spec.SecuritySchemes["signed"]
	if scheme == nil {
		t.Fatal(`manifest.Spec.SecuritySchemes["signed"] = nil, want generated scheme`)
		return
	}
	if scheme.Type != providermanifestv1.HTTPSecuritySchemeTypeHMAC {
		t.Fatalf("scheme.Type = %q, want %q", scheme.Type, providermanifestv1.HTTPSecuritySchemeTypeHMAC)
	}
	if scheme.Secret == nil || scheme.Secret.Env != "REQUEST_SIGNING_SECRET" {
		t.Fatalf("scheme.Secret = %+v, want env-backed secret", scheme.Secret)
		return
	}
	if scheme.SignatureHeader != "X-Request-Signature" {
		t.Fatalf("scheme.SignatureHeader = %q, want %q", scheme.SignatureHeader, "X-Request-Signature")
	}
	if scheme.SignaturePrefix != "v0=" {
		t.Fatalf("scheme.SignaturePrefix = %q, want %q", scheme.SignaturePrefix, "v0=")
	}
	if scheme.PayloadTemplate != "v0:{header:X-Request-Timestamp}:{raw_body}" {
		t.Fatalf("scheme.PayloadTemplate = %q, want %q", scheme.PayloadTemplate, "v0:{header:X-Request-Timestamp}:{raw_body}")
	}
	if scheme.TimestampHeader != "X-Request-Timestamp" {
		t.Fatalf("scheme.TimestampHeader = %q, want %q", scheme.TimestampHeader, "X-Request-Timestamp")
	}
	if scheme.MaxAgeSeconds != 300 {
		t.Fatalf("scheme.MaxAgeSeconds = %d, want %d", scheme.MaxAgeSeconds, 300)
	}

	binding := manifest.Spec.HTTP["command"]
	if binding == nil {
		t.Fatal(`manifest.Spec.HTTP["command"] = nil, want generated HTTP binding`)
		return
	}
	if binding.Path != "/command" {
		t.Fatalf("binding.Path = %q, want %q", binding.Path, "/command")
	}
	if binding.Method != http.MethodPost {
		t.Fatalf("binding.Method = %q, want %q", binding.Method, http.MethodPost)
	}
	if binding.Security != "signed" {
		t.Fatalf("binding.Security = %q, want %q", binding.Security, "signed")
	}
	if binding.Target != target {
		t.Fatalf("binding.Target = %q, want %q", binding.Target, target)
	}
	if binding.RequestBody == nil {
		t.Fatal("binding.RequestBody = nil, want form request body metadata")
	}
	if _, ok := binding.RequestBody.Content["application/x-www-form-urlencoded"]; !ok {
		t.Fatalf("binding.RequestBody.Content = %#v, want form content type", binding.RequestBody.Content)
	}
	if binding.Ack == nil {
		t.Fatal("binding.Ack = nil, want hosted HTTP ack metadata")
	}
	if binding.Ack.Status != http.StatusOK {
		t.Fatalf("binding.Ack.Status = %d, want %d", binding.Ack.Status, http.StatusOK)
	}
	body, ok := binding.Ack.Body.(map[string]any)
	if !ok {
		t.Fatalf("binding.Ack.Body type = %T, want map[string]any", binding.Ack.Body)
	}
	if got := body["status"]; got != "accepted" {
		t.Fatalf("binding.Ack.Body[status] = %#v, want %#v", got, "accepted")
	}
}

func assertReleasePlatforms(
	t *testing.T,
	outputDir string,
	platforms []releasePlatform,
	archiveName func(releasePlatform) string,
	assertPlatform func(*testing.T, providermanifestv1.Artifact, releasePlatform),
) {
	t.Helper()

	for _, platform := range platforms {
		manifest := readReleasedManifest(t, outputDir, archiveName(platform))
		if len(manifest.Artifacts) != 1 {
			t.Fatalf("artifacts for %s/%s = %+v, want one artifact", platform.GOOS, platform.GOARCH, manifest.Artifacts)
		}
		assertPlatform(t, manifest.Artifacts[0], platform)
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
		t.Setenv(providerpkgPythonEnvVar(platform.GOOS, platform.GOARCH), interpreterPath)
	}
}

func providerpkgPythonEnvVar(goos, goarch string) string {
	replacer := strings.NewReplacer("-", "_", ".", "_", "/", "_")
	return "GESTALT_PYTHON_" + strings.ToUpper(replacer.Replace(goos)) + "_" + strings.ToUpper(replacer.Replace(goarch))
}

func writeManagedPluginConfigForTest(t *testing.T, dir, pluginKey, metadataURL, mountPath string) string {
	t.Helper()

	indexedDBManifest := writeStubIndexedDBManifestForTest(t, dir)
	externalCredentialsManifest := componentProviderManifestPath(t, setupExternalCredentialsProviderDir(t, dir))
	configData := fmt.Sprintf(`apiVersion: %s
providers:
  externalCredentials:
    default:
      source:
        path: %q
  indexeddb:
    sqlite:
      source:
        path: %q
      config:
        path: %q
plugins:
  %s:
    source: %q
    mountPath: %q
server:
  providers:
    externalCredentials: default
    indexeddb: sqlite
  artifactsDir: %q
  encryptionKey: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`, config.APIVersionV3, externalCredentialsManifest, indexedDBManifest, filepath.Join(dir, "gestalt.db"), pluginKey, metadataURL, mountPath, filepath.Join(dir, "prepared-artifacts"))
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configData), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath
}

func writeStubIndexedDBManifestForTest(t *testing.T, dir string) string {
	t.Helper()

	artifactPath := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "indexeddb"))
	artifactFullPath := filepath.Join(dir, filepath.FromSlash(artifactPath))
	if err := os.MkdirAll(filepath.Dir(artifactFullPath), 0o755); err != nil {
		t.Fatalf("mkdir indexeddb artifact dir: %v", err)
	}
	artifactContent := []byte("indexeddb-stub-binary")
	if err := os.WriteFile(artifactFullPath, artifactContent, 0o755); err != nil {
		t.Fatalf("write indexeddb artifact: %v", err)
	}
	artifactSum := sha256.Sum256(artifactContent)
	manifestPath := filepath.Join(dir, "indexeddb-manifest.yaml")
	data, err := providerpkg.EncodeSourceManifestFormat(&providermanifestv1.Manifest{
		Source:     "github.com/test/providers/indexeddb-stub",
		Version:    "0.0.1-alpha.1",
		Kind:       providermanifestv1.KindIndexedDB,
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: artifactPath},
		Artifacts: []providermanifestv1.Artifact{{
			OS:     runtime.GOOS,
			Arch:   runtime.GOARCH,
			Path:   artifactPath,
			SHA256: hex.EncodeToString(artifactSum[:]),
		}},
		Spec: &providermanifestv1.Spec{},
	}, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("encode indexeddb manifest: %v", err)
	}
	if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
		t.Fatalf("write indexeddb manifest: %v", err)
	}
	return manifestPath
}

type sourceComponentReleaseFixtureParams struct {
	pluginName string
	schemaPath string
	sourceFile string
	sourceCode string
	manifest   *providermanifestv1.Manifest
}

func newSourceComponentReleaseFixture(t *testing.T, dir string, p sourceComponentReleaseFixtureParams) string {
	t.Helper()

	pluginDir := filepath.Join(dir, p.pluginName)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(pluginDir): %v", err)
	}
	writeTestFile(t, pluginDir, "go.mod", []byte(testutil.GeneratedProviderModuleSource(t, "example.com/"+p.pluginName)), 0o644)
	writeTestFile(t, pluginDir, "go.sum", testutil.GeneratedProviderModuleSum(t), 0o644)
	writeTestFile(t, pluginDir, p.sourceFile, []byte(p.sourceCode), 0o644)
	writeReleaseTestManifest(t, pluginDir, p.manifest)
	writeTestFile(t, pluginDir, p.schemaPath, []byte(`{"type":"object"}`), 0o644)
	return pluginDir
}

func newRustSourceAuthReleaseFixture(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := filepath.Join(dir, authReleasePluginName)
	copyFixtureTree(t, rustAuthProviderFixturePath(t), pluginDir)
	writeReleaseTestManifest(t, pluginDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindAuthentication,
		Source:      authReleaseSource,
		Version:     "0.0.1",
		DisplayName: "Auth Release",
		Spec: &providermanifestv1.Spec{
			ConfigSchemaPath: authReleaseSchemaPath,
		},
	})
	writeTestFile(t, pluginDir, authReleaseSchemaPath, []byte(`{"type":"object"}`), 0o644)
	return pluginDir
}

func buildGoSourceComponentBinaryForTest(t *testing.T, pluginDir, kind string) string {
	t.Helper()

	outputDir := t.TempDir()
	outputPath := filepath.Join(outputDir, releaseBinaryName(filepath.Base(pluginDir), runtime.GOOS))
	if _, err := providerpkg.BuildSourceComponentReleaseBinary(pluginDir, outputPath, kind, runtime.GOOS, runtime.GOARCH); err != nil {
		t.Fatalf("BuildSourceComponentReleaseBinary(%s): %v", kind, err)
	}
	return outputPath
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
	writeReleaseTestManifest(t, pluginDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindPlugin,
		Source:      releaseTestSource,
		Version:     "0.0.1",
		DisplayName: "Release Test",
		IconFile:    releaseTestIconPath,
		Spec: &providermanifestv1.Spec{
			ConfigSchemaPath: releaseProviderSchemaPath,
		},
	})
	writeTestFile(t, pluginDir, releaseTestIconPath, []byte("<svg></svg>\n"), 0644)
	writeTestFile(t, pluginDir, releaseProviderSchemaPath, []byte(`{"type":"object"}`), 0644)
	return pluginDir
}

func newBuiltSourceProviderReleaseFixture(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := newSourceProviderReleaseFixture(t, dir)
	if err := os.Remove(filepath.Join(pluginDir, releaseProviderSchemaPath)); err != nil {
		t.Fatalf("Remove(%s): %v", releaseProviderSchemaPath, err)
	}
	addReleaseBuild(t, pluginDir, filepath.Join("scripts", "build.sh"), "", "mkdir -p schemas\nprintf '{\"type\":\"object\"}\\n' > "+releaseProviderSchemaPath+"\n")
	return pluginDir
}

func newGoSourceReleaseFixture(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := filepath.Join(dir, releaseTestPluginName)
	testutil.CopyExampleProviderPlugin(t, pluginDir)
	writeReleaseTestManifest(t, pluginDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindPlugin,
		Source:      releaseTestSource,
		Version:     "0.0.1",
		DisplayName: "Release Test",
		Spec: &providermanifestv1.Spec{
			Connections: map[string]*providermanifestv1.ManifestConnectionDef{
				"default": {
					Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeNone},
				},
			},
		},
	})
	injectGoManifestMetadata(t, filepath.Join(pluginDir, "provider.go"))
	return pluginDir
}

func newTypeScriptSourceReleaseFixture(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := filepath.Join(dir, typeScriptReleasePluginName)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(pluginDir): %v", err)
	}
	writeReleaseTestManifest(t, pluginDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindPlugin,
		Source:      typeScriptReleaseSource,
		Version:     "0.0.1",
		DisplayName: "TypeScript Release",
		Spec: &providermanifestv1.Spec{
			Connections: map[string]*providermanifestv1.ManifestConnectionDef{
				"default": {
					Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeNone},
				},
			},
		},
	})
	if err := os.Remove(filepath.Join(pluginDir, providerpkg.StaticCatalogFile)); err != nil {
		t.Fatalf("Remove(%s): %v", providerpkg.StaticCatalogFile, err)
	}
	writeTestFile(t, pluginDir, "package.json", []byte(`{
  "name": "ts-release",
  "version": "0.0.1",
  "gestalt": {
    "provider": "`+typeScriptReleaseModule+`"
  }
}
`), 0o644)
	writeTestFile(t, pluginDir, "provider.ts", []byte(`import { definePlugin } from "@valon-technologies/gestalt";

export const provider = definePlugin({
  securitySchemes: {
    signed: {
      type: "hmac",
      secret: {
        env: "REQUEST_SIGNING_SECRET",
      },
      signatureHeader: "X-Request-Signature",
      signaturePrefix: "v0=",
      payloadTemplate: "v0:{header:X-Request-Timestamp}:{raw_body}",
      timestampHeader: "X-Request-Timestamp",
      maxAgeSeconds: 300,
    },
  },
  http: {
    command: {
      path: "/command",
      method: "POST",
      security: "signed",
      target: "greet",
      requestBody: {
        required: true,
        content: {
          "application/x-www-form-urlencoded": {},
        },
      },
      ack: {
        status: 200,
        body: {
          status: "accepted",
        },
      },
    },
  },
  operations: [
    {
      id: "greet",
      method: "GET",
      readOnly: true,
      handler(input: { name?: string }) {
        return {
          message: "Hello, " + (input.name || "World") + "!",
        };
      },
    },
  ],
});
`), 0o644)
	return pluginDir
}

func newDeclarativeProviderReleaseFixture(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := filepath.Join(dir, declarativeReleasePluginName)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(pluginDir): %v", err)
	}
	writeReleaseTestManifest(t, pluginDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindPlugin,
		Source:      declarativeReleaseSource,
		Version:     "0.0.1",
		DisplayName: "Declarative Release",
		Spec: &providermanifestv1.Spec{
			Surfaces: &providermanifestv1.ProviderSurfaces{
				REST: &providermanifestv1.RESTSurface{
					BaseURL: "https://api.example.test",
					Operations: []providermanifestv1.ProviderOperation{
						{
							Name:   "list_widgets",
							Method: "GET",
							Path:   "/widgets",
						},
					},
				},
			},
		},
	})
	return pluginDir
}

func newSourceProviderReleaseFixtureWithoutCatalog(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := newSourceProviderReleaseFixture(t, dir)
	_ = os.Remove(filepath.Join(pluginDir, providerpkg.StaticCatalogFile))

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

func injectGoManifestMetadata(t *testing.T, providerPath string) {
	t.Helper()

	data, err := os.ReadFile(providerPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", providerPath, err)
	}
	old := "\t)\n)"
	new := "\t).WithManifestMetadata(gestalt.ManifestMetadata{\n\t\tSecuritySchemes: map[string]gestalt.HTTPSecurityScheme{\n\t\t\t\"signed\": {\n\t\t\t\tType:            gestalt.HTTPSecuritySchemeTypeHMAC,\n\t\t\t\tSecret:          &gestalt.HTTPSecretRef{Env: \"REQUEST_SIGNING_SECRET\"},\n\t\t\t\tSignatureHeader: \"X-Request-Signature\",\n\t\t\t\tSignaturePrefix: \"v0=\",\n\t\t\t\tPayloadTemplate: \"v0:{header:X-Request-Timestamp}:{raw_body}\",\n\t\t\t\tTimestampHeader: \"X-Request-Timestamp\",\n\t\t\t\tMaxAgeSeconds:   300,\n\t\t\t},\n\t\t},\n\t\tHTTP: map[string]gestalt.HTTPBinding{\n\t\t\t\"command\": {\n\t\t\t\tPath:     \"/command\",\n\t\t\t\tMethod:   http.MethodPost,\n\t\t\t\tSecurity: \"signed\",\n\t\t\t\tTarget:   \"echo\",\n\t\t\t\tRequestBody: &gestalt.HTTPRequestBody{\n\t\t\t\t\tRequired: true,\n\t\t\t\t\tContent: map[string]gestalt.HTTPMediaType{\n\t\t\t\t\t\t\"application/x-www-form-urlencoded\": {},\n\t\t\t\t\t},\n\t\t\t\t},\n\t\t\t\tAck: &gestalt.HTTPAck{\n\t\t\t\t\tStatus: 200,\n\t\t\t\t\tBody: map[string]any{\n\t\t\t\t\t\t\"status\": \"accepted\",\n\t\t\t\t\t},\n\t\t\t\t},\n\t\t\t},\n\t\t},\n\t})\n)"
	updated := strings.Replace(string(data), old, new, 1)
	if updated == string(data) {
		t.Fatalf("provider fixture %s missing router terminator %q", providerPath, old)
	}
	if err := os.WriteFile(providerPath, []byte(updated), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", providerPath, err)
	}
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
	if err := providerpkg.BuildGoComponentBinary(providerDir, outputPath, providermanifestv1.KindAuthentication, runtime.GOOS, runtime.GOARCH); err != nil {
		t.Fatalf("BuildGoComponentBinary(auth): %v", err)
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
	ExpectedPluginName        string
	ExpectedServeExport       string
	ExpectedCatalogWrite      bool
	GeneratedCatalog          string
	GeneratedManifestMetadata string
	DelegateBinary            string
	AllowedTargets            []string
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
if [ -n "${GESTALT_PLUGIN_WRITE_CATALOG:-}" ] || [ -n "${GESTALT_PLUGIN_WRITE_MANIFEST_METADATA:-}" ]; then
  if [ -n "${GESTALT_PLUGIN_WRITE_CATALOG:-}" ]; then
  cat > "$GESTALT_PLUGIN_WRITE_CATALOG" <<'YAML'
name: ` + cfg.GeneratedCatalog + `
operations:
  - id: greet
    method: GET
YAML
  fi
` + func() string {
		if strings.TrimSpace(cfg.GeneratedManifestMetadata) == "" {
			return ""
		}
		return `  if [ -n "${GESTALT_PLUGIN_WRITE_MANIFEST_METADATA:-}" ]; then
  cat > "$GESTALT_PLUGIN_WRITE_MANIFEST_METADATA" <<'YAML'
` + cfg.GeneratedManifestMetadata + `
YAML
  fi
`
	}() + `
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

func providerpkgRustTargetTriple(goos, goarch, _ string) (string, string, error) {
	switch goos {
	case "darwin":
		switch goarch {
		case "amd64":
			return "x86_64-apple-darwin", "", nil
		case "arm64":
			return "aarch64-apple-darwin", "", nil
		}
	case "linux":
		switch goarch {
		case "amd64":
			return "x86_64-unknown-linux-musl", "", nil
		case "arm64":
			return "aarch64-unknown-linux-musl", "", nil
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
	writeReleaseTestManifest(t, pluginDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindPlugin,
		Source:      prebuiltProviderSource,
		Version:     "0.0.1",
		DisplayName: "Prebuilt Provider",
		IconFile:    releaseTestIconPath,
		Spec: &providermanifestv1.Spec{
			ConfigSchemaPath: releaseProviderSchemaPath,
		},
		Artifacts: []providermanifestv1.Artifact{
			{
				OS:   runtime.GOOS,
				Arch: runtime.GOARCH,
				Path: prebuiltProviderBinaryPath,
			},
		},
		Entrypoint: &providermanifestv1.Entrypoint{
			ArtifactPath: prebuiltProviderBinaryPath,
		},
	})
	writeTestFile(t, pluginDir, releaseProviderSchemaPath, []byte(`{"type":"object"}`), 0o644)
	return pluginDir
}

func newUIReleaseFixture(t *testing.T, dir string) string {
	return newUIReleaseFixtureWithAssetRoot(t, dir, uiTestAssetRoot)
}

func newBuiltUIReleaseFixture(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := filepath.Join(dir, uiTestPluginName)
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("MkdirAll(pluginDir): %v", err)
	}
	writeReleaseTestManifest(t, pluginDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindUI,
		Source:      uiTestSource,
		Version:     "0.0.1",
		DisplayName: "UI Test",
		IconFile:    releaseTestIconPath,
		Release: &providermanifestv1.ReleaseMetadata{
			Build: &providermanifestv1.ReleaseBuild{
				Workdir: "ui",
				Command: []string{"sh", "./build.sh"},
			},
		},
		Spec: &providermanifestv1.Spec{
			AssetRoot: "ui/out",
		},
	})
	writeTestFile(t, pluginDir, releaseTestIconPath, []byte("<svg></svg>\n"), 0644)
	writeReleaseBuildScript(t, pluginDir, filepath.Join("ui", "build.sh"), "mkdir -p out/static\nprintf '<html></html>\\n' > out/index.html\nprintf 'console.log(\"ok\")\\n' > out/static/app.js\n")
	return pluginDir
}

func newSourceProviderReleaseFixtureWithOwnedUI(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := newSourceProviderReleaseFixture(t, dir)
	uiDir := filepath.Join(dir, "roadmap-ui")
	if err := os.MkdirAll(uiDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(uiDir): %v", err)
	}
	writeReleaseTestManifest(t, uiDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindUI,
		Source:      "github.com/testowner/web/roadmap-ui",
		Version:     "0.0.1",
		DisplayName: "Roadmap UI",
		IconFile:    releaseTestIconPath,
		Spec: &providermanifestv1.Spec{
			AssetRoot: "dist",
		},
	})
	writeTestFile(t, uiDir, releaseTestIconPath, []byte("<svg></svg>\n"), 0o644)
	writeTestFile(t, uiDir, "dist/index.html", []byte("<html>roadmap</html>\n"), 0o644)
	writeTestFile(t, uiDir, "dist/static/app.js", []byte("console.log('roadmap')\n"), 0o644)

	manifestPath := filepath.Join(pluginDir, providerpkg.ManifestFile)
	_, manifest, err := providerpkg.ReadSourceManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadSourceManifestFile(%s): %v", providerpkg.ManifestFile, err)
	}
	manifest.Spec.UI = &providermanifestv1.OwnedUI{Path: "../roadmap-ui/" + providerpkg.ManifestFile}
	writeReleaseTestManifest(t, pluginDir, manifest)

	return pluginDir
}

func newSourceProviderReleaseFixtureWithBuiltOwnedUI(t *testing.T, dir string) string {
	t.Helper()

	pluginDir := newSourceProviderReleaseFixture(t, dir)
	uiDir := filepath.Join(dir, "roadmap-ui")
	if err := os.MkdirAll(uiDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(uiDir): %v", err)
	}
	writeReleaseTestManifest(t, uiDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindUI,
		Source:      "github.com/testowner/web/roadmap-ui",
		Version:     "0.0.1",
		DisplayName: "Roadmap UI",
		IconFile:    releaseTestIconPath,
		Release: &providermanifestv1.ReleaseMetadata{
			Build: &providermanifestv1.ReleaseBuild{
				Workdir: "ui",
				Command: []string{"sh", "./build.sh"},
			},
		},
		Spec: &providermanifestv1.Spec{
			AssetRoot: "ui/dist",
		},
	})
	writeTestFile(t, uiDir, releaseTestIconPath, []byte("<svg></svg>\n"), 0o644)
	writeReleaseBuildScript(t, uiDir, filepath.Join("ui", "build.sh"), "mkdir -p dist/static\nprintf '<html>roadmap</html>\\n' > dist/index.html\nprintf 'console.log(\"roadmap\")\\n' > dist/static/app.js\n")

	manifestPath := filepath.Join(pluginDir, providerpkg.ManifestFile)
	_, manifest, err := providerpkg.ReadSourceManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadSourceManifestFile(%s): %v", providerpkg.ManifestFile, err)
	}
	manifest.Spec.UI = &providermanifestv1.OwnedUI{Path: "../roadmap-ui/" + providerpkg.ManifestFile}
	writeReleaseTestManifest(t, pluginDir, manifest)

	return pluginDir
}

func newUIReleaseFixtureWithAssetRoot(t *testing.T, dir, assetRoot string) string {
	t.Helper()

	pluginDir := filepath.Join(dir, uiTestPluginName)
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("MkdirAll(pluginDir): %v", err)
	}
	writeReleaseTestManifest(t, pluginDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindUI,
		Source:      uiTestSource,
		Version:     "0.0.1",
		DisplayName: "UI Test",
		IconFile:    releaseTestIconPath,
		Spec: &providermanifestv1.Spec{
			AssetRoot: assetRoot,
		},
	})
	writeTestFile(t, pluginDir, releaseTestIconPath, []byte("<svg></svg>\n"), 0644)
	writeTestFile(t, pluginDir, assetRoot+"/index.html", []byte("<html></html>\n"), 0644)
	writeTestFile(t, pluginDir, assetRoot+"/static/app.js", []byte("console.log('ok')\n"), 0644)
	return pluginDir
}

func addReleaseBuild(t *testing.T, pluginDir, scriptPath, workdir, body string) {
	t.Helper()

	_, manifest, err := providerpkg.ReadSourceManifestFile(filepath.Join(pluginDir, providerpkg.ManifestFile))
	if err != nil {
		t.Fatalf("ReadSourceManifestFile(%s): %v", providerpkg.ManifestFile, err)
	}
	manifest.Release = &providermanifestv1.ReleaseMetadata{
		Build: &providermanifestv1.ReleaseBuild{
			Workdir: workdir,
			Command: []string{"sh", "./" + filepath.ToSlash(scriptPath)},
		},
	}
	writeReleaseTestManifest(t, pluginDir, manifest)
	writeReleaseBuildScript(t, pluginDir, scriptPath, body)
}

func writeReleaseBuildScript(t *testing.T, dir, rel, body string) {
	t.Helper()

	writeTestFile(t, dir, rel, []byte("#!/bin/sh\nset -eu\n"+body), 0o755)
}

func runProviderReleaseCommand(t *testing.T, pluginDir string, args ...string) string {
	t.Helper()

	out, err := runProviderReleaseCommandResult(pluginDir, args...)
	if err != nil {
		t.Fatalf("provider release failed: %v\n%s", err, out)
	}
	return string(out)
}

func runProviderCommandResult(pluginDir string, args ...string) ([]byte, error) {
	cmdArgs := append([]string{"provider"}, args...)
	cmd := exec.Command(gestaltdBin, cmdArgs...)
	cmd.Dir = pluginDir
	return cmd.CombinedOutput()
}

func runProviderReleaseCommandResult(pluginDir string, args ...string) ([]byte, error) {
	return runProviderCommandResult(pluginDir, append([]string{"release"}, args...)...)
}

func extractReleasedArchive(t *testing.T, outputDir, archiveName string) string {
	t.Helper()

	archivePath := filepath.Join(outputDir, archiveName)
	if _, err := os.Stat(archivePath); err != nil {
		t.Fatalf("expected archive %s to exist: %v", archiveName, err)
	}
	extractDir := filepath.Join(outputDir, strings.TrimSuffix(archiveName, ".tar.gz"))
	if err := providerpkg.ExtractPackage(archivePath, extractDir); err != nil {
		t.Fatalf("extract archive: %v", err)
	}
	return extractDir
}

func readReleasedManifest(t *testing.T, outputDir, archiveName string) *providermanifestv1.Manifest {
	t.Helper()

	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifestPath, err := providerpkg.FindManifestFile(extractDir)
	if err != nil {
		t.Fatalf("find released manifest: %v", err)
	}
	_, manifest, err := providerpkg.ReadManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("read released manifest: %v", err)
	}
	return manifest
}

func readProviderReleaseMetadata(t *testing.T, outputDir string) *providerReleaseMetadata {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(outputDir, providerReleaseMetadataFile))
	if err != nil {
		t.Fatalf("read %s: %v", providerReleaseMetadataFile, err)
	}
	var metadata providerReleaseMetadata
	if err := yaml.Unmarshal(data, &metadata); err != nil {
		t.Fatalf("decode %s: %v", providerReleaseMetadataFile, err)
	}
	return &metadata
}

func readManifestFromDir(t *testing.T, dir string) (string, *providermanifestv1.Manifest) {
	t.Helper()

	manifestPath, err := providerpkg.FindManifestFile(dir)
	if err != nil {
		t.Fatalf("find manifest: %v", err)
	}
	_, manifest, err := providerpkg.ReadManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	return manifestPath, manifest
}

func writeReleaseTestManifest(t *testing.T, dir string, manifest *providermanifestv1.Manifest) {
	t.Helper()
	writeReleaseTestManifestFormat(t, dir, providerpkg.ManifestFile, manifest)
}

func writeReleaseTestManifestFormat(t *testing.T, dir, manifestFile string, manifest *providermanifestv1.Manifest) {
	t.Helper()
	populateMissingArtifactDigests(t, dir, manifest)
	data, err := encodeTestManifestFormat(manifest, providerpkg.ManifestFormatFromPath(manifestFile))
	if err != nil {
		t.Fatalf("encodeTestManifestFormat(%s): %v", manifestFile, err)
	}
	writeTestFile(t, dir, manifestFile, data, 0644)
	if manifest.Kind == providermanifestv1.KindPlugin && manifest.Spec != nil {
		writeTestFile(t, dir, providerpkg.StaticCatalogFile, []byte("name: provider\noperations:\n  - id: echo\n    method: POST\n"), 0644)
	}
}

func populateMissingArtifactDigests(t *testing.T, dir string, manifest *providermanifestv1.Manifest) {
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

func encodeTestManifestFormat(manifest *providermanifestv1.Manifest, format string) ([]byte, error) {
	return providerpkg.EncodeSourceManifestFormat(manifest, format)
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
