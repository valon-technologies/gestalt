package mcp

import (
	"context"
	"net/http"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/egress"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

func makeHandler(invoker invocation.Invoker, provName, opName, connection string) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		p := principal.FromContext(ctx)
		if p == nil {
			return mcpgo.NewToolResultError("not authenticated"), nil
		}

		args := req.GetArguments()
		instance, _ := args["_instance"].(string)
		delete(args, "_instance")

		if connection != "" {
			ctx = invocation.WithConnection(ctx, connection)
		}
		result, err := invoker.Invoke(ctx, p, provName, instance, opName, args)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}

		if result.Status >= http.StatusBadRequest {
			return mcpgo.NewToolResultError(result.Body), nil
		}

		return mcpgo.NewToolResultText(result.Body), nil
	}
}

func makeDirectHandler(cfg Config, prov core.Provider, provName, opName, connection string) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		args := req.GetArguments()
		instance, _ := args["_instance"].(string)
		delete(args, "_instance")

		result, err := invocation.CallDirectTool(ctx, cfg.TokenResolver, principal.FromContext(ctx), prov, provName, opName, connection, instance, args, req.Params.Meta)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		return result, nil
	}
}

func attachEgressSubject(ctx context.Context, p *principal.Principal) context.Context {
	return egress.WithSubjectFromPrincipal(ctx, p)
}
