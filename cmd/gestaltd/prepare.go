package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
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
	preparedLockfileName = "gestalt.lock.json"
	preparedProvidersDir = ".gestalt/providers"
	preparedLockVersion  = 2
)

const (
	providerResolutionPrefer providerResolutionMode = iota
	providerResolutionRequire
	providerResolutionAuto
)

type providerResolutionMode int

type preparedPaths struct {
	configPath   string
	configDir    string
	lockfilePath string
	providersDir string
}

type preparedLockfile struct {
	Version   int                              `json:"version"`
	Providers map[string]preparedProviderEntry `json:"providers"`
	Plugins   map[string]preparedPluginEntry   `json:"plugins"`
}

type preparedProviderEntry struct {
	Fingerprint string `json:"fingerprint"`
	Provider    string `json:"provider"`
}

type preparedPluginEntry struct {
	Fingerprint string `json:"fingerprint"`
	Ref         string `json:"ref"`
	Manifest    string `json:"manifest"`
	Executable  string `json:"executable"`
	SHA256      string `json:"sha256"`
}

type preparedFingerprintInput struct {
	Type              string            `json:"type"`
	URL               string            `json:"url"`
	AllowedOperations map[string]string `json:"allowed_operations,omitempty"`
	DisplayName       string            `json:"display_name,omitempty"`
	Description       string            `json:"description,omitempty"`
	HasIcon           bool              `json:"has_icon,omitempty"`
	IconSHA256        string            `json:"icon_sha256,omitempty"`
	IconReadError     string            `json:"icon_read_error,omitempty"`
}

func runPrepare(args []string) error {
	fs := flag.NewFlagSet("gestaltd prepare", flag.ContinueOnError)
	fs.Usage = func() { printPrepareUsage(fs.Output()) }
	configPath := fs.String("config", "", "path to config file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	return prepareConfig(*configPath)
}

func prepareConfig(configFlag string) error {
	configPath := resolveConfigPath(configFlag)
	_, err := prepareConfigAtPath(configPath)
	return err
}

func prepareConfigAtPath(configPath string) (*preparedLockfile, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, fmt.Errorf("loading config: %v", err)
	}

	paths := preparePathsForConfig(configPath)
	if err := os.MkdirAll(paths.providersDir, 0755); err != nil {
		return nil, fmt.Errorf("creating providers dir: %w", err)
	}

	lock := &preparedLockfile{
		Version:   preparedLockVersion,
		Providers: make(map[string]preparedProviderEntry),
		Plugins:   make(map[string]preparedPluginEntry),
	}

	written, err := writePreparedArtifacts(context.Background(), cfg, paths)
	if err != nil {
		return nil, err
	}
	for name, entry := range written {
		lock.Providers[name] = entry
	}
	resolvedPlugins, err := writePreparedPlugins(configPath, cfg, paths)
	if err != nil {
		return nil, err
	}
	for key, entry := range resolvedPlugins {
		lock.Plugins[key] = entry
	}

	if err := writePreparedLockfile(paths.lockfilePath, lock); err != nil {
		return nil, err
	}

	log.Printf("prepared providers: %d", len(lock.Providers))
	log.Printf("wrote lockfile %s", paths.lockfilePath)
	return lock, nil
}

func loadConfigForExecution(configFlag string, mode providerResolutionMode) (string, *config.Config, map[string]string, error) {
	configPath := resolveConfigPath(configFlag)

	cfg, err := config.Load(configPath)
	if err != nil {
		return "", nil, nil, fmt.Errorf("loading config: %v", err)
	}

	preparedProviders, err := preparedProvidersForConfig(configPath, cfg, mode)
	if err != nil {
		return "", nil, nil, err
	}
	if err := applyPreparedPlugins(configPath, cfg, mode); err != nil {
		return "", nil, nil, err
	}

	return configPath, cfg, preparedProviders, nil
}

