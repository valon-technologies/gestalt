package providermanifestv1

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	KindPlugin         = "plugin"
	KindAuthentication = "authentication"
	KindAuthorization  = "authorization"
	KindIndexedDB      = "indexeddb"
	KindCache          = "cache"
	KindS3             = "s3"
	KindWorkflow       = "workflow"
	KindSecrets        = "secrets"
	KindUI             = "ui"
)

func NormalizeKind(kind string) string {
	switch strings.TrimSpace(strings.ToLower(kind)) {
	case "auth":
		return KindAuthentication
	case KindAuthentication:
		return KindAuthentication
	case KindPlugin:
		return KindPlugin
	case KindAuthorization:
		return KindAuthorization
	case KindIndexedDB:
		return KindIndexedDB
	case KindCache:
		return KindCache
	case KindS3:
		return KindS3
	case KindWorkflow:
		return KindWorkflow
	case KindSecrets:
		return KindSecrets
	case "webui":
		return KindUI
	case KindUI:
		return KindUI
	default:
		return strings.TrimSpace(kind)
	}
}

type Manifest struct {
	Kind        string           `json:"kind,omitempty" yaml:"kind,omitempty"`
	Source      string           `json:"source,omitempty" yaml:"source,omitempty"`
	Version     string           `json:"version" yaml:"version"`
	DisplayName string           `json:"displayName,omitempty" yaml:"displayName,omitempty"`
	Description string           `json:"description,omitempty" yaml:"description,omitempty"`
	IconFile    string           `json:"iconFile,omitempty" yaml:"iconFile,omitempty"`
	Release     *ReleaseMetadata `json:"release,omitempty" yaml:"release,omitempty"`
	Artifacts   []Artifact       `json:"artifacts,omitempty" yaml:"artifacts,omitempty"`
	Entrypoint  *Entrypoint      `json:"entrypoint,omitempty" yaml:"entrypoint,omitempty"`
	Spec        *Spec            `json:"spec,omitempty" yaml:"spec,omitempty"`
}

type ReleaseMetadata struct {
	Build *ReleaseBuild `json:"build,omitempty" yaml:"build,omitempty"`
}

type ReleaseBuild struct {
	Workdir string   `json:"workdir,omitempty" yaml:"workdir,omitempty"`
	Command []string `json:"command" yaml:"command"`
}

// Spec is a union type validated per kind. For auth/datastore/secrets only
// ConfigSchemaPath is valid. For plugins all surface/connection fields are
// valid. For ui AssetRoot + ConfigSchemaPath.
type Spec struct {
	ConfigSchemaPath string `json:"configSchemaPath,omitempty" yaml:"configSchemaPath,omitempty"`

	// Plugin-specific fields
	RouteAuth         *RouteAuthRef                         `json:"auth,omitempty" yaml:"auth,omitempty"`
	SecuritySchemes   map[string]*HTTPSecurityScheme        `json:"securitySchemes,omitempty" yaml:"securitySchemes,omitempty"`
	HTTP              map[string]*HTTPBinding               `json:"http,omitempty" yaml:"http,omitempty"`
	MCP               bool                                  `json:"mcp,omitempty" yaml:"mcp,omitempty"`
	Headers           map[string]string                     `json:"headers,omitempty" yaml:"headers,omitempty"`
	ManagedParameters []ManagedParameter                    `json:"managedParameters,omitempty" yaml:"managedParameters,omitempty"`
	Surfaces          *ProviderSurfaces                     `json:"surfaces,omitempty" yaml:"surfaces,omitempty"`
	AllowedOperations map[string]*ManifestOperationOverride `json:"allowedOperations,omitempty" yaml:"allowedOperations,omitempty"`
	DefaultConnection string                                `json:"defaultConnection,omitempty" yaml:"defaultConnection,omitempty"`
	Connections       map[string]*ManifestConnectionDef     `json:"connections,omitempty" yaml:"connections,omitempty"`
	ResponseMapping   *ManifestResponseMapping              `json:"responseMapping,omitempty" yaml:"responseMapping,omitempty"`
	Pagination        *ManifestPaginationConfig             `json:"pagination,omitempty" yaml:"pagination,omitempty"`
	Requires          []string                              `json:"requires,omitempty" yaml:"requires,omitempty"`
	UI                *OwnedUI                              `json:"ui,omitempty" yaml:"ui,omitempty"`

	// UI-specific fields
	AssetRoot string    `json:"assetRoot,omitempty" yaml:"assetRoot,omitempty"`
	Routes    []UIRoute `json:"routes,omitempty" yaml:"routes,omitempty"`
}

