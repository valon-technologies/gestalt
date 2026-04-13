package mcp

import (
	"context"
	"errors"
	"net/http"

	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/mcpupstream"
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
		ctx = mcpupstream.WithCallToolMeta(ctx, req.Params.Meta)
		result, err := invoker.Invoke(ctx, p, provName, instance, opName, args)
		if err != nil {
			if errors.Is(err, invocation.ErrAuthorizationDenied) || errors.Is(err, invocation.ErrScopeDenied) {
				return mcpgo.NewToolResultError("operation access denied"), nil
			}
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if result == nil {
			return mcpgo.NewToolResultText("{}"), nil
		}

		if orig, ok := result.MCPResult.(*mcpgo.CallToolResult); ok && orig != nil {
			return orig, nil
		}

		if result.Status >= http.StatusBadRequest {
			return mcpgo.NewToolResultError(result.Body), nil
		}
		return mcpgo.NewToolResultText(result.Body), nil
	}
}
