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

func (s SpecSurface) NamedConnectionRequirementContext() string {
	switch s {
	case SpecSurfaceOpenAPI:
		return "using openapi with named connections and no top-level auth"
	case SpecSurfaceGraphQL:
		return "using graphql_url with named connections and no top-level auth"
	case SpecSurfaceMCP:
		return "using mcp_url with named connections and no top-level auth"
	default:
		return "using a spec surface with named connections and no top-level auth"
	}
}

func (p *PluginDef) SurfaceURL(surface SpecSurface) string {
	if p == nil {
		return ""
	}
	switch surface {
	case SpecSurfaceOpenAPI:
		return p.OpenAPI
	case SpecSurfaceGraphQL:
		return p.GraphQLURL
	case SpecSurfaceMCP:
		return p.MCPURL
	default:
		return ""
	}
}

func (p *PluginDef) SurfaceConnectionName(surface SpecSurface) string {
	if p == nil {
		return ""
	}
	switch surface {
	case SpecSurfaceOpenAPI:
		return p.OpenAPIConnection
	case SpecSurfaceGraphQL:
		return p.GraphQLConnection
	case SpecSurfaceMCP:
		return p.MCPConnection
	default:
		return ""
	}
}

func ManifestProviderSurfaceURL(provider *pluginmanifestv1.Provider, surface SpecSurface) string {
	if provider == nil {
		return ""
	}
	switch surface {
	case SpecSurfaceOpenAPI:
		return provider.OpenAPI
	case SpecSurfaceGraphQL:
		return provider.GraphQLURL
	case SpecSurfaceMCP:
		return provider.MCPURL
	default:
		return ""
	}
}

func ManifestProviderSurfaceConnectionName(provider *pluginmanifestv1.Provider, surface SpecSurface) string {
	if provider == nil {
		return ""
	}
	switch surface {
	case SpecSurfaceOpenAPI:
		return provider.OpenAPIConnection
	case SpecSurfaceGraphQL:
		return provider.GraphQLConnection
	case SpecSurfaceMCP:
		return provider.MCPConnection
	default:
		return ""
	}
}