func preparedProvidersForConfig(configPath string, cfg *config.Config, mode providerResolutionMode) (map[string]string, error) {
	if !configHasRemoteAPIUpstreams(cfg) {
		return nil, nil
	}

	paths := preparePathsForConfig(configPath)
	lock, err := readPreparedLockfile(paths.lockfilePath)
	if mode == providerResolutionAuto && (err != nil || !preparedLockMatchesConfig(cfg, paths, lock)) {
		lock, err = prepareConfigAtPath(configPath)
	}
	if err != nil {
		if mode == providerResolutionRequire {
			return nil, fmt.Errorf("remote REST/GraphQL upstreams require prepared artifacts; run `gestaltd prepare --config %s`: %w", configPath, err)
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
			if mode == providerResolutionRequire {
				return nil, fmt.Errorf("prepared artifact for integration %q is missing or stale; run `gestaltd prepare --config %s`", name, configPath)
			}
			continue
		}

		absPath := resolveProviderPath(paths.configDir, entry.Provider)
		if _, statErr := os.Stat(absPath); statErr != nil {
			if mode == providerResolutionRequire {
				return nil, fmt.Errorf("prepared artifact for integration %q not found at %s; run `gestaltd prepare --config %s`", name, absPath, configPath)
			}
			continue
		}

		preparedProviders[name] = absPath
	}

	return preparedProviders, nil
}

