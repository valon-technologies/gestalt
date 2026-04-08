package config

import (
	"fmt"
	"io"
	"maps"
	"net"
	"os"
	"path/filepath"
	"slices"
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
	Audit        AuditConfig               `yaml:"audit"`
	Integrations map[string]IntegrationDef `yaml:"plugins"`
	Server       ServerConfig              `yaml:"server"`
	Egress       EgressConfig              `yaml:"egress"`
	UI           UIConfig                  `yaml:"ui"`
}

type TelemetryConfig struct {
	Provider string    `yaml:"provider"`
	Config   yaml.Node `yaml:"config"`
}

type AuditConfig struct {
	Provider string    `yaml:"provider"`
	Config   yaml.Node `yaml:"config"`
}

type UIConfig struct {
	Plugin *UIPluginDef `yaml:"plugin"`
}

type UIPluginDef struct {
	Source  string `yaml:"source"`
	Version string `yaml:"version"`

	ResolvedAssetRoot string `yaml:"-"`
}

func (p *UIPluginDef) HasManagedArtifacts() bool {
	return p != nil && p.Source != ""
}

type PluginSourceAuthDef struct {
	Token string `yaml:"token"`
}

type PluginSourceDef struct {
	Path    string               `yaml:"path"`
	Ref     string               `yaml:"ref"`
	Version string               `yaml:"version"`
	Auth    *PluginSourceAuthDef `yaml:"auth,omitempty"`
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
	Command string            `yaml:"-"`
	Source  *PluginSourceDef  `yaml:"source"`
	Args    []string          `yaml:"-"`
	Env     map[string]string `yaml:"env"`

	Config       yaml.Node `yaml:"-"`
	AllowedHosts []string  `yaml:"allowed_hosts"`

	PostConnectDiscovery *pluginmanifestv1.ProviderPostConnectDiscovery `yaml:"-"`

	Auth              *ConnectionAuthDef        `yaml:"-"`
	ConnectionMode    string                    `yaml:"-"`
	Connections       map[string]*ConnectionDef `yaml:"-"`
	DefaultConnection string                    `yaml:"-"`

	ConnectionParams  map[string]ConnectionParamDef `yaml:"-"`
	MCP               bool                          `yaml:"-"`
	AllowedOperations map[string]*OperationOverride `yaml:"-"`

	ResolvedManifestPath string                     `yaml:"-"`
	ResolvedManifest     *pluginmanifestv1.Manifest `yaml:"-"`
	ResolvedIconFile     string                     `yaml:"-"`
	HostBinary           string                     `yaml:"-"`
}

func (p *PluginDef) HasManagedArtifacts() bool {
	return p != nil && p.HasManagedSource()
}

func (p *PluginDef) HasLocalSource() bool {
	return p != nil && p.Source != nil && p.Source.Path != ""
}

func (p *PluginDef) HasManagedSource() bool {
	return p != nil && p.Source != nil && p.Source.Ref != ""
}

func (p *PluginDef) SourcePath() string {
	if p == nil || p.Source == nil {
		return ""
	}
	return p.Source.Path
}

func (p *PluginDef) SourceRef() string {
	if p == nil || p.Source == nil {
		return ""
	}
	return p.Source.Ref
}

func (p *PluginDef) SourceVersion() string {
	if p == nil || p.Source == nil {
		return ""
	}
	return p.Source.Version
}

func (p *PluginDef) HasResolvedManifest() bool {
	return p != nil && p.ResolvedManifest != nil
}

func (p *PluginDef) ManifestPlugin() *pluginmanifestv1.Plugin {
	if p == nil || p.ResolvedManifest == nil {
		return nil
	}
	return p.ResolvedManifest.Plugin
}

func (p *PluginDef) DeclaresMCP() bool {
	if p == nil {
		return false
	}
	if p.MCP {
		return true
	}
	if !p.HasResolvedManifest() {
		return false
	}
	provider := p.ManifestPlugin()
	if provider == nil {
		return false
	}
	return provider.MCP
}

