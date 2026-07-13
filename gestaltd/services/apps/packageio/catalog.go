package packageio

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

const StaticCatalogFile = "catalog.yaml"

func StaticCatalogPath(rootDir string) string {
	if rootDir == "" {
		return StaticCatalogFile
	}
	return filepath.Join(rootDir, StaticCatalogFile)
}

func StaticCatalogRequired(manifest *providermanifestv1.Manifest) bool {
	return manifest != nil && manifest.Kind == providermanifestv1.KindApp && manifest.Spec != nil && !manifest.Spec.IsManifestBacked()
}

func ReadStaticCatalog(rootDir, name string) (*catalog.Catalog, error) {
	catalogPath := StaticCatalogPath(rootDir)
	data, err := os.ReadFile(catalogPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read static catalog %q: %w", catalogPath, err)
	}

	var cat catalog.Catalog
	if err := decodeStrict(data, ManifestFormatFromPath(catalogPath), "static catalog", &cat); err != nil {
		return nil, err
	}

	if cat.Name == "" && name != "" {
		cat.Name = name
	}
	if err := validateStaticCatalog(&cat); err != nil {
		return nil, fmt.Errorf("validate static catalog %q: %w", catalogPath, err)
	}
	return &cat, nil
}

func validateStaticCatalog(cat *catalog.Catalog) error {
	for i := range cat.Operations {
		if len(cat.Operations[i].AllowedRoles) > 0 {
			return fmt.Errorf("catalog %q operation %q: provider-owned allowedRoles are not supported; move roles to apps.<name>.allowedOperations in config.yaml", cat.Name, cat.Operations[i].ID)
		}
	}
	return cat.Validate()
}
