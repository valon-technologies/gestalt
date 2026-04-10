package pluginmanifestv1

import (
	_ "embed"
)

const (
	KindPlugin    = "plugin"
	KindAuth      = "auth"
	KindIndexedDB = "indexeddb"
	KindSecrets   = "secrets"
	KindWebUI     = "webui"
)

type Manifest struct {
	Kind        string             `json:"kind,omitempty" yaml:"kind,omitempty"`
	Source      string             `json:"source,omitempty" yaml:"source,omitempty"`
	Version     string             `json:"version" yaml:"version"`
	DisplayName string             `json:"displayName,omitempty" yaml:"displayName,omitempty"`
	Description string             `json:"description,omitempty" yaml:"description,omitempty"`
	IconFile    string             `json:"iconFile,omitempty" yaml:"iconFile,omitempty"`
	Release     *ReleaseMetadata   `json:"release,omitempty" yaml:"release,omitempty"`
	Plugin      *Plugin            `json:"plugin,omitempty" yaml:"plugin,omitempty"`
	Auth        *AuthMetadata      `json:"auth,omitempty" yaml:"auth,omitempty"`
	Datastore   *DatastoreMetadata `json:"datastore,omitempty" yaml:"datastore,omitempty"`
	Secrets     *SecretsMetadata   `json:"secrets,omitempty" yaml:"secrets,omitempty"`
	WebUI       *WebUIMetadata     `json:"webui,omitempty" yaml:"webui,omitempty"`
	Artifacts   []Artifact         `json:"artifacts,omitempty" yaml:"artifacts,omitempty"`
	Entrypoints Entrypoints        `json:"entrypoints,omitzero" yaml:"entrypoints,omitempty"`
}

type ReleaseMetadata struct {
	Build *ReleaseBuild `json:"build,omitempty" yaml:"build,omitempty"`
}

type ReleaseBuild struct {
	Workdir string   `json:"workdir,omitempty" yaml:"workdir,omitempty"`
	Command []string `json:"command" yaml:"command"`
}

type AuthMetadata struct {
	ConfigSchemaPath string `json:"configSchemaPath,omitempty" yaml:"configSchemaPath,omitempty"`
}

type DatastoreMetadata struct {
	ConfigSchemaPath string `json:"configSchemaPath,omitempty" yaml:"configSchemaPath,omitempty"`
}

type SecretsMetadata struct {
	ConfigSchemaPath string `json:"configSchemaPath,omitempty" yaml:"configSchemaPath,omitempty"`
}

type WebUIMetadata struct {
	AssetRoot        string `json:"assetRoot" yaml:"assetRoot"`
	ConfigSchemaPath string `json:"configSchemaPath,omitempty" yaml:"configSchemaPath,omitempty"`
}

type Plugin struct {
	ConfigSchemaPath  string                             `json:"configSchemaPath,omitempty" yaml:"configSchemaPath,omitempty"`
	Auth              *ProviderAuth                      `json:"auth,omitempty" yaml:"auth,omitempty"`
	ConnectionMode    ConnectionMode                     `json:"connectionMode,omitempty" yaml:"connectionMode,omitempty"`
	MCP               bool                               `json:"mcp,omitempty" yaml:"mcp,omitempty"`
	Headers           map[string]string                  `json:"headers,omitempty" yaml:"headers,omitempty"`
	ManagedParameters []ManagedParameter                 `json:"managedParameters,omitempty" yaml:"managedParameters,omitempty"`
	Discovery         *ProviderDiscovery                 `json:"discovery,omitempty" yaml:"discovery,omitempty"`
	ConnectionParams  map[string]ProviderConnectionParam `json:"connectionParams,omitempty" yaml:"connectionParams,omitempty"`

	Surfaces          *PluginSurfaces                       `json:"surfaces,omitempty" yaml:"surfaces,omitempty"`
	AllowedOperations map[string]*ManifestOperationOverride `json:"allowedOperations,omitempty" yaml:"allowedOperations,omitempty"`
	DefaultConnection string                                `json:"defaultConnection,omitempty" yaml:"defaultConnection,omitempty"`
	Connections       map[string]*ManifestConnectionDef     `json:"connections,omitempty" yaml:"connections,omitempty"`
	ResponseMapping   *ManifestResponseMapping              `json:"responseMapping,omitempty" yaml:"responseMapping,omitempty"`
	Pagination        *ManifestPaginationConfig             `json:"pagination,omitempty" yaml:"pagination,omitempty"`
}