type RouteAuthRef struct {
	Provider string `json:"provider,omitempty" yaml:"provider,omitempty"`
}

type HTTPSecuritySchemeType string

const (
	HTTPSecuritySchemeTypeSlackSignature HTTPSecuritySchemeType = "slack_signature"
	HTTPSecuritySchemeTypeAPIKey         HTTPSecuritySchemeType = "apiKey"
	HTTPSecuritySchemeTypeHTTP           HTTPSecuritySchemeType = "http"
	HTTPSecuritySchemeTypeNone           HTTPSecuritySchemeType = "none"
)

type HTTPIn string

const (
	HTTPInHeader HTTPIn = "header"
	HTTPInQuery  HTTPIn = "query"
)

type HTTPAuthScheme string

const (
	HTTPAuthSchemeBasic  HTTPAuthScheme = "basic"
	HTTPAuthSchemeBearer HTTPAuthScheme = "bearer"
)

type HTTPBinding struct {
	Path        string           `json:"path" yaml:"path"`
	Method      string           `json:"method" yaml:"method"`
	RequestBody *HTTPRequestBody `json:"requestBody,omitempty" yaml:"requestBody,omitempty"`
	Security    string           `json:"security,omitempty" yaml:"security,omitempty"`
	Target      string           `json:"target" yaml:"target"`
	Ack         *HTTPAck         `json:"ack,omitempty" yaml:"ack,omitempty"`
}

type HTTPRequestBody struct {
	Required bool                      `json:"required,omitempty" yaml:"required,omitempty"`
	Content  map[string]*HTTPMediaType `json:"content,omitempty" yaml:"content,omitempty"`
}

type HTTPMediaType struct {
}

type HTTPAck struct {
	Status  int               `json:"status,omitempty" yaml:"status,omitempty"`
	Headers map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
	Body    any               `json:"body,omitempty" yaml:"body,omitempty"`
}

type HTTPSecurityScheme struct {
	Type        HTTPSecuritySchemeType `json:"type,omitempty" yaml:"type,omitempty"`
	Description string                 `json:"description,omitempty" yaml:"description,omitempty"`
	Name        string                 `json:"name,omitempty" yaml:"name,omitempty"`
	In          HTTPIn                 `json:"in,omitempty" yaml:"in,omitempty"`
	Scheme      HTTPAuthScheme         `json:"scheme,omitempty" yaml:"scheme,omitempty"`
	Secret      *HTTPSecretRef         `json:"secret,omitempty" yaml:"secret,omitempty"`
}

type HTTPSecretRef struct {
	Env    string `json:"env,omitempty" yaml:"env,omitempty"`
	Secret string `json:"secret,omitempty" yaml:"secret,omitempty"`
}

type OwnedUI struct {
	Path string `json:"path,omitempty" yaml:"path,omitempty"`
}

func (s *Spec) IsDeclarative() bool {
	return s != nil && s.Surfaces != nil && s.Surfaces.REST != nil && len(s.Surfaces.REST.Operations) > 0
}

func (s *Spec) IsSpecLoaded() bool {
	return s != nil && s.Surfaces != nil &&
		(s.Surfaces.OpenAPI != nil || s.Surfaces.GraphQL != nil || s.Surfaces.MCP != nil)
}

func (s *Spec) IsManifestBacked() bool {
	return s != nil && (s.IsDeclarative() || s.IsSpecLoaded())
}

