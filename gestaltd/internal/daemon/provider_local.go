package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/operator"
	"github.com/valon-technologies/gestalt/server/internal/pluginsource"
	"github.com/valon-technologies/gestalt/server/internal/providerdev"
	"github.com/valon-technologies/gestalt/server/internal/providerpkg"
	"github.com/valon-technologies/gestalt/server/internal/ui"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"gopkg.in/yaml.v3"
)

const (
	providerDevHost          = "127.0.0.1"
	providerDevIndexedDBName = "main"
	providerLocalPluginDir   = "plugin"
	providerLocalSiblingUI   = "ui"
	gestaltAPIKeyEnv         = "GESTALT_API_KEY"
)

type providerLocalCommandOptions struct {
	Path        string
	ConfigPaths []string
	Name        string
	Port        int
	Remote      string
	RemoteToken string
}

type providerLocalSession struct {
	Dir               string
	Kind              string
	ManifestPath      string
	TargetKey         string
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
	nameFlag := fs.String("name", "", "provider key override")
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
	nameFlag := fs.String("name", "", "provider key override")
	portFlag := fs.Int("port", 0, "public port (defaults to a free localhost port)")
	remoteFlag := fs.String("remote", "", "remote gestaltd base URL to attach local source plugins to")
	remoteTokenFlag := fs.String("remote-token", "", "bearer token for --remote (defaults to GESTALT_API_KEY)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if strings.TrimSpace(*remoteFlag) != "" {
		if *portFlag != 0 {
			return errors.New("--port is only supported for local provider dev")
		}
		return runProviderRemoteDev(providerLocalCommandOptions{
			Path:        *pathFlag,
			ConfigPaths: []string(configPaths),
			Name:        *nameFlag,
			Remote:      *remoteFlag,
			RemoteToken: *remoteTokenFlag,
		})
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

type providerRemoteTarget struct {
	Name                string
	Source              string
	Entry               *config.ProviderEntry
	InheritRemoteConfig bool
}

type storedGestaltCLICredential struct {
	APIURL   string `json:"api_url"`
	APIToken string `json:"api_token"`
}

var providerRemoteOpenBrowser = openProviderRemoteBrowser

func runProviderRemoteDev(opts providerLocalCommandOptions) error {
	configPaths, cleanup, err := prepareProviderRemoteConfigPaths(opts)
	if err != nil {
		return err
	}
	defer cleanup()

	cfg, err := config.LoadPartialAllowMissingEnvPaths(configPaths)
	if err != nil {
		return fmt.Errorf("loading provider dev remote config: %w", err)
	}
	inheritRemoteConfig := opts.Path != "" && len(opts.ConfigPaths) == 0
	targets, err := collectProviderRemoteTargets(cfg, opts.Name, inheritRemoteConfig)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return errors.New("provider dev --remote requires at least one source-backed plugin in --config or --path")
	}

	if _, err := providerRemoteBaseURL(opts.Remote); err != nil {
		return fmt.Errorf("invalid --remote %q: %w", opts.Remote, err)
	}
	remoteToken := resolveProviderRemoteAttachToken(opts)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	client := providerdev.Client{
		BaseURL: opts.Remote,
		Token:   remoteToken,
	}
	requestedProviders := make([]providerdev.AttachProvider, 0, len(targets))
	localUIHandlersByTarget := map[int]http.Handler{}
	for i, target := range targets {
		spec, _, err := bootstrap.BuildStartupProviderSpec(target.Name, target.Entry)
		if err != nil {
			return fmt.Errorf("build provider dev remote spec for plugins.%s: %w", target.Name, err)
		}
		attachName := target.Name
		if target.InheritRemoteConfig && opts.Name == "" {
			attachName = ""
		}
		requested := providerdev.AttachProvider{
			Name:   attachName,
			Source: target.Source,
			Spec:   spec,
		}
		if !target.InheritRemoteConfig {
			pluginConfig, err := config.NodeToMap(target.Entry.Config)
			if err != nil {
				return fmt.Errorf("build provider dev remote config for plugins.%s: %w", target.Name, err)
			}
			if pluginConfig == nil {
				pluginConfig = map[string]any{}
			}
			requested.Config = &pluginConfig
		}
		uiHandler, hasUI, err := providerRemoteUIHandler(cfg, target)
		if err != nil {
			return fmt.Errorf("prepare provider dev remote ui for plugins.%s: %w", target.Name, err)
		}
		if hasUI {
			requested.UI = true
			localUIHandlersByTarget[i] = uiHandler
		}
		requestedProviders = append(requestedProviders, requested)
	}
	sessionReq := providerdev.CreateSessionRequest{Providers: requestedProviders}
	session, err := createProviderRemoteSession(ctx, &client, sessionReq)
	if err != nil {
		return err
	}
	attachID := strings.TrimSpace(session.AttachID)
	if attachID == "" {
		return fmt.Errorf("remote provider dev did not return attachId")
	}
	defer func() { _ = client.CloseSession(context.Background(), attachID) }()

	sessionProviders := make(map[string]providerdev.CreateSessionProvider, len(session.Providers))
	for _, provider := range session.Providers {
		sessionProviders[provider.Name] = provider
	}
	localUIHandlers := make(map[string]http.Handler, len(localUIHandlersByTarget))
	for i := range targets {
		remoteName := targets[i].Name
		if targets[i].InheritRemoteConfig && opts.Name == "" {
			if len(targets) != 1 || len(session.Providers) != 1 {
				return fmt.Errorf("remote provider dev could not resolve unique provider for source %q; pass --name", targets[i].Source)
			}
			remoteName = session.Providers[0].Name
			targets[i].Name = remoteName
		}
		if handler, ok := localUIHandlersByTarget[i]; ok {
			localUIHandlers[remoteName] = handler
		}
	}

	processes := make([]*runtimehost.PluginProcess, 0, len(targets))
	providerClients := make(map[string]proto.IntegrationProviderClient, len(targets))
	cleanupProcesses := func() {
		for _, process := range processes {
			_ = process.Close()
		}
	}
	defer cleanupProcesses()

	for _, target := range targets {
		sessionProvider, ok := sessionProviders[target.Name]
		if !ok {
			return fmt.Errorf("remote provider dev did not return runtime env for provider %q", target.Name)
		}
		process, err := startProviderRemoteProcess(ctx, target, sessionProvider)
		if err != nil {
			return err
		}
		processes = append(processes, process)
		providerClients[target.Name] = process.Integration()
	}

	slog.Info("provider dev attached",
		"remote", strings.TrimRight(opts.Remote, "/"),
		"attachId", attachID,
		"providers", providerRemoteTargetNames(targets),
		"ui_providers", providerRemoteUIProviderNames(localUIHandlers),
		"config_files", configPaths,
	)
	return client.RunDispatcher(ctx, attachID, providerClients, providerdev.WithUIHandlers(localUIHandlers))
}

func resolveProviderRemoteAttachToken(opts providerLocalCommandOptions) string {
	if token := strings.TrimSpace(opts.RemoteToken); token != "" {
		return token
	}
	if token := strings.TrimSpace(os.Getenv(gestaltAPIKeyEnv)); token != "" {
		return token
	}
	return ""
}

func createProviderRemoteSession(ctx context.Context, client *providerdev.Client, req providerdev.CreateSessionRequest) (*providerdev.CreateSessionResponse, error) {
	if strings.TrimSpace(client.Token) != "" {
		session, err := client.CreateSession(ctx, req)
		if err == nil {
			return session, nil
		}
		return nil, providerRemoteCreateSessionError(err)
	}
	return createProviderRemoteSessionWithBrowser(ctx, client, req)
}

func createProviderRemoteSessionWithBrowser(ctx context.Context, client *providerdev.Client, req providerdev.CreateSessionRequest) (*providerdev.CreateSessionResponse, error) {
	authorization, err := client.CreateAttachAuthorization(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("create provider dev browser approval: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Approve provider-dev attach in your browser:\n\n  %s\n\nVerification code: %s\n\nOnly approve if the browser asks for this code.\n\n", authorization.ApprovalURL, authorization.VerificationCode)
	if err := providerRemoteOpenBrowser(authorization.ApprovalURL); err != nil {
		fmt.Fprintf(os.Stderr, "Could not open browser automatically: %v\nOpen the URL above to continue.\n\n", err)
	}

	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
		status, err := client.PollAttachAuthorization(ctx, authorization.AuthorizationID)
		if err != nil {
			return nil, fmt.Errorf("poll provider dev browser approval: %w", err)
		}
		if status.Approved {
			return client.CreateAuthorizedSession(ctx, authorization.AuthorizationID, req)
		}
		if !authorization.ExpiresAt.IsZero() && time.Now().After(authorization.ExpiresAt) {
			return nil, errors.New("provider dev browser approval expired")
		}
		timer.Reset(time.Second)
	}
}

func openProviderRemoteBrowser(rawURL string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", rawURL).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL).Start()
	default:
		return exec.Command("xdg-open", rawURL).Start()
	}
}

