package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/valon-technologies/toolshed/internal/invocation"
	toolshedmcp "github.com/valon-technologies/toolshed/internal/mcp"
	"github.com/valon-technologies/toolshed/internal/principal"

	mcpserver "github.com/mark3labs/mcp-go/server"
)

const apiKeyEnv = "TOOLSHED_API_KEY"

func runMCP(args []string) error {
	env, err := setupBootstrap("mcp", args)
	if err != nil {
		return err
	}
	defer env.Close()

	resolver := principal.NewResolver(env.Result.Auth, env.Result.Datastore)
	apiKey := os.Getenv(apiKeyEnv)
	if apiKey == "" {
		return fmt.Errorf("%s environment variable is required", apiKeyEnv)
	}
	p, err := resolver.ResolveToken(env.Ctx, apiKey)
	if err != nil {
		return fmt.Errorf("resolving principal: %v", err)
	}

	broker := invocation.NewBroker(env.Result.Providers, env.Result.Datastore)

	mcpCfg := toolshedmcp.Config{
		Broker:    broker,
		Providers: env.Result.Providers,
	}
	if env.Config.MCP.Providers != nil {
		mcpCfg.AllowedProviders = env.Config.MCP.Providers
	}
	if env.Config.MCP.ToolNamePrefix != "" {
		mcpCfg.ToolNamePrefix = env.Config.MCP.ToolNamePrefix
	}

	mcpSrv := toolshedmcp.NewServer(mcpCfg)

	transport := mcpserver.NewStdioServer(mcpSrv)
	transport.SetContextFunc(func(ctx context.Context) context.Context {
		return principal.WithPrincipal(ctx, p)
	})

	log.Printf("toolshed MCP server starting on stdio")
	return transport.Listen(env.Ctx, os.Stdin, os.Stdout)
}
