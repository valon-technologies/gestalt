package operator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/staticvalidation"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func TestValidateInstalledPackageMatchesReleaseBundleCatalogRelax(t *testing.T) {
	t.Parallel()

	manifest := &providermanifestv1.Manifest{
		Kind:    providermanifestv1.KindAuthentication,
		Source:  "github.com/acme/auth",
		Version: "1.0.0",
		Spec:    &providermanifestv1.Spec{},
	}

	root := t.TempDir()
	catalogPath := filepath.Join(root, "catalog.yaml")
	if err := os.WriteFile(catalogPath, []byte("name: installed\noperations:\n  - id: ping\n    method: GET\n"), 0o644); err != nil {
		t.Fatalf("write catalog: %v", err)
	}

	installed := &installedPackage{
		Root:     root,
		Manifest: manifest,
	}
	bundleCatalog := &catalog.Catalog{Name: "bundle", Operations: []catalog.CatalogOperation{{ID: "ping", Method: "GET"}}}
	bundle := providerReleaseValidationBundle{
		Manifest: manifest,
		Catalog:  bundleCatalog,
	}
	projected, err := staticvalidation.ProjectManifest(installed.Manifest, "", true)
	if err != nil {
		t.Fatalf("ProjectManifest: %v", err)
	}
	addCatalogOperationIDsToManifest(projected, bundle.Catalog)
	entry := LockEntry{
		ValidationManifest: projected,
	}

	if err := validateInstalledPackageMatchesReleaseBundle("auth", installed, entry, bundle, true); err != nil {
		t.Fatalf("relaxed catalog drift: %v", err)
	}
	if err := validateInstalledPackageMatchesReleaseBundle("auth", installed, entry, bundle, false); err == nil {
		t.Fatal("strict catalog drift: expected error, got nil")
	}
}
