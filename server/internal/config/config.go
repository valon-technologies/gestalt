package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/egress"
	"github.com/valon-technologies/gestalt/server/internal/pluginsource"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
	"gopkg.in/yaml.v3"
)

// Callback paths must match the routes registered in server.go.
const (
	AuthCallbackPath        = "/api/v1/auth/login/callback"
	IntegrationCallbackPath = "/api/v1/auth/callback"
)

// PluginConnectionName is the implicit connection name used when storing
// tokens for plugin-only integrations that do not declare YAML connections.
const PluginConnectionName = "_plugin"

// PluginConnectionAlias is the user-facing alias that maps to
// PluginConnectionName. In hybrid integrations, mcp.connection can be set
// to "plugin" to reuse the plugin's OAuth token.
const PluginConnectionAlias = "plugin"

type Config struct {
	Auth         AuthConfig                `yaml:"auth"`
	Datastore    DatastoreConfig           `yaml:"datastore"`
	Secrets      SecretsConfig             `yaml:"secrets"`
	Telemetry    TelemetryConfig           `yaml:"telemetry"`
	Integrations map[string]IntegrationDef `yaml:"providers"`
	Bindings     map[string]BindingDef     `yaml:"bindings"`
	Server       ServerConfig              `yaml:"server"`
	Egress       EgressConfig              `yaml:"egress"`
	UI           UIConfig                  `yaml:"ui"`
}

type TelemetryConfig struct {
	Provider string    `yaml:"provider"`
	Config   yaml.Node `yaml:"config"`
}

type UIConfig struct {
	Plugin *UIPluginDef `yaml:"plugin"`
}

type UIPluginDef struct {
	Package string `yaml:"package"`
	Source  string `yaml:"source"`
	Version string `yaml:"version"`

	ResolvedAssetRoot string `yaml:"-"`
}

func (p *UIPluginDef) HasManagedArtifacts() bool {
	return p != nil && (p.Package != "" || p.Source != "")
}

type EgressConfig struct {
	DefaultAction string                  `yaml:"default_action"`
	Policies      []EgressPolicyRule      `yaml:"policies"`
	Credentials   []EgressCredentialGrant `yaml:"credentials"`
}

type EgressPolicyRule struct {
	Action      string `yaml:"action"`
	SubjectKind string `yaml:"subject_kind"`
	SubjectID   string `yaml:"subject_id"`
	Provider    string `yaml:"provider"`
	Operation   string `yaml:"operation"`
	Method      string `yaml:"method"`
	Host        string `yaml:"host"`
	PathPrefix  string `yaml:"path_prefix"`
}

type EgressCredentialGrant struct {
	SecretRef   string `yaml:"secret_ref"`
	AuthStyle   string `yaml:"auth_style"`
	SubjectKind string `yaml:"subject_kind"`
	SubjectID   string `yaml:"subject_id"`
	Operation   string `yaml:"operation"`
	Method      string `yaml:"method"`
	Host        string `yaml:"host"`
	PathPrefix  string `yaml:"path_prefix"`
}

type PluginDef struct {
	Command string            `yaml:"command"`
	Package string            `yaml:"package"`
	Source  string            `yaml:"source"`
	Version string            `yaml:"version"`
	Args    []string          `yaml:"args"`
	Env     map[string]string `yaml:"env"`

	Config       yaml.Node `yaml:"config"`
	AllowedHosts []string  `yaml:"allowed_hosts"`

	OpenAPI              string                                         `yaml:"openapi"`
	GraphQLURL           string                                         `yaml:"graphql_url"`
	MCPURL               string                                         `yaml:"mcp_url"`
	BaseURL              string                                         `yaml:"base_url"`
	Headers              map[string]string                              `yaml:"headers"`
	ManagedParameters    []ManagedParameterDef                          `yaml:"managed_parameters"`
	PostConnectDiscovery *pluginmanifestv1.ProviderPostConnectDiscovery `yaml:"-"`

	Auth            *ConnectionAuthDef        `yaml:"auth"`
	ConnectionMode  string                    `yaml:"-"`
	Connections     map[string]*ConnectionDef `yaml:"connections"`
	Operations      []InlineOperationDef      `yaml:"operations"`
	ResponseMapping *ResponseMappingDef       `yaml:"response_mapping"`

	OpenAPIConnection string `yaml:"openapi_connection"`
	GraphQLConnection string `yaml:"graphql_connection"`
	MCPConnection     string `yaml:"mcp_connection"`
	DefaultConnection string `yaml:"default_connection"`

	ConnectionParams  map[string]ConnectionParamDef `yaml:"connection_params"`
	MCP               bool                          `yaml:"mcp"`
	AllowedOperations map[string]*OperationOverride `yaml:"allowed_operations"`

	ResolvedManifestPath string                     `yaml:"-"`
	ResolvedManifest     *pluginmanifestv1.Manifest `yaml:"-"`
	ResolvedIconFile     string                     `yaml:"-"`
	IsDeclarative        bool                       `yaml:"-"`
	HostBinary           string                     `yaml:"-"`
}

func (p *PluginDef) IsInline() bool {
	if p == nil {
		return false
	}
	if p.Source != "" || p.Package != "" || p.Command != "" {
		return false
	}
	return p.OpenAPI != "" || p.GraphQLURL != "" || p.MCPURL != "" || len(p.Operations) > 0 ||
		p.BaseURL != "" || p.Auth != nil || len(p.Connections) > 0
}

