package mcp

import (
	"context"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

func allowOperation(ctx context.Context, cfg Config, p *principal.Principal, provider, operation string) bool {
	if cfg.Authorizer == nil {
		return true
	}
	if p != nil && !p.HasUserContext() {
		return cfg.Authorizer.AllowOperation(ctx, p, provider, operation)
	}
	op, ok := catalogOperationForTool(ctx, cfg, provider, operation)
	if !ok {
		return false
	}
	return cfg.Authorizer.AllowCatalogOperation(ctx, p, provider, op)
}

func toolTarget(cfg Config, providers []string, name string) (provider, operation string, ok bool) {
	provider = providerNameForTool(cfg.ToolPrefixes, providers, name)
	if provider == "" {
		return "", "", false
	}

	prefix := toolName(cfg.ToolPrefixes, provider, "")
	if !strings.HasPrefix(name, prefix) || len(name) <= len(prefix) {
		return "", "", false
	}
	return provider, strings.TrimPrefix(name, prefix), true
}

func filterVisibleTools(ctx context.Context, cfg Config, providers []string, result *mcpgo.ListToolsResult) {
	if result == nil {
		return
	}

	p := principal.FromContext(ctx)
	tools := result.Tools[:0]
	for i := range result.Tools {
		if isHydrationMarkerTool(result.Tools[i].Name) ||
			isHydrationAttemptMarkerTool(result.Tools[i].Name) ||
			isSessionCatalogOperationMarkerTool(result.Tools[i].Name) ||
			strings.HasPrefix(result.Tools[i].Name, instanceHydratedToolMarkerPrefix) {
			continue
		}
		provider, operation, hasTarget := toolTarget(cfg, providers, result.Tools[i].Name)
		if hasTarget {
			if _, ok := catalogOperationForTool(ctx, cfg, provider, operation); !ok {
				continue
			}
		}
		if cfg.Authorizer != nil {
			if hasTarget && !allowOperation(ctx, cfg, p, provider, operation) {
				continue
			}
		}
		tools = append(tools, result.Tools[i])
	}
	result.Tools = tools
}

func catalogOperationForTool(ctx context.Context, cfg Config, providerName, operation string) (catalog.CatalogOperation, bool) {
	if cfg.Providers == nil {
		return catalog.CatalogOperation{}, false
	}
	if sessionCatalogOperationSuppressedFromContext(ctx, providerName, operation, "") {
		return catalog.CatalogOperation{}, false
	}
	prov, err := cfg.Providers.Get(providerName)
	if err != nil {
		return catalog.CatalogOperation{}, false
	}
	if op, _, ok := sessionCatalogOperationFromContext(ctx, providerName, operation, ""); ok {
		return op, true
	}
	if core.SupportsSessionCatalog(prov) &&
		sessionProviderHydrationAttemptedFromContext(ctx, providerName, "") &&
		!sessionProviderHydratedFromContext(ctx, providerName, "") {
		return catalog.CatalogOperation{}, false
	}
	if op, ok := invocation.CatalogOperation(prov.Catalog(), operation); ok && catalogOperationProjectedToMCP(cfg, providerName, op) {
		return op, true
	}
	if !core.SupportsSessionCatalog(prov) {
		return catalog.CatalogOperation{}, false
	}
	return catalog.CatalogOperation{}, false
}
