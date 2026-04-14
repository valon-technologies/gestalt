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
	"slices"
	"strconv"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/pluginsource"
	ghresolver "github.com/valon-technologies/gestalt/server/internal/pluginsource/github"
	"github.com/valon-technologies/gestalt/server/internal/pluginstore"
	"github.com/valon-technologies/gestalt/server/internal/providerpkg"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"gopkg.in/yaml.v3"
)

const (
	InitLockfileName     = "gestalt.lock.json"
	PreparedProvidersDir = ".gestaltd/providers"
	PreparedAuthDir      = ".gestaltd/auth"
	PreparedSecretsDir   = ".gestaltd/secrets"
	PreparedTelemetryDir = ".gestaltd/telemetry"
	PreparedAuditDir     = ".gestaltd/audit"
	PreparedCacheDir     = ".gestaltd/cache"
	PreparedUIDir        = ".gestaltd/ui"
	LockVersion          = 7

	platformKeyGeneric = "generic"
)

type Lockfile struct {
	Version    int                          `json:"version"`
	Providers  map[string]LockProviderEntry `json:"providers"`
	Auth       map[string]LockEntry         `json:"auth,omitempty"`
	IndexedDBs map[string]LockEntry         `json:"indexeddbs,omitempty"`
	Caches     map[string]LockEntry         `json:"cache,omitempty"`
	Secrets    map[string]LockEntry         `json:"secrets,omitempty"`
	Telemetry  map[string]LockEntry         `json:"telemetry,omitempty"`
	Audit      map[string]LockEntry         `json:"audit,omitempty"`
	UIs        map[string]LockUIEntry       `json:"ui,omitempty"`
}

// LockArchive records a platform-specific archive URL and optional integrity hash.
type LockArchive struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256,omitempty"`
}

type LockEntry struct {
	Fingerprint string                 `json:"fingerprint"`
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
	sourceResolver       pluginsource.Resolver
	configSecretResolver func(context.Context, *config.Config) error
}

func NewLifecycle(sourceResolver pluginsource.Resolver) *Lifecycle {
	return &Lifecycle{sourceResolver: sourceResolver}
}

func (l *Lifecycle) WithConfigSecretResolver(resolve func(context.Context, *config.Config) error) *Lifecycle {
	l.configSecretResolver = resolve
	return l
}

func (l *Lifecycle) InitAtPath(configPath string) (*Lockfile, error) {
	lock, _, err := l.initAtPath(configPath, "")
	return lock, err
}

func (l *Lifecycle) InitAtPathWithArtifactsDir(configPath, artifactsDir string) (*Lockfile, error) {
	lock, _, err := l.initAtPath(configPath, artifactsDir)
	return lock, err
}

// InitAtPathWithPlatforms runs init and additionally downloads and hashes
// archives for the specified extra platforms.
func (l *Lifecycle) InitAtPathWithPlatforms(configPath, artifactsDir string, platforms []struct{ GOOS, GOARCH, LibC string }) (*Lockfile, error) {
	lock, cfg, err := l.initAtPath(configPath, artifactsDir)
	if err != nil {
		return nil, err
	}
	if len(platforms) == 0 {
		return lock, nil
	}

	tokenForSource := buildSourceTokenMap(cfg)
	if err := downloadPlatformArchives(context.Background(), lock, platforms, tokenForSource); err != nil {
		return nil, err
	}

	lockPath := lockfilePathForConfig(configPath)
	if err := WriteLockfile(lockPath, lock); err != nil {
		return nil, err
	}
	return lock, nil
}

func (l *Lifecycle) initAtPath(configPath, artifactsDir string) (*Lockfile, *config.Config, error) {
	cfg, err := config.LoadAllowMissingEnv(configPath)
	if err != nil {
		return nil, nil, fmt.Errorf("loading config: %v", err)
	}
	if err := config.OverlayManagedPluginConfig(configPath, cfg); err != nil {
		return nil, nil, fmt.Errorf("loading config: %v", err)
	}
	paths := initPathsForConfigWithArtifactsDir(configPath, resolveArtifactsDir(configPath, cfg, artifactsDir))
	secretsEntries, err := l.primeSecretsProviderForConfigResolution(context.Background(), paths, cfg, nil)
	if err != nil {
		return nil, nil, err
	}
	if err := l.resolveConfigSecrets(context.Background(), cfg); err != nil {
		return nil, nil, err
	}
	if err := os.MkdirAll(paths.providersDir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("creating providers dir: %w", err)
	}

	lock := &Lockfile{
		Version:    LockVersion,
		Providers:  make(map[string]LockProviderEntry),
		Auth:       make(map[string]LockEntry),
		IndexedDBs: make(map[string]LockEntry),
		Caches:     make(map[string]LockEntry),
		Secrets:    make(map[string]LockEntry),
		Telemetry:  make(map[string]LockEntry),
		Audit:      make(map[string]LockEntry),
		UIs:        make(map[string]LockUIEntry),
	}

	resolvedProviders, err := l.writeProviderArtifacts(context.Background(), cfg, paths)
	if err != nil {
		return nil, nil, err
	}
	for name := range resolvedProviders {
		lock.Providers[name] = resolvedProviders[name]
	}
	for _, collection := range hostProviderCollections(cfg) {
		for name, entry := range collection.entries {
			if entry == nil || !entry.HasManagedSource() {
				continue
			}
			if collection.kind == config.HostProviderKindSecrets {
				if _, alreadyPrepared := secretsEntries[name]; alreadyPrepared {
					continue
				}
			}
			destDir := componentDestDir(paths, collection.kind, name)
			lockEntry, err := l.writeComponentArtifact(context.Background(), paths, providerManifestKind(collection.kind), name, destDir, entry, entry.Config)
			if err != nil {
				return nil, nil, err
			}
			lockEntriesForKind(lock, collection.kind)[name] = lockEntry
		}
	}
	if err := l.resolveConfiguredPlugins(paths, lock, cfg, true); err != nil {
		return nil, nil, err
	}
	if err := synthesizeAppOwnedUIEntries(cfg); err != nil {
		return nil, nil, err
	}
	for name, lockEntry := range secretsEntries {
		lock.Secrets[name] = lockEntry
	}
	for name, def := range cfg.Providers.IndexedDB {
		if def != nil && def.HasManagedSource() {
			entry, err := l.writeComponentArtifact(context.Background(), paths, providermanifestv1.KindIndexedDB, name, indexeddbDestDir(paths, name), def, def.Config)
			if err != nil {
				return nil, nil, err
			}
			lock.IndexedDBs[name] = entry
		}
	}
	for name, entry := range cfg.Providers.UI {
		if entry != nil && !entry.Disabled && entry.HasManagedSource() {
			uiEntry, err := l.writeNamedUIProviderArtifact(context.Background(), paths, name, &entry.ProviderEntry, uiDestDir(paths, name), "ui "+strconv.Quote(name))
			if err != nil {
				return nil, nil, err
			}
			lock.UIs[name] = uiEntry
		}
	}

	if err := WriteLockfile(paths.lockfilePath, lock); err != nil {
		return nil, nil, err
	}
	if err := l.applyLockedProviders(configPath, artifactsDir, cfg, true); err != nil {
		return nil, nil, err
	}
	if err := config.ValidateResolvedStructure(cfg); err != nil {
		return nil, nil, err
	}

	slog.Info("prepared locked artifacts", "providers", len(lock.Providers), "auth", len(lock.Auth), "indexeddbs", len(lock.IndexedDBs), "cache", len(lock.Caches), "secrets", len(lock.Secrets), "telemetry", len(lock.Telemetry), "audit", len(lock.Audit), "uis", len(lock.UIs))
	slog.Info("wrote lockfile", "path", paths.lockfilePath)
	return lock, cfg, nil
}

