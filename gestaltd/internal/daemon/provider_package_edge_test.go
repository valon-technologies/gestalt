package daemon

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/operator"
	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

func TestRun_ProviderPackageAndReleaseStagesOwnedUIPackage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		fixture       func(*testing.T, string) string
		wantFiles     []string
		wantAssetRoot string
		skipOnWin     bool
	}{
		{
			name:          "checked-in owned ui assets with build command",
			fixture:       newSourceProviderReleaseFixtureWithOwnedUI,
			wantFiles:     []string{"_owned_ui/roadmap-ui/branding/icon.svg", "_owned_ui/roadmap-ui/dist/index.html", "_owned_ui/roadmap-ui/dist/static/app.js"},
			wantAssetRoot: filepath.Join("_owned_ui", "roadmap-ui", "dist"),
		},
		{
			name:          "source-built owned ui assets",
			fixture:       newSourceProviderReleaseFixtureWithSourceBuiltOwnedUI,
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

			runProviderPackageAndReleaseCommand(t, pluginDir,
				"--version", testVersion,
				"--platform", runtime.GOOS+"/"+runtime.GOARCH,
				"--output", outputDir,
			)

			archiveName := "gestalt-app-release-test_v" + testVersion + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
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
			if ownedUIManifest.Build != nil {
				t.Fatalf("owned ui manifest unexpectedly retained build metadata: %+v", ownedUIManifest.Build)
			}
			metadata := readProviderReleaseMetadata(t, outputDir)
			if metadata.Package != releaseTestSource {
				t.Fatalf("release metadata package = %q, want %q", metadata.Package, releaseTestSource)
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
			artifact := providerReleaseArtifactForTarget(t, metadata, providerpkg.CurrentPlatformString())
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
			if _, err := lc.PrepareAtPath(configPath); err != nil {
				t.Fatalf("PrepareAtPath: %v", err)
			}

			loaded, _, err := lc.LoadForExecutionAtPath(configPath, true)
			if err != nil {
				t.Fatalf("LoadForExecutionAtPath(locked=true): %v", err)
			}
			app := loaded.Apps["roadmap"]
			if app == nil || app.ResolvedManifest == nil {
				t.Fatalf("ResolvedManifest = %+v", app)
			}
			if app.Command == "" {
				t.Fatalf("app.Command = %q, want packaged executable path", app.Command)
			}
			if got := app.ResolvedManifest.Version; got != testVersion {
				t.Fatalf("ResolvedManifest.Version = %q, want %q", got, testVersion)
			}

			uiEntry := loaded.Providers.UI["roadmap"]
			if uiEntry == nil || uiEntry.ResolvedManifest == nil {
				t.Fatalf("Resolved app-owned UI = %+v", uiEntry)
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

			lock, err := operator.ReadLockfile(filepath.Join(configDir, operator.LockfileName))
			if err != nil {
				t.Fatalf("ReadLockfile: %v", err)
			}
			if len(lock.Providers.UI) != 0 {
				t.Fatalf("lock.Providers.UI = %#v, want no separate UI entries for packaged owned UI", lock.Providers.UI)
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

func TestRun_ProviderPackageAndReleaseBuildsSourceUIAssetsBeforePackaging(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("source build fixture uses POSIX shell")
	}

	pluginDir := newSourceBuiltUIReleaseFixture(t, t.TempDir())
	outputDir := t.TempDir()
	const testVersion = "0.0.3-source-build-ui"

	runProviderPackageAndReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)

	archiveName := "gestalt-app-ui-test_v" + testVersion + ".tar.gz"
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	manifest := readReleasedManifest(t, outputDir, archiveName)
	if manifest.Build != nil {
		t.Fatalf("released manifest unexpectedly retained build metadata: %+v", manifest.Build)
	}
	if manifest.Spec == nil || manifest.Spec.AssetRoot != "ui/out" {
		t.Fatalf("released manifest spec.assetRoot = %+v, want ui/out", manifest.Spec)
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

func TestRun_ProviderPackageAndReleaseAllowsOverlappingSupportPaths(t *testing.T) {
	t.Parallel()

	pluginDir := filepath.Join(t.TempDir(), "ui-overlap")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(pluginDir): %v", err)
	}
	writeReleaseTestManifest(t, pluginDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindUI,
		Source:      "github.com/testowner/apps/ui-overlap",
		Version:     "0.0.1",
		DisplayName: "UI Overlap",
		IconFile:    "out/icon.svg",
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"build.sh"},
		},
		Spec: &providermanifestv1.Spec{AssetRoot: "out"},
	})
	writeTestFile(t, pluginDir, "out/icon.svg", []byte("<svg></svg>\n"), 0o644)
	writeTestFile(t, pluginDir, "out/index.html", []byte("<html></html>\n"), 0o644)
	writeTestFile(t, pluginDir, "build.sh", []byte("mkdir -p out\nprintf '<svg></svg>\\n' > out/icon.svg\nprintf '<html></html>\\n' > out/index.html\n"), 0o755)

	outputDir := t.TempDir()
	const testVersion = "0.0.3-overlap.1"

	runProviderPackageAndReleaseCommand(t, pluginDir,
		"--version", testVersion,
		"--output", outputDir,
	)

	archiveName := "gestalt-app-ui-overlap_v" + testVersion + ".tar.gz"
	extractDir := extractReleasedArchive(t, outputDir, archiveName)
	for _, rel := range []string{"out/icon.svg", "out/index.html"} {
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
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: ".gestalt/build/provider"},
	})
	if err := os.Remove(filepath.Join(pluginDir, providerpkg.ManifestFile)); err != nil {
		t.Fatalf("remove manifest.json: %v", err)
	}

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

