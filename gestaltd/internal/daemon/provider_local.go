package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/operator"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
	"github.com/valon-technologies/gestalt/server/services/apps/source"
	"gopkg.in/yaml.v3"
)

const (
	providerLocalHost          = "127.0.0.1"
	providerLocalIndexedDBName = "main"
)

type providerLocalCommandOptions struct {
	Paths           []string
	ConfigPaths     []string
	Port            int
	Locked          bool
	NoSync          bool
	Remote          string
	RemoteToken     string
	Dev             bool
	ArtifactsDir    string
	LockfilePath    string
	GestaltdVersion string
	// FleetOverlay is true for serve PATH arguments with --config. Validate always leaves
	// this false so it keeps the synthesized baseline even with --config.
	FleetOverlay bool
}

type providerLocalSession struct {
	Dir               string
	Kind              string
	ManifestPath      string
	TargetKey         string
	TargetKeys        []string
	DevAppKeys        []string
	ConfigPaths       []string
	LockfilePath      string
	ArtifactsDir      string
	Locked            bool
	NoSync            bool
	PublicPort        int
	PublicURL         string
	AdminURL          string
	PublicUIPaths     []string
	AutoMountedUIPath string
}

type providerLocalTargetOverlay struct {
	overlayPath  string
	targetKey    string
	devAppKey    string
	kind         string
	manifestPath string
	mountPath    string
}

func runProviderValidate(args []string) error {
	fs := flag.NewFlagSet("gestaltd provider validate", flag.ContinueOnError)
	fs.Usage = func() { printProviderValidateUsage(fs.Output()) }
	var configPaths repeatedStringFlag
	fs.Var(&configPaths, "config", "path to config file (repeat to layer overrides)")
	pathFlag := fs.String("path", "", "provider manifest path or directory (defaults to current working directory)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	session, err := prepareProviderLocalSession(providerLocalCommandOptions{
		Paths:       []string{*pathFlag},
		ConfigPaths: []string(configPaths),
		Port:        8080,
	})
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(session.Dir) }()

	scratchArtifactsDir, err := os.MkdirTemp("", "gestaltd-provider-validate-*")
	if err != nil {
		return fmt.Errorf("create validation scratch dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(scratchArtifactsDir) }()

	result, err := validateConfigAtPaths(session.ConfigPaths, session.LockfilePath, scratchArtifactsDir, validateConfigOptions{Runtime: true})
	if err != nil {
		return err
	}

	logProviderLocalSummary("provider validated", session)
	logConfigSummary(result.Paths, result.Config)
	for _, warning := range result.Warnings {
		currentCLIReporter().Warning(warning)
	}
	currentCLIReporter().Status("config ok")
	return nil
}

func runServeProviderLocal(opts providerLocalCommandOptions) error {
	opts.FleetOverlay = len(opts.ConfigPaths) > 0
	session, err := prepareProviderLocalSession(opts)
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(session.Dir) }()

	var forcedDevAppKeys []string
	if session.Locked {
		forcedDevAppKeys = session.DevAppKeys
	}
	env, err := setupBootstrapWithConfigPaths(session.ConfigPaths, session.LockfilePath, session.ArtifactsDir, session.Locked, session.NoSync, opts.Remote, opts.RemoteToken, opts.Dev, forcedDevAppKeys...)
	if err != nil {
		return err
	}
	if err := resolveServePort(env.Config, session.PublicPort); err != nil {
		env.Close()
		return err
	}
	session.PublicURL = providerLocalPublicURL(env.Config)
	session.AdminURL = strings.TrimRight(session.PublicURL, "/") + "/admin/"
	return runServerWithReady(env, opts.GestaltdVersion, func() {
		logProviderLocalSummary("local provider ready", session)
	})
}

