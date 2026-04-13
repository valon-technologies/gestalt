package mcp

import (
	"context"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/principal"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

func allowOperation(cfg Config, p *principal.Principal, provider, operation string) bool {
	if cfg.Authorizer == nil {
		return true
	}
	return cfg.Authorizer.AllowOperation(p, provider, operation)
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
	workload := cfg.Authorizer != nil && cfg.Authorizer.IsWorkload(p)

	tools := result.Tools[:0]
	for i := range result.Tools {
		if isHydrationMarkerTool(result.Tools[i].Name) {
			continue
		}
		if workload {
			provider, operation, ok := toolTarget(cfg, providers, result.Tools[i].Name)
			if ok && !allowOperation(cfg, p, provider, operation) {
				continue
			}
		}
		tools = append(tools, result.Tools[i])
	}
	result.Tools = tools
}
