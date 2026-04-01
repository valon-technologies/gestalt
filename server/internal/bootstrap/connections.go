package bootstrap

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

type ConnectionMaps struct {
	DefaultConnection map[string]string
	APIConnection     map[string]string
	MCPConnection     map[string]string
}

func BuildConnectionMaps(cfg *config.Config) (ConnectionMaps, error) {
	maps := ConnectionMaps{
		DefaultConnection: make(map[string]string, len(cfg.Integrations)),
		APIConnection:     make(map[string]string, len(cfg.Integrations)),
		MCPConnection:     make(map[string]string, len(cfg.Integrations)),
	}

	for name, intg := range cfg.Integrations {
		defaultConnection := config.PluginConnectionName
		apiConnection := config.PluginConnectionName
		mcpConnection := config.PluginConnectionName

		if intg.Plugin != nil {
			plan, err := buildPluginConnectionPlan(intg.Plugin, intg.Plugin.ManifestProvider())
			if err != nil {
				return ConnectionMaps{}, fmt.Errorf("integration %q: %w", name, err)
			}
			defaultConnection = plan.authDefaultConnection()
			apiConnection = plan.apiConnection()
			mcpConnection = plan.mcpConnection()
		}

		maps.DefaultConnection[name] = defaultConnection
		maps.APIConnection[name] = apiConnection
		maps.MCPConnection[name] = mcpConnection
	}

	return maps, nil
}

type resolvedSpecSurface struct {
	surface        config.SpecSurface
	url            string
	connectionName string
	connection     config.ConnectionDef
}

type pluginConnectionPlan struct {
	pluginConnection  config.ConnectionDef
	namedConnections  map[string]config.ConnectionDef
	surfaces          map[config.SpecSurface]resolvedSpecSurface
	defaultConnection string
}

func buildPluginConnectionPlan(plugin *config.PluginDef, manifestProvider *pluginmanifestv1.Provider) (pluginConnectionPlan, error) {
	declaredNames := namedConnectionNames(plugin, manifestProvider)
	plan := pluginConnectionPlan{
		pluginConnection: config.EffectivePluginConnectionDef(plugin, manifestProvider),
		namedConnections: make(map[string]config.ConnectionDef),
		surfaces:         make(map[config.SpecSurface]resolvedSpecSurface),
	}

	for name := range declaredNames {
		conn, ok := config.EffectiveNamedConnectionDef(plugin, manifestProvider, name)
		if !ok {
			continue
		}
		plan.namedConnections[name] = conn
	}

	for _, surface := range config.OrderedSpecSurfaces {
		url := surfaceURL(plugin, manifestProvider, surface)
		if url == "" {
			continue
		}
		resolved := resolvedSpecSurface{
			surface:        surface,
			url:            url,
			connectionName: resolveSurfaceConnectionName(plugin, manifestProvider, surface),
		}
		conn, err := plan.connectionDef(resolved.connectionName)
		if err != nil {
			return pluginConnectionPlan{}, fmt.Errorf("%s references undeclared connection %q", surface.ConnectionField(), resolved.connectionName)
		}
		resolved.connection = conn
		plan.surfaces[surface] = resolved
	}

	defaultConnection := resolveDefaultConnectionName(plugin, manifestProvider)
	if defaultConnection != "" {
		if _, err := plan.connectionDef(defaultConnection); err != nil {
			return pluginConnectionPlan{}, fmt.Errorf("default_connection references undeclared connection %q", defaultConnection)
		}
		plan.defaultConnection = defaultConnection
	}

	return plan, nil
}

func (plan pluginConnectionPlan) configuredAPISurface() (resolvedSpecSurface, bool) {
	for _, surface := range []config.SpecSurface{config.SpecSurfaceOpenAPI, config.SpecSurfaceGraphQL} {
		if resolved, ok := plan.resolvedSurface(surface); ok {
			return resolved, true
		}
	}
	return resolvedSpecSurface{}, false
}

func (plan pluginConnectionPlan) configuredSpecSurface() (resolvedSpecSurface, bool) {
	if resolved, ok := plan.configuredAPISurface(); ok {
		return resolved, true
	}
	return plan.resolvedSurface(config.SpecSurfaceMCP)
}

func (plan pluginConnectionPlan) resolvedSurface(surface config.SpecSurface) (resolvedSpecSurface, bool) {
	resolved, ok := plan.surfaces[surface]
	return resolved, ok
}

func (plan pluginConnectionPlan) authDefaultConnection() string {
	if plan.defaultConnection != "" {
		return plan.defaultConnection
	}
	if len(plan.namedConnections) == 0 {
		return config.PluginConnectionName
	}
	return ""
}

func (plan pluginConnectionPlan) apiConnection() string {
	if resolved, ok := plan.configuredAPISurface(); ok {
		return resolved.connectionName
	}
	if plan.defaultConnection != "" {
		return plan.defaultConnection
	}
	if len(plan.namedConnections) == 0 {
		return config.PluginConnectionName
	}
	return ""
}

func (plan pluginConnectionPlan) mcpConnection() string {
	if resolved, ok := plan.resolvedSurface(config.SpecSurfaceMCP); ok {
		return resolved.connectionName
	}
	if plan.defaultConnection != "" {
		return plan.defaultConnection
	}
	if len(plan.namedConnections) == 0 {
		return config.PluginConnectionName
	}
	return ""
}

