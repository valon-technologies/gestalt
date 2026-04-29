package bootstrap

import (
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
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

func BuildConnectionRuntime(cfg *config.Config) (invocation.ConnectionRuntimeMap, error) {
	runtime := invocation.ConnectionRuntimeMap{}
	if cfg == nil {
		return runtime, nil
	}

	for name, entry := range cfg.Plugins {
		if entry == nil {
			continue
		}
		plan, err := config.BuildStaticConnectionPlan(entry, entry.ManifestSpec())
		if err != nil {
			return nil, fmt.Errorf("integration %q: %w", name, err)
		}
		addRuntimeInfo := func(connName string, conn *config.ConnectionDef) error {
			info, err := connectionRuntimeInfo(name, connName, conn)
			if err != nil {
				return err
			}
			if _, ok := runtime[name]; !ok {
				runtime[name] = map[string]invocation.ConnectionRuntimeInfo{}
			}
			runtime[name][connName] = info
			return nil
		}

		pluginConn := plan.PluginConnection()
		if err := addRuntimeInfo(config.PluginConnectionName, &pluginConn); err != nil {
			return nil, err
		}
		for _, connName := range plan.NamedConnectionNames() {
			conn, _ := plan.NamedConnectionDef(connName)
			if err := addRuntimeInfo(connName, &conn); err != nil {
				return nil, err
			}
		}
	}
	return runtime, nil
}

func connectionRuntimeInfo(integration, connection string, conn *config.ConnectionDef) (invocation.ConnectionRuntimeInfo, error) {
	mode := config.ConnectionModeForConnection(*conn)
	info := invocation.ConnectionRuntimeInfo{Mode: mode}
	if mode != core.ConnectionModePlatform {
		return info, nil
	}
	if len(conn.Auth.Credentials) > 0 {
		return invocation.ConnectionRuntimeInfo{}, fmt.Errorf("integration %q connection %q mode platform does not support user credential fields", integration, connection)
	}
	switch conn.Auth.Type {
	case providermanifestv1.AuthTypeBearer:
		if strings.TrimSpace(conn.Auth.Token) == "" {
			return invocation.ConnectionRuntimeInfo{}, fmt.Errorf("integration %q connection %q mode platform requires auth.token in deployment config", integration, connection)
		}
		if conn.Auth.AuthMapping != nil {
			return invocation.ConnectionRuntimeInfo{}, fmt.Errorf("integration %q connection %q mode platform bearer auth does not support authMapping", integration, connection)
		}
		info.Token = strings.TrimSpace(conn.Auth.Token)
		return info, nil
	case providermanifestv1.AuthTypeManual:
		token := strings.TrimSpace(conn.Auth.Token)
		if token == "" {
			if authMappingNeedsToken(conn.Auth.AuthMapping) {
				return invocation.ConnectionRuntimeInfo{}, fmt.Errorf("integration %q connection %q mode platform manual auth with credential refs requires auth.token in deployment config", integration, connection)
			}
			token = "{}"
		}
		info.Token = token
		return info, nil
	default:
		return invocation.ConnectionRuntimeInfo{}, fmt.Errorf("integration %q connection %q mode platform requires auth.type bearer or manual", integration, connection)
	}
}

func authMappingNeedsToken(mapping *config.AuthMappingDef) bool {
	if mapping == nil {
		return true
	}
	hasMaterialization := len(mapping.Headers) > 0 || mapping.Basic != nil
	if !hasMaterialization {
		return true
	}
	for _, value := range mapping.Headers {
		if authValueNeedsToken(value) {
			return true
		}
	}
	if mapping.Basic != nil {
		if authValueNeedsToken(mapping.Basic.Username) || authValueNeedsToken(mapping.Basic.Password) {
			return true
		}
	}
	return false
}

func authValueNeedsToken(value config.AuthValueDef) bool {
	return value.ValueFrom != nil
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
		return authFallback.definitionFor(connectionName)
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