type providerRemoteTokenErrors struct {
	AuthMissing                  func(remoteOrigin string) error
	StoredCredentialUnscoped     func(remoteOrigin, credentialPath string) error
	StoredCredentialMismatch     func(remoteOrigin, storedOrigin, credentialPath string) error
	StoredCredentialMissingToken func(credentialPath string) error
}

func resolveProviderRemoteTokenWithErrors(remote, explicitToken string, tokenErrors providerRemoteTokenErrors) (string, error) {
	remoteBaseURL, err := providerRemoteBaseURL(remote)
	if err != nil {
		return "", fmt.Errorf("invalid --remote %q: %w", remote, err)
	}
	if token := strings.TrimSpace(explicitToken); token != "" {
		return token, nil
	}
	if token := strings.TrimSpace(os.Getenv(gestaltAPIKeyEnv)); token != "" {
		return token, nil
	}

	credential, credentialPath, ok, err := loadStoredGestaltCLICredential()
	if err != nil {
		return "", err
	}
	if !ok {
		return "", tokenErrors.AuthMissing(remoteBaseURL)
	}
	if strings.TrimSpace(credential.APIURL) == "" {
		return "", tokenErrors.StoredCredentialUnscoped(remoteBaseURL, credentialPath)
	}
	storedBaseURL, err := providerRemoteBaseURL(credential.APIURL)
	if err != nil {
		return "", fmt.Errorf("stored Gestalt CLI credential has invalid api_url in %s: %w", credentialPath, err)
	}
	if storedBaseURL != remoteBaseURL {
		return "", tokenErrors.StoredCredentialMismatch(remoteBaseURL, storedBaseURL, credentialPath)
	}
	if token := strings.TrimSpace(credential.APIToken); token != "" {
		return token, nil
	}
	return "", tokenErrors.StoredCredentialMissingToken(credentialPath)
}

