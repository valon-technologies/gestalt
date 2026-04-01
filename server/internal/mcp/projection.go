package mcp

import (
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

func addCatalogTools(srv *mcpserver.MCPServer, cfg Config, provName string, cat *catalog.Catalog, prov core.Provider) {
	m := buildToolMap(cfg, provName, prov, cat)
	for name := range m {
		srv.AddTool(m[name].Tool, m[name].Handler)
	}
}

func buildToolMap(cfg Config, provName string, prov core.Provider, cat *catalog.Catalog) map[string]mcpserver.ServerTool {
	caller, isDirect := unwrapDirectCaller(prov)
	if isDirect && cfg.TokenResolver == nil {
		isDirect = false
	}

	tools := make(map[string]mcpserver.ServerTool, len(cat.Operations))
	for i := range cat.Operations {
		op := &cat.Operations[i]
		if op.Visible != nil && !*op.Visible {
			continue
		}
		if cfg.IncludeREST != nil && op.Transport == catalog.TransportREST && !cfg.IncludeREST[provName] {
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

		// Direct token resolution bypasses Invoke, so it needs an explicit
		// connection instead of relying on broker-side provider defaults.
		conn := connectionForCatalogTransport(cfg, provName, op.Transport)

		var handler mcpserver.ToolHandlerFunc
		if isDirect && op.Transport != catalog.TransportREST {
			handler = makeDirectHandler(cfg, provName, op.ID, conn, caller)
		} else {
			handler = makeHandler(cfg.Invoker, provName, op.ID, "")
		}
		tools[name] = mcpserver.ServerTool{Tool: tool, Handler: handler}
	}
	return tools
}

func unwrapDirectCaller(prov core.Provider) (directToolCaller, bool) {
	if c, ok := prov.(directToolCaller); ok {
		return c, true
	}
	type inner interface{ Inner() core.Provider }
	if r, ok := prov.(inner); ok {
		c, ok := r.Inner().(directToolCaller)
		return c, ok
	}
	return nil, false
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
