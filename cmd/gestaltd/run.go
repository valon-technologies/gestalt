package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/crypto"
	"github.com/valon-technologies/gestalt/internal/bootstrap"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/invocation"
	gestaltmcp "github.com/valon-technologies/gestalt/internal/mcp"
	"github.com/valon-technologies/gestalt/internal/pluginpkg"
	"github.com/valon-technologies/gestalt/internal/pluginsource"
	"github.com/valon-technologies/gestalt/internal/server"
	"github.com/valon-technologies/gestalt/internal/webui"

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
		case "bundle":
			return runBundle(args[1:])
		case "validate":
			return runValidate(args[1:])
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
	locked := fs.Bool("locked", false, "require exact lock state; do not auto-init")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	env, err := setupBootstrap(*configPath, *locked)
	if err != nil {
		return err
	}
	return runServer(env)
}

func runStartCommand(name string, usage func(io.Writer), args []string, locked bool, autoGenerate bool) error {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.Usage = func() { usage(fs.Output()) }
	configPath := fs.String("config", "", "path to config file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	if autoGenerate && *configPath == "" {
		resolved := resolveConfigPath("")
		if _, err := os.Stat(resolved); os.IsNotExist(err) {
			if p := defaultLocalConfigPath(); p != "" {
				generated, genErr := generateDefaultConfig(filepath.Dir(p))
				if genErr == nil {
					*configPath = generated
				}
			}
		}
	}

	env, err := setupBootstrap(*configPath, locked)
	if err != nil {
		return err
	}
	return runServer(env)
}

func runServer(env *bootstrapEnv) error {
	defer env.Close()

	result := env.Result

	mcpSurface := buildMCPSurface(env.Config)

	if env.Config.Server.BaseURL != "" {
		log.Printf("gestaltd base URL: %s", env.Config.Server.BaseURL)
		log.Printf("  auth callback:        %s%s", env.Config.Server.BaseURL, config.AuthCallbackPath)
		log.Printf("  integration callback: %s%s", env.Config.Server.BaseURL, config.IntegrationCallbackPath)
	}

	uiHandler, err := resolveUIHandler(env.Config)
	if err != nil {
		return fmt.Errorf("resolving ui handler: %w", err)
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

	srv, err := server.New(server.Config{
		Auth:              result.Auth,
		Datastore:         result.Datastore,
		Providers:         result.Providers,
		Runtimes:          result.Runtimes,
		Bindings:          result.Bindings,
		Invoker:           result.Invoker,
		DefaultConnection: bootstrap.BuildConnectionMap(env.Config),
		ConnectionAuth:    result.ConnectionAuth,
		IntegrationDefs:   env.Config.Integrations,
		SecureCookies:     strings.HasPrefix(env.Config.Server.BaseURL, "https://"),
		StateSecret:       crypto.DeriveKey(env.Config.Server.EncryptionKey),
		APITokenTTL:       apiTokenTTL,
		Readiness: composeReadiness(
			readinessFromChannel(result.ProvidersReady, "providers loading"),
			datastoreReadiness(result.Datastore),
		),
		MCPHandler: mcpHandler,
		WebUI:      uiHandler,
	})
	if err != nil {
		return fmt.Errorf("creating server: %w", err)
	}

	addr := fmt.Sprintf(":%d", env.Config.Server.Port)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
	}

	listenErr := make(chan error, 1)
	go func() {
		log.Printf("gestaltd listening on %s", addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			listenErr <- err
		}
	}()

	defer func() {
		drainCtx, drainCancel := context.WithTimeout(context.Background(), gracefulShutdownTimeout)
		defer drainCancel()
		if err := httpServer.Shutdown(drainCtx); err != nil {
			log.Printf("server shutdown: %v", err)
		}
	}()

	select {
	case <-result.ProvidersReady:
		log.Printf("all providers ready (%d loaded)", len(result.Providers.List()))
	case err := <-listenErr:
		return fmt.Errorf("http server: %v", err)
	case <-env.Ctx.Done():
		return nil
	}

	if err := result.Start(env.Ctx); err != nil {
		return err
	}

	mcpInner, err := mcpSurface.handler(result)
	if err != nil {
		return err
	}
	mcpSlot.Set(mcpInner)
	log.Println("MCP endpoint enabled at /mcp")

	select {
	case err := <-listenErr:
		return fmt.Errorf("http server: %v", err)
	case <-env.Ctx.Done():
	}

	return nil
}

