package config

import (
	"encoding/json"
	"fmt"
	"maps"
	"net"
	"net/url"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	cronv3 "github.com/robfig/cron/v3"
	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/internal/providerregistry"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/plugins/packageio"
	"github.com/valon-technologies/gestalt/server/services/s3"
)

// ValidateStructure checks config shape: integration references, plugin
// declarations, connection references, URL template params, egress rules.
// Called by Load (and therefore by init, validate, and serve). Does not require
// runtime secrets like encryption_key.
func ValidateStructure(cfg *Config) error {
	if err := CanonicalizeStructure(cfg); err != nil {
		return err
	}
	return ValidateCanonicalStructure(cfg)
}

// CanonicalizeStructure applies the config-shape normalization required before
// structural validation or bootstrap consumers operate on the config.
func CanonicalizeStructure(cfg *Config) error {
	if err := validateAPIVersion(cfg); err != nil {
		return err
	}
	if err := normalizeConfigShape(cfg); err != nil {
		return err
	}
	if err := normalizeConnectionBindings(cfg); err != nil {
		return err
	}
	pluginOwnedUIBindings := pluginOwnedUIBindings(cfg)
	if err := normalizeMountedUIPaths(cfg, pluginOwnedUIBindings); err != nil {
		return err
	}
	return nil
}

// ValidateCanonicalStructure checks config shape assuming canonicalization has
// already run on the config.
func ValidateCanonicalStructure(cfg *Config) error {
	pluginOwnedUIBindings := pluginOwnedUIBindings(cfg)
	if err := validateAuthorizationPolicies(cfg); err != nil {
		return err
	}
	if err := validateServerListeners(cfg.Server); err != nil {
		return err
	}
	if err := validateAdminConfig(cfg); err != nil {
		return err
	}
	if err := validateProviderDevConfig(cfg); err != nil {
		return err
	}
	if cfg.Server.APITokenTTL != "" {
		if _, err := ParseDuration(cfg.Server.APITokenTTL); err != nil {
			return fmt.Errorf("config validation: server.apiTokenTtl: %w", err)
		}
	}
	if threshold := cfg.Server.Agent.DefaultToolNarrowingThreshold; threshold != nil && *threshold < 0 {
		return fmt.Errorf("config validation: server.agent.defaultToolNarrowingThreshold must be non-negative")
	}
	if err := validateEgress(&cfg.Server.Egress); err != nil {
		return err
	}
	if err := validateRuntimeConfig(cfg); err != nil {
		return err
	}
	if err := validateTopLevelConnections(cfg); err != nil {
		return err
	}
	if err := validateProviderRepositories(cfg); err != nil {
		return err
	}

	for _, collection := range []struct {
		kind    HostProviderKind
		entries map[string]*ProviderEntry
	}{
		{HostProviderKindAuthentication, cfg.Providers.Authentication},
		{HostProviderKindAuthorization, cfg.Providers.Authorization},
		{HostProviderKindExternalCredentials, cfg.Providers.ExternalCredentials},
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
			if _, ok := pluginOwnedUIBindings[name]; ok {
				continue
			}
			return fmt.Errorf("config validation: ui.%s.path is required", name)
		}
	}
	if err := validateMountedUICollisions(cfg, pluginOwnedUIBindings); err != nil {
		return err
	}

	// Validate indexeddbs
	if err := validateDatastoreConfig(cfg); err != nil {
		return err
	}
	if err := validateCacheConfig(cfg); err != nil {
		return err
	}
	if err := validateWorkflowConfig(cfg); err != nil {
		return err
	}
	if err := validateAgentConfig(cfg); err != nil {
		return err
	}
	if err := validateS3Config(cfg); err != nil {
		return err
	}
	if err := validateConfigSecretRefs(cfg); err != nil {
		return err
	}

	// Validate plugins
	for name, entry := range cfg.Plugins {
		if err := validatePlugin(cfg, name, entry); err != nil {
			return err
		}
	}
	return validateWorkflowsConfig(cfg)
}

func validateProviderDevConfig(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	state := DevAttachmentState(strings.TrimSpace(string(cfg.Server.Dev.AttachmentState)))
	cfg.Server.Dev.AttachmentState = state
	switch state {
	case "", DevAttachmentStateIndexedDB:
		return nil
	default:
		return fmt.Errorf("config validation: server.dev.attachmentState %q is not supported; use %q", state, DevAttachmentStateIndexedDB)
	}
}

func validateAPIVersion(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	// YAML roots require apiVersion before this point; direct programmatic
	// Config values may omit it and use current source normalization.
	apiVersion := strings.TrimSpace(cfg.APIVersion)
	switch apiVersion {
	case "", ConfigAPIVersion:
		return nil
	default:
		return fmt.Errorf("config validation: unsupported apiVersion %q", apiVersion)
	}
}

func requiredAPIVersionError() error {
	return fmt.Errorf("config validation: apiVersion is required; supported value is %q", ConfigAPIVersion)
}

func validateProviderRepositories(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	for name, repo := range cfg.ProviderRepositories {
		if err := providerregistry.ValidateRepositoryName(name); err != nil {
			return fmt.Errorf("config validation: providerRepositories.%s: %w", name, err)
		}
		if strings.TrimSpace(repo.URL) == "" {
			return fmt.Errorf("config validation: providerRepositories.%s.url is required", name)
		}
	}
	return nil
}

func normalizeConnectionBindings(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	if cfg.Connections == nil {
		cfg.Connections = map[string]*ConnectionDef{}
	}
	for id, conn := range cfg.Connections {
		id = strings.TrimSpace(id)
		if id == "" {
			return fmt.Errorf("config validation: connections contains an empty connection id")
		}
		if conn == nil {
			return fmt.Errorf("config validation: connections.%s is required", id)
		}
		conn.Ref = strings.TrimSpace(conn.Ref)
		if conn.Ref != "" {
			return fmt.Errorf("config validation: connections.%s.ref is not allowed on top-level connections", id)
		}
		if conn.Exposure != "" {
			return fmt.Errorf("config validation: connections.%s.exposure is binding-only; set exposure where the connection is used", id)
		}
		conn.ConnectionID = id
		conn.BindingResolved = false
	}

	normalizeEntry := func(kind, name string, entry *ProviderEntry) error {
		if entry == nil || len(entry.Connections) == 0 {
			return nil
		}
		for _, rawLocalName := range slices.Sorted(maps.Keys(entry.Connections)) {
			binding := entry.Connections[rawLocalName]
			if binding == nil {
				return fmt.Errorf("config validation: %s.%s.connections.%s is required", kind, name, rawLocalName)
			}
			localName := ResolveConnectionAlias(rawLocalName)
			if localName == "" {
				return fmt.Errorf("config validation: %s.%s.connections contains an empty connection name", kind, name)
			}
			if localName != rawLocalName {
				if existing := entry.Connections[localName]; existing != nil && existing != binding {
					return fmt.Errorf("config validation: %s.%s.connections.%s conflicts with alias %q", kind, name, localName, rawLocalName)
				}
				delete(entry.Connections, rawLocalName)
				entry.Connections[localName] = binding
			}
			ref := strings.TrimSpace(binding.Ref)
			if ref == "" {
				if binding.ConnectionID == "" {
					binding.ConnectionID = inlineConnectionID(kind, name, localName)
				}
				if !binding.BindingResolved && ConnectionModeForConnection(*binding) == core.ConnectionModeUser && connectionBindingRequiresUserCredential(binding) {
					return fmt.Errorf("config validation: %s.%s.connections.%s user-owned inline connections are not supported; define a top-level connection and reference it with ref", kind, name, localName)
				}
				continue
			}
			if binding.BindingResolved && binding.ConnectionID == ref {
				continue
			}
			if connectionBindingHasCredentialMaterial(binding) {
				return fmt.Errorf("config validation: %s.%s.connections.%s uses ref %q and cannot override mode, auth, or params", kind, name, localName, ref)
			}
			global := cfg.Connections[ref]
			if global == nil {
				return fmt.Errorf("config validation: %s.%s.connections.%s references unknown top-level connection %q", kind, name, localName, ref)
			}
			resolved := cloneConnectionDef(*global)
			resolved.Ref = ref
			resolved.ConnectionID = ref
			resolved.BindingResolved = true
			if binding.DisplayName != "" {
				resolved.DisplayName = binding.DisplayName
			}
			resolved.Exposure = binding.Exposure
			if binding.CredentialRefresh != nil {
				resolved.CredentialRefresh = cloneCredentialRefreshDef(binding.CredentialRefresh)
			}
			entry.Connections[localName] = &resolved
		}
		return nil
	}
	for name, entry := range cfg.Plugins {
		if err := normalizeEntry("plugins", name, entry); err != nil {
			return err
		}
	}
	for name, entry := range cfg.Providers.Agent {
		if err := normalizeEntry("providers.agent", name, entry); err != nil {
			return err
		}
	}
	return nil
}

func inlineConnectionID(kind, name, localName string) string {
	return "inline:" + strings.TrimSpace(kind) + ":" + strings.TrimSpace(name) + ":" + strings.TrimSpace(localName)
}

func connectionBindingHasCredentialMaterial(conn *ConnectionDef) bool {
	if conn == nil {
		return false
	}
	return conn.Mode != "" ||
		conn.Auth.Type != "" ||
		conn.Auth.Token != "" ||
		conn.Auth.GrantType != "" ||
		conn.Auth.RefreshToken != "" ||
		conn.Auth.AuthorizationURL != "" ||
		conn.Auth.TokenURL != "" ||
		conn.Auth.ClientID != "" ||
		conn.Auth.ClientSecret != "" ||
		conn.Auth.RedirectURL != "" ||
		conn.Auth.ClientAuth != "" ||
		conn.Auth.TokenExchange != "" ||
		conn.Auth.TokenPrefix != "" ||
		len(conn.Auth.Scopes) > 0 ||
		conn.Auth.ScopeParam != "" ||
		conn.Auth.ScopeSeparator != "" ||
		conn.Auth.PKCE ||
		len(conn.Auth.AuthorizationParams) > 0 ||
		len(conn.Auth.TokenParams) > 0 ||
		len(conn.Auth.RefreshParams) > 0 ||
		conn.Auth.AcceptHeader != "" ||
		conn.Auth.AccessTokenPath != "" ||
		len(conn.Auth.TokenMetadata) > 0 ||
		len(conn.Auth.Credentials) > 0 ||
		conn.Auth.AuthMapping != nil ||
		len(conn.ConnectionParams) > 0
}

