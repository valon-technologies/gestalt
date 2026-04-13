package mcp

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

const hydrationMarkerPrefix = "__gestalt_internal_hydrated__:"

func hydrateSessionTools(ctx context.Context, cfg Config, providerNames []string, staticToolNames map[string]struct{}) {
	session := mcpserver.ClientSessionFromContext(ctx)
	if session == nil {
		return
	}
	sessionWithTools, ok := session.(mcpserver.SessionWithTools)
	if !ok {
		return
	}

	tools := sessionWithTools.GetSessionTools()
	if tools == nil {
		tools = make(map[string]mcpserver.ServerTool)
	}

	changed := false
	for _, provName := range providerNames {
		if sessionProviderHydrated(tools, provName) {
			continue
		}

		prov, err := cfg.Providers.Get(provName)
		if err != nil {
			continue
		}

		scp, ok := prov.(core.SessionCatalogProvider)
		if !ok {
			continue
		}

		sessionCtx, token, err := resolveSessionToken(ctx, cfg, provName, prov)
		if err != nil {
			continue
		}

		cat, err := scp.CatalogForRequest(sessionCtx, token)
		if err != nil {
			continue
		}
		if markSessionProviderHydrated(tools, provName) {
			changed = true
		}
		if cat == nil {
			continue
		}

		m := buildToolMap(cfg, provName, cat)
		for name := range m {
			if _, exists := staticToolNames[name]; exists {
				continue
			}
			tools[name] = m[name]
		}
		changed = true
	}

	if changed {
		sessionWithTools.SetSessionTools(tools)
	}
}

func resolveSessionToken(ctx context.Context, cfg Config, provName string, prov core.Provider) (context.Context, string, error) {
	if prov.ConnectionMode() == core.ConnectionModeNone {
		return invocation.WithCredentialContext(ctx, invocation.CredentialContext{Mode: core.ConnectionModeNone}), "", nil
	}
	if cfg.TokenResolver == nil {
		return ctx, "", nil
	}
	p := principal.FromContext(ctx)
	if p == nil {
		return ctx, "", fmt.Errorf("not authenticated")
	}
	connection, instance := sessionTokenSelectors(cfg, p, provName)
	return cfg.TokenResolver.ResolveToken(ctx, p, provName, connection, instance)
}

func sessionProviderHydrated(tools map[string]mcpserver.ServerTool, provider string) bool {
	_, ok := tools[hydrationMarkerName(provider)]
	return ok
}

func markSessionProviderHydrated(tools map[string]mcpserver.ServerTool, provider string) bool {
	name := hydrationMarkerName(provider)
	if _, ok := tools[name]; ok {
		return false
	}
	tools[name] = mcpserver.ServerTool{
		Tool: mcpgo.NewTool(name, mcpgo.WithDescription("gestalt internal hydration marker")),
		Handler: func(context.Context, mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			return mcpgo.NewToolResultError("tool not found"), nil
		},
	}
	return true
}

func hydrationMarkerName(provider string) string {
	return hydrationMarkerPrefix + provider
}

func isHydrationMarkerTool(name string) bool {
	return len(name) > len(hydrationMarkerPrefix) && name[:len(hydrationMarkerPrefix)] == hydrationMarkerPrefix
}

func sessionTokenSelectors(cfg Config, p *principal.Principal, provName string) (string, string) {
	connection := cfg.MCPConnection[provName]
	if cfg.Authorizer == nil || !cfg.Authorizer.IsWorkload(p) {
		return connection, ""
	}
	if binding, ok := cfg.Authorizer.Binding(p, provName); ok {
		return binding.Connection, binding.Instance
	}
	return connection, ""
}