func TestProviderPackagePreservesOtherPlatformArchives(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	linuxArchive := platformArchiveNameForTest(releaseTestAppName, "1.0.0", "linux", "amd64")
	darwinArchive := platformArchiveNameForTest(releaseTestAppName, "0.9.0", "darwin", "arm64")
	writeTestFile(t, outputDir, linuxArchive, []byte("linux archive"), 0o644)
	writeTestFile(t, outputDir, darwinArchive, []byte("stale darwin archive"), 0o644)

	err := removeStalePackageArchives(outputDir, releaseTestAppName, releaseArchiveTargets([]releasePlatform{{GOOS: "darwin", GOARCH: "arm64"}}))
	if err != nil {
		t.Fatalf("removeStalePackageArchives: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outputDir, linuxArchive)); err != nil {
		t.Fatalf("expected other platform archive %s to remain: %v", linuxArchive, err)
	}
	if _, err := os.Stat(filepath.Join(outputDir, darwinArchive)); !os.IsNotExist(err) {
		t.Fatalf("expected same platform archive %s to be removed, got err=%v", darwinArchive, err)
	}
}

func TestRun_ProviderPackageRejectsOutputInsideUIAssetRoot(t *testing.T) {
	t.Parallel()

	pluginDir := newUIReleaseFixtureWithAssetRoot(t, t.TempDir(), "release-output")
	outputDir := filepath.Join(pluginDir, "release-output", "nested")

	out, err := runProviderPackageAndReleaseCommandResult(pluginDir, "--version", "1.0.0", "--output", outputDir)
	if err == nil {
		t.Fatalf("expected provider release to fail, got output: %s", out)
	}
	if !strings.Contains(string(out), "must not be inside ui asset root") {
		t.Fatalf("expected overlap error, got: %s", out)
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
	const defaultArtifactPath = ".gestalt/build/release-test"
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
	writeTestFile(t, pluginDir, "build.sh", []byte("mkdir -p .gestalt/build\ngo build -o "+defaultArtifactPath+" ./cmd/provider\n"), 0o755)
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

	if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Path != defaultArtifactPath {
		t.Fatalf("artifacts = %+v", manifest.Artifacts)
	}
	if manifest.Entrypoint == nil || manifest.Entrypoint.ArtifactPath != defaultArtifactPath {
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
	if !strings.Contains(string(out), "declare object-form build.command and entrypoint.artifactPath") {
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
	if !strings.Contains(string(out), "provider package requires build.command for executable source providers") {
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
	if !strings.Contains(string(out), "--platform requires build.command for executable source providers") {
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
	if !strings.Contains(string(out), "provider package requires build.command for executable source providers") {
		t.Fatalf("unexpected output: %s", out)
	}
}