func loadStoredGestaltCLICredential() (storedGestaltCLICredential, string, bool, error) {
	path, err := storedGestaltCLICredentialPath()
	if err != nil {
		return storedGestaltCLICredential{}, "", false, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return storedGestaltCLICredential{}, path, false, nil
		}
		return storedGestaltCLICredential{}, path, false, fmt.Errorf("read stored Gestalt CLI credential from %s: %w", path, err)
	}
	var credential storedGestaltCLICredential
	if err := json.Unmarshal(data, &credential); err != nil {
		return storedGestaltCLICredential{}, path, false, fmt.Errorf("parse stored Gestalt CLI credential from %s: %w", path, err)
	}
	return credential, path, true, nil
}

func storedGestaltCLICredentialPath() (string, error) {
	if xdgConfigHome := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdgConfigHome != "" {
		return filepath.Join(xdgConfigHome, "gestalt", "credentials.json"), nil
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate stored Gestalt CLI credential: %w", err)
	}
	return filepath.Join(homeDir, ".config", "gestalt", "credentials.json"), nil
}

func providerRemoteBaseURL(rawURL string) (string, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return "", errors.New("URL is required")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", err
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", errors.New("URL scheme must be http or https")
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return "", errors.New("URL must include a host")
	}
	port := parsed.Port()
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		port = ""
	}
	if port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	baseURL := scheme + "://" + host
	if path := strings.TrimRight(parsed.EscapedPath(), "/"); path != "" {
		baseURL += path
	}
	return baseURL, nil
}

func providerRemoteCreateSessionError(err error) error {
	if err == nil {
		return nil
	}
	if !strings.Contains(err.Error(), "provider dev attach access denied") {
		return err
	}
	return fmt.Errorf(`%w

remote provider-dev attach was denied. The remote plugin must grant providerDev.attach.allowedRoles for your resolved role, and API token callers must use a user token with permissions[].actions including provider_dev.attach for every attached plugin.

provider scopes, operation permissions, and subject-owned API tokens do not grant direct remote attach. Run without --remote-token/%s to use browser approval when the server supports it`, err, gestaltAPIKeyEnv)
}

func providerRemoteTargetNames(targets []providerRemoteTarget) []string {
	names := make([]string, 0, len(targets))
	for _, target := range targets {
		names = append(names, target.Name)
	}
	return names
}

