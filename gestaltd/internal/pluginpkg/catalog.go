package pluginpkg

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

const StaticCatalogFile = "catalog.yaml"

func StaticCatalogPath(rootDir string) string {
	if rootDir == "" {
		return StaticCatalogFile
	}
	return filepath.Join(rootDir, StaticCatalogFile)
}

func StaticCatalogRequired(manifest *pluginmanifestv1.Manifest) bool {
	return manifest != nil && manifest.Provider != nil && !manifest.Provider.IsManifestBacked()
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
	if err := cat.Validate(); err != nil {
		return nil, fmt.Errorf("validate static catalog %q: %w", catalogPath, err)
	}
	return &cat, nil
}
