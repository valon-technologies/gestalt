package config

import (
	"encoding/json"
	"fmt"
	"maps"
	"net"
	"net/mail"
	"net/url"
	"os"
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
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/providerregistry"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

// ValidateStructure checks config shape: integration references, app
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
	if err := normalizeAppStaticMounts(cfg); err != nil {
		return err
	}
	normalizeSCIMConfig(cfg)
	return canonicalizeRemotes(cfg)
}

func normalizeSCIMConfig(cfg *Config) {
	if cfg == nil {
		return
	}
	for clientID, client := range cfg.Server.SCIM.Clients {
		for i := range client.Credentials {
			client.Credentials[i].ID = strings.TrimSpace(client.Credentials[i].ID)
		}
		for i := range client.AuthoritativeUserDomains {
			client.AuthoritativeUserDomains[i] = strings.ToLower(strings.TrimSpace(client.AuthoritativeUserDomains[i]))
		}
		for i := range client.ActiveUserRelationships {
			client.ActiveUserRelationships[i].Relation = strings.TrimSpace(client.ActiveUserRelationships[i].Relation)
			client.ActiveUserRelationships[i].Resource.Type = strings.TrimSpace(client.ActiveUserRelationships[i].Resource.Type)
			client.ActiveUserRelationships[i].Resource.ID = strings.TrimSpace(client.ActiveUserRelationships[i].Resource.ID)
		}
		cfg.Server.SCIM.Clients[clientID] = client
	}
}