func buildProviderLocalTargetOverlay(sessionDir string, index int, opts providerLocalCommandOptions, path string, port int, configPathsSoFar []string) (*providerLocalTargetOverlay, error) {
	manifestPath, manifest, err := resolveProviderTargetManifest(path)
	if err != nil {
		return nil, err
	}
	kind, err := providerpkg.ManifestKind(manifest)
	if err != nil {
		return nil, err
	}
	targetManifestPath, err := canonicalPath(manifestPath)
	if err != nil {
		return nil, err
	}
	if kind != providermanifestv1.KindApp {
		if kind == "ui" {
			appKey := derivedPluginKey(manifest, targetManifestPath)
			if appKey == "" {
				appKey = "<name>"
			}
			return nil, fmt.Errorf("kind: ui is no longer supported for gestaltd serve --path; configure apps.%s.static instead", appKey)
		}
		return nil, fmt.Errorf("gestaltd serve PATH arguments and provider validate only support kind: app in v1 (got %q)", kind)
	}

	overlayPath := filepath.Join(sessionDir, fmt.Sprintf("provider-target-%d.yaml", index))
	result := &providerLocalTargetOverlay{
		overlayPath:  overlayPath,
		kind:         kind,
		manifestPath: targetManifestPath,
	}

	resolvedKey, err := resolveProviderPluginKey(opts.ConfigPaths, targetManifestPath, manifest, loadConfiguredPlugins)
	if err != nil {
		return nil, err
	}
	mountPath, err := resolveProviderLocalAppMount(configPathsSoFar, resolvedKey, targetManifestPath, manifest)
	if err != nil {
		return nil, err
	}
	if err := writeProviderLocalPluginOverlayConfig(overlayPath, resolvedKey, targetManifestPath, port, mountPath); err != nil {
		return nil, err
	}
	result.targetKey = resolvedKey
	result.mountPath = mountPath
	if providerpkg.HasExplicitSourceRun(manifest) {
		result.devAppKey = resolvedKey
	}
	return result, nil
}

func resolveProviderLocalAppMount(configPaths []string, resolvedKey, targetManifestPath string, manifest *providermanifestv1.Manifest) (mountPath string, err error) {
	loadedCfg, loadErr := config.LoadPaths(configPaths)
	if loadErr != nil {
		return "", fmt.Errorf("load provider overlay config: %w", loadErr)
	}
	// Compute a default mount from the manifest so apps not present in the
	// loaded config (e.g. PATH-arg dev sessions without --config) still get
	// a mount path for the tunnel UI handler's BasePath. Only do this for
	// hybrid apps that have a UI run command (more than one run command);
	// pure API providers have no UI to mount.
	mountPath = ""
	if commands, err := providerpkg.SourceRunCommands(targetManifestPath); err == nil && len(commands) > 1 {
		mountPath = defaultProviderLocalMountPath(manifest, targetManifestPath, resolvedKey)
	}
	if entry := loadedCfg.Apps[resolvedKey]; entry != nil && entry.Static != nil {
		if configured := strings.TrimSpace(entry.Static.Mount); configured != "" {
			mountPath = configured
		}
	}
	if err := ensureNoPublicStaticPathCollision(loadedCfg, resolvedKey, mountPath); err != nil {
		return "", err
	}
	return mountPath, nil
}

type validatedConfigResult struct {
	Paths    []string
	Config   *config.Config
	Warnings []string
}

func validateConfigAtPaths(configFlags []string, lockfilePath, artifactsDir string, opts validateConfigOptions) (*validatedConfigResult, error) {
	paths, cfg, err := loadConfigForValidation(configFlags, lockfilePath, artifactsDir, opts)
	if err != nil {
		return nil, err
	}

	var warnings []string
	if opts.Runtime {
		var err error
		warnings, err = bootstrap.Validate(context.Background(), cfg, buildFactories())
		if err != nil {
			return nil, err
		}
	}

	return &validatedConfigResult{
		Paths:    paths,
		Config:   cfg,
		Warnings: warnings,
	}, nil
}

