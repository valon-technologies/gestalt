package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"os"
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
	providerLocalPluginDir     = "app"
	providerLocalSiblingUI     = "ui"
)

type providerLocalCommandOptions struct {
	Paths        []string
	ConfigPaths  []string
	Port         int
	Locked       bool
	NoSync       bool
	ArtifactsDir string
	LockfilePath string
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
	DevUIKeys         []string
	ConfigPaths       []string
	State             operator.StatePaths
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
	devUIKey     string
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

	result, err := validateConfigWithStatePaths(session.ConfigPaths, session.State, validateConfigOptions{Runtime: true})
	if err != nil {
		return err
	}

	logProviderLocalSummary("provider validated", session)
	logConfigSummary(result.Paths, result.Config)
	for _, warning := range result.Warnings {
		slog.Warn(warning)
	}
	slog.Info("config ok")
	return nil
}

func runServeProviderLocal(opts providerLocalCommandOptions) error {
	opts.FleetOverlay = len(opts.ConfigPaths) > 0
	session, err := prepareProviderLocalSession(opts)
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(session.Dir) }()

	var forcedDevUIKeys []string
	if session.Locked {
		forcedDevUIKeys = session.DevUIKeys
	}
	env, err := setupBootstrapWithConfigPaths(session.ConfigPaths, session.State, session.Locked, session.NoSync, forcedDevUIKeys...)
	if err != nil {
		return err
	}
	if err := resolveServePort(env.Config, session.PublicPort); err != nil {
		env.Close()
		return err
	}
	session.PublicURL = providerLocalPublicURL(env.Config)
	session.AdminURL = strings.TrimRight(session.PublicURL, "/") + "/admin/"
	logProviderLocalSummary("local provider ready", session)
	return runServer(env)
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
	if kind != providermanifestv1.KindApp && kind != providermanifestv1.KindUI {
		return nil, fmt.Errorf("gestaltd serve PATH arguments and provider validate only support kind: app or ui in v1 (got %q)", kind)
	}
	targetManifestPath, err := canonicalPath(manifestPath)
	if err != nil {
		return nil, err
	}

	overlayPath := filepath.Join(sessionDir, fmt.Sprintf("provider-target-%d.yaml", index))
	result := &providerLocalTargetOverlay{
		overlayPath:  overlayPath,
		kind:         kind,
		manifestPath: targetManifestPath,
	}

	switch kind {
	case providermanifestv1.KindUI:
		resolvedKey, err := resolveProviderLocalUIKey(opts.ConfigPaths, targetManifestPath, manifest)
		if err != nil {
			return nil, err
		}
		mountPath := defaultProviderLocalMountPath(manifest, targetManifestPath, resolvedKey)
		if configuredUIs, loadErr := loadConfiguredUIs(opts.ConfigPaths); loadErr == nil {
			if entry := configuredUIs[resolvedKey]; entry != nil && strings.TrimSpace(entry.Path) != "" {
				mountPath = strings.TrimSpace(entry.Path)
			}
		}
		if err := writeProviderLocalUIOverlayConfig(overlayPath, resolvedKey, targetManifestPath, port, mountPath); err != nil {
			return nil, err
		}
		result.targetKey = resolvedKey
		result.devUIKey = resolvedKey
		result.mountPath = mountPath
		return result, nil
	case providermanifestv1.KindApp:
		resolvedKey, err := resolveProviderPluginKey(opts.ConfigPaths, targetManifestPath, manifest, loadConfiguredPlugins)
		if err != nil {
			return nil, err
		}
		autoMountPath, uiName, uiManifestPath, err := resolveProviderLocalAppMount(configPathsSoFar, resolvedKey, targetManifestPath, manifest)
		if err != nil {
			return nil, err
		}
		if err := writeProviderLocalPluginOverlayConfig(overlayPath, resolvedKey, targetManifestPath, port, autoMountPath, uiName, uiManifestPath); err != nil {
			return nil, err
		}
		result.targetKey = resolvedKey
		result.mountPath = autoMountPath
		return result, nil
	default:
		return nil, fmt.Errorf("unsupported provider local target kind %q", kind)
	}
}

