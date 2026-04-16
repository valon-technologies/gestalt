package bootstrap

import (
	"fmt"

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
		DefaultConnection: make(map[string]string, len(cfg.Plugins)),
		APIConnection:     make(map[string]string, len(cfg.Plugins)),
		MCPConnection:     make(map[string]string, len(cfg.Plugins)),
	}

	for name, entry := range cfg.Plugins {
		defaultConnection := config.PluginConnectionName
		apiConnection := config.PluginConnectionName
		mcpConnection := config.PluginConnectionName

		if entry != nil {
			plan, err := config.BuildStaticConnectionPlan(entry, entry.ManifestSpec())
			if err != nil {
				return ConnectionMaps{}, fmt.Errorf("integration %q: %w", name, err)
			}
			defaultConnection = plan.AuthDefaultConnection()
			apiConnection = plan.APIConnection()
			mcpConnection = plan.MCPConnection()
		}

		maps.DefaultConnection[name] = defaultConnection
		maps.APIConnection[name] = apiConnection
		maps.MCPConnection[name] = mcpConnection
	}

	return maps, nil
}

func buildConnectionAuthMap(name string, entry *config.ProviderEntry, manifest *providermanifestv1.Manifest, pluginConfig map[string]any, authFallback *specAuthFallback, deps Deps) (map[string]OAuthHandler, error) {
	manifestPlugin := (*providermanifestv1.Spec)(nil)
	if manifest != nil {
		manifestPlugin = manifest.Spec
	}
	plan, err := config.BuildStaticConnectionPlan(entry, manifestPlugin)
	if err != nil {
		return nil, fmt.Errorf("resolve connections for %q: %w", name, err)
	}

	mcpURL := ""
	if resolved, ok := plan.ResolvedSurface(config.SpecSurfaceMCP); ok {
		mcpURL = resolved.URL
	}

	specAuthForConnection := func(connectionName string) *provider.Definition {
		if authFallback == nil || authFallback.definition == nil || authFallback.connectionName != connectionName {
			return nil
		}
		return authFallback.definition
	}

	handlers := make(map[string]OAuthHandler)
	if handler, err := buildConnectionHandler(plan.PluginConnection(), mcpURL, pluginConfig, specAuthForConnection(config.PluginConnectionName), deps); err != nil {
		return nil, fmt.Errorf("build plugin connection auth for %q: %w", name, err)
	} else if handler != nil {
		handlers[config.PluginConnectionName] = handler
	}

	for _, resolvedName := range plan.NamedConnectionNames() {
		conn, _ := plan.NamedConnectionDef(resolvedName)
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
