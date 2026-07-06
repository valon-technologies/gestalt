package e2e

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

func TestRun_ProviderPackageAndReleaseStagesAppStaticBundle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		fixture   func(*testing.T, string) string
		skipOnWin bool
	}{
		{
			name:    "checked-in static bundle with build command",
			fixture: newSourceProviderReleaseFixtureWithStaticBundle,
		},
		{
			name:      "source-built static bundle",
			fixture:   newSourceBuiltStaticAppReleaseFixture,
			skipOnWin: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.skipOnWin && runtime.GOOS == "windows" {
				t.Skip("static bundle release-build fixture uses POSIX shell")
			}

			pluginDir := tc.fixture(t, t.TempDir())
			outputDir := t.TempDir()
			testVersion := "0.0.3-static-bundle"

			runProviderPackageAndReleaseCommand(t, pluginDir,
				"--version", testVersion,
				"--platform", runtime.GOOS+"/"+runtime.GOARCH,
				"--output", outputDir,
			)

			archiveName := staticBuildArchiveName(testVersion)
			extractDir := extractReleasedArchive(t, outputDir, archiveName)
			manifest := readReleasedManifest(t, outputDir, archiveName)
			if manifest.Spec == nil || manifest.Spec.AssetRoot != "static" {
				t.Fatalf("released manifest spec.assetRoot = %+v, want %q", manifest.Spec, "static")
			}
			for _, rel := range []string{"branding/icon.svg", "static/index.html", "static/assets/app.js"} {
				if _, err := os.Stat(filepath.Join(extractDir, filepath.FromSlash(rel))); err != nil {
					t.Fatalf("expected %s in archive: %v", rel, err)
				}
			}
			metadata := readProviderReleaseMetadata(t, outputDir)
			if metadata.Package != uiTestSource {
				t.Fatalf("release metadata package = %q, want %q", metadata.Package, uiTestSource)
			}
			if metadata.Kind != providermanifestv1.KindApp {
				t.Fatalf("release metadata kind = %q, want %q", metadata.Kind, providermanifestv1.KindApp)
			}
			if metadata.Version != testVersion {
				t.Fatalf("release metadata version = %q, want %q", metadata.Version, testVersion)
			}
			if len(metadata.Artifacts) != 1 {
				t.Fatalf("release metadata artifacts = %+v, want 1 entry", metadata.Artifacts)
			}
			artifact := providerReleaseArtifactForTarget(t, metadata, providerrelease.GenericTarget)
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
		})
	}
}

func TestRun_ProviderPackageAndReleaseBuildsProviderSupportFilesBeforePackaging(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("release build fixture uses POSIX shell")
	}

	pluginDir := newBuiltSourceProviderReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.3-build-provider"

	runProviderPackageAndReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", runtime.GOOS+"/"+runtime.GOARCH,
		"--output", outputDir,
	)

	archiveName := "gestalt-app-release-test_v" + testVersion + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	_ = readReleasedManifest(t, outputDir, archiveName)
	if _, err := os.Stat(filepath.Join(extractDir, releaseProviderSchemaPath)); err != nil {
		t.Fatalf("expected %s in archive: %v", releaseProviderSchemaPath, err)
	}
}

func TestRun_ProviderPackageRejectsDeletedBuild(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("legacy release-build fixture uses POSIX shell")
	}

	pluginDir := newBuiltUIReleaseFixture(t, t.TempDir())
	out, err := runProviderPackageAndReleaseCommandResult(pluginDir,
		"--version", "0.0.3-legacy-build-ui",
		"--output", t.TempDir(),
	)
	if err == nil {
		t.Fatalf("expected provider release to reject deleted release field\n%s", out)
	}
	if !strings.Contains(string(out), "unknown field") {
		t.Fatalf("expected deleted release field rejection, got: %s", out)
	}
}