func prepareProviderLocalSession(opts providerLocalCommandOptions) (*providerLocalSession, error) {
	if len(opts.Paths) == 0 {
		return nil, fmt.Errorf("at least one --path is required")
	}

	sessionDir, err := os.MkdirTemp("", "gestaltd-provider-*")
	if err != nil {
		return nil, fmt.Errorf("create provider session dir: %w", err)
	}
	cleanupSessionDir := true
	defer func() {
		if cleanupSessionDir {
			_ = os.RemoveAll(sessionDir)
		}
	}()

	var configPaths []string
	lockfilePath := opts.LockfilePath
	artifactsDir := opts.ArtifactsDir
	locked := opts.Locked && opts.FleetOverlay
	noSync := opts.NoSync && opts.FleetOverlay
	devPort := 0
	if opts.FleetOverlay {
		configPaths = append([]string(nil), opts.ConfigPaths...)
	} else {
		baseConfigPath := filepath.Join(sessionDir, "provider-base.yaml")
		dbPath := filepath.Join(sessionDir, "provider.db")
		if err := writeProviderLocalBaseConfig(baseConfigPath, dbPath); err != nil {
			return nil, err
		}
		configPaths = append([]string{baseConfigPath}, opts.ConfigPaths...)
		lockfilePath = filepath.Join(sessionDir, operator.LockfileName)
		artifactsDir = filepath.Join(sessionDir, "artifacts")
		locked = false
		selectedPort, err := reserveLocalPort()
		if err != nil {
			return nil, err
		}
		devPort = selectedPort
	}

	var (
		targetKeys  []string
		devAppKeys  []string
		publicPaths []string
		lastOverlay *providerLocalTargetOverlay
	)
	for i, path := range opts.Paths {
		overlay, err := buildProviderLocalTargetOverlay(sessionDir, i, opts, path, devPort, configPaths)
		if err != nil {
			return nil, err
		}
		configPaths = append(configPaths, overlay.overlayPath)
		targetKeys = append(targetKeys, overlay.targetKey)
		if overlay.devAppKey != "" && locked {
			devAppKeys = append(devAppKeys, overlay.devAppKey)
		}
		if overlay.mountPath != "" {
			publicPaths = append(publicPaths, overlay.mountPath)
		}
		lastOverlay = overlay
	}

	loadedCfg, err := config.LoadPaths(configPaths)
	if err != nil {
		return nil, fmt.Errorf("loading local provider config: %w", err)
	}
	publicUIPaths := mountedPublicUIPaths(loadedCfg)
	for _, mountPath := range publicPaths {
		if mountPath != "" && !slices.Contains(publicUIPaths, mountPath) {
			publicUIPaths = append(publicUIPaths, mountPath)
		}
	}
	slices.Sort(publicUIPaths)
	publicUIPaths = slices.Compact(publicUIPaths)

	session := &providerLocalSession{
		Dir:           sessionDir,
		TargetKeys:    targetKeys,
		DevAppKeys:    devAppKeys,
		ConfigPaths:   configPaths,
		LockfilePath:  lockfilePath,
		ArtifactsDir:  artifactsDir,
		Locked:        locked,
		NoSync:        noSync,
		PublicPort:    opts.Port,
		PublicURL:     providerLocalPublicURL(loadedCfg),
		AdminURL:      strings.TrimRight(providerLocalPublicURL(loadedCfg), "/") + "/admin/",
		PublicUIPaths: publicUIPaths,
	}
	if lastOverlay != nil {
		session.Kind = lastOverlay.kind
		session.ManifestPath = lastOverlay.manifestPath
		session.TargetKey = lastOverlay.targetKey
		session.AutoMountedUIPath = lastOverlay.mountPath
	}
	cleanupSessionDir = false
	return session, nil
}

func resolveProviderTargetManifest(pathFlag string) (string, *providermanifestv1.Manifest, error) {
	targetPath := pathFlag
	if strings.TrimSpace(targetPath) == "" {
		targetPath = "."
	}
	info, err := os.Stat(targetPath)
	if err != nil {
		return "", nil, err
	}

	manifestPath := targetPath
	if info.IsDir() {
		manifestPath, err = providerpkg.FindManifestFile(targetPath)
		if err != nil {
			return "", nil, err
		}
	} else if !providerpkg.IsManifestFile(targetPath) {
		return "", nil, fmt.Errorf("path %q must point to a provider manifest file or directory", targetPath)
	}

	_, manifest, err := providerpkg.ReadSourceManifestFile(manifestPath)
	if err != nil {
		return "", nil, err
	}
	return manifestPath, manifest, nil
}

