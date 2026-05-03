package operator

import (
	"bytes"
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
	"github.com/valon-technologies/gestalt/server/internal/providerregistry"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	pluginservice "github.com/valon-technologies/gestalt/server/services/plugins"
	"github.com/valon-technologies/gestalt/server/services/plugins/providerpkg"
	"gopkg.in/yaml.v3"
)

const (
	LockfileName                   = "gestalt.lock.json"
	PreparedProvidersDir           = ".gestaltd/providers"
	PreparedAuthDir                = ".gestaltd/auth"
	PreparedAuthorizationDir       = ".gestaltd/authorization"
	PreparedExternalCredentialsDir = ".gestaltd/external-credentials"
	PreparedSecretsDir             = ".gestaltd/secrets"
	PreparedTelemetryDir           = ".gestaltd/telemetry"
	PreparedAuditDir               = ".gestaltd/audit"
	PreparedCacheDir               = ".gestaltd/cache"
	PreparedWorkflowDir            = ".gestaltd/workflow"
	PreparedAgentDir               = ".gestaltd/agent"
	PreparedRuntimeDir             = ".gestaltd/runtime"
	PreparedUIDir                  = ".gestaltd/ui"

	platformKeyGeneric = "generic"
)

type Lockfile struct {
	Providers           map[string]LockEntry `json:"providers"`
	Authentication      map[string]LockEntry `json:"authentication,omitempty"`
	Authorization       map[string]LockEntry `json:"authorization,omitempty"`
	ExternalCredentials map[string]LockEntry `json:"externalCredentials,omitempty"`
	IndexedDBs          map[string]LockEntry `json:"indexeddbs,omitempty"`
	Caches              map[string]LockEntry `json:"cache,omitempty"`
	S3                  map[string]LockEntry `json:"s3,omitempty"`
	Workflows           map[string]LockEntry `json:"workflow,omitempty"`
	Agents              map[string]LockEntry `json:"agent,omitempty"`
	Runtimes            map[string]LockEntry `json:"runtime,omitempty"`
	Secrets             map[string]LockEntry `json:"secrets,omitempty"`
	Telemetry           map[string]LockEntry `json:"telemetry,omitempty"`
	Audit               map[string]LockEntry `json:"audit,omitempty"`
	UIs                 map[string]LockEntry `json:"ui,omitempty"`
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

type Lifecycle struct {
	configSecretResolver func(context.Context, *config.Config) error
	httpClient           *http.Client
	providerResolver     *providerregistry.Resolver
}

type StatePaths struct {
	ArtifactsDir string
	LockfilePath string
}

type artifactMode int

const (
	artifactModeMaterialize artifactMode = iota
	artifactModeCheck
	artifactModeReadOnly
)

func (m artifactMode) canMaterialize() bool {
	return m == artifactModeMaterialize
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

func (l *Lifecycle) WithProviderResolver(resolver *providerregistry.Resolver) *Lifecycle {
	l.providerResolver = resolver
	return l
}

func (l *Lifecycle) metadataHTTPClient() *http.Client {
	if l != nil && l.httpClient != nil {
		return l.httpClient
	}
	return http.DefaultClient
}

func (l *Lifecycle) providerPackageResolver() *providerregistry.Resolver {
	if l != nil && l.providerResolver != nil {
		return l.providerResolver
	}
	return &providerregistry.Resolver{Client: l.metadataHTTPClient()}
}

func (l *Lifecycle) PrepareAtPath(configPath string) (*Lockfile, error) {
	return l.PrepareAtPaths([]string{configPath})
}

func (l *Lifecycle) PrepareAtPaths(configPaths []string) (*Lockfile, error) {
	return l.PrepareAtPathsWithStatePaths(configPaths, StatePaths{})
}

func (l *Lifecycle) PrepareAtPathsWithStatePaths(configPaths []string, state StatePaths) (*Lockfile, error) {
	lock, _, _, err := l.prepareAtPathsAndWriteLock(configPaths, state)
	return lock, err
}

func (l *Lifecycle) LockAtPathsWithStatePaths(configPaths []string, state StatePaths) (*Lockfile, error) {
	return l.LockAtPathsWithPlatforms(configPaths, state, nil)
}

func (l *Lifecycle) LockAtPathsWithPlatforms(configPaths []string, state StatePaths, platforms []struct{ GOOS, GOARCH string }) (*Lockfile, error) {
	lock, cfg, paths, cleanup, err := l.prepareLockAtPathsInScratch(configPaths, state)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		return nil, err
	}
	if len(platforms) > 0 {
		tokenForSource := buildSourceTokenMap(cfg)
		if err := l.downloadPlatformArchives(context.Background(), lock, paths, platforms, tokenForSource); err != nil {
			return nil, err
		}
	}
	if err := WriteLockfile(paths.lockfilePath, lock); err != nil {
		return nil, err
	}
	slog.Info("wrote lockfile", "path", paths.lockfilePath)
	return providerLockfileFromLockfile(lock).toLockfile(), nil
}

func (l *Lifecycle) CheckLockAtPathsWithStatePaths(configPaths []string, state StatePaths, platforms []struct{ GOOS, GOARCH string }) error {
	lock, cfg, paths, cleanup, err := l.prepareLockAtPathsInScratch(configPaths, state)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		return err
	}
	if len(platforms) > 0 {
		tokenForSource := buildSourceTokenMap(cfg)
		if err := l.downloadPlatformArchives(context.Background(), lock, paths, platforms, tokenForSource); err != nil {
			return err
		}
	}
	expected, err := canonicalLockfileJSON(lock)
	if err != nil {
		return err
	}
	currentLock, err := ReadLockfile(paths.lockfilePath)
	if err != nil {
		return fmt.Errorf("lockfile is missing or unreadable; run `%s`: %w", formatLockCommand(paths), err)
	}
	current, err := canonicalLockfileJSON(currentLock)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, expected) {
		return fmt.Errorf("lockfile is out of date; run `%s`", formatLockCommand(paths))
	}
	return nil
}

func (l *Lifecycle) SyncAtPathsWithStatePaths(configPaths []string, state StatePaths) error {
	return l.syncAtPathsWithStatePaths(configPaths, state, artifactModeMaterialize)
}

func (l *Lifecycle) CheckSyncAtPathsWithStatePaths(configPaths []string, state StatePaths) error {
	return l.syncAtPathsWithStatePaths(configPaths, state, artifactModeCheck)
}

// PrepareAtPathWithPlatforms runs preparation and additionally downloads and hashes
// archives for the specified extra platforms.
func (l *Lifecycle) PrepareAtPathWithPlatforms(configPath, artifactsDir string, platforms []struct{ GOOS, GOARCH string }) (*Lockfile, error) {
	return l.PrepareAtPathsWithPlatforms([]string{configPath}, StatePaths{ArtifactsDir: artifactsDir}, platforms)
}

