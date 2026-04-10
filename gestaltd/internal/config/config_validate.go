package config

import (
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/egress"
	"github.com/valon-technologies/gestalt/server/internal/pluginsource"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
	"gopkg.in/yaml.v3"
)

// ValidateStructure checks config shape: integration references, plugin
// declarations, connection references, URL template params, egress rules.
// Called by Load (and therefore by init, validate, and serve). Does not require
// runtime secrets like encryption_key.
func ValidateStructure(cfg *Config) error {
	if err := validateServerListeners(cfg.Server); err != nil {
		return err
	}
	if cfg.Server.APITokenTTL != "" {
		if _, err := ParseDuration(cfg.Server.APITokenTTL); err != nil {
			return fmt.Errorf("config validation: server.apiTokenTtl: %w", err)
		}
	}
	if err := validateTelemetry(cfg.Telemetry); err != nil {
		return err
	}
	if err := validateAudit(cfg.Audit); err != nil {
		return err
	}
	if err := validateEgress(&cfg.Egress); err != nil {
		return err
	}
	if err := validateUIConfig(cfg.UI); err != nil {
		return err
	}
	if err := validateTopLevelComponentConfig("auth", cfg.Auth.Provider, cfg.Auth.Config); err != nil {
		return err
	}
	if err := validateDatastoreConfig(cfg); err != nil {
		return err
	}
	if err := validateDatastores(cfg); err != nil {
		return err
	}
	if cfg.Secrets.Provider != nil {
		if err := validateTopLevelComponentConfig("secrets", cfg.Secrets.Provider, cfg.Secrets.Config); err != nil {
			return err
		}
	}
	for name := range cfg.Plugins {
		intg := cfg.Plugins[name]
		if err := validatePlugin(name, intg); err != nil {
			return err
		}
	}
	return nil
}

func validateTelemetry(cfg TelemetryConfig) error {
	if cfg.Provider != nil {
		return validateTopLevelComponentProvider("telemetry", cfg.Provider)
	}
	return nil
}

func validateAudit(cfg AuditConfig) error {
	if cfg.Provider != nil {
		return validateTopLevelComponentProvider("audit", cfg.Provider)
	}
	switch cfg.BuiltinProvider {
	case "", "inherit", "noop":
		if cfg.Config.Kind == 0 {
			return nil
		}
		return fmt.Errorf("config validation: audit.config is not supported when audit.provider is %q", cfg.BuiltinProvider)
	case "stdout":
		if cfg.Config.Kind == 0 {
			return nil
		}
		var stdoutCfg struct {
			Level  string `yaml:"level"`
			Format string `yaml:"format"`
		}
		if err := cfg.Config.Decode(&stdoutCfg); err != nil {
			return fmt.Errorf("config validation: stdout audit: parsing config: %w", err)
		}
		return nil
	case "otlp":
		if cfg.Config.Kind == 0 {
			return nil
		}
		var otlpCfg struct {
			Protocol string `yaml:"protocol"`
			Logs     struct {
				Exporter string `yaml:"exporter"`
			} `yaml:"logs"`
		}
		if err := cfg.Config.Decode(&otlpCfg); err != nil {
			return fmt.Errorf("config validation: otlp audit: parsing config: %w", err)
		}
		if otlpCfg.Protocol != "" {
			switch strings.ToLower(otlpCfg.Protocol) {
			case "grpc", "http":
			default:
				return fmt.Errorf("config validation: otlp audit: unknown protocol %q (expected \"grpc\" or \"http\")", otlpCfg.Protocol)
			}
		}
		if otlpCfg.Logs.Exporter != "" && !strings.EqualFold(otlpCfg.Logs.Exporter, "otlp") {
			return fmt.Errorf("config validation: otlp audit: logs.exporter must be %q", "otlp")
		}
		return nil
	default:
		return fmt.Errorf("config validation: unknown audit.provider %q", cfg.BuiltinProvider)
	}
}