func (s *Spec) OpenAPIDocument() string {
	if s == nil || s.Surfaces == nil || s.Surfaces.OpenAPI == nil {
		return ""
	}
	return s.Surfaces.OpenAPI.Document
}

func (s *Spec) OpenAPIBaseURL() string {
	if s == nil || s.Surfaces == nil || s.Surfaces.OpenAPI == nil {
		return ""
	}
	return s.Surfaces.OpenAPI.BaseURL
}

func (s *Spec) SpecBaseURL() string {
	if u := s.RESTBaseURL(); u != "" {
		return u
	}
	return s.OpenAPIBaseURL()
}

func (s *Spec) GraphQLURL() string {
	if s == nil || s.Surfaces == nil || s.Surfaces.GraphQL == nil {
		return ""
	}
	return s.Surfaces.GraphQL.URL
}

func (s *Spec) GraphQLOperationSelections() map[string]string {
	if s == nil || s.Surfaces == nil || s.Surfaces.GraphQL == nil {
		return nil
	}
	return s.Surfaces.GraphQL.OperationSelections
}

func (s *Spec) MCPURL() string {
	if s == nil || s.Surfaces == nil || s.Surfaces.MCP == nil {
		return ""
	}
	return s.Surfaces.MCP.URL
}

func (s *Spec) RESTBaseURL() string {
	if s == nil || s.Surfaces == nil || s.Surfaces.REST == nil {
		return ""
	}
	return s.Surfaces.REST.BaseURL
}

func (s *Spec) RESTOperations() []ProviderOperation {
	if s == nil || s.Surfaces == nil || s.Surfaces.REST == nil {
		return nil
	}
	return s.Surfaces.REST.Operations
}

func (s *Spec) SurfaceConnectionName(surface string) string {
	if s == nil || s.Surfaces == nil {
		return ""
	}
	switch surface {
	case "openapi":
		if s.Surfaces.OpenAPI != nil {
			return s.Surfaces.OpenAPI.Connection
		}
	case "graphql":
		if s.Surfaces.GraphQL != nil {
			return s.Surfaces.GraphQL.Connection
		}
	case "mcp":
		if s.Surfaces.MCP != nil {
			return s.Surfaces.MCP.Connection
		}
	case "rest":
		if s.Surfaces.REST != nil {
			return s.Surfaces.REST.Connection
		}
	}
	return ""
}

func (s *Spec) DefaultConnectionDef() *ManifestConnectionDef {
	if s == nil || s.Connections == nil {
		return nil
	}
	return s.Connections["default"]
}

func (m *Manifest) IsHybridProvider() bool {
	return m != nil && m.Spec != nil && m.Spec.IsManifestBacked() && m.Entrypoint != nil
}

func (m *Manifest) IsDeclarativeOnlyProvider() bool {
	return m != nil && m.Spec != nil && m.Spec.IsManifestBacked() && m.Entrypoint == nil
}

type ProviderSurfaces struct {
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
	Connection          string            `json:"connection,omitempty" yaml:"connection,omitempty"`
	URL                 string            `json:"url" yaml:"url"`
	OperationSelections map[string]string `json:"operationSelections,omitempty" yaml:"operationSelections,omitempty"`
}

type MCPSurface struct {
	Connection string `json:"connection,omitempty" yaml:"connection,omitempty"`
	URL        string `json:"url" yaml:"url"`
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
	Alias        string                    `json:"alias,omitempty" yaml:"alias,omitempty"`
	Description  string                    `json:"description,omitempty" yaml:"description,omitempty"`
	AllowedRoles []string                  `json:"allowedRoles,omitempty" yaml:"allowedRoles,omitempty"`
	Paginate     bool                      `json:"paginate,omitempty" yaml:"paginate,omitempty"`
	Pagination   *ManifestPaginationConfig `json:"pagination,omitempty" yaml:"pagination,omitempty"`
}

