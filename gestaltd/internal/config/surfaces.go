package config

import (
	"strings"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/packageio"
)

type SpecSurface string

const (
	SpecSurfaceOpenAPI SpecSurface = "openapi"
	SpecSurfaceGraphQL SpecSurface = "graphql"
	SpecSurfaceMCP     SpecSurface = "mcp"
)

var OrderedSpecSurfaces = []SpecSurface{
	SpecSurfaceOpenAPI,
	SpecSurfaceGraphQL,
	SpecSurfaceMCP,
}

func (s SpecSurface) ConnectionField() string {
	switch s {
	case SpecSurfaceOpenAPI:
		return "openapi_connection"
	case SpecSurfaceGraphQL:
		return "graphql_connection"
	case SpecSurfaceMCP:
		return "mcp_connection"
	default:
		return "connection"
	}
}

func ManifestProviderSurfaceURL(provider *providermanifestv1.Spec, surface SpecSurface) string {
	return packageio.ManifestProviderSurfaceURL(provider, string(surface))
}

func ManifestProviderSurfaceConnectionName(provider *providermanifestv1.Spec, surface SpecSurface) string {
	return provider.SurfaceConnectionName(string(surface))
}

func ProviderSurfaceURLOverride(entry *ProviderEntry, surface SpecSurface) string {
	if entry != nil && entry.Surfaces != nil {
		switch surface {
		case SpecSurfaceGraphQL:
			if entry.Surfaces.GraphQL != nil {
				if url := strings.TrimSpace(entry.Surfaces.GraphQL.URL); url != "" {
					return url
				}
			}
		case SpecSurfaceMCP:
			if entry.Surfaces.MCP != nil {
				if url := strings.TrimSpace(entry.Surfaces.MCP.URL); url != "" {
					return url
				}
			}
		}
	}
	return ""
}

func EffectiveProviderSpecBaseURL(entry *ProviderEntry, provider *providermanifestv1.Spec) string {
	if entry != nil && entry.Surfaces != nil {
		if entry.Surfaces.REST != nil {
			if baseURL := strings.TrimSpace(entry.Surfaces.REST.BaseURL); baseURL != "" {
				return baseURL
			}
		}
		if entry.Surfaces.OpenAPI != nil {
			if baseURL := strings.TrimSpace(entry.Surfaces.OpenAPI.BaseURL); baseURL != "" {
				return baseURL
			}
		}
	}
	return provider.SpecBaseURL()
}

func ResolveManifestRelativeSpecURL(entry *ProviderEntry, raw string) string {
	if entry == nil {
		return raw
	}
	return packageio.ResolveManifestRelativeSpecURL(entry.ResolvedManifestPath, raw)
}
