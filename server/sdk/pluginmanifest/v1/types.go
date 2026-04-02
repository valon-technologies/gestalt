package pluginmanifestv1

import (
	_ "embed"
)

const (
	KindProvider = "provider"
	KindWebUI    = "webui"
)

type Manifest struct {
	Source      string         `json:"source,omitempty" yaml:"source,omitempty"`
	Version     string         `json:"version" yaml:"version"`
	DisplayName string         `json:"display_name,omitempty" yaml:"display_name,omitempty"`
	Description string         `json:"description,omitempty" yaml:"description,omitempty"`
	IconFile    string         `json:"icon_file,omitempty" yaml:"icon_file,omitempty"`
	Kinds       []string       `json:"kinds" yaml:"kinds"`
	Provider    *Provider      `json:"provider,omitempty" yaml:"provider,omitempty"`
	WebUI       *WebUIMetadata `json:"webui,omitempty" yaml:"webui,omitempty"`
	Artifacts   []Artifact     `json:"artifacts,omitempty" yaml:"artifacts,omitempty"`
	Entrypoints Entrypoints    `json:"entrypoints,omitzero" yaml:"entrypoints,omitempty"`
}

type WebUIMetadata struct {
	AssetRoot string `json:"asset_root" yaml:"asset_root"`
}

type Provider struct {
	ConfigSchemaPath     string                             `json:"config_schema_path,omitempty" yaml:"config_schema_path,omitempty"`
	Auth                 *ProviderAuth                      `json:"auth,omitempty" yaml:"auth,omitempty"`
	MCP                  bool                               `json:"mcp,omitempty" yaml:"mcp,omitempty"`
	BaseURL              string                             `json:"base_url,omitempty" yaml:"base_url,omitempty"`
	Headers              map[string]string                  `json:"headers,omitempty" yaml:"headers,omitempty"`
	ManagedParameters    []ManagedParameter                 `json:"managed_parameters,omitempty" yaml:"managed_parameters,omitempty"`
	Operations           []ProviderOperation                `json:"operations,omitempty" yaml:"operations,omitempty"`
	PostConnectDiscovery *ProviderPostConnectDiscovery      `json:"post_connect_discovery,omitempty" yaml:"post_connect_discovery,omitempty"`
	ConnectionParams     map[string]ProviderConnectionParam `json:"connection_params,omitempty" yaml:"connection_params,omitempty"`

	OpenAPI           string                                `json:"openapi,omitempty" yaml:"openapi,omitempty"`
	GraphQLURL        string                                `json:"graphql_url,omitempty" yaml:"graphql_url,omitempty"`
	MCPURL            string                                `json:"mcp_url,omitempty" yaml:"mcp_url,omitempty"`
	AllowedOperations map[string]*ManifestOperationOverride `json:"allowed_operations,omitempty" yaml:"allowed_operations,omitempty"`
	OpenAPIConnection string                                `json:"openapi_connection,omitempty" yaml:"openapi_connection,omitempty"`
	GraphQLConnection string                                `json:"graphql_connection,omitempty" yaml:"graphql_connection,omitempty"`
	MCPConnection     string                                `json:"mcp_connection,omitempty" yaml:"mcp_connection,omitempty"`
	DefaultConnection string                                `json:"default_connection,omitempty" yaml:"default_connection,omitempty"`
	Connections       map[string]*ManifestConnectionDef     `json:"connections,omitempty" yaml:"connections,omitempty"`
	ResponseMapping   *ManifestResponseMapping              `json:"response_mapping,omitempty" yaml:"response_mapping,omitempty"`
}

type ManifestResponseMapping struct {
	DataPath   string                     `json:"data_path" yaml:"data_path"`
	Pagination *ManifestPaginationMapping `json:"pagination,omitempty" yaml:"pagination,omitempty"`
}

type ManifestPaginationMapping struct {
	HasMorePath string `json:"has_more_path" yaml:"has_more_path"`
	CursorPath  string `json:"cursor_path" yaml:"cursor_path"`
}

type ProviderPostConnectDiscovery struct {
	URL             string            `json:"url" yaml:"url"`
	IDPath          string            `json:"id_path,omitempty" yaml:"id_path,omitempty"`
	NamePath        string            `json:"name_path,omitempty" yaml:"name_path,omitempty"`
	MetadataMapping map[string]string `json:"metadata_mapping,omitempty" yaml:"metadata_mapping,omitempty"`
}