type ManifestConnectionDef struct {
	DisplayName string                             `json:"displayName,omitempty" yaml:"displayName,omitempty"`
	Mode        ConnectionMode                     `json:"mode,omitempty" yaml:"mode,omitempty"`
	Auth        *ProviderAuth                      `json:"auth,omitempty" yaml:"auth,omitempty"`
	Params      map[string]ProviderConnectionParam `json:"params,omitempty" yaml:"params,omitempty"`
	Discovery   *ProviderDiscovery                 `json:"discovery,omitempty" yaml:"discovery,omitempty"`
}

type UIRoute struct {
	Path         string   `json:"path" yaml:"path"`
	AllowedRoles []string `json:"allowedRoles,omitempty" yaml:"allowedRoles,omitempty"`
}

type ProviderOperation struct {
	Name         string              `json:"name" yaml:"name"`
	Description  string              `json:"description,omitempty" yaml:"description,omitempty"`
	Method       string              `json:"method" yaml:"method"`
	Path         string              `json:"path" yaml:"path"`
	AllowedRoles []string            `json:"allowedRoles,omitempty" yaml:"allowedRoles,omitempty"`
	Parameters   []ProviderParameter `json:"parameters,omitempty" yaml:"parameters,omitempty"`
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
	ConnectionModeNone ConnectionMode = "none"
	ConnectionModeUser ConnectionMode = "user"
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

type Entrypoint struct {
	ArtifactPath string   `json:"artifactPath" yaml:"artifactPath"`
	Args         []string `json:"args,omitempty" yaml:"args,omitempty"`
}

type specJSONWire struct {
	ConfigSchemaPath  string                                `json:"configSchemaPath,omitempty"`
	Auth              *RouteAuthRef                         `json:"auth,omitempty"`
	SecuritySchemes   map[string]*HTTPSecurityScheme        `json:"securitySchemes,omitempty"`
	HTTP              map[string]*HTTPBinding               `json:"http,omitempty"`
	MCP               bool                                  `json:"mcp,omitempty"`
	Headers           map[string]string                     `json:"headers,omitempty"`
	ManagedParameters []ManagedParameter                    `json:"managedParameters,omitempty"`
	Surfaces          *ProviderSurfaces                     `json:"surfaces,omitempty"`
	AllowedOperations map[string]*ManifestOperationOverride `json:"allowedOperations,omitempty"`
	DefaultConnection string                                `json:"defaultConnection,omitempty"`
	Connections       map[string]*ManifestConnectionDef     `json:"connections,omitempty"`
	ResponseMapping   *ManifestResponseMapping              `json:"responseMapping,omitempty"`
	Pagination        *ManifestPaginationConfig             `json:"pagination,omitempty"`
	Requires          []string                              `json:"requires,omitempty"`
	UI                *OwnedUI                              `json:"ui,omitempty"`
	AssetRoot         string                                `json:"assetRoot,omitempty"`
	Routes            []UIRoute                             `json:"routes,omitempty"`
}

type specYAMLWire struct {
	ConfigSchemaPath  string                                `yaml:"configSchemaPath,omitempty"`
	Auth              *RouteAuthRef                         `yaml:"auth,omitempty"`
	SecuritySchemes   map[string]*HTTPSecurityScheme        `yaml:"securitySchemes,omitempty"`
	HTTP              map[string]*HTTPBinding               `yaml:"http,omitempty"`
	MCP               bool                                  `yaml:"mcp,omitempty"`
	Headers           map[string]string                     `yaml:"headers,omitempty"`
	ManagedParameters []ManagedParameter                    `yaml:"managedParameters,omitempty"`
	Surfaces          *ProviderSurfaces                     `yaml:"surfaces,omitempty"`
	AllowedOperations map[string]*ManifestOperationOverride `yaml:"allowedOperations,omitempty"`
	DefaultConnection string                                `yaml:"defaultConnection,omitempty"`
	Connections       map[string]*ManifestConnectionDef     `yaml:"connections,omitempty"`
	ResponseMapping   *ManifestResponseMapping              `yaml:"responseMapping,omitempty"`
	Pagination        *ManifestPaginationConfig             `yaml:"pagination,omitempty"`
	Requires          []string                              `yaml:"requires,omitempty"`
	UI                *OwnedUI                              `yaml:"ui,omitempty"`
	AssetRoot         string                                `yaml:"assetRoot,omitempty"`
	Routes            []UIRoute                             `yaml:"routes,omitempty"`
}

type specWire struct {
	ConfigSchemaPath  string                                `json:"configSchemaPath,omitempty" yaml:"configSchemaPath,omitempty"`
	Auth              *RouteAuthRef                         `json:"auth,omitempty" yaml:"auth,omitempty"`
	SecuritySchemes   map[string]*HTTPSecurityScheme        `json:"securitySchemes,omitempty" yaml:"securitySchemes,omitempty"`
	HTTP              map[string]*HTTPBinding               `json:"http,omitempty" yaml:"http,omitempty"`
	MCP               bool                                  `json:"mcp,omitempty" yaml:"mcp,omitempty"`
	Headers           map[string]string                     `json:"headers,omitempty" yaml:"headers,omitempty"`
	ManagedParameters []ManagedParameter                    `json:"managedParameters,omitempty" yaml:"managedParameters,omitempty"`
	Surfaces          *ProviderSurfaces                     `json:"surfaces,omitempty" yaml:"surfaces,omitempty"`
	AllowedOperations map[string]*ManifestOperationOverride `json:"allowedOperations,omitempty" yaml:"allowedOperations,omitempty"`
	DefaultConnection string                                `json:"defaultConnection,omitempty" yaml:"defaultConnection,omitempty"`
	Connections       map[string]*ManifestConnectionDef     `json:"connections,omitempty" yaml:"connections,omitempty"`
	ResponseMapping   *ManifestResponseMapping              `json:"responseMapping,omitempty" yaml:"responseMapping,omitempty"`
	Pagination        *ManifestPaginationConfig             `json:"pagination,omitempty" yaml:"pagination,omitempty"`
	Requires          []string                              `json:"requires,omitempty" yaml:"requires,omitempty"`
	UI                *OwnedUI                              `json:"ui,omitempty" yaml:"ui,omitempty"`
	AssetRoot         string                                `json:"assetRoot,omitempty" yaml:"assetRoot,omitempty"`
	Routes            []UIRoute                             `json:"routes,omitempty" yaml:"routes,omitempty"`
}

func (s *Spec) UnmarshalJSON(data []byte) error {
	if err := validateJSONWireObjectFields(data, specWireFields); err != nil {
		return err
	}

	var raw specJSONWire
	if err := decodeJSONKnownFields(data, &raw); err != nil {
		return err
	}

	spec := Spec{
		ConfigSchemaPath:  raw.ConfigSchemaPath,
		RouteAuth:         raw.Auth,
		SecuritySchemes:   cloneHTTPSecuritySchemes(raw.SecuritySchemes),
		HTTP:              cloneHTTPBindings(raw.HTTP),
		MCP:               raw.MCP,
		Headers:           raw.Headers,
		ManagedParameters: raw.ManagedParameters,
		Surfaces:          raw.Surfaces,
		AllowedOperations: raw.AllowedOperations,
		DefaultConnection: raw.DefaultConnection,
		Connections:       raw.Connections,
		ResponseMapping:   raw.ResponseMapping,
		Pagination:        raw.Pagination,
		Requires:          raw.Requires,
		UI:                raw.UI,
		AssetRoot:         raw.AssetRoot,
		Routes:            raw.Routes,
	}

	*s = spec
	return nil
}

func (s Spec) MarshalJSON() ([]byte, error) {
	wire, err := s.canonicalWire()
	if err != nil {
		return nil, err
	}
	return json.Marshal(wire)
}

func (s *Spec) UnmarshalYAML(value *yaml.Node) error {
	if value == nil {
		*s = Spec{}
		return nil
	}
	if value.Kind != yaml.MappingNode {
		return fmt.Errorf("spec must be a mapping")
	}
	if err := validateYAMLWireObjectFields(value, specWireFields, "spec"); err != nil {
		return err
	}

	var raw specYAMLWire
	if err := decodeYAMLKnownFields(value, &raw); err != nil {
		return err
	}

	spec := Spec{
		ConfigSchemaPath:  raw.ConfigSchemaPath,
		RouteAuth:         raw.Auth,
		SecuritySchemes:   cloneHTTPSecuritySchemes(raw.SecuritySchemes),
		HTTP:              cloneHTTPBindings(raw.HTTP),
		MCP:               raw.MCP,
		Headers:           raw.Headers,
		ManagedParameters: raw.ManagedParameters,
		Surfaces:          raw.Surfaces,
		AllowedOperations: raw.AllowedOperations,
		DefaultConnection: raw.DefaultConnection,
		Connections:       raw.Connections,
		ResponseMapping:   raw.ResponseMapping,
		Pagination:        raw.Pagination,
		Requires:          raw.Requires,
		UI:                raw.UI,
		AssetRoot:         raw.AssetRoot,
		Routes:            raw.Routes,
	}

	*s = spec
	return nil
}

func (s *Spec) MarshalYAML() (any, error) {
	if s == nil {
		return nil, nil
	}
	return s.canonicalWire()
}

func (s Spec) canonicalWire() (specWire, error) {
	return specWire{
		ConfigSchemaPath:  s.ConfigSchemaPath,
		Auth:              s.RouteAuth,
		SecuritySchemes:   cloneHTTPSecuritySchemes(s.SecuritySchemes),
		HTTP:              cloneHTTPBindings(s.HTTP),
		MCP:               s.MCP,
		Headers:           s.Headers,
		ManagedParameters: s.ManagedParameters,
		Surfaces:          s.Surfaces,
		AllowedOperations: s.AllowedOperations,
		DefaultConnection: s.DefaultConnection,
		Connections:       cloneManifestConnections(s.Connections),
		ResponseMapping:   s.ResponseMapping,
		Pagination:        s.Pagination,
		Requires:          s.Requires,
		UI:                s.UI,
		AssetRoot:         s.AssetRoot,
		Routes:            s.Routes,
	}, nil
}

func cloneManifestConnections(src map[string]*ManifestConnectionDef) map[string]*ManifestConnectionDef {
	if src == nil {
		return nil
	}
	cloned := make(map[string]*ManifestConnectionDef, len(src))
	for name, def := range src {
		if def == nil {
			cloned[name] = nil
			continue
		}
		copyDef := *def
		cloned[name] = &copyDef
	}
	return cloned
}

func cloneHTTPSecuritySchemes(src map[string]*HTTPSecurityScheme) map[string]*HTTPSecurityScheme {
	if src == nil {
		return nil
	}
	cloned := make(map[string]*HTTPSecurityScheme, len(src))
	for name, scheme := range src {
		cloned[name] = cloneHTTPSecurityScheme(scheme)
	}
	return cloned
}

func cloneHTTPSecurityScheme(src *HTTPSecurityScheme) *HTTPSecurityScheme {
	if src == nil {
		return nil
	}
	cloned := *src
	cloned.Secret = cloneHTTPSecretRef(src.Secret)
	return &cloned
}

func cloneHTTPSecretRef(src *HTTPSecretRef) *HTTPSecretRef {
	if src == nil {
		return nil
	}
	cloned := *src
	return &cloned
}

func cloneHTTPBindings(src map[string]*HTTPBinding) map[string]*HTTPBinding {
	if src == nil {
		return nil
	}
	cloned := make(map[string]*HTTPBinding, len(src))
	for name, binding := range src {
		cloned[name] = cloneHTTPBinding(binding)
	}
	return cloned
}

func cloneHTTPBinding(src *HTTPBinding) *HTTPBinding {
	if src == nil {
		return nil
	}
	cloned := *src
	cloned.RequestBody = cloneHTTPRequestBody(src.RequestBody)
	cloned.Ack = cloneHTTPAck(src.Ack)
	return &cloned
}

func cloneHTTPRequestBody(src *HTTPRequestBody) *HTTPRequestBody {
	if src == nil {
		return nil
	}
	cloned := *src
	if src.Content != nil {
		cloned.Content = make(map[string]*HTTPMediaType, len(src.Content))
		for name, mediaType := range src.Content {
			cloned.Content[name] = cloneHTTPMediaType(mediaType)
		}
	}
	return &cloned
}

func cloneHTTPMediaType(src *HTTPMediaType) *HTTPMediaType {
	if src == nil {
		return nil
	}
	cloned := *src
	return &cloned
}

func cloneHTTPAck(src *HTTPAck) *HTTPAck {
	if src == nil {
		return nil
	}
	cloned := *src
	if src.Headers != nil {
		cloned.Headers = make(map[string]string, len(src.Headers))
		for name, value := range src.Headers {
			cloned.Headers[name] = value
		}
	}
	cloned.Body = cloneHTTPBodyValue(src.Body)
	return &cloned
}

func cloneHTTPBodyValue(src any) any {
	if src == nil {
		return nil
	}
	return cloneHTTPBodyReflectValue(reflect.ValueOf(src)).Interface()
}

func cloneHTTPBodyReflectValue(value reflect.Value) reflect.Value {
	if !value.IsValid() {
		return reflect.Value{}
	}
	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		return cloneHTTPBodyReflectValue(value.Elem())
	case reflect.Pointer:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		cloned := reflect.New(value.Type().Elem())
		cloned.Elem().Set(cloneHTTPBodyReflectValue(value.Elem()))
		return cloned
	case reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		cloned := reflect.MakeMapWithSize(value.Type(), value.Len())
		iter := value.MapRange()
		for iter.Next() {
			cloned.SetMapIndex(cloneHTTPBodyReflectValue(iter.Key()), cloneHTTPBodyReflectValue(iter.Value()))
		}
		return cloned
	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		cloned := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for i := 0; i < value.Len(); i++ {
			cloned.Index(i).Set(cloneHTTPBodyReflectValue(value.Index(i)))
		}
		return cloned
	case reflect.Array:
		cloned := reflect.New(value.Type()).Elem()
		for i := 0; i < value.Len(); i++ {
			cloned.Index(i).Set(cloneHTTPBodyReflectValue(value.Index(i)))
		}
		return cloned
	default:
		return value
	}
}

