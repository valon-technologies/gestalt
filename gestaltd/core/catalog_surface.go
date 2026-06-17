package core

import "context"

// CatalogSurface scopes session catalog discovery to a provider surface.
// An empty value means all surfaces (legacy compatibility).
type CatalogSurface string

const (
	CatalogSurfaceAll CatalogSurface = ""
	CatalogSurfaceAPI CatalogSurface = "api"
	CatalogSurfaceMCP CatalogSurface = "mcp"
)

type catalogSurfaceContextKey struct{}

func WithCatalogSurface(ctx context.Context, surface CatalogSurface) context.Context {
	if surface == CatalogSurfaceAll {
		return ctx
	}
	return context.WithValue(ctx, catalogSurfaceContextKey{}, surface)
}

func CatalogSurfaceFromContext(ctx context.Context) CatalogSurface {
	if ctx == nil {
		return CatalogSurfaceAll
	}
	surface, _ := ctx.Value(catalogSurfaceContextKey{}).(CatalogSurface)
	return surface
}
