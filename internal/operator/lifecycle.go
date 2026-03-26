package operator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/pluginpkg"
	"github.com/valon-technologies/gestalt/internal/pluginstore"
	"github.com/valon-technologies/gestalt/internal/provider"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/sdk/pluginmanifest/v1"
)

const (
	InitLockfileName     = "gestalt.lock.json"
	PreparedProvidersDir = ".gestalt/providers"
	LockVersion          = 3
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
	Fingerprint  string `json:"fingerprint"`
	Package      string `json:"package,omitempty"`
	SourceDigest string `json:"source_digest,omitempty"`
	Manifest     string `json:"manifest"`
	Executable   string `json:"executable"`
}

type UpstreamLoader func(ctx context.Context, name string, upstream config.UpstreamDef) (*provider.Definition, error)

type Lifecycle struct {
	loadAPIUpstream UpstreamLoader
}

func NewLifecycle(loadAPIUpstream UpstreamLoader) *Lifecycle {
	return &Lifecycle{loadAPIUpstream: loadAPIUpstream}
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

	resolvedPlugins, err := writePluginArtifacts(context.Background(), configPath, cfg, paths)
	if err != nil {
		return nil, err
	}
	for key, entry := range resolvedPlugins {
		lock.Plugins[key] = entry
	}

	if err := WriteLockfile(paths.lockfilePath, lock); err != nil {
		return nil, err
	}

	log.Printf("prepared providers: %d", len(lock.Providers))
	log.Printf("wrote lockfile %s", paths.lockfilePath)
	return lock, nil
}

func (l *Lifecycle) LoadForExecutionAtPath(configPath string, locked bool) (*config.Config, map[string]string, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, nil, fmt.Errorf("loading config: %v", err)
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
	Type              string            `json:"type"`
	URL               string            `json:"url"`
	AllowedOperations map[string]string `json:"allowed_operations,omitempty"`
	DisplayName       string            `json:"display_name,omitempty"`
	Description       string            `json:"description,omitempty"`
	HasIcon           bool              `json:"has_icon,omitempty"`
	IconSHA256        string            `json:"icon_sha256,omitempty"`
	IconReadError     string            `json:"icon_read_error,omitempty"`
}

type pluginFingerprintInput struct {
	Name    string            `json:"name"`
	Mode    string            `json:"mode,omitempty"`
	Command string            `json:"command,omitempty"`
	Package string            `json:"package,omitempty"`
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
			return nil, fmt.Errorf("remote REST/GraphQL upstreams require prepared artifacts; run `gestaltd init --config %s`: %w", configPath, err)
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
				return nil, fmt.Errorf("prepared artifact for integration %q is missing or stale; run `gestaltd init --config %s`", name, configPath)
			}
			continue
		}

		absPath := resolveLockPath(paths.configDir, entry.Provider)
		if _, statErr := os.Stat(absPath); statErr != nil {
			if locked {
				return nil, fmt.Errorf("prepared artifact for integration %q not found at %s; run `gestaltd init --config %s`", name, absPath, configPath)
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
		if err := provider.ApplyArtifactOverrides(def, intg); err != nil {
			return nil, fmt.Errorf("applying artifact overrides for %q: %w", name, err)
		}

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
		log.Printf("wrote prepared provider %s", outPath)
	}
	return written, nil
}

