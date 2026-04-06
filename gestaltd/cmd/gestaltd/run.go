package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/crypto"
	"github.com/valon-technologies/gestalt/server/internal/adminui"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	gestaltmcp "github.com/valon-technologies/gestalt/server/internal/mcp"
	"github.com/valon-technologies/gestalt/server/internal/operator"
	"github.com/valon-technologies/gestalt/server/internal/pluginsource"
	"github.com/valon-technologies/gestalt/server/internal/sandbox"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/webui"

	mcpserver "github.com/mark3labs/mcp-go/server"
)

func run(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "-h", "--help", "help":
			printMainUsage(os.Stderr)
			return flag.ErrHelp
		case "version", "--version", "-v":
			fmt.Println(version)
			return nil
		case "plugin":
			return runPlugin(args[1:])
		case "serve":
			return runServe(args[1:])
		case "init":
			return runInit(args[1:])
		case "validate":
			return runValidate(args[1:])
		case "__sandbox":
			return sandbox.RunSubcommand(args[1:])
		}
	}

	return runDefaultServe(args)
}

func runDefaultServe(args []string) error {
	return runStartCommand("gestaltd", printMainUsage, args, false, true)
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("gestaltd serve", flag.ContinueOnError)
	fs.Usage = func() { printServeUsage(fs.Output()) }
	configPath := fs.String("config", "", "path to config file")
	artifactsDir := fs.String("artifacts-dir", "", "path to writable prepared-artifacts directory")
	locked := fs.Bool("locked", false, "require exact lock state; do not auto-init")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	env, err := setupBootstrapWithArtifactsDir(*configPath, *artifactsDir, *locked)
	if err != nil {
		return err
	}
	return runServer(env)
}

func runStartCommand(name string, usage func(io.Writer), args []string, locked bool, autoGenerate bool) error {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.Usage = func() { usage(fs.Output()) }
	configPath := fs.String("config", "", "path to config file")
	artifactsDir := fs.String("artifacts-dir", "", "path to writable prepared-artifacts directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	if autoGenerate && *configPath == "" {
		resolved := resolveConfigPath("")
		if _, err := os.Stat(resolved); os.IsNotExist(err) {
			if p := operator.DefaultLocalConfigPath(); p != "" {
				generated, genErr := operator.GenerateDefaultConfig(filepath.Dir(p))
				if genErr == nil {
					*configPath = generated
				}
			}
		}
	}

	env, err := setupBootstrapWithArtifactsDir(*configPath, *artifactsDir, locked)
	if err != nil {
		return err
	}
	return runServer(env)
}

