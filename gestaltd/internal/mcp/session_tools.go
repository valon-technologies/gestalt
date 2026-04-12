package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/principal"

	mcpserver "github.com/mark3labs/mcp-go/server"
)

func hydrateSessionTools(ctx context.Context, cfg Config, providerNames []string) {
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
		prefix := toolName(cfg.ToolPrefixes, provName, "")
		if hasToolsWithPrefix(tools, prefix) {
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

		token, err := resolveSessionToken(ctx, cfg, provName, prov)
		if err != nil {
			continue
		}

		cat, err := scp.CatalogForRequest(ctx, token)
		if err != nil || cat == nil {
			continue
		}

		m := buildToolMap(cfg, provName, cat)
		for name := range m {
			tools[name] = m[name]
		}
		changed = true
	}

	if changed {
		sessionWithTools.SetSessionTools(tools)
	}
}

func hasToolsWithPrefix(tools map[string]mcpserver.ServerTool, prefix string) bool {
	for name := range tools {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func resolveSessionToken(ctx context.Context, cfg Config, provName string, prov core.Provider) (string, error) {
	if cfg.TokenResolver == nil || prov.ConnectionMode() == core.ConnectionModeNone {
		return "", nil
	}
	p := principal.FromContext(ctx)
	if p == nil {
		return "", fmt.Errorf("not authenticated")
	}
	return cfg.TokenResolver.ResolveToken(ctx, p, provName, cfg.MCPConnection[provName], "")
}
