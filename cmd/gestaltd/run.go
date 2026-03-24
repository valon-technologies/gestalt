package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/crypto"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/invocation"
	gestaltmcp "github.com/valon-technologies/gestalt/internal/mcp"
	"github.com/valon-technologies/gestalt/internal/registry"
	"github.com/valon-technologies/gestalt/internal/server"
	"github.com/valon-technologies/gestalt/internal/webui"

	mcpserver "github.com/mark3labs/mcp-go/server"
)

func run(args []string) error {
	fs := flag.NewFlagSet("gestaltd", flag.ContinueOnError)
	configPath := fs.String("config", "", "path to config file")
	check := fs.Bool("check", false, "validate configuration and exit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	if *check {
		return runCheck(*configPath)
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

	if env.Config.Server.BaseURL != "" {
		log.Printf("gestaltd base URL: %s", env.Config.Server.BaseURL)
		log.Printf("  auth callback:        %s%s", env.Config.Server.BaseURL, config.AuthCallbackPath)
		log.Printf("  integration callback: %s%s", env.Config.Server.BaseURL, config.IntegrationCallbackPath)
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

func runCheck(configFlag string) error {
	path := resolveConfigPath(configFlag)

	cfg, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("config invalid: %v", err)
	}

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

	log.Printf("config ok")
	return nil
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

func closeBindings(bindings *registry.PluginMap[core.Binding], names []string) {
	for _, name := range names {
		b, err := bindings.Get(name)
		if err != nil {
			log.Printf("looking up binding %q during shutdown: %v", name, err)
			continue
		}
		if err := b.Close(); err != nil {
			log.Printf("closing binding %q: %v", name, err)
		}
	}
}

func stopRuntimes(ctx context.Context, runtimes *registry.PluginMap[core.Runtime], names []string) {
	for _, name := range names {
		rt, err := runtimes.Get(name)
		if err != nil {
			log.Printf("looking up runtime %q during shutdown: %v", name, err)
			continue
		}
		if err := rt.Stop(ctx); err != nil {
			log.Printf("stopping runtime %q: %v", name, err)
		}
	}
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
