package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
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

func TestProviderReleaseMetadataAllowsMCPOnlyWithoutCatalog(t *testing.T) {
	t.Parallel()

	manifest := &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindApp,
		Source:      releaseTestSource,
		Version:     "1.0.0",
		DisplayName: "MCP Only",
		Spec: &providermanifestv1.Spec{
			Surfaces: &providermanifestv1.ProviderSurfaces{
				MCP: &providermanifestv1.MCPSurface{URL: "https://mcp.example.test"},
			},
		},
	}
	metadata, err := buildProviderReleaseMetadataForManifest(t, manifest, map[string]string{
		providerpkg.StaticCatalogFile: "name: ignored\noperations:\n  - id: ignored\n    method: POST\n",
	})
	if err != nil {
		t.Fatalf("buildProviderReleaseMetadata: %v", err)
	}
	if metadata.StaticValidation == nil {
		t.Fatal("static validation metadata missing")
	}
	if metadata.StaticValidation.Catalog != nil {
		t.Fatalf("catalog = %#v, want nil for MCP-only release", metadata.StaticValidation.Catalog)
	}
	if !metadata.StaticValidation.CatalogSessionOnly {
		t.Fatal("catalogSessionOnly = false, want true for MCP-only release")
	}
}

func TestProviderReleaseMetadataBuildsOpenAPICatalog(t *testing.T) {
	t.Parallel()

	manifest := &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindApp,
		Source:      releaseTestSource,
		Version:     "1.0.0",
		DisplayName: "OpenAPI App",
		IconFile:    "assets/icon.svg",
		Spec: &providermanifestv1.Spec{
			Surfaces: &providermanifestv1.ProviderSurfaces{
				OpenAPI: &providermanifestv1.OpenAPISurface{
					Connection: "default",
					Document:   "openapi.yaml",
				},
			},
			Connections: map[string]*providermanifestv1.ManifestConnectionDef{
				"default": {Mode: providermanifestv1.ConnectionModeNone, Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeNone}},
			},
		},
	}
	metadata, err := buildProviderReleaseMetadataForManifest(t, manifest, map[string]string{
		"assets/icon.svg": "<svg/>",
		"openapi.yaml": `openapi: 3.0.0
info:
  title: Test API
  version: 1.0.0
servers:
  - url: https://api.example.test
paths:
  /pets:
    get:
      operationId: listPets
      responses:
        "200":
          description: ok
`,
	})
	if err != nil {
		t.Fatalf("buildProviderReleaseMetadata: %v", err)
	}
	if metadata.StaticValidation == nil || metadata.StaticValidation.Catalog == nil {
		t.Fatalf("catalog metadata missing: %#v", metadata.StaticValidation)
	}
	if providerrelease.ManifestReferencesPackageFiles(metadata.StaticValidation.Manifest) {
		t.Fatalf("validation manifest retained package-local references: %#v", metadata.StaticValidation.Manifest)
	}
	if _, ok := catalog.OperationByID(metadata.StaticValidation.Catalog, "listPets"); !ok {
		t.Fatalf("catalog operations = %#v, want listPets", metadata.StaticValidation.Catalog.Operations)
	}
}

func TestProviderReleaseMetadataMergesExecutableStaticAndAPICatalogs(t *testing.T) {
	t.Parallel()

	metadata, err := buildProviderReleaseMetadataForRawManifest(t, `kind: app
source: github.com/testowner/apps/catalog/release-test
version: 1.0.0
displayName: Mixed App
artifacts:
  - os: test
    arch: test
    path: provider
    sha256: e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
entrypoint:
  artifactPath: provider
spec:
  surfaces:
    rest:
      connection: default
      baseUrl: https://api.example.test
      operations:
        - name: createThing
          method: POST
          path: /things
  connections:
    default:
      auth:
        type: none
`, map[string]string{
		"provider": "",
		"catalog.yaml": `name: test
operations:
  - id: pluginOp
    method: POST
    description: Static plugin operation
`,
	})
	if err != nil {
		t.Fatalf("buildProviderReleaseMetadata: %v", err)
	}
	cat := metadata.StaticValidation.Catalog
	if cat == nil {
		t.Fatalf("catalog metadata missing: %#v", metadata.StaticValidation)
	}
	if _, ok := catalog.OperationByID(cat, "pluginOp"); !ok {
		t.Fatalf("catalog operations = %#v, want pluginOp", cat.Operations)
	}
	if _, ok := catalog.OperationByID(cat, "createThing"); !ok {
		t.Fatalf("catalog operations = %#v, want createThing", cat.Operations)
	}
}

func TestProviderReleaseMetadataMergesExecutableStaticAndOpenAPICatalogs(t *testing.T) {
	t.Parallel()

	metadata, err := buildProviderReleaseMetadataForRawManifest(t, `kind: app
source: github.com/testowner/apps/catalog/release-test
version: 1.0.0
displayName: Mixed OpenAPI App
artifacts:
  - os: test
    arch: test
    path: provider
    sha256: e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
entrypoint:
  artifactPath: provider
spec:
  surfaces:
    openapi:
      connection: default
      document: openapi.yaml
  connections:
    default:
      auth:
        type: none
`, map[string]string{
		"provider": "",
		"catalog.yaml": `name: test
operations:
  - id: pluginOp
    method: POST
    description: Static plugin operation
`,
		"openapi.yaml": `openapi: 3.0.0
info:
  title: Test API
  version: 1.0.0
paths:
  /teams:
    get:
      operationId: getTeams
      responses:
        "200":
          description: ok
`,
	})
	if err != nil {
		t.Fatalf("buildProviderReleaseMetadata: %v", err)
	}
	cat := metadata.StaticValidation.Catalog
	if cat == nil {
		t.Fatalf("catalog metadata missing: %#v", metadata.StaticValidation)
	}
	if _, ok := catalog.OperationByID(cat, "pluginOp"); !ok {
		t.Fatalf("catalog operations = %#v, want pluginOp", cat.Operations)
	}
	if _, ok := catalog.OperationByID(cat, "getTeams"); !ok {
		t.Fatalf("catalog operations = %#v, want getTeams", cat.Operations)
	}
}