func connectionBindingRequiresUserCredential(conn *ConnectionDef) bool {
	if conn == nil {
		return false
	}
	switch conn.Auth.Type {
	case providermanifestv1.AuthTypeBearer, providermanifestv1.AuthTypeManual, providermanifestv1.AuthTypeOAuth2, providermanifestv1.AuthTypeMCPOAuth:
		return true
	default:
		return false
	}
}

func cloneConnectionDef(src ConnectionDef) ConnectionDef {
	dst := src
	dst.Auth = cloneConnectionAuthDef(src.Auth)
	dst.ConnectionParams = maps.Clone(src.ConnectionParams)
	dst.CredentialRefresh = cloneCredentialRefreshDef(src.CredentialRefresh)
	dst.PostConnect = providermanifestv1.CloneProviderPostConnect(src.PostConnect)
	return dst
}

func cloneCredentialRefreshDef(src *CredentialRefreshDef) *CredentialRefreshDef {
	if src == nil {
		return nil
	}
	dst := *src
	return &dst
}

func cloneConnectionAuthDef(src ConnectionAuthDef) ConnectionAuthDef {
	dst := src
	dst.Scopes = slices.Clone(src.Scopes)
	dst.AuthorizationParams = maps.Clone(src.AuthorizationParams)
	dst.TokenParams = maps.Clone(src.TokenParams)
	dst.RefreshParams = maps.Clone(src.RefreshParams)
	dst.TokenMetadata = slices.Clone(src.TokenMetadata)
	dst.Credentials = slices.Clone(src.Credentials)
	dst.AuthMapping = CloneAuthMapping(src.AuthMapping)
	return dst
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
		case HostProviderKindAuthentication:
			if entry.Source.IsBuiltin() {
				return fmt.Errorf("config validation: authentication provider %q does not support builtin providers; use a provider source reference or omit authentication", name)
			}
			if err := validateProviderEntrySource("authentication", name, entry); err != nil {
				return err
			}
		case HostProviderKindAuthorization:
			if entry.Source.IsBuiltin() {
				return fmt.Errorf("config validation: authorization provider %q does not support builtin providers; use a provider source reference or omit authorization", name)
			}
			if err := validateProviderEntrySource("authorization", name, entry); err != nil {
				return err
			}
		case HostProviderKindExternalCredentials:
			if entry.Source.IsBuiltin() {
				return fmt.Errorf("config validation: externalCredentials provider %q does not support builtin providers; use a provider source reference or omit externalCredentials", name)
			}
			if err := validateProviderEntrySource("externalCredentials", name, entry); err != nil {
				return err
			}
		case HostProviderKindSecrets, HostProviderKindTelemetry:
			if !entry.Source.IsBuiltin() {
				if err := validateProviderEntrySource(string(kind), name, entry); err != nil {
					return err
				}
			}
		case HostProviderKindAudit:
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
	auth := src.Auth
	if src.IsBuiltin() {
		if auth != nil {
			return fmt.Errorf("config validation: %s %q auth is only valid with metadata URL sources", kind, name)
		}
		return nil
	}
	if src.UnsupportedURL() != "" {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(src.UnsupportedURL())), "git+") {
			return fmt.Errorf("config validation: %s %q git+ sources are not supported", kind, name)
		}
		return fmt.Errorf("config validation: %s %q only provider-release.yaml metadata URLs are supported for remote sources", kind, name)
	}
	modeCount := 0
	if src.IsLocal() {
		modeCount++
	}
	if src.IsMetadataURL() {
		modeCount++
	}
	if src.IsGitHubRelease() {
		modeCount++
	}
	if src.IsPackage() {
		modeCount++
	}
	if src.IsLocalMetadataPath() {
		modeCount++
	}
	if modeCount == 0 {
		return fmt.Errorf("config validation: %s %q source.path or provider-release metadata URL is required", kind, name)
	}
	if modeCount > 1 {
		return fmt.Errorf("config validation: %s %q source.path and metadata URL sources are mutually exclusive", kind, name)
	}
	if src.IsLocalMetadataPath() {
		if path.Base(filepath.ToSlash(src.MetadataPath())) != "provider-release.yaml" {
			return fmt.Errorf("config validation: %s %q source.path must reference provider-release.yaml metadata", kind, name)
		}
	}
	if src.IsMetadataURL() {
		if parsed, err := url.ParseRequestURI(src.MetadataURL()); err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return fmt.Errorf("config validation: %s %q source metadata URL must be an absolute http(s) URL", kind, name)
		}
	}
	if src.IsGitHubRelease() {
		release := src.GitHubReleaseSource()
		switch {
		case release == nil:
			return fmt.Errorf("config validation: %s %q githubRelease source is required", kind, name)
		case strings.TrimSpace(release.Repo) == "":
			return fmt.Errorf("config validation: %s %q source.githubRelease.repo is required", kind, name)
		case strings.TrimSpace(release.Tag) == "":
			return fmt.Errorf("config validation: %s %q source.githubRelease.tag is required", kind, name)
		case strings.TrimSpace(release.Asset) == "":
			return fmt.Errorf("config validation: %s %q source.githubRelease.asset is required", kind, name)
		}
		repoParts := strings.Split(strings.TrimSpace(release.Repo), "/")
		if len(repoParts) != 2 || strings.TrimSpace(repoParts[0]) == "" || strings.TrimSpace(repoParts[1]) == "" {
			return fmt.Errorf("config validation: %s %q source.githubRelease.repo must be owner/name", kind, name)
		}
	}
	if src.IsPackage() {
		if err := providerregistry.ValidatePackageAddress(src.PackageAddress()); err != nil {
			return fmt.Errorf("config validation: %s %q source.package: %w", kind, name, err)
		}
		if src.PackageRepo() != "" {
			if err := providerregistry.ValidateRepositoryName(src.PackageRepo()); err != nil {
				return fmt.Errorf("config validation: %s %q source.repo: %w", kind, name, err)
			}
		}
	}
	if auth != nil {
		if !src.IsMetadataURL() && !src.IsGitHubRelease() && !src.IsLocalMetadataPath() && !src.IsPackage() {
			return fmt.Errorf("config validation: %s %q auth is only valid with provider-release metadata sources", kind, name)
		}
		if strings.TrimSpace(auth.Token) == "" {
			return fmt.Errorf("config validation: %s %q auth.token is required when auth is set", kind, name)
		}
	}
	return nil
}

func validateConfigSecretRefs(cfg *Config) error {
	referenced, err := ReferencedConfigSecretProviders(cfg)
	if err != nil {
		return fmt.Errorf("config validation: %w", err)
	}
	for name := range referenced {
		entry, ok := cfg.Providers.Secrets[name]
		if !ok || entry == nil {
			return fmt.Errorf("config validation: secret refs reference unknown secrets provider %q", name)
		}
	}
	return nil
}

func validateTopLevelConnections(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	for id, conn := range cfg.Connections {
		if conn == nil {
			return fmt.Errorf("config validation: connections.%s is required", id)
		}
		mode := ConnectionModeForConnection(*conn)
		switch mode {
		case core.ConnectionModeNone, core.ConnectionModePlatform, core.ConnectionModeUser:
		default:
			return fmt.Errorf("config validation: connections.%s mode %q is not supported", id, conn.Mode)
		}
		if err := validateCredentialRefresh(fmt.Sprintf("connections.%s", id), *conn); err != nil {
			return err
		}
		if len(conn.Auth.TokenExchangeDrivers) > 0 {
			if mode != core.ConnectionModePlatform {
				return fmt.Errorf("config validation: connections.%s tokenExchangeDrivers requires mode platform", id)
			}
			if strings.TrimSpace(conn.Auth.RefreshToken) != "" {
				return fmt.Errorf("config validation: connections.%s auth.refreshToken is only supported for oauth2 refresh_token", id)
			}
			for i, driver := range conn.Auth.TokenExchangeDrivers {
				if strings.TrimSpace(driver.Type) == "" {
					return fmt.Errorf("config validation: connections.%s auth.tokenExchangeDrivers[%d].type is required", id, i)
				}
			}
			continue
		}
		if conn.Auth.Type == providermanifestv1.AuthTypeOAuth2 && strings.TrimSpace(conn.Auth.GrantType) == "client_credentials" {
			if mode != core.ConnectionModePlatform {
				return fmt.Errorf("config validation: connections.%s oauth2 client_credentials requires mode platform", id)
			}
			if strings.TrimSpace(conn.Auth.RefreshToken) != "" {
				return fmt.Errorf("config validation: connections.%s auth.refreshToken is only supported for oauth2 refresh_token", id)
			}
			if strings.TrimSpace(conn.Auth.TokenURL) == "" {
				return fmt.Errorf("config validation: connections.%s auth.tokenUrl is required for oauth2 client_credentials", id)
			}
			if strings.TrimSpace(conn.Auth.ClientID) == "" {
				return fmt.Errorf("config validation: connections.%s auth.clientId is required for oauth2 client_credentials", id)
			}
			if strings.TrimSpace(conn.Auth.ClientSecret) == "" {
				return fmt.Errorf("config validation: connections.%s auth.clientSecret is required for oauth2 client_credentials", id)
			}
			continue
		}
		if conn.Auth.Type == providermanifestv1.AuthTypeOAuth2 && strings.TrimSpace(conn.Auth.GrantType) == "refresh_token" {
			if mode != core.ConnectionModePlatform {
				return fmt.Errorf("config validation: connections.%s oauth2 refresh_token requires mode platform", id)
			}
			if strings.TrimSpace(conn.Auth.TokenURL) == "" {
				return fmt.Errorf("config validation: connections.%s auth.tokenUrl is required for oauth2 refresh_token", id)
			}
			if strings.TrimSpace(conn.Auth.ClientID) == "" {
				return fmt.Errorf("config validation: connections.%s auth.clientId is required for oauth2 refresh_token", id)
			}
			if strings.TrimSpace(conn.Auth.ClientSecret) == "" {
				return fmt.Errorf("config validation: connections.%s auth.clientSecret is required for oauth2 refresh_token", id)
			}
			if strings.TrimSpace(conn.Auth.RefreshToken) == "" {
				return fmt.Errorf("config validation: connections.%s auth.refreshToken is required for oauth2 refresh_token", id)
			}
			continue
		}
		if strings.TrimSpace(conn.Auth.GrantType) != "" {
			return fmt.Errorf("config validation: connections.%s auth.grantType is only supported for oauth2 client_credentials or refresh_token", id)
		}
		if strings.TrimSpace(conn.Auth.RefreshToken) != "" {
			return fmt.Errorf("config validation: connections.%s auth.refreshToken is only supported for oauth2 refresh_token", id)
		}
	}
	return nil
}