func (p *PluginDef) HasManagedArtifacts() bool {
	return p != nil && (p.Package != "" || p.Source != "")
}

func (p *PluginDef) HasResolvedManifest() bool {
	return p != nil && p.ResolvedManifest != nil
}

func (p *PluginDef) ManifestProvider() *pluginmanifestv1.Provider {
	if p == nil || p.ResolvedManifest == nil {
		return nil
	}
	return p.ResolvedManifest.Provider
}

func (p *PluginDef) DeclaresMCP() bool {
	if p == nil {
		return false
	}
	if p.MCP || p.MCPURL != "" || p.OpenAPI != "" || p.GraphQLURL != "" || len(p.Operations) > 0 {
		return true
	}
	if !p.HasResolvedManifest() {
		return true
	}
	provider := p.ManifestProvider()
	if provider == nil {
		return false
	}
	return provider.MCP || provider.IsSpecLoaded() || len(provider.Operations) > 0
}

type InlineOperationDef struct {
	Name        string                 `yaml:"name"`
	Description string                 `yaml:"description"`
	Method      string                 `yaml:"method"`
	Path        string                 `yaml:"path"`
	Parameters  []InlineOperationParam `yaml:"parameters"`
}

type InlineOperationParam struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	In          string `yaml:"in"`
	Description string `yaml:"description"`
	Required    bool   `yaml:"required"`
}

type ResponseMappingDef struct {
	DataPath   string             `yaml:"data_path"`
	Pagination *PaginationMapping `yaml:"pagination"`
}

type ManagedParameterDef = pluginmanifestv1.ManagedParameter

type PaginationMapping struct {
	HasMorePath string `yaml:"has_more_path"`
	CursorPath  string `yaml:"cursor_path"`
}

type BindingDef struct {
	Type      string    `yaml:"type"`
	Providers []string  `yaml:"providers"`
	Config    yaml.Node `yaml:"config"`
}

type SecretsConfig struct {
	Provider string    `yaml:"provider"`
	Config   yaml.Node `yaml:"config"`
}

type AuthConfig struct {
	Provider string    `yaml:"provider"`
	Config   yaml.Node `yaml:"config"`
}

type DatastoreConfig struct {
	Provider string    `yaml:"provider"`
	Config   yaml.Node `yaml:"config"`
}

type ServerConfig struct {
	Port          int    `yaml:"port"`
	BaseURL       string `yaml:"base_url"`
	EncryptionKey string `yaml:"encryption_key"`
	APITokenTTL   string `yaml:"api_token_ttl"`
}

type IntegrationDef struct {
	Plugin        *PluginDef `yaml:"plugin"`
	DisplayName   string     `yaml:"display_name"`
	Description   string     `yaml:"description"`
	MCPToolPrefix string     `yaml:"-"`
	IconFile      string     `yaml:"icon_file"`
}

// ConnectionDef owns authentication and connection parameters for a named
// connection. All connections in a single integration must share the same Mode.
type ConnectionDef struct {
	Mode             string                        `yaml:"mode"`
	Auth             ConnectionAuthDef             `yaml:"auth"`
	ConnectionParams map[string]ConnectionParamDef `yaml:"params"`
}

type ConnectionAuthDef struct {
	Type                string               `yaml:"type"`
	AuthorizationURL    string               `yaml:"authorization_url"`
	TokenURL            string               `yaml:"token_url"`
	ClientID            string               `yaml:"client_id"`
	ClientSecret        string               `yaml:"client_secret"`
	RedirectURL         string               `yaml:"redirect_url"`
	ClientAuth          string               `yaml:"client_auth"`
	TokenExchange       string               `yaml:"token_exchange"`
	Scopes              []string             `yaml:"scopes"`
	ScopeParam          string               `yaml:"scope_param"`
	ScopeSeparator      string               `yaml:"scope_separator"`
	PKCE                bool                 `yaml:"pkce"`
	AuthorizationParams map[string]string    `yaml:"authorization_params"`
	TokenParams         map[string]string    `yaml:"token_params"`
	RefreshParams       map[string]string    `yaml:"refresh_params"`
	AcceptHeader        string               `yaml:"accept_header"`
	AccessTokenPath     string               `yaml:"access_token_path"`
	TokenMetadata       []string             `yaml:"token_metadata"`
	Credentials         []CredentialFieldDef `yaml:"credentials"`
	AuthMapping         *AuthMappingDef      `yaml:"auth_mapping"`
}

type CredentialFieldDef struct {
	Name        string `yaml:"name"`
	Label       string `yaml:"label"`
	Description string `yaml:"description"`
	HelpURL     string `yaml:"help_url"`
}

type AuthMappingDef struct {
	Headers map[string]string `yaml:"headers"`
}

type ConnectionParamDef struct {
	Required    bool   `yaml:"required"`
	Description string `yaml:"description"`
	From        string `yaml:"from"`
}

// ResolveConnectionAlias maps the user-facing "plugin" alias to the
// internal PluginConnectionName. All other names pass through unchanged.
func ResolveConnectionAlias(name string) string {
	if name == PluginConnectionAlias {
		return PluginConnectionName
	}
	return name
}