func TestRun_ProviderPackageAndReleaseBuildsSourceStaticAssetsBeforePackaging(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("source build fixture uses POSIX shell")
	}

	pluginDir := newSourceBuiltUIReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.3-source-build-static"

	runProviderPackageAndReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", runtime.GOOS+"/"+runtime.GOARCH,
		"--output", outputDir,
	)

	archiveName := staticBuildArchiveName(testVersion)
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifest := readReleasedManifest(t, outputDir, archiveName)
	if manifest.Build != nil {
		t.Fatalf("released manifest unexpectedly retained build metadata: %+v", manifest.Build)
	}
	if manifest.Spec == nil || manifest.Spec.AssetRoot != "static" {
		t.Fatalf("released manifest spec.assetRoot = %+v, want static", manifest.Spec)
	}
	for _, rel := range []string{
		"branding/icon.svg",
		"static/index.html",
		"static/assets/app.js",
	} {
		if _, err := os.Stat(filepath.Join(extractDir, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("expected %s in archive: %v", rel, err)
		}
	}
}

func TestRun_ProviderPackageAndReleaseAllowsOverlappingSupportPaths(t *testing.T) {
	t.Parallel()

	pluginDir := filepath.Join(t.TempDir(), "ui-overlap")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(pluginDir): %v", err)
	}
	writeReleaseTestManifest(t, pluginDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindApp,
		Source:      "github.com/testowner/apps/ui-overlap",
		Version:     "0.0.1",
		DisplayName: "Static Overlap",
		IconFile:    "out/icon.svg",
		Spec:        declarativeRESTSpec(),
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"build.sh"},
		},
	})
	writeTestFile(t, pluginDir, "out/icon.svg", []byte("<svg></svg>\n"), 0o644)
	writeDeclarativeStaticCatalog(t, pluginDir)
	writeTestFile(t, pluginDir, "build.sh", []byte("mkdir -p \"$GESTALT_BUILD_STATIC\"\nprintf '<svg></svg>\\n' > out/icon.svg\nprintf '<html></html>\\n' > \"$GESTALT_BUILD_STATIC/index.html\"\n"), 0o755)

	outputDir := t.TempDir()
	const testVersion = "0.0.3-overlap.1"

	runProviderPackageAndReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", runtime.GOOS+"/"+runtime.GOARCH,
		"--output", outputDir,
	)

	archiveName := platformArchiveNameForTest("ui-overlap", testVersion, runtime.GOOS, runtime.GOARCH)
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	for _, rel := range []string{"out/icon.svg", "static/index.html"} {
		if _, err := os.Stat(filepath.Join(extractDir, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("expected %s in archive: %v", rel, err)
		}
	}
}

func TestRun_ProviderPackageAndReleaseTreatsGoModWithoutProviderPackageAsDeclarative(t *testing.T) {
	t.Parallel()

	pluginDir := newUIReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	testVersion := "0.0.4-test"

	writeTestFile(t, pluginDir, "go.mod", []byte("module example.com/ui-test\n\ngo 1.22\n"), 0644)

	runProviderPackageAndReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)

	archiveName := "gestalt-app-ui-test_v" + testVersion + ".tar.gz"
	if _, err := os.Stat(filepath.Join(outputDir, archiveName)); err != nil {
		t.Fatalf("expected declarative archive %s to exist: %v", archiveName, err)
	}

	compiledArchiveName := "gestalt-app-ui-test_v" + testVersion + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	if _, err := os.Stat(filepath.Join(outputDir, compiledArchiveName)); !os.IsNotExist(err) {
		t.Fatalf("unexpected compiled archive %s: %v", compiledArchiveName, err)
	}
}

