package invocation

import (
	"context"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/mcpupstream"
	"github.com/valon-technologies/gestalt/server/internal/principal"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

type directToolCaller interface {
	CallTool(ctx context.Context, name string, args map[string]any) (*mcpgo.CallToolResult, error)
}

func CallDirectTool(ctx context.Context, resolver TokenResolver, p *principal.Principal, prov core.Provider, provName, opName, connection, instance string, boundCredential CredentialBindingResolution, args map[string]any, meta *mcpgo.Meta) (*mcpgo.CallToolResult, error) {
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
		ctx, token, err = ResolveTokenForBinding(ctx, resolver, p, provName, connection, instance, boundCredential)
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