func validateTopLevelComponentProvider(kind string, provider *ProviderDef) error {
	if provider == nil {
		return nil
	}
	if provider.Config.Kind != 0 {
		return fmt.Errorf("config validation: %s.provider.config is not supported; use %s.config", kind, kind)
	}
	return validateExternalPlugin(kind, kind, provider)
}

func validateTopLevelComponentConfig(kind string, provider *ProviderDef, cfg yaml.Node) error {
	if provider == nil {
		if cfg.Kind != 0 {
			return fmt.Errorf("config validation: %s.config is not supported when %s.provider is unset", kind, kind)
		}
		return nil
	}
	return validateTopLevelComponentProvider(kind, provider)
}

// ValidateResolvedStructure checks integration fields whose support depends on
// resolved managed plugin manifests. Callers should use this after init has
// applied locked plugin artifacts into the config. It intentionally does not
// rerun the full structural validator on the mutated config.
func ValidateResolvedStructure(cfg *Config) error {
	for name := range cfg.Plugins {
		intg := cfg.Plugins[name]
		if intg.Plugin == nil {
			return fmt.Errorf("config validation: integration %q requires a plugin", name)
		}
		if err := validateManifestBackedIntegration(name, intg.Plugin); err != nil {
			return err
		}
	}
	return nil
}

// ValidateRuntime checks runtime-only requirements: encryption key plus the
// required top-level datastore provider. Platform auth is optional; omitting it
// or setting auth.provider to "none" disables authentication. Callers that need a fully
// operational config (serve) should call this after Load. Callers that only
// need structural correctness (init, validate) should not.
func ValidateRuntime(cfg *Config) error {
	if cfg.Datastore.Provider != nil {
		return fmt.Errorf("config validation: datastore.provider is no longer supported; use the datastores section with datastore: <name>")
	}
	if cfg.Datastore.Resource == "" {
		return fmt.Errorf("config validation: datastore is required (set datastore: <name> referencing a datastores entry)")
	}
	if _, ok := cfg.Datastores[cfg.Datastore.Resource]; !ok {
		return fmt.Errorf("config validation: datastore references unknown datastore %q", cfg.Datastore.Resource)
	}
	if cfg.Server.EncryptionKey == "" {
		return fmt.Errorf("config validation: server.encryption_key is required")
	}
	return nil
}

func validatePlugin(name string, intg PluginDef) error {
	if intg.Plugin == nil {
		return fmt.Errorf("config validation: integration %q requires a plugin", name)
	}
	p := intg.Plugin
	if err := validateExternalPlugin("integration", name, p); err != nil {
		return err
	}
	return validateManifestBackedIntegration(name, p)
}

type inlineConnectionReference struct {
	field    string
	name     string
	required bool
	context  string
}

func manifestBackedConnectionReferences(plugin *ProviderDef, provider *pluginmanifestv1.Plugin) []inlineConnectionReference {
	if provider == nil {
		return nil
	}

	defaultConnection := provider.DefaultConnection
	if plugin != nil && plugin.DefaultConnection != "" {
		defaultConnection = plugin.DefaultConnection
	}
	if defaultConnection == "" {
		return nil
	}
	return []inlineConnectionReference{
		{
			field: "plugin.defaultConnection",
			name:  defaultConnection,
		},
	}
}

func validateManifestBackedConnectionReferences(name string, plugin *ProviderDef, provider *pluginmanifestv1.Plugin) error {
	return validateConnectionReferences(name, declaredManifestBackedConnections(plugin, provider), manifestBackedConnectionReferences(plugin, provider))
}

func validateManifestBackedConnectionDefaults(name string, plugin *ProviderDef, provider *pluginmanifestv1.Plugin) error {
	return validateConnectionDefaults(name, "integration", len(declaredManifestBackedConnections(plugin, provider)), manifestBackedConnectionReferences(plugin, provider))
}

