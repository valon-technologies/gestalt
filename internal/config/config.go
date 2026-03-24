package config

import (
	"fmt"
	"io"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Callback paths must match the routes registered in server.go.
const (
	AuthCallbackPath        = "/api/v1/auth/login/callback"
	IntegrationCallbackPath = "/api/v1/auth/callback"
)

type Config struct {
	Auth         AuthConfig                `yaml:"auth"`
	Datastore    DatastoreConfig           `yaml:"datastore"`
	Secrets      SecretsConfig             `yaml:"secrets"`
	AuthProfiles map[string]AuthProfile    `yaml:"auth_profiles"`
	Integrations map[string]IntegrationDef `yaml:"integrations"`
	Runtimes     map[string]RuntimeDef     `yaml:"runtimes"`
	Bindings     map[string]BindingDef     `yaml:"bindings"`
	ProviderDirs []string                  `yaml:"provider_dirs"`
	Server       ServerConfig              `yaml:"server"`
}

type RuntimeDef struct {
	Type      string    `yaml:"type"`
	Providers []string  `yaml:"providers"`
	Config    yaml.Node `yaml:"config"`
}

type BindingDef struct {
	Type      string    `yaml:"type"`
	Providers []string  `yaml:"providers"`
	Config    yaml.Node `yaml:"config"`
}

type SecretsConfig struct {
	Provider string    `yaml:"provider"`
	Config   yaml.Node `yaml:"config"`
}

type AuthProfile struct {
	ClientID     string        `yaml:"client_id"`
	ClientSecret string        `yaml:"client_secret"`
	RedirectURL  string        `yaml:"redirect_url"`
	Auth         AuthOverrides `yaml:"auth"`
}

type AuthConfig struct {
	Provider string    `yaml:"provider"`
	Config   yaml.Node `yaml:"config"`
}

type DatastoreConfig struct {
	Provider string    `yaml:"provider"`
	Config   yaml.Node `yaml:"config"`
}

type ServerConfig struct {
	Port          int    `yaml:"port"`
	BaseURL       string `yaml:"base_url"`
	EncryptionKey string `yaml:"encryption_key"`
	DevMode       bool   `yaml:"dev_mode"`
}

type IntegrationDef struct {
	Upstreams      []UpstreamDef `yaml:"upstreams"`
	DisplayName    string        `yaml:"display_name"`
	Description    string        `yaml:"description"`
	AuthProfile    string        `yaml:"auth_profile"`
	ConnectionMode string        `yaml:"connection_mode"`
	MCPToolPrefix  string        `yaml:"mcp_tool_prefix"`

	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
	RedirectURL  string `yaml:"redirect_url"`
	BaseURL      string `yaml:"base_url"`

	Auth AuthOverrides `yaml:"auth"`

	AuthHeader       string            `yaml:"auth_header"`
	AuthMapping      *AuthMappingDef   `yaml:"auth_mapping"`
	ErrorMessagePath string            `yaml:"error_message_path"`
	ResponseCheck    string            `yaml:"response_check"`
	TokenParser      string            `yaml:"token_parser"`
	RequestMutator   string            `yaml:"request_mutator"`
	PostConnect      string            `yaml:"post_connect"`
	TokenPrefix      string            `yaml:"token_prefix"`
	AuthStyle        string            `yaml:"auth_style"`
	IconFile         string            `yaml:"icon_file"`
	Headers          map[string]string `yaml:"headers"`
}

const (
	UpstreamTypeREST    = "rest"
	UpstreamTypeGraphQL = "graphql"
	UpstreamTypeMCP     = "mcp"
)

type UpstreamDef struct {
	Type              string     `yaml:"type"`
	URL               string     `yaml:"url"`
	Provider          string     `yaml:"provider"`
	MCP               bool       `yaml:"mcp"`
	AllowedOperations AllowedOps `yaml:"allowed_operations"`
}

// AllowedOps is a map of operation name to optional description override.
// It can be unmarshaled from either a YAML list (names only, descriptions
// from upstream spec) or a YAML map (names to description overrides).
type AllowedOps map[string]string

func (a *AllowedOps) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.SequenceNode:
		*a = make(AllowedOps, len(value.Content))
		for _, item := range value.Content {
			(*a)[item.Value] = ""
		}
	case yaml.MappingNode:
		m := make(map[string]string)
		if err := value.Decode(&m); err != nil {
			return err
		}
		*a = m
	default:
		return fmt.Errorf("allowed_operations must be a list or map")
	}
	return nil
}

type AuthOverrides struct {
	Type                string            `yaml:"type"`
	AuthorizationURL    string            `yaml:"authorization_url"`
	TokenURL            string            `yaml:"token_url"`
	ClientAuth          string            `yaml:"client_auth"`
	TokenExchange       string            `yaml:"token_exchange"`
	Scopes              []string          `yaml:"scopes"`
	ScopeSeparator      string            `yaml:"scope_separator"`
	PKCE                bool              `yaml:"pkce"`
	AuthorizationParams map[string]string `yaml:"authorization_params"`
	TokenParams         map[string]string `yaml:"token_params"`
	RefreshParams       map[string]string `yaml:"refresh_params"`
	AcceptHeader        string            `yaml:"accept_header"`
	TokenMetadata       []string          `yaml:"token_metadata"`
	ResponseHook        string            `yaml:"response_hook"`
	AuthHeader          string            `yaml:"auth_header"`
}

