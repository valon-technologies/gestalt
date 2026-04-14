package config

import (
	"fmt"
	"io"
	"maps"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"gopkg.in/yaml.v3"
)

// Callback paths must match the routes registered in server.go.
const (
	AuthCallbackPath        = "/api/v1/auth/login/callback"
	IntegrationCallbackPath = "/api/v1/auth/callback"
)

const (
	DefaultProviderRepo = "github.com/valon-technologies/gestalt-providers"

	DefaultIndexedDBProvider = DefaultProviderRepo + "/indexeddb/relationaldb"
	DefaultIndexedDBVersion  = "0.0.1-alpha.2"
	DefaultProviderInstance  = "default"
)

// PluginConnectionName is the implicit connection name used when storing
// tokens for plugin-only integrations that do not declare YAML connections.
const PluginConnectionName = "_plugin"

// PluginConnectionAlias is the user-facing alias that maps to
// PluginConnectionName. In hybrid integrations, mcp.connection can be set
// to "plugin" to reuse the plugin's OAuth token.
const PluginConnectionAlias = "plugin"

type Config struct {
	Server        ServerConfig              `yaml:"server"`
	Authorization AuthorizationConfig       `yaml:"authorization,omitempty"`
	Apps          map[string]*AppEntry      `yaml:"apps,omitempty"`
	Providers     ProvidersConfig           `yaml:"providers"`
	Plugins       map[string]*ProviderEntry `yaml:"plugins,omitempty"`
}

type AppEntry struct {
	Plugin              string `yaml:"plugin"`
	UI                  string `yaml:"ui"`
	Path                string `yaml:"path,omitempty"`
	AuthorizationPolicy string `yaml:"authorizationPolicy,omitempty"`
	Disabled            bool   `yaml:"disabled,omitempty"`
}

type ProvidersConfig struct {
	Auth      map[string]*ProviderEntry `yaml:"auth,omitempty"`
	Secrets   map[string]*ProviderEntry `yaml:"secrets,omitempty"`
	Telemetry map[string]*ProviderEntry `yaml:"telemetry,omitempty"`
	Audit     map[string]*ProviderEntry `yaml:"audit,omitempty"`
	UI        map[string]*UIEntry       `yaml:"ui,omitempty"`
	IndexedDB map[string]*ProviderEntry `yaml:"indexeddb,omitempty"`

	// Legacy aliases retained for internal call sites and tests while the
	// canonical YAML shape is migrating to top-level plugins and indexeddb.
	IndexedDBs map[string]*ProviderEntry `yaml:"-"`
	Plugins    map[string]*ProviderEntry `yaml:"-"`
}

type HostProviderKind string

const (
	HostProviderKindAuth      HostProviderKind = "auth"
	HostProviderKindSecrets   HostProviderKind = "secrets"
	HostProviderKindTelemetry HostProviderKind = "telemetry"
	HostProviderKindAudit     HostProviderKind = "audit"
	HostProviderKindIndexedDB HostProviderKind = "indexeddb"
)

