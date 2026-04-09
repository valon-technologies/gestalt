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
	PreparedDatastoreDir = ".gestaltd/datastore"
	PreparedUIDir = ".gestaltd/ui"
	LockVersion          = 1
)

type Lockfile struct {
	Version   int                          `json:"version"`
	Providers map[string]LockProviderEntry `json:"providers"`
	Auth      *LockEntry                   `json:"auth,omitempty"`
	Datastore *LockEntry                   `json:"datastore,omitempty"`
	UI *LockUIEntry `json:"ui,omitempty"`
}

type LockEntry struct {
	Fingerprint   string `json:"fingerprint"`
	Source        string `json:"source,omitempty"`
	Version       string `json:"version,omitempty"`
	ResolvedURL   string `json:"resolved_url,omitempty"`
	ArchiveSHA256 string `json:"archive_sha256,omitempty"`
	Manifest      string `json:"manifest"`
	Executable    string `json:"executable,omitempty"`
	AssetRoot     string `json:"asset_root,omitempty"`
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
	return l.InitAtPathWithArtifactsDir(configPath, "")
}

func (l *Lifecycle) InitAtPathWithArtifactsDir(configPath, artifactsDir string) (*Lockfile, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, fmt.Errorf("loading config: %v", err)
	}
	if err := config.OverlayManagedPluginConfig(configPath, cfg); err != nil {
		return nil, fmt.Errorf("loading config: %v", err)
	}
	if err := l.resolveConfigSecrets(context.Background(), cfg); err != nil {
		return nil, err
	}

	paths := initPathsForConfigWithArtifactsDir(configPath, resolveArtifactsDir(configPath, cfg, artifactsDir))
	if err := os.MkdirAll(paths.providersDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating providers dir: %w", err)
	}

	lock := &Lockfile{
		Version:   LockVersion,
		Providers: make(map[string]LockProviderEntry),
	}

	resolvedProviders, err := l.writeProviderArtifacts(context.Background(), cfg, paths)
	if err != nil {
		return nil, err
	}
	for name := range resolvedProviders {
		lock.Providers[name] = resolvedProviders[name]
	}
	if cfg.Auth.Provider != nil && cfg.Auth.Provider.HasManagedArtifacts() {
		entry, err := l.writeComponentArtifact(context.Background(), paths, pluginmanifestv1.KindAuth, "auth", authDestDir(paths), cfg.Auth.Provider, cfg.Auth.Config)
		if err != nil {
			return nil, err
		}
		lock.Auth = &entry
	}
	if cfg.Datastore.Provider != nil && cfg.Datastore.Provider.HasManagedArtifacts() {
		entry, err := l.writeComponentArtifact(context.Background(), paths, pluginmanifestv1.KindDatastore, "datastore", datastoreDestDir(paths), cfg.Datastore.Provider, cfg.Datastore.Config)
		if err != nil {
			return nil, err
		}
		lock.Datastore = &entry
	}
	if cfg.UI.Plugin.HasManagedArtifacts() {
		uiEntry, err := l.writeUIPluginArtifact(context.Background(), cfg, paths)
		if err != nil {
			return nil, err
		}
		lock.UI = &uiEntry
	}

	if err := WriteLockfile(paths.lockfilePath, lock); err != nil {
		return nil, err
	}
	if err := l.applyLockedPlugins(configPath, artifactsDir, cfg, true); err != nil {
		return nil, err
	}
	if err := config.ValidateResolvedStructure(cfg); err != nil {
		return nil, err
	}

	slog.Info("prepared locked artifacts", "providers", len(lock.Providers), "auth", lock.Auth != nil, "datastore", lock.Datastore != nil, "ui", lock.UI != nil)
	slog.Info("wrote lockfile", "path", paths.lockfilePath)
	return lock, nil
}

func (l *Lifecycle) LoadForExecutionAtPath(configPath string, locked bool) (*config.Config, map[string]string, error) {
	return l.LoadForExecutionAtPathWithArtifactsDir(configPath, "", locked)
}