type PluginSurfaces struct {
	REST    *RESTSurface    `json:"rest,omitempty" yaml:"rest,omitempty"`
	OpenAPI *OpenAPISurface `json:"openapi,omitempty" yaml:"openapi,omitempty"`
	GraphQL *GraphQLSurface `json:"graphql,omitempty" yaml:"graphql,omitempty"`
	MCP     *MCPSurface     `json:"mcp,omitempty" yaml:"mcp,omitempty"`
}

type RESTSurface struct {
	Connection string              `json:"connection,omitempty" yaml:"connection,omitempty"`
	BaseURL    string              `json:"baseUrl" yaml:"baseUrl"`
	Operations []ProviderOperation `json:"operations" yaml:"operations"`
}

type OpenAPISurface struct {
	Connection string `json:"connection,omitempty" yaml:"connection,omitempty"`
	Document   string `json:"document" yaml:"document"`
	BaseURL    string `json:"baseUrl,omitempty" yaml:"baseUrl,omitempty"`
}

type GraphQLSurface struct {
	Connection string `json:"connection,omitempty" yaml:"connection,omitempty"`
	URL        string `json:"url" yaml:"url"`
}

type MCPSurface struct {
	Connection string `json:"connection,omitempty" yaml:"connection,omitempty"`
	URL        string `json:"url" yaml:"url"`
}

func (p *Plugin) IsDeclarative() bool {
	return p != nil && p.Surfaces != nil && p.Surfaces.REST != nil && len(p.Surfaces.REST.Operations) > 0
}

func (p *Plugin) IsSpecLoaded() bool {
	return p != nil && p.Surfaces != nil &&
		(p.Surfaces.OpenAPI != nil || p.Surfaces.GraphQL != nil || p.Surfaces.MCP != nil)
}

func (p *Plugin) IsManifestBacked() bool {
	return p != nil && (p.IsDeclarative() || p.IsSpecLoaded())
}

func (p *Plugin) OpenAPIDocument() string {
	if p == nil || p.Surfaces == nil || p.Surfaces.OpenAPI == nil {
		return ""
	}
	return p.Surfaces.OpenAPI.Document
}

func (p *Plugin) OpenAPIBaseURL() string {
	if p == nil || p.Surfaces == nil || p.Surfaces.OpenAPI == nil {
		return ""
	}
	return p.Surfaces.OpenAPI.BaseURL
}

func (p *Plugin) SpecBaseURL() string {
	if u := p.RESTBaseURL(); u != "" {
		return u
	}
	return p.OpenAPIBaseURL()
}

func (p *Plugin) GraphQLURL() string {
	if p == nil || p.Surfaces == nil || p.Surfaces.GraphQL == nil {
		return ""
	}
	return p.Surfaces.GraphQL.URL
}

func (p *Plugin) MCPURL() string {
	if p == nil || p.Surfaces == nil || p.Surfaces.MCP == nil {
		return ""
	}
	return p.Surfaces.MCP.URL
}

func (p *Plugin) RESTBaseURL() string {
	if p == nil || p.Surfaces == nil || p.Surfaces.REST == nil {
		return ""
	}
	return p.Surfaces.REST.BaseURL
}

func (p *Plugin) RESTOperations() []ProviderOperation {
	if p == nil || p.Surfaces == nil || p.Surfaces.REST == nil {
		return nil
	}
	return p.Surfaces.REST.Operations
}

func (p *Plugin) SurfaceConnectionName(surface string) string {
	if p == nil || p.Surfaces == nil {
		return ""
	}
	switch surface {
	case "openapi":
		if p.Surfaces.OpenAPI != nil {
			return p.Surfaces.OpenAPI.Connection
		}
	case "graphql":
		if p.Surfaces.GraphQL != nil {
			return p.Surfaces.GraphQL.Connection
		}
	case "mcp":
		if p.Surfaces.MCP != nil {
			return p.Surfaces.MCP.Connection
		}
	case "rest":
		if p.Surfaces.REST != nil {
			return p.Surfaces.REST.Connection
		}
	}
	return ""
}

