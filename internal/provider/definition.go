package provider

type Definition struct {
	Provider       string            `yaml:"provider"`
	DisplayName    string            `yaml:"display_name"`
	Description    string            `yaml:"description"`
	IconSVG        string            `yaml:"icon_svg"`
	ConnectionMode string            `yaml:"connection_mode"`
	BaseURL        string            `yaml:"base_url"`
	Auth           AuthDef           `yaml:"auth"`
	AuthStyle      string            `yaml:"auth_style"` // bearer (default), raw, none
	TokenPrefix    string            `yaml:"token_prefix"`
	Headers        map[string]string `yaml:"headers"`

	ResponseCheck  string `yaml:"response_check"`
	TokenParser    string `yaml:"token_parser"`
	RequestMutator string `yaml:"request_mutator"`

	Operations map[string]OperationDef `yaml:"operations"`
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
	Description string         `yaml:"description"`
	Method      string         `yaml:"method"`
	Path        string         `yaml:"path"`
	Parameters  []ParameterDef `yaml:"parameters"`
	Query       string         `yaml:"query"`     // GraphQL query/mutation template
	Transport   string         `yaml:"transport"` // "rest" (default) or "graphql"
	Pagination  *PaginationDef `yaml:"pagination"`
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
