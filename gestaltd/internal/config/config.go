package config

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"maps"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/providerpkg"
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
	DefaultIndexedDBVersion  = "0.0.1-alpha.1"
	DefaultWebUIProvider     = DefaultProviderRepo + "/web/default"
	DefaultWebUIVersion      = "0.0.1-alpha.8"
	DefaultProviderInstance  = "default"
)

// PluginConnectionName is the implicit connection name used when storing
// tokens for plugin-only integrations that do not declare YAML connections.
const PluginConnectionName = "_plugin"

// PluginConnectionAlias is the user-facing alias that maps to
// PluginConnectionName. In hybrid integrations, mcp.connection can be set
// to "plugin" to reuse the plugin's OAuth token.
const PluginConnectionAlias = "plugin"
const APIVersionV3 = "gestaltd.config/v3"
const APIVersionV4 = "gestaltd.config/v4"

type providerSourceSyntaxMode int

const (
	providerSourceSyntaxLegacy providerSourceSyntaxMode = iota
	providerSourceSyntaxV3
	providerSourceSyntaxV4
)

type Config struct {
	APIVersion    string                    `yaml:"apiVersion,omitempty"`
	Server        ServerConfig              `yaml:"server"`
	Authorization AuthorizationConfig       `yaml:"authorization,omitempty"`
	Providers     ProvidersConfig           `yaml:"providers"`
	Plugins       map[string]*ProviderEntry `yaml:"plugins,omitempty"`
}

type ProvidersConfig struct {
	Auth          map[string]*ProviderEntry `yaml:"auth,omitempty"`
	Authorization map[string]*ProviderEntry `yaml:"authorization,omitempty"`
	Secrets       map[string]*ProviderEntry `yaml:"secrets,omitempty"`
	Telemetry     map[string]*ProviderEntry `yaml:"telemetry,omitempty"`
	Audit         map[string]*ProviderEntry `yaml:"audit,omitempty"`
	UI            map[string]*UIEntry       `yaml:"ui,omitempty"`
	IndexedDB     map[string]*ProviderEntry `yaml:"indexeddb,omitempty"`
	Cache         map[string]*ProviderEntry `yaml:"cache,omitempty"`
	S3            map[string]*ProviderEntry `yaml:"s3,omitempty"`
	Workflow      map[string]*ProviderEntry `yaml:"workflow,omitempty"`
}

type HostProviderKind string

const (
	HostProviderKindAuth          HostProviderKind = "auth"
	HostProviderKindAuthorization HostProviderKind = "authorization"
	HostProviderKindSecrets       HostProviderKind = "secrets"
	HostProviderKindTelemetry     HostProviderKind = "telemetry"
	HostProviderKindAudit         HostProviderKind = "audit"
	HostProviderKindIndexedDB     HostProviderKind = "indexeddb"
	HostProviderKindCache         HostProviderKind = "cache"
	HostProviderKindWorkflow      HostProviderKind = "workflow"
)

type ServerProvidersConfig struct {
	Auth          string `yaml:"auth,omitempty"`
	Authorization string `yaml:"authorization,omitempty"`
	Secrets       string `yaml:"secrets,omitempty"`
	Telemetry     string `yaml:"telemetry,omitempty"`
	Audit         string `yaml:"audit,omitempty"`
	IndexedDB     string `yaml:"indexeddb,omitempty"`
}

// ProviderSource supports handwritten config in three forms via custom
// UnmarshalYAML:
//   - Builtin:  source: "name"                               -> ProviderSource{Builtin: "name"}
//   - Metadata: source: "https://.../provider-release.yaml"  -> ProviderSource{metadataURL: "..."}
//   - Local:    source: {path} or source: "./manifest.yaml"  -> ProviderSource{Path: "..."} in v3
//   - Local:    source: {path} or source: "./dist/provider-release.yaml"
//     -> ProviderSource{metadataPath: "..."} in v4
type ProviderSource struct {
	Builtin      string         `yaml:"-"`
	scalar       string         `yaml:"-"`
	metadataURL  string         `yaml:"-"`
	metadataPath string         `yaml:"-"`
	unsupported  string         `yaml:"-"`
	Path         string         `yaml:"path,omitempty"`
	Auth         *SourceAuthDef `yaml:"auth,omitempty"`
}

func (s *ProviderSource) UnmarshalYAML(value *yaml.Node) error {
	*s = ProviderSource{}
	if value.Kind == yaml.ScalarNode {
		scalar := strings.TrimSpace(value.Value)
		switch {
		case looksLikeMetadataURL(scalar):
			s.metadataURL = scalar
		case looksLikeUnsupportedScalarSource(scalar):
			s.unsupported = scalar
		default:
			s.scalar = scalar
		}
		return nil
	}
	if value.Kind == yaml.MappingNode {
		hasRef := false
		hasVersion := false
		hasAuth := false
		for i := 0; i+1 < len(value.Content); i += 2 {
			key := strings.TrimSpace(value.Content[i].Value)
			switch key {
			case "ref":
				hasRef = true
			case "version":
				hasVersion = true
			case "auth":
				hasAuth = true
			}
		}
		if hasRef {
			return fmt.Errorf("source.ref/source.version are no longer supported; use source: <provider-release.yaml URL>")
		}
		if hasVersion {
			return fmt.Errorf("source.version is no longer supported; use source: <provider-release.yaml URL>")
		}
		if hasAuth {
			return fmt.Errorf("source.auth is no longer supported; use sibling auth alongside source")
		}
	}
	type raw ProviderSource
	return value.Decode((*raw)(s))
}

func (s ProviderSource) MarshalYAML() (any, error) {
	if s.Builtin != "" {
		return s.Builtin, nil
	}
	if s.metadataURL != "" && s.Path == "" && s.metadataPath == "" {
		return s.metadataURL, nil
	}
	if s.scalar != "" && s.Path == "" && s.metadataPath == "" {
		return s.scalar, nil
	}
	if s.metadataPath != "" && s.Path == "" {
		type raw struct {
			Path string `yaml:"path,omitempty"`
		}
		return raw{Path: s.metadataPath}, nil
	}
	type raw ProviderSource
	return raw(s), nil
}