func MergeConnectionAuth(dst *ConnectionAuthDef, src ConnectionAuthDef) {
	if dst == nil {
		return
	}
	if src.Type != "" && dst.Type != "" && src.Type != dst.Type {
		*dst = ConnectionAuthDef{}
	}
	setString := func(dst *string, src string) {
		if src != "" {
			*dst = src
		}
	}
	setString(&dst.Type, src.Type)
	setString(&dst.AuthorizationURL, src.AuthorizationURL)
	setString(&dst.TokenURL, src.TokenURL)
	setString(&dst.ClientID, src.ClientID)
	setString(&dst.ClientSecret, src.ClientSecret)
	setString(&dst.RedirectURL, src.RedirectURL)
	setString(&dst.ClientAuth, src.ClientAuth)
	setString(&dst.TokenExchange, src.TokenExchange)
	if src.Scopes != nil {
		dst.Scopes = src.Scopes
	}
	setString(&dst.ScopeParam, src.ScopeParam)
	setString(&dst.ScopeSeparator, src.ScopeSeparator)
	if src.PKCE {
		dst.PKCE = true
	}
	if src.AuthorizationParams != nil {
		dst.AuthorizationParams = src.AuthorizationParams
	}
	if src.TokenParams != nil {
		dst.TokenParams = src.TokenParams
	}
	if src.RefreshParams != nil {
		dst.RefreshParams = src.RefreshParams
	}
	setString(&dst.AcceptHeader, src.AcceptHeader)
	setString(&dst.AccessTokenPath, src.AccessTokenPath)
	if src.TokenMetadata != nil {
		dst.TokenMetadata = src.TokenMetadata
	}
	if len(src.Credentials) > 0 {
		dst.Credentials = mergeCredentialFields(dst.Credentials, src.Credentials)
	}
	if src.AuthMapping != nil {
		dst.AuthMapping = src.AuthMapping
	}
}

func mergeCredentialFields(base, override []CredentialFieldDef) []CredentialFieldDef {
	if len(base) == 0 {
		return append([]CredentialFieldDef(nil), override...)
	}

	merged := append([]CredentialFieldDef(nil), base...)
	indexByName := make(map[string]int, len(merged))
	for i, field := range merged {
		if field.Name != "" {
			indexByName[field.Name] = i
		}
	}

	for _, field := range override {
		if idx, ok := indexByName[field.Name]; ok {
			mergeCredentialField(&merged[idx], field)
			continue
		}
		merged = append(merged, field)
		if field.Name != "" {
			indexByName[field.Name] = len(merged) - 1
		}
	}

	return merged
}

func mergeCredentialField(dst *CredentialFieldDef, src CredentialFieldDef) {
	if dst == nil {
		return
	}
	if dst.Name == "" {
		dst.Name = src.Name
	}
	if src.Label != "" {
		dst.Label = src.Label
	}
	if src.Description != "" {
		dst.Description = src.Description
	}
	if src.HelpURL != "" {
		dst.HelpURL = src.HelpURL
	}
}

func MergeConnectionDef(dst *ConnectionDef, src *ConnectionDef) {
	if dst == nil || src == nil {
		return
	}
	if src.Mode != "" {
		dst.Mode = src.Mode
	}
	MergeConnectionAuth(&dst.Auth, src.Auth)
	if len(src.ConnectionParams) > 0 {
		dst.ConnectionParams = src.ConnectionParams
	}
}

func ManifestAuthToConnectionAuthDef(auth *pluginmanifestv1.ProviderAuth) ConnectionAuthDef {
	if auth == nil {
		return ConnectionAuthDef{}
	}
	out := ConnectionAuthDef{
		Type:                auth.Type,
		AuthorizationURL:    auth.AuthorizationURL,
		TokenURL:            auth.TokenURL,
		ClientID:            auth.ClientID,
		ClientSecret:        auth.ClientSecret,
		ClientAuth:          auth.ClientAuth,
		TokenExchange:       auth.TokenExchange,
		Scopes:              auth.Scopes,
		ScopeParam:          auth.ScopeParam,
		ScopeSeparator:      auth.ScopeSeparator,
		PKCE:                auth.PKCE,
		AuthorizationParams: auth.AuthorizationParams,
		TokenParams:         auth.TokenParams,
		RefreshParams:       auth.RefreshParams,
		AcceptHeader:        auth.AcceptHeader,
		AccessTokenPath:     auth.AccessTokenPath,
		TokenMetadata:       auth.TokenMetadata,
	}
	if len(auth.Credentials) > 0 {
		out.Credentials = make([]CredentialFieldDef, len(auth.Credentials))
		for i, field := range auth.Credentials {
			out.Credentials[i] = CredentialFieldDef{
				Name:        field.Name,
				Label:       field.Label,
				Description: field.Description,
				HelpURL:     field.HelpURL,
			}
		}
	}
	return out
}

func EffectivePluginConnectionDef(plugin *PluginDef, manifestProvider *pluginmanifestv1.Provider) ConnectionDef {
	conn := ConnectionDef{}
	if manifestProvider != nil {
		conn.Mode = manifestProvider.ConnectionMode
		if manifestProvider.Auth != nil {
			MergeConnectionAuth(&conn.Auth, ManifestAuthToConnectionAuthDef(manifestProvider.Auth))
		}
	}
	if plugin != nil {
		override := &ConnectionDef{
			Mode:             plugin.ConnectionMode,
			ConnectionParams: plugin.ConnectionParams,
		}
		if plugin.Auth != nil {
			override.Auth = *plugin.Auth
		}
		MergeConnectionDef(&conn, override)
	}
	return conn
}

