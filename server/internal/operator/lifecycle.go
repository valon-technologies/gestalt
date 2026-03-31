package operator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/pluginpkg"
	"github.com/valon-technologies/gestalt/server/internal/pluginsource"
	"github.com/valon-technologies/gestalt/server/internal/pluginstore"
	"github.com/valon-technologies/gestalt/server/internal/provider"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

const (
	InitLockfileName     = "gestalt.lock.json"
	PreparedProvidersDir = ".gestalt/providers"
	LockVersion          = 4
	LockVersionCompat    = 3
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

type UpstreamLoader func(ctx context.Context, name string, api config.APIDef) (*provider.Definition, error)

type Lifecycle struct {
	loadAPIUpstream UpstreamLoader
	sourceResolver  pluginsource.Resolver
}

func NewLifecycle(loadAPIUpstream UpstreamLoader, sourceResolver pluginsource.Resolver) *Lifecycle {
	return &Lifecycle{loadAPIUpstream: loadAPIUpstream, sourceResolver: sourceResolver}
}

func (l *Lifecycle) InitAtPath(configPath string) (*Lockfile, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, fmt.Errorf("loading config: %v", err)
	}

	paths := initPathsForConfig(configPath)
	if err := os.MkdirAll(paths.providersDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating providers dir: %w", err)
	}

	lock := &Lockfile{
		Version:   LockVersion,
		Providers: make(map[string]LockProviderEntry),
		Plugins:   make(map[string]LockPluginEntry),
	}

	written, err := l.writeProviderArtifacts(context.Background(), cfg, paths)
	if err != nil {
		return nil, err
	}
	for name, entry := range written {
		lock.Providers[name] = entry
	}

	resolvedPlugins, err := l.writePluginArtifacts(context.Background(), configPath, cfg, paths)
	if err != nil {
		return nil, err
	}
	for key := range resolvedPlugins {
		lock.Plugins[key] = resolvedPlugins[key]
	}

	if err := WriteLockfile(paths.lockfilePath, lock); err != nil {
		return nil, err
	}

	slog.Info("prepared providers", "count", len(lock.Providers))
	slog.Info("wrote lockfile", "path", paths.lockfilePath)
	return lock, nil
}

func (l *Lifecycle) LoadForExecutionAtPath(configPath string, locked bool) (*config.Config, map[string]string, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, nil, fmt.Errorf("loading config: %v", err)
	}
	if err := config.ValidateRuntime(cfg); err != nil {
		return nil, nil, err
	}

	preparedProviders, err := l.providersForConfig(configPath, cfg, locked)
	if err != nil {
		return nil, nil, err
	}
	if err := l.applyLockedPlugins(configPath, cfg, locked); err != nil {
		return nil, nil, err
	}

	return cfg, preparedProviders, nil
}

type initPaths struct {
	configPath   string
	configDir    string
	lockfilePath string
	providersDir string
}

type providerFingerprintInput struct {
	Type          string `json:"type"`
	URL           string `json:"url"`
	DisplayName   string `json:"display_name,omitempty"`
	Description   string `json:"description,omitempty"`
	HasIcon       bool   `json:"has_icon,omitempty"`
	IconSHA256    string `json:"icon_sha256,omitempty"`
	IconReadError string `json:"icon_read_error,omitempty"`
}