// PrepareAtPathsWithPlatforms runs preparation and additionally downloads and hashes
// archives for the specified extra platforms.
func (l *Lifecycle) PrepareAtPathsWithPlatforms(configPaths []string, state StatePaths, platforms []struct{ GOOS, GOARCH string }) (*Lockfile, error) {
	lock, cfg, paths, err := l.prepareAtPathsAndWriteLock(configPaths, state)
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

func (l *Lifecycle) prepareAtPathsAndWriteLock(configPaths []string, state StatePaths) (*Lockfile, *config.Config, lifecyclePaths, error) {
	lock, cfg, paths, err := l.prepareLockAtPaths(configPaths, state)
	if err != nil {
		return nil, nil, lifecyclePaths{}, err
	}
	if err := WriteLockfile(paths.lockfilePath, lock); err != nil {
		return nil, nil, lifecyclePaths{}, err
	}

	slog.Info("prepared locked artifacts", "providers", len(lock.Providers), "authentication", len(lock.Authentication), "authorization", len(lock.Authorization), "indexeddbs", len(lock.IndexedDBs), "cache", len(lock.Caches), "s3", len(lock.S3), "workflow", len(lock.Workflows), "agent", len(lock.Agents), "runtime", len(lock.Runtimes), "secrets", len(lock.Secrets), "telemetry", len(lock.Telemetry), "audit", len(lock.Audit), "uis", len(lock.UIs))
	slog.Info("wrote lockfile", "path", paths.lockfilePath)
	return lock, cfg, paths, nil
}

func (l *Lifecycle) prepareLockAtPathsInScratch(configPaths []string, state StatePaths) (*Lockfile, *config.Config, lifecyclePaths, func(), error) {
	scratchDir, err := os.MkdirTemp("", "gestaltd-lock-*")
	if err != nil {
		return nil, nil, lifecyclePaths{}, nil, fmt.Errorf("create lock scratch dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(scratchDir) }
	scratchState := state
	scratchState.ArtifactsDir = scratchDir
	lock, cfg, paths, err := l.prepareLockAtPaths(configPaths, scratchState, state)
	if err != nil {
		return nil, nil, lifecyclePaths{}, cleanup, err
	}
	return lock, cfg, paths, cleanup, nil
}

func (l *Lifecycle) prepareLockAtPaths(configPaths []string, state StatePaths, displayState ...StatePaths) (*Lockfile, *config.Config, lifecyclePaths, error) {
	cfg, err := config.LoadAllowMissingEnvPaths(configPaths)
	if err != nil {
		return nil, nil, lifecyclePaths{}, fmt.Errorf("loading config: %v", err)
	}
	if err := config.OverlayRemotePluginConfigPaths(configPaths, cfg); err != nil {
		return nil, nil, lifecyclePaths{}, fmt.Errorf("loading config: %v", err)
	}
	paths := resolveLifecyclePaths(configPaths, cfg, state)
	if len(displayState) > 0 {
		paths.configFlags = formatConfigStateFlags(configPaths, displayState[0])
	}
	lock, err := l.prepareRuntimeLockFromLoadedConfig(context.Background(), paths, cfg)
	if err != nil {
		return nil, nil, lifecyclePaths{}, err
	}
	if err := l.applyPreparedProviders(paths, lock, cfg, artifactModeMaterialize); err != nil {
		return nil, nil, lifecyclePaths{}, err
	}
	if err := config.ValidateResolvedStructure(cfg); err != nil {
		return nil, nil, lifecyclePaths{}, err
	}
	if err := pluginservice.ValidateEffectiveCatalogsAndDependencies(context.Background(), config.PluginValidationConfig(cfg)); err != nil {
		return nil, nil, lifecyclePaths{}, err
	}
	return lock, cfg, paths, nil
}

func (l *Lifecycle) prepareRuntimeLockFromLoadedConfig(ctx context.Context, paths lifecyclePaths, cfg *config.Config) (*Lockfile, error) {
	if err := l.resolvePackageSources(ctx, cfg); err != nil {
		return nil, err
	}
	secretsEntries, err := l.primeSecretsProviderForConfigResolution(ctx, paths, cfg, nil, artifactModeMaterialize)
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
	for name, entry := range cfg.Runtime.Providers {
		if entry == nil || !runtimeSourceBacked(entry) {
			continue
		}
		destDir := componentDestDir(paths, config.HostProviderKindRuntime, name)
		lockEntry, err := l.writeComponentArtifact(ctx, paths, providerManifestKind(config.HostProviderKindRuntime), name, destDir, &entry.ProviderEntry, entry.Config)
		if err != nil {
			return nil, err
		}
		lock.Runtimes[name] = lockEntry
	}
	if err := l.resolveConfiguredPlugins(paths, lock, cfg, artifactModeMaterialize); err != nil {
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
			if _, existed := existingUIEntries[name]; !existed && (entry.HasLocalSource() || entry.HasLocalReleaseSource()) {
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

func (l *Lifecycle) resolvePackageSources(ctx context.Context, cfg *config.Config) error {
	if cfg == nil {
		return nil
	}
	if !configHasPackageSources(cfg) {
		return nil
	}
	repos, err := providerRepositoriesForConfig(cfg)
	if err != nil {
		return err
	}
	resolveEntry := func(subject string, entry *config.ProviderEntry) error {
		if entry == nil || !entry.Source.IsPackage() || entry.Source.ResolvedPackageMetadataURL() != "" {
			return nil
		}
		reqRepos := cloneProviderRepositories(repos)
		if token := sourceAuthToken(entry); token != "" {
			for i := range reqRepos {
				if entry.Source.PackageRepo() == "" || reqRepos[i].Name == entry.Source.PackageRepo() {
					reqRepos[i].Token = token
				}
			}
		}
		resolved, err := l.providerPackageResolver().Resolve(ctx, providerregistry.ResolveRequest{
			Package:           entry.Source.PackageAddress(),
			VersionConstraint: entry.Source.PackageVersionConstraint(),
			RepositoryName:    entry.Source.PackageRepo(),
			Repositories:      reqRepos,
		})
		if err != nil {
			return fmt.Errorf("%s resolve provider package: %w", subject, err)
		}
		entry.Source.SetResolvedPackage(resolved.MetadataURL, resolved.Version)
		return nil
	}
	for name, entry := range cfg.Plugins {
		if err := resolveEntry("plugin "+strconv.Quote(name), entry); err != nil {
			return err
		}
	}
	for _, collection := range hostProviderCollections(cfg) {
		for name, entry := range collection.entries {
			if err := resolveEntry(string(collection.kind)+" "+strconv.Quote(name), entry); err != nil {
				return err
			}
		}
	}
	for name, entry := range cfg.Providers.IndexedDB {
		if err := resolveEntry(string(config.HostProviderKindIndexedDB)+" "+strconv.Quote(name), entry); err != nil {
			return err
		}
	}
	for name, entry := range cfg.Providers.S3 {
		if err := resolveEntry(providermanifestv1.KindS3+" "+strconv.Quote(name), entry); err != nil {
			return err
		}
	}
	for name, entry := range cfg.Runtime.Providers {
		if entry != nil {
			if err := resolveEntry("runtime "+strconv.Quote(name), &entry.ProviderEntry); err != nil {
				return err
			}
		}
	}
	for name, entry := range cfg.Providers.UI {
		if entry != nil {
			if err := resolveEntry("ui "+strconv.Quote(name), &entry.ProviderEntry); err != nil {
				return err
			}
		}
	}
	return nil
}

func configHasPackageSources(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	for _, entry := range cfg.Plugins {
		if entry != nil && entry.Source.IsPackage() {
			return true
		}
	}
	for _, collection := range hostProviderCollections(cfg) {
		for _, entry := range collection.entries {
			if entry != nil && entry.Source.IsPackage() {
				return true
			}
		}
	}
	for _, entry := range cfg.Providers.IndexedDB {
		if entry != nil && entry.Source.IsPackage() {
			return true
		}
	}
	for _, entry := range cfg.Providers.S3 {
		if entry != nil && entry.Source.IsPackage() {
			return true
		}
	}
	for _, entry := range cfg.Runtime.Providers {
		if entry != nil && entry.Source.IsPackage() {
			return true
		}
	}
	for _, entry := range cfg.Providers.UI {
		if entry != nil && entry.Source.IsPackage() {
			return true
		}
	}
	return false
}

func providerRepositoriesForConfig(cfg *config.Config) ([]providerregistry.NamedRepository, error) {
	byName := make(map[string]providerregistry.NamedRepository)
	order := []string{providerregistry.DefaultRepositoryName}
	for _, repo := range providerregistry.DefaultRepositories() {
		byName[repo.Name] = repo
	}
	if storePath := providerregistry.UserRepositoryStorePath(); storePath != "" {
		store, err := providerregistry.ReadRepositoryStore(storePath)
		if err != nil {
			return nil, fmt.Errorf("read provider repository store: %w", err)
		}
		userNames := slices.Sorted(maps.Keys(store.Repositories))
		for _, name := range userNames {
			repo := store.Repositories[name]
			if err := providerregistry.ValidateRepositoryName(name); err != nil {
				return nil, err
			}
			if _, ok := byName[name]; !ok {
				order = append(order, name)
			}
			byName[name] = providerregistry.NamedRepository{Name: name, URL: repo.URL, Token: repo.Token}
		}
	}
	projectNames := slices.Sorted(maps.Keys(cfg.ProviderRepositories))
	for _, name := range projectNames {
		repo := cfg.ProviderRepositories[name]
		if err := providerregistry.ValidateRepositoryName(name); err != nil {
			return nil, err
		}
		if _, ok := byName[name]; !ok {
			order = append(order, name)
		}
		byName[name] = providerregistry.NamedRepository{Name: name, URL: repo.URL}
	}
	out := make([]providerregistry.NamedRepository, 0, len(order))
	for _, name := range order {
		if repo, ok := byName[name]; ok {
			out = append(out, repo)
		}
	}
	return out, nil
}

func cloneProviderRepositories(repos []providerregistry.NamedRepository) []providerregistry.NamedRepository {
	if len(repos) == 0 {
		return nil
	}
	return append([]providerregistry.NamedRepository(nil), repos...)
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
	for _, entry := range cfg.Runtime.Providers {
		if entry != nil {
			addEntry(&entry.ProviderEntry)
		}
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
	return filepath.Join(dir, LockfileName)
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
	paths := resolveLifecyclePaths(configPaths, cfg, state)
	mode := artifactModeMaterialize
	if locked {
		mode = artifactModeReadOnly
	}
	secretsLock, secretsValidated, err := l.lockForSecretsBootstrap(configPaths, state, paths, cfg, locked)
	if err != nil {
		return nil, nil, err
	}
	if _, err := l.primeSecretsProviderForConfigResolution(context.Background(), paths, cfg, secretsLock, mode); err != nil {
		return nil, nil, err
	}
	if err := l.resolveConfigSecrets(context.Background(), cfg); err != nil {
		return nil, nil, err
	}
	if err := config.ValidateRuntime(cfg); err != nil {
		return nil, nil, err
	}

	dependenciesValidated, err := l.applyLockedProviders(configPaths, state, cfg, locked, secretsLock, mode)
	if err != nil {
		return nil, nil, err
	}
	if err := config.ValidateResolvedStructure(cfg); err != nil {
		return nil, nil, err
	}
	if !secretsValidated && !dependenciesValidated {
		if err := pluginservice.ValidateDependencies(context.Background(), config.PluginValidationConfig(cfg)); err != nil {
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
	paths := resolveLifecyclePaths(configPaths, cfg, state)
	lock, err := l.prepareRuntimeLockFromLoadedConfig(context.Background(), paths, cfg)
	if err != nil {
		return nil, err
	}
	if err := config.ValidateRuntime(cfg); err != nil {
		return nil, err
	}
	if err := l.applyPreparedProviders(paths, lock, cfg, artifactModeMaterialize); err != nil {
		return nil, err
	}
	if err := config.ValidateResolvedStructure(cfg); err != nil {
		return nil, err
	}
	if err := pluginservice.ValidateDependencies(context.Background(), config.PluginValidationConfig(cfg)); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (l *Lifecycle) syncAtPathsWithStatePaths(configPaths []string, state StatePaths, mode artifactMode) error {
	cfg, err := config.LoadAllowMissingEnvPaths(configPaths)
	if err != nil {
		return fmt.Errorf("loading config: %v", err)
	}
	if err := config.OverlayRemotePluginConfigPaths(configPaths, cfg); err != nil {
		return fmt.Errorf("loading config: %v", err)
	}
	paths := resolveLifecyclePaths(configPaths, cfg, state)
	lock, err := ReadLockfile(paths.lockfilePath)
	if err != nil {
		return fmt.Errorf("source-backed providers require lock metadata; run `%s`: %w", formatLockCommand(paths), err)
	}
	if !lockMetadataMatchesConfig(cfg, paths, lock) {
		return fmt.Errorf("lockfile is out of date; run `%s`", formatLockCommand(paths))
	}
	if _, err := l.primeSecretsProviderForConfigResolution(context.Background(), paths, cfg, lock, mode); err != nil {
		return err
	}
	if err := l.resolveConfigSecrets(context.Background(), cfg); err != nil {
		return err
	}
	if err := config.ValidateRuntime(cfg); err != nil {
		return err
	}
	if err := l.applyPreparedProviders(paths, lock, cfg, mode); err != nil {
		return err
	}
	if err := config.ValidateResolvedStructure(cfg); err != nil {
		return err
	}
	if err := pluginservice.ValidateEffectiveCatalogsAndDependencies(context.Background(), config.PluginValidationConfig(cfg)); err != nil {
		return err
	}
	return nil
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

func (l *Lifecycle) lockForSecretsBootstrap(configPaths []string, state StatePaths, paths lifecyclePaths, cfg *config.Config, locked bool) (*Lockfile, bool, error) {
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
	validatedDuringPrepare := false
	if !locked && (err != nil || !lockMatchesConfig(cfg, paths, lock) || configHasLocalProviderSources(cfg) || configHasMetadataProviderSources(cfg)) {
		lock, err = l.PrepareAtPathsWithStatePaths(configPaths, state)
		validatedDuringPrepare = err == nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("source-backed providers require lock metadata; run `%s` then `%s`: %w", formatLockCommand(paths), formatSyncLockedCommand(paths), err)
	}
	return lock, validatedDuringPrepare, nil
}

func (l *Lifecycle) primeSecretsProviderForConfigResolution(ctx context.Context, paths lifecyclePaths, cfg *config.Config, lock *Lockfile, mode artifactMode) (map[string]LockEntry, error) {
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
						return nil, lockMetadataStaleError(paths, "lock entry for %s %q is missing or stale", providermanifestv1.KindSecrets, name)
					}
					if err := l.applyLockedComponentEntry(paths, &lockEntry, providermanifestv1.KindSecrets, name, provider, configMap, secretsDestDir(paths, name), mode); err != nil {
						return nil, err
					}
				} else {
					entry, err := l.writeComponentArtifact(ctx, paths, providermanifestv1.KindSecrets, name, secretsDestDir(paths, name), provider, provider.Config)
					if err != nil {
						return nil, err
					}
					if err := l.applyLockedComponentEntry(paths, &entry, providermanifestv1.KindSecrets, name, provider, configMap, secretsDestDir(paths, name), artifactModeMaterialize); err != nil {
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

type lifecyclePaths struct {
	configPaths            []string
	configFlags            string
	lockFlags              string
	configPath             string
	configDir              string
	artifactsDir           string
	lockfilePath           string
	providersDir           string
	authDir                string
	authorizationDir       string
	externalCredentialsDir string
	secretsDir             string
	telemetryDir           string
	auditDir               string
	cacheDir               string
	workflowDir            string
	agentDir               string
	runtimeDir             string
	uiDir                  string
}

func primaryConfigPath(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	return paths[0]
}

func formatConfigStateFlags(paths []string, state StatePaths) string {
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

func formatLockFlags(paths []string, state StatePaths) string {
	if len(paths) == 0 && state.LockfilePath == "" {
		return ""
	}
	args := make([]string, 0, len(paths)*2+2)
	for _, path := range paths {
		args = append(args, "--config", path)
	}
	if state.LockfilePath != "" {
		args = append(args, "--lockfile", state.LockfilePath)
	}
	return strings.Join(args, " ")
}

func formatLockCommand(paths lifecyclePaths) string {
	args := strings.TrimSpace(paths.lockFlags)
	if args == "" {
		return "gestaltd lock"
	}
	return "gestaltd lock " + args
}

func formatSyncLockedCommand(paths lifecyclePaths) string {
	args := strings.TrimSpace(paths.configFlags)
	if args == "" {
		return "gestaltd sync --locked"
	}
	return "gestaltd sync --locked " + args
}

func preparedArtifactStaleError(paths lifecyclePaths, format string, args ...any) error {
	return fmt.Errorf(format+"; run `%s`", append(args, formatSyncLockedCommand(paths))...)
}

func lockMetadataStaleError(paths lifecyclePaths, format string, args ...any) error {
	return fmt.Errorf(format+"; run `%s`", append(args, formatLockCommand(paths))...)
}

type providerFingerprintInput struct {
	Name   string `json:"name"`
	Source string `json:"source,omitempty"`
	Path   string `json:"path,omitempty"`
	Digest string `json:"digest,omitempty"`
}

func sourceBacked(entry *config.ProviderEntry) bool {
	return entry != nil && (entry.HasRemoteSource() || entry.HasLocalSource() || entry.HasLocalReleaseSource())
}

func runtimeSourceBacked(entry *config.RuntimeProviderEntry) bool {
	return entry != nil && sourceBacked(&entry.ProviderEntry)
}

func hostProviderCollections(cfg *config.Config) []struct {
	kind    config.HostProviderKind
	entries map[string]*config.ProviderEntry
} {
	return []struct {
		kind    config.HostProviderKind
		entries map[string]*config.ProviderEntry
	}{
		{config.HostProviderKindAuthentication, cfg.Providers.Authentication},
		{config.HostProviderKindAuthorization, cfg.Providers.Authorization},
		{config.HostProviderKindExternalCredentials, cfg.Providers.ExternalCredentials},
		{config.HostProviderKindSecrets, cfg.Providers.Secrets},
		{config.HostProviderKindTelemetry, cfg.Providers.Telemetry},
		{config.HostProviderKindAudit, cfg.Providers.Audit},
		{config.HostProviderKindCache, cfg.Providers.Cache},
		{config.HostProviderKindWorkflow, cfg.Providers.Workflow},
		{config.HostProviderKindAgent, cfg.Providers.Agent},
	}
}

func lockEntriesForKind(lock *Lockfile, kind config.HostProviderKind) map[string]LockEntry {
	if lock == nil {
		return nil
	}
	switch kind {
	case config.HostProviderKindAuthentication:
		return lock.Authentication
	case config.HostProviderKindAuthorization:
		return lock.Authorization
	case config.HostProviderKindExternalCredentials:
		return lock.ExternalCredentials
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
	case config.HostProviderKindAgent:
		return lock.Agents
	case config.HostProviderKindRuntime:
		return lock.Runtimes
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
	for _, entry := range cfg.Runtime.Providers {
		if runtimeSourceBacked(entry) {
			return true
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
		if entry.HasLocalSource() || entry.HasLocalReleaseSource() {
			return true
		}
	}
	for _, collection := range hostProviderCollections(cfg) {
		for _, entry := range collection.entries {
			if entry != nil && (entry.HasLocalSource() || entry.HasLocalReleaseSource()) {
				return true
			}
		}
	}
	for _, entry := range cfg.Runtime.Providers {
		if entry != nil && (entry.HasLocalSource() || entry.HasLocalReleaseSource()) {
			return true
		}
	}
	for _, entry := range cfg.Providers.UI {
		if entry != nil && (entry.HasLocalSource() || entry.HasLocalReleaseSource()) {
			return true
		}
	}
	for _, def := range cfg.Providers.IndexedDB {
		if def != nil && (def.HasLocalSource() || def.HasLocalReleaseSource()) {
			return true
		}
	}
	for _, def := range cfg.Providers.S3 {
		if def != nil && (def.HasLocalSource() || def.HasLocalReleaseSource()) {
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
	for _, entry := range cfg.Runtime.Providers {
		if entry != nil && entry.HasMetadataSource() {
			return true
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

func resolveLifecyclePaths(configPaths []string, cfg *config.Config, state StatePaths) lifecyclePaths {
	configPath := primaryConfigPath(configPaths)
	configDir := filepath.Dir(configPath)
	artifactsDir := resolveArtifactsDir(configPath, cfg, state.ArtifactsDir)
	lockfilePath := resolveLockfilePath(configPath, state.LockfilePath)
	return lifecyclePaths{
		configPaths:            append([]string(nil), configPaths...),
		configFlags:            formatConfigStateFlags(configPaths, state),
		lockFlags:              formatLockFlags(configPaths, state),
		configPath:             configPath,
		configDir:              configDir,
		artifactsDir:           artifactsDir,
		lockfilePath:           lockfilePath,
		providersDir:           filepath.Join(artifactsDir, filepath.FromSlash(PreparedProvidersDir)),
		authDir:                filepath.Join(artifactsDir, filepath.FromSlash(PreparedAuthDir)),
		authorizationDir:       filepath.Join(artifactsDir, filepath.FromSlash(PreparedAuthorizationDir)),
		externalCredentialsDir: filepath.Join(artifactsDir, filepath.FromSlash(PreparedExternalCredentialsDir)),
		secretsDir:             filepath.Join(artifactsDir, filepath.FromSlash(PreparedSecretsDir)),
		telemetryDir:           filepath.Join(artifactsDir, filepath.FromSlash(PreparedTelemetryDir)),
		auditDir:               filepath.Join(artifactsDir, filepath.FromSlash(PreparedAuditDir)),
		cacheDir:               filepath.Join(artifactsDir, filepath.FromSlash(PreparedCacheDir)),
		workflowDir:            filepath.Join(artifactsDir, filepath.FromSlash(PreparedWorkflowDir)),
		agentDir:               filepath.Join(artifactsDir, filepath.FromSlash(PreparedAgentDir)),
		runtimeDir:             filepath.Join(artifactsDir, filepath.FromSlash(PreparedRuntimeDir)),
		uiDir:                  filepath.Join(artifactsDir, filepath.FromSlash(PreparedUIDir)),
	}
}

func lifecyclePathsForConfig(configPath string) lifecyclePaths {
	return resolveLifecyclePaths([]string{configPath}, nil, StatePaths{})
}

func providerDestDir(paths lifecyclePaths, name string) string {
	return filepath.Join(paths.providersDir, name)
}

func uiDestDir(paths lifecyclePaths, name string) string {
	return filepath.Join(paths.uiDir, name)
}

func authDestDir(paths lifecyclePaths, name string) string {
	return filepath.Join(paths.authDir, name)
}

func authorizationDestDir(paths lifecyclePaths, name string) string {
	return filepath.Join(paths.authorizationDir, name)
}

func externalCredentialsDestDir(paths lifecyclePaths, name string) string {
	return filepath.Join(paths.externalCredentialsDir, name)
}

func secretsDestDir(paths lifecyclePaths, name string) string {
	return filepath.Join(paths.secretsDir, name)
}

func telemetryDestDir(paths lifecyclePaths, name string) string {
	return filepath.Join(paths.telemetryDir, name)
}

func auditDestDir(paths lifecyclePaths, name string) string {
	return filepath.Join(paths.auditDir, name)
}

func cacheDestDir(paths lifecyclePaths, name string) string {
	return filepath.Join(paths.cacheDir, name)
}

func workflowDestDir(paths lifecyclePaths, name string) string {
	return filepath.Join(paths.workflowDir, name)
}

func agentDestDir(paths lifecyclePaths, name string) string {
	return filepath.Join(paths.agentDir, name)
}

func runtimeDestDir(paths lifecyclePaths, name string) string {
	return filepath.Join(paths.runtimeDir, name)
}

func indexeddbDestDir(paths lifecyclePaths, name string) string {
	return filepath.Join(paths.artifactsDir, "indexeddb", name)
}

func s3DestDir(paths lifecyclePaths, name string) string {
	return filepath.Join(paths.artifactsDir, "s3", name)
}

func componentDestDir(paths lifecyclePaths, kind config.HostProviderKind, name string) string {
	switch kind {
	case config.HostProviderKindAuthentication:
		return authDestDir(paths, name)
	case config.HostProviderKindAuthorization:
		return authorizationDestDir(paths, name)
	case config.HostProviderKindExternalCredentials:
		return externalCredentialsDestDir(paths, name)
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
	case config.HostProviderKindAgent:
		return agentDestDir(paths, name)
	case config.HostProviderKindRuntime:
		return runtimeDestDir(paths, name)
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
	case config.HostProviderKindAuthentication:
		return providermanifestv1.KindAuthentication
	case config.HostProviderKindAuthorization:
		return providermanifestv1.KindAuthorization
	case config.HostProviderKindExternalCredentials:
		return providermanifestv1.KindExternalCredentials
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
	case config.HostProviderKindAgent:
		return providermanifestv1.KindAgent
	case config.HostProviderKindRuntime:
		return providermanifestv1.KindRuntime
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

func canonicalLockfileJSON(lock *Lockfile) ([]byte, error) {
	data, err := json.MarshalIndent(providerLockfileFromLockfile(lock), "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func ReadLockfile(path string) (*Lockfile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var lock providerLockfile
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("parsing lockfile %s: %w", path, err)
	}
	if err := validateProviderLockfile(&lock); err != nil {
		return nil, err
	}
	return lock.toLockfile(), nil
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

func lockMatchesConfig(cfg *config.Config, paths lifecyclePaths, lock *Lockfile) bool {
	if lock == nil {
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
	for name, entry := range cfg.Runtime.Providers {
		if !runtimeSourceBacked(entry) {
			continue
		}
		lockEntry, found := lock.Runtimes[name]
		if !lockEntryMatches(paths, providermanifestv1.KindRuntime, name, &entry.ProviderEntry, lockEntry, found, runtimeDestDir(paths, name)) {
			return false
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
		if !uiLockEntryMetadataMatches(paths, name, &entry.ProviderEntry, lockEntry, true) {
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

func lockMetadataMatchesConfig(cfg *config.Config, paths lifecyclePaths, lock *Lockfile) bool {
	if lock == nil {
		return false
	}
	for name, entry := range cfg.Plugins {
		if !sourceBacked(entry) {
			continue
		}
		lockEntry, found := lock.Providers[name]
		if !lockEntryMetadataMatches(paths, providermanifestv1.KindPlugin, name, entry, lockEntry, found) {
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
			if !lockEntryMetadataMatches(paths, providerManifestKind(collection.kind), name, entry, lockEntry, found) {
				return false
			}
		}
	}
	for name, entry := range cfg.Runtime.Providers {
		if !runtimeSourceBacked(entry) {
			continue
		}
		lockEntry, found := lock.Runtimes[name]
		if !lockEntryMetadataMatches(paths, providermanifestv1.KindRuntime, name, &entry.ProviderEntry, lockEntry, found) {
			return false
		}
	}
	for name, entry := range cfg.Providers.IndexedDB {
		if !sourceBacked(entry) {
			continue
		}
		lockEntry, found := lock.IndexedDBs[name]
		if !lockEntryMetadataMatches(paths, providermanifestv1.KindIndexedDB, name, entry, lockEntry, found) {
			return false
		}
	}
	for name, entry := range cfg.Providers.S3 {
		if !sourceBacked(entry) {
			continue
		}
		lockEntry, found := lock.S3[name]
		if !lockEntryMetadataMatches(paths, providermanifestv1.KindS3, name, entry, lockEntry, found) {
			return false
		}
	}
	for name, entry := range cfg.Providers.UI {
		if entry == nil || !sourceBacked(&entry.ProviderEntry) {
			continue
		}
		lockEntry, found := lock.UIs[name]
		if !uiLockEntryMetadataMatches(paths, name, &entry.ProviderEntry, lockEntry, found) {
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
		Source: providerSourceFingerprintLocation(entry, configDir),
	}
	if entry.HasLocalSource() {
		input.Path = fingerprintLocalSourcePath(entry.SourcePath(), configDir)
		digest, err := fingerprintLocalSourceDigest(entry.SourcePath())
		if err != nil {
			return "", err
		}
		input.Digest = digest
	} else if entry.HasLocalReleaseSource() {
		digest, err := fingerprintLocalReleaseMetadataDigest(entry.SourceReleasePath())
		if err != nil {
			return "", err
		}
		input.Digest = digest
	}

	return hashProviderFingerprintInput(input)
}

func hashProviderFingerprintInput(input providerFingerprintInput) (string, error) {
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

func providerSourceLockLocation(entry *config.ProviderEntry, configDir string) string {
	if entry == nil {
		return ""
	}
	if entry.Source.IsPackage() {
		return strings.TrimSpace(entry.Source.ResolvedPackageMetadataURL())
	}
	if entry.HasRemoteSource() {
		return entry.SourceRemoteLocation()
	}
	if entry.HasLocalReleaseSource() {
		return fingerprintLocalSourcePath(entry.SourceReleasePath(), configDir)
	}
	return ""
}

func providerSourceFingerprintLocation(entry *config.ProviderEntry, configDir string) string {
	if entry == nil {
		return ""
	}
	if entry.Source.IsPackage() {
		return strings.Join([]string{
			"package",
			entry.Source.PackageRepo(),
			entry.Source.PackageAddress(),
			entry.Source.PackageVersionConstraint(),
		}, "\x00")
	}
	return providerSourceLockLocation(entry, configDir)
}

func lockEntrySourceMatchesProvider(paths lifecyclePaths, provider *config.ProviderEntry, entry LockEntry) bool {
	if provider == nil {
		return false
	}
	if provider.Source.IsPackage() {
		if strings.TrimSpace(entry.Package) != provider.Source.PackageAddress() {
			return false
		}
		return providerregistry.VersionSatisfiesConstraint(entry.Version, provider.Source.PackageVersionConstraint())
	}
	return entry.Source == providerSourceLockLocation(provider, paths.configDir)
}

func lockEntryFingerprintMatchesProvider(name string, provider *config.ProviderEntry, configDir string, entry LockEntry) (bool, error) {
	fingerprint, err := ProviderFingerprint(name, provider, configDir)
	if err != nil {
		return false, err
	}
	return entry.Fingerprint == fingerprint, nil
}

func resolveLockedArchiveLocation(configDir, sourceLocation, archiveRef string) (string, error) {
	if isRemoteReleaseMetadataLocation(sourceLocation) {
		return resolveArchiveSourceLocation(sourceLocation, archiveRef, nil)
	}
	metadataPath := resolveLockPath(configDir, sourceLocation)
	return resolveArchiveSourceLocation(metadataPath, archiveRef, nil)
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

func fingerprintLocalReleaseMetadataDigest(sourcePath string) (string, error) {
	payload, err := normalizedLocalReleaseMetadataFingerprintPayloadFromFile(sourcePath)
	if err != nil {
		return "", err
	}
	if digest, ok, err := fingerprintLocalReleaseSourceTreeDigest(sourcePath); err != nil {
		return "", err
	} else if ok {
		input := struct {
			SourceDigest string          `json:"sourceDigest"`
			Metadata     json.RawMessage `json:"metadata"`
		}{
			SourceDigest: digest,
			Metadata:     payload,
		}
		data, err := json.Marshal(input)
		if err != nil {
			return "", err
		}
		sum := sha256.Sum256(data)
		return hex.EncodeToString(sum[:]), nil
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func fingerprintLocalReleaseSourceTreeDigest(sourcePath string) (string, bool, error) {
	cleaned := filepath.Clean(sourcePath)
	if filepath.Base(cleaned) != "provider-release.yaml" {
		return "", false, nil
	}
	distDir := filepath.Dir(cleaned)
	if filepath.Base(distDir) != "dist" {
		return "", false, nil
	}
	sourceDir := filepath.Dir(distDir)

	var manifestPath string
	for _, name := range providerpkg.ManifestFiles {
		candidate := filepath.Join(sourceDir, name)
		if _, err := os.Stat(candidate); err == nil {
			manifestPath = candidate
			break
		} else if !os.IsNotExist(err) {
			return "", false, err
		}
	}
	if manifestPath == "" {
		return "", false, nil
	}

	_, manifest, err := providerpkg.PrepareSourceManifest(manifestPath)
	if err != nil {
		return "", false, err
	}
	digest, err := providerpkg.DirectoryDigest(sourceDir, manifestPath, manifest)
	if err != nil {
		return "", false, err
	}
	return digest, true, nil
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
	case providermanifestv1.KindUI:
		return fmt.Sprintf("ui provider %q", name)
	default:
		return fmt.Sprintf("%s %q", kind, name)
	}
}

func lockEntryDestDir(paths lifecyclePaths, kind, name string) string {
	switch kind {
	case providermanifestv1.KindPlugin:
		return providerDestDir(paths, name)
	case providermanifestv1.KindAuthentication:
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
	case providermanifestv1.KindUI:
		return uiDestDir(paths, name)
	default:
		return ""
	}
}

func readLockEntryManifest(paths lifecyclePaths, entry LockEntry, destDir string) (*providermanifestv1.Manifest, error) {
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

func lockEntryMatches(paths lifecyclePaths, kind, name string, providerEntry *config.ProviderEntry, entry LockEntry, found bool, destDir string) bool {
	if !lockEntryMetadataMatches(paths, kind, name, providerEntry, entry, found) {
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
		var err error
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

func lockEntryMetadataMatches(paths lifecyclePaths, kind, name string, providerEntry *config.ProviderEntry, entry LockEntry, found bool) bool {
	if !found {
		return false
	}
	fingerprintMatches, err := lockEntryFingerprintMatchesProvider(name, providerEntry, paths.configDir, entry)
	if err != nil || !fingerprintMatches {
		return false
	}
	if !lockEntrySourceMatchesProvider(paths, providerEntry, entry) {
		return false
	}
	if entry.Kind != "" && entry.Kind != kind {
		return false
	}
	return true
}

func uiLockEntryMetadataMatches(paths lifecyclePaths, name string, providerEntry *config.ProviderEntry, entry LockEntry, found bool) bool {
	if !found {
		return false
	}
	fingerprintMatches, err := lockEntryFingerprintMatchesProvider("ui:"+name, providerEntry, paths.configDir, entry)
	if err != nil || !fingerprintMatches {
		return false
	}
	if !lockEntrySourceMatchesProvider(paths, providerEntry, entry) {
		return false
	}
	if entry.Kind != "" && entry.Kind != providermanifestv1.KindUI {
		return false
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
// string using a fallback chain: exact match → generic.
func resolveArchiveForPlatform(entry LockEntry, platform string) (LockArchive, string, bool) {
	if a, ok := entry.Archives[platform]; ok {
		return a, platform, true
	}
	if a, ok := entry.Archives[platformKeyGeneric]; ok {
		return a, platformKeyGeneric, true
	}
	return LockArchive{}, "", false
}

func prepareLocalSourceInstall(kind, name, manifestPath, destDir string) (*preparedInstall, error) {
	_, cleanupInstall, commitInstall, err := stageLocalSourceInstall(kind, name, manifestPath, destDir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = cleanupInstall() }()
	if err := commitInstall(); err != nil {
		return nil, err
	}
	install, err := inspectPreparedInstall(destDir)
	if err != nil {
		return nil, fmt.Errorf("inspect prepared install for %s %q: %w", kind, name, err)
	}
	return install, nil
}

func stageLocalSourceInstall(kind, name, manifestPath, destDir string) (*preparedInstall, func() error, func() error, error) {
	if strings.TrimSpace(manifestPath) == "" {
		return nil, nil, nil, fmt.Errorf("manifest path for %s %q is required", kind, name)
	}
	if _, err := os.Stat(manifestPath); err != nil {
		return nil, nil, nil, fmt.Errorf("manifest for %s %q not found at %s: %w", kind, name, manifestPath, err)
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
	cleanupInstall := func() error {
		if cleanupDir == "" {
			return nil
		}
		return os.RemoveAll(cleanupDir)
	}

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
		_ = cleanupInstall()
		return nil, nil, nil, fmt.Errorf("prepare manifest for %s %q: %w", kind, name, err)
	}
	install, err := inspectPreparedInstall(tempDir)
	if err != nil {
		_ = cleanupInstall()
		return nil, nil, nil, fmt.Errorf("inspect prepared install for %s %q: %w", kind, name, err)
	}

	commitInstall := func() error {
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
		cleanupDir = ""
		if backupDir != "" {
			if err := os.RemoveAll(backupDir); err != nil {
				return fmt.Errorf("remove staged provider cache backup at %s: %w", backupDir, err)
			}
		}
		return nil
	}

	return install, cleanupInstall, commitInstall, nil
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

func localLockEntryFromPreparedInstall(paths lifecyclePaths, kind, name string, plugin *config.ProviderEntry, install *preparedInstall) (LockEntry, error) {
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

func localUILockEntryFromPreparedInstall(paths lifecyclePaths, name string, plugin *config.ProviderEntry, install *preparedInstall) (LockEntry, error) {
	fingerprint, err := NamedUIProviderFingerprint(name, plugin, paths.configDir)
	if err != nil {
		return LockEntry{}, fmt.Errorf("fingerprinting ui %q: %w", name, err)
	}
	manifestPath, err := relativePreparedPath(paths.artifactsDir, install.manifestPath)
	if err != nil {
		return LockEntry{}, fmt.Errorf("compute manifest path for ui %q: %w", name, err)
	}
	assetRoot, err := relativePreparedPath(paths.artifactsDir, install.assetRootPath)
	if err != nil {
		return LockEntry{}, fmt.Errorf("compute asset root path for ui %q: %w", name, err)
	}
	return LockEntry{
		Fingerprint: fingerprint,
		Manifest:    manifestPath,
		AssetRoot:   assetRoot,
	}, nil
}

func (l *Lifecycle) installMetadataSourcePackage(ctx context.Context, expectedKind, name, subject, destDir string, plugin *config.ProviderEntry, configDir string) (*installedPackage, LockEntry, error) {
	sourceLocation := plugin.SourceReleaseLocation()
	metadata, resolvedMetadataLocation, gitHubReleaseAssets, err := fetchProviderReleaseMetadata(ctx, l.metadataHTTPClient(), sourceLocation, sourceAuthToken(plugin))
	if err != nil {
		return nil, LockEntry{}, fmt.Errorf("%s fetch metadata %q: %w", subject, sourceLocation, err)
	}
	expectedManifestKind := archivePolicyKind(expectedKind)
	if metadata.Kind != expectedManifestKind {
		return nil, LockEntry{}, fmt.Errorf("%s metadata kind %q does not match expected kind %q", subject, metadata.Kind, expectedManifestKind)
	}
	archives, err := providerReleaseArchives(resolvedMetadataLocation, metadata, gitHubReleaseAssets)
	if err != nil {
		return nil, LockEntry{}, fmt.Errorf("%s resolve archive metadata %q: %w", subject, sourceLocation, err)
	}
	entry := LockEntry{
		Package:  metadata.Package,
		Kind:     metadata.Kind,
		Runtime:  metadata.Runtime,
		Source:   providerSourceLockLocation(plugin, configDir),
		Version:  metadata.Version,
		Archives: archives,
	}

	currentPlatform := providerpkg.CurrentPlatformString()
	archive, resolvedKey, ok := resolveArchiveForPlatform(entry, currentPlatform)
	if !ok || archive.URL == "" {
		return nil, LockEntry{}, fmt.Errorf("no archive for platform %s for %s; publish an explicit %s target or a generic package where allowed", currentPlatform, subject, currentPlatform)
	}
	archiveLocation, err := resolveLockedArchiveLocation(configDir, entry.Source, archive.URL)
	if err != nil {
		return nil, LockEntry{}, fmt.Errorf("resolve archive for %s: %w", subject, err)
	}
	download, err := downloadArchiveForSource(ctx, l.metadataHTTPClient(), sourceAuthToken(plugin), archiveLocation)
	if err != nil {
		return nil, LockEntry{}, fmt.Errorf("download metadata source package for %s: %w", subject, err)
	}
	defer download.Cleanup()
	if archive.SHA256 != "" && download.SHA256Hex != archive.SHA256 {
		return nil, LockEntry{}, fmt.Errorf("metadata source digest mismatch for %s: got %s, want %s", subject, download.SHA256Hex, archive.SHA256)
	}

	installed, err := installPackage(download.LocalPath, destDir)
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

func (l *Lifecycle) writeProviderArtifacts(ctx context.Context, cfg *config.Config, paths lifecyclePaths) (map[string]LockEntry, error) {
	written := make(map[string]LockEntry)
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

func (l *Lifecycle) writeComponentArtifact(ctx context.Context, paths lifecyclePaths, kind, name, destDir string, plugin *config.ProviderEntry, configNode yaml.Node) (LockEntry, error) {
	configMap, err := config.NodeToMap(configNode)
	if err != nil {
		return LockEntry{}, fmt.Errorf("decode provider config for %s %q: %w", kind, name, err)
	}
	return l.lockComponentEntryForSource(ctx, paths, kind, name, destDir, plugin, configMap)
}

func (l *Lifecycle) lockComponentEntryForSource(ctx context.Context, paths lifecyclePaths, kind, name, destDir string, plugin *config.ProviderEntry, configMap map[string]any) (LockEntry, error) {
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

	sourceLocation := plugin.SourceReleaseLocation()
	var (
		installed *installedPackage
		entry     LockEntry
		err       error
	)
	subject := fmt.Sprintf("%s %q", kind, name)
	if !plugin.HasReleaseMetadataSource() {
		return LockEntry{}, fmt.Errorf("%s %q source %q: only provider-release metadata sources and local manifest paths are supported", kind, name, sourceLocation)
	}
	installed, entry, err = l.installMetadataSourcePackage(ctx, kind, name, subject, destDir, plugin, paths.configDir)
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

func (l *Lifecycle) lockProviderEntryForSource(ctx context.Context, paths lifecyclePaths, name string, plugin *config.ProviderEntry, configMap map[string]any) (LockEntry, error) {
	if plugin != nil && plugin.HasLocalSource() {
		install, err := prepareLocalSourceInstall(providermanifestv1.KindPlugin, name, plugin.SourcePath(), providerDestDir(paths, name))
		if err != nil {
			return LockEntry{}, err
		}
		if err := validateInstalledManifestKind(providermanifestv1.KindPlugin, name, install.manifest); err != nil {
			return LockEntry{}, err
		}
		if err := providerpkg.ValidateConfigForManifest(install.manifestPath, install.manifest, providermanifestv1.KindPlugin, configMap); err != nil {
			return LockEntry{}, fmt.Errorf("provider config validation for provider %q: %w", name, err)
		}
		return localLockEntryFromPreparedInstall(paths, providermanifestv1.KindPlugin, name, plugin, install)
	}

	sourceLocation := plugin.SourceReleaseLocation()
	destDir := providerDestDir(paths, name)
	var (
		installed *installedPackage
		entry     LockEntry
		err       error
	)
	if !plugin.HasReleaseMetadataSource() {
		return LockEntry{}, fmt.Errorf("provider %q source %q: only provider-release metadata sources and local manifest paths are supported", name, sourceLocation)
	}
	installed, entry, err = l.installMetadataSourcePackage(ctx, providermanifestv1.KindPlugin, name, fmt.Sprintf("provider %q", name), destDir, plugin, paths.configDir)
	if err != nil {
		return LockEntry{}, err
	}

	if err := providerpkg.ValidateConfigForManifest(installed.ManifestPath, installed.Manifest, providermanifestv1.KindPlugin, configMap); err != nil {
		return LockEntry{}, fmt.Errorf("provider config validation for provider %q: %w", name, err)
	}
	fingerprint, err := ProviderFingerprint(name, plugin, paths.configDir)
	if err != nil {
		return LockEntry{}, fmt.Errorf("fingerprinting provider %q: %w", name, err)
	}
	manifestPath, err := filepath.Rel(paths.artifactsDir, installed.ManifestPath)
	if err != nil {
		return LockEntry{}, fmt.Errorf("compute manifest path for provider %q: %w", name, err)
	}
	executableRel := ""
	if installed.ExecutablePath != "" {
		executableRel, err = filepath.Rel(paths.artifactsDir, installed.ExecutablePath)
		if err != nil {
			return LockEntry{}, fmt.Errorf("compute executable path for provider %q: %w", name, err)
		}
	}
	entry.Fingerprint = fingerprint
	entry.Manifest = filepath.ToSlash(manifestPath)
	entry.Executable = filepath.ToSlash(executableRel)
	return entry, nil
}

func (l *Lifecycle) writeNamedUIProviderArtifact(ctx context.Context, paths lifecyclePaths, name string, plugin *config.ProviderEntry, destDir string, subject string) (LockEntry, error) {
	if plugin == nil || !sourceBacked(plugin) {
		return LockEntry{}, fmt.Errorf("%s requires source configuration", subject)
	}
	configMap, err := config.NodeToMap(plugin.Config)
	if err != nil {
		return LockEntry{}, fmt.Errorf("decode %s config: %w", subject, err)
	}
	fingerprint, err := NamedUIProviderFingerprint(name, plugin, paths.configDir)
	if err != nil {
		return LockEntry{}, fmt.Errorf("fingerprinting %s: %w", subject, err)
	}
	if plugin.HasLocalSource() {
		install, err := prepareLocalSourceInstall(providermanifestv1.KindUI, name, plugin.SourcePath(), destDir)
		if err != nil {
			return LockEntry{}, err
		}
		if err := validateInstalledManifestKind(providermanifestv1.KindUI, subject, install.manifest); err != nil {
			return LockEntry{}, err
		}
		if err := providerpkg.ValidateConfigForManifest(install.manifestPath, install.manifest, providermanifestv1.KindUI, configMap); err != nil {
			return LockEntry{}, fmt.Errorf("provider config validation for %s: %w", subject, err)
		}
		entry, err := localUILockEntryFromPreparedInstall(paths, name, plugin, install)
		if err != nil {
			return LockEntry{}, err
		}
		entry.Fingerprint = fingerprint
		return entry, nil
	}
	expectedPackage := plugin.SourceReleaseLocation()

	var (
		installed *installedPackage
		entry     LockEntry
		opErr     error
	)
	if !plugin.HasReleaseMetadataSource() {
		return LockEntry{}, fmt.Errorf("%s source %q: only provider-release metadata sources and local manifest paths are supported", subject, expectedPackage)
	}
	installed, entry, opErr = l.installMetadataSourcePackage(ctx, providermanifestv1.KindUI, name, subject, destDir, plugin, paths.configDir)
	if opErr != nil {
		return LockEntry{}, opErr
	}
	if err := providerpkg.ValidateConfigForManifest(installed.ManifestPath, installed.Manifest, providermanifestv1.KindUI, configMap); err != nil {
		return LockEntry{}, fmt.Errorf("provider config validation for %s: %w", subject, err)
	}
	manifestPath, err := filepath.Rel(paths.artifactsDir, installed.ManifestPath)
	if err != nil {
		return LockEntry{}, fmt.Errorf("compute manifest path for %s: %w", subject, err)
	}
	assetRoot, err := filepath.Rel(paths.artifactsDir, installed.AssetRoot)
	if err != nil {
		return LockEntry{}, fmt.Errorf("compute asset root path for %s: %w", subject, err)
	}
	entry.Fingerprint = fingerprint
	entry.Manifest = filepath.ToSlash(manifestPath)
	entry.AssetRoot = filepath.ToSlash(assetRoot)
	return entry, nil
}

func (l *Lifecycle) applyPreparedProviders(paths lifecyclePaths, lock *Lockfile, cfg *config.Config, mode artifactMode) error {
	if !configHasProviderLoading(cfg) {
		return nil
	}

	if err := l.resolveConfiguredPlugins(paths, lock, cfg, mode); err != nil {
		return err
	}
	if err := synthesizePluginOwnedUIEntries(cfg); err != nil {
		return err
	}
	for _, collection := range hostProviderCollections(cfg) {
		lockEntries := lockEntriesForKind(lock, collection.kind)
		for name, entry := range collection.entries {
			if entry == nil {
				continue
			}
			if err := l.applyComponentProvider(paths, lockEntries, providerManifestKind(collection.kind), name, entry, entry.Config, &entry.Config, componentDestDir(paths, collection.kind, name), mode); err != nil {
				return err
			}
		}
	}
	runtimeLocks := map[string]LockEntry(nil)
	if lock != nil {
		runtimeLocks = lock.Runtimes
	}
	for name, entry := range cfg.Runtime.Providers {
		if entry == nil {
			continue
		}
		if err := l.applyComponentProvider(paths, runtimeLocks, providermanifestv1.KindRuntime, name, &entry.ProviderEntry, entry.Config, &entry.Config, runtimeDestDir(paths, name), mode); err != nil {
			return err
		}
	}
	indexedDBLocks := map[string]LockEntry(nil)
	if lock != nil {
		indexedDBLocks = lock.IndexedDBs
	}
	for name, def := range cfg.Providers.IndexedDB {
		if def != nil {
			if err := l.applyComponentProvider(paths, indexedDBLocks, providermanifestv1.KindIndexedDB, name, def, def.Config, &def.Config, indexeddbDestDir(paths, name), mode); err != nil {
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
			if err := l.applyComponentProvider(paths, s3Locks, providermanifestv1.KindS3, name, def, def.Config, &def.Config, s3DestDir(paths, name), mode); err != nil {
				return err
			}
		}
	}
	for name, entry := range cfg.Providers.UI {
		if entry == nil {
			continue
		}
		var lockEntry *LockEntry
		if lock != nil {
			if le, ok := lock.UIs[name]; ok {
				lockEntry = &le
			}
		}
		resolvedAssetRoot, err := l.applyConfiguredUIProvider(paths, lockEntry, &entry.ProviderEntry, name, "ui "+strconv.Quote(name), uiDestDir(paths, name), mode)
		if err != nil {
			return err
		}
		entry.ResolvedAssetRoot = resolvedAssetRoot
	}

	return nil
}

func (l *Lifecycle) applyLockedProviders(configPaths []string, state StatePaths, cfg *config.Config, locked bool, bootstrapLock *Lockfile, mode artifactMode) (bool, error) {
	if !configHasProviderLoading(cfg) {
		return false, nil
	}

	paths := resolveLifecyclePaths(configPaths, cfg, state)
	lock := bootstrapLock
	var err error
	validatedDuringPrepare := false
	if lock == nil {
		lock, err = ReadLockfile(paths.lockfilePath)
	}
	if !locked && (err != nil || !lockMatchesConfig(cfg, paths, lock) || (bootstrapLock == nil && configHasLocalProviderSources(cfg)) || (bootstrapLock == nil && configHasMetadataProviderSources(cfg))) {
		lock, err = l.PrepareAtPathsWithStatePaths(configPaths, state)
		validatedDuringPrepare = err == nil
	}
	if err != nil {
		return false, fmt.Errorf("source-backed providers require lock metadata; run `%s` then `%s`: %w", formatLockCommand(paths), formatSyncLockedCommand(paths), err)
	}
	if err := l.applyPreparedProviders(paths, lock, cfg, mode); err != nil {
		return false, err
	}
	return validatedDuringPrepare, nil
}

func installLockedPackageAtomic(packagePath, destDir string) (*installedPackage, func() error, func() error, error) {
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
	installed, err := installPackage(packagePath, tempDir)
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

func (l *Lifecycle) resolveConfiguredPlugins(paths lifecyclePaths, lock *Lockfile, cfg *config.Config, mode artifactMode) error {
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
			if err := l.applyLockedProviderEntry(paths, lock, name, entry, configMap, mode); err != nil {
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
			return fmt.Errorf("plugin %q ui.path requires spec.ui or plugins.%s.ui.bundle", pluginName, pluginName)
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
	if current := strings.TrimSpace(existing.SourceReleaseLocation()); current != "" {
		return fmt.Errorf("config validation: plugins.%s owned ui conflicts with providers.ui.%s.source", pluginName, pluginName)
	}
	if current := strings.TrimSpace(existing.Source.Path); current != "" && !equivalentProviderManifestPath(current, expected.Source.Path) {
		return fmt.Errorf("config validation: plugins.%s owned ui conflicts with providers.ui.%s.source.path", pluginName, pluginName)
	}
	if current := strings.TrimSpace(existing.Path); current != "" && current != expected.Path {
		return fmt.Errorf("config validation: plugins.%s.ui.path %q conflicts with providers.ui.%s.path", pluginName, expected.Path, pluginName)
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

func (l *Lifecycle) applyConfiguredUIProvider(paths lifecyclePaths, lockEntry *LockEntry, provider *config.ProviderEntry, logicalName, subject, destDir string, mode artifactMode) (string, error) {
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
			return "", lockMetadataStaleError(paths, "lock entry for %s is missing or stale", subject)
		}
		if !lockEntrySourceMatchesProvider(paths, provider, *lockEntry) {
			return "", lockMetadataStaleError(paths, "lock entry for %s is stale", subject)
		}
		fingerprintMatches, err := lockEntryFingerprintMatchesProvider("ui:"+logicalName, provider, paths.configDir, *lockEntry)
		if err != nil {
			return "", fmt.Errorf("fingerprinting %s: %w", subject, err)
		}
		var stagedInstall *preparedInstall
		var cleanupStaged func() error
		var commitStaged func() error
		defer func() {
			if cleanupStaged != nil {
				_ = cleanupStaged()
			}
		}()
		if !fingerprintMatches {
			return "", lockMetadataStaleError(paths, "lock entry for %s is stale", subject)
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
			if !mode.canMaterialize() {
				return "", preparedArtifactStaleError(paths, "prepared artifact for %s is missing or stale", subject)
			}
			if len(lockEntry.Archives) == 0 {
				if !provider.HasLocalSource() {
					return "", preparedArtifactStaleError(paths, "prepared artifact for %s is missing or stale", subject)
				}
				if stagedInstall == nil {
					stagedInstall, cleanupStaged, commitStaged, err = stageLocalSourceInstall(providermanifestv1.KindUI, logicalName, provider.SourcePath(), destDir)
					if err != nil {
						return "", err
					}
					fingerprint, err := NamedUIProviderFingerprint(logicalName, provider, paths.configDir)
					if err != nil {
						return "", fmt.Errorf("fingerprinting %s: %w", subject, err)
					}
					if lockEntry.Fingerprint != fingerprint {
						return "", lockMetadataStaleError(paths, "lock entry for %s is stale", subject)
					}
				}
				if !preparedManifestMatchesLock(*lockEntry, stagedInstall.manifest) {
					return "", lockMetadataStaleError(paths, "lock entry for %s is stale", subject)
				}
				if err := commitStaged(); err != nil {
					return "", err
				}
			} else {
				if err := l.materializeLockedUIProvider(context.Background(), paths, provider, *lockEntry, destDir); err != nil {
					return "", err
				}
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

func (l *Lifecycle) applyComponentProvider(paths lifecyclePaths, lockEntries map[string]LockEntry, kind, name string, provider *config.ProviderEntry, providerConfig yaml.Node, targetNode *yaml.Node, destDir string, mode artifactMode) error {
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
			return lockMetadataStaleError(paths, "lock entry for %s %q is missing or stale", kind, name)
		}
		lockEntry, ok := lockEntries[name]
		if !ok {
			return lockMetadataStaleError(paths, "lock entry for %s %q is missing or stale", kind, name)
		}
		if err := l.applyLockedComponentEntry(paths, &lockEntry, kind, name, provider, configMap, destDir, mode); err != nil {
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

func (l *Lifecycle) applyLockedProviderEntry(paths lifecyclePaths, lock *Lockfile, name string, plugin *config.ProviderEntry, configMap map[string]any, mode artifactMode) error {
	if lock == nil {
		return lockMetadataStaleError(paths, "lock entry for provider %q is missing or stale", name)
	}
	entry, ok := lock.Providers[name]
	if !ok {
		return lockMetadataStaleError(paths, "lock entry for provider %q is missing or stale", name)
	}
	if !lockEntrySourceMatchesProvider(paths, plugin, entry) {
		return lockMetadataStaleError(paths, "lock entry for provider %q is stale", name)
	}
	fingerprintMatches, err := lockEntryFingerprintMatchesProvider(name, plugin, paths.configDir, entry)
	if err != nil {
		return fmt.Errorf("fingerprinting provider %q: %w", name, err)
	}

	destDir := providerDestDir(paths, name)
	var stagedInstall *preparedInstall
	var cleanupStaged func() error
	var commitStaged func() error
	defer func() {
		if cleanupStaged != nil {
			_ = cleanupStaged()
		}
	}()
	if !fingerprintMatches {
		return lockMetadataStaleError(paths, "lock entry for provider %q is stale", name)
	}

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
		if !mode.canMaterialize() {
			return preparedArtifactStaleError(paths, "prepared artifact for provider %q is missing or stale", name)
		}
		if len(entry.Archives) == 0 {
			if !plugin.HasLocalSource() {
				return preparedArtifactStaleError(paths, "prepared artifact for provider %q is missing or stale", name)
			}
			if stagedInstall == nil {
				stagedInstall, cleanupStaged, commitStaged, err = stageLocalSourceInstall(providermanifestv1.KindPlugin, name, plugin.SourcePath(), destDir)
				if err != nil {
					return err
				}
				fingerprint, err := ProviderFingerprint(name, plugin, paths.configDir)
				if err != nil {
					return fmt.Errorf("fingerprinting provider %q: %w", name, err)
				}
				if entry.Fingerprint != fingerprint {
					return lockMetadataStaleError(paths, "lock entry for provider %q is stale", name)
				}
			}
			if !preparedManifestMatchesLock(entry, stagedInstall.manifest) {
				return lockMetadataStaleError(paths, "lock entry for provider %q is stale", name)
			}
			if err := commitStaged(); err != nil {
				return err
			}
		} else {
			if err := l.materializeLockedProvider(context.Background(), paths, name, plugin, entry); err != nil {
				return err
			}
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
			return preparedArtifactStaleError(paths, "prepared executable for provider %q not found at %s", name, install.executablePath)
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

func (l *Lifecycle) applyLockedComponentEntry(paths lifecyclePaths, entry *LockEntry, kind, name string, plugin *config.ProviderEntry, configMap map[string]any, destDir string, mode artifactMode) error {
	if entry == nil {
		return lockMetadataStaleError(paths, "lock entry for %s %q is missing or stale", kind, name)
	}
	if !lockEntrySourceMatchesProvider(paths, plugin, *entry) {
		return lockMetadataStaleError(paths, "lock entry for %s %q is stale", kind, name)
	}
	fingerprintMatches, err := lockEntryFingerprintMatchesProvider(name, plugin, paths.configDir, *entry)
	if err != nil {
		return fmt.Errorf("fingerprinting %s %q provider: %w", kind, name, err)
	}

	var stagedInstall *preparedInstall
	var cleanupStaged func() error
	var commitStaged func() error
	defer func() {
		if cleanupStaged != nil {
			_ = cleanupStaged()
		}
	}()
	if !fingerprintMatches {
		return lockMetadataStaleError(paths, "lock entry for %s %q is stale", kind, name)
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
		if !mode.canMaterialize() {
			return preparedArtifactStaleError(paths, "prepared artifact for %s %q is missing or stale", kind, name)
		}
		if len(entry.Archives) == 0 {
			if !plugin.HasLocalSource() {
				return preparedArtifactStaleError(paths, "prepared artifact for %s %q is missing or stale", kind, name)
			}
			if stagedInstall == nil {
				stagedInstall, cleanupStaged, commitStaged, err = stageLocalSourceInstall(kind, name, plugin.SourcePath(), destDir)
				if err != nil {
					return err
				}
				fingerprint, err := ProviderFingerprint(name, plugin, paths.configDir)
				if err != nil {
					return fmt.Errorf("fingerprinting %s %q provider: %w", kind, name, err)
				}
				if entry.Fingerprint != fingerprint {
					return lockMetadataStaleError(paths, "lock entry for %s %q is stale", kind, name)
				}
			}
			if !preparedManifestMatchesLock(*entry, stagedInstall.manifest) {
				return lockMetadataStaleError(paths, "lock entry for %s %q is stale", kind, name)
			}
			if err := commitStaged(); err != nil {
				return err
			}
		} else {
			if err := l.materializeLockedComponent(context.Background(), paths, kind, name, plugin, *entry, destDir); err != nil {
				return err
			}
		}
		install, err = inspectPreparedInstall(destDir)
		if err != nil {
			return fmt.Errorf("read prepared manifest for %s %q: %w", kind, name, err)
		}
	}
	if install.executablePath == "" {
		return preparedArtifactStaleError(paths, "prepared executable for %s %q not found in %s", kind, name, destDir)
	}
	if err := bindResolvedComponentManifest(kind, name, plugin, install.manifestPath, install.manifest, configMap); err != nil {
		return err
	}
	if _, err := os.Stat(install.executablePath); err != nil {
		return preparedArtifactStaleError(paths, "prepared executable for %s %q not found at %s", kind, name, install.executablePath)
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
	if err := validateInstalledManifestKind(providermanifestv1.KindUI, "provider", manifest); err != nil {
		return err
	}
	if err := providerpkg.ValidateConfigForManifest(manifestPath, manifest, providermanifestv1.KindUI, configMap); err != nil {
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

func (l *Lifecycle) materializeLockedProvider(ctx context.Context, paths lifecyclePaths, name string, plugin *config.ProviderEntry, entry LockEntry) error {
	platform := providerpkg.CurrentPlatformString()
	archive, resolvedKey, ok := resolveArchiveForPlatform(entry, platform)
	if !ok || archive.URL == "" {
		return fmt.Errorf("no archive for platform %s for provider %q; run `%s --platform %s`", platform, name, formatLockCommand(paths), platform)
	}
	archiveLocation, err := resolveLockedArchiveLocation(paths.configDir, entry.Source, archive.URL)
	if err != nil {
		return fmt.Errorf("resolve locked source provider for provider %q: %w", name, err)
	}
	if archive.SHA256 == "" && archiveReferenceNeedsIntegrityHash(archiveLocation) {
		return fmt.Errorf("no verified hash for platform %s for provider %q; run `%s --platform %s`", platform, name, formatLockCommand(paths), platform)
	}
	download, err := downloadArchiveForSource(ctx, l.metadataHTTPClient(), sourceAuthToken(plugin), archiveLocation)
	if err != nil {
		return fmt.Errorf("download locked source provider for provider %q: %w", name, err)
	}
	defer download.Cleanup()
	if archive.SHA256 != "" && download.SHA256Hex != archive.SHA256 {
		return fmt.Errorf("locked source provider digest mismatch for provider %q: got %s, want %s", name, download.SHA256Hex, archive.SHA256)
	}
	if archive.SHA256 == "" {
		sourceLocation := entry.Source
		if !isRemoteReleaseMetadataLocation(sourceLocation) {
			sourceLocation = resolveLockPath(paths.configDir, sourceLocation)
		}
		expectedSHA, err := localReleaseArchiveExpectedSHA(sourceLocation, resolvedKey, archiveLocation)
		if err != nil {
			return fmt.Errorf("load local archive metadata for provider %q: %w", name, err)
		}
		if expectedSHA != "" && download.SHA256Hex != expectedSHA {
			return fmt.Errorf("locked source provider digest mismatch for provider %q: got %s, want %s", name, download.SHA256Hex, expectedSHA)
		}
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

func (l *Lifecycle) materializeLockedComponent(ctx context.Context, paths lifecyclePaths, kind, name string, plugin *config.ProviderEntry, entry LockEntry, destDir string) error {
	platform := providerpkg.CurrentPlatformString()
	archive, resolvedKey, ok := resolveArchiveForPlatform(entry, platform)
	if !ok || archive.URL == "" {
		return fmt.Errorf("no archive for platform %s for %s %q; run `%s --platform %s`", platform, kind, name, formatLockCommand(paths), platform)
	}
	archiveLocation, err := resolveLockedArchiveLocation(paths.configDir, entry.Source, archive.URL)
	if err != nil {
		return fmt.Errorf("resolve locked source provider for %s %q: %w", kind, name, err)
	}
	if archive.SHA256 == "" && archiveReferenceNeedsIntegrityHash(archiveLocation) {
		return fmt.Errorf("no verified hash for platform %s for %s %q; run `%s --platform %s`", platform, kind, name, formatLockCommand(paths), platform)
	}
	download, err := downloadArchiveForSource(ctx, l.metadataHTTPClient(), sourceAuthToken(plugin), archiveLocation)
	if err != nil {
		return fmt.Errorf("download locked source provider for %s %q: %w", kind, name, err)
	}
	defer download.Cleanup()
	if archive.SHA256 != "" && download.SHA256Hex != archive.SHA256 {
		return fmt.Errorf("locked source provider digest mismatch for %s %q: got %s, want %s", kind, name, download.SHA256Hex, archive.SHA256)
	}
	if archive.SHA256 == "" {
		sourceLocation := entry.Source
		if !isRemoteReleaseMetadataLocation(sourceLocation) {
			sourceLocation = resolveLockPath(paths.configDir, sourceLocation)
		}
		expectedSHA, err := localReleaseArchiveExpectedSHA(sourceLocation, resolvedKey, archiveLocation)
		if err != nil {
			return fmt.Errorf("load local archive metadata for %s %q: %w", kind, name, err)
		}
		if expectedSHA != "" && download.SHA256Hex != expectedSHA {
			return fmt.Errorf("locked source provider digest mismatch for %s %q: got %s, want %s", kind, name, download.SHA256Hex, expectedSHA)
		}
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

func (l *Lifecycle) materializeLockedUIProvider(ctx context.Context, paths lifecyclePaths, plugin *config.ProviderEntry, entry LockEntry, destDir string) error {
	platform := providerpkg.CurrentPlatformString()
	archive, resolvedKey, ok := resolveArchiveForPlatform(entry, platform)
	if !ok || archive.URL == "" {
		return fmt.Errorf("no archive for platform %s for ui provider; run `%s --platform %s`", platform, formatLockCommand(paths), platform)
	}
	archiveLocation, err := resolveLockedArchiveLocation(paths.configDir, entry.Source, archive.URL)
	if err != nil {
		return fmt.Errorf("resolve locked source for ui provider: %w", err)
	}
	if archive.SHA256 == "" && archiveReferenceNeedsIntegrityHash(archiveLocation) {
		return fmt.Errorf("no verified hash for platform %s for ui provider; run `%s --platform %s`", platform, formatLockCommand(paths), platform)
	}
	download, err := downloadArchiveForSource(ctx, l.metadataHTTPClient(), sourceAuthToken(plugin), archiveLocation)
	if err != nil {
		return fmt.Errorf("download locked source for ui provider: %w", err)
	}
	defer download.Cleanup()
	if archive.SHA256 != "" && download.SHA256Hex != archive.SHA256 {
		return fmt.Errorf("locked source digest mismatch for ui provider: got %s, want %s", download.SHA256Hex, archive.SHA256)
	}
	if archive.SHA256 == "" {
		sourceLocation := entry.Source
		if !isRemoteReleaseMetadataLocation(sourceLocation) {
			sourceLocation = resolveLockPath(paths.configDir, sourceLocation)
		}
		expectedSHA, err := localReleaseArchiveExpectedSHA(sourceLocation, resolvedKey, archiveLocation)
		if err != nil {
			return fmt.Errorf("load local archive metadata for ui provider: %w", err)
		}
		if expectedSHA != "" && download.SHA256Hex != expectedSHA {
			return fmt.Errorf("locked source digest mismatch for ui provider: got %s, want %s", download.SHA256Hex, expectedSHA)
		}
	}

	if err := os.RemoveAll(destDir); err != nil {
		return fmt.Errorf("remove stale cache for ui provider: %w", err)
	}
	installed, err := installPackage(download.LocalPath, destDir)
	if err != nil {
		return fmt.Errorf("install locked source for ui provider: %w", err)
	}
	if err := validateInstalledManifestKind(providermanifestv1.KindUI, "ui provider", installed.Manifest); err != nil {
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

func (l *Lifecycle) downloadPlatformArchives(ctx context.Context, lock *Lockfile, paths lifecyclePaths, platforms []struct{ GOOS, GOARCH string }, tokenForSource map[string]string) error {
	for _, plat := range platforms {
		platformKey := providerpkg.PlatformString(plat.GOOS, plat.GOARCH)
		if err := l.hashPlatformInEntries(ctx, lock, paths, platformKey, tokenForSource); err != nil {
			return err
		}
	}
	return nil
}

func (l *Lifecycle) hashPlatformInEntries(ctx context.Context, lock *Lockfile, paths lifecyclePaths, platformKey string, tokenForSource map[string]string) error {
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

func (l *Lifecycle) hashArchiveEntry(ctx context.Context, kind, name string, entry *LockEntry, paths lifecyclePaths, platformKey string, tokenForSource map[string]string) error {
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
	archiveLocation, err := resolveLockedArchiveLocation(paths.configDir, entry.Source, archive.URL)
	if err != nil {
		return fmt.Errorf("resolve archive for platform %s, source %s: %w", platformKey, entry.Source, err)
	}
	if !archiveReferenceNeedsIntegrityHash(archiveLocation) {
		return nil
	}
	token := tokenForSource[entry.Source]
	dl, err := downloadArchiveForSource(ctx, l.metadataHTTPClient(), token, archiveLocation)
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