func validateExecutableConnectionAuthSupport(name string, plugin *ProviderDef, provider *pluginmanifestv1.Plugin) error {
	supportsMCPOAuth := provider != nil && provider.MCPURL != ""
	if conn := EffectivePluginConnectionDef(plugin, provider); conn.Auth.Type == pluginmanifestv1.AuthTypeMCPOAuth && !supportsMCPOAuth {
		return fmt.Errorf("config validation: integration %q plugin auth type %q requires an MCP surface", name, pluginmanifestv1.AuthTypeMCPOAuth)
	}

	declared := declaredManifestBackedConnections(plugin, provider)
	declaredNames := make([]string, 0, len(declared))
	for connName := range declared {
		declaredNames = append(declaredNames, connName)
	}
	slices.Sort(declaredNames)
	for _, connName := range declaredNames {
		conn, ok := EffectiveNamedConnectionDef(plugin, provider, connName)
		if !ok || conn.Auth.Type != pluginmanifestv1.AuthTypeMCPOAuth {
			continue
		}
		if !supportsMCPOAuth {
			return fmt.Errorf("config validation: integration %q connection %q auth type %q requires an MCP surface", name, connName, pluginmanifestv1.AuthTypeMCPOAuth)
		}
	}
	return nil
}

func validateConnectionReferences(name string, declared map[string]struct{}, refs []inlineConnectionReference) error {
	for _, ref := range refs {
		if ref.name == "" {
			continue
		}
		resolved := ResolveConnectionAlias(ref.name)
		if resolved == PluginConnectionName {
			continue
		}
		if _, ok := declared[resolved]; ok {
			continue
		}
		return fmt.Errorf("config validation: integration %q %s references undeclared connection %q", name, ref.field, ref.name)
	}
	return nil
}

func validateConnectionDefaults(name, subject string, declaredCount int, refs []inlineConnectionReference) error {
	if declaredCount == 0 {
		return nil
	}
	for _, ref := range refs {
		if ref.required && ref.name == "" {
			return fmt.Errorf("config validation: %s %q %s is required when %s", subject, name, ref.field, ref.context)
		}
	}
	return nil
}

func declaredManifestBackedConnections(plugin *ProviderDef, provider *pluginmanifestv1.Plugin) map[string]struct{} {
	size := 0
	if plugin != nil {
		size += len(plugin.Connections)
	}
	if provider != nil {
		size += len(provider.Connections)
	}
	declared := make(map[string]struct{}, size)
	if provider != nil {
		for rawName := range provider.Connections {
			addDeclaredConnection(declared, rawName)
		}
	}
	if plugin != nil {
		for rawName := range plugin.Connections {
			addDeclaredConnection(declared, rawName)
		}
	}
	return declared
}

func addDeclaredConnection(declared map[string]struct{}, rawName string) {
	resolved := ResolveConnectionAlias(rawName)
	if resolved == "" {
		return
	}
	declared[resolved] = struct{}{}
}

