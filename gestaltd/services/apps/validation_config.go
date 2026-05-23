package plugins

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/declarative"
	"github.com/valon-technologies/gestalt/server/services/apps/openapi"
	"github.com/valon-technologies/gestalt/server/services/apps/packageio"
)

func ValidateEffectiveManifest(ctx context.Context, name, manifestPath string, manifest *providermanifestv1.Manifest) error {
	return ValidateEffectiveCatalog(ctx, name, ValidationAppFromManifest(manifestPath, manifest))
}

func ValidationAppFromManifest(manifestPath string, manifest *providermanifestv1.Manifest) *ValidationApp {
	return &ValidationApp{
		Manifest:          manifest,
		ManifestPath:      manifestPath,
		ReadStaticCatalog: StaticCatalogReaderForManifest(manifestPath),
		LoadAPICatalog:    DefaultAPICatalogLoader,
	}
}

func StaticCatalogReaderForManifest(manifestPath string) StaticCatalogReader {
	if manifestPath == "" {
		return nil
	}
	root := filepath.Dir(manifestPath)
	return func(name string) (*catalog.Catalog, error) {
		return packageio.ReadStaticCatalog(root, name)
	}
}

func DefaultAPICatalogLoader(ctx context.Context, name string, surface SpecSurface, specURL string, allowed map[string]*OperationOverride) (*catalog.Catalog, error) {
	switch surface {
	case SpecSurfaceOpenAPI:
		def, err := openapi.LoadDefinition(ctx, name, specURL, allowed)
		if err != nil {
			return nil, err
		}
		return declarative.CatalogFromDefinition(def), nil
	default:
		return nil, fmt.Errorf("unsupported API catalog surface %q", surface)
	}
}