func decodeJSONKnownFields(data []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	return nil
}

func decodeYAMLKnownFields(node *yaml.Node, out any) error {
	data, err := yaml.Marshal(node)
	if err != nil {
		return err
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil && err != io.EOF {
		return err
	}
	return nil
}

func validateJSONWireObjectFields(data []byte, allowed map[string]struct{}) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for name := range fields {
		if _, ok := allowed[name]; !ok {
			return fmt.Errorf("json: unknown field %q", name)
		}
	}
	return nil
}

func validateYAMLWireObjectFields(node *yaml.Node, allowed map[string]struct{}, subject string) error {
	for i := 0; i+1 < len(node.Content); i += 2 {
		name := node.Content[i].Value
		if _, ok := allowed[name]; !ok {
			return fmt.Errorf("%s.%s is not supported", subject, name)
		}
	}
	return nil
}

var specWireFields = map[string]struct{}{
	"configSchemaPath":  {},
	"auth":              {},
	"securitySchemes":   {},
	"http":              {},
	"mcp":               {},
	"headers":           {},
	"managedParameters": {},
	"surfaces":          {},
	"allowedOperations": {},
	"defaultConnection": {},
	"connections":       {},
	"responseMapping":   {},
	"pagination":        {},
	"requires":          {},
	"ui":                {},
	"assetRoot":         {},
	"routes":            {},
}

//go:embed manifest.jsonschema.json
var ManifestJSONSchema []byte
