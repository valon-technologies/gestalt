package packageio

import (
	"strings"
	"testing"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func TestValidatePackageStaticCatalogRejectsProviderOwnedAllowedRoles(t *testing.T) {
	t.Parallel()

	manifest := &providermanifestv1.Manifest{
		Kind: providermanifestv1.KindApp,
		Spec: &providermanifestv1.Spec{},
	}
	data := []byte(`
name: provider
operations:
  - id: echo
    method: POST
    allowedRoles:
      - admin
`)

	err := validatePackageStaticCatalog(manifest, data, true)
	if err == nil || !strings.Contains(err.Error(), "allowedRoles") {
		t.Fatalf("validatePackageStaticCatalog error = %v, want allowedRoles rejection", err)
	}
}
