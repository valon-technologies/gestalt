package mcp

import (
	"context"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/registry"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

const (
	serverName    = "gestalt"
	serverVersion = "0.1.0"
	toolNameSep   = "_"
)

type Config struct {
	Invoker          invocation.Invoker
	TokenResolver    invocation.TokenResolver
	AuditSink        core.AuditSink
	Providers        *registry.PluginMap[core.Provider]
	AllowedProviders []string
	ToolPrefixes     map[string]string
	IncludeREST      map[string]bool
	MCPConnection map[string]string
}

func NewServer(cfg Config) *mcpserver.MCPServer {
	hooks := &mcpserver.Hooks{}
	srv := mcpserver.NewMCPServer(
		serverName,
		serverVersion,
		mcpserver.WithHooks(hooks),
		mcpserver.WithToolCapabilities(true),
	)

	allowed := make(map[string]struct{}, len(cfg.AllowedProviders))
	for _, p := range cfg.AllowedProviders {
		allowed[p] = struct{}{}
	}

	var dynamicProviders []string
	for _, provName := range cfg.Providers.List() {
		if cfg.AllowedProviders != nil {
			if _, ok := allowed[provName]; !ok {
				continue
			}
		}

		prov, err := cfg.Providers.Get(provName)
		if err != nil {
			continue
		}

		if _, ok := prov.(core.SessionCatalogProvider); ok {
			dynamicProviders = append(dynamicProviders, provName)
		}

		if cat := prov.Catalog(); cat != nil {
			addCatalogTools(srv, cfg, provName, cat)
		}
	}

	if len(dynamicProviders) > 0 {
		hooks.AddBeforeListTools(func(ctx context.Context, _ any, _ *mcpgo.ListToolsRequest) {
			hydrateSessionTools(ctx, cfg, dynamicProviders)
		})
		hooks.AddBeforeCallTool(func(ctx context.Context, _ any, req *mcpgo.CallToolRequest) {
			if provName := providerNameForTool(cfg.ToolPrefixes, dynamicProviders, req.Params.Name); provName != "" {
				hydrateSessionTools(ctx, cfg, []string{provName})
			}
		})
	}

	return srv
}