type ServerProvidersConfig struct {
	Auth      string `yaml:"auth,omitempty"`
	Secrets   string `yaml:"secrets,omitempty"`
	Telemetry string `yaml:"telemetry,omitempty"`
	Audit     string `yaml:"audit,omitempty"`
	IndexedDB string `yaml:"indexeddb,omitempty"`
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
	Default      bool              `yaml:"default,omitempty"`
	Disabled     bool              `yaml:"disabled,omitempty"`
	Env          map[string]string `yaml:"env,omitempty"`
	AllowedHosts []string          `yaml:"allowedHosts,omitempty"`
	DisplayName  string            `yaml:"displayName,omitempty"`
	Description  string            `yaml:"description,omitempty"`
	IconFile     string            `yaml:"iconFile,omitempty"`
	// AuthorizationPolicy binds this provider to a shared human access policy.
	AuthorizationPolicy string `yaml:"authorizationPolicy,omitempty"`

	// Plugin-specific config fields (parsed from YAML, only valid on plugin entries)
	Connections       map[string]*ConnectionDef     `yaml:"connections,omitempty"`
	AllowedOperations map[string]*OperationOverride `yaml:"allowedOperations,omitempty"`
	IndexedDBs        []PluginIndexedDBBinding      `yaml:"indexeddb,omitempty"`
	IndexedDBSchema   string                        `yaml:"indexeddbSchema,omitempty"`
	Surfaces          *ProviderSurfaceOverrides     `yaml:"surfaces,omitempty"`

	// Runtime-resolved fields (populated during init/bootstrap, not from YAML)
	Command              string                                `yaml:"-"`
	Args                 []string                              `yaml:"-"`
	ResolvedManifestPath string                                `yaml:"-"`
	ResolvedManifest     *providermanifestv1.Manifest          `yaml:"-"`
	ResolvedIconFile     string                                `yaml:"-"`
	HostBinary           string                                `yaml:"-"`
	ConnectionMode       providermanifestv1.ConnectionMode     `yaml:"-"`
	Auth                 *ConnectionAuthDef                    `yaml:"-"`
	DefaultConnection    string                                `yaml:"-"`
	ConnectionParams     map[string]ConnectionParamDef         `yaml:"-"`
	MCP                  bool                                  `yaml:"-"`
	Discovery            *providermanifestv1.ProviderDiscovery `yaml:"-"`
	ResolvedAssetRoot    string                                `yaml:"-"`
	MCPToolPrefix        string                                `yaml:"-"`
	AppBinding           string                                `yaml:"-"`
}

type ProviderSurfaceOverrides struct {
	REST    *ProviderRESTSurfaceOverride    `yaml:"rest,omitempty"`
	OpenAPI *ProviderOpenAPISurfaceOverride `yaml:"openapi,omitempty"`
	GraphQL *ProviderGraphQLSurfaceOverride `yaml:"graphql,omitempty"`
	MCP     *ProviderMCPSurfaceOverride     `yaml:"mcp,omitempty"`
}

type PluginIndexedDBBinding struct {
	Name         string   `yaml:"name,omitempty"`
	ObjectStores []string `yaml:"objectStore,omitempty"`
}

func (b *PluginIndexedDBBinding) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		b.Name = strings.TrimSpace(value.Value)
		return nil
	}
	type raw PluginIndexedDBBinding
	return value.Decode((*raw)(b))
}

type ProviderRESTSurfaceOverride struct {
	BaseURL string `yaml:"baseUrl,omitempty"`
}

type ProviderOpenAPISurfaceOverride struct {
	BaseURL string `yaml:"baseUrl,omitempty"`
}

type ProviderGraphQLSurfaceOverride struct {
	URL string `yaml:"url,omitempty"`
}

type ProviderMCPSurfaceOverride struct {
	URL string `yaml:"url,omitempty"`
}

