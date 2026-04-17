package operator

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/plugininvocation"
	"github.com/valon-technologies/gestalt/server/internal/pluginstore"
	"github.com/valon-technologies/gestalt/server/internal/providerpkg"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"gopkg.in/yaml.v3"
)

const (
	InitLockfileName         = "gestalt.lock.json"
	PreparedProvidersDir     = ".gestaltd/providers"
	PreparedAuthDir          = ".gestaltd/auth"
	PreparedAuthorizationDir = ".gestaltd/authorization"
	PreparedSecretsDir       = ".gestaltd/secrets"
	PreparedTelemetryDir     = ".gestaltd/telemetry"
	PreparedAuditDir         = ".gestaltd/audit"
	PreparedCacheDir         = ".gestaltd/cache"
	PreparedWorkflowDir      = ".gestaltd/workflow"
	PreparedUIDir            = ".gestaltd/ui"
	LockVersion              = 8

	platformKeyGeneric = "generic"
)

type Lockfile struct {
	Version       int                          `json:"version"`
	Providers     map[string]LockProviderEntry `json:"providers"`
	Auth          map[string]LockEntry         `json:"auth,omitempty"`
	Authorization map[string]LockEntry         `json:"authorization,omitempty"`
	IndexedDBs    map[string]LockEntry         `json:"indexeddbs,omitempty"`
	Caches        map[string]LockEntry         `json:"cache,omitempty"`
	S3            map[string]LockEntry         `json:"s3,omitempty"`
	Workflows     map[string]LockEntry         `json:"workflow,omitempty"`
	Secrets       map[string]LockEntry         `json:"secrets,omitempty"`
	Telemetry     map[string]LockEntry         `json:"telemetry,omitempty"`
	Audit         map[string]LockEntry         `json:"audit,omitempty"`
	UIs           map[string]LockUIEntry       `json:"ui,omitempty"`
}

// LockArchive records a platform-specific archive URL and optional integrity hash.
type LockArchive struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256,omitempty"`
}

type LockEntry struct {
	Fingerprint string                 `json:"fingerprint"`
	Package     string                 `json:"package,omitempty"`
	Kind        string                 `json:"kind,omitempty"`
	Runtime     string                 `json:"runtime,omitempty"`
	Source      string                 `json:"source,omitempty"`
	Version     string                 `json:"version,omitempty"`
	Archives    map[string]LockArchive `json:"archives,omitempty"`
	Manifest    string                 `json:"manifest"`
	Executable  string                 `json:"executable,omitempty"`
	AssetRoot   string                 `json:"assetRoot,omitempty"`
}

type LockProviderEntry = LockEntry
type LockUIEntry = LockEntry

type Lifecycle struct {
	configSecretResolver func(context.Context, *config.Config) error
	httpClient           *http.Client
}

type StatePaths struct {
	ArtifactsDir string
	LockfilePath string
}

func NewLifecycle() *Lifecycle {
	return &Lifecycle{}
}

// WithConfigSecretResolver installs a resolver that may mutate cfg in place and
// must leave it in canonicalized form for subsequent structural validation.
func (l *Lifecycle) WithConfigSecretResolver(resolve func(context.Context, *config.Config) error) *Lifecycle {
	l.configSecretResolver = resolve
	return l
}

func (l *Lifecycle) WithHTTPClient(client *http.Client) *Lifecycle {
	l.httpClient = client
	return l
}

func (l *Lifecycle) metadataHTTPClient() *http.Client {
	if l != nil && l.httpClient != nil {
		return l.httpClient
	}
	return http.DefaultClient
}

func (l *Lifecycle) InitAtPath(configPath string) (*Lockfile, error) {
	return l.InitAtPaths([]string{configPath})
}

func (l *Lifecycle) InitAtPaths(configPaths []string) (*Lockfile, error) {
	return l.InitAtPathsWithStatePaths(configPaths, StatePaths{})
}

func (l *Lifecycle) InitAtPathsWithStatePaths(configPaths []string, state StatePaths) (*Lockfile, error) {
	lock, _, _, err := l.initAtPaths(configPaths, state)
	return lock, err
}

// InitAtPathWithPlatforms runs init and additionally downloads and hashes
// archives for the specified extra platforms.
func (l *Lifecycle) InitAtPathWithPlatforms(configPath, artifactsDir string, platforms []struct{ GOOS, GOARCH, LibC string }) (*Lockfile, error) {
	return l.InitAtPathsWithPlatforms([]string{configPath}, StatePaths{ArtifactsDir: artifactsDir}, platforms)
}

// InitAtPathsWithPlatforms runs init and additionally downloads and hashes
// archives for the specified extra platforms.
func (l *Lifecycle) InitAtPathsWithPlatforms(configPaths []string, state StatePaths, platforms []struct{ GOOS, GOARCH, LibC string }) (*Lockfile, error) {
	lock, cfg, paths, err := l.initAtPaths(configPaths, state)
	if err != nil {
		return nil, err
	}
	if len(platforms) == 0 {
		return lock, nil
	}

	tokenForSource := buildSourceTokenMap(cfg)
	if err := l.downloadPlatformArchives(context.Background(), lock, paths, platforms, tokenForSource); err != nil {
		return nil, err
	}

	if err := WriteLockfile(paths.lockfilePath, lock); err != nil {
		return nil, err
	}
	return lock, nil
}

func (l *Lifecycle) initAtPaths(configPaths []string, state StatePaths) (*Lockfile, *config.Config, initPaths, error) {
	cfg, err := config.LoadAllowMissingEnvPaths(configPaths)
	if err != nil {
		return nil, nil, initPaths{}, fmt.Errorf("loading config: %v", err)
	}
	if err := config.OverlayRemotePluginConfigPaths(configPaths, cfg); err != nil {
		return nil, nil, initPaths{}, fmt.Errorf("loading config: %v", err)
	}
	paths := resolveInitPaths(configPaths, cfg, state)
	lock, err := l.prepareRuntimeLockFromLoadedConfig(context.Background(), paths, cfg)
	if err != nil {
		return nil, nil, initPaths{}, err
	}
	if err := l.applyPreparedProviders(paths, lock, cfg, true); err != nil {
		return nil, nil, initPaths{}, err
	}
	if err := config.ValidateResolvedStructure(cfg); err != nil {
		return nil, nil, initPaths{}, err
	}
	if err := plugininvocation.ValidateDependencies(context.Background(), cfg); err != nil {
		return nil, nil, initPaths{}, err
	}
	if err := WriteLockfile(paths.lockfilePath, lock); err != nil {
		return nil, nil, initPaths{}, err
	}

	slog.Info("prepared locked artifacts", "providers", len(lock.Providers), "auth", len(lock.Auth), "authorization", len(lock.Authorization), "indexeddbs", len(lock.IndexedDBs), "cache", len(lock.Caches), "s3", len(lock.S3), "workflow", len(lock.Workflows), "secrets", len(lock.Secrets), "telemetry", len(lock.Telemetry), "audit", len(lock.Audit), "uis", len(lock.UIs))
	slog.Info("wrote lockfile", "path", paths.lockfilePath)
	return lock, cfg, paths, nil
}

func (l *Lifecycle) prepareRuntimeLockFromLoadedConfig(ctx context.Context, paths initPaths, cfg *config.Config) (*Lockfile, error) {
	secretsEntries, err := l.primeSecretsProviderForConfigResolution(ctx, paths, cfg, nil)
	if err != nil {
		return nil, err
	}
	if err := l.resolveConfigSecrets(ctx, cfg); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(paths.providersDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating providers dir: %w", err)
	}

	lock := newLockfile()
	resolvedProviders, err := l.writeProviderArtifacts(ctx, cfg, paths)
	if err != nil {
		return nil, err
	}
	for name := range resolvedProviders {
		lock.Providers[name] = resolvedProviders[name]
	}
	for _, collection := range hostProviderCollections(cfg) {
		for name, entry := range collection.entries {
			if entry == nil || !sourceBacked(entry) {
				continue
			}
			if collection.kind == config.HostProviderKindSecrets {
				if _, alreadyPrepared := secretsEntries[name]; alreadyPrepared {
					continue
				}
			}
			destDir := componentDestDir(paths, collection.kind, name)
			lockEntry, err := l.writeComponentArtifact(ctx, paths, providerManifestKind(collection.kind), name, destDir, entry, entry.Config)
			if err != nil {
				return nil, err
			}
			lockEntriesForKind(lock, collection.kind)[name] = lockEntry
		}
	}
	if err := l.resolveConfiguredPlugins(paths, lock, cfg, true); err != nil {
		return nil, err
	}
	existingUIEntries := make(map[string]struct{}, len(cfg.Providers.UI))
	for name := range cfg.Providers.UI {
		existingUIEntries[name] = struct{}{}
	}
	if err := synthesizePluginOwnedUIEntries(cfg); err != nil {
		return nil, err
	}
	for name := range secretsEntries {
		lock.Secrets[name] = secretsEntries[name]
	}
	for name, def := range cfg.Providers.IndexedDB {
		if sourceBacked(def) {
			entry, err := l.writeComponentArtifact(ctx, paths, providermanifestv1.KindIndexedDB, name, indexeddbDestDir(paths, name), def, def.Config)
			if err != nil {
				return nil, err
			}
			lock.IndexedDBs[name] = entry
		}
	}
	for name, def := range cfg.Providers.S3 {
		if sourceBacked(def) {
			entry, err := l.writeComponentArtifact(ctx, paths, providermanifestv1.KindS3, name, s3DestDir(paths, name), def, def.Config)
			if err != nil {
				return nil, err
			}
			lock.S3[name] = entry
		}
	}
	for name, entry := range cfg.Providers.UI {
		if entry != nil && sourceBacked(&entry.ProviderEntry) {
			if _, existed := existingUIEntries[name]; !existed && entry.HasLocalSource() {
				if plugin := cfg.Plugins[name]; plugin != nil && strings.TrimSpace(plugin.UI) == "" && strings.TrimSpace(plugin.MountPath) != "" {
					continue
				}
			}
			uiEntry, err := l.writeNamedUIProviderArtifact(ctx, paths, name, &entry.ProviderEntry, uiDestDir(paths, name), "ui "+strconv.Quote(name))
			if err != nil {
				return nil, err
			}
			lock.UIs[name] = uiEntry
		}
	}
	return lock, nil
}

func buildSourceTokenMap(cfg *config.Config) map[string]string {
	tokens := make(map[string]string)
	addEntry := func(entry *config.ProviderEntry) {
		if entry == nil || entry.Source.Auth == nil {
			return
		}
		location := strings.TrimSpace(entry.SourceRemoteLocation())
		if location == "" {
			return
		}
		tokens[location] = entry.Source.Auth.Token
	}
	for _, entry := range cfg.Plugins {
		addEntry(entry)
	}
	for _, collection := range hostProviderCollections(cfg) {
		for _, entry := range collection.entries {
			addEntry(entry)
		}
	}
	for _, def := range cfg.Providers.IndexedDB {
		addEntry(def)
	}
	for _, def := range cfg.Providers.S3 {
		addEntry(def)
	}
	for _, entry := range cfg.Providers.UI {
		addEntry(&entry.ProviderEntry)
	}
	return tokens
}

func defaultLockfilePath(configPath string) string {
	dir := filepath.Dir(configPath)
	if !filepath.IsAbs(dir) {
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs
		}
	}
	return filepath.Join(dir, InitLockfileName)
}

func (l *Lifecycle) LoadForExecutionAtPath(configPath string, locked bool) (*config.Config, map[string]string, error) {
	return l.LoadForExecutionAtPaths([]string{configPath}, locked)
}

func (l *Lifecycle) LoadForExecutionAtPaths(configPaths []string, locked bool) (*config.Config, map[string]string, error) {
	return l.LoadForExecutionAtPathsWithStatePaths(configPaths, StatePaths{}, locked)
}

func (l *Lifecycle) LoadForExecutionAtPathsWithStatePaths(configPaths []string, state StatePaths, locked bool) (*config.Config, map[string]string, error) {
	cfg, err := config.LoadPaths(configPaths)
	if err != nil {
		return nil, nil, fmt.Errorf("loading config: %v", err)
	}
	paths := resolveInitPaths(configPaths, cfg, state)
	secretsLock, secretsValidated, err := l.lockForSecretsBootstrap(configPaths, state, paths, cfg, locked)
	if err != nil {
		return nil, nil, err
	}
	if _, err := l.primeSecretsProviderForConfigResolution(context.Background(), paths, cfg, secretsLock); err != nil {
		return nil, nil, err
	}
	if err := l.resolveConfigSecrets(context.Background(), cfg); err != nil {
		return nil, nil, err
	}
	if err := config.ValidateRuntime(cfg); err != nil {
		return nil, nil, err
	}

	dependenciesValidated, err := l.applyLockedProviders(configPaths, state, cfg, locked, secretsLock)
	if err != nil {
		return nil, nil, err
	}
	if err := config.ValidateResolvedStructure(cfg); err != nil {
		return nil, nil, err
	}
	if !secretsValidated && !dependenciesValidated {
		if err := plugininvocation.ValidateDependencies(context.Background(), cfg); err != nil {
			return nil, nil, err
		}
	}
	return cfg, nil, nil
}