// ValidateResolvedStructure checks integration fields whose support depends on
// resolved remote plugin manifests.
func ValidateResolvedStructure(cfg *Config) error {
	for name, entry := range cfg.Plugins {
		if entry == nil {
			return fmt.Errorf("config validation: integration %q requires a source", name)
		}
		if err := validatePluginIntegrationConnections(name, entry); err != nil {
			return err
		}
		if strings.TrimSpace(entry.MountPath) == "" || strings.TrimSpace(entry.UI) != "" {
			continue
		}
		if entry.ResolvedManifest == nil || entry.ManifestSpec() == nil || entry.ManifestSpec().UI == nil {
			return fmt.Errorf("config validation: plugins.%s.ui.path requires plugins.%s.ui.bundle or plugin spec.ui", name, name)
		}
	}
	for name, entry := range cfg.Providers.UI {
		if entry == nil {
			return fmt.Errorf("config validation: ui %q requires a source", name)
		}
		if entry.AuthorizationPolicy == "" {
			continue
		}
		if entry.StaticManifestUnavailable {
			continue
		}
		if entry.ResolvedManifest == nil || entry.ManifestSpec() == nil {
			return fmt.Errorf("config validation: ui %q authorizationPolicy requires a resolved ui manifest", name)
		}
		if err := packageio.ValidatePolicyBoundUIRoutes(entry.ManifestSpec().Routes); err != nil {
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

func validateRuntimeConfig(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	cfg.Server.Runtime.DefaultHostedProvider = strings.TrimSpace(cfg.Server.Runtime.DefaultHostedProvider)
	cfg.Server.Runtime.RelayBaseURL = strings.TrimRight(strings.TrimSpace(cfg.Server.Runtime.RelayBaseURL), "/")
	if err := validateRuntimeRelayBaseURL(cfg.Server.Runtime.RelayBaseURL); err != nil {
		return err
	}
	for name, entry := range cfg.Runtime.Providers {
		if entry == nil {
			return fmt.Errorf("config validation: runtime.providers.%s is required", name)
		}
		if err := validatePluginOnlyProviderFields("runtime.providers."+name, &entry.ProviderEntry); err != nil {
			return err
		}
		entry.Driver = RuntimeProviderDriver(strings.TrimSpace(string(entry.Driver)))
		switch entry.Driver {
		case "":
			if entry.Source.IsBuiltin() {
				return fmt.Errorf("config validation: runtime provider %q does not support builtin providers; use a provider source reference or driver: %q", name, RuntimeProviderDriverLocal)
			}
			if err := validateProviderEntrySource("runtime", name, &entry.ProviderEntry); err != nil {
				return err
			}
		case RuntimeProviderDriverLocal:
			if runtimeProviderUsesSource(entry) {
				return fmt.Errorf("config validation: runtime.providers.%s.source is not supported when driver is %q", name, RuntimeProviderDriverLocal)
			}
			if entry.Config.Kind != 0 {
				return fmt.Errorf("config validation: runtime.providers.%s.config is not supported when driver is %q", name, RuntimeProviderDriverLocal)
			}
		default:
			return fmt.Errorf("config validation: runtime.providers.%s.driver %q is not supported; use driver %q or a provider source reference", name, entry.Driver, RuntimeProviderDriverLocal)
		}
	}
	if _, _, err := cfg.SelectedRuntimeProvider(); err != nil {
		return err
	}
	return nil
}

func validateRuntimeRelayBaseURL(raw string) error {
	parsed, err := validateAbsoluteBaseURL("server.runtime.relayBaseUrl", raw)
	if err != nil || parsed == nil {
		return err
	}
	if path := strings.TrimSpace(parsed.EscapedPath()); path != "" && path != "/" {
		return fmt.Errorf("config validation: server.runtime.relayBaseUrl must not include a path")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		return nil
	default:
		return fmt.Errorf("config validation: server.runtime.relayBaseUrl must use http or https")
	}
}

func runtimeProviderUsesSource(entry *RuntimeProviderEntry) bool {
	if entry == nil {
		return false
	}
	return entry.Source.IsBuiltin() ||
		entry.Source.IsMetadataURL() ||
		entry.Source.IsGitHubRelease() ||
		entry.Source.IsLocalMetadataPath() ||
		entry.Source.IsLocal() ||
		entry.Source.UnsupportedURL() != ""
}

func validatePlugin(cfg *Config, name string, entry *ProviderEntry) error {
	if entry == nil {
		return fmt.Errorf("config validation: plugin %q requires a source", name)
	}
	if entry.Default {
		return fmt.Errorf("config validation: plugins.%s.default is not supported on plugins", name)
	}
	if entry.Lifecycle != nil {
		return fmt.Errorf("config validation: plugins.%s.lifecycle is only supported on providers.agent.*", name)
	}
	entry.MountPath = strings.TrimSpace(entry.MountPath)
	entry.UI = strings.TrimSpace(entry.UI)
	if entry.IndexedDB != nil {
		entry.IndexedDB.Provider = strings.TrimSpace(entry.IndexedDB.Provider)
		entry.IndexedDB.DB = strings.TrimSpace(entry.IndexedDB.DB)
		for i, store := range entry.IndexedDB.ObjectStores {
			entry.IndexedDB.ObjectStores[i] = strings.TrimSpace(store)
		}
	}
	if entry.Execution != nil {
		if err := normalizeExecutionConfig("plugins."+name, entry.Execution, false); err != nil {
			return err
		}
	}
	seenInvokes := make(map[string]int, len(entry.Invokes))
	for i := range entry.Invokes {
		entry.Invokes[i].Plugin = strings.TrimSpace(entry.Invokes[i].Plugin)
		entry.Invokes[i].Operation = strings.TrimSpace(entry.Invokes[i].Operation)
		entry.Invokes[i].Surface = strings.ToLower(strings.TrimSpace(entry.Invokes[i].Surface))
		entry.Invokes[i].CredentialMode = providermanifestv1.ConnectionMode(strings.ToLower(strings.TrimSpace(string(entry.Invokes[i].CredentialMode))))
		switch {
		case entry.Invokes[i].Plugin == "":
			return fmt.Errorf("config validation: plugins.%s.invokes[%d].plugin is required", name, i)
		case entry.Invokes[i].Operation == "" && entry.Invokes[i].Surface == "":
			return fmt.Errorf("config validation: plugins.%s.invokes[%d].operation or .surface is required", name, i)
		case entry.Invokes[i].Operation != "" && entry.Invokes[i].Surface != "":
			return fmt.Errorf("config validation: plugins.%s.invokes[%d] may set only one of .operation or .surface", name, i)
		case entry.Invokes[i].Surface != "" && entry.Invokes[i].Surface != string(SpecSurfaceGraphQL):
			return fmt.Errorf("config validation: plugins.%s.invokes[%d].surface %q is not supported", name, i, entry.Invokes[i].Surface)
		case entry.Invokes[i].CredentialMode != "" && entry.Invokes[i].CredentialMode != providermanifestv1.ConnectionModeNone && entry.Invokes[i].CredentialMode != providermanifestv1.ConnectionModeUser:
			return fmt.Errorf("config validation: plugins.%s.invokes[%d].credentialMode %q is not supported", name, i, entry.Invokes[i].CredentialMode)
		}
		if err := normalizePluginInvocationRunAs("plugins."+name+".invokes["+strconv.Itoa(i)+"]", &entry.Invokes[i]); err != nil {
			return err
		}
		key := entry.Invokes[i].Plugin + "\x00op:" + entry.Invokes[i].Operation + "\x00surface:" + entry.Invokes[i].Surface
		if prev, ok := seenInvokes[key]; ok {
			return fmt.Errorf("config validation: plugins.%s.invokes[%d] duplicates invokes[%d]", name, i, prev)
		}
		seenInvokes[key] = i
	}
	if entry.UI != "" && entry.MountPath == "" {
		return fmt.Errorf("config validation: plugins.%s.ui.bundle requires plugins.%s.ui.path", name, name)
	}
	if err := validateProviderEntrySource("plugin", name, entry); err != nil {
		return err
	}
	if err := validatePluginRouteAuth(cfg, name, entry); err != nil {
		return err
	}
	if err := validatePluginIndexedDBConfig(cfg, name, entry); err != nil {
		return err
	}
	if err := validatePluginCacheBindings(cfg, name, entry); err != nil {
		return err
	}
	if err := validatePluginS3Bindings(cfg, name, entry); err != nil {
		return err
	}
	if err := validateAuthorizationPolicyReference(cfg, "plugin", name, entry.AuthorizationPolicy); err != nil {
		return err
	}
	if _, err := cfg.EffectiveHostedRuntime("plugins."+name, entry); err != nil {
		return err
	}
	return validatePluginIntegrationConnections(name, entry)
}

func validatePluginRouteAuth(cfg *Config, name string, entry *ProviderEntry) error {
	if entry == nil || entry.RouteAuth == nil {
		return nil
	}
	entry.RouteAuth.Provider = strings.TrimSpace(entry.RouteAuth.Provider)
	if entry.RouteAuth.Provider == "" {
		return fmt.Errorf("config validation: plugins.%s.auth.provider is required", name)
	}
	if entry.RouteAuth.Provider == "server" {
		_, authProvider, err := cfg.SelectedAuthenticationProvider()
		if err != nil {
			return err
		}
		if authProvider == nil {
			return fmt.Errorf("config validation: plugins.%s.auth.provider %q requires a configured platform authentication provider", name, entry.RouteAuth.Provider)
		}
		return nil
	}
	if _, ok := cfg.Providers.Authentication[entry.RouteAuth.Provider]; !ok {
		return fmt.Errorf("config validation: plugins.%s.auth.provider references unknown authentication provider %q", name, entry.RouteAuth.Provider)
	}
	return nil
}

func normalizePluginInvocationRunAs(path string, invoke *PluginInvocationDependency) error {
	if invoke == nil || invoke.RunAs == nil {
		return nil
	}
	if strings.TrimSpace(invoke.Surface) != "" {
		return fmt.Errorf("config validation: %s.runAs requires an exact operation", path)
	}
	runAs := invoke.RunAs
	count := 0
	if runAs.Subject != nil {
		count++
	}
	if count != 1 {
		return fmt.Errorf("config validation: %s.runAs must set exactly one delegation target", path)
	}
	subject := runAs.Subject
	subject.ID = strings.TrimSpace(subject.ID)
	subject.Kind = strings.TrimSpace(subject.Kind)
	subject.CredentialSubjectID = strings.TrimSpace(subject.CredentialSubjectID)
	subject.DisplayName = strings.TrimSpace(subject.DisplayName)
	subject.AuthSource = strings.TrimSpace(subject.AuthSource)
	if subject.ID == "" {
		return fmt.Errorf("config validation: %s.runAs.subject.id is required", path)
	}
	if subject.Kind == "" {
		if kind, _, ok := core.ParseSubjectID(subject.ID); ok {
			subject.Kind = kind
		}
	}
	if subject.Kind == "" {
		return fmt.Errorf("config validation: %s.runAs.subject.kind is required", path)
	}
	if subject.CredentialSubjectID == "" {
		subject.CredentialSubjectID = subject.ID
	}
	return nil
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

func validateCacheConfig(cfg *Config) error {
	for name, entry := range cfg.Providers.Cache {
		if entry == nil {
			return fmt.Errorf("config validation: providers.cache.%s is required", name)
		}
		if entry.Default {
			return fmt.Errorf("config validation: providers.cache.%s.default is not supported", name)
		}
		if err := validatePluginOnlyProviderFields("providers.cache."+name, entry); err != nil {
			return err
		}
		if err := validateProviderEntrySource("cache", name, entry); err != nil {
			return err
		}
	}
	return nil
}

func validateS3Config(cfg *Config) error {
	for name, entry := range cfg.Providers.S3 {
		if entry == nil {
			return fmt.Errorf("config validation: providers.s3.%s is required", name)
		}
		if err := validatePluginOnlyProviderFields("providers.s3."+name, entry); err != nil {
			return err
		}
		if err := validateProviderEntrySource("s3", name, entry); err != nil {
			return err
		}
	}
	return nil
}

func validateWorkflowConfig(cfg *Config) error {
	defaults := make([]string, 0, len(cfg.Providers.Workflow))
	for name, entry := range cfg.Providers.Workflow {
		if entry == nil {
			return fmt.Errorf("config validation: providers.workflow.%s is required", name)
		}
		if entry.Default {
			defaults = append(defaults, name)
		}
		if entry.IndexedDB != nil {
			entry.IndexedDB.Provider = strings.TrimSpace(entry.IndexedDB.Provider)
			entry.IndexedDB.DB = strings.TrimSpace(entry.IndexedDB.DB)
			for i, store := range entry.IndexedDB.ObjectStores {
				entry.IndexedDB.ObjectStores[i] = strings.TrimSpace(store)
			}
		}
		if err := validateWorkflowProviderFields(cfg, name, entry); err != nil {
			return err
		}
		if err := validateProviderEntrySource("workflow", name, entry); err != nil {
			return err
		}
	}
	if len(defaults) > 1 {
		sort.Strings(defaults)
		return fmt.Errorf("config validation: providers.workflow declares multiple defaults: %s", strings.Join(defaults, ", "))
	}
	return nil
}

func validateAgentConfig(cfg *Config) error {
	defaults := make([]string, 0, len(cfg.Providers.Agent))
	for name, entry := range cfg.Providers.Agent {
		if entry == nil {
			return fmt.Errorf("config validation: providers.agent.%s is required", name)
		}
		if entry.Default {
			defaults = append(defaults, name)
		}
		if entry.IndexedDB != nil {
			entry.IndexedDB.Provider = strings.TrimSpace(entry.IndexedDB.Provider)
			entry.IndexedDB.DB = strings.TrimSpace(entry.IndexedDB.DB)
			for i, store := range entry.IndexedDB.ObjectStores {
				entry.IndexedDB.ObjectStores[i] = strings.TrimSpace(store)
			}
		}
		if entry.Execution != nil {
			if err := normalizeExecutionConfig("providers.agent."+name, entry.Execution, true); err != nil {
				return err
			}
		}
		if err := validateAgentProviderLifecycleConfig("providers.agent."+name+".lifecycle", entry.Lifecycle); err != nil {
			return err
		}
		if err := validateAgentProviderFields(cfg, name, entry); err != nil {
			return err
		}
		if err := validateProviderEntrySource("agent", name, entry); err != nil {
			return err
		}
	}
	if len(defaults) > 1 {
		sort.Strings(defaults)
		return fmt.Errorf("config validation: providers.agent declares multiple defaults: %s", strings.Join(defaults, ", "))
	}
	return nil
}

func validateAgentProviderFields(cfg *Config, name string, entry *ProviderEntry) error {
	if entry == nil {
		return nil
	}
	subject := "providers.agent." + name
	if entry.RouteAuth != nil {
		return fmt.Errorf("config validation: %s.auth is only supported on plugins.*; use %s.source.auth for source auth", subject, subject)
	}
	if strings.TrimSpace(entry.MountPath) != "" {
		return fmt.Errorf("config validation: %s.mountPath is only supported on plugins.*", subject)
	}
	if strings.TrimSpace(entry.UI) != "" {
		return fmt.Errorf("config validation: %s.ui is only supported on plugins.*", subject)
	}
	if len(entry.Cache) > 0 {
		return fmt.Errorf("config validation: %s.cache is only supported on plugins.*", subject)
	}
	if len(entry.S3) > 0 {
		return fmt.Errorf("config validation: %s.s3 is only supported on plugins.*", subject)
	}
	if len(entry.Invokes) > 0 {
		return fmt.Errorf("config validation: %s.invokes is only supported on plugins.*", subject)
	}
	if entry.Surfaces != nil {
		return fmt.Errorf("config validation: %s.surfaces is only supported on plugins.*", subject)
	}
	if entry.AuthorizationPolicy != "" {
		return fmt.Errorf("config validation: %s.authorizationPolicy is only supported on plugins.* and ui.*", subject)
	}
	if _, err := cfg.EffectiveHostedRuntime(subject, entry); err != nil {
		return err
	}
	if entry.UsesHostedExecution() {
		if err := validateHostedAgentRuntimeLifecyclePolicy(subject, entry); err != nil {
			return err
		}
	}
	if entry.IndexedDB == nil {
		return nil
	}

	seenStores := make(map[string]struct{}, len(entry.IndexedDB.ObjectStores))
	for i, store := range entry.IndexedDB.ObjectStores {
		if store == "" {
			return fmt.Errorf("config validation: %s.indexeddb.objectStores[%d] is required", subject, i)
		}
		if _, exists := seenStores[store]; exists {
			return fmt.Errorf("config validation: %s.indexeddb.objectStores[%d] duplicates %q", subject, i, store)
		}
		seenStores[store] = struct{}{}
	}
	if _, err := cfg.EffectiveAgentIndexedDB(name, entry); err != nil {
		return err
	}
	return nil
}

func normalizeHostedRuntimeConfig(subject string, runtimeCfg *HostedRuntimeConfig) error {
	if runtimeCfg == nil {
		return nil
	}
	if runtimeCfg.Pool != nil {
		runtimeCfg.Pool.StartupTimeout = strings.TrimSpace(runtimeCfg.Pool.StartupTimeout)
		runtimeCfg.Pool.HealthCheckInterval = strings.TrimSpace(runtimeCfg.Pool.HealthCheckInterval)
		runtimeCfg.Pool.RestartPolicy = HostedRuntimeRestartPolicy(strings.TrimSpace(string(runtimeCfg.Pool.RestartPolicy)))
		runtimeCfg.Pool.DrainTimeout = strings.TrimSpace(runtimeCfg.Pool.DrainTimeout)
	}
	runtimeCfg.Provider = strings.TrimSpace(runtimeCfg.Provider)
	runtimeCfg.Template = strings.TrimSpace(runtimeCfg.Template)
	runtimeCfg.Image = strings.TrimSpace(runtimeCfg.Image)
	if runtimeCfg.ImagePullAuth != nil {
		if runtimeCfg.Image == "" {
			return fmt.Errorf("config validation: %s.runtime.imagePullAuth requires %s.runtime.image", subject, subject)
		}
		if strings.TrimSpace(runtimeCfg.ImagePullAuth.DockerConfigJSON) == "" {
			return fmt.Errorf("config validation: %s.runtime.imagePullAuth.dockerConfigJson is required when imagePullAuth is set", subject)
		}
		if err := validateHostedRuntimeDockerConfigJSON(runtimeCfg.ImagePullAuth.DockerConfigJSON); err != nil {
			return fmt.Errorf("config validation: %s.runtime.imagePullAuth.dockerConfigJson: %w", subject, err)
		}
	}
	trimmed := make(map[string]string, len(runtimeCfg.Metadata))
	for key, value := range runtimeCfg.Metadata {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" {
			return fmt.Errorf("config validation: %s.runtime.metadata keys must be non-empty", subject)
		}
		trimmed[trimmedKey] = strings.TrimSpace(value)
	}
	if runtimeCfg.Metadata != nil {
		runtimeCfg.Metadata = trimmed
	}
	if err := normalizeHostedRuntimeWorkspaceConfig(subject, runtimeCfg.Workspace); err != nil {
		return err
	}
	return nil
}

func normalizeHostedRuntimeWorkspaceConfig(subject string, workspace *HostedRuntimeWorkspaceConfig) error {
	if workspace == nil {
		return nil
	}
	workspace.PrepareTimeout = strings.TrimSpace(workspace.PrepareTimeout)
	if workspace.PrepareTimeout != "" {
		duration, err := ParseDuration(workspace.PrepareTimeout)
		if err != nil {
			return fmt.Errorf("config validation: %s.runtime.workspace.prepareTimeout: %w", subject, err)
		}
		if duration <= 0 {
			return fmt.Errorf("config validation: %s.runtime.workspace.prepareTimeout must be greater than 0", subject)
		}
	}
	if workspace.Git == nil {
		return nil
	}
	allowed := make([]string, 0, len(workspace.Git.AllowedRepositories))
	seen := map[string]struct{}{}
	for i, repo := range workspace.Git.AllowedRepositories {
		repo = strings.TrimSpace(repo)
		if repo == "" {
			return fmt.Errorf("config validation: %s.runtime.workspace.git.allowedRepositories[%d] is required", subject, i)
		}
		if !strings.Contains(repo, "*") {
			identity, err := coreagent.CanonicalGitRepositoryIdentity(repo)
			if err != nil {
				return fmt.Errorf("config validation: %s.runtime.workspace.git.allowedRepositories[%d]: %w", subject, i, err)
			}
			repo = identity
		}
		if _, ok := seen[repo]; ok {
			return fmt.Errorf("config validation: %s.runtime.workspace.git.allowedRepositories[%d] duplicates %q", subject, i, repo)
		}
		seen[repo] = struct{}{}
		allowed = append(allowed, repo)
	}
	workspace.Git.AllowedRepositories = allowed
	return nil
}

func validateHostedRuntimeDockerConfigJSON(value string) error {
	if _, isSecretRef, err := ParseSecretRefTransport(value); isSecretRef || err != nil {
		return err
	}
	var doc struct {
		Auths map[string]json.RawMessage `json:"auths"`
	}
	if err := json.Unmarshal([]byte(value), &doc); err != nil {
		return fmt.Errorf("must be valid Docker config JSON: %w", err)
	}
	if len(doc.Auths) == 0 {
		return fmt.Errorf(`must contain a non-empty "auths" object`)
	}
	return nil
}

func validateHostedAgentRuntimeLifecyclePolicy(subject string, entry *ProviderEntry) error {
	if entry == nil || !entry.UsesHostedExecution() {
		return nil
	}
	runtimeCfg, runtimePath := hostedRuntimeConfigAndPath(subject, entry)
	if runtimeCfg == nil {
		return fmt.Errorf("config validation: %s is required for hosted agent providers", runtimePath)
	}
	lifecycle := runtimeCfg.lifecyclePolicyConfig()
	lifecycleSubject := runtimePath + ".pool"
	if lifecycle.MinReadyInstances <= 0 {
		return fmt.Errorf("config validation: %s.minReadyInstances is required and must be greater than 0", lifecycleSubject)
	}
	if lifecycle.MaxReadyInstances <= 0 {
		return fmt.Errorf("config validation: %s.maxReadyInstances is required and must be greater than 0", lifecycleSubject)
	}
	if lifecycle.MaxReadyInstances < lifecycle.MinReadyInstances {
		return fmt.Errorf("config validation: %s.maxReadyInstances must be greater than or equal to minReadyInstances", lifecycleSubject)
	}
	if strings.TrimSpace(lifecycle.StartupTimeout) == "" {
		return fmt.Errorf("config validation: %s.startupTimeout is required", lifecycleSubject)
	}
	if strings.TrimSpace(lifecycle.HealthCheckInterval) == "" {
		return fmt.Errorf("config validation: %s.healthCheckInterval is required", lifecycleSubject)
	}
	if strings.TrimSpace(lifecycle.DrainTimeout) == "" {
		return fmt.Errorf("config validation: %s.drainTimeout is required", lifecycleSubject)
	}
	switch lifecycle.RestartPolicy {
	case HostedRuntimeRestartPolicyAlways, HostedRuntimeRestartPolicyNever:
	default:
		return fmt.Errorf("config validation: %s.restartPolicy must be one of %q or %q", lifecycleSubject, HostedRuntimeRestartPolicyAlways, HostedRuntimeRestartPolicyNever)
	}
	if lifecycle.RestartPolicy == HostedRuntimeRestartPolicyAlways && entry.IndexedDB == nil {
		return fmt.Errorf("config validation: %s.restartPolicy %q requires %s.indexeddb as the provider persistence hook; runtime replacement does not make backend-local state durable", lifecycleSubject, HostedRuntimeRestartPolicyAlways, subject)
	}
	if _, err := runtimeCfg.LifecyclePolicy(); err != nil {
		return fmt.Errorf("config validation: %s.%w", lifecycleSubject, err)
	}
	return nil
}

func hostedRuntimeConfigAndPath(subject string, entry *ProviderEntry) (*HostedRuntimeConfig, string) {
	if entry != nil && entry.Execution != nil {
		return entry.Execution.Runtime, subject + ".execution.runtime"
	}
	return nil, subject + ".execution.runtime"
}

func validateWorkflowProviderFields(cfg *Config, name string, entry *ProviderEntry) error {
	if entry == nil {
		return nil
	}
	subject := "providers.workflow." + name
	if entry.RouteAuth != nil {
		return fmt.Errorf("config validation: %s.auth is only supported on plugins.*; use %s.source.auth for source auth", subject, subject)
	}
	if strings.TrimSpace(entry.MountPath) != "" {
		return fmt.Errorf("config validation: %s.mountPath is only supported on plugins.*", subject)
	}
	if strings.TrimSpace(entry.UI) != "" {
		return fmt.Errorf("config validation: %s.ui is only supported on plugins.*", subject)
	}
	if len(entry.Cache) > 0 {
		return fmt.Errorf("config validation: %s.cache is only supported on plugins.*", subject)
	}
	if len(entry.S3) > 0 {
		return fmt.Errorf("config validation: %s.s3 is only supported on plugins.*", subject)
	}
	if len(entry.Invokes) > 0 {
		return fmt.Errorf("config validation: %s.invokes is only supported on plugins.*", subject)
	}
	if entry.Lifecycle != nil {
		return fmt.Errorf("config validation: %s.lifecycle is only supported on providers.agent.*", subject)
	}
	if entry.Execution != nil {
		return fmt.Errorf("config validation: %s.execution is only supported on plugins.* and providers.agent.*", subject)
	}
	if entry.Surfaces != nil {
		return fmt.Errorf("config validation: %s.surfaces is only supported on plugins.*", subject)
	}
	if entry.AuthorizationPolicy != "" {
		return fmt.Errorf("config validation: %s.authorizationPolicy is only supported on plugins.* and ui.*", subject)
	}
	if entry.IndexedDB == nil {
		return nil
	}

	seenStores := make(map[string]struct{}, len(entry.IndexedDB.ObjectStores))
	for i, store := range entry.IndexedDB.ObjectStores {
		if store == "" {
			return fmt.Errorf("config validation: %s.indexeddb.objectStores[%d] is required", subject, i)
		}
		if _, exists := seenStores[store]; exists {
			return fmt.Errorf("config validation: %s.indexeddb.objectStores[%d] duplicates %q", subject, i, store)
		}
		seenStores[store] = struct{}{}
	}
	if _, err := cfg.EffectiveWorkflowIndexedDB(name, entry); err != nil {
		return err
	}
	return nil
}

func validatePluginOnlyProviderFields(subject string, entry *ProviderEntry) error {
	if entry == nil {
		return nil
	}
	if entry.RouteAuth != nil {
		return fmt.Errorf("config validation: %s.auth is only supported on plugins.*; use %s.source.auth for source auth", subject, subject)
	}
	if strings.TrimSpace(entry.MountPath) != "" {
		return fmt.Errorf("config validation: %s.mountPath is only supported on plugins.*", subject)
	}
	if strings.TrimSpace(entry.UI) != "" {
		return fmt.Errorf("config validation: %s.ui is only supported on plugins.*", subject)
	}
	if entry.IndexedDB != nil {
		return fmt.Errorf("config validation: %s.indexeddb is only supported on plugins.*", subject)
	}
	if len(entry.Cache) > 0 {
		return fmt.Errorf("config validation: %s.cache is only supported on plugins.*", subject)
	}
	if len(entry.S3) > 0 {
		return fmt.Errorf("config validation: %s.s3 is only supported on plugins.*", subject)
	}
	if len(entry.Invokes) > 0 {
		return fmt.Errorf("config validation: %s.invokes is only supported on plugins.*", subject)
	}
	if entry.Lifecycle != nil {
		return fmt.Errorf("config validation: %s.lifecycle is only supported on providers.agent.*", subject)
	}
	if entry.Execution != nil {
		return fmt.Errorf("config validation: %s.execution is only supported on plugins.* and providers.agent.*", subject)
	}
	if entry.Surfaces != nil {
		return fmt.Errorf("config validation: %s.surfaces is only supported on plugins.*", subject)
	}
	if entry.AuthorizationPolicy != "" && !strings.HasPrefix(subject, "ui.") {
		return fmt.Errorf("config validation: %s.authorizationPolicy is only supported on plugins.* and ui.*", subject)
	}
	return nil
}

func validateAgentProviderLifecycleConfig(subject string, lifecycle *AgentProviderLifecycleConfig) error {
	if lifecycle == nil {
		return nil
	}
	seen := make(map[string]struct{}, len(lifecycle.SessionStart))
	for i := range lifecycle.SessionStart {
		hook := &lifecycle.SessionStart[i]
		hookSubject := fmt.Sprintf("%s.sessionStart[%d]", subject, i)
		hook.ID = strings.TrimSpace(hook.ID)
		if hook.ID == "" {
			return fmt.Errorf("config validation: %s.id is required", hookSubject)
		}
		if !validSessionStartHookID(hook.ID) {
			return fmt.Errorf("config validation: %s.id must contain only letters, numbers, underscores, or dashes", hookSubject)
		}
		if _, ok := seen[hook.ID]; ok {
			return fmt.Errorf("config validation: %s.id duplicates %q", hookSubject, hook.ID)
		}
		seen[hook.ID] = struct{}{}
		hook.Type = strings.TrimSpace(hook.Type)
		if hook.Type == "" {
			hook.Type = "command"
		}
		if hook.Type != "command" {
			return fmt.Errorf("config validation: %s.type %q is not supported; use %q", hookSubject, hook.Type, "command")
		}
		if len(hook.Command) == 0 || strings.TrimSpace(hook.Command[0]) == "" {
			return fmt.Errorf("config validation: %s.command is required", hookSubject)
		}
		hook.Command[0] = strings.TrimSpace(hook.Command[0])
		hook.CWD = strings.TrimSpace(hook.CWD)
		hook.Timeout = strings.TrimSpace(hook.Timeout)
		if hook.Timeout != "" {
			duration, err := ParseDuration(hook.Timeout)
			if err != nil {
				return fmt.Errorf("config validation: %s.timeout: %w", hookSubject, err)
			}
			if duration <= 0 {
				return fmt.Errorf("config validation: %s.timeout must be greater than 0", hookSubject)
			}
		}
		if hook.Env != nil {
			trimmed := make(map[string]string, len(hook.Env))
			for key, value := range hook.Env {
				key = strings.TrimSpace(key)
				if key == "" {
					return fmt.Errorf("config validation: %s.env keys must be non-empty", hookSubject)
				}
				trimmed[key] = value
			}
			hook.Env = trimmed
		}
	}
	return nil
}

func validSessionStartHookID(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func normalizeExecutionConfig(subject string, execution *ExecutionConfig, allowLifecycle bool) error {
	if execution == nil {
		return nil
	}
	execution.Mode = ExecutionMode(strings.ToLower(strings.TrimSpace(string(execution.Mode))))
	switch execution.Mode {
	case "", ExecutionModeLocal, ExecutionModeHosted:
	default:
		return fmt.Errorf("config validation: %s.execution.mode must be %q or %q, got %q", subject, ExecutionModeLocal, ExecutionModeHosted, execution.Mode)
	}
	if execution.Mode == ExecutionModeLocal && execution.Runtime != nil {
		return fmt.Errorf("config validation: %s.execution.runtime is only valid when execution.mode is %q", subject, ExecutionModeHosted)
	}
	if execution.Runtime == nil {
		return nil
	}
	if err := normalizeHostedRuntimeConfig(subject+".execution", execution.Runtime); err != nil {
		return err
	}
	if !allowLifecycle && execution.Runtime.LifecyclePolicyFieldsSet() {
		return fmt.Errorf("config validation: %s.execution.runtime lifecycle fields are only supported on providers.agent.*.execution.runtime", subject)
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
		for i, member := range policy.Members {
			subjectID := strings.TrimSpace(member.SubjectID)
			role := strings.TrimSpace(member.Role)
			switch {
			case role == "":
				return fmt.Errorf("config validation: authorization.policies.%s.members[%d].role is required", name, i)
			case subjectID == "":
				return fmt.Errorf("config validation: authorization.policies.%s.members[%d].subjectID is required", name, i)
			}
			if prev, exists := seenSubjectIDs[subjectID]; exists {
				return fmt.Errorf("config validation: authorization.policies.%s.members[%d].subjectID duplicates members[%d]", name, i, prev)
			}
			seenSubjectIDs[subjectID] = i
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

func validateAdminConfig(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	admin := cfg.Server.Admin
	policy := strings.TrimSpace(admin.AuthorizationPolicy)
	if adminUI := strings.TrimSpace(admin.UI); adminUI != "" {
		if cfg.Providers.UI == nil || cfg.Providers.UI[adminUI] == nil {
			return fmt.Errorf("config validation: server.admin.ui references unknown ui %q", adminUI)
		}
	}
	if policy == "" {
		if len(admin.AllowedRoles) > 0 {
			return fmt.Errorf("config validation: server.admin.allowedRoles requires server.admin.authorizationPolicy")
		}
	} else {
		_, authProvider, err := cfg.SelectedAuthenticationProvider()
		if err != nil {
			return err
		}
		if authProvider == nil {
			return fmt.Errorf("config validation: server.admin.authorizationPolicy requires providers.authentication to be configured")
		}
		if err := validateAuthorizationPolicyReference(cfg, "server.admin", "/admin", policy); err != nil {
			return err
		}
		if len(admin.AllowedRoles) == 0 {
			return fmt.Errorf("config validation: server.admin.allowedRoles must not be empty when server.admin.authorizationPolicy is set")
		}
	}

	_, hasManagement := cfg.Server.ManagementListener()
	if !hasManagement {
		if policy != "" && strings.TrimSpace(cfg.Server.ManagementBaseURL()) != "" {
			return fmt.Errorf("config validation: server.management.baseUrl requires server.management.host/server.management.port when server.admin.authorizationPolicy is set")
		}
		return nil
	}
	if policy == "" {
		return nil
	}

	publicURL, err := validateAbsoluteBaseURL("server.baseUrl", cfg.Server.BaseURL)
	if err != nil {
		return err
	}
	managementURL, err := validateAbsoluteBaseURL("server.management.baseUrl", cfg.Server.ManagementBaseURL())
	if err != nil {
		return err
	}

	if publicURL == nil {
		return fmt.Errorf("config validation: server.admin.authorizationPolicy on a split management listener requires server.baseUrl")
	}
	if managementURL == nil {
		return fmt.Errorf("config validation: server.admin.authorizationPolicy on a split management listener requires server.management.baseUrl")
	}
	if publicURL.Hostname() != managementURL.Hostname() {
		return fmt.Errorf("config validation: server.baseUrl and server.management.baseUrl must use the same hostname when server.admin.authorizationPolicy is enabled on a split management listener")
	}
	if strings.EqualFold(publicURL.Scheme, "https") && !strings.EqualFold(managementURL.Scheme, "https") {
		return fmt.Errorf("config validation: server.management.baseUrl must use https when server.baseUrl uses https and server.admin.authorizationPolicy is enabled on a split management listener")
	}
	return nil
}

func validateAbsoluteBaseURL(label, raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" {
		return nil, fmt.Errorf("config validation: %s must be an absolute URL", label)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("config validation: %s may not include query or fragment", label)
	}
	return parsed, nil
}

func validatePluginIndexedDBConfig(cfg *Config, name string, entry *ProviderEntry) error {
	if entry == nil {
		return nil
	}
	if entry.IndexedDB == nil {
		return nil
	}
	indexedDB := entry.IndexedDB
	seenStores := make(map[string]struct{}, len(indexedDB.ObjectStores))
	for i, store := range indexedDB.ObjectStores {
		if store == "" {
			return fmt.Errorf("config validation: plugins.%s.indexeddb.objectStores[%d] is required", name, i)
		}
		if _, exists := seenStores[store]; exists {
			return fmt.Errorf("config validation: plugins.%s.indexeddb.objectStores[%d] duplicates %q", name, i, store)
		}
		seenStores[store] = struct{}{}
	}
	if _, err := cfg.EffectivePluginIndexedDB(name, entry); err != nil {
		return err
	}
	return nil
}

func validatePluginS3Bindings(cfg *Config, name string, entry *ProviderEntry) error {
	if entry == nil {
		return nil
	}
	seen := make(map[string]struct{}, len(entry.S3))
	envNames := make(map[string]string, len(entry.S3))
	for i, binding := range entry.S3 {
		binding = strings.TrimSpace(binding)
		if binding == "" {
			return fmt.Errorf("config validation: plugin %q s3[%d] is required", name, i)
		}
		if _, exists := seen[binding]; exists {
			return fmt.Errorf("config validation: plugin %q s3[%d] duplicates %q", name, i, binding)
		}
		boundEntry, ok := cfg.Providers.S3[binding]
		if !ok || boundEntry == nil {
			return fmt.Errorf("config validation: plugin %q s3[%d] references unknown s3 %q", name, i, binding)
		}
		envName := s3.SocketEnv(binding)
		if otherBinding, exists := envNames[envName]; exists {
			return fmt.Errorf("config validation: plugin %q s3[%d] %q conflicts with %q after S3 env normalization (%s)", name, i, binding, otherBinding, envName)
		}
		seen[binding] = struct{}{}
		envNames[envName] = binding
		entry.S3[i] = binding
	}
	return nil
}

var workflowScheduleCronParser = cronv3.NewParser(
	cronv3.Minute | cronv3.Hour | cronv3.Dom | cronv3.Month | cronv3.Dow,
)

func validateWorkflowScheduleCron(scheduleKey, spec string) error {
	if _, err := workflowScheduleCronParser.Parse(spec); err != nil {
		return fmt.Errorf("config validation: workflows.schedules.%s.cron %q is invalid: %w", scheduleKey, spec, err)
	}
	return nil
}

func validateWorkflowsConfig(cfg *Config) error {
	if len(cfg.Workflows.Schedules) > 0 {
		normalized := make(map[string]WorkflowScheduleConfig, len(cfg.Workflows.Schedules))
		for key := range cfg.Workflows.Schedules {
			schedule := cfg.Workflows.Schedules[key]
			key = strings.TrimSpace(key)
			if key == "" {
				return fmt.Errorf("config validation: workflows.schedules keys must not be empty")
			}
			if _, exists := normalized[key]; exists {
				return fmt.Errorf("config validation: workflows.schedules duplicates %q", key)
			}
			if err := validateWorkflowScheduleTarget(cfg, key, &schedule); err != nil {
				return err
			}
			permissions, err := normalizeWorkflowExecutionPermissions(cfg, "workflows.schedules."+key+".permissions", schedule.Permissions)
			if err != nil {
				return err
			}
			schedule.Permissions = permissions
			schedule.Provider = strings.TrimSpace(schedule.Provider)
			providerName, _, err := cfg.EffectiveWorkflowProvider(schedule.Provider)
			if err != nil {
				return fmt.Errorf("config validation: workflows.schedules.%s.provider: %w", key, err)
			}
			if providerName == "" {
				return fmt.Errorf("config validation: workflows.schedules.%s.provider is required", key)
			}
			schedule.Cron = strings.TrimSpace(schedule.Cron)
			if schedule.Cron == "" {
				return fmt.Errorf("config validation: workflows.schedules.%s.cron is required", key)
			}
			if err := validateWorkflowScheduleCron(key, schedule.Cron); err != nil {
				return err
			}
			schedule.Timezone = strings.TrimSpace(schedule.Timezone)
			if schedule.Timezone == "" {
				schedule.Timezone = "UTC"
			}
			if _, err := time.LoadLocation(schedule.Timezone); err != nil {
				return fmt.Errorf("config validation: workflows.schedules.%s.timezone %q is invalid: %w", key, schedule.Timezone, err)
			}
			normalized[key] = schedule
		}
		cfg.Workflows.Schedules = normalized
	}

	if len(cfg.Workflows.EventTriggers) > 0 {
		normalized := make(map[string]WorkflowEventTriggerConfig, len(cfg.Workflows.EventTriggers))
		for key := range cfg.Workflows.EventTriggers {
			trigger := cfg.Workflows.EventTriggers[key]
			key = strings.TrimSpace(key)
			if key == "" {
				return fmt.Errorf("config validation: workflows.eventTriggers keys must not be empty")
			}
			if _, exists := normalized[key]; exists {
				return fmt.Errorf("config validation: workflows.eventTriggers duplicates %q", key)
			}
			if err := validateWorkflowEventTriggerTarget(cfg, key, &trigger); err != nil {
				return err
			}
			permissions, err := normalizeWorkflowExecutionPermissions(cfg, "workflows.eventTriggers."+key+".permissions", trigger.Permissions)
			if err != nil {
				return err
			}
			trigger.Permissions = permissions
			trigger.Provider = strings.TrimSpace(trigger.Provider)
			providerName, _, err := cfg.EffectiveWorkflowProvider(trigger.Provider)
			if err != nil {
				return fmt.Errorf("config validation: workflows.eventTriggers.%s.provider: %w", key, err)
			}
			if providerName == "" {
				return fmt.Errorf("config validation: workflows.eventTriggers.%s.provider is required", key)
			}
			trigger.Match.Type = strings.TrimSpace(trigger.Match.Type)
			if trigger.Match.Type == "" {
				return fmt.Errorf("config validation: workflows.eventTriggers.%s.match.type is required", key)
			}
			trigger.Match.Source = strings.TrimSpace(trigger.Match.Source)
			trigger.Match.Subject = strings.TrimSpace(trigger.Match.Subject)
			normalized[key] = trigger
		}
		cfg.Workflows.EventTriggers = normalized
	}
	return nil
}

func validateWorkflowScheduleTarget(cfg *Config, key string, schedule *WorkflowScheduleConfig) error {
	if schedule == nil {
		return fmt.Errorf("config validation: workflows.schedules.%s is required", key)
	}
	targetPath := "workflows.schedules." + key + ".target"
	if err := normalizeWorkflowTarget(targetPath, schedule.Target); err != nil {
		return err
	}
	if schedule.Target.Plugin != nil {
		if _, ok := cfg.Plugins[schedule.Target.Plugin.Name]; !ok {
			return fmt.Errorf("config validation: %s.plugin.name references unknown plugin %q", targetPath, schedule.Target.Plugin.Name)
		}
		return nil
	}
	return validateWorkflowAgentConfig(cfg, targetPath+".agent", schedule.Target.Agent)
}

func validateWorkflowEventTriggerTarget(cfg *Config, key string, trigger *WorkflowEventTriggerConfig) error {
	if trigger == nil {
		return fmt.Errorf("config validation: workflows.eventTriggers.%s is required", key)
	}
	targetPath := "workflows.eventTriggers." + key + ".target"
	if err := normalizeWorkflowTarget(targetPath, trigger.Target); err != nil {
		return err
	}
	if trigger.Target.Plugin != nil {
		if _, ok := cfg.Plugins[trigger.Target.Plugin.Name]; !ok {
			return fmt.Errorf("config validation: %s.plugin.name references unknown plugin %q", targetPath, trigger.Target.Plugin.Name)
		}
		return nil
	}
	return validateWorkflowAgentConfig(cfg, targetPath+".agent", trigger.Target.Agent)
}

func normalizeWorkflowExecutionPermissions(cfg *Config, path string, values []core.AccessPermission) ([]core.AccessPermission, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]core.AccessPermission, 0, len(values))
	pluginIndexes := make(map[string]int, len(values))
	seenOperations := make(map[string]map[string]struct{}, len(values))
	for i, value := range values {
		plugin := strings.TrimSpace(value.Plugin)
		if plugin == "" {
			return nil, fmt.Errorf("config validation: %s[%d].plugin is required", path, i)
		}
		if _, ok := cfg.Plugins[plugin]; !ok {
			return nil, fmt.Errorf("config validation: %s[%d].plugin references unknown plugin %q", path, i, plugin)
		}
		if len(value.Actions) > 0 {
			return nil, fmt.Errorf("config validation: %s[%d].actions is not supported", path, i)
		}
		operations, err := normalizeWorkflowExecutionPermissionNames(fmt.Sprintf("%s[%d].operations", path, i), value.Operations)
		if err != nil {
			return nil, err
		}
		if len(operations) == 0 {
			return nil, fmt.Errorf("config validation: %s[%d].operations is required", path, i)
		}
		idx, ok := pluginIndexes[plugin]
		if !ok {
			idx = len(out)
			pluginIndexes[plugin] = idx
			seenOperations[plugin] = map[string]struct{}{}
			out = append(out, core.AccessPermission{Plugin: plugin})
		}
		for _, operation := range operations {
			if _, exists := seenOperations[plugin][operation]; exists {
				continue
			}
			seenOperations[plugin][operation] = struct{}{}
			out[idx].Operations = append(out[idx].Operations, operation)
		}
	}
	return out, nil
}

func normalizeWorkflowExecutionPermissionNames(path string, values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(values))
	for i, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("config validation: %s[%d] is required", path, i)
		}
		out = append(out, value)
	}
	return out, nil
}

func normalizeWorkflowTarget(path string, target *WorkflowTargetConfig) error {
	if target == nil {
		return fmt.Errorf("config validation: %s must set exactly one of plugin or agent", path)
	}
	hasPlugin := target.Plugin != nil
	hasAgent := target.Agent != nil
	if hasPlugin == hasAgent {
		return fmt.Errorf("config validation: %s must set exactly one of plugin or agent", path)
	}
	if hasPlugin {
		plugin := *target.Plugin
		plugin.Name = strings.TrimSpace(plugin.Name)
		plugin.Operation = strings.TrimSpace(plugin.Operation)
		plugin.Connection = strings.TrimSpace(plugin.Connection)
		plugin.Instance = strings.TrimSpace(plugin.Instance)
		if plugin.Name == "" {
			return fmt.Errorf("config validation: %s.plugin.name is required", path)
		}
		if plugin.Operation == "" {
			return fmt.Errorf("config validation: %s.plugin.operation is required", path)
		}
		target.Plugin = &plugin
		return nil
	}
	return nil
}

func validateWorkflowAgentConfig(cfg *Config, path string, agent *WorkflowAgentConfig) error {
	if agent == nil {
		return fmt.Errorf("config validation: %s is required", path)
	}
	agent.Provider = strings.TrimSpace(agent.Provider)
	providerName, _, err := cfg.EffectiveAgentProvider(agent.Provider)
	if err != nil {
		return fmt.Errorf("config validation: %s.provider: %w", path, err)
	}
	if providerName == "" {
		return fmt.Errorf("config validation: %s.provider is required", path)
	}
	agent.Provider = providerName
	agent.Model = strings.TrimSpace(agent.Model)
	agent.Prompt = strings.TrimSpace(agent.Prompt)
	for i := range agent.Messages {
		agent.Messages[i].Role = strings.TrimSpace(agent.Messages[i].Role)
		agent.Messages[i].Text = strings.TrimSpace(agent.Messages[i].Text)
	}
	if agent.Prompt == "" && len(agent.Messages) == 0 {
		return fmt.Errorf("config validation: %s.prompt or messages is required", path)
	}
	hasSystemTool := false
	for i := range agent.Tools {
		if strings.TrimSpace(agent.Tools[i].System) != "" {
			hasSystemTool = true
			break
		}
	}
	for i := range agent.Tools {
		tool := &agent.Tools[i]
		tool.System = strings.TrimSpace(tool.System)
		tool.Plugin = strings.TrimSpace(tool.Plugin)
		if tool.System == "" && tool.Plugin == "" {
			return fmt.Errorf("config validation: %s.tools[%d].plugin or system is required", path, i)
		}
		if tool.System != "" && tool.Plugin != "" {
			return fmt.Errorf("config validation: %s.tools[%d] must set exactly one of plugin or system", path, i)
		}
		tool.Operation = strings.TrimSpace(tool.Operation)
		tool.Connection = strings.TrimSpace(tool.Connection)
		tool.Instance = strings.TrimSpace(tool.Instance)
		tool.Title = strings.TrimSpace(tool.Title)
		tool.Description = strings.TrimSpace(tool.Description)
		if tool.System != "" {
			if tool.System != "workflow" {
				return fmt.Errorf("config validation: %s.tools[%d].system references unknown system %q", path, i, tool.System)
			}
			if tool.Operation == "" {
				return fmt.Errorf("config validation: %s.tools[%d].operation is required for system tool refs", path, i)
			}
			if tool.Connection != "" || tool.Instance != "" {
				return fmt.Errorf("config validation: %s.tools[%d] system refs cannot include connection or instance", path, i)
			}
			continue
		}
		if _, ok := cfg.Plugins[tool.Plugin]; !ok {
			return fmt.Errorf("config validation: %s.tools[%d].plugin references unknown plugin %q", path, i, tool.Plugin)
		}
		if hasSystemTool && tool.Operation == "" {
			return fmt.Errorf("config validation: %s.tools[%d].operation is required when workflow system tools are delegated", path, i)
		}
	}
	agent.Timeout = strings.TrimSpace(agent.Timeout)
	if agent.Timeout != "" {
		if _, err := time.ParseDuration(agent.Timeout); err != nil {
			return fmt.Errorf("config validation: %s.timeout %q is invalid: %w", path, agent.Timeout, err)
		}
	}
	return nil
}

func validatePluginCacheBindings(cfg *Config, name string, entry *ProviderEntry) error {
	if entry == nil {
		return nil
	}
	seen := make(map[string]struct{}, len(entry.Cache))
	envNames := make(map[string]string, len(entry.Cache))
	for i, binding := range entry.Cache {
		binding = strings.TrimSpace(binding)
		if binding == "" {
			return fmt.Errorf("config validation: plugin %q cache[%d] is required", name, i)
		}
		if _, exists := seen[binding]; exists {
			return fmt.Errorf("config validation: plugin %q cache[%d] duplicates %q", name, i, binding)
		}
		boundEntry, ok := cfg.Providers.Cache[binding]
		if !ok || boundEntry == nil {
			return fmt.Errorf("config validation: plugin %q cache[%d] references unknown cache %q", name, i, binding)
		}
		envName := cacheSocketEnv(binding)
		if otherBinding, exists := envNames[envName]; exists {
			return fmt.Errorf("config validation: plugin %q cache[%d] %q conflicts with %q after cache env normalization (%s)", name, i, binding, otherBinding, envName)
		}
		entry.Cache[i] = binding
		seen[binding] = struct{}{}
		envNames[envName] = binding
	}
	return nil
}

func cacheSocketEnv(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "GESTALT_CACHE_SOCKET"
	}
	var b strings.Builder
	b.WriteString("GESTALT_CACHE_SOCKET")
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
func normalizeMountedUIPaths(cfg *Config, pluginOwnedUIBindings map[string]struct{}) error {
	for name, entry := range cfg.Providers.UI {
		if entry == nil {
			continue
		}
		if strings.TrimSpace(entry.Path) == "" {
			if _, ok := pluginOwnedUIBindings[name]; ok {
				continue
			}
			return fmt.Errorf("config validation: ui.%s.path: path is required", name)
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

func validateMountedUICollisions(cfg *Config, pluginOwnedUIBindings map[string]struct{}) error {
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
	type mountedPathSubject struct {
		label           string
		path            string
		allowNestedPath bool
	}
	subjects := make([]mountedPathSubject, 0, len(cfg.Providers.UI)+len(cfg.Plugins))
	names := slices.Sorted(maps.Keys(cfg.Providers.UI))
	for _, name := range names {
		entry := cfg.Providers.UI[name]
		if entry == nil || strings.TrimSpace(entry.Path) == "" {
			continue
		}
		subjects = append(subjects, mountedPathSubject{
			label:           "ui." + name + ".path",
			path:            entry.Path,
			allowNestedPath: true,
		})
	}
	pluginNames := slices.Sorted(maps.Keys(cfg.Plugins))
	for _, name := range pluginNames {
		entry := cfg.Plugins[name]
		if entry == nil || strings.TrimSpace(entry.UI) != "" || strings.TrimSpace(entry.MountPath) == "" {
			continue
		}
		if _, ok := pluginOwnedUIBindings[name]; ok {
			if ui := cfg.Providers.UI[name]; ui != nil && mountedUIPathsMatch(ui.Path, entry.MountPath) {
				continue
			}
		}
		subjects = append(subjects, mountedPathSubject{
			label: "plugins." + name + ".ui.path",
			path:  entry.MountPath,
		})
	}
	for i, subject := range subjects {
		if subject.path == "/" {
			for _, other := range subjects[i+1:] {
				if other.path == "/" {
					return fmt.Errorf("config validation: %s %q conflicts with %s %q", subject.label, subject.path, other.label, other.path)
				}
			}
			continue
		}
		for _, reservedPath := range reserved {
			if mountedUIPathsConflict(subject.path, reservedPath) {
				return fmt.Errorf("config validation: %s %q conflicts with reserved path %q", subject.label, subject.path, reservedPath)
			}
		}
		for _, other := range subjects[i+1:] {
			if subject.path == other.path {
				return fmt.Errorf("config validation: %s %q conflicts with %s %q", subject.label, subject.path, other.label, other.path)
			}
			if !subject.allowNestedPath || !other.allowNestedPath {
				if mountedUIPathsConflict(subject.path, other.path) {
					return fmt.Errorf("config validation: %s %q conflicts with %s %q", subject.label, subject.path, other.label, other.path)
				}
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

func mountedUIPathsMatch(uiPath, mountPath string) bool {
	if strings.TrimSpace(uiPath) == "" {
		return false
	}
	normalizedMountPath, err := normalizeMountedUIPath(mountPath)
	if err != nil {
		return false
	}
	return uiPath == normalizedMountPath
}

func pluginOwnedUIBindings(cfg *Config) map[string]struct{} {
	refs := make(map[string]struct{}, len(cfg.Plugins))
	for name, entry := range cfg.Plugins {
		if entry == nil || strings.TrimSpace(entry.UI) != "" || strings.TrimSpace(entry.MountPath) == "" {
			continue
		}
		refs[name] = struct{}{}
	}
	return refs
}

func validateExecutableConnectionAuthSupport(name string, plan StaticConnectionPlan) error {
	_, supportsMCPOAuth := plan.ResolvedSurface(SpecSurfaceMCP)
	if conn := plan.PluginConnection(); conn.Auth.Type == providermanifestv1.AuthTypeMCPOAuth && !supportsMCPOAuth {
		return fmt.Errorf("config validation: integration %q plugin auth type %q requires an MCP surface", name, providermanifestv1.AuthTypeMCPOAuth)
	}
	for _, connName := range plan.NamedConnectionNames() {
		conn, _ := plan.NamedConnectionDef(connName)
		if conn.Auth.Type != providermanifestv1.AuthTypeMCPOAuth {
			continue
		}
		if !supportsMCPOAuth {
			return fmt.Errorf("config validation: integration %q connection %q auth type %q requires an MCP surface", name, connName, providermanifestv1.AuthTypeMCPOAuth)
		}
	}
	return nil
}

func validatePluginIntegrationConnections(name string, entry *ProviderEntry) error {
	if entry == nil {
		return nil
	}
	effectiveProvider := entry.ManifestSpec()
	plan, err := BuildStaticConnectionPlan(entry, effectiveProvider)
	if err != nil {
		return fmt.Errorf("config validation: integration %q %w", name, err)
	}
	if err := validateExecutableConnectionAuthSupport(name, plan); err != nil {
		return err
	}
	if err := validateConnectionAuthMappings(name, plan.PluginConnection().Auth, "plugin"); err != nil {
		return err
	}
	if err := validateCredentialRefresh(fmt.Sprintf("integration %q plugin connection", name), plan.PluginConnection()); err != nil {
		return err
	}
	for _, connName := range plan.NamedConnectionNames() {
		conn, _ := plan.NamedConnectionDef(connName)
		if err := validateConnectionAuthMappings(name, conn.Auth, fmt.Sprintf("connection %q", connName)); err != nil {
			return err
		}
		if err := validateCredentialRefresh(fmt.Sprintf("integration %q connection %q", name, connName), conn); err != nil {
			return err
		}
	}
	return nil
}

func validateCredentialRefresh(scope string, conn ConnectionDef) error {
	refresh := conn.CredentialRefresh
	if refresh == nil {
		return nil
	}
	if _, err := ParseDuration(strings.TrimSpace(refresh.RefreshInterval)); err != nil {
		return fmt.Errorf("config validation: %s credentialRefresh.refreshInterval: %w", scope, err)
	}
	if _, err := ParseDuration(strings.TrimSpace(refresh.RefreshBeforeExpiry)); err != nil {
		return fmt.Errorf("config validation: %s credentialRefresh.refreshBeforeExpiry: %w", scope, err)
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
