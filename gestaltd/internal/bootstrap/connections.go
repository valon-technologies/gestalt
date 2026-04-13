package bootstrap

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/provider"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

type ConnectionMaps struct {
	DefaultConnection map[string]string
	APIConnection     map[string]string
	MCPConnection     map[string]string
}

func BuildConnectionMaps(cfg *config.Config) (ConnectionMaps, error) {
	maps := ConnectionMaps{
		DefaultConnection: make(map[string]string, len(cfg.Providers.Plugins)),
		APIConnection:     make(map[string]string, len(cfg.Providers.Plugins)),
		MCPConnection:     make(map[string]string, len(cfg.Providers.Plugins)),
	}

	for name, entry := range cfg.Providers.Plugins {
		defaultConnection := config.PluginConnectionName
		apiConnection := config.PluginConnectionName
		mcpConnection := config.PluginConnectionName

		if entry != nil {
			plan, err := buildPluginConnectionPlan(entry, entry.ManifestSpec())
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

type pluginConnectionPlan struct {
	manifestBacked    bool
	pluginConnection  config.ConnectionDef
	namedConnections  map[string]config.ConnectionDef
	surfaces          map[config.SpecSurface]resolvedSpecSurface
	restConnection    string
	defaultConnection string
}

type resolvedSpecSurface struct {
	surface        config.SpecSurface
	url            string
	connectionName string
	connection     config.ConnectionDef
}

func buildPluginConnectionPlan(plugin *config.ProviderEntry, manifestPlugin *providermanifestv1.Spec) (pluginConnectionPlan, error) {
	declaredNames := namedConnectionNames(plugin, manifestPlugin)
	plan := pluginConnectionPlan{
		manifestBacked:   manifestPlugin != nil && manifestPlugin.IsManifestBacked(),
		pluginConnection: config.EffectivePluginConnectionDef(plugin, manifestPlugin),
		namedConnections: make(map[string]config.ConnectionDef),
		surfaces:         make(map[config.SpecSurface]resolvedSpecSurface),
	}

	for name := range declaredNames {
		conn, ok := config.EffectiveNamedConnectionDef(plugin, manifestPlugin, name)
		if !ok {
			continue
		}
		plan.namedConnections[name] = conn
	}

	defaultConnection := resolveDefaultConnectionName(plugin, manifestPlugin)
	if defaultConnection != "" {
		if _, err := plan.connectionDef(defaultConnection); err != nil {
			return pluginConnectionPlan{}, fmt.Errorf("default_connection references undeclared connection %q", defaultConnection)
		}
		plan.defaultConnection = defaultConnection
	}

	if manifestPlugin != nil && manifestPlugin.Surfaces != nil && manifestPlugin.Surfaces.REST != nil {
		plan.restConnection = plan.resolveSurfaceConnectionName(manifestPlugin.Surfaces.REST.Connection)
		if plan.restConnection != "" {
			if _, err := plan.connectionDef(plan.restConnection); err != nil {
				return pluginConnectionPlan{}, fmt.Errorf("rest connection references undeclared connection %q", plan.restConnection)
			}
		}
	}

	for _, surface := range config.OrderedSpecSurfaces {
		url := surfaceURL(plugin, manifestPlugin, surface)
		if url == "" {
			continue
		}
		resolved := resolvedSpecSurface{
			surface:        surface,
			url:            url,
			connectionName: plan.resolveSurfaceConnectionName(config.ManifestProviderSurfaceConnectionName(manifestPlugin, surface)),
		}
		conn, err := plan.connectionDef(resolved.connectionName)
		if err != nil {
			return pluginConnectionPlan{}, fmt.Errorf("%s references undeclared connection %q", surface.ConnectionField(), resolved.connectionName)
		}
		resolved.connection = conn
		plan.surfaces[surface] = resolved
	}

	return plan, nil
}

func AdvertisedConnectionNames(plugin *config.ProviderEntry) ([]string, error) {
	if plugin == nil {
		return []string{}, nil
	}
	plan, err := buildPluginConnectionPlan(plugin, plugin.ManifestSpec())
	if err != nil {
		return nil, err
	}
	return plan.advertisedConnectionNames(), nil
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
	return plan.fallbackConnection()
}

func (plan pluginConnectionPlan) apiConnection() string {
	if resolved, ok := plan.configuredAPISurface(); ok {
		return resolved.connectionName
	}
	if plan.restConnection != "" {
		return plan.restConnection
	}
	return plan.fallbackConnection()
}

func (plan pluginConnectionPlan) mcpConnection() string {
	if resolved, ok := plan.resolvedSurface(config.SpecSurfaceMCP); ok {
		return resolved.connectionName
	}
	return plan.fallbackConnection()
}

func (plan pluginConnectionPlan) fallbackConnection() string {
	if plan.defaultConnection != "" {
		return plan.defaultConnection
	}
	if len(plan.namedConnections) == 0 {
		return config.PluginConnectionName
	}
	if len(plan.namedConnections) == 1 {
		for name := range plan.namedConnections {
			return name
		}
	}
	return ""
}

func (plan pluginConnectionPlan) resolveSurfaceConnectionName(raw string) string {
	if name := config.ResolveConnectionAlias(raw); name != "" {
		return name
	}
	if plan.defaultConnection != "" {
		return plan.defaultConnection
	}
	if len(plan.namedConnections) == 1 {
		for name := range plan.namedConnections {
			return name
		}
	}
	return config.PluginConnectionName
}

func (plan pluginConnectionPlan) shouldAdvertisePluginConnection() bool {
	if !plan.manifestBacked {
		return true
	}
	if len(plan.namedConnections) == 0 {
		return true
	}
	if plan.defaultConnection == config.PluginConnectionName {
		return true
	}
	if plan.apiConnection() == config.PluginConnectionName {
		return true
	}
	if plan.mcpConnection() == config.PluginConnectionName {
		return true
	}
	return false
}

func (plan pluginConnectionPlan) advertisedConnectionNames() []string {
	names := make([]string, 0, len(plan.namedConnections))
	for name := range plan.namedConnections {
		names = append(names, name)
	}
	sort.Strings(names)
	if !plan.shouldAdvertisePluginConnection() {
		return names
	}
	return append([]string{config.PluginConnectionName}, names...)
}

func (plan pluginConnectionPlan) connectionMode() core.ConnectionMode {
	needUser := false
	needIdentity := false

	addMode := func(mode core.ConnectionMode) {
		switch mode {
		case core.ConnectionModeUser:
			needUser = true
		case core.ConnectionModeIdentity:
			needIdentity = true
		case core.ConnectionModeEither:
			needUser = true
			needIdentity = true
		}
	}

	addMode(connectionModeForConnection(plan.pluginConnection))
	for name := range plan.namedConnections {
		addMode(connectionModeForConnection(plan.namedConnections[name]))
	}

	switch {
	case needUser && needIdentity:
		return core.ConnectionModeEither
	case needUser:
		return core.ConnectionModeUser
	case needIdentity:
		return core.ConnectionModeIdentity
	default:
		return core.ConnectionModeNone
	}
}

func connectionModeForConnection(conn config.ConnectionDef) core.ConnectionMode {
	if conn.Mode != "" {
		return core.ConnectionMode(conn.Mode)
	}
	switch conn.Auth.Type {
	case "", providermanifestv1.AuthTypeNone:
		return core.ConnectionModeNone
	default:
		return core.ConnectionModeUser
	}
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

func resolveDefaultConnectionName(plugin *config.ProviderEntry, manifestPlugin *providermanifestv1.Spec) string {
	if plugin != nil {
		if name := config.ResolveConnectionAlias(plugin.DefaultConnection); name != "" {
			return name
		}
	}
	if manifestPlugin != nil {
		if name := config.ResolveConnectionAlias(manifestPlugin.DefaultConnection); name != "" {
			return name
		}
	}
	return ""
}

func surfaceURL(plugin *config.ProviderEntry, manifestPlugin *providermanifestv1.Spec, surface config.SpecSurface) string {
	if url := config.ProviderSurfaceURLOverride(plugin, surface); url != "" {
		return url
	}
	url := config.ManifestProviderSurfaceURL(manifestPlugin, surface)
	if url == "" {
		return ""
	}
	return resolveManifestRelativeSpecURL(plugin, url)
}

func resolveManifestRelativeSpecURL(plugin *config.ProviderEntry, raw string) string {
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

func buildConnectionAuthMap(name string, entry *config.ProviderEntry, manifest *providermanifestv1.Manifest, pluginConfig map[string]any, authFallback *specAuthFallback, deps Deps) (map[string]OAuthHandler, error) {
	manifestPlugin := (*providermanifestv1.Spec)(nil)
	if manifest != nil {
		manifestPlugin = manifest.Spec
	}
	plan, err := buildPluginConnectionPlan(entry, manifestPlugin)
	if err != nil {
		return nil, fmt.Errorf("resolve connections for %q: %w", name, err)
	}

	mcpURL := ""
	if resolved, ok := plan.resolvedSurface(config.SpecSurfaceMCP); ok {
		mcpURL = resolved.url
	}

	specAuthForConnection := func(connectionName string) *provider.Definition {
		if authFallback == nil || authFallback.definition == nil || authFallback.connectionName != connectionName {
			return nil
		}
		return authFallback.definition
	}

	handlers := make(map[string]OAuthHandler)
	if handler, err := buildConnectionHandler(plan.pluginConnection, mcpURL, pluginConfig, specAuthForConnection(config.PluginConnectionName), deps); err != nil {
		return nil, fmt.Errorf("build plugin connection auth for %q: %w", name, err)
	} else if handler != nil {
		handlers[config.PluginConnectionName] = handler
	}

	for resolvedName := range plan.namedConnections {
		conn := plan.namedConnections[resolvedName]
		handler, err := buildConnectionHandler(conn, mcpURL, pluginConfig, specAuthForConnection(resolvedName), deps)
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

func namedConnectionNames(plugin *config.ProviderEntry, manifestPlugin *providermanifestv1.Spec) map[string]struct{} {
	names := make(map[string]struct{})
	add := func(name string) {
		resolved := config.ResolveConnectionAlias(name)
		if resolved != "" && resolved != config.PluginConnectionName {
			names[resolved] = struct{}{}
		}
	}
	if manifestPlugin != nil {
		for name := range manifestPlugin.Connections {
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

func buildConnectionHandler(conn config.ConnectionDef, mcpURL string, pluginConfig map[string]any, specDef *provider.Definition, deps Deps) (OAuthHandler, error) {
	switch conn.Auth.Type {
	case "", providermanifestv1.AuthTypeOAuth2:
		handler, err := buildOAuthHandlerFromAuth(&conn.Auth, pluginConfig, deps)
		if err != nil || handler != nil || conn.Auth.Type == providermanifestv1.AuthTypeOAuth2 {
			return handler, err
		}
		return buildOAuthHandlerFromDefinition(specDef, conn, pluginConfig, deps)
	case providermanifestv1.AuthTypeMCPOAuth:
		if mcpURL == "" {
			return nil, fmt.Errorf("mcp_oauth auth requires mcp_url")
		}
		return buildMCPOAuthHandler(conn, mcpURL, buildRegistrationStore(deps), deps), nil
	default:
		return nil, nil
	}
}
