package operator

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/pluginpkg"
	"github.com/valon-technologies/gestalt/server/internal/pluginsource"
	ghresolver "github.com/valon-technologies/gestalt/server/internal/pluginsource/github"
	"github.com/valon-technologies/gestalt/server/internal/pluginstore"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
	"gopkg.in/yaml.v3"
)

const (
	InitLockfileName     = "gestalt.lock.json"
	PreparedProvidersDir = ".gestaltd/providers"
	PreparedAuthDir      = ".gestaltd/auth"
	PreparedSecretsDir   = ".gestaltd/secrets"
	PreparedTelemetryDir = ".gestaltd/telemetry"
	PreparedAuditDir     = ".gestaltd/audit"
	PreparedUIDir        = ".gestaltd/ui"
	LockVersion          = 2

	platformKeyGeneric = "generic"
)

type Lockfile struct {
	Version   int                          `json:"version"`
	Providers map[string]LockProviderEntry `json:"providers"`
	Auth      *LockEntry                   `json:"auth,omitempty"`
	Datastore *LockEntry                   `json:"datastore,omitempty"`
	Secrets   *LockEntry                   `json:"secrets,omitempty"`
	Telemetry *LockEntry                   `json:"telemetry,omitempty"`
	Audit     *LockEntry                   `json:"audit,omitempty"`
	UI        *LockUIEntry                 `json:"ui,omitempty"`
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
	secretsEntry, err := l.primeSecretsProviderForConfigResolution(context.Background(), paths, cfg, nil)
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
		Version:   LockVersion,
		Providers: make(map[string]LockProviderEntry),
	}

	resolvedProviders, err := l.writeProviderArtifacts(context.Background(), cfg, paths)
	if err != nil {
		return nil, nil, err
	}
	for name := range resolvedProviders {
		lock.Providers[name] = resolvedProviders[name]
	}
	if cfg.Auth.Provider != nil && cfg.Auth.Provider.HasManagedArtifacts() {
		entry, err := l.writeComponentArtifact(context.Background(), paths, pluginmanifestv1.KindAuth, "auth", authDestDir(paths), cfg.Auth.Provider, cfg.Auth.Config)
		if err != nil {
			return nil, nil, err
		}
		lock.Auth = &entry
	}
	if secretsEntry != nil {
		lock.Secrets = secretsEntry
	}
	if cfg.Telemetry.Provider != nil && cfg.Telemetry.Provider.HasManagedArtifacts() {
		entry, err := l.writeComponentArtifact(context.Background(), paths, pluginmanifestv1.KindPlugin, "telemetry", telemetryDestDir(paths), cfg.Telemetry.Provider, cfg.Telemetry.Config)
		if err != nil {
			return nil, nil, err
		}
		lock.Telemetry = &entry
	}
	if cfg.Audit.Provider != nil && cfg.Audit.Provider.HasManagedArtifacts() {
		entry, err := l.writeComponentArtifact(context.Background(), paths, pluginmanifestv1.KindPlugin, "audit", auditDestDir(paths), cfg.Audit.Provider, cfg.Audit.Config)
		if err != nil {
			return nil, nil, err
		}
		lock.Audit = &entry
	}
	for name := range cfg.IndexedDBs {
		def := cfg.IndexedDBs[name]
		if def.Provider != nil && def.Provider.HasManagedArtifacts() {
			entry, err := l.writeComponentArtifact(context.Background(), paths, pluginmanifestv1.KindIndexedDB, "indexeddb-"+name, indexeddbDestDir(paths, name), def.Provider, def.Config)
			if err != nil {
				return nil, nil, err
			}
			lock.Datastore = &entry
		}
	}
	if cfg.UI.Provider != nil && cfg.UI.Provider.HasManagedArtifacts() {
		uiEntry, err := l.writeUIProviderArtifact(context.Background(), cfg, paths)
		if err != nil {
			return nil, nil, err
		}
		lock.UI = &uiEntry
	}

	if err := WriteLockfile(paths.lockfilePath, lock); err != nil {
		return nil, nil, err
	}
	if err := l.applyLockedPlugins(configPath, artifactsDir, cfg, true); err != nil {
		return nil, nil, err
	}
	if err := config.ValidateResolvedStructure(cfg); err != nil {
		return nil, nil, err
	}

	slog.Info("prepared locked artifacts", "providers", len(lock.Providers), "auth", lock.Auth != nil, "secrets", lock.Secrets != nil, "telemetry", lock.Telemetry != nil, "audit", lock.Audit != nil, "ui", lock.UI != nil)
	slog.Info("wrote lockfile", "path", paths.lockfilePath)
	return lock, cfg, nil
}