// ValidateCanonicalStructure checks config shape assuming canonicalization has
// already run on the config.
func ValidateCanonicalStructure(cfg *Config) error {
	if err := validateAuthorizationModelConfig(cfg); err != nil {
		return err
	}
	if err := validateSCIMConfig(cfg); err != nil {
		return err
	}
	if err := validateServerListeners(cfg.Server); err != nil {
		return err
	}
	if err := validateAdminConfig(cfg); err != nil {
		return err
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
	if err := validateProviderSnapshotRepositories(cfg); err != nil {
		return err
	}
	if err := validateAppRegistries(cfg); err != nil {
		return err
	}
	if err := validateServerAppRegistry(cfg); err != nil {
		return err
	}

	for _, collection := range []struct {
		kind    HostProviderKind
		entries map[string]*ProviderEntry
	}{
		{HostProviderKindIdentity, cfg.Providers.Identity},
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
	if err := validateStaticMountCollisions(cfg); err != nil {
		return err
	}

	// Validate indexeddbs
	if err := validateIndexedDBConfig(cfg); err != nil {
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

	// Validate apps
	for name, entry := range cfg.Apps {
		if err := validateApp(cfg, name, entry); err != nil {
			return err
		}
	}
	return validateWorkflowsConfig(cfg)
}

func validateSCIMConfig(cfg *Config) error {
	if cfg == nil || len(cfg.Server.SCIM.Clients) == 0 {
		return nil
	}
	resourceTypes := make(map[string]AuthorizationResourceTypeDef)
	for _, model := range cfg.Authorization.Models {
		for name, resourceType := range model.ResourceTypes {
			resourceTypes[name] = resourceType
		}
	}
	groupType, hasGroup := resourceTypes["group"]
	if !hasGroup {
		return fmt.Errorf("config validation: SCIM requires authorization resource type %q", "group")
	}
	if !groupType.Dynamic.AllowAdditionalRelationships {
		return fmt.Errorf("config validation: authorization resource type %q must set dynamic.allowAdditionalRelationships: true for SCIM groups", "group")
	}
	memberRelation, hasMember := groupType.Relations["member"]
	if !hasMember {
		return fmt.Errorf("config validation: SCIM requires group.member authorization relation")
	}
	hasUserSubject, hasGroupSubjectSet := false, false
	for _, subjectType := range memberRelation.SubjectTypes {
		if strings.TrimSpace(subjectType) == "subject" {
			hasUserSubject = true
		}
	}
	for _, target := range memberRelation.AllowedTargets {
		if target.SubjectType == "subject" {
			hasUserSubject = true
		}
		if target.SubjectSet != nil && target.SubjectSet.ResourceType == "group" && target.SubjectSet.Relation == "member" {
			hasGroupSubjectSet = true
		}
	}
	if !hasUserSubject || !hasGroupSubjectSet {
		return fmt.Errorf("config validation: SCIM requires group.member to permit both subject and group#member targets")
	}
	domainOwners := make(map[string]string)
	credentialIDs := make(map[string]string)
	tokens := make(map[string]string)
	needsAuthorization := false
	projectionOwners := make(map[string]string)
	for clientID, client := range cfg.Server.SCIM.Clients {
		path := "server.scim.clients." + clientID
		if strings.TrimSpace(clientID) == "" || strings.TrimSpace(clientID) != clientID {
			return fmt.Errorf("config validation: server.scim.clients keys must be non-empty and trimmed")
		}
		if len(client.Credentials) == 0 || len(client.Credentials) > 2 {
			return fmt.Errorf("config validation: %s.credentials must contain one or two credentials", path)
		}
		if len(client.ActiveUserRelationships) == 0 {
			return fmt.Errorf("config validation: %s.activeUserRelationships must contain at least one relationship", path)
		}
		for i, credential := range client.Credentials {
			credentialPath := fmt.Sprintf("%s.credentials[%d]", path, i)
			id := strings.TrimSpace(credential.ID)
			if id == "" {
				return fmt.Errorf("config validation: %s.id is required", credentialPath)
			}
			if previous, ok := credentialIDs[clientID+"\x00"+id]; ok {
				return fmt.Errorf("config validation: %s.id duplicates %s", credentialPath, previous)
			}
			credentialIDs[clientID+"\x00"+id] = credentialPath
			token := strings.TrimSpace(credential.BearerToken)
			if token == "" {
				return fmt.Errorf("config validation: %s.bearerToken is required", credentialPath)
			}
			if previous, ok := tokens[token]; ok {
				return fmt.Errorf("config validation: %s.bearerToken duplicates %s", credentialPath, previous)
			}
			tokens[token] = credentialPath
		}
		for i, rawDomain := range client.AuthoritativeUserDomains {
			domain := strings.ToLower(strings.TrimSpace(rawDomain))
			if domain == "" || domain != rawDomain || strings.Contains(domain, "@") {
				return fmt.Errorf("config validation: %s.authoritativeUserDomains[%d] must be a normalized domain", path, i)
			}
			if previous, ok := domainOwners[domain]; ok {
				return fmt.Errorf("config validation: %s.authoritativeUserDomains[%d] overlaps client %q", path, i, previous)
			}
			domainOwners[domain] = clientID
			needsAuthorization = true
		}
		for i, projection := range client.ActiveUserRelationships {
			projectionPath := fmt.Sprintf("%s.activeUserRelationships[%d]", path, i)
			resourceTypeName := strings.TrimSpace(projection.Resource.Type)
			resourceType, ok := resourceTypes[resourceTypeName]
			if !ok {
				return fmt.Errorf("config validation: %s.resource.type references unknown resource type %q", projectionPath, resourceTypeName)
			}
			if strings.TrimSpace(projection.Resource.ID) == "" {
				return fmt.Errorf("config validation: %s.resource.id is required", projectionPath)
			}
			if _, ok := resourceType.Relations[strings.TrimSpace(projection.Relation)]; !ok {
				return fmt.Errorf("config validation: %s.relation references unknown relation %q for resource type %q", projectionPath, projection.Relation, resourceTypeName)
			}
			projectionKey := resourceTypeName + "\x00" + strings.TrimSpace(projection.Resource.ID) + "\x00" + strings.TrimSpace(projection.Relation)
			if previous, ok := projectionOwners[projectionKey]; ok {
				return fmt.Errorf("config validation: %s duplicates active projection configured by client %q", projectionPath, previous)
			}
			projectionOwners[projectionKey] = clientID
			hasSubjectTarget := false
			for _, subjectType := range resourceType.Relations[strings.TrimSpace(projection.Relation)].SubjectTypes {
				if strings.TrimSpace(subjectType) == "subject" {
					hasSubjectTarget = true
				}
			}
			for _, target := range resourceType.Relations[strings.TrimSpace(projection.Relation)].AllowedTargets {
				if target.SubjectType == "subject" {
					hasSubjectTarget = true
					break
				}
			}
			if !hasSubjectTarget {
				return fmt.Errorf("config validation: %s relation must permit direct subject targets for SCIM users", projectionPath)
			}
			if !resourceType.Dynamic.AllowAdditionalRelationships {
				return fmt.Errorf("config validation: authorization resource type %q must set dynamic.allowAdditionalRelationships: true for SCIM projections", resourceTypeName)
			}
			needsAuthorization = true
		}
	}
	if needsAuthorization || len(cfg.Server.SCIM.Clients) > 0 {
		_, provider, err := cfg.SelectedAuthorizationProvider()
		if err != nil {
			return err
		}
		if provider == nil {
			return fmt.Errorf("config validation: SCIM authoritative domains and projections require providers.authorization to be configured")
		}
	}
	return nil
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

func validateProviderSnapshotRepositories(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	for name, repo := range cfg.ProviderSnapshotRepositories {
		if err := providerregistry.ValidateRepositoryName(name); err != nil {
			return fmt.Errorf("config validation: providerSnapshotRepositories.%s: %w", name, err)
		}
		rawURL := strings.TrimSpace(repo.URL)
		if rawURL == "" {
			return fmt.Errorf("config validation: providerSnapshotRepositories.%s.url is required", name)
		}
		parsed, err := url.ParseRequestURI(rawURL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return fmt.Errorf("config validation: providerSnapshotRepositories.%s.url must be an absolute http(s) URL", name)
		}
		if ref := strings.TrimSpace(repo.GestaltRef); ref != "" && !isFullGitSHA(ref) {
			return fmt.Errorf("config validation: providerSnapshotRepositories.%s.gestaltRef must be a 40-character commit SHA", name)
		}
		switch repo.Auth {
		case "":
		case ProviderSnapshotRepositoryAuthGCPADC:
			if parsed.Scheme != "https" || !strings.EqualFold(parsed.Hostname(), "storage.googleapis.com") || parsed.Port() != "" || parsed.User != nil {
				return fmt.Errorf("config validation: providerSnapshotRepositories.%s.auth %q requires an https://storage.googleapis.com URL", name, repo.Auth)
			}
		default:
			return fmt.Errorf("config validation: providerSnapshotRepositories.%s.auth must be %q when set", name, ProviderSnapshotRepositoryAuthGCPADC)
		}
		if err := validateProviderSnapshotRepositoryPublish(name, repo.Publish); err != nil {
			return err
		}
	}
	return nil
}

func validateAppRegistries(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	for name, registry := range cfg.AppRegistries {
		if err := providerregistry.ValidateRepositoryName(name); err != nil {
			return fmt.Errorf("config validation: appRegistries.%s: %w", name, err)
		}
		if err := validateAppRegistryLocation("appRegistries", name, registry); err != nil {
			return err
		}
	}
	return nil
}

func validateAppRegistryLocation(prefix, name string, registry AppRegistryConfig) error {
	if strings.TrimSpace(registry.Kind) != AppRegistryKindGCS {
		return fmt.Errorf("config validation: %s.%s.kind must be gcs", prefix, name)
	}
	if registry.GCS == nil {
		return fmt.Errorf("config validation: %s.%s.gcs is required", prefix, name)
	}
	if _, err := registry.StorageURL(); err != nil {
		return fmt.Errorf("config validation: %s.%s: %w", prefix, name, err)
	}
	if _, err := registry.PublicURL(); err != nil {
		return fmt.Errorf("config validation: %s.%s: %w", prefix, name, err)
	}
	if registry.Retention != nil {
		if _, err := registry.UnusedRetentionDuration(); err != nil {
			return fmt.Errorf("config validation: %s.%s: %w", prefix, name, err)
		}
		if _, err := registry.DeployedRetentionDuration(); err != nil {
			return fmt.Errorf("config validation: %s.%s: %w", prefix, name, err)
		}
	}
	return nil
}

func validateServerAppRegistry(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	attempts := cfg.Server.AppRegistry.MaxReconcileAttempts
	if !cfg.Server.AppRegistry.maxReconcileAttemptsSet && attempts == 0 {
		cfg.Server.AppRegistry.MaxReconcileAttempts = DefaultAppRegistryMaxReconcileAttempts
	} else if attempts <= 0 {
		return fmt.Errorf("config validation: server.appRegistry.maxReconcileAttempts must be a positive integer")
	}
	if _, err := cfg.Server.AppRegistry.CatalogPollIntervalDuration(); err != nil {
		return fmt.Errorf("config validation: server.appRegistry.catalogPollInterval: %w", err)
	}
	if _, err := cfg.Server.AppRegistry.AutoDeployPollIntervalDuration(); err != nil {
		return fmt.Errorf("config validation: server.appRegistry.autoDeployPollInterval: %w", err)
	}
	heartbeatInterval, err := cfg.Server.AppRegistry.HeartbeatIntervalDuration()
	if err != nil {
		return fmt.Errorf("config validation: server.appRegistry.heartbeatInterval: %w", err)
	}
	heartbeatTTL, err := cfg.Server.AppRegistry.HeartbeatTTLDuration()
	if err != nil {
		return fmt.Errorf("config validation: server.appRegistry.heartbeatTtl: %w", err)
	}
	if heartbeatTTL <= heartbeatInterval {
		return fmt.Errorf("config validation: server.appRegistry.heartbeatTtl must be greater than heartbeatInterval")
	}
	healthyStabilityWindow, err := cfg.Server.AppRegistry.HealthyStabilityWindowDuration()
	if err != nil {
		return fmt.Errorf("config validation: server.appRegistry.healthyStabilityWindow: %w", err)
	}
	if healthyStabilityWindow <= heartbeatTTL {
		return fmt.Errorf("config validation: server.appRegistry.healthyStabilityWindow must be greater than heartbeatTtl")
	}
	if _, err := cfg.Server.AppRegistry.HeartbeatRetentionDuration(); err != nil {
		return fmt.Errorf("config validation: server.appRegistry.heartbeatRetention: %w", err)
	}
	cfg.Server.AppRegistry.RolloutMode = AppRegistryRolloutMode(strings.TrimSpace(string(cfg.Server.AppRegistry.RolloutMode)))
	if cfg.Server.AppRegistry.RolloutMode == "" {
		cfg.Server.AppRegistry.RolloutMode = AppRegistryRolloutModeEnrollment
	}
	switch cfg.Server.AppRegistry.RolloutMode {
	case AppRegistryRolloutModeEnrollment, AppRegistryRolloutModeHeartbeat:
	default:
		return fmt.Errorf(
			"config validation: server.appRegistry.rolloutMode must be %q or %q",
			AppRegistryRolloutModeEnrollment,
			AppRegistryRolloutModeHeartbeat,
		)
	}
	return validateAppRegistryPublishSettings(cfg)
}

func validateAppRegistryPublishSettings(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	publish := cfg.Server.AppRegistry.Publish
	if !publish.Enabled {
		return nil
	}
	registryName := strings.TrimSpace(publish.WritableRegistry)
	if registryName == "" {
		return fmt.Errorf("config validation: server.appRegistry.publish.writableRegistry is required when publish is enabled")
	}
	registry, ok := cfg.AppRegistries[registryName]
	if !ok {
		return fmt.Errorf("config validation: server.appRegistry.publish.writableRegistry %q is not configured under appRegistries", registryName)
	}
	if strings.TrimSpace(registry.Kind) != AppRegistryKindGCS {
		return fmt.Errorf("config validation: server.appRegistry.publish.writableRegistry %q must be a GCS app registry", registryName)
	}
	if _, err := registry.StorageURL(); err != nil {
		return fmt.Errorf("config validation: server.appRegistry.publish.writableRegistry %q: %w", registryName, err)
	}
	if _, err := publish.PublishLimits(); err != nil {
		return fmt.Errorf("config validation: server.appRegistry.publish: %w", err)
	}
	if len(publish.AllowedAppSet()) == 0 {
		return fmt.Errorf("config validation: server.appRegistry.publish.allowedApps must list at least one app when publish is enabled")
	}
	return nil
}

func validateProviderSnapshotRepositoryPublish(name string, publish ProviderSnapshotRepositoryPublishConfig) error {
	if publish.PathLayout == "" && !publish.Immutable && publish.Storage.Kind == "" && publish.Storage.URL == "" {
		return nil
	}
	if strings.TrimSpace(publish.PathLayout) != "sourceRef" {
		return fmt.Errorf("config validation: providerSnapshotRepositories.%s.publish.pathLayout must be sourceRef", name)
	}
	if !publish.Immutable {
		return fmt.Errorf("config validation: providerSnapshotRepositories.%s.publish.immutable must be true", name)
	}
	if strings.TrimSpace(publish.Storage.Kind) != "objectStore" {
		return fmt.Errorf("config validation: providerSnapshotRepositories.%s.publish.storage.kind must be objectStore", name)
	}
	if strings.TrimSpace(publish.Storage.URL) == "" {
		return fmt.Errorf("config validation: providerSnapshotRepositories.%s.publish.storage.url is required", name)
	}
	if !strings.HasPrefix(strings.TrimSpace(publish.Storage.URL), "gs://") {
		return fmt.Errorf("config validation: providerSnapshotRepositories.%s.publish.storage.url currently supports gs:// object store URLs", name)
	}
	return nil
}

func isFullGitSHA(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 40 {
		return false
	}
	for _, ch := range value {
		switch {
		case ch >= '0' && ch <= '9':
		case ch >= 'a' && ch <= 'f':
		case ch >= 'A' && ch <= 'F':
		default:
			return false
		}
	}
	return true
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
			return fmt.Errorf("config validation: connections.%s.exposure is not supported", id)
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
				if !binding.BindingResolved && ConnectionModeForConnection(*binding) == core.ConnectionModeSubject && connectionBindingRequiresUserCredential(binding) {
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
			if strings.TrimSpace(binding.Exposure) != "" {
				return fmt.Errorf("config validation: %s.%s.connections.%s.exposure is not supported", kind, name, localName)
			}
			if binding.CredentialRefresh != nil {
				resolved.CredentialRefresh = cloneCredentialRefreshDef(binding.CredentialRefresh)
			}
			entry.Connections[localName] = &resolved
		}
		return nil
	}
	for name, entry := range cfg.Apps {
		if err := normalizeEntry("apps", name, entry); err != nil {
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
		if err := validateAppOnlyProviderFields("providers."+string(kind)+"."+name, entry); err != nil {
			return err
		}
		switch kind {
		case HostProviderKindIdentity, HostProviderKindAuthorization:
			if err := validateProviderEntryRemote("providers."+string(kind)+"."+name, entry); err != nil {
				return err
			}
		default:
			if err := validateUnsupportedRemotePlacement("providers."+string(kind)+"."+name, entry); err != nil {
				return err
			}
		}
		switch kind {
		case HostProviderKindIdentity:
			if entry.Source.IsBuiltin() {
				return fmt.Errorf("config validation: identity provider %q does not support builtin providers; use a provider source reference or omit identity", name)
			}
			if err := validateProviderEntrySource("identity", name, entry); err != nil {
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
	if src.IsGit() {
		modeCount++
	}
	if src.IsRegistry() {
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
	if src.IsRegistry() && kind != "app" {
		return fmt.Errorf("config validation: %s %q source.registry is only supported on apps", kind, name)
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
	if src.IsGit() {
		git := src.GitSource()
		switch {
		case git == nil:
			return fmt.Errorf("config validation: %s %q source.git is required", kind, name)
		case strings.TrimSpace(git.Repo) == "":
			return fmt.Errorf("config validation: %s %q source.git.repo is required", kind, name)
		case strings.TrimSpace(git.Ref) == "":
			return fmt.Errorf("config validation: %s %q source.git.ref is required", kind, name)
		case !isFullGitSHA(git.Ref):
			return fmt.Errorf("config validation: %s %q source.git.ref must be a 40-character commit SHA", kind, name)
		case strings.TrimSpace(git.Path) == "":
			return fmt.Errorf("config validation: %s %q source.git.path is required", kind, name)
		}
		cleanPath := path.Clean(filepath.ToSlash(strings.TrimSpace(git.Path)))
		if cleanPath == "." || cleanPath == ".." || strings.HasPrefix(cleanPath, "../") || path.IsAbs(cleanPath) {
			return fmt.Errorf("config validation: %s %q source.git.path must be a clean relative path", kind, name)
		}
		if base := path.Base(cleanPath); base != "manifest.yaml" && base != "manifest.json" {
			return fmt.Errorf("config validation: %s %q source.git.path must reference a provider manifest", kind, name)
		}
		parsedRepo, err := url.Parse(strings.TrimSpace(git.Repo))
		if err != nil || parsedRepo.Scheme == "" {
			return fmt.Errorf("config validation: %s %q source.git.repo must be an absolute URL", kind, name)
		}
		switch parsedRepo.Scheme {
		case "http", "https", "file":
		default:
			return fmt.Errorf("config validation: %s %q source.git.repo must use http(s) or file URL", kind, name)
		}
		materialization := strings.TrimSpace(git.Materialization)
		repoName := strings.TrimSpace(git.ArtifactRepository)
		switch materialization {
		case "":
			materialization = "source"
		case "source", "snapshot":
		default:
			return fmt.Errorf("config validation: %s %q source.git.materialization must be source or snapshot", kind, name)
		}
		if materialization == "snapshot" {
			if repoName == "" {
				return fmt.Errorf("config validation: %s %q source.git.artifactRepository is required for materialization %q", kind, name, materialization)
			}
		} else if repoName != "" {
			return fmt.Errorf("config validation: %s %q source.git.artifactRepository is only supported for materialization \"snapshot\"", kind, name)
		}
		if repoName != "" {
			if err := providerregistry.ValidateRepositoryName(repoName); err != nil {
				return fmt.Errorf("config validation: %s %q source.git.artifactRepository: %w", kind, name, err)
			}
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
		if !src.IsMetadataURL() && !src.IsGitHubRelease() && !src.IsLocalMetadataPath() && !src.IsPackage() && !src.IsGit() {
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
		case core.ConnectionModeNone, core.ConnectionModeSubject:
		default:
			return fmt.Errorf("config validation: connections.%s mode %q is not supported", id, conn.Mode)
		}
		if err := validateCredentialRefresh(fmt.Sprintf("connections.%s", id), *conn); err != nil {
			return err
		}
		if len(conn.Auth.TokenExchangeDrivers) > 0 {
			return fmt.Errorf("config validation: connections.%s auth.tokenExchangeDrivers is not supported; use managed-subject credentials", id)
		}
		if conn.Auth.Type == providermanifestv1.AuthTypeOAuth2 && strings.TrimSpace(conn.Auth.GrantType) == "client_credentials" {
			return fmt.Errorf("config validation: connections.%s oauth2 client_credentials is not supported; use managed-subject credentials", id)
		}
		if conn.Auth.Type == providermanifestv1.AuthTypeOAuth2 && strings.TrimSpace(conn.Auth.GrantType) == "refresh_token" {
			return fmt.Errorf("config validation: connections.%s oauth2 refresh_token is not supported; use managed-subject credentials", id)
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
// resolved remote app manifests.
func ValidateResolvedStructure(cfg *Config) error {
	for name, entry := range cfg.Apps {
		if entry == nil {
			return fmt.Errorf("config validation: integration %q requires a source", name)
		}
		if err := validateAppIntegrationConnections(name, entry); err != nil {
			return err
		}
	}
	return nil
}

// ValidateRuntime checks runtime-only requirements: encryption key plus the
// required top-level IndexedDB provider.
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
	cfg.Server.Runtime.DefaultProvider = strings.TrimSpace(cfg.Server.Runtime.DefaultProvider)
	cfg.Server.Runtime.RelayBaseURL = strings.TrimRight(strings.TrimSpace(cfg.Server.Runtime.RelayBaseURL), "/")
	if err := validateRuntimeRelayBaseURL(cfg.Server.Runtime.RelayBaseURL); err != nil {
		return err
	}
	if err := validateRemotesConfig(cfg); err != nil {
		return err
	}
	for name, entry := range cfg.Runtime.Providers {
		if entry == nil {
			return fmt.Errorf("config validation: runtime.providers.%s is required", name)
		}
		if err := validateAppOnlyProviderFields("runtime.providers."+name, &entry.ProviderEntry); err != nil {
			return err
		}
		if err := validateUnsupportedRemotePlacement("runtime.providers."+name, &entry.ProviderEntry); err != nil {
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

func validateHTTPOriginURL(label, raw string) error {
	parsed, err := validateAbsoluteBaseURL(label, raw)
	if err != nil || parsed == nil {
		return err
	}
	if path := strings.TrimSpace(parsed.EscapedPath()); path != "" && path != "/" {
		return fmt.Errorf("config validation: %s must not include a path", label)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		return nil
	default:
		return fmt.Errorf("config validation: %s must use http or https", label)
	}
}

func validateRuntimeRelayBaseURL(raw string) error {
	return validateHTTPOriginURL("server.runtime.relayBaseUrl", raw)
}

// ValidateRemoteGestaltd checks serve-time remote requirements.
func ValidateRemoteGestaltd(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	if err := validateRemotesConfig(cfg); err != nil {
		return err
	}
	referenced := cfg.ReferencedRemoteNames()
	for _, name := range referenced {
		remote := cfg.Server.Remotes[name]
		if remote == nil || strings.TrimSpace(remote.Token) != "" {
			continue
		}
		return fmt.Errorf("config validation: server.remotes.%s.token is required when remote %q is referenced", name, name)
	}
	return nil
}

// ApplyServeRemoteOverrides applies gestaltd serve CLI overrides for remote
// gestaltd delegation and validates the resolved remote configuration.
// When dev is true, empty remote and remoteToken values are resolved from
// the gestalt CLI config (~/.config/gestalt/config.json and credentials.json)
// before applying overrides, so `gestaltd dev` users can omit --remote/--remote-token.
func ApplyServeRemoteOverrides(cfg *Config, remote, remoteToken string, dev bool) error {
	if cfg == nil {
		return nil
	}
	cfg.Server.Dev = dev
	if dev {
		if remote == "" {
			remote, _ = defaultRemoteURL()
		}
		if remoteToken == "" {
			remoteToken, _ = defaultRemoteToken()
		}
	}
	if len(cfg.Server.Remotes) > 0 {
		_, defaultRemote, ok := cfg.DefaultRemoteEntry()
		if !ok || defaultRemote == nil {
			return fmt.Errorf("config validation: --remote/--remote-token require a default server.remotes entry (set default: true on one)")
		}
		if remote != "" {
			defaultRemote.URL = remote
		}
		if remoteToken != "" {
			defaultRemote.Token = remoteToken
		}
	} else {
		if remote != "" {
			cfg.Server.Remote = remote
		}
		if remoteToken != "" {
			cfg.Server.RemoteToken = remoteToken
		}
	}
	if err := canonicalizeRemotes(cfg); err != nil {
		return err
	}
	// Resolve a missing default-remote token from the environment or stored
	// credentials, but only when the default is actually referenced — an
	// unreferenced default needs no token. This runs for both serve and dev;
	// for dev the token was already resolved above, so this is a no-op.
	if defaultName, defaultRemote, ok := cfg.DefaultRemoteEntry(); ok && defaultRemote != nil {
		if strings.TrimSpace(defaultRemote.Token) == "" && slices.Contains(cfg.ReferencedRemoteNames(), defaultName) {
			token, err := defaultRemoteToken()
			if err != nil {
				return err
			}
			defaultRemote.Token = token
		}
	}
	return ValidateRemoteGestaltd(cfg)
}

// ApplyServeRemotePreviewOverrides configures gestaltd serve --remote-preview.
// It resolves the Gestalt bearer token from CLI config when omitted and marks
// the default remote for Cloud Run IAM authentication to the preview URL.
func ApplyServeRemotePreviewOverrides(cfg *Config, previewURL, remoteToken string) error {
	if cfg == nil {
		return nil
	}
	previewURL = normalizeRemoteURL(previewURL)
	if previewURL == "" {
		return fmt.Errorf("--remote-preview URL is required")
	}
	if strings.TrimSpace(remoteToken) == "" {
		var err error
		remoteToken, err = defaultRemoteToken()
		if err != nil {
			return err
		}
	}
	if strings.TrimSpace(remoteToken) == "" {
		return fmt.Errorf("remote-preview requires a Gestalt API token; run gestalt auth login or set GESTALT_API_KEY")
	}
	cfg.Server.RemotePreviewServe = true
	cfg.Server.Remote = previewURL
	cfg.Server.RemoteToken = remoteToken
	if cfg.Server.Remotes == nil {
		cfg.Server.Remotes = make(map[string]*RemoteConfig)
	}
	cfg.Server.Remotes[DefaultRemoteName] = &RemoteConfig{
		URL:         previewURL,
		Token:       remoteToken,
		Default:     true,
		CloudRunIAM: shouldUseCloudRunIAM(previewURL),
	}
	if err := canonicalizeRemotes(cfg); err != nil {
		return err
	}
	stampDefaultRemote(cfg.Apps, DefaultRemoteName, false)
	for _, entries := range []map[string]*ProviderEntry{
		cfg.Providers.Identity,
		cfg.Providers.Authorization,
		cfg.Providers.IndexedDB,
		cfg.Providers.Workflow,
		cfg.Providers.Agent,
	} {
		stampDefaultRemote(entries, DefaultRemoteName, false)
	}
	return ValidateRemoteGestaltd(cfg)
}

func shouldUseCloudRunIAM(rawURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" {
		return false
	}
	switch parsed.Scheme {
	case "https":
		return !actionableLoopbackHost(parsed.Hostname())
	default:
		return false
	}
}

func actionableLoopbackHost(host string) bool {
	switch strings.TrimSpace(host) {
	case "", "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

func defaultRemoteToken() (string, error) {
	if token := strings.TrimSpace(os.Getenv("GESTALT_API_KEY")); token != "" {
		return token, nil
	}
	path := gestaltCredentialsPath()
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("config validation: read Gestalt credentials: %w", err)
	}
	var creds struct {
		APIToken string `json:"api_token"`
	}
	if err := json.Unmarshal(data, &creds); err != nil {
		return "", fmt.Errorf("config validation: parse Gestalt credentials: %w", err)
	}
	return strings.TrimSpace(creds.APIToken), nil
}

// ResolveGestaltCLIURL returns GESTALT_URL or the gestalt CLI config from `gestalt init`.
func ResolveGestaltCLIURL() (string, error) {
	return defaultRemoteURL()
}

// ResolveGestaltCLIToken returns GESTALT_API_KEY or credentials from `gestalt auth login`.
func ResolveGestaltCLIToken() (string, error) {
	return defaultRemoteToken()
}

func gestaltCredentialsPath() string {
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "gestalt", "credentials.json")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "gestalt", "credentials.json")
	}
	return ""
}

func defaultRemoteURL() (string, error) {
	raw := strings.TrimSpace(os.Getenv("GESTALT_URL"))
	if raw == "" {
		path := gestaltConfigPath()
		if path == "" {
			return "", nil
		}
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			return "", nil
		}
		if err != nil {
			return "", fmt.Errorf("config validation: read Gestalt config: %w", err)
		}
		var cfg map[string]string
		if err := json.Unmarshal(data, &cfg); err != nil {
			return "", fmt.Errorf("config validation: parse Gestalt config: %w", err)
		}
		raw = strings.TrimSpace(cfg["url"])
	}
	if raw == "" {
		return "", nil
	}
	return normalizeRemoteURL(raw), nil
}

// normalizeRemoteURL mirrors the gestalt CLI's URL normalization: adds an
// http/https scheme when missing and strips a trailing slash.
func normalizeRemoteURL(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimRight(raw, "/")
	if strings.Contains(raw, "://") {
		return raw
	}
	scheme := "https"
	if parsed, err := url.Parse("http://" + raw); err == nil && parsed.Host != "" {
		switch parsed.Hostname() {
		case "localhost", "127.0.0.1", "::1":
			scheme = "http"
		}
	}
	return scheme + "://" + raw
}

func gestaltConfigPath() string {
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "gestalt", "config.json")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "gestalt", "config.json")
	}
	return ""
}

func runtimeProviderUsesSource(entry *RuntimeProviderEntry) bool {
	if entry == nil {
		return false
	}
	return entry.Source.IsBuiltin() ||
		entry.Source.IsMetadataURL() ||
		entry.Source.IsGitHubRelease() ||
		entry.Source.IsGit() ||
		entry.Source.IsLocalMetadataPath() ||
		entry.Source.IsLocal() ||
		entry.Source.UnsupportedURL() != ""
}

func validateApp(cfg *Config, name string, entry *ProviderEntry) error {
	if entry == nil {
		return fmt.Errorf("config validation: app %q requires a source", name)
	}
	if err := validateRootAppPromptConfig(entry); err != nil {
		return fmt.Errorf("config validation: apps.%s.config.prompts: %w", name, err)
	}
	if entry.Default {
		return fmt.Errorf("config validation: apps.%s.default is not supported on apps", name)
	}
	if entry.Lifecycle != nil {
		return fmt.Errorf("config validation: apps.%s.lifecycle is only supported on providers.agent.*", name)
	}
	if entry.IndexedDB != nil {
		entry.IndexedDB.Provider = strings.TrimSpace(entry.IndexedDB.Provider)
		entry.IndexedDB.DB = strings.TrimSpace(entry.IndexedDB.DB)
		for i, store := range entry.IndexedDB.ObjectStores {
			entry.IndexedDB.ObjectStores[i] = strings.TrimSpace(store)
		}
	}
	if err := normalizeProviderRuntimeConfig("apps."+name, entry, false); err != nil {
		return err
	}
	if err := validateProviderEntryRemote("apps."+name, entry); err != nil {
		return err
	}
	if err := validateProviderEntrySource("app", name, entry); err != nil {
		return err
	}
	if entry.Source.IsRegistry() {
		registryName := strings.TrimSpace(entry.Source.Registry)
		if _, ok := cfg.AppRegistries[registryName]; !ok {
			return fmt.Errorf("config validation: apps.%s.source.registry references unknown app registry %q", name, registryName)
		}
	}
	if err := validateAppRouteAuth(cfg, name, entry); err != nil {
		return err
	}
	if err := validateAppIndexedDBConfig(cfg, name, entry); err != nil {
		return err
	}
	if err := validateAppCacheBindings(cfg, name, entry); err != nil {
		return err
	}
	if err := validateAppS3Bindings(cfg, name, entry); err != nil {
		return err
	}
	if _, err := cfg.EffectiveRuntimePlacement("apps."+name, entry); err != nil {
		return err
	}
	return validateAppIntegrationConnections(name, entry)
}

func validateAppRouteAuth(cfg *Config, name string, entry *ProviderEntry) error {
	if entry == nil || entry.RouteAuth == nil {
		return nil
	}
	entry.RouteAuth.Provider = strings.TrimSpace(entry.RouteAuth.Provider)
	if entry.RouteAuth.Provider == "" {
		return fmt.Errorf("config validation: apps.%s.auth.provider is required", name)
	}
	if entry.RouteAuth.Provider == "server" {
		_, authProvider, err := cfg.SelectedIdentityProvider()
		if err != nil {
			return err
		}
		if authProvider == nil {
			return fmt.Errorf("config validation: apps.%s.auth.provider %q requires a configured platform identity provider", name, entry.RouteAuth.Provider)
		}
		return nil
	}
	if _, ok := cfg.Providers.Identity[entry.RouteAuth.Provider]; !ok {
		return fmt.Errorf("config validation: apps.%s.auth.provider references unknown identity provider %q", name, entry.RouteAuth.Provider)
	}
	return nil
}

func validateIndexedDBConfig(cfg *Config) error {
	for name, entry := range cfg.Providers.IndexedDB {
		if entry == nil {
			return fmt.Errorf("config validation: providers.indexeddb.%s is required", name)
		}
		if err := validateAppOnlyProviderFields("providers.indexeddb."+name, entry); err != nil {
			return err
		}
		if err := validateProviderEntryRemote("providers.indexeddb."+name, entry); err != nil {
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
		if err := validateAppOnlyProviderFields("providers.cache."+name, entry); err != nil {
			return err
		}
		if err := validateUnsupportedRemotePlacement("providers.cache."+name, entry); err != nil {
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
		if err := validateAppOnlyProviderFields("providers.s3."+name, entry); err != nil {
			return err
		}
		if err := validateUnsupportedRemotePlacement("providers.s3."+name, entry); err != nil {
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
		if err := validateProviderEntryRemote("providers.workflow."+name, entry); err != nil {
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
		if err := normalizeProviderRuntimeConfig("providers.agent."+name, entry, true); err != nil {
			return err
		}
		if err := validateAgentProviderLifecycleConfig("providers.agent."+name+".lifecycle", entry.Lifecycle); err != nil {
			return err
		}
		if err := validateAgentProviderFields(cfg, name, entry); err != nil {
			return err
		}
		if err := validateProviderEntryRemote("providers.agent."+name, entry); err != nil {
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
		return fmt.Errorf("config validation: %s.auth is only supported on apps.*; use %s.source.auth for source auth", subject, subject)
	}
	if len(entry.Cache) > 0 {
		return fmt.Errorf("config validation: %s.cache is only supported on apps.*", subject)
	}
	if len(entry.S3) > 0 {
		return fmt.Errorf("config validation: %s.s3 is only supported on apps.*", subject)
	}
	if entry.Surfaces != nil {
		return fmt.Errorf("config validation: %s.surfaces is only supported on apps.*", subject)
	}
	if entry.AuthorizationPolicy != "" {
		return fmt.Errorf("config validation: %s.authorizationPolicy is only supported on apps.*", subject)
	}
	if _, err := cfg.EffectiveRuntimePlacement(subject, entry); err != nil {
		return err
	}
	if entry.UsesRuntimePlacement() {
		if err := validateRuntimePlacedAgentLifecyclePolicy(subject, entry); err != nil {
			return err
		}
	}
	if entry.IndexedDB == nil {
		return nil
	}

	seenStores := make(map[string]struct{}, len(entry.IndexedDB.ObjectStores))
	for i, store := range entry.IndexedDB.ObjectStores {
		if store == "" {
			return fmt.Errorf("config validation: %s.idb.objectStores[%d] is required", subject, i)
		}
		if _, exists := seenStores[store]; exists {
			return fmt.Errorf("config validation: %s.idb.objectStores[%d] duplicates %q", subject, i, store)
		}
		seenStores[store] = struct{}{}
	}
	if _, err := cfg.EffectiveAgentIndexedDB(name, entry); err != nil {
		return err
	}
	return nil
}

func normalizeRuntimePlacementConfig(subject string, runtimeCfg *RuntimePlacementConfig) error {
	if runtimeCfg == nil {
		return nil
	}
	if runtimeCfg.Pool != nil {
		runtimeCfg.Pool.StartupTimeout = strings.TrimSpace(runtimeCfg.Pool.StartupTimeout)
		runtimeCfg.Pool.HealthCheckInterval = strings.TrimSpace(runtimeCfg.Pool.HealthCheckInterval)
		runtimeCfg.Pool.RestartPolicy = RuntimePlacementRestartPolicy(strings.TrimSpace(string(runtimeCfg.Pool.RestartPolicy)))
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
		if err := validateRuntimePlacementDockerConfigJSON(runtimeCfg.ImagePullAuth.DockerConfigJSON); err != nil {
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
	if err := normalizeRuntimePlacementWorkspaceConfig(subject, runtimeCfg.Workspace); err != nil {
		return err
	}
	return nil
}

func normalizeRuntimePlacementWorkspaceConfig(subject string, workspace *RuntimePlacementWorkspaceConfig) error {
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
			identity, err := coreagent.CanonicalGitRepositoryAllowlistIdentity(repo)
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

func validateRuntimePlacementDockerConfigJSON(value string) error {
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

func validateRuntimePlacedAgentLifecyclePolicy(subject string, entry *ProviderEntry) error {
	return validateRuntimePlacementLifecyclePolicy(subject, entry, "agent")
}

func validateRuntimePlacedWorkflowLifecyclePolicy(subject string, entry *ProviderEntry) error {
	runtimeCfg, runtimePath := runtimePlacementConfigAndPath(subject, entry)
	if runtimeCfg == nil {
		return fmt.Errorf("config validation: %s is required for runtime-placed workflow providers", runtimePath)
	}
	if !runtimeCfg.LifecyclePolicyFieldsSet() {
		return fmt.Errorf("config validation: %s.pool is required for runtime-placed workflow providers", runtimePath)
	}
	if err := validateRuntimePlacementLifecyclePolicy(subject, entry, "workflow"); err != nil {
		return err
	}
	lifecycle := runtimeCfg.lifecyclePolicyConfig()
	if lifecycle.MaxReadyInstances != lifecycle.MinReadyInstances {
		return fmt.Errorf("config validation: %s.pool.maxReadyInstances must equal minReadyInstances for fixed workflow worker pools", runtimePath)
	}
	return nil
}

func validateRuntimePlacementLifecyclePolicy(subject string, entry *ProviderEntry, providerKind string) error {
	if entry == nil || !entry.UsesRuntimePlacement() {
		return nil
	}
	runtimeCfg, runtimePath := runtimePlacementConfigAndPath(subject, entry)
	if runtimeCfg == nil {
		if providerKind == "" {
			providerKind = "provider"
		}
		return fmt.Errorf("config validation: %s is required for hosted %s providers", runtimePath, providerKind)
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
	case RuntimePlacementRestartPolicyAlways, RuntimePlacementRestartPolicyNever:
	default:
		return fmt.Errorf("config validation: %s.restartPolicy must be one of %q or %q", lifecycleSubject, RuntimePlacementRestartPolicyAlways, RuntimePlacementRestartPolicyNever)
	}
	if _, err := runtimeCfg.LifecyclePolicy(); err != nil {
		return fmt.Errorf("config validation: %s.%w", lifecycleSubject, err)
	}
	return nil
}

func runtimePlacementConfigAndPath(subject string, entry *ProviderEntry) (*RuntimePlacementConfig, string) {
	if entry == nil {
		return nil, subject + ".runtime"
	}
	if entry.Runtime != nil {
		return entry.Runtime, subject + ".runtime"
	}
	return nil, subject + ".runtime"
}

func validateWorkflowProviderFields(cfg *Config, name string, entry *ProviderEntry) error {
	if entry == nil {
		return nil
	}
	subject := "providers.workflow." + name
	if entry.RouteAuth != nil {
		return fmt.Errorf("config validation: %s.auth is only supported on apps.*; use %s.source.auth for source auth", subject, subject)
	}
	if len(entry.Cache) > 0 {
		return fmt.Errorf("config validation: %s.cache is only supported on apps.*", subject)
	}
	if len(entry.S3) > 0 {
		return fmt.Errorf("config validation: %s.s3 is only supported on apps.*", subject)
	}
	if entry.Lifecycle != nil {
		return fmt.Errorf("config validation: %s.lifecycle is only supported on providers.agent.*", subject)
	}
	if err := normalizeProviderRuntimeConfig(subject, entry, true); err != nil {
		return err
	}
	if _, err := cfg.EffectiveRuntimePlacement(subject, entry); err != nil {
		return err
	}
	if entry.UsesRuntimePlacement() {
		if err := validateRuntimePlacedWorkflowLifecyclePolicy(subject, entry); err != nil {
			return err
		}
	}
	if entry.Surfaces != nil {
		return fmt.Errorf("config validation: %s.surfaces is only supported on apps.*", subject)
	}
	if entry.AuthorizationPolicy != "" {
		return fmt.Errorf("config validation: %s.authorizationPolicy is only supported on apps.*", subject)
	}
	if entry.IndexedDB == nil {
		return nil
	}

	seenStores := make(map[string]struct{}, len(entry.IndexedDB.ObjectStores))
	for i, store := range entry.IndexedDB.ObjectStores {
		if store == "" {
			return fmt.Errorf("config validation: %s.idb.objectStores[%d] is required", subject, i)
		}
		if _, exists := seenStores[store]; exists {
			return fmt.Errorf("config validation: %s.idb.objectStores[%d] duplicates %q", subject, i, store)
		}
		seenStores[store] = struct{}{}
	}
	if _, err := cfg.EffectiveWorkflowIndexedDB(name, entry); err != nil {
		return err
	}
	return nil
}

func validateAppOnlyProviderFields(subject string, entry *ProviderEntry) error {
	if entry == nil {
		return nil
	}
	if entry.RouteAuth != nil {
		return fmt.Errorf("config validation: %s.auth is only supported on apps.*; use %s.source.auth for source auth", subject, subject)
	}
	if entry.IndexedDB != nil {
		return fmt.Errorf("config validation: %s.indexeddb is only supported on apps.*", subject)
	}
	if len(entry.Cache) > 0 {
		return fmt.Errorf("config validation: %s.cache is only supported on apps.*", subject)
	}
	if len(entry.S3) > 0 {
		return fmt.Errorf("config validation: %s.s3 is only supported on apps.*", subject)
	}
	if entry.Lifecycle != nil {
		return fmt.Errorf("config validation: %s.lifecycle is only supported on providers.agent.*", subject)
	}
	if entry.Runtime != nil {
		return fmt.Errorf("config validation: %s.runtime is only supported on apps.* and providers.agent.* and providers.workflow.*", subject)
	}
	if entry.Surfaces != nil {
		return fmt.Errorf("config validation: %s.surfaces is only supported on apps.*", subject)
	}
	if entry.AuthorizationPolicy != "" {
		return fmt.Errorf("config validation: %s.authorizationPolicy is only supported on apps.*", subject)
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

func normalizeProviderRuntimeConfig(subject string, entry *ProviderEntry, allowLifecycle bool) error {
	if entry == nil {
		return nil
	}
	if entry.Runtime == nil {
		return nil
	}
	if err := normalizeRuntimePlacementConfig(subject, entry.Runtime); err != nil {
		return err
	}
	if !allowLifecycle && entry.Runtime.LifecyclePolicyFieldsSet() {
		return fmt.Errorf("config validation: %s.runtime lifecycle fields are only supported on hosted agent and workflow providers", subject)
	}
	return nil
}

func validateAuthorizationModelConfig(cfg *Config) error {
	definedResourceTypes := make(map[string]string)
	resourceTypeRelations := make(map[string]map[string]AuthorizationRelationDef)
	for modelName, model := range cfg.Authorization.Models {
		if err := validateAuthorizationMapKey("authorization.models", modelName); err != nil {
			return err
		}
		if err := validateAuthorizationSourceMetadata("authorization.models."+modelName+".source", model.Source); err != nil {
			return err
		}
		for resourceTypeName, resourceType := range model.ResourceTypes {
			resourcePath := fmt.Sprintf("authorization.models.%s.resourceTypes.%s", modelName, resourceTypeName)
			if err := validateAuthorizationResourceTypeDef(fmt.Sprintf("authorization.models.%s.resourceTypes", modelName), resourceTypeName, resourcePath, resourceType); err != nil {
				return err
			}
			if prev, exists := definedResourceTypes[resourceTypeName]; exists {
				return fmt.Errorf("config validation: %s duplicates resource type already defined at %s", resourcePath, prev)
			}
			definedResourceTypes[resourceTypeName] = resourcePath
			resourceTypeRelations[resourceTypeName] = resourceType.Relations
		}
	}
	for modelName, model := range cfg.Authorization.Models {
		for resourceTypeName, resourceType := range model.ResourceTypes {
			resourcePath := fmt.Sprintf("authorization.models.%s.resourceTypes.%s", modelName, resourceTypeName)
			if err := validateAuthorizationResourceTypeReferences(resourcePath, resourceType, resourceTypeRelations); err != nil {
				return err
			}
		}
	}
	for resourceTypeName := range cfg.Authorization.ResourceTypes {
		if err := validateAuthorizationMapKey("authorization.resourceTypes", resourceTypeName); err != nil {
			return err
		}
		if _, ok := definedResourceTypes[resourceTypeName]; !ok {
			return fmt.Errorf("config validation: authorization.resourceTypes.%s references unknown resource type %q", resourceTypeName, resourceTypeName)
		}
	}
	for i := range cfg.Authorization.Relationships {
		relationship := &cfg.Authorization.Relationships[i]
		path := fmt.Sprintf("authorization.relationships[%d]", i)
		if err := validateAuthorizationRelationshipDef(path, *relationship, resourceTypeRelations); err != nil {
			return err
		}
	}
	return nil
}

func validateAuthorizationResourceTypeReferences(path string, def AuthorizationResourceTypeDef, resourceTypes map[string]map[string]AuthorizationRelationDef) error {
	for relationName, relation := range def.Relations {
		relationPath := path + ".relations." + relationName
		for i := range relation.AllowedTargets {
			target := &relation.AllowedTargets[i]
			targetPath := fmt.Sprintf("%s.allowedTargets[%d]", relationPath, i)
			if resourceType := strings.TrimSpace(target.ResourceType); resourceType != "" {
				if _, ok := resourceTypes[resourceType]; !ok {
					return fmt.Errorf("config validation: %s.resourceType references unknown resource type %q", targetPath, resourceType)
				}
			}
			if target.SubjectSet != nil {
				resourceType := strings.TrimSpace(target.SubjectSet.ResourceType)
				relations, ok := resourceTypes[resourceType]
				if !ok {
					return fmt.Errorf("config validation: %s.subjectSet.resourceType references unknown resource type %q", targetPath, resourceType)
				}
				relation := strings.TrimSpace(target.SubjectSet.Relation)
				if _, ok := relations[relation]; !ok {
					return fmt.Errorf("config validation: %s.subjectSet.relation references unknown relation %q for resource type %q", targetPath, relation, resourceType)
				}
			}
		}
	}
	return nil
}

func validateAuthorizationResourceTypeDef(parentPath, name, path string, def AuthorizationResourceTypeDef) error {
	if err := validateAuthorizationMapKey(parentPath, name); err != nil {
		return err
	}
	if len(def.Relations) == 0 {
		return fmt.Errorf("config validation: %s.relations must define at least one relation", path)
	}
	if err := validateAuthorizationSourceMetadata(path+".source", def.Source); err != nil {
		return err
	}
	defaultRole := strings.TrimSpace(def.DefaultRole)
	if defaultRole != "" {
		if err := validateAuthorizationMapKey(path, defaultRole); err != nil {
			return fmt.Errorf("config validation: %s.defaultRole: %w", path, err)
		}
		if _, ok := def.Relations[defaultRole]; !ok {
			return fmt.Errorf("config validation: %s.defaultRole references undefined relation %q", path, defaultRole)
		}
	}
	for relationName, relation := range def.Relations {
		relationPath := path + ".relations." + relationName
		if err := validateAuthorizationMapKey(path+".relations", relationName); err != nil {
			return err
		}
		if err := validateStringList(relationPath+".subjectTypes", relation.SubjectTypes); err != nil {
			return err
		}
		if len(relation.SubjectTypes) == 0 && len(relation.AllowedTargets) == 0 {
			return fmt.Errorf("config validation: %s must set subjectTypes or allowedTargets", relationPath)
		}
		for i, target := range relation.AllowedTargets {
			if err := validateAuthorizationAllowedTargetDef(fmt.Sprintf("%s.allowedTargets[%d]", relationPath, i), target); err != nil {
				return err
			}
		}
		if err := validateAuthorizationSourceMetadata(relationPath+".source", relation.Source); err != nil {
			return err
		}
	}
	for actionName, action := range def.Actions {
		actionPath := path + ".actions." + actionName
		if err := validateAuthorizationMapKey(path+".actions", actionName); err != nil {
			return err
		}
		if err := validateStringList(actionPath+".relations", action.Relations); err != nil {
			return err
		}
		if len(action.Relations) == 0 {
			return fmt.Errorf("config validation: %s.relations must contain at least one value", actionPath)
		}
		for _, relation := range action.Relations {
			if _, ok := def.Relations[relation]; !ok {
				return fmt.Errorf("config validation: %s.relations references unknown relation %q", actionPath, relation)
			}
		}
		if err := validateAuthorizationSourceMetadata(actionPath+".source", action.Source); err != nil {
			return err
		}
	}
	return nil
}

func validateAuthorizationAllowedTargetDef(path string, target AuthorizationAllowedTargetDef) error {
	set := 0
	if strings.TrimSpace(target.SubjectType) != "" {
		set++
	}
	if strings.TrimSpace(target.ResourceType) != "" {
		set++
	}
	if target.SubjectSet != nil {
		set++
		if strings.TrimSpace(target.SubjectSet.ResourceType) == "" {
			return fmt.Errorf("config validation: %s.subjectSet.resourceType is required", path)
		}
		if strings.TrimSpace(target.SubjectSet.Relation) == "" {
			return fmt.Errorf("config validation: %s.subjectSet.relation is required", path)
		}
	}
	if set != 1 {
		return fmt.Errorf("config validation: %s must set exactly one of subjectType, resourceType, or subjectSet", path)
	}
	return nil
}

func validateAuthorizationRelationshipDef(path string, relationship AuthorizationRelationshipDef, resourceTypes map[string]map[string]AuthorizationRelationDef) error {
	if hasAuthorizationRelationshipTarget(relationship.Target) {
		if err := validateAuthorizationRelationshipTargetDef(path+".target", relationship.Target, resourceTypes); err != nil {
			return err
		}
	} else {
		if err := validateAuthorizationSubjectDef(path+".subject", relationship.Subject); err != nil {
			return err
		}
	}
	if strings.TrimSpace(relationship.Relation) == "" {
		return fmt.Errorf("config validation: %s.relation is required", path)
	}
	if err := validateAuthorizationResourceDef(path+".resource", relationship.Resource); err != nil {
		return err
	}
	resourceType := strings.TrimSpace(relationship.Resource.Type)
	relations, ok := resourceTypes[resourceType]
	if !ok {
		return fmt.Errorf("config validation: %s.resource.type references unknown resource type %q", path, resourceType)
	}
	if _, ok := relations[strings.TrimSpace(relationship.Relation)]; !ok {
		return fmt.Errorf("config validation: %s.relation references unknown relation %q for resource type %q", path, relationship.Relation, resourceType)
	}
	if err := validateAuthorizationSourceMetadata(path+".source", relationship.Source); err != nil {
		return err
	}
	return nil
}

func hasAuthorizationRelationshipTarget(target AuthorizationRelationshipTargetDef) bool {
	return target.Subject != nil || target.Resource != nil || target.SubjectSet != nil
}

func validateAuthorizationMapKey(path, key string) error {
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("config validation: %s keys must be non-empty", path)
	}
	if key != strings.TrimSpace(key) {
		return fmt.Errorf("config validation: %s key %q must not have surrounding whitespace", path, key)
	}
	return nil
}

func validateAuthorizationSubjectDef(path string, subject AuthorizationSubjectDef) error {
	if strings.TrimSpace(subject.Type) == "" {
		return fmt.Errorf("config validation: %s.type is required", path)
	}
	id := strings.TrimSpace(subject.ID)
	email := strings.TrimSpace(subject.Email)
	if (id == "") == (email == "") {
		return fmt.Errorf("config validation: %s must set exactly one of id or email", path)
	}
	if email == "" {
		return nil
	}
	if strings.TrimSpace(subject.Type) != "subject" {
		return fmt.Errorf("config validation: %s.email requires type %q", path, "subject")
	}
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email {
		return fmt.Errorf("config validation: %s.email must be a valid bare email address", path)
	}
	return nil
}

func validateAuthorizationResourceDef(path string, resource AuthorizationResourceDef) error {
	if strings.TrimSpace(resource.Type) == "" {
		return fmt.Errorf("config validation: %s.type is required", path)
	}
	if strings.TrimSpace(resource.ID) == "" {
		return fmt.Errorf("config validation: %s.id is required", path)
	}
	return nil
}

func validateAuthorizationRelationshipTargetDef(path string, target AuthorizationRelationshipTargetDef, resourceTypes map[string]map[string]AuthorizationRelationDef) error {
	set := 0
	if target.Subject != nil {
		set++
		if err := validateAuthorizationSubjectDef(path+".subject", *target.Subject); err != nil {
			return err
		}
	}
	if target.Resource != nil {
		set++
		if err := validateAuthorizationResourceDef(path+".resource", *target.Resource); err != nil {
			return err
		}
		if len(resourceTypes) > 0 {
			if _, ok := resourceTypes[strings.TrimSpace(target.Resource.Type)]; !ok {
				return fmt.Errorf("config validation: %s.resource.type references unknown resource type %q", path, target.Resource.Type)
			}
		}
	}
	if target.SubjectSet != nil {
		set++
		if err := validateAuthorizationResourceDef(path+".subjectSet.resource", target.SubjectSet.Resource); err != nil {
			return err
		}
		if strings.TrimSpace(target.SubjectSet.Relation) == "" {
			return fmt.Errorf("config validation: %s.subjectSet.relation is required", path)
		}
		if len(resourceTypes) > 0 {
			relations, ok := resourceTypes[strings.TrimSpace(target.SubjectSet.Resource.Type)]
			if !ok {
				return fmt.Errorf("config validation: %s.subjectSet.resource.type references unknown resource type %q", path, target.SubjectSet.Resource.Type)
			}
			if _, ok := relations[strings.TrimSpace(target.SubjectSet.Relation)]; !ok {
				return fmt.Errorf("config validation: %s.subjectSet.relation references unknown relation %q for resource type %q", path, target.SubjectSet.Relation, target.SubjectSet.Resource.Type)
			}
		}
	}
	if set > 1 {
		return fmt.Errorf("config validation: %s must set at most one of subject, resource, or subjectSet", path)
	}
	return nil
}

func validateAuthorizationSourceMetadata(path string, source AuthorizationSourceMetadataDef) error {
	ownerKind := strings.TrimSpace(source.OwnerKind)
	ownerID := strings.TrimSpace(source.OwnerID)
	if ownerKind == "" && ownerID != "" {
		return fmt.Errorf("config validation: %s.ownerKind is required when ownerId is set", path)
	}
	if ownerKind != "" && ownerID == "" {
		return fmt.Errorf("config validation: %s.ownerId is required when ownerKind is set", path)
	}
	return nil
}

func validateStringList(path string, values []string) error {
	seen := make(map[string]int, len(values))
	for i, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return fmt.Errorf("config validation: %s[%d] is required", path, i)
		}
		if prev, exists := seen[trimmed]; exists {
			return fmt.Errorf("config validation: %s[%d] duplicates [%d]", path, i, prev)
		}
		seen[trimmed] = i
	}
	return nil
}

func validateAdminConfig(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	admin := cfg.Server.Admin
	policy := strings.TrimSpace(admin.AuthorizationPolicy)
	if policy != "" || len(admin.AllowedRoles) > 0 {
		_, authProvider, err := cfg.SelectedIdentityProvider()
		if err != nil {
			return err
		}
		if authProvider == nil {
			return fmt.Errorf("config validation: server.admin authorization requires providers.identity to be configured")
		}
		_, authorizationProvider, err := cfg.SelectedAuthorizationProvider()
		if err != nil {
			return err
		}
		if authorizationProvider == nil {
			return fmt.Errorf("config validation: server.admin authorization requires providers.authorization to be configured")
		}
		if policy != "" && len(admin.AllowedRoles) == 0 {
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

func validateAppIndexedDBConfig(cfg *Config, name string, entry *ProviderEntry) error {
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
			return fmt.Errorf("config validation: apps.%s.idb.objectStores[%d] is required", name, i)
		}
		if _, exists := seenStores[store]; exists {
			return fmt.Errorf("config validation: apps.%s.idb.objectStores[%d] duplicates %q", name, i, store)
		}
		seenStores[store] = struct{}{}
	}
	if _, err := cfg.EffectiveAppIndexedDB(name, entry); err != nil {
		return err
	}
	return nil
}

func validateAppS3Bindings(cfg *Config, name string, entry *ProviderEntry) error {
	if entry == nil {
		return nil
	}
	seen := make(map[string]struct{}, len(entry.S3))
	for i, binding := range entry.S3 {
		binding = strings.TrimSpace(binding)
		if binding == "" {
			return fmt.Errorf("config validation: app %q s3[%d] is required", name, i)
		}
		if _, exists := seen[binding]; exists {
			return fmt.Errorf("config validation: app %q s3[%d] duplicates %q", name, i, binding)
		}
		boundEntry, ok := cfg.Providers.S3[binding]
		if !ok || boundEntry == nil {
			return fmt.Errorf("config validation: app %q s3[%d] references unknown s3 %q", name, i, binding)
		}
		seen[binding] = struct{}{}
		entry.S3[i] = binding
	}
	return nil
}

var workflowScheduleCronParser = cronv3.NewParser(
	cronv3.Minute | cronv3.Hour | cronv3.Dom | cronv3.Month | cronv3.Dow,
)

func validateWorkflowScheduleCron(path, spec string) error {
	if _, err := workflowScheduleCronParser.Parse(spec); err != nil {
		return fmt.Errorf("config validation: %s.schedule.cron %q is invalid: %w", path, spec, err)
	}
	return nil
}

func validateWorkflowsConfig(cfg *Config) error {
	if len(cfg.Workflows.Definitions) > 0 {
		normalized := make(map[string]WorkflowDefinitionConfig, len(cfg.Workflows.Definitions))
		for key := range cfg.Workflows.Definitions {
			definition := cfg.Workflows.Definitions[key]
			key = strings.TrimSpace(key)
			if key == "" {
				return fmt.Errorf("config validation: workflows.definitions keys must not be empty")
			}
			if _, exists := normalized[key]; exists {
				return fmt.Errorf("config validation: workflows.definitions duplicates %q", key)
			}
			if err := validateWorkflowDefinitionTarget(cfg, key, &definition); err != nil {
				return err
			}
			runAs, err := normalizeWorkflowRunAs("workflows.definitions."+key+".runAs", definition.RunAs)
			if err != nil {
				return err
			}
			definition.RunAs = runAs
			definition.Provider = strings.TrimSpace(definition.Provider)
			providerName, _, err := cfg.EffectiveWorkflowProvider(definition.Provider)
			if err != nil {
				return fmt.Errorf("config validation: workflows.definitions.%s.provider: %w", key, err)
			}
			if providerName == "" {
				return fmt.Errorf("config validation: workflows.definitions.%s.provider is required", key)
			}
			if err := normalizeWorkflowActivationsConfig(key, &definition); err != nil {
				return err
			}
			normalized[key] = definition
		}
		cfg.Workflows.Definitions = normalized
	}
	return nil
}

func validateWorkflowDefinitionTarget(cfg *Config, key string, definition *WorkflowDefinitionConfig) error {
	if definition == nil {
		return fmt.Errorf("config validation: workflows.definitions.%s is required", key)
	}
	stepsPath := "workflows.definitions." + key + ".steps"
	if err := normalizeWorkflowSteps(cfg, stepsPath, definition.Steps); err != nil {
		return err
	}
	return validateWorkflowStepApps(cfg, stepsPath, definition.Steps)
}

func normalizeWorkflowActivationsConfig(definitionKey string, definition *WorkflowDefinitionConfig) error {
	if len(definition.On) == 0 {
		return nil
	}
	normalized := make(map[string]WorkflowActivationConfig, len(definition.On))
	for activationID := range definition.On {
		activation := definition.On[activationID]
		activationID = strings.TrimSpace(activationID)
		if activationID == "" {
			return fmt.Errorf("config validation: workflows.definitions.%s.on keys must not be empty", definitionKey)
		}
		if _, exists := normalized[activationID]; exists {
			return fmt.Errorf("config validation: workflows.definitions.%s.on duplicates %q", definitionKey, activationID)
		}
		path := "workflows.definitions." + definitionKey + ".on." + activationID
		switch {
		case activation.Schedule != nil && activation.Event != nil:
			return fmt.Errorf("config validation: %s must set exactly one of schedule or event", path)
		case activation.Schedule != nil:
			activation.Schedule.Cron = strings.TrimSpace(activation.Schedule.Cron)
			if activation.Schedule.Cron == "" {
				return fmt.Errorf("config validation: %s.schedule.cron is required", path)
			}
			if err := validateWorkflowScheduleCron(path, activation.Schedule.Cron); err != nil {
				return err
			}
			activation.Schedule.Timezone = strings.TrimSpace(activation.Schedule.Timezone)
			if activation.Schedule.Timezone == "" {
				activation.Schedule.Timezone = "UTC"
			}
			if _, err := time.LoadLocation(activation.Schedule.Timezone); err != nil {
				return fmt.Errorf("config validation: %s.schedule.timezone %q is invalid: %w", path, activation.Schedule.Timezone, err)
			}
		case activation.Event != nil:
			activation.Event.Type = strings.TrimSpace(activation.Event.Type)
			if activation.Event.Type == "" {
				return fmt.Errorf("config validation: %s.event.type is required", path)
			}
			activation.Event.Source = strings.TrimSpace(activation.Event.Source)
			activation.Event.Subject = strings.TrimSpace(activation.Event.Subject)
		default:
			return fmt.Errorf("config validation: %s must set schedule or event", path)
		}
		if err := normalizeWorkflowValueConfig(path+".input", &activation.Input); err != nil {
			return err
		}
		if err := coreworkflow.ValidateValueRefs(path+".input", WorkflowValueToCore(activation.Input), map[string]struct{}{}); err != nil {
			return fmt.Errorf("config validation: %w", err)
		}
		normalized[activationID] = activation
	}
	definition.On = normalized
	return nil
}

func validateWorkflowStepApps(cfg *Config, path string, steps []WorkflowStepConfig) error {
	for i := range steps {
		step := &steps[i]
		stepPath := fmt.Sprintf("%s[%d]", path, i)
		if step.App != nil {
			if _, ok := cfg.Apps[step.App.Name]; !ok {
				return fmt.Errorf("config validation: %s.app.name references unknown app %q", stepPath, step.App.Name)
			}
		}
	}
	return nil
}

func normalizeWorkflowRunAs(path string, runAs string) (string, error) {
	runAs = strings.TrimSpace(runAs)
	if runAs == "" {
		return "", fmt.Errorf("config validation: %s is required", path)
	}
	return runAs, nil
}

func normalizeWorkflowSteps(cfg *Config, path string, steps []WorkflowStepConfig) error {
	if len(steps) == 0 {
		return fmt.Errorf("config validation: %s is required", path)
	}
	seen := map[string]struct{}{}
	for i := range steps {
		stepPath := fmt.Sprintf("%s[%d]", path, i)
		step := &steps[i]
		step.ID = strings.TrimSpace(step.ID)
		if step.ID == "" {
			return fmt.Errorf("config validation: %s.id is required", stepPath)
		}
		if _, exists := seen[step.ID]; exists {
			return fmt.Errorf("config validation: %s.id duplicates %q", stepPath, step.ID)
		}
		if err := normalizeWorkflowValueMapConfig(stepPath+".inputs", step.Inputs); err != nil {
			return err
		}
		if err := coreworkflow.ValidateValueMapRefs(stepPath+".inputs", workflowValueMapToCore(step.Inputs), seen); err != nil {
			return fmt.Errorf("config validation: %w", err)
		}
		if (step.App == nil) == (step.Agent == nil) {
			return fmt.Errorf("config validation: %s must set exactly one of app or agent", stepPath)
		}
		step.Timeout = strings.TrimSpace(step.Timeout)
		var parsedTimeout time.Duration
		if step.Timeout != "" {
			var err error
			parsedTimeout, err = time.ParseDuration(step.Timeout)
			if err != nil {
				return fmt.Errorf("config validation: %s.timeout %q is invalid: %w", stepPath, step.Timeout, err)
			}
		}
		if step.App != nil {
			if err := normalizeWorkflowStepAppCallConfig(stepPath+".app", step.App, true); err != nil {
				return err
			}
			if err := coreworkflow.ValidateValueRefs(stepPath+".app.input", WorkflowValueToCore(step.App.Input), seen); err != nil {
				return fmt.Errorf("config validation: %w", err)
			}
		}
		if step.Agent != nil {
			if step.Timeout != "" && parsedTimeout < 0 {
				return fmt.Errorf("config validation: %s.timeout must not be negative for agent steps", stepPath)
			}
			if err := validateWorkflowStepAgentConfig(cfg, stepPath+".agent", step.Agent, seen); err != nil {
				return err
			}
		}
		if step.When != nil {
			if err := normalizeWorkflowValueConfig(stepPath+".when.value", &step.When.Value); err != nil {
				return err
			}
			when := &coreworkflow.StepWhen{
				Value:     WorkflowValueToCore(step.When.Value),
				Equals:    step.When.Equals,
				EqualsSet: step.When.EqualsSet(),
			}
			if err := coreworkflow.ValidateStepWhen(stepPath+".when", when, seen); err != nil {
				return fmt.Errorf("config validation: %w", err)
			}
		}
		seen[step.ID] = struct{}{}
	}
	return nil
}

func normalizeWorkflowValueMapConfig(path string, values map[string]WorkflowValueConfig) error {
	for key := range values {
		value := values[key]
		if err := normalizeWorkflowValueConfig(path+"."+key, &value); err != nil {
			return err
		}
		values[key] = value
	}
	return nil
}

func normalizeWorkflowStepAppCallConfig(path string, app *WorkflowStepAppCallConfig, allowCredentialMode bool) error {
	if app == nil {
		return fmt.Errorf("config validation: %s is required", path)
	}
	app.Name = strings.TrimSpace(app.Name)
	app.Operation = strings.TrimSpace(app.Operation)
	app.Connection = strings.TrimSpace(app.Connection)
	app.Instance = strings.TrimSpace(app.Instance)
	app.CredentialMode = providermanifestv1.NormalizeOptionalConnectionMode(app.CredentialMode)
	if app.Name == "" {
		return fmt.Errorf("config validation: %s.name is required", path)
	}
	if app.Operation == "" {
		return fmt.Errorf("config validation: %s.operation is required", path)
	}
	switch app.CredentialMode {
	case "":
	case providermanifestv1.ConnectionModeNone, providermanifestv1.ConnectionModeSubject:
		if !allowCredentialMode {
			return fmt.Errorf("config validation: %s.credentialMode is not supported", path)
		}
	default:
		return fmt.Errorf("config validation: %s.credentialMode %q is not supported", path, app.CredentialMode)
	}
	if err := normalizeWorkflowValueConfig(path+".input", &app.Input); err != nil {
		return err
	}
	return nil
}

func validateWorkflowStepAgentConfig(cfg *Config, path string, agent *WorkflowStepAgentConfig, previousSteps map[string]struct{}) error {
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
	agent.SessionKey = strings.TrimSpace(agent.SessionKey)
	agent.Prompt.Template = strings.TrimSpace(agent.Prompt.Template)
	if err := coreworkflow.ValidateTemplateRefs(path+".prompt", agent.Prompt.Template, previousSteps); err != nil {
		return fmt.Errorf("config validation: %w", err)
	}
	for i := range agent.Messages {
		agent.Messages[i].Role = strings.TrimSpace(agent.Messages[i].Role)
		agent.Messages[i].Text.Template = strings.TrimSpace(agent.Messages[i].Text.Template)
		if err := coreworkflow.ValidateTemplateRefs(fmt.Sprintf("%s.messages[%d].text", path, i), agent.Messages[i].Text.Template, previousSteps); err != nil {
			return fmt.Errorf("config validation: %w", err)
		}
	}
	if agent.Prompt.Template == "" && len(agent.Messages) == 0 {
		return fmt.Errorf("config validation: %s.prompt or messages is required", path)
	}
	if err := validateWorkflowAgentToolsConfig(cfg, path+".tools", agent.Tools); err != nil {
		return err
	}
	if err := validateWorkflowAgentOutputConfig(path+".output", agent.Output); err != nil {
		return err
	}
	return nil
}

func validateWorkflowAgentOutputConfig(path string, output *WorkflowAgentOutputConfig) error {
	if output == nil {
		return fmt.Errorf("config validation: %s is required", path)
	}
	textSet := output.Text != nil
	structuredSet := output.Structured != nil
	if textSet == structuredSet {
		return fmt.Errorf("config validation: exactly one of %s.text or %s.structured is required", path, path)
	}
	if output.Structured == nil {
		return nil
	}
	schema := output.Structured.Schema
	if len(schema) == 0 {
		return fmt.Errorf("config validation: %s.structured.schema must be a non-empty JSON schema object with type %q", path, "object")
	}
	rawType, ok := schema["type"]
	if !ok {
		return fmt.Errorf("config validation: %s.structured.schema.type must be %q", path, "object")
	}
	typeValue, ok := rawType.(string)
	if !ok || strings.TrimSpace(typeValue) != "object" {
		return fmt.Errorf("config validation: %s.structured.schema.type must be %q", path, "object")
	}
	return nil
}

func validateWorkflowAgentToolsConfig(cfg *Config, path string, tools []WorkflowAgentToolRef) error {
	hasSystemTool := false
	for i := range tools {
		if strings.TrimSpace(tools[i].System) != "" {
			hasSystemTool = true
			break
		}
	}
	for i := range tools {
		tool := &tools[i]
		tool.System = strings.TrimSpace(tool.System)
		tool.App = strings.TrimSpace(tool.App)
		if tool.System == "" && tool.App == "" {
			return fmt.Errorf("config validation: %s[%d].app or system is required", path, i)
		}
		if tool.System != "" && tool.App != "" {
			return fmt.Errorf("config validation: %s[%d] must set exactly one of app or system", path, i)
		}
		tool.Operation = strings.TrimSpace(tool.Operation)
		tool.Connection = strings.TrimSpace(tool.Connection)
		tool.Instance = strings.TrimSpace(tool.Instance)
		tool.CredentialMode = string(core.NormalizeOptionalConnectionMode(core.ConnectionMode(tool.CredentialMode)))
		switch core.ConnectionMode(tool.CredentialMode) {
		case "", core.ConnectionModeNone, core.ConnectionModeSubject:
		default:
			return fmt.Errorf("config validation: %s[%d].credentialMode %q is not supported", path, i, tool.CredentialMode)
		}
		tool.Title = strings.TrimSpace(tool.Title)
		tool.Description = strings.TrimSpace(tool.Description)
		if tool.System != "" {
			if tool.System != "workflow" {
				return fmt.Errorf("config validation: %s[%d].system references unknown system %q", path, i, tool.System)
			}
			if tool.Operation == "" {
				return fmt.Errorf("config validation: %s[%d].operation is required for system tool refs", path, i)
			}
			if tool.Connection != "" || tool.Instance != "" || tool.CredentialMode != "" {
				return fmt.Errorf("config validation: %s[%d] system refs cannot include connection, instance, or credentialMode", path, i)
			}
			continue
		}
		if _, ok := cfg.Apps[tool.App]; !ok {
			return fmt.Errorf("config validation: %s[%d].app references unknown app %q", path, i, tool.App)
		}
		if hasSystemTool && tool.Operation == "" {
			return fmt.Errorf("config validation: %s[%d].operation is required when workflow system tools are granted", path, i)
		}
	}
	return nil
}

func normalizeWorkflowValueConfig(path string, value *WorkflowValueConfig) error {
	if value == nil {
		return nil
	}
	set := 0
	if value.LiteralSet {
		set++
	}
	if value.Object != nil {
		set++
		for key := range value.Object {
			nested := value.Object[key]
			if err := normalizeWorkflowValueConfig(path+"."+key, &nested); err != nil {
				return err
			}
			value.Object[key] = nested
		}
	}
	if value.Array != nil {
		set++
		for i := range value.Array {
			if err := normalizeWorkflowValueConfig(fmt.Sprintf("%s[%d]", path, i), &value.Array[i]); err != nil {
				return err
			}
		}
	}
	if value.Template != nil {
		value.Template.Template = strings.TrimSpace(value.Template.Template)
		if value.Template.Template != "" {
			set++
		}
	}
	if strings.TrimSpace(value.Input) != "" {
		value.Input = strings.TrimSpace(value.Input)
		set++
	}
	if strings.TrimSpace(value.Signal) != "" {
		value.Signal = strings.TrimSpace(value.Signal)
		set++
	}
	if value.StepOutput != nil {
		value.StepOutput.StepID = strings.TrimSpace(value.StepOutput.StepID)
		value.StepOutput.Path = strings.TrimSpace(value.StepOutput.Path)
		set++
	}
	if value.StepInput != nil {
		value.StepInput.StepID = strings.TrimSpace(value.StepInput.StepID)
		value.StepInput.Path = strings.TrimSpace(value.StepInput.Path)
		set++
	}
	if set > 1 {
		return fmt.Errorf("config validation: %s must set exactly one value kind", path)
	}
	return nil
}

func validateAppCacheBindings(cfg *Config, name string, entry *ProviderEntry) error {
	if entry == nil {
		return nil
	}
	seen := make(map[string]struct{}, len(entry.Cache))
	for i, binding := range entry.Cache {
		binding = strings.TrimSpace(binding)
		if binding == "" {
			return fmt.Errorf("config validation: app %q cache[%d] is required", name, i)
		}
		if _, exists := seen[binding]; exists {
			return fmt.Errorf("config validation: app %q cache[%d] duplicates %q", name, i, binding)
		}
		boundEntry, ok := cfg.Providers.Cache[binding]
		if !ok || boundEntry == nil {
			return fmt.Errorf("config validation: app %q cache[%d] references unknown cache %q", name, i, binding)
		}
		entry.Cache[i] = binding
		seen[binding] = struct{}{}
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

func normalizeAppStaticMounts(cfg *Config) error {
	if cfg == nil || len(cfg.Apps) == 0 {
		return nil
	}
	for name, entry := range cfg.Apps {
		if entry == nil || entry.Static == nil {
			continue
		}
		mount := strings.TrimSpace(entry.Static.Mount)
		if mount == "" {
			entry.Static.Mount = "/" + name
			continue
		}
		normalized, err := normalizeMountedUIPath(mount)
		if err != nil {
			return fmt.Errorf("config validation: apps.%s.static.mount: %w", name, err)
		}
		entry.Static.Mount = normalized
	}
	return nil
}

func validateStaticMountCollisions(cfg *Config) error {
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
		label string
		path  string
	}
	var subjects []mountedPathSubject
	for _, name := range slices.Sorted(maps.Keys(cfg.Apps)) {
		entry := cfg.Apps[name]
		if entry == nil || entry.Static == nil || strings.TrimSpace(entry.Static.Mount) == "" {
			continue
		}
		subjects = append(subjects, mountedPathSubject{
			label: "apps." + name + ".static.mount",
			path:  entry.Static.Mount,
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
			if mountedUIPathsConflict(subject.path, other.path) {
				return fmt.Errorf("config validation: %s %q conflicts with %s %q", subject.label, subject.path, other.label, other.path)
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

func validateExecutableConnectionAuthSupport(name string, plan StaticConnectionPlan) error {
	_, supportsMCPOAuth := plan.ResolvedSurface(SpecSurfaceMCP)
	if conn := plan.AppConnection(); conn.Auth.Type == providermanifestv1.AuthTypeMCPOAuth && !supportsMCPOAuth {
		return fmt.Errorf("config validation: integration %q app auth type %q requires an MCP surface", name, providermanifestv1.AuthTypeMCPOAuth)
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

func validateAppIntegrationConnections(name string, entry *ProviderEntry) error {
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
	if err := validateConnectionAuthMappings(name, plan.AppConnection().Auth, "app"); err != nil {
		return err
	}
	if err := validateCredentialRefresh(fmt.Sprintf("integration %q app connection", name), plan.AppConnection()); err != nil {
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
