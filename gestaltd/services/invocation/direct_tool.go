package invocation

import (
	"context"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/plugins/mcpupstream"

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
	mode := effectiveConnectionMode(ctx, prov)
	if resolver != nil && mode != core.ConnectionModeNone {
		var token string
		var err error
		ctx, token, err = resolver.ResolveToken(ctx, p, provName, connection, instance)
		if err != nil {
			return nil, err
		}
		ctx = mcpupstream.WithUpstreamToken(ctx, token)
	} else if mode == core.ConnectionModeNone {
		ctx = WithCredentialContext(ctx, CredentialContext{Mode: core.ConnectionModeNone})
	}
	ctx = mcpupstream.WithCallToolMeta(ctx, meta)
	return caller.CallTool(ctx, opName, args)
}