func validateExternalPlugin(kind, name string, plugin *ProviderDef) error {
	if plugin == nil {
		return nil
	}
	if plugin.Source != nil {
		modeCount := 0
		if plugin.Source.Path != "" {
			modeCount++
		}
		if plugin.Source.Ref != "" {
			modeCount++
		}
		switch {
		case modeCount == 0:
			return fmt.Errorf("config validation: %s %q plugin.source.path or plugin.source.ref is required when plugin.source is set", kind, name)
		case modeCount > 1:
			return fmt.Errorf("config validation: %s %q plugin.source.path and plugin.source.ref are mutually exclusive", kind, name)
		}
	}
	modeCount := 0
	if plugin.HasLocalSource() {
		modeCount++
	}
	if plugin.HasManagedSource() {
		modeCount++
	}
	switch {
	case modeCount == 0:
		return fmt.Errorf("config validation: %s %q provider.source.path or provider.source.ref is required", kind, name)
	case modeCount > 1:
		return fmt.Errorf("config validation: %s %q provider.source.path and provider.source.ref are mutually exclusive", kind, name)
	}
	if plugin.HasManagedSource() {
		if _, err := pluginsource.Parse(plugin.SourceRef()); err != nil {
			return fmt.Errorf("config validation: %s %q plugin.source.ref: %w", kind, name, err)
		}
		if plugin.SourceVersion() == "" {
			return fmt.Errorf("config validation: %s %q plugin.source.version is required when plugin.source.ref is set", kind, name)
		}
		if err := pluginsource.ValidateVersion(plugin.SourceVersion()); err != nil {
			return fmt.Errorf("config validation: %s %q plugin.source.version: %w", kind, name, err)
		}
	}

	if plugin.HasLocalSource() && plugin.SourceVersion() != "" {
		return fmt.Errorf("config validation: %s %q plugin.source.version is only valid with plugin.source.ref", kind, name)
	}
	if plugin.Source != nil && plugin.Source.Auth != nil {
		if !plugin.HasManagedSource() {
			return fmt.Errorf("config validation: %s %q plugin.source.auth is only valid with plugin.source.ref", kind, name)
		}
		if strings.TrimSpace(plugin.Source.Auth.Token) == "" {
			return fmt.Errorf("config validation: %s %q plugin.source.auth.token is required when plugin.source.auth is set", kind, name)
		}
	}

	if kind != "integration" {
		hasIntegrationConfig := plugin.Auth != nil || len(plugin.Connections) > 0 ||
			len(plugin.ConnectionParams) > 0 || plugin.MCP || len(plugin.AllowedOperations) > 0 ||
			plugin.DefaultConnection != "" || plugin.Discovery != nil
		if hasIntegrationConfig {
			return fmt.Errorf("config validation: %s %q provider cannot use integration-only fields", kind, name)
		}
	}

	return nil
}

func validateManifestBackedIntegration(name string, plugin *ProviderDef) error {
	if plugin == nil {
		return nil
	}
	effectiveProvider := plugin.ManifestPlugin()
	if effectiveProvider != nil {
		if err := validateManifestBackedConnectionReferences(name, plugin, effectiveProvider); err != nil {
			return err
		}
		if err := validateManifestBackedConnectionDefaults(name, plugin, effectiveProvider); err != nil {
			return err
		}
	}
	if err := validateExecutableConnectionAuthSupport(name, plugin, effectiveProvider); err != nil {
		return err
	}
	if err := validateConnectionAuthMappings(name, EffectivePluginConnectionDef(plugin, effectiveProvider).Auth, "plugin"); err != nil {
		return err
	}
	declared := declaredManifestBackedConnections(plugin, effectiveProvider)
	names := make([]string, 0, len(declared))
	for connName := range declared {
		if connName == PluginConnectionName {
			continue
		}
		names = append(names, connName)
	}
	slices.Sort(names)
	for _, connName := range names {
		conn, ok := EffectiveNamedConnectionDef(plugin, effectiveProvider, connName)
		if !ok {
			continue
		}
		if err := validateConnectionAuthMappings(name, conn.Auth, fmt.Sprintf("connection %q", connName)); err != nil {
			return err
		}
	}
	return nil
}

func validateConnectionAuthMappings(integration string, auth ConnectionAuthDef, subject string) error {
	credentialNames := make(map[string]struct{}, len(auth.Credentials))
	for _, field := range auth.Credentials {
		if field.Name != "" {
			credentialNames[field.Name] = struct{}{}
		}
	}
	if auth.AuthMapping == nil {
		return nil
	}
	for headerName, value := range auth.AuthMapping.Headers {
		if err := validateAuthValueDef(integration, subject, fmt.Sprintf("authMapping.headers[%q]", headerName), value, credentialNames); err != nil {
			return err
		}
	}
	if auth.AuthMapping.Basic != nil {
		if err := validateAuthValueDef(integration, subject, "authMapping.basic.username", auth.AuthMapping.Basic.Username, credentialNames); err != nil {
			return err
		}
		if err := validateAuthValueDef(integration, subject, "authMapping.basic.password", auth.AuthMapping.Basic.Password, credentialNames); err != nil {
			return err
		}
	}
	return nil
}

