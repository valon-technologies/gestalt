package mcp

import (
	"context"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/authorization"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
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
	Providers        *registry.ProviderMap[core.Provider]
	Authorizer       authorization.RuntimeAuthorizer
	AllowedProviders []string
	ToolPrefixes     map[string]string
	IncludeREST      map[string]bool
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

	staticToolNames := map[string]struct{}{}
	visibleProviders := make([]string, 0, len(cfg.Providers.List()))
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

		if core.SupportsSessionCatalog(prov) {
			dynamicProviders = append(dynamicProviders, provName)
		}

		if cat := prov.Catalog(); cat != nil {
			for name := range buildToolMap(cfg, provName, cat) {
				staticToolNames[name] = struct{}{}
			}
			addCatalogTools(srv, cfg, provName, cat)
		}
		visibleProviders = append(visibleProviders, provName)
	}

	if len(dynamicProviders) > 0 {
		hooks.AddBeforeListTools(func(ctx context.Context, _ any, _ *mcpgo.ListToolsRequest) {
			hydrateSessionTools(ctx, cfg, dynamicProviders, staticToolNames)
		})
		hooks.AddBeforeCallTool(func(ctx context.Context, _ any, req *mcpgo.CallToolRequest) {
			if provName := providerNameForTool(cfg.ToolPrefixes, dynamicProviders, req.Params.Name); provName != "" {
				instance := normalizedSessionCatalogInstance(req.GetArguments()["_instance"])
				if headlessInstanceOverrideRequested(cfg.Authorizer, principal.FromContext(ctx), instance) {
					return
				}
				hydrateSessionToolsForInstance(ctx, cfg, []string{provName}, staticToolNames, instance)
			}
		})
		hooks.AddAfterCallTool(func(ctx context.Context, _ any, req *mcpgo.CallToolRequest, _ any) {
			if provName := providerNameForTool(cfg.ToolPrefixes, dynamicProviders, req.Params.Name); provName != "" {
				instance := normalizedSessionCatalogInstance(req.GetArguments()["_instance"])
				cleanupSessionToolsForInstance(ctx, provName, instance)
			}
		})
		hooks.AddOnError(func(ctx context.Context, _ any, _ mcpgo.MCPMethod, message any, _ error) {
			req, ok := message.(*mcpgo.CallToolRequest)
			if !ok || req == nil {
				return
			}
			if provName := providerNameForTool(cfg.ToolPrefixes, dynamicProviders, req.Params.Name); provName != "" {
				instance := normalizedSessionCatalogInstance(req.GetArguments()["_instance"])
				cleanupSessionToolsForInstance(ctx, provName, instance)
			}
		})
	}
	hooks.AddAfterListTools(func(ctx context.Context, _ any, _ *mcpgo.ListToolsRequest, result *mcpgo.ListToolsResult) {
		filterVisibleTools(ctx, cfg, visibleProviders, result)
	})

	return srv
}
