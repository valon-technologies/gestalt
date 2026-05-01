package providerpkg

import (
	"github.com/valon-technologies/gestalt/server/core/catalog"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/plugins/packageio"
)

const StaticCatalogFile = packageio.StaticCatalogFile

func StaticCatalogPath(rootDir string) string {
	return packageio.StaticCatalogPath(rootDir)
}

func StaticCatalogRequired(manifest *providermanifestv1.Manifest) bool {
	return packageio.StaticCatalogRequired(manifest)
}

func ReadStaticCatalog(rootDir, name string) (*catalog.Catalog, error) {
	return packageio.ReadStaticCatalog(rootDir, name)
}