func validateAuthValueDef(integration, subject, path string, value AuthValueDef, credentialNames map[string]struct{}) error {
	hasValue := value.Value != ""
	hasValueFrom := value.ValueFrom != nil
	if hasValue == hasValueFrom {
		return fmt.Errorf("config validation: integration %q %s %s must set exactly one of value or valueFrom", integration, subject, path)
	}
	if hasValue {
		return nil
	}
	if value.ValueFrom.CredentialFieldRef == nil {
		return fmt.Errorf("config validation: integration %q %s %s.valueFrom must set credentialFieldRef", integration, subject, path)
	}
	name := value.ValueFrom.CredentialFieldRef.Name
	if name == "" {
		return fmt.Errorf("config validation: integration %q %s %s.valueFrom.credentialFieldRef.name is required", integration, subject, path)
	}
	if _, ok := credentialNames[name]; !ok {
		return fmt.Errorf("config validation: integration %q %s %s.valueFrom.credentialFieldRef references undeclared credential %q", integration, subject, path, name)
	}
	return nil
}

func validateUIConfig(cfg UIConfig) error {
	if cfg.Disabled {
		if cfg.Config.Kind != 0 {
			return fmt.Errorf(`config validation: ui.config is not supported when ui.provider is "none"`)
		}
		return nil
	}
	if cfg.Provider == nil {
		return nil
	}
	return validateExternalPlugin("ui", "provider", cfg.Provider)
}

func validateServerListeners(cfg ServerConfig) error {
	public := cfg.PublicListener()
	if public.Port <= 0 {
		return fmt.Errorf("config validation: server.public.port must be greater than zero")
	}
	if _, err := net.ResolveTCPAddr("tcp", net.JoinHostPort(public.Host, strconv.Itoa(public.Port))); err != nil {
		return fmt.Errorf("config validation: server.public is invalid: %w", err)
	}

	management, ok := cfg.ManagementListener()
	if !ok {
		if cfg.Management.Host != "" {
			return fmt.Errorf("config validation: server.management.port is required when server.management.host is set")
		}
		return nil
	}
	if management.Port <= 0 {
		return fmt.Errorf("config validation: server.management.port must be greater than zero")
	}
	managementAddr := net.JoinHostPort(management.Host, strconv.Itoa(management.Port))
	if _, err := net.ResolveTCPAddr("tcp", managementAddr); err != nil {
		return fmt.Errorf("config validation: server.management is invalid: %w", err)
	}
	if managementAddr == cfg.PublicAddr() {
		return fmt.Errorf("config validation: server.management must differ from server.public")
	}
	return nil
}

func validateDatastoreConfig(cfg *Config) error {
	if cfg.Datastore.Provider != nil {
		return fmt.Errorf("config validation: datastore.provider is no longer supported; use the datastores section with datastore: <name>")
	}
	if cfg.Datastore.Resource != "" {
		if _, ok := cfg.Datastores[cfg.Datastore.Resource]; !ok {
			return fmt.Errorf("config validation: datastore references unknown datastore %q", cfg.Datastore.Resource)
		}
	}
	return nil
}

func validateDatastores(cfg *Config) error {
	for name := range cfg.Datastores {
		ds := cfg.Datastores[name]
		if ds.Provider == nil {
			return fmt.Errorf("config validation: datastores.%s.provider is required", name)
		}
	}
	for name := range cfg.Plugins {
		intg := cfg.Plugins[name]
		if err := validateDatastoreBindings(name, intg.Datastores, cfg); err != nil {
			return err
		}
	}
	return nil
}

func validateDatastoreBindings(integrationName string, bindings map[string]string, cfg *Config) error {
	for alias, resourceName := range bindings {
		if _, ok := cfg.Datastores[resourceName]; !ok {
			return fmt.Errorf("config validation: integration %q datastore binding %q references unknown datastore %q", integrationName, alias, resourceName)
		}
	}
	return nil
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
		if strings.HasPrefix(c.SecretRef, "secret://") {
			return fmt.Errorf("config validation: egress.credentials[%d]: secret_ref must be a bare secret name without secret://", i)
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