// UIEntry configures a mounted web UI bundle served under a fixed path prefix.
type UIEntry struct {
	ProviderEntry `yaml:",inline"`
	Path          string `yaml:"path,omitempty"`
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

func (e *ProviderEntry) ManifestSpec() *providermanifestv1.Spec {
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

type AuthorizationConfig struct {
	Workloads map[string]WorkloadDef    `yaml:"workloads,omitempty"`
	Policies  map[string]HumanPolicyDef `yaml:"policies,omitempty"`
}

type HumanPolicyDef struct {
	Default string                 `yaml:"default,omitempty"`
	Members []HumanPolicyMemberDef `yaml:"members,omitempty"`
}

type HumanPolicyMemberDef struct {
	SubjectID string `yaml:"subjectID,omitempty"`
	Email     string `yaml:"email,omitempty"`
	Role      string `yaml:"role"`
}

type WorkloadDef struct {
	DisplayName string                         `yaml:"displayName,omitempty"`
	Token       string                         `yaml:"token"`
	Providers   map[string]WorkloadProviderDef `yaml:"providers,omitempty"`
}

type WorkloadProviderDef struct {
	Connection string   `yaml:"connection,omitempty"`
	Instance   string   `yaml:"instance,omitempty"`
	Allow      []string `yaml:"allow,omitempty"`
}

type ListenerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type ServerConfig struct {
	Public        ListenerConfig        `yaml:"public"`
	Management    ListenerConfig        `yaml:"management"`
	BaseURL       string                `yaml:"baseUrl"`
	EncryptionKey string                `yaml:"encryptionKey"`
	APITokenTTL   string                `yaml:"apiTokenTtl"`
	ArtifactsDir  string                `yaml:"artifactsDir"`
	Providers     ServerProvidersConfig `yaml:"providers,omitempty"`
	IndexedDB     string                `yaml:"-"`
	Egress        EgressConfig          `yaml:"egress,omitempty"`
	Authorization AuthorizationConfig   `yaml:"authorization,omitempty"`
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
	DisplayName      string                                `yaml:"displayName,omitempty"`
	Mode             providermanifestv1.ConnectionMode     `yaml:"mode"`
	Auth             ConnectionAuthDef                     `yaml:"auth"`
	ConnectionParams map[string]ConnectionParamDef         `yaml:"params"`
	Discovery        *providermanifestv1.ProviderDiscovery `yaml:"-"`
}

type ConnectionAuthDef struct {
	Type                providermanifestv1.AuthType `yaml:"type"`
	AuthorizationURL    string                      `yaml:"authorizationUrl"`
	TokenURL            string                      `yaml:"tokenUrl"`
	ClientID            string                      `yaml:"clientId"`
	ClientSecret        string                      `yaml:"clientSecret"`
	RedirectURL         string                      `yaml:"redirectUrl"`
	ClientAuth          string                      `yaml:"clientAuth"`
	TokenExchange       string                      `yaml:"tokenExchange"`
	Scopes              []string                    `yaml:"scopes"`
	ScopeParam          string                      `yaml:"scopeParam"`
	ScopeSeparator      string                      `yaml:"scopeSeparator"`
	PKCE                bool                        `yaml:"pkce"`
	AuthorizationParams map[string]string           `yaml:"authorizationParams"`
	TokenParams         map[string]string           `yaml:"tokenParams"`
	RefreshParams       map[string]string           `yaml:"refreshParams"`
	AcceptHeader        string                      `yaml:"acceptHeader"`
	AccessTokenPath     string                      `yaml:"accessTokenPath"`
	TokenMetadata       []string                    `yaml:"tokenMetadata"`
	Credentials         []CredentialFieldDef        `yaml:"credentials"`
	AuthMapping         *AuthMappingDef             `yaml:"authMapping"`
}

type CredentialFieldDef = providermanifestv1.CredentialField
type AuthMappingDef = providermanifestv1.AuthMapping
type BasicAuthMappingDef = providermanifestv1.BasicAuthMapping
type AuthValueDef = providermanifestv1.AuthValue
type AuthValueFromDef = providermanifestv1.AuthValueFrom
type CredentialFieldRefDef = providermanifestv1.CredentialFieldRef

type ConnectionParamDef = providermanifestv1.ProviderConnectionParam

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

func ManifestAuthToConnectionAuthDef(auth *providermanifestv1.ProviderAuth) ConnectionAuthDef {
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

func EffectivePluginConnectionDef(plugin *ProviderEntry, manifestPlugin *providermanifestv1.Spec) ConnectionDef {
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

func EffectiveNamedConnectionDef(plugin *ProviderEntry, manifestPlugin *providermanifestv1.Spec, name string) (ConnectionDef, bool) {
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
type OperationOverride = providermanifestv1.ManifestOperationOverride

func Load(path string) (*Config, error) {
	return loadWithLookup(path, os.LookupEnv, false)
}

func LoadWithLookup(path string, lookup func(string) (string, bool)) (*Config, error) {
	return loadWithLookup(path, lookup, false)
}

func LoadAllowMissingEnv(path string) (*Config, error) {
	return loadWithLookup(path, os.LookupEnv, true)
}

func NormalizeCompatibility(cfg *Config) error {
	if err := normalizeAuthorizationConfig(cfg); err != nil {
		return err
	}
	return applyAppBindings(cfg)
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
	if err := normalizeConfigRoot(&root); err != nil {
		return err
	}
	doc := documentValueNode(&root)
	providersNode := mappingValueNode(documentValueNode(&root), "providers")
	pluginsNode := mappingValueNode(doc, "plugins")
	for name, entry := range cfg.Plugins {
		if entry == nil || !entry.HasManagedSource() {
			continue
		}
		if err := overlayManagedEntryConfigNode(mappingValueNode(pluginsNode, name), entry, "plugin "+strconv.Quote(name)); err != nil {
			return err
		}
	}
	for _, collection := range []struct {
		kind    HostProviderKind
		entries map[string]*ProviderEntry
	}{
		{HostProviderKindAuth, cfg.Providers.Auth},
		{HostProviderKindSecrets, cfg.Providers.Secrets},
		{HostProviderKindTelemetry, cfg.Providers.Telemetry},
		{HostProviderKindAudit, cfg.Providers.Audit},
		{HostProviderKindIndexedDB, cfg.Providers.IndexedDB},
	} {
		kindNode := mappingValueNode(providersNode, string(collection.kind))
		for name, entry := range collection.entries {
			if entry == nil || !entry.HasManagedSource() {
				continue
			}
			subject := fmt.Sprintf("%s %q", collection.kind, name)
			if err := overlayManagedEntryConfigNode(mappingValueNode(kindNode, name), entry, subject); err != nil {
				return err
			}
		}
	}
	uiNode := mappingValueNode(providersNode, "ui")
	for name, entry := range cfg.Providers.UI {
		if entry == nil || !entry.HasManagedSource() {
			continue
		}
		if err := overlayManagedEntryConfigNode(mappingValueNode(uiNode, name), &entry.ProviderEntry, "ui "+strconv.Quote(name)); err != nil {
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

type configShapeUsage struct {
	legacy    bool
	canonical bool
}

var legacyProviderEntryKeys = map[string]struct{}{
	"source":            {},
	"config":            {},
	"default":           {},
	"disabled":          {},
	"env":               {},
	"allowedHosts":      {},
	"displayName":       {},
	"description":       {},
	"iconFile":          {},
	"connections":       {},
	"allowedOperations": {},
	"indexeddb":         {},
	"indexeddbs":        {},
	"surfaces":          {},
}

func normalizeConfigRoot(root *yaml.Node) error {
	doc := documentValueNode(root)
	if doc == nil || doc.Kind == 0 {
		return nil
	}
	if doc.Kind != yaml.MappingNode {
		return fmt.Errorf("parsing config YAML: expected mapping document")
	}

	usage := configShapeUsage{}
	providersNode := mappingValueNode(doc, "providers")
	if err := normalizePluginsNode(doc, providersNode, &usage); err != nil {
		return err
	}
	if err := normalizeProvidersNode(providersNode, &usage); err != nil {
		return err
	}
	if err := normalizeServerNode(mappingValueNode(doc, "server"), &usage); err != nil {
		return err
	}
	if err := normalizePluginBindings(mappingValueNode(doc, "plugins"), &usage); err != nil {
		return err
	}
	return nil
}

func normalizePluginsNode(root, providersNode *yaml.Node, usage *configShapeUsage) error {
	root = documentValueNode(root)
	providersNode = documentValueNode(providersNode)
	canonical := mappingValueNode(root, "plugins")
	legacy := mappingValueNode(providersNode, "plugins")
	if canonical != nil {
		usage.canonical = true
	}
	if legacy != nil {
		usage.legacy = true
	}
	if canonical != nil && legacy != nil {
		return fmt.Errorf("config validation: plugins must be configured using top-level plugins or providers.plugins, not both")
	}
	if canonical == nil && legacy != nil {
		setMappingValueNode(root, "plugins", legacy)
		removeMappingKey(providersNode, "plugins")
	}
	return nil
}

func normalizeProvidersNode(providersNode *yaml.Node, usage *configShapeUsage) error {
	providersNode = documentValueNode(providersNode)
	if providersNode == nil {
		return nil
	}
	legacyIndexedDB := mappingValueNode(providersNode, "indexeddbs")
	canonicalIndexedDB := mappingValueNode(providersNode, "indexeddb")
	if legacyIndexedDB != nil {
		usage.legacy = true
	}
	if canonicalIndexedDB != nil {
		usage.canonical = true
	}
	if legacyIndexedDB != nil && canonicalIndexedDB != nil {
		return fmt.Errorf("config validation: providers.indexeddb must be configured using indexeddb or indexeddbs, not both")
	}
	if canonicalIndexedDB == nil && legacyIndexedDB != nil {
		setMappingValueNode(providersNode, "indexeddb", legacyIndexedDB)
		removeMappingKey(providersNode, "indexeddbs")
	}

	for _, kind := range []string{"auth", "secrets", "telemetry", "audit"} {
		node := mappingValueNode(providersNode, kind)
		if node == nil {
			continue
		}
		if isLegacySingletonProviderNode(node) {
			usage.legacy = true
			setMappingValueNode(providersNode, kind, wrapLegacyProviderNode(kind, node))
			continue
		}
		usage.canonical = true
	}
	return nil
}

func normalizeServerNode(serverNode *yaml.Node, usage *configShapeUsage) error {
	serverNode = documentValueNode(serverNode)
	if serverNode == nil {
		return nil
	}
	providersNode := mappingValueNode(serverNode, "providers")
	legacyIndexedDB := mappingValueNode(serverNode, "indexeddb")
	if providersNode != nil {
		usage.canonical = true
	}
	if legacyIndexedDB != nil {
		usage.legacy = true
	}
	if providersNode != nil && legacyIndexedDB != nil && mappingValueNode(providersNode, "indexeddb") != nil {
		return fmt.Errorf("config validation: server.providers.indexeddb and server.indexeddb cannot both be set")
	}
	if legacyIndexedDB != nil {
		if providersNode == nil {
			providersNode = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			setMappingValueNode(serverNode, "providers", providersNode)
		}
		if mappingValueNode(providersNode, "indexeddb") == nil {
			setMappingValueNode(providersNode, "indexeddb", legacyIndexedDB)
		}
		removeMappingKey(serverNode, "indexeddb")
	}
	return nil
}

func normalizePluginBindings(pluginsNode *yaml.Node, usage *configShapeUsage) error {
	pluginsNode = documentValueNode(pluginsNode)
	if pluginsNode == nil || pluginsNode.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(pluginsNode.Content); i += 2 {
		entryNode := documentValueNode(pluginsNode.Content[i+1])
		if entryNode == nil || entryNode.Kind != yaml.MappingNode {
			continue
		}
		legacy := mappingValueNode(entryNode, "indexeddbs")
		canonical := mappingValueNode(entryNode, "indexeddb")
		if legacy != nil {
			usage.legacy = true
		}
		if canonical != nil {
			usage.canonical = true
		}
		if legacy != nil && canonical != nil {
			return fmt.Errorf("config validation: plugin %q cannot set both indexeddb and indexeddbs", pluginsNode.Content[i].Value)
		}
		if canonical == nil && legacy != nil {
			setMappingValueNode(entryNode, "indexeddb", legacy)
			removeMappingKey(entryNode, "indexeddbs")
		}
	}
	return nil
}

func isLegacySingletonProviderNode(node *yaml.Node) bool {
	node = documentValueNode(node)
	if node == nil || node.Kind != yaml.MappingNode || len(node.Content) == 0 {
		return false
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i].Value
		if _, ok := legacyProviderEntryKeys[key]; !ok {
			return false
		}
		if !looksLikeProviderEntryMap(node.Content[i+1]) {
			return true
		}
	}
	return false
}

func looksLikeProviderEntryMap(node *yaml.Node) bool {
	node = documentValueNode(node)
	if node == nil || node.Kind != yaml.MappingNode {
		return false
	}
	if len(node.Content) == 0 {
		return true
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if _, ok := legacyProviderEntryKeys[node.Content[i].Value]; ok {
			return true
		}
	}
	return false
}

func wrapLegacyProviderNode(name string, node *yaml.Node) *yaml.Node {
	return &yaml.Node{
		Kind: yaml.MappingNode,
		Tag:  "!!map",
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: name},
			node,
		},
	}
}

func setMappingValueNode(node *yaml.Node, key string, value *yaml.Node) {
	node = documentValueNode(node)
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			node.Content[i+1] = value
			return
		}
	}
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		value,
	)
}

func removeMappingKey(node *yaml.Node, key string) {
	node = documentValueNode(node)
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			node.Content = append(node.Content[:i], node.Content[i+2:]...)
			return
		}
	}
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

	var root yaml.Node
	normalizeDecoder := yaml.NewDecoder(strings.NewReader(resolved))
	if err := normalizeDecoder.Decode(&root); err != nil && err != io.EOF {
		return nil, fmt.Errorf("parsing config YAML: %w", err)
	}
	if err := normalizeConfigRoot(&root); err != nil {
		return nil, err
	}
	normalized, err := yaml.Marshal(documentValueNode(&root))
	if err != nil {
		return nil, fmt.Errorf("marshaling normalized config YAML: %w", err)
	}

	var cfg Config
	dec := yaml.NewDecoder(strings.NewReader(string(normalized)))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil && err != io.EOF {
		return nil, fmt.Errorf("parsing config YAML: %w", err)
	}

	applyDefaults(&cfg)
	if err := NormalizeCompatibility(&cfg); err != nil {
		return nil, err
	}
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
	cfg.SyncCompatFields()
	if cfg.Server.Public.Port == 0 {
		cfg.Server.Public.Port = 8080
	}
	cfg.Plugins = nonNilProviderEntryMap(cfg.Plugins)
	cfg.Providers.UI = nonNilUIEntryMap(cfg.Providers.UI)
	cfg.Providers.Auth = nonNilProviderEntryMap(cfg.Providers.Auth)
	cfg.Providers.Secrets = applyDefaultBuiltinProviderEntries(cfg.Providers.Secrets, DefaultProviderInstance, "env")
	cfg.Providers.Telemetry = applyDefaultBuiltinProviderEntries(cfg.Providers.Telemetry, DefaultProviderInstance, "stdout")
	cfg.Providers.Audit = applyDefaultBuiltinProviderEntries(cfg.Providers.Audit, DefaultProviderInstance, "inherit")
	cfg.Providers.IndexedDB = nonNilProviderEntryMap(cfg.Providers.IndexedDB)
	cfg.Providers.Plugins = cfg.Plugins
	cfg.Providers.IndexedDBs = cfg.Providers.IndexedDB
	cfg.Server.IndexedDB = cfg.Server.Providers.IndexedDB
	cfg.SyncCompatFields()
}