func resolveProviderLocalAppMount(configPaths []string, resolvedKey, targetManifestPath string, manifest *providermanifestv1.Manifest) (autoMountPath, uiName, uiManifestPath string, err error) {
	siblingUIManifestPath, err := findSiblingUIManifestPath(targetManifestPath, manifest)
	if err != nil {
		return "", "", "", err
	}
	loadedCfg, loadErr := config.LoadPaths(configPaths)
	if loadErr != nil {
		return "", "", "", fmt.Errorf("load provider overlay config: %w", loadErr)
	}
	switch {
	case shouldAutoMountOwnedUI(loadedCfg, resolvedKey, manifest):
		autoMountPath = defaultProviderLocalMountPath(manifest, targetManifestPath, resolvedKey)
		if err := ensureNoPublicUIPathCollision(loadedCfg, resolvedKey, autoMountPath); err != nil {
			return "", "", "", err
		}
	case siblingUIManifestPath != "":
		if entry := loadedCfg.Apps[resolvedKey]; entry != nil {
			uiName = strings.TrimSpace(entry.UI)
			autoMountPath = strings.TrimSpace(entry.MountPath)
		}
		if uiName == "" {
			uiName = resolvedKey
		}
		if autoMountPath == "" {
			autoMountPath = defaultProviderLocalMountPath(manifest, targetManifestPath, resolvedKey)
			if err := ensureNoPublicUIPathCollision(loadedCfg, resolvedKey, autoMountPath); err != nil {
				return "", "", "", err
			}
		}
		uiManifestPath = siblingUIManifestPath
	}
	return autoMountPath, uiName, uiManifestPath, nil
}

type validatedConfigResult struct {
	Paths    []string
	Config   *config.Config
	Warnings []string
}

