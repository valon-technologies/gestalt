package invocation

import (
	"context"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/egress"
	"github.com/valon-technologies/gestalt/server/internal/mcpupstream"
	"github.com/valon-technologies/gestalt/server/internal/principal"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

type directToolCaller interface {
	CallTool(ctx context.Context, name string, args map[string]any) (*mcpgo.CallToolResult, error)
}

func CallDirectTool(ctx context.Context, resolver TokenResolver, p *principal.Principal, prov core.Provider, provName, opName, connection, instance string, args map[string]any, meta *mcpgo.Meta) (*mcpgo.CallToolResult, error) {
	caller, ok := prov.(directToolCaller)
	if !ok {
		return nil, core.ErrMCPOnly
	}
	if p == nil {
		return nil, ErrNotAuthenticated
	}
	ctx = egress.WithSubjectFromPrincipal(ctx, p)

	if resolver != nil && prov.ConnectionMode() != core.ConnectionModeNone {
		token, err := resolver.ResolveToken(ctx, p, provName, connection, instance)
		if err != nil {
			return nil, err
		}
		ctx = mcpupstream.WithUpstreamToken(ctx, token)
	}
	ctx = mcpupstream.WithCallToolMeta(ctx, meta)
	return caller.CallTool(ctx, opName, args)
}
