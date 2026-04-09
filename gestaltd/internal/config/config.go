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

	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
	"gopkg.in/yaml.v3"
)

// Callback paths must match the routes registered in server.go.
const (
	AuthCallbackPath        = "/api/v1/auth/login/callback"
	IntegrationCallbackPath = "/api/v1/auth/callback"
)

const (
	DefaultProviderRepo    = "github.com/valon-technologies/gestalt-providers"
	DefaultProviderVersion = "0.0.1-alpha.1"
	DefaultWebUIProvider   = DefaultProviderRepo + "/web/default"
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
	Provider          *ProviderDef `yaml:"provider"`
	Config            yaml.Node    `yaml:"config"`
	Disabled          bool         `yaml:"-"`
	ResolvedAssetRoot string       `yaml:"-"`
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

type ProviderDef struct {
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

func (p *ProviderDef) HasManagedArtifacts() bool {
	return p != nil && p.HasManagedSource()
}

func (p *ProviderDef) HasLocalSource() bool {
	return p != nil && p.Source != nil && p.Source.Path != ""
}

func (p *ProviderDef) HasManagedSource() bool {
	return p != nil && p.Source != nil && p.Source.Ref != ""
}

func (p *ProviderDef) SourcePath() string {
	if p == nil || p.Source == nil {
		return ""
	}
	return p.Source.Path
}

func (p *ProviderDef) SourceRef() string {
	if p == nil || p.Source == nil {
		return ""
	}
	return p.Source.Ref
}

func (p *ProviderDef) SourceVersion() string {
	if p == nil || p.Source == nil {
		return ""
	}
	return p.Source.Version
}

func (p *ProviderDef) HasResolvedManifest() bool {
	return p != nil && p.ResolvedManifest != nil
}

func (p *ProviderDef) ManifestPlugin() *pluginmanifestv1.Plugin {
	if p == nil || p.ResolvedManifest == nil {
		return nil
	}
	return p.ResolvedManifest.Plugin
}

func (p *ProviderDef) DeclaresMCP() bool {
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
	Provider *ProviderDef `yaml:"provider"`
	Config   yaml.Node    `yaml:"config"`

	BuiltinProvider string `yaml:"-"`
}

type AuthConfig struct {
	Provider *ProviderDef `yaml:"provider"`
	Config   yaml.Node    `yaml:"config"`
}

type DatastoreConfig struct {
	Provider *ProviderDef `yaml:"provider"`
	Config   yaml.Node    `yaml:"config"`
}

func (c *AuthConfig) UnmarshalYAML(value *yaml.Node) error {
	return unmarshalTopLevelComponentConfig(value, "AuthConfig", "auth", &c.Provider, &c.Config)
}

func (c *DatastoreConfig) UnmarshalYAML(value *yaml.Node) error {
	return unmarshalTopLevelComponentConfig(value, "DatastoreConfig", "datastore", &c.Provider, &c.Config)
}

func (c *UIConfig) UnmarshalYAML(value *yaml.Node) error {
	if value == nil || value.Kind == 0 {
		*c = UIConfig{}
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
			return fmt.Errorf("field %s not found in type config.UIConfig", key)
		}
	}
	c.Provider = nil
	c.Config = yaml.Node{}
	c.Disabled = false
	if providerNode := mappingValueNode(value, "provider"); providerNode != nil {
		decoded, disabled, err := decodeUIProvider(providerNode)
		if err != nil {
			return err
		}
		c.Provider = decoded
		c.Disabled = disabled
	}
	if configNode := mappingValueNode(value, "config"); configNode != nil {
		c.Config = *configNode
	}
	return nil
}

func (c *SecretsConfig) UnmarshalYAML(value *yaml.Node) error {
	if value == nil || value.Kind == 0 {
		c.Provider = nil
		c.BuiltinProvider = ""
		c.Config = yaml.Node{}
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
			return fmt.Errorf("field %s not found in type config.SecretsConfig", key)
		}
	}
	c.Provider = nil
	c.BuiltinProvider = ""
	c.Config = yaml.Node{}
	providerNode := mappingValueNode(value, "provider")
	if providerNode != nil {
		if providerNode.Kind == yaml.ScalarNode && providerNode.Tag != "!!null" {
			c.BuiltinProvider = strings.TrimSpace(providerNode.Value)
		} else if providerNode.Kind == yaml.MappingNode {
			decoded, err := decodeTopLevelComponentProvider("secrets", providerNode)
			if err != nil {
				return err
			}
			c.Provider = decoded
		} else if providerNode.Kind != yaml.ScalarNode || providerNode.Tag != "!!null" {
			return fmt.Errorf("secrets.provider must be a string or a provider reference mapping")
		}
	}
	if configNode := mappingValueNode(value, "config"); configNode != nil {
		c.Config = *configNode
	}
	return nil
}