func buildSourceTokenMap(cfg *config.Config) map[string]string {
	tokens := make(map[string]string)
	for _, intg := range cfg.Plugins {
		if intg.Plugin != nil && intg.Plugin.Source != nil && intg.Plugin.Source.Auth != nil {
			tokens[intg.Plugin.SourceRef()] = intg.Plugin.Source.Auth.Token
		}
	}
	for _, p := range []*config.ProviderDef{cfg.Auth.Provider, cfg.Secrets.Provider, cfg.Telemetry.Provider, cfg.Audit.Provider} {
		if p != nil && p.Source != nil && p.Source.Auth != nil {
			tokens[p.SourceRef()] = p.Source.Auth.Token
		}
	}
	for name := range cfg.IndexedDBs {
		def := cfg.IndexedDBs[name]
		if def.Provider != nil && def.Provider.Source != nil && def.Provider.Source.Auth != nil {
			tokens[def.Provider.SourceRef()] = def.Provider.Source.Auth.Token
		}
	}
	if cfg.UI.Provider != nil && cfg.UI.Provider.Source != nil && cfg.UI.Provider.Source.Auth != nil {
		tokens[cfg.UI.Provider.SourceRef()] = cfg.UI.Provider.Source.Auth.Token
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

	if err := l.applyLockedPlugins(configPath, artifactsDir, cfg, locked); err != nil {
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
	if cfg == nil || cfg.Secrets.Provider == nil || !cfg.Secrets.Provider.HasManagedArtifacts() {
		return nil, nil
	}
	if !configHasManagedPlugins(cfg) {
		return nil, nil
	}

	lock, err := ReadLockfile(paths.lockfilePath)
	if !locked && (err != nil || !lockMatchesConfig(cfg, paths, lock)) {
		lock, err = l.InitAtPathWithArtifactsDir(configPath, artifactsDir)
	}
	if err != nil {
		return nil, fmt.Errorf("managed plugins require prepared artifacts; run `gestaltd init --config %s`: %w", configPath, err)
	}
	return lock, nil
}

func (l *Lifecycle) primeSecretsProviderForConfigResolution(ctx context.Context, paths initPaths, cfg *config.Config, lock *Lockfile) (*LockEntry, error) {
	if cfg == nil || cfg.Secrets.Provider == nil {
		return nil, nil
	}

	provider := cfg.Secrets.Provider
	configMap, err := config.NodeToMap(cfg.Secrets.Config)
	if err != nil {
		return nil, fmt.Errorf("decode provider config for %s %q: %w", pluginmanifestv1.KindSecrets, "secrets", err)
	}

	switch {
	case provider.HasManagedArtifacts():
		if lock != nil {
			if err := l.applyLockedComponentEntry(paths, lock.Secrets, pluginmanifestv1.KindSecrets, "secrets", provider, configMap, false); err != nil {
				return nil, err
			}
			return nil, nil
		}
		entry, err := l.writeComponentArtifact(ctx, paths, pluginmanifestv1.KindSecrets, "secrets", secretsDestDir(paths), provider, cfg.Secrets.Config)
		if err != nil {
			return nil, err
		}
		if err := l.applyLockedComponentEntry(paths, &entry, pluginmanifestv1.KindSecrets, "secrets", provider, configMap, false); err != nil {
			return nil, err
		}
		return &entry, nil
	case provider.HasLocalSource():
		if err := applyLocalComponentManifest(pluginmanifestv1.KindSecrets, "secrets", provider, configMap); err != nil {
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
	uiDir        string
}

type pluginFingerprintInput struct {
	Name    string `json:"name"`
	Source  string `json:"source,omitempty"`
	Version string `json:"version,omitempty"`
}

func configHasPluginLoading(cfg *config.Config) bool {
	for name := range cfg.Plugins {
		plugin := cfg.Plugins[name].Plugin
		if plugin.HasManagedArtifacts() || plugin.HasLocalSource() {
			return true
		}
	}
	for _, p := range []*config.ProviderDef{cfg.Auth.Provider, cfg.Secrets.Provider, cfg.UI.Provider, cfg.Telemetry.Provider, cfg.Audit.Provider} {
		if p != nil && (p.HasManagedArtifacts() || p.HasLocalSource()) {
			return true
		}
	}
	for name := range cfg.IndexedDBs {
		def := cfg.IndexedDBs[name]
		if def.Provider != nil && (def.Provider.HasManagedArtifacts() || def.Provider.HasLocalSource()) {
			return true
		}
	}
	return false
}

func configHasManagedPlugins(cfg *config.Config) bool {
	for name := range cfg.Plugins {
		if cfg.Plugins[name].Plugin.HasManagedArtifacts() {
			return true
		}
	}
	for _, p := range []*config.ProviderDef{cfg.Auth.Provider, cfg.Secrets.Provider, cfg.UI.Provider, cfg.Telemetry.Provider, cfg.Audit.Provider} {
		if p != nil && p.HasManagedArtifacts() {
			return true
		}
	}
	for name := range cfg.IndexedDBs {
		if def := cfg.IndexedDBs[name]; def.Provider != nil && def.Provider.HasManagedArtifacts() {
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
		uiDir:        filepath.Join(artifactsDir, filepath.FromSlash(PreparedUIDir)),
	}
}

func providerDestDir(paths initPaths, name string) string {
	return filepath.Join(paths.providersDir, name)
}

func uiDestDir(paths initPaths) string {
	return paths.uiDir
}

func authDestDir(paths initPaths) string {
	return paths.authDir
}

func secretsDestDir(paths initPaths) string {
	return paths.secretsDir
}

func telemetryDestDir(paths initPaths) string {
	return paths.telemetryDir
}

func auditDestDir(paths initPaths) string {
	return paths.auditDir
}

func indexeddbDestDir(paths initPaths, name string) string {
	return filepath.Join(paths.artifactsDir, "indexeddb", name)
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
	var lock Lockfile
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("parsing lockfile %s: %w", path, err)
	}
	if lock.Version != LockVersion {
		return nil, fmt.Errorf("unsupported lockfile version %d; run `gestaltd init` to upgrade", lock.Version)
	}
	if lock.Providers == nil {
		lock.Providers = make(map[string]LockProviderEntry)
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
	for name := range cfg.Plugins {
		provider := cfg.Plugins[name]
		if !provider.Plugin.HasManagedArtifacts() {
			continue
		}
		entry, found := lock.Providers[name]
		if !lockEntryMatches(paths, name, provider.Plugin, entry, found) {
			return false
		}
	}
	if cfg.Auth.Provider != nil && cfg.Auth.Provider.HasManagedArtifacts() {
		if lock.Auth == nil || !lockEntryMatches(paths, "auth", cfg.Auth.Provider, *lock.Auth, true) {
			return false
		}
	}
	if cfg.Secrets.Provider != nil && cfg.Secrets.Provider.HasManagedArtifacts() {
		if lock.Secrets == nil || !lockEntryMatches(paths, "secrets", cfg.Secrets.Provider, *lock.Secrets, true) {
			return false
		}
	}
	if cfg.Telemetry.Provider != nil && cfg.Telemetry.Provider.HasManagedArtifacts() {
		if lock.Telemetry == nil || !lockEntryMatches(paths, "telemetry", cfg.Telemetry.Provider, *lock.Telemetry, true) {
			return false
		}
	}
	if cfg.Audit.Provider != nil && cfg.Audit.Provider.HasManagedArtifacts() {
		if lock.Audit == nil || !lockEntryMatches(paths, "audit", cfg.Audit.Provider, *lock.Audit, true) {
			return false
		}
	}
	if cfg.UI.Provider != nil && cfg.UI.Provider.HasManagedArtifacts() {
		if lock.UI == nil {
			return false
		}
		fingerprint, err := UIProviderFingerprint(cfg.UI.Provider)
		if err != nil || lock.UI.Fingerprint != fingerprint {
			return false
		}
		manifestPath := resolveLockPath(paths.artifactsDir, lock.UI.Manifest)
		if _, err := os.Stat(manifestPath); err != nil {
			return false
		}
		assetRootPath := resolveLockPath(paths.artifactsDir, lock.UI.AssetRoot)
		if _, err := os.Stat(assetRootPath); err != nil {
			return false
		}
	}
	return true
}

func PluginFingerprint(name string, plugin *config.ProviderDef, configDir string) (string, error) {
	if plugin == nil {
		return "", nil
	}

	input := pluginFingerprintInput{
		Name:    name,
		Source:  plugin.SourceRef(),
		Version: plugin.SourceVersion(),
	}

	payload, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func UIProviderFingerprint(plugin *config.ProviderDef) (string, error) {
	return PluginFingerprint("ui", plugin, "")
}

func lockEntryMatches(paths initPaths, name string, plugin *config.ProviderDef, entry LockEntry, found bool) bool {
	if !found {
		return false
	}
	fingerprint, err := PluginFingerprint(name, plugin, paths.configDir)
	if err != nil || entry.Fingerprint != fingerprint {
		return false
	}
	if entry.Source != plugin.SourceRef() || entry.Version != plugin.SourceVersion() {
		return false
	}
	if len(entry.Archives) > 0 {
		platform := pluginpkg.CurrentPlatformString()
		if _, ok := resolveArchiveForPlatform(entry, platform); !ok {
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

// resolveArchiveForPlatform looks up a LockArchive for the given platform
// string using a fallback chain: exact match → without libc → generic.
func resolveArchiveForPlatform(entry LockEntry, platform string) (LockArchive, bool) {
	if a, ok := entry.Archives[platform]; ok {
		return a, true
	}
	goos, goarch, _, err := pluginpkg.ParsePlatformString(platform)
	if err == nil {
		key := pluginpkg.PlatformString(goos, goarch, "")
		if key != platform {
			if a, ok := entry.Archives[key]; ok {
				return a, true
			}
		}
	}
	if a, ok := entry.Archives[platformKeyGeneric]; ok {
		return a, true
	}
	return LockArchive{}, false
}

// buildArchivesMap constructs the Archives map for a lock entry. It enumerates
// all platform archives from the resolver (if supported) and records the
// verified SHA256 for the current platform.
func (l *Lifecycle) buildArchivesMap(ctx context.Context, src pluginsource.Source, version, currentURL, currentSHA256 string) map[string]LockArchive {
	currentPlatform := pluginpkg.CurrentPlatformString()
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
	for name := range cfg.Plugins {
		provider := cfg.Plugins[name]
		if provider.Plugin == nil {
			continue
		}
		configMap, err := config.NodeToMap(provider.Plugin.Config)
		if err != nil {
			return nil, fmt.Errorf("decode plugin config for provider %q: %w", name, err)
		}
		if !provider.Plugin.HasManagedSource() {
			continue
		}
		entry, err := l.lockProviderEntryForSource(ctx, paths, name, provider.Plugin, configMap)
		if err != nil {
			return nil, err
		}
		written[name] = entry
	}

	return written, nil
}

func (l *Lifecycle) writeComponentArtifact(ctx context.Context, paths initPaths, kind, name, destDir string, plugin *config.ProviderDef, configNode yaml.Node) (LockEntry, error) {
	configMap, err := config.NodeToMap(configNode)
	if err != nil {
		return LockEntry{}, fmt.Errorf("decode plugin config for %s %q: %w", kind, name, err)
	}
	return l.lockComponentEntryForSource(ctx, paths, kind, name, destDir, plugin, configMap)
}

func (l *Lifecycle) lockComponentEntryForSource(ctx context.Context, paths initPaths, kind, name, destDir string, plugin *config.ProviderDef, configMap map[string]any) (LockEntry, error) {
	src, err := sourceForPlugin(plugin)
	if err != nil {
		return LockEntry{}, fmt.Errorf("%s %q plugin.source.ref %q: %w", kind, name, plugin.SourceRef(), err)
	}
	if l.sourceResolver == nil {
		return LockEntry{}, fmt.Errorf("%s %q: source plugin resolution requires a source resolver", kind, name)
	}
	resolved, err := l.sourceResolver.Resolve(ctx, src, plugin.SourceVersion())
	if err != nil {
		return LockEntry{}, fmt.Errorf("%s %q resolve source %q@%s: %w", kind, name, plugin.SourceRef(), plugin.SourceVersion(), err)
	}
	defer resolved.Cleanup()

	installed, err := pluginstore.Install(resolved.LocalPath, destDir)
	if err != nil {
		return LockEntry{}, fmt.Errorf("%s %q install source plugin: %w", kind, name, err)
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
	if err := pluginpkg.ValidateConfigForManifest(installed.ManifestPath, installed.Manifest, kind, configMap); err != nil {
		return LockEntry{}, fmt.Errorf("plugin config validation for %s %q: %w", kind, name, err)
	}

	entrypoint := pluginpkg.EntrypointForKind(installed.Manifest, kind)
	if entrypoint == nil {
		return LockEntry{}, fmt.Errorf("%s %q manifest does not define a %s entrypoint", kind, name, kind)
	}
	fingerprint, err := PluginFingerprint(name, plugin, paths.configDir)
	if err != nil {
		return LockEntry{}, fmt.Errorf("fingerprinting %s %q plugin: %w", kind, name, err)
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

func (l *Lifecycle) lockProviderEntryForSource(ctx context.Context, paths initPaths, name string, plugin *config.ProviderDef, configMap map[string]any) (LockProviderEntry, error) {
	src, err := sourceForPlugin(plugin)
	if err != nil {
		return LockProviderEntry{}, fmt.Errorf("provider %q plugin.source.ref %q: %w", name, plugin.SourceRef(), err)
	}
	if l.sourceResolver == nil {
		return LockProviderEntry{}, fmt.Errorf("provider %q: source plugin resolution requires a source resolver", name)
	}
	resolved, err := l.sourceResolver.Resolve(ctx, src, plugin.SourceVersion())
	if err != nil {
		return LockProviderEntry{}, fmt.Errorf("provider %q resolve source %q@%s: %w", name, plugin.SourceRef(), plugin.SourceVersion(), err)
	}
	defer resolved.Cleanup()

	destDir := providerDestDir(paths, name)
	installed, err := pluginstore.Install(resolved.LocalPath, destDir)
	if err != nil {
		return LockProviderEntry{}, fmt.Errorf("provider %q install source plugin: %w", name, err)
	}
	if err := validateInstalledManifestKind(pluginmanifestv1.KindPlugin, name, installed.Manifest); err != nil {
		return LockProviderEntry{}, err
	}

	if installed.Manifest.Source != plugin.SourceRef() {
		return LockProviderEntry{}, fmt.Errorf("provider %q: manifest source %q does not match config source %q", name, installed.Manifest.Source, plugin.SourceRef())
	}
	if installed.Manifest.Version != plugin.SourceVersion() {
		return LockProviderEntry{}, fmt.Errorf("provider %q: manifest version %q does not match config version %q", name, installed.Manifest.Version, plugin.SourceVersion())
	}

	if err := pluginpkg.ValidateConfigForManifest(installed.ManifestPath, installed.Manifest, pluginmanifestv1.KindPlugin, configMap); err != nil {
		return LockProviderEntry{}, fmt.Errorf("plugin config validation for provider %q: %w", name, err)
	}
	fingerprint, err := PluginFingerprint(name, plugin, paths.configDir)
	if err != nil {
		return LockProviderEntry{}, fmt.Errorf("fingerprinting provider %q plugin: %w", name, err)
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

func (l *Lifecycle) writeUIProviderArtifact(ctx context.Context, cfg *config.Config, paths initPaths) (LockUIEntry, error) {
	plugin := cfg.UI.Provider
	if plugin == nil || !plugin.HasManagedSource() {
		return LockUIEntry{}, fmt.Errorf("ui provider requires managed source")
	}
	configMap, err := config.NodeToMap(cfg.UI.Config)
	if err != nil {
		return LockUIEntry{}, fmt.Errorf("decode ui provider config: %w", err)
	}
	fingerprint, err := UIProviderFingerprint(plugin)
	if err != nil {
		return LockUIEntry{}, fmt.Errorf("fingerprinting ui provider: %w", err)
	}

	destDir := uiDestDir(paths)
	src, err := sourceForPlugin(plugin)
	if err != nil {
		return LockUIEntry{}, fmt.Errorf("ui provider source.ref %q: %w", plugin.SourceRef(), err)
	}
	if l.sourceResolver == nil {
		return LockUIEntry{}, fmt.Errorf("ui provider: source resolution requires a source resolver")
	}
	resolved, err := l.sourceResolver.Resolve(ctx, src, plugin.SourceVersion())
	if err != nil {
		return LockUIEntry{}, fmt.Errorf("ui provider resolve source %q@%s: %w", plugin.SourceRef(), plugin.SourceVersion(), err)
	}
	defer resolved.Cleanup()

	installed, err := pluginstore.Install(resolved.LocalPath, destDir)
	if err != nil {
		return LockUIEntry{}, fmt.Errorf("ui provider install source: %w", err)
	}
	if err := validateInstalledManifestKind(pluginmanifestv1.KindWebUI, "provider", installed.Manifest); err != nil {
		return LockUIEntry{}, err
	}
	if installed.Manifest.Source != plugin.SourceRef() {
		return LockUIEntry{}, fmt.Errorf("ui provider manifest source %q does not match config source %q", installed.Manifest.Source, plugin.SourceRef())
	}
	if installed.Manifest.Version != plugin.SourceVersion() {
		return LockUIEntry{}, fmt.Errorf("ui provider manifest version %q does not match config version %q", installed.Manifest.Version, plugin.SourceVersion())
	}
	if err := pluginpkg.ValidateConfigForManifest(installed.ManifestPath, installed.Manifest, pluginmanifestv1.KindWebUI, configMap); err != nil {
		return LockUIEntry{}, fmt.Errorf("plugin config validation for ui provider: %w", err)
	}
	manifestPath, err := filepath.Rel(paths.artifactsDir, installed.ManifestPath)
	if err != nil {
		return LockUIEntry{}, fmt.Errorf("compute manifest path for ui provider: %w", err)
	}
	assetRoot, err := filepath.Rel(paths.artifactsDir, installed.AssetRoot)
	if err != nil {
		return LockUIEntry{}, fmt.Errorf("compute asset root path for ui provider: %w", err)
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

func sourceForPlugin(plugin *config.ProviderDef) (pluginsource.Source, error) {
	src, err := pluginsource.Parse(plugin.SourceRef())
	if err != nil {
		return pluginsource.Source{}, err
	}
	if plugin != nil && plugin.Source != nil && plugin.Source.Auth != nil {
		auth := plugin.Source.Auth
		src.Token = auth.Token
	}
	return src, nil
}

func (l *Lifecycle) applyLockedPlugins(configPath, artifactsDir string, cfg *config.Config, locked bool) error {
	if !configHasPluginLoading(cfg) {
		return nil
	}

	paths := initPathsForConfigWithArtifactsDir(configPath, resolveArtifactsDir(configPath, cfg, artifactsDir))
	var lock *Lockfile
	var err error
	if configHasManagedPlugins(cfg) {
		lock, err = ReadLockfile(paths.lockfilePath)
		if !locked && (err != nil || !lockMatchesConfig(cfg, paths, lock)) {
			lock, err = l.InitAtPathWithArtifactsDir(configPath, artifactsDir)
		}
		if err != nil {
			return fmt.Errorf("managed plugins require prepared artifacts; run `gestaltd init --config %s`: %w", configPath, err)
		}
	}

	for name := range cfg.Plugins {
		provider := cfg.Plugins[name]
		if provider.Plugin == nil {
			continue
		}
		configMap, err := config.NodeToMap(provider.Plugin.Config)
		if err != nil {
			return fmt.Errorf("decode plugin config for provider %q: %w", name, err)
		}
		switch {
		case provider.Plugin.HasManagedArtifacts():
			if err := l.applyLockedProviderEntry(paths, lock, name, provider.Plugin, configMap, locked); err != nil {
				return err
			}
		case provider.Plugin.HasLocalSource():
			if err := applyLocalProviderManifest(name, provider.Plugin, configMap); err != nil {
				return err
			}
		default:
			continue
		}
		if manifest := provider.Plugin.ResolvedManifest; manifest != nil {
			provider.DisplayName = cmp.Or(provider.DisplayName, manifest.DisplayName)
			provider.Description = cmp.Or(provider.Description, manifest.Description)
		}
		provider.IconFile = cmp.Or(provider.IconFile, provider.Plugin.ResolvedIconFile)
		cfg.Plugins[name] = provider
	}
	if cfg.Auth.Provider != nil {
		if err := l.applyComponentProvider(paths, lock, pluginmanifestv1.KindAuth, "auth", cfg.Auth.Provider, cfg.Auth.Config, &cfg.Auth.Config, locked); err != nil {
			return err
		}
	}
	if cfg.Secrets.Provider != nil {
		if err := l.applyComponentProvider(paths, lock, pluginmanifestv1.KindSecrets, "secrets", cfg.Secrets.Provider, cfg.Secrets.Config, &cfg.Secrets.Config, locked); err != nil {
			return err
		}
	}
	if cfg.Telemetry.Provider != nil {
		if err := l.applyComponentProvider(paths, lock, pluginmanifestv1.KindPlugin, "telemetry", cfg.Telemetry.Provider, cfg.Telemetry.Config, &cfg.Telemetry.Config, locked); err != nil {
			return err
		}
	}
	if cfg.Audit.Provider != nil {
		if err := l.applyComponentProvider(paths, lock, pluginmanifestv1.KindPlugin, "audit", cfg.Audit.Provider, cfg.Audit.Config, &cfg.Audit.Config, locked); err != nil {
			return err
		}
	}
	for name := range cfg.IndexedDBs {
		def := cfg.IndexedDBs[name]
		if def.Provider != nil {
			if err := l.applyComponentProvider(paths, lock, pluginmanifestv1.KindIndexedDB, "indexeddb-"+name, def.Provider, def.Config, &def.Config, locked); err != nil {
				return err
			}
			cfg.IndexedDBs[name] = def
		}
	}
	if cfg.UI.Provider != nil {
		configMap, err := config.NodeToMap(cfg.UI.Config)
		if err != nil {
			return fmt.Errorf("decode ui provider config: %w", err)
		}
		switch {
		case cfg.UI.Provider.HasManagedArtifacts():
			if lock.UI == nil {
				return fmt.Errorf("prepared artifact for ui provider is missing or stale; run `gestaltd init --config %s`", paths.configPath)
			}
			fingerprint, err := UIProviderFingerprint(cfg.UI.Provider)
			if err != nil || lock.UI.Fingerprint != fingerprint {
				return fmt.Errorf("prepared artifact for ui provider is missing or stale; run `gestaltd init --config %s`", paths.configPath)
			}
			manifestPath := resolveLockPath(paths.artifactsDir, lock.UI.Manifest)
			assetRootPath := resolveLockPath(paths.artifactsDir, lock.UI.AssetRoot)
			needMaterialize := false
			if _, err := os.Stat(manifestPath); err != nil {
				needMaterialize = true
			}
			if !needMaterialize {
				if _, err := os.Stat(assetRootPath); err != nil {
					needMaterialize = true
				}
			}
			if needMaterialize {
				if err := l.materializeLockedUIProvider(context.Background(), paths, cfg.UI.Provider, *lock.UI, locked); err != nil {
					return err
				}
			}
			if _, err := os.Stat(manifestPath); err != nil {
				return fmt.Errorf("prepared manifest for ui provider not found at %s", manifestPath)
			}
			if _, err := os.Stat(assetRootPath); err != nil {
				return fmt.Errorf("prepared asset root for ui provider not found at %s", assetRootPath)
			}
			_, manifest, err := pluginpkg.ReadManifestFile(manifestPath)
			if err != nil {
				return fmt.Errorf("read prepared manifest for ui provider: %w", err)
			}
			if err := bindResolvedUIManifest(cfg.UI.Provider, manifestPath, manifest, configMap); err != nil {
				return err
			}
			cfg.UI.ResolvedAssetRoot = assetRootPath
		case cfg.UI.Provider.HasLocalSource():
			if err := applyLocalUIManifest(cfg.UI.Provider, configMap, &cfg.UI.ResolvedAssetRoot); err != nil {
				return err
			}
		}
	}

	return nil
}

func (l *Lifecycle) applyComponentProvider(paths initPaths, lock *Lockfile, kind, name string, provider *config.ProviderDef, providerConfig yaml.Node, targetNode *yaml.Node, locked bool) error {
	if provider == nil {
		return nil
	}
	configMap, err := config.NodeToMap(providerConfig)
	if err != nil {
		return fmt.Errorf("decode provider config for %s %q: %w", kind, name, err)
	}
	switch {
	case provider.HasManagedArtifacts():
		if lock == nil {
			return fmt.Errorf("prepared artifact for %s %q is missing or stale; run `gestaltd init --config %s`", kind, name, paths.configPath)
		}
		var entry *LockEntry
		switch name {
		case "auth":
			entry = lock.Auth
		case "secrets":
			entry = lock.Secrets
		case "telemetry":
			entry = lock.Telemetry
		case "audit":
			entry = lock.Audit
		default:
			if kind == pluginmanifestv1.KindIndexedDB && strings.HasPrefix(name, "indexeddb-") {
				entry = lock.Datastore
			}
		}
		if err := l.applyLockedComponentEntry(paths, entry, kind, name, provider, configMap, locked); err != nil {
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

func applyLocalProviderManifest(name string, plugin *config.ProviderDef, configMap map[string]any) error {
	if plugin == nil || !plugin.HasLocalSource() {
		return nil
	}

	manifestPath := plugin.SourcePath()
	if _, err := os.Stat(manifestPath); err != nil {
		return fmt.Errorf("manifest for provider %q not found at %s: %w", name, manifestPath, err)
	}

	_, manifest, err := pluginpkg.PrepareSourceManifest(manifestPath)
	if err != nil {
		return fmt.Errorf("prepare manifest for provider %q: %w", name, err)
	}
	if err := bindResolvedProviderManifest(name, plugin, manifestPath, manifest, configMap); err != nil {
		return err
	}
	if plugin.Command != "" {
		return nil
	}
	if entry := pluginpkg.EntrypointForKind(plugin.ResolvedManifest, pluginmanifestv1.KindPlugin); entry != nil {
		candidate := filepath.Join(filepath.Dir(manifestPath), filepath.FromSlash(entry.ArtifactPath))
		if _, err := os.Stat(candidate); err == nil {
			plugin.Command = candidate
			plugin.Args = append([]string(nil), entry.Args...)
		}
	}
	return nil
}

func applyLocalComponentManifest(kind, name string, plugin *config.ProviderDef, configMap map[string]any) error {
	if plugin == nil || !plugin.HasLocalSource() {
		return nil
	}

	manifestPath := plugin.SourcePath()
	if _, err := os.Stat(manifestPath); err != nil {
		return fmt.Errorf("manifest for %s %q not found at %s: %w", kind, name, manifestPath, err)
	}

	_, manifest, err := pluginpkg.ReadSourceManifestFile(manifestPath)
	if err != nil {
		return fmt.Errorf("prepare manifest for %s %q: %w", kind, name, err)
	}
	if err := bindResolvedComponentManifest(kind, name, plugin, manifestPath, manifest, configMap); err != nil {
		return err
	}
	if plugin.Command != "" {
		return nil
	}
	if entry := pluginpkg.EntrypointForKind(plugin.ResolvedManifest, kind); entry != nil {
		candidate := filepath.Join(filepath.Dir(manifestPath), filepath.FromSlash(entry.ArtifactPath))
		if _, err := os.Stat(candidate); err == nil {
			plugin.Command = candidate
			plugin.Args = append([]string(nil), entry.Args...)
		}
	}
	return nil
}

func applyLocalUIManifest(plugin *config.ProviderDef, configMap map[string]any, resolvedAssetRoot *string) error {
	if plugin == nil || !plugin.HasLocalSource() {
		return nil
	}

	manifestPath := plugin.SourcePath()
	if _, err := os.Stat(manifestPath); err != nil {
		return fmt.Errorf("manifest for ui provider not found at %s: %w", manifestPath, err)
	}

	_, manifest, err := pluginpkg.ReadSourceManifestFile(manifestPath)
	if err != nil {
		return fmt.Errorf("prepare manifest for ui provider: %w", err)
	}
	if err := bindResolvedUIManifest(plugin, manifestPath, manifest, configMap); err != nil {
		return err
	}
	assetRoot := filepath.Join(filepath.Dir(manifestPath), filepath.FromSlash(manifest.WebUI.AssetRoot))
	if _, err := os.Stat(assetRoot); err != nil {
		return fmt.Errorf("ui provider asset root not found at %s: %w", assetRoot, err)
	}
	*resolvedAssetRoot = assetRoot
	return nil
}

func (l *Lifecycle) applyLockedProviderEntry(paths initPaths, lock *Lockfile, name string, plugin *config.ProviderDef, configMap map[string]any, locked bool) error {
	entry, ok := lock.Providers[name]
	if !ok {
		return fmt.Errorf("prepared artifact for provider %q is missing or stale; run `gestaltd init --config %s`", name, paths.configPath)
	}
	fingerprint, err := PluginFingerprint(name, plugin, paths.configDir)
	if err != nil {
		return fmt.Errorf("fingerprinting provider %q plugin: %w", name, err)
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
	if needMaterialize {
		if err := l.materializeLockedProvider(context.Background(), paths, name, plugin, entry, locked); err != nil {
			return err
		}
	}
	if _, err := os.Stat(manifestPath); err != nil {
		return fmt.Errorf("prepared manifest for provider %q not found at %s; run `gestaltd init --config %s`", name, manifestPath, paths.configPath)
	}

	_, manifest, err := pluginpkg.ReadManifestFile(manifestPath)
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

func (l *Lifecycle) applyLockedComponentEntry(paths initPaths, entry *LockEntry, kind, name string, plugin *config.ProviderDef, configMap map[string]any, locked bool) error {
	if entry == nil {
		return fmt.Errorf("prepared artifact for %s %q is missing or stale; run `gestaltd init --config %s`", kind, name, paths.configPath)
	}
	fingerprint, err := PluginFingerprint(name, plugin, paths.configDir)
	if err != nil {
		return fmt.Errorf("fingerprinting %s %q plugin: %w", kind, name, err)
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
	if needMaterialize {
		if err := l.materializeLockedComponent(context.Background(), paths, kind, name, plugin, *entry, locked); err != nil {
			return err
		}
	}
	if _, err := os.Stat(manifestPath); err != nil {
		return fmt.Errorf("prepared manifest for %s %q not found at %s; run `gestaltd init --config %s`", kind, name, manifestPath, paths.configPath)
	}

	_, manifest, err := pluginpkg.ReadManifestFile(manifestPath)
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

func bindResolvedProviderManifest(name string, plugin *config.ProviderDef, manifestPath string, manifest *pluginmanifestv1.Manifest, configMap map[string]any) error {
	manifest = pluginpkg.ResolveManifestLocalReferences(manifest, manifestPath)
	if err := validateInstalledManifestKind(pluginmanifestv1.KindPlugin, name, manifest); err != nil {
		return err
	}
	if err := pluginpkg.ValidateConfigForManifest(manifestPath, manifest, pluginmanifestv1.KindPlugin, configMap); err != nil {
		return fmt.Errorf("plugin config validation for provider %q: %w", name, err)
	}
	resolvePluginIcon(manifest, manifestPath, plugin)
	plugin.ResolvedManifestPath = manifestPath
	plugin.ResolvedManifest = manifest
	return nil
}

func bindResolvedComponentManifest(kind, name string, plugin *config.ProviderDef, manifestPath string, manifest *pluginmanifestv1.Manifest, configMap map[string]any) error {
	manifest = pluginpkg.ResolveManifestLocalReferences(manifest, manifestPath)
	if err := validateInstalledManifestKind(kind, name, manifest); err != nil {
		return err
	}
	if err := pluginpkg.ValidateConfigForManifest(manifestPath, manifest, kind, configMap); err != nil {
		return fmt.Errorf("plugin config validation for %s %q: %w", kind, name, err)
	}
	resolvePluginIcon(manifest, manifestPath, plugin)
	plugin.ResolvedManifestPath = manifestPath
	plugin.ResolvedManifest = manifest
	return nil
}

func bindResolvedUIManifest(plugin *config.ProviderDef, manifestPath string, manifest *pluginmanifestv1.Manifest, configMap map[string]any) error {
	manifest = pluginpkg.ResolveManifestLocalReferences(manifest, manifestPath)
	if err := validateInstalledManifestKind(pluginmanifestv1.KindWebUI, "provider", manifest); err != nil {
		return err
	}
	if err := pluginpkg.ValidateConfigForManifest(manifestPath, manifest, pluginmanifestv1.KindWebUI, configMap); err != nil {
		return fmt.Errorf("plugin config validation for ui provider: %w", err)
	}
	resolvePluginIcon(manifest, manifestPath, plugin)
	plugin.ResolvedManifestPath = manifestPath
	plugin.ResolvedManifest = manifest
	return nil
}

func (l *Lifecycle) materializeLockedProvider(ctx context.Context, paths initPaths, name string, plugin *config.ProviderDef, entry LockProviderEntry, locked bool) error {
	platform := pluginpkg.CurrentPlatformString()
	archive, ok := resolveArchiveForPlatform(entry, platform)
	if !ok || archive.URL == "" {
		return fmt.Errorf("no archive for platform %s for provider %q; run `gestaltd init --config %s`", platform, name, paths.configPath)
	}
	if locked && archive.SHA256 == "" {
		return fmt.Errorf("no verified hash for platform %s for provider %q; run `gestaltd init --platform %s`", platform, name, platform)
	}

	src, parseErr := sourceForPlugin(plugin)
	if parseErr != nil {
		src, parseErr = pluginsource.Parse(entry.Source)
	}
	var (
		download *pluginpkg.DownloadResult
		err      error
	)
	if parseErr == nil && src.Host == pluginsource.HostGitHub {
		download, err = ghresolver.DownloadResolvedAsset(ctx, http.DefaultClient, archive.URL, src.Token)
	} else {
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, archive.URL, nil)
		if reqErr != nil {
			return fmt.Errorf("create locked source plugin request for provider %q: %w", name, reqErr)
		}
		download, err = pluginpkg.DownloadRequest(http.DefaultClient, req)
	}
	if err != nil {
		return fmt.Errorf("download locked source plugin for provider %q: %w", name, err)
	}
	defer download.Cleanup()
	if archive.SHA256 != "" && download.SHA256Hex != archive.SHA256 {
		return fmt.Errorf("locked source plugin digest mismatch for provider %q: got %s, want %s", name, download.SHA256Hex, archive.SHA256)
	}

	destDir := providerDestDir(paths, name)
	if err := os.RemoveAll(destDir); err != nil {
		return fmt.Errorf("remove stale plugin cache for provider %q: %w", name, err)
	}
	installed, err := pluginstore.Install(download.LocalPath, destDir)
	if err != nil {
		return fmt.Errorf("install locked source plugin for provider %q: %w", name, err)
	}
	if installed.Manifest.Source != entry.Source {
		return fmt.Errorf("locked source plugin manifest source mismatch for provider %q: got %q, want %q", name, installed.Manifest.Source, entry.Source)
	}
	if installed.Manifest.Version != entry.Version {
		return fmt.Errorf("locked source plugin manifest version mismatch for provider %q: got %q, want %q", name, installed.Manifest.Version, entry.Version)
	}
	return nil
}

func (l *Lifecycle) materializeLockedComponent(ctx context.Context, paths initPaths, kind, name string, plugin *config.ProviderDef, entry LockEntry, locked bool) error {
	platform := pluginpkg.CurrentPlatformString()
	archive, ok := resolveArchiveForPlatform(entry, platform)
	if !ok || archive.URL == "" {
		return fmt.Errorf("no archive for platform %s for %s %q; run `gestaltd init --config %s`", platform, kind, name, paths.configPath)
	}
	if locked && archive.SHA256 == "" {
		return fmt.Errorf("no verified hash for platform %s for %s %q; run `gestaltd init --platform %s`", platform, kind, name, platform)
	}

	src, parseErr := sourceForPlugin(plugin)
	if parseErr != nil {
		src, parseErr = pluginsource.Parse(entry.Source)
	}
	var (
		download *pluginpkg.DownloadResult
		err      error
	)
	if parseErr == nil && src.Host == pluginsource.HostGitHub {
		download, err = ghresolver.DownloadResolvedAsset(ctx, http.DefaultClient, archive.URL, src.Token)
	} else {
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, archive.URL, nil)
		if reqErr != nil {
			return fmt.Errorf("create locked source plugin request for %s %q: %w", kind, name, reqErr)
		}
		download, err = pluginpkg.DownloadRequest(http.DefaultClient, req)
	}
	if err != nil {
		return fmt.Errorf("download locked source plugin for %s %q: %w", kind, name, err)
	}
	defer download.Cleanup()
	if archive.SHA256 != "" && download.SHA256Hex != archive.SHA256 {
		return fmt.Errorf("locked source plugin digest mismatch for %s %q: got %s, want %s", kind, name, download.SHA256Hex, archive.SHA256)
	}

	var destDir string
	switch name {
	case "auth":
		destDir = authDestDir(paths)
	case "secrets":
		destDir = secretsDestDir(paths)
	case "telemetry":
		destDir = telemetryDestDir(paths)
	case "audit":
		destDir = auditDestDir(paths)
	default:
		if kind == pluginmanifestv1.KindIndexedDB && strings.HasPrefix(name, "indexeddb-") {
			destDir = indexeddbDestDir(paths, strings.TrimPrefix(name, "indexeddb-"))
			break
		}
		return fmt.Errorf("unsupported component %q", name)
	}
	if err := os.RemoveAll(destDir); err != nil {
		return fmt.Errorf("remove stale plugin cache for %s %q: %w", kind, name, err)
	}
	installed, err := pluginstore.Install(download.LocalPath, destDir)
	if err != nil {
		return fmt.Errorf("install locked source plugin for %s %q: %w", kind, name, err)
	}
	if err := validateInstalledManifestKind(kind, name, installed.Manifest); err != nil {
		return err
	}
	if installed.Manifest.Source != entry.Source {
		return fmt.Errorf("locked source plugin manifest source mismatch for %s %q: got %q, want %q", kind, name, installed.Manifest.Source, entry.Source)
	}
	if installed.Manifest.Version != entry.Version {
		return fmt.Errorf("locked source plugin manifest version mismatch for %s %q: got %q, want %q", kind, name, installed.Manifest.Version, entry.Version)
	}
	return nil
}

func (l *Lifecycle) materializeLockedUIProvider(ctx context.Context, paths initPaths, plugin *config.ProviderDef, entry LockUIEntry, locked bool) error {
	platform := pluginpkg.CurrentPlatformString()
	archive, ok := resolveArchiveForPlatform(entry, platform)
	if !ok || archive.URL == "" {
		return fmt.Errorf("no archive for platform %s for ui provider; run `gestaltd init --config %s`", platform, paths.configPath)
	}
	if locked && archive.SHA256 == "" {
		return fmt.Errorf("no verified hash for platform %s for ui provider; run `gestaltd init --platform %s`", platform, platform)
	}

	src, parseErr := sourceForPlugin(plugin)
	if parseErr != nil {
		src, parseErr = pluginsource.Parse(entry.Source)
	}
	var (
		download *pluginpkg.DownloadResult
		err      error
	)
	if parseErr == nil && src.Host == pluginsource.HostGitHub {
		download, err = ghresolver.DownloadResolvedAsset(ctx, http.DefaultClient, archive.URL, src.Token)
	} else {
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, archive.URL, nil)
		if reqErr != nil {
			return fmt.Errorf("create locked source request for ui provider: %w", reqErr)
		}
		download, err = pluginpkg.DownloadRequest(http.DefaultClient, req)
	}
	if err != nil {
		return fmt.Errorf("download locked source for ui provider: %w", err)
	}
	defer download.Cleanup()
	if archive.SHA256 != "" && download.SHA256Hex != archive.SHA256 {
		return fmt.Errorf("locked source digest mismatch for ui provider: got %s, want %s", download.SHA256Hex, archive.SHA256)
	}

	destDir := uiDestDir(paths)
	if err := os.RemoveAll(destDir); err != nil {
		return fmt.Errorf("remove stale cache for ui provider: %w", err)
	}
	installed, err := pluginstore.Install(download.LocalPath, destDir)
	if err != nil {
		return fmt.Errorf("install locked source for ui provider: %w", err)
	}
	if err := validateInstalledManifestKind(pluginmanifestv1.KindWebUI, "ui provider", installed.Manifest); err != nil {
		return err
	}
	return nil
}

func downloadPlatformArchives(ctx context.Context, lock *Lockfile, platforms []struct{ GOOS, GOARCH, LibC string }, tokenForSource map[string]string) error {
	for _, plat := range platforms {
		platformKey := pluginpkg.PlatformString(plat.GOOS, plat.GOARCH, plat.LibC)
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
	for _, entry := range []*LockEntry{lock.Auth, lock.Datastore, lock.Secrets, lock.UI} {
		if entry == nil {
			continue
		}
		if err := hashArchiveEntry(ctx, entry, platformKey, tokenForSource); err != nil {
			return err
		}
	}
	return nil
}

func hashArchiveEntry(ctx context.Context, entry *LockEntry, platformKey string, tokenForSource map[string]string) error {
	if entry.Archives == nil {
		return nil
	}
	archive, ok := entry.Archives[platformKey]
	if !ok || archive.URL == "" || archive.SHA256 != "" {
		return nil
	}
	token := tokenForSource[entry.Source]
	src, parseErr := pluginsource.Parse(entry.Source)
	var (
		dl  *pluginpkg.DownloadResult
		err error
	)
	if parseErr == nil && src.Host == pluginsource.HostGitHub {
		dl, err = ghresolver.DownloadResolvedAsset(ctx, http.DefaultClient, archive.URL, token)
	} else {
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, archive.URL, nil)
		if reqErr != nil {
			return fmt.Errorf("create request for platform %s, source %s: %w", platformKey, entry.Source, reqErr)
		}
		dl, err = pluginpkg.DownloadRequest(http.DefaultClient, req)
	}
	if err != nil {
		return fmt.Errorf("download archive for platform %s, source %s: %w", platformKey, entry.Source, err)
	}
	archive.SHA256 = dl.SHA256Hex
	dl.Cleanup()
	entry.Archives[platformKey] = archive
	return nil
}

func resolvePluginIcon(manifest *pluginmanifestv1.Manifest, manifestPath string, plugin *config.ProviderDef) {
	if manifest.IconFile == "" {
		return
	}
	iconPath := filepath.Join(filepath.Dir(manifestPath), filepath.FromSlash(manifest.IconFile))
	if _, err := os.Stat(iconPath); err != nil {
		slog.Warn("plugin icon_file not found", "path", iconPath, "error", err)
		return
	}
	plugin.ResolvedIconFile = iconPath
}

func providerEntrypointArgs(manifest *pluginmanifestv1.Manifest) ([]string, error) {
	entry := manifest.Entrypoints.Plugin
	if entry == nil {
		return nil, fmt.Errorf("manifest does not define a provider entrypoint")
	}
	return append([]string(nil), entry.Args...), nil
}

func componentEntrypointArgs(manifest *pluginmanifestv1.Manifest, kind string) ([]string, error) {
	entry := pluginpkg.EntrypointForKind(manifest, kind)
	if entry == nil {
		return nil, fmt.Errorf("manifest does not define a %s entrypoint", kind)
	}
	return append([]string(nil), entry.Args...), nil
}

func validateInstalledManifestKind(kind, name string, manifest *pluginmanifestv1.Manifest) error {
	if manifest == nil {
		return fmt.Errorf("manifest for %s %q is required", kind, name)
	}
	declared, err := pluginpkg.ManifestKind(manifest)
	if err != nil {
		return fmt.Errorf("%s %q manifest is invalid: %w", kind, name, err)
	}
	if declared != kind {
		return fmt.Errorf("%s %q manifest has kind %q, want %q", kind, name, declared, kind)
	}
	return nil
}

func buildComponentRuntimeConfigNode(name, kind string, provider *config.ProviderDef, providerConfig yaml.Node) (yaml.Node, error) {
	return config.BuildComponentRuntimeConfigNode(name, kind, provider, providerConfig)
}
