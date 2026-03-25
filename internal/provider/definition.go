package provider

import (
	"encoding/json"

	"github.com/valon-technologies/gestalt/core"
)

type Definition struct {
	Provider         string            `yaml:"provider" json:"provider"`
	DisplayName      string            `yaml:"display_name" json:"display_name"`
	Description      string            `yaml:"description" json:"description"`
	IconSVG          string            `yaml:"icon_svg" json:"icon_svg"`
	ConnectionMode   string            `yaml:"connection_mode" json:"connection_mode"`
	BaseURL          string            `yaml:"base_url" json:"base_url"`
	Auth             AuthDef           `yaml:"auth" json:"auth"`
	AuthStyle        string            `yaml:"auth_style" json:"auth_style"` // bearer (default), raw, none, basic
	AuthHeader       string            `yaml:"auth_header" json:"auth_header"`
	TokenPrefix      string            `yaml:"token_prefix" json:"token_prefix"`
	Headers          map[string]string `yaml:"headers" json:"headers"`
	AuthMapping      *AuthMappingDef   `yaml:"auth_mapping" json:"auth_mapping"`
	ErrorMessagePath string            `yaml:"error_message_path" json:"error_message_path"`

	ResponseCheck *ResponseCheckDef `yaml:"response_check" json:"response_check,omitempty"`
	ManualAuth    bool              `yaml:"manual_auth" json:"manual_auth"`

	PostConnectDiscovery *PostConnectDiscoveryDef `yaml:"post_connect_discovery" json:"post_connect_discovery,omitempty"`

	Connection map[string]ConnectionParamDef `yaml:"connection" json:"connection"`
	Operations map[string]OperationDef       `yaml:"operations" json:"operations"`
}

type ResponseCheckDef struct {
	SuccessBodyMatch map[string]any `yaml:"success_body_match" json:"success_body_match,omitempty"`
	ErrorMessagePath string         `yaml:"error_message_path" json:"error_message_path,omitempty"`
}

type PostConnectDiscoveryDef struct {
	URL             string            `yaml:"url" json:"url"`
	ItemsPath       string            `yaml:"items_path" json:"items_path"`
	IDPath          string            `yaml:"id_path" json:"id_path"`
	NamePath        string            `yaml:"name_path" json:"name_path"`
	MetadataMapping map[string]string `yaml:"metadata_mapping" json:"metadata_mapping"`
}

func (d *PostConnectDiscoveryDef) ToCore() *core.DiscoveryConfig {
	return &core.DiscoveryConfig{
		URL:             d.URL,
		ItemsPath:       d.ItemsPath,
		IDPath:          d.IDPath,
		NamePath:        d.NamePath,
		MetadataMapping: d.MetadataMapping,
	}
}

type ConnectionParamDef struct {
	Required    bool   `yaml:"required" json:"required"`
	Description string `yaml:"description" json:"description"`
	Default     string `yaml:"default" json:"default"`
	From        string `yaml:"from" json:"from"`   // "" = user-provided, "token_response" = from OAuth response
	Field       string `yaml:"field" json:"field"` // JSON field name for token_response extraction
}

type AuthDef struct {
	Type                string            `yaml:"type" json:"type"` // oauth2, manual
	AuthorizationURL    string            `yaml:"authorization_url" json:"authorization_url"`
	TokenURL            string            `yaml:"token_url" json:"token_url"`
	ClientAuth          string            `yaml:"client_auth" json:"client_auth"`       // body (default), header
	TokenExchange       string            `yaml:"token_exchange" json:"token_exchange"` // form (default), json
	Scopes              []string          `yaml:"scopes" json:"scopes"`
	ScopeSeparator      string            `yaml:"scope_separator" json:"scope_separator"`
	PKCE                bool              `yaml:"pkce" json:"pkce"`
	AuthorizationParams map[string]string `yaml:"authorization_params" json:"authorization_params"`
	TokenParams         map[string]string `yaml:"token_params" json:"token_params"`
	RefreshParams       map[string]string `yaml:"refresh_params" json:"refresh_params"`
	AcceptHeader        string            `yaml:"accept_header" json:"accept_header"`
	TokenMetadata       []string          `yaml:"token_metadata" json:"token_metadata"`
	ResponseCheck       *ResponseCheckDef `yaml:"response_check" json:"response_check,omitempty"`
}

type OperationDef struct {
	Description string          `yaml:"description" json:"description"`
	Method      string          `yaml:"method" json:"method"`
	Path        string          `yaml:"path" json:"path"`
	Parameters  []ParameterDef  `yaml:"parameters" json:"parameters"`
	Query       string          `yaml:"query" json:"query"`                         // GraphQL query/mutation template
	Transport   string          `yaml:"transport" json:"transport"`                 // "rest" (default) or "graphql"
	InputSchema json.RawMessage `yaml:"input_schema" json:"input_schema,omitempty"` // pre-built JSON Schema (skips synthesis)
	Pagination  *PaginationDef  `yaml:"pagination" json:"pagination"`
}

type AuthMappingDef struct {
	Headers map[string]string `yaml:"headers" json:"headers"`
}

type PaginationDef struct {
	Style        string `yaml:"style" json:"style"`
	CursorParam  string `yaml:"cursor_param" json:"cursor_param"`
	CursorPath   string `yaml:"cursor_path" json:"cursor_path"`
	LimitParam   string `yaml:"limit_param" json:"limit_param"`
	DefaultLimit int    `yaml:"default_limit" json:"default_limit"`
	ResultsPath  string `yaml:"results_path" json:"results_path"`
	MaxPages     int    `yaml:"max_pages" json:"max_pages"`
}

type ParameterDef struct {
	Name        string `yaml:"name" json:"name"`
	Type        string `yaml:"type" json:"type"`
	Description string `yaml:"description" json:"description"`
	Required    bool   `yaml:"required" json:"required"`
	Default     any    `yaml:"default" json:"default"`
}
