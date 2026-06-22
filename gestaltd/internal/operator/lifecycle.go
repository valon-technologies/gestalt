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

	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/providerregistry"
	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
	"github.com/valon-technologies/gestalt/server/internal/staticvalidation"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	appservice "github.com/valon-technologies/gestalt/server/services/apps"
	"github.com/valon-technologies/gestalt/server/services/apps/packageio"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
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

	platformKeyGeneric = providerrelease.GenericTarget
)

type Lockfile struct {
	Schema        string              `json:"schema"`
	SchemaVersion int                 `json:"schemaVersion"`
	Revision      int                 `json:"revision"`
	Providers     providerLockBuckets `json:"providers"`
}

// LockArchive records a platform-specific archive URL and optional integrity hash.
type LockArchive struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256,omitempty"`
}

type LockSourceRef struct {
	Type               string `json:"type,omitempty"`
	Repo               string `json:"repo,omitempty"`
	Ref                string `json:"ref,omitempty"`
	Path               string `json:"path,omitempty"`
	ArtifactRepository string `json:"artifactRepository,omitempty"`
	Materialization    string `json:"materialization,omitempty"`
	ResolvedGestaltRef string `json:"resolvedGestaltRef,omitempty"`
}

type LockEntry struct {
	InputDigest        string                       `json:"inputDigest,omitempty"`
	Package            string                       `json:"package,omitempty"`
	Kind               string                       `json:"kind,omitempty"`
	Runtime            string                       `json:"runtime,omitempty"`
	Source             string                       `json:"source,omitempty"`
	SourceRef          *LockSourceRef               `json:"sourceRef,omitempty"`
	Version            string                       `json:"version,omitempty"`
	Archives           map[string]LockArchive       `json:"archives,omitempty"`
	ValidationManifest *providermanifestv1.Manifest `json:"manifest,omitempty"`
	CatalogAvailable   bool                         `json:"catalogAvailable,omitempty"`
	CatalogFingerprint string                       `json:"catalogFingerprint,omitempty"`
	CatalogSessionOnly bool                         `json:"catalogSessionOnly,omitempty"`
	ArtifactManifest   string                       `json:"-"`
	Executable         string                       `json:"-"`
	AssetRoot          string                       `json:"-"`
}

type StaticValidationOptions struct {
	Platform string
}

type Lifecycle struct {
	configSecretResolver     func(context.Context, *config.Config) error
	sourceAuthSecretResolver func(context.Context, *config.Config) error
	httpClient               *http.Client
	providerResolver         *providerregistry.Resolver
	// devServeEligible enables manifest dev: activation. Set true only for an
	// unlocked `gestaltd serve`; false for lock/sync/validate and serve --locked,
	// so committed lockfiles build and pin dev UIs normally.
	devServeEligible bool
	// forcedDevUIKeys marks specific UI keys dev-active even when devServeEligible
	// is false (serve --locked --config … --path … overlay mode).
	forcedDevUIKeys map[string]bool
}

type StatePaths struct {
	ArtifactsDir string
	LockfilePath string
}

type SyncOptions struct {
	Parallelism   int
	CacheDir      string
	Observability SyncObservability
}

func DefaultSyncParallelism() int {
	parallelism := runtime.GOMAXPROCS(0)
	if parallelism > 4 {
		parallelism = 4
	}
	if parallelism < 1 {
		parallelism = 1
	}
	return parallelism
}

func normalizeSyncOptions(opts SyncOptions) SyncOptions {
	if opts.Parallelism == 0 {
		opts.Parallelism = DefaultSyncParallelism()
	}
	return opts
}

type artifactMode int

const (
	artifactModeMaterialize artifactMode = iota
	artifactModeCheck
	artifactModeReadOnly
)

type configSecretResolutionMode int

const (
	configSecretResolutionAll configSecretResolutionMode = iota
	configSecretResolutionSourceAuth
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

// WithSourceAuthSecretResolver installs a resolver for provider source.auth.token
// secret refs. Artifact preparation uses this narrower resolver so runtime-only
// secrets are not required during lock/sync.
func (l *Lifecycle) WithSourceAuthSecretResolver(resolve func(context.Context, *config.Config) error) *Lifecycle {
	l.sourceAuthSecretResolver = resolve
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

func (l *Lifecycle) WithDevServeEligible(v bool) *Lifecycle {
	l.devServeEligible = v
	return l
}

func (l *Lifecycle) WithForcedDevUIKeys(keys []string) *Lifecycle {
	if len(keys) == 0 {
		l.forcedDevUIKeys = nil
		return l
	}
	l.forcedDevUIKeys = make(map[string]bool, len(keys))
	for _, key := range keys {
		if key = strings.TrimSpace(key); key != "" {
			l.forcedDevUIKeys[key] = true
		}
	}
	return l
}

func (l *Lifecycle) shouldMarkUIForDev(name string) bool {
	if l == nil {
		return false
	}
	if l.forcedDevUIKeys[name] {
		return true
	}
	return l.devServeEligible
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
	lock, _, paths, cleanup, err := l.prepareCommittedLockAtPathsInScratch(configPaths, state)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		return nil, err
	}
	if err := WriteLockfile(paths.lockfilePath, lock); err != nil {
		return nil, err
	}
	slog.Info("wrote lockfile", "path", paths.lockfilePath)
	return normalizeLockfile(lock), nil
}

func (l *Lifecycle) CheckLockAtPathsWithStatePaths(configPaths []string, state StatePaths) error {
	lock, cfg, paths, cleanup, err := l.prepareCommittedLockAtPathsInScratch(configPaths, state)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		return err
	}
	expected, err := canonicalLockfileJSON(lock)
	if err != nil {
		return err
	}
	currentLock, err := ReadLockfile(paths.lockfilePath)
	if err != nil {
		if os.IsNotExist(err) && !configRequiresCommittedLock(cfg) {
			return nil
		}
		return fmt.Errorf("lockfile is missing or unreadable; run `%s`: %w", formatLockCommand(paths), err)
	}
	current, err := canonicalLockfileJSON(currentLock)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, expected) {
		driftReport := formatLockDriftReport(diagnoseLockfileDrift(lock, currentLock))
		if driftReport != "" {
			return fmt.Errorf("lockfile is out of date; run `%s`\n%s", formatLockCommand(paths), driftReport)
		}
		return fmt.Errorf("lockfile is out of date; run `%s`", formatLockCommand(paths))
	}
	return nil
}

func (l *Lifecycle) SyncAtPathsWithStatePaths(configPaths []string, state StatePaths) error {
	return l.SyncAtPathsWithStatePathsOptions(configPaths, state, SyncOptions{})
}

func (l *Lifecycle) CheckSyncAtPathsWithStatePaths(configPaths []string, state StatePaths) error {
	return l.CheckSyncAtPathsWithStatePathsOptions(configPaths, state, SyncOptions{})
}

func (l *Lifecycle) SyncAtPathsWithStatePathsOptions(configPaths []string, state StatePaths, opts SyncOptions) error {
	return l.syncAtPathsWithStatePaths(configPaths, state, artifactModeMaterialize, opts)
}

func (l *Lifecycle) CheckSyncAtPathsWithStatePathsOptions(configPaths []string, state StatePaths, opts SyncOptions) error {
	return l.syncAtPathsWithStatePaths(configPaths, state, artifactModeCheck, opts)
}

func (l *Lifecycle) prepareAtPathsAndWriteLock(configPaths []string, state StatePaths) (*Lockfile, *config.Config, lifecyclePaths, error) {
	lock, cfg, paths, err := l.prepareLockAtPaths(configPaths, state)
	if err != nil {
		return nil, nil, lifecyclePaths{}, err
	}
	if err := WriteLockfile(paths.lockfilePath, lock); err != nil {
		return nil, nil, lifecyclePaths{}, err
	}

	slog.Info("prepared locked artifacts", "providers", len(lock.Providers.App), "authentication", len(lock.Providers.Identity), "authorization", len(lock.Providers.Authorization), "indexeddbs", len(lock.Providers.IndexedDB), "cache", len(lock.Providers.Cache), "s3", len(lock.Providers.S3), "workflow", len(lock.Providers.Workflow), "agent", len(lock.Providers.Agent), "runtime", len(lock.Providers.Runtime), "secrets", len(lock.Providers.Secrets), "telemetry", len(lock.Providers.Telemetry), "audit", len(lock.Providers.Audit), "uis", len(lock.Providers.UI))
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
	actualPaths := resolveLifecyclePaths(configPaths, cfg, state)
	paths.lockfilePath = actualPaths.lockfilePath
	paths.lockFlags = actualPaths.lockFlags
	return lock, cfg, paths, cleanup, nil
}