func (l *Lifecycle) LoadForExecutionAtPathWithArtifactsDir(configPath, artifactsDir string, locked bool) (*config.Config, map[string]string, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, nil, fmt.Errorf("loading config: %v", err)
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

type initPaths struct {
	configPath   string
	configDir    string
	artifactsDir string
	lockfilePath string
	providersDir string
	authDir      string
	datastoreDir string
	uiDir string
}

type pluginFingerprintInput struct {
	Name    string `json:"name"`
	Source  string `json:"source,omitempty"`
	Version string `json:"version,omitempty"`
}

func configHasPluginLoading(cfg *config.Config) bool {
	for name := range cfg.Integrations {
		plugin := cfg.Integrations[name].Plugin
		if plugin.HasManagedArtifacts() || plugin.HasLocalSource() {
			return true
		}
	}
	if cfg.Auth.Provider != nil && (cfg.Auth.Provider.HasManagedArtifacts() || cfg.Auth.Provider.HasLocalSource()) {
		return true
	}
	if cfg.Datastore.Provider != nil && (cfg.Datastore.Provider.HasManagedArtifacts() || cfg.Datastore.Provider.HasLocalSource()) {
		return true
	}
	return cfg.UI.Plugin.HasManagedArtifacts()
}

func configHasManagedPlugins(cfg *config.Config) bool {
	for name := range cfg.Integrations {
		if cfg.Integrations[name].Plugin.HasManagedArtifacts() {
			return true
		}
	}
	if cfg.Auth.Provider != nil && cfg.Auth.Provider.HasManagedArtifacts() {
		return true
	}
	if cfg.Datastore.Provider != nil && cfg.Datastore.Provider.HasManagedArtifacts() {
		return true
	}
	return cfg.UI.Plugin.HasManagedArtifacts()
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
		datastoreDir: filepath.Join(artifactsDir, filepath.FromSlash(PreparedDatastoreDir)),
		uiDir: filepath.Join(artifactsDir, filepath.FromSlash(PreparedUIDir)),
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

func datastoreDestDir(paths initPaths) string {
	return paths.datastoreDir
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
		return nil, fmt.Errorf("unsupported lockfile version %d", lock.Version)
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
	for name := range cfg.Integrations {
		provider := cfg.Integrations[name]
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
	if cfg.Datastore.Provider != nil && cfg.Datastore.Provider.HasManagedArtifacts() {
		if lock.Datastore == nil || !lockEntryMatches(paths, "datastore", cfg.Datastore.Provider, *lock.Datastore, true) {
			return false
		}
	}
	if cfg.UI.Plugin.HasManagedArtifacts() {
		if lock.UI == nil {
			return false
		}
		fingerprint, err := UIPluginFingerprint(cfg.UI.Plugin)
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

func PluginFingerprint(name string, plugin *config.PluginDef, configDir string) (string, error) {
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

func UIPluginFingerprint(plugin *config.UIPluginDef) (string, error) {
	input := struct {
		Source  string `json:"source,omitempty"`
		Version string `json:"version,omitempty"`
	}{
		Source:  plugin.Source,
		Version: plugin.Version,
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func lockEntryMatches(paths initPaths, name string, plugin *config.PluginDef, entry LockEntry, found bool) bool {
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

func (l *Lifecycle) writeProviderArtifacts(ctx context.Context, cfg *config.Config, paths initPaths) (map[string]LockProviderEntry, error) {
	written := make(map[string]LockProviderEntry)
	for name := range cfg.Integrations {
		provider := cfg.Integrations[name]
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

func (l *Lifecycle) writeComponentArtifact(ctx context.Context, paths initPaths, kind, name, destDir string, plugin *config.PluginDef, configNode yaml.Node) (LockEntry, error) {
	configMap, err := config.NodeToMap(configNode)
	if err != nil {
		return LockEntry{}, fmt.Errorf("decode plugin config for %s %q: %w", kind, name, err)
	}
	return l.lockComponentEntryForSource(ctx, paths, kind, name, destDir, plugin, configMap)
}

func (l *Lifecycle) lockComponentEntryForSource(ctx context.Context, paths initPaths, kind, name, destDir string, plugin *config.PluginDef, configMap map[string]any) (LockEntry, error) {
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
	return LockEntry{
		Fingerprint:   fingerprint,
		Source:        plugin.SourceRef(),
		Version:       plugin.SourceVersion(),
		ResolvedURL:   resolved.ResolvedURL,
		ArchiveSHA256: resolved.ArchiveSHA256,
		Manifest:      filepath.ToSlash(manifestPath),
		Executable:    filepath.ToSlash(executablePath),
	}, nil
}

func (l *Lifecycle) lockProviderEntryForSource(ctx context.Context, paths initPaths, name string, plugin *config.PluginDef, configMap map[string]any) (LockProviderEntry, error) {
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
	return LockProviderEntry{
		Fingerprint:   fingerprint,
		Source:        plugin.SourceRef(),
		Version:       plugin.SourceVersion(),
		ResolvedURL:   resolved.ResolvedURL,
		ArchiveSHA256: resolved.ArchiveSHA256,
		Manifest:      filepath.ToSlash(manifestPath),
		Executable:    filepath.ToSlash(executableRel),
	}, nil
}

func (l *Lifecycle) writeUIPluginArtifact(ctx context.Context, cfg *config.Config, paths initPaths) (LockUIEntry, error) {
	plugin := cfg.UI.Plugin
	fingerprint, err := UIPluginFingerprint(plugin)
	if err != nil {
		return LockUIEntry{}, fmt.Errorf("fingerprinting ui plugin: %w", err)
	}

	destDir := uiDestDir(paths)
	var installed *pluginstore.InstalledPlugin
	if plugin.Source != "" {
		src, err := pluginsource.Parse(plugin.Source)
		if err != nil {
			return LockUIEntry{}, fmt.Errorf("ui plugin.source %q: %w", plugin.Source, err)
		}
		if l.sourceResolver == nil {
			return LockUIEntry{}, fmt.Errorf("ui plugin: source resolution requires a source resolver")
		}
		resolved, err := l.sourceResolver.Resolve(ctx, src, plugin.Version)
		if err != nil {
			return LockUIEntry{}, fmt.Errorf("ui plugin resolve source %q@%s: %w", plugin.Source, plugin.Version, err)
		}
		defer resolved.Cleanup()
		installed, err = pluginstore.Install(resolved.LocalPath, destDir)
		if err != nil {
			return LockUIEntry{}, fmt.Errorf("ui plugin install source: %w", err)
		}
		if installed.Manifest.Source != plugin.Source {
			return LockUIEntry{}, fmt.Errorf("ui plugin manifest source %q does not match config source %q", installed.Manifest.Source, plugin.Source)
		}
		if installed.Manifest.Version != plugin.Version {
			return LockUIEntry{}, fmt.Errorf("ui plugin manifest version %q does not match config version %q", installed.Manifest.Version, plugin.Version)
		}
		manifestPath, err := filepath.Rel(paths.artifactsDir, installed.ManifestPath)
		if err != nil {
			return LockUIEntry{}, fmt.Errorf("compute manifest path for ui plugin: %w", err)
		}
		assetRoot, err := filepath.Rel(paths.artifactsDir, installed.AssetRoot)
		if err != nil {
			return LockUIEntry{}, fmt.Errorf("compute asset root path for ui plugin: %w", err)
		}
		return LockUIEntry{
			Fingerprint:   fingerprint,
			Source:        plugin.Source,
			Version:       plugin.Version,
			ResolvedURL:   resolved.ResolvedURL,
			ArchiveSHA256: resolved.ArchiveSHA256,
			Manifest:      filepath.ToSlash(manifestPath),
			AssetRoot:     filepath.ToSlash(assetRoot),
		}, nil
	}

	return LockUIEntry{}, fmt.Errorf("ui plugin requires source")
}

func sourceForPlugin(plugin *config.PluginDef) (pluginsource.Source, error) {
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

	for name := range cfg.Integrations {
		provider := cfg.Integrations[name]
		if provider.Plugin == nil {
			continue
		}
		configMap, err := config.NodeToMap(provider.Plugin.Config)
		if err != nil {
			return fmt.Errorf("decode plugin config for provider %q: %w", name, err)
		}
		switch {
		case provider.Plugin.HasManagedArtifacts():
			if err := l.applyLockedProviderEntry(paths, lock, name, provider.Plugin, configMap); err != nil {
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
		cfg.Integrations[name] = provider
	}
	if cfg.Auth.Provider != nil {
		if err := l.applyComponentProvider(paths, lock, pluginmanifestv1.KindAuth, "auth", cfg.Auth.Provider, cfg.Auth.Config, &cfg.Auth.Config); err != nil {
			return err
		}
	}
	if cfg.Datastore.Provider != nil {
		if err := l.applyComponentProvider(paths, lock, pluginmanifestv1.KindDatastore, "datastore", cfg.Datastore.Provider, cfg.Datastore.Config, &cfg.Datastore.Config); err != nil {
			return err
		}
	}
	if cfg.UI.Plugin.HasManagedArtifacts() {
		if lock.UI == nil {
			return fmt.Errorf("prepared artifact for ui plugin is missing or stale; run `gestaltd init --config %s`", paths.configPath)
		}
		fingerprint, err := UIPluginFingerprint(cfg.UI.Plugin)
		if err != nil || lock.UI.Fingerprint != fingerprint {
			return fmt.Errorf("prepared artifact for ui plugin is missing or stale; run `gestaltd init --config %s`", paths.configPath)
		}
		manifestPath := resolveLockPath(paths.artifactsDir, lock.UI.Manifest)
		assetRootPath := resolveLockPath(paths.artifactsDir, lock.UI.AssetRoot)
		if _, err := os.Stat(manifestPath); err != nil {
			return fmt.Errorf("prepared manifest for ui plugin not found at %s", manifestPath)
		}
		if _, err := os.Stat(assetRootPath); err != nil {
			return fmt.Errorf("prepared asset root for ui plugin not found at %s", assetRootPath)
		}
		cfg.UI.Plugin.ResolvedAssetRoot = assetRootPath
	}

	return nil
}

func (l *Lifecycle) applyComponentProvider(paths initPaths, lock *Lockfile, kind, name string, provider *config.PluginDef, providerConfig yaml.Node, targetNode *yaml.Node) error {
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
		switch kind {
		case pluginmanifestv1.KindAuth:
			entry = lock.Auth
		case pluginmanifestv1.KindDatastore:
			entry = lock.Datastore
		}
		if err := l.applyLockedComponentEntry(paths, entry, kind, name, provider, configMap); err != nil {
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

func applyLocalProviderManifest(name string, plugin *config.PluginDef, configMap map[string]any) error {
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

func applyLocalComponentManifest(kind, name string, plugin *config.PluginDef, configMap map[string]any) error {
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

func (l *Lifecycle) applyLockedProviderEntry(paths initPaths, lock *Lockfile, name string, plugin *config.PluginDef, configMap map[string]any) error {
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
		if err := l.materializeLockedProvider(context.Background(), paths, name, plugin, entry); err != nil {
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

func (l *Lifecycle) applyLockedComponentEntry(paths initPaths, entry *LockEntry, kind, name string, plugin *config.PluginDef, configMap map[string]any) error {
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
		if err := l.materializeLockedComponent(context.Background(), paths, kind, name, plugin, *entry); err != nil {
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

func bindResolvedProviderManifest(name string, plugin *config.PluginDef, manifestPath string, manifest *pluginmanifestv1.Manifest, configMap map[string]any) error {
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

func bindResolvedComponentManifest(kind, name string, plugin *config.PluginDef, manifestPath string, manifest *pluginmanifestv1.Manifest, configMap map[string]any) error {
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

func (l *Lifecycle) materializeLockedProvider(ctx context.Context, paths initPaths, name string, plugin *config.PluginDef, entry LockProviderEntry) error {
	if entry.ResolvedURL == "" || entry.ArchiveSHA256 == "" {
		return fmt.Errorf("prepared artifact for provider %q is missing or stale; run `gestaltd init --config %s`", name, paths.configPath)
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
		download, err = ghresolver.DownloadResolvedAsset(ctx, http.DefaultClient, entry.ResolvedURL, src.Token)
	} else {
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, entry.ResolvedURL, nil)
		if reqErr != nil {
			return fmt.Errorf("create locked source plugin request for provider %q: %w", name, reqErr)
		}
		download, err = pluginpkg.DownloadRequest(http.DefaultClient, req)
	}
	if err != nil {
		return fmt.Errorf("download locked source plugin for provider %q: %w", name, err)
	}
	defer download.Cleanup()
	if download.SHA256Hex != entry.ArchiveSHA256 {
		return fmt.Errorf("locked source plugin digest mismatch for provider %q: got %s, want %s", name, download.SHA256Hex, entry.ArchiveSHA256)
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

func (l *Lifecycle) materializeLockedComponent(ctx context.Context, paths initPaths, kind, name string, plugin *config.PluginDef, entry LockEntry) error {
	if entry.ResolvedURL == "" || entry.ArchiveSHA256 == "" {
		return fmt.Errorf("prepared artifact for %s %q is missing or stale; run `gestaltd init --config %s`", kind, name, paths.configPath)
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
		download, err = ghresolver.DownloadResolvedAsset(ctx, http.DefaultClient, entry.ResolvedURL, src.Token)
	} else {
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, entry.ResolvedURL, nil)
		if reqErr != nil {
			return fmt.Errorf("create locked source plugin request for %s %q: %w", kind, name, reqErr)
		}
		download, err = pluginpkg.DownloadRequest(http.DefaultClient, req)
	}
	if err != nil {
		return fmt.Errorf("download locked source plugin for %s %q: %w", kind, name, err)
	}
	defer download.Cleanup()
	if download.SHA256Hex != entry.ArchiveSHA256 {
		return fmt.Errorf("locked source plugin digest mismatch for %s %q: got %s, want %s", kind, name, download.SHA256Hex, entry.ArchiveSHA256)
	}

	var destDir string
	switch kind {
	case pluginmanifestv1.KindAuth:
		destDir = authDestDir(paths)
	case pluginmanifestv1.KindDatastore:
		destDir = datastoreDestDir(paths)
	default:
		return fmt.Errorf("unsupported component kind %q", kind)
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

func resolvePluginIcon(manifest *pluginmanifestv1.Manifest, manifestPath string, plugin *config.PluginDef) {
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
	entry := manifest.Entrypoints.Provider
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

func buildComponentRuntimeConfigNode(name, kind string, provider *config.PluginDef, providerConfig yaml.Node) (yaml.Node, error) {
	return config.BuildComponentRuntimeConfigNode(name, kind, provider, providerConfig)
}