func unmarshalTopLevelComponentConfig(value *yaml.Node, typeName, kind string, provider **ProviderDef, cfg *yaml.Node) error {
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

func decodeTopLevelComponentProvider(kind string, node *yaml.Node) (*ProviderDef, error) {
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
	var provider ProviderDef
	if err := node.Decode(&provider); err != nil {
		return nil, err
	}
	return &provider, nil
}

func decodeUIProvider(node *yaml.Node) (*ProviderDef, bool, error) {
	if node == nil || node.Kind == 0 {
		return nil, false, nil
	}
	if node.Kind == yaml.ScalarNode {
		value := strings.TrimSpace(node.Value)
		if node.Tag == "!!null" || value == "" {
			return nil, false, nil
		}
		if value == "none" {
			return nil, true, nil
		}
		return nil, false, fmt.Errorf(`config validation: ui.provider must be a provider reference mapping or the string "none"`)
	}
	var provider ProviderDef
	if err := node.Decode(&provider); err != nil {
		return nil, false, err
	}
	return &provider, false, nil
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
	Plugin        *ProviderDef `yaml:"plugin"`
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
type AuthMappingDef = pluginmanifestv1.AuthMapping
type BasicAuthMappingDef = pluginmanifestv1.BasicAuthMapping
type AuthValueDef = pluginmanifestv1.AuthValue
type AuthValueFromDef = pluginmanifestv1.AuthValueFrom
type CredentialFieldRefDef = pluginmanifestv1.CredentialFieldRef

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
		AuthMapping:         CloneAuthMapping(auth.AuthMapping),
	}
	return out
}

func CloneAuthMapping(src *AuthMappingDef) *AuthMappingDef {
	if src == nil {
		return nil
	}
	dst := &AuthMappingDef{}
	if len(src.Headers) > 0 {
		dst.Headers = make(map[string]AuthValueDef, len(src.Headers))
		for name, value := range src.Headers {
			dst.Headers[name] = cloneAuthValue(value)
		}
	}
	if src.Basic != nil {
		dst.Basic = &BasicAuthMappingDef{
			Username: cloneAuthValue(src.Basic.Username),
			Password: cloneAuthValue(src.Basic.Password),
		}
	}
	return dst
}

func cloneAuthValue(src AuthValueDef) AuthValueDef {
	dst := AuthValueDef{Value: src.Value}
	if src.ValueFrom != nil && src.ValueFrom.CredentialFieldRef != nil {
		dst.ValueFrom = &AuthValueFromDef{
			CredentialFieldRef: &CredentialFieldRefDef{Name: src.ValueFrom.CredentialFieldRef.Name},
		}
	}
	return dst
}

func EffectivePluginConnectionDef(plugin *ProviderDef, manifestPlugin *pluginmanifestv1.Plugin) ConnectionDef {
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

func EffectiveNamedConnectionDef(plugin *ProviderDef, manifestPlugin *pluginmanifestv1.Plugin, name string) (ConnectionDef, bool) {
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
	if err := overlayManagedComponentConfigNode(mappingValueNode(documentValueNode(&root), "secrets"), cfg.Secrets.Provider, &cfg.Secrets.Config, "secrets"); err != nil {
		return err
	}
	if err := overlayManagedComponentConfigNode(mappingValueNode(documentValueNode(&root), "ui"), cfg.UI.Provider, &cfg.UI.Config, "ui"); err != nil {
		return err
	}
	return nil
}

func overlayManagedPluginConfigNode(raw *yaml.Node, plugin *ProviderDef, subject string) error {
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

func overlayManagedComponentConfigNode(raw *yaml.Node, provider *ProviderDef, target *yaml.Node, subject string) error {
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
	if cfg.Server.Public.Port == 0 {
		cfg.Server.Public.Port = 8080
	}
	if cfg.Secrets.Provider == nil && cfg.Secrets.BuiltinProvider == "" {
		cfg.Secrets.BuiltinProvider = "env"
	}
	if cfg.Telemetry.Provider == "" {
		cfg.Telemetry.Provider = "stdout"
	}
	if cfg.Audit.Provider == "" {
		cfg.Audit.Provider = "inherit"
	}
	if !cfg.UI.Disabled && cfg.UI.Provider == nil {
		cfg.UI.Provider = defaultUIProvider()
	}
}

func defaultUIProvider() *ProviderDef {
	return &ProviderDef{
		Source: &PluginSourceDef{
			Ref:     DefaultWebUIProvider,
			Version: DefaultProviderVersion,
		},
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
	if cfg.Secrets.Provider != nil && cfg.Secrets.Provider.Source != nil {
		cfg.Secrets.Provider.Source.Path = resolveRelativePath(baseDir, cfg.Secrets.Provider.Source.Path)
	}
	if cfg.UI.Provider != nil && cfg.UI.Provider.Source != nil {
		cfg.UI.Provider.Source.Path = resolveRelativePath(baseDir, cfg.UI.Provider.Source.Path)
	}
}

func resolveRelativePath(baseDir, value string) string {
	if value == "" || filepath.IsAbs(value) {
		return value
	}
	return filepath.Clean(filepath.Join(baseDir, value))
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
