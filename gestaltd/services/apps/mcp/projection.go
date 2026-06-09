package mcp

import (
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

func projectCatalog(cfg Config, provName string, prov core.Provider, cat *catalog.Catalog) *catalog.Catalog {
	if cfg.CatalogProjection == nil || cat == nil {
		return cat
	}
	return cfg.CatalogProjection(provName, prov, cat)
}

func catalogOperationProjectedToMCP(cfg Config, provName string, op catalog.CatalogOperation) bool {
	if op.Visible != nil && !*op.Visible {
		return false
	}
	if cfg.IncludeREST != nil {
		if includeProjectedOps, ok := cfg.IncludeREST[provName]; ok && !includeProjectedOps && op.Transport != catalog.TransportMCPPassthrough {
			return false
		}
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
	raw := prefixes[provider] + provider + toolNameSep + operation
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '_' || r == '-' || r == ':':
			return r
		default:
			return '_'
		}
	}, raw)
}

func mapAnnotations(a catalog.CapabilityAnnotations) mcpgo.ToolAnnotation {
	return mcpgo.ToolAnnotation{
		ReadOnlyHint:    a.ReadOnlyHint,
		DestructiveHint: a.DestructiveHint,
		IdempotentHint:  a.IdempotentHint,
		OpenWorldHint:   a.OpenWorldHint,
	}
}