func resolveProviderPluginKey(configPaths []string, targetManifestPath string, manifest *providermanifestv1.Manifest, loadApps func([]string) (map[string]*config.ProviderEntry, error)) (string, error) {
	plugins, err := loadApps(configPaths)
	if err != nil {
		return "", err
	}
	matchingKeys, err := matchingPluginKeys(plugins, targetManifestPath)
	if err != nil {
		return "", err
	}

	if len(matchingKeys) == 1 {
		return matchingKeys[0], nil
	}
	if len(matchingKeys) > 1 {
		return "", fmt.Errorf("target manifest is configured by multiple app keys (%s); remove the ambiguity in config", strings.Join(matchingKeys, ", "))
	}

	if name := derivedPluginKey(manifest, targetManifestPath); name != "" {
		if _, ok := plugins[name]; ok {
			return name, nil
		}
		return name, nil
	}
	return "", fmt.Errorf("unable to derive an app key for %s; set a manifest source app name or rename the directory", targetManifestPath)
}

func writeProviderLocalBaseConfig(path, dbPath string) error {
	encryptionKey, err := randomHex(32)
	if err != nil {
		return err
	}

	cfg := map[string]any{
		"apiVersion": config.ConfigAPIVersion,
		"server": map[string]any{
			"encryptionKey": encryptionKey,
			"providers": map[string]any{
				"externalCredentials": config.DefaultProviderInstance,
				"indexeddb":           providerLocalIndexedDBName,
			},
		},
		"providers": map[string]any{
			"externalCredentials": map[string]any{
				config.DefaultProviderInstance: map[string]any{
					"local":  true,
					"source": providerLocalExternalCredentialsSourceConfig(),
				},
			},
			"indexeddb": map[string]any{
				providerLocalIndexedDBName: map[string]any{
					"local":  true,
					"source": providerLocalIndexedDBSourceConfig(),
					"config": map[string]any{
						"dsn": "sqlite://" + dbPath,
					},
				},
			},
			"secrets": map[string]any{
				"env": map[string]any{
					"source": "env",
				},
			},
		},
	}
	return writeYAMLFile(path, cfg)
}

func writeProviderLocalPluginOverlayConfig(path, pluginKey, manifestPath string, port int, mountPath string) error {
	pluginEntry := map[string]any{
		"source":  providerLocalSourceOverride(manifestPath),
		"runtime": nil,
	}
	if mountPath != "" {
		pluginEntry["static"] = map[string]any{
			"mount": mountPath,
		}
	}

	cfg := map[string]any{
		"apiVersion": config.ConfigAPIVersion,
		"apps": map[string]any{
			pluginKey: pluginEntry,
		},
	}
	if port > 0 {
		cfg["server"] = map[string]any{
			"baseUrl": providerLocalBaseURL(port),
			"public": map[string]any{
				"host": providerLocalHost,
				"port": port,
			},
		}
	}
	return writeYAMLFile(path, cfg)
}

func providerLocalSourceOverride(manifestPath string) map[string]any {
	return map[string]any{
		"path":          manifestPath,
		"url":           nil,
		"githubRelease": nil,
		"git":           nil,
		"registry":      nil,
		"auth":          nil,
	}
}

func ensureNoPublicStaticPathCollision(cfg *config.Config, pluginKey, mountPath string) error {
	if cfg == nil || strings.TrimSpace(mountPath) == "" {
		return nil
	}
	for name, entry := range cfg.Apps {
		if entry == nil || name == pluginKey {
			continue
		}
		if entry.Static != nil && strings.TrimSpace(entry.Static.Mount) == mountPath {
			return fmt.Errorf("auto-mounted static path %q for apps.%s collides with apps.%s.static.mount", mountPath, pluginKey, name)
		}
	}
	return nil
}

func providerLocalIndexedDBSourceConfig() any {
	if providersDir := strings.TrimSpace(os.Getenv("GESTALT_PROVIDERS_DIR")); providersDir != "" {
		return map[string]any{
			"path": config.DefaultLocalProviderManifestPath(providersDir, config.DefaultIndexedDBProvider),
		}
	}
	return config.DefaultProviderMetadataURL(config.DefaultIndexedDBProvider, config.DefaultIndexedDBVersion)
}

func providerLocalExternalCredentialsSourceConfig() any {
	if providersDir := strings.TrimSpace(os.Getenv("GESTALT_PROVIDERS_DIR")); providersDir != "" {
		return map[string]any{
			"path": config.DefaultLocalProviderManifestPath(providersDir, config.DefaultExternalCredentialsProvider),
		}
	}
	return config.DefaultProviderMetadataURL(config.DefaultExternalCredentialsProvider, config.DefaultExternalCredentialsVersion)
}

