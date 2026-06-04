package mcp

import (
	"context"
	"strings"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

func filterVisibleTools(ctx context.Context, cfg Config, visibleProviders []string, result *mcpgo.ListToolsResult) {
	_ = ctx
	_ = cfg
	_ = visibleProviders
	if result == nil || len(result.Tools) == 0 {
		return
	}
	visible := result.Tools[:0]
	for i := range result.Tools {
		name := result.Tools[i].Name
		if isHydrationMarkerTool(name) ||
			isHydrationAttemptMarkerTool(name) ||
			isSessionCatalogOperationMarkerTool(name) ||
			strings.HasPrefix(name, instanceHydratedToolMarkerPrefix) {
			continue
		}
		visible = append(visible, result.Tools[i])
	}
	result.Tools = visible
}
