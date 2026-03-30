package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/egress"
	"github.com/valon-technologies/gestalt/server/internal/pluginsource"
	"gopkg.in/yaml.v3"
)

// Callback paths must match the routes registered in server.go.
const (
	AuthCallbackPath        = "/api/v1/auth/login/callback"
	IntegrationCallbackPath = "/api/v1/auth/callback"
)

// PluginConnectionName is the implicit connection name used when storing
// tokens for plugin-only integrations that do not declare YAML connections.
const PluginConnectionName = "_plugin"

// PluginConnectionAlias is the user-facing alias that maps to
// PluginConnectionName. In hybrid integrations, mcp.connection can be set
// to "plugin" to reuse the plugin's OAuth token.
const PluginConnectionAlias = "plugin"

type Config struct {
	Auth         AuthConfig                `yaml:"auth"`
	Datastore    DatastoreConfig           `yaml:"datastore"`
	Secrets      SecretsConfig             `yaml:"secrets"`
	Telemetry    TelemetryConfig           `yaml:"telemetry"`
	Integrations map[string]IntegrationDef `yaml:"integrations"`
	Runtimes     map[string]RuntimeDef     `yaml:"runtimes"`
	Bindings     map[string]BindingDef     `yaml:"bindings"`
	Server       ServerConfig              `yaml:"server"`
	Egress       EgressConfig              `yaml:"egress"`
	UI           UIConfig                  `yaml:"ui"`
}

type TelemetryConfig struct {
	Provider string    `yaml:"provider"`
	Config   yaml.Node `yaml:"config"`
}

type UIConfig struct {
	Plugin *UIPluginDef `yaml:"plugin"`
}

type UIPluginDef struct {
	Package string `yaml:"package"`
	Source  string `yaml:"source"`
	Version string `yaml:"version"`

	ResolvedAssetRoot    string `yaml:"-"`
	ResolvedManifestPath string `yaml:"-"`
}

func (p *UIPluginDef) HasManagedArtifacts() bool {
	return p != nil && (p.Package != "" || p.Source != "")
}

type EgressConfig struct {
	DefaultAction string                  `yaml:"default_action"`
	Policies      []EgressPolicyRule      `yaml:"policies"`
	Credentials   []EgressCredentialGrant `yaml:"credentials"`
}

type EgressPolicyRule struct {
	Action      string `yaml:"action"`
	SubjectKind string `yaml:"subject_kind"`
	SubjectID   string `yaml:"subject_id"`
	Provider    string `yaml:"provider"`
	Operation   string `yaml:"operation"`
	Method      string `yaml:"method"`
	Host        string `yaml:"host"`
	PathPrefix  string `yaml:"path_prefix"`
}

type EgressCredentialGrant struct {
	SecretRef   string `yaml:"secret_ref"`
	AuthStyle   string `yaml:"auth_style"`
	SubjectKind string `yaml:"subject_kind"`
	SubjectID   string `yaml:"subject_id"`
	Operation   string `yaml:"operation"`
	Method      string `yaml:"method"`
	Host        string `yaml:"host"`
	PathPrefix  string `yaml:"path_prefix"`
}

type ExecutablePluginDef struct {
	Command           string                        `yaml:"command"`
	Package           string                        `yaml:"package"`
	Source            string                        `yaml:"source"`
	Version           string                        `yaml:"version"`
	Args              []string                      `yaml:"args"`
	Env               map[string]string             `yaml:"env"`
	AllowedHosts      []string                      `yaml:"allowed_hosts"`
	Config            yaml.Node                     `yaml:"config"`
	AllowedOperations map[string]*OperationOverride `yaml:"allowed_operations"`

	ResolvedManifestPath string `yaml:"-"`
	IsDeclarative        bool   `yaml:"-"`
}

func (p *ExecutablePluginDef) HasManagedArtifacts() bool {
	return p != nil && (p.Package != "" || p.Source != "")
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
	APITokenTTL   string `yaml:"api_token_ttl"`
}

// IntegrationDef represents a declarative integration (connections +
// api/mcp surfaces), a plugin-only integration, or a hybrid that composes
// a plugin with an MCP surface.
type IntegrationDef struct {
	Connections   map[string]ConnectionDef `yaml:"connections"`
	API           *APIDef                  `yaml:"api"`
	MCP           *MCPDef                  `yaml:"mcp"`
	Plugin        *ExecutablePluginDef     `yaml:"plugin"`
	DisplayName   string                   `yaml:"display_name"`
	Description   string                   `yaml:"description"`
	MCPToolPrefix string                   `yaml:"mcp_tool_prefix"`
	IconFile      string                   `yaml:"icon_file"`
}

