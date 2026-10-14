package plugins

import (
	"context"
	"strings"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/operationexposure"
	"github.com/valon-technologies/gestalt/server/services/apps/packageio"
)

type OperationOverride = operationexposure.OperationOverride

type SpecSurface string

const (
	SpecSurfaceOpenAPI SpecSurface = "openapi"
	SpecSurfaceGraphQL SpecSurface = "graphql"
	SpecSurfaceMCP     SpecSurface = "mcp"

	StaticCatalogFile = packageio.StaticCatalogFile
)

type StaticCatalogReader func(name string) (*catalog.Catalog, error)

type APICatalogLoader func(ctx context.Context, name string, surface SpecSurface, specURL string, allowed map[string]*OperationOverride) (*catalog.Catalog, error)

type ValidationConfig struct {
	Apps map[string]*ValidationApp
}

type ValidationApp struct {
	Manifest                    *providermanifestv1.Manifest
	ManifestPath                string
	AllowedOperations           map[string]*OperationOverride
	SurfaceURLOverrides         map[SpecSurface]string
	EffectiveCatalog            *catalog.Catalog
	EffectiveCatalogAvailable   bool
	EffectiveCatalogSessionOnly bool
	StaticMetadataUnavailable   bool
	ReadStaticCatalog           StaticCatalogReader
	LoadAPICatalog              APICatalogLoader
}

func (p *ValidationApp) manifestSpec() *providermanifestv1.Spec {
	if p == nil || p.Manifest == nil {
		return nil
	}
	return p.Manifest.Spec
}

func (p *ValidationApp) surfaceURL(spec *providermanifestv1.Spec, surface SpecSurface) (string, bool) {
	if p == nil {
		return "", false
	}
	if p.SurfaceURLOverrides != nil {
		if url := strings.TrimSpace(p.SurfaceURLOverrides[surface]); url != "" {
			return url, true
		}
	}
	url := packageio.ManifestProviderSurfaceURL(spec, string(surface))
	if url == "" {
		return "", false
	}
	return packageio.ResolveManifestRelativeSpecURL(p.ManifestPath, url), true
}

func staticCatalogRequired(manifest *providermanifestv1.Manifest) bool {
	return packageio.StaticCatalogRequired(manifest)
}
