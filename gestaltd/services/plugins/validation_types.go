package plugins

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/plugins/operationexposure"
)

type OperationOverride = operationexposure.OperationOverride

type SpecSurface string

const (
	SpecSurfaceOpenAPI SpecSurface = "openapi"
	SpecSurfaceGraphQL SpecSurface = "graphql"
	SpecSurfaceMCP     SpecSurface = "mcp"

	StaticCatalogFile = "catalog.yaml"
)

type InvocationDependency struct {
	Plugin    string
	Operation string
	Surface   SpecSurface
}

type StaticCatalogReader func(name string) (*catalog.Catalog, error)

type APICatalogLoader func(ctx context.Context, name string, surface SpecSurface, specURL string, allowed map[string]*OperationOverride) (*catalog.Catalog, error)

type ValidationConfig struct {
	Plugins map[string]*ValidationPlugin
}

type ValidationPlugin struct {
	Manifest            *providermanifestv1.Manifest
	ManifestPath        string
	AllowedOperations   map[string]*OperationOverride
	Invokes             []InvocationDependency
	SurfaceURLOverrides map[SpecSurface]string
	ReadStaticCatalog   StaticCatalogReader
	LoadAPICatalog      APICatalogLoader
}

func (p *ValidationPlugin) manifestSpec() *providermanifestv1.Spec {
	if p == nil || p.Manifest == nil {
		return nil
	}
	return p.Manifest.Spec
}

func (p *ValidationPlugin) surfaceURL(spec *providermanifestv1.Spec, surface SpecSurface) (string, bool) {
	if p == nil {
		return "", false
	}
	if p.SurfaceURLOverrides != nil {
		if url := strings.TrimSpace(p.SurfaceURLOverrides[surface]); url != "" {
			return url, true
		}
	}
	url := manifestProviderSurfaceURL(spec, surface)
	if url == "" {
		return "", false
	}
	return resolveManifestRelativeSpecURL(p.ManifestPath, url), true
}

func manifestProviderSurfaceURL(provider *providermanifestv1.Spec, surface SpecSurface) string {
	if provider == nil {
		return ""
	}
	switch surface {
	case SpecSurfaceOpenAPI:
		return provider.OpenAPIDocument()
	case SpecSurfaceGraphQL:
		return provider.GraphQLURL()
	case SpecSurfaceMCP:
		return provider.MCPURL()
	default:
		return ""
	}
}

func resolveManifestRelativeSpecURL(manifestPath, raw string) string {
	if manifestPath == "" || raw == "" {
		return raw
	}
	if filepath.IsAbs(raw) || strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	if strings.HasPrefix(raw, "file://") {
		path := strings.TrimPrefix(raw, "file://")
		if filepath.IsAbs(path) {
			return raw
		}
		return "file://" + filepath.Clean(filepath.Join(filepath.Dir(manifestPath), path))
	}
	return filepath.Clean(filepath.Join(filepath.Dir(manifestPath), raw))
}

func staticCatalogRequired(manifest *providermanifestv1.Manifest) bool {
	return manifest != nil && manifest.Kind == providermanifestv1.KindPlugin && manifest.Spec != nil && !manifest.Spec.IsManifestBacked()
}