func TestRun_ProviderPackageAndReleaseWritesProviderReleaseMetadataForDeclarativeApp(t *testing.T) {
	t.Parallel()

	pluginDir := newDeclarativeProviderReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.4-declarative.1"

	runProviderPackageAndReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)

	archiveName := "gestalt-app-" + declarativeReleaseAppName + "_v" + testVersion + ".tar.gz"
	if _, err := os.Stat(filepath.Join(outputDir, archiveName)); err != nil {
		t.Fatalf("expected archive %s to exist: %v", archiveName, err)
	}

	metadata := readProviderReleaseMetadata(t, outputDir)
	if metadata.Package != declarativeReleaseSource {
		t.Fatalf("release metadata package = %q, want %q", metadata.Package, declarativeReleaseSource)
	}
	if metadata.Kind != providermanifestv1.KindApp {
		t.Fatalf("release metadata kind = %q, want %q", metadata.Kind, providermanifestv1.KindApp)
	}
	if metadata.Version != testVersion {
		t.Fatalf("release metadata version = %q, want %q", metadata.Version, testVersion)
	}
	if len(metadata.Artifacts) != 1 {
		t.Fatalf("release metadata artifacts = %+v, want 1 entry", metadata.Artifacts)
	}
	artifact := providerReleaseArtifactForTarget(t, metadata, providerrelease.GenericTarget)
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

func TestRun_ProviderPackageAndReleasePreservesYAMLManifestFormatAndConnectionDefaults(t *testing.T) {
	t.Parallel()

	pluginDir := newSourceProviderReleaseFixture(t, t.TempDir())
	writeReleaseTestManifestFormat(t, pluginDir, "manifest.yaml", &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindApp,
		Source:      "github.com/testowner/apps/provider-yaml",
		Version:     "0.0.1",
		DisplayName: "Provider YAML",
		Spec: &providermanifestv1.Spec{
			ConfigSchemaPath: releaseProviderSchemaPath,
			MCP:              true,
			Connections: map[string]*providermanifestv1.ManifestConnectionDef{
				"default": {
					Mode: providermanifestv1.ConnectionModeSubject,
					Params: map[string]providermanifestv1.ProviderConnectionParam{
						"tenant": {Required: true},
					},
				},
			},
		},
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"go.mod", "go.sum", "provider.go", "cmd", "build.sh"},
		},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: "bin/release-test"},
	})
	if err := os.Remove(filepath.Join(pluginDir, providerpkg.ManifestFile)); err != nil {
		t.Fatalf("remove manifest.json: %v", err)
	}
	writeTestFile(t, pluginDir, "build.sh", []byte("mkdir -p .gestaltd/bin\ngo build -o .gestaltd/bin/provider-yaml ./cmd/provider\n"), 0o755)

	outputDir := t.TempDir()
	const testVersion = "0.0.4-yaml.1"

	runProviderPackageAndReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)

	archiveName := "gestalt-app-provider-yaml_v" + testVersion + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifestPath, manifest := readManifestFromDir(t, extractDir)
	if filepath.Base(manifestPath) != "manifest.yaml" {
		t.Fatalf("released manifest = %q, want manifest.yaml", filepath.Base(manifestPath))
	}
	if manifest.Spec == nil || manifest.Spec.Connections["default"] == nil || len(manifest.Spec.Connections["default"].Params) != 1 || !manifest.Spec.Connections["default"].Params["tenant"].Required {
		t.Fatalf("provider connection_params = %+v", manifest.Spec)
	}
	if manifest.Spec.Connections["default"].Mode != providermanifestv1.ConnectionModeSubject {
		t.Fatalf("provider default connection mode = %q, want %q", manifest.Spec.Connections["default"].Mode, providermanifestv1.ConnectionModeSubject)
	}

	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read released manifest: %v", err)
	}
	for _, expected := range []string{
		"spec:",
		"connections:",
		"default:",
		"mode: subject",
		"params:",
		"mcp: true",
		"entrypoint:",
		"artifactPath:",
	} {
		if !strings.Contains(string(manifestData), expected) {
			t.Fatalf("expected released manifest to contain canonical field %q, got: %s", expected, manifestData)
		}
	}
	for _, unsupported := range []string{
		"connectionMode:",
		"connectionParams:",
	} {
		if strings.Contains(string(manifestData), unsupported) {
			t.Fatalf("expected released manifest to emit only canonical connection fields; found %q in: %s", unsupported, manifestData)
		}
	}
}