func nonNilProviderEntryMap(entries map[string]*ProviderEntry) map[string]*ProviderEntry {
	if entries == nil {
		return make(map[string]*ProviderEntry)
	}
	return entries
}

func nonNilUIEntryMap(entries map[string]*UIEntry) map[string]*UIEntry {
	if entries == nil {
		return make(map[string]*UIEntry)
	}
	return entries
}

func applyDefaultBuiltinProviderEntries(entries map[string]*ProviderEntry, defaultName, builtin string) map[string]*ProviderEntry {
	if len(entries) == 0 {
		return map[string]*ProviderEntry{
			defaultName: {
				Source:  ProviderSource{Builtin: builtin},
				Default: true,
			},
		}
	}
	for _, entry := range entries {
		if entry == nil || entry.Disabled || entry.Source.IsBuiltin() || entry.Source.IsManaged() || entry.Source.IsLocal() {
			continue
		}
		entry.Source.Builtin = builtin
	}
	return entries
}

func normalizeAuthorizationConfig(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	cfg.Authorization = normalizedAuthorizationConfig(cfg.Authorization)
	cfg.Server.Authorization = normalizedAuthorizationConfig(cfg.Server.Authorization)
	topLevelSet := hasAuthorizationConfig(cfg.Authorization)
	legacySet := hasAuthorizationConfig(cfg.Server.Authorization)
	switch {
	case topLevelSet && legacySet:
		if !reflect.DeepEqual(cfg.Authorization, cfg.Server.Authorization) {
			return fmt.Errorf("config validation: authorization and server.authorization may not both be set with different values")
		}
	case topLevelSet:
		cfg.Server.Authorization = cfg.Authorization
	case legacySet:
		cfg.Authorization = cfg.Server.Authorization
	default:
		cfg.Authorization = AuthorizationConfig{}
		cfg.Server.Authorization = AuthorizationConfig{}
	}
	return nil
}