func resolveUIHandler(cfg *config.Config) (http.Handler, error) {
	if cfg.UI.Plugin == nil {
		return webui.EmbeddedHandler(), nil
	}
	if cfg.UI.Plugin.ResolvedAssetRoot != "" {
		return webui.DirHandler(cfg.UI.Plugin.ResolvedAssetRoot)
	}
	return nil, fmt.Errorf("ui plugin configured but asset root not resolved")
}

type mcpSurface struct {
	providers     []string
	toolPrefixes  map[string]string
	includeREST   map[string]bool
	apiConnection map[string]string
	mcpConnection map[string]string
}

func buildMCPSurface(cfg *config.Config) mcpSurface {
	surface := mcpSurface{
		providers:     []string{},
		toolPrefixes:  make(map[string]string),
		includeREST:   make(map[string]bool),
		apiConnection: make(map[string]string),
		mcpConnection: make(map[string]string),
	}

	for name, intg := range cfg.Integrations {
		if intg.Plugin != nil {
			if !pluginDeclaresMCP(intg.Plugin) {
				continue
			}
			surface.providers = append(surface.providers, name)
			surface.apiConnection[name] = config.PluginConnectionName
			surface.mcpConnection[name] = config.PluginConnectionName
			if intg.MCPToolPrefix == "" && intg.Plugin.Source != "" {
				if src, err := pluginsource.Parse(intg.Plugin.Source); err == nil {
					surface.toolPrefixes[name] = src.Plugin + "_"
				}
			}
		} else if intg.API != nil || intg.MCP != nil {
			surface.providers = append(surface.providers, name)
			if intg.API != nil {
				surface.includeREST[name] = true
				surface.apiConnection[name] = intg.API.Connection
			}
			if intg.MCP != nil {
				surface.mcpConnection[name] = intg.MCP.Connection
			}
		}
		if intg.MCPToolPrefix != "" {
			surface.toolPrefixes[name] = intg.MCPToolPrefix
		}
	}

	return surface
}

func pluginDeclaresMCP(plugin *config.ExecutablePluginDef) bool {
	if plugin.ResolvedManifestPath == "" {
		return true
	}
	_, manifest, err := pluginpkg.ReadManifestFile(plugin.ResolvedManifestPath)
	if err != nil {
		log.Printf("WARNING: reading plugin manifest %s: %v", plugin.ResolvedManifestPath, err)
		return true
	}
	return manifest.Provider != nil && manifest.Provider.MCP
}

func (s mcpSurface) handler(result *bootstrap.Result) (http.Handler, error) {
	broker, ok := result.Invoker.(*invocation.Broker)
	if !ok {
		return nil, fmt.Errorf("MCP token resolution requires *invocation.Broker as invoker")
	}
	return mcpserver.NewStreamableHTTPServer(
		gestaltmcp.NewServer(gestaltmcp.Config{
			Invoker:          result.Invoker,
			TokenResolver:    broker,
			Providers:        result.Providers,
			AllowedProviders: s.providers,
			ToolPrefixes:     s.toolPrefixes,
			IncludeREST:      s.includeREST,
			APIConnection:    s.apiConnection,
			MCPConnection:    s.mcpConnection,
		}),
		mcpserver.WithStateLess(true),
	), nil
}

func runValidate(args []string) error {
	fs := flag.NewFlagSet("gestaltd validate", flag.ContinueOnError)
	fs.Usage = func() { printValidateUsage(fs.Output()) }
	configPath := fs.String("config", "", "path to config file")
	initFirst := fs.Bool("init", false, "run init before validating")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	if *initFirst {
		if err := initConfig(*configPath); err != nil {
			return err
		}
	}

	return validateConfig(*configPath)
}