func runServer(env *bootstrapEnv) error {
	defer env.Close()

	result := env.Result
	httpInvoker := invocation.NewGuarded(result.Invoker, result.CapabilityLister, "http", result.AuditSink, invocation.WithoutRateLimit())
	mcpInvoker := invocation.NewGuarded(result.Invoker, result.CapabilityLister, "mcp", result.AuditSink, invocation.WithoutRateLimit())
	connMaps, err := bootstrap.BuildConnectionMaps(env.Config)
	if err != nil {
		return err
	}

	mcpSurface := buildMCPSurface(env.Config, connMaps)

	if env.Config.Server.BaseURL != "" {
		slog.Info("gestaltd base URL configured",
			"base_url", env.Config.Server.BaseURL,
			"auth_callback", env.Config.Server.BaseURL+config.AuthCallbackPath,
			"integration_callback", env.Config.Server.BaseURL+config.IntegrationCallbackPath,
		)
	}

	clientUI, publicAdminUI, managementAdminUI, err := resolveUIHandlers(env.Config)
	if err != nil {
		return fmt.Errorf("resolving ui handlers: %w", err)
	}

	mcpSlot := &lateHandler{}
	var mcpHandler http.Handler = mcpSlot

	var apiTokenTTL time.Duration
	if env.Config.Server.APITokenTTL != "" {
		var err error
		apiTokenTTL, err = config.ParseDuration(env.Config.Server.APITokenTTL)
		if err != nil {
			return fmt.Errorf("parsing server.api_token_ttl: %w", err)
		}
	}

	baseServerConfig := server.Config{
		Auth:              result.Auth,
		AuditSink:         result.AuditSink,
		Datastore:         result.Datastore,
		Providers:         result.Providers,
		Invoker:           httpInvoker,
		DefaultConnection: connMaps.DefaultConnection,
		CatalogConnection: connMaps.MCPConnection,
		ConnectionAuth:    result.ConnectionAuth,
		IntegrationDefs:   env.Config.Integrations,
		SecureCookies:     strings.HasPrefix(env.Config.Server.BaseURL, "https://"),
		StateSecret:       crypto.DeriveKey(env.Config.Server.EncryptionKey),
		APITokenTTL:       apiTokenTTL,
		Readiness: composeReadiness(
			readinessFromChannel(result.ProvidersReady, "providers loading"),
			datastoreReadiness(result.Datastore),
		),
		PrometheusMetrics: env.Result.Telemetry.PrometheusHandler(),
		MCPHandler:        mcpHandler,
		ClientUI:          clientUI,
		AdminUI:           publicAdminUI,
	}

	publicProfile := server.RouteProfileAll
	if env.Config.Server.ManagementAddr() != "" {
		publicProfile = server.RouteProfilePublic
	}
	baseServerConfig.RouteProfile = publicProfile

	srv, err := server.New(baseServerConfig)
	if err != nil {
		return fmt.Errorf("creating server: %w", err)
	}

	type namedHTTPServer struct {
		name   string
		server *http.Server
	}

	newHTTPServer := func(name, addr string, handler http.Handler) *http.Server {
		return &http.Server{
			Addr:              addr,
			Handler:           handler,
			ReadHeaderTimeout: 10 * time.Second,
			IdleTimeout:       120 * time.Second,
			MaxHeaderBytes:    1 << 20,
		}
	}

	servers := []namedHTTPServer{{
		name:   "public",
		server: newHTTPServer("public", env.Config.Server.PublicAddr(), srv),
	}}

	if managementAddr := env.Config.Server.ManagementAddr(); managementAddr != "" {
		slog.Warn(
			"management listener serves /admin and /metrics without Gestalt auth; protect server.management with private networking or an internal reverse proxy",
			"addr", managementAddr,
		)

		managementConfig := baseServerConfig
		managementConfig.RouteProfile = server.RouteProfileManagement
		managementConfig.MCPHandler = nil
		managementConfig.ClientUI = nil
		managementConfig.AdminUI = managementAdminUI
		managementSrv, err := server.New(managementConfig)
		if err != nil {
			return fmt.Errorf("creating management server: %w", err)
		}
		servers = append(servers, namedHTTPServer{
			name:   "management",
			server: newHTTPServer("management", managementAddr, managementSrv),
		})
	}

	type listenFailure struct {
		name string
		err  error
	}
	listenErr := make(chan listenFailure, len(servers))
	for _, entry := range servers {
		entry := entry
		go func() {
			slog.Info("gestaltd listening", "listener", entry.name, "addr", entry.server.Addr)
			if err := entry.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				listenErr <- listenFailure{name: entry.name, err: err}
			}
		}()
	}

	defer func() {
		drainCtx, drainCancel := context.WithTimeout(context.Background(), gracefulShutdownTimeout)
		defer drainCancel()
		for _, entry := range servers {
			if err := entry.server.Shutdown(drainCtx); err != nil {
				slog.Warn("server shutdown", "listener", entry.name, "error", err)
			}
		}
	}()

	select {
	case <-result.ProvidersReady:
		slog.Info("all providers ready", "count", len(result.Providers.List()))
	case failure := <-listenErr:
		return fmt.Errorf("%s http server: %v", failure.name, failure.err)
	case <-env.Ctx.Done():
		return nil
	}

	if err := result.Start(env.Ctx); err != nil {
		return err
	}

	mcpInner, err := mcpSurface.handler(result, mcpInvoker)
	if err != nil {
		return err
	}
	mcpSlot.Set(mcpInner)
	slog.Info("MCP endpoint enabled", "path", "/mcp")

	select {
	case failure := <-listenErr:
		return fmt.Errorf("%s http server: %v", failure.name, failure.err)
	case <-env.Ctx.Done():
	}

	return nil
}

const (
	clientUIDirEnv = "GESTALTD_CLIENT_UI_DIR"
	adminUIDirEnv  = "GESTALTD_ADMIN_UI_DIR"
)

func resolveUIHandlers(cfg *config.Config) (http.Handler, http.Handler, http.Handler, error) {
	clientUI, err := resolveClientUIHandler(cfg)
	if err != nil {
		return nil, nil, nil, err
	}

	publicAdminUI, err := resolveAdminUIHandler(adminui.Options{
		BrandHref:    "/",
		ClientUIHref: "/",
	})
	if err != nil {
		return nil, nil, nil, err
	}

	managementClientUIHref := ""
	if cfg.Server.BaseURL != "" {
		managementClientUIHref = cfg.Server.BaseURL
	}
	managementAdminUI, err := resolveAdminUIHandler(adminui.Options{
		BrandHref:    "/admin/",
		ClientUIHref: managementClientUIHref,
	})
	if err != nil {
		return nil, nil, nil, err
	}

	return clientUI, publicAdminUI, managementAdminUI, nil
}

