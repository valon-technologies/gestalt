package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	Server       ServerConfig              `yaml:"server"`
}

const (
	PluginModeReplace = "replace"
	PluginModeOverlay = "overlay"
)

type ExecutablePluginDef struct {
	Mode    string            `yaml:"mode"`
	Command string            `yaml:"command"`
	Args    []string          `yaml:"args"`
	Env     map[string]string `yaml:"env"`
}

type RuntimeDef struct {
	Type      string               `yaml:"type"`
	Providers []string             `yaml:"providers"`
	Config    yaml.Node            `yaml:"config"`
	Plugin    *ExecutablePluginDef `yaml:"plugin"`
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
	Upstreams      []UpstreamDef        `yaml:"upstreams"`
	DisplayName    string               `yaml:"display_name"`
	Description    string               `yaml:"description"`
	AuthProfile    string               `yaml:"auth_profile"`
	ConnectionMode string               `yaml:"connection_mode"`
	MCPToolPrefix  string               `yaml:"mcp_tool_prefix"`
	Plugin         *ExecutablePluginDef `yaml:"plugin"`

	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
	RedirectURL  string `yaml:"redirect_url"`
	BaseURL      string `yaml:"base_url"`

	Auth AuthOverrides `yaml:"auth"`

	AuthHeader       string            `yaml:"auth_header"`
	AuthMapping      *AuthMappingDef   `yaml:"auth_mapping"`
	ErrorMessagePath string            `yaml:"error_message_path"`
	ResponseCheck    *ResponseCheckDef `yaml:"response_check"`
	RequestMutator   string            `yaml:"request_mutator"`
	PostConnect      string            `yaml:"post_connect"`
	ManualAuth       bool              `yaml:"manual_auth"`
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
	MCP               bool       `yaml:"mcp"`
	AllowedOperations AllowedOps `yaml:"allowed_operations"`

	// Per-upstream auth overrides. When set, these take precedence over
	// integration-level auth for this upstream. When empty, the
	// integration-level values are inherited during config resolution.
	AuthProfile  string        `yaml:"auth_profile"`
	Auth         AuthOverrides `yaml:"auth"`
	ClientID     string        `yaml:"client_id"`
	ClientSecret string        `yaml:"client_secret"`
	RedirectURL  string        `yaml:"redirect_url"`

	// MCPURL is set during config resolution for non-MCP upstreams that use
	// mcp_oauth auth. It points to the sibling MCP upstream's URL so that
	// OAuth discovery probes the correct endpoint.
	MCPURL string `yaml:"-"`
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
	ResponseCheck       *ResponseCheckDef `yaml:"response_check"`
	AuthHeader          string            `yaml:"auth_header"`
}

type AuthMappingDef struct {
	Headers map[string]string `yaml:"headers"`
}

type ResponseCheckDef struct {
	SuccessBodyMatch map[string]any `yaml:"success_body_match"`
	ErrorMessagePath string         `yaml:"error_message_path"`
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

	if err := resolveUpstreamAuth(&cfg); err != nil {
		return nil, err
	}

	resolveBaseURL(&cfg) // after resolveAuthProfiles so inherited fields take priority
	resolveRelativePaths(path, &cfg)

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
		fillAuthDefaults(&intg.Auth, &profile.Auth)
		cfg.Integrations[name] = intg
	}
	return nil
}

// resolveUpstreamAuth cascades integration-level auth to upstreams that don't
// specify their own, and resolves upstream-level auth profiles.
func resolveUpstreamAuth(cfg *Config) error {
	for name := range cfg.Integrations {
		intg := cfg.Integrations[name]
		for i := range intg.Upstreams {
			us := &intg.Upstreams[i]

			if us.AuthProfile != "" {
				profile, ok := cfg.AuthProfiles[us.AuthProfile]
				if !ok {
					return fmt.Errorf("integration %q upstream %d references unknown auth_profile %q", name, i, us.AuthProfile)
				}
				fillUpstreamFromProfile(us, profile)
			}

			if us.Auth.Type == "" {
				us.Auth.Type = intg.Auth.Type
			}
			if us.ClientID == "" {
				us.ClientID = intg.ClientID
			}
			if us.ClientSecret == "" {
				us.ClientSecret = intg.ClientSecret
			}
			if us.RedirectURL == "" {
				us.RedirectURL = intg.RedirectURL
			}
			fillAuthDefaults(&us.Auth, &intg.Auth)
		}

		// Populate MCPURL so non-MCP upstreams with mcp_oauth can discover
		// OAuth endpoints from the sibling MCP server.
		for i := range intg.Upstreams {
			us := &intg.Upstreams[i]
			if us.Auth.Type == "mcp_oauth" && us.Type != UpstreamTypeMCP && us.MCPURL == "" {
				for j := range intg.Upstreams {
					if intg.Upstreams[j].Type == UpstreamTypeMCP {
						us.MCPURL = intg.Upstreams[j].URL
						break
					}
				}
			}
		}

		cfg.Integrations[name] = intg
	}
	return nil
}

func fillUpstreamFromProfile(us *UpstreamDef, profile AuthProfile) {
	if us.ClientID == "" {
		us.ClientID = profile.ClientID
	}
	if us.ClientSecret == "" {
		us.ClientSecret = profile.ClientSecret
	}
	if us.RedirectURL == "" {
		us.RedirectURL = profile.RedirectURL
	}
	fillAuthDefaults(&us.Auth, &profile.Auth)
}