// ConnectionDef owns authentication and connection parameters for a named
// connection. All connections in a single integration must share the same Mode.
type ConnectionDef struct {
	Mode   string                        `yaml:"mode"`
	Auth   ConnectionAuthDef             `yaml:"auth"`
	Params map[string]ConnectionParamDef `yaml:"params"`
}

type ConnectionAuthDef struct {
	Type                string               `yaml:"type"`
	AuthorizationURL    string               `yaml:"authorization_url"`
	TokenURL            string               `yaml:"token_url"`
	ClientID            string               `yaml:"client_id"`
	ClientSecret        string               `yaml:"client_secret"`
	RedirectURL         string               `yaml:"redirect_url"`
	ClientAuth          string               `yaml:"client_auth"`
	TokenExchange       string               `yaml:"token_exchange"`
	Scopes              []string             `yaml:"scopes"`
	ScopeParam          string               `yaml:"scope_param"`
	ScopeSeparator      string               `yaml:"scope_separator"`
	PKCE                bool                 `yaml:"pkce"`
	AuthorizationParams map[string]string    `yaml:"authorization_params"`
	TokenParams         map[string]string    `yaml:"token_params"`
	RefreshParams       map[string]string    `yaml:"refresh_params"`
	AcceptHeader        string               `yaml:"accept_header"`
	TokenMetadata       []string             `yaml:"token_metadata"`
	Credentials         []CredentialFieldDef `yaml:"credentials"`
	AuthMapping         *AuthMappingDef      `yaml:"auth_mapping"`
}

type CredentialFieldDef struct {
	Name        string `yaml:"name"`
	Label       string `yaml:"label"`
	Description string `yaml:"description"`
	HelpURL     string `yaml:"help_url"`
}

type AuthMappingDef struct {
	Headers map[string]string `yaml:"headers"`
}

type ConnectionParamDef struct {
	Required bool `yaml:"required"`
}

const (
	APITypeREST    = "rest"
	APITypeGraphQL = "graphql"
)

// ResolveConnectionAlias maps the user-facing "plugin" alias to the
// internal PluginConnectionName. All other names pass through unchanged.
func ResolveConnectionAlias(name string) string {
	if name == PluginConnectionAlias {
		return PluginConnectionName
	}
	return name
}

// ConnectionMode returns the shared connection mode for a set of connections.
// All connections are validated to have the same mode; this returns the first
// non-empty mode found, defaulting to "user".
func ConnectionMode(connections map[string]ConnectionDef) string {
	for cname := range connections {
		if connections[cname].Mode != "" {
			return connections[cname].Mode
		}
	}
	return "user"
}

// OperationOverride holds optional alias and description for an allowed operation.
type OperationOverride struct {
	Alias       string `yaml:"alias" json:"alias,omitempty"`
	Description string `yaml:"description" json:"description,omitempty"`
}

type APIDef struct {
	Type              string                        `yaml:"type"`
	OpenAPI           string                        `yaml:"openapi"`
	URL               string                        `yaml:"url"`
	Connection        string                        `yaml:"connection"`
	AllowedOperations map[string]*OperationOverride `yaml:"allowed_operations"`
}

type MCPDef struct {
	URL               string                        `yaml:"url"`
	Connection        string                        `yaml:"connection"`
	AllowedOperations map[string]*OperationOverride `yaml:"allowed_operations"`
}

func Load(path string) (*Config, error) {
	return LoadWithLookup(path, os.LookupEnv)
}

func LoadWithMapping(path string, getenv func(string) string) (*Config, error) {
	return LoadWithLookup(path, func(key string) (string, bool) {
		// Preserve the legacy os.Expand-style contract for callers that only
		// provide a string mapping: the mapped value wins even when it is empty.
		return getenv(key), true
	})
}