func (s ProviderSource) IsBuiltin() bool     { return s.Builtin != "" }
func (s ProviderSource) IsMetadataURL() bool { return s.metadataURL != "" }
func (s ProviderSource) IsLocal() bool       { return s.Path != "" }
func (s ProviderSource) IsLocalMetadataPath() bool {
	return s.metadataPath != ""
}
func (s ProviderSource) MetadataURL() string  { return s.metadataURL }
func (s ProviderSource) MetadataPath() string { return s.metadataPath }
func (s ProviderSource) UnsupportedURL() string {
	return s.unsupported
}

func NewMetadataSource(rawURL string) ProviderSource {
	return ProviderSource{metadataURL: strings.TrimSpace(rawURL)}
}

func NewLocalReleaseMetadataSource(rawPath string) ProviderSource {
	return ProviderSource{metadataPath: strings.TrimSpace(rawPath)}
}

// ProviderEntry is the universal configuration for any provider.
type ProviderEntry struct {
	Source           ProviderSource    `yaml:"source"`
	Config           yaml.Node         `yaml:"config,omitempty"`
	Default          bool              `yaml:"default,omitempty"`
	Env              map[string]string `yaml:"env,omitempty"`
	AllowedHosts     []string          `yaml:"allowedHosts,omitempty"`
	DisplayName      string            `yaml:"displayName,omitempty"`
	Description      string            `yaml:"description,omitempty"`
	IconFile         string            `yaml:"iconFile,omitempty"`
	InlineSourceAuth *SourceAuthDef    `yaml:"auth,omitempty"`
	// AuthorizationPolicy binds this provider to a shared human access policy.
	AuthorizationPolicy string `yaml:"authorizationPolicy,omitempty"`

	// Plugin-specific config fields (parsed from YAML, only valid on plugin entries)
	MountPath         string                        `yaml:"mountPath,omitempty"`
	UI                string                        `yaml:"ui,omitempty"`
	Connections       map[string]*ConnectionDef     `yaml:"connections,omitempty"`
	AllowedOperations map[string]*OperationOverride `yaml:"allowedOperations,omitempty"`
	Invokes           []PluginInvocationDependency  `yaml:"invokes,omitempty"`
	IndexedDB         *PluginIndexedDBConfig        `yaml:"indexeddb,omitempty"`
	Cache             []string                      `yaml:"cache,omitempty"`
	S3                []string                      `yaml:"s3,omitempty"`
	Workflow          *PluginWorkflowConfig         `yaml:"workflow,omitempty"`
	Surfaces          *ProviderSurfaceOverrides     `yaml:"surfaces,omitempty"`
	MCP               bool                          `yaml:"mcp,omitempty"`

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
	Discovery            *providermanifestv1.ProviderDiscovery `yaml:"-"`
	ResolvedAssetRoot    string                                `yaml:"-"`
	MCPToolPrefix        string                                `yaml:"-"`
}

func (e ProviderEntry) MarshalYAML() (any, error) {
	type raw ProviderEntry
	if e.HasReleaseMetadataSource() && e.Source.Auth != nil && e.InlineSourceAuth == nil {
		auth := *e.Source.Auth
		e.InlineSourceAuth = &auth
		e.Source.Auth = nil
	}
	return raw(e), nil
}

type ProviderSurfaceOverrides struct {
	REST    *ProviderRESTSurfaceOverride    `yaml:"rest,omitempty"`
	OpenAPI *ProviderOpenAPISurfaceOverride `yaml:"openapi,omitempty"`
	GraphQL *ProviderGraphQLSurfaceOverride `yaml:"graphql,omitempty"`
	MCP     *ProviderMCPSurfaceOverride     `yaml:"mcp,omitempty"`
}

type PluginIndexedDBConfig struct {
	Provider     string   `yaml:"provider,omitempty"`
	DB           string   `yaml:"db,omitempty"`
	ObjectStores []string `yaml:"objectStores,omitempty"`
}

type PluginWorkflowConfig struct {
	Provider      string                                `yaml:"provider,omitempty"`
	Operations    []string                              `yaml:"operations,omitempty"`
	Schedules     map[string]PluginWorkflowSchedule     `yaml:"schedules,omitempty"`
	EventTriggers map[string]PluginWorkflowEventTrigger `yaml:"eventTriggers,omitempty"`
}

type PluginWorkflowSchedule struct {
	Cron      string         `yaml:"cron,omitempty"`
	Timezone  string         `yaml:"timezone,omitempty"`
	Operation string         `yaml:"operation,omitempty"`
	Input     map[string]any `yaml:"input,omitempty"`
	Paused    bool           `yaml:"paused,omitempty"`
}

type PluginWorkflowEventTrigger struct {
	Match     PluginWorkflowEventMatch `yaml:"match,omitempty"`
	Operation string                   `yaml:"operation,omitempty"`
	Input     map[string]any           `yaml:"input,omitempty"`
	Paused    bool                     `yaml:"paused,omitempty"`
}

type PluginWorkflowEventMatch struct {
	Type    string `yaml:"type,omitempty"`
	Source  string `yaml:"source,omitempty"`
	Subject string `yaml:"subject,omitempty"`
}

func (c *PluginIndexedDBConfig) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		c.Provider = strings.TrimSpace(value.Value)
		return nil
	case yaml.SequenceNode:
		return fmt.Errorf("plugin indexeddb must be a mapping or scalar provider name")
	default:
		for i := 0; i+1 < len(value.Content); i += 2 {
			if key := value.Content[i]; key != nil && key.Value == "disabled" {
				return fmt.Errorf("field disabled not found in type config.raw")
			}
		}
		type raw PluginIndexedDBConfig
		return value.Decode((*raw)(c))
	}
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
	OwnerPlugin   string `yaml:"-"`
}

func (e *ProviderEntry) HasMetadataSource() bool {
	return e != nil && e.Source.IsMetadataURL()
}

func (e *ProviderEntry) HasReleaseMetadataSource() bool {
	return e != nil && (e.Source.IsMetadataURL() || e.Source.IsLocalMetadataPath())
}

func (e *ProviderEntry) HasRemoteSource() bool {
	return e != nil && e.Source.IsMetadataURL()
}

func (e *ProviderEntry) HasRemoteReleaseSource() bool {
	return e != nil && e.Source.IsMetadataURL()
}