func validateConfigWithStatePaths(configFlags []string, state operator.StatePaths, opts validateConfigOptions) (*validatedConfigResult, error) {
	paths, cfg, err := loadConfigForValidationWithStatePaths(configFlags, state, opts)
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
	state := operator.StatePaths{
		ArtifactsDir: opts.ArtifactsDir,
		LockfilePath: opts.LockfilePath,
	}
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
		state = operator.StatePaths{
			ArtifactsDir: filepath.Join(sessionDir, "artifacts"),
			LockfilePath: filepath.Join(sessionDir, "gestalt.lock.json"),
		}
		locked = false
		selectedPort, err := reserveLocalPort()
		if err != nil {
			return nil, err
		}
		devPort = selectedPort
	}

	var (
		targetKeys  []string
		devUIKeys   []string
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
		if overlay.devUIKey != "" && locked {
			devUIKeys = append(devUIKeys, overlay.devUIKey)
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
		DevUIKeys:     devUIKeys,
		ConfigPaths:   configPaths,
		State:         state,
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
					"source": providerLocalExternalCredentialsSourceConfig(),
				},
			},
			"indexeddb": map[string]any{
				providerLocalIndexedDBName: map[string]any{
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

func writeProviderLocalPluginOverlayConfig(path, pluginKey, manifestPath string, port int, mountPath, uiName, uiManifestPath string) error {
	pluginEntry := map[string]any{
		"source":  providerLocalSourceOverride(manifestPath),
		"runtime": nil,
	}
	if mountPath != "" || uiName != "" {
		ui := map[string]any{}
		if mountPath != "" {
			ui["path"] = mountPath
		}
		if uiName != "" {
			ui["bundle"] = uiName
		}
		pluginEntry["ui"] = ui
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
	if uiName != "" && uiManifestPath != "" {
		cfg["providers"] = map[string]any{
			"ui": map[string]any{
				uiName: map[string]any{
					"source": providerLocalSourceOverride(uiManifestPath),
				},
			},
		}
	}
	return writeYAMLFile(path, cfg)
}

func writeProviderLocalUIOverlayConfig(path, uiKey, manifestPath string, port int, mountPath string) error {
	uiEntry := map[string]any{
		"source": providerLocalSourceOverride(manifestPath),
	}
	if mountPath != "" {
		uiEntry["path"] = mountPath
	}

	cfg := map[string]any{
		"apiVersion": config.ConfigAPIVersion,
		"providers": map[string]any{
			"ui": map[string]any{
				uiKey: uiEntry,
			},
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
		"auth":          nil,
	}
}

func shouldAutoMountOwnedUI(cfg *config.Config, pluginKey string, manifest *providermanifestv1.Manifest) bool {
	if cfg == nil || manifest == nil || manifest.Spec == nil || manifest.Spec.UI == nil {
		return false
	}
	entry := cfg.Apps[pluginKey]
	if entry == nil {
		return true
	}
	return strings.TrimSpace(entry.MountPath) == "" && strings.TrimSpace(entry.UI) == ""
}

func ensureNoPublicUIPathCollision(cfg *config.Config, pluginKey, mountPath string) error {
	if cfg == nil {
		return nil
	}
	for name, entry := range cfg.Providers.UI {
		if entry == nil || strings.TrimSpace(entry.Path) == "" {
			continue
		}
		if strings.TrimSpace(entry.Path) != mountPath {
			continue
		}
		if entry.OwnerApp == pluginKey || name == pluginKey {
			continue
		}
		return fmt.Errorf("auto-mounted ui path %q for apps.%s collides with providers.ui.%s", mountPath, pluginKey, name)
	}
	for name, entry := range cfg.Apps {
		if entry == nil || name == pluginKey {
			continue
		}
		if strings.TrimSpace(entry.MountPath) == mountPath {
			return fmt.Errorf("auto-mounted ui path %q for apps.%s collides with apps.%s.ui.path", mountPath, pluginKey, name)
		}
	}
	return nil
}

func findSiblingUIManifestPath(pluginManifestPath string, manifest *providermanifestv1.Manifest) (string, error) {
	if manifest == nil || manifest.Spec == nil || manifest.Spec.UI != nil {
		return "", nil
	}
	pluginDir := filepath.Dir(pluginManifestPath)
	for filepath.Base(pluginDir) != providerLocalPluginDir {
		parentDir := filepath.Dir(pluginDir)
		if parentDir == pluginDir {
			return "", nil
		}
		pluginDir = parentDir
	}

	uiDir := filepath.Join(filepath.Dir(pluginDir), providerLocalSiblingUI)
	info, err := os.Stat(uiDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("stat sibling ui dir %s: %w", uiDir, err)
	}
	if !info.IsDir() {
		return "", nil
	}

	uiManifestPath, err := providerpkg.FindManifestFile(uiDir)
	if err != nil {
		return "", fmt.Errorf("find sibling ui manifest: %w", err)
	}
	uiManifestPath, err = canonicalPath(uiManifestPath)
	if err != nil {
		return "", err
	}

	_, uiManifest, err := providerpkg.ReadSourceManifestFile(uiManifestPath)
	if err != nil {
		return "", err
	}
	kind, err := providerpkg.ManifestKind(uiManifest)
	if err != nil {
		return "", err
	}
	if kind != providermanifestv1.KindUI {
		return "", fmt.Errorf("sibling ui manifest %q must have kind %q (got %q)", uiManifestPath, providermanifestv1.KindUI, kind)
	}
	return uiManifestPath, nil
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
	if cfg == nil {
		return ""
	}
	addr := cfg.Server.PublicAddr()
	if addr == "" {
		return ""
	}
	return (&url.URL{Scheme: "http", Host: addr}).String()
}

func providerLocalBaseURL(port int) string {
	return (&url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(providerLocalHost, fmt.Sprint(port)),
	}).String()
}

func mountedPublicUIPaths(cfg *config.Config) []string {
	if cfg == nil || len(cfg.Providers.UI) == 0 {
		return nil
	}
	paths := make([]string, 0, len(cfg.Providers.UI))
	for _, entry := range cfg.Providers.UI {
		if entry == nil || strings.TrimSpace(entry.Path) == "" {
			continue
		}
		paths = append(paths, strings.TrimSpace(entry.Path))
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
	args := []any{
		"config_files", session.ConfigPaths,
		"public_url", session.PublicURL,
		"admin_url", session.AdminURL,
		"mounted_ui_paths", publicUIPaths,
	}
	if session.Locked || len(session.DevUIKeys) > 0 || len(session.TargetKeys) > 0 {
		args = append(args,
			"dev_ui_keys", session.DevUIKeys,
			"target_keys", session.TargetKeys,
			"artifacts_dir", session.State.ArtifactsDir,
			"lockfile", session.State.LockfilePath,
		)
	}
	if session.ManifestPath != "" {
		args = append(args,
			"kind", session.Kind,
			"manifest", session.ManifestPath,
			"auto_mounted_ui_path", session.AutoMountedUIPath,
		)
		switch session.Kind {
		case providermanifestv1.KindUI:
			args = append(args, "ui", session.TargetKey)
		default:
			args = append(args, "app", session.TargetKey)
		}
	}
	slog.Info(message, args...)
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
			if name := sanitizeDerivedPluginKey(src.AppName()); name != "" {
				return name
			}
		}
	}
	return sanitizeDerivedPluginKey(filepath.Base(filepath.Dir(manifestPath)))
}

func sanitizeDerivedPluginKey(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	var b strings.Builder
	previousUnderscore := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			previousUnderscore = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			previousUnderscore = false
		default:
			if previousUnderscore || b.Len() == 0 {
				continue
			}
			b.WriteByte('_')
			previousUnderscore = true
		}
	}
	return strings.Trim(b.String(), "_")
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

func resolveProviderLocalUIKey(configPaths []string, targetManifestPath string, manifest *providermanifestv1.Manifest) (string, error) {
	uis, err := loadConfiguredUIs(configPaths)
	if err != nil {
		return "", err
	}
	matchingKeys, err := matchingUIKeys(uis, targetManifestPath)
	if err != nil {
		return "", err
	}

	if len(matchingKeys) == 1 {
		return matchingKeys[0], nil
	}
	if len(matchingKeys) > 1 {
		return "", fmt.Errorf("target manifest is configured by multiple ui keys (%s); remove the ambiguity in config", strings.Join(matchingKeys, ", "))
	}

	if name := derivedPluginKey(manifest, targetManifestPath); name != "" {
		return name, nil
	}
	return "", fmt.Errorf("unable to derive a ui key for %s; set a manifest source app name or rename the directory", targetManifestPath)
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

func loadConfiguredUIs(configPaths []string) (map[string]*config.UIEntry, error) {
	if len(configPaths) == 0 {
		return map[string]*config.UIEntry{}, nil
	}
	cfg, err := config.LoadPaths(configPaths)
	if err != nil {
		return nil, fmt.Errorf("load provider overlay config: %w", err)
	}
	if cfg.Providers.UI == nil {
		return map[string]*config.UIEntry{}, nil
	}
	return cfg.Providers.UI, nil
}

func matchingPluginKeys(plugins map[string]*config.ProviderEntry, targetManifestPath string) ([]string, error) {
	targetCanonical, err := canonicalPath(targetManifestPath)
	if err != nil {
		return nil, err
	}

	var matches []string
	for name, entry := range plugins {
		if !providerEntryMatchesTarget(entry, targetCanonical) {
			continue
		}
		matches = append(matches, name)
	}
	slices.Sort(matches)
	return matches, nil
}

func matchingUIKeys(entries map[string]*config.UIEntry, targetManifestPath string) ([]string, error) {
	targetCanonical, err := canonicalPath(targetManifestPath)
	if err != nil {
		return nil, err
	}

	var matches []string
	for name, entry := range entries {
		if !uiEntryMatchesTarget(entry, targetCanonical) {
			continue
		}
		matches = append(matches, name)
	}
	slices.Sort(matches)
	return matches, nil
}

func providerEntryMatchesTarget(entry *config.ProviderEntry, targetManifestPath string) bool {
	if entry == nil || !entry.HasLocalSource() {
		return false
	}
	canonicalSource, err := canonicalPath(entry.SourcePath())
	if err != nil {
		return false
	}
	targetCanonical, err := canonicalPath(targetManifestPath)
	if err != nil {
		return false
	}
	return canonicalSource == targetCanonical
}

func uiEntryMatchesTarget(entry *config.UIEntry, targetManifestPath string) bool {
	if entry == nil || !entry.HasLocalSource() {
		return false
	}
	canonicalSource, err := canonicalPath(entry.SourcePath())
	if err != nil {
		return false
	}
	targetCanonical, err := canonicalPath(targetManifestPath)
	if err != nil {
		return false
	}
	return canonicalSource == targetCanonical
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
	writeUsageLine(w, "Validate a local source app or ui inside a synthesized Gestalt config.")
	writeUsageLine(w, "v1 supports kind: app and kind: ui manifests.")
	writeUsageLine(w, "Repeated --config flags merge left-to-right using the normal Gestalt rules.")
	writeUsageLine(w, "")
	writeUsageLine(w, "Flags:")
	writeUsageLine(w, "  --path     Provider manifest path or directory (default: current working directory)")
	writeUsageLine(w, "  --config   Additional config file to merge; repeat to add support providers or null deletions")
}