func writePreparedArtifacts(ctx context.Context, cfg *config.Config, paths preparedPaths) (map[string]preparedProviderEntry, error) {
	written := make(map[string]preparedProviderEntry)
	for _, name := range slices.Sorted(maps.Keys(cfg.Integrations)) {
		intg := cfg.Integrations[name]
		upstream, hasRemote := remoteAPIUpstreamForPrepare(intg)
		if !hasRemote {
			continue
		}

		def, err := loadAPIUpstream(ctx, name, upstream, nil)
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
		written[name] = preparedProviderEntry{
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

func configHasPluginRefs(cfg *config.Config) bool {
	for name := range cfg.Integrations {
		intg := cfg.Integrations[name]
		if intg.Plugin != nil && intg.Plugin.Ref != "" {
			return true
		}
	}
	for name := range cfg.Runtimes {
		rt := cfg.Runtimes[name]
		if rt.Plugin != nil && rt.Plugin.Ref != "" {
			return true
		}
	}
	return false
}

func resolveProviderPath(baseDir, provider string) string {
	if filepath.IsAbs(provider) {
		return provider
	}
	return filepath.Join(baseDir, filepath.FromSlash(provider))
}

func preparePathsForConfig(configPath string) preparedPaths {
	configDir := filepath.Dir(configPath)
	return preparedPaths{
		configPath:   configPath,
		configDir:    configDir,
		lockfilePath: filepath.Join(configDir, preparedLockfileName),
		providersDir: filepath.Join(configDir, filepath.FromSlash(preparedProvidersDir)),
	}
}

func writeJSONFile(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0644)
}

func readPreparedLockfile(path string) (*preparedLockfile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var lock preparedLockfile
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("parsing lockfile %s: %w", path, err)
	}
	if lock.Version != preparedLockVersion {
		return nil, fmt.Errorf("unsupported lockfile version %d", lock.Version)
	}
	if lock.Providers == nil {
		lock.Providers = make(map[string]preparedProviderEntry)
	}
	if lock.Plugins == nil {
		lock.Plugins = make(map[string]preparedPluginEntry)
	}
	return &lock, nil
}

func writePreparedLockfile(path string, lock *preparedLockfile) error {
	if err := writeJSONFile(path, lock); err != nil {
		return fmt.Errorf("writing lockfile: %w", err)
	}
	return nil
}

func preparedLockMatchesConfig(cfg *config.Config, paths preparedPaths, lock *preparedLockfile) bool {
	if lock == nil || lock.Version != preparedLockVersion {
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
		absPath := resolveProviderPath(paths.configDir, entry.Provider)
		if _, err := os.Stat(absPath); err != nil {
			return false
		}
	}
	for name := range cfg.Integrations {
		intg := cfg.Integrations[name]
		if intg.Plugin == nil || intg.Plugin.Ref == "" {
			continue
		}
		entry, found := lock.Plugins[preparedPluginKey("integration", name)]
		if !pluginEntryMatches(paths, name, intg.Plugin, entry, found) {
			return false
		}
	}
	for name := range cfg.Runtimes {
		rt := cfg.Runtimes[name]
		if rt.Plugin == nil || rt.Plugin.Ref == "" {
			continue
		}
		entry, found := lock.Plugins[preparedPluginKey("runtime", name)]
		if !pluginEntryMatches(paths, name, rt.Plugin, entry, found) {
			return false
		}
	}
	return true
}

type preparedPluginFingerprintInput struct {
	Name    string            `json:"name"`
	Mode    string            `json:"mode,omitempty"`
	Command string            `json:"command,omitempty"`
	Ref     string            `json:"ref,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Config  map[string]any    `json:"config,omitempty"`
}

func pluginFingerprint(name string, plugin *config.ExecutablePluginDef) (string, error) {
	if plugin == nil {
		return "", nil
	}

	input := preparedPluginFingerprintInput{
		Name:    name,
		Mode:    plugin.Mode,
		Command: plugin.Command,
		Ref:     plugin.Ref,
		Args:    plugin.Args,
		Env:     plugin.Env,
	}
	if plugin.Config.Kind != 0 {
		var cfg map[string]any
		if err := plugin.Config.Decode(&cfg); err != nil {
			return "", fmt.Errorf("decoding plugin config fingerprint input: %w", err)
		}
		input.Config = cfg
	}

	payload, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func preparedPluginKey(kind, name string) string {
	return kind + ":" + name
}

func resolveInstalledPluginRef(store *pluginstore.Store, raw string) (*pluginstore.InstalledPlugin, error) {
	ref, err := pluginstore.ParseRef(raw)
	if err != nil {
		return nil, err
	}
	return store.Resolve(ref)
}

func pluginEntryMatches(paths preparedPaths, name string, plugin *config.ExecutablePluginDef, entry preparedPluginEntry, found bool) bool {
	if !found {
		return false
	}
	fingerprint, err := pluginFingerprint(name, plugin)
	if err != nil || entry.Fingerprint != fingerprint || entry.Ref != plugin.Ref {
		return false
	}
	manifestPath := resolveProviderPath(paths.configDir, entry.Manifest)
	executablePath := resolveProviderPath(paths.configDir, entry.Executable)
	if _, err := os.Stat(manifestPath); err != nil {
		return false
	}
	if _, err := os.Stat(executablePath); err != nil {
		return false
	}
	installed, err := resolveInstalledPluginRef(pluginstore.New(paths.configPath), plugin.Ref)
	if err != nil {
		return false
	}
	return installed.SHA256 == entry.SHA256
}

func writePreparedPlugins(configPath string, cfg *config.Config, paths preparedPaths) (map[string]preparedPluginEntry, error) {
	store := pluginstore.New(configPath)
	written := make(map[string]preparedPluginEntry)
	for name := range cfg.Integrations {
		intg := cfg.Integrations[name]
		if intg.Plugin == nil || intg.Plugin.Ref == "" {
			continue
		}
		entry, err := preparedPluginEntryForConfigItem(paths, store, "integration", name, intg.Plugin)
		if err != nil {
			return nil, err
		}
		written[preparedPluginKey("integration", name)] = entry
	}
	for name := range cfg.Runtimes {
		rt := cfg.Runtimes[name]
		if rt.Plugin == nil || rt.Plugin.Ref == "" {
			continue
		}
		entry, err := preparedPluginEntryForConfigItem(paths, store, "runtime", name, rt.Plugin)
		if err != nil {
			return nil, err
		}
		written[preparedPluginKey("runtime", name)] = entry
	}
	return written, nil
}

func preparedPluginEntryForConfigItem(paths preparedPaths, store *pluginstore.Store, kind, name string, plugin *config.ExecutablePluginDef) (preparedPluginEntry, error) {
	installed, err := resolveInstalledPluginRef(store, plugin.Ref)
	if err != nil {
		return preparedPluginEntry{}, fmt.Errorf("%s %q plugin.ref %q is not installed; run `gestaltd plugin install --config %s <package>`: %w", kind, name, plugin.Ref, paths.configPath, err)
	}
	fingerprint, err := pluginFingerprint(name, plugin)
	if err != nil {
		return preparedPluginEntry{}, fmt.Errorf("fingerprinting %s %q plugin: %w", kind, name, err)
	}
	manifestPath, err := filepath.Rel(paths.configDir, installed.ManifestPath)
	if err != nil {
		return preparedPluginEntry{}, fmt.Errorf("compute manifest path for %s %q: %w", kind, name, err)
	}
	executablePath, err := filepath.Rel(paths.configDir, installed.ExecutablePath)
	if err != nil {
		return preparedPluginEntry{}, fmt.Errorf("compute executable path for %s %q: %w", kind, name, err)
	}
	return preparedPluginEntry{
		Fingerprint: fingerprint,
		Ref:         plugin.Ref,
		Manifest:    filepath.ToSlash(manifestPath),
		Executable:  filepath.ToSlash(executablePath),
		SHA256:      installed.SHA256,
	}, nil
}

func applyPreparedPlugins(configPath string, cfg *config.Config, mode providerResolutionMode) error {
	if !configHasPluginRefs(cfg) {
		return nil
	}

	paths := preparePathsForConfig(configPath)
	lock, err := readPreparedLockfile(paths.lockfilePath)
	if mode == providerResolutionAuto && (err != nil || !preparedLockMatchesConfig(cfg, paths, lock)) {
		lock, err = prepareConfigAtPath(configPath)
	}
	if err != nil {
		return fmt.Errorf("plugin refs require prepared artifacts; run `gestaltd prepare --config %s`: %w", configPath, err)
	}

	for name := range cfg.Integrations {
		intg := cfg.Integrations[name]
		if intg.Plugin == nil || intg.Plugin.Ref == "" {
			continue
		}
		if err := applyPreparedPluginEntry(paths, lock, "integration", name, intg.Plugin); err != nil {
			return err
		}
	}
	for name := range cfg.Runtimes {
		rt := cfg.Runtimes[name]
		if rt.Plugin == nil || rt.Plugin.Ref == "" {
			continue
		}
		if err := applyPreparedPluginEntry(paths, lock, "runtime", name, rt.Plugin); err != nil {
			return err
		}
	}
	return nil
}

func applyPreparedPluginEntry(paths preparedPaths, lock *preparedLockfile, kind, name string, plugin *config.ExecutablePluginDef) error {
	key := preparedPluginKey(kind, name)
	entry, ok := lock.Plugins[key]
	if !ok {
		return fmt.Errorf("prepared artifact for %s %q is missing or stale; run `gestaltd prepare --config %s`", kind, name, paths.configPath)
	}
	fingerprint, err := pluginFingerprint(name, plugin)
	if err != nil {
		return fmt.Errorf("fingerprinting %s %q plugin: %w", kind, name, err)
	}
	if entry.Fingerprint != fingerprint || entry.Ref != plugin.Ref {
		return fmt.Errorf("prepared artifact for %s %q is missing or stale; run `gestaltd prepare --config %s`", kind, name, paths.configPath)
	}

	manifestPath := resolveProviderPath(paths.configDir, entry.Manifest)
	executablePath := resolveProviderPath(paths.configDir, entry.Executable)
	if _, err := os.Stat(manifestPath); err != nil {
		return fmt.Errorf("prepared manifest for %s %q not found at %s; run `gestaltd prepare --config %s`", kind, name, manifestPath, paths.configPath)
	}
	if _, err := os.Stat(executablePath); err != nil {
		return fmt.Errorf("prepared executable for %s %q not found at %s; run `gestaltd prepare --config %s`", kind, name, executablePath, paths.configPath)
	}

	_, manifest, err := pluginpkg.ReadManifestFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read prepared manifest for %s %q: %w", kind, name, err)
	}
	args, err := preparedEntrypointArgs(kind, manifest)
	if err != nil {
		return fmt.Errorf("resolve entrypoint for %s %q: %w", kind, name, err)
	}

	plugin.Command = executablePath
	plugin.Args = append([]string(nil), args...)
	return nil
}

func preparedEntrypointArgs(kind string, manifest *pluginmanifestv1.Manifest) ([]string, error) {
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

func integrationFingerprint(name string, intg config.IntegrationDef, upstream config.UpstreamDef) (string, error) {
	input := preparedFingerprintInput{
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
		preparedFingerprintInput
	}{
		Name:                     name,
		preparedFingerprintInput: input,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}