type AuthMappingDef struct {
	Headers map[string]string `yaml:"headers"`
}

func Load(path string) (*Config, error) {
	return LoadWithMapping(path, os.Getenv)
}

func LoadWithMapping(path string, getenv func(string) string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	resolved := os.Expand(string(data), getenv)

	var cfg Config
	dec := yaml.NewDecoder(strings.NewReader(resolved))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil && err != io.EOF {
		return nil, fmt.Errorf("parsing config YAML: %w", err)
	}

	applyDefaults(&cfg)

	if err := resolveAuthProfiles(&cfg); err != nil {
		return nil, err
	}

	resolveBaseURL(&cfg) // after resolveAuthProfiles so inherited fields take priority

	if err := validate(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.Secrets.Provider == "" {
		cfg.Secrets.Provider = "env"
	}
}

func resolveAuthProfiles(cfg *Config) error {
	for name := range cfg.Integrations {
		intg := cfg.Integrations[name]
		if intg.AuthProfile == "" {
			continue
		}
		profile, ok := cfg.AuthProfiles[intg.AuthProfile]
		if !ok {
			return fmt.Errorf("integration %q references unknown auth_profile %q", name, intg.AuthProfile)
		}
		if intg.ClientID == "" {
			intg.ClientID = profile.ClientID
		}
		if intg.ClientSecret == "" {
			intg.ClientSecret = profile.ClientSecret
		}
		if intg.RedirectURL == "" {
			intg.RedirectURL = profile.RedirectURL
		}
		if intg.Auth.Type == "" {
			intg.Auth.Type = profile.Auth.Type
		}
		if intg.Auth.AuthorizationURL == "" {
			intg.Auth.AuthorizationURL = profile.Auth.AuthorizationURL
		}
		if intg.Auth.TokenURL == "" {
			intg.Auth.TokenURL = profile.Auth.TokenURL
		}
		if intg.Auth.ClientAuth == "" {
			intg.Auth.ClientAuth = profile.Auth.ClientAuth
		}
		if intg.Auth.TokenExchange == "" {
			intg.Auth.TokenExchange = profile.Auth.TokenExchange
		}
		if !intg.Auth.PKCE && profile.Auth.PKCE {
			intg.Auth.PKCE = true
		}
		if intg.Auth.AuthorizationParams == nil {
			intg.Auth.AuthorizationParams = profile.Auth.AuthorizationParams
		}
		if intg.Auth.TokenParams == nil {
			intg.Auth.TokenParams = profile.Auth.TokenParams
		}
		if intg.Auth.RefreshParams == nil {
			intg.Auth.RefreshParams = profile.Auth.RefreshParams
		}
		if intg.Auth.Scopes == nil {
			intg.Auth.Scopes = profile.Auth.Scopes
		}
		if intg.Auth.ScopeSeparator == "" {
			intg.Auth.ScopeSeparator = profile.Auth.ScopeSeparator
		}
		if intg.Auth.AcceptHeader == "" {
			intg.Auth.AcceptHeader = profile.Auth.AcceptHeader
		}
		if intg.Auth.TokenMetadata == nil {
			intg.Auth.TokenMetadata = profile.Auth.TokenMetadata
		}
		if intg.Auth.ResponseHook == "" {
			intg.Auth.ResponseHook = profile.Auth.ResponseHook
		}
		if intg.Auth.AuthHeader == "" {
			intg.Auth.AuthHeader = profile.Auth.AuthHeader
		}
		cfg.Integrations[name] = intg
	}
	return nil
}

func resolveBaseURL(cfg *Config) {
	base := strings.TrimRight(cfg.Server.BaseURL, "/")
	if base == "" {
		return
	}
	cfg.Server.BaseURL = base

	for name := range cfg.AuthProfiles {
		p := cfg.AuthProfiles[name]
		if p.RedirectURL == "" {
			p.RedirectURL = base + IntegrationCallbackPath
			cfg.AuthProfiles[name] = p
		}
	}

	for name := range cfg.Integrations {
		intg := cfg.Integrations[name]
		if intg.RedirectURL == "" {
			intg.RedirectURL = base + IntegrationCallbackPath
			cfg.Integrations[name] = intg
		}
	}
}

func validate(cfg *Config) error {
	if cfg.Auth.Provider == "" {
		return fmt.Errorf("config validation: auth.provider is required")
	}
	if cfg.Datastore.Provider == "" {
		return fmt.Errorf("config validation: datastore.provider is required")
	}
	if !cfg.Server.DevMode && cfg.Server.EncryptionKey == "" {
		return fmt.Errorf("config validation: server.encryption_key is required (set server.dev_mode to true to skip)")
	}
	return nil
}