func buildSourceTokenMap(cfg *config.Config) map[string]string {
	tokens := make(map[string]string)
	for _, entry := range cfg.Plugins {
		if entry != nil && entry.Source.Auth != nil {
			tokens[entry.SourceRef()] = entry.Source.Auth.Token
		}
	}
	for _, collection := range hostProviderCollections(cfg) {
		for _, entry := range collection.entries {
			if entry != nil && entry.Source.Auth != nil {
				tokens[entry.SourceRef()] = entry.Source.Auth.Token
			}
		}
	}
	for _, def := range cfg.Providers.IndexedDB {
		if def != nil && def.Source.Auth != nil {
			tokens[def.SourceRef()] = def.Source.Auth.Token
		}
	}
	for _, entry := range cfg.Providers.UI {
		if entry != nil && !entry.Disabled && entry.Source.Auth != nil {
			tokens[entry.SourceRef()] = entry.Source.Auth.Token
		}
	}
	return tokens
}

func lockfilePathForConfig(configPath string) string {
	dir := filepath.Dir(configPath)
	if !filepath.IsAbs(dir) {
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs
		}
	}
	return filepath.Join(dir, InitLockfileName)
}

func (l *Lifecycle) LoadForExecutionAtPath(configPath string, locked bool) (*config.Config, map[string]string, error) {
	return l.LoadForExecutionAtPathWithArtifactsDir(configPath, "", locked)
}

func (l *Lifecycle) LoadForExecutionAtPathWithArtifactsDir(configPath, artifactsDir string, locked bool) (*config.Config, map[string]string, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, nil, fmt.Errorf("loading config: %v", err)
	}
	if err := synthesizeLocalSourceAppOwnedUIEntries(cfg); err != nil {
		return nil, nil, fmt.Errorf("loading config: %v", err)
	}
	paths := initPathsForConfigWithArtifactsDir(configPath, resolveArtifactsDir(configPath, cfg, artifactsDir))
	secretsLock, err := l.lockForSecretsBootstrap(configPath, artifactsDir, paths, cfg, locked)
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

	if err := l.applyLockedProviders(configPath, artifactsDir, cfg, locked); err != nil {
		return nil, nil, err
	}
	if err := config.ValidateResolvedStructure(cfg); err != nil {
		return nil, nil, err
	}

	return cfg, nil, nil
}

func (l *Lifecycle) resolveConfigSecrets(ctx context.Context, cfg *config.Config) error {
	if l.configSecretResolver == nil {
		return nil
	}
	if err := l.configSecretResolver(ctx, cfg); err != nil {
		return fmt.Errorf("resolving config secrets: %w", err)
	}
	return config.ValidateStructure(cfg)
}

func (l *Lifecycle) lockForSecretsBootstrap(configPath, artifactsDir string, paths initPaths, cfg *config.Config, locked bool) (*Lockfile, error) {
	if cfg == nil {
		return nil, nil
	}
	_, provider, err := cfg.SelectedSecretsProvider()
	if err != nil {
		return nil, err
	}
	if provider == nil || !provider.HasManagedSource() {
		return nil, nil
	}
	if !configHasManagedProviderSources(cfg) {
		return nil, nil
	}

	lock, err := ReadLockfile(paths.lockfilePath)
	if !locked && (err != nil || !lockMatchesConfig(cfg, paths, lock)) {
		lock, err = l.InitAtPathWithArtifactsDir(configPath, artifactsDir)
	}
	if err != nil {
		return nil, fmt.Errorf("managed providers require prepared artifacts; run `gestaltd init --config %s`: %w", configPath, err)
	}
	return lock, nil
}

func (l *Lifecycle) primeSecretsProviderForConfigResolution(ctx context.Context, paths initPaths, cfg *config.Config, lock *Lockfile) (map[string]LockEntry, error) {
	if cfg == nil {
		return nil, nil
	}
	name, provider, err := cfg.SelectedSecretsProvider()
	if err != nil || provider == nil {
		return nil, err
	}

	configMap, err := config.NodeToMap(provider.Config)
	if err != nil {
		return nil, fmt.Errorf("decode provider config for %s %q: %w", providermanifestv1.KindSecrets, name, err)
	}

	switch {
	case provider.HasManagedSource():
		if lock != nil {
			lockEntry, ok := lock.Secrets[name]
			if !ok {
				return nil, fmt.Errorf("prepared artifact for %s %q is missing or stale; run `gestaltd init --config %s`", providermanifestv1.KindSecrets, name, paths.configPath)
			}
			if err := l.applyLockedComponentEntry(paths, &lockEntry, providermanifestv1.KindSecrets, name, provider, configMap, secretsDestDir(paths, name), false); err != nil {
				return nil, err
			}
			return nil, nil
		}
		entry, err := l.writeComponentArtifact(ctx, paths, providermanifestv1.KindSecrets, name, secretsDestDir(paths, name), provider, provider.Config)
		if err != nil {
			return nil, err
		}
		if err := l.applyLockedComponentEntry(paths, &entry, providermanifestv1.KindSecrets, name, provider, configMap, secretsDestDir(paths, name), false); err != nil {
			return nil, err
		}
		return map[string]LockEntry{name: entry}, nil
	case provider.HasLocalSource():
		if err := applyLocalComponentManifest(providermanifestv1.KindSecrets, name, provider, configMap); err != nil {
			return nil, err
		}
	}

	return nil, nil
}

type initPaths struct {
	configPath   string
	configDir    string
	artifactsDir string
	lockfilePath string
	providersDir string
	authDir      string
	secretsDir   string
	telemetryDir string
	auditDir     string
	cacheDir     string
	uiDir        string
}

type providerFingerprintInput struct {
	Name    string `json:"name"`
	Source  string `json:"source,omitempty"`
	Version string `json:"version,omitempty"`
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
		{config.HostProviderKindSecrets, cfg.Providers.Secrets},
		{config.HostProviderKindTelemetry, cfg.Providers.Telemetry},
		{config.HostProviderKindAudit, cfg.Providers.Audit},
		{config.HostProviderKindCache, cfg.Providers.Cache},
	}
}

func lockEntriesForKind(lock *Lockfile, kind config.HostProviderKind) map[string]LockEntry {
	if lock == nil {
		return nil
	}
	switch kind {
	case config.HostProviderKindAuth:
		return lock.Auth
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
	default:
		return nil
	}
}

func configHasProviderLoading(cfg *config.Config) bool {
	for _, entry := range cfg.Plugins {
		if entry.HasManagedSource() || entry.HasLocalSource() {
			return true
		}
	}
	for _, collection := range hostProviderCollections(cfg) {
		for _, entry := range collection.entries {
			if entry != nil && (entry.HasManagedSource() || entry.HasLocalSource()) {
				return true
			}
		}
	}
	for _, entry := range cfg.Providers.UI {
		if entry != nil && !entry.Disabled && (entry.HasManagedSource() || entry.HasLocalSource()) {
			return true
		}
	}
	for _, def := range cfg.Providers.IndexedDB {
		if def != nil && (def.HasManagedSource() || def.HasLocalSource()) {
			return true
		}
	}
	return false
}

