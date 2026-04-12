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
	DefaultProviderRepo = "github.com/valon-technologies/gestalt-providers"

	DefaultWebUIProvider = DefaultProviderRepo + "/web/default"
	DefaultWebUIVersion  = "0.0.1-alpha.9"

	DefaultIndexedDBProvider = DefaultProviderRepo + "/indexeddb/relationaldb"
	DefaultIndexedDBVersion  = "0.0.1-alpha.2"
)

// PluginConnectionName is the implicit connection name used when storing
// tokens for plugin-only integrations that do not declare YAML connections.
const PluginConnectionName = "_plugin"

// PluginConnectionAlias is the user-facing alias that maps to
// PluginConnectionName. In hybrid integrations, mcp.connection can be set
// to "plugin" to reuse the plugin's OAuth token.
const PluginConnectionAlias = "plugin"

type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Providers ProvidersConfig `yaml:"providers"`
}

type ProvidersConfig struct {
	Auth       *ProviderEntry            `yaml:"auth,omitempty"`
	Secrets    *ProviderEntry            `yaml:"secrets,omitempty"`
	Telemetry  *ProviderEntry            `yaml:"telemetry,omitempty"`
	Audit      *ProviderEntry            `yaml:"audit,omitempty"`
	UI         *ProviderEntry            `yaml:"ui,omitempty"`
	IndexedDBs map[string]*ProviderEntry `yaml:"indexeddbs,omitempty"`
	Plugins    map[string]*ProviderEntry `yaml:"plugins,omitempty"`
}

// ProviderSource supports three modes via custom UnmarshalYAML:
//   - Builtin: source: "name"         -> ProviderSource{Builtin: "name"}
//   - Managed: source: {ref, version} -> ProviderSource{Ref: "...", Version: "..."}
//   - Local:   source: {path}         -> ProviderSource{Path: "..."}
type ProviderSource struct {
	Builtin string         `yaml:"-"`
	Ref     string         `yaml:"ref,omitempty"`
	Version string         `yaml:"version,omitempty"`
	Path    string         `yaml:"path,omitempty"`
	Auth    *SourceAuthDef `yaml:"auth,omitempty"`
}

func (s *ProviderSource) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		s.Builtin = strings.TrimSpace(value.Value)
		return nil
	}
	type raw ProviderSource
	return value.Decode((*raw)(s))
}

func (s ProviderSource) MarshalYAML() (any, error) {
	if s.Builtin != "" {
		return s.Builtin, nil
	}
	type raw ProviderSource
	return raw(s), nil
}

func (s ProviderSource) IsBuiltin() bool { return s.Builtin != "" }
func (s ProviderSource) IsManaged() bool { return s.Ref != "" }
func (s ProviderSource) IsLocal() bool   { return s.Path != "" }

// ProviderEntry is the universal configuration for any provider.
type ProviderEntry struct {
	Source       ProviderSource    `yaml:"source"`
	Config       yaml.Node         `yaml:"config,omitempty"`
	Disabled     bool              `yaml:"disabled,omitempty"`
	Env          map[string]string `yaml:"env,omitempty"`
	AllowedHosts []string          `yaml:"allowedHosts,omitempty"`
	DisplayName  string            `yaml:"displayName,omitempty"`
	Description  string            `yaml:"description,omitempty"`
	IconFile     string            `yaml:"iconFile,omitempty"`

	// Plugin-specific config fields (parsed from YAML, only valid on plugin entries)
	Connections       map[string]*ConnectionDef     `yaml:"connections,omitempty"`
	AllowedOperations map[string]*OperationOverride `yaml:"allowedOperations,omitempty"`

	// Runtime-resolved fields (populated during init/bootstrap, not from YAML)
	Command              string                              `yaml:"-"`
	Args                 []string                            `yaml:"-"`
	ResolvedManifestPath string                              `yaml:"-"`
	ResolvedManifest     *pluginmanifestv1.Manifest          `yaml:"-"`
	ResolvedIconFile     string                              `yaml:"-"`
	HostBinary           string                              `yaml:"-"`
	ConnectionMode       pluginmanifestv1.ConnectionMode     `yaml:"-"`
	Auth                 *ConnectionAuthDef                  `yaml:"-"`
	DefaultConnection    string                              `yaml:"-"`
	ConnectionParams     map[string]ConnectionParamDef       `yaml:"-"`
	MCP                  bool                                `yaml:"-"`
	Discovery            *pluginmanifestv1.ProviderDiscovery `yaml:"-"`
	ResolvedAssetRoot    string                              `yaml:"-"`
	MCPToolPrefix        string                              `yaml:"-"`
	IndexedDBs           map[string]string                   `yaml:"-"`
}

