package mcp

import (
	"strings"

	"github.com/valon-technologies/gestalt/server/core/catalog"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

func addCatalogTools(srv *mcpserver.MCPServer, cfg Config, provName string, cat *catalog.Catalog) {
	m := buildToolMap(cfg, provName, cat)
	for name := range m {
		srv.AddTool(m[name].Tool, m[name].Handler)
	}
}

func buildToolMap(cfg Config, provName string, cat *catalog.Catalog) map[string]mcpserver.ServerTool {
	tools := make(map[string]mcpserver.ServerTool, len(cat.Operations))
	for i := range cat.Operations {
		op := &cat.Operations[i]
		if !catalogOperationProjectedToMCP(cfg, provName, *op) {
			continue
		}

		name := toolName(cfg.ToolPrefixes, provName, op.ID)

		var tool mcpgo.Tool
		if len(op.InputSchema) > 0 {
			tool = mcpgo.NewToolWithRawSchema(name, op.Description, op.InputSchema)
		} else {
			tool = mcpgo.NewTool(name, mcpgo.WithDescription(op.Description))
		}

		tool.Annotations = mapAnnotations(op.Annotations)
		if op.Title != "" {
			tool.Annotations.Title = op.Title
		} else {
			tool.Annotations.Title = op.ID
		}

		if len(op.OutputSchema) > 0 {
			tool.RawOutputSchema = op.OutputSchema
		}

		tools[name] = mcpserver.ServerTool{Tool: tool, Handler: makeHandler(cfg, provName, op.ID, "")}
	}
	return tools
}

func catalogOperationProjectedToMCP(cfg Config, provName string, op catalog.CatalogOperation) bool {
	if op.Visible != nil && !*op.Visible {
		return false
	}
	if cfg.IncludeREST != nil && op.Transport == catalog.TransportREST && !cfg.IncludeREST[provName] {
		return false
	}
	return true
}

func providerNameForTool(prefixes map[string]string, providers []string, tool string) string {
	var best string
	bestLen := -1
	for _, prov := range providers {
		prefix := toolName(prefixes, prov, "")
		if !strings.HasPrefix(tool, prefix) {
			continue
		}
		if len(prefix) > bestLen {
			best = prov
			bestLen = len(prefix)
		}
	}
	return best
}

func toolName(prefixes map[string]string, provider, operation string) string {
	return prefixes[provider] + provider + toolNameSep + operation
}

func mapAnnotations(a catalog.OperationAnnotations) mcpgo.ToolAnnotation {
	return mcpgo.ToolAnnotation{
		ReadOnlyHint:    a.ReadOnlyHint,
		DestructiveHint: a.DestructiveHint,
		IdempotentHint:  a.IdempotentHint,
		OpenWorldHint:   a.OpenWorldHint,
	}
}
