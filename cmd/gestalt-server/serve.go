package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
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

func runServe(args []string) error {
	env, err := setupBootstrap("serve", args)
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
		log.Printf("gestalt-server base URL: %s", env.Config.Server.BaseURL)
		log.Printf("  auth callback:        %s%s", env.Config.Server.BaseURL, config.AuthCallbackPath)
		log.Printf("  integration callback: %s%s", env.Config.Server.BaseURL, config.IntegrationCallbackPath)
	}

	listenErr := make(chan error, 1)
	go func() {
		log.Printf("gestalt-server listening on %s", addr)
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