func EffectiveNamedConnectionDef(plugin *PluginDef, manifestProvider *pluginmanifestv1.Provider, name string) (ConnectionDef, bool) {
	conn := ConnectionDef{}
	found := false

	if manifestProvider != nil && manifestProvider.Connections != nil {
		if def, ok := manifestProvider.Connections[name]; ok && def != nil {
			found = true
			if def.Mode != "" {
				conn.Mode = def.Mode
			}
			if def.Auth != nil {
				MergeConnectionAuth(&conn.Auth, ManifestAuthToConnectionAuthDef(def.Auth))
			}
		}
	}
	if plugin != nil {
		if def, ok := plugin.Connections[name]; ok {
			found = true
			MergeConnectionDef(&conn, def)
		}
	}

	if found {
		return conn, true
	}
	return ConnectionDef{}, false
}

// OperationOverride holds optional alias and description for an allowed operation.
type OperationOverride struct {
	Alias       string `yaml:"alias" json:"alias,omitempty"`
	Description string `yaml:"description" json:"description,omitempty"`
}

func OperationOverridesFromManifest(overrides map[string]*pluginmanifestv1.ManifestOperationOverride) map[string]*OperationOverride {
	if overrides == nil {
		return nil
	}
	result := make(map[string]*OperationOverride, len(overrides))
	for name, override := range overrides {
		if override == nil {
			result[name] = nil
			continue
		}
		result[name] = &OperationOverride{
			Alias:       override.Alias,
			Description: override.Description,
		}
	}
	return result
}

func Load(path string) (*Config, error) {
	return LoadWithLookup(path, os.LookupEnv)
}

func LoadWithMapping(path string, getenv func(string) string) (*Config, error) {
	return LoadWithLookup(path, func(key string) (string, bool) {
		// Preserve the legacy os.Expand-style contract for callers that only
		// provide a string mapping: the mapped value wins even when it is empty.
		return getenv(key), true
	})
}

func LoadWithLookup(path string, lookup func(string) (string, bool)) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	resolved, err := expandEnvVariables(string(data), lookup)
	if err != nil {
		return nil, err
	}

	var cfg Config
	dec := yaml.NewDecoder(strings.NewReader(resolved))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil && err != io.EOF {
		return nil, fmt.Errorf("parsing config YAML: %w", err)
	}

	applyDefaults(&cfg)
	resolveBaseURL(&cfg)
	resolveRelativePaths(path, &cfg)

	if err := ValidateStructure(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func expandEnvVariables(input string, lookup func(string) (string, bool)) (string, error) {
	var expandErr error
	resolved := os.Expand(input, func(key string) string {
		if expandErr != nil {
			return ""
		}
		if val, ok := lookup(key); ok {
			return val
		}
		filePath, ok := lookup(key + "_FILE")
		if !ok || filePath == "" {
			return ""
		}
		data, err := os.ReadFile(filePath)
		if err != nil {
			expandErr = fmt.Errorf("resolving %s_FILE: %w", key, err)
			return ""
		}
		return strings.TrimRight(string(data), "\r\n")
	})
	if expandErr != nil {
		return "", fmt.Errorf("expanding config environment variables: %w", expandErr)
	}
	return resolved, nil
}

func applyDefaults(cfg *Config) {
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.Secrets.Provider == "" {
		cfg.Secrets.Provider = "env"
	}
	if cfg.Telemetry.Provider == "" {
		cfg.Telemetry.Provider = "stdout"
	}
}

func resolveBaseURL(cfg *Config) {
	base := strings.TrimRight(cfg.Server.BaseURL, "/")
	if base == "" {
		return
	}
	cfg.Server.BaseURL = base
}

func resolveRelativePaths(configPath string, cfg *Config) {
	baseDir := filepath.Dir(configPath)
	if absPath, err := filepath.Abs(configPath); err == nil {
		baseDir = filepath.Dir(absPath)
	}

	for name := range cfg.Integrations {
		intg := cfg.Integrations[name]
		if intg.IconFile != "" {
			intg.IconFile = resolveRelativePath(baseDir, intg.IconFile)
		}
		if intg.Plugin != nil {
			intg.Plugin.Command = resolveExecutablePath(baseDir, intg.Plugin.Command)
			intg.Plugin.Package = resolvePackagePath(baseDir, intg.Plugin.Package)
			if intg.Plugin.OpenAPI != "" {
				intg.Plugin.OpenAPI = resolveUpstreamURL(baseDir, intg.Plugin.OpenAPI)
			}
			if intg.Plugin.GraphQLURL != "" {
				intg.Plugin.GraphQLURL = resolveUpstreamURL(baseDir, intg.Plugin.GraphQLURL)
			}
			if intg.Plugin.MCPURL != "" {
				intg.Plugin.MCPURL = resolveUpstreamURL(baseDir, intg.Plugin.MCPURL)
			}
		}
		cfg.Integrations[name] = intg
	}

	if cfg.UI.Plugin != nil {
		cfg.UI.Plugin.Package = resolvePackagePath(baseDir, cfg.UI.Plugin.Package)
	}
}

func resolveRelativePath(baseDir, value string) string {
	if value == "" || filepath.IsAbs(value) {
		return value
	}
	return filepath.Clean(filepath.Join(baseDir, value))
}

func resolveExecutablePath(baseDir, value string) string {
	if value == "" || filepath.IsAbs(value) {
		return value
	}
	if strings.HasPrefix(value, ".") || strings.ContainsRune(value, os.PathSeparator) {
		return filepath.Clean(filepath.Join(baseDir, value))
	}
	return value
}

func resolveUpstreamURL(baseDir, value string) string {
	if value == "" || filepath.IsAbs(value) || strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "http://") {
		return value
	}
	return filepath.Clean(filepath.Join(baseDir, value))
}

