package config

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/pluginsource"
	"github.com/valon-technologies/gestalt/server/internal/providerpkg"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

// ValidateStructure checks config shape: integration references, plugin
// declarations, connection references, URL template params, egress rules.
// Called by Load (and therefore by init, validate, and serve). Does not require
// runtime secrets like encryption_key.
func ValidateStructure(cfg *Config) error {
	if err := NormalizeCompatibility(cfg); err != nil {
		return err
	}
	if err := normalizeMountedUIPaths(cfg); err != nil {
		return err
	}
	if err := validateAuthorizationPolicies(cfg); err != nil {
		return err
	}
	if err := validateServerListeners(cfg.Server); err != nil {
		return err
	}
	if cfg.Server.APITokenTTL != "" {
		if _, err := ParseDuration(cfg.Server.APITokenTTL); err != nil {
			return fmt.Errorf("config validation: server.apiTokenTtl: %w", err)
		}
	}
	if err := validateEgress(&cfg.Server.Egress); err != nil {
		return err
	}

	for _, collection := range []struct {
		kind    HostProviderKind
		entries map[string]*ProviderEntry
	}{
		{HostProviderKindAuth, cfg.Providers.Auth},
		{HostProviderKindSecrets, cfg.Providers.Secrets},
		{HostProviderKindTelemetry, cfg.Providers.Telemetry},
		{HostProviderKindAudit, cfg.Providers.Audit},
	} {
		if err := validateHostProviderEntries(collection.kind, collection.entries); err != nil {
			return err
		}
		if _, _, err := cfg.SelectedHostProvider(collection.kind); err != nil {
			return err
		}
	}
	for name, entry := range cfg.Providers.UI {
		if entry == nil {
			return fmt.Errorf("config validation: ui.%s is required", name)
		}
		if err := validatePluginOnlyProviderFields("ui."+name, &entry.ProviderEntry); err != nil {
			return err
		}
		if entry.Disabled {
			continue
		}
		if entry.Source.IsBuiltin() {
			return fmt.Errorf("config validation: ui %q does not support builtin providers; use a provider source reference", name)
		}
		if err := validateProviderEntrySource("ui", name, &entry.ProviderEntry); err != nil {
			return err
		}
		if err := validateAuthorizationPolicyReference(cfg, "ui", name, entry.AuthorizationPolicy); err != nil {
			return err
		}
		if entry.Path == "" {
			return fmt.Errorf("config validation: ui.%s.path is required", name)
		}
	}
	if err := validateMountedUICollisions(cfg); err != nil {
		return err
	}

	// Validate indexeddbs
	if err := validateDatastoreConfig(cfg); err != nil {
		return err
	}
	if err := validateFileAPIConfig(cfg); err != nil {
		return err
	}

	// Validate plugins
	for name, entry := range cfg.Plugins {
		if err := validatePlugin(cfg, name, entry); err != nil {
			return err
		}
	}
	return nil
}