func hasAuthorizationConfig(cfg AuthorizationConfig) bool {
	return len(cfg.Policies) > 0 || len(cfg.Workloads) > 0
}

func normalizedAuthorizationConfig(cfg AuthorizationConfig) AuthorizationConfig {
	if len(cfg.Policies) == 0 {
		cfg.Policies = nil
	} else {
		policies := make(map[string]HumanPolicyDef, len(cfg.Policies))
		for name, policy := range cfg.Policies {
			policies[name] = normalizedHumanPolicyDef(policy)
		}
		cfg.Policies = policies
	}
	if len(cfg.Workloads) == 0 {
		cfg.Workloads = nil
	} else {
		workloads := make(map[string]WorkloadDef, len(cfg.Workloads))
		for name, workload := range cfg.Workloads {
			workloads[name] = normalizedWorkloadDef(workload)
		}
		cfg.Workloads = workloads
	}
	return cfg
}

func normalizedHumanPolicyDef(policy HumanPolicyDef) HumanPolicyDef {
	if len(policy.Members) == 0 {
		policy.Members = nil
	}
	return policy
}

func normalizedWorkloadDef(workload WorkloadDef) WorkloadDef {
	if len(workload.Providers) == 0 {
		workload.Providers = nil
		return workload
	}
	providers := make(map[string]WorkloadProviderDef, len(workload.Providers))
	for name, provider := range workload.Providers {
		if len(provider.Allow) == 0 {
			provider.Allow = nil
		}
		providers[name] = provider
	}
	workload.Providers = providers
	return workload
}

