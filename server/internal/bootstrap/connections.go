package bootstrap

import (
	"fmt"
	"log/slog"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/pluginpkg"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

type ConnectionMaps struct {
	DefaultConnection map[string]string
	APIConnection     map[string]string
	MCPConnection     map[string]string
}

func BuildConnectionMaps(cfg *config.Config) ConnectionMaps {
	maps := ConnectionMaps{
		DefaultConnection: make(map[string]string, len(cfg.Integrations)),
		APIConnection:     make(map[string]string, len(cfg.Integrations)),
		MCPConnection:     make(map[string]string, len(cfg.Integrations)),
	}

	for name, intg := range cfg.Integrations {
		meta := describeIntegrationConnections(intg)
		maps.DefaultConnection[name] = meta.defaultConnection
		maps.APIConnection[name] = meta.apiConnection
		maps.MCPConnection[name] = meta.mcpConnection
	}

	return maps
}

type integrationConnectionMeta struct {
	defaultConnection string
	apiConnection     string
	mcpConnection     string
}

func describeIntegrationConnections(intg config.IntegrationDef) integrationConnectionMeta {
	meta := integrationConnectionMeta{
		defaultConnection: config.PluginConnectionName,
		apiConnection:     config.PluginConnectionName,
		mcpConnection:     config.PluginConnectionName,
	}
	if intg.Plugin == nil {
		return meta
	}

	manifestProvider := resolvedManifestProvider(intg.Plugin)
	if surface, ok := primaryConfiguredSpecSurface(intg.Plugin, manifestProvider); ok {
		conn := resolvedSurfaceConnectionName(intg.Plugin, manifestProvider, surface)
		meta.defaultConnection = conn
		meta.apiConnection = conn
	} else if basePluginConnectionDef(intg.Plugin, manifestProvider).Auth.Type == "" {
		if name, ok := soleNamedConnection(intg.Plugin, manifestProvider); ok {
			meta.defaultConnection = name
			meta.apiConnection = name
		}
	}
	if surfaceConfigured(intg.Plugin, manifestProvider, specSurfaceMCP) {
		meta.mcpConnection = resolvedSurfaceConnectionName(intg.Plugin, manifestProvider, specSurfaceMCP)
	} else {
		meta.mcpConnection = meta.defaultConnection
	}
	if meta.apiConnection == "" {
		meta.apiConnection = meta.defaultConnection
	}
	if meta.mcpConnection == "" {
		meta.mcpConnection = meta.defaultConnection
	}
	return meta
}

func soleNamedConnection(plugin *config.PluginDef, manifestProvider *pluginmanifestv1.Provider) (string, bool) {
	names := namedConnectionNames(plugin, manifestProvider)
	if len(names) != 1 {
		return "", false
	}
	for name := range names {
		if name == "" || name == config.PluginConnectionName {
			return "", false
		}
		return name, true
	}
	return "", false
}

func primaryConfiguredSpecSurface(plugin *config.PluginDef, manifestProvider *pluginmanifestv1.Provider) (specSurface, bool) {
	for _, surface := range []specSurface{specSurfaceOpenAPI, specSurfaceGraphQL, specSurfaceMCP} {
		if surfaceConfigured(plugin, manifestProvider, surface) {
			return surface, true
		}
	}
	return "", false
}

func surfaceConfigured(plugin *config.PluginDef, manifestProvider *pluginmanifestv1.Provider, surface specSurface) bool {
	return resolvedSurfaceURL(plugin, manifestProvider, surface) != ""
}

func resolvedSurfaceURL(plugin *config.PluginDef, manifestProvider *pluginmanifestv1.Provider, surface specSurface) string {
	if plugin != nil {
		switch surface {
		case specSurfaceOpenAPI:
			if plugin.OpenAPI != "" {
				return plugin.OpenAPI
			}
		case specSurfaceGraphQL:
			if plugin.GraphQLURL != "" {
				return plugin.GraphQLURL
			}
		case specSurfaceMCP:
			if plugin.MCPURL != "" {
				return plugin.MCPURL
			}
		}
	}
	if manifestProvider != nil {
		switch surface {
		case specSurfaceOpenAPI:
			return manifestProvider.OpenAPI
		case specSurfaceGraphQL:
			return manifestProvider.GraphQLURL
		case specSurfaceMCP:
			return manifestProvider.MCPURL
		}
	}
	return ""
}

func resolvedManifestProvider(plugin *config.PluginDef) *pluginmanifestv1.Provider {
	if plugin == nil || plugin.ResolvedManifestPath == "" {
		return nil
	}
	_, manifest, err := pluginpkg.ReadManifestFile(plugin.ResolvedManifestPath)
	if err != nil {
		slog.Warn("reading plugin manifest for connection metadata", "path", plugin.ResolvedManifestPath, "error", err)
		return nil
	}
	if manifest == nil {
		return nil
	}
	return manifest.Provider
}

func resolvedSurfaceConnectionDef(plugin *config.PluginDef, manifestProvider *pluginmanifestv1.Provider, surface specSurface) config.ConnectionDef {
	name := resolvedSurfaceConnectionName(plugin, manifestProvider, surface)
	if name == config.PluginConnectionName {
		return basePluginConnectionDef(plugin, manifestProvider)
	}
	return resolvedNamedConnectionDef(plugin, manifestProvider, name)
}

func resolvedSurfaceConnectionName(plugin *config.PluginDef, manifestProvider *pluginmanifestv1.Provider, surface specSurface) string {
	name := config.ResolveConnectionAlias(pluginSurfaceConnectionName(plugin, surface))
	if name == "" {
		name = config.ResolveConnectionAlias(manifestSurfaceConnectionName(manifestProvider, surface))
	}
	if name == "" {
		return config.PluginConnectionName
	}
	return name
}

func manifestSurfaceConnectionName(provider *pluginmanifestv1.Provider, surface specSurface) string {
	if provider == nil {
		return ""
	}
	switch surface {
	case specSurfaceOpenAPI:
		return provider.OpenAPIConnection
	case specSurfaceGraphQL:
		return provider.GraphQLConnection
	case specSurfaceMCP:
		return provider.MCPConnection
	default:
		return ""
	}
}

func basePluginConnectionDef(plugin *config.PluginDef, manifestProvider *pluginmanifestv1.Provider) config.ConnectionDef {
	conn := config.ConnectionDef{}
	if plugin != nil {
		if plugin.Auth != nil {
			conn.Auth = *plugin.Auth
		}
		conn.Params = plugin.ConnectionParams
	}
	if manifestProvider != nil && manifestProvider.Auth != nil {
		mergeConnectionDef(&conn, manifestTopLevelConnectionDef(manifestProvider))
	}
	return conn
}

func resolvedNamedConnectionDef(plugin *config.PluginDef, manifestProvider *pluginmanifestv1.Provider, name string) config.ConnectionDef {
	conn := basePluginConnectionDef(plugin, manifestProvider)
	if manifestProvider != nil {
		mergeConnectionDef(&conn, manifestNamedConnectionDef(manifestProvider, name))
	}
	if plugin != nil {
		mergeConnectionDef(&conn, plugin.Connections[name])
	}
	return conn
}

func manifestTopLevelConnectionDef(provider *pluginmanifestv1.Provider) *config.ConnectionDef {
	if provider == nil || provider.Auth == nil {
		return nil
	}
	return &config.ConnectionDef{Auth: manifestAuthToConfig(provider.Auth)}
}

func manifestNamedConnectionDef(provider *pluginmanifestv1.Provider, name string) *config.ConnectionDef {
	if provider == nil || provider.Connections == nil {
		return nil
	}
	def, ok := provider.Connections[name]
	if !ok || def == nil {
		return nil
	}
	out := &config.ConnectionDef{Mode: def.Mode}
	if def.Auth != nil {
		out.Auth = manifestAuthToConfig(def.Auth)
	}
	return out
}

func manifestAuthToConfig(auth *pluginmanifestv1.ProviderAuth) config.ConnectionAuthDef {
	if auth == nil {
		return config.ConnectionAuthDef{}
	}
	out := config.ConnectionAuthDef{
		Type:                auth.Type,
		AuthorizationURL:    auth.AuthorizationURL,
		TokenURL:            auth.TokenURL,
		ClientID:            auth.ClientID,
		ClientSecret:        auth.ClientSecret,
		Scopes:              auth.Scopes,
		PKCE:                auth.PKCE,
		ClientAuth:          auth.ClientAuth,
		TokenExchange:       auth.TokenExchange,
		AccessTokenPath:     auth.AccessTokenPath,
		ScopeParam:          auth.ScopeParam,
		ScopeSeparator:      auth.ScopeSeparator,
		AuthorizationParams: auth.AuthorizationParams,
		TokenParams:         auth.TokenParams,
		RefreshParams:       auth.RefreshParams,
		AcceptHeader:        auth.AcceptHeader,
		TokenMetadata:       auth.TokenMetadata,
	}
	if len(auth.Credentials) > 0 {
		out.Credentials = make([]config.CredentialFieldDef, len(auth.Credentials))
		for i, cf := range auth.Credentials {
			out.Credentials[i] = config.CredentialFieldDef{
				Name:        cf.Name,
				Label:       cf.Label,
				Description: cf.Description,
				HelpURL:     cf.HelpURL,
			}
		}
	}
	return out
}

func buildConnectionAuthMap(name string, intg config.IntegrationDef, manifest *pluginmanifestv1.Manifest, pluginConfig map[string]any, deps Deps, regStore *lazyRegStore) (map[string]OAuthHandler, error) {
	manifestProvider := (*pluginmanifestv1.Provider)(nil)
	if manifest != nil {
		manifestProvider = manifest.Provider
	}

	handlers := make(map[string]OAuthHandler)
	if handler, err := buildConnectionHandler(basePluginConnectionDef(intg.Plugin, manifestProvider), effectiveMCPURL(intg.Plugin, manifestProvider), pluginConfig, deps, regStore); err != nil {
		return nil, fmt.Errorf("build plugin connection auth for %q: %w", name, err)
	} else if handler != nil {
		handlers[config.PluginConnectionName] = handler
	}

	for connName := range namedConnectionNames(intg.Plugin, manifestProvider) {
		resolvedName := config.ResolveConnectionAlias(connName)
		if resolvedName == "" || resolvedName == config.PluginConnectionName {
			continue
		}
		conn := resolvedNamedConnectionDef(intg.Plugin, manifestProvider, resolvedName)
		handler, err := buildConnectionHandler(conn, effectiveMCPURL(intg.Plugin, manifestProvider), pluginConfig, deps, regStore)
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
		if resolved != "" {
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
	for _, surface := range []specSurface{specSurfaceOpenAPI, specSurfaceGraphQL, specSurfaceMCP} {
		add(pluginSurfaceConnectionName(plugin, surface))
		add(manifestSurfaceConnectionName(manifestProvider, surface))
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

func effectiveMCPURL(plugin *config.PluginDef, manifestProvider *pluginmanifestv1.Provider) string {
	if plugin != nil && plugin.MCPURL != "" {
		return plugin.MCPURL
	}
	if manifestProvider != nil {
		return manifestProvider.MCPURL
	}
	return ""
}