type pluginFingerprintInput struct {
	Name    string            `json:"name"`
	Command string            `json:"command,omitempty"`
	Package string            `json:"package,omitempty"`
	Source  string            `json:"source,omitempty"`
	Version string            `json:"version,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Config  map[string]any    `json:"config,omitempty"`
}

func (l *Lifecycle) providersForConfig(configPath string, cfg *config.Config, locked bool) (map[string]string, error) {
	if !configHasRemoteAPIUpstreams(cfg) {
		return nil, nil
	}

	paths := initPathsForConfig(configPath)
	lock, err := ReadLockfile(paths.lockfilePath)
	if !locked && (err != nil || !lockMatchesConfig(cfg, paths, lock)) {
		lock, err = l.InitAtPath(configPath)
	}
	if err != nil {
		if locked {
			return nil, fmt.Errorf("remote REST/GraphQL upstreams require prepared artifacts; run `gestaltd bundle --config %s --output DIR`: %w", configPath, err)
		}
		return nil, nil
	}

	preparedProviders := make(map[string]string)
	for name := range cfg.Integrations {
		intg := cfg.Integrations[name]
		upstream, hasRemote := remoteAPIUpstreamForPrepare(intg)
		if !hasRemote {
			continue
		}

		fingerprint, err := integrationFingerprint(name, intg, upstream)
		if err != nil {
			return nil, fmt.Errorf("fingerprinting integration %q: %w", name, err)
		}

		entry, ok := lock.Providers[name]
		if !ok || entry.Fingerprint != fingerprint {
			if locked {
				return nil, fmt.Errorf("prepared artifact for integration %q is missing or stale; run `gestaltd bundle --config %s --output DIR`", name, configPath)
			}
			continue
		}

		absPath := resolveLockPath(paths.configDir, entry.Provider)
		if _, statErr := os.Stat(absPath); statErr != nil {
			if locked {
				return nil, fmt.Errorf("prepared artifact for integration %q not found at %s; run `gestaltd bundle --config %s --output DIR`", name, absPath, configPath)
			}
			continue
		}

		preparedProviders[name] = absPath
	}

	return preparedProviders, nil
}

func (l *Lifecycle) writeProviderArtifacts(ctx context.Context, cfg *config.Config, paths initPaths) (map[string]LockProviderEntry, error) {
	written := make(map[string]LockProviderEntry)
	for _, name := range slices.Sorted(maps.Keys(cfg.Integrations)) {
		intg := cfg.Integrations[name]
		upstream, hasRemote := remoteAPIUpstreamForPrepare(intg)
		if !hasRemote {
			continue
		}

		def, err := l.loadAPIUpstream(ctx, name, upstream)
		if err != nil {
			return nil, fmt.Errorf("compiling provider %q: %w", name, err)
		}
		copied := *def
		def = &copied
		provider.ApplyDisplayOverrides(def, intg)

		outPath := filepath.Join(paths.providersDir, name+".json")
		if err := writeJSONFile(outPath, def); err != nil {
			return nil, fmt.Errorf("writing provider %q: %w", name, err)
		}

		fingerprint, err := integrationFingerprint(name, intg, upstream)
		if err != nil {
			return nil, fmt.Errorf("fingerprinting integration %q: %w", name, err)
		}

		relPath, err := filepath.Rel(paths.configDir, outPath)
		if err != nil {
			return nil, fmt.Errorf("computing provider path for %q: %w", name, err)
		}
		written[name] = LockProviderEntry{
			Fingerprint: fingerprint,
			Provider:    filepath.ToSlash(relPath),
		}
		slog.Info("wrote prepared provider", "path", outPath)
	}
	return written, nil
}

func remoteAPIUpstreamForPrepare(intg config.IntegrationDef) (config.APIDef, bool) {
	if intg.API == nil {
		return config.APIDef{}, false
	}
	switch intg.API.Type {
	case config.APITypeREST:
		if intg.API.OpenAPI != "" {
			return *intg.API, true
		}
	case config.APITypeGraphQL:
		if intg.API.URL != "" {
			return *intg.API, true
		}
	}
	return config.APIDef{}, false
}

func configHasRemoteAPIUpstreams(cfg *config.Config) bool {
	for name := range cfg.Integrations {
		_, ok := remoteAPIUpstreamForPrepare(cfg.Integrations[name])
		if ok {
			return true
		}
	}
	return false
}

func configHasPlugins(cfg *config.Config) bool {
	for name := range cfg.Integrations {
		if cfg.Integrations[name].Plugin.HasManagedArtifacts() {
			return true
		}
	}
	for name := range cfg.Runtimes {
		if cfg.Runtimes[name].Plugin.HasManagedArtifacts() {
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

func initPathsForConfig(configPath string) initPaths {
	configDir := filepath.Dir(configPath)
	return initPaths{
		configPath:   configPath,
		configDir:    configDir,
		lockfilePath: filepath.Join(configDir, InitLockfileName),
		providersDir: filepath.Join(configDir, filepath.FromSlash(PreparedProvidersDir)),
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
	var lock Lockfile
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("parsing lockfile %s: %w", path, err)
	}
	if lock.Version != LockVersion && lock.Version != LockVersionCompat {
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
	if lock == nil || (lock.Version != LockVersion && lock.Version != LockVersionCompat) {
		return false
	}
	for name := range cfg.Integrations {
		intg := cfg.Integrations[name]
		upstream, ok := remoteAPIUpstreamForPrepare(intg)
		if !ok {
			continue
		}
		entry, found := lock.Providers[name]
		if !found {
			return false
		}
		fingerprint, err := integrationFingerprint(name, intg, upstream)
		if err != nil || entry.Fingerprint != fingerprint {
			return false
		}
		absPath := resolveLockPath(paths.configDir, entry.Provider)
		if _, err := os.Stat(absPath); err != nil {
			return false
		}
	}
	for name := range cfg.Integrations {
		intg := cfg.Integrations[name]
		if !intg.Plugin.HasManagedArtifacts() {
			continue
		}
		entry, found := lock.Plugins[LockPluginKey("integration", name)]
		configMap, err := config.NodeToMap(intg.Plugin.Config)
		if err != nil || !pluginEntryMatches(paths, name, intg.Plugin, configMap, entry, found) {
			return false
		}
	}
	for name := range cfg.Runtimes {
		rt := cfg.Runtimes[name]
		if !rt.Plugin.HasManagedArtifacts() {
			continue
		}
		entry, found := lock.Plugins[LockPluginKey("runtime", name)]
		configMap, err := config.NodeToMap(rt.Config)
		if err != nil || !pluginEntryMatches(paths, name, rt.Plugin, configMap, entry, found) {
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
		manifestPath := resolveLockPath(paths.configDir, entry.Manifest)
		if _, err := os.Stat(manifestPath); err != nil {
			return false
		}
		assetRootPath := resolveLockPath(paths.configDir, entry.AssetRoot)
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

func PluginFingerprint(name string, plugin *config.ExecutablePluginDef, configMap map[string]any) (string, error) {
	if plugin == nil {
		return "", nil
	}

	input := pluginFingerprintInput{
		Name:    name,
		Command: plugin.Command,
		Package: plugin.Package,
		Source:  plugin.Source,
		Version: plugin.Version,
		Args:    plugin.Args,
		Env:     plugin.Env,
	}
	if configMap != nil {
		input.Config = configMap
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

func pluginEntryMatches(paths initPaths, name string, plugin *config.ExecutablePluginDef, configMap map[string]any, entry LockPluginEntry, found bool) bool {
	if !found {
		return false
	}
	fingerprint, err := PluginFingerprint(name, plugin, configMap)
	if err != nil || entry.Fingerprint != fingerprint {
		return false
	}
	if entry.Source != "" {
		if entry.Source != plugin.Source || entry.Version != plugin.Version {
			return false
		}
	} else if entry.Package != plugin.Package {
		return false
	}
	manifestPath := resolveLockPath(paths.configDir, entry.Manifest)
	if _, err := os.Stat(manifestPath); err != nil {
		return false
	}
	if entry.Executable != "" {
		executablePath := resolveLockPath(paths.configDir, entry.Executable)
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

func (l *Lifecycle) writePluginArtifacts(ctx context.Context, configPath string, cfg *config.Config, paths initPaths) (map[string]LockPluginEntry, error) {
	store := pluginstore.New(configPath)
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
			entry, err = l.lockEntryForSource(ctx, paths, store, "integration", name, intg.Plugin, configMap)
		case intg.Plugin.Package != "":
			entry, err = lockEntryForPackage(ctx, paths, store, "integration", name, intg.Plugin, configMap)
		default:
			continue
		}
		if err != nil {
			return nil, err
		}
		written[LockPluginKey("integration", name)] = entry
	}
	for name := range cfg.Runtimes {
		rt := cfg.Runtimes[name]
		if rt.Plugin == nil {
			continue
		}
		configMap, err := config.NodeToMap(rt.Config)
		if err != nil {
			return nil, fmt.Errorf("decode runtime config for runtime %q: %w", name, err)
		}
		var entry LockPluginEntry
		switch {
		case rt.Plugin.Source != "":
			entry, err = l.lockEntryForSource(ctx, paths, store, "runtime", name, rt.Plugin, configMap)
		case rt.Plugin.Package != "":
			entry, err = lockEntryForPackage(ctx, paths, store, "runtime", name, rt.Plugin, configMap)
		default:
			continue
		}
		if err != nil {
			return nil, err
		}
		written[LockPluginKey("runtime", name)] = entry
	}

	if cfg.UI.Plugin.HasManagedArtifacts() {
		entry, err := l.writeUIPluginArtifact(ctx, cfg, paths, store)
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

func lockEntryForPackage(ctx context.Context, paths initPaths, store *pluginstore.Store, kind, name string, plugin *config.ExecutablePluginDef, configMap map[string]any) (LockPluginEntry, error) {
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

	var installed *pluginstore.InstalledPlugin
	if info.IsDir() {
		installed, err = store.InstallFromDir(packagePath)
	} else {
		installed, err = store.Install(packagePath)
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
	fingerprint, err := PluginFingerprint(name, plugin, configMap)
	if err != nil {
		return LockPluginEntry{}, fmt.Errorf("fingerprinting %s %q plugin: %w", kind, name, err)
	}
	manifestPath, err := filepath.Rel(paths.configDir, installed.ManifestPath)
	if err != nil {
		return LockPluginEntry{}, fmt.Errorf("compute manifest path for %s %q: %w", kind, name, err)
	}
	var executableRel string
	if installed.ExecutablePath != "" {
		executablePath, err := filepath.Rel(paths.configDir, installed.ExecutablePath)
		if err != nil {
			return LockPluginEntry{}, fmt.Errorf("compute executable path for %s %q: %w", kind, name, err)
		}
		executableRel = filepath.ToSlash(executablePath)
	}
	return LockPluginEntry{
		Fingerprint:  fingerprint,
		Package:      plugin.Package,
		SourceDigest: sourceDigest,
		Manifest:     filepath.ToSlash(manifestPath),
		Executable:   executableRel,
	}, nil
}

func (l *Lifecycle) lockEntryForSource(ctx context.Context, paths initPaths, store *pluginstore.Store, kind, name string, plugin *config.ExecutablePluginDef, configMap map[string]any) (LockPluginEntry, error) {
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

	installed, err := store.Install(resolved.LocalPath)
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
	fingerprint, err := PluginFingerprint(name, plugin, configMap)
	if err != nil {
		return LockPluginEntry{}, fmt.Errorf("fingerprinting %s %q plugin: %w", kind, name, err)
	}
	manifestPath, err := filepath.Rel(paths.configDir, installed.ManifestPath)
	if err != nil {
		return LockPluginEntry{}, fmt.Errorf("compute manifest path for %s %q: %w", kind, name, err)
	}
	var executableRel string
	if installed.ExecutablePath != "" {
		ep, err := filepath.Rel(paths.configDir, installed.ExecutablePath)
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

func (l *Lifecycle) writeUIPluginArtifact(ctx context.Context, cfg *config.Config, paths initPaths, store *pluginstore.Store) (LockPluginEntry, error) {
	plugin := cfg.UI.Plugin
	fingerprint, err := UIPluginFingerprint(plugin)
	if err != nil {
		return LockPluginEntry{}, fmt.Errorf("fingerprinting ui plugin: %w", err)
	}

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
		installed, err = store.Install(resolved.LocalPath)
		if err != nil {
			return LockPluginEntry{}, fmt.Errorf("ui plugin install source: %w", err)
		}
		if installed.Manifest.Source != plugin.Source {
			return LockPluginEntry{}, fmt.Errorf("ui plugin manifest source %q does not match config source %q", installed.Manifest.Source, plugin.Source)
		}
		if installed.Manifest.Version != plugin.Version {
			return LockPluginEntry{}, fmt.Errorf("ui plugin manifest version %q does not match config version %q", installed.Manifest.Version, plugin.Version)
		}
		manifestPath, err := filepath.Rel(paths.configDir, installed.ManifestPath)
		if err != nil {
			return LockPluginEntry{}, fmt.Errorf("compute manifest path for ui plugin: %w", err)
		}
		assetRoot, err := filepath.Rel(paths.configDir, installed.AssetRoot)
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
			installed, err = store.InstallFromDir(packagePath)
		} else {
			installed, err = store.Install(packagePath)
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
		manifestPath, err := filepath.Rel(paths.configDir, installed.ManifestPath)
		if err != nil {
			return LockPluginEntry{}, fmt.Errorf("compute manifest path for ui plugin: %w", err)
		}
		assetRoot, err := filepath.Rel(paths.configDir, installed.AssetRoot)
		if err != nil {
			return LockPluginEntry{}, fmt.Errorf("compute asset root path for ui plugin: %w", err)
		}
		return LockPluginEntry{
			Fingerprint:  fingerprint,
			Package:      plugin.Package,
			SourceDigest: sourceDigest,
			Manifest:     filepath.ToSlash(manifestPath),
			AssetRoot:    filepath.ToSlash(assetRoot),
		}, nil
	}

	return LockPluginEntry{}, fmt.Errorf("ui plugin requires package or source")
}

func (l *Lifecycle) applyLockedPlugins(configPath string, cfg *config.Config, locked bool) error {
	if !configHasPlugins(cfg) {
		return nil
	}

	paths := initPathsForConfig(configPath)
	lock, err := ReadLockfile(paths.lockfilePath)
	if !locked && (err != nil || !lockMatchesConfig(cfg, paths, lock)) {
		lock, err = l.InitAtPath(configPath)
	}
	if err != nil {
		return fmt.Errorf("plugin packages require prepared artifacts; run `gestaltd bundle --config %s --output DIR`: %w", configPath, err)
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
		if err := applyLockedPluginEntry(paths, lock, "integration", name, intg.Plugin, configMap); err != nil {
			return err
		}
		if intg.IconFile == "" && intg.Plugin.ResolvedIconFile != "" {
			intg.IconFile = intg.Plugin.ResolvedIconFile
			cfg.Integrations[name] = intg
		}
	}
	for name := range cfg.Runtimes {
		rt := cfg.Runtimes[name]
		if !rt.Plugin.HasManagedArtifacts() {
			continue
		}
		configMap, err := config.NodeToMap(rt.Config)
		if err != nil {
			return fmt.Errorf("decode runtime config for runtime %q: %w", name, err)
		}
		if err := applyLockedPluginEntry(paths, lock, "runtime", name, rt.Plugin, configMap); err != nil {
			return err
		}
	}

	if cfg.UI.Plugin.HasManagedArtifacts() {
		key := LockPluginKey("ui", "default")
		entry, ok := lock.Plugins[key]
		if !ok {
			return fmt.Errorf("prepared artifact for ui plugin is missing or stale; run `gestaltd bundle --config %s --output DIR`", paths.configPath)
		}
		fingerprint, err := UIPluginFingerprint(cfg.UI.Plugin)
		if err != nil || entry.Fingerprint != fingerprint {
			return fmt.Errorf("prepared artifact for ui plugin is missing or stale; run `gestaltd bundle --config %s --output DIR`", paths.configPath)
		}
		manifestPath := resolveLockPath(paths.configDir, entry.Manifest)
		assetRootPath := resolveLockPath(paths.configDir, entry.AssetRoot)
		if _, err := os.Stat(manifestPath); err != nil {
			return fmt.Errorf("prepared manifest for ui plugin not found at %s", manifestPath)
		}
		if _, err := os.Stat(assetRootPath); err != nil {
			return fmt.Errorf("prepared asset root for ui plugin not found at %s", assetRootPath)
		}
		cfg.UI.Plugin.ResolvedAssetRoot = assetRootPath
		cfg.UI.Plugin.ResolvedManifestPath = manifestPath
	}

	return nil
}

func applyLockedPluginEntry(paths initPaths, lock *Lockfile, kind, name string, plugin *config.ExecutablePluginDef, configMap map[string]any) error {
	key := LockPluginKey(kind, name)
	entry, ok := lock.Plugins[key]
	if !ok {
		return fmt.Errorf("prepared artifact for %s %q is missing or stale; run `gestaltd bundle --config %s --output DIR`", kind, name, paths.configPath)
	}
	fingerprint, err := PluginFingerprint(name, plugin, configMap)
	if err != nil {
		return fmt.Errorf("fingerprinting %s %q plugin: %w", kind, name, err)
	}
	if entry.Source != "" {
		if entry.Fingerprint != fingerprint || entry.Source != plugin.Source || entry.Version != plugin.Version {
			return fmt.Errorf("prepared artifact for %s %q is missing or stale; run `gestaltd bundle --config %s --output DIR`", kind, name, paths.configPath)
		}
	} else if entry.Fingerprint != fingerprint || entry.Package != plugin.Package {
		return fmt.Errorf("prepared artifact for %s %q is missing or stale; run `gestaltd bundle --config %s --output DIR`", kind, name, paths.configPath)
	}

	manifestPath := resolveLockPath(paths.configDir, entry.Manifest)
	if _, err := os.Stat(manifestPath); err != nil {
		return fmt.Errorf("prepared manifest for %s %q not found at %s; run `gestaltd bundle --config %s --output DIR`", kind, name, manifestPath, paths.configPath)
	}

	_, manifest, err := pluginpkg.ReadManifestFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read prepared manifest for %s %q: %w", kind, name, err)
	}
	if err := pluginpkg.ValidateConfigForManifest(manifestPath, manifest, manifestKind(kind), configMap); err != nil {
		return fmt.Errorf("plugin config validation for %s %q: %w", kind, name, err)
	}

	resolvePluginIcon(manifest, manifestPath, plugin)

	plugin.ResolvedManifestPath = manifestPath
	if kind == "integration" && manifest.IsHybridProvider() {
		plugin.IsHybrid = true
	} else if kind == "integration" && manifest.Provider.IsDeclarative() {
		plugin.IsDeclarative = true
		return nil
	}

	executablePath := resolveLockPath(paths.configDir, entry.Executable)
	if _, err := os.Stat(executablePath); err != nil {
		return fmt.Errorf("prepared executable for %s %q not found at %s; run `gestaltd bundle --config %s --output DIR`", kind, name, executablePath, paths.configPath)
	}

	args, err := entrypointArgs(kind, manifest)
	if err != nil {
		return fmt.Errorf("resolve entrypoint for %s %q: %w", kind, name, err)
	}

	plugin.Command = executablePath
	plugin.Args = append([]string(nil), args...)
	plugin.ResolvedManifestPath = manifestPath
	return nil
}

func resolvePluginIcon(manifest *pluginmanifestv1.Manifest, manifestPath string, plugin *config.ExecutablePluginDef) {
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
	case "runtime":
		entry = manifest.Entrypoints.Runtime
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
	case "runtime":
		return pluginmanifestv1.KindRuntime
	default:
		return ""
	}
}

func integrationFingerprint(name string, intg config.IntegrationDef, api config.APIDef) (string, error) {
	specURL := api.URL
	if api.Type == config.APITypeREST {
		specURL = api.OpenAPI
	}
	input := providerFingerprintInput{
		Type:        api.Type,
		URL:         specURL,
		DisplayName: intg.DisplayName,
		Description: intg.Description,
		HasIcon:     intg.IconFile != "",
	}
	if intg.IconFile != "" {
		data, err := os.ReadFile(intg.IconFile)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				input.IconReadError = err.Error()
			} else {
				input.IconReadError = os.ErrNotExist.Error()
			}
		} else {
			sum := sha256.Sum256(data)
			input.IconSHA256 = hex.EncodeToString(sum[:])
		}
	}
	payload, err := json.Marshal(struct {
		Name string `json:"name"`
		providerFingerprintInput
	}{
		Name:                     name,
		providerFingerprintInput: input,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}
