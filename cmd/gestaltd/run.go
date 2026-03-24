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
		case "validate":
			return runValidate(args[1:])
		case "bundle":
			return runBundle(args[1:])
		case "compile-providers":
			return runCompileProviders(args[1:])
		}
	}

	return runServe(args)
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("gestaltd", flag.ContinueOnError)
	fs.Usage = func() { printMainUsage(fs.Output()) }
	configPath := fs.String("config", "", "path to config file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	env, err := setupBootstrap(*configPath)
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
		for _, us := range intg.Upstreams {
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
		MCPHandler:  mcpHandler,
		WebUI:       webui.Handler(),
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

func runCompileProviders(args []string) error {
	fs := flag.NewFlagSet("gestaltd compile-providers", flag.ContinueOnError)
	fs.Usage = func() { printCompileProvidersUsage(fs.Output()) }
	configPath := fs.String("config", "", "path to config file")
	outputDir := fs.String("output-dir", "", "directory to write provider artifacts into")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if *outputDir == "" {
		return fmt.Errorf("--output-dir is required")
	}

	return compileProviders(*configPath, *outputDir)
}

func runBundle(args []string) error {
	fs := flag.NewFlagSet("gestaltd bundle", flag.ContinueOnError)
	fs.Usage = func() { printBundleUsage(fs.Output()) }
	configPath := fs.String("config", "", "path to config file")
	outputDir := fs.String("output-dir", "", "directory to write bundle into")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if *outputDir == "" {
		return fmt.Errorf("--output-dir is required")
	}

	return bundleConfig(*configPath, *outputDir)
}

func validateConfig(configFlag string) error {
	path := resolveConfigPath(configFlag)

	cfg, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("config invalid: %v", err)
	}

	warnings, err := bootstrap.Validate(context.Background(), cfg, buildFactories(cfg.ProviderDirs, cfg.Server.DevMode))
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
			for _, us := range intg.Upstreams {
				sources = append(sources, us.Type)
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
	writeUsageLine(w, "  gestaltd validate [--config PATH]")
	writeUsageLine(w, "  gestaltd bundle [--config PATH] --output-dir DIR")
	writeUsageLine(w, "  gestaltd compile-providers [--config PATH] --output-dir DIR")
	writeUsageLine(w, "")
	writeUsageLine(w, "Commands:")
	writeUsageLine(w, "  validate    Load and validate configuration, then exit")
	writeUsageLine(w, "  bundle      Build a deployable config bundle with local provider artifacts")
	writeUsageLine(w, "  compile-providers  Build deterministic provider artifacts from configured REST/GraphQL upstreams")
	writeUsageLine(w, "")
	writeUsageLine(w, "Flags:")
	writeUsageLine(w, "  --config    Path to the config file")
}

func printValidateUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd validate [--config PATH]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Validate the configuration using the daemon's full bootstrap path,")
	writeUsageLine(w, "without starting the server or running datastore migrations.")
}

func printCompileProvidersUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd compile-providers [--config PATH] --output-dir DIR")
	writeUsageLine(w, "")
	writeUsageLine(w, "Resolve configured REST and GraphQL upstreams into deterministic local")
	writeUsageLine(w, "provider definition artifacts. Integrations with only MCP upstreams are skipped.")
}

func printBundleUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd bundle [--config PATH] --output-dir DIR")
	writeUsageLine(w, "")
	writeUsageLine(w, "Write a self-contained deploy bundle with:")
	writeUsageLine(w, "  - config.yaml rewritten to use local provider artifacts")
	writeUsageLine(w, "  - providers/*.json artifacts for REST and GraphQL integrations")
	writeUsageLine(w, "")
	writeUsageLine(w, "This is the recommended production packaging workflow. Use")
	writeUsageLine(w, "compile-providers directly only when you need the low-level artifacts.")
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
