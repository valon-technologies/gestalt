package mcp

import (
	"context"

	"github.com/valon-technologies/gestalt/internal/egress"
	"github.com/valon-technologies/gestalt/internal/invocation"
	"github.com/valon-technologies/gestalt/internal/mcpupstream"
	"github.com/valon-technologies/gestalt/internal/principal"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

func makeHandler(invoker invocation.Invoker, provName, opName string) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		p := principal.FromContext(ctx)
		if p == nil {
			return mcpgo.NewToolResultError("not authenticated"), nil
		}

		args := req.GetArguments()
		instance, _ := args["_instance"].(string)
		delete(args, "_instance")

		result, err := invoker.Invoke(ctx, p, provName, instance, opName, args)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}

		if result.Status >= httpErrorMin {
			return mcpgo.NewToolResultError(result.Body), nil
		}

		return mcpgo.NewToolResultText(result.Body), nil
	}
}

func makeDirectHandler(cfg Config, provName, opName string, caller directToolCaller) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		p := principal.FromContext(ctx)
		if p == nil {
			return mcpgo.NewToolResultError("not authenticated"), nil
		}
		ctx = attachEgressSubject(ctx, p)

		args := req.GetArguments()
		instance, _ := args["_instance"].(string)
		delete(args, "_instance")

		token, err := cfg.TokenResolver.ResolveToken(ctx, p, provName, instance)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}

		ctx = mcpupstream.WithUpstreamToken(ctx, token)
		return caller.CallTool(ctx, opName, args)
	}
}

func attachEgressSubject(ctx context.Context, p *principal.Principal) context.Context {
	return egress.WithSubjectFromPrincipal(ctx, p)
}