type ProviderConnectionParam struct {
	Required    bool   `json:"required,omitempty" yaml:"required,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	From        string `json:"from,omitempty" yaml:"from,omitempty"`
}

func (p *Provider) IsDeclarative() bool {
	return p != nil && len(p.Operations) > 0
}

func (p *Provider) IsSpecLoaded() bool {
	return p != nil && (p.OpenAPI != "" || p.GraphQLURL != "" || p.MCPURL != "")
}

func (p *Provider) IsManifestBacked() bool {
	return p != nil && (p.IsDeclarative() || p.IsSpecLoaded())
}

func (m *Manifest) IsHybridProvider() bool {
	return m != nil && m.Provider != nil && m.Provider.IsManifestBacked() && m.Entrypoints.Provider != nil
}

func (m *Manifest) IsDeclarativeOnlyProvider() bool {
	return m != nil && m.Provider != nil && m.Provider.IsManifestBacked() && m.Entrypoints.Provider == nil
}

type ManifestOperationOverride struct {
	Alias       string `json:"alias,omitempty" yaml:"alias,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

type ManifestConnectionDef struct {
	Mode string        `json:"mode,omitempty" yaml:"mode,omitempty"`
	Auth *ProviderAuth `json:"auth,omitempty" yaml:"auth,omitempty"`
}

type ProviderOperation struct {
	Name        string              `json:"name" yaml:"name"`
	Description string              `json:"description,omitempty" yaml:"description,omitempty"`
	Method      string              `json:"method" yaml:"method"`
	Path        string              `json:"path" yaml:"path"`
	Parameters  []ProviderParameter `json:"parameters,omitempty" yaml:"parameters,omitempty"`
}

type ProviderParameter struct {
	Name        string `json:"name" yaml:"name"`
	Type        string `json:"type" yaml:"type"`
	In          string `json:"in" yaml:"in"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Required    bool   `json:"required,omitempty" yaml:"required,omitempty"`
}

type ManagedParameter struct {
	In    string `json:"in" yaml:"in"`
	Name  string `json:"name" yaml:"name"`
	Value string `json:"value" yaml:"value"`
}

const (
	AuthTypeOAuth2   = "oauth2"
	AuthTypeMCPOAuth = "mcp_oauth"
	AuthTypeBearer   = "bearer"
	AuthTypeManual   = "manual"
	AuthTypeNone     = "none"
)

type ProviderAuth struct {
	Type                string            `json:"type" yaml:"type"`
	AuthorizationURL    string            `json:"authorization_url,omitempty" yaml:"authorization_url,omitempty"`
	TokenURL            string            `json:"token_url,omitempty" yaml:"token_url,omitempty"`
	ClientID            string            `json:"client_id,omitempty" yaml:"client_id,omitempty"`
	ClientSecret        string            `json:"client_secret,omitempty" yaml:"client_secret,omitempty"`
	Scopes              []string          `json:"scopes,omitempty" yaml:"scopes,omitempty"`
	PKCE                bool              `json:"pkce,omitempty" yaml:"pkce,omitempty"`
	ClientAuth          string            `json:"client_auth,omitempty" yaml:"client_auth,omitempty"`
	TokenExchange       string            `json:"token_exchange,omitempty" yaml:"token_exchange,omitempty"`
	AccessTokenPath     string            `json:"access_token_path,omitempty" yaml:"access_token_path,omitempty"`
	ScopeParam          string            `json:"scope_param,omitempty" yaml:"scope_param,omitempty"`
	ScopeSeparator      string            `json:"scope_separator,omitempty" yaml:"scope_separator,omitempty"`
	AuthorizationParams map[string]string `json:"authorization_params,omitempty" yaml:"authorization_params,omitempty"`
	TokenParams         map[string]string `json:"token_params,omitempty" yaml:"token_params,omitempty"`
	RefreshParams       map[string]string `json:"refresh_params,omitempty" yaml:"refresh_params,omitempty"`
	AcceptHeader        string            `json:"accept_header,omitempty" yaml:"accept_header,omitempty"`
	TokenMetadata       []string          `json:"token_metadata,omitempty" yaml:"token_metadata,omitempty"`
	Credentials         []CredentialField `json:"credentials,omitempty" yaml:"credentials,omitempty"`
}

type CredentialField struct {
	Name        string `json:"name" yaml:"name"`
	Label       string `json:"label,omitempty" yaml:"label,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	HelpURL     string `json:"help_url,omitempty" yaml:"help_url,omitempty"`
}

type Artifact struct {
	OS     string `json:"os" yaml:"os"`
	Arch   string `json:"arch" yaml:"arch"`
	Path   string `json:"path" yaml:"path"`
	SHA256 string `json:"sha256,omitempty" yaml:"sha256,omitempty"`
}

type Entrypoints struct {
	Provider *Entrypoint `json:"provider,omitempty" yaml:"provider,omitempty"`
}

type Entrypoint struct {
	ArtifactPath string   `json:"artifact_path" yaml:"artifact_path"`
	Args         []string `json:"args,omitempty" yaml:"args,omitempty"`
}

//go:embed manifest.jsonschema.json
var ManifestJSONSchema []byte
