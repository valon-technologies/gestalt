package server

import (
	"log/slog"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

// appSurfaceInfo is one way a provider can be called, as its manifest declares
// it. Clients read this instead of inferring surfaces from connection names.
type appSurfaceInfo struct {
	// Kind is openapi, rest, graphql, or mcp.
	Kind config.SurfaceKind `json:"kind"`
	// Connection is the user-facing name of the connection the surface
	// authenticates with, matching the names in the app's connections list.
	// For rest it is the default; individual operations may bind another.
	Connection string `json:"connection,omitempty"`
}

// appSurfacesForPlugin returns an empty list, never nil, so clients can always
// iterate. A plan that fails to build is logged and reported as no surfaces.
func appSurfacesForPlugin(integration string, app *config.ProviderEntry) []appSurfaceInfo {
	if app == nil {
		return []appSurfaceInfo{}
	}
	plan, err := config.BuildStaticConnectionPlan(app, app.ManifestSpec())
	if err != nil {
		slog.Warn("app surfaces unavailable: connection plan failed", "integration", integration, "error", err)
		return []appSurfaceInfo{}
	}
	declared := plan.DeclaredSurfaces()
	surfaces := make([]appSurfaceInfo, 0, len(declared))
	for _, surface := range declared {
		surfaces = append(surfaces, appSurfaceInfo{
			Kind:       surface.Kind,
			Connection: userFacingConnectionName(surface.ConnectionName),
		})
	}
	return surfaces
}

func nonNilSurfaces(surfaces []appSurfaceInfo) []appSurfaceInfo {
	if surfaces == nil {
		return []appSurfaceInfo{}
	}
	return surfaces
}