func resolvePackagePath(baseDir, value string) string {
	if value == "" || filepath.IsAbs(value) || strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "http://") {
		return value
	}
	return filepath.Clean(filepath.Join(baseDir, value))
}

// ValidateStructure checks config shape: integration references, plugin
// declarations, connection references, URL template params, egress rules.
// Called by Load (and therefore by init, validate, and serve). Does not require
// runtime secrets like encryption_key, auth.provider, or datastore.provider.
func ValidateStructure(cfg *Config) error {
	if cfg.Server.APITokenTTL != "" {
		if _, err := ParseDuration(cfg.Server.APITokenTTL); err != nil {
			return fmt.Errorf("config validation: server.api_token_ttl: %w", err)
		}
	}
	if err := validateEgress(&cfg.Egress); err != nil {
		return err
	}
	if err := validateUIPlugin(cfg.UI.Plugin); err != nil {
		return err
	}
	for name := range cfg.Integrations {
		intg := cfg.Integrations[name]
		if err := validatePluginIntegration(name, intg); err != nil {
			return err
		}
	}
	return nil
}

// ValidateResolvedStructure checks integration fields whose support depends on
// resolved managed plugin manifests. Callers should use this after init has
// applied locked plugin artifacts into the config. It intentionally does not
// rerun the full structural validator on the mutated config.
func ValidateResolvedStructure(cfg *Config) error {
	for name := range cfg.Integrations {
		intg := cfg.Integrations[name]
		if intg.Plugin == nil {
			return fmt.Errorf("config validation: integration %q requires a plugin", name)
		}
		if err := validateSupportedPluginFields(name, intg.Plugin); err != nil {
			return err
		}
	}
	return nil
}

// ValidateRuntime checks runtime-only requirements: encryption key, auth
// provider, and datastore provider. Callers that need a fully operational
// config (serve) should call this after Load. Callers that only need
// structural correctness (init, validate) should not.
func ValidateRuntime(cfg *Config) error {
	if cfg.Auth.Provider == "" {
		return fmt.Errorf("config validation: auth.provider is required")
	}
	if cfg.Datastore.Provider == "" {
		return fmt.Errorf("config validation: datastore.provider is required")
	}
	if cfg.Server.EncryptionKey == "" && cfg.Auth.Provider != "none" {
		return fmt.Errorf("config validation: server.encryption_key is required when auth is enabled (auth.provider is %q)", cfg.Auth.Provider)
	}
	return nil
}

func validatePluginIntegration(name string, intg IntegrationDef) error {
	if intg.Plugin == nil {
		return fmt.Errorf("config validation: integration %q requires a plugin", name)
	}
	p := intg.Plugin
	if p.IsInline() {
		if err := validateInlinePlugin(name, p); err != nil {
			return err
		}
	} else {
		if err := validateExternalPlugin("integration", name, p); err != nil {
			return err
		}
	}
	return validateSupportedPluginFields(name, p)
}

func validateInlinePlugin(name string, p *PluginDef) error {
	if p.OpenAPI == "" && p.GraphQLURL == "" && p.MCPURL == "" && len(p.Operations) == 0 {
		return fmt.Errorf("config validation: inline integration %q requires at least one of openapi, graphql_url, mcp_url, or operations", name)
	}
	if len(p.Operations) > 0 && (p.OpenAPI != "" || p.GraphQLURL != "" || p.MCPURL != "") {
		return fmt.Errorf("config validation: inline integration %q plugin.operations are only valid when no openapi, graphql_url, or mcp_url is configured", name)
	}
	if err := validateManagedParameterConfig("config validation: integration "+strconv.Quote(name), p.Headers, p.ManagedParameters); err != nil {
		return err
	}
	for i, op := range p.Operations {
		if op.Name == "" {
			return fmt.Errorf("config validation: integration %q operations[%d].name is required", name, i)
		}
		if op.Method == "" {
			return fmt.Errorf("config validation: integration %q operations[%d].method is required", name, i)
		}
		if op.Path == "" {
			return fmt.Errorf("config validation: integration %q operations[%d].path is required", name, i)
		}
	}
	if err := validateInlineConnectionReferences(name, p); err != nil {
		return err
	}
	if err := validateInlineConnectionDefaults(name, p); err != nil {
		return err
	}
	return nil
}

type inlineConnectionReference struct {
	field    string
	name     string
	required bool
	context  string
}

func inlineConnectionReferences(p *PluginDef) []inlineConnectionReference {
	if p == nil {
		return nil
	}

	var refs []inlineConnectionReference
	if p.DefaultConnection != "" || len(p.Operations) > 0 {
		refs = append(refs, inlineConnectionReference{
			field:    "plugin.default_connection",
			name:     p.DefaultConnection,
			required: len(p.Operations) > 0,
			context:  "using inline operations with named connections",
		})
	}
	if p.Auth != nil {
		return refs
	}
	for _, surface := range OrderedSpecSurfaces {
		if p.SurfaceURL(surface) == "" {
			continue
		}
		refs = append(refs, inlineConnectionReference{
			field:    "plugin." + surface.ConnectionField(),
			name:     p.SurfaceConnectionName(surface),
			required: true,
			context:  surface.NamedConnectionRequirementContext(),
		})
	}
	return refs
}