func (e *ProviderEntry) HasLocalSource() bool {
	return e != nil && e.Source.IsLocal()
}

func (e *ProviderEntry) HasLocalReleaseSource() bool {
	return e != nil && e.Source.IsLocalMetadataPath()
}

func (e *ProviderEntry) SourceMetadataURL() string {
	if e == nil {
		return ""
	}
	return e.Source.MetadataURL()
}

func (e *ProviderEntry) SourceRemoteLocation() string {
	if e == nil {
		return ""
	}
	if e.Source.IsMetadataURL() {
		return e.Source.MetadataURL()
	}
	return ""
}

func (e *ProviderEntry) SourceReleaseLocation() string {
	if e == nil {
		return ""
	}
	if e.Source.IsMetadataURL() {
		return e.Source.MetadataURL()
	}
	if e.Source.IsLocalMetadataPath() {
		return e.Source.MetadataPath()
	}
	return ""
}

func (e *ProviderEntry) SourcePath() string {
	if e.Source.IsLocal() {
		return e.Source.Path
	}
	return ""
}

func (e *ProviderEntry) SourceReleasePath() string {
	if e.Source.IsLocalMetadataPath() {
		return e.Source.MetadataPath()
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

func (e *ProviderEntry) HasMCPSurface() bool {
	if e == nil {
		return false
	}
	if ProviderSurfaceURLOverride(e, SpecSurfaceMCP) != "" {
		return true
	}
	return ManifestProviderSurfaceURL(e.ManifestSpec(), SpecSurfaceMCP) != ""
}

func (e *ProviderEntry) ExposesMCP() bool {
	return e.DeclaresMCP() || e.HasMCPSurface()
}

func (e *ProviderEntry) IncludeRESTInMCP() bool {
	return e.DeclaresMCP()
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

type ManagementListenerConfig struct {
	ListenerConfig `yaml:",inline"`
	BaseURL        string `yaml:"baseUrl,omitempty"`
}

type AdminConfig struct {
	AuthorizationPolicy string   `yaml:"authorizationPolicy,omitempty"`
	AllowedRoles        []string `yaml:"allowedRoles,omitempty"`
}

type ServerConfig struct {
	Public        ListenerConfig           `yaml:"public"`
	Management    ManagementListenerConfig `yaml:"management"`
	BaseURL       string                   `yaml:"baseUrl"`
	EncryptionKey string                   `yaml:"encryptionKey"`
	APITokenTTL   string                   `yaml:"apiTokenTtl"`
	ArtifactsDir  string                   `yaml:"artifactsDir"`
	Providers     ServerProvidersConfig    `yaml:"providers,omitempty"`
	Egress        EgressConfig             `yaml:"egress,omitempty"`
	Admin         AdminConfig              `yaml:"admin,omitempty"`
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
	return s.Management.ListenerConfig, true
}

func (s ServerConfig) ManagementAddr() string {
	listener, ok := s.ManagementListener()
	if !ok {
		return ""
	}
	return net.JoinHostPort(listener.Host, strconv.Itoa(listener.Port))
}

func (s ServerConfig) ManagementBaseURL() string {
	return strings.TrimRight(strings.TrimSpace(s.Management.BaseURL), "/")
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

type PluginInvocationDependency struct {
	Plugin    string `yaml:"plugin,omitempty"`
	Operation string `yaml:"operation,omitempty"`
}

func Load(path string) (*Config, error) {
	return LoadPaths([]string{path})
}

func LoadPaths(paths []string) (*Config, error) {
	return loadWithLookupPaths(paths, os.LookupEnv, false)
}

func LoadWithLookup(path string, lookup func(string) (string, bool)) (*Config, error) {
	return LoadWithLookupPaths([]string{path}, lookup)
}

func LoadWithLookupPaths(paths []string, lookup func(string) (string, bool)) (*Config, error) {
	return loadWithLookupPaths(paths, lookup, false)
}

func LoadAllowMissingEnv(path string) (*Config, error) {
	return LoadAllowMissingEnvPaths([]string{path})
}

func LoadAllowMissingEnvPaths(paths []string) (*Config, error) {
	return loadWithLookupPaths(paths, os.LookupEnv, true)
}

func NormalizeCompatibility(cfg *Config) error {
	normalizeProviderSourceShapes(cfg)
	if err := normalizeAuthorizationConfig(cfg); err != nil {
		return err
	}
	if err := normalizeAdminConfig(cfg); err != nil {
		return err
	}
	return applyPluginMountBindings(cfg)
}

func OverlayRemotePluginConfig(path string, cfg *Config) error {
	return OverlayRemotePluginConfigPaths([]string{path}, cfg)
}

func OverlayRemotePluginConfigPaths(paths []string, cfg *Config) error {
	root, err := loadMergedConfigRoot(paths, os.LookupEnv, envMissingPreserve, "")
	if err != nil {
		return err
	}
	doc := documentValueNode(&root)
	providersNode := mappingValueNode(documentValueNode(&root), "providers")
	pluginsNode := mappingValueNode(doc, "plugins")
	for name, entry := range cfg.Plugins {
		if entry == nil || !entry.HasRemoteSource() {
			continue
		}
		if err := overlayRemoteEntryConfigNode(mappingValueNode(pluginsNode, name), entry, "plugin "+strconv.Quote(name)); err != nil {
			return err
		}
	}
	for _, collection := range []struct {
		kind    HostProviderKind
		entries map[string]*ProviderEntry
	}{
		{HostProviderKindAuth, cfg.Providers.Auth},
		{HostProviderKindAuthorization, cfg.Providers.Authorization},
		{HostProviderKindSecrets, cfg.Providers.Secrets},
		{HostProviderKindTelemetry, cfg.Providers.Telemetry},
		{HostProviderKindAudit, cfg.Providers.Audit},
		{HostProviderKindIndexedDB, cfg.Providers.IndexedDB},
		{HostProviderKindCache, cfg.Providers.Cache},
		{HostProviderKindWorkflow, cfg.Providers.Workflow},
	} {
		kindNode := mappingValueNode(providersNode, string(collection.kind))
		for name, entry := range collection.entries {
			if entry == nil || !entry.HasRemoteSource() {
				continue
			}
			subject := fmt.Sprintf("%s %q", collection.kind, name)
			if err := overlayRemoteEntryConfigNode(mappingValueNode(kindNode, name), entry, subject); err != nil {
				return err
			}
		}
	}
	s3Node := mappingValueNode(providersNode, "s3")
	for name, entry := range cfg.Providers.S3 {
		if entry == nil || !entry.HasRemoteSource() {
			continue
		}
		if err := overlayRemoteEntryConfigNode(mappingValueNode(s3Node, name), entry, "s3 "+strconv.Quote(name)); err != nil {
			return err
		}
	}
	uiNode := mappingValueNode(providersNode, "ui")
	for name, entry := range cfg.Providers.UI {
		if entry == nil || !entry.HasRemoteSource() {
			continue
		}
		if err := overlayRemoteEntryConfigNode(mappingValueNode(uiNode, name), &entry.ProviderEntry, "ui "+strconv.Quote(name)); err != nil {
			return err
		}
	}
	return nil
}

type envMissingMode int

const (
	envMissingBlank envMissingMode = iota
	envMissingSentinel
	envMissingPreserve
)

func overlayRemoteEntryConfigNode(raw *yaml.Node, entry *ProviderEntry, subject string) error {
	if entry == nil || !entry.HasRemoteSource() || raw == nil {
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
	allowSecretRefs := !strings.HasPrefix(subject, "secrets ")
	if err := NormalizeOpaqueSecretRefs(&node, allowSecretRefs); err != nil {
		return err
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

func normalizeConfigRoot(root *yaml.Node) error {
	doc := documentValueNode(root)
	if doc == nil || doc.Kind == 0 {
		return nil
	}
	if doc.Kind != yaml.MappingNode {
		return fmt.Errorf("parsing config YAML: expected mapping document")
	}
	return nil
}

func loadWithLookupPaths(paths []string, lookup func(string) (string, bool), allowMissing bool) (*Config, error) {
	mode := envMissingBlank
	missingEnvSentinelContext := ""
	if !allowMissing {
		mode = envMissingSentinel
		var err error
		missingEnvSentinelContext, err = newMissingEnvSentinelPrefix()
		if err != nil {
			return nil, err
		}
	}

	root, err := loadMergedConfigRoot(paths, lookup, mode, missingEnvSentinelContext)
	if err != nil {
		return nil, err
	}
	if err := rejectRemovedDisabledFields(&root); err != nil {
		return nil, err
	}
	if err := NormalizeConfigSecretRefs(&root); err != nil {
		return nil, err
	}
	if !allowMissing {
		firstMissing := firstMissingEnvSentinel(&root, missingEnvSentinelContext)
		if firstMissing != "" {
			return nil, fmt.Errorf("expanding config environment variables: environment variable %q not set; use ${%s:-} to allow an empty default", firstMissing, firstMissing)
		}
	}
	normalized, err := yaml.Marshal(documentValueNode(&root))
	if err != nil {
		return nil, fmt.Errorf("marshaling normalized config YAML: %w", err)
	}
	normalizedInput := string(normalized)
	if !allowMissing {
		normalizedInput = restoreMissingEnvSentinels(normalizedInput, missingEnvSentinelContext)
	}

	var cfg Config
	dec := yaml.NewDecoder(strings.NewReader(normalizedInput))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil && err != io.EOF {
		return nil, fmt.Errorf("parsing config YAML: %w", err)
	}

	if err := CanonicalizeStructure(&cfg); err != nil {
		return nil, err
	}
	applyDefaults(&cfg)
	resolveBaseURL(&cfg)
	resolveRelativePaths(primaryConfigPath(paths), &cfg)

	if err := ValidateCanonicalStructure(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func primaryConfigPath(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	return paths[0]
}

func loadMergedConfigRoot(paths []string, lookup func(string) (string, bool), mode envMissingMode, sentinelPrefix string) (yaml.Node, error) {
	if len(paths) == 0 {
		return yaml.Node{}, fmt.Errorf("reading config file: no config files provided")
	}

	roots := make([]yaml.Node, len(paths))
	sourceSyntax := providerSourceSyntaxLegacy
	for i, path := range paths {
		root, err := loadValidatedConfigRoot(path, lookup, mode, sentinelPrefix)
		if err != nil {
			return yaml.Node{}, err
		}
		rootSyntax, err := configRootSourceSyntax(root)
		if err != nil {
			return yaml.Node{}, err
		}
		if rootSyntax != providerSourceSyntaxLegacy {
			if sourceSyntax != providerSourceSyntaxLegacy && sourceSyntax != rootSyntax {
				return yaml.Node{}, fmt.Errorf("config validation: mixed apiVersion values are not supported across merged config files")
			}
			sourceSyntax = rootSyntax
		}
		roots[i] = root
	}

	var merged any
	for i, path := range paths {
		value, err := loadConfigValue(path, roots[i], sourceSyntax)
		if err != nil {
			return yaml.Node{}, err
		}
		merged = mergeConfigValues(merged, value)
	}

	if merged == nil {
		return yaml.Node{}, nil
	}

	data, err := yaml.Marshal(merged)
	if err != nil {
		return yaml.Node{}, fmt.Errorf("marshaling merged config YAML: %w", err)
	}

	var root yaml.Node
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	if err := dec.Decode(&root); err != nil && err != io.EOF {
		return yaml.Node{}, fmt.Errorf("parsing config YAML: %w", err)
	}
	if err := normalizeConfigRoot(&root); err != nil {
		return yaml.Node{}, err
	}
	return root, nil
}

func configRootSourceSyntax(root yaml.Node) (providerSourceSyntaxMode, error) {
	doc := documentValueNode(&root)
	if doc == nil || doc.Kind != yaml.MappingNode {
		return providerSourceSyntaxLegacy, nil
	}
	node := mappingValueNode(doc, "apiVersion")
	if node == nil {
		return providerSourceSyntaxLegacy, nil
	}
	value := strings.TrimSpace(node.Value)
	if value == "" {
		return providerSourceSyntaxLegacy, nil
	}
	mode, err := providerSourceSyntaxForAPIVersion(value)
	if err != nil {
		return providerSourceSyntaxLegacy, err
	}
	return mode, nil
}

func loadConfigValue(path string, root yaml.Node, sourceSyntax providerSourceSyntaxMode) (any, error) {
	doc := documentValueNode(&root)
	if doc == nil || doc.Kind == 0 {
		return nil, nil
	}

	var raw any
	if err := doc.Decode(&raw); err != nil {
		return nil, fmt.Errorf("parsing config YAML: %w", err)
	}

	normalized, err := normalizeConfigValue(raw)
	if err != nil {
		return nil, fmt.Errorf("parsing config YAML: %w", err)
	}
	if normalized != nil {
		root, ok := normalized.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("expected mapping document")
		}
		resolveRelativePathsInValue(path, root, sourceSyntax)
	}
	return normalized, nil
}

func loadValidatedConfigRoot(path string, lookup func(string) (string, bool), mode envMissingMode, sentinelPrefix string) (yaml.Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return yaml.Node{}, fmt.Errorf("reading config file: %w", err)
	}

	resolved, err := expandConfigInput(string(data), lookup, mode, sentinelPrefix)
	if err != nil {
		return yaml.Node{}, err
	}

	var root yaml.Node
	dec := yaml.NewDecoder(strings.NewReader(resolved))
	if err := dec.Decode(&root); err != nil && err != io.EOF {
		return yaml.Node{}, fmt.Errorf("parsing config YAML: %w", err)
	}
	if err := normalizeConfigRoot(&root); err != nil {
		return yaml.Node{}, err
	}
	if err := rejectRemovedDisabledFields(&root); err != nil {
		return yaml.Node{}, err
	}

	validationRoot, err := cloneConfigRoot(root)
	if err != nil {
		return yaml.Node{}, err
	}
	if err := NormalizeOpaqueSecretRefs(&validationRoot, true); err != nil {
		return yaml.Node{}, err
	}
	normalizedData, err := yaml.Marshal(documentValueNode(&validationRoot))
	if err != nil {
		return yaml.Node{}, fmt.Errorf("marshaling config YAML: %w", err)
	}
	normalizedInput := string(normalizedData)
	if mode == envMissingSentinel {
		normalizedInput = restoreMissingEnvSentinels(normalizedInput, sentinelPrefix)
	}
	validationInput := normalizedInput
	if mode == envMissingSentinel || mode == envMissingPreserve {
		validationInput, _, err = expandEnvVariables(normalizedInput, func(string) (string, bool) {
			return "", false
		}, false)
		if err != nil {
			return yaml.Node{}, err
		}
	}
	var cfg Config
	dec = yaml.NewDecoder(strings.NewReader(validationInput))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil && err != io.EOF {
		return yaml.Node{}, fmt.Errorf("parsing config YAML: %w", err)
	}

	return root, nil
}

func cloneConfigRoot(root yaml.Node) (yaml.Node, error) {
	data, err := yaml.Marshal(documentValueNode(&root))
	if err != nil {
		return yaml.Node{}, fmt.Errorf("marshaling config YAML: %w", err)
	}

	var clone yaml.Node
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	if err := dec.Decode(&clone); err != nil && err != io.EOF {
		return yaml.Node{}, fmt.Errorf("parsing config YAML: %w", err)
	}
	if err := normalizeConfigRoot(&clone); err != nil {
		return yaml.Node{}, err
	}
	return clone, nil
}

func expandConfigInput(input string, lookup func(string) (string, bool), mode envMissingMode, sentinelPrefix string) (string, error) {
	switch mode {
	case envMissingBlank:
		resolved, _, err := expandEnvVariables(input, lookup, false)
		return resolved, err
	case envMissingSentinel:
		return expandEnvVariablesWithMissingSentinels(input, lookup, sentinelPrefix)
	case envMissingPreserve:
		resolved, _, err := expandEnvVariables(input, lookup, true)
		return resolved, err
	default:
		return "", fmt.Errorf("unsupported env expansion mode %d", mode)
	}
}

func normalizeConfigValue(value any) (any, error) {
	switch current := value.(type) {
	case nil:
		return nil, nil
	case map[string]any:
		out := make(map[string]any, len(current))
		for key, child := range current {
			normalized, err := normalizeConfigValue(child)
			if err != nil {
				return nil, err
			}
			out[key] = normalized
		}
		return out, nil
	case map[any]any:
		out := make(map[string]any, len(current))
		for key, child := range current {
			keyString, ok := key.(string)
			if !ok {
				return nil, fmt.Errorf("expected string mapping key, got %T", key)
			}
			normalized, err := normalizeConfigValue(child)
			if err != nil {
				return nil, err
			}
			out[keyString] = normalized
		}
		return out, nil
	case []any:
		out := make([]any, len(current))
		for i, child := range current {
			normalized, err := normalizeConfigValue(child)
			if err != nil {
				return nil, err
			}
			out[i] = normalized
		}
		return out, nil
	default:
		return current, nil
	}
}

func mergeConfigValues(base, overlay any) any {
	if overlay == nil {
		return cloneConfigValue(base)
	}

	baseMap, baseIsMap := base.(map[string]any)
	overlayMap, overlayIsMap := overlay.(map[string]any)
	if baseIsMap && overlayIsMap {
		out := cloneConfigValue(baseMap).(map[string]any)
		for key, value := range overlayMap {
			if value == nil {
				delete(out, key)
				continue
			}
			out[key] = mergeConfigValues(out[key], value)
		}
		return out
	}

	return cloneConfigValue(overlay)
}

func cloneConfigValue(value any) any {
	switch current := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(current))
		for key, child := range current {
			out[key] = cloneConfigValue(child)
		}
		return out
	case []any:
		out := make([]any, len(current))
		for i, child := range current {
			out[i] = cloneConfigValue(child)
		}
		return out
	default:
		return current
	}
}

func rejectRemovedDisabledFields(root *yaml.Node) error {
	if err := rejectRemovedDisabledEntryFields(mappingValueNode(root, "plugins"), "plugins"); err != nil {
		return err
	}
	providersNode := mappingValueNode(root, "providers")
	for _, section := range []string{"auth", "secrets", "telemetry", "audit", "indexeddb", "cache", "s3", "ui"} {
		if err := rejectRemovedDisabledEntryFields(mappingValueNode(providersNode, section), "providers."+section); err != nil {
			return err
		}
	}
	return nil
}

func rejectRemovedDisabledEntryFields(node *yaml.Node, prefix string) error {
	node = documentValueNode(node)
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		entryNode := node.Content[i+1]
		if keyNode == nil || entryNode == nil {
			continue
		}
		if mappingValueNode(entryNode, "disabled") != nil {
			return fmt.Errorf("parsing config YAML: %s.%s.disabled: field disabled not found; omit the entry instead", prefix, keyNode.Value)
		}
		if prefix == "plugins" {
			if indexedDBNode := mappingValueNode(entryNode, "indexeddb"); indexedDBNode != nil && mappingValueNode(indexedDBNode, "disabled") != nil {
				return fmt.Errorf("parsing config YAML: %s.%s.indexeddb.disabled: field disabled not found; omit indexeddb to inherit the host provider", prefix, keyNode.Value)
			}
		}
	}
	return nil
}

const (
	missingEnvSentinelPrefix = "__GESTALT_MISSING_ENV_"
	missingEnvSentinelSuffix = "__"
)

func firstMissingEnvSentinel(node *yaml.Node, sentinelPrefix string) string {
	node = documentValueNode(node)
	if node == nil {
		return ""
	}
	if node.Kind == yaml.ScalarNode {
		return firstMissingEnvSentinelInString(node.Value, sentinelPrefix)
	}
	for _, child := range node.Content {
		if name := firstMissingEnvSentinel(child, sentinelPrefix); name != "" {
			return name
		}
	}
	return ""
}

func firstMissingEnvSentinelInString(value, sentinelPrefix string) string {
	if sentinelPrefix == "" {
		return ""
	}
	for start := 0; start < len(value); {
		idx := strings.Index(value[start:], sentinelPrefix)
		if idx < 0 {
			break
		}
		idx += start + len(sentinelPrefix)
		end := strings.Index(value[idx:], missingEnvSentinelSuffix)
		if end < 0 {
			break
		}
		decoded, err := base64.RawURLEncoding.DecodeString(value[idx : idx+end])
		if err == nil {
			return string(decoded)
		}
		start = idx + end + len(missingEnvSentinelSuffix)
	}
	ref, ok, err := ParseSecretRefTransport(value)
	if err != nil || !ok {
		return ""
	}
	if name := firstMissingEnvSentinelInString(ref.Provider, sentinelPrefix); name != "" {
		return name
	}
	return firstMissingEnvSentinelInString(ref.Name, sentinelPrefix)
}

func restoreMissingEnvSentinels(input, sentinelPrefix string) string {
	if sentinelPrefix == "" {
		return input
	}
	var b strings.Builder
	b.Grow(len(input))
	for start := 0; start < len(input); {
		idx := strings.Index(input[start:], sentinelPrefix)
		if idx < 0 {
			b.WriteString(input[start:])
			break
		}
		idx += start
		b.WriteString(input[start:idx])
		encodedStart := idx + len(sentinelPrefix)
		end := strings.Index(input[encodedStart:], missingEnvSentinelSuffix)
		if end < 0 {
			b.WriteString(input[idx:])
			break
		}
		encodedEnd := encodedStart + end
		decoded, err := base64.RawURLEncoding.DecodeString(input[encodedStart:encodedEnd])
		if err != nil {
			b.WriteString(input[idx : encodedEnd+len(missingEnvSentinelSuffix)])
			start = encodedEnd + len(missingEnvSentinelSuffix)
			continue
		}
		b.WriteString("${")
		b.Write(decoded)
		b.WriteString("}")
		start = encodedEnd + len(missingEnvSentinelSuffix)
	}
	return b.String()
}

func newMissingEnvSentinelPrefix() (string, error) {
	var token [9]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", fmt.Errorf("creating missing env sentinel: %w", err)
	}
	return missingEnvSentinelPrefix + base64.RawURLEncoding.EncodeToString(token[:]) + "_", nil
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

func expandEnvVariablesWithMissingSentinels(input string, lookup func(string) (string, bool), sentinelPrefix string) (string, error) {
	var expandErr error
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
			return sentinelPrefix + base64.RawURLEncoding.EncodeToString([]byte(name)) + missingEnvSentinelSuffix
		}
		data, err := os.ReadFile(filePath)
		if err != nil {
			expandErr = fmt.Errorf("resolving %s_FILE: %w", name, err)
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
	cfg.Plugins = nonNilProviderEntryMap(cfg.Plugins)
	cfg.Providers.UI = nonNilUIEntryMap(cfg.Providers.UI)
	cfg.Providers.Auth = nonNilProviderEntryMap(cfg.Providers.Auth)
	cfg.Providers.Authorization = nonNilProviderEntryMap(cfg.Providers.Authorization)
	cfg.Providers.Secrets = applyDefaultBuiltinProviderEntries(cfg.Providers.Secrets, DefaultProviderInstance, "env")
	cfg.Providers.Telemetry = applyDefaultBuiltinProviderEntries(cfg.Providers.Telemetry, DefaultProviderInstance, "stdout")
	cfg.Providers.Audit = applyDefaultBuiltinProviderEntries(cfg.Providers.Audit, DefaultProviderInstance, "inherit")
	cfg.Providers.IndexedDB = nonNilProviderEntryMap(cfg.Providers.IndexedDB)
	cfg.Providers.Cache = nonNilProviderEntryMap(cfg.Providers.Cache)
	cfg.Providers.S3 = nonNilProviderEntryMap(cfg.Providers.S3)
	cfg.Providers.Workflow = nonNilProviderEntryMap(cfg.Providers.Workflow)
}

func normalizeProviderSourceShapes(cfg *Config) {
	if cfg == nil {
		return
	}
	cfg.APIVersion = strings.TrimSpace(cfg.APIVersion)
	sourceSyntax := sourceSyntaxForConfig(cfg.APIVersion)

	normalizeEntry := func(kind string, entry *ProviderEntry) {
		if entry == nil {
			return
		}
		normalizeProviderSource(kind, &entry.Source, sourceSyntax)
		if entry.HasReleaseMetadataSource() && entry.Source.Auth == nil && entry.InlineSourceAuth != nil {
			auth := *entry.InlineSourceAuth
			entry.Source.Auth = &auth
			entry.InlineSourceAuth = nil
		}
	}

	for _, entry := range cfg.Plugins {
		normalizeEntry(providermanifestv1.KindPlugin, entry)
	}
	for _, collection := range []struct {
		kind    string
		entries map[string]*ProviderEntry
	}{
		{providermanifestv1.KindAuth, cfg.Providers.Auth},
		{providermanifestv1.KindAuthorization, cfg.Providers.Authorization},
		{providermanifestv1.KindSecrets, cfg.Providers.Secrets},
		{string(HostProviderKindTelemetry), cfg.Providers.Telemetry},
		{string(HostProviderKindAudit), cfg.Providers.Audit},
		{providermanifestv1.KindIndexedDB, cfg.Providers.IndexedDB},
		{providermanifestv1.KindCache, cfg.Providers.Cache},
		{providermanifestv1.KindS3, cfg.Providers.S3},
		{string(HostProviderKindWorkflow), cfg.Providers.Workflow},
	} {
		for _, entry := range collection.entries {
			normalizeEntry(collection.kind, entry)
		}
	}
	for _, entry := range cfg.Providers.UI {
		if entry != nil {
			normalizeEntry(providermanifestv1.KindWebUI, &entry.ProviderEntry)
		}
	}
}

func normalizeProviderSource(kind string, source *ProviderSource, sourceSyntax providerSourceSyntaxMode) {
	if source == nil {
		return
	}
	source.scalar = strings.TrimSpace(source.scalar)
	if sourceSyntax == providerSourceSyntaxV4 && source.Path != "" && source.metadataPath == "" {
		source.metadataPath = source.Path
		source.Path = ""
	}
	if source.Builtin != "" || source.Path != "" || source.metadataURL != "" || source.metadataPath != "" {
		source.scalar = ""
		return
	}
	if source.scalar == "" {
		return
	}
	switch {
	case sourceSyntax == providerSourceSyntaxLegacy:
		source.Builtin = source.scalar
	case isBuiltinScalarSource(kind, source.scalar):
		source.Builtin = source.scalar
	case looksLikeMetadataURL(source.scalar):
		source.metadataURL = source.scalar
	case looksLikeUnsupportedScalarSource(source.scalar):
		source.unsupported = source.scalar
	case sourceSyntax == providerSourceSyntaxV4:
		source.metadataPath = source.scalar
	default:
		source.Path = source.scalar
	}
	source.scalar = ""
}

func isBuiltinScalarSource(kind, source string) bool {
	switch kind {
	case providermanifestv1.KindSecrets:
		switch source {
		case "env", "file":
			return true
		}
	case string(HostProviderKindTelemetry):
		switch source {
		case "noop", "stdout", "otlp":
			return true
		}
	case string(HostProviderKindAudit):
		switch source {
		case "inherit", "noop", "stdout", "otlp":
			return true
		}
	}
	return false
}

func looksLikeMetadataURL(value string) bool {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(value))
	if err != nil {
		return false
	}
	switch parsed.Scheme {
	case "http", "https":
		return parsed.Host != ""
	default:
		return false
	}
}

func looksLikeUnsupportedScalarSource(value string) bool {
	trimmed := strings.TrimSpace(value)
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "git+") {
		return true
	}
	if strings.Contains(trimmed, "://") {
		parsed, err := url.ParseRequestURI(trimmed)
		if err != nil {
			return true
		}
		switch parsed.Scheme {
		case "http", "https":
			return true
		default:
			return parsed.Scheme != ""
		}
	}
	for _, prefix := range []string{"http:", "https:", "ssh:", "file:", "ftp:", "ftps:"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func providerSourceSyntaxForAPIVersion(apiVersion string) (providerSourceSyntaxMode, error) {
	switch strings.TrimSpace(apiVersion) {
	case "":
		return providerSourceSyntaxLegacy, nil
	case APIVersionV3:
		return providerSourceSyntaxV3, nil
	case APIVersionV4:
		return providerSourceSyntaxV4, nil
	default:
		return providerSourceSyntaxLegacy, fmt.Errorf("config validation: unsupported apiVersion %q", strings.TrimSpace(apiVersion))
	}
}

func sourceSyntaxForConfig(apiVersion string) providerSourceSyntaxMode {
	mode, err := providerSourceSyntaxForAPIVersion(apiVersion)
	if err != nil {
		return providerSourceSyntaxLegacy
	}
	return mode
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
		if entry == nil || entry.Source.IsBuiltin() || entry.Source.IsMetadataURL() || entry.Source.IsLocalMetadataPath() || entry.Source.IsLocal() || entry.Source.UnsupportedURL() != "" {
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
	return nil
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

func normalizeAdminConfig(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	admin := cfg.Server.Admin
	admin.AuthorizationPolicy = strings.TrimSpace(admin.AuthorizationPolicy)
	if len(admin.AllowedRoles) == 0 {
		if admin.AuthorizationPolicy != "" {
			admin.AllowedRoles = []string{"admin"}
		} else {
			admin.AllowedRoles = nil
		}
		cfg.Server.Admin = admin
		return nil
	}

	roles, err := providerpkg.NormalizeWebUIAllowedRoles("server.admin.allowedRoles", admin.AllowedRoles)
	if err != nil {
		if admin.AuthorizationPolicy == "" {
			return fmt.Errorf("normalize admin config: server.admin.allowedRoles requires server.admin.authorizationPolicy")
		}
		return fmt.Errorf("normalize admin config: %w", err)
	}
	admin.AllowedRoles = roles
	cfg.Server.Admin = admin
	return nil
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

func applyPluginMountBindings(cfg *Config) error {
	if cfg == nil || len(cfg.Plugins) == 0 {
		return nil
	}

	pluginNames := slices.Sorted(maps.Keys(cfg.Plugins))
	seenUIs := make(map[string]string, len(pluginNames))
	for _, pluginName := range pluginNames {
		plugin := cfg.Plugins[pluginName]
		if plugin == nil {
			return fmt.Errorf("config validation: plugins.%s is required", pluginName)
		}
		plugin.UI = strings.TrimSpace(plugin.UI)
		plugin.MountPath = strings.TrimSpace(plugin.MountPath)
		plugin.AuthorizationPolicy = strings.TrimSpace(plugin.AuthorizationPolicy)

		if plugin.MountPath == "" {
			if plugin.UI != "" {
				return fmt.Errorf("config validation: plugins.%s.ui requires plugins.%s.mountPath", pluginName, pluginName)
			}
			continue
		}
		normalizedPath, err := normalizeMountedUIPath(plugin.MountPath)
		if err != nil {
			return fmt.Errorf("config validation: plugins.%s.mountPath: %w", pluginName, err)
		}
		plugin.MountPath = normalizedPath
		if err := validateAuthorizationPolicyReference(cfg, "plugin", pluginName, plugin.AuthorizationPolicy); err != nil {
			return err
		}
		if plugin.UI == "" {
			continue
		}
		if prev, exists := seenUIs[plugin.UI]; exists && prev != pluginName {
			return fmt.Errorf("config validation: plugins.%s.ui %q duplicates plugins.%s", pluginName, plugin.UI, prev)
		}
		ui := cfg.Providers.UI[plugin.UI]
		if ui == nil {
			return fmt.Errorf("config validation: plugins.%s.ui references unknown ui %q", pluginName, plugin.UI)
		}
		if current := strings.TrimSpace(ui.AuthorizationPolicy); current != "" && current != plugin.AuthorizationPolicy {
			return fmt.Errorf("config validation: plugins.%s.ui %q conflicts with providers.ui.%s.authorizationPolicy", pluginName, plugin.UI, plugin.UI)
		}
		if current := strings.TrimSpace(ui.Path); current != "" && current != plugin.MountPath {
			return fmt.Errorf("config validation: plugins.%s.ui %q conflicts with providers.ui.%s.path", pluginName, plugin.UI, plugin.UI)
		}
		if current := strings.TrimSpace(ui.OwnerPlugin); current != "" && current != pluginName {
			return fmt.Errorf("config validation: plugins.%s.ui %q conflicts with providers.ui.%s owner", pluginName, plugin.UI, plugin.UI)
		}
		ui.AuthorizationPolicy = plugin.AuthorizationPolicy
		ui.Path = plugin.MountPath
		ui.OwnerPlugin = pluginName
		seenUIs[plugin.UI] = pluginName
	}

	return nil
}

func resolveBaseURL(cfg *Config) {
	cfg.Server.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.Server.BaseURL), "/")
	cfg.Server.Management.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.Server.Management.BaseURL), "/")
}

func resolveRelativePathsInValue(configPath string, root map[string]any, sourceSyntax providerSourceSyntaxMode) {
	baseDir := filepath.Dir(configPath)
	if absPath, err := filepath.Abs(configPath); err == nil {
		baseDir = filepath.Dir(absPath)
	}

	if server := nestedMap(root, "server"); server != nil {
		resolveRelativeStringField(server, "artifactsDir", baseDir)
	}

	if providers := nestedMap(root, "providers"); providers != nil {
		for _, section := range []struct {
			key  string
			kind string
		}{
			{key: "auth", kind: providermanifestv1.KindAuth},
			{key: "authorization", kind: providermanifestv1.KindAuthorization},
			{key: "secrets", kind: providermanifestv1.KindSecrets},
			{key: "telemetry", kind: string(HostProviderKindTelemetry)},
			{key: "audit", kind: string(HostProviderKindAudit)},
			{key: "ui", kind: providermanifestv1.KindWebUI},
			{key: "indexeddb", kind: providermanifestv1.KindIndexedDB},
			{key: "cache", kind: providermanifestv1.KindCache},
			{key: "s3", kind: providermanifestv1.KindS3},
			{key: "workflow", kind: string(HostProviderKindWorkflow)},
		} {
			for _, entry := range mapValues(nestedMap(providers, section.key)) {
				resolveRelativePathsInEntry(section.kind, entry, baseDir, sourceSyntax)
			}
		}
	}

	for _, entry := range mapValues(nestedMap(root, "plugins")) {
		resolveRelativePathsInEntry(providermanifestv1.KindPlugin, entry, baseDir, sourceSyntax)
	}
}

func resolveRelativePathsInEntry(kind string, entry map[string]any, baseDir string, sourceSyntax providerSourceSyntaxMode) {
	if entry == nil {
		return
	}
	resolveRelativeStringField(entry, "iconFile", baseDir)
	if source, ok := entry["source"].(map[string]any); ok {
		resolveRelativeStringField(source, "path", baseDir)
		return
	}
	if sourceSyntax == providerSourceSyntaxLegacy {
		return
	}
	sourceValue, ok := entry["source"].(string)
	if !ok {
		return
	}
	sourceValue = strings.TrimSpace(sourceValue)
	if sourceValue == "" || isBuiltinScalarSource(kind, sourceValue) || looksLikeMetadataURL(sourceValue) || looksLikeUnsupportedScalarSource(sourceValue) {
		return
	}
	entry["source"] = resolveRelativeConfigValue(baseDir, sourceValue)
}

func resolveRelativeStringField(fields map[string]any, key, baseDir string) {
	if fields == nil {
		return
	}
	value, ok := fields[key].(string)
	if !ok {
		return
	}
	fields[key] = resolveRelativeConfigValue(baseDir, value)
}

func nestedMap(root map[string]any, key string) map[string]any {
	if root == nil {
		return nil
	}
	current, _ := root[key].(map[string]any)
	return current
}

func mapValues(entries map[string]any) []map[string]any {
	if len(entries) == 0 {
		return nil
	}
	values := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		values = append(values, entryMap)
	}
	return values
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
	for _, entry := range cfg.Providers.Authorization {
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
	for _, entry := range cfg.Providers.S3 {
		resolveEntry(entry)
	}
	for _, entry := range cfg.Providers.Workflow {
		resolveEntry(entry)
	}
	for _, entry := range cfg.Plugins {
		resolveEntry(entry)
	}
}

func resolveRelativePath(baseDir, value string) string {
	return resolveRelativeConfigValue(baseDir, value)
}

func resolveRelativeConfigValue(baseDir, value string) string {
	if value == "" || filepath.IsAbs(value) {
		return value
	}
	if strings.Contains(value, "${") {
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