func (l *Lifecycle) LoadForValidationAtPathsWithStatePaths(configPaths []string, state StatePaths) (*config.Config, error) {
	cfg, err := config.LoadPaths(configPaths)
	if err != nil {
		return nil, fmt.Errorf("loading config: %v", err)
	}
	paths := resolveInitPaths(configPaths, cfg, state)
	lock, err := l.prepareRuntimeLockFromLoadedConfig(context.Background(), paths, cfg)
	if err != nil {
		return nil, err
	}
	if err := config.ValidateRuntime(cfg); err != nil {
		return nil, err
	}
	if err := l.applyPreparedProviders(paths, lock, cfg, true); err != nil {
		return nil, err
	}
	if err := config.ValidateResolvedStructure(cfg); err != nil {
		return nil, err
	}
	if err := plugininvocation.ValidateDependencies(context.Background(), cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (l *Lifecycle) resolveConfigSecrets(ctx context.Context, cfg *config.Config) error {
	if l.configSecretResolver == nil {
		return nil
	}
	if err := l.configSecretResolver(ctx, cfg); err != nil {
		return fmt.Errorf("resolving config secrets: %w", err)
	}
	return config.ValidateCanonicalStructure(cfg)
}

func referencedConfigSecretsProviders(cfg *config.Config) (map[string]*config.ProviderEntry, error) {
	referenced, err := config.ReferencedConfigSecretProviders(cfg)
	if err != nil {
		return nil, err
	}
	if len(referenced) == 0 {
		return nil, nil
	}
	entries := make(map[string]*config.ProviderEntry, len(referenced))
	for name := range referenced {
		entries[name] = cfg.Providers.Secrets[name]
	}
	return entries, nil
}

func secretsProviderMetadataDependencies(name string, provider *config.ProviderEntry) (map[string]struct{}, error) {
	if provider == nil {
		return nil, nil
	}
	tmp := &config.Config{
		Providers: config.ProvidersConfig{
			Secrets: map[string]*config.ProviderEntry{
				name: provider,
			},
		},
	}
	deps, err := config.ReferencedConfigSecretProviders(tmp)
	if err != nil {
		return nil, err
	}
	delete(deps, name)
	if len(deps) == 0 {
		return nil, nil
	}
	return deps, nil
}

func (l *Lifecycle) resolveSecretsProviderMetadata(ctx context.Context, name string, provider *config.ProviderEntry, available map[string]*config.ProviderEntry) error {
	if l.configSecretResolver == nil || provider == nil {
		return nil
	}
	secrets := make(map[string]*config.ProviderEntry, len(available)+1)
	for availableName, entry := range available {
		secrets[availableName] = entry
	}
	secrets[name] = provider
	tmp := &config.Config{
		Providers: config.ProvidersConfig{
			Secrets: secrets,
		},
	}
	if err := l.configSecretResolver(ctx, tmp); err != nil {
		return fmt.Errorf("resolve metadata for %s %q: %w", providermanifestv1.KindSecrets, name, err)
	}
	return nil
}

func (l *Lifecycle) lockForSecretsBootstrap(configPaths []string, state StatePaths, paths initPaths, cfg *config.Config, locked bool) (*Lockfile, bool, error) {
	if cfg == nil {
		return nil, false, nil
	}
	referenced, err := referencedConfigSecretsProviders(cfg)
	if err != nil {
		return nil, false, err
	}
	needsPreparedSecrets := false
	for _, provider := range referenced {
		if sourceBacked(provider) {
			needsPreparedSecrets = true
			break
		}
	}
	if !needsPreparedSecrets {
		return nil, false, nil
	}
	if !configHasProviderLoading(cfg) {
		return nil, false, nil
	}

	lock, err := ReadLockfile(paths.lockfilePath)
	validatedDuringInit := false
	if !locked && (err != nil || !lockMatchesConfig(cfg, paths, lock) || configHasLocalProviderSources(cfg) || configHasMetadataProviderSources(cfg)) {
		lock, err = l.InitAtPathsWithStatePaths(configPaths, state)
		validatedDuringInit = err == nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("source-backed providers require prepared artifacts; run `gestaltd init %s`: %w", paths.configFlags, err)
	}
	return lock, validatedDuringInit, nil
}

func (l *Lifecycle) primeSecretsProviderForConfigResolution(ctx context.Context, paths initPaths, cfg *config.Config, lock *Lockfile) (map[string]LockEntry, error) {
	if cfg == nil {
		return nil, nil
	}
	referenced, err := referencedConfigSecretsProviders(cfg)
	if err != nil {
		return nil, err
	}
	available := make(map[string]*config.ProviderEntry, len(referenced))
	pending := make(map[string]*config.ProviderEntry, len(referenced))
	dependencies := make(map[string]map[string]struct{}, len(referenced))
	for name, provider := range referenced {
		if provider == nil {
			continue
		}
		deps, err := secretsProviderMetadataDependencies(name, provider)
		if err != nil {
			return nil, err
		}
		dependencies[name] = deps
		if provider.Source.IsBuiltin() {
			available[name] = provider
			continue
		}
		pending[name] = provider
	}
	prepared := make(map[string]LockEntry)
	for len(pending) > 0 {
		progress := false
		names := make([]string, 0, len(pending))
		for name := range pending {
			names = append(names, name)
		}
		slices.Sort(names)
		for _, name := range names {
			provider := pending[name]
			ready := true
			for dep := range dependencies[name] {
				depProvider := referenced[dep]
				if depProvider == nil {
					return nil, fmt.Errorf("config validation: secret refs reference unknown secrets provider %q", dep)
				}
				if _, ok := available[dep]; !ok {
					ready = false
					break
				}
			}
			if !ready {
				continue
			}
			if err := l.resolveSecretsProviderMetadata(ctx, name, provider, available); err != nil {
				return nil, err
			}
			configMap, err := config.NodeToMap(provider.Config)
			if err != nil {
				return nil, fmt.Errorf("decode provider config for %s %q: %w", providermanifestv1.KindSecrets, name, err)
			}

			if sourceBacked(provider) {
				if lock != nil {
					lockEntry, ok := lock.Secrets[name]
					if !ok {
						return nil, fmt.Errorf("prepared artifact for %s %q is missing or stale; run `gestaltd init %s`", providermanifestv1.KindSecrets, name, paths.configFlags)
					}
					if err := l.applyLockedComponentEntry(paths, &lockEntry, providermanifestv1.KindSecrets, name, provider, configMap, secretsDestDir(paths, name), false); err != nil {
						return nil, err
					}
				} else {
					entry, err := l.writeComponentArtifact(ctx, paths, providermanifestv1.KindSecrets, name, secretsDestDir(paths, name), provider, provider.Config)
					if err != nil {
						return nil, err
					}
					if err := l.applyLockedComponentEntry(paths, &entry, providermanifestv1.KindSecrets, name, provider, configMap, secretsDestDir(paths, name), false); err != nil {
						return nil, err
					}
					prepared[name] = entry
				}
			}

			available[name] = provider
			delete(pending, name)
			progress = true
		}
		if progress {
			continue
		}
		blocked := make([]string, 0, len(pending))
		for _, name := range names {
			deps := make([]string, 0, len(dependencies[name]))
			for dep := range dependencies[name] {
				if _, ok := available[dep]; !ok {
					deps = append(deps, dep)
				}
			}
			slices.Sort(deps)
			if len(deps) == 0 {
				blocked = append(blocked, name)
				continue
			}
			blocked = append(blocked, fmt.Sprintf("%s -> %s", name, strings.Join(deps, ", ")))
		}
		return nil, fmt.Errorf("bootstrap %s providers for config resolution: unresolved dependencies: %s", providermanifestv1.KindSecrets, strings.Join(blocked, "; "))
	}
	if len(prepared) == 0 {
		return nil, nil
	}
	return prepared, nil
}

type initPaths struct {
	configPaths      []string
	configFlags      string
	configPath       string
	configDir        string
	artifactsDir     string
	lockfilePath     string
	providersDir     string
	authDir          string
	authorizationDir string
	secretsDir       string
	telemetryDir     string
	auditDir         string
	cacheDir         string
	workflowDir      string
	uiDir            string
}

func primaryConfigPath(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	return paths[0]
}

func formatInitFlags(paths []string, state StatePaths) string {
	if len(paths) == 0 {
		return ""
	}
	args := make([]string, 0, len(paths)*2+4)
	for _, path := range paths {
		args = append(args, "--config", path)
	}
	if state.ArtifactsDir != "" {
		args = append(args, "--artifacts-dir", state.ArtifactsDir)
	}
	if state.LockfilePath != "" {
		args = append(args, "--lockfile", state.LockfilePath)
	}
	return strings.Join(args, " ")
}

func formatPlatformInitFlags(paths initPaths, platform string) string {
	args := strings.TrimSpace(paths.configFlags)
	if platform == "" {
		return args
	}
	if args == "" {
		return "--platform " + platform
	}
	return args + " --platform " + platform
}

type providerFingerprintInput struct {
	Name   string `json:"name"`
	Source string `json:"source,omitempty"`
	Path   string `json:"path,omitempty"`
	Digest string `json:"digest,omitempty"`
}

func sourceBacked(entry *config.ProviderEntry) bool {
	return entry != nil && (entry.HasRemoteSource() || entry.HasLocalSource())
}

func hostProviderCollections(cfg *config.Config) []struct {
	kind    config.HostProviderKind
	entries map[string]*config.ProviderEntry
} {
	return []struct {
		kind    config.HostProviderKind
		entries map[string]*config.ProviderEntry
	}{
		{config.HostProviderKindAuth, cfg.Providers.Auth},
		{config.HostProviderKindAuthorization, cfg.Providers.Authorization},
		{config.HostProviderKindSecrets, cfg.Providers.Secrets},
		{config.HostProviderKindTelemetry, cfg.Providers.Telemetry},
		{config.HostProviderKindAudit, cfg.Providers.Audit},
		{config.HostProviderKindCache, cfg.Providers.Cache},
		{config.HostProviderKindWorkflow, cfg.Providers.Workflow},
	}
}

func lockEntriesForKind(lock *Lockfile, kind config.HostProviderKind) map[string]LockEntry {
	if lock == nil {
		return nil
	}
	switch kind {
	case config.HostProviderKindAuth:
		return lock.Auth
	case config.HostProviderKindAuthorization:
		return lock.Authorization
	case config.HostProviderKindSecrets:
		return lock.Secrets
	case config.HostProviderKindTelemetry:
		return lock.Telemetry
	case config.HostProviderKindAudit:
		return lock.Audit
	case config.HostProviderKindCache:
		return lock.Caches
	case config.HostProviderKindIndexedDB:
		return lock.IndexedDBs
	case config.HostProviderKindWorkflow:
		return lock.Workflows
	default:
		return nil
	}
}

func configHasProviderLoading(cfg *config.Config) bool {
	for _, entry := range cfg.Plugins {
		if sourceBacked(entry) {
			return true
		}
	}
	for _, collection := range hostProviderCollections(cfg) {
		for _, entry := range collection.entries {
			if sourceBacked(entry) {
				return true
			}
		}
	}
	for _, entry := range cfg.Providers.UI {
		if entry != nil && sourceBacked(&entry.ProviderEntry) {
			return true
		}
	}
	for _, def := range cfg.Providers.IndexedDB {
		if sourceBacked(def) {
			return true
		}
	}
	for _, def := range cfg.Providers.S3 {
		if sourceBacked(def) {
			return true
		}
	}
	return false
}

func configHasLocalProviderSources(cfg *config.Config) bool {
	for _, entry := range cfg.Plugins {
		if entry.HasLocalSource() {
			return true
		}
	}
	for _, collection := range hostProviderCollections(cfg) {
		for _, entry := range collection.entries {
			if entry != nil && entry.HasLocalSource() {
				return true
			}
		}
	}
	for _, entry := range cfg.Providers.UI {
		if entry != nil && entry.HasLocalSource() {
			return true
		}
	}
	for _, def := range cfg.Providers.IndexedDB {
		if def != nil && def.HasLocalSource() {
			return true
		}
	}
	for _, def := range cfg.Providers.S3 {
		if def != nil && def.HasLocalSource() {
			return true
		}
	}
	return false
}

func configHasMetadataProviderSources(cfg *config.Config) bool {
	for _, entry := range cfg.Plugins {
		if entry != nil && entry.HasMetadataSource() {
			return true
		}
	}
	for _, collection := range hostProviderCollections(cfg) {
		for _, entry := range collection.entries {
			if entry != nil && entry.HasMetadataSource() {
				return true
			}
		}
	}
	for _, entry := range cfg.Providers.UI {
		if entry != nil && entry.HasMetadataSource() {
			return true
		}
	}
	for _, def := range cfg.Providers.IndexedDB {
		if def != nil && def.HasMetadataSource() {
			return true
		}
	}
	for _, def := range cfg.Providers.S3 {
		if def != nil && def.HasMetadataSource() {
			return true
		}
	}
	return false
}

func resolveLockPath(baseDir, provider string) string {
	if filepath.IsAbs(provider) {
		return provider
	}
	return filepath.Join(baseDir, filepath.FromSlash(provider))
}

func resolveArtifactsDir(configPath string, cfg *config.Config, override string) string {
	dir := override
	if dir == "" && cfg != nil {
		dir = cfg.Server.ArtifactsDir
	}
	if dir == "" {
		return filepath.Dir(configPath)
	}
	if filepath.IsAbs(dir) {
		return dir
	}
	return filepath.Join(filepath.Dir(configPath), dir)
}

func resolveLockfilePath(configPath, override string) string {
	if override == "" {
		return defaultLockfilePath(configPath)
	}
	if filepath.IsAbs(override) {
		return override
	}
	if abs, err := filepath.Abs(override); err == nil {
		return abs
	}
	return override
}

func resolveInitPaths(configPaths []string, cfg *config.Config, state StatePaths) initPaths {
	configPath := primaryConfigPath(configPaths)
	configDir := filepath.Dir(configPath)
	artifactsDir := resolveArtifactsDir(configPath, cfg, state.ArtifactsDir)
	lockfilePath := resolveLockfilePath(configPath, state.LockfilePath)
	return initPaths{
		configPaths:      append([]string(nil), configPaths...),
		configFlags:      formatInitFlags(configPaths, state),
		configPath:       configPath,
		configDir:        configDir,
		artifactsDir:     artifactsDir,
		lockfilePath:     lockfilePath,
		providersDir:     filepath.Join(artifactsDir, filepath.FromSlash(PreparedProvidersDir)),
		authDir:          filepath.Join(artifactsDir, filepath.FromSlash(PreparedAuthDir)),
		authorizationDir: filepath.Join(artifactsDir, filepath.FromSlash(PreparedAuthorizationDir)),
		secretsDir:       filepath.Join(artifactsDir, filepath.FromSlash(PreparedSecretsDir)),
		telemetryDir:     filepath.Join(artifactsDir, filepath.FromSlash(PreparedTelemetryDir)),
		auditDir:         filepath.Join(artifactsDir, filepath.FromSlash(PreparedAuditDir)),
		cacheDir:         filepath.Join(artifactsDir, filepath.FromSlash(PreparedCacheDir)),
		workflowDir:      filepath.Join(artifactsDir, filepath.FromSlash(PreparedWorkflowDir)),
		uiDir:            filepath.Join(artifactsDir, filepath.FromSlash(PreparedUIDir)),
	}
}

func initPathsForConfig(configPath string) initPaths {
	return resolveInitPaths([]string{configPath}, nil, StatePaths{})
}

func providerDestDir(paths initPaths, name string) string {
	return filepath.Join(paths.providersDir, name)
}

func uiDestDir(paths initPaths, name string) string {
	return filepath.Join(paths.uiDir, name)
}

func authDestDir(paths initPaths, name string) string {
	return filepath.Join(paths.authDir, name)
}

func authorizationDestDir(paths initPaths, name string) string {
	return filepath.Join(paths.authorizationDir, name)
}

func secretsDestDir(paths initPaths, name string) string {
	return filepath.Join(paths.secretsDir, name)
}

func telemetryDestDir(paths initPaths, name string) string {
	return filepath.Join(paths.telemetryDir, name)
}

func auditDestDir(paths initPaths, name string) string {
	return filepath.Join(paths.auditDir, name)
}

func cacheDestDir(paths initPaths, name string) string {
	return filepath.Join(paths.cacheDir, name)
}

func workflowDestDir(paths initPaths, name string) string {
	return filepath.Join(paths.workflowDir, name)
}

func indexeddbDestDir(paths initPaths, name string) string {
	return filepath.Join(paths.artifactsDir, "indexeddb", name)
}

func s3DestDir(paths initPaths, name string) string {
	return filepath.Join(paths.artifactsDir, "s3", name)
}

func componentDestDir(paths initPaths, kind config.HostProviderKind, name string) string {
	switch kind {
	case config.HostProviderKindAuth:
		return authDestDir(paths, name)
	case config.HostProviderKindAuthorization:
		return authorizationDestDir(paths, name)
	case config.HostProviderKindSecrets:
		return secretsDestDir(paths, name)
	case config.HostProviderKindTelemetry:
		return telemetryDestDir(paths, name)
	case config.HostProviderKindAudit:
		return auditDestDir(paths, name)
	case config.HostProviderKindCache:
		return cacheDestDir(paths, name)
	case config.HostProviderKindIndexedDB:
		return indexeddbDestDir(paths, name)
	case config.HostProviderKindWorkflow:
		return workflowDestDir(paths, name)
	default:
		return ""
	}
}

type preparedInstall struct {
	manifestPath   string
	executablePath string
	assetRootPath  string
	manifest       *providermanifestv1.Manifest
}

func inspectPreparedInstall(destDir string) (*preparedInstall, error) {
	manifestPath, err := providerpkg.FindManifestFile(destDir)
	if err != nil {
		return nil, err
	}
	_, manifest, err := providerpkg.ReadManifestFile(manifestPath)
	if err != nil {
		return nil, err
	}

	install := &preparedInstall{
		manifestPath: manifestPath,
		manifest:     manifest,
	}
	if entry := providerpkg.EntrypointForKind(manifest, ""); entry != nil {
		if strings.TrimSpace(entry.ArtifactPath) == "" {
			return nil, fmt.Errorf("manifest entrypoint artifact_path is required")
		}
		install.executablePath = filepath.Join(destDir, filepath.FromSlash(entry.ArtifactPath))
	}
	if manifest != nil && manifest.Spec != nil && strings.TrimSpace(manifest.Spec.AssetRoot) != "" {
		install.assetRootPath = filepath.Join(destDir, filepath.FromSlash(manifest.Spec.AssetRoot))
	}
	return install, nil
}

func providerManifestKind(kind config.HostProviderKind) string {
	switch kind {
	case config.HostProviderKindAuth:
		return providermanifestv1.KindAuth
	case config.HostProviderKindAuthorization:
		return providermanifestv1.KindAuthorization
	case config.HostProviderKindSecrets:
		return providermanifestv1.KindSecrets
	case config.HostProviderKindTelemetry, config.HostProviderKindAudit:
		return providermanifestv1.KindPlugin
	case config.HostProviderKindCache:
		return providermanifestv1.KindCache
	case config.HostProviderKindIndexedDB:
		return providermanifestv1.KindIndexedDB
	case config.HostProviderKindWorkflow:
		return providermanifestv1.KindWorkflow
	default:
		return ""
	}
}

func writeJSONFile(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func ReadLockfile(path string) (*Lockfile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var header struct {
		Schema        string `json:"schema"`
		SchemaVersion int    `json:"schemaVersion"`
		Version       int    `json:"version"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return nil, fmt.Errorf("parsing lockfile %s: %w", path, err)
	}

	if header.Schema != "" {
		var lock providerLockfile
		if err := json.Unmarshal(data, &lock); err != nil {
			return nil, fmt.Errorf("parsing lockfile %s: %w", path, err)
		}
		if err := validateProviderLockfile(&lock); err != nil {
			return nil, err
		}
		return lock.toLockfile(), nil
	}

	if header.Version != LockVersion {
		return nil, fmt.Errorf("unsupported lockfile version %d; run `gestaltd init` to upgrade", header.Version)
	}

	var lock Lockfile
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("parsing lockfile %s: %w", path, err)
	}
	return normalizeLockfile(&lock), nil
}

func WriteLockfile(path string, lock *Lockfile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create lockfile parent directory: %w", err)
	}
	if err := writeJSONFile(path, providerLockfileFromLockfile(lock)); err != nil {
		return fmt.Errorf("writing lockfile: %w", err)
	}
	return nil
}

func lockMatchesConfig(cfg *config.Config, paths initPaths, lock *Lockfile) bool {
	if lock == nil || lock.Version != LockVersion {
		return false
	}
	for name, entry := range cfg.Plugins {
		if !sourceBacked(entry) {
			continue
		}
		lockEntry, found := lock.Providers[name]
		if !lockEntryMatches(paths, providermanifestv1.KindPlugin, name, entry, lockEntry, found, providerDestDir(paths, name)) {
			return false
		}
	}
	for _, collection := range hostProviderCollections(cfg) {
		lockEntries := lockEntriesForKind(lock, collection.kind)
		for name, entry := range collection.entries {
			if entry == nil || !sourceBacked(entry) {
				continue
			}
			lockEntry, found := lockEntries[name]
			if !lockEntryMatches(paths, providerManifestKind(collection.kind), name, entry, lockEntry, found, componentDestDir(paths, collection.kind, name)) {
				return false
			}
		}
	}
	for name, entry := range cfg.Providers.IndexedDB {
		if !sourceBacked(entry) {
			continue
		}
		lockEntry, found := lock.IndexedDBs[name]
		if !lockEntryMatches(paths, providermanifestv1.KindIndexedDB, name, entry, lockEntry, found, indexeddbDestDir(paths, name)) {
			return false
		}
	}
	for name, entry := range cfg.Providers.S3 {
		if !sourceBacked(entry) {
			continue
		}
		lockEntry, found := lock.S3[name]
		if !lockEntryMatches(paths, providermanifestv1.KindS3, name, entry, lockEntry, found, s3DestDir(paths, name)) {
			return false
		}
	}
	for name, entry := range cfg.Providers.UI {
		if entry == nil || !sourceBacked(&entry.ProviderEntry) {
			continue
		}
		lockEntry, ok := lock.UIs[name]
		if !ok {
			return false
		}
		fingerprint, err := NamedUIProviderFingerprint(name, &entry.ProviderEntry, paths.configDir)
		if err != nil || lockEntry.Fingerprint != fingerprint {
			return false
		}
		install, err := inspectPreparedInstall(uiDestDir(paths, name))
		if err != nil {
			return false
		}
		if !preparedManifestMatchesLock(lockEntry, install.manifest) {
			return false
		}
		if install.assetRootPath == "" {
			return false
		}
		if _, err := os.Stat(install.assetRootPath); err != nil {
			return false
		}
	}
	return true
}

func ProviderFingerprint(name string, entry *config.ProviderEntry, configDir string) (string, error) {
	if entry == nil {
		return "", nil
	}

	input := providerFingerprintInput{
		Name:   name,
		Source: entry.SourceRemoteLocation(),
	}
	if entry.HasLocalSource() {
		input.Path = fingerprintLocalSourcePath(entry.SourcePath(), configDir)
		digest, err := fingerprintLocalSourceDigest(entry.SourcePath())
		if err != nil {
			return "", err
		}
		input.Digest = digest
	}

	payload, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func NamedUIProviderFingerprint(name string, entry *config.ProviderEntry, configDir string) (string, error) {
	return ProviderFingerprint("ui:"+name, entry, configDir)
}

func fingerprintLocalSourcePath(sourcePath, configDir string) string {
	path := filepath.Clean(sourcePath)
	if configDir == "" {
		return filepath.ToSlash(path)
	}

	baseDir := filepath.Clean(configDir)
	if !filepath.IsAbs(baseDir) {
		if abs, err := filepath.Abs(baseDir); err == nil {
			baseDir = abs
		}
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}
	if rel, err := filepath.Rel(baseDir, path); err == nil {
		return filepath.ToSlash(filepath.Clean(rel))
	}
	return filepath.ToSlash(path)
}

func fingerprintLocalSourceDigest(sourcePath string) (string, error) {
	manifestPath := sourcePath
	sourceDir := sourcePath

	info, err := os.Stat(sourcePath)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		manifestPath, err = providerpkg.FindManifestFile(sourcePath)
		if err != nil {
			return "", err
		}
	} else {
		sourceDir = filepath.Dir(sourcePath)
	}

	_, manifest, err := providerpkg.PrepareSourceManifest(manifestPath)
	if err != nil {
		return "", err
	}
	return providerpkg.DirectoryDigest(sourceDir, manifestPath, manifest)
}

func archivePolicyKind(kind string) string {
	switch kind {
	case providerLockKindTelemetry, providerLockKindAudit:
		return providermanifestv1.KindPlugin
	default:
		return kind
	}
}

func archivePolicySubject(kind, name string) string {
	switch archivePolicyKind(kind) {
	case providermanifestv1.KindPlugin:
		return fmt.Sprintf("provider %q", name)
	case providermanifestv1.KindWebUI:
		return fmt.Sprintf("ui provider %q", name)
	default:
		return fmt.Sprintf("%s %q", kind, name)
	}
}

func lockEntryDestDir(paths initPaths, kind, name string) string {
	switch kind {
	case providermanifestv1.KindPlugin:
		return providerDestDir(paths, name)
	case providermanifestv1.KindAuth:
		return authDestDir(paths, name)
	case providermanifestv1.KindAuthorization:
		return authorizationDestDir(paths, name)
	case providermanifestv1.KindSecrets:
		return secretsDestDir(paths, name)
	case providermanifestv1.KindCache:
		return cacheDestDir(paths, name)
	case providermanifestv1.KindIndexedDB:
		return indexeddbDestDir(paths, name)
	case providermanifestv1.KindS3:
		return s3DestDir(paths, name)
	case providermanifestv1.KindWorkflow:
		return workflowDestDir(paths, name)
	case providerLockKindTelemetry:
		return telemetryDestDir(paths, name)
	case providerLockKindAudit:
		return auditDestDir(paths, name)
	case providermanifestv1.KindWebUI:
		return uiDestDir(paths, name)
	default:
		return ""
	}
}

func readLockEntryManifest(paths initPaths, entry LockEntry, destDir string) (*providermanifestv1.Manifest, error) {
	if strings.TrimSpace(entry.Manifest) != "" {
		manifestPath := resolveLockPath(paths.artifactsDir, entry.Manifest)
		if _, manifest, err := providerpkg.ReadManifestFile(manifestPath); err == nil {
			return manifest, nil
		}
	}
	if destDir == "" {
		return nil, fmt.Errorf("manifest path is unavailable")
	}
	install, err := inspectPreparedInstall(destDir)
	if err != nil {
		return nil, err
	}
	if install.manifest == nil {
		return nil, fmt.Errorf("prepared install at %s is missing a manifest", destDir)
	}
	return install.manifest, nil
}

func lockEntryMatches(paths initPaths, kind, name string, providerEntry *config.ProviderEntry, entry LockEntry, found bool, destDir string) bool {
	if !found {
		return false
	}
	fingerprint, err := ProviderFingerprint(name, providerEntry, paths.configDir)
	if err != nil || entry.Fingerprint != fingerprint {
		return false
	}
	if entry.Source != providerEntry.SourceRemoteLocation() {
		return false
	}
	if len(entry.Archives) > 0 {
		platform := providerpkg.CurrentPlatformString()
		_, resolvedKey, ok := resolveArchiveForPlatform(entry, platform)
		if !ok {
			return false
		}
		policyKind := archivePolicyKind(kind)
		var manifest *providermanifestv1.Manifest
		if policyKind == providermanifestv1.KindPlugin {
			manifest, err = readLockEntryManifest(paths, entry, destDir)
			if err != nil {
				return false
			}
		}
		if err := validateLockedArchivePolicy(archivePolicySubject(kind, name), policyKind, manifest, entry, platform, resolvedKey); err != nil {
			return false
		}
	}
	if strings.TrimSpace(entry.Manifest) != "" {
		manifestPath := resolveLockPath(paths.artifactsDir, entry.Manifest)
		if _, err := os.Stat(manifestPath); err == nil {
			if entry.Executable != "" {
				executablePath := resolveLockPath(paths.artifactsDir, entry.Executable)
				if _, err := os.Stat(executablePath); err != nil {
					return false
				}
			}
			return true
		}
	}
	install, err := inspectPreparedInstall(destDir)
	if err != nil {
		return false
	}
	if !preparedManifestMatchesLock(entry, install.manifest) {
		return false
	}
	if install.executablePath != "" {
		if _, err := os.Stat(install.executablePath); err != nil {
			return false
		}
	}
	return true
}

func preparedManifestMatchesLock(entry LockEntry, manifest *providermanifestv1.Manifest) bool {
	if manifest == nil {
		return false
	}
	if expectedPackage := lockEntryPackage(entry); expectedPackage != "" && manifest.Source != expectedPackage {
		return false
	}
	if entry.Version != "" && manifest.Version != entry.Version {
		return false
	}
	return true
}

// resolveArchiveForPlatform looks up a LockArchive for the given platform
// string using a fallback chain: exact match → without libc → generic.
func resolveArchiveForPlatform(entry LockEntry, platform string) (LockArchive, string, bool) {
	if a, ok := entry.Archives[platform]; ok {
		return a, platform, true
	}
	goos, goarch, err := providerpkg.ParsePlatformString(platform)
	if err == nil {
		key := providerpkg.PlatformString(goos, goarch)
		if key != platform {
			if a, ok := entry.Archives[key]; ok {
				return a, key, true
			}
		}
	}
	if a, ok := entry.Archives[platformKeyGeneric]; ok {
		return a, platformKeyGeneric, true
	}
	return LockArchive{}, "", false
}

func prepareLocalSourceInstall(kind, name, manifestPath, destDir string) (*preparedInstall, error) {
	if strings.TrimSpace(manifestPath) == "" {
		return nil, fmt.Errorf("manifest path for %s %q is required", kind, name)
	}
	if _, err := os.Stat(manifestPath); err != nil {
		return nil, fmt.Errorf("manifest for %s %q not found at %s: %w", kind, name, manifestPath, err)
	}
	parentDir := filepath.Dir(destDir)
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		return nil, fmt.Errorf("create destination parent directory: %w", err)
	}
	tempDir, err := os.MkdirTemp(parentDir, filepath.Base(destDir)+".tmp-*")
	if err != nil {
		return nil, fmt.Errorf("create temp install directory: %w", err)
	}
	cleanupDir := tempDir
	defer func() {
		if cleanupDir != "" {
			_ = os.RemoveAll(cleanupDir)
		}
	}()

	stageKind := kind
	if stageKind == providerLockKindTelemetry || stageKind == providerLockKindAudit {
		stageKind = providermanifestv1.KindPlugin
	}
	if _, err := providerpkg.StageSourcePreparedInstallDir(manifestPath, tempDir, providerpkg.StageSourcePreparedInstallOptions{
		Kind:       stageKind,
		PluginName: name,
		GOOS:       runtime.GOOS,
		GOARCH:     runtime.GOARCH,
	}); err != nil {
		return nil, fmt.Errorf("prepare manifest for %s %q: %w", kind, name, err)
	}

	backupDir := ""
	if _, err := os.Stat(destDir); err == nil {
		backupDir = destDir + ".backup-" + strconv.FormatInt(time.Now().UnixNano(), 10)
		if err := os.Rename(destDir, backupDir); err != nil {
			return nil, fmt.Errorf("stage existing provider cache at %s: %w", destDir, err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("inspect existing provider cache at %s: %w", destDir, err)
	}
	if err := os.Rename(tempDir, destDir); err != nil {
		if backupDir != "" {
			if restoreErr := os.Rename(backupDir, destDir); restoreErr != nil {
				return nil, fmt.Errorf("activate prepared install at %s: %w (rollback failed: %v)", destDir, err, restoreErr)
			}
		}
		return nil, fmt.Errorf("activate prepared install at %s: %w", destDir, err)
	}
	cleanupDir = ""
	if backupDir != "" {
		if err := os.RemoveAll(backupDir); err != nil {
			return nil, fmt.Errorf("remove staged provider cache backup at %s: %w", backupDir, err)
		}
	}
	install, err := inspectPreparedInstall(destDir)
	if err != nil {
		return nil, fmt.Errorf("inspect prepared install for %s %q: %w", kind, name, err)
	}
	return install, nil
}

func relativePreparedPath(artifactsDir, path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", nil
	}
	rel, err := filepath.Rel(artifactsDir, path)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

func pathWithinRoot(root, target string) bool {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(target) == "" {
		return false
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}

func localLockEntryFromPreparedInstall(paths initPaths, kind, name string, plugin *config.ProviderEntry, install *preparedInstall) (LockEntry, error) {
	fingerprint, err := ProviderFingerprint(name, plugin, paths.configDir)
	if err != nil {
		return LockEntry{}, fmt.Errorf("fingerprinting %s %q provider: %w", kind, name, err)
	}
	manifestPath, err := relativePreparedPath(paths.artifactsDir, install.manifestPath)
	if err != nil {
		return LockEntry{}, fmt.Errorf("compute manifest path for %s %q: %w", kind, name, err)
	}
	executablePath, err := relativePreparedPath(paths.artifactsDir, install.executablePath)
	if err != nil {
		return LockEntry{}, fmt.Errorf("compute executable path for %s %q: %w", kind, name, err)
	}
	return LockEntry{
		Fingerprint: fingerprint,
		Manifest:    manifestPath,
		Executable:  executablePath,
	}, nil
}

func localUILockEntryFromPreparedInstall(paths initPaths, name string, plugin *config.ProviderEntry, install *preparedInstall) (LockUIEntry, error) {
	fingerprint, err := NamedUIProviderFingerprint(name, plugin, paths.configDir)
	if err != nil {
		return LockUIEntry{}, fmt.Errorf("fingerprinting ui %q: %w", name, err)
	}
	manifestPath, err := relativePreparedPath(paths.artifactsDir, install.manifestPath)
	if err != nil {
		return LockUIEntry{}, fmt.Errorf("compute manifest path for ui %q: %w", name, err)
	}
	assetRoot, err := relativePreparedPath(paths.artifactsDir, install.assetRootPath)
	if err != nil {
		return LockUIEntry{}, fmt.Errorf("compute asset root path for ui %q: %w", name, err)
	}
	return LockUIEntry{
		Fingerprint: fingerprint,
		Manifest:    manifestPath,
		AssetRoot:   assetRoot,
	}, nil
}

func (l *Lifecycle) installMetadataSourcePackage(ctx context.Context, expectedKind, name, subject, destDir string, plugin *config.ProviderEntry) (*pluginstore.InstalledPlugin, LockEntry, error) {
	sourceLocation := plugin.SourceMetadataURL()
	metadata, err := fetchProviderReleaseMetadata(ctx, l.metadataHTTPClient(), sourceLocation, sourceAuthToken(plugin))
	if err != nil {
		return nil, LockEntry{}, fmt.Errorf("%s fetch metadata %q: %w", subject, sourceLocation, err)
	}
	expectedManifestKind := archivePolicyKind(expectedKind)
	if metadata.Kind != expectedManifestKind {
		return nil, LockEntry{}, fmt.Errorf("%s metadata kind %q does not match expected kind %q", subject, metadata.Kind, expectedManifestKind)
	}
	archives, err := providerReleaseArchives(sourceLocation, metadata)
	if err != nil {
		return nil, LockEntry{}, fmt.Errorf("%s resolve archive metadata %q: %w", subject, sourceLocation, err)
	}
	entry := LockEntry{
		Package:  metadata.Package,
		Kind:     metadata.Kind,
		Runtime:  metadata.Runtime,
		Source:   sourceLocation,
		Version:  metadata.Version,
		Archives: archives,
	}

	currentPlatform := providerpkg.CurrentPlatformString()
	archive, resolvedKey, ok := resolveArchiveForPlatform(entry, currentPlatform)
	if !ok || archive.URL == "" {
		return nil, LockEntry{}, fmt.Errorf("no archive for platform %s for %s; publish an explicit %s target or a generic package where allowed", currentPlatform, subject, currentPlatform)
	}
	download, err := downloadArchiveForSource(ctx, l.metadataHTTPClient(), sourceAuthToken(plugin), archive.URL)
	if err != nil {
		return nil, LockEntry{}, fmt.Errorf("download metadata source package for %s: %w", subject, err)
	}
	defer download.Cleanup()
	if archive.SHA256 != "" && download.SHA256Hex != archive.SHA256 {
		return nil, LockEntry{}, fmt.Errorf("metadata source digest mismatch for %s: got %s, want %s", subject, download.SHA256Hex, archive.SHA256)
	}

	installed, err := pluginstore.Install(download.LocalPath, destDir)
	if err != nil {
		return nil, LockEntry{}, fmt.Errorf("install metadata source package for %s: %w", subject, err)
	}
	if err := validateInstalledManifestKind(expectedManifestKind, name, installed.Manifest); err != nil {
		return nil, LockEntry{}, err
	}
	if installed.Manifest.Source != metadata.Package {
		return nil, LockEntry{}, fmt.Errorf("%s manifest source %q does not match metadata package %q", subject, installed.Manifest.Source, metadata.Package)
	}
	if installed.Manifest.Version != metadata.Version {
		return nil, LockEntry{}, fmt.Errorf("%s manifest version %q does not match metadata version %q", subject, installed.Manifest.Version, metadata.Version)
	}
	installedRuntime := releaseRuntimeForManifest(installed.Manifest, expectedManifestKind)
	if metadata.Runtime != installedRuntime {
		return nil, LockEntry{}, fmt.Errorf("%s manifest runtime %q does not match metadata runtime %q", subject, installedRuntime, metadata.Runtime)
	}
	entry.Package = installed.Manifest.Source
	entry.Kind = installed.Manifest.Kind
	entry.Runtime = installedRuntime
	entry.Version = installed.Manifest.Version
	if err := validateLockedArchivePolicy(subject, expectedManifestKind, installed.Manifest, entry, currentPlatform, resolvedKey); err != nil {
		return nil, LockEntry{}, err
	}
	return installed, entry, nil
}

func (l *Lifecycle) writeProviderArtifacts(ctx context.Context, cfg *config.Config, paths initPaths) (map[string]LockProviderEntry, error) {
	written := make(map[string]LockProviderEntry)
	for name, entry := range cfg.Plugins {
		if entry == nil {
			continue
		}
		configMap, err := config.NodeToMap(entry.Config)
		if err != nil {
			return nil, fmt.Errorf("decode provider config for provider %q: %w", name, err)
		}
		if !sourceBacked(entry) {
			continue
		}
		lockEntry, err := l.lockProviderEntryForSource(ctx, paths, name, entry, configMap)
		if err != nil {
			return nil, err
		}
		written[name] = lockEntry
	}

	return written, nil
}

func (l *Lifecycle) writeComponentArtifact(ctx context.Context, paths initPaths, kind, name, destDir string, plugin *config.ProviderEntry, configNode yaml.Node) (LockEntry, error) {
	configMap, err := config.NodeToMap(configNode)
	if err != nil {
		return LockEntry{}, fmt.Errorf("decode provider config for %s %q: %w", kind, name, err)
	}
	return l.lockComponentEntryForSource(ctx, paths, kind, name, destDir, plugin, configMap)
}

func (l *Lifecycle) lockComponentEntryForSource(ctx context.Context, paths initPaths, kind, name, destDir string, plugin *config.ProviderEntry, configMap map[string]any) (LockEntry, error) {
	if plugin != nil && plugin.HasLocalSource() {
		install, err := prepareLocalSourceInstall(kind, name, plugin.SourcePath(), destDir)
		if err != nil {
			return LockEntry{}, err
		}
		if err := validateInstalledManifestKind(kind, name, install.manifest); err != nil {
			return LockEntry{}, err
		}
		if err := providerpkg.ValidateConfigForManifest(install.manifestPath, install.manifest, kind, configMap); err != nil {
			return LockEntry{}, fmt.Errorf("provider config validation for %s %q: %w", kind, name, err)
		}
		return localLockEntryFromPreparedInstall(paths, kind, name, plugin, install)
	}

	sourceLocation := plugin.SourceRemoteLocation()
	var (
		installed *pluginstore.InstalledPlugin
		entry     LockEntry
		err       error
	)
	subject := fmt.Sprintf("%s %q", kind, name)
	if !plugin.HasMetadataSource() {
		return LockEntry{}, fmt.Errorf("%s %q source %q: only metadata URL and local path sources are supported", kind, name, sourceLocation)
	}
	installed, entry, err = l.installMetadataSourcePackage(ctx, kind, name, subject, destDir, plugin)
	if err != nil {
		return LockEntry{}, err
	}
	if err := providerpkg.ValidateConfigForManifest(installed.ManifestPath, installed.Manifest, kind, configMap); err != nil {
		return LockEntry{}, fmt.Errorf("provider config validation for %s %q: %w", kind, name, err)
	}

	entrypoint := providerpkg.EntrypointForKind(installed.Manifest, kind)
	if entrypoint == nil {
		return LockEntry{}, fmt.Errorf("%s %q manifest does not define a %s entrypoint", kind, name, kind)
	}
	fingerprint, err := ProviderFingerprint(name, plugin, paths.configDir)
	if err != nil {
		return LockEntry{}, fmt.Errorf("fingerprinting %s %q provider: %w", kind, name, err)
	}
	manifestPath, err := filepath.Rel(paths.artifactsDir, installed.ManifestPath)
	if err != nil {
		return LockEntry{}, fmt.Errorf("compute manifest path for %s %q: %w", kind, name, err)
	}
	executablePath, err := filepath.Rel(paths.artifactsDir, filepath.Join(installed.Root, filepath.FromSlash(entrypoint.ArtifactPath)))
	if err != nil {
		return LockEntry{}, fmt.Errorf("compute executable path for %s %q: %w", kind, name, err)
	}
	entry.Fingerprint = fingerprint
	entry.Manifest = filepath.ToSlash(manifestPath)
	entry.Executable = filepath.ToSlash(executablePath)
	return entry, nil
}

func (l *Lifecycle) lockProviderEntryForSource(ctx context.Context, paths initPaths, name string, plugin *config.ProviderEntry, configMap map[string]any) (LockProviderEntry, error) {
	if plugin != nil && plugin.HasLocalSource() {
		install, err := prepareLocalSourceInstall(providermanifestv1.KindPlugin, name, plugin.SourcePath(), providerDestDir(paths, name))
		if err != nil {
			return LockProviderEntry{}, err
		}
		if err := validateInstalledManifestKind(providermanifestv1.KindPlugin, name, install.manifest); err != nil {
			return LockProviderEntry{}, err
		}
		if err := providerpkg.ValidateConfigForManifest(install.manifestPath, install.manifest, providermanifestv1.KindPlugin, configMap); err != nil {
			return LockProviderEntry{}, fmt.Errorf("provider config validation for provider %q: %w", name, err)
		}
		return localLockEntryFromPreparedInstall(paths, providermanifestv1.KindPlugin, name, plugin, install)
	}

	sourceLocation := plugin.SourceRemoteLocation()
	destDir := providerDestDir(paths, name)
	var (
		installed *pluginstore.InstalledPlugin
		entry     LockProviderEntry
		err       error
	)
	if !plugin.HasMetadataSource() {
		return LockProviderEntry{}, fmt.Errorf("provider %q source %q: only metadata URL and local path sources are supported", name, sourceLocation)
	}
	installed, entry, err = l.installMetadataSourcePackage(ctx, providermanifestv1.KindPlugin, name, fmt.Sprintf("provider %q", name), destDir, plugin)
	if err != nil {
		return LockProviderEntry{}, err
	}

	if err := providerpkg.ValidateConfigForManifest(installed.ManifestPath, installed.Manifest, providermanifestv1.KindPlugin, configMap); err != nil {
		return LockProviderEntry{}, fmt.Errorf("provider config validation for provider %q: %w", name, err)
	}
	fingerprint, err := ProviderFingerprint(name, plugin, paths.configDir)
	if err != nil {
		return LockProviderEntry{}, fmt.Errorf("fingerprinting provider %q: %w", name, err)
	}
	manifestPath, err := filepath.Rel(paths.artifactsDir, installed.ManifestPath)
	if err != nil {
		return LockProviderEntry{}, fmt.Errorf("compute manifest path for provider %q: %w", name, err)
	}
	executableRel := ""
	if installed.ExecutablePath != "" {
		executableRel, err = filepath.Rel(paths.artifactsDir, installed.ExecutablePath)
		if err != nil {
			return LockProviderEntry{}, fmt.Errorf("compute executable path for provider %q: %w", name, err)
		}
	}
	entry.Fingerprint = fingerprint
	entry.Manifest = filepath.ToSlash(manifestPath)
	entry.Executable = filepath.ToSlash(executableRel)
	return entry, nil
}

func (l *Lifecycle) writeNamedUIProviderArtifact(ctx context.Context, paths initPaths, name string, plugin *config.ProviderEntry, destDir string, subject string) (LockUIEntry, error) {
	if plugin == nil || !sourceBacked(plugin) {
		return LockUIEntry{}, fmt.Errorf("%s requires source configuration", subject)
	}
	configMap, err := config.NodeToMap(plugin.Config)
	if err != nil {
		return LockUIEntry{}, fmt.Errorf("decode %s config: %w", subject, err)
	}
	fingerprint, err := NamedUIProviderFingerprint(name, plugin, paths.configDir)
	if err != nil {
		return LockUIEntry{}, fmt.Errorf("fingerprinting %s: %w", subject, err)
	}
	if plugin.HasLocalSource() {
		install, err := prepareLocalSourceInstall(providermanifestv1.KindWebUI, name, plugin.SourcePath(), destDir)
		if err != nil {
			return LockUIEntry{}, err
		}
		if err := validateInstalledManifestKind(providermanifestv1.KindWebUI, subject, install.manifest); err != nil {
			return LockUIEntry{}, err
		}
		if err := providerpkg.ValidateConfigForManifest(install.manifestPath, install.manifest, providermanifestv1.KindWebUI, configMap); err != nil {
			return LockUIEntry{}, fmt.Errorf("provider config validation for %s: %w", subject, err)
		}
		entry, err := localUILockEntryFromPreparedInstall(paths, name, plugin, install)
		if err != nil {
			return LockUIEntry{}, err
		}
		entry.Fingerprint = fingerprint
		return entry, nil
	}
	expectedPackage := plugin.SourceRemoteLocation()

	var (
		installed *pluginstore.InstalledPlugin
		entry     LockUIEntry
		opErr     error
	)
	if !plugin.HasMetadataSource() {
		return LockUIEntry{}, fmt.Errorf("%s source %q: only metadata URL and local path sources are supported", subject, expectedPackage)
	}
	installed, entry, opErr = l.installMetadataSourcePackage(ctx, providermanifestv1.KindWebUI, name, subject, destDir, plugin)
	if opErr != nil {
		return LockUIEntry{}, opErr
	}
	if err := providerpkg.ValidateConfigForManifest(installed.ManifestPath, installed.Manifest, providermanifestv1.KindWebUI, configMap); err != nil {
		return LockUIEntry{}, fmt.Errorf("provider config validation for %s: %w", subject, err)
	}
	manifestPath, err := filepath.Rel(paths.artifactsDir, installed.ManifestPath)
	if err != nil {
		return LockUIEntry{}, fmt.Errorf("compute manifest path for %s: %w", subject, err)
	}
	assetRoot, err := filepath.Rel(paths.artifactsDir, installed.AssetRoot)
	if err != nil {
		return LockUIEntry{}, fmt.Errorf("compute asset root path for %s: %w", subject, err)
	}
	entry.Fingerprint = fingerprint
	entry.Manifest = filepath.ToSlash(manifestPath)
	entry.AssetRoot = filepath.ToSlash(assetRoot)
	return entry, nil
}

func (l *Lifecycle) applyPreparedProviders(paths initPaths, lock *Lockfile, cfg *config.Config, locked bool) error {
	if !configHasProviderLoading(cfg) {
		return nil
	}

	synthesizedPluginUIs, err := synthesizeLockedSourcePluginOwnedUIEntries(cfg, paths, lock)
	if err != nil {
		return err
	}

	if err := l.resolveConfiguredPlugins(paths, lock, cfg, locked); err != nil {
		clearSynthesizedPluginOwnedUIEntries(cfg, synthesizedPluginUIs)
		return err
	}
	if err := synthesizePluginOwnedUIEntries(cfg); err != nil {
		clearSynthesizedPluginOwnedUIEntries(cfg, synthesizedPluginUIs)
		return err
	}
	for _, collection := range hostProviderCollections(cfg) {
		lockEntries := lockEntriesForKind(lock, collection.kind)
		for name, entry := range collection.entries {
			if entry == nil {
				continue
			}
			if err := l.applyComponentProvider(paths, lockEntries, providerManifestKind(collection.kind), name, entry, entry.Config, &entry.Config, componentDestDir(paths, collection.kind, name), locked); err != nil {
				clearSynthesizedPluginOwnedUIEntries(cfg, synthesizedPluginUIs)
				return err
			}
		}
	}
	indexedDBLocks := map[string]LockEntry(nil)
	if lock != nil {
		indexedDBLocks = lock.IndexedDBs
	}
	for name, def := range cfg.Providers.IndexedDB {
		if def != nil {
			if err := l.applyComponentProvider(paths, indexedDBLocks, providermanifestv1.KindIndexedDB, name, def, def.Config, &def.Config, indexeddbDestDir(paths, name), locked); err != nil {
				clearSynthesizedPluginOwnedUIEntries(cfg, synthesizedPluginUIs)
				return err
			}
		}
	}
	s3Locks := map[string]LockEntry(nil)
	if lock != nil {
		s3Locks = lock.S3
	}
	for name, def := range cfg.Providers.S3 {
		if def != nil {
			if err := l.applyComponentProvider(paths, s3Locks, providermanifestv1.KindS3, name, def, def.Config, &def.Config, s3DestDir(paths, name), locked); err != nil {
				clearSynthesizedPluginOwnedUIEntries(cfg, synthesizedPluginUIs)
				return err
			}
		}
	}
	for name, entry := range cfg.Providers.UI {
		if entry == nil {
			continue
		}
		var lockEntry *LockUIEntry
		if lock != nil {
			if le, ok := lock.UIs[name]; ok {
				lockEntry = &le
			}
		}
		resolvedAssetRoot, err := l.applyConfiguredUIProvider(paths, lockEntry, &entry.ProviderEntry, name, "ui "+strconv.Quote(name), uiDestDir(paths, name), locked)
		if err != nil {
			clearSynthesizedPluginOwnedUIEntries(cfg, synthesizedPluginUIs)
			return err
		}
		entry.ResolvedAssetRoot = resolvedAssetRoot
	}

	return nil
}

func (l *Lifecycle) applyLockedProviders(configPaths []string, state StatePaths, cfg *config.Config, locked bool, bootstrapLock *Lockfile) (bool, error) {
	if !configHasProviderLoading(cfg) {
		return false, nil
	}

	paths := resolveInitPaths(configPaths, cfg, state)
	lock := bootstrapLock
	var err error
	validatedDuringInit := false
	if lock == nil {
		lock, err = ReadLockfile(paths.lockfilePath)
	}
	if !locked && (err != nil || !lockMatchesConfig(cfg, paths, lock) || (bootstrapLock == nil && configHasLocalProviderSources(cfg)) || (bootstrapLock == nil && configHasMetadataProviderSources(cfg))) {
		lock, err = l.InitAtPathsWithStatePaths(configPaths, state)
		validatedDuringInit = err == nil
	}
	if err != nil {
		return false, fmt.Errorf("source-backed providers require prepared artifacts; run `gestaltd init %s`: %w", paths.configFlags, err)
	}
	if err := l.applyPreparedProviders(paths, lock, cfg, locked); err != nil {
		return false, err
	}
	return validatedDuringInit, nil
}

func installLockedPackageAtomic(packagePath, destDir string) (*pluginstore.InstalledPlugin, func() error, func() error, error) {
	if destDir == "" {
		return nil, nil, nil, fmt.Errorf("destination directory is required")
	}
	parentDir := filepath.Dir(destDir)
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		return nil, nil, nil, fmt.Errorf("create destination parent directory: %w", err)
	}
	tempDir, err := os.MkdirTemp(parentDir, filepath.Base(destDir)+".tmp-*")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create temp install directory: %w", err)
	}
	cleanupDir := tempDir
	installed, err := pluginstore.Install(packagePath, tempDir)
	if err != nil {
		_ = os.RemoveAll(cleanupDir)
		return nil, nil, nil, err
	}
	commit := func() error {
		backupDir := ""
		if _, err := os.Stat(destDir); err == nil {
			backupDir = destDir + ".backup-" + strconv.FormatInt(time.Now().UnixNano(), 10)
			if err := os.Rename(destDir, backupDir); err != nil {
				return fmt.Errorf("stage existing provider cache at %s: %w", destDir, err)
			}
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect existing provider cache at %s: %w", destDir, err)
		}
		if err := os.Rename(tempDir, destDir); err != nil {
			if backupDir != "" {
				if restoreErr := os.Rename(backupDir, destDir); restoreErr != nil {
					return fmt.Errorf("activate prepared install at %s: %w (rollback failed: %v)", destDir, err, restoreErr)
				}
			}
			return fmt.Errorf("activate prepared install at %s: %w", destDir, err)
		}
		if backupDir != "" {
			if err := os.RemoveAll(backupDir); err != nil {
				return fmt.Errorf("remove staged provider cache backup at %s: %w", backupDir, err)
			}
		}
		cleanupDir = ""
		return nil
	}
	return installed, func() error {
		if cleanupDir == "" {
			return nil
		}
		return os.RemoveAll(cleanupDir)
	}, commit, nil
}

func (l *Lifecycle) resolveConfiguredPlugins(paths initPaths, lock *Lockfile, cfg *config.Config, locked bool) error {
	for name, entry := range cfg.Plugins {
		if entry == nil {
			continue
		}
		configMap, err := config.NodeToMap(entry.Config)
		if err != nil {
			return fmt.Errorf("decode provider config for provider %q: %w", name, err)
		}
		switch {
		case sourceBacked(entry):
			if err := l.applyLockedProviderEntry(paths, lock, name, entry, configMap, locked); err != nil {
				return err
			}
		default:
			continue
		}
		if manifest := entry.ResolvedManifest; manifest != nil {
			entry.DisplayName = cmp.Or(entry.DisplayName, manifest.DisplayName)
			entry.Description = cmp.Or(entry.Description, manifest.Description)
		}
		entry.IconFile = cmp.Or(entry.IconFile, entry.ResolvedIconFile)
	}
	return nil
}

func synthesizePluginOwnedUIEntries(cfg *config.Config) error {
	if cfg == nil || len(cfg.Plugins) == 0 {
		return nil
	}
	if cfg.Providers.UI == nil {
		cfg.Providers.UI = map[string]*config.UIEntry{}
	}

	pluginNames := slices.Sorted(maps.Keys(cfg.Plugins))
	for _, pluginName := range pluginNames {
		plugin := cfg.Plugins[pluginName]
		if plugin == nil || strings.TrimSpace(plugin.UI) != "" || strings.TrimSpace(plugin.MountPath) == "" {
			continue
		}
		manifestSpec := plugin.ManifestSpec()
		if manifestSpec == nil || manifestSpec.UI == nil {
			return fmt.Errorf("plugin %q mountPath requires spec.ui or plugins.%s.ui", pluginName, pluginName)
		}
		ownedUI := manifestSpec.UI
		entry, err := ownedUIEntryForPlugin(plugin, ownedUI)
		if err != nil {
			return fmt.Errorf("plugin %q ui: %w", pluginName, err)
		}
		entry.Path = strings.TrimSpace(plugin.MountPath)
		entry.AuthorizationPolicy = strings.TrimSpace(plugin.AuthorizationPolicy)
		entry.OwnerPlugin = pluginName
		if existing := cfg.Providers.UI[pluginName]; existing != nil {
			if err := validateSynthesizedPluginUIEntry(pluginName, existing, entry); err != nil {
				return err
			}
			if existing.Source.Auth == nil && entry.Source.Auth != nil {
				existing.Source.Auth = entry.Source.Auth
			}
			existing.Path = cmp.Or(existing.Path, entry.Path)
			existing.AuthorizationPolicy = cmp.Or(existing.AuthorizationPolicy, entry.AuthorizationPolicy)
			existing.OwnerPlugin = cmp.Or(existing.OwnerPlugin, entry.OwnerPlugin)
			continue
		}
		cfg.Providers.UI[pluginName] = entry
	}
	return nil
}

func synthesizeLockedSourcePluginOwnedUIEntries(cfg *config.Config, paths initPaths, lock *Lockfile) (map[string]struct{}, error) {
	added := map[string]struct{}{}
	if cfg == nil || len(cfg.Plugins) == 0 || lock == nil {
		return added, nil
	}
	if cfg.Providers.UI == nil {
		cfg.Providers.UI = map[string]*config.UIEntry{}
	}
	pluginNames := slices.Sorted(maps.Keys(cfg.Plugins))
	for _, pluginName := range pluginNames {
		plugin := cfg.Plugins[pluginName]
		if plugin == nil || strings.TrimSpace(plugin.UI) != "" || strings.TrimSpace(plugin.MountPath) == "" || !sourceBacked(plugin) {
			continue
		}
		lockEntry, ok := lock.Providers[pluginName]
		if !ok {
			continue
		}
		install, err := inspectPreparedInstall(providerDestDir(paths, pluginName))
		if err != nil {
			return added, fmt.Errorf("prepare manifest for provider %q: %w", pluginName, err)
		}
		if !preparedManifestMatchesLock(lockEntry, install.manifest) {
			return added, fmt.Errorf("prepared manifest for provider %q is missing or stale", pluginName)
		}
		if install.manifest == nil || install.manifest.Spec == nil || install.manifest.Spec.UI == nil {
			continue
		}
		entry, err := ownedUIEntryFromManifest(install.manifestPath, install.manifest.Spec.UI)
		if err != nil {
			return added, fmt.Errorf("plugin %q ui: %w", pluginName, err)
		}
		entry.Path = strings.TrimSpace(plugin.MountPath)
		entry.AuthorizationPolicy = strings.TrimSpace(plugin.AuthorizationPolicy)
		entry.OwnerPlugin = pluginName
		if existing := cfg.Providers.UI[pluginName]; existing != nil {
			if err := validateSynthesizedPluginUIEntry(pluginName, existing, entry); err != nil {
				return added, err
			}
			if existing.Source.Auth == nil && entry.Source.Auth != nil {
				existing.Source.Auth = entry.Source.Auth
			}
			existing.Path = cmp.Or(existing.Path, entry.Path)
			existing.AuthorizationPolicy = cmp.Or(existing.AuthorizationPolicy, entry.AuthorizationPolicy)
			existing.OwnerPlugin = cmp.Or(existing.OwnerPlugin, entry.OwnerPlugin)
			continue
		}
		cfg.Providers.UI[pluginName] = entry
		added[pluginName] = struct{}{}
	}
	return added, nil
}

func clearSynthesizedPluginOwnedUIEntries(cfg *config.Config, added map[string]struct{}) {
	if cfg == nil || len(added) == 0 || cfg.Providers.UI == nil {
		return
	}
	for name := range added {
		delete(cfg.Providers.UI, name)
	}
}

func ownedUIEntryForPlugin(plugin *config.ProviderEntry, ownedUI *providermanifestv1.OwnedUI) (*config.UIEntry, error) {
	if plugin == nil || ownedUI == nil {
		return nil, fmt.Errorf("owned ui definition is required")
	}
	return ownedUIEntryFromManifest(plugin.ResolvedManifestPath, ownedUI)
}

func ownedUIEntryFromManifest(manifestPath string, ownedUI *providermanifestv1.OwnedUI) (*config.UIEntry, error) {
	if ownedUI == nil {
		return nil, fmt.Errorf("owned ui definition is required")
	}
	if strings.TrimSpace(ownedUI.Path) == "" {
		return nil, fmt.Errorf("spec.ui.path is required")
	}
	if strings.TrimSpace(manifestPath) == "" {
		return nil, fmt.Errorf("resolved plugin manifest path is required for spec.ui.path")
	}
	entry := &config.UIEntry{}
	entry.Source.Path = filepath.Clean(filepath.Join(filepath.Dir(manifestPath), filepath.FromSlash(ownedUI.Path)))
	return entry, nil
}

func validateSynthesizedPluginUIEntry(pluginName string, existing, expected *config.UIEntry) error {
	if existing == nil || expected == nil {
		return nil
	}
	if current := strings.TrimSpace(existing.SourceRemoteLocation()); current != "" {
		return fmt.Errorf("config validation: plugins.%s owned ui conflicts with providers.ui.%s.source", pluginName, pluginName)
	}
	if current := strings.TrimSpace(existing.Source.Path); current != "" && !equivalentProviderManifestPath(current, expected.Source.Path) {
		return fmt.Errorf("config validation: plugins.%s owned ui conflicts with providers.ui.%s.source.path", pluginName, pluginName)
	}
	if current := strings.TrimSpace(existing.Path); current != "" && current != expected.Path {
		return fmt.Errorf("config validation: plugins.%s.mountPath %q conflicts with providers.ui.%s.path", pluginName, expected.Path, pluginName)
	}
	if current := strings.TrimSpace(existing.AuthorizationPolicy); current != "" && current != expected.AuthorizationPolicy {
		return fmt.Errorf("config validation: plugins.%s.authorizationPolicy conflicts with providers.ui.%s.authorizationPolicy", pluginName, pluginName)
	}
	if current := strings.TrimSpace(existing.OwnerPlugin); current != "" && current != expected.OwnerPlugin {
		return fmt.Errorf("config validation: plugins.%s owned ui conflicts with providers.ui.%s owner", pluginName, pluginName)
	}
	return nil
}

func equivalentProviderManifestPath(current, expected string) bool {
	current = strings.TrimSpace(current)
	expected = strings.TrimSpace(expected)
	if current == expected {
		return true
	}
	if current == "" || expected == "" {
		return false
	}
	_, currentManifest, currentErr := providerpkg.ReadManifestFile(current)
	_, expectedManifest, expectedErr := providerpkg.ReadManifestFile(expected)
	if currentErr != nil || expectedErr != nil {
		return false
	}
	return currentManifest != nil && expectedManifest != nil &&
		currentManifest.Kind == expectedManifest.Kind &&
		currentManifest.Source == expectedManifest.Source &&
		currentManifest.Version == expectedManifest.Version
}

func (l *Lifecycle) applyConfiguredUIProvider(paths initPaths, lockEntry *LockUIEntry, provider *config.ProviderEntry, logicalName, subject, destDir string, locked bool) (string, error) {
	if provider == nil {
		return "", nil
	}
	configMap, err := config.NodeToMap(provider.Config)
	if err != nil {
		return "", fmt.Errorf("decode %s config: %w", subject, err)
	}
	switch {
	case sourceBacked(provider):
		if lockEntry == nil {
			if provider.HasLocalSource() && pathWithinRoot(filepath.Join(paths.artifactsDir, ".gestaltd"), provider.SourcePath()) {
				return bindPathBackedUIManifest(provider, configMap)
			}
			return "", fmt.Errorf("prepared artifact for %s is missing or stale; run `gestaltd init %s`", subject, paths.configFlags)
		}
		fingerprint, err := NamedUIProviderFingerprint(logicalName, provider, paths.configDir)
		if err != nil || lockEntry.Fingerprint != fingerprint {
			return "", fmt.Errorf("prepared artifact for %s is missing or stale; run `gestaltd init %s`", subject, paths.configFlags)
		}
		install, err := inspectPreparedInstall(destDir)
		needMaterialize := err != nil || !preparedManifestMatchesLock(*lockEntry, install.manifest)
		if !needMaterialize && install.assetRootPath == "" {
			needMaterialize = true
		}
		if !needMaterialize {
			if _, err := os.Stat(install.assetRootPath); err != nil {
				needMaterialize = true
			}
		}
		if needMaterialize {
			if len(lockEntry.Archives) == 0 {
				return "", fmt.Errorf("prepared artifact for %s is missing or stale; run `gestaltd init %s`", subject, paths.configFlags)
			}
			if err := l.materializeLockedUIProvider(context.Background(), paths, provider, *lockEntry, destDir, locked); err != nil {
				return "", err
			}
			install, err = inspectPreparedInstall(destDir)
			if err != nil {
				return "", fmt.Errorf("read prepared manifest for %s: %w", subject, err)
			}
		}
		if install.assetRootPath == "" {
			return "", fmt.Errorf("prepared asset root for %s not found in %s", subject, destDir)
		}
		if _, err := os.Stat(install.assetRootPath); err != nil {
			return "", fmt.Errorf("prepared asset root for %s not found at %s", subject, install.assetRootPath)
		}
		if err := bindResolvedUIManifest(provider, install.manifestPath, install.manifest, configMap); err != nil {
			return "", err
		}
		return install.assetRootPath, nil
	default:
		return "", nil
	}
}

func (l *Lifecycle) applyComponentProvider(paths initPaths, lockEntries map[string]LockEntry, kind, name string, provider *config.ProviderEntry, providerConfig yaml.Node, targetNode *yaml.Node, destDir string, locked bool) error {
	if provider == nil {
		return nil
	}
	configMap, err := config.NodeToMap(providerConfig)
	if err != nil {
		return fmt.Errorf("decode provider config for %s %q: %w", kind, name, err)
	}
	switch {
	case sourceBacked(provider):
		if lockEntries == nil {
			return fmt.Errorf("prepared artifact for %s %q is missing or stale; run `gestaltd init %s`", kind, name, paths.configFlags)
		}
		lockEntry, ok := lockEntries[name]
		if !ok {
			return fmt.Errorf("prepared artifact for %s %q is missing or stale; run `gestaltd init %s`", kind, name, paths.configFlags)
		}
		if err := l.applyLockedComponentEntry(paths, &lockEntry, kind, name, provider, configMap, destDir, locked); err != nil {
			return err
		}
	default:
		return nil
	}

	node, err := buildComponentRuntimeConfigNode(name, kind, provider, providerConfig)
	if err != nil {
		return err
	}
	*targetNode = node
	return nil
}

func (l *Lifecycle) applyLockedProviderEntry(paths initPaths, lock *Lockfile, name string, plugin *config.ProviderEntry, configMap map[string]any, locked bool) error {
	entry, ok := lock.Providers[name]
	if !ok {
		return fmt.Errorf("prepared artifact for provider %q is missing or stale; run `gestaltd init %s`", name, paths.configFlags)
	}
	fingerprint, err := ProviderFingerprint(name, plugin, paths.configDir)
	if err != nil {
		return fmt.Errorf("fingerprinting provider %q: %w", name, err)
	}
	if entry.Fingerprint != fingerprint || entry.Source != plugin.SourceRemoteLocation() {
		return fmt.Errorf("prepared artifact for provider %q is missing or stale; run `gestaltd init %s`", name, paths.configFlags)
	}

	destDir := providerDestDir(paths, name)
	install, err := inspectPreparedInstall(destDir)
	needMaterialize := err != nil || !preparedManifestMatchesLock(entry, install.manifest)
	if err == nil && !needMaterialize {
		platform := providerpkg.CurrentPlatformString()
		if _, resolvedKey, ok := resolveArchiveForPlatform(entry, platform); ok {
			if policyErr := validateLockedArchivePolicy(fmt.Sprintf("provider %q", name), providermanifestv1.KindPlugin, install.manifest, entry, platform, resolvedKey); policyErr != nil {
				return policyErr
			}
		}
	}
	if !needMaterialize && install.executablePath != "" {
		if _, err := os.Stat(install.executablePath); err != nil {
			needMaterialize = true
		}
	}
	if needMaterialize {
		if len(entry.Archives) == 0 {
			return fmt.Errorf("prepared artifact for provider %q is missing or stale; run `gestaltd init %s`", name, paths.configFlags)
		}
		if err := l.materializeLockedProvider(context.Background(), paths, name, plugin, entry, locked); err != nil {
			return err
		}
		install, err = inspectPreparedInstall(destDir)
		if err != nil {
			return fmt.Errorf("read prepared manifest for provider %q: %w", name, err)
		}
	}
	if err := bindResolvedProviderManifest(name, plugin, install.manifestPath, install.manifest, configMap); err != nil {
		return err
	}
	if install.executablePath != "" {
		if _, err := os.Stat(install.executablePath); err != nil {
			return fmt.Errorf("prepared executable for provider %q not found at %s; run `gestaltd init %s`", name, install.executablePath, paths.configFlags)
		}
		args, err := providerEntrypointArgs(install.manifest)
		if err != nil {
			return fmt.Errorf("resolve entrypoint for provider %q: %w", name, err)
		}
		plugin.Command = install.executablePath
		plugin.Args = append([]string(nil), args...)
	}
	return nil
}

func (l *Lifecycle) applyLockedComponentEntry(paths initPaths, entry *LockEntry, kind, name string, plugin *config.ProviderEntry, configMap map[string]any, destDir string, locked bool) error {
	if entry == nil {
		return fmt.Errorf("prepared artifact for %s %q is missing or stale; run `gestaltd init %s`", kind, name, paths.configFlags)
	}
	fingerprint, err := ProviderFingerprint(name, plugin, paths.configDir)
	if err != nil {
		return fmt.Errorf("fingerprinting %s %q provider: %w", kind, name, err)
	}
	if entry.Fingerprint != fingerprint || entry.Source != plugin.SourceRemoteLocation() {
		return fmt.Errorf("prepared artifact for %s %q is missing or stale; run `gestaltd init %s`", kind, name, paths.configFlags)
	}

	install, err := inspectPreparedInstall(destDir)
	needMaterialize := err != nil || !preparedManifestMatchesLock(*entry, install.manifest)
	if err == nil && !needMaterialize {
		platform := providerpkg.CurrentPlatformString()
		if _, resolvedKey, ok := resolveArchiveForPlatform(*entry, platform); ok {
			if policyErr := validateLockedArchivePolicy(fmt.Sprintf("%s %q", kind, name), archivePolicyKind(kind), install.manifest, *entry, platform, resolvedKey); policyErr != nil {
				return policyErr
			}
		}
	}
	if !needMaterialize && install.executablePath == "" {
		needMaterialize = true
	}
	if !needMaterialize {
		if _, err := os.Stat(install.executablePath); err != nil {
			needMaterialize = true
		}
	}
	if needMaterialize {
		if len(entry.Archives) == 0 {
			return fmt.Errorf("prepared artifact for %s %q is missing or stale; run `gestaltd init %s`", kind, name, paths.configFlags)
		}
		if err := l.materializeLockedComponent(context.Background(), paths, kind, name, plugin, *entry, destDir, locked); err != nil {
			return err
		}
		install, err = inspectPreparedInstall(destDir)
		if err != nil {
			return fmt.Errorf("read prepared manifest for %s %q: %w", kind, name, err)
		}
	}
	if install.executablePath == "" {
		return fmt.Errorf("prepared executable for %s %q not found in %s; run `gestaltd init %s`", kind, name, destDir, paths.configFlags)
	}
	if err := bindResolvedComponentManifest(kind, name, plugin, install.manifestPath, install.manifest, configMap); err != nil {
		return err
	}
	if _, err := os.Stat(install.executablePath); err != nil {
		return fmt.Errorf("prepared executable for %s %q not found at %s; run `gestaltd init %s`", kind, name, install.executablePath, paths.configFlags)
	}
	args, err := componentEntrypointArgs(install.manifest, kind)
	if err != nil {
		return fmt.Errorf("resolve entrypoint for %s %q: %w", kind, name, err)
	}
	plugin.Command = install.executablePath
	plugin.Args = append([]string(nil), args...)
	return nil
}

func bindResolvedProviderManifest(name string, plugin *config.ProviderEntry, manifestPath string, manifest *providermanifestv1.Manifest, configMap map[string]any) error {
	manifest = providerpkg.ResolveManifestLocalReferences(manifest, manifestPath)
	if err := validateInstalledManifestKind(providermanifestv1.KindPlugin, name, manifest); err != nil {
		return err
	}
	if err := providerpkg.ValidateConfigForManifest(manifestPath, manifest, providermanifestv1.KindPlugin, configMap); err != nil {
		return fmt.Errorf("provider config validation for provider %q: %w", name, err)
	}
	resolveProviderIcon(manifest, manifestPath, plugin)
	plugin.ResolvedManifestPath = manifestPath
	plugin.ResolvedManifest = manifest
	return nil
}

func bindResolvedComponentManifest(kind, name string, plugin *config.ProviderEntry, manifestPath string, manifest *providermanifestv1.Manifest, configMap map[string]any) error {
	manifest = providerpkg.ResolveManifestLocalReferences(manifest, manifestPath)
	if err := validateInstalledManifestKind(kind, name, manifest); err != nil {
		return err
	}
	if err := providerpkg.ValidateConfigForManifest(manifestPath, manifest, kind, configMap); err != nil {
		return fmt.Errorf("provider config validation for %s %q: %w", kind, name, err)
	}
	resolveProviderIcon(manifest, manifestPath, plugin)
	plugin.ResolvedManifestPath = manifestPath
	plugin.ResolvedManifest = manifest
	return nil
}

func bindResolvedUIManifest(plugin *config.ProviderEntry, manifestPath string, manifest *providermanifestv1.Manifest, configMap map[string]any) error {
	manifest = providerpkg.ResolveManifestLocalReferences(manifest, manifestPath)
	if err := validateInstalledManifestKind(providermanifestv1.KindWebUI, "provider", manifest); err != nil {
		return err
	}
	if err := providerpkg.ValidateConfigForManifest(manifestPath, manifest, providermanifestv1.KindWebUI, configMap); err != nil {
		return fmt.Errorf("provider config validation for ui provider: %w", err)
	}
	resolveProviderIcon(manifest, manifestPath, plugin)
	plugin.ResolvedManifestPath = manifestPath
	plugin.ResolvedManifest = manifest
	return nil
}

func bindPathBackedUIManifest(plugin *config.ProviderEntry, configMap map[string]any) (string, error) {
	manifestPath := plugin.SourcePath()
	if strings.TrimSpace(manifestPath) == "" {
		return "", fmt.Errorf("resolved ui manifest path is required")
	}
	if _, err := os.Stat(manifestPath); err != nil {
		return "", fmt.Errorf("ui provider manifest not found at %s: %w", manifestPath, err)
	}
	_, manifest, err := providerpkg.ReadManifestFile(manifestPath)
	if err != nil {
		return "", fmt.Errorf("prepare manifest for ui provider: %w", err)
	}
	if err := bindResolvedUIManifest(plugin, manifestPath, manifest, configMap); err != nil {
		return "", err
	}
	assetRoot := filepath.Join(filepath.Dir(manifestPath), filepath.FromSlash(manifest.Spec.AssetRoot))
	if _, err := os.Stat(assetRoot); err != nil {
		return "", fmt.Errorf("ui provider asset root not found at %s: %w", assetRoot, err)
	}
	return assetRoot, nil
}

func (l *Lifecycle) materializeLockedProvider(ctx context.Context, paths initPaths, name string, plugin *config.ProviderEntry, entry LockProviderEntry, locked bool) error {
	platform := providerpkg.CurrentPlatformString()
	archive, resolvedKey, ok := resolveArchiveForPlatform(entry, platform)
	if !ok || archive.URL == "" {
		return fmt.Errorf("no archive for platform %s for provider %q; run `gestaltd init %s`", platform, name, paths.configFlags)
	}
	if locked && archive.SHA256 == "" {
		return fmt.Errorf("no verified hash for platform %s for provider %q; run `gestaltd init %s`", platform, name, formatPlatformInitFlags(paths, platform))
	}

	download, err := downloadArchiveForSource(ctx, l.metadataHTTPClient(), sourceAuthToken(plugin), archive.URL)
	if err != nil {
		return fmt.Errorf("download locked source provider for provider %q: %w", name, err)
	}
	defer download.Cleanup()
	if archive.SHA256 != "" && download.SHA256Hex != archive.SHA256 {
		return fmt.Errorf("locked source provider digest mismatch for provider %q: got %s, want %s", name, download.SHA256Hex, archive.SHA256)
	}

	destDir := providerDestDir(paths, name)
	installed, cleanupInstall, commitInstall, err := installLockedPackageAtomic(download.LocalPath, destDir)
	if err != nil {
		return fmt.Errorf("install locked source provider for provider %q: %w", name, err)
	}
	defer func() { _ = cleanupInstall() }()
	if installed.Manifest.Source != lockEntryPackage(entry) {
		return fmt.Errorf("locked source provider manifest source mismatch for provider %q: got %q, want %q", name, installed.Manifest.Source, lockEntryPackage(entry))
	}
	if installed.Manifest.Version != entry.Version {
		return fmt.Errorf("locked source provider manifest version mismatch for provider %q: got %q, want %q", name, installed.Manifest.Version, entry.Version)
	}
	if err := validateLockedArchivePolicy(fmt.Sprintf("provider %q", name), providermanifestv1.KindPlugin, installed.Manifest, entry, platform, resolvedKey); err != nil {
		return err
	}
	if err := commitInstall(); err != nil {
		return fmt.Errorf("activate locked source provider for provider %q: %w", name, err)
	}
	return nil
}

func (l *Lifecycle) materializeLockedComponent(ctx context.Context, paths initPaths, kind, name string, plugin *config.ProviderEntry, entry LockEntry, destDir string, locked bool) error {
	platform := providerpkg.CurrentPlatformString()
	archive, resolvedKey, ok := resolveArchiveForPlatform(entry, platform)
	if !ok || archive.URL == "" {
		return fmt.Errorf("no archive for platform %s for %s %q; run `gestaltd init %s`", platform, kind, name, paths.configFlags)
	}
	if locked && archive.SHA256 == "" {
		return fmt.Errorf("no verified hash for platform %s for %s %q; run `gestaltd init %s`", platform, kind, name, formatPlatformInitFlags(paths, platform))
	}

	download, err := downloadArchiveForSource(ctx, l.metadataHTTPClient(), sourceAuthToken(plugin), archive.URL)
	if err != nil {
		return fmt.Errorf("download locked source provider for %s %q: %w", kind, name, err)
	}
	defer download.Cleanup()
	if archive.SHA256 != "" && download.SHA256Hex != archive.SHA256 {
		return fmt.Errorf("locked source provider digest mismatch for %s %q: got %s, want %s", kind, name, download.SHA256Hex, archive.SHA256)
	}

	if destDir == "" {
		return fmt.Errorf("unsupported component %q", name)
	}
	installed, cleanupInstall, commitInstall, err := installLockedPackageAtomic(download.LocalPath, destDir)
	if err != nil {
		return fmt.Errorf("install locked source provider for %s %q: %w", kind, name, err)
	}
	defer func() { _ = cleanupInstall() }()
	if err := validateInstalledManifestKind(kind, name, installed.Manifest); err != nil {
		return err
	}
	if installed.Manifest.Source != lockEntryPackage(entry) {
		return fmt.Errorf("locked source provider manifest source mismatch for %s %q: got %q, want %q", kind, name, installed.Manifest.Source, lockEntryPackage(entry))
	}
	if installed.Manifest.Version != entry.Version {
		return fmt.Errorf("locked source provider manifest version mismatch for %s %q: got %q, want %q", kind, name, installed.Manifest.Version, entry.Version)
	}
	if err := validateLockedArchivePolicy(fmt.Sprintf("%s %q", kind, name), archivePolicyKind(kind), installed.Manifest, entry, platform, resolvedKey); err != nil {
		return err
	}
	if err := commitInstall(); err != nil {
		return fmt.Errorf("activate locked source provider for %s %q: %w", kind, name, err)
	}
	return nil
}

func (l *Lifecycle) materializeLockedUIProvider(ctx context.Context, paths initPaths, plugin *config.ProviderEntry, entry LockUIEntry, destDir string, locked bool) error {
	platform := providerpkg.CurrentPlatformString()
	archive, _, ok := resolveArchiveForPlatform(entry, platform)
	if !ok || archive.URL == "" {
		return fmt.Errorf("no archive for platform %s for ui provider; run `gestaltd init %s`", platform, paths.configFlags)
	}
	if locked && archive.SHA256 == "" {
		return fmt.Errorf("no verified hash for platform %s for ui provider; run `gestaltd init %s`", platform, formatPlatformInitFlags(paths, platform))
	}

	download, err := downloadArchiveForSource(ctx, l.metadataHTTPClient(), sourceAuthToken(plugin), archive.URL)
	if err != nil {
		return fmt.Errorf("download locked source for ui provider: %w", err)
	}
	defer download.Cleanup()
	if archive.SHA256 != "" && download.SHA256Hex != archive.SHA256 {
		return fmt.Errorf("locked source digest mismatch for ui provider: got %s, want %s", download.SHA256Hex, archive.SHA256)
	}

	if err := os.RemoveAll(destDir); err != nil {
		return fmt.Errorf("remove stale cache for ui provider: %w", err)
	}
	installed, err := pluginstore.Install(download.LocalPath, destDir)
	if err != nil {
		return fmt.Errorf("install locked source for ui provider: %w", err)
	}
	if err := validateInstalledManifestKind(providermanifestv1.KindWebUI, "ui provider", installed.Manifest); err != nil {
		return err
	}
	if installed.Manifest.Source != lockEntryPackage(entry) {
		return fmt.Errorf("locked source manifest source mismatch for ui provider: got %q, want %q", installed.Manifest.Source, lockEntryPackage(entry))
	}
	if installed.Manifest.Version != entry.Version {
		return fmt.Errorf("locked source manifest version mismatch for ui provider: got %q, want %q", installed.Manifest.Version, entry.Version)
	}
	return nil
}

func (l *Lifecycle) downloadPlatformArchives(ctx context.Context, lock *Lockfile, paths initPaths, platforms []struct{ GOOS, GOARCH, LibC string }, tokenForSource map[string]string) error {
	for _, plat := range platforms {
		platformKey := providerpkg.PlatformString(plat.GOOS, plat.GOARCH)
		if err := l.hashPlatformInEntries(ctx, lock, paths, platformKey, tokenForSource); err != nil {
			return err
		}
	}
	return nil
}

func (l *Lifecycle) hashPlatformInEntries(ctx context.Context, lock *Lockfile, paths initPaths, platformKey string, tokenForSource map[string]string) error {
	for _, kind := range providerLockKinds() {
		lockEntries := lockEntriesForProviderKind(lock, kind)
		for name := range lockEntries {
			entry := lockEntries[name]
			if err := l.hashArchiveEntry(ctx, kind, name, &entry, paths, platformKey, tokenForSource); err != nil {
				return err
			}
			lockEntries[name] = entry
		}
	}
	return nil
}

func (l *Lifecycle) hashArchiveEntry(ctx context.Context, kind, name string, entry *LockEntry, paths initPaths, platformKey string, tokenForSource map[string]string) error {
	if entry.Archives == nil {
		return nil
	}
	archive, resolvedKey, ok := resolveArchiveForPlatform(*entry, platformKey)
	if !ok || archive.URL == "" || archive.SHA256 != "" {
		return nil
	}
	policyKind := archivePolicyKind(kind)
	var manifest *providermanifestv1.Manifest
	if policyKind == providermanifestv1.KindPlugin {
		var manifestErr error
		manifest, manifestErr = readLockEntryManifest(paths, *entry, lockEntryDestDir(paths, kind, name))
		if manifestErr != nil {
			return fmt.Errorf("load manifest for %s: %w", archivePolicySubject(kind, name), manifestErr)
		}
	}
	if err := validateLockedArchivePolicy(archivePolicySubject(kind, name), policyKind, manifest, *entry, platformKey, resolvedKey); err != nil {
		return err
	}
	token := tokenForSource[entry.Source]
	dl, err := downloadArchiveForSource(ctx, l.metadataHTTPClient(), token, archive.URL)
	if err != nil {
		return fmt.Errorf("download archive for platform %s, source %s: %w", platformKey, entry.Source, err)
	}
	archive.SHA256 = dl.SHA256Hex
	dl.Cleanup()
	entry.Archives[resolvedKey] = archive
	return nil
}

func resolveProviderIcon(manifest *providermanifestv1.Manifest, manifestPath string, plugin *config.ProviderEntry) {
	if manifest.IconFile == "" {
		return
	}
	iconPath := filepath.Join(filepath.Dir(manifestPath), filepath.FromSlash(manifest.IconFile))
	if _, err := os.Stat(iconPath); err != nil {
		slog.Warn("provider icon_file not found", "path", iconPath, "error", err)
		return
	}
	plugin.ResolvedIconFile = iconPath
}

func providerEntrypointArgs(manifest *providermanifestv1.Manifest) ([]string, error) {
	entry := manifest.Entrypoint
	if entry == nil {
		return nil, fmt.Errorf("manifest does not define a provider entrypoint")
	}
	return append([]string(nil), entry.Args...), nil
}

func componentEntrypointArgs(manifest *providermanifestv1.Manifest, kind string) ([]string, error) {
	entry := providerpkg.EntrypointForKind(manifest, kind)
	if entry == nil {
		return nil, fmt.Errorf("manifest does not define a %s entrypoint", kind)
	}
	return append([]string(nil), entry.Args...), nil
}

func validateInstalledManifestKind(kind, name string, manifest *providermanifestv1.Manifest) error {
	if manifest == nil {
		return fmt.Errorf("manifest for %s %q is required", kind, name)
	}
	declared, err := providerpkg.ManifestKind(manifest)
	if err != nil {
		return fmt.Errorf("%s %q manifest is invalid: %w", kind, name, err)
	}
	expectedKind := archivePolicyKind(kind)
	if declared != expectedKind {
		return fmt.Errorf("%s %q manifest has kind %q, want %q", kind, name, declared, expectedKind)
	}
	return nil
}

func buildComponentRuntimeConfigNode(name, kind string, provider *config.ProviderEntry, providerConfig yaml.Node) (yaml.Node, error) {
	return config.BuildComponentRuntimeConfigNode(name, kind, provider, providerConfig)
}
