package bootstrap

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/declarative"
	"github.com/valon-technologies/gestalt/server/services/apps/oauth"
	"github.com/valon-technologies/gestalt/server/services/egress"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

type ConnectionMaps struct {
	DefaultConnection map[string]string
	APIConnection     map[string]string
	MCPConnection     map[string]string
}

func agentConnectionBindings(cfg *config.Config) map[string][]string {
	if cfg == nil || len(cfg.Providers.Agent) == 0 {
		return nil
	}
	out := make(map[string][]string, len(cfg.Providers.Agent))
	for providerName, entry := range cfg.Providers.Agent {
		if entry == nil || len(entry.Connections) == 0 {
			continue
		}
		names := make([]string, 0, len(entry.Connections))
		for name := range entry.Connections {
			name = config.ResolveConnectionAlias(name)
			if name != "" {
				names = append(names, name)
			}
		}
		if len(names) > 0 {
			out[providerName] = names
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func BuildConnectionMaps(cfg *config.Config) (ConnectionMaps, error) {
	maps := ConnectionMaps{
		DefaultConnection: make(map[string]string, len(cfg.Apps)),
		APIConnection:     make(map[string]string, len(cfg.Apps)),
		MCPConnection:     make(map[string]string, len(cfg.Apps)),
	}

	for name, entry := range cfg.Apps {
		defaultConnection := config.AppConnectionName
		apiConnection := config.AppConnectionName
		mcpConnection := config.AppConnectionName

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
	egressDeps := newEgressDeps(cfg)

	addProviderRuntime := func(kind, name string, entry *config.ProviderEntry) error {
		if entry == nil {
			return nil
		}
		name = strings.TrimSpace(name)
		if name == "" {
			return fmt.Errorf("%s connection runtime name is empty", kind)
		}
		if _, exists := runtime[name]; exists {
			return fmt.Errorf("%s %q conflicts with another provider connection namespace", kind, name)
		}
		plan, err := config.BuildStaticConnectionPlan(entry, entry.ManifestSpec())
		if err != nil {
			return fmt.Errorf("%s %q: %w", kind, name, err)
		}
		providerConfig, err := config.NodeToMap(entry.Config)
		if err != nil {
			return fmt.Errorf("%s %q config: %w", kind, name, err)
		}
		policy := egressDeps.ProviderPolicy(entry)
		addRuntimeInfo := func(connName string, conn *config.ConnectionDef) error {
			info, err := connectionRuntimeInfo(name, connName, conn, policy, providerConfig)
			if err != nil {
				return err
			}
			if _, ok := runtime[name]; !ok {
				runtime[name] = map[string]invocation.ConnectionRuntimeInfo{}
			}
			runtime[name][connName] = info
			return nil
		}

		pluginConn := plan.AppConnection()
		if err := addRuntimeInfo(config.AppConnectionName, &pluginConn); err != nil {
			return err
		}
		for _, connName := range plan.NamedConnectionNames() {
			conn, _ := plan.NamedConnectionDef(connName)
			if err := addRuntimeInfo(connName, &conn); err != nil {
				return err
			}
		}
		return nil
	}

	for name, entry := range cfg.Apps {
		if err := addProviderRuntime("integration", name, entry); err != nil {
			return nil, err
		}
	}
	for name, entry := range cfg.Providers.Agent {
		if err := addProviderRuntime("agent provider", name, entry); err != nil {
			return nil, err
		}
	}
	return runtime, nil
}

func ValidateConnectionRuntimeCredentials(ctx context.Context, provider core.ExternalCredentialProvider, runtime invocation.ConnectionRuntimeMap) error {
	if len(runtime) == 0 || core.ExternalCredentialProviderMissing(provider) {
		return nil
	}
	for providerName, connections := range runtime {
		for connectionName := range connections {
			info := connections[connectionName]
			if err := provider.ValidateCredentialConfig(ctx, &core.ValidateExternalCredentialConfigRequest{
				Provider:         providerName,
				Connection:       connectionName,
				ConnectionID:     info.ConnectionID,
				Mode:             info.Mode,
				Auth:             info.AuthConfig,
				ConnectionParams: info.Params,
			}); err != nil {
				return fmt.Errorf("validate credential config for %s/%s: %w", providerName, connectionName, err)
			}
		}
	}
	return nil
}

func connectionRuntimeInfo(integration, connection string, conn *config.ConnectionDef, policy egress.Policy, providerConfig map[string]any) (invocation.ConnectionRuntimeInfo, error) {
	return staticConnectionRuntimeInfo(integration, connection, *conn, policy, providerConfig)
}

// StaticConnectionRuntimeInfo validates and materializes deployment-owned
// connection material using the same rules as invocation bootstrap.
func StaticConnectionRuntimeInfo(integration, connection string, conn config.ConnectionDef) (invocation.ConnectionRuntimeInfo, error) {
	return staticConnectionRuntimeInfo(integration, connection, conn, egress.Policy{DefaultAction: egress.PolicyAllow}, nil)
}

func staticConnectionRuntimeInfo(_, _ string, conn config.ConnectionDef, _ egress.Policy, providerConfig map[string]any) (invocation.ConnectionRuntimeInfo, error) {
	mode := config.ConnectionModeForConnection(conn)
	authConfig := applyConnectionRuntimeAuthOverlay(ExternalCredentialAuthConfig(conn.Auth), providerConfig)
	info := invocation.ConnectionRuntimeInfo{
		ConnectionID:      conn.ConnectionID,
		Mode:              mode,
		AuthType:          conn.Auth.Type,
		AuthConfig:        authConfig,
		AuthMapping:       config.CloneAuthMapping(conn.Auth.AuthMapping),
		Params:            connectionParamDefaults(conn.ConnectionParams),
		CredentialRefresh: cloneCredentialRefreshConfig(conn.CredentialRefresh),
	}
	return info, nil
}

func applyConnectionRuntimeAuthOverlay(auth core.ExternalCredentialAuthConfig, providerConfig map[string]any) core.ExternalCredentialAuthConfig {
	if auth.Type != string(providermanifestv1.AuthTypeOAuth2) || providerConfig == nil {
		return auth
	}
	if id, _ := providerConfig["clientId"].(string); id != "" {
		auth.ClientID = id
	}
	if sec, _ := providerConfig["clientSecret"].(string); sec != "" {
		auth.ClientSecret = sec
	}
	return auth
}

func cloneCredentialRefreshConfig(src *providermanifestv1.CredentialRefreshConfig) *providermanifestv1.CredentialRefreshConfig {
	if src == nil {
		return nil
	}
	dst := *src
	return &dst
}

func ExternalCredentialAuthConfig(auth config.ConnectionAuthDef) core.ExternalCredentialAuthConfig {
	drivers := make([]core.ExternalCredentialTokenExchangeDriver, 0, len(auth.TokenExchangeDrivers))
	for _, driver := range auth.TokenExchangeDrivers {
		drivers = append(drivers, core.ExternalCredentialTokenExchangeDriver{
			Type:            strings.TrimSpace(driver.Type),
			TargetPrincipal: strings.TrimSpace(driver.TargetPrincipal),
			Scopes:          slices.Clone(driver.Scopes),
			LifetimeSeconds: driver.LifetimeSeconds,
			Endpoint:        strings.TrimSpace(driver.Endpoint),
			Params:          maps.Clone(driver.Params),
		})
	}
	return core.ExternalCredentialAuthConfig{
		Type:                 string(auth.Type),
		Token:                auth.Token,
		TokenPrefix:          auth.TokenPrefix,
		GrantType:            auth.GrantType,
		RefreshToken:         auth.RefreshToken,
		TokenURL:             auth.TokenURL,
		ClientID:             auth.ClientID,
		ClientSecret:         auth.ClientSecret,
		ClientAuth:           auth.ClientAuth,
		TokenExchange:        auth.TokenExchange,
		Scopes:               slices.Clone(auth.Scopes),
		ScopeParam:           auth.ScopeParam,
		ScopeSeparator:       auth.ScopeSeparator,
		TokenParams:          maps.Clone(auth.TokenParams),
		RefreshParams:        maps.Clone(auth.RefreshParams),
		AcceptHeader:         auth.AcceptHeader,
		AccessTokenPath:      auth.AccessTokenPath,
		TokenExchangeDrivers: drivers,
	}
}

func connectionParamDefaults(params map[string]config.ConnectionParamDef) map[string]string {
	if len(params) == 0 {
		return nil
	}
	out := make(map[string]string, len(params))
	for name, param := range params {
		if strings.TrimSpace(param.Default) != "" {
			out[name] = param.Default
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func buildConnectionAuthMap(name string, entry *config.ProviderEntry, manifest *providermanifestv1.Manifest, pluginConfig map[string]any, authFallback *specAuthFallback, deps Deps) (map[string]OAuthHandler, error) {
	manifestApp := (*providermanifestv1.Spec)(nil)
	if manifest != nil {
		manifestApp = manifest.Spec
	}
	plan, err := config.BuildStaticConnectionPlan(entry, manifestApp)
	if err != nil {
		return nil, fmt.Errorf("resolve connections for %q: %w", name, err)
	}

	mcpURL := ""
	if resolved, ok := plan.ResolvedSurface(config.SpecSurfaceMCP); ok {
		mcpURL = resolved.URL
	}

	specAuthForConnection := func(connectionName string) *declarative.Definition {
		return authFallback.definitionFor(connectionName)
	}

	handlers := make(map[string]OAuthHandler)
	if handler, err := buildConnectionHandler(plan.AppConnection(), mcpURL, pluginConfig, specAuthForConnection(config.AppConnectionName), deps); err != nil {
		return nil, fmt.Errorf("build app connection auth for %q: %w", name, err)
	} else if handler != nil {
		handlers[config.AppConnectionName] = handler
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

func buildConnectionHandler(conn config.ConnectionDef, mcpURL string, pluginConfig map[string]any, specDef *declarative.Definition, deps Deps) (OAuthHandler, error) {
	switch conn.Auth.Type {
	case "", providermanifestv1.AuthTypeOAuth2:
		switch strings.TrimSpace(conn.Auth.GrantType) {
		case "client_credentials", "refresh_token":
			return nil, nil
		}
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

func buildManualConnectionAuthMap(name string, entry *config.ProviderEntry, manifest *providermanifestv1.Manifest, authFallback *specAuthFallback) (map[string]ManualTokenExchanger, error) {
	manifestApp := (*providermanifestv1.Spec)(nil)
	if manifest != nil {
		manifestApp = manifest.Spec
	}
	plan, err := config.BuildStaticConnectionPlan(entry, manifestApp)
	if err != nil {
		return nil, fmt.Errorf("resolve manual token connections for %q: %w", name, err)
	}

	specAuthForConnection := func(connectionName string) *declarative.Definition {
		return authFallback.definitionFor(connectionName)
	}

	handlers := make(map[string]ManualTokenExchanger)
	if handler, err := buildManualConnectionHandler(plan.AppConnection(), specAuthForConnection(config.AppConnectionName)); err != nil {
		return nil, fmt.Errorf("build app manual token auth for %q: %w", name, err)
	} else if handler != nil {
		handlers[config.AppConnectionName] = handler
	}

	for _, resolvedName := range plan.NamedConnectionNames() {
		conn, _ := plan.NamedConnectionDef(resolvedName)
		handler, err := buildManualConnectionHandler(conn, specAuthForConnection(resolvedName))
		if err != nil {
			return nil, fmt.Errorf("build named manual token auth for %q/%q: %w", name, resolvedName, err)
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

func buildManualConnectionHandler(conn config.ConnectionDef, specDef *declarative.Definition) (ManualTokenExchanger, error) {
	auth := conn.Auth
	if auth.Type == "" && specDef != nil && specDef.Auth.Type != "" {
		auth = config.ConnectionAuthDef{
			Type:            providermanifestv1.AuthType(specDef.Auth.Type),
			TokenURL:        specDef.Auth.TokenURL,
			TokenExchange:   specDef.Auth.TokenExchange,
			TokenParams:     specDef.Auth.TokenParams,
			AcceptHeader:    specDef.Auth.AcceptHeader,
			AccessTokenPath: specDef.Auth.AccessTokenPath,
			Credentials:     append([]config.CredentialFieldDef(nil), specDef.CredentialFields...),
		}
	}
	if auth.Type != providermanifestv1.AuthTypeManual || strings.TrimSpace(auth.TokenURL) == "" {
		return nil, nil
	}
	return buildManualTokenExchangerFromAuth(auth)
}

func buildManualTokenExchangerFromAuth(auth config.ConnectionAuthDef) (ManualTokenExchanger, error) {
	tokenExchange, err := oauth.ParseTokenExchangeFormat(auth.TokenExchange)
	if err != nil {
		return nil, err
	}
	return oauth.NewCredentialExchanger(oauth.CredentialExchangeConfig{
		TokenURL:        auth.TokenURL,
		TokenParams:     auth.TokenParams,
		TokenExchange:   tokenExchange,
		AcceptHeader:    auth.AcceptHeader,
		AccessTokenPath: auth.AccessTokenPath,
	}), nil
}
