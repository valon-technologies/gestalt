package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/crypto"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/invocation"
	gestaltmcp "github.com/valon-technologies/gestalt/internal/mcp"
	"github.com/valon-technologies/gestalt/internal/registry"
	"github.com/valon-technologies/gestalt/internal/server"

	mcpserver "github.com/mark3labs/mcp-go/server"
)

func run(args []string) error {
	fs := flag.NewFlagSet("gestaltd", flag.ContinueOnError)
	configPath := fs.String("config", "", "path to config file")
	check := fs.Bool("check", false, "validate configuration and exit")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *check {
		return runCheck(*configPath)
	}

	env, err := setupBootstrap(*configPath)
	if err != nil {
		return err
	}
	defer env.Close()

	if err := startPlugins(env); err != nil {
		return err
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), gracefulShutdownTimeout)
		defer cancel()
		shutdownPlugins(ctx, env)
	}()

	result := env.Result

	var mcpHandler http.Handler
	if env.Config.MCP.Enabled {
		broker, ok := result.Invoker.(*invocation.Broker)
		if !ok {
			return fmt.Errorf("MCP token resolution requires *invocation.Broker as invoker")
		}
		mcpCfg := gestaltmcp.Config{
			Invoker:       result.Invoker,
			TokenResolver: broker,
			Providers:     result.Providers,
		}
		if env.Config.MCP.Providers != nil {
			mcpCfg.AllowedProviders = env.Config.MCP.Providers
		}
		if env.Config.MCP.ToolNamePrefix != "" {
			mcpCfg.ToolNamePrefix = env.Config.MCP.ToolNamePrefix
		}
		mcpHandler = mcpserver.NewStreamableHTTPServer(
			gestaltmcp.NewServer(mcpCfg),
			mcpserver.WithStateLess(true),
		)
		log.Println("MCP endpoint enabled at /mcp")
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

	select {
	case err := <-listenErr:
		return fmt.Errorf("http server: %v", err)
	case <-env.Ctx.Done():
	}
	log.Println("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), gracefulShutdownTimeout)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("server shutdown: %v", err)
	}

	log.Println("shutdown complete")
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