type SecretsConfig struct {
	Provider string    `yaml:"provider"`
	Config   yaml.Node `yaml:"config"`
}

type AuthConfig struct {
	Provider *PluginDef `yaml:"provider"`
	Config   yaml.Node  `yaml:"config"`
}

type DatastoreConfig struct {
	Provider *PluginDef `yaml:"provider"`
	Config   yaml.Node  `yaml:"config"`
}

func (c *AuthConfig) UnmarshalYAML(value *yaml.Node) error {
	return unmarshalTopLevelComponentConfig(value, "AuthConfig", "auth", &c.Provider, &c.Config)
}

func (c *DatastoreConfig) UnmarshalYAML(value *yaml.Node) error {
	return unmarshalTopLevelComponentConfig(value, "DatastoreConfig", "datastore", &c.Provider, &c.Config)
}

func unmarshalTopLevelComponentConfig(value *yaml.Node, typeName, kind string, provider **PluginDef, cfg *yaml.Node) error {
	if value == nil || value.Kind == 0 {
		*provider = nil
		*cfg = yaml.Node{}
		return nil
	}
	if value.Kind != yaml.MappingNode {
		var probe map[string]any
		return value.Decode(&probe)
	}
	for i := 0; i+1 < len(value.Content); i += 2 {
		key := value.Content[i].Value
		switch key {
		case "provider", "config":
		default:
			return fmt.Errorf("field %s not found in type config.%s", key, typeName)
		}
	}
	*provider = nil
	*cfg = yaml.Node{}
	if providerNode := mappingValueNode(value, "provider"); providerNode != nil {
		decoded, err := decodeTopLevelComponentProvider(kind, providerNode)
		if err != nil {
			return err
		}
		*provider = decoded
	}
	if configNode := mappingValueNode(value, "config"); configNode != nil {
		*cfg = *configNode
	}
	return nil
}

func decodeTopLevelComponentProvider(kind string, node *yaml.Node) (*PluginDef, error) {
	if node == nil || node.Kind == 0 {
		return nil, nil
	}
	if node.Kind == yaml.ScalarNode {
		value := strings.TrimSpace(node.Value)
		if node.Tag == "!!null" || value == "" {
			return nil, nil
		}
		if kind == "auth" && value == "none" {
			return nil, nil
		}
		return nil, fmt.Errorf("config validation: %s.provider must be a provider reference mapping%s", kind, authProviderScalarHint(kind))
	}
	var provider PluginDef
	if err := node.Decode(&provider); err != nil {
		return nil, err
	}
	return &provider, nil
}

func authProviderScalarHint(kind string) string {
	if kind == "auth" {
		return ` or the string "none"`
	}
	return ""
}

type ListenerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type ServerConfig struct {
	Port          int            `yaml:"port"`
	Public        ListenerConfig `yaml:"public"`
	Management    ListenerConfig `yaml:"management"`
	BaseURL       string         `yaml:"base_url"`
	EncryptionKey string         `yaml:"encryption_key"`
	APITokenTTL   string         `yaml:"api_token_ttl"`
	ArtifactsDir  string         `yaml:"artifacts_dir"`
}

func (s ServerConfig) PublicListener() ListenerConfig {
	port := s.Public.Port
	if port == 0 {
		port = s.Port
	}
	if port == 0 {
		port = 8080
	}
	return ListenerConfig{
		Host: s.Public.Host,
		Port: port,
	}
}

func (s ServerConfig) PublicAddr() string {
	listener := s.PublicListener()
	return net.JoinHostPort(listener.Host, strconv.Itoa(listener.Port))
}

func (s ServerConfig) ManagementListener() (ListenerConfig, bool) {
	if s.Management.Port == 0 {
		return ListenerConfig{}, false
	}
	return s.Management, true
}