func providerLocalPublicURL(cfg *config.Config) string {
	return canonicalPublicURL(cfg)
}

func providerLocalBaseURL(port int) string {
	return (&url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(providerLocalHost, fmt.Sprint(port)),
	}).String()
}

func mountedPublicUIPaths(cfg *config.Config) []string {
	if cfg == nil || len(cfg.Apps) == 0 {
		return nil
	}
	paths := make([]string, 0, len(cfg.Apps))
	for _, entry := range cfg.Apps {
		if entry == nil || entry.Static == nil || strings.TrimSpace(entry.Static.Mount) == "" {
			continue
		}
		paths = append(paths, strings.TrimSpace(entry.Static.Mount))
	}
	slices.Sort(paths)
	return slices.Compact(paths)
}

func logProviderLocalSummary(message string, session *providerLocalSession) {
	if session == nil {
		return
	}
	publicUIPaths := session.PublicUIPaths
	if len(publicUIPaths) == 0 {
		publicUIPaths = nil
	}
	detailArgs := []any{
		"config_files", session.ConfigPaths,
		"public_url", session.PublicURL,
		"admin_url", session.AdminURL,
		"mounted_ui_paths", publicUIPaths,
	}
	if session.Locked || len(session.DevAppKeys) > 0 || len(session.TargetKeys) > 0 {
		detailArgs = append(detailArgs,
			"dev_app_keys", session.DevAppKeys,
			"target_keys", session.TargetKeys,
			"artifacts_dir", session.ArtifactsDir,
			"lockfile", session.LockfilePath,
		)
	}
	if session.ManifestPath != "" {
		detailArgs = append(detailArgs,
			"kind", session.Kind,
			"manifest", session.ManifestPath,
			"auto_mounted_ui_path", session.AutoMountedUIPath,
			"app", session.TargetKey,
		)
	}
	if strings.Contains(message, "ready") {
		currentCLIReporter().Status(message + ": " + session.PublicURL)
	} else {
		currentCLIReporter().Status(message)
	}
	currentCLIReporter().Verbose(formatCLIFields(message+" details", detailArgs...))
}

func reserveLocalPort() (int, error) {
	listener, err := net.Listen("tcp", net.JoinHostPort(providerLocalHost, "0"))
	if err != nil {
		return 0, fmt.Errorf("reserve local provider port: %w", err)
	}
	defer func() { _ = listener.Close() }()
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, errors.New("reserve local provider port: unexpected listener address type")
	}
	return addr.Port, nil
}

func randomHex(numBytes int) (string, error) {
	key := make([]byte, numBytes)
	if _, err := rand.Read(key); err != nil {
		return "", fmt.Errorf("generate encryption key: %w", err)
	}
	return hex.EncodeToString(key), nil
}

func canonicalPath(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absPath)
	if err == nil {
		return resolved, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return filepath.Clean(absPath), nil
	}
	return "", err
}

func derivedPluginKey(manifest *providermanifestv1.Manifest, manifestPath string) string {
	if manifest != nil {
		if src, err := source.Parse(manifest.Source); err == nil {
			if name := strings.TrimSpace(src.AppName()); name != "" {
				return name
			}
		}
	}
	return sanitizeProviderLocalMountSlug(filepath.Base(filepath.Dir(manifestPath)))
}

func defaultProviderLocalMountPath(manifest *providermanifestv1.Manifest, manifestPath, fallbackKey string) string {
	if slug := derivedProviderLocalMountSlug(manifest, manifestPath); slug != "" {
		return "/" + slug
	}
	if slug := sanitizeProviderLocalMountSlug(fallbackKey); slug != "" {
		return "/" + slug
	}
	return "/" + fallbackKey
}

func derivedProviderLocalMountSlug(manifest *providermanifestv1.Manifest, manifestPath string) string {
	if manifest != nil {
		if src, err := source.Parse(manifest.Source); err == nil {
			if slug := strings.TrimSpace(src.AppName()); slug != "" {
				return slug
			}
		}
	}
	return sanitizeProviderLocalMountSlug(filepath.Base(filepath.Dir(manifestPath)))
}

