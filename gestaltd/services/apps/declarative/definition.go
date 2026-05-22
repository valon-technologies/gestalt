package declarative

import (
	"encoding/json"

	"github.com/valon-technologies/gestalt/server/core"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

type Definition struct {
	Provider         string            `yaml:"provider" json:"provider"`
	DisplayName      string            `yaml:"displayName" json:"displayName"`
	Description      string            `yaml:"description" json:"description"`
	IconSVG          string            `yaml:"iconSvg" json:"iconSvg"`
	ConnectionMode   string            `yaml:"connectionMode" json:"connectionMode"`
	BaseURL          string            `yaml:"baseUrl" json:"baseUrl"`
	Auth             AuthDef           `yaml:"auth" json:"auth"`
	AuthStyle        string            `yaml:"authStyle" json:"authStyle"` // bearer (default), raw, none, basic
	AuthHeader       string            `yaml:"authHeader" json:"authHeader"`
	TokenPrefix      string            `yaml:"tokenPrefix" json:"tokenPrefix"`
	Headers          map[string]string `yaml:"headers" json:"headers"`
	AuthMapping      *AuthMappingDef   `yaml:"authMapping" json:"authMapping"`
	ErrorMessagePath string            `yaml:"errorMessagePath" json:"errorMessagePath"`

	ResponseCheck    *ResponseCheckDef    `yaml:"responseCheck" json:"responseCheck,omitempty"`
	ManualAuth       bool                 `yaml:"manualAuth" json:"manualAuth"`
	CredentialFields []CredentialFieldDef `yaml:"credentialFields" json:"credentialFields,omitempty"`

	Discovery       *DiscoveryDef       `yaml:"discovery" json:"discovery,omitempty"`
	ResponseMapping *ResponseMappingDef `yaml:"responseMapping" json:"responseMapping,omitempty"`

	Connection map[string]ConnectionParamDef `yaml:"connection" json:"connection"`
	Operations map[string]OperationDef       `yaml:"operations" json:"operations"`
}

type ResponseCheckDef struct {
	SuccessBodyMatch map[string]any `yaml:"successBodyMatch" json:"successBodyMatch,omitempty"`
	ErrorMessagePath string         `yaml:"errorMessagePath" json:"errorMessagePath,omitempty"`
}

type DiscoveryDef struct {
	URL       string            `yaml:"url" json:"url"`
	ItemsPath string            `yaml:"itemsPath" json:"itemsPath"`
	IDPath    string            `yaml:"idPath" json:"idPath"`
	NamePath  string            `yaml:"namePath" json:"namePath"`
	Metadata  map[string]string `yaml:"metadata" json:"metadata"`
}

func (d *DiscoveryDef) ToCore() *core.DiscoveryConfig {
	return &core.DiscoveryConfig{
		URL:       d.URL,
		ItemsPath: d.ItemsPath,
		IDPath:    d.IDPath,
		NamePath:  d.NamePath,
		Metadata:  d.Metadata,
	}
}

type ConnectionParamDef struct {
	Required    bool   `yaml:"required" json:"required"`
	Description string `yaml:"description" json:"description"`
	Default     string `yaml:"default" json:"default"`
	From        string `yaml:"from" json:"from"`   // "" = user-provided, "token_response" = from OAuth response, "discovery" = from post-connect discovery
	Field       string `yaml:"field" json:"field"` // JSON field name for token_response extraction
}

type AuthDef struct {
	Type                string            `yaml:"type" json:"type"` // oauth2, manual
	AuthorizationURL    string            `yaml:"authorizationUrl" json:"authorizationUrl"`
	TokenURL            string            `yaml:"tokenUrl" json:"tokenUrl"`
	ClientAuth          string            `yaml:"clientAuth" json:"clientAuth"`       // body (default), header
	TokenExchange       string            `yaml:"tokenExchange" json:"tokenExchange"` // form (default), json
	Scopes              []string          `yaml:"scopes" json:"scopes"`
	ScopeParam          string            `yaml:"scopeParam" json:"scopeParam"`
	ScopeSeparator      string            `yaml:"scopeSeparator" json:"scopeSeparator"`
	PKCE                bool              `yaml:"pkce" json:"pkce"`
	AuthorizationParams map[string]string `yaml:"authorizationParams" json:"authorizationParams"`
	TokenParams         map[string]string `yaml:"tokenParams" json:"tokenParams"`
	RefreshParams       map[string]string `yaml:"refreshParams" json:"refreshParams"`
	AcceptHeader        string            `yaml:"acceptHeader" json:"acceptHeader"`
	TokenMetadata       []string          `yaml:"tokenMetadata" json:"tokenMetadata"`
	AccessTokenPath     string            `yaml:"accessTokenPath" json:"accessTokenPath,omitempty"`
	ResponseCheck       *ResponseCheckDef `yaml:"responseCheck" json:"responseCheck,omitempty"`
}

type OperationDef struct {
	Description  string          `yaml:"description" json:"description"`
	Method       string          `yaml:"method" json:"method"`
	Path         string          `yaml:"path" json:"path"`
	AllowedRoles []string        `yaml:"allowedRoles,omitempty" json:"allowedRoles,omitempty"`
	Tags         []string        `yaml:"tags,omitempty" json:"tags,omitempty"`
	Parameters   []ParameterDef  `yaml:"parameters" json:"parameters"`
	Query        string          `yaml:"query" json:"query"`                       // GraphQL query/mutation template
	Transport    string          `yaml:"transport" json:"transport"`               // "rest" (default) or "graphql"
	InputSchema  json.RawMessage `yaml:"inputSchema" json:"inputSchema,omitempty"` // pre-built JSON Schema (skips synthesis)
	Pagination   *PaginationDef  `yaml:"pagination" json:"pagination"`
}

type AuthMappingDef = providermanifestv1.AuthMapping
type BasicAuthMappingDef = providermanifestv1.BasicAuthMapping
type AuthValueDef = providermanifestv1.AuthValue
type AuthValueFromDef = providermanifestv1.AuthValueFrom
type CredentialFieldRefDef = providermanifestv1.CredentialFieldRef

type ValueSelectorDef struct {
	Source string `yaml:"source" json:"source"`
	Path   string `yaml:"path" json:"path"`
}

type PaginationDef struct {
	Style        string            `yaml:"style" json:"style"`
	CursorParam  string            `yaml:"cursorParam" json:"cursorParam"`
	Cursor       *ValueSelectorDef `yaml:"cursor" json:"cursor,omitempty"`
	LimitParam   string            `yaml:"limitParam" json:"limitParam"`
	DefaultLimit int               `yaml:"defaultLimit" json:"defaultLimit"`
	ResultsPath  string            `yaml:"resultsPath" json:"resultsPath"`
	MaxPages     int               `yaml:"maxPages" json:"maxPages"`
}

type CredentialFieldDef = providermanifestv1.CredentialField

type ResponseMappingDef struct {
	DataPath   string                `yaml:"dataPath" json:"dataPath"`
	Pagination *PaginationMappingDef `yaml:"pagination" json:"pagination,omitempty"`
}

type PaginationMappingDef struct {
	HasMore *ValueSelectorDef `yaml:"hasMore" json:"hasMore,omitempty"`
	Cursor  *ValueSelectorDef `yaml:"cursor" json:"cursor,omitempty"`
}

type ParameterDef struct {
	Name        string `yaml:"name" json:"name"`
	WireName    string `yaml:"wireName,omitempty" json:"wireName,omitempty"`
	Type        string `yaml:"type" json:"type"`
	Location    string `yaml:"location,omitempty" json:"location,omitempty"`
	Description string `yaml:"description" json:"description"`
	Required    bool   `yaml:"required" json:"required"`
	Default     any    `yaml:"default" json:"default"`
}
