package bootstrap

import (
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
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

type pluginConnectionPlan struct {
	pluginConnection  config.ConnectionDef
	namedConnections  map[string]config.ConnectionDef
	defaultConnection string
}

func buildPluginConnectionPlan(plugin *config.PluginDef, manifestProvider *pluginmanifestv1.Provider) (pluginConnectionPlan, error) {
	declaredNames := namedConnectionNames(plugin, manifestProvider)
	plan := pluginConnectionPlan{
		pluginConnection: config.EffectivePluginConnectionDef(plugin, manifestProvider),
		namedConnections: make(map[string]config.ConnectionDef),
	}

	for name := range declaredNames {
		conn, ok := config.EffectiveNamedConnectionDef(plugin, manifestProvider, name)
		if !ok {
			continue
		}
		plan.namedConnections[name] = conn
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

func (plan pluginConnectionPlan) authDefaultConnection() string {
	return plan.fallbackConnection()
}

func (plan pluginConnectionPlan) apiConnection() string {
	return plan.fallbackConnection()
}

func (plan pluginConnectionPlan) mcpConnection() string {
	return plan.fallbackConnection()
}

func (plan pluginConnectionPlan) fallbackConnection() string {
	if plan.defaultConnection != "" {
		return plan.defaultConnection
	}
	if len(plan.namedConnections) == 0 {
		return config.PluginConnectionName
	}
	return ""
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
	for _, conn := range plan.namedConnections {
		addMode(connectionModeForConnection(conn))
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
	case "", pluginmanifestv1.AuthTypeNone:
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

func buildConnectionAuthMap(name string, intg config.IntegrationDef, manifest *pluginmanifestv1.Manifest, pluginConfig map[string]any, deps Deps) (map[string]OAuthHandler, error) {
	manifestProvider := (*pluginmanifestv1.Provider)(nil)
	if manifest != nil {
		manifestProvider = manifest.Provider
	}
	plan, err := buildPluginConnectionPlan(intg.Plugin, manifestProvider)
	if err != nil {
		return nil, fmt.Errorf("resolve connections for %q: %w", name, err)
	}

	handlers := make(map[string]OAuthHandler)
	if handler, err := buildConnectionHandler(plan.pluginConnection, pluginConfig, deps); err != nil {
		return nil, fmt.Errorf("build plugin connection auth for %q: %w", name, err)
	} else if handler != nil {
		handlers[config.PluginConnectionName] = handler
	}

	for resolvedName := range plan.namedConnections {
		conn := plan.namedConnections[resolvedName]
		handler, err := buildConnectionHandler(conn, pluginConfig, deps)
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

func buildConnectionHandler(conn config.ConnectionDef, pluginConfig map[string]any, deps Deps) (OAuthHandler, error) {
	switch conn.Auth.Type {
	case "", pluginmanifestv1.AuthTypeOAuth2:
		return buildOAuthHandlerFromAuth(&conn.Auth, pluginConfig, deps)
	case pluginmanifestv1.AuthTypeMCPOAuth:
		return nil, fmt.Errorf("mcp_oauth auth is no longer supported for executable providers")
	default:
		return nil, nil
	}
}
