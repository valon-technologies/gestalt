package main

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
	"github.com/valon-technologies/gestalt/server/internal/pluginsource"
	"github.com/valon-technologies/gestalt/server/internal/providerpkg"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"gopkg.in/yaml.v3"
)

const (
	providerDevHost          = "127.0.0.1"
	providerDevIndexedDBName = "main"
)

type providerLocalCommandOptions struct {
	Path        string
	ConfigPaths []string
	Name        string
	Port        int
}

type providerLocalSession struct {
	Dir               string
	ManifestPath      string
	PluginKey         string
	ConfigPaths       []string
	State             operator.StatePaths
	PublicURL         string
	AdminURL          string
	PublicUIPaths     []string
	AutoMountedUIPath string
}

func runProviderValidate(args []string) error {
	fs := flag.NewFlagSet("gestaltd provider validate", flag.ContinueOnError)
	fs.Usage = func() { printProviderValidateUsage(fs.Output()) }
	var configPaths repeatedStringFlag
	fs.Var(&configPaths, "config", "path to config file (repeat to layer overrides)")
	pathFlag := fs.String("path", "", "provider manifest path or directory (defaults to current working directory)")
	nameFlag := fs.String("name", "", "plugin key override")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	session, err := prepareProviderLocalSession(providerLocalCommandOptions{
		Path:        *pathFlag,
		ConfigPaths: []string(configPaths),
		Name:        *nameFlag,
		Port:        8080,
	})
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(session.Dir) }()

	result, err := validateConfigWithStatePaths(session.ConfigPaths, session.State)
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

func runProviderDev(args []string) error {
	fs := flag.NewFlagSet("gestaltd provider dev", flag.ContinueOnError)
	fs.Usage = func() { printProviderDevUsage(fs.Output()) }
	var configPaths repeatedStringFlag
	fs.Var(&configPaths, "config", "path to config file (repeat to layer overrides)")
	pathFlag := fs.String("path", "", "provider manifest path or directory (defaults to current working directory)")
	nameFlag := fs.String("name", "", "plugin key override")
	portFlag := fs.Int("port", 0, "public port (defaults to a free localhost port)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	port := *portFlag
	if port == 0 {
		selectedPort, err := reserveLocalPort()
		if err != nil {
			return err
		}
		port = selectedPort
	}

	session, err := prepareProviderLocalSession(providerLocalCommandOptions{
		Path:        *pathFlag,
		ConfigPaths: []string(configPaths),
		Name:        *nameFlag,
		Port:        port,
	})
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(session.Dir) }()

	env, err := setupBootstrapWithConfigPaths(session.ConfigPaths, session.State, false)
	if err != nil {
		return err
	}
	logProviderLocalSummary("provider dev ready", session)
	return runServer(env)
}

type validatedConfigResult struct {
	Paths    []string
	Config   *config.Config
	Warnings []string
}

func validateConfigWithStatePaths(configFlags []string, state operator.StatePaths) (*validatedConfigResult, error) {
	paths, cfg, err := loadConfigForValidationWithStatePaths(configFlags, state)
	if err != nil {
		return nil, err
	}

	warnings, err := bootstrap.Validate(context.Background(), cfg, buildFactories())
	if err != nil {
		return nil, err
	}

	return &validatedConfigResult{
		Paths:    paths,
		Config:   cfg,
		Warnings: warnings,
	}, nil
}