func manifestBackedConnectionReferences(plugin *PluginDef, provider *pluginmanifestv1.Provider) []inlineConnectionReference {
	if provider == nil {
		return nil
	}

	var refs []inlineConnectionReference
	defaultConnection := provider.DefaultConnection
	if plugin != nil && plugin.DefaultConnection != "" {
		defaultConnection = plugin.DefaultConnection
	}
	if defaultConnection != "" || len(provider.Operations) > 0 {
		refs = append(refs, inlineConnectionReference{
			field:    "plugin.default_connection",
			name:     defaultConnection,
			required: len(provider.Operations) > 0,
			context:  "using declarative operations with named connections",
		})
	}
	if provider.Auth != nil || (plugin != nil && plugin.Auth != nil) {
		return refs
	}
	for _, surface := range OrderedSpecSurfaces {
		url := ManifestProviderSurfaceURL(provider, surface)
		if plugin != nil {
			if overrideURL := plugin.SurfaceURL(surface); overrideURL != "" {
				url = overrideURL
			}
		}
		if url == "" {
			continue
		}
		connectionName := ManifestProviderSurfaceConnectionName(provider, surface)
		if plugin != nil {
			if overrideName := plugin.SurfaceConnectionName(surface); overrideName != "" {
				connectionName = overrideName
			}
		}
		refs = append(refs, inlineConnectionReference{
			field:    "plugin." + surface.ConnectionField(),
			name:     connectionName,
			required: true,
			context:  surface.NamedConnectionRequirementContext(),
		})
	}
	return refs
}

func validateInlineConnectionReferences(name string, p *PluginDef) error {
	return validateConnectionReferences(name, declaredInlineConnections(p), inlineConnectionReferences(p))
}

func validateInlineConnectionDefaults(name string, p *PluginDef) error {
	declared := declaredInlineConnections(p)
	return validateConnectionDefaults(name, "inline integration", len(declared), inlineConnectionReferences(p))
}

func validateManifestBackedConnectionReferences(name string, plugin *PluginDef, provider *pluginmanifestv1.Provider) error {
	return validateConnectionReferences(name, declaredManifestBackedConnections(plugin, provider), manifestBackedConnectionReferences(plugin, provider))
}

func validateManifestBackedConnectionDefaults(name string, plugin *PluginDef, provider *pluginmanifestv1.Provider) error {
	return validateConnectionDefaults(name, "integration", len(declaredManifestBackedConnections(plugin, provider)), manifestBackedConnectionReferences(plugin, provider))
}

func validateConnectionReferences(name string, declared map[string]struct{}, refs []inlineConnectionReference) error {
	for _, ref := range refs {
		if ref.name == "" {
			continue
		}
		resolved := ResolveConnectionAlias(ref.name)
		if resolved == PluginConnectionName {
			continue
		}
		if _, ok := declared[resolved]; ok {
			continue
		}
		return fmt.Errorf("config validation: integration %q %s references undeclared connection %q", name, ref.field, ref.name)
	}
	return nil
}

func validateConnectionDefaults(name, subject string, declaredCount int, refs []inlineConnectionReference) error {
	if declaredCount == 0 {
		return nil
	}
	for _, ref := range refs {
		if ref.required && ref.name == "" {
			return fmt.Errorf("config validation: %s %q %s is required when %s", subject, name, ref.field, ref.context)
		}
	}
	return nil
}

func declaredInlineConnections(plugin *PluginDef) map[string]struct{} {
	if plugin == nil {
		return nil
	}
	declared := make(map[string]struct{}, len(plugin.Connections))
	for rawName := range plugin.Connections {
		addDeclaredConnection(declared, rawName)
	}
	return declared
}

func declaredManifestBackedConnections(plugin *PluginDef, provider *pluginmanifestv1.Provider) map[string]struct{} {
	size := 0
	if plugin != nil {
		size += len(plugin.Connections)
	}
	if provider != nil {
		size += len(provider.Connections)
	}
	declared := make(map[string]struct{}, size)
	if provider != nil {
		for rawName := range provider.Connections {
			addDeclaredConnection(declared, rawName)
		}
	}
	if plugin != nil {
		for rawName := range plugin.Connections {
			addDeclaredConnection(declared, rawName)
		}
	}
	return declared
}

func addDeclaredConnection(declared map[string]struct{}, rawName string) {
	resolved := ResolveConnectionAlias(rawName)
	if resolved == "" {
		return
	}
	declared[resolved] = struct{}{}
}