func providerRemoteUIProviderNames(handlers map[string]http.Handler) []string {
	if len(handlers) == 0 {
		return nil
	}
	names := make([]string, 0, len(handlers))
	for name := range handlers {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
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
	if kind != providermanifestv1.KindPlugin && kind != providermanifestv1.KindUI {
		return nil, fmt.Errorf("gestaltd provider dev and validate only support kind: plugin or ui in v1 (got %q)", kind)
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
	state := operator.StatePaths{
		ArtifactsDir: filepath.Join(sessionDir, "artifacts"),
		LockfilePath: filepath.Join(sessionDir, "gestalt.lock.json"),
	}

	var session *providerLocalSession
	switch kind {
	case providermanifestv1.KindPlugin:
		session, err = preparePluginLocalSession(sessionDir, baseConfigPath, state, opts, targetManifestPath, manifest)
	case providermanifestv1.KindUI:
		session, err = prepareUILocalSession(sessionDir, baseConfigPath, state, opts, targetManifestPath, manifest)
	default:
		err = fmt.Errorf("unsupported provider local target kind %q", kind)
	}
	if err != nil {
		return nil, err
	}
	cleanupSessionDir = false
	return session, nil
}

func preparePluginLocalSession(sessionDir, baseConfigPath string, state operator.StatePaths, opts providerLocalCommandOptions, targetManifestPath string, manifest *providermanifestv1.Manifest) (*providerLocalSession, error) {
	resolvedKey, err := resolveProviderLocalPluginKey(opts.ConfigPaths, targetManifestPath, manifest, opts.Name)
	if err != nil {
		return nil, err
	}

	overlayConfigPath := filepath.Join(sessionDir, "provider-target.yaml")
	if err := writeProviderLocalPluginOverlayConfig(overlayConfigPath, resolvedKey, targetManifestPath, opts.Port, "", "", ""); err != nil {
		return nil, err
	}

	configPaths := append([]string{baseConfigPath}, opts.ConfigPaths...)
	configPaths = append(configPaths, overlayConfigPath)

	loadedCfg, err := config.LoadPaths(configPaths)
	if err != nil {
		return nil, fmt.Errorf("loading provider dev config: %w", err)
	}

	autoMountPath := ""
	siblingUIManifestPath, err := findSiblingUIManifestPath(targetManifestPath, manifest)
	if err != nil {
		return nil, err
	}
	switch {
	case shouldAutoMountOwnedUI(loadedCfg, resolvedKey, manifest):
		autoMountPath = defaultProviderLocalMountPath(manifest, targetManifestPath, resolvedKey)
		if err := ensureNoPublicUIPathCollision(loadedCfg, resolvedKey, autoMountPath); err != nil {
			return nil, err
		}
		if err := writeProviderLocalPluginOverlayConfig(overlayConfigPath, resolvedKey, targetManifestPath, opts.Port, autoMountPath, "", ""); err != nil {
			return nil, err
		}
	case siblingUIManifestPath != "":
		var uiName string
		if entry := loadedCfg.Plugins[resolvedKey]; entry != nil {
			uiName = strings.TrimSpace(entry.UI)
			autoMountPath = strings.TrimSpace(entry.MountPath)
		}
		if uiName == "" {
			uiName = resolvedKey
		}
		if autoMountPath == "" {
			autoMountPath = defaultProviderLocalMountPath(manifest, targetManifestPath, resolvedKey)
			if err := ensureNoPublicUIPathCollision(loadedCfg, resolvedKey, autoMountPath); err != nil {
				return nil, err
			}
		}
		if err := writeProviderLocalPluginOverlayConfig(overlayConfigPath, resolvedKey, targetManifestPath, opts.Port, autoMountPath, uiName, siblingUIManifestPath); err != nil {
			return nil, err
		}
	}

	loadedCfg, err = config.LoadPaths(configPaths)
	if err != nil {
		return nil, fmt.Errorf("loading provider dev config with mounted ui: %w", err)
	}

	publicURL := providerLocalPublicURL(loadedCfg)
	publicUIPaths := mountedPublicUIPaths(loadedCfg)
	if autoMountPath != "" && !slices.Contains(publicUIPaths, autoMountPath) {
		publicUIPaths = append(publicUIPaths, autoMountPath)
		slices.Sort(publicUIPaths)
	}

	return &providerLocalSession{
		Dir:               sessionDir,
		Kind:              providermanifestv1.KindPlugin,
		ManifestPath:      targetManifestPath,
		TargetKey:         resolvedKey,
		ConfigPaths:       configPaths,
		State:             state,
		PublicURL:         publicURL,
		AdminURL:          strings.TrimRight(publicURL, "/") + "/admin/",
		PublicUIPaths:     publicUIPaths,
		AutoMountedUIPath: autoMountPath,
	}, nil
}

func prepareUILocalSession(sessionDir, baseConfigPath string, state operator.StatePaths, opts providerLocalCommandOptions, targetManifestPath string, manifest *providermanifestv1.Manifest) (*providerLocalSession, error) {
	resolvedKey, err := resolveProviderLocalUIKey(opts.ConfigPaths, targetManifestPath, manifest, opts.Name)
	if err != nil {
		return nil, err
	}

	autoMountPath := defaultProviderLocalMountPath(manifest, targetManifestPath, resolvedKey)
	if configuredUIs, err := loadConfiguredUIs(opts.ConfigPaths); err == nil {
		if entry := configuredUIs[resolvedKey]; entry != nil && strings.TrimSpace(entry.Path) != "" {
			autoMountPath = strings.TrimSpace(entry.Path)
		}
	}

	overlayConfigPath := filepath.Join(sessionDir, "provider-target.yaml")
	if err := writeProviderLocalUIOverlayConfig(overlayConfigPath, resolvedKey, targetManifestPath, opts.Port, autoMountPath); err != nil {
		return nil, err
	}

	configPaths := append([]string{baseConfigPath}, opts.ConfigPaths...)
	configPaths = append(configPaths, overlayConfigPath)

	loadedCfg, err := config.LoadPaths(configPaths)
	if err != nil {
		return nil, fmt.Errorf("loading provider dev config: %w", err)
	}

	publicURL := providerLocalPublicURL(loadedCfg)
	publicUIPaths := mountedPublicUIPaths(loadedCfg)
	if autoMountPath != "" && !slices.Contains(publicUIPaths, autoMountPath) {
		publicUIPaths = append(publicUIPaths, autoMountPath)
		slices.Sort(publicUIPaths)
	}

	return &providerLocalSession{
		Dir:               sessionDir,
		Kind:              providermanifestv1.KindUI,
		ManifestPath:      targetManifestPath,
		TargetKey:         resolvedKey,
		ConfigPaths:       configPaths,
		State:             state,
		PublicURL:         publicURL,
		AdminURL:          strings.TrimRight(publicURL, "/") + "/admin/",
		PublicUIPaths:     publicUIPaths,
		AutoMountedUIPath: autoMountPath,
	}, nil
}

func prepareProviderRemoteConfigPaths(opts providerLocalCommandOptions) ([]string, func(), error) {
	cleanup := func() {}
	if strings.TrimSpace(opts.Path) == "" {
		if len(opts.ConfigPaths) == 0 {
			return nil, cleanup, errors.New("provider dev --remote requires --config or --path")
		}
		return append([]string(nil), opts.ConfigPaths...), cleanup, nil
	}

	manifestPath, manifest, err := resolveProviderTargetManifest(opts.Path)
	if err != nil {
		return nil, cleanup, err
	}
	kind, err := providerpkg.ManifestKind(manifest)
	if err != nil {
		return nil, cleanup, err
	}
	if kind != providermanifestv1.KindPlugin {
		return nil, cleanup, fmt.Errorf("provider dev --remote only supports kind: plugin in v1 (got %q)", kind)
	}
	targetManifestPath, err := canonicalPath(manifestPath)
	if err != nil {
		return nil, cleanup, err
	}
	resolvedKey, err := resolveProviderRemotePluginKey(opts.ConfigPaths, targetManifestPath, manifest, opts.Name)
	if err != nil {
		return nil, cleanup, err
	}

	sessionDir, err := os.MkdirTemp("", "gestaltd-provider-remote-*")
	if err != nil {
		return nil, cleanup, fmt.Errorf("create provider remote session dir: %w", err)
	}
	cleanup = func() { _ = os.RemoveAll(sessionDir) }
	overlayPath := filepath.Join(sessionDir, "provider-remote-target.yaml")
	if err := writeProviderRemotePluginOverlayConfig(overlayPath, resolvedKey, targetManifestPath); err != nil {
		cleanup()
		return nil, func() {}, err
	}
	configPaths := append([]string(nil), opts.ConfigPaths...)
	configPaths = append(configPaths, overlayPath)
	return configPaths, cleanup, nil
}

func collectProviderRemoteTargets(cfg *config.Config, explicitName string, inheritRemoteConfig bool) ([]providerRemoteTarget, error) {
	if cfg == nil {
		return nil, nil
	}
	var targets []providerRemoteTarget
	names := make([]string, 0, len(cfg.Plugins))
	for name := range cfg.Plugins {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if explicitName != "" && name != explicitName {
			continue
		}
		entry := cfg.Plugins[name]
		if entry == nil || !entry.HasLocalSource() {
			continue
		}
		manifestPath, manifest, err := ensureProviderRemoteManifestResolved(name, entry)
		if err != nil {
			return nil, err
		}
		if manifest == nil || manifestPath == "" {
			return nil, fmt.Errorf("plugins.%s must resolve to a local source manifest", name)
		}
		kind, err := providerpkg.ManifestKind(manifest)
		if err != nil {
			return nil, err
		}
		if kind != providermanifestv1.KindPlugin {
			return nil, fmt.Errorf("provider dev --remote only supports plugins in v1 (plugins.%s has kind %q)", name, kind)
		}
		source := strings.TrimSpace(manifest.Source)
		if inheritRemoteConfig && explicitName == "" && source == "" {
			return nil, fmt.Errorf("plugins.%s manifest source is required for provider dev --remote --path without --name", name)
		}
		targets = append(targets, providerRemoteTarget{
			Name:                name,
			Source:              source,
			Entry:               entry,
			InheritRemoteConfig: inheritRemoteConfig,
		})
	}
	if explicitName != "" && len(targets) == 0 {
		return nil, fmt.Errorf("no source-backed plugins.%s entry found in provider dev remote config", explicitName)
	}
	return targets, nil
}

func ensureProviderRemoteManifestResolved(name string, entry *config.ProviderEntry) (string, *providermanifestv1.Manifest, error) {
	if entry == nil {
		return "", nil, fmt.Errorf("plugins.%s is not configured", name)
	}
	if entry.ResolvedManifest != nil && entry.ResolvedManifestPath != "" {
		return entry.ResolvedManifestPath, entry.ResolvedManifest, nil
	}
	sourcePath := strings.TrimSpace(entry.SourcePath())
	if sourcePath == "" {
		return "", nil, fmt.Errorf("plugins.%s must use a local source path", name)
	}
	info, err := os.Stat(sourcePath)
	if err != nil {
		return "", nil, fmt.Errorf("stat plugins.%s source %q: %w", name, sourcePath, err)
	}
	manifestPath := sourcePath
	if info.IsDir() {
		manifestPath, err = providerpkg.FindManifestFile(sourcePath)
		if err != nil {
			return "", nil, err
		}
	} else if !providerpkg.IsManifestFile(sourcePath) {
		return "", nil, fmt.Errorf("plugins.%s source %q must point to a provider manifest file or directory", name, sourcePath)
	}
	manifestPath, err = canonicalPath(manifestPath)
	if err != nil {
		return "", nil, err
	}
	_, manifest, err := providerpkg.ReadSourceManifestFile(manifestPath)
	if err != nil {
		return "", nil, err
	}
	entry.ResolvedManifestPath = manifestPath
	entry.ResolvedManifest = manifest
	return manifestPath, manifest, nil
}

func startProviderRemoteProcess(ctx context.Context, target providerRemoteTarget, remote providerdev.CreateSessionProvider) (*runtimehost.PluginProcess, error) {
	entry := target.Entry
	command := entry.Command
	args := slices.Clone(entry.Args)
	env := cloneStringMap(entry.Env)
	var cleanup func()
	if command == "" {
		if entry.ResolvedManifestPath == "" {
			return nil, fmt.Errorf("plugins.%s resolved manifest path is required for source provider execution", target.Name)
		}
		rootDir := filepath.Dir(entry.ResolvedManifestPath)
		var err error
		command, args, cleanup, err = providerpkg.SourceProviderExecutionCommand(rootDir, runtime.GOOS, runtime.GOARCH)
		if errors.Is(err, providerpkg.ErrNoSourceProviderPackage) {
			return nil, fmt.Errorf("plugins.%s: prepare source provider execution: no Go, Python, Rust, or TypeScript provider source found", target.Name)
		}
		if err != nil {
			return nil, fmt.Errorf("plugins.%s: prepare source provider execution: %w", target.Name, err)
		}
		execEnv, err := providerpkg.SourceProviderExecutionEnv(rootDir, runtime.GOOS, runtime.GOARCH)
		if err != nil {
			if cleanup != nil {
				cleanup()
			}
			return nil, fmt.Errorf("plugins.%s: prepare source provider environment: %w", target.Name, err)
		}
		env = mergeStringMaps(env, execEnv)
	}
	env = mergeStringMaps(env, remote.Env)

	// Remote dev runs arbitrary local source trees. The plugin sandbox is tuned
	// for staged executables and can hide source files from TypeScript/Python
	// runtimes, so v1 leaves local source execution unsandboxed.
	process, err := runtimehost.StartPluginProcess(ctx, runtimehost.ProcessConfig{
		Command:      command,
		Args:         args,
		Env:          env,
		HostBinary:   entry.HostBinary,
		Cleanup:      cleanup,
		ProviderName: target.Name,
		Stdout:       os.Stdout,
		Stderr:       os.Stderr,
	})
	if err != nil {
		if cleanup != nil {
			cleanup()
		}
		return nil, fmt.Errorf("start local provider plugins.%s: %w", target.Name, err)
	}
	return process, nil
}

func providerRemoteUIHandler(cfg *config.Config, target providerRemoteTarget) (http.Handler, bool, error) {
	uiManifestPath, ok, err := providerRemoteUIManifestPath(cfg, target)
	if err != nil || !ok {
		return nil, ok, err
	}
	handler, err := sourceUIHandler(uiManifestPath)
	if err != nil {
		return nil, true, err
	}
	return handler, true, nil
}

func providerRemoteUIManifestPath(cfg *config.Config, target providerRemoteTarget) (string, bool, error) {
	entry := target.Entry
	if entry == nil {
		return "", false, nil
	}
	if uiName := strings.TrimSpace(entry.UI); uiName != "" {
		if cfg == nil || cfg.Providers.UI == nil || cfg.Providers.UI[uiName] == nil {
			return "", false, fmt.Errorf("plugins.%s.ui references unknown ui %q", target.Name, uiName)
		}
		uiEntry := cfg.Providers.UI[uiName]
		manifestPath, ok, err := sourceUIManifestPathFromEntry(uiEntry)
		if err != nil || ok {
			return manifestPath, ok, err
		}
	}
	if entry.ResolvedManifest != nil && entry.ResolvedManifest.Spec != nil && entry.ResolvedManifest.Spec.UI != nil {
		ownedUIPath := strings.TrimSpace(entry.ResolvedManifest.Spec.UI.Path)
		if ownedUIPath == "" {
			return "", false, nil
		}
		if strings.TrimSpace(entry.ResolvedManifestPath) == "" {
			return "", false, fmt.Errorf("resolved manifest path is required for owned ui")
		}
		manifestPath, err := canonicalPath(filepath.Join(filepath.Dir(entry.ResolvedManifestPath), filepath.FromSlash(ownedUIPath)))
		if err != nil {
			return "", false, err
		}
		return manifestPath, true, nil
	}
	siblingPath, err := findSiblingUIManifestPath(entry.ResolvedManifestPath, entry.ResolvedManifest)
	if err != nil {
		return "", false, err
	}
	return siblingPath, siblingPath != "", nil
}

func sourceUIManifestPathFromEntry(entry *config.UIEntry) (string, bool, error) {
	if entry == nil {
		return "", false, nil
	}
	sourcePath := strings.TrimSpace(entry.SourcePath())
	if sourcePath == "" {
		return "", false, nil
	}
	info, err := os.Stat(sourcePath)
	if err != nil {
		return "", false, fmt.Errorf("stat ui source %q: %w", sourcePath, err)
	}
	manifestPath := sourcePath
	if info.IsDir() {
		manifestPath, err = providerpkg.FindManifestFile(sourcePath)
		if err != nil {
			return "", false, err
		}
	} else if !providerpkg.IsManifestFile(sourcePath) {
		return "", false, fmt.Errorf("ui source %q must point to a provider manifest file or directory", sourcePath)
	}
	manifestPath, err = canonicalPath(manifestPath)
	if err != nil {
		return "", false, err
	}
	return manifestPath, true, nil
}

func sourceUIHandler(manifestPath string) (http.Handler, error) {
	_, manifest, err := providerpkg.ReadSourceManifestFile(manifestPath)
	if err != nil {
		return nil, err
	}
	kind, err := providerpkg.ManifestKind(manifest)
	if err != nil {
		return nil, err
	}
	if kind != providermanifestv1.KindUI {
		return nil, fmt.Errorf("ui manifest %q must have kind %q (got %q)", manifestPath, providermanifestv1.KindUI, kind)
	}
	if err := providerpkg.RunSourceReleaseBuild(manifestPath, manifest); err != nil {
		return nil, err
	}
	_, manifest, err = providerpkg.ReadSourceManifestFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read ui manifest after release build: %w", err)
	}
	if manifest.Spec == nil || strings.TrimSpace(manifest.Spec.AssetRoot) == "" {
		return nil, fmt.Errorf("ui manifest %q missing spec.assetRoot", manifestPath)
	}
	assetRoot := filepath.Join(filepath.Dir(manifestPath), filepath.FromSlash(manifest.Spec.AssetRoot))
	if _, err := os.Stat(assetRoot); err != nil {
		return nil, fmt.Errorf("ui asset root not found at %s: %w", assetRoot, err)
	}
	return ui.DirHandler(assetRoot)
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
	return resolveProviderPluginKey(configPaths, targetManifestPath, manifest, explicitName, loadConfiguredPlugins)
}

func resolveProviderRemotePluginKey(configPaths []string, targetManifestPath string, manifest *providermanifestv1.Manifest, explicitName string) (string, error) {
	return resolveProviderPluginKey(configPaths, targetManifestPath, manifest, explicitName, loadConfiguredPluginsAllowMissingEnv)
}

func resolveProviderPluginKey(configPaths []string, targetManifestPath string, manifest *providermanifestv1.Manifest, explicitName string, loadPlugins func([]string) (map[string]*config.ProviderEntry, error)) (string, error) {
	plugins, err := loadPlugins(configPaths)
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
		"apiVersion": config.ConfigAPIVersion,
		"server": map[string]any{
			"encryptionKey": encryptionKey,
			"providers": map[string]any{
				"externalCredentials": config.DefaultProviderInstance,
				"indexeddb":           providerDevIndexedDBName,
			},
		},
		"providers": map[string]any{
			"externalCredentials": map[string]any{
				config.DefaultProviderInstance: map[string]any{
					"source": providerLocalExternalCredentialsSourceConfig(),
				},
			},
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

func writeProviderLocalPluginOverlayConfig(path, pluginKey, manifestPath string, port int, mountPath, uiName, uiManifestPath string) error {
	pluginEntry := map[string]any{
		"source":    providerLocalSourceOverride(manifestPath),
		"execution": nil,
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

func writeProviderRemotePluginOverlayConfig(path, pluginKey, manifestPath string) error {
	cfg := map[string]any{
		"apiVersion": config.ConfigAPIVersion,
		"plugins": map[string]any{
			pluginKey: map[string]any{
				"source":    providerLocalSourceOverride(manifestPath),
				"execution": nil,
			},
		},
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
		"server": map[string]any{
			"public": map[string]any{
				"host": providerDevHost,
				"port": port,
			},
		},
		"providers": map[string]any{
			"ui": map[string]any{
				uiKey: uiEntry,
			},
		},
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
			return fmt.Errorf("auto-mounted ui path %q for plugins.%s collides with plugins.%s.ui.path", mountPath, pluginKey, name)
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
		"kind", session.Kind,
		"manifest", session.ManifestPath,
		"public_url", session.PublicURL,
		"admin_url", session.AdminURL,
		"mounted_ui_paths", publicUIPaths,
		"auto_mounted_ui_path", session.AutoMountedUIPath,
		"config_files", session.ConfigPaths,
	}
	switch session.Kind {
	case providermanifestv1.KindUI:
		args = append(args, "ui", session.TargetKey)
	default:
		args = append(args, "plugin", session.TargetKey)
	}
	slog.Info(message, args...)
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

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func mergeStringMaps(base map[string]string, overlay map[string]string) map[string]string {
	if len(overlay) == 0 {
		return base
	}
	if base == nil {
		base = make(map[string]string, len(overlay))
	}
	for key, value := range overlay {
		base[key] = value
	}
	return base
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
		if src, err := pluginsource.Parse(manifest.Source); err == nil {
			if slug := strings.TrimSpace(src.PluginName()); slug != "" {
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

func loadConfiguredPluginsAllowMissingEnv(configPaths []string) (map[string]*config.ProviderEntry, error) {
	if len(configPaths) == 0 {
		return map[string]*config.ProviderEntry{}, nil
	}
	cfg, err := config.LoadPartialAllowMissingEnvPaths(configPaths)
	if err != nil {
		return nil, fmt.Errorf("load provider overlay config: %w", err)
	}
	if cfg.Plugins == nil {
		return map[string]*config.ProviderEntry{}, nil
	}
	return cfg.Plugins, nil
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

func resolveProviderLocalUIKey(configPaths []string, targetManifestPath string, manifest *providermanifestv1.Manifest, explicitName string) (string, error) {
	uis, err := loadConfiguredUIs(configPaths)
	if err != nil {
		return "", err
	}
	matchingKeys, err := matchingUIKeys(uis, targetManifestPath)
	if err != nil {
		return "", err
	}

	if explicitName != "" {
		if !isValidExplicitPluginKey(explicitName) {
			return "", fmt.Errorf("invalid --name %q: use only letters, numbers, and underscores", explicitName)
		}
		if len(matchingKeys) == 1 && matchingKeys[0] != explicitName {
			return "", fmt.Errorf("target manifest is already configured as providers.ui.%s; pass --name %q or remove the conflicting config entry", matchingKeys[0], matchingKeys[0])
		}
		if len(matchingKeys) > 1 && !slices.Contains(matchingKeys, explicitName) {
			return "", fmt.Errorf("target manifest is configured by multiple ui keys (%s); remove the ambiguity before using --name", strings.Join(matchingKeys, ", "))
		}
		return explicitName, nil
	}

	if len(matchingKeys) == 1 {
		return matchingKeys[0], nil
	}
	if len(matchingKeys) > 1 {
		return "", fmt.Errorf("target manifest is configured by multiple ui keys (%s); pass --name to choose the target key", strings.Join(matchingKeys, ", "))
	}

	if name := derivedPluginKey(manifest, targetManifestPath); name != "" {
		return name, nil
	}
	return "", fmt.Errorf("unable to derive a ui key for %s; pass --name", targetManifestPath)
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
	writeUsageLine(w, "  gestaltd provider validate [--path PATH] [--config PATH]... [--name NAME]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Validate a local source plugin or ui inside a synthesized Gestalt config.")
	writeUsageLine(w, "v1 supports kind: plugin and kind: ui manifests.")
	writeUsageLine(w, "Repeated --config flags merge left-to-right using the normal Gestalt rules.")
	writeUsageLine(w, "")
	writeUsageLine(w, "Flags:")
	writeUsageLine(w, "  --path     Provider manifest path or directory (default: current working directory)")
	writeUsageLine(w, "  --config   Additional config file to merge; repeat to add support providers or null deletions")
	writeUsageLine(w, "  --name     Provider key override when the target key is ambiguous")
}

func printProviderDevUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd provider dev [--path PATH] [--config PATH]... [--name NAME] [--port PORT]")
	writeUsageLine(w, "  gestaltd provider dev --remote URL --config PATH [--config PATH]... [--name NAME]")
	writeUsageLine(w, "  gestaltd provider dev --remote URL --path PATH [--config PATH]... [--name NAME]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Run a local source plugin or ui inside a synthesized Gestalt config.")
	writeUsageLine(w, "v1 supports kind: plugin and kind: ui manifests.")
	writeUsageLine(w, "The built-in admin UI remains available at /admin; configured, owned, or sibling public UIs")
	writeUsageLine(w, "are mounted when present.")
	writeUsageLine(w, "With --remote, source-backed local plugins attach to an authenticated remote gestaltd session")
	writeUsageLine(w, "while the remote config keeps its normal auth, authorization, connections, host services,")
	writeUsageLine(w, "and mounted plugin UI routes.")
	writeUsageLine(w, "")
	writeUsageLine(w, "Flags:")
	writeUsageLine(w, "  --path     Provider manifest path or directory (default: current working directory)")
	writeUsageLine(w, "  --config   Additional config file to merge; repeat to add support providers or null deletions")
	writeUsageLine(w, "  --name     Provider key override when the target key is ambiguous")
	writeUsageLine(w, "  --port     Public port (default: auto-selected free localhost port)")
	writeUsageLine(w, "  --remote   Remote gestaltd base URL to attach local source plugins to")
	writeUsageLine(w, "  --remote-token  Bearer token for non-interactive --remote attach (must grant provider_dev.attach)")
}