func (m *Manifest) IsHybridProvider() bool {
	return m != nil && m.Plugin != nil && m.Plugin.IsManifestBacked() && m.Entrypoints.Plugin != nil
}

func (m *Manifest) IsDeclarativeOnlyProvider() bool {
	return m != nil && m.Plugin != nil && m.Plugin.IsManifestBacked() && m.Entrypoints.Plugin == nil
}

type ManifestResponseMapping struct {
	DataPath   string                     `json:"dataPath" yaml:"dataPath"`
	Pagination *ManifestPaginationMapping `json:"pagination,omitempty" yaml:"pagination,omitempty"`
}

type ManifestValueSelector struct {
	Source string `json:"source" yaml:"source"`
	Path   string `json:"path" yaml:"path"`
}

type ManifestPaginationMapping struct {
	HasMore *ManifestValueSelector `json:"hasMore,omitempty" yaml:"hasMore,omitempty"`
	Cursor  *ManifestValueSelector `json:"cursor,omitempty" yaml:"cursor,omitempty"`
}

type ProviderDiscovery struct {
	URL      string            `json:"url" yaml:"url"`
	IDPath   string            `json:"idPath,omitempty" yaml:"idPath,omitempty"`
	NamePath string            `json:"namePath,omitempty" yaml:"namePath,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

type ProviderConnectionParam struct {
	Required    bool   `json:"required,omitempty" yaml:"required,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	From        string `json:"from,omitempty" yaml:"from,omitempty"`
}

type ManifestPaginationConfig struct {
	Style        PaginationStyle        `json:"style" yaml:"style"`
	CursorParam  string                 `json:"cursorParam,omitempty" yaml:"cursorParam,omitempty"`
	Cursor       *ManifestValueSelector `json:"cursor,omitempty" yaml:"cursor,omitempty"`
	LimitParam   string                 `json:"limitParam,omitempty" yaml:"limitParam,omitempty"`
	DefaultLimit int                    `json:"defaultLimit,omitempty" yaml:"defaultLimit,omitempty"`
	ResultsPath  string                 `json:"resultsPath,omitempty" yaml:"resultsPath,omitempty"`
	MaxPages     int                    `json:"maxPages,omitempty" yaml:"maxPages,omitempty"`
}

type ManifestOperationOverride struct {
	Alias       string                    `json:"alias,omitempty" yaml:"alias,omitempty"`
	Description string                    `json:"description,omitempty" yaml:"description,omitempty"`
	Paginate    bool                      `json:"paginate,omitempty" yaml:"paginate,omitempty"`
	Pagination  *ManifestPaginationConfig `json:"pagination,omitempty" yaml:"pagination,omitempty"`
}