func validateExternalPlugin(kind, name string, plugin *PluginDef) error {
	if plugin == nil {
		return nil
	}
	if err := validateManagedParameterConfig("config validation: "+kind+" "+strconv.Quote(name), plugin.Headers, plugin.ManagedParameters); err != nil {
		return err
	}
	sourceCount := 0
	if plugin.Command != "" {
		sourceCount++
	}
	if plugin.Package != "" {
		sourceCount++
	}
	if plugin.Source != "" {
		sourceCount++
	}
	switch {
	case sourceCount == 0:
		return fmt.Errorf("config validation: %s %q plugin.command, plugin.package, or plugin.source is required", kind, name)
	case sourceCount > 1:
		return fmt.Errorf("config validation: %s %q plugin.command, plugin.package, and plugin.source are mutually exclusive", kind, name)
	}

	if plugin.Source != "" {
		if _, err := pluginsource.Parse(plugin.Source); err != nil {
			return fmt.Errorf("config validation: %s %q plugin.source: %w", kind, name, err)
		}
		if plugin.Version == "" {
			return fmt.Errorf("config validation: %s %q plugin.version is required when plugin.source is set", kind, name)
		}
		if err := pluginsource.ValidateVersion(plugin.Version); err != nil {
			return fmt.Errorf("config validation: %s %q plugin.version: %w", kind, name, err)
		}
	}

	if (plugin.Command != "" || plugin.Package != "") && plugin.Version != "" {
		return fmt.Errorf("config validation: %s %q plugin.version is only valid with plugin.source", kind, name)
	}
	if plugin.Command == "" && len(plugin.Args) > 0 {
		return fmt.Errorf("config validation: %s %q plugin.args are only valid with plugin.command", kind, name)
	}
	if strings.HasPrefix(plugin.Package, "http://") {
		return fmt.Errorf("config validation: %s %q plugin.package requires HTTPS; plain HTTP is not supported", kind, name)
	}

	if len(plugin.Operations) > 0 {
		return fmt.Errorf("config validation: %s %q external plugin cannot use inline operations", kind, name)
	}

	if kind != "integration" {
		hasInline := plugin.OpenAPI != "" || plugin.GraphQLURL != "" || plugin.MCPURL != "" ||
			plugin.BaseURL != "" || plugin.Auth != nil || len(plugin.Connections) > 0
		if hasInline {
			return fmt.Errorf("config validation: %s %q plugin cannot use inline fields", kind, name)
		}
	}

	return nil
}

func validateSupportedPluginFields(name string, plugin *PluginDef) error {
	if plugin == nil {
		return nil
	}
	if plugin.HasManagedArtifacts() && !plugin.HasResolvedManifest() {
		// Package/source plugins may gain supported surfaces or declarative
		// behavior from the resolved manifest. Validate those fields once init has
		// prepared the artifact and loaded the manifest.
		return nil
	}

	effectiveProvider := plugin.ManifestProvider()
	hasDirectOpenAPI := plugin.OpenAPI != ""
	hasDirectGraphQL := plugin.GraphQLURL != ""
	hasDirectMCP := plugin.MCPURL != ""
	hasOpenAPI := hasDirectOpenAPI || ManifestProviderSurfaceURL(effectiveProvider, SpecSurfaceOpenAPI) != ""
	hasGraphQL := hasDirectGraphQL || ManifestProviderSurfaceURL(effectiveProvider, SpecSurfaceGraphQL) != ""
	hasMCP := hasDirectMCP || ManifestProviderSurfaceURL(effectiveProvider, SpecSurfaceMCP) != ""
	hasAPISurface := hasOpenAPI || hasGraphQL
	hasSpecSurface := hasAPISurface || hasMCP
	hasExecutableProcess := !plugin.IsInline() && !plugin.IsDeclarative
	hasInlineOperations := plugin.IsInline() && len(plugin.Operations) > 0
	hasDeclarativeRuntime := hasInlineOperations || (!hasExecutableProcess && effectiveProvider != nil && effectiveProvider.IsDeclarative())

	supportsBaseURL := hasDeclarativeRuntime || hasDirectOpenAPI || hasDirectGraphQL || (!plugin.IsInline() && hasAPISurface)
	supportsHeaders := false
	switch {
	case plugin.IsInline():
		supportsHeaders = hasInlineOperations || hasSpecSurface
	case hasExecutableProcess:
		supportsHeaders = hasSpecSurface
	default:
		supportsHeaders = effectiveProvider != nil && effectiveProvider.IsManifestBacked()
	}
	supportsResponseMapping := hasAPISurface

	checks := []struct {
		field     string
		present   bool
		supported bool
		reason    string
	}{
		{
			field:     "plugin.env",
			present:   len(plugin.Env) > 0,
			supported: hasExecutableProcess,
			reason:    "is only valid when the plugin runs as an executable process; remove plugin.env or switch this integration to plugin.command, plugin.package, or plugin.source",
		},
		{
			field:     "plugin.allowed_hosts",
			present:   len(plugin.AllowedHosts) > 0,
			supported: hasExecutableProcess,
			reason:    "is only valid when the plugin runs as an executable process; remove plugin.allowed_hosts or switch this integration to plugin.command, plugin.package, or plugin.source",
		},
		{
			field:     "plugin.openapi_connection",
			present:   plugin.OpenAPIConnection != "",
			supported: hasOpenAPI,
			reason:    "is only valid when openapi is configured; remove plugin.openapi_connection or configure an OpenAPI surface",
		},
		{
			field:     "plugin.graphql_connection",
			present:   plugin.GraphQLConnection != "",
			supported: hasGraphQL,
			reason:    "is only valid when graphql_url is configured; remove plugin.graphql_connection or configure a GraphQL surface",
		},
		{
			field:     "plugin.mcp_connection",
			present:   plugin.MCPConnection != "",
			supported: hasMCP,
			reason:    "is only valid when mcp_url is configured; remove plugin.mcp_connection or configure an MCP surface",
		},
		{
			field:     "plugin.base_url",
			present:   plugin.BaseURL != "",
			supported: supportsBaseURL,
			reason:    "is only valid when the resolved provider actually uses a base URL; remove plugin.base_url or configure inline operations, OpenAPI, or GraphQL",
		},
		{
			field:     "plugin.headers",
			present:   len(plugin.Headers) > 0,
			supported: supportsHeaders,
			reason:    "are only valid when the plugin exposes declarative operations or a spec surface; remove plugin.headers or configure declarative operations, OpenAPI, GraphQL, or MCP",
		},
		{
			field:     "plugin.managed_parameters",
			present:   len(plugin.ManagedParameters) > 0,
			supported: hasAPISurface,
			reason:    "are only valid with openapi/graphql surfaces; remove plugin.managed_parameters or configure OpenAPI or GraphQL",
		},
		{
			field:     "plugin.response_mapping",
			present:   plugin.ResponseMapping != nil,
			supported: supportsResponseMapping,
			reason:    "is only valid for openapi/graphql integrations; remove plugin.response_mapping or configure an OpenAPI or GraphQL surface",
		},
	}
	for _, check := range checks {
		if check.present && !check.supported {
			return fmt.Errorf("config validation: integration %q %s %s", name, check.field, check.reason)
		}
	}
	if !plugin.IsInline() && effectiveProvider != nil {
		if err := validateManifestBackedConnectionReferences(name, plugin, effectiveProvider); err != nil {
			return err
		}
		if err := validateManifestBackedConnectionDefaults(name, plugin, effectiveProvider); err != nil {
			return err
		}
	}
	return nil
}