func prepareProviderLocalSession(opts providerLocalCommandOptions) (*providerLocalSession, error) {
	manifestPath, manifest, err := resolveProviderTargetManifest(opts.Path)
	if err != nil {
		return nil, err
	}

	kind, err := providerpkg.ManifestKind(manifest)
	if err != nil {
		return nil, err
	}
	if kind != providermanifestv1.KindPlugin {
		return nil, fmt.Errorf("gestaltd provider dev and validate only support kind: plugin in v1 (got %q)", kind)
	}

	targetManifestPath, err := canonicalPath(manifestPath)
	if err != nil {
		return nil, err
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

	baseConfigPath := filepath.Join(sessionDir, "provider-base.yaml")
	dbPath := filepath.Join(sessionDir, "provider.db")
	if err := writeProviderLocalBaseConfig(baseConfigPath, dbPath); err != nil {
		return nil, err
	}

	resolvedKey, err := resolveProviderLocalPluginKey(opts.ConfigPaths, targetManifestPath, manifest, opts.Name)
	if err != nil {
		return nil, err
	}

	overlayConfigPath := filepath.Join(sessionDir, "provider-target.yaml")
	autoMountPath := ""
	if err := writeProviderLocalOverlayConfig(overlayConfigPath, resolvedKey, targetManifestPath, opts.Port, ""); err != nil {
		return nil, err
	}

	configPaths := append([]string{baseConfigPath}, opts.ConfigPaths...)
	configPaths = append(configPaths, overlayConfigPath)

	loadedCfg, err := config.LoadPaths(configPaths)
	if err != nil {
		return nil, fmt.Errorf("loading provider dev config: %w", err)
	}

	if shouldAutoMountOwnedUI(loadedCfg, resolvedKey, manifest) {
		autoMountPath = "/" + resolvedKey
		if err := ensureNoPublicUIPathCollision(loadedCfg, resolvedKey, autoMountPath); err != nil {
			return nil, err
		}
		if err := writeProviderLocalOverlayConfig(overlayConfigPath, resolvedKey, targetManifestPath, opts.Port, autoMountPath); err != nil {
			return nil, err
		}
		configPaths[len(configPaths)-1] = overlayConfigPath
		loadedCfg, err = config.LoadPaths(configPaths)
		if err != nil {
			return nil, fmt.Errorf("loading provider dev config with auto-mounted ui: %w", err)
		}
	}

	publicURL := providerLocalPublicURL(loadedCfg)
	publicUIPaths := mountedPublicUIPaths(loadedCfg)
	if autoMountPath != "" && !slices.Contains(publicUIPaths, autoMountPath) {
		publicUIPaths = append(publicUIPaths, autoMountPath)
		slices.Sort(publicUIPaths)
	}
	cleanupSessionDir = false
	return &providerLocalSession{
		Dir:               sessionDir,
		ManifestPath:      targetManifestPath,
		PluginKey:         resolvedKey,
		ConfigPaths:       configPaths,
		State:             operator.StatePaths{ArtifactsDir: filepath.Join(sessionDir, "artifacts"), LockfilePath: filepath.Join(sessionDir, "gestalt.lock.json")},
		PublicURL:         publicURL,
		AdminURL:          strings.TrimRight(publicURL, "/") + "/admin/",
		PublicUIPaths:     publicUIPaths,
		AutoMountedUIPath: autoMountPath,
	}, nil
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

func resolveProviderLocalPluginKey(configPaths []string, targetManifestPath string, manifest *providermanifestv1.Manifest, explicitName string) (string, error) {
	plugins, err := loadConfiguredPlugins(configPaths)
	if err != nil {
		return "", err
	}
	matchingKeys, err := matchingPluginKeys(plugins, targetManifestPath)
	if err != nil {
		return "", err
	}

	if explicitName != "" {
		if !isValidExplicitPluginKey(explicitName) {
			return "", fmt.Errorf("invalid --name %q: use only letters, numbers, and underscores", explicitName)
		}
		if len(matchingKeys) == 1 && matchingKeys[0] != explicitName {
			return "", fmt.Errorf("target manifest is already configured as plugins.%s; pass --name %q or remove the conflicting config entry", matchingKeys[0], matchingKeys[0])
		}
		if len(matchingKeys) > 1 && !slices.Contains(matchingKeys, explicitName) {
			return "", fmt.Errorf("target manifest is configured by multiple plugin keys (%s); remove the ambiguity before using --name", strings.Join(matchingKeys, ", "))
		}
		return explicitName, nil
	}

	if len(matchingKeys) == 1 {
		return matchingKeys[0], nil
	}
	if len(matchingKeys) > 1 {
		return "", fmt.Errorf("target manifest is configured by multiple plugin keys (%s); pass --name to choose the target key", strings.Join(matchingKeys, ", "))
	}

	if name := derivedPluginKey(manifest, targetManifestPath); name != "" {
		if _, ok := plugins[name]; ok {
			return name, nil
		}
		return name, nil
	}
	return "", fmt.Errorf("unable to derive a plugin key for %s; pass --name", targetManifestPath)
}

func writeProviderLocalBaseConfig(path, dbPath string) error {
	encryptionKey, err := randomHex(32)
	if err != nil {
		return err
	}

	cfg := map[string]any{
		"server": map[string]any{
			"encryptionKey": encryptionKey,
			"providers": map[string]any{
				"indexeddb": providerDevIndexedDBName,
			},
		},
		"providers": map[string]any{
			"indexeddb": map[string]any{
				providerDevIndexedDBName: map[string]any{
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

func writeProviderLocalOverlayConfig(path, pluginKey, manifestPath string, port int, mountPath string) error {
	pluginEntry := map[string]any{
		"source": map[string]any{
			"path": manifestPath,
		},
		"runtime": nil,
	}
	if mountPath != "" {
		pluginEntry["mountPath"] = mountPath
	}

	cfg := map[string]any{
		"server": map[string]any{
			"public": map[string]any{
				"host": providerDevHost,
				"port": port,
			},
		},
		"plugins": map[string]any{
			pluginKey: pluginEntry,
		},
	}
	return writeYAMLFile(path, cfg)
}

func shouldAutoMountOwnedUI(cfg *config.Config, pluginKey string, manifest *providermanifestv1.Manifest) bool {
	if cfg == nil || manifest == nil || manifest.Spec == nil || manifest.Spec.UI == nil {
		return false
	}
	entry := cfg.Plugins[pluginKey]
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
		if entry.OwnerPlugin == pluginKey || name == pluginKey {
			continue
		}
		return fmt.Errorf("auto-mounted ui path %q for plugins.%s collides with providers.ui.%s", mountPath, pluginKey, name)
	}
	for name, entry := range cfg.Plugins {
		if entry == nil || name == pluginKey {
			continue
		}
		if strings.TrimSpace(entry.MountPath) == mountPath {
			return fmt.Errorf("auto-mounted ui path %q for plugins.%s collides with plugins.%s.mountPath", mountPath, pluginKey, name)
		}
	}
	return nil
}

func providerLocalIndexedDBSourceConfig() any {
	if providersDir := strings.TrimSpace(os.Getenv("GESTALT_PROVIDERS_DIR")); providersDir != "" {
		return map[string]any{
			"path": filepath.Join(providersDir, "indexeddb", "relationaldb", "manifest.yaml"),
		}
	}
	return defaultProviderMetadataURL(config.DefaultIndexedDBProvider, config.DefaultIndexedDBVersion)
}

func defaultProviderMetadataURL(source, version string) string {
	rel := strings.TrimPrefix(strings.TrimSpace(source), config.DefaultProviderRepo+"/")
	return fmt.Sprintf("https://github.com/valon-technologies/gestalt-providers/releases/download/%s/v%s/provider-release.yaml", rel, strings.TrimSpace(version))
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
	slog.Info(message,
		"plugin", session.PluginKey,
		"manifest", session.ManifestPath,
		"public_url", session.PublicURL,
		"admin_url", session.AdminURL,
		"mounted_ui_paths", publicUIPaths,
		"auto_mounted_ui_path", session.AutoMountedUIPath,
		"config_files", session.ConfigPaths,
	)
}

func reserveLocalPort() (int, error) {
	listener, err := net.Listen("tcp", net.JoinHostPort(providerDevHost, "0"))
	if err != nil {
		return 0, fmt.Errorf("reserve provider dev port: %w", err)
	}
	defer func() { _ = listener.Close() }()
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, errors.New("reserve provider dev port: unexpected listener address type")
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
		if src, err := pluginsource.Parse(manifest.Source); err == nil {
			if name := sanitizeDerivedPluginKey(src.PluginName()); name != "" {
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

func isValidExplicitPluginKey(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return false
	}
	return true
}

func loadConfiguredPlugins(configPaths []string) (map[string]*config.ProviderEntry, error) {
	if len(configPaths) == 0 {
		return map[string]*config.ProviderEntry{}, nil
	}
	cfg, err := config.LoadPaths(configPaths)
	if err != nil {
		return nil, fmt.Errorf("load provider overlay config: %w", err)
	}
	if cfg.Plugins == nil {
		return map[string]*config.ProviderEntry{}, nil
	}
	return cfg.Plugins, nil
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
	writeUsageLine(w, "  gestaltd provider validate [--path PATH] [--config PATH]... [--name NAME]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Validate a local source plugin inside a synthesized Gestalt config.")
	writeUsageLine(w, "v1 supports kind: plugin manifests only.")
	writeUsageLine(w, "Repeated --config flags merge left-to-right using the normal Gestalt rules.")
	writeUsageLine(w, "")
	writeUsageLine(w, "Flags:")
	writeUsageLine(w, "  --path     Provider manifest path or directory (default: current working directory)")
	writeUsageLine(w, "  --config   Additional config file to merge; repeat to add support providers or null deletions")
	writeUsageLine(w, "  --name     Plugin key override when the target key is ambiguous")
}

func printProviderDevUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd provider dev [--path PATH] [--config PATH]... [--name NAME] [--port PORT]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Run a local source plugin inside a synthesized Gestalt config.")
	writeUsageLine(w, "v1 supports kind: plugin manifests only.")
	writeUsageLine(w, "The built-in admin UI remains available at /admin; configured or owned public UIs")
	writeUsageLine(w, "are mounted when present.")
	writeUsageLine(w, "")
	writeUsageLine(w, "Flags:")
	writeUsageLine(w, "  --path     Provider manifest path or directory (default: current working directory)")
	writeUsageLine(w, "  --config   Additional config file to merge; repeat to add support providers or null deletions")
	writeUsageLine(w, "  --name     Plugin key override when the target key is ambiguous")
	writeUsageLine(w, "  --port     Public port (default: auto-selected free localhost port)")
}