func validateHostProviderEntries(kind HostProviderKind, entries map[string]*ProviderEntry) error {
	for name, entry := range entries {
		if entry == nil {
			return fmt.Errorf("config validation: providers.%s.%s is required", kind, name)
		}
		if err := validatePluginOnlyProviderFields("providers."+string(kind)+"."+name, entry); err != nil {
			return err
		}
		switch kind {
		case HostProviderKindAuth:
			if entry.Source.IsBuiltin() {
				return fmt.Errorf("config validation: auth provider %q does not support builtin providers; use a provider source reference or omit auth", name)
			}
			if err := validateProviderEntrySource("auth", name, entry); err != nil {
				return err
			}
		case HostProviderKindSecrets, HostProviderKindTelemetry:
			if !entry.Disabled && !entry.Source.IsBuiltin() {
				if err := validateProviderEntrySource(string(kind), name, entry); err != nil {
					return err
				}
			}
		case HostProviderKindAudit:
			if entry.Disabled {
				continue
			}
			if entry.Source.IsBuiltin() {
				if err := validateBuiltinAudit(entry); err != nil {
					return err
				}
			} else {
				if err := validateProviderEntrySource("audit", name, entry); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func validateBuiltinAudit(entry *ProviderEntry) error {
	name := entry.Source.Builtin
	switch name {
	case "", "inherit", "noop":
		if entry.Config.Kind != 0 {
			return fmt.Errorf("config validation: audit.config is not supported when audit.provider is %q", name)
		}
		return nil
	case "stdout":
		if entry.Config.Kind == 0 {
			return nil
		}
		var stdoutCfg struct {
			Level  string `yaml:"level"`
			Format string `yaml:"format"`
		}
		if err := entry.Config.Decode(&stdoutCfg); err != nil {
			return fmt.Errorf("config validation: stdout audit: parsing config: %w", err)
		}
		return nil
	case "otlp":
		if entry.Config.Kind == 0 {
			return nil
		}
		var otlpCfg struct {
			Protocol string `yaml:"protocol"`
			Logs     struct {
				Exporter string `yaml:"exporter"`
			} `yaml:"logs"`
		}
		if err := entry.Config.Decode(&otlpCfg); err != nil {
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
		return fmt.Errorf("config validation: unknown audit provider %q", name)
	}
}

func validateProviderEntrySource(kind, name string, entry *ProviderEntry) error {
	if entry == nil {
		return nil
	}
	src := entry.Source
	if src.IsBuiltin() {
		return nil
	}
	modeCount := 0
	if src.IsLocal() {
		modeCount++
	}
	if src.IsManaged() {
		modeCount++
	}
	if modeCount == 0 && !entry.Disabled {
		return fmt.Errorf("config validation: %s %q source.path or source.ref is required", kind, name)
	}
	if modeCount > 1 {
		return fmt.Errorf("config validation: %s %q source.path and source.ref are mutually exclusive", kind, name)
	}
	if src.IsManaged() {
		if _, err := pluginsource.Parse(src.Ref); err != nil {
			return fmt.Errorf("config validation: %s %q source.ref: %w", kind, name, err)
		}
		if src.Version == "" {
			return fmt.Errorf("config validation: %s %q source.version is required when source.ref is set", kind, name)
		}
		if err := pluginsource.ValidateVersion(src.Version); err != nil {
			return fmt.Errorf("config validation: %s %q source.version: %w", kind, name, err)
		}
	}
	if src.IsLocal() && src.Version != "" {
		return fmt.Errorf("config validation: %s %q source.version is only valid with source.ref", kind, name)
	}
	if src.Auth != nil {
		if !src.IsManaged() {
			return fmt.Errorf("config validation: %s %q source.auth is only valid with source.ref", kind, name)
		}
		if strings.TrimSpace(src.Auth.Token) == "" {
			return fmt.Errorf("config validation: %s %q source.auth.token is required when source.auth is set", kind, name)
		}
	}
	return nil
}

// ValidateResolvedStructure checks integration fields whose support depends on
// resolved managed plugin manifests.
func ValidateResolvedStructure(cfg *Config) error {
	for name, entry := range cfg.Plugins {
		if entry == nil {
			return fmt.Errorf("config validation: integration %q requires a source", name)
		}
		if err := validateManifestBackedIntegration(name, entry); err != nil {
			return err
		}
	}
	for name, entry := range cfg.Providers.UI {
		if entry == nil {
			return fmt.Errorf("config validation: ui %q requires a source", name)
		}
		if entry.Disabled || entry.AuthorizationPolicy == "" {
			continue
		}
		if entry.ResolvedManifest == nil || entry.ManifestSpec() == nil {
			return fmt.Errorf("config validation: ui %q authorizationPolicy requires a resolved webui manifest", name)
		}
		if err := providerpkg.ValidatePolicyBoundWebUIRoutes(entry.ManifestSpec().Routes); err != nil {
			return fmt.Errorf("config validation: ui %q authorizationPolicy: %w", name, err)
		}
	}
	return nil
}

// ValidateRuntime checks runtime-only requirements: encryption key plus the
// required top-level datastore provider.
func ValidateRuntime(cfg *Config) error {
	name, _, err := cfg.SelectedIndexedDBProvider()
	if err != nil {
		return err
	}
	if name == "" {
		return fmt.Errorf("config validation: server.providers.indexeddb is required (set server.providers.indexeddb or mark one providers.indexeddb entry default: true)")
	}
	if cfg.Server.EncryptionKey == "" {
		return fmt.Errorf("config validation: server.encryption_key is required")
	}
	return nil
}

func validatePlugin(cfg *Config, name string, entry *ProviderEntry) error {
	if entry == nil {
		return fmt.Errorf("config validation: plugin %q requires a source", name)
	}
	if entry.Default {
		return fmt.Errorf("config validation: plugins.%s.default is not supported on plugins", name)
	}
	entry.IndexedDBSchema = strings.TrimSpace(entry.IndexedDBSchema)
	if err := validateProviderEntrySource("plugin", name, entry); err != nil {
		return err
	}
	if err := validatePluginIndexedDBBindings(cfg, name, entry); err != nil {
		return err
	}
	if err := validatePluginFileAPIBindings(cfg, name, entry); err != nil {
		return err
	}
	if err := validateAuthorizationPolicyReference(cfg, "plugin", name, entry.AuthorizationPolicy); err != nil {
		return err
	}
	return validateManifestBackedIntegration(name, entry)
}

func validateDatastoreConfig(cfg *Config) error {
	for name, entry := range cfg.Providers.IndexedDB {
		if entry == nil {
			return fmt.Errorf("config validation: providers.indexeddb.%s is required", name)
		}
		if err := validatePluginOnlyProviderFields("providers.indexeddb."+name, entry); err != nil {
			return err
		}
		if err := validateProviderEntrySource("indexeddb", name, entry); err != nil {
			return err
		}
	}
	if _, _, err := cfg.SelectedIndexedDBProvider(); err != nil {
		return err
	}
	return nil
}

func validatePluginOnlyProviderFields(subject string, entry *ProviderEntry) error {
	if entry == nil {
		return nil
	}
	if len(entry.IndexedDBs) > 0 {
		return fmt.Errorf("config validation: %s.indexeddb is only supported on plugins.*", subject)
	}
	if len(entry.FileAPIs) > 0 {
		return fmt.Errorf("config validation: %s.fileapi is only supported on plugins.*", subject)
	}
	if strings.TrimSpace(entry.IndexedDBSchema) != "" {
		return fmt.Errorf("config validation: %s.indexeddbSchema is only supported on plugins.*", subject)
	}
	if entry.Surfaces != nil {
		return fmt.Errorf("config validation: %s.surfaces is only supported on plugins.*", subject)
	}
	if entry.AuthorizationPolicy != "" && !strings.HasPrefix(subject, "ui.") {
		return fmt.Errorf("config validation: %s.authorizationPolicy is only supported on plugins.* and ui.*", subject)
	}
	return nil
}

func validateAuthorizationPolicies(cfg *Config) error {
	for name, policy := range cfg.Authorization.Policies {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("config validation: authorization.policies keys must be non-empty")
		}
		switch strings.TrimSpace(policy.Default) {
		case "", "allow", "deny":
		default:
			return fmt.Errorf("config validation: authorization.policies.%s.default must be %q or %q", name, "allow", "deny")
		}
		seenSubjectIDs := make(map[string]int, len(policy.Members))
		seenEmails := make(map[string]int, len(policy.Members))
		for i, member := range policy.Members {
			subjectID := strings.TrimSpace(member.SubjectID)
			email := strings.ToLower(strings.TrimSpace(member.Email))
			role := strings.TrimSpace(member.Role)
			switch {
			case role == "":
				return fmt.Errorf("config validation: authorization.policies.%s.members[%d].role is required", name, i)
			case subjectID == "" && email == "":
				return fmt.Errorf("config validation: authorization.policies.%s.members[%d] must set subjectID or email", name, i)
			case subjectID != "" && email != "":
				return fmt.Errorf("config validation: authorization.policies.%s.members[%d] may not set both subjectID and email", name, i)
			}
			if subjectID != "" {
				if prev, exists := seenSubjectIDs[subjectID]; exists {
					return fmt.Errorf("config validation: authorization.policies.%s.members[%d].subjectID duplicates members[%d]", name, i, prev)
				}
				seenSubjectIDs[subjectID] = i
			}
			if email != "" {
				if prev, exists := seenEmails[email]; exists {
					return fmt.Errorf("config validation: authorization.policies.%s.members[%d].email duplicates members[%d]", name, i, prev)
				}
				seenEmails[email] = i
			}
		}
	}
	return nil
}

func validateAuthorizationPolicyReference(cfg *Config, kind, name, policy string) error {
	policy = strings.TrimSpace(policy)
	if policy == "" {
		return nil
	}
	if _, ok := cfg.Authorization.Policies[policy]; !ok {
		return fmt.Errorf("config validation: %s %q authorizationPolicy references unknown policy %q", kind, name, policy)
	}
	return nil
}

func validatePluginIndexedDBBindings(cfg *Config, name string, entry *ProviderEntry) error {
	if entry == nil {
		return nil
	}
	seen := make(map[string]struct{}, len(entry.IndexedDBs))
	envNames := make(map[string]string, len(entry.IndexedDBs))
	restrictedBindingCount := 0
	unrestrictedBindingCount := 0
	for i := range entry.IndexedDBs {
		binding := &entry.IndexedDBs[i]
		binding.Name = strings.TrimSpace(binding.Name)
		if binding.Name == "" {
			return fmt.Errorf("config validation: plugin %q indexeddb[%d] is required", name, i)
		}
		if _, exists := seen[binding.Name]; exists {
			return fmt.Errorf("config validation: plugin %q indexeddb[%d] duplicates %q", name, i, binding.Name)
		}
		if _, ok := cfg.Providers.IndexedDB[binding.Name]; !ok {
			return fmt.Errorf("config validation: plugin %q indexeddb[%d] references unknown indexeddb %q", name, i, binding.Name)
		}
		envName := indexedDBSocketEnv(binding.Name)
		if otherBinding, exists := envNames[envName]; exists {
			return fmt.Errorf("config validation: plugin %q indexeddb[%d] %q conflicts with %q after IndexedDB env normalization (%s)", name, i, binding.Name, otherBinding, envName)
		}
		seenStores := make(map[string]struct{}, len(binding.ObjectStores))
		if len(binding.ObjectStores) > 0 {
			restrictedBindingCount++
		} else {
			unrestrictedBindingCount++
		}
		for j, store := range binding.ObjectStores {
			store = strings.TrimSpace(store)
			if store == "" {
				return fmt.Errorf("config validation: plugin %q indexeddb[%d].objectStore[%d] is required", name, i, j)
			}
			if _, exists := seenStores[store]; exists {
				return fmt.Errorf("config validation: plugin %q indexeddb[%d].objectStore[%d] duplicates %q", name, i, j, store)
			}
			seenStores[store] = struct{}{}
			binding.ObjectStores[j] = store
		}
		seen[binding.Name] = struct{}{}
		envNames[envName] = binding.Name
	}
	if restrictedBindingCount > 0 && unrestrictedBindingCount > 1 {
		return fmt.Errorf("config validation: plugin %q indexeddb may declare at most one unrestricted binding when objectStore allowlists are used", name)
	}
	return nil
}

func indexedDBSocketEnv(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "GESTALT_INDEXEDDB_SOCKET"
	}
	var b strings.Builder
	b.WriteString("GESTALT_INDEXEDDB_SOCKET")
	b.WriteByte('_')
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - ('a' - 'A'))
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func validateFileAPIConfig(cfg *Config) error {
	for name, entry := range cfg.Providers.FileAPI {
		if entry == nil {
			return fmt.Errorf("config validation: providers.fileapi.%s is required", name)
		}
		if err := validatePluginOnlyProviderFields("providers.fileapi."+name, entry); err != nil {
			return err
		}
		if err := validateProviderEntrySource("fileapi", name, entry); err != nil {
			return err
		}
	}
	return nil
}

func validatePluginFileAPIBindings(cfg *Config, name string, entry *ProviderEntry) error {
	if entry == nil {
		return nil
	}
	seen := make(map[string]struct{}, len(entry.FileAPIs))
	envNames := make(map[string]string, len(entry.FileAPIs))
	for i, binding := range entry.FileAPIs {
		binding = strings.TrimSpace(binding)
		if binding == "" {
			return fmt.Errorf("config validation: plugin %q fileapi[%d] is required", name, i)
		}
		if _, exists := seen[binding]; exists {
			return fmt.Errorf("config validation: plugin %q fileapi[%d] duplicates %q", name, i, binding)
		}
		if _, ok := cfg.Providers.FileAPI[binding]; !ok {
			return fmt.Errorf("config validation: plugin %q fileapi[%d] references unknown fileapi %q", name, i, binding)
		}
		envName := fileAPISocketEnv(binding)
		if otherBinding, exists := envNames[envName]; exists {
			return fmt.Errorf("config validation: plugin %q fileapi[%d] %q conflicts with %q after FileAPI env normalization (%s)", name, i, binding, otherBinding, envName)
		}
		seen[binding] = struct{}{}
		envNames[envName] = binding
		entry.FileAPIs[i] = binding
	}
	return nil
}

func fileAPISocketEnv(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "GESTALT_FILEAPI_SOCKET"
	}
	var b strings.Builder
	b.WriteString("GESTALT_FILEAPI_SOCKET")
	b.WriteByte('_')
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - ('a' - 'A'))
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func normalizeMountedUIPaths(cfg *Config) error {
	for name, entry := range cfg.Providers.UI {
		if entry == nil || entry.Disabled {
			continue
		}
		normalized, err := normalizeMountedUIPath(entry.Path)
		if err != nil {
			return fmt.Errorf("config validation: ui.%s.path: %w", name, err)
		}
		entry.Path = normalized
	}
	return nil
}

func normalizeMountedUIPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	if !strings.HasPrefix(path, "/") {
		return "", fmt.Errorf("must start with /")
	}
	path = strings.TrimRight(path, "/")
	if path == "" {
		return "/", nil
	}
	if strings.ContainsAny(path, "{}*") {
		return "", fmt.Errorf("route patterns are not supported")
	}
	return path, nil
}

func validateMountedUICollisions(cfg *Config) error {
	reserved := []string{
		"/api",
		"/api/v1",
		AuthCallbackPath,
		IntegrationCallbackPath,
		"/mcp",
		"/admin",
		"/metrics",
		"/health",
		"/ready",
	}
	names := mapsKeys(cfg.Providers.UI)
	sort.Strings(names)
	for i, name := range names {
		entry := cfg.Providers.UI[name]
		if entry == nil || entry.Disabled {
			continue
		}
		if entry.Path == "/" {
			for _, otherName := range names[i+1:] {
				other := cfg.Providers.UI[otherName]
				if other == nil || other.Disabled {
					continue
				}
				if other.Path == "/" {
					return fmt.Errorf("config validation: ui.%s.path %q conflicts with ui.%s.path %q", name, entry.Path, otherName, other.Path)
				}
			}
			continue
		}
		for _, reservedPath := range reserved {
			if mountedUIPathsConflict(entry.Path, reservedPath) {
				return fmt.Errorf("config validation: ui.%s.path %q conflicts with reserved path %q", name, entry.Path, reservedPath)
			}
		}
		for _, otherName := range names[i+1:] {
			other := cfg.Providers.UI[otherName]
			if other == nil || other.Disabled {
				continue
			}
			if mountedUIPathsConflict(entry.Path, other.Path) {
				return fmt.Errorf("config validation: ui.%s.path %q conflicts with ui.%s.path %q", name, entry.Path, otherName, other.Path)
			}
		}
	}
	return nil
}

func mountedUIPathsConflict(a, b string) bool {
	if a == b {
		return true
	}
	return strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

func mapsKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

type inlineConnectionReference struct {
	field    string
	name     string
	required bool
	context  string
}

func manifestBackedConnectionReferences(plugin *ProviderEntry, provider *providermanifestv1.Spec) []inlineConnectionReference {
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

func validateManifestBackedConnectionReferences(name string, plugin *ProviderEntry, provider *providermanifestv1.Spec) error {
	return validateConnectionReferences(name, declaredManifestBackedConnections(plugin, provider), manifestBackedConnectionReferences(plugin, provider))
}

func validateManifestBackedConnectionDefaults(name string, plugin *ProviderEntry, provider *providermanifestv1.Spec) error {
	return validateConnectionDefaults(name, "integration", len(declaredManifestBackedConnections(plugin, provider)), manifestBackedConnectionReferences(plugin, provider))
}

func validateExecutableConnectionAuthSupport(name string, plugin *ProviderEntry, provider *providermanifestv1.Spec) error {
	supportsMCPOAuth := provider != nil && provider.MCPURL() != ""
	if conn := EffectivePluginConnectionDef(plugin, provider); conn.Auth.Type == providermanifestv1.AuthTypeMCPOAuth && !supportsMCPOAuth {
		return fmt.Errorf("config validation: integration %q plugin auth type %q requires an MCP surface", name, providermanifestv1.AuthTypeMCPOAuth)
	}

	declared := declaredManifestBackedConnections(plugin, provider)
	declaredNames := make([]string, 0, len(declared))
	for connName := range declared {
		declaredNames = append(declaredNames, connName)
	}
	sort.Strings(declaredNames)
	for _, connName := range declaredNames {
		conn, ok := EffectiveNamedConnectionDef(plugin, provider, connName)
		if !ok || conn.Auth.Type != providermanifestv1.AuthTypeMCPOAuth {
			continue
		}
		if !supportsMCPOAuth {
			return fmt.Errorf("config validation: integration %q connection %q auth type %q requires an MCP surface", name, connName, providermanifestv1.AuthTypeMCPOAuth)
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

func declaredManifestBackedConnections(plugin *ProviderEntry, provider *providermanifestv1.Spec) map[string]struct{} {
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

func validateManifestBackedIntegration(name string, entry *ProviderEntry) error {
	if entry == nil {
		return nil
	}
	effectiveProvider := entry.ManifestSpec()
	if effectiveProvider != nil {
		if err := validateManifestBackedConnectionReferences(name, entry, effectiveProvider); err != nil {
			return err
		}
		if err := validateManifestBackedConnectionDefaults(name, entry, effectiveProvider); err != nil {
			return err
		}
	}
	if err := validateExecutableConnectionAuthSupport(name, entry, effectiveProvider); err != nil {
		return err
	}
	if err := validateConnectionAuthMappings(name, EffectivePluginConnectionDef(entry, effectiveProvider).Auth, "plugin"); err != nil {
		return err
	}
	declared := declaredManifestBackedConnections(entry, effectiveProvider)
	names := make([]string, 0, len(declared))
	for connName := range declared {
		if connName == PluginConnectionName {
			continue
		}
		names = append(names, connName)
	}
	sort.Strings(names)
	for _, connName := range names {
		conn, ok := EffectiveNamedConnectionDef(entry, effectiveProvider, connName)
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

func validateEgress(cfg *EgressConfig) error {
	switch cfg.DefaultAction {
	case "", "allow", "deny":
	default:
		return fmt.Errorf("config validation: egress.default_action must be \"allow\" or \"deny\", got %q", cfg.DefaultAction)
	}
	return nil
}