func applyAppBindings(cfg *Config) error {
	if cfg == nil || len(cfg.Apps) == 0 {
		return nil
	}

	appNames := mapsKeys(cfg.Apps)
	slices.Sort(appNames)
	seenPlugins := make(map[string]string, len(appNames))
	seenUIs := make(map[string]string, len(appNames))
	for _, appName := range appNames {
		app := cfg.Apps[appName]
		if app == nil {
			return fmt.Errorf("config validation: apps.%s is required", appName)
		}
		if app.Disabled {
			continue
		}
		if strings.TrimSpace(appName) == "" {
			return fmt.Errorf("config validation: apps keys must be non-empty")
		}
		app.Plugin = strings.TrimSpace(app.Plugin)
		app.UI = strings.TrimSpace(app.UI)
		app.Path = strings.TrimSpace(app.Path)
		app.AuthorizationPolicy = strings.TrimSpace(app.AuthorizationPolicy)

		if app.Plugin == "" {
			return fmt.Errorf("config validation: apps.%s.plugin is required", appName)
		}
		if app.UI == "" {
			return fmt.Errorf("config validation: apps.%s.ui is required", appName)
		}
		if app.Path == "" {
			return fmt.Errorf("config validation: apps.%s.path is required", appName)
		}
		normalizedPath, err := normalizeMountedUIPath(app.Path)
		if err != nil {
			return fmt.Errorf("config validation: apps.%s.path: %w", appName, err)
		}
		app.Path = normalizedPath
		if err := validateAuthorizationPolicyReference(cfg, "app", appName, app.AuthorizationPolicy); err != nil {
			return err
		}
		if prev, exists := seenPlugins[app.Plugin]; exists && prev != appName {
			return fmt.Errorf("config validation: apps.%s.plugin %q duplicates apps.%s", appName, app.Plugin, prev)
		}
		if prev, exists := seenUIs[app.UI]; exists && prev != appName {
			return fmt.Errorf("config validation: apps.%s.ui %q duplicates apps.%s", appName, app.UI, prev)
		}

		plugin := cfg.Plugins[app.Plugin]
		if plugin == nil {
			return fmt.Errorf("config validation: apps.%s.plugin references unknown plugin %q", appName, app.Plugin)
		}
		if plugin.Disabled {
			return fmt.Errorf("config validation: apps.%s.plugin references disabled plugin %q", appName, app.Plugin)
		}
		if current := strings.TrimSpace(plugin.AuthorizationPolicy); current != "" && current != app.AuthorizationPolicy {
			return fmt.Errorf("config validation: apps.%s.plugin %q conflicts with plugins.%s.authorizationPolicy", appName, app.Plugin, app.Plugin)
		}
		plugin.AuthorizationPolicy = app.AuthorizationPolicy
		plugin.AppBinding = appName
		seenPlugins[app.Plugin] = appName
		ui := cfg.Providers.UI[app.UI]
		if ui == nil {
			return fmt.Errorf("config validation: apps.%s.ui references unknown ui %q", appName, app.UI)
		}
		if ui.Disabled {
			return fmt.Errorf("config validation: apps.%s.ui references disabled ui %q", appName, app.UI)
		}
		if current := strings.TrimSpace(ui.AuthorizationPolicy); current != "" && current != app.AuthorizationPolicy {
			return fmt.Errorf("config validation: apps.%s.ui %q conflicts with providers.ui.%s.authorizationPolicy", appName, app.UI, app.UI)
		}
		if current := strings.TrimSpace(ui.Path); current != "" && current != app.Path {
			return fmt.Errorf("config validation: apps.%s.ui %q conflicts with providers.ui.%s.path", appName, app.UI, app.UI)
		}

		plugin.AuthorizationPolicy = app.AuthorizationPolicy
		ui.AuthorizationPolicy = app.AuthorizationPolicy
		ui.Path = app.Path
		seenPlugins[app.Plugin] = appName
		seenUIs[app.UI] = appName
	}

	return nil
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

	for _, entry := range cfg.Providers.Auth {
		resolveEntry(entry)
	}
	for _, entry := range cfg.Providers.Secrets {
		resolveEntry(entry)
	}
	for _, entry := range cfg.Providers.Telemetry {
		resolveEntry(entry)
	}
	for _, entry := range cfg.Providers.Audit {
		resolveEntry(entry)
	}
	for _, entry := range cfg.Providers.UI {
		if entry != nil {
			resolveEntry(&entry.ProviderEntry)
		}
	}
	for _, entry := range cfg.Providers.IndexedDB {
		resolveEntry(entry)
	}
	for _, entry := range cfg.Plugins {
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
