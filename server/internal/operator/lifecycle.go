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
)

const (
	InitLockfileName     = "gestalt.lock.json"
	PreparedProvidersDir = ".gestaltd/providers"
	PluginsDir           = ".gestaltd/plugins"
	LockVersion          = 1
)

type Lockfile struct {
	Version   int                          `json:"version"`
	Providers map[string]LockProviderEntry `json:"providers"`
	Plugins   map[string]LockPluginEntry   `json:"plugins"`
}

type LockProviderEntry struct {
	Fingerprint string `json:"fingerprint"`
	Provider    string `json:"provider"`
}

type LockPluginEntry struct {
	Fingerprint   string `json:"fingerprint"`
	Package       string `json:"package,omitempty"`
	SourceDigest  string `json:"source_digest,omitempty"`
	Source        string `json:"source,omitempty"`
	Version       string `json:"version,omitempty"`
	ResolvedURL   string `json:"resolved_url,omitempty"`
	ArchiveSHA256 string `json:"archive_sha256,omitempty"`
	Manifest      string `json:"manifest"`
	Executable    string `json:"executable,omitempty"`
	AssetRoot     string `json:"asset_root,omitempty"`
}

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
		Plugins:   make(map[string]LockPluginEntry),
	}

	resolvedPlugins, err := l.writePluginArtifacts(context.Background(), cfg, paths)
	if err != nil {
		return nil, err
	}
	for key := range resolvedPlugins {
		lock.Plugins[key] = resolvedPlugins[key]
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

	slog.Info("prepared plugins", "count", len(lock.Plugins))
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
	pluginsDir   string
}

type pluginFingerprintInput struct {
	Name    string `json:"name"`
	Package string `json:"package,omitempty"`
	Source  string `json:"source,omitempty"`
	Version string `json:"version,omitempty"`
}

func configHasPlugins(cfg *config.Config) bool {
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
		pluginsDir:   filepath.Join(artifactsDir, filepath.FromSlash(PluginsDir)),
	}
}

func pluginDestDir(paths initPaths, kind, name string) string {
	return filepath.Join(paths.pluginsDir, kind+"_"+name)
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
	if lock.Plugins == nil {
		lock.Plugins = make(map[string]LockPluginEntry)
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
		intg := cfg.Integrations[name]
		if !intg.Plugin.HasManagedArtifacts() {
			continue
		}
		entry, found := lock.Plugins[LockPluginKey("integration", name)]
		if !pluginEntryMatches(paths, name, intg.Plugin, entry, found) {
			return false
		}
	}
	if cfg.UI.Plugin.HasManagedArtifacts() {
		key := LockPluginKey("ui", "default")
		entry, found := lock.Plugins[key]
		if !found {
			return false
		}
		fingerprint, err := UIPluginFingerprint(cfg.UI.Plugin)
		if err != nil || entry.Fingerprint != fingerprint {
			return false
		}
		manifestPath := resolveLockPath(paths.artifactsDir, entry.Manifest)
		if _, err := os.Stat(manifestPath); err != nil {
			return false
		}
		assetRootPath := resolveLockPath(paths.artifactsDir, entry.AssetRoot)
		if _, err := os.Stat(assetRootPath); err != nil {
			return false
		}
		if entry.SourceDigest != "" && cfg.UI.Plugin.Package != "" && !strings.HasPrefix(cfg.UI.Plugin.Package, "https://") {
			digest, err := sourceDigestForPackage(cfg.UI.Plugin.Package)
			if err != nil || digest != entry.SourceDigest {
				return false
			}
		}
	}
	return true
}

func relativePackagePath(configDir, pkg string) string {
	if pkg == "" || strings.HasPrefix(pkg, "https://") || strings.HasPrefix(pkg, "http://") {
		return pkg
	}
	if rel, err := filepath.Rel(configDir, pkg); err == nil {
		return filepath.ToSlash(rel)
	}
	return pkg
}