type ManifestConnectionDef struct {
	Mode      ConnectionMode                     `json:"mode,omitempty" yaml:"mode,omitempty"`
	Auth      *ProviderAuth                      `json:"auth,omitempty" yaml:"auth,omitempty"`
	Params    map[string]ProviderConnectionParam `json:"params,omitempty" yaml:"params,omitempty"`
	Discovery *ProviderDiscovery                 `json:"discovery,omitempty" yaml:"discovery,omitempty"`
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

type AuthType string

const (
	AuthTypeOAuth2   AuthType = "oauth2"
	AuthTypeMCPOAuth AuthType = "mcp_oauth"
	AuthTypeBearer   AuthType = "bearer"
	AuthTypeManual   AuthType = "manual"
	AuthTypeNone     AuthType = "none"
)

type ConnectionMode string

const (
	ConnectionModeNone     ConnectionMode = "none"
	ConnectionModeUser     ConnectionMode = "user"
	ConnectionModeIdentity ConnectionMode = "identity"
	ConnectionModeEither   ConnectionMode = "either"
)

type PaginationStyle string

const (
	PaginationStyleCursor PaginationStyle = "cursor"
	PaginationStyleOffset PaginationStyle = "offset"
	PaginationStylePage   PaginationStyle = "page"
)

type ProviderAuth struct {
	Type                AuthType          `json:"type" yaml:"type"`
	AuthorizationURL    string            `json:"authorizationUrl,omitempty" yaml:"authorizationUrl,omitempty"`
	TokenURL            string            `json:"tokenUrl,omitempty" yaml:"tokenUrl,omitempty"`
	ClientID            string            `json:"clientId,omitempty" yaml:"clientId,omitempty"`
	ClientSecret        string            `json:"clientSecret,omitempty" yaml:"clientSecret,omitempty"`
	Scopes              []string          `json:"scopes,omitempty" yaml:"scopes,omitempty"`
	PKCE                bool              `json:"pkce,omitempty" yaml:"pkce,omitempty"`
	ClientAuth          string            `json:"clientAuth,omitempty" yaml:"clientAuth,omitempty"`
	TokenExchange       string            `json:"tokenExchange,omitempty" yaml:"tokenExchange,omitempty"`
	AccessTokenPath     string            `json:"accessTokenPath,omitempty" yaml:"accessTokenPath,omitempty"`
	ScopeParam          string            `json:"scopeParam,omitempty" yaml:"scopeParam,omitempty"`
	ScopeSeparator      string            `json:"scopeSeparator,omitempty" yaml:"scopeSeparator,omitempty"`
	AuthorizationParams map[string]string `json:"authorizationParams,omitempty" yaml:"authorizationParams,omitempty"`
	TokenParams         map[string]string `json:"tokenParams,omitempty" yaml:"tokenParams,omitempty"`
	RefreshParams       map[string]string `json:"refreshParams,omitempty" yaml:"refreshParams,omitempty"`
	AcceptHeader        string            `json:"acceptHeader,omitempty" yaml:"acceptHeader,omitempty"`
	TokenMetadata       []string          `json:"tokenMetadata,omitempty" yaml:"tokenMetadata,omitempty"`
	Credentials         []CredentialField `json:"credentials,omitempty" yaml:"credentials,omitempty"`
	AuthMapping         *AuthMapping      `json:"authMapping,omitempty" yaml:"authMapping,omitempty"`
}

type CredentialField struct {
	Name        string `json:"name" yaml:"name"`
	Label       string `json:"label,omitempty" yaml:"label,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

type AuthMapping struct {
	Headers map[string]AuthValue `json:"headers,omitempty" yaml:"headers,omitempty"`
	Basic   *BasicAuthMapping    `json:"basic,omitempty" yaml:"basic,omitempty"`
}

type BasicAuthMapping struct {
	Username AuthValue `json:"username" yaml:"username"`
	Password AuthValue `json:"password" yaml:"password"`
}

type AuthValue struct {
	Value     string         `json:"value,omitempty" yaml:"value,omitempty"`
	ValueFrom *AuthValueFrom `json:"valueFrom,omitempty" yaml:"valueFrom,omitempty"`
}

type AuthValueFrom struct {
	CredentialFieldRef *CredentialFieldRef `json:"credentialFieldRef,omitempty" yaml:"credentialFieldRef,omitempty"`
}

type CredentialFieldRef struct {
	Name string `json:"name" yaml:"name"`
}

type Artifact struct {
	OS     string `json:"os" yaml:"os"`
	Arch   string `json:"arch" yaml:"arch"`
	LibC   string `json:"libc,omitempty" yaml:"libc,omitempty"`
	Path   string `json:"path" yaml:"path"`
	SHA256 string `json:"sha256,omitempty" yaml:"sha256,omitempty"`
}

type Entrypoints struct {
	Plugin    *Entrypoint `json:"plugin,omitempty" yaml:"plugin,omitempty"`
	Auth      *Entrypoint `json:"auth,omitempty" yaml:"auth,omitempty"`
	Datastore *Entrypoint `json:"datastore,omitempty" yaml:"datastore,omitempty"`
	Secrets   *Entrypoint `json:"secrets,omitempty" yaml:"secrets,omitempty"`
}

type Entrypoint struct {
	ArtifactPath string   `json:"artifactPath" yaml:"artifactPath"`
	Args         []string `json:"args,omitempty" yaml:"args,omitempty"`
}

//go:embed manifest.jsonschema.json
var ManifestJSONSchema []byte