func (s ServerConfig) ManagementAddr() string {
	listener, ok := s.ManagementListener()
	if !ok {
		return ""
	}
	return net.JoinHostPort(listener.Host, strconv.Itoa(listener.Port))
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

type CredentialFieldDef = pluginmanifestv1.CredentialField

type AuthMappingDef struct {
	Headers map[string]string `yaml:"headers"`
}

type ConnectionParamDef = pluginmanifestv1.ProviderConnectionParam

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
		dst.ConnectionParams = maps.Clone(src.ConnectionParams)
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
		Scopes:              slices.Clone(auth.Scopes),
		ScopeParam:          auth.ScopeParam,
		ScopeSeparator:      auth.ScopeSeparator,
		PKCE:                auth.PKCE,
		AuthorizationParams: maps.Clone(auth.AuthorizationParams),
		TokenParams:         maps.Clone(auth.TokenParams),
		RefreshParams:       maps.Clone(auth.RefreshParams),
		AcceptHeader:        auth.AcceptHeader,
		AccessTokenPath:     auth.AccessTokenPath,
		TokenMetadata:       slices.Clone(auth.TokenMetadata),
		Credentials:         slices.Clone(auth.Credentials),
	}
	return out
}

func EffectivePluginConnectionDef(plugin *PluginDef, manifestPlugin *pluginmanifestv1.Plugin) ConnectionDef {
	conn := ConnectionDef{}
	if manifestPlugin != nil {
		conn.Mode = manifestPlugin.ConnectionMode
		if len(manifestPlugin.ConnectionParams) > 0 {
			conn.ConnectionParams = maps.Clone(manifestPlugin.ConnectionParams)
		}
		if manifestPlugin.Auth != nil {
			MergeConnectionAuth(&conn.Auth, ManifestAuthToConnectionAuthDef(manifestPlugin.Auth))
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

func EffectiveNamedConnectionDef(plugin *PluginDef, manifestPlugin *pluginmanifestv1.Plugin, name string) (ConnectionDef, bool) {
	conn := ConnectionDef{}
	found := false

	if manifestPlugin != nil && manifestPlugin.Connections != nil {
		if def, ok := manifestPlugin.Connections[name]; ok && def != nil {
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
type OperationOverride = pluginmanifestv1.ManifestOperationOverride

func Load(path string) (*Config, error) {
	return loadWithLookup(path, os.LookupEnv, false)
}

func LoadWithMapping(path string, getenv func(string) string) (*Config, error) {
	return loadWithLookup(path, func(key string) (string, bool) {
		// Preserve the legacy os.Expand-style contract for callers that only
		// provide a string mapping: the mapped value wins even when it is empty.
		return getenv(key), true
	}, false)
}

func LoadWithLookup(path string, lookup func(string) (string, bool)) (*Config, error) {
	return loadWithLookup(path, lookup, false)
}

func OverlayManagedPluginConfig(path string, cfg *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading config file: %w", err)
	}

	var root yaml.Node
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	if err := dec.Decode(&root); err != nil && err != io.EOF {
		return fmt.Errorf("parsing config YAML: %w", err)
	}
	plugins := mappingValueNode(documentValueNode(&root), "plugins")
	for name := range cfg.Integrations {
		intg := cfg.Integrations[name]
		if intg.Plugin == nil || !intg.Plugin.HasManagedArtifacts() {
			continue
		}
		if err := overlayManagedPluginConfigNode(mappingValueNode(plugins, name), intg.Plugin, "integration "+strconv.Quote(name)); err != nil {
			return err
		}
		cfg.Integrations[name] = intg
	}
	if err := overlayManagedComponentConfigNode(mappingValueNode(documentValueNode(&root), "auth"), cfg.Auth.Provider, &cfg.Auth.Config, "auth"); err != nil {
		return err
	}
	if err := overlayManagedComponentConfigNode(mappingValueNode(documentValueNode(&root), "datastore"), cfg.Datastore.Provider, &cfg.Datastore.Config, "datastore"); err != nil {
		return err
	}
	return nil
}

func overlayManagedPluginConfigNode(raw *yaml.Node, plugin *PluginDef, subject string) error {
	if plugin == nil || !plugin.HasManagedArtifacts() || raw == nil {
		return nil
	}
	providerNode := mappingValueNode(raw, "provider")
	if providerNode == nil {
		return nil
	}
	configNode := mappingValueNode(raw, "config")
	if configNode == nil || configNode.Kind == 0 {
		configNode = mappingValueNode(providerNode, "config")
	}
	if configNode == nil || configNode.Kind == 0 {
		return nil
	}
	node, err := overlayEnvIntoNode(*configNode, os.LookupEnv, true)
	if err != nil {
		return fmt.Errorf("expanding managed plugin config for %s: %w", subject, err)
	}
	plugin.Config = node
	return nil
}

func overlayManagedComponentConfigNode(raw *yaml.Node, provider *PluginDef, target *yaml.Node, subject string) error {
	if provider == nil || !provider.HasManagedArtifacts() || raw == nil {
		return nil
	}
	configNode := mappingValueNode(raw, "config")
	if configNode == nil || configNode.Kind == 0 {
		return nil
	}
	node, err := overlayEnvIntoNode(*configNode, os.LookupEnv, true)
	if err != nil {
		return fmt.Errorf("expanding managed provider config for %s: %w", subject, err)
	}
	*target = node
	return nil
}

func documentValueNode(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.DocumentNode && len(node.Content) == 1 {
		return node.Content[0]
	}
	return node
}

func mappingValueNode(node *yaml.Node, key string) *yaml.Node {
	node = documentValueNode(node)
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func loadWithLookup(path string, lookup func(string) (string, bool), preserveMissing bool) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	resolved, err := expandEnvVariables(string(data), lookup, preserveMissing)
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

func expandEnvVariables(input string, lookup func(string) (string, bool), preserveMissing bool) (string, error) {
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
			if preserveMissing {
				return "${" + key + "}"
			}
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

func overlayEnvIntoNode(node yaml.Node, lookup func(string) (string, bool), preserveMissing bool) (yaml.Node, error) {
	data, err := yaml.Marshal(&node)
	if err != nil {
		return yaml.Node{}, fmt.Errorf("marshaling config node: %w", err)
	}

	resolved, err := expandEnvVariables(string(data), lookup, preserveMissing)
	if err != nil {
		return yaml.Node{}, err
	}

	var out yaml.Node
	dec := yaml.NewDecoder(strings.NewReader(resolved))
	if err := dec.Decode(&out); err != nil && err != io.EOF {
		return yaml.Node{}, fmt.Errorf("parsing config YAML: %w", err)
	}
	if out.Kind == yaml.DocumentNode && len(out.Content) == 1 {
		return *out.Content[0], nil
	}
	return out, nil
}

func applyDefaults(cfg *Config) {
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.Server.Public.Port != 0 {
		cfg.Server.Port = cfg.Server.Public.Port
	}
	if cfg.Secrets.Provider == "" {
		cfg.Secrets.Provider = "env"
	}
	if cfg.Telemetry.Provider == "" {
		cfg.Telemetry.Provider = "stdout"
	}
	if cfg.Audit.Provider == "" {
		cfg.Audit.Provider = "inherit"
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
			if intg.Plugin.Source != nil {
				intg.Plugin.Source.Path = resolveRelativePath(baseDir, intg.Plugin.Source.Path)
			}
		}
		cfg.Integrations[name] = intg
	}
	if cfg.Auth.Provider != nil && cfg.Auth.Provider.Source != nil {
		cfg.Auth.Provider.Source.Path = resolveRelativePath(baseDir, cfg.Auth.Provider.Source.Path)
	}
	if cfg.Datastore.Provider != nil && cfg.Datastore.Provider.Source != nil {
		cfg.Datastore.Provider.Source.Path = resolveRelativePath(baseDir, cfg.Datastore.Provider.Source.Path)
	}

}

func resolveRelativePath(baseDir, value string) string {
	if value == "" || filepath.IsAbs(value) {
		return value
	}
	return filepath.Clean(filepath.Join(baseDir, value))
}

// ValidateStructure checks config shape: integration references, plugin
// declarations, connection references, URL template params, egress rules.
// Called by Load (and therefore by init, validate, and serve). Does not require
// runtime secrets like encryption_key.
func ValidateStructure(cfg *Config) error {
	if err := validateServerListeners(cfg.Server); err != nil {
		return err
	}
	if cfg.Server.APITokenTTL != "" {
		if _, err := ParseDuration(cfg.Server.APITokenTTL); err != nil {
			return fmt.Errorf("config validation: server.api_token_ttl: %w", err)
		}
	}
	if err := validateAudit(cfg.Audit); err != nil {
		return err
	}
	if err := validateEgress(&cfg.Egress); err != nil {
		return err
	}
	if err := validateUIPlugin(cfg.UI.Plugin); err != nil {
		return err
	}
	if err := validateTopLevelComponentConfig("auth", cfg.Auth.Provider, cfg.Auth.Config); err != nil {
		return err
	}
	if err := validateTopLevelComponentConfig("datastore", cfg.Datastore.Provider, cfg.Datastore.Config); err != nil {
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

func validateAudit(cfg AuditConfig) error {
	switch cfg.Provider {
	case "", "inherit", "noop":
		if cfg.Config.Kind == 0 {
			return nil
		}
		return fmt.Errorf("config validation: audit.config is not supported when audit.provider is %q", cfg.Provider)
	case "stdout":
		if cfg.Config.Kind == 0 {
			return nil
		}
		var stdoutCfg struct {
			Level  string `yaml:"level"`
			Format string `yaml:"format"`
		}
		if err := cfg.Config.Decode(&stdoutCfg); err != nil {
			return fmt.Errorf("config validation: stdout audit: parsing config: %w", err)
		}
		return nil
	case "otlp":
		if cfg.Config.Kind == 0 {
			return nil
		}
		var otlpCfg struct {
			Protocol string `yaml:"protocol"`
			Logs     struct {
				Exporter string `yaml:"exporter"`
			} `yaml:"logs"`
		}
		if err := cfg.Config.Decode(&otlpCfg); err != nil {
			return fmt.Errorf("config validation: otlp audit: parsing config: %w", err)
		}
		if otlpCfg.Protocol != "" {
			switch strings.ToLower(otlpCfg.Protocol) {
			case "grpc", "http":
			default:
				return fmt.Errorf("config validation: otlp audit: unknown protocol %q (expected \"grpc\" or \"http\")", otlpCfg.Protocol)
			}
		}
		if otlpCfg.Logs.Exporter != "" && !strings.EqualFold(otlpCfg.Logs.Exporter, "otlp") {
			return fmt.Errorf("config validation: otlp audit: logs.exporter must be %q", "otlp")
		}
		return nil
	default:
		return fmt.Errorf("config validation: unknown audit.provider %q", cfg.Provider)
	}
}

func validateTopLevelComponentProvider(kind string, provider *PluginDef) error {
	if provider == nil {
		return nil
	}
	if provider.Config.Kind != 0 {
		return fmt.Errorf("config validation: %s.provider.config is not supported; use %s.config", kind, kind)
	}
	return validateExternalPlugin(kind, kind, provider)
}

func validateTopLevelComponentConfig(kind string, provider *PluginDef, cfg yaml.Node) error {
	if provider == nil {
		if cfg.Kind != 0 {
			return fmt.Errorf("config validation: %s.config is not supported when %s.provider is unset", kind, kind)
		}
		return nil
	}
	return validateTopLevelComponentProvider(kind, provider)
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
		if err := validateManifestBackedIntegration(name, intg.Plugin); err != nil {
			return err
		}
	}
	return nil
}

// ValidateRuntime checks runtime-only requirements: encryption key plus the
// required top-level datastore provider. Platform auth is optional; omitting it
// or setting auth.provider to "none" disables authentication. Callers that need a fully
// operational config (serve) should call this after Load. Callers that only
// need structural correctness (init, validate) should not.
func ValidateRuntime(cfg *Config) error {
	if cfg.Datastore.Provider == nil {
		return fmt.Errorf("config validation: datastore.provider is required")
	}
	if cfg.Server.EncryptionKey == "" {
		return fmt.Errorf("config validation: server.encryption_key is required")
	}
	return nil
}

func validatePluginIntegration(name string, intg IntegrationDef) error {
	if intg.Plugin == nil {
		return fmt.Errorf("config validation: integration %q requires a plugin", name)
	}
	p := intg.Plugin
	if err := validateExternalPlugin("integration", name, p); err != nil {
		return err
	}
	return validateManifestBackedIntegration(name, p)
}

type inlineConnectionReference struct {
	field    string
	name     string
	required bool
	context  string
}

func manifestBackedConnectionReferences(plugin *PluginDef, provider *pluginmanifestv1.Plugin) []inlineConnectionReference {
	if provider == nil {
		return nil
	}

	defaultConnection := provider.DefaultConnection
	if plugin != nil && plugin.DefaultConnection != "" {
		defaultConnection = plugin.DefaultConnection
	}
	if defaultConnection == "" {
		return nil
	}
	return []inlineConnectionReference{
		{
			field: "plugin.default_connection",
			name:  defaultConnection,
		},
	}
}

func validateManifestBackedConnectionReferences(name string, plugin *PluginDef, provider *pluginmanifestv1.Plugin) error {
	return validateConnectionReferences(name, declaredManifestBackedConnections(plugin, provider), manifestBackedConnectionReferences(plugin, provider))
}

func validateManifestBackedConnectionDefaults(name string, plugin *PluginDef, provider *pluginmanifestv1.Plugin) error {
	return validateConnectionDefaults(name, "integration", len(declaredManifestBackedConnections(plugin, provider)), manifestBackedConnectionReferences(plugin, provider))
}

func validateExecutableConnectionAuthSupport(name string, plugin *PluginDef, provider *pluginmanifestv1.Plugin) error {
	supportsMCPOAuth := provider != nil && provider.MCPURL != ""
	if conn := EffectivePluginConnectionDef(plugin, provider); conn.Auth.Type == pluginmanifestv1.AuthTypeMCPOAuth && !supportsMCPOAuth {
		return fmt.Errorf("config validation: integration %q plugin auth type %q requires an MCP surface", name, pluginmanifestv1.AuthTypeMCPOAuth)
	}

	declared := declaredManifestBackedConnections(plugin, provider)
	declaredNames := make([]string, 0, len(declared))
	for connName := range declared {
		declaredNames = append(declaredNames, connName)
	}
	slices.Sort(declaredNames)
	for _, connName := range declaredNames {
		conn, ok := EffectiveNamedConnectionDef(plugin, provider, connName)
		if !ok || conn.Auth.Type != pluginmanifestv1.AuthTypeMCPOAuth {
			continue
		}
		if !supportsMCPOAuth {
			return fmt.Errorf("config validation: integration %q connection %q auth type %q requires an MCP surface", name, connName, pluginmanifestv1.AuthTypeMCPOAuth)
		}
	}
	return nil
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

func declaredManifestBackedConnections(plugin *PluginDef, provider *pluginmanifestv1.Plugin) map[string]struct{} {
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
	if plugin.Source != nil {
		modeCount := 0
		if plugin.Source.Path != "" {
			modeCount++
		}
		if plugin.Source.Ref != "" {
			modeCount++
		}
		switch {
		case modeCount == 0:
			return fmt.Errorf("config validation: %s %q plugin.source.path or plugin.source.ref is required when plugin.source is set", kind, name)
		case modeCount > 1:
			return fmt.Errorf("config validation: %s %q plugin.source.path and plugin.source.ref are mutually exclusive", kind, name)
		}
	}
	modeCount := 0
	if plugin.HasLocalSource() {
		modeCount++
	}
	if plugin.HasManagedSource() {
		modeCount++
	}
	switch {
	case modeCount == 0:
		return fmt.Errorf("config validation: %s %q provider.source.path or provider.source.ref is required", kind, name)
	case modeCount > 1:
		return fmt.Errorf("config validation: %s %q provider.source.path and provider.source.ref are mutually exclusive", kind, name)
	}
	if plugin.HasManagedSource() {
		if _, err := pluginsource.Parse(plugin.SourceRef()); err != nil {
			return fmt.Errorf("config validation: %s %q plugin.source.ref: %w", kind, name, err)
		}
		if plugin.SourceVersion() == "" {
			return fmt.Errorf("config validation: %s %q plugin.source.version is required when plugin.source.ref is set", kind, name)
		}
		if err := pluginsource.ValidateVersion(plugin.SourceVersion()); err != nil {
			return fmt.Errorf("config validation: %s %q plugin.source.version: %w", kind, name, err)
		}
	}

	if plugin.HasLocalSource() && plugin.SourceVersion() != "" {
		return fmt.Errorf("config validation: %s %q plugin.source.version is only valid with plugin.source.ref", kind, name)
	}
	if plugin.Source != nil && plugin.Source.Auth != nil {
		if !plugin.HasManagedSource() {
			return fmt.Errorf("config validation: %s %q plugin.source.auth is only valid with plugin.source.ref", kind, name)
		}
		if strings.TrimSpace(plugin.Source.Auth.Token) == "" {
			return fmt.Errorf("config validation: %s %q plugin.source.auth.token is required when plugin.source.auth is set", kind, name)
		}
	}

	if kind != "integration" {
		hasIntegrationConfig := plugin.Auth != nil || len(plugin.Connections) > 0 ||
			len(plugin.ConnectionParams) > 0 || plugin.MCP || len(plugin.AllowedOperations) > 0 ||
			plugin.DefaultConnection != "" || plugin.PostConnectDiscovery != nil
		if hasIntegrationConfig {
			return fmt.Errorf("config validation: %s %q provider cannot use integration-only fields", kind, name)
		}
	}

	return nil
}

func validateManifestBackedIntegration(name string, plugin *PluginDef) error {
	if plugin == nil {
		return nil
	}
	effectiveProvider := plugin.ManifestPlugin()
	if effectiveProvider != nil {
		if err := validateManifestBackedConnectionReferences(name, plugin, effectiveProvider); err != nil {
			return err
		}
		if err := validateManifestBackedConnectionDefaults(name, plugin, effectiveProvider); err != nil {
			return err
		}
	}
	if err := validateExecutableConnectionAuthSupport(name, plugin, effectiveProvider); err != nil {
		return err
	}
	return nil
}

func validateUIPlugin(plugin *UIPluginDef) error {
	if plugin == nil {
		return nil
	}
	if plugin.Source == "" {
		return fmt.Errorf("config validation: ui plugin.source is required")
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

	return nil
}

func validateServerListeners(cfg ServerConfig) error {
	public := cfg.PublicListener()
	if public.Port <= 0 {
		return fmt.Errorf("config validation: server.public.port must be greater than zero")
	}
	if _, err := net.ResolveTCPAddr("tcp", net.JoinHostPort(public.Host, strconv.Itoa(public.Port))); err != nil {
		return fmt.Errorf("config validation: server.public is invalid: %w", err)
	}

	management, ok := cfg.ManagementListener()
	if !ok {
		if cfg.Management.Host != "" {
			return fmt.Errorf("config validation: server.management.port is required when server.management.host is set")
		}
		return nil
	}
	if management.Port <= 0 {
		return fmt.Errorf("config validation: server.management.port must be greater than zero")
	}
	managementAddr := net.JoinHostPort(management.Host, strconv.Itoa(management.Port))
	if _, err := net.ResolveTCPAddr("tcp", managementAddr); err != nil {
		return fmt.Errorf("config validation: server.management is invalid: %w", err)
	}
	if managementAddr == cfg.PublicAddr() {
		return fmt.Errorf("config validation: server.management must differ from server.public")
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
		if strings.HasPrefix(c.SecretRef, "secret://") {
			return fmt.Errorf("config validation: egress.credentials[%d]: secret_ref must be a bare secret name without secret://", i)
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
