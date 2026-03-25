package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/crypto"
	"github.com/valon-technologies/gestalt/internal/bootstrap"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/invocation"
	gestaltmcp "github.com/valon-technologies/gestalt/internal/mcp"
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
		case "plugin":
			return runPlugin(args[1:])
		case "dev":
			return runDev(args[1:])
		case "serve":
			return runServe(args[1:])
		case "prepare":
			return runPrepare(args[1:])
		case "validate":
			return runValidate(args[1:])
		}
	}

	return runDefaultServe(args)
}

func runDefaultServe(args []string) error {
	return runStartCommand("gestaltd", printMainUsage, args, providerResolutionPrefer)
}

func runDev(args []string) error {
	return runStartCommand("gestaltd dev", printDevUsage, args, providerResolutionAuto)
}

func runServe(args []string) error {
	return runStartCommand("gestaltd serve", printServeUsage, args, providerResolutionRequire)
}

func runStartCommand(name string, usage func(io.Writer), args []string, mode providerResolutionMode) error {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.Usage = func() { usage(fs.Output()) }
	configPath := fs.String("config", "", "path to config file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	env, err := setupBootstrap(*configPath, mode)
	if err != nil {
		return err
	}
	defer env.Close()

	result := env.Result

	var mcpProviders []string
	mcpPrefixes := make(map[string]string)
	includeHTTP := make(map[string]bool)
	for name := range env.Config.Integrations {
		intg := env.Config.Integrations[name]
		hasMCP := false
		apiMCP := false
		for i := range intg.Upstreams {
			us := &intg.Upstreams[i]
			if us.Type == config.UpstreamTypeMCP {
				hasMCP = true
			}
			if (us.Type == config.UpstreamTypeREST || us.Type == config.UpstreamTypeGraphQL) && us.MCP {
				hasMCP = true
				apiMCP = true
			}
		}
		if hasMCP {
			mcpProviders = append(mcpProviders, name)
			includeHTTP[name] = apiMCP
			if intg.MCPToolPrefix != "" {
				mcpPrefixes[name] = intg.MCPToolPrefix
			}
		}
	}

	if env.Config.Server.BaseURL != "" {
		log.Printf("gestaltd base URL: %s", env.Config.Server.BaseURL)
		log.Printf("  auth callback:        %s%s", env.Config.Server.BaseURL, config.AuthCallbackPath)
		log.Printf("  integration callback: %s%s", env.Config.Server.BaseURL, config.IntegrationCallbackPath)
	}

	var mcpSlot *lateHandler
	var mcpHandler http.Handler
	if len(mcpProviders) > 0 {
		mcpSlot = &lateHandler{}
		mcpHandler = mcpSlot
	}

	srv, err := server.New(server.Config{
		Auth:        result.Auth,
		Datastore:   result.Datastore,
		Providers:   result.Providers,
		Runtimes:    result.Runtimes,
		Bindings:    result.Bindings,
		Invoker:     result.Invoker,
		DevMode:     result.DevMode,
		StateSecret: crypto.DeriveKey(env.Config.Server.EncryptionKey),
		Readiness: composeReadiness(
			readinessFromChannel(result.ProvidersReady, "providers loading"),
			datastoreReadiness(result.Datastore),
		),
		MCPHandler: mcpHandler,
		WebUI:      webui.Handler(),
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

	pluginsStarted := false
	defer func() {
		drainCtx, drainCancel := context.WithTimeout(context.Background(), gracefulShutdownTimeout)
		defer drainCancel()
		if err := httpServer.Shutdown(drainCtx); err != nil {
			log.Printf("server shutdown: %v", err)
		}
		if pluginsStarted {
			pluginCtx, pluginCancel := context.WithTimeout(context.Background(), gracefulShutdownTimeout)
			defer pluginCancel()
			shutdownPlugins(pluginCtx, env)
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

	if err := startPlugins(env); err != nil {
		return err
	}
	pluginsStarted = true

	if mcpSlot != nil {
		broker, ok := result.Invoker.(*invocation.Broker)
		if !ok {
			return fmt.Errorf("MCP token resolution requires *invocation.Broker as invoker")
		}
		mcpCfg := gestaltmcp.Config{
			Invoker:          result.Invoker,
			TokenResolver:    broker,
			Providers:        result.Providers,
			AllowedProviders: mcpProviders,
			ToolPrefixes:     mcpPrefixes,
			IncludeHTTP:      includeHTTP,
		}
		mcpSlot.Set(mcpserver.NewStreamableHTTPServer(
			gestaltmcp.NewServer(mcpCfg),
			mcpserver.WithStateLess(true),
		))
		log.Println("MCP endpoint enabled at /mcp")
	}

	select {
	case err := <-listenErr:
		return fmt.Errorf("http server: %v", err)
	case <-env.Ctx.Done():
	}

	return nil
}

func runValidate(args []string) error {
	fs := flag.NewFlagSet("gestaltd validate", flag.ContinueOnError)
	fs.Usage = func() { printValidateUsage(fs.Output()) }
	configPath := fs.String("config", "", "path to config file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	return validateConfig(*configPath)
}

func validateConfig(configFlag string) error {
	path, cfg, preparedProviders, err := loadConfigForExecution(configFlag, providerResolutionPrefer)
	if err != nil {
		return fmt.Errorf("config invalid: %v", err)
	}

	warnings, err := bootstrap.Validate(context.Background(), cfg, buildFactories(preparedProviders, cfg.Server.DevMode))
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
	log.Printf("  dev_mode:   %t", cfg.Server.DevMode)
	log.Printf("  encryption: %s", maskSecret(cfg.Server.EncryptionKey))
	log.Printf("auth:")
	log.Printf("  provider:   %s", cfg.Auth.Provider)
	log.Printf("datastore:")
	log.Printf("  provider:   %s", cfg.Datastore.Provider)
	log.Printf("secrets:")
	log.Printf("  provider:   %s", cfg.Secrets.Provider)

	if len(cfg.Integrations) > 0 {
		log.Printf("integrations: %d", len(cfg.Integrations))
		for name := range cfg.Integrations {
			intg := cfg.Integrations[name]
			var sources []string
			for i := range intg.Upstreams {
				sources = append(sources, intg.Upstreams[i].Type)
			}
			auth := "oauth"
			if intg.Auth.Type == "manual" || intg.ConnectionMode == "manual" {
				auth = "manual"
			}
			log.Printf("  %s: upstreams=[%s] auth=%s", name, strings.Join(sources, ","), auth)
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
	writeUsageLine(w, "  gestaltd dev [--config PATH]")
	writeUsageLine(w, "  gestaltd plugin <command> [flags]")
	writeUsageLine(w, "  gestaltd serve [--config PATH]")
	writeUsageLine(w, "  gestaltd prepare [--config PATH]")
	writeUsageLine(w, "  gestaltd validate [--config PATH]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Commands:")
	writeUsageLine(w, "  dev         Refresh prepared provider artifacts if needed, then start the server")
	writeUsageLine(w, "  plugin      Package, install, inspect, or list plugins")
	writeUsageLine(w, "  serve       Start the server with prepared REST/GraphQL providers only")
	writeUsageLine(w, "  prepare     Resolve remote REST/GraphQL upstreams into gestalt.lock.json and .gestalt/providers/")
	writeUsageLine(w, "  validate    Load and validate configuration, then exit")
	writeUsageLine(w, "")
	writeUsageLine(w, "Flags:")
	writeUsageLine(w, "  --config    Path to the config file")
}

func printDevUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd dev [--config PATH]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Prepare remote REST/GraphQL providers into gestalt.lock.json and")
	writeUsageLine(w, ".gestalt/providers/ when needed, then start the server from the")
	writeUsageLine(w, "source config.")
}

func printServeUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd serve [--config PATH]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Start the server from the source config using only prepared")
	writeUsageLine(w, "REST/GraphQL providers. If remote providers are not prepared,")
	writeUsageLine(w, "run `gestaltd prepare` first.")
}

func printPrepareUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd prepare [--config PATH]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Write deterministic REST/GraphQL provider artifacts next to the source")
	writeUsageLine(w, "config using:")
	writeUsageLine(w, "  - gestalt.lock.json")
	writeUsageLine(w, "  - .gestalt/providers/*.json")
	writeUsageLine(w, "")
	writeUsageLine(w, "Use `gestaltd dev` locally and `gestaltd serve` in production.")
}

func printValidateUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd validate [--config PATH]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Validate the configuration using the daemon's full bootstrap path,")
	writeUsageLine(w, "without starting the server or running datastore migrations. If")
	writeUsageLine(w, "gestalt.lock.json is present, validation prefers prepared providers.")
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
