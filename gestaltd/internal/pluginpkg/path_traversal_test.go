package pluginpkg

import (
	"path/filepath"
	"strings"
	"testing"

	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

// writeManifestRaw writes a manifest to disk without validation, so we can test
// that the loader rejects malicious content at load time.
func writeManifestRaw(t *testing.T, dir string, manifest *pluginmanifestv1.Manifest) {
	t.Helper()
	data := mustRawManifestJSON(t, manifest)
	mustWriteFile(t, filepath.Join(dir, ManifestFile), data, 0644)
	mustWriteStaticCatalog(t, dir, manifest)
}

func setOpenAPIDocument(manifest *pluginmanifestv1.Manifest, doc string) {
	if manifest.Plugin.Surfaces == nil {
		manifest.Plugin.Surfaces = &pluginmanifestv1.PluginSurfaces{}
	}
	manifest.Plugin.Surfaces.OpenAPI = &pluginmanifestv1.OpenAPISurface{Document: doc}
}

func TestValidatePackageDirRejectsTraversalInOpenAPIField(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sourceDir, _ := mustWriteProviderPackageDir(t, dir, "github.com/acme/plugins/provider", "0.0.1-alpha.1", "provider")

	_, manifest, err := loadManifestFromDir(sourceDir)
	if err != nil {
		t.Fatalf("loadManifestFromDir: %v", err)
	}
	setOpenAPIDocument(manifest, "../../../etc/passwd")
	writeManifestRaw(t, sourceDir, manifest)

	_, err = ValidatePackageDir(sourceDir)
	if err == nil {
		t.Fatal("expected ValidatePackageDir to reject manifest with traversal in openapi field")
	}
	if !strings.Contains(err.Error(), "must stay within the package") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidatePackageDirRejectsAbsolutePathInOpenAPIField(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sourceDir, _ := mustWriteProviderPackageDir(t, dir, "github.com/acme/plugins/provider", "0.0.1-alpha.2", "provider")

	_, manifest, err := loadManifestFromDir(sourceDir)
	if err != nil {
		t.Fatalf("loadManifestFromDir: %v", err)
	}
	setOpenAPIDocument(manifest, "/etc/passwd")
	writeManifestRaw(t, sourceDir, manifest)

	_, err = ValidatePackageDir(sourceDir)
	if err == nil {
		t.Fatal("expected ValidatePackageDir to reject manifest with absolute openapi path")
	}
	if !strings.Contains(err.Error(), "must be relative") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidatePackageDirAllowsHTTPOpenAPIURL(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sourceDir, _ := mustWriteProviderPackageDir(t, dir, "github.com/acme/plugins/provider", "0.0.1-alpha.3", "provider")

	_, manifest, err := loadManifestFromDir(sourceDir)
	if err != nil {
		t.Fatalf("loadManifestFromDir: %v", err)
	}
	setOpenAPIDocument(manifest, "https://api.example.com/openapi.yaml")
	writeManifestRaw(t, sourceDir, manifest)

	_, err = ValidatePackageDir(sourceDir)
	if err != nil && strings.Contains(err.Error(), "surfaces.openapi") {
		t.Fatalf("HTTP URL should be accepted for openapi field: %v", err)
	}
}

func TestResolveManifestLocalReferencesRejectsTraversal(t *testing.T) {
	t.Parallel()

	manifestPath := filepath.Join("/opt", "providers", "github", "manifest.yaml")
	manifest := &pluginmanifestv1.Manifest{
		Plugin: &pluginmanifestv1.Plugin{
			Surfaces: &pluginmanifestv1.PluginSurfaces{
				OpenAPI: &pluginmanifestv1.OpenAPISurface{
					Document: "../../../etc/passwd",
				},
			},
		},
	}

	_, err := ResolveManifestLocalReferences(manifest, manifestPath)
	if err == nil {
		t.Fatal("expected ResolveManifestLocalReferences to reject traversal")
	}
	if !strings.Contains(err.Error(), "escapes the manifest directory") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveManifestLocalReferencesResolvesValidRelativePath(t *testing.T) {
	t.Parallel()

	manifestPath := filepath.Join("/opt", "providers", "github", "manifest.yaml")
	manifest := &pluginmanifestv1.Manifest{
		Plugin: &pluginmanifestv1.Plugin{
			Surfaces: &pluginmanifestv1.PluginSurfaces{
				OpenAPI: &pluginmanifestv1.OpenAPISurface{
					Document: "openapi.yaml",
				},
			},
		},
	}

	got, err := ResolveManifestLocalReferences(manifest, manifestPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join("/opt", "providers", "github", "openapi.yaml")
	if got.Plugin.OpenAPIDocument() != want {
		t.Fatalf("OpenAPIDocument() = %q, want %q", got.Plugin.OpenAPIDocument(), want)
	}
}
