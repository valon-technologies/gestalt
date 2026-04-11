package config

import pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"

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

func ManifestProviderSurfaceURL(provider *pluginmanifestv1.Spec, surface SpecSurface) string {
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

func ManifestProviderSurfaceConnectionName(provider *pluginmanifestv1.Spec, surface SpecSurface) string {
	if provider == nil {
		return ""
	}
	return provider.SurfaceConnectionName(string(surface))
}