func TestRun_ProviderPackageRemovesStaleArchivesForSameApp(t *testing.T) {
	t.Parallel()

	pluginDir := newUIReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()

	runProviderPackageAndReleaseCommand(t, pluginDir,
		"--version", "1.0.0",
		"--output", outputDir,
	)
	runProviderPackageAndReleaseCommand(t, pluginDir,
		"--version", "1.0.1",
		"--output", outputDir,
	)

	staleArchives, err := filepath.Glob(filepath.Join(outputDir, "gestalt-app-*_v1.0.0*.tar.gz"))
	if err != nil {
		t.Fatalf("glob stale archives: %v", err)
	}
	if len(staleArchives) != 0 {
		t.Fatalf("stale archives were not removed: %v", staleArchives)
	}
	currentArchives, err := filepath.Glob(filepath.Join(outputDir, "gestalt-app-*_v1.0.1*.tar.gz"))
	if err != nil {
		t.Fatalf("glob current archives: %v", err)
	}
	if len(currentArchives) == 0 {
		t.Fatalf("expected current version archives in %s", outputDir)
	}
}

func TestRun_ProviderPackageDoesNotWriteReleaseFinalizationFiles(t *testing.T) {
	t.Parallel()

	pluginDir := newUIReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(outputDir, providerrelease.MetadataFile), []byte("stale\n"), 0o644); err != nil {
		t.Fatalf("write stale metadata: %v", err)
	}

	runProviderPackageCommand(t, pluginDir,
		"--version", "1.0.0",
		"--output", outputDir,
	)

	if _, err := os.Stat(filepath.Join(outputDir, providerrelease.MetadataFile)); err == nil {
		t.Fatalf("provider package unexpectedly wrote %s", providerrelease.MetadataFile)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat %s: %v", providerrelease.MetadataFile, err)
	}
}

func TestRun_ProviderPackageRejectsHybridExecutableDuplicateEffectiveOperation(t *testing.T) {
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
	manifestData, err := encodeTestManifestFormat(manifest, providerpkg.ManifestFormatFromPath(manifestPath))
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
      operationId: generated_op
      responses:
        "200":
          description: OK
`), 0o644); err != nil {
		t.Fatalf("WriteFile openapi.yaml: %v", err)
	}
	writeTestFile(t, pluginDir, providerpkg.StaticCatalogFile, []byte("name: release-test\noperations:\n  - id: generated_op\n    method: GET\n"), 0o644)

	out, err := runProviderPackageAndReleaseCommandResult(pluginDir, "--version", "0.0.4-source.1", "--platform", runtime.GOOS+"/"+runtime.GOARCH, "--output", t.TempDir())
	if err == nil {
		t.Fatalf("expected provider release to fail, got output: %s", out)
	}
	if !strings.Contains(string(out), `duplicate operation \"generated_op\" across merged catalogs`) {
		t.Fatalf("expected duplicate effective operation error, got: %s", out)
	}
}