func sanitizeProviderLocalMountSlug(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	var b strings.Builder
	previousSeparator := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			previousSeparator = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			previousSeparator = false
		case r == '-' || r == '_' || r == '.':
			if previousSeparator || b.Len() == 0 {
				continue
			}
			b.WriteRune(r)
			previousSeparator = true
		default:
			if previousSeparator || b.Len() == 0 {
				continue
			}
			b.WriteByte('-')
			previousSeparator = true
		}
	}
	return strings.Trim(b.String(), "-_.")
}

func loadConfiguredPlugins(configPaths []string) (map[string]*config.ProviderEntry, error) {
	if len(configPaths) == 0 {
		return map[string]*config.ProviderEntry{}, nil
	}
	cfg, err := config.LoadPaths(configPaths)
	if err != nil {
		return nil, fmt.Errorf("load provider overlay config: %w", err)
	}
	if cfg.Apps == nil {
		return map[string]*config.ProviderEntry{}, nil
	}
	return cfg.Apps, nil
}

func matchingPluginKeys(plugins map[string]*config.ProviderEntry, targetManifestPath string) ([]string, error) {
	targetCanonical, err := canonicalPath(targetManifestPath)
	if err != nil {
		return nil, err
	}

	var matches []string
	for name, entry := range plugins {
		if providerEntryMatchesTarget(entry, targetCanonical) {
			matches = append(matches, name)
		}
	}

	if len(matches) == 0 {
		if _, manifest, err := providerpkg.ReadSourceManifestFile(targetManifestPath); err == nil && manifest != nil {
			if src, err := source.Parse(manifest.Source); err == nil {
				appName := strings.TrimSpace(src.AppName())
				if appName != "" {
					if _, ok := plugins[appName]; ok && !slices.Contains(matches, appName) {
						matches = append(matches, appName)
					}
				}
			}
		}
	}

	slices.Sort(matches)
	return matches, nil
}

func providerEntryMatchesTarget(entry *config.ProviderEntry, targetManifestPath string) bool {
	if entry == nil {
		return false
	}
	targetCanonical, err := canonicalPath(targetManifestPath)
	if err != nil {
		return false
	}
	if entry.HasLocalSource() {
		canonicalSource, err := canonicalPath(entry.SourcePath())
		if err != nil {
			return false
		}
		return canonicalSource == targetCanonical
	}
	if entry.HasGitSource() && entry.Source.Git != nil {
		_, _, gitManifestPath := entry.Source.Git.NormalizedLocationParts()
		return manifestPathSuffixMatch(gitManifestPath, targetCanonical)
	}
	return false
}

func manifestPathSuffixMatch(configuredPath, targetPath string) bool {
	configuredPath = normalizeManifestPathCompare(configuredPath)
	targetPath = normalizeManifestPathCompare(targetPath)
	if configuredPath == "" || targetPath == "" {
		return false
	}
	if targetPath == configuredPath {
		return true
	}
	configuredElems := strings.Split(configuredPath, "/")
	targetElems := strings.Split(targetPath, "/")
	if len(targetElems) < len(configuredElems) {
		return false
	}
	for i := range configuredElems {
		if configuredElems[len(configuredElems)-1-i] != targetElems[len(targetElems)-1-i] {
			return false
		}
	}
	return true
}

func normalizeManifestPathCompare(raw string) string {
	return path.Clean(filepath.ToSlash(strings.TrimSpace(raw)))
}

func writeYAMLFile(path string, value any) error {
	data, err := yaml.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func printProviderValidateUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd provider validate [--path PATH] [--config PATH]...")
	writeUsageLine(w, "")
	writeUsageLine(w, "Validate a local source app inside a synthesized Gestalt config.")
	writeUsageLine(w, "v1 supports kind: app manifests.")
	writeUsageLine(w, "Repeated --config flags merge left-to-right using the normal Gestalt rules.")
	writeUsageLine(w, "")
	writeUsageLine(w, "Flags:")
	writeUsageLine(w, "  --path     Provider manifest path or directory (default: current working directory)")
	writeUsageLine(w, "  --config   Additional config file to merge; repeat to add support providers or null deletions")
}