func TestProviderReleaseMetadataBuildsGraphQLMCPCatalog(t *testing.T) {
	t.Parallel()

	manifest := &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindApp,
		Source:      releaseTestSource,
		Version:     "1.0.0",
		DisplayName: "GraphQL MCP",
		Spec: &providermanifestv1.Spec{
			Surfaces: &providermanifestv1.ProviderSurfaces{
				GraphQL: &providermanifestv1.GraphQLSurface{Connection: "default", URL: "https://graphql.example.test/graphql"},
				MCP:     &providermanifestv1.MCPSurface{Connection: "default", URL: "https://mcp.example.test/mcp"},
			},
			Connections: map[string]*providermanifestv1.ManifestConnectionDef{
				"default": {Mode: providermanifestv1.ConnectionModeNone, Auth: &providermanifestv1.ProviderAuth{Type: providermanifestv1.AuthTypeNone}},
			},
			AllowedOperations: map[string]*providermanifestv1.ManifestOperationOverride{
				"viewer": {
					GraphQL: &providermanifestv1.ManifestGraphQLOperation{
						OperationName: "Viewer",
						Document:      "query Viewer { viewer { id } }",
					},
				},
			},
		},
	}
	metadata, err := buildProviderReleaseMetadataForManifest(t, manifest, nil)
	if err != nil {
		t.Fatalf("buildProviderReleaseMetadata: %v", err)
	}
	if metadata.StaticValidation == nil || metadata.StaticValidation.Catalog == nil {
		t.Fatalf("catalog metadata missing: %#v", metadata.StaticValidation)
	}
	if _, ok := catalog.OperationByID(metadata.StaticValidation.Catalog, "viewer"); !ok {
		t.Fatalf("catalog operations = %#v, want viewer", metadata.StaticValidation.Catalog.Operations)
	}
}

func TestProviderReleaseMetadataRejectsMCPAppWithoutStaticCatalog(t *testing.T) {
	t.Parallel()

	manifest := &providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindApp,
		Source:  releaseTestSource,
		Version: "1.0.0",
		Spec: &providermanifestv1.Spec{
			Surfaces: &providermanifestv1.ProviderSurfaces{
				MCP:     &providermanifestv1.MCPSurface{URL: "https://mcp.example.test"},
				GraphQL: &providermanifestv1.GraphQLSurface{URL: "https://graphql.example.test/graphql"},
			},
		},
	}
	_, err := buildProviderReleaseMetadataForManifest(t, manifest, nil)
	if err == nil || !strings.Contains(err.Error(), "must include catalog metadata unless the validation manifest is MCP-only") {
		t.Fatalf("buildProviderReleaseMetadata error = %v, want static catalog validation failure", err)
	}
}

func buildProviderReleaseMetadataForManifest(t *testing.T, manifest *providermanifestv1.Manifest, files map[string]string) (*providerrelease.Metadata, error) {
	t.Helper()

	packageDir := t.TempDir()
	data, err := providerpkg.EncodeManifestFormat(manifest, providerpkg.ManifestFormatJSON)
	if err != nil {
		t.Fatalf("EncodeManifestFormat: %v", err)
	}
	if err := os.WriteFile(filepath.Join(packageDir, providerpkg.ManifestFile), data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	for name, contents := range files {
		path := filepath.Join(packageDir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	outputDir := t.TempDir()
	archive := filepath.Join(outputDir, "gestalt-app-release-test_v1.0.0.tar.gz")
	if err := providerpkg.CreatePackageFromDir(packageDir, archive); err != nil {
		t.Fatalf("CreatePackageFromDir: %v", err)
	}
	archiveSHA, err := providerpkg.ArchiveDigest(archive)
	if err != nil {
		t.Fatalf("ArchiveDigest: %v", err)
	}

	return buildProviderReleaseMetadata(
		manifest,
		"1.0.0",
		[]releaseArchive{{Path: archive, SHA256: archiveSHA, Target: providerpkg.CurrentPlatformString()}},
	)
}

func buildProviderReleaseMetadataForRawManifest(t *testing.T, rawManifest string, files map[string]string) (*providerrelease.Metadata, error) {
	t.Helper()

	packageDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(packageDir, "manifest.yaml"), []byte(rawManifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	for name, contents := range files {
		path := filepath.Join(packageDir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	_, manifest, _, err := providerpkg.LoadManifestFromPath(packageDir)
	if err != nil {
		t.Fatalf("LoadManifestFromPath: %v", err)
	}
	outputDir := t.TempDir()
	archive := filepath.Join(outputDir, "gestalt-app-release-test_v1.0.0.tar.gz")
	if err := providerpkg.CreatePackageFromDir(packageDir, archive); err != nil {
		t.Fatalf("CreatePackageFromDir: %v", err)
	}
	archiveSHA, err := providerpkg.ArchiveDigest(archive)
	if err != nil {
		t.Fatalf("ArchiveDigest: %v", err)
	}

	return buildProviderReleaseMetadata(
		manifest,
		"1.0.0",
		[]releaseArchive{{Path: archive, SHA256: archiveSHA, Target: providerpkg.CurrentPlatformString()}},
	)
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