func PluginFingerprint(name string, plugin *config.PluginDef, configDir string) (string, error) {
	if plugin == nil {
		return "", nil
	}

	input := pluginFingerprintInput{
		Name:    name,
		Package: relativePackagePath(configDir, plugin.Package),
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

func UIPluginFingerprint(plugin *config.UIPluginDef) (string, error) {
	input := struct {
		Package string `json:"package,omitempty"`
		Source  string `json:"source,omitempty"`
		Version string `json:"version,omitempty"`
	}{
		Package: plugin.Package,
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

func LockPluginKey(kind, name string) string {
	return kind + ":" + name
}

func pluginEntryMatches(paths initPaths, name string, plugin *config.PluginDef, entry LockPluginEntry, found bool) bool {
	if !found {
		return false
	}
	fingerprint, err := PluginFingerprint(name, plugin, paths.configDir)
	if err != nil || entry.Fingerprint != fingerprint {
		return false
	}
	if entry.Source != "" {
		if entry.Source != plugin.Source || entry.Version != plugin.Version {
			return false
		}
	} else if entry.Package != relativePackagePath(paths.configDir, plugin.Package) {
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
	if entry.Source == "" && entry.SourceDigest != "" && !strings.HasPrefix(plugin.Package, "https://") {
		digest, err := sourceDigestForPackage(plugin.Package)
		if err != nil || digest != entry.SourceDigest {
			return false
		}
	}
	return true
}

func (l *Lifecycle) writePluginArtifacts(ctx context.Context, cfg *config.Config, paths initPaths) (map[string]LockPluginEntry, error) {
	written := make(map[string]LockPluginEntry)
	for name := range cfg.Integrations {
		intg := cfg.Integrations[name]
		if intg.Plugin == nil {
			continue
		}
		configMap, err := config.NodeToMap(intg.Plugin.Config)
		if err != nil {
			return nil, fmt.Errorf("decode plugin config for integration %q: %w", name, err)
		}
		var entry LockPluginEntry
		switch {
		case intg.Plugin.Source != "":
			entry, err = l.lockEntryForSource(ctx, paths, "integration", name, intg.Plugin, configMap)
		case intg.Plugin.Package != "":
			entry, err = lockEntryForPackage(ctx, paths, "integration", name, intg.Plugin, configMap)
		default:
			continue
		}
		if err != nil {
			return nil, err
		}
		written[LockPluginKey("integration", name)] = entry
	}
	if cfg.UI.Plugin.HasManagedArtifacts() {
		entry, err := l.writeUIPluginArtifact(ctx, cfg, paths)
		if err != nil {
			return nil, err
		}
		written[LockPluginKey("ui", "default")] = entry
	}

	return written, nil
}

func sourceDigestForPackage(packagePath string) (string, error) {
	info, err := os.Stat(packagePath)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		_, manifest, _, err := pluginpkg.LoadManifestFromPath(packagePath)
		if err != nil {
			return "", err
		}
		return pluginpkg.DirectoryDigest(packagePath, manifest)
	}
	return pluginpkg.ArchiveDigest(packagePath)
}

func lockEntryForPackage(ctx context.Context, paths initPaths, kind, name string, plugin *config.PluginDef, configMap map[string]any) (LockPluginEntry, error) {
	packagePath := plugin.Package
	isURL := strings.HasPrefix(packagePath, "https://")

	var sourceDigest string
	if isURL {
		tmpPath, cleanup, err := pluginpkg.FetchPackage(ctx, packagePath)
		if err != nil {
			return LockPluginEntry{}, fmt.Errorf("%s %q plugin.package %q: %w", kind, name, packagePath, err)
		}
		defer cleanup()
		packagePath = tmpPath
	}

	info, err := os.Stat(packagePath)
	if err != nil {
		return LockPluginEntry{}, fmt.Errorf("%s %q plugin.package %q: %w", kind, name, plugin.Package, err)
	}

	destDir := pluginDestDir(paths, kind, name)
	var installed *pluginstore.InstalledPlugin
	if info.IsDir() {
		installed, err = pluginstore.InstallFromDir(packagePath, destDir)
	} else {
		installed, err = pluginstore.Install(packagePath, destDir)
	}
	if err != nil {
		return LockPluginEntry{}, fmt.Errorf("%s %q plugin.package %q: %w", kind, name, plugin.Package, err)
	}

	if !isURL {
		sourceDigest, err = sourceDigestForPackage(packagePath)
		if err != nil {
			return LockPluginEntry{}, fmt.Errorf("%s %q source digest: %w", kind, name, err)
		}
	}

	if err := pluginpkg.ValidateConfigForManifest(installed.ManifestPath, installed.Manifest, manifestKind(kind), configMap); err != nil {
		return LockPluginEntry{}, fmt.Errorf("plugin config validation for %s %q: %w", kind, name, err)
	}
	fingerprint, err := PluginFingerprint(name, plugin, paths.configDir)
	if err != nil {
		return LockPluginEntry{}, fmt.Errorf("fingerprinting %s %q plugin: %w", kind, name, err)
	}
	manifestPath, err := filepath.Rel(paths.artifactsDir, installed.ManifestPath)
	if err != nil {
		return LockPluginEntry{}, fmt.Errorf("compute manifest path for %s %q: %w", kind, name, err)
	}
	var executableRel string
	if installed.ExecutablePath != "" {
		executablePath, err := filepath.Rel(paths.artifactsDir, installed.ExecutablePath)
		if err != nil {
			return LockPluginEntry{}, fmt.Errorf("compute executable path for %s %q: %w", kind, name, err)
		}
		executableRel = filepath.ToSlash(executablePath)
	}
	return LockPluginEntry{
		Fingerprint:  fingerprint,
		Package:      relativePackagePath(paths.configDir, plugin.Package),
		SourceDigest: sourceDigest,
		Manifest:     filepath.ToSlash(manifestPath),
		Executable:   executableRel,
	}, nil
}

func (l *Lifecycle) lockEntryForSource(ctx context.Context, paths initPaths, kind, name string, plugin *config.PluginDef, configMap map[string]any) (LockPluginEntry, error) {
	src, err := pluginsource.Parse(plugin.Source)
	if err != nil {
		return LockPluginEntry{}, fmt.Errorf("%s %q plugin.source %q: %w", kind, name, plugin.Source, err)
	}
	if l.sourceResolver == nil {
		return LockPluginEntry{}, fmt.Errorf("%s %q: source plugin resolution requires a source resolver", kind, name)
	}
	resolved, err := l.sourceResolver.Resolve(ctx, src, plugin.Version)
	if err != nil {
		return LockPluginEntry{}, fmt.Errorf("%s %q resolve source %q@%s: %w", kind, name, plugin.Source, plugin.Version, err)
	}
	defer resolved.Cleanup()

	destDir := pluginDestDir(paths, kind, name)
	installed, err := pluginstore.Install(resolved.LocalPath, destDir)
	if err != nil {
		return LockPluginEntry{}, fmt.Errorf("%s %q install source plugin: %w", kind, name, err)
	}

	if installed.Manifest.Source != plugin.Source {
		return LockPluginEntry{}, fmt.Errorf("%s %q: manifest source %q does not match config source %q", kind, name, installed.Manifest.Source, plugin.Source)
	}
	if installed.Manifest.Version != plugin.Version {
		return LockPluginEntry{}, fmt.Errorf("%s %q: manifest version %q does not match config version %q", kind, name, installed.Manifest.Version, plugin.Version)
	}

	if err := pluginpkg.ValidateConfigForManifest(installed.ManifestPath, installed.Manifest, manifestKind(kind), configMap); err != nil {
		return LockPluginEntry{}, fmt.Errorf("plugin config validation for %s %q: %w", kind, name, err)
	}
	fingerprint, err := PluginFingerprint(name, plugin, paths.configDir)
	if err != nil {
		return LockPluginEntry{}, fmt.Errorf("fingerprinting %s %q plugin: %w", kind, name, err)
	}
	manifestPath, err := filepath.Rel(paths.artifactsDir, installed.ManifestPath)
	if err != nil {
		return LockPluginEntry{}, fmt.Errorf("compute manifest path for %s %q: %w", kind, name, err)
	}
	var executableRel string
	if installed.ExecutablePath != "" {
		ep, err := filepath.Rel(paths.artifactsDir, installed.ExecutablePath)
		if err != nil {
			return LockPluginEntry{}, fmt.Errorf("compute executable path for %s %q: %w", kind, name, err)
		}
		executableRel = filepath.ToSlash(ep)
	}
	return LockPluginEntry{
		Fingerprint:   fingerprint,
		Source:        plugin.Source,
		Version:       plugin.Version,
		ResolvedURL:   resolved.ResolvedURL,
		ArchiveSHA256: resolved.ArchiveSHA256,
		Manifest:      filepath.ToSlash(manifestPath),
		Executable:    executableRel,
	}, nil
}

func (l *Lifecycle) writeUIPluginArtifact(ctx context.Context, cfg *config.Config, paths initPaths) (LockPluginEntry, error) {
	plugin := cfg.UI.Plugin
	fingerprint, err := UIPluginFingerprint(plugin)
	if err != nil {
		return LockPluginEntry{}, fmt.Errorf("fingerprinting ui plugin: %w", err)
	}

	destDir := pluginDestDir(paths, "ui", "default")
	var installed *pluginstore.InstalledPlugin
	var sourceDigest string
	switch {
	case plugin.Source != "":
		src, err := pluginsource.Parse(plugin.Source)
		if err != nil {
			return LockPluginEntry{}, fmt.Errorf("ui plugin.source %q: %w", plugin.Source, err)
		}
		if l.sourceResolver == nil {
			return LockPluginEntry{}, fmt.Errorf("ui plugin: source resolution requires a source resolver")
		}
		resolved, err := l.sourceResolver.Resolve(ctx, src, plugin.Version)
		if err != nil {
			return LockPluginEntry{}, fmt.Errorf("ui plugin resolve source %q@%s: %w", plugin.Source, plugin.Version, err)
		}
		defer resolved.Cleanup()
		installed, err = pluginstore.Install(resolved.LocalPath, destDir)
		if err != nil {
			return LockPluginEntry{}, fmt.Errorf("ui plugin install source: %w", err)
		}
		if installed.Manifest.Source != plugin.Source {
			return LockPluginEntry{}, fmt.Errorf("ui plugin manifest source %q does not match config source %q", installed.Manifest.Source, plugin.Source)
		}
		if installed.Manifest.Version != plugin.Version {
			return LockPluginEntry{}, fmt.Errorf("ui plugin manifest version %q does not match config version %q", installed.Manifest.Version, plugin.Version)
		}
		manifestPath, err := filepath.Rel(paths.artifactsDir, installed.ManifestPath)
		if err != nil {
			return LockPluginEntry{}, fmt.Errorf("compute manifest path for ui plugin: %w", err)
		}
		assetRoot, err := filepath.Rel(paths.artifactsDir, installed.AssetRoot)
		if err != nil {
			return LockPluginEntry{}, fmt.Errorf("compute asset root path for ui plugin: %w", err)
		}
		return LockPluginEntry{
			Fingerprint:   fingerprint,
			Source:        plugin.Source,
			Version:       plugin.Version,
			ResolvedURL:   resolved.ResolvedURL,
			ArchiveSHA256: resolved.ArchiveSHA256,
			Manifest:      filepath.ToSlash(manifestPath),
			AssetRoot:     filepath.ToSlash(assetRoot),
		}, nil

	case plugin.Package != "":
		packagePath := plugin.Package
		isURL := strings.HasPrefix(packagePath, "https://")
		if isURL {
			tmpPath, cleanup, err := pluginpkg.FetchPackage(ctx, packagePath)
			if err != nil {
				return LockPluginEntry{}, fmt.Errorf("ui plugin.package %q: %w", packagePath, err)
			}
			defer cleanup()
			packagePath = tmpPath
		}
		info, err := os.Stat(packagePath)
		if err != nil {
			return LockPluginEntry{}, fmt.Errorf("ui plugin.package %q: %w", plugin.Package, err)
		}
		if info.IsDir() {
			installed, err = pluginstore.InstallFromDir(packagePath, destDir)
		} else {
			installed, err = pluginstore.Install(packagePath, destDir)
		}
		if err != nil {
			return LockPluginEntry{}, fmt.Errorf("ui plugin.package %q: %w", plugin.Package, err)
		}
		if !isURL {
			sourceDigest, err = sourceDigestForPackage(packagePath)
			if err != nil {
				return LockPluginEntry{}, fmt.Errorf("ui plugin source digest: %w", err)
			}
		}
		manifestPath, err := filepath.Rel(paths.artifactsDir, installed.ManifestPath)
		if err != nil {
			return LockPluginEntry{}, fmt.Errorf("compute manifest path for ui plugin: %w", err)
		}
		assetRoot, err := filepath.Rel(paths.artifactsDir, installed.AssetRoot)
		if err != nil {
			return LockPluginEntry{}, fmt.Errorf("compute asset root path for ui plugin: %w", err)
		}
		return LockPluginEntry{
			Fingerprint:  fingerprint,
			Package:      relativePackagePath(paths.configDir, plugin.Package),
			SourceDigest: sourceDigest,
			Manifest:     filepath.ToSlash(manifestPath),
			AssetRoot:    filepath.ToSlash(assetRoot),
		}, nil
	}

	return LockPluginEntry{}, fmt.Errorf("ui plugin requires package or source")
}

func (l *Lifecycle) applyLockedPlugins(configPath, artifactsDir string, cfg *config.Config, locked bool) error {
	if !configHasPlugins(cfg) {
		return nil
	}

	paths := initPathsForConfigWithArtifactsDir(configPath, resolveArtifactsDir(configPath, cfg, artifactsDir))
	lock, err := ReadLockfile(paths.lockfilePath)
	if !locked && (err != nil || !lockMatchesConfig(cfg, paths, lock)) {
		lock, err = l.InitAtPathWithArtifactsDir(configPath, artifactsDir)
	}
	if err != nil {
		return fmt.Errorf("plugin packages require prepared artifacts; run `gestaltd init --config %s`: %w", configPath, err)
	}

	for name := range cfg.Integrations {
		intg := cfg.Integrations[name]
		if !intg.Plugin.HasManagedArtifacts() {
			continue
		}
		configMap, err := config.NodeToMap(intg.Plugin.Config)
		if err != nil {
			return fmt.Errorf("decode plugin config for integration %q: %w", name, err)
		}
		if err := l.applyLockedPluginEntry(paths, lock, "integration", name, intg.Plugin, configMap); err != nil {
			return err
		}
		if manifest := intg.Plugin.ResolvedManifest; manifest != nil {
			intg.DisplayName = cmp.Or(intg.DisplayName, manifest.DisplayName)
			intg.Description = cmp.Or(intg.Description, manifest.Description)
		}
		intg.IconFile = cmp.Or(intg.IconFile, intg.Plugin.ResolvedIconFile)
		cfg.Integrations[name] = intg
	}
	if cfg.UI.Plugin.HasManagedArtifacts() {
		key := LockPluginKey("ui", "default")
		entry, ok := lock.Plugins[key]
		if !ok {
			return fmt.Errorf("prepared artifact for ui plugin is missing or stale; run `gestaltd init --config %s`", paths.configPath)
		}
		fingerprint, err := UIPluginFingerprint(cfg.UI.Plugin)
		if err != nil || entry.Fingerprint != fingerprint {
			return fmt.Errorf("prepared artifact for ui plugin is missing or stale; run `gestaltd init --config %s`", paths.configPath)
		}
		manifestPath := resolveLockPath(paths.artifactsDir, entry.Manifest)
		assetRootPath := resolveLockPath(paths.artifactsDir, entry.AssetRoot)
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

func (l *Lifecycle) applyLockedPluginEntry(paths initPaths, lock *Lockfile, kind, name string, plugin *config.PluginDef, configMap map[string]any) error {
	key := LockPluginKey(kind, name)
	entry, ok := lock.Plugins[key]
	if !ok {
		return fmt.Errorf("prepared artifact for %s %q is missing or stale; run `gestaltd init --config %s`", kind, name, paths.configPath)
	}
	fingerprint, err := PluginFingerprint(name, plugin, paths.configDir)
	if err != nil {
		return fmt.Errorf("fingerprinting %s %q plugin: %w", kind, name, err)
	}
	if entry.Source != "" {
		if entry.Fingerprint != fingerprint || entry.Source != plugin.Source || entry.Version != plugin.Version {
			return fmt.Errorf("prepared artifact for %s %q is missing or stale; run `gestaltd init --config %s`", kind, name, paths.configPath)
		}
	} else if entry.Fingerprint != fingerprint || entry.Package != relativePackagePath(paths.configDir, plugin.Package) {
		return fmt.Errorf("prepared artifact for %s %q is missing or stale; run `gestaltd init --config %s`", kind, name, paths.configPath)
	}

	manifestPath := resolveLockPath(paths.artifactsDir, entry.Manifest)
	executablePath := resolveLockPath(paths.artifactsDir, entry.Executable)
	needMaterialize := false
	if entry.Source != "" {
		if _, err := os.Stat(manifestPath); err != nil {
			needMaterialize = true
		}
		if !needMaterialize && entry.Executable != "" {
			if _, err := os.Stat(executablePath); err != nil {
				needMaterialize = true
			}
		}
		if needMaterialize {
			if err := l.materializeLockedSourcePlugin(context.Background(), paths, kind, name, entry); err != nil {
				return err
			}
		}
	}
	if _, err := os.Stat(manifestPath); err != nil {
		return fmt.Errorf("prepared manifest for %s %q not found at %s; run `gestaltd init --config %s`", kind, name, manifestPath, paths.configPath)
	}

	_, manifest, err := pluginpkg.ReadManifestFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read prepared manifest for %s %q: %w", kind, name, err)
	}
	manifest = pluginpkg.ResolveManifestLocalReferences(manifest, manifestPath)
	if err := pluginpkg.ValidateConfigForManifest(manifestPath, manifest, manifestKind(kind), configMap); err != nil {
		return fmt.Errorf("plugin config validation for %s %q: %w", kind, name, err)
	}

	resolvePluginIcon(manifest, manifestPath, plugin)

	plugin.ResolvedManifestPath = manifestPath
	plugin.ResolvedManifest = manifest
	if kind == "integration" && manifest.IsDeclarativeOnlyProvider() {
		plugin.IsDeclarative = true
		return nil
	}

	if _, err := os.Stat(executablePath); err != nil {
		return fmt.Errorf("prepared executable for %s %q not found at %s; run `gestaltd init --config %s`", kind, name, executablePath, paths.configPath)
	}

	args, err := entrypointArgs(kind, manifest)
	if err != nil {
		return fmt.Errorf("resolve entrypoint for %s %q: %w", kind, name, err)
	}

	plugin.Command = executablePath
	plugin.Args = append([]string(nil), args...)
	return nil
}

func (l *Lifecycle) materializeLockedSourcePlugin(ctx context.Context, paths initPaths, kind, name string, entry LockPluginEntry) error {
	if entry.ResolvedURL == "" || entry.ArchiveSHA256 == "" {
		return fmt.Errorf("prepared artifact for %s %q is missing or stale; run `gestaltd init --config %s`", kind, name, paths.configPath)
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

	destDir := pluginDestDir(paths, kind, name)
	if err := os.RemoveAll(destDir); err != nil {
		return fmt.Errorf("remove stale plugin cache for %s %q: %w", kind, name, err)
	}
	installed, err := pluginstore.Install(download.LocalPath, destDir)
	if err != nil {
		return fmt.Errorf("install locked source plugin for %s %q: %w", kind, name, err)
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

func entrypointArgs(kind string, manifest *pluginmanifestv1.Manifest) ([]string, error) {
	var entry *pluginmanifestv1.Entrypoint
	switch kind {
	case "integration":
		entry = manifest.Entrypoints.Provider
	default:
		return nil, fmt.Errorf("unknown plugin kind %q", kind)
	}
	if entry == nil {
		return nil, fmt.Errorf("manifest does not define an entrypoint for %s", kind)
	}
	return append([]string(nil), entry.Args...), nil
}

func manifestKind(kind string) string {
	switch kind {
	case "integration":
		return pluginmanifestv1.KindProvider
	default:
		return ""
	}
}