func validateManagedParameterConfig(prefix string, headers map[string]string, params []ManagedParameterDef) error {
	if len(params) == 0 {
		return nil
	}
	if err := ValidateManagedParameters(params); err != nil {
		return fmt.Errorf("%s %w", prefix, err)
	}
	if err := ValidateManagedParameterHeaderConflicts(NormalizeHeaders(headers), params); err != nil {
		return fmt.Errorf("%s %w", prefix, err)
	}
	return nil
}

func validateUIPlugin(plugin *UIPluginDef) error {
	if plugin == nil {
		return nil
	}
	sourceCount := 0
	if plugin.Package != "" {
		sourceCount++
	}
	if plugin.Source != "" {
		sourceCount++
	}
	switch {
	case sourceCount == 0:
		return fmt.Errorf("config validation: ui plugin.package or plugin.source is required")
	case sourceCount > 1:
		return fmt.Errorf("config validation: ui plugin.package and plugin.source are mutually exclusive")
	}

	if plugin.Source != "" {
		if _, err := pluginsource.Parse(plugin.Source); err != nil {
			return fmt.Errorf("config validation: ui plugin.source: %w", err)
		}
		if plugin.Version == "" {
			return fmt.Errorf("config validation: ui plugin.version is required when plugin.source is set")
		}
		if err := pluginsource.ValidateVersion(plugin.Version); err != nil {
			return fmt.Errorf("config validation: ui plugin.version: %w", err)
		}
	}

	if plugin.Package != "" && plugin.Version != "" {
		return fmt.Errorf("config validation: ui plugin.version is only valid with plugin.source")
	}
	if strings.HasPrefix(plugin.Package, "http://") {
		return fmt.Errorf("config validation: ui plugin.package requires HTTPS; plain HTTP is not supported")
	}
	return nil
}

func ParseDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, fmt.Errorf("empty duration string")
	}
	if strings.HasSuffix(s, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return 0, fmt.Errorf("invalid day duration %q: %w", s, err)
		}
		if days <= 0 {
			return 0, fmt.Errorf("duration must be positive, got %q", s)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, err
	}
	if d <= 0 {
		return 0, fmt.Errorf("duration must be positive, got %q", s)
	}
	return d, nil
}

func NodeToMap(node yaml.Node) (map[string]any, error) {
	if node.Kind == 0 {
		return nil, nil
	}
	var out map[string]any
	if err := node.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func validateEgress(cfg *EgressConfig) error {
	switch cfg.DefaultAction {
	case "", "allow", "deny":
	default:
		return fmt.Errorf("config validation: egress.default_action must be \"allow\" or \"deny\", got %q", cfg.DefaultAction)
	}
	for i := range cfg.Policies {
		switch cfg.Policies[i].Action {
		case "allow", "deny":
		default:
			return fmt.Errorf("config validation: egress.policies[%d].action must be \"allow\" or \"deny\", got %q", i, cfg.Policies[i].Action)
		}
	}
	for i := range cfg.Credentials {
		c := &cfg.Credentials[i]
		if c.SecretRef == "" {
			return fmt.Errorf("config validation: egress.credentials[%d]: secret_ref is required", i)
		}
		if err := egress.ValidateCredentialGrant(egress.CredentialGrantValidationInput{
			SubjectKind: c.SubjectKind,
			SubjectID:   c.SubjectID,
			Operation:   c.Operation,
			Method:      c.Method,
			Host:        c.Host,
			PathPrefix:  c.PathPrefix,
			AuthStyle:   c.AuthStyle,
		}); err != nil {
			return fmt.Errorf("config validation: egress.credentials[%d]: %w", i, err)
		}
	}
	return nil
}
