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
)

const (
	InitLockfileName     = "gestalt.lock.json"
	PreparedProvidersDir = ".gestaltd/providers"
	PreparedUIDir        = ".gestaltd/ui"
	LockVersion          = 2
)

type Lockfile struct {
	Version   int                          `json:"version"`
	Providers map[string]LockProviderEntry `json:"providers"`
	UI        *LockUIEntry                 `json:"ui,omitempty"`
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
	sourceResolver pluginsource.Resolver
}

func NewLifecycle(sourceResolver pluginsource.Resolver) *Lifecycle {
	return &Lifecycle{sourceResolver: sourceResolver}
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

	slog.Info("prepared locked artifacts", "providers", len(lock.Providers), "ui", lock.UI != nil)
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

type initPaths struct {
	configPath   string
	configDir    string
	artifactsDir string
	lockfilePath string
	providersDir string
	uiDir        string
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
	return cfg.UI.Plugin.HasManagedArtifacts()
}

func configHasManagedPlugins(cfg *config.Config) bool {
	for name := range cfg.Integrations {
		if cfg.Integrations[name].Plugin.HasManagedArtifacts() {
			return true
		}
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
		uiDir:        filepath.Join(artifactsDir, filepath.FromSlash(PreparedUIDir)),
	}
}

func providerDestDir(paths initPaths, name string) string {
	return filepath.Join(paths.providersDir, name)
}

func uiDestDir(paths initPaths) string {
	return paths.uiDir
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
		if !providerEntryMatches(paths, name, provider.Plugin, entry, found) {
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

func providerEntryMatches(paths initPaths, name string, plugin *config.PluginDef, entry LockProviderEntry, found bool) bool {
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

func (l *Lifecycle) lockProviderEntryForSource(ctx context.Context, paths initPaths, name string, plugin *config.PluginDef, configMap map[string]any) (LockProviderEntry, error) {
	src, err := pluginsource.Parse(plugin.SourceRef())
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

	if installed.Manifest.Source != plugin.SourceRef() {
		return LockProviderEntry{}, fmt.Errorf("provider %q: manifest source %q does not match config source %q", name, installed.Manifest.Source, plugin.SourceRef())
	}
	if installed.Manifest.Version != plugin.SourceVersion() {
		return LockProviderEntry{}, fmt.Errorf("provider %q: manifest version %q does not match config version %q", name, installed.Manifest.Version, plugin.SourceVersion())
	}

	if err := pluginpkg.ValidateConfigForManifest(installed.ManifestPath, installed.Manifest, pluginmanifestv1.KindProvider, configMap); err != nil {
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
	var executableRel string
	if installed.ExecutablePath != "" {
		ep, err := filepath.Rel(paths.artifactsDir, installed.ExecutablePath)
		if err != nil {
			return LockProviderEntry{}, fmt.Errorf("compute executable path for provider %q: %w", name, err)
		}
		executableRel = filepath.ToSlash(ep)
	}
	return LockProviderEntry{
		Fingerprint:   fingerprint,
		Source:        plugin.SourceRef(),
		Version:       plugin.SourceVersion(),
		ResolvedURL:   resolved.ResolvedURL,
		ArchiveSHA256: resolved.ArchiveSHA256,
		Manifest:      filepath.ToSlash(manifestPath),
		Executable:    executableRel,
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
	return bindResolvedProviderManifest(name, plugin, manifestPath, manifest, configMap)
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
		if err := l.materializeLockedProvider(context.Background(), paths, name, entry); err != nil {
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
	if plugin.IsDeclarative {
		return nil
	}

	if _, err := os.Stat(executablePath); err != nil {
		return fmt.Errorf("prepared executable for provider %q not found at %s; run `gestaltd init --config %s`", name, executablePath, paths.configPath)
	}

	args, err := providerEntrypointArgs(manifest)
	if err != nil {
		return fmt.Errorf("resolve entrypoint for provider %q: %w", name, err)
	}

	plugin.Command = executablePath
	plugin.Args = append([]string(nil), args...)
	return nil
}

func bindResolvedProviderManifest(name string, plugin *config.PluginDef, manifestPath string, manifest *pluginmanifestv1.Manifest, configMap map[string]any) error {
	manifest = pluginpkg.ResolveManifestLocalReferences(manifest, manifestPath)
	if err := pluginpkg.ValidateConfigForManifest(manifestPath, manifest, pluginmanifestv1.KindProvider, configMap); err != nil {
		return fmt.Errorf("plugin config validation for provider %q: %w", name, err)
	}
	resolvePluginIcon(manifest, manifestPath, plugin)
	plugin.ResolvedManifestPath = manifestPath
	plugin.ResolvedManifest = manifest
	isDeclarative := manifest.IsDeclarativeOnlyProvider()
	if isDeclarative && plugin != nil && plugin.HasLocalSource() {
		if plugin.Command != "" {
			isDeclarative = false
		} else {
			hasProviderPackage, err := pluginpkg.HasGoProviderPackage(filepath.Dir(manifestPath))
			if err != nil {
				return fmt.Errorf("detect local source executable for provider %q: %w", name, err)
			}
			isDeclarative = !hasProviderPackage
		}
	}
	plugin.IsDeclarative = isDeclarative
	return nil
}

func (l *Lifecycle) materializeLockedProvider(ctx context.Context, paths initPaths, name string, entry LockProviderEntry) error {
	if entry.ResolvedURL == "" || entry.ArchiveSHA256 == "" {
		return fmt.Errorf("prepared artifact for provider %q is missing or stale; run `gestaltd init --config %s`", name, paths.configPath)
	}

	src, parseErr := pluginsource.Parse(entry.Source)
	var (
		download *pluginpkg.DownloadResult
		err      error
	)
	if parseErr == nil && src.Host == pluginsource.HostGitHub {
		download, err = ghresolver.DownloadResolvedAsset(ctx, http.DefaultClient, entry.ResolvedURL, "")
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