func TestRun_ProviderPackageAndReleaseCompilesProviderWithoutSourceArtifacts(t *testing.T) {
	t.Parallel()

	pluginDir := newSourceProviderReleaseFixtureWithoutCatalog(t, t.TempDir())
	const defaultArtifactPath = ".gestaltd/bin/release-test"
	writeReleaseTestManifest(t, pluginDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindApp,
		Source:      releaseTestSource,
		Version:     "0.0.1",
		DisplayName: "Release Test",
		IconFile:    releaseTestIconPath,
		Spec: &providermanifestv1.Spec{
			ConfigSchemaPath: releaseProviderSchemaPath,
		},
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"go.mod", "go.sum", "provider.go", "cmd", "build.sh"},
		},
	})
	writeTestFile(t, pluginDir, "build.sh", []byte("mkdir -p .gestaltd/bin\ngo build -o "+defaultArtifactPath+" ./cmd/provider\n"), 0o755)
	_ = os.Remove(filepath.Join(pluginDir, providerpkg.StaticCatalogFile))
	outputDir := t.TempDir()
	const testVersion = "0.0.4-source.1"

	runProviderPackageAndReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--platform", runtime.GOOS+"/"+runtime.GOARCH,
		"--output", outputDir,
	)

	archiveName := "gestalt-app-" + releaseTestAppName + "_v" + testVersion + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifest := readReleasedManifest(t, outputDir, archiveName)

	if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Path != providerpkg.PackageExecutablePath(releaseTestAppName, runtime.GOOS) {
		t.Fatalf("artifacts = %+v", manifest.Artifacts)
	}
	packageBinary := providerpkg.PackageExecutablePath(releaseTestAppName, runtime.GOOS)
	if manifest.Entrypoint == nil || manifest.Entrypoint.ArtifactPath != packageBinary {
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

func TestRun_ProviderPackageRejectsSDKSourceProviderWithoutBuildCommand(t *testing.T) {
	t.Parallel()

	pluginDir := newSourceProviderReleaseFixtureWithoutCatalog(t, t.TempDir())
	writeReleaseTestManifest(t, pluginDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindApp,
		Source:      releaseTestSource,
		Version:     "0.0.1",
		DisplayName: "Release Test",
		IconFile:    releaseTestIconPath,
		Spec: &providermanifestv1.Spec{
			ConfigSchemaPath: releaseProviderSchemaPath,
		},
	})
	_ = os.Remove(filepath.Join(pluginDir, providerpkg.StaticCatalogFile))
	_ = os.Remove(filepath.Join(pluginDir, "build.sh"))
	_ = os.RemoveAll(filepath.Join(pluginDir, "cmd"))
	_ = os.Remove(filepath.Join(pluginDir, "provider.go"))
	_ = os.Remove(filepath.Join(pluginDir, "go.mod"))
	_ = os.Remove(filepath.Join(pluginDir, "go.sum"))

	outputDir := t.TempDir()
	const testVersion = "0.0.4-sdk-source.1"

	out, err := runProviderPackageAndReleaseCommandResult(pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)
	if err == nil {
		t.Fatalf("expected provider release to reject SDK source provider without build.command\n%s", out)
	}
	if !strings.Contains(string(out), "declare object-form build.command") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestRun_ProviderPackageRejectsPrebuiltExecutableProvider(t *testing.T) {
	t.Parallel()

	pluginDir := newPrebuiltProviderReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.5-test"

	out, err := runProviderPackageAndReleaseCommandResult(pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)
	if err == nil {
		t.Fatalf("expected provider release to reject executable provider without build.command\n%s", out)
	}
	if !strings.Contains(string(out), "declare object-form build.command") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestRun_ProviderPackageRejectsExplicitPlatformForPrebuiltProvider(t *testing.T) {
	t.Parallel()

	pluginDir := newPrebuiltProviderReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()

	out, err := runProviderPackageAndReleaseCommandResult(pluginDir,
		"--version", "0.0.5-platform-test",
		"--platform", runtime.GOOS+"/"+runtime.GOARCH,
		"--output", outputDir,
	)
	if err == nil {
		t.Fatalf("expected provider release to reject explicit platform for prebuilt provider\n%s", out)
	}
	if !strings.Contains(string(out), "declare object-form build.command") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestRun_ProviderPackageRejectsGoModuleWithoutBuildCommand(t *testing.T) {
	t.Parallel()

	pluginDir := newPrebuiltProviderReleaseFixture(t, t.TempDir())
	writeTestFile(t, pluginDir, "go.mod", []byte("module example.com/prebuilt-provider\n\ngo 1.22\n"), 0644)

	outputDir := t.TempDir()
	const testVersion = "0.0.6-test"

	out, err := runProviderPackageAndReleaseCommandResult(pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)
	if err == nil {
		t.Fatalf("expected provider release to reject executable provider without build.command\n%s", out)
	}
	if !strings.Contains(string(out), "declare object-form build.command") {
		t.Fatalf("unexpected output: %s", out)
	}
}
