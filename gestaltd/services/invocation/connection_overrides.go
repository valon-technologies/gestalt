package invocation

// ConnectionSurface names the routing table a connection lookup reads.
type ConnectionSurface string

const (
	// ConnectionSurfaceDefault is the compatibility default connection.
	ConnectionSurfaceDefault ConnectionSurface = "default"
	// ConnectionSurfaceAPI routes REST-visible operations and API catalogs.
	ConnectionSurfaceAPI ConnectionSurface = "api"
	// ConnectionSurfaceMCP routes MCP session operations and catalogs.
	ConnectionSurfaceMCP ConnectionSurface = "mcp"
)

// ConnectionOverrides supplies connection routing resolved after startup.
// Static routing maps are built from config at boot, but a registry-installed
// app's connection plan is only known once a package version is installed.
// ok=false means no override: callers use the static map.
type ConnectionOverrides interface {
	ConnectionName(surface ConnectionSurface, provider string) (string, bool)
}

// ConnectionNameFor returns provider's connection for surface, preferring
// overrides over the static map.
func ConnectionNameFor(overrides ConnectionOverrides, surface ConnectionSurface, static map[string]string, provider string) string {
	if overrides != nil {
		if name, ok := overrides.ConnectionName(surface, provider); ok {
			return name
		}
	}
	return static[provider]
}

// OverriddenConnectionMap is a ConnectionMapper over a static map whose
// entries ConnectionOverrides can replace at runtime.
type OverriddenConnectionMap struct {
	Static    map[string]string
	Overrides ConnectionOverrides
	Surface   ConnectionSurface
}

func (m OverriddenConnectionMap) ConnectionForProvider(provider string) string {
	return ConnectionNameFor(m.Overrides, m.Surface, m.Static, provider)
}