func (e *ProviderEntry) HasManagedSource() bool {
	return e != nil && e.Source.IsManaged()
}

func (e *ProviderEntry) HasLocalSource() bool {
	return e != nil && e.Source.IsLocal()
}

func (e *ProviderEntry) SourceRef() string {
	return e.Source.Ref
}

func (e *ProviderEntry) SourceVersion() string {
	return e.Source.Version
}

func (e *ProviderEntry) SourcePath() string {
	if e.Source.IsLocal() {
		return e.Source.Path
	}
	return ""
}

func (e *ProviderEntry) HasResolvedManifest() bool {
	return e != nil && e.ResolvedManifest != nil
}

func (e *ProviderEntry) ManifestSpec() *pluginmanifestv1.Spec {
	if e == nil || e.ResolvedManifest == nil {
		return nil
	}
	return e.ResolvedManifest.Spec
}

func (e *ProviderEntry) DeclaresMCP() bool {
	if e == nil {
		return false
	}
	if e.MCP {
		return true
	}
	spec := e.ManifestSpec()
	if spec == nil {
		return false
	}
	return spec.MCP
}

type SourceAuthDef struct {
	Token string `yaml:"token"`
}

type EgressConfig struct {
	DefaultAction string `yaml:"defaultAction"`
}

type ListenerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type ServerConfig struct {
	Public        ListenerConfig `yaml:"public"`
	Management    ListenerConfig `yaml:"management"`
	BaseURL       string         `yaml:"baseUrl"`
	EncryptionKey string         `yaml:"encryptionKey"`
	APITokenTTL   string         `yaml:"apiTokenTtl"`
	ArtifactsDir  string         `yaml:"artifactsDir"`
	IndexedDB     string         `yaml:"indexeddb,omitempty"`
	Egress        EgressConfig   `yaml:"egress,omitempty"`
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

// ConnectionDef owns authentication and connection parameters for a named
// connection. All connections in a single integration must share the same Mode.
type ConnectionDef struct {
	DisplayName      string                              `yaml:"displayName,omitempty"`
	Mode             pluginmanifestv1.ConnectionMode     `yaml:"mode"`
	Auth             ConnectionAuthDef                   `yaml:"auth"`
	ConnectionParams map[string]ConnectionParamDef       `yaml:"params"`
	Discovery        *pluginmanifestv1.ProviderDiscovery `yaml:"-"`
}

type ConnectionAuthDef struct {
	Type                pluginmanifestv1.AuthType `yaml:"type"`
	AuthorizationURL    string                    `yaml:"authorizationUrl"`
	TokenURL            string                    `yaml:"tokenUrl"`
	ClientID            string                    `yaml:"clientId"`
	ClientSecret        string                    `yaml:"clientSecret"`
	RedirectURL         string                    `yaml:"redirectUrl"`
	ClientAuth          string                    `yaml:"clientAuth"`
	TokenExchange       string                    `yaml:"tokenExchange"`
	Scopes              []string                  `yaml:"scopes"`
	ScopeParam          string                    `yaml:"scopeParam"`
	ScopeSeparator      string                    `yaml:"scopeSeparator"`
	PKCE                bool                      `yaml:"pkce"`
	AuthorizationParams map[string]string         `yaml:"authorizationParams"`
	TokenParams         map[string]string         `yaml:"tokenParams"`
	RefreshParams       map[string]string         `yaml:"refreshParams"`
	AcceptHeader        string                    `yaml:"acceptHeader"`
	AccessTokenPath     string                    `yaml:"accessTokenPath"`
	TokenMetadata       []string                  `yaml:"tokenMetadata"`
	Credentials         []CredentialFieldDef      `yaml:"credentials"`
	AuthMapping         *AuthMappingDef           `yaml:"authMapping"`
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
	if src.Type != "" {
		dst.Type = src.Type
	}
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
}

func MergeConnectionDef(dst *ConnectionDef, src *ConnectionDef) {
	if dst == nil || src == nil {
		return
	}
	if src.DisplayName != "" {
		dst.DisplayName = src.DisplayName
	}
	if src.Mode != "" {
		dst.Mode = src.Mode
	}
	MergeConnectionAuth(&dst.Auth, src.Auth)
	if len(src.ConnectionParams) > 0 {
		dst.ConnectionParams = maps.Clone(src.ConnectionParams)
	}
	if src.Discovery != nil {
		dst.Discovery = src.Discovery
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

func EffectivePluginConnectionDef(plugin *ProviderEntry, manifestPlugin *pluginmanifestv1.Spec) ConnectionDef {
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

func EffectiveNamedConnectionDef(plugin *ProviderEntry, manifestPlugin *pluginmanifestv1.Spec, name string) (ConnectionDef, bool) {
	conn := ConnectionDef{}
	found := false

	if manifestPlugin != nil && manifestPlugin.Connections != nil {
		if def, ok := manifestPlugin.Connections[name]; ok && def != nil {
			found = true
			conn.DisplayName = def.DisplayName
			if def.Mode != "" {
				conn.Mode = def.Mode
			}
			if def.Auth != nil {
				MergeConnectionAuth(&conn.Auth, ManifestAuthToConnectionAuthDef(def.Auth))
			}
			if len(def.Params) > 0 {
				conn.ConnectionParams = maps.Clone(def.Params)
			}
			if def.Discovery != nil {
				conn.Discovery = def.Discovery
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

func LoadAllowMissingEnv(path string) (*Config, error) {
	return loadWithLookup(path, os.LookupEnv, true)
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
	providersNode := mappingValueNode(documentValueNode(&root), "providers")
	pluginsNode := mappingValueNode(providersNode, "plugins")
	for name, entry := range cfg.Providers.Plugins {
		if entry == nil || !entry.HasManagedSource() {
			continue
		}
		if err := overlayManagedEntryConfigNode(mappingValueNode(pluginsNode, name), entry, "plugin "+strconv.Quote(name)); err != nil {
			return err
		}
	}
	for _, c := range []struct {
		name  string
		entry *ProviderEntry
	}{
		{"auth", cfg.Providers.Auth},
		{"secrets", cfg.Providers.Secrets},
		{"ui", cfg.Providers.UI},
		{"telemetry", cfg.Providers.Telemetry},
		{"audit", cfg.Providers.Audit},
	} {
		if c.entry == nil || !c.entry.HasManagedSource() {
			continue
		}
		node := mappingValueNode(providersNode, c.name)
		if err := overlayManagedEntryConfigNode(node, c.entry, c.name); err != nil {
			return err
		}
	}
	return nil
}

func overlayManagedEntryConfigNode(raw *yaml.Node, entry *ProviderEntry, subject string) error {
	if entry == nil || !entry.HasManagedSource() || raw == nil {
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
	entry.Config = node
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

func loadWithLookup(path string, lookup func(string) (string, bool), allowMissing bool) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	resolved, firstMissing, err := expandEnvVariables(string(data), lookup, !allowMissing)
	if err != nil {
		return nil, err
	}
	if !allowMissing && firstMissing != "" {
		return nil, fmt.Errorf("expanding config environment variables: environment variable %q not set; use ${%s:-} to allow an empty default", firstMissing, firstMissing)
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

func parseEnvPlaceholder(key string) (name string, allowEmptyDefault bool, err error) {
	if !strings.Contains(key, ":-") {
		return key, false, nil
	}
	name, defaultValue, _ := strings.Cut(key, ":-")
	if name == "" {
		return "", false, fmt.Errorf("invalid environment placeholder ${%s}", key)
	}
	if defaultValue != "" {
		return "", false, fmt.Errorf("unsupported environment placeholder ${%s}: only ${%s:-} is supported for empty defaults", key, name)
	}
	return name, true, nil
}

func expandEnvVariables(input string, lookup func(string) (string, bool), preserveMissing bool) (string, string, error) {
	var expandErr error
	var firstMissing string
	resolved := os.Expand(input, func(key string) string {
		if expandErr != nil {
			return ""
		}
		name, allowEmptyDefault, err := parseEnvPlaceholder(key)
		if err != nil {
			expandErr = err
			return ""
		}
		if val, ok := lookup(name); ok {
			return val
		}
		filePath, ok := lookup(name + "_FILE")
		if !ok || filePath == "" {
			if allowEmptyDefault {
				return ""
			}
			if preserveMissing {
				if firstMissing == "" {
					firstMissing = name
				}
				return "${" + key + "}"
			}
			return ""
		}
		data, err := os.ReadFile(filePath)
		if err != nil {
			expandErr = fmt.Errorf("resolving %s_FILE: %w", name, err)
			return ""
		}
		return strings.TrimRight(string(data), "\r\n")
	})
	if expandErr != nil {
		return "", "", fmt.Errorf("expanding config environment variables: %w", expandErr)
	}
	return resolved, firstMissing, nil
}

func overlayEnvIntoNode(node yaml.Node, lookup func(string) (string, bool), preserveMissing bool) (yaml.Node, error) {
	data, err := yaml.Marshal(&node)
	if err != nil {
		return yaml.Node{}, fmt.Errorf("marshaling config node: %w", err)
	}

	resolved, _, err := expandEnvVariables(string(data), lookup, preserveMissing)
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
	if cfg.Providers.Secrets == nil {
		cfg.Providers.Secrets = &ProviderEntry{Source: ProviderSource{Builtin: "env"}}
	} else if !cfg.Providers.Secrets.Disabled && !cfg.Providers.Secrets.Source.IsBuiltin() && !cfg.Providers.Secrets.Source.IsManaged() && !cfg.Providers.Secrets.Source.IsLocal() {
		cfg.Providers.Secrets.Source.Builtin = "env"
	}
	if cfg.Providers.Telemetry == nil {
		cfg.Providers.Telemetry = &ProviderEntry{Source: ProviderSource{Builtin: "stdout"}}
	} else if !cfg.Providers.Telemetry.Disabled && !cfg.Providers.Telemetry.Source.IsBuiltin() && !cfg.Providers.Telemetry.Source.IsManaged() && !cfg.Providers.Telemetry.Source.IsLocal() {
		cfg.Providers.Telemetry.Source.Builtin = "stdout"
	}
	if cfg.Providers.Audit == nil {
		cfg.Providers.Audit = &ProviderEntry{Source: ProviderSource{Builtin: "inherit"}}
	} else if !cfg.Providers.Audit.Disabled && !cfg.Providers.Audit.Source.IsBuiltin() && !cfg.Providers.Audit.Source.IsManaged() && !cfg.Providers.Audit.Source.IsLocal() {
		cfg.Providers.Audit.Source.Builtin = "inherit"
	}
	if cfg.Providers.UI == nil {
		cfg.Providers.UI = defaultUIProvider()
	} else if !cfg.Providers.UI.Disabled && !cfg.Providers.UI.Source.IsBuiltin() && !cfg.Providers.UI.Source.IsManaged() && !cfg.Providers.UI.Source.IsLocal() {
		cfg.Providers.UI = defaultUIProvider()
	}
}

func defaultUIProvider() *ProviderEntry {
	return &ProviderEntry{
		Source: ProviderSource{
			Ref:     DefaultWebUIProvider,
			Version: DefaultWebUIVersion,
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

	resolveEntry := func(entry *ProviderEntry) {
		if entry == nil {
			return
		}
		entry.IconFile = resolveRelativePath(baseDir, entry.IconFile)
		entry.Source.Path = resolveRelativePath(baseDir, entry.Source.Path)
	}

	resolveEntry(cfg.Providers.Auth)
	resolveEntry(cfg.Providers.Secrets)
	resolveEntry(cfg.Providers.Telemetry)
	resolveEntry(cfg.Providers.Audit)
	resolveEntry(cfg.Providers.UI)
	for _, entry := range cfg.Providers.IndexedDBs {
		resolveEntry(entry)
	}
	for _, entry := range cfg.Providers.Plugins {
		resolveEntry(entry)
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
