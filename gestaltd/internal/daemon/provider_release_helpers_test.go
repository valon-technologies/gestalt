package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

func defaultReleasePlatformsForTest(t *testing.T) []releasePlatform {
	t.Helper()

	platforms, err := parseReleasePlatforms(defaultPlatforms)
	if err != nil {
		t.Fatalf("parseReleasePlatforms(defaultPlatforms): %v", err)
	}
	return platforms
}

func platformArchiveNameForTest(appName, version, goos, goarch string) string {
	return fmt.Sprintf("gestalt-app-%s_v%s_%s_%s.tar.gz", appName, version, goos, goarch)
}

func writeProviderReleaseArchiveForTest(t *testing.T, outputDir, archiveName string, manifest *providermanifestv1.Manifest) string {
	t.Helper()

	packageDir := t.TempDir()
	writeProviderReleaseManifestSupportFilesForTest(t, packageDir, manifest)
	writeReleasedManifestForArchiveTest(t, packageDir, manifest)

	archivePath := filepath.Join(outputDir, archiveName)
	if err := providerpkg.CreatePackageFromDir(packageDir, archivePath); err != nil {
		t.Fatalf("CreatePackageFromDir(%s): %v", archiveName, err)
	}
	return archivePath
}

func writeProviderReleaseManifestSupportFilesForTest(t *testing.T, dir string, manifest *providermanifestv1.Manifest) {
	t.Helper()

	if manifest == nil {
		return
	}
	if manifest.IconFile != "" {
		writeTestFile(t, dir, manifest.IconFile, []byte("<svg></svg>\n"), 0o644)
	}
	if manifest.Spec != nil {
		if manifest.Spec.ConfigSchemaPath != "" {
			writeTestFile(t, dir, manifest.Spec.ConfigSchemaPath, []byte(`{"type":"object"}`), 0o644)
		}
		if manifest.Spec.AssetRoot != "" {
			writeTestFile(t, dir, filepath.Join(filepath.FromSlash(manifest.Spec.AssetRoot), "index.html"), []byte("<html></html>\n"), 0o644)
		}
	}
	for _, artifact := range manifest.Artifacts {
		if artifact.Path == "" {
			continue
		}
		writeTestFile(t, dir, artifact.Path, []byte("artifact:"+artifact.Path), 0o755)
	}
}

func writeReleasedManifestForArchiveTest(t *testing.T, dir string, manifest *providermanifestv1.Manifest) {
	t.Helper()

	populateMissingArtifactDigests(t, dir, manifest)
	data, err := providerpkg.EncodeManifestFormat(manifest, providerpkg.ManifestFormatJSON)
	if err != nil {
		t.Fatalf("EncodeManifestFormat: %v", err)
	}
	writeTestFile(t, dir, providerpkg.ManifestFile, data, 0o644)
	if manifest.Kind == providermanifestv1.KindApp && manifest.Spec != nil {
		writeTestFile(t, dir, providerpkg.StaticCatalogFile, []byte("name: provider\noperations:\n  - id: echo\n    method: POST\n"), 0o644)
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
	if manifest == nil {
		return providerpkg.EncodeSourceManifestFormat(nil, format)
	}
	clone := *manifest
	clone.Entrypoint = nil
	return providerpkg.EncodeSourceManifestFormat(&clone, format)
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
