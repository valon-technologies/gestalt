package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/plugins/declarative"
	"golang.org/x/sync/singleflight"
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

	for name, entry := range cfg.Plugins {
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

func connectionRuntimeInfo(integration, connection string, conn *config.ConnectionDef) (invocation.ConnectionRuntimeInfo, error) {
	return StaticConnectionRuntimeInfo(integration, connection, *conn)
}

// StaticConnectionRuntimeInfo validates and materializes deployment-owned
// connection material using the same rules as invocation bootstrap.
func StaticConnectionRuntimeInfo(integration, connection string, conn config.ConnectionDef) (invocation.ConnectionRuntimeInfo, error) {
	mode := config.ConnectionModeForConnection(conn)
	info := invocation.ConnectionRuntimeInfo{
		ConnectionID: conn.ConnectionID,
		Mode:         mode,
		Exposure:     config.ConnectionExposureForConnection(conn),
		AuthType:     conn.Auth.Type,
		AuthMapping:  config.CloneAuthMapping(conn.Auth.AuthMapping),
		Params:       connectionParamDefaults(conn.ConnectionParams),
	}
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
	case providermanifestv1.AuthTypeOAuth2:
		if strings.TrimSpace(conn.Auth.GrantType) != "client_credentials" {
			return invocation.ConnectionRuntimeInfo{}, fmt.Errorf("integration %q connection %q mode platform oauth2 requires auth.grantType client_credentials", integration, connection)
		}
		source, err := newClientCredentialsTokenSource(conn.Auth)
		if err != nil {
			return invocation.ConnectionRuntimeInfo{}, fmt.Errorf("integration %q connection %q oauth2 client_credentials: %w", integration, connection, err)
		}
		info.TokenSource = source
		return info, nil
	default:
		return invocation.ConnectionRuntimeInfo{}, fmt.Errorf("integration %q connection %q mode platform requires auth.type bearer, manual, or oauth2 client_credentials", integration, connection)
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

type clientCredentialsTokenSource struct {
	auth         config.ConnectionAuthDef
	now          func() time.Time
	fetchTimeout time.Duration

	mu     sync.Mutex
	cached invocation.ConnectionRuntimeCredential
	group  singleflight.Group
}

const clientCredentialsTokenFetchTimeout = 30 * time.Second

func newClientCredentialsTokenSource(auth config.ConnectionAuthDef) (*clientCredentialsTokenSource, error) {
	if strings.TrimSpace(auth.TokenURL) == "" {
		return nil, fmt.Errorf("auth.tokenUrl is required")
	}
	if strings.TrimSpace(auth.ClientID) == "" {
		return nil, fmt.Errorf("auth.clientId is required")
	}
	if strings.TrimSpace(auth.ClientSecret) == "" {
		return nil, fmt.Errorf("auth.clientSecret is required")
	}
	return &clientCredentialsTokenSource{auth: auth, now: time.Now, fetchTimeout: clientCredentialsTokenFetchTimeout}, nil
}

func (s *clientCredentialsTokenSource) ResolveConnectionCredential(ctx context.Context) (invocation.ConnectionRuntimeCredential, error) {
	if s == nil {
		return invocation.ConnectionRuntimeCredential{}, fmt.Errorf("token source is not configured")
	}
	s.mu.Lock()
	if s.cached.Token != "" && s.cached.ExpiresAt != nil && s.now().Add(60*time.Second).Before(*s.cached.ExpiresAt) {
		cached := s.cached
		s.mu.Unlock()
		return cached, nil
	}
	s.mu.Unlock()

	resultCh := s.group.DoChan("token", func() (any, error) {
		s.mu.Lock()
		if s.cached.Token != "" && s.cached.ExpiresAt != nil && s.now().Add(60*time.Second).Before(*s.cached.ExpiresAt) {
			cached := s.cached
			s.mu.Unlock()
			return cached, nil
		}
		s.mu.Unlock()
		timeout := s.fetchTimeout
		if timeout <= 0 {
			timeout = clientCredentialsTokenFetchTimeout
		}
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
		defer cancel()
		credential, err := s.fetch(fetchCtx)
		if err != nil {
			return invocation.ConnectionRuntimeCredential{}, err
		}
		s.mu.Lock()
		s.cached = credential
		s.mu.Unlock()
		return credential, nil
	})
	select {
	case result := <-resultCh:
		if result.Err != nil {
			return invocation.ConnectionRuntimeCredential{}, result.Err
		}
		return result.Val.(invocation.ConnectionRuntimeCredential), nil
	case <-ctx.Done():
		return invocation.ConnectionRuntimeCredential{}, ctx.Err()
	}
}

func (s *clientCredentialsTokenSource) fetch(ctx context.Context) (invocation.ConnectionRuntimeCredential, error) {
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	clientID := strings.TrimSpace(s.auth.ClientID)
	clientSecret := strings.TrimSpace(s.auth.ClientSecret)
	clientAuth := strings.TrimSpace(s.auth.ClientAuth)
	if clientAuth != "header" {
		form.Set("client_id", clientID)
		form.Set("client_secret", clientSecret)
	}
	if len(s.auth.Scopes) > 0 {
		sep := strings.TrimSpace(s.auth.ScopeSeparator)
		if sep == "" {
			sep = " "
		}
		scopeParam := strings.TrimSpace(s.auth.ScopeParam)
		if scopeParam == "" {
			scopeParam = "scope"
		}
		form.Set(scopeParam, strings.Join(s.auth.Scopes, sep))
	}
	for k, v := range s.auth.TokenParams {
		if strings.TrimSpace(k) != "" {
			form.Set(k, v)
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSpace(s.auth.TokenURL), strings.NewReader(form.Encode()))
	if err != nil {
		return invocation.ConnectionRuntimeCredential{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if s.auth.AcceptHeader != "" {
		req.Header.Set("Accept", s.auth.AcceptHeader)
	}
	if clientAuth == "header" {
		req.SetBasicAuth(clientID, clientSecret)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return invocation.ConnectionRuntimeCredential{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return invocation.ConnectionRuntimeCredential{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return invocation.ConnectionRuntimeCredential{}, fmt.Errorf("token endpoint returned %s", resp.Status)
	}
	var decoded struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return invocation.ConnectionRuntimeCredential{}, fmt.Errorf("decode token response: %w", err)
	}
	token := strings.TrimSpace(decoded.AccessToken)
	if token == "" {
		return invocation.ConnectionRuntimeCredential{}, fmt.Errorf("token response missing access_token")
	}
	var expiresAt *time.Time
	if decoded.ExpiresIn > 0 {
		t := s.now().Add(time.Duration(decoded.ExpiresIn) * time.Second)
		expiresAt = &t
	}
	return invocation.ConnectionRuntimeCredential{Token: token, ExpiresAt: expiresAt}, nil
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

	specAuthForConnection := func(connectionName string) *declarative.Definition {
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

func buildConnectionHandler(conn config.ConnectionDef, mcpURL string, pluginConfig map[string]any, specDef *declarative.Definition, deps Deps) (OAuthHandler, error) {
	switch conn.Auth.Type {
	case "", providermanifestv1.AuthTypeOAuth2:
		if strings.TrimSpace(conn.Auth.GrantType) == "client_credentials" {
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