func resolveClientUIHandler(cfg *config.Config) (http.Handler, error) {
	if dir := strings.TrimSpace(os.Getenv(clientUIDirEnv)); dir != "" {
		return webui.DirHandler(dir)
	}
	if cfg.UI.Plugin == nil {
		return webui.EmbeddedHandler(), nil
	}
	if cfg.UI.Plugin.ResolvedAssetRoot != "" {
		return webui.DirHandler(cfg.UI.Plugin.ResolvedAssetRoot)
	}
	return nil, fmt.Errorf("ui plugin configured but asset root not resolved")
}

func resolveAdminUIHandler(opts adminui.Options) (http.Handler, error) {
	if dir := strings.TrimSpace(os.Getenv(adminUIDirEnv)); dir != "" {
		return adminui.DirHandler(dir, opts)
	}
	handler := adminui.EmbeddedHandler(opts)
	if handler == nil {
		return nil, fmt.Errorf("embedded admin ui assets not found")
	}
	return handler, nil
}

type mcpSurface struct {
	providers     []string
	toolPrefixes  map[string]string
	apiConnection map[string]string
	mcpConnection map[string]string
}

func buildMCPSurface(cfg *config.Config, connMaps bootstrap.ConnectionMaps) mcpSurface {
	surface := mcpSurface{
		providers:     []string{},
		toolPrefixes:  make(map[string]string),
		apiConnection: make(map[string]string),
		mcpConnection: make(map[string]string),
	}

	for name, intg := range cfg.Integrations {
		if intg.Plugin == nil {
			continue
		}
		if !intg.Plugin.DeclaresMCP() {
			continue
		}
		surface.providers = append(surface.providers, name)
		surface.apiConnection[name] = connMaps.APIConnection[name]
		surface.mcpConnection[name] = connMaps.MCPConnection[name]
		if intg.MCPToolPrefix == "" && intg.Plugin.HasManagedSource() {
			if src, err := pluginsource.Parse(intg.Plugin.SourceRef()); err == nil {
				surface.toolPrefixes[name] = src.Plugin + "_"
			}
		}
		if intg.MCPToolPrefix != "" {
			surface.toolPrefixes[name] = intg.MCPToolPrefix
		}
	}

	return surface
}

func (s mcpSurface) handler(result *bootstrap.Result, invoker invocation.Invoker) (http.Handler, error) {
	broker, ok := result.Invoker.(*invocation.Broker)
	if !ok {
		return nil, fmt.Errorf("MCP token resolution requires *invocation.Broker as invoker")
	}
	return mcpserver.NewStreamableHTTPServer(
		gestaltmcp.NewServer(gestaltmcp.Config{
			Invoker:          invoker,
			TokenResolver:    broker,
			AuditSink:        result.AuditSink,
			Providers:        result.Providers,
			AllowedProviders: s.providers,
			ToolPrefixes:     s.toolPrefixes,
			APIConnection:    s.apiConnection,
			MCPConnection:    s.mcpConnection,
		}),
		mcpserver.WithStateLess(true),
	), nil
}

