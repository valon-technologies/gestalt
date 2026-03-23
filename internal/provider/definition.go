package provider

import "encoding/json"

type Definition struct {
	Provider         string            `yaml:"provider"`
	DisplayName      string            `yaml:"display_name"`
	Description      string            `yaml:"description"`
	IconSVG          string            `yaml:"icon_svg"`
	ConnectionMode   string            `yaml:"connection_mode"`
	BaseURL          string            `yaml:"base_url"`
	Auth             AuthDef           `yaml:"auth"`
	AuthStyle        string            `yaml:"auth_style"` // bearer (default), raw, none, basic
	AuthHeader       string            `yaml:"auth_header"`
	TokenPrefix      string            `yaml:"token_prefix"`
	Headers          map[string]string `yaml:"headers"`
	AuthMapping      *AuthMappingDef   `yaml:"auth_mapping"`
	ErrorMessagePath string            `yaml:"error_message_path"`

	ResponseCheck  string `yaml:"response_check"`
	TokenParser    string `yaml:"token_parser"`
	RequestMutator string `yaml:"request_mutator"`

	Connection map[string]ConnectionParamDef `yaml:"connection"`
	Operations map[string]OperationDef       `yaml:"operations"`
}

type ConnectionParamDef struct {
	Required    bool   `yaml:"required"`
	Description string `yaml:"description"`
	Default     string `yaml:"default"`
	From        string `yaml:"from"`  // "" = user-provided, "token_response" = from OAuth response
	Field       string `yaml:"field"` // JSON field name for token_response extraction
}

type AuthDef struct {
	Type                string            `yaml:"type"` // oauth2, manual
	AuthorizationURL    string            `yaml:"authorization_url"`
	TokenURL            string            `yaml:"token_url"`
	ClientAuth          string            `yaml:"client_auth"`    // body (default), header
	TokenExchange       string            `yaml:"token_exchange"` // form (default), json
	Scopes              []string          `yaml:"scopes"`
	ScopeSeparator      string            `yaml:"scope_separator"`
	PKCE                bool              `yaml:"pkce"`
	AuthorizationParams map[string]string `yaml:"authorization_params"`
	TokenParams         map[string]string `yaml:"token_params"`
	RefreshParams       map[string]string `yaml:"refresh_params"`
	AcceptHeader        string            `yaml:"accept_header"`
	TokenMetadata       []string          `yaml:"token_metadata"`
	ResponseHook        string            `yaml:"response_hook"`
}

type OperationDef struct {
	Description string          `yaml:"description"`
	Method      string          `yaml:"method"`
	Path        string          `yaml:"path"`
	Parameters  []ParameterDef  `yaml:"parameters"`
	Query       string          `yaml:"query"`        // GraphQL query/mutation template
	Transport   string          `yaml:"transport"`    // "rest" (default) or "graphql"
	InputSchema json.RawMessage `yaml:"input_schema"` // pre-built JSON Schema (skips synthesis)
	Pagination  *PaginationDef  `yaml:"pagination"`
}

type AuthMappingDef struct {
	Headers map[string]string `yaml:"headers"`
}

type PaginationDef struct {
	Style        string `yaml:"style"`
	CursorParam  string `yaml:"cursor_param"`
	CursorPath   string `yaml:"cursor_path"`
	LimitParam   string `yaml:"limit_param"`
	DefaultLimit int    `yaml:"default_limit"`
	ResultsPath  string `yaml:"results_path"`
	MaxPages     int    `yaml:"max_pages"`
}

type ParameterDef struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	Description string `yaml:"description"`
	Required    bool   `yaml:"required"`
	Default     any    `yaml:"default"`
}