func fillAuthDefaults(dst, src *AuthOverrides) {
	if dst.Type == "" {
		dst.Type = src.Type
	}
	if dst.AuthorizationURL == "" {
		dst.AuthorizationURL = src.AuthorizationURL
	}
	if dst.TokenURL == "" {
		dst.TokenURL = src.TokenURL
	}
	if dst.ClientAuth == "" {
		dst.ClientAuth = src.ClientAuth
	}
	if dst.TokenExchange == "" {
		dst.TokenExchange = src.TokenExchange
	}
	if !dst.PKCE && src.PKCE {
		dst.PKCE = true
	}
	if dst.Scopes == nil {
		dst.Scopes = src.Scopes
	}
	if dst.ScopeSeparator == "" {
		dst.ScopeSeparator = src.ScopeSeparator
	}
	if dst.AuthorizationParams == nil {
		dst.AuthorizationParams = src.AuthorizationParams
	}
	if dst.TokenParams == nil {
		dst.TokenParams = src.TokenParams
	}
	if dst.RefreshParams == nil {
		dst.RefreshParams = src.RefreshParams
	}
	if dst.AcceptHeader == "" {
		dst.AcceptHeader = src.AcceptHeader
	}
	if dst.TokenMetadata == nil {
		dst.TokenMetadata = src.TokenMetadata
	}
	if dst.ResponseCheck == nil {
		dst.ResponseCheck = src.ResponseCheck
	}
	if dst.AuthHeader == "" {
		dst.AuthHeader = src.AuthHeader
	}
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

func resolveRelativePaths(configPath string, cfg *Config) {
	baseDir := filepath.Dir(configPath)
	if absPath, err := filepath.Abs(configPath); err == nil {
		baseDir = filepath.Dir(absPath)
	}

	for name := range cfg.Integrations {
		intg := cfg.Integrations[name]
		if intg.IconFile != "" {
			intg.IconFile = resolveRelativePath(baseDir, intg.IconFile)
		}
		if intg.Plugin != nil {
			intg.Plugin.Command = resolveExecutablePath(baseDir, intg.Plugin.Command)
		}
		cfg.Integrations[name] = intg
	}

	for name := range cfg.Runtimes {
		rt := cfg.Runtimes[name]
		if rt.Plugin != nil {
			rt.Plugin.Command = resolveExecutablePath(baseDir, rt.Plugin.Command)
		}
		cfg.Runtimes[name] = rt
	}
}

func resolveRelativePath(baseDir, value string) string {
	if value == "" || filepath.IsAbs(value) {
		return value
	}
	return filepath.Clean(filepath.Join(baseDir, value))
}

func resolveExecutablePath(baseDir, value string) string {
	if value == "" || filepath.IsAbs(value) {
		return value
	}
	if strings.HasPrefix(value, ".") || strings.ContainsRune(value, os.PathSeparator) {
		return filepath.Clean(filepath.Join(baseDir, value))
	}
	return value
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
	for name := range cfg.Integrations {
		intg := cfg.Integrations[name]
		if err := validateExecutablePlugin("integration", name, intg.Plugin); err != nil {
			return err
		}
		if intg.Plugin != nil {
			mode := intg.Plugin.Mode
			if mode == "" {
				mode = PluginModeReplace
			}
			switch mode {
			case PluginModeReplace:
				if len(intg.Upstreams) > 0 {
					return fmt.Errorf("config validation: integration %q cannot set both plugin and upstreams; use mode: overlay to combine them", name)
				}
				continue
			case PluginModeOverlay:
			default:
				return fmt.Errorf("config validation: integration %q has unknown plugin mode %q", name, intg.Plugin.Mode)
			}
		}
		apiCount := 0
		for i := range intg.Upstreams {
			us := &intg.Upstreams[i]
			switch us.Type {
			case UpstreamTypeREST, UpstreamTypeGraphQL:
				apiCount++
				if us.URL == "" {
					return fmt.Errorf("config validation: integration %q %s upstream requires url", name, us.Type)
				}
			case UpstreamTypeMCP:
				if us.URL == "" {
					return fmt.Errorf("config validation: integration %q mcp upstream requires url", name)
				}
			case "":
				return fmt.Errorf("config validation: integration %q has upstream with empty type", name)
			default:
				return fmt.Errorf("config validation: integration %q has unknown upstream type %q", name, us.Type)
			}
		}
		if apiCount > 1 {
			return fmt.Errorf("config validation: integration %q has multiple REST/GraphQL upstreams; only one is supported", name)
		}
	}
	for name := range cfg.Runtimes {
		rt := cfg.Runtimes[name]
		if err := validateExecutablePlugin("runtime", name, rt.Plugin); err != nil {
			return err
		}
		if rt.Plugin != nil {
			if rt.Plugin.Mode == PluginModeOverlay {
				return fmt.Errorf("config validation: runtime %q plugin.mode cannot be overlay", name)
			}
			if rt.Type != "" {
				return fmt.Errorf("config validation: runtime %q cannot set both plugin and type", name)
			}
			continue
		}
		if rt.Type == "" {
			return fmt.Errorf("config validation: runtime %q requires either type or plugin", name)
		}
	}
	return nil
}

func validateExecutablePlugin(kind, name string, plugin *ExecutablePluginDef) error {
	if plugin == nil {
		return nil
	}
	if plugin.Command == "" {
		return fmt.Errorf("config validation: %s %q plugin.command is required", kind, name)
	}
	return nil
}