func runInit(args []string) error {
	fs := flag.NewFlagSet("gestaltd init", flag.ContinueOnError)
	fs.Usage = func() { printInitUsage(fs.Output()) }
	configPath := fs.String("config", "", "path to config file")
	artifactsDir := fs.String("artifacts-dir", "", "path to writable prepared-artifacts directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	return initConfigWithArtifactsDir(*configPath, *artifactsDir)
}

func runValidate(args []string) error {
	fs := flag.NewFlagSet("gestaltd validate", flag.ContinueOnError)
	fs.Usage = func() { printValidateUsage(fs.Output()) }
	configPath := fs.String("config", "", "path to config file")
	artifactsDir := fs.String("artifacts-dir", "", "path to writable prepared-artifacts directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	return validateConfigWithArtifactsDir(*configPath, *artifactsDir)
}

func validateConfigWithArtifactsDir(configFlag, artifactsDir string) error {
	path, cfg, err := loadConfigForExecutionWithArtifactsDir(configFlag, artifactsDir, true)
	if err != nil {
		return err
	}

	warnings, err := bootstrap.Validate(context.Background(), cfg, buildFactories())
	if err != nil {
		return err
	}

	logConfigSummary(path, cfg)
	for _, w := range warnings {
		slog.Warn(w)
	}
	slog.Info("config ok")
	return nil
}

func logConfigSummary(path string, cfg *config.Config) {
	slog.Info("config loaded",
		"config_file", path,
		"server_port", cfg.Server.Port,
		"server_public_addr", cfg.Server.PublicAddr(),
		"server_management_addr", maskEmpty(cfg.Server.ManagementAddr()),
		"server_base_url", maskEmpty(cfg.Server.BaseURL),
		"server_encryption", maskSecret(cfg.Server.EncryptionKey),
		"auth_provider", cfg.Auth.Provider,
		"datastore_provider", cfg.Datastore.Provider,
		"secrets_provider", cfg.Secrets.Provider,
		"telemetry_provider", cfg.Telemetry.Provider,
	)

	for name, intg := range cfg.Integrations {
		if intg.Plugin != nil && intg.Plugin.IsInline() {
			slog.Info("integration configured", "integration", name, "type", "inline")
		} else {
			slog.Info("integration configured", "integration", name, "type", "plugin")
		}
	}

}

func maskSecret(s string) string {
	if s == "" {
		return "(not set)"
	}
	return "***"
}

func maskEmpty(s string) string {
	if s == "" {
		return "(not set)"
	}
	return s
}

func printMainUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd [--config PATH] [--artifacts-dir PATH]")
	writeUsageLine(w, "  gestaltd init [--config PATH] [--artifacts-dir PATH]")
	writeUsageLine(w, "  gestaltd serve [--config PATH] [--artifacts-dir PATH] [--locked]")
	writeUsageLine(w, "  gestaltd plugin <command> [flags]")
	writeUsageLine(w, "  gestaltd validate [--config PATH] [--artifacts-dir PATH]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Commands:")
	writeUsageLine(w, "  init        Resolve providers and plugins and write lock state")
	writeUsageLine(w, "  serve       Start the server (use --locked for production)")
	writeUsageLine(w, "  plugin      Build plugin release archives")
	writeUsageLine(w, "  validate    Load and validate configuration without starting the server")
	writeUsageLine(w, "  version     Print the version and exit")
	writeUsageLine(w, "")
	writeUsageLine(w, "Flags:")
	writeUsageLine(w, "  --config          Path to the config file")
	writeUsageLine(w, "  --artifacts-dir   Path to writable prepared-artifacts directory")
}

func printServeUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd serve [--config PATH] [--artifacts-dir PATH] [--locked]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Start the server. Auto-inits if lock state is missing or stale.")
	writeUsageLine(w, "Use --locked for production deployments to prevent automatic mutation")
	writeUsageLine(w, "at startup. When locked, run `gestaltd init` first to prepare artifacts.")
}

func printInitUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd init [--config PATH] [--artifacts-dir PATH]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Resolve managed plugin sources and write lock state.")
	writeUsageLine(w, "Creates gestalt.lock.json in the config directory and prepared artifacts")
	writeUsageLine(w, "in the artifacts directory.")
	writeUsageLine(w, "Use this before `gestaltd serve --locked` for production deployments.")
	writeUsageLine(w, "")
	writeUsageLine(w, "Flags:")
	writeUsageLine(w, "  --config          Path to the config file")
	writeUsageLine(w, "  --artifacts-dir   Path to writable prepared-artifacts directory")
}

func printValidateUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd validate [--config PATH] [--artifacts-dir PATH]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Validate configuration without starting the server or running migrations.")
}

func writeUsageLine(w io.Writer, line string) {
	_, _ = fmt.Fprintln(w, line)
}

type lateHandler struct {
	mu      sync.RWMutex
	handler http.Handler
}

func (h *lateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	inner := h.handler
	h.mu.RUnlock()
	if inner == nil {
		http.Error(w, "service starting", http.StatusServiceUnavailable)
		return
	}
	inner.ServeHTTP(w, r)
}

func (h *lateHandler) Set(handler http.Handler) {
	h.mu.Lock()
	h.handler = handler
	h.mu.Unlock()
}

func readinessFromChannel(ch <-chan struct{}, reason string) server.ReadinessChecker {
	return func() string {
		select {
		case <-ch:
			return ""
		default:
			return reason
		}
	}
}

func composeReadiness(checks ...server.ReadinessChecker) server.ReadinessChecker {
	return func() string {
		for _, check := range checks {
			if reason := check(); reason != "" {
				return reason
			}
		}
		return ""
	}
}

func datastoreReadiness(ds core.Datastore) server.ReadinessChecker {
	return func() string {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := ds.Ping(ctx); err != nil {
			return "datastore unavailable"
		}
		return ""
	}
}
