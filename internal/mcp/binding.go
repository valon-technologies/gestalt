package mcp

import (
	"context"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/invocation"
	"github.com/valon-technologies/gestalt/internal/principal"
	"github.com/valon-technologies/gestalt/internal/registry"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

const (
	serverName    = "gestalt"
	serverVersion = "0.1.0"
	toolNameSep   = "_"
)

type TokenResolver interface {
	ResolveToken(ctx context.Context, p *principal.Principal, providerName, connection, instance string) (string, error)
}

type directToolCaller interface {
	CallTool(ctx context.Context, name string, args map[string]any) (*mcpgo.CallToolResult, error)
}

type Config struct {
	Invoker          invocation.Invoker
	TokenResolver    TokenResolver
	Providers        *registry.PluginMap[core.Provider]
	AllowedProviders []string
	ToolPrefixes     map[string]string
	IncludeREST      map[string]bool
	APIConnection    map[string]string
	MCPConnection    map[string]string
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

		if cp, ok := prov.(core.CatalogProvider); ok {
			if cat := cp.Catalog(); cat != nil {
				addCatalogTools(srv, cfg, provName, cat, prov)
				continue
			}
		}

		addFlatTools(srv, cfg, provName, prov)
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
