package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/valon-technologies/toolshed/core"
	"github.com/valon-technologies/toolshed/core/crypto"
	"github.com/valon-technologies/toolshed/internal/config"
	"github.com/valon-technologies/toolshed/internal/invocation"
	toolshedmcp "github.com/valon-technologies/toolshed/internal/mcp"
	"github.com/valon-technologies/toolshed/internal/registry"
	"github.com/valon-technologies/toolshed/internal/server"

	mcpserver "github.com/mark3labs/mcp-go/server"
)

func runServe(args []string) error {
	env, err := setupBootstrap("serve", args)
	if err != nil {
		return err
	}
	defer env.Close()

	result := env.Result

	if result.Runtimes != nil {
		var started []string
		for _, name := range result.Runtimes.List() {
			rt, err := result.Runtimes.Get(name)
			if err != nil {
				return fmt.Errorf("getting runtime %q: %v", name, err)
			}
			if err := rt.Start(env.Ctx); err != nil {
				stopRuntimes(env.Ctx, result.Runtimes, started)
				return fmt.Errorf("starting runtime %q: %v", name, err)
			}
			started = append(started, name)
		}
	}

	if result.Bindings != nil {
		var started []string
		for _, name := range result.Bindings.List() {
			binding, err := result.Bindings.Get(name)
			if err != nil {
				closeBindings(result.Bindings, started)
				if result.Runtimes != nil {
					stopRuntimes(env.Ctx, result.Runtimes, result.Runtimes.List())
				}
				return fmt.Errorf("getting binding %q: %v", name, err)
			}
			if err := binding.Start(env.Ctx); err != nil {
				closeBindings(result.Bindings, started)
				if result.Runtimes != nil {
					stopRuntimes(env.Ctx, result.Runtimes, result.Runtimes.List())
				}
				return fmt.Errorf("starting binding %q: %v", name, err)
			}
			started = append(started, name)
		}
	}

	broker := invocation.NewBroker(result.Providers, result.Datastore)

	var mcpHandler http.Handler
	if env.Config.MCP.Enabled {
		mcpCfg := toolshedmcp.Config{
			Broker:    broker,
			Providers: result.Providers,
		}
		if env.Config.MCP.Providers != nil {
			mcpCfg.AllowedProviders = env.Config.MCP.Providers
		}
		if env.Config.MCP.ToolNamePrefix != "" {
			mcpCfg.ToolNamePrefix = env.Config.MCP.ToolNamePrefix
		}
		mcpHandler = mcpserver.NewStreamableHTTPServer(
			toolshedmcp.NewServer(mcpCfg),
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
		Broker:      broker,
		DevMode:     result.DevMode,
		StateSecret: crypto.DeriveKey(env.Config.Server.EncryptionKey),
		MCPHandler:  mcpHandler,
	})
	if err != nil {
		if result.Bindings != nil {
			closeBindings(result.Bindings, result.Bindings.List())
		}
		if result.Runtimes != nil {
			stopRuntimes(env.Ctx, result.Runtimes, result.Runtimes.List())
		}
		return fmt.Errorf("creating server: %w", err)
	}

	addr := fmt.Sprintf(":%d", env.Config.Server.Port)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
	}

	if env.Config.Server.BaseURL != "" {
		log.Printf("toolshed base URL: %s", env.Config.Server.BaseURL)
		log.Printf("  auth callback:        %s%s", env.Config.Server.BaseURL, config.AuthCallbackPath)
		log.Printf("  integration callback: %s%s", env.Config.Server.BaseURL, config.IntegrationCallbackPath)
	}

	listenErr := make(chan error, 1)
	go func() {
		log.Printf("toolshed listening on %s", addr)
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

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server shutdown: %v", err)
	}

	if result.Runtimes != nil {
		stopRuntimes(shutdownCtx, result.Runtimes, result.Runtimes.List())
	}

	if result.Bindings != nil {
		closeBindings(result.Bindings, result.Bindings.List())
	}

	log.Println("shutdown complete")
	return nil
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