func configHasManagedProviderSources(cfg *config.Config) bool {
	for _, entry := range cfg.Plugins {
		if entry.HasManagedSource() {
			return true
		}
	}
	for _, collection := range hostProviderCollections(cfg) {
		for _, entry := range collection.entries {
			if entry != nil && entry.HasManagedSource() {
				return true
			}
		}
	}
	for _, entry := range cfg.Providers.UI {
		if entry != nil && !entry.Disabled && entry.HasManagedSource() {
			return true
		}
	}
	for _, def := range cfg.Providers.IndexedDB {
		if def != nil && def.HasManagedSource() {
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

func initPathsForConfig(configPath string) initPaths {
	return initPathsForConfigWithArtifactsDir(configPath, "")
}

func initPathsForConfigWithArtifactsDir(configPath, artifactsDir string) initPaths {
	configDir := filepath.Dir(configPath)
	if artifactsDir == "" {
		artifactsDir = configDir
	} else if !filepath.IsAbs(artifactsDir) {
		artifactsDir = filepath.Join(configDir, artifactsDir)
	}
	return initPaths{
		configPath:   configPath,
		configDir:    configDir,
		artifactsDir: artifactsDir,
		lockfilePath: filepath.Join(configDir, InitLockfileName),
		providersDir: filepath.Join(artifactsDir, filepath.FromSlash(PreparedProvidersDir)),
		authDir:      filepath.Join(artifactsDir, filepath.FromSlash(PreparedAuthDir)),
		secretsDir:   filepath.Join(artifactsDir, filepath.FromSlash(PreparedSecretsDir)),
		telemetryDir: filepath.Join(artifactsDir, filepath.FromSlash(PreparedTelemetryDir)),
		auditDir:     filepath.Join(artifactsDir, filepath.FromSlash(PreparedAuditDir)),
		cacheDir:     filepath.Join(artifactsDir, filepath.FromSlash(PreparedCacheDir)),
		uiDir:        filepath.Join(artifactsDir, filepath.FromSlash(PreparedUIDir)),
	}
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

func indexeddbDestDir(paths initPaths, name string) string {
	return filepath.Join(paths.artifactsDir, "indexeddb", name)
}

func componentDestDir(paths initPaths, kind config.HostProviderKind, name string) string {
	switch kind {
	case config.HostProviderKindAuth:
		return authDestDir(paths, name)
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
	default:
		return ""
	}
}

func providerManifestKind(kind config.HostProviderKind) string {
	switch kind {
	case config.HostProviderKindAuth:
		return providermanifestv1.KindAuth
	case config.HostProviderKindSecrets:
		return providermanifestv1.KindSecrets
	case config.HostProviderKindTelemetry, config.HostProviderKindAudit:
		return providermanifestv1.KindPlugin
	case config.HostProviderKindCache:
		return providermanifestv1.KindCache
	case config.HostProviderKindIndexedDB:
		return providermanifestv1.KindIndexedDB
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
	var version struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &version); err != nil {
		return nil, fmt.Errorf("parsing lockfile %s: %w", path, err)
	}
	if version.Version != LockVersion {
		return nil, fmt.Errorf("unsupported lockfile version %d; run `gestaltd init` to upgrade", version.Version)
	}
	var lock Lockfile
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("parsing lockfile %s: %w", path, err)
	}
	if lock.Providers == nil {
		lock.Providers = make(map[string]LockProviderEntry)
	}
	if lock.Auth == nil {
		lock.Auth = make(map[string]LockEntry)
	}
	if lock.Secrets == nil {
		lock.Secrets = make(map[string]LockEntry)
	}
	if lock.Caches == nil {
		lock.Caches = make(map[string]LockEntry)
	}
	if lock.Telemetry == nil {
		lock.Telemetry = make(map[string]LockEntry)
	}
	if lock.Audit == nil {
		lock.Audit = make(map[string]LockEntry)
	}
	if lock.IndexedDBs == nil {
		lock.IndexedDBs = make(map[string]LockEntry)
	}
	if lock.UIs == nil {
		lock.UIs = make(map[string]LockUIEntry)
	}
	return &lock, nil
}

func WriteLockfile(path string, lock *Lockfile) error {
	if err := writeJSONFile(path, lock); err != nil {
		return fmt.Errorf("writing lockfile: %w", err)
	}
	return nil
}

func lockMatchesConfig(cfg *config.Config, paths initPaths, lock *Lockfile) bool {
	if lock == nil || lock.Version != LockVersion {
		return false
	}
	for name, entry := range cfg.Plugins {
		if !entry.HasManagedSource() {
			continue
		}
		lockEntry, found := lock.Providers[name]
		if !lockEntryMatches(paths, name, entry, lockEntry, found) {
			return false
		}
	}
	for _, collection := range hostProviderCollections(cfg) {
		lockEntries := lockEntriesForKind(lock, collection.kind)
		for name, entry := range collection.entries {
			if entry == nil || !entry.HasManagedSource() {
				continue
			}
			lockEntry, found := lockEntries[name]
			if !lockEntryMatches(paths, name, entry, lockEntry, found) {
				return false
			}
		}
	}
	for name, entry := range cfg.Providers.IndexedDB {
		if entry == nil || !entry.HasManagedSource() {
			continue
		}
		lockEntry, found := lock.IndexedDBs[name]
		if !lockEntryMatches(paths, name, entry, lockEntry, found) {
			return false
		}
	}
	for name, entry := range cfg.Providers.UI {
		if entry == nil || entry.Disabled || !entry.HasManagedSource() {
			continue
		}
		lockEntry, ok := lock.UIs[name]
		if !ok {
			return false
		}
		fingerprint, err := NamedUIProviderFingerprint(name, &entry.ProviderEntry)
		if err != nil || lockEntry.Fingerprint != fingerprint {
			return false
		}
		manifestPath := resolveLockPath(paths.artifactsDir, lockEntry.Manifest)
		if _, err := os.Stat(manifestPath); err != nil {
			return false
		}
		assetRootPath := resolveLockPath(paths.artifactsDir, lockEntry.AssetRoot)
		if _, err := os.Stat(assetRootPath); err != nil {
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
		Name:    name,
		Source:  entry.SourceRef(),
		Version: entry.SourceVersion(),
	}

	payload, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func NamedUIProviderFingerprint(name string, entry *config.ProviderEntry) (string, error) {
	return ProviderFingerprint("ui:"+name, entry, "")
}

func lockEntryMatches(paths initPaths, name string, providerEntry *config.ProviderEntry, entry LockEntry, found bool) bool {
	if !found {
		return false
	}
	fingerprint, err := ProviderFingerprint(name, providerEntry, paths.configDir)
	if err != nil || entry.Fingerprint != fingerprint {
		return false
	}
	if entry.Source != providerEntry.SourceRef() || entry.Version != providerEntry.SourceVersion() {
		return false
	}
	if len(entry.Archives) > 0 {
		platform := providerpkg.CurrentPlatformString()
		if _, _, ok := resolveArchiveForPlatform(entry, platform); !ok {
			return false
		}
	}
	manifestPath := resolveLockPath(paths.artifactsDir, entry.Manifest)
	if _, err := os.Stat(manifestPath); err != nil {
		return false
	}
	if entry.Executable != "" {
		executablePath := resolveLockPath(paths.artifactsDir, entry.Executable)
		if _, err := os.Stat(executablePath); err != nil {
			return false
		}
	}
	return true
}

func preparedManifestMatchesLock(entry LockEntry, manifest *providermanifestv1.Manifest) bool {
	if manifest == nil {
		return false
	}
	if entry.Source != "" && manifest.Source != entry.Source {
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

// buildArchivesMap constructs the Archives map for a lock entry. It enumerates
// all platform archives from the resolver (if supported) and records the
// verified SHA256 for the current platform.
func (l *Lifecycle) buildArchivesMap(ctx context.Context, src pluginsource.Source, version, currentURL, currentSHA256 string) map[string]LockArchive {
	currentPlatform := providerpkg.CurrentPlatformString()
	archives := map[string]LockArchive{
		currentPlatform: {URL: currentURL, SHA256: currentSHA256},
	}
	enumerator, ok := l.sourceResolver.(pluginsource.PlatformEnumerator)
	if !ok {
		return archives
	}
	platformArchives, err := enumerator.ListPlatformArchives(ctx, src, version)
	if err != nil {
		slog.Warn("failed to enumerate platform archives; lockfile will only contain current platform", "error", err)
		return archives
	}
	for _, pa := range platformArchives {
		if _, exists := archives[pa.Platform]; exists {
			continue
		}
		archives[pa.Platform] = LockArchive{URL: pa.URL}
	}
	return archives
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
		if !entry.HasManagedSource() {
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
	src, err := sourceForProvider(plugin)
	if err != nil {
		return LockEntry{}, fmt.Errorf("%s %q source.ref %q: %w", kind, name, plugin.SourceRef(), err)
	}
	if l.sourceResolver == nil {
		return LockEntry{}, fmt.Errorf("%s %q: source provider resolution requires a source resolver", kind, name)
	}
	resolved, err := l.sourceResolver.Resolve(ctx, src, plugin.SourceVersion())
	if err != nil {
		return LockEntry{}, fmt.Errorf("%s %q resolve source %q@%s: %w", kind, name, plugin.SourceRef(), plugin.SourceVersion(), err)
	}
	defer resolved.Cleanup()

	installed, err := pluginstore.Install(resolved.LocalPath, destDir)
	if err != nil {
		return LockEntry{}, fmt.Errorf("%s %q install source provider: %w", kind, name, err)
	}
	if err := validateInstalledManifestKind(kind, name, installed.Manifest); err != nil {
		return LockEntry{}, err
	}
	if installed.Manifest.Source != plugin.SourceRef() {
		return LockEntry{}, fmt.Errorf("%s %q: manifest source %q does not match config source %q", kind, name, installed.Manifest.Source, plugin.SourceRef())
	}
	if installed.Manifest.Version != plugin.SourceVersion() {
		return LockEntry{}, fmt.Errorf("%s %q: manifest version %q does not match config version %q", kind, name, installed.Manifest.Version, plugin.SourceVersion())
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
	archives := l.buildArchivesMap(ctx, src, plugin.SourceVersion(), resolved.ResolvedURL, resolved.ArchiveSHA256)
	return LockEntry{
		Fingerprint: fingerprint,
		Source:      plugin.SourceRef(),
		Version:     plugin.SourceVersion(),
		Archives:    archives,
		Manifest:    filepath.ToSlash(manifestPath),
		Executable:  filepath.ToSlash(executablePath),
	}, nil
}

func (l *Lifecycle) lockProviderEntryForSource(ctx context.Context, paths initPaths, name string, plugin *config.ProviderEntry, configMap map[string]any) (LockProviderEntry, error) {
	src, err := sourceForProvider(plugin)
	if err != nil {
		return LockProviderEntry{}, fmt.Errorf("provider %q source.ref %q: %w", name, plugin.SourceRef(), err)
	}
	if l.sourceResolver == nil {
		return LockProviderEntry{}, fmt.Errorf("provider %q: source provider resolution requires a source resolver", name)
	}
	resolved, err := l.sourceResolver.Resolve(ctx, src, plugin.SourceVersion())
	if err != nil {
		return LockProviderEntry{}, fmt.Errorf("provider %q resolve source %q@%s: %w", name, plugin.SourceRef(), plugin.SourceVersion(), err)
	}
	defer resolved.Cleanup()

	destDir := providerDestDir(paths, name)
	installed, err := pluginstore.Install(resolved.LocalPath, destDir)
	if err != nil {
		return LockProviderEntry{}, fmt.Errorf("provider %q install source provider: %w", name, err)
	}
	if err := validateInstalledManifestKind(providermanifestv1.KindPlugin, name, installed.Manifest); err != nil {
		return LockProviderEntry{}, err
	}

	if installed.Manifest.Source != plugin.SourceRef() {
		return LockProviderEntry{}, fmt.Errorf("provider %q: manifest source %q does not match config source %q", name, installed.Manifest.Source, plugin.SourceRef())
	}
	if installed.Manifest.Version != plugin.SourceVersion() {
		return LockProviderEntry{}, fmt.Errorf("provider %q: manifest version %q does not match config version %q", name, installed.Manifest.Version, plugin.SourceVersion())
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
	archives := l.buildArchivesMap(ctx, src, plugin.SourceVersion(), resolved.ResolvedURL, resolved.ArchiveSHA256)
	return LockProviderEntry{
		Fingerprint: fingerprint,
		Source:      plugin.SourceRef(),
		Version:     plugin.SourceVersion(),
		Archives:    archives,
		Manifest:    filepath.ToSlash(manifestPath),
		Executable:  filepath.ToSlash(executableRel),
	}, nil
}

func (l *Lifecycle) writeNamedUIProviderArtifact(ctx context.Context, paths initPaths, name string, plugin *config.ProviderEntry, destDir string, subject string) (LockUIEntry, error) {
	if plugin == nil || !plugin.HasManagedSource() {
		return LockUIEntry{}, fmt.Errorf("%s requires managed source", subject)
	}
	configMap, err := config.NodeToMap(plugin.Config)
	if err != nil {
		return LockUIEntry{}, fmt.Errorf("decode %s config: %w", subject, err)
	}
	fingerprint, err := NamedUIProviderFingerprint(name, plugin)
	if err != nil {
		return LockUIEntry{}, fmt.Errorf("fingerprinting %s: %w", subject, err)
	}

	src, err := sourceForProvider(plugin)
	if err != nil {
		return LockUIEntry{}, fmt.Errorf("%s source.ref %q: %w", subject, plugin.SourceRef(), err)
	}
	if l.sourceResolver == nil {
		return LockUIEntry{}, fmt.Errorf("%s: source resolution requires a source resolver", subject)
	}
	resolved, err := l.sourceResolver.Resolve(ctx, src, plugin.SourceVersion())
	if err != nil {
		return LockUIEntry{}, fmt.Errorf("%s resolve source %q@%s: %w", subject, plugin.SourceRef(), plugin.SourceVersion(), err)
	}
	defer resolved.Cleanup()

	installed, err := pluginstore.Install(resolved.LocalPath, destDir)
	if err != nil {
		return LockUIEntry{}, fmt.Errorf("%s install source: %w", subject, err)
	}
	if err := validateInstalledManifestKind(providermanifestv1.KindWebUI, subject, installed.Manifest); err != nil {
		return LockUIEntry{}, err
	}
	if installed.Manifest.Source != plugin.SourceRef() {
		return LockUIEntry{}, fmt.Errorf("%s manifest source %q does not match config source %q", subject, installed.Manifest.Source, plugin.SourceRef())
	}
	if installed.Manifest.Version != plugin.SourceVersion() {
		return LockUIEntry{}, fmt.Errorf("%s manifest version %q does not match config version %q", subject, installed.Manifest.Version, plugin.SourceVersion())
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
	archives := l.buildArchivesMap(ctx, src, plugin.SourceVersion(), resolved.ResolvedURL, resolved.ArchiveSHA256)
	return LockUIEntry{
		Fingerprint: fingerprint,
		Source:      plugin.SourceRef(),
		Version:     plugin.SourceVersion(),
		Archives:    archives,
		Manifest:    filepath.ToSlash(manifestPath),
		AssetRoot:   filepath.ToSlash(assetRoot),
	}, nil
}

func sourceForProvider(providerEntry *config.ProviderEntry) (pluginsource.Source, error) {
	src, err := pluginsource.Parse(providerEntry.SourceRef())
	if err != nil {
		return pluginsource.Source{}, err
	}
	if providerEntry != nil && providerEntry.Source.Auth != nil {
		auth := providerEntry.Source.Auth
		src.Token = auth.Token
	}
	return src, nil
}

func (l *Lifecycle) applyLockedProviders(configPath, artifactsDir string, cfg *config.Config, locked bool) error {
	if !configHasProviderLoading(cfg) {
		return nil
	}

	paths := initPathsForConfigWithArtifactsDir(configPath, resolveArtifactsDir(configPath, cfg, artifactsDir))
	var lock *Lockfile
	var err error
	if configHasManagedProviderSources(cfg) {
		var synthesizedLockedAppUIs map[string]struct{}
		lock, err = ReadLockfile(paths.lockfilePath)
		if err == nil {
			synthesizedLockedAppUIs, err = synthesizeLockedSourceAppOwnedUIEntries(cfg, paths, lock)
			if err != nil {
				clearSynthesizedAppOwnedUIEntries(cfg, synthesizedLockedAppUIs)
			}
		}
		if !locked && (err != nil || !lockMatchesConfig(cfg, paths, lock)) {
			clearSynthesizedAppOwnedUIEntries(cfg, synthesizedLockedAppUIs)
			lock, err = l.InitAtPathWithArtifactsDir(configPath, artifactsDir)
		}
		if err != nil {
			return fmt.Errorf("managed providers require prepared artifacts; run `gestaltd init --config %s`: %w", configPath, err)
		}
	}

	if err := l.resolveConfiguredPlugins(paths, lock, cfg, locked); err != nil {
		return err
	}
	if err := synthesizeAppOwnedUIEntries(cfg); err != nil {
		return err
	}
	for _, collection := range hostProviderCollections(cfg) {
		lockEntries := lockEntriesForKind(lock, collection.kind)
		for name, entry := range collection.entries {
			if entry == nil {
				continue
			}
			if err := l.applyComponentProvider(paths, lockEntries, providerManifestKind(collection.kind), name, entry, entry.Config, &entry.Config, componentDestDir(paths, collection.kind, name), locked); err != nil {
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
				return err
			}
		}
	}
	for name, entry := range cfg.Providers.UI {
		if entry == nil || entry.Disabled {
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
			return err
		}
		entry.ResolvedAssetRoot = resolvedAssetRoot
	}

	return nil
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
		case entry.HasManagedSource():
			if err := l.applyLockedProviderEntry(paths, lock, name, entry, configMap, locked); err != nil {
				return err
			}
		case entry.HasLocalSource():
			if err := applyLocalProviderManifest(name, entry, configMap); err != nil {
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

func synthesizeAppOwnedUIEntries(cfg *config.Config) error {
	if cfg == nil || len(cfg.Apps) == 0 {
		return nil
	}
	if cfg.Providers.UI == nil {
		cfg.Providers.UI = map[string]*config.UIEntry{}
	}

	appNames := slices.Sorted(maps.Keys(cfg.Apps))
	for _, appName := range appNames {
		app := cfg.Apps[appName]
		if app == nil || app.Disabled || strings.TrimSpace(app.UI) != "" {
			continue
		}
		pluginName := strings.TrimSpace(app.Plugin)
		plugin := cfg.Plugins[pluginName]
		if plugin == nil {
			return fmt.Errorf("config validation: apps.%s.plugin references unknown plugin %q", appName, pluginName)
		}
		manifestSpec := plugin.ManifestSpec()
		if manifestSpec == nil || manifestSpec.UI == nil {
			return fmt.Errorf("app %q plugin %q does not declare spec.ui", appName, pluginName)
		}
		ownedUI := manifestSpec.UI
		entry, err := ownedUIEntryForPlugin(plugin, ownedUI)
		if err != nil {
			return fmt.Errorf("app %q plugin %q ui: %w", appName, pluginName, err)
		}
		entry.Path = strings.TrimSpace(app.Path)
		entry.AuthorizationPolicy = strings.TrimSpace(app.AuthorizationPolicy)
		if existing := cfg.Providers.UI[appName]; existing != nil {
			if err := validateSynthesizedAppUIEntry(appName, existing, entry); err != nil {
				return err
			}
			if existing.Source.Auth == nil && entry.Source.Auth != nil {
				existing.Source.Auth = entry.Source.Auth
			}
			existing.Path = cmp.Or(existing.Path, entry.Path)
			existing.AuthorizationPolicy = cmp.Or(existing.AuthorizationPolicy, entry.AuthorizationPolicy)
			continue
		}
		cfg.Providers.UI[appName] = entry
	}
	return nil
}

func synthesizeLocalSourceAppOwnedUIEntries(cfg *config.Config) error {
	if cfg == nil || len(cfg.Apps) == 0 {
		return nil
	}
	if cfg.Providers.UI == nil {
		cfg.Providers.UI = map[string]*config.UIEntry{}
	}
	appNames := slices.Sorted(maps.Keys(cfg.Apps))
	for _, appName := range appNames {
		app := cfg.Apps[appName]
		if app == nil || app.Disabled || strings.TrimSpace(app.UI) != "" {
			continue
		}
		pluginName := strings.TrimSpace(app.Plugin)
		plugin := cfg.Plugins[pluginName]
		if plugin == nil || !plugin.HasLocalSource() {
			continue
		}
		manifestPath := plugin.SourcePath()
		if manifestPath == "" {
			continue
		}
		_, manifest, err := providerpkg.ReadSourceManifestFile(manifestPath)
		if err != nil {
			return fmt.Errorf("prepare manifest for provider %q: %w", pluginName, err)
		}
		if manifest == nil || manifest.Spec == nil || manifest.Spec.UI == nil {
			continue
		}
		entry, err := ownedUIEntryFromManifest(manifestPath, manifest.Version, manifest.Spec.UI)
		if err != nil {
			return fmt.Errorf("app %q plugin %q ui: %w", appName, pluginName, err)
		}
		entry.Path = strings.TrimSpace(app.Path)
		entry.AuthorizationPolicy = strings.TrimSpace(app.AuthorizationPolicy)
		if existing := cfg.Providers.UI[appName]; existing != nil {
			if err := validateSynthesizedAppUIEntry(appName, existing, entry); err != nil {
				return err
			}
			if existing.Source.Auth == nil && entry.Source.Auth != nil {
				existing.Source.Auth = entry.Source.Auth
			}
			existing.Path = cmp.Or(existing.Path, entry.Path)
			existing.AuthorizationPolicy = cmp.Or(existing.AuthorizationPolicy, entry.AuthorizationPolicy)
			continue
		}
		cfg.Providers.UI[appName] = entry
	}
	return nil
}

func synthesizeLockedSourceAppOwnedUIEntries(cfg *config.Config, paths initPaths, lock *Lockfile) (map[string]struct{}, error) {
	added := map[string]struct{}{}
	if cfg == nil || len(cfg.Apps) == 0 || lock == nil {
		return added, nil
	}
	if cfg.Providers.UI == nil {
		cfg.Providers.UI = map[string]*config.UIEntry{}
	}
	appNames := slices.Sorted(maps.Keys(cfg.Apps))
	for _, appName := range appNames {
		app := cfg.Apps[appName]
		if app == nil || app.Disabled || strings.TrimSpace(app.UI) != "" {
			continue
		}
		pluginName := strings.TrimSpace(app.Plugin)
		plugin := cfg.Plugins[pluginName]
		if plugin == nil || !plugin.HasManagedSource() {
			continue
		}
		lockEntry, ok := lock.Providers[pluginName]
		if !ok || strings.TrimSpace(lockEntry.Manifest) == "" {
			continue
		}
		manifestPath := resolveLockPath(paths.artifactsDir, lockEntry.Manifest)
		_, manifest, err := providerpkg.ReadManifestFile(manifestPath)
		if err != nil {
			return added, fmt.Errorf("prepare manifest for provider %q: %w", pluginName, err)
		}
		if manifest == nil || manifest.Spec == nil || manifest.Spec.UI == nil {
			continue
		}
		entry, err := ownedUIEntryFromManifest(manifestPath, manifest.Version, manifest.Spec.UI)
		if err != nil {
			return added, fmt.Errorf("app %q plugin %q ui: %w", appName, pluginName, err)
		}
		entry.Path = strings.TrimSpace(app.Path)
		entry.AuthorizationPolicy = strings.TrimSpace(app.AuthorizationPolicy)
		if existing := cfg.Providers.UI[appName]; existing != nil {
			if err := validateSynthesizedAppUIEntry(appName, existing, entry); err != nil {
				return added, err
			}
			if existing.Source.Auth == nil && entry.Source.Auth != nil {
				existing.Source.Auth = entry.Source.Auth
			}
			existing.Path = cmp.Or(existing.Path, entry.Path)
			existing.AuthorizationPolicy = cmp.Or(existing.AuthorizationPolicy, entry.AuthorizationPolicy)
			continue
		}
		cfg.Providers.UI[appName] = entry
		added[appName] = struct{}{}
	}
	return added, nil
}

func clearSynthesizedAppOwnedUIEntries(cfg *config.Config, added map[string]struct{}) {
	if cfg == nil || len(added) == 0 || cfg.Providers.UI == nil {
		return
	}
	for name := range added {
		delete(cfg.Providers.UI, name)
	}
}

func ownedUIEntryForPlugin(plugin *config.ProviderEntry, ownedUI *providermanifestv1.OwnedUIRef) (*config.UIEntry, error) {
	if plugin == nil || ownedUI == nil {
		return nil, fmt.Errorf("owned ui definition is required")
	}
	manifestVersion := plugin.SourceVersion()
	if plugin.ResolvedManifest != nil {
		manifestVersion = cmp.Or(plugin.ResolvedManifest.Version, manifestVersion)
	}
	return ownedUIEntryFromManifest(plugin.ResolvedManifestPath, manifestVersion, ownedUI)
}

func ownedUIEntryFromManifest(manifestPath, manifestVersion string, ownedUI *providermanifestv1.OwnedUIRef) (*config.UIEntry, error) {
	if ownedUI == nil {
		return nil, fmt.Errorf("owned ui definition is required")
	}
	entry := &config.UIEntry{}
	switch {
	case strings.TrimSpace(ownedUI.Ref) != "":
		entry.Source.Ref = strings.TrimSpace(ownedUI.Ref)
		entry.Source.Version = cmp.Or(strings.TrimSpace(ownedUI.Version), manifestVersion)
		if ownedUI.Auth != nil {
			entry.Source.Auth = &config.SourceAuthDef{Token: strings.TrimSpace(ownedUI.Auth.Token)}
		}
	case strings.TrimSpace(ownedUI.Path) != "":
		if strings.TrimSpace(manifestPath) == "" {
			return nil, fmt.Errorf("resolved plugin manifest path is required for spec.ui.path")
		}
		entry.Source.Path = filepath.Clean(filepath.Join(filepath.Dir(manifestPath), filepath.FromSlash(ownedUI.Path)))
	default:
		return nil, fmt.Errorf("spec.ui.path or spec.ui.ref is required")
	}
	return entry, nil
}

func validateSynthesizedAppUIEntry(appName string, existing, expected *config.UIEntry) error {
	if existing == nil || expected == nil {
		return nil
	}
	if existing.Disabled {
		return fmt.Errorf("config validation: apps.%s owned ui conflicts with disabled providers.ui.%s", appName, appName)
	}
	if current := strings.TrimSpace(existing.Source.Ref); current != "" && current != expected.Source.Ref {
		return fmt.Errorf("config validation: apps.%s owned ui conflicts with providers.ui.%s.source.ref", appName, appName)
	}
	if current := strings.TrimSpace(existing.Source.Version); current != "" && current != expected.Source.Version {
		return fmt.Errorf("config validation: apps.%s owned ui conflicts with providers.ui.%s.source.version", appName, appName)
	}
	currentAuth := ""
	if existing.Source.Auth != nil {
		currentAuth = strings.TrimSpace(existing.Source.Auth.Token)
	}
	expectedAuth := ""
	if expected.Source.Auth != nil {
		expectedAuth = strings.TrimSpace(expected.Source.Auth.Token)
	}
	if currentAuth != "" && currentAuth != expectedAuth {
		return fmt.Errorf("config validation: apps.%s owned ui conflicts with providers.ui.%s.source.auth.token", appName, appName)
	}
	if current := strings.TrimSpace(existing.Source.Path); current != "" && current != expected.Source.Path {
		return fmt.Errorf("config validation: apps.%s owned ui conflicts with providers.ui.%s.source.path", appName, appName)
	}
	if current := strings.TrimSpace(existing.Path); current != "" && current != expected.Path {
		return fmt.Errorf("config validation: apps.%s.path %q conflicts with providers.ui.%s.path", appName, expected.Path, appName)
	}
	if current := strings.TrimSpace(existing.AuthorizationPolicy); current != "" && current != expected.AuthorizationPolicy {
		return fmt.Errorf("config validation: apps.%s.authorizationPolicy conflicts with providers.ui.%s.authorizationPolicy", appName, appName)
	}
	return nil
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
	case provider.HasManagedSource():
		if lockEntry == nil {
			return "", fmt.Errorf("prepared artifact for %s is missing or stale; run `gestaltd init --config %s`", subject, paths.configPath)
		}
		fingerprint, err := NamedUIProviderFingerprint(logicalName, provider)
		if err != nil || lockEntry.Fingerprint != fingerprint {
			return "", fmt.Errorf("prepared artifact for %s is missing or stale; run `gestaltd init --config %s`", subject, paths.configPath)
		}
		manifestPath := resolveLockPath(paths.artifactsDir, lockEntry.Manifest)
		assetRootPath := resolveLockPath(paths.artifactsDir, lockEntry.AssetRoot)
		needMaterialize := false
		if _, err := os.Stat(manifestPath); err != nil {
			needMaterialize = true
		}
		if !needMaterialize {
			if _, err := os.Stat(assetRootPath); err != nil {
				needMaterialize = true
			}
		}
		if !needMaterialize {
			_, manifest, err := providerpkg.ReadManifestFile(manifestPath)
			if err != nil {
				return "", fmt.Errorf("read prepared manifest for %s: %w", subject, err)
			}
			if !preparedManifestMatchesLock(*lockEntry, manifest) {
				needMaterialize = true
			}
		}
		if needMaterialize {
			if err := l.materializeLockedUIProvider(context.Background(), paths, provider, *lockEntry, destDir, locked); err != nil {
				return "", err
			}
		}
		if _, err := os.Stat(manifestPath); err != nil {
			return "", fmt.Errorf("prepared manifest for %s not found at %s", subject, manifestPath)
		}
		if _, err := os.Stat(assetRootPath); err != nil {
			return "", fmt.Errorf("prepared asset root for %s not found at %s", subject, assetRootPath)
		}
		_, manifest, err := providerpkg.ReadManifestFile(manifestPath)
		if err != nil {
			return "", fmt.Errorf("read prepared manifest for %s: %w", subject, err)
		}
		if err := bindResolvedUIManifest(provider, manifestPath, manifest, configMap); err != nil {
			return "", err
		}
		return assetRootPath, nil
	case provider.HasLocalSource():
		var resolvedAssetRoot string
		if err := applyLocalUIManifest(provider, configMap, &resolvedAssetRoot); err != nil {
			return "", err
		}
		return resolvedAssetRoot, nil
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
	case provider.HasManagedSource():
		if lockEntries == nil {
			return fmt.Errorf("prepared artifact for %s %q is missing or stale; run `gestaltd init --config %s`", kind, name, paths.configPath)
		}
		lockEntry, ok := lockEntries[name]
		if !ok {
			return fmt.Errorf("prepared artifact for %s %q is missing or stale; run `gestaltd init --config %s`", kind, name, paths.configPath)
		}
		if err := l.applyLockedComponentEntry(paths, &lockEntry, kind, name, provider, configMap, destDir, locked); err != nil {
			return err
		}
	case provider.HasLocalSource():
		if err := applyLocalComponentManifest(kind, name, provider, configMap); err != nil {
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

func applyLocalProviderManifest(name string, plugin *config.ProviderEntry, configMap map[string]any) error {
	if plugin == nil || !plugin.HasLocalSource() {
		return nil
	}

	manifestPath := plugin.SourcePath()
	if _, err := os.Stat(manifestPath); err != nil {
		return fmt.Errorf("manifest for provider %q not found at %s: %w", name, manifestPath, err)
	}

	_, manifest, err := providerpkg.PrepareSourceManifest(manifestPath)
	if err != nil {
		return fmt.Errorf("prepare manifest for provider %q: %w", name, err)
	}
	if err := bindResolvedProviderManifest(name, plugin, manifestPath, manifest, configMap); err != nil {
		return err
	}
	if plugin.Command != "" {
		return nil
	}
	if entry := providerpkg.EntrypointForKind(plugin.ResolvedManifest, providermanifestv1.KindPlugin); entry != nil {
		candidate := filepath.Join(filepath.Dir(manifestPath), filepath.FromSlash(entry.ArtifactPath))
		if _, err := os.Stat(candidate); err == nil {
			plugin.Command = candidate
			plugin.Args = append([]string(nil), entry.Args...)
		}
	}
	return nil
}

func applyLocalComponentManifest(kind, name string, plugin *config.ProviderEntry, configMap map[string]any) error {
	if plugin == nil || !plugin.HasLocalSource() {
		return nil
	}

	manifestPath := plugin.SourcePath()
	if _, err := os.Stat(manifestPath); err != nil {
		return fmt.Errorf("manifest for %s %q not found at %s: %w", kind, name, manifestPath, err)
	}

	_, manifest, err := providerpkg.ReadSourceManifestFile(manifestPath)
	if err != nil {
		return fmt.Errorf("prepare manifest for %s %q: %w", kind, name, err)
	}
	if err := bindResolvedComponentManifest(kind, name, plugin, manifestPath, manifest, configMap); err != nil {
		return err
	}
	if plugin.Command != "" {
		return nil
	}
	if entry := providerpkg.EntrypointForKind(plugin.ResolvedManifest, kind); entry != nil {
		candidate := filepath.Join(filepath.Dir(manifestPath), filepath.FromSlash(entry.ArtifactPath))
		if _, err := os.Stat(candidate); err == nil {
			plugin.Command = candidate
			plugin.Args = append([]string(nil), entry.Args...)
		}
	}
	return nil
}

func applyLocalUIManifest(plugin *config.ProviderEntry, configMap map[string]any, resolvedAssetRoot *string) error {
	if plugin == nil || !plugin.HasLocalSource() {
		return nil
	}

	manifestPath := plugin.SourcePath()
	if _, err := os.Stat(manifestPath); err != nil {
		return fmt.Errorf("manifest for ui provider not found at %s: %w", manifestPath, err)
	}

	_, manifest, err := providerpkg.ReadSourceManifestFile(manifestPath)
	if err != nil {
		return fmt.Errorf("prepare manifest for ui provider: %w", err)
	}
	if err := bindResolvedUIManifest(plugin, manifestPath, manifest, configMap); err != nil {
		return err
	}
	assetRoot := filepath.Join(filepath.Dir(manifestPath), filepath.FromSlash(manifest.Spec.AssetRoot))
	if _, err := os.Stat(assetRoot); err != nil {
		return fmt.Errorf("ui provider asset root not found at %s: %w", assetRoot, err)
	}
	*resolvedAssetRoot = assetRoot
	return nil
}

func (l *Lifecycle) applyLockedProviderEntry(paths initPaths, lock *Lockfile, name string, plugin *config.ProviderEntry, configMap map[string]any, locked bool) error {
	entry, ok := lock.Providers[name]
	if !ok {
		return fmt.Errorf("prepared artifact for provider %q is missing or stale; run `gestaltd init --config %s`", name, paths.configPath)
	}
	fingerprint, err := ProviderFingerprint(name, plugin, paths.configDir)
	if err != nil {
		return fmt.Errorf("fingerprinting provider %q: %w", name, err)
	}
	if entry.Fingerprint != fingerprint || entry.Source != plugin.SourceRef() || entry.Version != plugin.SourceVersion() {
		return fmt.Errorf("prepared artifact for provider %q is missing or stale; run `gestaltd init --config %s`", name, paths.configPath)
	}

	manifestPath := resolveLockPath(paths.artifactsDir, entry.Manifest)
	executablePath := resolveLockPath(paths.artifactsDir, entry.Executable)
	needMaterialize := false
	if _, err := os.Stat(manifestPath); err != nil {
		needMaterialize = true
	}
	if !needMaterialize && entry.Executable != "" {
		if _, err := os.Stat(executablePath); err != nil {
			needMaterialize = true
		}
	}
	if !needMaterialize {
		_, manifest, err := providerpkg.ReadManifestFile(manifestPath)
		if err != nil {
			return fmt.Errorf("read prepared manifest for provider %q: %w", name, err)
		}
		if !preparedManifestMatchesLock(entry, manifest) {
			needMaterialize = true
		}
	}
	if needMaterialize {
		if err := l.materializeLockedProvider(context.Background(), paths, name, plugin, entry, locked); err != nil {
			return err
		}
	}
	if _, err := os.Stat(manifestPath); err != nil {
		return fmt.Errorf("prepared manifest for provider %q not found at %s; run `gestaltd init --config %s`", name, manifestPath, paths.configPath)
	}

	_, manifest, err := providerpkg.ReadManifestFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read prepared manifest for provider %q: %w", name, err)
	}
	if err := bindResolvedProviderManifest(name, plugin, manifestPath, manifest, configMap); err != nil {
		return err
	}
	if entry.Executable != "" {
		if _, err := os.Stat(executablePath); err != nil {
			return fmt.Errorf("prepared executable for provider %q not found at %s; run `gestaltd init --config %s`", name, executablePath, paths.configPath)
		}
		args, err := providerEntrypointArgs(manifest)
		if err != nil {
			return fmt.Errorf("resolve entrypoint for provider %q: %w", name, err)
		}
		plugin.Command = executablePath
		plugin.Args = append([]string(nil), args...)
	}
	return nil
}

func (l *Lifecycle) applyLockedComponentEntry(paths initPaths, entry *LockEntry, kind, name string, plugin *config.ProviderEntry, configMap map[string]any, destDir string, locked bool) error {
	if entry == nil {
		return fmt.Errorf("prepared artifact for %s %q is missing or stale; run `gestaltd init --config %s`", kind, name, paths.configPath)
	}
	fingerprint, err := ProviderFingerprint(name, plugin, paths.configDir)
	if err != nil {
		return fmt.Errorf("fingerprinting %s %q provider: %w", kind, name, err)
	}
	if entry.Fingerprint != fingerprint || entry.Source != plugin.SourceRef() || entry.Version != plugin.SourceVersion() {
		return fmt.Errorf("prepared artifact for %s %q is missing or stale; run `gestaltd init --config %s`", kind, name, paths.configPath)
	}

	manifestPath := resolveLockPath(paths.artifactsDir, entry.Manifest)
	executablePath := resolveLockPath(paths.artifactsDir, entry.Executable)
	needMaterialize := false
	if _, err := os.Stat(manifestPath); err != nil {
		needMaterialize = true
	}
	if !needMaterialize {
		if _, err := os.Stat(executablePath); err != nil {
			needMaterialize = true
		}
	}
	if !needMaterialize {
		_, manifest, err := providerpkg.ReadManifestFile(manifestPath)
		if err != nil {
			return fmt.Errorf("read prepared manifest for %s %q: %w", kind, name, err)
		}
		if !preparedManifestMatchesLock(*entry, manifest) {
			needMaterialize = true
		}
	}
	if needMaterialize {
		if err := l.materializeLockedComponent(context.Background(), paths, kind, name, plugin, *entry, destDir, locked); err != nil {
			return err
		}
	}
	if _, err := os.Stat(manifestPath); err != nil {
		return fmt.Errorf("prepared manifest for %s %q not found at %s; run `gestaltd init --config %s`", kind, name, manifestPath, paths.configPath)
	}

	_, manifest, err := providerpkg.ReadManifestFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read prepared manifest for %s %q: %w", kind, name, err)
	}
	if err := bindResolvedComponentManifest(kind, name, plugin, manifestPath, manifest, configMap); err != nil {
		return err
	}
	if _, err := os.Stat(executablePath); err != nil {
		return fmt.Errorf("prepared executable for %s %q not found at %s; run `gestaltd init --config %s`", kind, name, executablePath, paths.configPath)
	}
	args, err := componentEntrypointArgs(manifest, kind)
	if err != nil {
		return fmt.Errorf("resolve entrypoint for %s %q: %w", kind, name, err)
	}
	plugin.Command = executablePath
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

func (l *Lifecycle) materializeLockedProvider(ctx context.Context, paths initPaths, name string, plugin *config.ProviderEntry, entry LockProviderEntry, locked bool) error {
	platform := providerpkg.CurrentPlatformString()
	archive, _, ok := resolveArchiveForPlatform(entry, platform)
	if !ok || archive.URL == "" {
		return fmt.Errorf("no archive for platform %s for provider %q; run `gestaltd init --config %s`", platform, name, paths.configPath)
	}
	if locked && archive.SHA256 == "" {
		return fmt.Errorf("no verified hash for platform %s for provider %q; run `gestaltd init --platform %s`", platform, name, platform)
	}

	src, parseErr := sourceForProvider(plugin)
	if parseErr != nil {
		src, parseErr = pluginsource.Parse(entry.Source)
	}
	var (
		download *providerpkg.DownloadResult
		err      error
	)
	if parseErr == nil && src.Host == pluginsource.HostGitHub {
		download, err = ghresolver.DownloadResolvedAsset(ctx, http.DefaultClient, archive.URL, src.Token)
	} else {
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, archive.URL, nil)
		if reqErr != nil {
			return fmt.Errorf("create locked source provider request for provider %q: %w", name, reqErr)
		}
		download, err = providerpkg.DownloadRequest(http.DefaultClient, req)
	}
	if err != nil {
		return fmt.Errorf("download locked source provider for provider %q: %w", name, err)
	}
	defer download.Cleanup()
	if archive.SHA256 != "" && download.SHA256Hex != archive.SHA256 {
		return fmt.Errorf("locked source provider digest mismatch for provider %q: got %s, want %s", name, download.SHA256Hex, archive.SHA256)
	}

	destDir := providerDestDir(paths, name)
	if err := os.RemoveAll(destDir); err != nil {
		return fmt.Errorf("remove stale provider cache for provider %q: %w", name, err)
	}
	installed, err := pluginstore.Install(download.LocalPath, destDir)
	if err != nil {
		return fmt.Errorf("install locked source provider for provider %q: %w", name, err)
	}
	if installed.Manifest.Source != entry.Source {
		return fmt.Errorf("locked source provider manifest source mismatch for provider %q: got %q, want %q", name, installed.Manifest.Source, entry.Source)
	}
	if installed.Manifest.Version != entry.Version {
		return fmt.Errorf("locked source provider manifest version mismatch for provider %q: got %q, want %q", name, installed.Manifest.Version, entry.Version)
	}
	return nil
}

func (l *Lifecycle) materializeLockedComponent(ctx context.Context, paths initPaths, kind, name string, plugin *config.ProviderEntry, entry LockEntry, destDir string, locked bool) error {
	platform := providerpkg.CurrentPlatformString()
	archive, _, ok := resolveArchiveForPlatform(entry, platform)
	if !ok || archive.URL == "" {
		return fmt.Errorf("no archive for platform %s for %s %q; run `gestaltd init --config %s`", platform, kind, name, paths.configPath)
	}
	if locked && archive.SHA256 == "" {
		return fmt.Errorf("no verified hash for platform %s for %s %q; run `gestaltd init --platform %s`", platform, kind, name, platform)
	}

	src, parseErr := sourceForProvider(plugin)
	if parseErr != nil {
		src, parseErr = pluginsource.Parse(entry.Source)
	}
	var (
		download *providerpkg.DownloadResult
		err      error
	)
	if parseErr == nil && src.Host == pluginsource.HostGitHub {
		download, err = ghresolver.DownloadResolvedAsset(ctx, http.DefaultClient, archive.URL, src.Token)
	} else {
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, archive.URL, nil)
		if reqErr != nil {
			return fmt.Errorf("create locked source provider request for %s %q: %w", kind, name, reqErr)
		}
		download, err = providerpkg.DownloadRequest(http.DefaultClient, req)
	}
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
	if err := os.RemoveAll(destDir); err != nil {
		return fmt.Errorf("remove stale provider cache for %s %q: %w", kind, name, err)
	}
	installed, err := pluginstore.Install(download.LocalPath, destDir)
	if err != nil {
		return fmt.Errorf("install locked source provider for %s %q: %w", kind, name, err)
	}
	if err := validateInstalledManifestKind(kind, name, installed.Manifest); err != nil {
		return err
	}
	if installed.Manifest.Source != entry.Source {
		return fmt.Errorf("locked source provider manifest source mismatch for %s %q: got %q, want %q", kind, name, installed.Manifest.Source, entry.Source)
	}
	if installed.Manifest.Version != entry.Version {
		return fmt.Errorf("locked source provider manifest version mismatch for %s %q: got %q, want %q", kind, name, installed.Manifest.Version, entry.Version)
	}
	return nil
}

func (l *Lifecycle) materializeLockedUIProvider(ctx context.Context, paths initPaths, plugin *config.ProviderEntry, entry LockUIEntry, destDir string, locked bool) error {
	platform := providerpkg.CurrentPlatformString()
	archive, _, ok := resolveArchiveForPlatform(entry, platform)
	if !ok || archive.URL == "" {
		return fmt.Errorf("no archive for platform %s for ui provider; run `gestaltd init --config %s`", platform, paths.configPath)
	}
	if locked && archive.SHA256 == "" {
		return fmt.Errorf("no verified hash for platform %s for ui provider; run `gestaltd init --platform %s`", platform, platform)
	}

	src, parseErr := sourceForProvider(plugin)
	if parseErr != nil {
		src, parseErr = pluginsource.Parse(entry.Source)
	}
	var (
		download *providerpkg.DownloadResult
		err      error
	)
	if parseErr == nil && src.Host == pluginsource.HostGitHub {
		download, err = ghresolver.DownloadResolvedAsset(ctx, http.DefaultClient, archive.URL, src.Token)
	} else {
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, archive.URL, nil)
		if reqErr != nil {
			return fmt.Errorf("create locked source request for ui provider: %w", reqErr)
		}
		download, err = providerpkg.DownloadRequest(http.DefaultClient, req)
	}
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
	return nil
}

func downloadPlatformArchives(ctx context.Context, lock *Lockfile, platforms []struct{ GOOS, GOARCH, LibC string }, tokenForSource map[string]string) error {
	for _, plat := range platforms {
		platformKey := providerpkg.PlatformString(plat.GOOS, plat.GOARCH)
		if err := hashPlatformInEntries(ctx, lock, platformKey, tokenForSource); err != nil {
			return err
		}
	}
	return nil
}

func hashPlatformInEntries(ctx context.Context, lock *Lockfile, platformKey string, tokenForSource map[string]string) error {
	for name, entry := range lock.Providers {
		if err := hashArchiveEntry(ctx, &entry, platformKey, tokenForSource); err != nil {
			return err
		}
		lock.Providers[name] = entry
	}
	for _, lockEntries := range []map[string]LockEntry{lock.Auth, lock.Secrets, lock.Telemetry, lock.Audit} {
		for name, entry := range lockEntries {
			if err := hashArchiveEntry(ctx, &entry, platformKey, tokenForSource); err != nil {
				return err
			}
			lockEntries[name] = entry
		}
	}
	for name, entry := range lock.IndexedDBs {
		if err := hashArchiveEntry(ctx, &entry, platformKey, tokenForSource); err != nil {
			return err
		}
		lock.IndexedDBs[name] = entry
	}
	for name, entry := range lock.UIs {
		if err := hashArchiveEntry(ctx, &entry, platformKey, tokenForSource); err != nil {
			return err
		}
		lock.UIs[name] = entry
	}
	return nil
}

func hashArchiveEntry(ctx context.Context, entry *LockEntry, platformKey string, tokenForSource map[string]string) error {
	if entry.Archives == nil {
		return nil
	}
	archive, resolvedKey, ok := resolveArchiveForPlatform(*entry, platformKey)
	if !ok || archive.URL == "" || archive.SHA256 != "" {
		return nil
	}
	token := tokenForSource[entry.Source]
	src, parseErr := pluginsource.Parse(entry.Source)
	var (
		dl  *providerpkg.DownloadResult
		err error
	)
	if parseErr == nil && src.Host == pluginsource.HostGitHub {
		dl, err = ghresolver.DownloadResolvedAsset(ctx, http.DefaultClient, archive.URL, token)
	} else {
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, archive.URL, nil)
		if reqErr != nil {
			return fmt.Errorf("create request for platform %s, source %s: %w", platformKey, entry.Source, reqErr)
		}
		dl, err = providerpkg.DownloadRequest(http.DefaultClient, req)
	}
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
	if declared != kind {
		return fmt.Errorf("%s %q manifest has kind %q, want %q", kind, name, declared, kind)
	}
	return nil
}

func buildComponentRuntimeConfigNode(name, kind string, provider *config.ProviderEntry, providerConfig yaml.Node) (yaml.Node, error) {
	return config.BuildComponentRuntimeConfigNode(name, kind, provider, providerConfig)
}