func validateConfig(configFlag string) error {
	path, cfg, preparedProviders, err := loadConfigForExecution(configFlag, true)
	if err != nil {
		return fmt.Errorf("config invalid: %v", err)
	}

	warnings, err := bootstrap.Validate(context.Background(), cfg, buildFactories(preparedProviders))
	if err != nil {
		return fmt.Errorf("config invalid: %v", err)
	}

	logConfigSummary(path, cfg)
	for _, w := range warnings {
		log.Printf("WARNING: %s", w)
	}
	log.Printf("config ok")
	return nil
}

func logConfigSummary(path string, cfg *config.Config) {
	log.Printf("config file: %s", path)
	log.Printf("server:")
	log.Printf("  port:       %d", cfg.Server.Port)
	log.Printf("  base_url:   %s", maskEmpty(cfg.Server.BaseURL))
	log.Printf("  encryption: %s", maskSecret(cfg.Server.EncryptionKey))
	log.Printf("auth:")
	log.Printf("  provider:   %s", cfg.Auth.Provider)
	log.Printf("datastore:")
	log.Printf("  provider:   %s", cfg.Datastore.Provider)
	log.Printf("secrets:")
	log.Printf("  provider:   %s", cfg.Secrets.Provider)

	if len(cfg.Integrations) > 0 {
		log.Printf("integrations: %d", len(cfg.Integrations))
		for name, intg := range cfg.Integrations {
			if intg.Plugin != nil {
				log.Printf("  %s: plugin", name)
			} else {
				var surfaces []string
				if intg.API != nil {
					surfaces = append(surfaces, intg.API.Type)
				}
				if intg.MCP != nil {
					surfaces = append(surfaces, "mcp")
				}
				log.Printf("  %s: surfaces=[%s] connections=%d", name, strings.Join(surfaces, ","), len(intg.Connections))
			}
		}
	}

	if len(cfg.Runtimes) > 0 {
		log.Printf("runtimes: %d", len(cfg.Runtimes))
		for name := range cfg.Runtimes {
			rt := cfg.Runtimes[name]
			log.Printf("  %s: type=%s providers=%s", name, rt.Type, strings.Join(rt.Providers, ","))
		}
	}

	if len(cfg.Bindings) > 0 {
		log.Printf("bindings: %d", len(cfg.Bindings))
		for name := range cfg.Bindings {
			b := cfg.Bindings[name]
			log.Printf("  %s: type=%s", name, b.Type)
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
	writeUsageLine(w, "  gestaltd [--config PATH]")
	writeUsageLine(w, "  gestaltd bundle --config PATH --output DIR")
	writeUsageLine(w, "  gestaltd serve [--config PATH] [--locked]")
	writeUsageLine(w, "  gestaltd plugin <command> [flags]")
	writeUsageLine(w, "  gestaltd validate [--config PATH] [--init]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Commands:")
	writeUsageLine(w, "  bundle      Prepare a self-contained bundle for production deployment")
	writeUsageLine(w, "  serve       Start the server (use --locked for production)")
	writeUsageLine(w, "  plugin      Package plugins for distribution")
	writeUsageLine(w, "  validate    Load and validate configuration without starting the server")
	writeUsageLine(w, "  version     Print the version and exit")
	writeUsageLine(w, "")
	writeUsageLine(w, "Flags:")
	writeUsageLine(w, "  --config    Path to the config file")
}

func printServeUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd serve [--config PATH] [--locked]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Start the server. Auto-inits if lock state is missing or stale.")
	writeUsageLine(w, "Use --locked for production deployments to prevent automatic mutation")
	writeUsageLine(w, "at startup. When locked, run `gestaltd bundle` first to prepare state.")
}

func printValidateUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd validate [--config PATH] [--init]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Validate configuration without starting the server or running migrations.")
	writeUsageLine(w, "Non-mutating by default; requires existing lock state. Use --init to")
	writeUsageLine(w, "hydrate before validating.")
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