func (l *Lifecycle) prepareCommittedLockAtPathsInScratch(configPaths []string, state StatePaths) (*Lockfile, *config.Config, lifecyclePaths, func(), error) {
	scratchDir, err := os.MkdirTemp("", "gestaltd-lock-*")
	if err != nil {
		return nil, nil, lifecyclePaths{}, nil, fmt.Errorf("create lock scratch dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(scratchDir) }
	scratchState := state
	scratchState.ArtifactsDir = scratchDir
	lock, cfg, paths, err := l.prepareCommittedLockAtPaths(configPaths, scratchState, state)
	if err != nil {
		return nil, nil, lifecyclePaths{}, cleanup, err
	}
	actualPaths := resolveLifecyclePaths(configPaths, cfg, state)
	paths.lockfilePath = actualPaths.lockfilePath
	paths.lockFlags = actualPaths.lockFlags
	return lock, cfg, paths, cleanup, nil
}

func (l *Lifecycle) prepareCommittedLockAtPaths(configPaths []string, state StatePaths, displayState ...StatePaths) (*Lockfile, *config.Config, lifecyclePaths, error) {
	cfg, err := l.loadConfigForLifecycle(configPaths, state, true)
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
	if l.sourceAuthSecretResolver == nil {
		if err := requireSourceAuthSecretResolver(cfg); err != nil {
			return nil, nil, lifecyclePaths{}, err
		}
	}
	if err := rejectLocalSourceSecretsForSourceAuthLock(cfg); err != nil {
		return nil, nil, lifecyclePaths{}, err
	}
	lock, err := l.prepareCommittedRuntimeLockFromLoadedConfig(context.Background(), paths, cfg)
	if err != nil {
		return nil, nil, lifecyclePaths{}, err
	}
	if err := validateResolvedStructureForCommittedLock(paths, cfg); err != nil {
		return nil, nil, lifecyclePaths{}, err
	}
	catalogs, err := effectiveCatalogsForCommittedLock(context.Background(), cfg, lock)
	if err != nil {
		return nil, nil, lifecyclePaths{}, err
	}
	if err := attachStaticValidationMetadata(lock, cfg, catalogs); err != nil {
		return nil, nil, lifecyclePaths{}, err
	}
	return lock, cfg, paths, nil
}

func (l *Lifecycle) prepareLockAtPaths(configPaths []string, state StatePaths, displayState ...StatePaths) (*Lockfile, *config.Config, lifecyclePaths, error) {
	cfg, err := l.loadConfigForLifecycle(configPaths, state, true)
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
	lock, err := l.prepareRuntimeLockFromLoadedConfigWithSecretMode(context.Background(), paths, cfg, configSecretResolutionSourceAuth)
	if err != nil {
		return nil, nil, lifecyclePaths{}, err
	}
	if err := l.applyPreparedProviders(paths, lock, cfg, artifactModeMaterialize, SyncOptions{Parallelism: 1}); err != nil {
		return nil, nil, lifecyclePaths{}, err
	}
	if err := config.ValidateResolvedStructure(cfg); err != nil {
		return nil, nil, lifecyclePaths{}, err
	}
	catalogs, err := appservice.EffectiveCatalogsAndDependencies(context.Background(), config.AppValidationConfig(cfg))
	if err != nil {
		return nil, nil, lifecyclePaths{}, err
	}
	if err := attachStaticValidationMetadata(lock, cfg, catalogs); err != nil {
		return nil, nil, lifecyclePaths{}, err
	}
	return lock, cfg, paths, nil
}

func (l *Lifecycle) prepareRuntimeLockFromLoadedConfig(ctx context.Context, paths lifecyclePaths, cfg *config.Config) (*Lockfile, error) {
	return l.prepareRuntimeLockFromLoadedConfigWithSecretMode(ctx, paths, cfg, configSecretResolutionAll)
}

func (l *Lifecycle) prepareCommittedRuntimeLockFromLoadedConfig(ctx context.Context, paths lifecyclePaths, cfg *config.Config) (*Lockfile, error) {
	secretsEntries, err := l.primeSecretsProviderForConfigResolution(ctx, paths, cfg, nil, artifactModeMaterialize, configSecretResolutionSourceAuth)
	if err != nil {
		return nil, err
	}
	if err := l.resolveConfigSecretsForMode(ctx, cfg, configSecretResolutionSourceAuth); err != nil {
		return nil, err
	}
	if err := l.resolvePackageSources(ctx, cfg); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(paths.providersDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating providers dir: %w", err)
	}

	lock := newLockfile()
	for name, entry := range cfg.Apps {
		if !providerRequiresCommittedLock(entry) {
			continue
		}
		configMap, err := config.NodeToMap(entry.Config)
		if err != nil {
			return nil, fmt.Errorf("decode provider config for provider %q: %w", name, err)
		}
		lockEntry, err := l.resolveLockedProvider(ctx, cfg, paths, providermanifestv1.KindApp, name, fmt.Sprintf("provider %q", name), providerDestDir(paths, name), entry, configMap, artifactModeCheck)
		if err != nil {
			return nil, err
		}
		lock.Providers.App[name] = lockEntry
		if !lockEntryStaticValidationOnly(lockEntry) {
			if err := l.applyLockedProviderEntry(paths, lock, name, entry, configMap, artifactModeCheck); err != nil {
				return nil, err
			}
		}
	}

	existingUIEntries := make(map[string]struct{}, len(cfg.Providers.UI))
	for name := range cfg.Providers.UI {
		existingUIEntries[name] = struct{}{}
	}
	if err := synthesizeCommittedPluginOwnedUIEntries(cfg); err != nil {
		return nil, err
	}
	for name := range secretsEntries {
		if providerRequiresCommittedLock(cfg.Providers.Secrets[name]) {
			lock.Providers.Secrets[name] = secretsEntries[name]
		}
	}
	for _, collection := range hostProviderCollections(cfg) {
		for name, entry := range collection.entries {
			if !providerRequiresCommittedLock(entry) {
				continue
			}
			if collection.kind == config.HostProviderKindSecrets {
				if _, alreadyPrepared := secretsEntries[name]; alreadyPrepared {
					continue
				}
			}
			kind := providerManifestKind(collection.kind)
			destDir := componentDestDir(paths, collection.kind, name)
			configMap, err := config.NodeToMap(entry.Config)
			if err != nil {
				return nil, fmt.Errorf("decode provider config for %s %q: %w", kind, name, err)
			}
			lockEntry, err := l.resolveLockedProvider(ctx, cfg, paths, kind, name, fmt.Sprintf("%s %q", kind, name), destDir, entry, configMap, artifactModeCheck)
			if err != nil {
				return nil, err
			}
			lockEntriesForKind(lock, collection.kind)[name] = lockEntry
			if !lockEntryStaticValidationOnly(lockEntry) {
				if err := l.applyLockedComponentEntry(paths, &lockEntry, kind, name, entry, configMap, destDir, artifactModeCheck); err != nil {
					return nil, err
				}
			}
		}
	}
	for name, entry := range cfg.Runtime.Providers {
		if !runtimeRequiresCommittedLock(entry) {
			continue
		}
		kind := providermanifestv1.KindRuntime
		destDir := runtimeDestDir(paths, name)
		configMap, err := config.NodeToMap(entry.Config)
		if err != nil {
			return nil, fmt.Errorf("decode provider config for %s %q: %w", kind, name, err)
		}
		lockEntry, err := l.resolveLockedProvider(ctx, cfg, paths, kind, name, fmt.Sprintf("%s %q", kind, name), destDir, &entry.ProviderEntry, configMap, artifactModeCheck)
		if err != nil {
			return nil, err
		}
		lock.Providers.Runtime[name] = lockEntry
		if !lockEntryStaticValidationOnly(lockEntry) {
			if err := l.applyLockedComponentEntry(paths, &lockEntry, kind, name, &entry.ProviderEntry, configMap, destDir, artifactModeCheck); err != nil {
				return nil, err
			}
		}
	}
	for name, def := range cfg.Providers.IndexedDB {
		if !providerRequiresCommittedLock(def) {
			continue
		}
		kind := providermanifestv1.KindIndexedDB
		destDir := indexeddbDestDir(paths, name)
		configMap, err := config.NodeToMap(def.Config)
		if err != nil {
			return nil, fmt.Errorf("decode provider config for %s %q: %w", kind, name, err)
		}
		lockEntry, err := l.resolveLockedProvider(ctx, cfg, paths, kind, name, fmt.Sprintf("%s %q", kind, name), destDir, def, configMap, artifactModeCheck)
		if err != nil {
			return nil, err
		}
		lock.Providers.IndexedDB[name] = lockEntry
		if !lockEntryStaticValidationOnly(lockEntry) {
			if err := l.applyLockedComponentEntry(paths, &lockEntry, kind, name, def, configMap, destDir, artifactModeCheck); err != nil {
				return nil, err
			}
		}
	}
	for name, def := range cfg.Providers.S3 {
		if !providerRequiresCommittedLock(def) {
			continue
		}
		kind := providermanifestv1.KindS3
		destDir := s3DestDir(paths, name)
		configMap, err := config.NodeToMap(def.Config)
		if err != nil {
			return nil, fmt.Errorf("decode provider config for %s %q: %w", kind, name, err)
		}
		lockEntry, err := l.resolveLockedProvider(ctx, cfg, paths, kind, name, fmt.Sprintf("%s %q", kind, name), destDir, def, configMap, artifactModeCheck)
		if err != nil {
			return nil, err
		}
		lock.Providers.S3[name] = lockEntry
		if !lockEntryStaticValidationOnly(lockEntry) {
			if err := l.applyLockedComponentEntry(paths, &lockEntry, kind, name, def, configMap, destDir, artifactModeCheck); err != nil {
				return nil, err
			}
		}
	}
	for name, entry := range cfg.Providers.UI {
		if entry == nil {
			continue
		}
		if entry.DevActive {
			continue
		}
		if _, existed := existingUIEntries[name]; !existed && entry.HasLocalSource() && pathWithinRoot(filepath.Join(paths.artifactsDir, ".gestaltd"), entry.SourcePath()) {
			if _, err := l.applyConfiguredUIProvider(paths, nil, &entry.ProviderEntry, name, "ui "+strconv.Quote(name), uiDestDir(paths, name), artifactModeCheck); err != nil {
				return nil, err
			}
			continue
		}
		if !providerRequiresCommittedLock(&entry.ProviderEntry) {
			continue
		}
		configMap, err := config.NodeToMap(entry.Config)
		if err != nil {
			return nil, fmt.Errorf("decode ui %q config: %w", name, err)
		}
		lockEntry, err := l.resolveLockedProvider(ctx, cfg, paths, providermanifestv1.KindUI, name, "ui "+strconv.Quote(name), uiDestDir(paths, name), &entry.ProviderEntry, configMap, artifactModeCheck)
		if err != nil {
			return nil, err
		}
		lock.Providers.UI[name] = lockEntry
		if !lockEntryStaticValidationOnly(lockEntry) {
			if _, err := l.applyConfiguredUIProvider(paths, &lockEntry, &entry.ProviderEntry, name, "ui "+strconv.Quote(name), uiDestDir(paths, name), artifactModeCheck); err != nil {
				return nil, err
			}
		}
	}
	return lock, nil
}

func (l *Lifecycle) prepareRuntimeLockFromLoadedConfigWithSecretMode(ctx context.Context, paths lifecyclePaths, cfg *config.Config, secretMode configSecretResolutionMode) (*Lockfile, error) {
	var secretsEntries map[string]LockEntry
	var err error
	secretsEntries, err = l.primeSecretsProviderForConfigResolution(ctx, paths, cfg, nil, artifactModeMaterialize, configSecretResolutionSourceAuth)
	if err != nil {
		return nil, err
	}
	if err := l.resolveConfigSecretsForMode(ctx, cfg, configSecretResolutionSourceAuth); err != nil {
		return nil, err
	}
	if err := l.resolvePackageSources(ctx, cfg); err != nil {
		return nil, err
	}
	if secretMode == configSecretResolutionAll {
		allSecretsEntries, err := l.primeSecretsProviderForConfigResolution(ctx, paths, cfg, nil, artifactModeMaterialize, secretMode)
		if err != nil {
			return nil, err
		}
		for name := range allSecretsEntries {
			if secretsEntries == nil {
				secretsEntries = make(map[string]LockEntry, len(allSecretsEntries))
			}
			secretsEntries[name] = allSecretsEntries[name]
		}
		if err := l.resolveConfigSecretsForMode(ctx, cfg, secretMode); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(paths.providersDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating providers dir: %w", err)
	}

	lock := newLockfile()
	for name, entry := range cfg.Apps {
		if entry == nil || !sourceBacked(entry) {
			continue
		}
		configMap, err := config.NodeToMap(entry.Config)
		if err != nil {
			return nil, fmt.Errorf("decode provider config for provider %q: %w", name, err)
		}
		lockEntry, err := l.resolveLockedProvider(ctx, cfg, paths, providermanifestv1.KindApp, name, fmt.Sprintf("provider %q", name), providerDestDir(paths, name), entry, configMap, artifactModeMaterialize)
		if err != nil {
			return nil, err
		}
		if providerRequiresCommittedLock(entry) {
			lock.Providers.App[name] = lockEntry
		}
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
			configMap, err := config.NodeToMap(entry.Config)
			if err != nil {
				return nil, fmt.Errorf("decode provider config for %s %q: %w", providerManifestKind(collection.kind), name, err)
			}
			lockEntry, err := l.resolveLockedProvider(ctx, cfg, paths, providerManifestKind(collection.kind), name, fmt.Sprintf("%s %q", providerManifestKind(collection.kind), name), destDir, entry, configMap, artifactModeMaterialize)
			if err != nil {
				return nil, err
			}
			if providerRequiresCommittedLock(entry) {
				lockEntriesForKind(lock, collection.kind)[name] = lockEntry
			}
		}
	}
	for name, entry := range cfg.Runtime.Providers {
		if entry == nil || !runtimeSourceBacked(entry) {
			continue
		}
		destDir := componentDestDir(paths, config.HostProviderKindRuntime, name)
		configMap, err := config.NodeToMap(entry.Config)
		if err != nil {
			return nil, fmt.Errorf("decode provider config for %s %q: %w", providerManifestKind(config.HostProviderKindRuntime), name, err)
		}
		lockEntry, err := l.resolveLockedProvider(ctx, cfg, paths, providerManifestKind(config.HostProviderKindRuntime), name, fmt.Sprintf("%s %q", providerManifestKind(config.HostProviderKindRuntime), name), destDir, &entry.ProviderEntry, configMap, artifactModeMaterialize)
		if err != nil {
			return nil, err
		}
		if runtimeRequiresCommittedLock(entry) {
			lock.Providers.Runtime[name] = lockEntry
		}
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
		if providerRequiresCommittedLock(cfg.Providers.Secrets[name]) {
			lock.Providers.Secrets[name] = secretsEntries[name]
		}
	}
	for name, def := range cfg.Providers.IndexedDB {
		if sourceBacked(def) {
			configMap, err := config.NodeToMap(def.Config)
			if err != nil {
				return nil, fmt.Errorf("decode provider config for %s %q: %w", providermanifestv1.KindIndexedDB, name, err)
			}
			entry, err := l.resolveLockedProvider(ctx, cfg, paths, providermanifestv1.KindIndexedDB, name, fmt.Sprintf("%s %q", providermanifestv1.KindIndexedDB, name), indexeddbDestDir(paths, name), def, configMap, artifactModeMaterialize)
			if err != nil {
				return nil, err
			}
			if providerRequiresCommittedLock(def) {
				lock.Providers.IndexedDB[name] = entry
			}
		}
	}
	for name, def := range cfg.Providers.S3 {
		if sourceBacked(def) {
			configMap, err := config.NodeToMap(def.Config)
			if err != nil {
				return nil, fmt.Errorf("decode provider config for %s %q: %w", providermanifestv1.KindS3, name, err)
			}
			entry, err := l.resolveLockedProvider(ctx, cfg, paths, providermanifestv1.KindS3, name, fmt.Sprintf("%s %q", providermanifestv1.KindS3, name), s3DestDir(paths, name), def, configMap, artifactModeMaterialize)
			if err != nil {
				return nil, err
			}
			if providerRequiresCommittedLock(def) {
				lock.Providers.S3[name] = entry
			}
		}
	}
	for name, entry := range cfg.Providers.UI {
		if entry != nil && sourceBacked(&entry.ProviderEntry) {
			if entry.DevActive {
				continue
			}
			if _, existed := existingUIEntries[name]; !existed && (entry.HasLocalSource() || entry.HasLocalReleaseSource()) {
				if app := cfg.Apps[name]; app != nil && strings.TrimSpace(app.UI) == "" && strings.TrimSpace(app.MountPath) != "" {
					continue
				}
			}
			configMap, err := config.NodeToMap(entry.Config)
			if err != nil {
				return nil, fmt.Errorf("decode ui %q config: %w", name, err)
			}
			uiEntry, err := l.resolveLockedProvider(ctx, cfg, paths, providermanifestv1.KindUI, name, "ui "+strconv.Quote(name), uiDestDir(paths, name), &entry.ProviderEntry, configMap, artifactModeMaterialize)
			if err != nil {
				return nil, err
			}
			if providerRequiresCommittedLock(&entry.ProviderEntry) {
				lock.Providers.UI[name] = uiEntry
			}
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
	for name, entry := range cfg.Apps {
		if err := l.resolvePackageSourceEntry(ctx, "plugin "+strconv.Quote(name), entry, repos); err != nil {
			return err
		}
	}
	for _, collection := range hostProviderCollections(cfg) {
		for name, entry := range collection.entries {
			if err := l.resolvePackageSourceEntry(ctx, string(collection.kind)+" "+strconv.Quote(name), entry, repos); err != nil {
				return err
			}
		}
	}
	for name, entry := range cfg.Providers.IndexedDB {
		if err := l.resolvePackageSourceEntry(ctx, string(config.HostProviderKindIndexedDB)+" "+strconv.Quote(name), entry, repos); err != nil {
			return err
		}
	}
	for name, entry := range cfg.Providers.S3 {
		if err := l.resolvePackageSourceEntry(ctx, providermanifestv1.KindS3+" "+strconv.Quote(name), entry, repos); err != nil {
			return err
		}
	}
	for name, entry := range cfg.Runtime.Providers {
		if entry != nil {
			if err := l.resolvePackageSourceEntry(ctx, "runtime "+strconv.Quote(name), &entry.ProviderEntry, repos); err != nil {
				return err
			}
		}
	}
	for name, entry := range cfg.Providers.UI {
		if entry != nil {
			if err := l.resolvePackageSourceEntry(ctx, "ui "+strconv.Quote(name), &entry.ProviderEntry, repos); err != nil {
				return err
			}
		}
	}
	return nil
}

func (l *Lifecycle) resolvePackageSourceEntry(ctx context.Context, subject string, entry *config.ProviderEntry, repos []providerregistry.NamedRepository) error {
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

func configHasPackageSources(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	for _, entry := range cfg.Apps {
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
		existing := byName[name]
		byName[name] = providerregistry.NamedRepository{Name: name, URL: repo.URL, Token: existing.Token}
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

func defaultLockfilePath(configPath string) string {
	dir := filepath.Dir(configPath)
	if !filepath.IsAbs(dir) {
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs
		}
	}
	return filepath.Join(dir, LockfileName)
}

func (l *Lifecycle) loadConfigForLifecycle(configPaths []string, _ StatePaths, allowMissingEnv bool) (*config.Config, error) {
	var cfg *config.Config
	var err error
	if allowMissingEnv {
		cfg, err = config.LoadAllowMissingEnvPaths(configPaths)
	} else {
		cfg, err = config.LoadPaths(configPaths)
	}
	if err != nil {
		return nil, err
	}
	if l != nil && (l.devServeEligible || len(l.forcedDevUIKeys) > 0) {
		if err := l.markDevActiveUIProviders(cfg); err != nil {
			return nil, err
		}
	}
	return cfg, nil
}

func (l *Lifecycle) markDevActiveUIProviders(cfg *config.Config) error {
	if cfg == nil {
		return nil
	}
	for _, name := range slices.Sorted(maps.Keys(cfg.Providers.UI)) {
		entry := cfg.Providers.UI[name]
		if entry == nil || !l.shouldMarkUIForDev(name) {
			continue
		}
		if !entry.HasLocalSource() {
			if l.forcedDevUIKeys[name] {
				return fmt.Errorf("--path target %q is not configured with a local source.path override", name)
			}
			continue
		}
		normalized, err := normalizeLocalSource(entry.SourcePath())
		if err != nil {
			if l.forcedDevUIKeys[name] {
				return fmt.Errorf("--path target %q: resolve local source: %w", name, err)
			}
			continue
		}
		_, manifest, err := providerpkg.ReadSourceManifestFile(normalized.manifestPath)
		if err != nil {
			if l.forcedDevUIKeys[name] {
				return fmt.Errorf("--path target %q: read source manifest: %w", name, err)
			}
			continue
		}
		if providerpkg.EffectiveDev(manifest) == nil {
			if l.forcedDevUIKeys[name] {
				return fmt.Errorf("--path target %q has no dev: block; cannot dev-serve under --locked", name)
			}
			continue
		}
		configMap, err := config.NodeToMap(entry.Config)
		if err != nil {
			return fmt.Errorf("ui %q: decode config: %w", name, err)
		}
		if err := bindResolvedUIManifest(&entry.ProviderEntry, normalized.manifestPath, manifest, configMap); err != nil {
			return fmt.Errorf("ui %q: %w", name, err)
		}
		workdir := normalized.sourceDir
		if w := strings.TrimSpace(manifest.Dev.Workdir); w != "" && w != "." {
			workdir = filepath.Join(normalized.sourceDir, filepath.FromSlash(w))
		}
		entry.DevActive = true
		entry.ResolvedDevWorkdir = workdir
	}
	return nil
}

func (l *Lifecycle) LoadForExecutionAtPath(configPath string, locked bool) (*config.Config, map[string]string, error) {
	return l.LoadForExecutionAtPaths([]string{configPath}, locked)
}

func (l *Lifecycle) LoadForExecutionAtPaths(configPaths []string, locked bool) (*config.Config, map[string]string, error) {
	return l.LoadForExecutionAtPathsWithStatePaths(configPaths, StatePaths{}, locked)
}

func (l *Lifecycle) LoadForExecutionAtPathsWithStatePaths(configPaths []string, state StatePaths, locked bool) (*config.Config, map[string]string, error) {
	cfg, err := l.loadConfigForLifecycle(configPaths, state, false)
	if err != nil {
		return nil, nil, fmt.Errorf("loading config: %v", err)
	}
	paths := resolveLifecyclePaths(configPaths, cfg, state)
	if l.devServeEligible || len(l.forcedDevUIKeys) > 0 {
		for _, name := range slices.Sorted(maps.Keys(cfg.Providers.UI)) {
			entry := cfg.Providers.UI[name]
			if entry != nil && entry.DevActive {
				if err := resolveUIThemeConfig(paths, name, entry); err != nil {
					return nil, nil, err
				}
			}
		}
	}
	mode := artifactModeMaterialize
	if locked {
		mode = artifactModeReadOnly
	}
	secretsLock, secretsValidated, err := l.lockForSecretsBootstrap(configPaths, state, paths, cfg, locked)
	if err != nil {
		return nil, nil, err
	}
	if _, err := l.primeSecretsProviderForConfigResolution(context.Background(), paths, cfg, secretsLock, mode, configSecretResolutionAll); err != nil {
		return nil, nil, err
	}
	if err := l.resolveConfigSecretsForMode(context.Background(), cfg, configSecretResolutionAll); err != nil {
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
		if err := appservice.ValidateDependencies(context.Background(), config.AppValidationConfig(cfg)); err != nil {
			return nil, nil, err
		}
	}
	return cfg, nil, nil
}

func (l *Lifecycle) LoadForValidationAtPathsWithStatePaths(configPaths []string, state StatePaths) (*config.Config, error) {
	cfg, err := l.loadConfigForLifecycle(configPaths, state, false)
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
	if err := l.applyPreparedProviders(paths, lock, cfg, artifactModeMaterialize, SyncOptions{Parallelism: 1}); err != nil {
		return nil, err
	}
	if err := config.ValidateResolvedStructure(cfg); err != nil {
		return nil, err
	}
	if err := appservice.ValidateDependencies(context.Background(), config.AppValidationConfig(cfg)); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (l *Lifecycle) LoadForStaticValidationAtPathsWithStatePaths(configPaths []string, state StatePaths, opts StaticValidationOptions) (*config.Config, error) {
	cfg, err := l.loadConfigForLifecycle(configPaths, state, true)
	if err != nil {
		return nil, fmt.Errorf("loading config: %v", err)
	}
	if err := config.OverlayRemotePluginConfigPaths(configPaths, cfg); err != nil {
		return nil, fmt.Errorf("loading config: %v", err)
	}
	paths := resolveLifecyclePaths(configPaths, cfg, state)
	platform := strings.TrimSpace(opts.Platform)
	if platform == "" {
		platform = providerpkg.CurrentPlatformString()
	}

	var lock *Lockfile
	lockErr := error(nil)
	if configHasProviderLoading(cfg) {
		lock, lockErr = ReadLockfile(paths.lockfilePath)
		if lockErr != nil && configRequiresStaticLock(cfg) {
			canPrepareScratchLock := os.IsNotExist(lockErr) &&
				platform == providerpkg.CurrentPlatformString() &&
				!configRequiresRemoteStaticLock(cfg)
			if !canPrepareScratchLock {
				return nil, fmt.Errorf("lockfile is missing or unreadable; run `%s`: %w", formatLockCommand(paths), lockErr)
			}
			_, scratchCfg, _, cleanup, err := l.prepareLockAtPathsInScratch(configPaths, state)
			if cleanup != nil {
				defer cleanup()
			}
			if err != nil {
				return nil, err
			}
			return scratchCfg, nil
		}
	}
	resolveSourceAuth, err := staticValidationNeedsSourceAuthSecrets(paths, cfg, lock, platform)
	if err != nil {
		return nil, err
	}
	if resolveSourceAuth {
		if _, err := l.primeSecretsProviderForConfigResolution(context.Background(), paths, cfg, lock, artifactModeMaterialize, configSecretResolutionSourceAuth); err != nil {
			return nil, err
		}
		if err := l.resolveConfigSecretsForMode(context.Background(), cfg, configSecretResolutionSourceAuth); err != nil {
			return nil, err
		}
	}
	if err := l.applyStaticValidationProviders(context.Background(), paths, lock, cfg, platform); err != nil {
		return nil, err
	}
	if err := config.ValidateResolvedStructure(cfg); err != nil {
		return nil, err
	}
	if err := appservice.ValidateEffectiveCatalogsAndDependencies(context.Background(), config.AppValidationConfig(cfg)); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (l *Lifecycle) syncAtPathsWithStatePaths(configPaths []string, state StatePaths, mode artifactMode, opts SyncOptions) error {
	opts = normalizeSyncOptions(opts)
	syncCache, err := materializedCacheFromSyncOptions(mode, opts)
	if err != nil {
		return err
	}
	recorder := opts.Observability.Recorder
	if recorder != nil {
		recorder.Begin(syncActionForArtifactMode(mode), configPaths, mode == artifactModeCheck, opts.Parallelism)
	}
	cfg, err := l.loadConfigForLifecycle(configPaths, state, true)
	if err != nil {
		return fmt.Errorf("loading config: %v", err)
	}
	if err := config.OverlayRemotePluginConfigPaths(configPaths, cfg); err != nil {
		return fmt.Errorf("loading config: %v", err)
	}
	paths := resolveLifecyclePaths(configPaths, cfg, state)
	paths.syncCache = syncCache
	paths.syncMetrics = recorder
	paths.syncBuildOutput = opts.Observability.BuildOutput
	if recorder != nil {
		recorder.SetPaths(paths.artifactsDir, paths.lockfilePath, paths.syncCache.dir, opts.CacheDir != "", mode == artifactModeMaterialize && paths.syncCache.dir != "")
	}
	lock, err := ReadLockfile(paths.lockfilePath)
	if err != nil {
		if os.IsNotExist(err) && !configRequiresCommittedLock(cfg) {
			lock = newLockfile()
		} else {
			return fmt.Errorf("source-backed providers require lock metadata; run `%s`: %w", formatLockCommand(paths), err)
		}
	}
	if !lockFreshForConfig(cfg, paths, lock, lockFreshnessOptions{}) {
		return fmt.Errorf("lockfile is out of date; run `%s`", formatLockCommand(paths))
	}
	if _, err := l.primeSecretsProviderForConfigResolution(context.Background(), paths, cfg, lock, mode, configSecretResolutionSourceAuth); err != nil {
		return err
	}
	if err := l.resolveConfigSecretsForMode(context.Background(), cfg, configSecretResolutionSourceAuth); err != nil {
		return err
	}
	if err := config.ValidateRuntime(cfg); err != nil {
		return err
	}
	if recorder != nil {
		recorder.FinishLoadPhase()
	}
	if err := l.applyPreparedProviders(paths, lock, cfg, mode, opts); err != nil {
		return err
	}
	if recorder != nil {
		recorder.FinishMaterializePhase()
	}
	if err := config.ValidateResolvedStructure(cfg); err != nil {
		return err
	}
	if err := appservice.ValidateEffectiveCatalogsAndDependencies(context.Background(), config.AppValidationConfig(cfg)); err != nil {
		return err
	}
	if recorder != nil {
		recorder.FinishValidatePhase()
		recorder.RecordOutputStats(mode == artifactModeMaterialize, paths.artifactsDir, preparedArtifactRoots(paths, cfg))
		recorder.Finish()
	}
	return nil
}

func syncActionForArtifactMode(mode artifactMode) string {
	if mode == artifactModeCheck {
		return syncActionCheck
	}
	return syncActionMaterialize
}

func recordSyncArtifact(paths lifecyclePaths, kind, name, subject, destDir, sourceKind, result, reason string, start time.Time, prepareDuration, activateDuration time.Duration) {
	if paths.syncMetrics == nil {
		return
	}
	paths.syncMetrics.RecordArtifact(syncArtifactMetricsEvent{
		Subject:          subject,
		Kind:             kind,
		Name:             name,
		SourceKind:       sourceKind,
		Result:           result,
		Reason:           reason,
		RelativePath:     relativeArtifactPath(paths.artifactsDir, destDir),
		Duration:         time.Since(start),
		PrepareDuration:  prepareDuration,
		ActivateDuration: activateDuration,
	})
}

func recordSyncArchiveDownload(paths lifecyclePaths, event syncArchiveMetricsEvent) {
	if paths.syncMetrics == nil {
		return
	}
	paths.syncMetrics.RecordArchiveFetch(event)
}

func recordSyncCacheEntry(paths lifecyclePaths, event syncCacheMetricsEvent) {
	if paths.syncMetrics == nil {
		return
	}
	paths.syncMetrics.RecordCacheEntry(event)
}

func syncArtifactArchiveSourceKind(paths lifecyclePaths, entry LockEntry) string {
	if isRemoteReleaseMetadataLocation(entry.Source) {
		return syncArtifactSourceRemoteArchive
	}
	return syncArtifactSourceLocalArchive
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

func (l *Lifecycle) resolveSourceAuthSecrets(ctx context.Context, cfg *config.Config) error {
	if l.sourceAuthSecretResolver == nil {
		return requireSourceAuthSecretResolver(cfg)
	}
	if err := l.sourceAuthSecretResolver(ctx, cfg); err != nil {
		return fmt.Errorf("resolving source auth secrets: %w", err)
	}
	return config.ValidateCanonicalStructure(cfg)
}

func (l *Lifecycle) resolveConfigSecretsForMode(ctx context.Context, cfg *config.Config, mode configSecretResolutionMode) error {
	if mode == configSecretResolutionSourceAuth {
		return l.resolveSourceAuthSecrets(ctx, cfg)
	}
	return l.resolveConfigSecrets(ctx, cfg)
}

func (l *Lifecycle) resolveConfigSecretsForModePartial(ctx context.Context, cfg *config.Config, mode configSecretResolutionMode) error {
	if mode == configSecretResolutionSourceAuth {
		if l.sourceAuthSecretResolver == nil {
			return requireSourceAuthSecretResolver(cfg)
		}
		if err := l.sourceAuthSecretResolver(ctx, cfg); err != nil {
			return fmt.Errorf("resolving source auth secrets: %w", err)
		}
		return nil
	}
	if l.configSecretResolver == nil {
		return nil
	}
	if err := l.configSecretResolver(ctx, cfg); err != nil {
		return fmt.Errorf("resolving config secrets: %w", err)
	}
	return nil
}

func requireSourceAuthSecretResolver(cfg *config.Config) error {
	referenced, err := config.ReferencedSourceAuthSecretProviders(cfg)
	if err != nil {
		return err
	}
	if len(referenced) == 0 {
		return nil
	}
	names := slices.Sorted(maps.Keys(referenced))
	return fmt.Errorf("source auth secret resolver is required for source auth secret refs: %s", strings.Join(names, ", "))
}

func referencedConfigSecretsProviders(cfg *config.Config) (map[string]*config.ProviderEntry, error) {
	return referencedSecretsProvidersFromCollector(cfg, config.ReferencedConfigSecretProviders)
}

func referencedSecretsProvidersFromCollector(cfg *config.Config, collect func(*config.Config) (map[string]struct{}, error)) (map[string]*config.ProviderEntry, error) {
	referenced, err := collect(cfg)
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

func referencedSecretProvidersForMode(cfg *config.Config, mode configSecretResolutionMode) (map[string]*config.ProviderEntry, error) {
	if mode == configSecretResolutionSourceAuth {
		return referencedSecretsProvidersFromCollector(cfg, config.ReferencedSourceAuthSecretProviders)
	}
	return referencedConfigSecretsProviders(cfg)
}

func secretsProviderMetadataDependencies(name string, provider *config.ProviderEntry, mode configSecretResolutionMode) (map[string]struct{}, error) {
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
	var (
		deps map[string]struct{}
		err  error
	)
	if mode == configSecretResolutionSourceAuth {
		deps, err = config.ReferencedSourceAuthSecretProviders(tmp)
	} else {
		deps, err = config.ReferencedConfigSecretProviders(tmp)
	}
	if err != nil {
		return nil, err
	}
	delete(deps, name)
	if len(deps) == 0 {
		return nil, nil
	}
	return deps, nil
}

func rejectLocalSourceSecretsForSourceAuthLock(cfg *config.Config) error {
	roots, err := config.ReferencedSourceAuthSecretProviders(cfg)
	if err != nil {
		return err
	}
	if len(roots) == 0 {
		return nil
	}
	visiting := map[string]struct{}{}
	visited := map[string]struct{}{}
	var visit func(string, []string) error
	visit = func(name string, path []string) error {
		if _, ok := visited[name]; ok {
			return nil
		}
		if _, ok := visiting[name]; ok {
			return nil
		}
		provider := cfg.Providers.Secrets[name]
		if provider == nil {
			return fmt.Errorf("config validation: secret refs reference unknown secrets provider %q", name)
		}
		nextPath := append(slices.Clone(path), name)
		if provider.HasLocalSource() {
			return fmt.Errorf("source auth secret provider %q uses source.path; gestaltd lock cannot materialize local source secrets providers (dependency path: %s)", name, strings.Join(nextPath, " -> "))
		}
		visiting[name] = struct{}{}
		deps, err := secretsProviderMetadataDependencies(name, provider, configSecretResolutionSourceAuth)
		if err != nil {
			delete(visiting, name)
			return err
		}
		for _, dep := range slices.Sorted(maps.Keys(deps)) {
			if err := visit(dep, nextPath); err != nil {
				delete(visiting, name)
				return err
			}
		}
		delete(visiting, name)
		visited[name] = struct{}{}
		return nil
	}
	for _, name := range slices.Sorted(maps.Keys(roots)) {
		if err := visit(name, nil); err != nil {
			return err
		}
	}
	return nil
}

func (l *Lifecycle) resolveSecretsProviderMetadata(ctx context.Context, name string, provider *config.ProviderEntry, available map[string]*config.ProviderEntry, mode configSecretResolutionMode) error {
	if provider == nil {
		return nil
	}
	deps, err := secretsProviderMetadataDependencies(name, provider, mode)
	if err != nil {
		return err
	}
	if len(deps) == 0 {
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
	if err := l.resolveConfigSecretsForModePartial(ctx, tmp, mode); err != nil {
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
	if locked && err != nil && os.IsNotExist(err) && !configRequiresCommittedLock(cfg) {
		lock = newLockfile()
		err = nil
	}
	if !locked && (err != nil || !lockFreshForConfig(cfg, paths, lock, lockFreshnessOptions{RequireArtifacts: true}) || configHasLocalProviderSources(cfg) || configHasMetadataProviderSources(cfg)) {
		lock, err = l.PrepareAtPathsWithStatePaths(configPaths, state)
		validatedDuringPrepare = err == nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("source-backed providers require lock metadata; full config mode prepares all source-backed providers. For local single-app development, use `gestaltd serve --path PATH` or a layered local config; otherwise run `%s` then `%s`: %w", formatLockCommand(paths), formatSyncLockedCommand(paths), err)
	}
	return lock, validatedDuringPrepare, nil
}

func (l *Lifecycle) primeSecretsProviderForConfigResolution(ctx context.Context, paths lifecyclePaths, cfg *config.Config, lock *Lockfile, mode artifactMode, secretMode configSecretResolutionMode) (map[string]LockEntry, error) {
	if cfg == nil {
		return nil, nil
	}
	referenced, err := referencedSecretProvidersForMode(cfg, secretMode)
	if err != nil {
		return nil, err
	}
	if secretMode == configSecretResolutionSourceAuth && l.sourceAuthSecretResolver == nil {
		return nil, requireSourceAuthSecretResolver(cfg)
	}
	available := make(map[string]*config.ProviderEntry, len(referenced))
	pending := make(map[string]*config.ProviderEntry, len(referenced))
	dependencies := make(map[string]map[string]struct{}, len(referenced))
	for name, provider := range referenced {
		if provider == nil {
			continue
		}
		deps, err := secretsProviderMetadataDependencies(name, provider, secretMode)
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
	var (
		packageRepos       []providerregistry.NamedRepository
		packageReposLoaded bool
	)
	resolvePackageSourceForProvider := func(name string, provider *config.ProviderEntry) error {
		if lock != nil || provider == nil || !provider.Source.IsPackage() || provider.Source.ResolvedPackageMetadataURL() != "" {
			return nil
		}
		if !packageReposLoaded {
			var err error
			packageRepos, err = providerRepositoriesForConfig(cfg)
			if err != nil {
				return err
			}
			packageReposLoaded = true
		}
		return l.resolvePackageSourceEntry(ctx, providermanifestv1.KindSecrets+" "+strconv.Quote(name), provider, packageRepos)
	}
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
			if err := l.resolveSecretsProviderMetadata(ctx, name, provider, available, secretMode); err != nil {
				return nil, err
			}
			if err := resolvePackageSourceForProvider(name, provider); err != nil {
				return nil, err
			}
			configMap, err := config.NodeToMap(provider.Config)
			if err != nil {
				return nil, fmt.Errorf("decode provider config for %s %q: %w", providermanifestv1.KindSecrets, name, err)
			}

			if sourceBacked(provider) {
				switch {
				case provider.HasLocalSource():
					if lock == nil {
						entry, err := l.resolveLockedProvider(ctx, cfg, paths, providermanifestv1.KindSecrets, name, fmt.Sprintf("%s %q", providermanifestv1.KindSecrets, name), secretsDestDir(paths, name), provider, configMap, artifactModeMaterialize)
						if err != nil {
							return nil, err
						}
						prepared[name] = entry
					}
					if err := l.applyLocalComponentEntry(paths, providermanifestv1.KindSecrets, name, provider, configMap, secretsDestDir(paths, name), mode); err != nil {
						return nil, err
					}
				case lock != nil:
					lockEntry, ok := lock.Providers.Secrets[name]
					if !ok {
						return nil, lockMetadataStaleError(paths, "lock entry for %s %q is missing or stale", providermanifestv1.KindSecrets, name)
					}
					if err := l.applyLockedComponentEntry(paths, &lockEntry, providermanifestv1.KindSecrets, name, provider, configMap, secretsDestDir(paths, name), mode); err != nil {
						return nil, err
					}
				default:
					entry, err := l.resolveLockedProvider(ctx, cfg, paths, providermanifestv1.KindSecrets, name, fmt.Sprintf("%s %q", providermanifestv1.KindSecrets, name), secretsDestDir(paths, name), provider, configMap, artifactModeMaterialize)
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
	syncCache              materializedCache
	syncMetrics            *SyncMetricsRecorder
	syncBuildOutput        providerpkg.CommandOutput
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

func providerRequiresCommittedLock(entry *config.ProviderEntry) bool {
	return sourceBacked(entry) && !entry.HasLocalSource()
}

func runtimeRequiresCommittedLock(entry *config.RuntimeProviderEntry) bool {
	return entry != nil && providerRequiresCommittedLock(&entry.ProviderEntry)
}

func hostProviderCollections(cfg *config.Config) []struct {
	kind    config.HostProviderKind
	entries map[string]*config.ProviderEntry
} {
	return []struct {
		kind    config.HostProviderKind
		entries map[string]*config.ProviderEntry
	}{
		{config.HostProviderKindIdentity, cfg.Providers.Identity},
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
	case config.HostProviderKindIdentity:
		return lock.Providers.Identity
	case config.HostProviderKindAuthorization:
		return lock.Providers.Authorization
	case config.HostProviderKindExternalCredentials:
		return lock.Providers.ExternalCredentials
	case config.HostProviderKindSecrets:
		return lock.Providers.Secrets
	case config.HostProviderKindTelemetry:
		return lock.Providers.Telemetry
	case config.HostProviderKindAudit:
		return lock.Providers.Audit
	case config.HostProviderKindCache:
		return lock.Providers.Cache
	case config.HostProviderKindIndexedDB:
		return lock.Providers.IndexedDB
	case config.HostProviderKindWorkflow:
		return lock.Providers.Workflow
	case config.HostProviderKindAgent:
		return lock.Providers.Agent
	case config.HostProviderKindRuntime:
		return lock.Providers.Runtime
	default:
		return nil
	}
}

func configHasProviderLoading(cfg *config.Config) bool {
	return configHasMatchingProviderEntry(cfg, sourceBacked)
}

func configRequiresCommittedLock(cfg *config.Config) bool {
	return configHasMatchingProviderEntry(cfg, providerRequiresCommittedLock)
}

func configHasLocalProviderSources(cfg *config.Config) bool {
	for _, entry := range cfg.Apps {
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
		if entry != nil && !entry.DevActive && (entry.HasLocalSource() || entry.HasLocalReleaseSource()) {
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
	for _, entry := range cfg.Apps {
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
	if override != "" {
		return resolveCLIArtifactsDir(override)
	}
	if cfg != nil {
		if cfg.Server.ArtifactsDir != "" {
			return resolveConfigArtifactsDir(configPath, cfg.Server.ArtifactsDir)
		}
	}
	return absoluteConfigDir(configPath)
}

func resolveCLIArtifactsDir(dir string) string {
	if filepath.IsAbs(dir) {
		return filepath.Clean(dir)
	}
	if abs, err := filepath.Abs(dir); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(dir)
}

func resolveConfigArtifactsDir(configPath, dir string) string {
	if filepath.IsAbs(dir) {
		return filepath.Clean(dir)
	}
	return filepath.Join(absoluteConfigDir(configPath), dir)
}

func absoluteConfigDir(configPath string) string {
	dir := filepath.Dir(configPath)
	if abs, err := filepath.Abs(dir); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(dir)
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
	case config.HostProviderKindIdentity:
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

const (
	preparedLockMetadataFile          = ".gestaltd-lock-metadata.json"
	preparedLockMetadataSchema        = "gestaltd-prepared-provider"
	preparedLockMetadataSchemaVersion = 2
)

type preparedLockMetadata struct {
	Schema            string `json:"schema,omitempty"`
	SchemaVersion     int    `json:"schemaVersion,omitempty"`
	InputDigest       string `json:"inputDigest"`
	SourceIdentity    string `json:"sourceIdentity,omitempty"`
	ConfiguredSource  string `json:"configuredSource,omitempty"`
	SourceInputDigest string `json:"sourceInputDigest,omitempty"`
	Kind              string `json:"kind"`
	Name              string `json:"name"`
	ManifestSource    string `json:"manifestSource,omitempty"`
	ManifestVersion   string `json:"manifestVersion,omitempty"`
	Runtime           string `json:"runtime,omitempty"`
	Platform          string `json:"platform,omitempty"`
	OutputDigest      string `json:"outputDigest,omitempty"`
}

type preparedLockMetadataState int

const (
	preparedLockMetadataMissing preparedLockMetadataState = iota
	preparedLockMetadataMatch
	preparedLockMetadataStale
)

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

func preparedInstallOutputDigest(install *preparedInstall) (string, error) {
	if install == nil || strings.TrimSpace(install.manifestPath) == "" {
		return "", fmt.Errorf("prepared install manifest path is required")
	}
	return preparedDirectoryDigest(filepath.Dir(install.manifestPath))
}

func preparedDirectoryDigest(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", fmt.Errorf("prepared install root is required")
	}
	root = filepath.Clean(root)
	var digests []string
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Name() == preparedLockMetadataFile {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		sum, err := providerpkg.FileSHA256(path)
		if err != nil {
			return err
		}
		digests = append(digests, filepath.ToSlash(filepath.Clean(rel))+"="+sum)
		return nil
	}); err != nil {
		return "", err
	}
	slices.Sort(digests)
	combined := sha256.Sum256([]byte(strings.Join(digests, "\n")))
	return hex.EncodeToString(combined[:]), nil
}

func localSourceDigest(provider *config.ProviderEntry) (string, error) {
	if provider == nil || !provider.HasLocalSource() {
		return "", fmt.Errorf("local source provider is required")
	}
	return fingerprintLocalSourceDigest(provider.SourcePath())
}

func localPreparedFingerprintName(kind, name string) string {
	if providermanifestv1.NormalizeKind(kind) == providermanifestv1.KindUI {
		return "ui:" + name
	}
	return name
}

func expectedPreparedLockMetadata(paths lifecyclePaths, install *preparedInstall, kind, name string, provider *config.ProviderEntry, includeSource bool) (preparedLockMetadata, error) {
	if install == nil || strings.TrimSpace(install.manifestPath) == "" {
		return preparedLockMetadata{}, fmt.Errorf("prepared install manifest path is required")
	}
	outputDigest, err := preparedInstallOutputDigest(install)
	if err != nil {
		return preparedLockMetadata{}, err
	}
	metadata := preparedLockMetadata{
		Schema:           preparedLockMetadataSchema,
		SchemaVersion:    preparedLockMetadataSchemaVersion,
		ConfiguredSource: fingerprintLocalSourcePath(provider.SourcePath(), paths.configDir),
		Kind:             providermanifestv1.NormalizeKind(kind),
		Name:             name,
		Platform:         providerpkg.CurrentPlatformString(),
		OutputDigest:     outputDigest,
	}
	if install.manifest != nil {
		metadata.ManifestSource = strings.TrimSpace(install.manifest.Source)
		metadata.ManifestVersion = strings.TrimSpace(install.manifest.Version)
		metadata.Runtime = providerrelease.RuntimeForManifest(archivePolicyKind(kind), install.manifest)
	}
	if includeSource {
		fingerprint, err := ProviderFingerprint(localPreparedFingerprintName(kind, name), provider, paths.configDir)
		if err != nil {
			return preparedLockMetadata{}, err
		}
		sourceIdentity, err := localSourceIdentity(provider.SourcePath(), paths.configDir)
		if err != nil {
			return preparedLockMetadata{}, err
		}
		sourceDigest, err := localSourceDigest(provider)
		if err != nil {
			return preparedLockMetadata{}, err
		}
		metadata.InputDigest = fingerprint
		metadata.SourceIdentity = sourceIdentity
		metadata.SourceInputDigest = sourceDigest
	}
	return metadata, nil
}

func writePreparedLockMetadata(paths lifecyclePaths, install *preparedInstall, kind, name string, provider *config.ProviderEntry) error {
	metadata, err := expectedPreparedLockMetadata(paths, install, kind, name, provider, true)
	if err != nil {
		return fmt.Errorf("prepare local source metadata: %w", err)
	}
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("encode prepared lock metadata: %w", err)
	}
	data = append(data, '\n')
	path := filepath.Join(filepath.Dir(install.manifestPath), preparedLockMetadataFile)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write prepared lock metadata: %w", err)
	}
	return nil
}

func inspectPreparedLockMetadata(paths lifecyclePaths, install *preparedInstall, kind, name string, provider *config.ProviderEntry, mode artifactMode) preparedLockMetadataState {
	if install == nil || strings.TrimSpace(install.manifestPath) == "" {
		return preparedLockMetadataStale
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(install.manifestPath), preparedLockMetadataFile))
	if err != nil {
		if os.IsNotExist(err) {
			return preparedLockMetadataMissing
		}
		return preparedLockMetadataStale
	}
	var metadata preparedLockMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return preparedLockMetadataStale
	}
	if metadata.Schema != preparedLockMetadataSchema ||
		metadata.SchemaVersion != preparedLockMetadataSchemaVersion ||
		providermanifestv1.NormalizeKind(metadata.Kind) != providermanifestv1.NormalizeKind(kind) ||
		strings.TrimSpace(metadata.Name) != strings.TrimSpace(name) {
		return preparedLockMetadataStale
	}
	expected, err := expectedPreparedLockMetadata(paths, install, kind, name, provider, mode != artifactModeReadOnly)
	if err != nil {
		return preparedLockMetadataStale
	}
	if strings.TrimSpace(metadata.ConfiguredSource) != strings.TrimSpace(expected.ConfiguredSource) ||
		strings.TrimSpace(metadata.ManifestSource) != strings.TrimSpace(expected.ManifestSource) ||
		strings.TrimSpace(metadata.ManifestVersion) != strings.TrimSpace(expected.ManifestVersion) ||
		strings.TrimSpace(metadata.Runtime) != strings.TrimSpace(expected.Runtime) ||
		strings.TrimSpace(metadata.Platform) != strings.TrimSpace(expected.Platform) ||
		strings.TrimSpace(metadata.OutputDigest) != strings.TrimSpace(expected.OutputDigest) {
		return preparedLockMetadataStale
	}
	if mode != artifactModeReadOnly {
		if strings.TrimSpace(metadata.InputDigest) != strings.TrimSpace(expected.InputDigest) ||
			strings.TrimSpace(metadata.SourceIdentity) != strings.TrimSpace(expected.SourceIdentity) ||
			strings.TrimSpace(metadata.SourceInputDigest) != strings.TrimSpace(expected.SourceInputDigest) {
			return preparedLockMetadataStale
		}
	}
	return preparedLockMetadataMatch
}

func providerManifestKind(kind config.HostProviderKind) string {
	switch kind {
	case config.HostProviderKindIdentity:
		return providermanifestv1.KindIdentity
	case config.HostProviderKindAuthorization:
		return providermanifestv1.KindAuthorization
	case config.HostProviderKindExternalCredentials:
		return providermanifestv1.KindExternalCredentials
	case config.HostProviderKindSecrets:
		return providermanifestv1.KindSecrets
	case config.HostProviderKindTelemetry, config.HostProviderKindAudit:
		return providermanifestv1.KindApp
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
	data, err := json.MarshalIndent(canonicalLockfile(lock), "", "  ")
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
	var lock Lockfile
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("parsing lockfile %s: %w", path, err)
	}
	if err := validateProviderLockfile(&lock); err != nil {
		return nil, err
	}
	return normalizeLockfile(&lock), nil
}

func WriteLockfile(path string, lock *Lockfile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create lockfile parent directory: %w", err)
	}
	if err := writeJSONFile(path, canonicalLockfile(lock)); err != nil {
		return fmt.Errorf("writing lockfile: %w", err)
	}
	return nil
}

type lockFreshnessOptions struct {
	RequireArtifacts bool
	UI               bool
}

func lockFreshForConfig(cfg *config.Config, paths lifecyclePaths, lock *Lockfile, opts lockFreshnessOptions) bool {
	if lock == nil {
		return false
	}
	for name, entry := range cfg.Apps {
		if !providerRequiresCommittedLock(entry) {
			continue
		}
		lockEntry, found := lock.Providers.App[name]
		if !lockEntryFresh(paths, providermanifestv1.KindApp, name, entry, lockEntry, found, providerDestDir(paths, name), opts) {
			return false
		}
	}
	for _, collection := range hostProviderCollections(cfg) {
		lockEntries := lockEntriesForKind(lock, collection.kind)
		for name, entry := range collection.entries {
			if entry == nil || !providerRequiresCommittedLock(entry) {
				continue
			}
			kind := providerManifestKind(collection.kind)
			lockEntry, found := lockEntries[name]
			if !lockEntryFresh(paths, kind, name, entry, lockEntry, found, componentDestDir(paths, collection.kind, name), opts) {
				return false
			}
		}
	}
	for name, entry := range cfg.Runtime.Providers {
		if !runtimeRequiresCommittedLock(entry) {
			continue
		}
		lockEntry, found := lock.Providers.Runtime[name]
		if !lockEntryFresh(paths, providermanifestv1.KindRuntime, name, &entry.ProviderEntry, lockEntry, found, runtimeDestDir(paths, name), opts) {
			return false
		}
	}
	for name, entry := range cfg.Providers.IndexedDB {
		if !providerRequiresCommittedLock(entry) {
			continue
		}
		lockEntry, found := lock.Providers.IndexedDB[name]
		if !lockEntryFresh(paths, providermanifestv1.KindIndexedDB, name, entry, lockEntry, found, indexeddbDestDir(paths, name), opts) {
			return false
		}
	}
	for name, entry := range cfg.Providers.S3 {
		if !providerRequiresCommittedLock(entry) {
			continue
		}
		lockEntry, found := lock.Providers.S3[name]
		if !lockEntryFresh(paths, providermanifestv1.KindS3, name, entry, lockEntry, found, s3DestDir(paths, name), opts) {
			return false
		}
	}
	for name, entry := range cfg.Providers.UI {
		if entry == nil || !providerRequiresCommittedLock(&entry.ProviderEntry) {
			continue
		}
		if entry.DevActive {
			continue
		}
		lockEntry, found := lock.Providers.UI[name]
		uiOpts := opts
		uiOpts.UI = true
		if !lockEntryFresh(paths, providermanifestv1.KindUI, name, &entry.ProviderEntry, lockEntry, found, uiDestDir(paths, name), uiOpts) {
			return false
		}
	}
	return true
}

func lockEntryFresh(paths lifecyclePaths, kind, name string, provider *config.ProviderEntry, entry LockEntry, found bool, destDir string, opts lockFreshnessOptions) bool {
	if opts.RequireArtifacts {
		if opts.UI {
			if !uiLockEntryMetadataMatches(paths, name, provider, entry, found) {
				return false
			}
			install, err := inspectPreparedInstall(destDir)
			if err != nil {
				return false
			}
			if !preparedManifestMatchesLock(entry, install.manifest) {
				return false
			}
			if install.assetRootPath == "" {
				return false
			}
			if _, err := os.Stat(install.assetRootPath); err != nil {
				return false
			}
			return true
		}
		return lockEntryMatches(paths, kind, name, provider, entry, found, destDir)
	}
	if opts.UI {
		return uiLockEntryMetadataMatches(paths, name, provider, entry, found)
	}
	return lockEntryMetadataMatches(paths, kind, name, provider, entry, found)
}

func localSourcePathMissing(provider *config.ProviderEntry) bool {
	if provider == nil || !provider.HasLocalSource() {
		return false
	}
	if _, err := os.Stat(provider.SourcePath()); err != nil {
		return os.IsNotExist(err)
	}
	return false
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
		sourceIdentity, err := localSourceIdentity(entry.SourcePath(), configDir)
		if err != nil {
			return "", err
		}
		input.Path = sourceIdentity
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
	if entry.Source.IsGit() {
		return canonicalGitSourceLocation(entry)
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
	if entry.Source.IsGit() {
		return gitSourceFingerprintLocation(entry)
	}
	return providerSourceLockLocation(entry, configDir)
}

func lockEntrySourceMatchesProvider(paths lifecyclePaths, provider *config.ProviderEntry, entry LockEntry) bool {
	if provider == nil {
		return false
	}
	if provider.Source.IsGit() {
		return gitSourceMatchesLockRef(provider, entry.SourceRef)
	}
	if provider.Source.IsPackage() {
		if strings.TrimSpace(entry.Package) != provider.Source.PackageAddress() {
			return false
		}
		if !providerregistry.VersionSatisfiesConstraint(entry.Version, provider.Source.PackageVersionConstraint()) {
			return false
		}
		if resolved := provider.Source.ResolvedPackageMetadataURL(); resolved != "" {
			return entry.Source == resolved
		}
		source := strings.TrimSpace(entry.Source)
		return source != "" && source != strings.TrimSpace(entry.Package)
	}
	return entry.Source == providerSourceLockLocation(provider, paths.configDir)
}

func lockEntryFingerprintMatchesProvider(name string, provider *config.ProviderEntry, configDir string, entry LockEntry) (bool, error) {
	fingerprint, err := ProviderFingerprint(name, provider, configDir)
	if err != nil {
		return false, err
	}
	return entry.InputDigest == fingerprint, nil
}

func lockEntryFingerprintMatchesProviderForMode(name string, provider *config.ProviderEntry, configDir string, entry LockEntry, mode artifactMode) (bool, error) {
	if mode == artifactModeReadOnly && providerHasReadOnlyLocalSource(provider) {
		return true, nil
	}
	if providerHasReadOnlyLocalSource(provider) && localSourcePathMissing(provider) {
		return true, nil
	}
	return lockEntryFingerprintMatchesProvider(name, provider, configDir, entry)
}

func providerHasReadOnlyLocalSource(provider *config.ProviderEntry) bool {
	return provider != nil && provider.HasLocalSource()
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

type normalizedLocalSource struct {
	sourceDir    string
	manifestPath string
}

func normalizeLocalSource(sourcePath string) (normalizedLocalSource, error) {
	sourcePath = strings.TrimSpace(sourcePath)
	if sourcePath == "" {
		return normalizedLocalSource{}, fmt.Errorf("local source path is required")
	}
	sourcePath = filepath.Clean(sourcePath)
	info, err := os.Stat(sourcePath)
	if err != nil {
		return normalizedLocalSource{}, err
	}
	sourceDir := sourcePath
	manifestPath := sourcePath
	if info.IsDir() {
		manifestPath, err = providerpkg.FindManifestFile(sourcePath)
		if err != nil {
			return normalizedLocalSource{}, err
		}
	} else {
		sourceDir = filepath.Dir(sourcePath)
	}
	return normalizedLocalSource{
		sourceDir:    filepath.Clean(sourceDir),
		manifestPath: filepath.Clean(manifestPath),
	}, nil
}

func localSourceIdentity(sourcePath, configDir string) (string, error) {
	normalized, err := normalizeLocalSource(sourcePath)
	if err != nil {
		return "", err
	}
	return fingerprintLocalSourcePath(normalized.manifestPath, configDir), nil
}

func fingerprintLocalSourceDigest(sourcePath string) (string, error) {
	normalized, err := normalizeLocalSource(sourcePath)
	if err != nil {
		return "", err
	}
	_, manifest, err := providerpkg.ReadSourceManifestFile(normalized.manifestPath)
	if err != nil {
		return "", err
	}
	build := providerpkg.EffectiveSourceBuild(manifest)
	if build != nil {
		return fingerprintLocalBuildInputs(normalized.sourceDir, normalized.manifestPath, manifest, build)
	}
	_, manifest, err = providerpkg.PrepareSourceManifest(normalized.manifestPath)
	if err != nil {
		return "", err
	}
	return providerpkg.DirectoryDigest(normalized.sourceDir, normalized.manifestPath, manifest)
}

func fingerprintLocalBuildInputs(sourceDir, manifestPath string, manifest *providermanifestv1.Manifest, build *providerpkg.ResolvedSourceBuild) (string, error) {
	var digests []string
	seen := map[string]struct{}{}

	addFile := func(path string) error {
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(filepath.Clean(rel))
		if _, ok := seen[rel]; ok {
			return nil
		}
		seen[rel] = struct{}{}
		sum, err := providerpkg.FileSHA256(path)
		if err != nil {
			return err
		}
		digests = append(digests, rel+"="+sum)
		return nil
	}

	if err := addFile(manifestPath); err != nil {
		return "", fmt.Errorf("digest manifest: %w", err)
	}
	if err := fingerprintLocalPackageSupportFiles(sourceDir, manifest, addFile); err != nil {
		return "", err
	}

	outputAbs := ""
	if outputRel, _, err := providerpkg.SourceBuildOutput(manifest); err == nil && outputRel != "" {
		outputAbs = filepath.Clean(filepath.Join(sourceDir, filepath.FromSlash(outputRel)))
	}
	excludedDirs := map[string]struct{}{
		".git":          {},
		".gestaltd":     {},
		".next":         {},
		".turbo":        {},
		".cache":        {},
		".pytest_cache": {},
		".ruff_cache":   {},
		".mypy_cache":   {},
		".venv":         {},
		"venv":          {},
		"node_modules":  {},
		"target":        {},
	}
	shouldSkip := func(path string, d os.DirEntry) bool {
		cleanPath := filepath.Clean(path)
		if outputAbs != "" && pathWithinRoot(outputAbs, cleanPath) {
			return true
		}
		if d.IsDir() {
			_, excluded := excludedDirs[d.Name()]
			return excluded
		}
		return false
	}
	walkInput := func(inputAbs string) error {
		return filepath.WalkDir(inputAbs, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if shouldSkip(path, d) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if err := addFile(path); err != nil {
				return fmt.Errorf("digest input %s: %w", path, err)
			}
			return nil
		})
	}

	inputs := append([]string(nil), build.Inputs...)
	if len(inputs) == 0 {
		workdir := "."
		if strings.TrimSpace(build.Workdir) != "" {
			workdir = build.Workdir
		}
		inputs = []string{workdir}
	}
	for _, input := range inputs {
		inputAbs := filepath.Clean(filepath.Join(sourceDir, filepath.FromSlash(input)))
		info, err := os.Stat(inputAbs)
		if err != nil {
			return "", fmt.Errorf("stat build input %q: %w", input, err)
		}
		if shouldSkip(inputAbs, dirEntryFromFileInfo(info)) {
			continue
		}
		if !info.IsDir() {
			if err := addFile(inputAbs); err != nil {
				return "", fmt.Errorf("digest build input %q: %w", input, err)
			}
			continue
		}
		if err := walkInput(inputAbs); err != nil {
			return "", fmt.Errorf("digest build input %q: %w", input, err)
		}
	}

	slices.Sort(digests)
	combined := sha256.Sum256([]byte(strings.Join(digests, "\n")))
	return hex.EncodeToString(combined[:]), nil
}

func fingerprintLocalPackageSupportFiles(sourceDir string, manifest *providermanifestv1.Manifest, addFile func(string) error) error {
	for _, ref := range providerpkg.LocalPackageReferences(manifest) {
		path := filepath.Clean(filepath.Join(sourceDir, filepath.FromSlash(ref.Path)))
		if err := addFile(path); err != nil {
			return fmt.Errorf("digest %s: %w", ref.Description, err)
		}
	}

	staticCatalogPath := providerpkg.StaticCatalogPath(sourceDir)
	if _, err := os.Stat(staticCatalogPath); err == nil {
		if err := addFile(staticCatalogPath); err != nil {
			return fmt.Errorf("digest provider static catalog: %w", err)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("digest provider static catalog: %w", err)
	}
	return nil
}

type fileInfoDirEntry struct {
	os.FileInfo
}

func (d fileInfoDirEntry) Type() os.FileMode {
	return d.FileInfo.Mode().Type()
}

func (d fileInfoDirEntry) Info() (os.FileInfo, error) {
	return d.FileInfo, nil
}

func dirEntryFromFileInfo(info os.FileInfo) os.DirEntry {
	return fileInfoDirEntry{FileInfo: info}
}

func fingerprintLocalReleaseMetadataDigest(sourcePath string) (string, error) {
	data, err := providerrelease.ReadLocalFile(sourcePath)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func archivePolicyKind(kind string) string {
	switch kind {
	case providerLockKindTelemetry, providerLockKindAudit:
		return providermanifestv1.KindApp
	default:
		return kind
	}
}

func archivePolicySubject(kind, name string) string {
	switch archivePolicyKind(kind) {
	case providermanifestv1.KindApp:
		return fmt.Sprintf("provider %q", name)
	case providermanifestv1.KindUI:
		return fmt.Sprintf("ui provider %q", name)
	default:
		return fmt.Sprintf("%s %q", kind, name)
	}
}

func readLockEntryManifest(paths lifecyclePaths, entry LockEntry, destDir string) (*providermanifestv1.Manifest, error) {
	if strings.TrimSpace(entry.ArtifactManifest) != "" {
		manifestPath := resolveLockPath(paths.artifactsDir, entry.ArtifactManifest)
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
		if policyKind == providermanifestv1.KindApp {
			manifest, err = readLockEntryManifest(paths, entry, destDir)
			if err != nil {
				return false
			}
		}
		if err := validateLockedArchivePolicy(archivePolicySubject(kind, name), policyKind, manifest, entry, platform, resolvedKey); err != nil {
			return false
		}
	}
	if strings.TrimSpace(entry.ArtifactManifest) != "" {
		manifestPath := resolveLockPath(paths.artifactsDir, entry.ArtifactManifest)
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

// LockEntryMetadataMatchesProvider checks whether a lock entry still matches a
// provider config entry without inspecting prepared artifact files.
func LockEntryMetadataMatchesProvider(configPath, kind, name string, providerEntry *config.ProviderEntry, entry LockEntry, found, ui bool) bool {
	paths := lifecyclePaths{
		configPath: configPath,
		configDir:  filepath.Dir(configPath),
	}
	if ui {
		return uiLockEntryMetadataMatches(paths, name, providerEntry, entry, found)
	}
	return lockEntryMetadataMatches(paths, kind, name, providerEntry, entry, found)
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

func preparedInstallMatchesLockForMode(kind, name string, provider *config.ProviderEntry, entry LockEntry, install *preparedInstall, mode artifactMode) bool {
	return install != nil && preparedManifestMatchesLock(entry, install.manifest)
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
	_, cleanupInstall, commitInstall, err := stageLocalSourceInstall(kind, name, manifestPath, destDir, providerpkg.StageSourcePreparedInstallOptions{})
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

func stageLocalSourceInstall(kind, name, manifestPath, destDir string, opts providerpkg.StageSourcePreparedInstallOptions) (*preparedInstall, func() error, func() error, error) {
	if strings.TrimSpace(manifestPath) == "" {
		return nil, nil, nil, fmt.Errorf("manifest path for %s %q is required", kind, name)
	}
	normalized, err := normalizeLocalSource(manifestPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("manifest for %s %q not found at %s: %w", kind, name, manifestPath, err)
	}
	manifestPath = normalized.manifestPath
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
		stageKind = providermanifestv1.KindApp
	}
	if _, err := providerpkg.StageSourcePreparedInstallDir(manifestPath, tempDir, providerpkg.StageSourcePreparedInstallOptions{
		Kind:        stageKind,
		AppName:     name,
		GOOS:        runtime.GOOS,
		GOARCH:      runtime.GOARCH,
		BuildOutput: opts.BuildOutput,
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
		if err := activatePreparedInstallDir(destDir, tempDir); err != nil {
			return err
		}
		cleanupDir = ""
		return nil
	}

	return install, cleanupInstall, commitInstall, nil
}

func activatePreparedInstallDir(destDir, tempDir string) error {
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
	return nil
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

func localLockEntryFromPreparedInstall(paths lifecyclePaths, kind, name string, app *config.ProviderEntry, install *preparedInstall) (LockEntry, error) {
	fingerprint, err := ProviderFingerprint(name, app, paths.configDir)
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
	entry := LockEntry{
		InputDigest:      fingerprint,
		Package:          strings.TrimSpace(install.manifest.Source),
		Kind:             providermanifestv1.NormalizeKind(install.manifest.Kind),
		Runtime:          providerrelease.RuntimeForManifest(archivePolicyKind(kind), install.manifest),
		Version:          strings.TrimSpace(install.manifest.Version),
		ArtifactManifest: manifestPath,
		Executable:       executablePath,
	}
	if app != nil && app.HasLocalSource() {
		if err := writePreparedLockMetadata(paths, install, kind, name, app); err != nil {
			return LockEntry{}, fmt.Errorf("write prepared lock metadata for %s %q: %w", kind, name, err)
		}
	}
	return entry, nil
}

func localUILockEntryFromPreparedInstall(paths lifecyclePaths, name string, app *config.ProviderEntry, install *preparedInstall) (LockEntry, error) {
	fingerprint, err := NamedUIProviderFingerprint(name, app, paths.configDir)
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
	entry := LockEntry{
		InputDigest:      fingerprint,
		Package:          strings.TrimSpace(install.manifest.Source),
		Kind:             providermanifestv1.NormalizeKind(install.manifest.Kind),
		Runtime:          providerrelease.RuntimeForManifest(providermanifestv1.KindUI, install.manifest),
		Version:          strings.TrimSpace(install.manifest.Version),
		ArtifactManifest: manifestPath,
		AssetRoot:        assetRoot,
	}
	if app != nil && app.HasLocalSource() {
		if err := writePreparedLockMetadata(paths, install, providermanifestv1.KindUI, name, app); err != nil {
			return LockEntry{}, fmt.Errorf("write prepared lock metadata for ui %q: %w", name, err)
		}
	}
	return entry, nil
}

func (l *Lifecycle) installMetadataSourcePackage(ctx context.Context, expectedKind, name, subject, destDir string, app *config.ProviderEntry, configDir string, mode artifactMode) (*installedPackage, LockEntry, error) {
	sourceLocation := app.SourceReleaseLocation()
	bundle, resolvedMetadataLocation, gitHubReleaseAssets, err := fetchProviderReleaseBundle(ctx, l.metadataHTTPClient(), sourceLocation, sourceAuthToken(app))
	if err != nil {
		return nil, LockEntry{}, fmt.Errorf("%s fetch metadata %q: %w", subject, sourceLocation, err)
	}
	metadata := bundle.Metadata
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
		Runtime:  providerrelease.RuntimeForManifest(metadata.Kind, bundle.Manifest),
		Source:   providerSourceLockLocation(app, configDir),
		Version:  metadata.Version,
		Archives: archives,
	}
	staticManifest, err := portableStaticValidationManifest(bundle.Manifest, "", true)
	if err != nil {
		return nil, LockEntry{}, fmt.Errorf("%s validation metadata: %w", subject, err)
	}
	addCatalogOperationIDsToManifest(staticManifest, bundle.Catalog)
	entry.ValidationManifest = staticManifest
	entry.CatalogAvailable = bundle.Catalog != nil
	entry.CatalogSessionOnly = bundle.Catalog == nil && providerrelease.CatalogSessionModeAllowed(metadata.Kind, bundle.Manifest)

	currentPlatform := providerpkg.CurrentPlatformString()
	archive, resolvedKey, ok := resolveArchiveForPlatform(entry, currentPlatform)
	if !ok || archive.URL == "" {
		return nil, LockEntry{}, fmt.Errorf("no archive for platform %s for %s; publish an explicit %s target or a generic package where allowed", currentPlatform, subject, currentPlatform)
	}
	if mode == artifactModeCheck {
		if err := validateStaticLockedArchivePolicy(subject, expectedManifestKind, entry, currentPlatform, resolvedKey); err != nil {
			return nil, LockEntry{}, err
		}
		return nil, entry, nil
	}
	archiveLocation, err := resolveLockedArchiveLocation(configDir, entry.Source, archive.URL)
	if err != nil {
		return nil, LockEntry{}, fmt.Errorf("resolve archive for %s: %w", subject, err)
	}
	download, err := downloadArchiveForSource(ctx, l.metadataHTTPClient(), sourceAuthToken(app), archiveLocation)
	if err != nil {
		return nil, LockEntry{}, fmt.Errorf("download metadata source package for %s: %w", subject, err)
	}
	defer download.Cleanup()
	if err := verifyArchiveSHA256(download.SHA256Hex, archive.SHA256); err != nil {
		return nil, LockEntry{}, fmt.Errorf("verify metadata source package for %s: %w", subject, err)
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
	installedRuntime := providerrelease.RuntimeForManifest(expectedManifestKind, installed.Manifest)
	if entry.Runtime != installedRuntime {
		return nil, LockEntry{}, fmt.Errorf("%s manifest runtime %q does not match metadata runtime %q", subject, installedRuntime, entry.Runtime)
	}
	if err := validateInstalledPackageMatchesReleaseBundle(subject, installed, entry, bundle); err != nil {
		return nil, LockEntry{}, err
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

func validateInstalledPackageMatchesReleaseBundle(subject string, installed *installedPackage, entry LockEntry, bundle providerReleaseValidationBundle) error {
	manifest, err := staticvalidation.ProjectManifest(installed.Manifest, "", true)
	if err != nil {
		return fmt.Errorf("%s project installed validation manifest: %w", subject, err)
	}
	addCatalogOperationIDsToManifest(manifest, bundle.Catalog)
	if !providerManifestsEqual(manifest, entry.ValidationManifest) {
		return fmt.Errorf("%s installed package manifest does not match provider release validation manifest", subject)
	}
	if entry.CatalogSessionOnly {
		return nil
	}
	installedCatalog, err := packageio.ReadStaticCatalog(installed.Root, installed.Manifest.Source)
	if err != nil {
		return fmt.Errorf("%s read installed validation catalog: %w", subject, err)
	}
	if !catalogsEqual(installedCatalog, bundle.Catalog) {
		return fmt.Errorf("%s installed package catalog does not match provider release validation catalog", subject)
	}
	return nil
}

func providerManifestsEqual(a, b *providermanifestv1.Manifest) bool {
	aData, aErr := json.Marshal(a)
	bData, bErr := json.Marshal(b)
	return aErr == nil && bErr == nil && bytes.Equal(aData, bData)
}

func catalogsEqual(a, b *catalog.Catalog) bool {
	aData, aErr := json.Marshal(a)
	bData, bErr := json.Marshal(b)
	return aErr == nil && bErr == nil && bytes.Equal(aData, bData)
}

func (l *Lifecycle) resolveLockedProvider(ctx context.Context, cfg *config.Config, paths lifecyclePaths, kind, name, subject, destDir string, provider *config.ProviderEntry, configMap map[string]any, mode artifactMode) (LockEntry, error) {
	if provider == nil || !sourceBacked(provider) {
		return LockEntry{}, fmt.Errorf("%s requires source configuration", subject)
	}
	isAppProvider := kind == providermanifestv1.KindApp && destDir == providerDestDir(paths, name)
	if provider.HasLocalSource() {
		install, err := prepareLocalSourceInstall(kind, name, provider.SourcePath(), destDir)
		if err != nil {
			return LockEntry{}, err
		}
		if err := validateInstalledManifestKind(kind, name, install.manifest); err != nil {
			return LockEntry{}, err
		}
		if err := providerpkg.ValidateConfigForManifest(install.manifestPath, install.manifest, kind, configMap); err != nil {
			return LockEntry{}, fmt.Errorf("provider config validation for %s: %w", subject, err)
		}
		if kind == providermanifestv1.KindUI {
			return localUILockEntryFromPreparedInstall(paths, name, provider, install)
		}
		return localLockEntryFromPreparedInstall(paths, kind, name, provider, install)
	}
	if provider.HasGitSource() {
		switch {
		case kind == providermanifestv1.KindUI:
			return l.lockGitUIEntryForSource(ctx, cfg, paths, name, provider, destDir, subject, configMap, mode)
		case isAppProvider:
			return l.lockGitProviderEntryForSource(ctx, cfg, paths, name, provider, configMap, mode)
		default:
			return l.lockGitComponentEntryForSource(ctx, cfg, paths, kind, name, destDir, provider, configMap, mode)
		}
	}

	sourceLocation := provider.SourceReleaseLocation()
	if !provider.HasReleaseMetadataSource() {
		return LockEntry{}, fmt.Errorf("%s source %q: only provider-release metadata sources and local manifest paths are supported", subject, sourceLocation)
	}
	installed, entry, err := l.installMetadataSourcePackage(ctx, kind, name, subject, destDir, provider, paths.configDir, mode)
	if err != nil {
		return LockEntry{}, err
	}
	if installed == nil {
		if err := providerpkg.ValidateConfigForManifest("", entry.ValidationManifest, kind, configMap); err != nil {
			return LockEntry{}, fmt.Errorf("provider config validation for %s: %w", subject, err)
		}
		fingerprintName := name
		if kind == providermanifestv1.KindUI {
			fingerprintName = "ui:" + name
		}
		fingerprint, err := ProviderFingerprint(fingerprintName, provider, paths.configDir)
		if err != nil {
			return LockEntry{}, fmt.Errorf("fingerprinting %s: %w", subject, err)
		}
		entry.InputDigest = fingerprint
		provider.ResolvedManifest = entry.ValidationManifest
		provider.ResolvedManifestPath = ""
		bindLockValidationCatalog(provider, entry)
		return entry, nil
	}
	if err := providerpkg.ValidateConfigForManifest(installed.ManifestPath, installed.Manifest, kind, configMap); err != nil {
		return LockEntry{}, fmt.Errorf("provider config validation for %s: %w", subject, err)
	}
	fingerprintName := name
	if kind == providermanifestv1.KindUI {
		fingerprintName = "ui:" + name
	}
	fingerprint, err := ProviderFingerprint(fingerprintName, provider, paths.configDir)
	if err != nil {
		return LockEntry{}, fmt.Errorf("fingerprinting %s: %w", subject, err)
	}
	manifestPath, err := filepath.Rel(paths.artifactsDir, installed.ManifestPath)
	if err != nil {
		return LockEntry{}, fmt.Errorf("compute manifest path for %s: %w", subject, err)
	}
	entry.InputDigest = fingerprint
	entry.ArtifactManifest = filepath.ToSlash(manifestPath)
	switch {
	case kind == providermanifestv1.KindUI:
		assetRoot, err := filepath.Rel(paths.artifactsDir, installed.AssetRoot)
		if err != nil {
			return LockEntry{}, fmt.Errorf("compute asset root path for %s: %w", subject, err)
		}
		entry.AssetRoot = filepath.ToSlash(assetRoot)
	case isAppProvider:
		executableRel := ""
		if installed.ExecutablePath != "" {
			executableRel, err = filepath.Rel(paths.artifactsDir, installed.ExecutablePath)
			if err != nil {
				return LockEntry{}, fmt.Errorf("compute executable path for %s: %w", subject, err)
			}
		}
		entry.Executable = filepath.ToSlash(executableRel)
	default:
		entrypoint := providerpkg.EntrypointForKind(installed.Manifest, kind)
		if entrypoint == nil {
			return LockEntry{}, fmt.Errorf("%s manifest does not define a %s entrypoint", subject, kind)
		}
		executablePath, err := filepath.Rel(paths.artifactsDir, filepath.Join(installed.Root, filepath.FromSlash(entrypoint.ArtifactPath)))
		if err != nil {
			return LockEntry{}, fmt.Errorf("compute executable path for %s: %w", subject, err)
		}
		entry.Executable = filepath.ToSlash(executablePath)
	}
	if err := bindResolvedInstall(paths, kind, name, subject, destDir, provider, configMap, installed, isAppProvider); err != nil {
		return LockEntry{}, err
	}
	return entry, nil
}

func lockEntryStaticValidationOnly(entry LockEntry) bool {
	return entry.ValidationManifest != nil &&
		entry.ArtifactManifest == "" &&
		entry.Executable == "" &&
		entry.AssetRoot == ""
}

func lockEntryHasCompleteStaticValidation(kind string, entry LockEntry) bool {
	if entry.ValidationManifest == nil || providerrelease.ManifestReferencesPackageFiles(entry.ValidationManifest) {
		return false
	}
	if providerrelease.CatalogRequired(kind, entry.ValidationManifest) {
		return entry.CatalogAvailable || entry.CatalogSessionOnly
	}
	return true
}

func bindResolvedInstall(paths lifecyclePaths, kind, name, subject, destDir string, provider *config.ProviderEntry, configMap map[string]any, install *installedPackage, isAppProvider bool) error {
	switch {
	case kind == providermanifestv1.KindUI:
		if install.AssetRoot == "" {
			return fmt.Errorf("prepared asset root for %s not found in %s", subject, destDir)
		}
		if _, err := os.Stat(install.AssetRoot); err != nil {
			return fmt.Errorf("prepared asset root for %s not found at %s", subject, install.AssetRoot)
		}
		return bindResolvedUIManifest(provider, install.ManifestPath, install.Manifest, configMap)
	case isAppProvider:
		if err := bindResolvedProviderManifest(name, provider, install.ManifestPath, install.Manifest, configMap); err != nil {
			return err
		}
		if install.ExecutablePath == "" {
			return nil
		}
		if _, err := os.Stat(install.ExecutablePath); err != nil {
			return preparedArtifactStaleError(paths, "prepared executable for provider %q not found at %s", name, install.ExecutablePath)
		}
		args, err := providerEntrypointArgs(install.Manifest)
		if err != nil {
			return fmt.Errorf("resolve entrypoint for provider %q: %w", name, err)
		}
		provider.Command = install.ExecutablePath
		provider.Args = append([]string(nil), args...)
		return nil
	default:
		if install.ExecutablePath == "" {
			return preparedArtifactStaleError(paths, "prepared executable for %s %q not found in %s", kind, name, destDir)
		}
		if err := bindResolvedComponentManifest(kind, name, provider, install.ManifestPath, install.Manifest, configMap); err != nil {
			return err
		}
		if _, err := os.Stat(install.ExecutablePath); err != nil {
			return preparedArtifactStaleError(paths, "prepared executable for %s %q not found at %s", kind, name, install.ExecutablePath)
		}
		args, err := componentEntrypointArgs(install.Manifest, kind)
		if err != nil {
			return fmt.Errorf("resolve entrypoint for %s %q: %w", kind, name, err)
		}
		provider.Command = install.ExecutablePath
		provider.Args = append([]string(nil), args...)
		return nil
	}
}

func (l *Lifecycle) applyPreparedProviders(paths lifecyclePaths, lock *Lockfile, cfg *config.Config, mode artifactMode, opts SyncOptions) error {
	if !configHasProviderLoading(cfg) {
		return nil
	}

	if err := l.resolveConfiguredPluginsWithOptions(paths, lock, cfg, mode, opts); err != nil {
		return err
	}
	if err := synthesizePluginOwnedUIEntries(cfg); err != nil {
		return err
	}
	if paths.syncMetrics != nil {
		paths.syncMetrics.SetArtifactRoots(preparedArtifactRoots(paths, cfg))
	}
	l.prefetchComponentMaterializedCache(paths, lock, cfg, mode, opts.Parallelism)
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
		runtimeLocks = lock.Providers.Runtime
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
		indexedDBLocks = lock.Providers.IndexedDB
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
		s3Locks = lock.Providers.S3
	}
	for name, def := range cfg.Providers.S3 {
		if def != nil {
			if err := l.applyComponentProvider(paths, s3Locks, providermanifestv1.KindS3, name, def, def.Config, &def.Config, s3DestDir(paths, name), mode); err != nil {
				return err
			}
		}
	}
	return l.applyPreparedUIProviders(paths, lock, cfg, mode, opts)
}

type preparedUIWork struct {
	name      string
	entry     *config.UIEntry
	lockEntry *LockEntry
	configMap map[string]any
	subject   string
	destDir   string
	install   *preparedInstall
}

func (l *Lifecycle) applyPreparedUIProviders(paths lifecyclePaths, lock *Lockfile, cfg *config.Config, mode artifactMode, opts SyncOptions) error {
	var localWork []*preparedUIWork
	var ordered []*preparedUIWork
	for _, name := range slices.Sorted(maps.Keys(cfg.Providers.UI)) {
		entry := cfg.Providers.UI[name]
		if entry == nil {
			continue
		}
		if entry.DevActive {
			continue
		}
		configMap, err := config.NodeToMap(entry.Config)
		if err != nil {
			return fmt.Errorf("decode ui %q config: %w", name, err)
		}
		var lockEntry *LockEntry
		if lock != nil {
			if le, ok := lock.Providers.UI[name]; ok {
				lockEntry = &le
			}
		}
		work := &preparedUIWork{
			name:      name,
			entry:     entry,
			lockEntry: lockEntry,
			configMap: configMap,
			subject:   "ui " + strconv.Quote(name),
			destDir:   uiDestDir(paths, name),
		}
		ordered = append(ordered, work)
		if localUIParallelPrepareCandidate(paths, entry, mode) {
			localWork = append(localWork, work)
		}
	}

	if err := l.prepareLocalUIProviders(paths, localWork, opts); err != nil {
		return err
	}

	for _, work := range ordered {
		if err := resolveUIThemeConfig(paths, work.name, work.entry); err != nil {
			return err
		}
		if work.install != nil {
			resolvedAssetRoot, err := bindPreparedUIInstall(&work.entry.ProviderEntry, work.subject, work.destDir, work.configMap, work.install)
			if err != nil {
				return err
			}
			work.entry.ResolvedAssetRoot = resolvedAssetRoot
			continue
		}
		resolvedAssetRoot, err := l.applyConfiguredUIProvider(paths, work.lockEntry, &work.entry.ProviderEntry, work.name, work.subject, work.destDir, mode)
		if err != nil {
			return err
		}
		work.entry.ResolvedAssetRoot = resolvedAssetRoot
	}
	return nil
}

func localUIParallelPrepareCandidate(paths lifecyclePaths, entry *config.UIEntry, mode artifactMode) bool {
	if mode != artifactModeMaterialize || entry == nil || !entry.HasLocalSource() || entry.DevActive {
		return false
	}
	if pathWithinRoot(filepath.Join(paths.artifactsDir, ".gestaltd"), entry.SourcePath()) {
		return false
	}
	return true
}

func (l *Lifecycle) prepareLocalUIProviders(paths lifecyclePaths, work []*preparedUIWork, opts SyncOptions) error {
	if len(work) == 0 {
		return nil
	}
	opts = normalizeSyncOptions(opts)
	tasks := make([]pathDomainTask, 0, len(work))
	for _, work := range work {
		work := work
		domains, err := localUISourcePathDomains(paths, work.name, work.entry, work.destDir)
		if err != nil {
			return err
		}
		tasks = append(tasks, pathDomainTask{
			name:    work.name,
			domains: domains,
			run: func() error {
				install, err := l.ensureLocalPreparedInstall(paths, providermanifestv1.KindUI, work.name, &work.entry.ProviderEntry, work.configMap, work.destDir, work.subject, artifactModeMaterialize)
				if err != nil {
					return err
				}
				work.install = install
				return nil
			},
		})
	}
	slices.SortFunc(tasks, func(a, b pathDomainTask) int {
		return cmp.Compare(a.name, b.name)
	})
	return runPathDomainTasks(tasks, opts.Parallelism)
}

func localUISourcePathDomains(paths lifecyclePaths, name string, entry *config.UIEntry, destDir string) ([]string, error) {
	if entry == nil {
		return nil, nil
	}
	normalized, err := normalizeLocalSource(entry.SourcePath())
	if err != nil {
		return nil, fmt.Errorf("manifest for ui %q not found at %s: %w", name, entry.SourcePath(), err)
	}
	domainPaths := []string{
		normalized.sourceDir,
		normalized.manifestPath,
		destDir,
	}
	_, manifest, err := providerpkg.ReadSourceManifestFile(normalized.manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", normalized.manifestPath, err)
	}
	buildDomains, err := sourceBuildPathDomains(normalized.sourceDir, manifest)
	if err != nil {
		return nil, err
	}
	domainPaths = append(domainPaths, buildDomains...)
	return normalizePathDomains(domainPaths...)
}

func sourceBuildPathDomains(sourceDir string, manifest *providermanifestv1.Manifest) ([]string, error) {
	build := providerpkg.EffectiveSourceBuild(manifest)
	if build == nil {
		return nil, nil
	}
	workdir := sourceDir
	if build.Workdir != "" && build.Workdir != "." {
		workdir = filepath.Join(sourceDir, filepath.FromSlash(build.Workdir))
	}
	outputRel, _, err := providerpkg.SourceBuildOutput(manifest)
	if err != nil {
		return nil, err
	}
	domains := []string{workdir}
	if outputRel != "" {
		domains = append(domains, filepath.Join(sourceDir, filepath.FromSlash(outputRel)))
	}
	return domains, nil
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
		if locked && err != nil && os.IsNotExist(err) {
			lock = newLockfile()
			err = nil
		}
	}
	if !locked && (err != nil || !lockFreshForConfig(cfg, paths, lock, lockFreshnessOptions{RequireArtifacts: true}) || (bootstrapLock == nil && configHasLocalProviderSources(cfg)) || (bootstrapLock == nil && configHasMetadataProviderSources(cfg))) {
		lock, err = l.PrepareAtPathsWithStatePaths(configPaths, state)
		validatedDuringPrepare = err == nil
	}
	if err != nil {
		return false, fmt.Errorf("source-backed providers require lock metadata; full config mode prepares all source-backed providers. For local single-app development, use `gestaltd serve --path PATH` or a layered local config; otherwise run `%s` then `%s`: %w", formatLockCommand(paths), formatSyncLockedCommand(paths), err)
	}
	if err := l.applyPreparedProviders(paths, lock, cfg, mode, SyncOptions{Parallelism: 1}); err != nil {
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
		if err := activatePreparedInstallDir(destDir, tempDir); err != nil {
			return err
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

func attachStaticValidationMetadata(lock *Lockfile, cfg *config.Config, catalogs map[string]appservice.EffectiveCatalogResult) error {
	if lock == nil || cfg == nil {
		return nil
	}
	attach := func(entries map[string]LockEntry, name string, entry *config.ProviderEntry) error {
		if entries == nil || entry == nil || entry.ResolvedManifest == nil {
			return nil
		}
		lockEntry, ok := entries[name]
		if !ok {
			return nil
		}
		if lockEntry.ValidationManifest == nil {
			staticManifest, err := portableStaticValidationManifest(entry.ResolvedManifest, entry.ResolvedManifestPath, true)
			if err != nil {
				return fmt.Errorf("project static validation manifest for %s: %w", name, err)
			}
			lockEntry.ValidationManifest = staticManifest
		}
		entries[name] = lockEntry
		return nil
	}
	for name, entry := range cfg.Apps {
		if err := attach(lock.Providers.App, name, entry); err != nil {
			return err
		}
		if entry == nil || entry.ResolvedManifest == nil {
			continue
		}
		lockEntry, ok := lock.Providers.App[name]
		if !ok {
			continue
		}
		if !lockEntryStaticValidationOnly(lockEntry) {
			resolved := catalogs[name]
			if resolved.SessionOnly {
				lockEntry.CatalogSessionOnly = true
				lockEntry.CatalogAvailable = false
			} else {
				addCatalogOperationIDsToManifest(lockEntry.ValidationManifest, resolved.Catalog)
				if resolved.Available {
					lockEntry.CatalogAvailable = true
				}
			}
		}
		fingerprint, err := staticCatalogInputFingerprint(entry)
		if err != nil {
			return err
		}
		if lockEntry.CatalogFingerprint == "" {
			lockEntry.CatalogFingerprint = fingerprint
		}
		lock.Providers.App[name] = lockEntry
		bindLockValidationCatalog(entry, lockEntry)
	}
	for _, collection := range hostProviderCollections(cfg) {
		entries := lockEntriesForKind(lock, collection.kind)
		for name, entry := range collection.entries {
			if err := attach(entries, name, entry); err != nil {
				return err
			}
		}
	}
	for name, entry := range cfg.Runtime.Providers {
		if entry != nil {
			if err := attach(lock.Providers.Runtime, name, &entry.ProviderEntry); err != nil {
				return err
			}
		}
	}
	for name, entry := range cfg.Providers.IndexedDB {
		if err := attach(lock.Providers.IndexedDB, name, entry); err != nil {
			return err
		}
	}
	for name, entry := range cfg.Providers.S3 {
		if err := attach(lock.Providers.S3, name, entry); err != nil {
			return err
		}
	}
	for name, entry := range cfg.Providers.UI {
		if entry != nil {
			if err := attach(lock.Providers.UI, name, &entry.ProviderEntry); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateResolvedStructureForCommittedLock(paths lifecyclePaths, cfg *config.Config) error {
	if cfg == nil {
		return nil
	}
	validation := &config.Config{
		Apps: make(map[string]*config.ProviderEntry),
		Providers: config.ProvidersConfig{
			UI: make(map[string]*config.UIEntry),
		},
	}
	for name, entry := range cfg.Apps {
		if providerRequiresCommittedLock(entry) {
			validation.Apps[name] = entry
		}
	}
	for name, entry := range cfg.Providers.UI {
		if entry == nil {
			continue
		}
		if providerRequiresCommittedLock(&entry.ProviderEntry) || synthesizedCommittedOwnedUIEntry(paths, cfg, name, entry) {
			validation.Providers.UI[name] = entry
		}
	}
	return config.ValidateResolvedStructure(validation)
}

func effectiveCatalogsForCommittedLock(ctx context.Context, cfg *config.Config, lock *Lockfile) (map[string]appservice.EffectiveCatalogResult, error) {
	if cfg == nil || lock == nil || len(lock.Providers.App) == 0 {
		return nil, nil
	}
	validation := &appservice.ValidationConfig{
		Apps: make(map[string]*appservice.ValidationApp, len(cfg.Apps)),
	}
	for name, entry := range cfg.Apps {
		if entry == nil {
			continue
		}
		if _, locked := lock.Providers.App[name]; locked {
			validation.Apps[name] = config.AppValidationEntry(entry)
			continue
		}
		if entry.HasLocalSource() {
			validation.Apps[name] = &appservice.ValidationApp{
				EffectiveCatalogAvailable: true,
				StaticMetadataUnavailable: true,
			}
		}
	}
	results, err := appservice.EffectiveCatalogsAndDependencies(ctx, validation)
	if err != nil {
		return nil, err
	}
	for _, name := range slices.Sorted(maps.Keys(results)) {
		if _, locked := lock.Providers.App[name]; !locked {
			delete(results, name)
		}
	}
	return results, nil
}

func synthesizedCommittedOwnedUIEntry(paths lifecyclePaths, cfg *config.Config, name string, entry *config.UIEntry) bool {
	if cfg == nil || entry == nil || !entry.HasLocalSource() {
		return false
	}
	if !pathWithinRoot(filepath.Join(paths.artifactsDir, ".gestaltd"), entry.SourcePath()) {
		return false
	}
	owner := strings.TrimSpace(entry.OwnerApp)
	if owner == "" {
		owner = name
	}
	return providerRequiresCommittedLock(cfg.Apps[owner])
}

func portableStaticValidationManifest(manifest *providermanifestv1.Manifest, manifestPath string, platformNeutral bool) (*providermanifestv1.Manifest, error) {
	return staticvalidation.ProjectManifest(manifest, manifestPath, platformNeutral)
}

func addCatalogOperationIDsToManifest(manifest *providermanifestv1.Manifest, cat *catalog.Catalog) {
	if manifest == nil || cat == nil || len(cat.Operations) == 0 {
		return
	}
	if manifest.Spec == nil {
		manifest.Spec = &providermanifestv1.Spec{}
	}
	if manifest.Spec.AllowedOperations == nil {
		manifest.Spec.AllowedOperations = make(map[string]*providermanifestv1.ManifestOperationOverride)
	}
	for _, id := range staticCatalogOperationIDs(cat) {
		if _, ok := manifest.Spec.AllowedOperations[id]; !ok {
			manifest.Spec.AllowedOperations[id] = &providermanifestv1.ManifestOperationOverride{}
		}
	}
}

func staticCatalogOperationIDs(cat *catalog.Catalog) []string {
	if cat == nil || len(cat.Operations) == 0 {
		return nil
	}
	ids := make([]string, 0, len(cat.Operations))
	for i := range cat.Operations {
		id := strings.TrimSpace(cat.Operations[i].ID)
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func catalogFromValidationManifest(manifest *providermanifestv1.Manifest, available bool) *catalog.Catalog {
	if !available {
		return nil
	}
	var ids []string
	if manifest != nil && manifest.Spec != nil {
		ids = slices.Sorted(maps.Keys(manifest.Spec.AllowedOperations))
	}
	cat := &catalog.Catalog{
		Operations: make([]catalog.CatalogOperation, 0, len(ids)),
	}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		cat.Operations = append(cat.Operations, catalog.CatalogOperation{ID: id})
	}
	return cat
}

func bindLockValidationCatalog(provider *config.ProviderEntry, entry LockEntry) {
	if provider == nil {
		return
	}
	provider.ResolvedCatalog = catalogFromValidationManifest(entry.ValidationManifest, entry.CatalogAvailable)
	provider.ResolvedCatalogAvailable = entry.CatalogAvailable
	provider.ResolvedCatalogSessionOnly = entry.CatalogSessionOnly
}

func staticCatalogInputFingerprint(entry *config.ProviderEntry) (string, error) {
	type input struct {
		AllowedOperations map[string]*config.OperationOverride `json:"allowedOperations,omitempty"`
		GraphQLSurfaceURL string                               `json:"graphqlSurfaceUrl,omitempty"`
		MCPSurfaceURL     string                               `json:"mcpSurfaceUrl,omitempty"`
	}
	var allowed map[string]*config.OperationOverride
	var graphQLURL, mcpURL string
	if entry != nil {
		allowed = entry.AllowedOperations
		graphQLURL = config.ProviderSurfaceURLOverride(entry, config.SpecSurfaceGraphQL)
		mcpURL = config.ProviderSurfaceURLOverride(entry, config.SpecSurfaceMCP)
	}
	payload, err := json.Marshal(input{
		AllowedOperations: allowed,
		GraphQLSurfaceURL: graphQLURL,
		MCPSurfaceURL:     mcpURL,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func configRequiresStaticLock(cfg *config.Config) bool {
	return configRequiresCommittedLock(cfg)
}

func configRequiresRemoteStaticLock(cfg *config.Config) bool {
	return configHasMatchingProviderEntry(cfg, func(entry *config.ProviderEntry) bool {
		return sourceBacked(entry) && !entry.HasLocalSource() && !entry.HasLocalReleaseSource()
	})
}

func configHasMatchingProviderEntry(cfg *config.Config, matches func(*config.ProviderEntry) bool) bool {
	if cfg == nil || matches == nil {
		return false
	}
	for _, entry := range cfg.Apps {
		if matches(entry) {
			return true
		}
	}
	for _, collection := range hostProviderCollections(cfg) {
		for _, entry := range collection.entries {
			if matches(entry) {
				return true
			}
		}
	}
	for _, entry := range cfg.Runtime.Providers {
		if entry != nil && matches(&entry.ProviderEntry) {
			return true
		}
	}
	for _, entry := range cfg.Providers.IndexedDB {
		if matches(entry) {
			return true
		}
	}
	for _, entry := range cfg.Providers.S3 {
		if matches(entry) {
			return true
		}
	}
	for _, entry := range cfg.Providers.UI {
		if entry != nil && matches(&entry.ProviderEntry) {
			return true
		}
	}
	return false
}

func (l *Lifecycle) applyStaticValidationProviders(ctx context.Context, paths lifecyclePaths, lock *Lockfile, cfg *config.Config, platform string) error {
	if !configHasProviderLoading(cfg) {
		return nil
	}
	for name, entry := range cfg.Apps {
		if entry == nil {
			continue
		}
		configMap, err := config.NodeToMap(entry.Config)
		if err != nil {
			return fmt.Errorf("decode provider config for provider %q: %w", name, err)
		}
		if err := l.applyStaticValidationEntry(ctx, paths, lockEntriesForProviderKind(lock, providermanifestv1.KindApp), providermanifestv1.KindApp, name, entry, configMap, providerDestDir(paths, name), platform, false, func(manifestPath string, manifest *providermanifestv1.Manifest) error {
			return bindResolvedProviderManifest(name, entry, manifestPath, manifest, configMap)
		}); err != nil {
			return err
		}
		if entry.ResolvedManifest != nil {
			entry.DisplayName = cmp.Or(entry.DisplayName, entry.ResolvedManifest.DisplayName)
			entry.Description = cmp.Or(entry.Description, entry.ResolvedManifest.Description)
		}
		entry.IconFile = cmp.Or(entry.IconFile, entry.ResolvedIconFile)
	}
	if err := synthesizePluginOwnedUIEntries(cfg); err != nil {
		return err
	}
	for _, collection := range hostProviderCollections(cfg) {
		lockEntries := lockEntriesForKind(lock, collection.kind)
		kind := providerManifestKind(collection.kind)
		for name, entry := range collection.entries {
			if entry == nil {
				continue
			}
			configMap, err := config.NodeToMap(entry.Config)
			if err != nil {
				return fmt.Errorf("decode provider config for %s %q: %w", kind, name, err)
			}
			if err := l.applyStaticValidationEntry(ctx, paths, lockEntries, kind, name, entry, configMap, componentDestDir(paths, collection.kind, name), platform, false, func(manifestPath string, manifest *providermanifestv1.Manifest) error {
				return bindResolvedComponentManifest(kind, name, entry, manifestPath, manifest, configMap)
			}); err != nil {
				return err
			}
		}
	}
	for name, entry := range cfg.Runtime.Providers {
		if entry == nil {
			continue
		}
		configMap, err := config.NodeToMap(entry.Config)
		if err != nil {
			return fmt.Errorf("decode provider config for %s %q: %w", providermanifestv1.KindRuntime, name, err)
		}
		if err := l.applyStaticValidationEntry(ctx, paths, lockEntriesForProviderKind(lock, providermanifestv1.KindRuntime), providermanifestv1.KindRuntime, name, &entry.ProviderEntry, configMap, runtimeDestDir(paths, name), platform, false, func(manifestPath string, manifest *providermanifestv1.Manifest) error {
			return bindResolvedComponentManifest(providermanifestv1.KindRuntime, name, &entry.ProviderEntry, manifestPath, manifest, configMap)
		}); err != nil {
			return err
		}
	}
	for name, entry := range cfg.Providers.IndexedDB {
		if entry == nil {
			continue
		}
		configMap, err := config.NodeToMap(entry.Config)
		if err != nil {
			return fmt.Errorf("decode provider config for %s %q: %w", providermanifestv1.KindIndexedDB, name, err)
		}
		if err := l.applyStaticValidationEntry(ctx, paths, lockEntriesForProviderKind(lock, providermanifestv1.KindIndexedDB), providermanifestv1.KindIndexedDB, name, entry, configMap, indexeddbDestDir(paths, name), platform, false, func(manifestPath string, manifest *providermanifestv1.Manifest) error {
			return bindResolvedComponentManifest(providermanifestv1.KindIndexedDB, name, entry, manifestPath, manifest, configMap)
		}); err != nil {
			return err
		}
	}
	for name, entry := range cfg.Providers.S3 {
		if entry == nil {
			continue
		}
		configMap, err := config.NodeToMap(entry.Config)
		if err != nil {
			return fmt.Errorf("decode provider config for %s %q: %w", providermanifestv1.KindS3, name, err)
		}
		if err := l.applyStaticValidationEntry(ctx, paths, lockEntriesForProviderKind(lock, providermanifestv1.KindS3), providermanifestv1.KindS3, name, entry, configMap, s3DestDir(paths, name), platform, false, func(manifestPath string, manifest *providermanifestv1.Manifest) error {
			return bindResolvedComponentManifest(providermanifestv1.KindS3, name, entry, manifestPath, manifest, configMap)
		}); err != nil {
			return err
		}
	}
	for name, entry := range cfg.Providers.UI {
		if entry == nil {
			continue
		}
		configMap, err := config.NodeToMap(entry.Config)
		if err != nil {
			return fmt.Errorf("decode ui %q config: %w", name, err)
		}
		if err := l.applyStaticValidationEntry(ctx, paths, lockEntriesForProviderKind(lock, providermanifestv1.KindUI), providermanifestv1.KindUI, name, &entry.ProviderEntry, configMap, uiDestDir(paths, name), platform, true, func(manifestPath string, manifest *providermanifestv1.Manifest) error {
			return bindResolvedUIManifest(&entry.ProviderEntry, manifestPath, manifest, configMap)
		}); err != nil {
			return err
		}
	}
	return nil
}

func (l *Lifecycle) applyStaticValidationEntry(ctx context.Context, paths lifecyclePaths, lockEntries map[string]LockEntry, kind, name string, provider *config.ProviderEntry, configMap map[string]any, destDir, platform string, ui bool, bind func(string, *providermanifestv1.Manifest) error) error {
	if provider == nil || !sourceBacked(provider) {
		return nil
	}
	if provider.HasLocalSource() {
		install, cleanup, _, err := stageLocalSourceInstall(kind, name, provider.SourcePath(), destDir, providerpkg.StageSourcePreparedInstallOptions{})
		if err != nil {
			if cleanup != nil {
				_ = cleanup()
			}
			return err
		}
		if err := bind(install.manifestPath, install.manifest); err != nil {
			if cleanup != nil {
				_ = cleanup()
			}
			return err
		}
		return nil
	}
	var entry LockEntry
	found := false
	if lockEntries != nil {
		entry, found = lockEntries[name]
	}
	if found {
		if ui {
			if !uiLockEntryMetadataMatches(paths, name, provider, entry, true) {
				return lockMetadataStaleError(paths, "lock entry for ui %q is stale", name)
			}
		} else if !lockEntryMetadataMatches(paths, kind, name, provider, entry, true) {
			return lockMetadataStaleError(paths, "lock entry for %s %q is stale", kind, name)
		}
		if len(entry.Archives) > 0 {
			_, resolvedKey, ok := resolveArchiveForPlatform(entry, platform)
			if !ok {
				return fmt.Errorf("no archive for platform %s for %s %q; publish an explicit %s archive with `gestaltd provider package --platform %s` or use a generic package where allowed", platform, kind, name, platform, platform)
			}
			if err := validateStaticLockedArchivePolicy(archivePolicySubject(kind, name), archivePolicyKind(kind), entry, platform, resolvedKey); err != nil {
				return err
			}
		}
		if lockEntryHasCompleteStaticValidation(kind, entry) {
			if kind == providermanifestv1.KindApp {
				fingerprint, err := staticCatalogInputFingerprint(provider)
				if err != nil {
					return err
				}
				if entry.CatalogFingerprint != "" && entry.CatalogFingerprint != fingerprint {
					return lockMetadataStaleError(paths, "lock entry for %s %q is stale", kind, name)
				}
			}
			bindLockValidationCatalog(provider, entry)
			return bind("", entry.ValidationManifest)
		}
	}
	if !found {
		return lockMetadataStaleError(paths, "lock entry for %s %q is missing or stale", kind, name)
	}
	return lockMetadataStaleError(paths, "lock entry for %s %q does not include static validation metadata", kind, name)
}

func staticValidationNeedsSourceAuthSecrets(paths lifecyclePaths, cfg *config.Config, lock *Lockfile, platform string) (bool, error) {
	if cfg == nil {
		return false, nil
	}
	check := func(lockEntries map[string]LockEntry, kind, name string, provider *config.ProviderEntry, ui bool) (bool, error) {
		return staticValidationEntryNeedsSourceAuthSecret(paths, lockEntries, kind, name, provider, platform, ui)
	}
	for name, entry := range cfg.Apps {
		needs, err := check(lockEntriesForProviderKind(lock, providermanifestv1.KindApp), providermanifestv1.KindApp, name, entry, false)
		if needs || err != nil {
			return needs, err
		}
	}
	for _, collection := range hostProviderCollections(cfg) {
		kind := providerManifestKind(collection.kind)
		lockEntries := lockEntriesForKind(lock, collection.kind)
		for name, entry := range collection.entries {
			needs, err := check(lockEntries, kind, name, entry, false)
			if needs || err != nil {
				return needs, err
			}
		}
	}
	for name, entry := range cfg.Runtime.Providers {
		if entry == nil {
			continue
		}
		needs, err := check(lockEntriesForProviderKind(lock, providermanifestv1.KindRuntime), providermanifestv1.KindRuntime, name, &entry.ProviderEntry, false)
		if needs || err != nil {
			return needs, err
		}
	}
	for name, entry := range cfg.Providers.IndexedDB {
		needs, err := check(lockEntriesForProviderKind(lock, providermanifestv1.KindIndexedDB), providermanifestv1.KindIndexedDB, name, entry, false)
		if needs || err != nil {
			return needs, err
		}
	}
	for name, entry := range cfg.Providers.S3 {
		needs, err := check(lockEntriesForProviderKind(lock, providermanifestv1.KindS3), providermanifestv1.KindS3, name, entry, false)
		if needs || err != nil {
			return needs, err
		}
	}
	for name, entry := range cfg.Providers.UI {
		if entry == nil {
			continue
		}
		needs, err := check(lockEntriesForProviderKind(lock, providermanifestv1.KindUI), providermanifestv1.KindUI, name, &entry.ProviderEntry, true)
		if needs || err != nil {
			return needs, err
		}
	}
	return false, nil
}

func staticValidationEntryNeedsSourceAuthSecret(paths lifecyclePaths, lockEntries map[string]LockEntry, kind, name string, provider *config.ProviderEntry, platform string, ui bool) (bool, error) {
	usesSecret, err := providerSourceAuthUsesSecretRef(provider)
	if err != nil || !usesSecret {
		return false, err
	}
	if provider == nil || !sourceBacked(provider) || provider.HasLocalSource() {
		return false, nil
	}
	var entry LockEntry
	found := false
	if lockEntries != nil {
		entry, found = lockEntries[name]
	}
	if !found {
		return false, nil
	}
	if ui {
		if !uiLockEntryMetadataMatches(paths, name, provider, entry, true) {
			return false, nil
		}
	} else if !lockEntryMetadataMatches(paths, kind, name, provider, entry, true) {
		return false, nil
	}
	if entry.ValidationManifest != nil && !providerrelease.ManifestReferencesPackageFiles(entry.ValidationManifest) {
		return false, nil
	}
	if provider.HasGitSource() && len(entry.Archives) == 0 {
		return true, nil
	}
	_, _, ok := resolveArchiveForPlatform(entry, platform)
	return ok, nil
}

func providerSourceAuthUsesSecretRef(provider *config.ProviderEntry) (bool, error) {
	if provider == nil || provider.Source.Auth == nil {
		return false, nil
	}
	_, ok, err := config.ParseSecretRefTransport(provider.Source.Auth.Token)
	if err != nil {
		return false, err
	}
	return ok, nil
}

type lockedArchiveDownloadError struct {
	err error
}

func (e lockedArchiveDownloadError) Error() string {
	return e.err.Error()
}

func (e lockedArchiveDownloadError) Unwrap() error {
	return e.err
}

type preparedAppWork struct {
	name          string
	entry         *config.ProviderEntry
	configMap     map[string]any
	localParallel bool
	install       *preparedInstall
}

func (l *Lifecycle) resolveConfiguredPlugins(paths lifecyclePaths, lock *Lockfile, cfg *config.Config, mode artifactMode) error {
	return l.resolveConfiguredPluginsWithOptions(paths, lock, cfg, mode, SyncOptions{Parallelism: 1})
}

func (l *Lifecycle) resolveConfiguredPluginsWithOptions(paths lifecyclePaths, lock *Lockfile, cfg *config.Config, mode artifactMode, opts SyncOptions) error {
	var localWork []*preparedAppWork
	var ordered []*preparedAppWork
	for _, name := range slices.Sorted(maps.Keys(cfg.Apps)) {
		entry := cfg.Apps[name]
		if entry == nil {
			continue
		}
		configMap, err := config.NodeToMap(entry.Config)
		if err != nil {
			return fmt.Errorf("decode provider config for provider %q: %w", name, err)
		}
		work := &preparedAppWork{
			name:          name,
			entry:         entry,
			configMap:     configMap,
			localParallel: localAppParallelPrepareCandidate(paths, entry, mode),
		}
		ordered = append(ordered, work)
		if work.localParallel {
			localWork = append(localWork, work)
		}
	}
	l.prefetchAppMaterializedCache(paths, lock, ordered, mode, opts.Parallelism)
	for _, work := range ordered {
		if work.localParallel || !sourceBacked(work.entry) {
			continue
		}
		if err := l.applyLockedProviderEntry(paths, lock, work.name, work.entry, work.configMap, mode); err != nil {
			return err
		}
	}
	if err := l.prepareLocalAppProviders(paths, localWork, opts); err != nil {
		return err
	}
	for _, work := range ordered {
		if work.install != nil {
			if err := bindPreparedProviderInstall(paths, work.name, work.entry, work.configMap, work.install); err != nil {
				return err
			}
		}
		if manifest := work.entry.ResolvedManifest; manifest != nil {
			work.entry.DisplayName = cmp.Or(work.entry.DisplayName, manifest.DisplayName)
			work.entry.Description = cmp.Or(work.entry.Description, manifest.Description)
		}
		work.entry.IconFile = cmp.Or(work.entry.IconFile, work.entry.ResolvedIconFile)
	}
	return nil
}

func localAppParallelPrepareCandidate(paths lifecyclePaths, entry *config.ProviderEntry, mode artifactMode) bool {
	if mode != artifactModeMaterialize || entry == nil || !entry.HasLocalSource() {
		return false
	}
	if pathWithinRoot(filepath.Join(paths.artifactsDir, ".gestaltd"), entry.SourcePath()) {
		return false
	}
	return true
}

func (l *Lifecycle) prepareLocalAppProviders(paths lifecyclePaths, work []*preparedAppWork, opts SyncOptions) error {
	if len(work) == 0 {
		return nil
	}
	opts = normalizeSyncOptions(opts)
	tasks := make([]pathDomainTask, 0, len(work))
	for _, work := range work {
		work := work
		domains, err := localAppSourcePathDomains(paths, work.name, work.entry)
		if err != nil {
			return err
		}
		tasks = append(tasks, pathDomainTask{
			name:    work.name,
			domains: domains,
			run: func() error {
				install, err := l.ensureLocalPreparedInstall(paths, providermanifestv1.KindApp, work.name, work.entry, work.configMap, providerDestDir(paths, work.name), "provider "+strconv.Quote(work.name), artifactModeMaterialize)
				if err != nil {
					return err
				}
				work.install = install
				return nil
			},
		})
	}
	slices.SortFunc(tasks, func(a, b pathDomainTask) int {
		return cmp.Compare(a.name, b.name)
	})
	return runPathDomainTasks(tasks, opts.Parallelism)
}

func localAppSourcePathDomains(paths lifecyclePaths, name string, entry *config.ProviderEntry) ([]string, error) {
	if entry == nil {
		return nil, nil
	}
	normalized, err := normalizeLocalSource(entry.SourcePath())
	if err != nil {
		return nil, fmt.Errorf("manifest for app %q not found at %s: %w", name, entry.SourcePath(), err)
	}
	domainPaths := []string{
		normalized.sourceDir,
		normalized.manifestPath,
		providerDestDir(paths, name),
	}
	_, manifest, err := providerpkg.ReadSourceManifestFile(normalized.manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", normalized.manifestPath, err)
	}
	buildDomains, err := sourceBuildPathDomains(normalized.sourceDir, manifest)
	if err != nil {
		return nil, err
	}
	domainPaths = append(domainPaths, buildDomains...)
	if manifest != nil && manifest.Kind == providermanifestv1.KindApp && manifest.Spec != nil && manifest.Spec.UI != nil && strings.TrimSpace(manifest.Spec.UI.Path) != "" {
		uiManifestPath := filepath.Join(normalized.sourceDir, filepath.FromSlash(manifest.Spec.UI.Path))
		uiNormalized, err := normalizeLocalSource(uiManifestPath)
		if err != nil {
			return nil, fmt.Errorf("manifest for provider %q owned ui not found at %s: %w", name, uiManifestPath, err)
		}
		domainPaths = append(domainPaths, uiNormalized.sourceDir, uiNormalized.manifestPath)
		_, uiManifest, err := providerpkg.ReadSourceManifestFile(uiNormalized.manifestPath)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", uiNormalized.manifestPath, err)
		}
		uiBuildDomains, err := sourceBuildPathDomains(uiNormalized.sourceDir, uiManifest)
		if err != nil {
			return nil, err
		}
		domainPaths = append(domainPaths, uiBuildDomains...)
	}
	return normalizePathDomains(domainPaths...)
}

func synthesizePluginOwnedUIEntries(cfg *config.Config) error {
	return synthesizePluginOwnedUIEntriesMatching(cfg, func(_ string, _ *config.ProviderEntry) bool {
		return true
	})
}

func synthesizeCommittedPluginOwnedUIEntries(cfg *config.Config) error {
	return synthesizePluginOwnedUIEntriesMatching(cfg, func(_ string, app *config.ProviderEntry) bool {
		return providerRequiresCommittedLock(app)
	})
}

func synthesizePluginOwnedUIEntriesMatching(cfg *config.Config, include func(string, *config.ProviderEntry) bool) error {
	if cfg == nil || len(cfg.Apps) == 0 {
		return nil
	}
	if cfg.Providers.UI == nil {
		cfg.Providers.UI = map[string]*config.UIEntry{}
	}

	pluginNames := slices.Sorted(maps.Keys(cfg.Apps))
	for _, pluginName := range pluginNames {
		app := cfg.Apps[pluginName]
		if app == nil || (include != nil && !include(pluginName, app)) || strings.TrimSpace(app.UI) != "" || strings.TrimSpace(app.MountPath) == "" {
			continue
		}
		manifestSpec := app.ManifestSpec()
		if manifestSpec == nil || manifestSpec.UI == nil {
			return fmt.Errorf("plugin %q ui.path requires spec.ui or apps.%s.ui.bundle", pluginName, pluginName)
		}
		ownedUI := manifestSpec.UI
		entry, err := ownedUIEntryForPlugin(app, ownedUI)
		if err != nil {
			return fmt.Errorf("plugin %q ui: %w", pluginName, err)
		}
		entry.Path = strings.TrimSpace(app.MountPath)
		entry.AuthorizationPolicy = strings.TrimSpace(app.AuthorizationPolicy)
		entry.OwnerApp = pluginName
		if existing := cfg.Providers.UI[pluginName]; existing != nil {
			if err := validateSynthesizedPluginUIEntry(pluginName, existing, entry); err != nil {
				return err
			}
			if existing.Source.Auth == nil && entry.Source.Auth != nil {
				existing.Source.Auth = entry.Source.Auth
			}
			existing.Path = cmp.Or(existing.Path, entry.Path)
			existing.AuthorizationPolicy = cmp.Or(existing.AuthorizationPolicy, entry.AuthorizationPolicy)
			existing.OwnerApp = cmp.Or(existing.OwnerApp, entry.OwnerApp)
			continue
		}
		cfg.Providers.UI[pluginName] = entry
	}
	return nil
}

func ownedUIEntryForPlugin(app *config.ProviderEntry, ownedUI *providermanifestv1.OwnedUI) (*config.UIEntry, error) {
	if app == nil || ownedUI == nil {
		return nil, fmt.Errorf("owned ui definition is required")
	}
	return ownedUIEntryFromManifest(app.ResolvedManifestPath, ownedUI)
}

func ownedUIEntryFromManifest(manifestPath string, ownedUI *providermanifestv1.OwnedUI) (*config.UIEntry, error) {
	if ownedUI == nil {
		return nil, fmt.Errorf("owned ui definition is required")
	}
	if strings.TrimSpace(ownedUI.Path) == "" {
		return nil, fmt.Errorf("spec.ui.path is required")
	}
	if strings.TrimSpace(manifestPath) == "" {
		return nil, fmt.Errorf("resolved app manifest path is required for spec.ui.path")
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
		return fmt.Errorf("config validation: apps.%s owned ui conflicts with providers.ui.%s.source", pluginName, pluginName)
	}
	if current := strings.TrimSpace(existing.Source.Path); current != "" && !equivalentProviderManifestPath(current, expected.Source.Path) {
		return fmt.Errorf("config validation: apps.%s owned ui conflicts with providers.ui.%s.source.path", pluginName, pluginName)
	}
	if current := strings.TrimSpace(existing.Path); current != "" && current != expected.Path {
		return fmt.Errorf("config validation: apps.%s.ui.path %q conflicts with providers.ui.%s.path", pluginName, expected.Path, pluginName)
	}
	if current := strings.TrimSpace(existing.AuthorizationPolicy); current != "" && current != expected.AuthorizationPolicy {
		return fmt.Errorf("config validation: apps.%s.authorizationPolicy conflicts with providers.ui.%s.authorizationPolicy", pluginName, pluginName)
	}
	if current := strings.TrimSpace(existing.OwnerApp); current != "" && current != expected.OwnerApp {
		return fmt.Errorf("config validation: apps.%s owned ui conflicts with providers.ui.%s owner", pluginName, pluginName)
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
	if currentErr != nil {
		_, currentManifest, currentErr = providerpkg.ReadSourceManifestFile(current)
	}
	_, expectedManifest, expectedErr := providerpkg.ReadManifestFile(expected)
	if expectedErr != nil {
		_, expectedManifest, expectedErr = providerpkg.ReadSourceManifestFile(expected)
	}
	if currentErr != nil || expectedErr != nil {
		return false
	}
	return currentManifest != nil && expectedManifest != nil &&
		currentManifest.Kind == expectedManifest.Kind &&
		currentManifest.Source == expectedManifest.Source &&
		currentManifest.Version == expectedManifest.Version
}

func (l *Lifecycle) ensureLocalPreparedInstall(paths lifecyclePaths, kind, name string, provider *config.ProviderEntry, configMap map[string]any, destDir, subject string, mode artifactMode) (*preparedInstall, error) {
	if provider == nil || !provider.HasLocalSource() {
		return nil, fmt.Errorf("%s requires local source configuration", subject)
	}
	start := time.Now()
	install, err := inspectPreparedInstall(destDir)
	needMaterialize := err != nil
	reason := syncArtifactReasonPreparedMissing
	if !needMaterialize {
		switch inspectPreparedLockMetadata(paths, install, kind, name, provider, mode) {
		case preparedLockMetadataMatch:
			needMaterialize = false
			reason = syncArtifactReasonFresh
		default:
			needMaterialize = true
			reason = syncArtifactReasonMetadataStale
		}
	}
	if needMaterialize {
		if !mode.canMaterialize() {
			return nil, preparedArtifactStaleError(paths, "prepared artifact for %s is missing or stale", subject)
		}
		prepareStart := time.Now()
		stagedInstall, cleanupStaged, commitStaged, err := stageLocalSourceInstall(kind, name, provider.SourcePath(), destDir, paths.stageOptions())
		prepareDuration := time.Since(prepareStart)
		if cleanupStaged != nil {
			defer func() { _ = cleanupStaged() }()
		}
		if err != nil {
			return nil, err
		}
		if err := validateInstalledManifestKind(kind, name, stagedInstall.manifest); err != nil {
			return nil, err
		}
		if err := providerpkg.ValidateConfigForManifest(stagedInstall.manifestPath, stagedInstall.manifest, kind, configMap); err != nil {
			return nil, fmt.Errorf("provider config validation for %s: %w", subject, err)
		}
		if err := writePreparedLockMetadata(paths, stagedInstall, kind, name, provider); err != nil {
			return nil, err
		}
		activateStart := time.Now()
		if err := commitStaged(); err != nil {
			return nil, err
		}
		activateDuration := time.Since(activateStart)
		install, err = inspectPreparedInstall(destDir)
		if err != nil {
			return nil, fmt.Errorf("read prepared manifest for %s: %w", subject, err)
		}
		recordSyncArtifact(paths, kind, name, subject, destDir, syncArtifactSourceLocalSource, syncArtifactResultMaterialized, reason, start, prepareDuration, activateDuration)
	}
	if inspectPreparedLockMetadata(paths, install, kind, name, provider, mode) != preparedLockMetadataMatch {
		return nil, preparedArtifactStaleError(paths, "prepared artifact for %s is missing or stale", subject)
	}
	if !needMaterialize {
		recordSyncArtifact(paths, kind, name, subject, destDir, syncArtifactSourceLocalSource, syncArtifactResultReused, syncArtifactReasonFresh, start, 0, 0)
	}
	return install, nil
}

func bindPreparedProviderInstall(paths lifecyclePaths, name string, app *config.ProviderEntry, configMap map[string]any, install *preparedInstall) error {
	if err := bindResolvedProviderManifest(name, app, install.manifestPath, install.manifest, configMap); err != nil {
		return err
	}
	if install.executablePath == "" {
		return nil
	}
	if _, err := os.Stat(install.executablePath); err != nil {
		return preparedArtifactStaleError(paths, "prepared executable for provider %q not found at %s", name, install.executablePath)
	}
	args, err := providerEntrypointArgs(install.manifest)
	if err != nil {
		return fmt.Errorf("resolve entrypoint for provider %q: %w", name, err)
	}
	app.Command = install.executablePath
	app.Args = append([]string(nil), args...)
	return nil
}

func (l *Lifecycle) applyLocalProviderEntry(paths lifecyclePaths, name string, app *config.ProviderEntry, configMap map[string]any, mode artifactMode) error {
	subject := "provider " + strconv.Quote(name)
	install, err := l.ensureLocalPreparedInstall(paths, providermanifestv1.KindApp, name, app, configMap, providerDestDir(paths, name), subject, mode)
	if err != nil {
		return err
	}
	return bindPreparedProviderInstall(paths, name, app, configMap, install)
}

func bindPreparedComponentInstall(paths lifecyclePaths, kind, name string, app *config.ProviderEntry, configMap map[string]any, destDir string, install *preparedInstall) error {
	if install.executablePath == "" {
		return preparedArtifactStaleError(paths, "prepared executable for %s %q not found in %s", kind, name, destDir)
	}
	if err := bindResolvedComponentManifest(kind, name, app, install.manifestPath, install.manifest, configMap); err != nil {
		return err
	}
	if _, err := os.Stat(install.executablePath); err != nil {
		return preparedArtifactStaleError(paths, "prepared executable for %s %q not found at %s", kind, name, install.executablePath)
	}
	args, err := componentEntrypointArgs(install.manifest, kind)
	if err != nil {
		return fmt.Errorf("resolve entrypoint for %s %q: %w", kind, name, err)
	}
	app.Command = install.executablePath
	app.Args = append([]string(nil), args...)
	return nil
}

func (l *Lifecycle) applyLocalComponentEntry(paths lifecyclePaths, kind, name string, app *config.ProviderEntry, configMap map[string]any, destDir string, mode artifactMode) error {
	subject := fmt.Sprintf("%s %q", kind, name)
	install, err := l.ensureLocalPreparedInstall(paths, kind, name, app, configMap, destDir, subject, mode)
	if err != nil {
		return err
	}
	return bindPreparedComponentInstall(paths, kind, name, app, configMap, destDir, install)
}

func bindPreparedUIInstall(provider *config.ProviderEntry, subject, destDir string, configMap map[string]any, install *preparedInstall) (string, error) {
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
}

func (l *Lifecycle) applyLocalUIProvider(paths lifecyclePaths, provider *config.ProviderEntry, logicalName, subject, destDir string, configMap map[string]any, mode artifactMode) (string, error) {
	install, err := l.ensureLocalPreparedInstall(paths, providermanifestv1.KindUI, logicalName, provider, configMap, destDir, subject, mode)
	if err != nil {
		return "", err
	}
	return bindPreparedUIInstall(provider, subject, destDir, configMap, install)
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
		if provider.HasLocalSource() {
			if pathWithinRoot(filepath.Join(paths.artifactsDir, ".gestaltd"), provider.SourcePath()) {
				start := time.Now()
				assetRoot, err := bindPathBackedUIManifest(provider, configMap)
				if err == nil {
					recordSyncArtifact(paths, providermanifestv1.KindUI, logicalName, subject, assetRoot, syncArtifactSourcePathBacked, syncArtifactResultPathBacked, syncArtifactReasonSourcePathBacked, start, 0, 0)
				}
				return assetRoot, err
			}
			return l.applyLocalUIProvider(paths, provider, logicalName, subject, destDir, configMap, mode)
		}
		if lockEntry == nil {
			return "", lockMetadataStaleError(paths, "lock entry for %s is missing or stale", subject)
		}
		if !lockEntrySourceMatchesProvider(paths, provider, *lockEntry) {
			return "", lockMetadataStaleError(paths, "lock entry for %s is stale", subject)
		}
		fingerprintMatches, err := lockEntryFingerprintMatchesProviderForMode("ui:"+logicalName, provider, paths.configDir, *lockEntry, mode)
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
		start := time.Now()
		install, err := inspectPreparedInstall(destDir)
		needMaterialize := err != nil || !preparedInstallMatchesLockForMode(providermanifestv1.KindUI, logicalName, provider, *lockEntry, install, mode)
		reason := syncArtifactReasonPreparedMissing
		if err == nil && needMaterialize {
			reason = syncArtifactReasonManifestStale
		}
		if !needMaterialize && install.assetRootPath == "" {
			needMaterialize = true
			reason = syncArtifactReasonAssetRootMissing
		}
		if !needMaterialize {
			if _, err := os.Stat(install.assetRootPath); err != nil {
				needMaterialize = true
				reason = syncArtifactReasonAssetRootMissing
			}
		}
		if needMaterialize {
			if !mode.canMaterialize() {
				return "", preparedArtifactStaleError(paths, "prepared artifact for %s is missing or stale", subject)
			}
			if len(lockEntry.Archives) == 0 {
				if !provider.HasLocalSource() && !provider.HasGitSource() {
					return "", preparedArtifactStaleError(paths, "prepared artifact for %s is missing or stale", subject)
				}
				var prepareDuration time.Duration
				if stagedInstall == nil {
					prepareStart := time.Now()
					if provider.HasGitSource() {
						stagedInstall, cleanupStaged, commitStaged, err = l.stageGitSourceInstall(context.Background(), paths, providermanifestv1.KindUI, logicalName, destDir, provider, paths.stageOptions())
					} else {
						stagedInstall, cleanupStaged, commitStaged, err = stageLocalSourceInstall(providermanifestv1.KindUI, logicalName, provider.SourcePath(), destDir, paths.stageOptions())
					}
					prepareDuration = time.Since(prepareStart)
					if err != nil {
						return "", err
					}
					fingerprint, err := NamedUIProviderFingerprint(logicalName, provider, paths.configDir)
					if err != nil {
						return "", fmt.Errorf("fingerprinting %s: %w", subject, err)
					}
					if lockEntry.InputDigest != fingerprint {
						return "", lockMetadataStaleError(paths, "lock entry for %s is stale", subject)
					}
				}
				if !preparedManifestMatchesLock(*lockEntry, stagedInstall.manifest) {
					return "", lockMetadataStaleError(paths, "lock entry for %s is stale", subject)
				}
				activateStart := time.Now()
				if err := commitStaged(); err != nil {
					return "", err
				}
				sourceKind := syncArtifactSourceLocalSource
				if provider.HasGitSource() {
					sourceKind = syncArtifactSourceGitSource
				}
				recordSyncArtifact(paths, providermanifestv1.KindUI, logicalName, subject, destDir, sourceKind, syncArtifactResultMaterialized, reason, start, prepareDuration, time.Since(activateStart))
			} else {
				if err := l.materializeLockedUIProvider(context.Background(), paths, logicalName, subject, provider, *lockEntry, destDir, reason); err != nil {
					return "", err
				}
			}
			install, err = inspectPreparedInstall(destDir)
			if err != nil {
				return "", fmt.Errorf("read prepared manifest for %s: %w", subject, err)
			}
		}
		if !needMaterialize {
			recordSyncArtifact(paths, providermanifestv1.KindUI, logicalName, subject, destDir, syncArtifactArchiveSourceKind(paths, *lockEntry), syncArtifactResultReused, syncArtifactReasonFresh, start, 0, 0)
		}
		return bindPreparedUIInstall(provider, subject, destDir, configMap, install)
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
		if provider.HasLocalSource() {
			if err := l.applyLocalComponentEntry(paths, kind, name, provider, configMap, destDir, mode); err != nil {
				return err
			}
			break
		}
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

func (l *Lifecycle) applyLockedProviderEntry(paths lifecyclePaths, lock *Lockfile, name string, app *config.ProviderEntry, configMap map[string]any, mode artifactMode) error {
	if app != nil && app.HasLocalSource() {
		return l.applyLocalProviderEntry(paths, name, app, configMap, mode)
	}
	if lock == nil {
		return lockMetadataStaleError(paths, "lock entry for provider %q is missing or stale", name)
	}
	entry, ok := lock.Providers.App[name]
	if !ok {
		return lockMetadataStaleError(paths, "lock entry for provider %q is missing or stale", name)
	}
	if !lockEntrySourceMatchesProvider(paths, app, entry) {
		return lockMetadataStaleError(paths, "lock entry for provider %q is stale", name)
	}
	fingerprintMatches, err := lockEntryFingerprintMatchesProviderForMode(name, app, paths.configDir, entry, mode)
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

	start := time.Now()
	install, err := inspectPreparedInstall(destDir)
	needMaterialize := err != nil || !preparedInstallMatchesLockForMode(providermanifestv1.KindApp, name, app, entry, install, mode)
	reason := syncArtifactReasonPreparedMissing
	if err == nil && needMaterialize {
		reason = syncArtifactReasonManifestStale
	}
	if err == nil && !needMaterialize {
		platform := providerpkg.CurrentPlatformString()
		if _, resolvedKey, ok := resolveArchiveForPlatform(entry, platform); ok {
			if policyErr := validateLockedArchivePolicy(fmt.Sprintf("provider %q", name), providermanifestv1.KindApp, install.manifest, entry, platform, resolvedKey); policyErr != nil {
				return policyErr
			}
		}
	}
	if !needMaterialize && install.executablePath != "" {
		if _, err := os.Stat(install.executablePath); err != nil {
			needMaterialize = true
			reason = syncArtifactReasonExecutableMissing
		}
	}
	if needMaterialize {
		if !mode.canMaterialize() {
			return preparedArtifactStaleError(paths, "prepared artifact for provider %q is missing or stale", name)
		}
		if len(entry.Archives) == 0 {
			if !app.HasLocalSource() && !app.HasGitSource() {
				return preparedArtifactStaleError(paths, "prepared artifact for provider %q is missing or stale", name)
			}
			var prepareDuration time.Duration
			if stagedInstall == nil {
				prepareStart := time.Now()
				if app.HasGitSource() {
					stagedInstall, cleanupStaged, commitStaged, err = l.stageGitSourceInstall(context.Background(), paths, providermanifestv1.KindApp, name, destDir, app, paths.stageOptions())
				} else {
					stagedInstall, cleanupStaged, commitStaged, err = stageLocalSourceInstall(providermanifestv1.KindApp, name, app.SourcePath(), destDir, paths.stageOptions())
				}
				prepareDuration = time.Since(prepareStart)
				if err != nil {
					return err
				}
				fingerprint, err := ProviderFingerprint(name, app, paths.configDir)
				if err != nil {
					return fmt.Errorf("fingerprinting provider %q: %w", name, err)
				}
				if entry.InputDigest != fingerprint {
					return lockMetadataStaleError(paths, "lock entry for provider %q is stale", name)
				}
			}
			if !preparedManifestMatchesLock(entry, stagedInstall.manifest) {
				return lockMetadataStaleError(paths, "lock entry for provider %q is stale", name)
			}
			activateStart := time.Now()
			if err := commitStaged(); err != nil {
				return err
			}
			sourceKind := syncArtifactSourceLocalSource
			if app.HasGitSource() {
				sourceKind = syncArtifactSourceGitSource
			}
			recordSyncArtifact(paths, providermanifestv1.KindApp, name, fmt.Sprintf("provider %q", name), destDir, sourceKind, syncArtifactResultMaterialized, reason, start, prepareDuration, time.Since(activateStart))
		} else {
			if err := l.materializeLockedProvider(context.Background(), paths, name, app, entry, reason); err != nil {
				return err
			}
		}
		install, err = inspectPreparedInstall(destDir)
		if err != nil {
			return fmt.Errorf("read prepared manifest for provider %q: %w", name, err)
		}
	}
	if !needMaterialize {
		recordSyncArtifact(paths, providermanifestv1.KindApp, name, fmt.Sprintf("provider %q", name), destDir, syncArtifactArchiveSourceKind(paths, entry), syncArtifactResultReused, syncArtifactReasonFresh, start, 0, 0)
	}
	if err := bindPreparedProviderInstall(paths, name, app, configMap, install); err != nil {
		return err
	}
	if lockEntryHasCompleteStaticValidation(providermanifestv1.KindApp, entry) {
		bindLockValidationCatalog(app, entry)
	}
	return nil
}

func (l *Lifecycle) applyLockedComponentEntry(paths lifecyclePaths, entry *LockEntry, kind, name string, app *config.ProviderEntry, configMap map[string]any, destDir string, mode artifactMode) error {
	if app != nil && app.HasLocalSource() {
		return l.applyLocalComponentEntry(paths, kind, name, app, configMap, destDir, mode)
	}
	if entry == nil {
		return lockMetadataStaleError(paths, "lock entry for %s %q is missing or stale", kind, name)
	}
	if !lockEntrySourceMatchesProvider(paths, app, *entry) {
		return lockMetadataStaleError(paths, "lock entry for %s %q is stale", kind, name)
	}
	fingerprintMatches, err := lockEntryFingerprintMatchesProviderForMode(name, app, paths.configDir, *entry, mode)
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

	start := time.Now()
	install, err := inspectPreparedInstall(destDir)
	needMaterialize := err != nil || !preparedInstallMatchesLockForMode(kind, name, app, *entry, install, mode)
	reason := syncArtifactReasonPreparedMissing
	if err == nil && needMaterialize {
		reason = syncArtifactReasonManifestStale
	}
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
		reason = syncArtifactReasonExecutableMissing
	}
	if !needMaterialize {
		if _, err := os.Stat(install.executablePath); err != nil {
			needMaterialize = true
			reason = syncArtifactReasonExecutableMissing
		}
	}
	if needMaterialize {
		if !mode.canMaterialize() {
			return preparedArtifactStaleError(paths, "prepared artifact for %s %q is missing or stale", kind, name)
		}
		if len(entry.Archives) == 0 {
			if !app.HasLocalSource() && !app.HasGitSource() {
				return preparedArtifactStaleError(paths, "prepared artifact for %s %q is missing or stale", kind, name)
			}
			var prepareDuration time.Duration
			if stagedInstall == nil {
				prepareStart := time.Now()
				if app.HasGitSource() {
					stagedInstall, cleanupStaged, commitStaged, err = l.stageGitSourceInstall(context.Background(), paths, kind, name, destDir, app, paths.stageOptions())
				} else {
					stagedInstall, cleanupStaged, commitStaged, err = stageLocalSourceInstall(kind, name, app.SourcePath(), destDir, paths.stageOptions())
				}
				prepareDuration = time.Since(prepareStart)
				if err != nil {
					return err
				}
				fingerprint, err := ProviderFingerprint(name, app, paths.configDir)
				if err != nil {
					return fmt.Errorf("fingerprinting %s %q provider: %w", kind, name, err)
				}
				if entry.InputDigest != fingerprint {
					return lockMetadataStaleError(paths, "lock entry for %s %q is stale", kind, name)
				}
			}
			if !preparedManifestMatchesLock(*entry, stagedInstall.manifest) {
				return lockMetadataStaleError(paths, "lock entry for %s %q is stale", kind, name)
			}
			activateStart := time.Now()
			if err := commitStaged(); err != nil {
				return err
			}
			sourceKind := syncArtifactSourceLocalSource
			if app.HasGitSource() {
				sourceKind = syncArtifactSourceGitSource
			}
			recordSyncArtifact(paths, kind, name, fmt.Sprintf("%s %q", kind, name), destDir, sourceKind, syncArtifactResultMaterialized, reason, start, prepareDuration, time.Since(activateStart))
		} else {
			if err := l.materializeLockedComponent(context.Background(), paths, kind, name, app, *entry, destDir, reason); err != nil {
				return err
			}
		}
		install, err = inspectPreparedInstall(destDir)
		if err != nil {
			return fmt.Errorf("read prepared manifest for %s %q: %w", kind, name, err)
		}
	}
	if !needMaterialize {
		recordSyncArtifact(paths, kind, name, fmt.Sprintf("%s %q", kind, name), destDir, syncArtifactArchiveSourceKind(paths, *entry), syncArtifactResultReused, syncArtifactReasonFresh, start, 0, 0)
	}
	return bindPreparedComponentInstall(paths, kind, name, app, configMap, destDir, install)
}

func bindResolvedProviderManifest(name string, app *config.ProviderEntry, manifestPath string, manifest *providermanifestv1.Manifest, configMap map[string]any) error {
	manifest = providerpkg.ResolveManifestLocalReferences(manifest, manifestPath)
	if err := validateInstalledManifestKind(providermanifestv1.KindApp, name, manifest); err != nil {
		return err
	}
	if err := providerpkg.ValidateConfigForManifest(manifestPath, manifest, providermanifestv1.KindApp, configMap); err != nil {
		return fmt.Errorf("provider config validation for provider %q: %w", name, err)
	}
	resolveProviderIcon(manifest, manifestPath, app)
	app.ResolvedManifestPath = manifestPath
	app.ResolvedManifest = manifest
	return nil
}

func bindResolvedComponentManifest(kind, name string, app *config.ProviderEntry, manifestPath string, manifest *providermanifestv1.Manifest, configMap map[string]any) error {
	manifest = providerpkg.ResolveManifestLocalReferences(manifest, manifestPath)
	if err := validateInstalledManifestKind(kind, name, manifest); err != nil {
		return err
	}
	if err := providerpkg.ValidateConfigForManifest(manifestPath, manifest, kind, configMap); err != nil {
		return fmt.Errorf("provider config validation for %s %q: %w", kind, name, err)
	}
	resolveProviderIcon(manifest, manifestPath, app)
	app.ResolvedManifestPath = manifestPath
	app.ResolvedManifest = manifest
	return nil
}

func bindResolvedUIManifest(app *config.ProviderEntry, manifestPath string, manifest *providermanifestv1.Manifest, configMap map[string]any) error {
	manifest = providerpkg.ResolveManifestLocalReferences(manifest, manifestPath)
	if err := validateInstalledManifestKind(providermanifestv1.KindUI, "provider", manifest); err != nil {
		return err
	}
	if err := providerpkg.ValidateConfigForManifest(manifestPath, manifest, providermanifestv1.KindUI, configMap); err != nil {
		return fmt.Errorf("provider config validation for ui provider: %w", err)
	}
	resolveProviderIcon(manifest, manifestPath, app)
	app.ResolvedManifestPath = manifestPath
	app.ResolvedManifest = manifest
	return nil
}

func bindPathBackedUIManifest(app *config.ProviderEntry, configMap map[string]any) (string, error) {
	manifestPath := app.SourcePath()
	if strings.TrimSpace(manifestPath) == "" {
		return "", fmt.Errorf("resolved ui manifest path is required")
	}
	normalized, err := normalizeLocalSource(manifestPath)
	if err != nil {
		return "", fmt.Errorf("ui provider manifest not found at %s: %w", manifestPath, err)
	}
	manifestPath = normalized.manifestPath
	_, manifest, err := providerpkg.ReadManifestFile(manifestPath)
	if err != nil {
		return "", fmt.Errorf("prepare manifest for ui provider: %w", err)
	}
	if err := bindResolvedUIManifest(app, manifestPath, manifest, configMap); err != nil {
		return "", err
	}
	assetRoot := filepath.Join(filepath.Dir(manifestPath), filepath.FromSlash(manifest.Spec.AssetRoot))
	if _, err := os.Stat(assetRoot); err != nil {
		return "", fmt.Errorf("ui provider asset root not found at %s: %w", assetRoot, err)
	}
	return assetRoot, nil
}

func (l *Lifecycle) materializeLockedProvider(ctx context.Context, paths lifecyclePaths, name string, app *config.ProviderEntry, entry LockEntry, reason string) error {
	return l.materializeLockedArchive(ctx, paths, providermanifestv1.KindApp, name, fmt.Sprintf("provider %q", name), app, entry, providerDestDir(paths, name), reason)
}

func (l *Lifecycle) materializeLockedComponent(ctx context.Context, paths lifecyclePaths, kind, name string, app *config.ProviderEntry, entry LockEntry, destDir, reason string) error {
	if destDir == "" {
		return fmt.Errorf("unsupported component %q", name)
	}
	return l.materializeLockedArchive(ctx, paths, kind, name, fmt.Sprintf("%s %q", kind, name), app, entry, destDir, reason)
}

func (l *Lifecycle) materializeLockedUIProvider(ctx context.Context, paths lifecyclePaths, name, subject string, app *config.ProviderEntry, entry LockEntry, destDir, reason string) error {
	return l.materializeLockedArchive(ctx, paths, providermanifestv1.KindUI, name, subject, app, entry, destDir, reason)
}

func (l *Lifecycle) materializeLockedArchive(ctx context.Context, paths lifecyclePaths, kind, name, subject string, app *config.ProviderEntry, entry LockEntry, destDir, reason string) error {
	start := time.Now()
	platform := providerpkg.CurrentPlatformString()
	archiveLocation, resolvedKey, expectedSHA, err := l.resolveLockedArchiveDownload(paths, entry, platform, subject)
	if err != nil {
		return err
	}
	archiveSourceKind := syncArtifactSourceLocalArchive
	if isRemoteReleaseMetadataLocation(archiveLocation) {
		archiveSourceKind = syncArtifactSourceRemoteArchive
	}

	cacheReq := materializedCacheRequest{
		Subject:        subject,
		Kind:           kind,
		Name:           name,
		SourceKind:     archiveSourceKind,
		ArchiveSHA256:  expectedSHA,
		ResolvedKey:    resolvedKey,
		Platform:       platform,
		Package:        lockEntryPackage(entry),
		Version:        entry.Version,
		DestinationDir: destDir,
	}
	cacheResult := syncCacheResultMiss
	cacheEligible := true
	cache := paths.syncCache
	if cache.dir != "" {
		cacheStart := time.Now()
		restore, err := cache.Restore(cacheReq)
		if restore != nil {
			cacheResult = string(restore.Result)
		}
		if restore == nil {
			cacheEligible = false
			cacheResult = syncCacheResultUncacheable
			recordSyncCacheEntry(paths, syncCacheMetricsEvent{
				Subject:    subject,
				SourceKind: cacheReq.SourceKind,
				Platform:   platform,
				Result:     cacheResult,
				Lookup:     true,
				Duration:   time.Since(cacheStart),
			})
		}
		if restore != nil {
			recordSyncCacheEntry(paths, syncCacheMetricsEvent{
				Subject:    subject,
				SourceKind: cacheReq.SourceKind,
				Key:        restore.Key.Display,
				SHA256:     restore.Key.ArchiveSHA256,
				Platform:   platform,
				Result:     string(restore.Result),
				Lookup:     true,
				Bytes:      restore.Bytes,
				Files:      restore.Files,
				Duration:   time.Since(cacheStart),
			})
		}
		if err != nil {
			return fmt.Errorf("restore materialized cache for %s: %w", subject, err)
		}
		if restore != nil && restore.Result == materializedCacheHit {
			defer func() { _ = restore.cleanup() }()
			if err := validateLockedInstalledManifest(kind, name, subject, restore.Install.manifest, entry, platform, resolvedKey); err != nil {
				return err
			}
			activateStart := time.Now()
			if err := restore.commit(); err != nil {
				return fmt.Errorf("activate cached locked source for %s: %w", subject, err)
			}
			recordSyncArtifact(paths, kind, name, subject, destDir, cacheReq.SourceKind, syncArtifactResultMaterialized, reason, start, 0, time.Since(activateStart))
			return nil
		}
	} else {
		recordSyncCacheEntry(paths, syncCacheMetricsEvent{
			Subject:    subject,
			SourceKind: cacheReq.SourceKind,
			Platform:   platform,
			Result:     syncCacheResultDisabled,
			Lookup:     true,
		})
	}

	downloadStart := time.Now()
	download, err := downloadArchiveForSource(ctx, l.metadataHTTPClient(), sourceAuthToken(app), archiveLocation)
	if err != nil {
		return lockedArchiveDownloadError{err: fmt.Errorf("download locked source archive for %s: %w", subject, err)}
	}
	defer download.Cleanup()
	if err := verifyArchiveSHA256(download.SHA256Hex, expectedSHA); err != nil {
		return lockedArchiveDownloadError{err: fmt.Errorf("download locked source archive for %s: %w", subject, err)}
	}
	archiveFetchSourceKind := syncArchiveSourceLocal
	if isRemoteReleaseMetadataLocation(archiveLocation) {
		archiveFetchSourceKind = syncArchiveSourceRemoteReleaseMetadata
	}
	recordSyncArchiveDownload(paths, syncArchiveMetricsEvent{
		Subject:          subject,
		SourceKind:       archiveFetchSourceKind,
		SHA256:           expectedSHA,
		Downloaded:       isRemoteReleaseMetadataLocation(archiveLocation),
		Bytes:            archiveFileSize(download.LocalPath),
		Duration:         time.Since(downloadStart),
		DownloadDuration: time.Since(downloadStart),
	})

	prepareStart := time.Now()
	installed, cleanupInstall, commitInstall, err := installLockedPackageAtomic(download.LocalPath, destDir)
	prepareDuration := time.Since(prepareStart)
	if err != nil {
		return fmt.Errorf("install locked source for %s: %w", subject, err)
	}
	defer func() { _ = cleanupInstall() }()
	if err := validateLockedInstalledManifest(kind, name, subject, installed.Manifest, entry, platform, resolvedKey); err != nil {
		return err
	}
	if cache.dir != "" && cacheEligible {
		cacheStart := time.Now()
		putResult, putErr := cache.Put(ctx, cacheReq, installed.Root)
		recordSyncCacheEntry(paths, syncCacheMetricsEvent{
			Subject:    subject,
			SourceKind: cacheReq.SourceKind,
			Key:        putResult.Key.Display,
			SHA256:     putResult.Key.ArchiveSHA256,
			Platform:   platform,
			Result:     cacheResult,
			Put:        true,
			PutFailed:  putErr != nil,
			Bytes:      putResult.Bytes,
			Files:      putResult.Files,
			Duration:   time.Since(cacheStart),
			PutTimings: putResult.Timings,
		})
	}
	activateStart := time.Now()
	if err := commitInstall(); err != nil {
		return fmt.Errorf("activate locked source for %s: %w", subject, err)
	}
	recordSyncArtifact(paths, kind, name, subject, destDir, cacheReq.SourceKind, syncArtifactResultMaterialized, reason, start, prepareDuration, time.Since(activateStart))
	return nil
}

func (l *Lifecycle) resolveLockedArchiveDownload(paths lifecyclePaths, entry LockEntry, platform, subject string) (string, string, string, error) {
	archive, resolvedKey, ok := resolveArchiveForPlatform(entry, platform)
	if !ok || archive.URL == "" {
		return "", "", "", fmt.Errorf("no archive for platform %s for %s; publish an explicit %s archive with `gestaltd provider package --platform %s` or use a generic package where allowed", platform, subject, platform, platform)
	}
	archiveLocation, err := resolveLockedArchiveLocation(paths.configDir, entry.Source, archive.URL)
	if err != nil {
		return "", "", "", fmt.Errorf("resolve locked source archive for %s: %w", subject, err)
	}
	expectedSHA, hasSHA, err := normalizeArchiveSHA256(archive.SHA256)
	if err != nil {
		return "", "", "", fmt.Errorf("lock archive sha256 for %s: %w", subject, err)
	}
	if !hasSHA {
		return "", "", "", fmt.Errorf("lock archive sha256 is required for platform %s for %s; run `gestaltd lock` with published release metadata that includes archive hashes", platform, subject)
	}
	return archiveLocation, resolvedKey, expectedSHA, nil
}

func validateLockedInstalledManifest(kind, name, subject string, manifest *providermanifestv1.Manifest, entry LockEntry, platform, resolvedKey string) error {
	if err := validateInstalledManifestKind(kind, name, manifest); err != nil {
		return err
	}
	if manifest.Source != lockEntryPackage(entry) {
		return fmt.Errorf("locked source manifest source mismatch for %s: got %q, want %q", subject, manifest.Source, lockEntryPackage(entry))
	}
	if manifest.Version != entry.Version {
		return fmt.Errorf("locked source manifest version mismatch for %s: got %q, want %q", subject, manifest.Version, entry.Version)
	}
	return validateLockedArchivePolicy(subject, archivePolicyKind(kind), manifest, entry, platform, resolvedKey)
}

// resolveUIThemeConfig decodes the optional theme block of a mounted ui's
// config and resolves its paths against the deployment config directory at
// sync time. Unlike resolveProviderIcon, a configured-but-missing path is an
// error: theme content is served verbatim and a typo would otherwise degrade
// silently to the empty stylesheet.
func resolveUIThemeConfig(paths lifecyclePaths, name string, entry *config.UIEntry) error {
	entry.ResolvedThemeStylesheet = ""
	entry.ResolvedThemeAssetsDir = ""
	theme, err := config.UIThemeConfigFromProviderConfig(entry.Config)
	if err != nil {
		return fmt.Errorf("decode ui %q theme config: %w", name, err)
	}
	if theme == nil {
		return nil
	}
	if stylesheet := strings.TrimSpace(theme.Stylesheet); stylesheet != "" {
		resolved := resolveUIThemePath(paths.configDir, stylesheet)
		info, err := os.Stat(resolved)
		if err != nil {
			return fmt.Errorf("ui %q theme stylesheet not found at %s: %w", name, resolved, err)
		}
		if info.IsDir() {
			return fmt.Errorf("ui %q theme stylesheet at %s is a directory", name, resolved)
		}
		entry.ResolvedThemeStylesheet = resolved
	}
	if assetsDir := strings.TrimSpace(theme.AssetsDir); assetsDir != "" {
		resolved := resolveUIThemePath(paths.configDir, assetsDir)
		info, err := os.Stat(resolved)
		if err != nil {
			return fmt.Errorf("ui %q theme assetsDir not found at %s: %w", name, resolved, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("ui %q theme assetsDir at %s is not a directory", name, resolved)
		}
		entry.ResolvedThemeAssetsDir = resolved
	}
	return nil
}

func resolveUIThemePath(configDir, value string) string {
	resolved := filepath.FromSlash(value)
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(configDir, resolved)
	}
	if abs, err := filepath.Abs(resolved); err == nil {
		return abs
	}
	return filepath.Clean(resolved)
}

func resolveProviderIcon(manifest *providermanifestv1.Manifest, manifestPath string, app *config.ProviderEntry) {
	if manifest.IconFile == "" {
		return
	}
	iconPath := filepath.Join(filepath.Dir(manifestPath), filepath.FromSlash(manifest.IconFile))
	if _, err := os.Stat(iconPath); err != nil {
		slog.Warn("provider icon_file not found", "path", iconPath, "error", err)
		return
	}
	app.ResolvedIconFile = iconPath
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