func (plan pluginConnectionPlan) connectionDef(name string) (config.ConnectionDef, error) {
	if name == "" || name == config.PluginConnectionName {
		return plan.pluginConnection, nil
	}
	conn, ok := plan.namedConnections[name]
	if !ok {
		return config.ConnectionDef{}, fmt.Errorf("undeclared connection %q", name)
	}
	return conn, nil
}

func surfaceURL(plugin *config.PluginDef, manifestProvider *pluginmanifestv1.Provider, surface config.SpecSurface) string {
	if pluginURL := plugin.SurfaceURL(surface); pluginURL != "" {
		return pluginURL
	}
	url := config.ManifestProviderSurfaceURL(manifestProvider, surface)
	if url == "" {
		return ""
	}
	return resolveManifestRelativeSpecURL(plugin, url)
}

func resolveSurfaceConnectionName(plugin *config.PluginDef, manifestProvider *pluginmanifestv1.Provider, surface config.SpecSurface) string {
	name := config.ResolveConnectionAlias(plugin.SurfaceConnectionName(surface))
	if name == "" {
		name = config.ResolveConnectionAlias(config.ManifestProviderSurfaceConnectionName(manifestProvider, surface))
	}
	if name == "" {
		return config.PluginConnectionName
	}
	return name
}

func resolveDefaultConnectionName(plugin *config.PluginDef, manifestProvider *pluginmanifestv1.Provider) string {
	if plugin != nil {
		if name := config.ResolveConnectionAlias(plugin.DefaultConnection); name != "" {
			return name
		}
	}
	if manifestProvider != nil {
		if name := config.ResolveConnectionAlias(manifestProvider.DefaultConnection); name != "" {
			return name
		}
	}
	return ""
}

func resolveManifestRelativeSpecURL(plugin *config.PluginDef, raw string) string {
	if plugin == nil || plugin.ResolvedManifestPath == "" || raw == "" {
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
		return "file://" + filepath.Clean(filepath.Join(filepath.Dir(plugin.ResolvedManifestPath), path))
	}
	return filepath.Clean(filepath.Join(filepath.Dir(plugin.ResolvedManifestPath), raw))
}

func buildConnectionAuthMap(name string, intg config.IntegrationDef, manifest *pluginmanifestv1.Manifest, pluginConfig map[string]any, deps Deps, regStore *lazyRegStore) (map[string]OAuthHandler, error) {
	manifestProvider := (*pluginmanifestv1.Provider)(nil)
	if manifest != nil {
		manifestProvider = manifest.Provider
	}
	plan, err := buildPluginConnectionPlan(intg.Plugin, manifestProvider)
	if err != nil {
		return nil, fmt.Errorf("resolve connections for %q: %w", name, err)
	}
	mcpURL := ""
	if resolved, ok := plan.resolvedSurface(config.SpecSurfaceMCP); ok {
		mcpURL = resolved.url
	}

	handlers := make(map[string]OAuthHandler)
	if handler, err := buildConnectionHandler(plan.pluginConnection, mcpURL, pluginConfig, deps, regStore); err != nil {
		return nil, fmt.Errorf("build plugin connection auth for %q: %w", name, err)
	} else if handler != nil {
		handlers[config.PluginConnectionName] = handler
	}

	for resolvedName := range plan.namedConnections {
		conn := plan.namedConnections[resolvedName]
		handler, err := buildConnectionHandler(conn, mcpURL, pluginConfig, deps, regStore)
		if err != nil {
			return nil, fmt.Errorf("build named connection auth for %q/%q: %w", name, resolvedName, err)
		}
		if handler != nil {
			handlers[resolvedName] = handler
		}
	}

	if len(handlers) == 0 {
		return nil, nil
	}
	return handlers, nil
}

func namedConnectionNames(plugin *config.PluginDef, manifestProvider *pluginmanifestv1.Provider) map[string]struct{} {
	names := make(map[string]struct{})
	add := func(name string) {
		resolved := config.ResolveConnectionAlias(name)
		if resolved != "" && resolved != config.PluginConnectionName {
			names[resolved] = struct{}{}
		}
	}
	if manifestProvider != nil {
		for name := range manifestProvider.Connections {
			add(name)
		}
	}
	if plugin != nil {
		for name := range plugin.Connections {
			add(name)
		}
	}
	return names
}

func buildConnectionHandler(conn config.ConnectionDef, mcpURL string, pluginConfig map[string]any, deps Deps, regStore *lazyRegStore) (OAuthHandler, error) {
	switch conn.Auth.Type {
	case "", pluginmanifestv1.AuthTypeOAuth2:
		return buildOAuthHandlerFromAuth(&conn.Auth, pluginConfig, deps)
	case pluginmanifestv1.AuthTypeMCPOAuth:
		if mcpURL == "" {
			return nil, fmt.Errorf("mcp_oauth auth requires mcp_url")
		}
		if regStore == nil {
			return buildMCPOAuthHandler(conn, mcpURL, nil, deps), nil
		}
		return buildMCPOAuthHandler(conn, mcpURL, regStore.get(), deps), nil
	default:
		return nil, nil
	}
}