func LoadWithLookup(path string, lookup func(string) (string, bool)) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	resolved, err := expandEnvVariables(string(data), lookup)
	if err != nil {
		return nil, err
	}

	var cfg Config
	dec := yaml.NewDecoder(strings.NewReader(resolved))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil && err != io.EOF {
		return nil, fmt.Errorf("parsing config YAML: %w", err)
	}

	applyDefaults(&cfg)
	resolveBaseURL(&cfg)
	resolveRelativePaths(path, &cfg)

	if err := ValidateStructure(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func expandEnvVariables(input string, lookup func(string) (string, bool)) (string, error) {
	var expandErr error
	resolved := os.Expand(input, func(key string) string {
		if expandErr != nil {
			return ""
		}
		if val, ok := lookup(key); ok {
			return val
		}
		filePath, ok := lookup(key + "_FILE")
		if !ok || filePath == "" {
			return ""
		}
		data, err := os.ReadFile(filePath)
		if err != nil {
			expandErr = fmt.Errorf("resolving %s_FILE: %w", key, err)
			return ""
		}
		return strings.TrimRight(string(data), "\r\n")
	})
	if expandErr != nil {
		return "", fmt.Errorf("expanding config environment variables: %w", expandErr)
	}
	return resolved, nil
}

func applyDefaults(cfg *Config) {
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.Secrets.Provider == "" {
		cfg.Secrets.Provider = "env"
	}
	if cfg.Telemetry.Provider == "" {
		cfg.Telemetry.Provider = "stdout"
	}
}

func resolveBaseURL(cfg *Config) {
	base := strings.TrimRight(cfg.Server.BaseURL, "/")
	if base == "" {
		return
	}
	cfg.Server.BaseURL = base

	redirectURL := base + IntegrationCallbackPath
	for name := range cfg.Integrations {
		intg := cfg.Integrations[name]
		for cname := range intg.Connections {
			conn := intg.Connections[cname]
			if conn.Auth.RedirectURL == "" {
				conn.Auth.RedirectURL = redirectURL
				intg.Connections[cname] = conn
			}
		}
		cfg.Integrations[name] = intg
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
		if intg.API != nil && intg.API.OpenAPI != "" {
			intg.API.OpenAPI = resolveUpstreamURL(baseDir, intg.API.OpenAPI)
		}
		if intg.Plugin != nil {
			intg.Plugin.Command = resolveExecutablePath(baseDir, intg.Plugin.Command)
			intg.Plugin.Package = resolvePackagePath(baseDir, intg.Plugin.Package)
		}
		cfg.Integrations[name] = intg
	}

	for name := range cfg.Runtimes {
		rt := cfg.Runtimes[name]
		if rt.Plugin != nil {
			rt.Plugin.Command = resolveExecutablePath(baseDir, rt.Plugin.Command)
			rt.Plugin.Package = resolvePackagePath(baseDir, rt.Plugin.Package)
		}
		cfg.Runtimes[name] = rt
	}

	if cfg.UI.Plugin != nil {
		cfg.UI.Plugin.Package = resolvePackagePath(baseDir, cfg.UI.Plugin.Package)
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

func resolveUpstreamURL(baseDir, value string) string {
	if value == "" || filepath.IsAbs(value) || strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "http://") {
		return value
	}
	return filepath.Clean(filepath.Join(baseDir, value))
}

func resolvePackagePath(baseDir, value string) string {
	if value == "" || filepath.IsAbs(value) || strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "http://") {
		return value
	}
	return filepath.Clean(filepath.Join(baseDir, value))
}

var templateParamRe = regexp.MustCompile(`\{(\w+)\}`)

// ValidateStructure checks config shape: integration references, plugin
// declarations, connection references, URL template params, egress rules.
// Called by Load (and therefore by bundle and validate). Does not require
// runtime secrets like encryption_key, auth.provider, or datastore.provider.
func ValidateStructure(cfg *Config) error {
	if cfg.Server.APITokenTTL != "" {
		if _, err := ParseDuration(cfg.Server.APITokenTTL); err != nil {
			return fmt.Errorf("config validation: server.api_token_ttl: %w", err)
		}
	}
	if err := validateEgress(&cfg.Egress); err != nil {
		return err
	}
	if err := validateUIPlugin(cfg.UI.Plugin); err != nil {
		return err
	}
	for name := range cfg.Integrations {
		intg := cfg.Integrations[name]
		if err := validateIntegration(name, intg); err != nil {
			return err
		}
	}
	for name := range cfg.Runtimes {
		rt := cfg.Runtimes[name]
		if err := validateExecutablePlugin("runtime", name, rt.Plugin); err != nil {
			return err
		}
		if rt.Plugin != nil {
			if rt.Plugin.Config.Kind != 0 {
				return fmt.Errorf("config validation: runtime %q must use config at runtimes.%s.config, not runtimes.%s.plugin.config", name, name, name)
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

// ValidateRuntime checks runtime-only requirements: encryption key, auth
// provider, and datastore provider. Callers that need a fully operational
// config (serve) should call this after Load. Callers that only need
// structural correctness (bundle, validate) should not.
func ValidateRuntime(cfg *Config) error {
	if cfg.Auth.Provider == "" {
		return fmt.Errorf("config validation: auth.provider is required")
	}
	if cfg.Datastore.Provider == "" {
		return fmt.Errorf("config validation: datastore.provider is required")
	}
	if cfg.Server.EncryptionKey == "" {
		return fmt.Errorf("config validation: server.encryption_key is required")
	}
	return nil
}

func validateIntegration(name string, intg IntegrationDef) error {
	if intg.Plugin != nil {
		return validateHybridIntegration(name, intg)
	}

	if len(intg.Connections) == 0 {
		return fmt.Errorf("config validation: integration %q requires connections (or use plugin for plugin-only integrations)", name)
	}
	if intg.API == nil && intg.MCP == nil {
		return fmt.Errorf("config validation: integration %q requires at least one of api or mcp", name)
	}

	if err := validateConnectionModes(name, intg.Connections); err != nil {
		return err
	}

	if intg.API != nil {
		if err := validateAPIDef(name, intg.API, intg.Connections); err != nil {
			return err
		}
	}
	if intg.MCP != nil {
		if err := validateMCPDef(name, intg.MCP, intg.Connections); err != nil {
			return err
		}
	}

	for cname := range intg.Connections {
		if err := validateConnectionAuthURLParams(name, cname, intg.Connections[cname]); err != nil {
			return err
		}
		if err := validateCredentialFields(name, cname, intg.Connections[cname].Auth); err != nil {
			return err
		}
	}

	return nil
}

func validateHybridIntegration(name string, intg IntegrationDef) error {
	if err := validateExecutablePlugin("integration", name, intg.Plugin); err != nil {
		return err
	}

	if intg.API != nil {
		return fmt.Errorf("config validation: integration %q cannot compose plugin with api; only plugin + mcp is supported", name)
	}

	if intg.MCP != nil {
		if intg.MCP.Connection == "" {
			return fmt.Errorf("config validation: integration %q mcp.connection is required when composing with plugin", name)
		}
		if intg.MCP.Connection != PluginConnectionAlias {
			if err := validateMCPDef(name, intg.MCP, intg.Connections); err != nil {
				return err
			}
		} else {
			if intg.MCP.URL == "" {
				return fmt.Errorf("config validation: integration %q mcp.url is required", name)
			}
		}
	}

	if len(intg.Connections) > 0 {
		if intg.MCP == nil {
			return fmt.Errorf("config validation: integration %q has connections but no mcp surface; plugin + bare connections is not supported", name)
		}
		if err := validateConnectionModes(name, intg.Connections); err != nil {
			return err
		}
		for cname := range intg.Connections {
			if err := validateConnectionAuthURLParams(name, cname, intg.Connections[cname]); err != nil {
				return err
			}
			if err := validateCredentialFields(name, cname, intg.Connections[cname].Auth); err != nil {
				return err
			}
		}
	}

	return nil
}

func validateConnectionModes(intgName string, connections map[string]ConnectionDef) error {
	var firstMode string
	var firstName string
	for cname := range connections {
		mode := connections[cname].Mode
		if mode == "" {
			mode = "user"
		}
		switch mode {
		case "user", "identity", "either", "none":
		default:
			return fmt.Errorf("config validation: integration %q connection %q has unknown mode %q", intgName, cname, mode)
		}
		if firstName == "" {
			firstName = cname
			firstMode = mode
		} else if mode != firstMode {
			return fmt.Errorf("config validation: integration %q connections have mixed modes (%q=%q, %q=%q); all connections must share the same mode", intgName, firstName, firstMode, cname, mode)
		}
	}
	return nil
}

func validateAPIDef(intgName string, api *APIDef, connections map[string]ConnectionDef) error {
	switch api.Type {
	case APITypeREST:
		if api.OpenAPI == "" {
			return fmt.Errorf("config validation: integration %q api.type rest requires api.openapi", intgName)
		}
	case APITypeGraphQL:
		if api.URL == "" {
			return fmt.Errorf("config validation: integration %q api.type graphql requires api.url", intgName)
		}
	case "":
		return fmt.Errorf("config validation: integration %q api.type is required", intgName)
	default:
		return fmt.Errorf("config validation: integration %q has unknown api.type %q", intgName, api.Type)
	}

	conn, err := resolveConnectionRef(intgName, "api", api.Connection, connections)
	if err != nil {
		return err
	}
	if api.URL != "" {
		if err := validateURLTemplateParams(intgName, "api.url", api.URL, conn); err != nil {
			return err
		}
	}
	return nil
}

func validateMCPDef(intgName string, mcp *MCPDef, connections map[string]ConnectionDef) error {
	if mcp.URL == "" {
		return fmt.Errorf("config validation: integration %q mcp.url is required", intgName)
	}
	conn, err := resolveConnectionRef(intgName, "mcp", mcp.Connection, connections)
	if err != nil {
		return err
	}
	return validateURLTemplateParams(intgName, "mcp.url", mcp.URL, conn)
}

func resolveConnectionRef(intgName, surface, connName string, connections map[string]ConnectionDef) (ConnectionDef, error) {
	if connName == "" {
		return ConnectionDef{}, fmt.Errorf("config validation: integration %q %s.connection is required", intgName, surface)
	}
	conn, ok := connections[connName]
	if !ok {
		return ConnectionDef{}, fmt.Errorf("config validation: integration %q %s.connection references unknown connection %q", intgName, surface, connName)
	}
	return conn, nil
}

func validateCredentialFields(intgName, connName string, auth ConnectionAuthDef) error {
	if len(auth.Credentials) == 0 {
		return nil
	}
	names := make(map[string]bool, len(auth.Credentials))
	for i, cf := range auth.Credentials {
		if cf.Name == "" {
			return fmt.Errorf("config validation: integration %q connection %q credentials[%d].name is required", intgName, connName, i)
		}
		if names[cf.Name] {
			return fmt.Errorf("config validation: integration %q connection %q has duplicate credential name %q", intgName, connName, cf.Name)
		}
		names[cf.Name] = true
	}
	if auth.AuthMapping != nil {
		for header, field := range auth.AuthMapping.Headers {
			if !names[field] {
				return fmt.Errorf("config validation: integration %q connection %q auth_mapping header %q references unknown credential %q", intgName, connName, header, field)
			}
		}
	}
	hasMapping := auth.AuthMapping != nil && len(auth.AuthMapping.Headers) > 0
	if len(auth.Credentials) == 1 && hasMapping {
		return fmt.Errorf("config validation: integration %q connection %q has auth_mapping with a single credential; use auth_header on the provider definition for single-credential header injection", intgName, connName)
	}
	if len(auth.Credentials) > 1 && !hasMapping {
		return fmt.Errorf("config validation: integration %q connection %q has multiple credentials but no auth_mapping", intgName, connName)
	}
	return nil
}

func validateConnectionAuthURLParams(intgName, connName string, conn ConnectionDef) error {
	if conn.Auth.AuthorizationURL != "" {
		field := fmt.Sprintf("connections.%s.auth.authorization_url", connName)
		if err := validateURLTemplateParams(intgName, field, conn.Auth.AuthorizationURL, conn); err != nil {
			return err
		}
	}
	if conn.Auth.TokenURL != "" {
		field := fmt.Sprintf("connections.%s.auth.token_url", connName)
		if err := validateURLTemplateParams(intgName, field, conn.Auth.TokenURL, conn); err != nil {
			return err
		}
	}
	return nil
}

func validateURLTemplateParams(intgName, field, url string, conn ConnectionDef) error {
	matches := templateParamRe.FindAllStringSubmatch(url, -1)
	for _, m := range matches {
		paramName := m[1]
		p, ok := conn.Params[paramName]
		if !ok {
			return fmt.Errorf("config validation: integration %q %s references param {%s} not declared in connection params", intgName, field, paramName)
		}
		if !p.Required {
			return fmt.Errorf("config validation: integration %q %s references param {%s} which must be required", intgName, field, paramName)
		}
	}
	return nil
}

func validateExecutablePlugin(kind, name string, plugin *ExecutablePluginDef) error {
	if plugin == nil {
		return nil
	}
	sourceCount := 0
	if plugin.Command != "" {
		sourceCount++
	}
	if plugin.Package != "" {
		sourceCount++
	}
	if plugin.Source != "" {
		sourceCount++
	}
	switch {
	case sourceCount == 0:
		return fmt.Errorf("config validation: %s %q plugin.command, plugin.package, or plugin.source is required", kind, name)
	case sourceCount > 1:
		return fmt.Errorf("config validation: %s %q plugin.command, plugin.package, and plugin.source are mutually exclusive", kind, name)
	}

	if plugin.Source != "" {
		if _, err := pluginsource.Parse(plugin.Source); err != nil {
			return fmt.Errorf("config validation: %s %q plugin.source: %w", kind, name, err)
		}
		if plugin.Version == "" {
			return fmt.Errorf("config validation: %s %q plugin.version is required when plugin.source is set", kind, name)
		}
		if err := pluginsource.ValidateVersion(plugin.Version); err != nil {
			return fmt.Errorf("config validation: %s %q plugin.version: %w", kind, name, err)
		}
	}

	if (plugin.Command != "" || plugin.Package != "") && plugin.Version != "" {
		return fmt.Errorf("config validation: %s %q plugin.version is only valid with plugin.source", kind, name)
	}
	if plugin.Command == "" && len(plugin.Args) > 0 {
		return fmt.Errorf("config validation: %s %q plugin.args are only valid with plugin.command", kind, name)
	}
	if strings.HasPrefix(plugin.Package, "http://") {
		return fmt.Errorf("config validation: %s %q plugin.package requires HTTPS; plain HTTP is not supported", kind, name)
	}
	return nil
}

func validateUIPlugin(plugin *UIPluginDef) error {
	if plugin == nil {
		return nil
	}
	sourceCount := 0
	if plugin.Package != "" {
		sourceCount++
	}
	if plugin.Source != "" {
		sourceCount++
	}
	switch {
	case sourceCount == 0:
		return fmt.Errorf("config validation: ui plugin.package or plugin.source is required")
	case sourceCount > 1:
		return fmt.Errorf("config validation: ui plugin.package and plugin.source are mutually exclusive")
	}

	if plugin.Source != "" {
		if _, err := pluginsource.Parse(plugin.Source); err != nil {
			return fmt.Errorf("config validation: ui plugin.source: %w", err)
		}
		if plugin.Version == "" {
			return fmt.Errorf("config validation: ui plugin.version is required when plugin.source is set")
		}
		if err := pluginsource.ValidateVersion(plugin.Version); err != nil {
			return fmt.Errorf("config validation: ui plugin.version: %w", err)
		}
	}

	if plugin.Package != "" && plugin.Version != "" {
		return fmt.Errorf("config validation: ui plugin.version is only valid with plugin.source")
	}
	if strings.HasPrefix(plugin.Package, "http://") {
		return fmt.Errorf("config validation: ui plugin.package requires HTTPS; plain HTTP is not supported")
	}
	return nil
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

func validateEgress(cfg *EgressConfig) error {
	switch cfg.DefaultAction {
	case "", "allow", "deny":
	default:
		return fmt.Errorf("config validation: egress.default_action must be \"allow\" or \"deny\", got %q", cfg.DefaultAction)
	}
	for i := range cfg.Policies {
		switch cfg.Policies[i].Action {
		case "allow", "deny":
		default:
			return fmt.Errorf("config validation: egress.policies[%d].action must be \"allow\" or \"deny\", got %q", i, cfg.Policies[i].Action)
		}
	}
	for i := range cfg.Credentials {
		c := &cfg.Credentials[i]
		if c.SecretRef == "" {
			return fmt.Errorf("config validation: egress.credentials[%d]: secret_ref is required", i)
		}
		if err := egress.ValidateCredentialGrant(egress.CredentialGrantValidationInput{
			SubjectKind: c.SubjectKind,
			SubjectID:   c.SubjectID,
			Operation:   c.Operation,
			Method:      c.Method,
			Host:        c.Host,
			PathPrefix:  c.PathPrefix,
			AuthStyle:   c.AuthStyle,
		}); err != nil {
			return fmt.Errorf("config validation: egress.credentials[%d]: %w", i, err)
		}
	}
	return nil
}