func remoteAPIUpstreamForPrepare(intg config.IntegrationDef) (config.UpstreamDef, bool) {
	for i := range intg.Upstreams {
		us := &intg.Upstreams[i]
		switch us.Type {
		case config.UpstreamTypeREST, config.UpstreamTypeGraphQL:
			if us.URL != "" {
				return *us, true
			}
		}
	}
	return config.UpstreamDef{}, false
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

func configHasPluginPackages(cfg *config.Config) bool {
	for name := range cfg.Integrations {
		intg := cfg.Integrations[name]
		if intg.Plugin != nil && intg.Plugin.Package != "" {
			return true
		}
	}
	for name := range cfg.Runtimes {
		rt := cfg.Runtimes[name]
		if rt.Plugin != nil && rt.Plugin.Package != "" {
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
		if intg.Plugin == nil || intg.Plugin.Package == "" {
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
		if rt.Plugin == nil || rt.Plugin.Package == "" {
			continue
		}
		entry, found := lock.Plugins[LockPluginKey("runtime", name)]
		configMap, err := config.NodeToMap(rt.Config)
		if err != nil || !pluginEntryMatches(paths, name, rt.Plugin, configMap, entry, found) {
			return false
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
		Mode:    plugin.Mode,
		Command: plugin.Command,
		Package: plugin.Package,
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

func LockPluginKey(kind, name string) string {
	return kind + ":" + name
}

func pluginEntryMatches(paths initPaths, name string, plugin *config.ExecutablePluginDef, configMap map[string]any, entry LockPluginEntry, found bool) bool {
	if !found {
		return false
	}
	fingerprint, err := PluginFingerprint(name, plugin, configMap)
	if err != nil || entry.Fingerprint != fingerprint || entry.Package != plugin.Package {
		return false
	}
	manifestPath := resolveLockPath(paths.configDir, entry.Manifest)
	executablePath := resolveLockPath(paths.configDir, entry.Executable)
	if _, err := os.Stat(manifestPath); err != nil {
		return false
	}
	if _, err := os.Stat(executablePath); err != nil {
		return false
	}
	if entry.SourceDigest != "" && !strings.HasPrefix(plugin.Package, "https://") {
		digest, err := sourceDigestForPackage(plugin.Package)
		if err != nil || digest != entry.SourceDigest {
			return false
		}
	}
	return true
}

func writePluginArtifacts(ctx context.Context, configPath string, cfg *config.Config, paths initPaths) (map[string]LockPluginEntry, error) {
	store := pluginstore.New(configPath)
	written := make(map[string]LockPluginEntry)
	for name := range cfg.Integrations {
		intg := cfg.Integrations[name]
		if intg.Plugin == nil || intg.Plugin.Package == "" {
			continue
		}
		configMap, err := config.NodeToMap(intg.Plugin.Config)
		if err != nil {
			return nil, fmt.Errorf("decode plugin config for integration %q: %w", name, err)
		}
		entry, err := lockEntryForPackage(ctx, paths, store, "integration", name, intg.Plugin, configMap)
		if err != nil {
			return nil, err
		}
		written[LockPluginKey("integration", name)] = entry
	}
	for name := range cfg.Runtimes {
		rt := cfg.Runtimes[name]
		if rt.Plugin == nil || rt.Plugin.Package == "" {
			continue
		}
		configMap, err := config.NodeToMap(rt.Config)
		if err != nil {
			return nil, fmt.Errorf("decode runtime config for runtime %q: %w", name, err)
		}
		entry, err := lockEntryForPackage(ctx, paths, store, "runtime", name, rt.Plugin, configMap)
		if err != nil {
			return nil, err
		}
		written[LockPluginKey("runtime", name)] = entry
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
	executablePath, err := filepath.Rel(paths.configDir, installed.ExecutablePath)
	if err != nil {
		return LockPluginEntry{}, fmt.Errorf("compute executable path for %s %q: %w", kind, name, err)
	}
	return LockPluginEntry{
		Fingerprint:  fingerprint,
		Package:      plugin.Package,
		SourceDigest: sourceDigest,
		Manifest:     filepath.ToSlash(manifestPath),
		Executable:   filepath.ToSlash(executablePath),
	}, nil
}

func (l *Lifecycle) applyLockedPlugins(configPath string, cfg *config.Config, locked bool) error {
	if !configHasPluginPackages(cfg) {
		return nil
	}

	paths := initPathsForConfig(configPath)
	lock, err := ReadLockfile(paths.lockfilePath)
	if !locked && (err != nil || !lockMatchesConfig(cfg, paths, lock)) {
		lock, err = l.InitAtPath(configPath)
	}
	if err != nil {
		return fmt.Errorf("plugin packages require prepared artifacts; run `gestaltd init --config %s`: %w", configPath, err)
	}

	for name := range cfg.Integrations {
		intg := cfg.Integrations[name]
		if intg.Plugin == nil || intg.Plugin.Package == "" {
			continue
		}
		configMap, err := config.NodeToMap(intg.Plugin.Config)
		if err != nil {
			return fmt.Errorf("decode plugin config for integration %q: %w", name, err)
		}
		if err := applyLockedPluginEntry(paths, lock, "integration", name, intg.Plugin, configMap); err != nil {
			return err
		}
	}
	for name := range cfg.Runtimes {
		rt := cfg.Runtimes[name]
		if rt.Plugin == nil || rt.Plugin.Package == "" {
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
	return nil
}

func applyLockedPluginEntry(paths initPaths, lock *Lockfile, kind, name string, plugin *config.ExecutablePluginDef, configMap map[string]any) error {
	key := LockPluginKey(kind, name)
	entry, ok := lock.Plugins[key]
	if !ok {
		return fmt.Errorf("prepared artifact for %s %q is missing or stale; run `gestaltd init --config %s`", kind, name, paths.configPath)
	}
	fingerprint, err := PluginFingerprint(name, plugin, configMap)
	if err != nil {
		return fmt.Errorf("fingerprinting %s %q plugin: %w", kind, name, err)
	}
	if entry.Fingerprint != fingerprint || entry.Package != plugin.Package {
		return fmt.Errorf("prepared artifact for %s %q is missing or stale; run `gestaltd init --config %s`", kind, name, paths.configPath)
	}

	manifestPath := resolveLockPath(paths.configDir, entry.Manifest)
	executablePath := resolveLockPath(paths.configDir, entry.Executable)
	if _, err := os.Stat(manifestPath); err != nil {
		return fmt.Errorf("prepared manifest for %s %q not found at %s; run `gestaltd init --config %s`", kind, name, manifestPath, paths.configPath)
	}
	if _, err := os.Stat(executablePath); err != nil {
		return fmt.Errorf("prepared executable for %s %q not found at %s; run `gestaltd init --config %s`", kind, name, executablePath, paths.configPath)
	}

	_, manifest, err := pluginpkg.ReadManifestFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read prepared manifest for %s %q: %w", kind, name, err)
	}
	if err := pluginpkg.ValidateConfigForManifest(manifestPath, manifest, manifestKind(kind), configMap); err != nil {
		return fmt.Errorf("plugin config validation for %s %q: %w", kind, name, err)
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

func integrationFingerprint(name string, intg config.IntegrationDef, upstream config.UpstreamDef) (string, error) {
	input := providerFingerprintInput{
		Type:              upstream.Type,
		URL:               upstream.URL,
		AllowedOperations: map[string]string(upstream.AllowedOperations),
		DisplayName:       intg.DisplayName,
		Description:       intg.Description,
		HasIcon:           intg.IconFile != "",
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
