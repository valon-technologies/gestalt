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

func BuildConnectionMaps(cfg *config.Config) ConnectionMaps {
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
			plan := buildPluginConnectionPlan(intg.Plugin, intg.Plugin.ManifestProvider())
			if resolved, ok := plan.configuredSpecSurface(); ok {
				defaultConnection = resolved.connectionName
				apiConnection = resolved.connectionName
			} else if plan.pluginConnection.Auth.Type == "" {
				if name, ok := plan.soleNamedConnection(); ok {
					defaultConnection = name
					apiConnection = name
				}
			}
			if resolved, ok := plan.resolvedSurface(specSurfaceMCP); ok {
				mcpConnection = resolved.connectionName
			} else {
				mcpConnection = defaultConnection
			}
		}

		maps.DefaultConnection[name] = defaultConnection
		maps.APIConnection[name] = apiConnection
		maps.MCPConnection[name] = mcpConnection
	}

	return maps
}

type resolvedSpecSurface struct {
	surface        specSurface
	url            string
	connectionName string
	connection     config.ConnectionDef
}

type pluginConnectionPlan struct {
	pluginConnection config.ConnectionDef
	namedConnections map[string]config.ConnectionDef
	surfaces         map[specSurface]resolvedSpecSurface
}

func buildPluginConnectionPlan(plugin *config.PluginDef, manifestProvider *pluginmanifestv1.Provider) pluginConnectionPlan {
	plan := pluginConnectionPlan{
		pluginConnection: basePluginConnectionDef(plugin, manifestProvider),
		namedConnections: make(map[string]config.ConnectionDef),
		surfaces:         make(map[specSurface]resolvedSpecSurface),
	}

	for name := range namedConnectionNames(plugin, manifestProvider) {
		if name == "" || name == config.PluginConnectionName {
			continue
		}
		plan.namedConnections[name] = resolvedNamedConnectionDef(plugin, manifestProvider, name)
	}

	for _, surface := range []specSurface{specSurfaceOpenAPI, specSurfaceGraphQL, specSurfaceMCP} {
		url := surfaceURL(plugin, manifestProvider, surface)
		if url == "" {
			continue
		}
		resolved := resolvedSpecSurface{
			surface:        surface,
			url:            url,
			connectionName: resolveSurfaceConnectionName(plugin, manifestProvider, surface),
		}
		if resolved.connectionName == config.PluginConnectionName {
			resolved.connection = plan.pluginConnection
		} else {
			resolved.connection = plan.namedConnections[resolved.connectionName]
		}
		plan.surfaces[surface] = resolved
	}

	return plan
}

func (plan pluginConnectionPlan) configuredAPISurface() (resolvedSpecSurface, bool) {
	for _, surface := range []specSurface{specSurfaceOpenAPI, specSurfaceGraphQL} {
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
	return plan.resolvedSurface(specSurfaceMCP)
}

func (plan pluginConnectionPlan) resolvedSurface(surface specSurface) (resolvedSpecSurface, bool) {
	resolved, ok := plan.surfaces[surface]
	return resolved, ok
}

func (plan pluginConnectionPlan) soleNamedConnection() (string, bool) {
	if len(plan.namedConnections) != 1 {
		return "", false
	}
	for name := range plan.namedConnections {
		if name == "" || name == config.PluginConnectionName {
			return "", false
		}
		return name, true
	}
	return "", false
}

func surfaceURL(plugin *config.PluginDef, manifestProvider *pluginmanifestv1.Provider, surface specSurface) string {
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
	if manifestProvider == nil {
		return ""
	}
	var url string
	switch surface {
	case specSurfaceOpenAPI:
		url = manifestProvider.OpenAPI
	case specSurfaceGraphQL:
		url = manifestProvider.GraphQLURL
	case specSurfaceMCP:
		url = manifestProvider.MCPURL
	default:
		return ""
	}
	return resolveManifestRelativeSpecURL(plugin, url)
}

func resolveSurfaceConnectionName(plugin *config.PluginDef, manifestProvider *pluginmanifestv1.Provider, surface specSurface) string {
	name := config.ResolveConnectionAlias(pluginSurfaceConnectionName(plugin, surface))
	if name == "" {
		name = config.ResolveConnectionAlias(manifestSurfaceConnectionName(manifestProvider, surface))
	}
	if name == "" {
		return config.PluginConnectionName
	}
	return name
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
		mergeConnectionAuth(&conn.Auth, manifestAuthToConfig(manifestProvider.Auth))
	}
	return conn
}

func resolvedNamedConnectionDef(plugin *config.PluginDef, manifestProvider *pluginmanifestv1.Provider, name string) config.ConnectionDef {
	conn := config.ConnectionDef{}
	found := false

	if manifestProvider != nil && manifestProvider.Connections != nil {
		if def, ok := manifestProvider.Connections[name]; ok && def != nil {
			found = true
			if def.Mode != "" {
				conn.Mode = def.Mode
			}
			if def.Auth != nil {
				mergeConnectionAuth(&conn.Auth, manifestAuthToConfig(def.Auth))
			}
		}
	}
	if plugin != nil {
		if def, ok := plugin.Connections[name]; ok {
			found = true
			mergeConnectionDef(&conn, def)
		}
	}

	if found {
		return conn
	}
	return basePluginConnectionDef(plugin, manifestProvider)
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
	plan := buildPluginConnectionPlan(intg.Plugin, manifestProvider)
	mcpURL := ""
	if resolved, ok := plan.resolvedSurface(specSurfaceMCP); ok {
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
