package mcp

import (
	"context"
	"errors"
	"net/http"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/mcpupstream"
	"github.com/valon-technologies/gestalt/server/internal/principal"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

func makeHandler(cfg Config, provName, opName, connection string) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		p := principal.FromContext(ctx)
		if p == nil {
			return mcpgo.NewToolResultError("not authenticated"), nil
		}

		rawArgs := req.GetArguments()
		instance := normalizedSessionCatalogInstance(rawArgs["_instance"])
		if headlessInstanceOverrideRequested(cfg.Authorizer, p, instance) {
			return mcpgo.NewToolResultError("identity-token callers may not override connection or instance bindings"), nil
		}
		args := make(map[string]any, len(rawArgs))
		for key, value := range rawArgs {
			if key == "_instance" {
				continue
			}
			args[key] = value
		}

		if connection != "" {
			ctx = invocation.WithConnection(ctx, connection)
		}
		if sessionCatalogOperationSuppressedFromContext(ctx, provName, opName, instance) {
			return mcpgo.NewToolResultError("requested instance is unavailable for this tool"), nil
		}
		opMeta, sessionConnection, ok := sessionCatalogOperationFromContext(ctx, provName, opName, instance)
		if ok {
			ctx = invocation.WithCatalogOperation(ctx, provName, opMeta)
			if invocation.ConnectionFromContext(ctx) == "" && sessionConnection != "" {
				ctx = invocation.WithConnection(ctx, sessionConnection)
			}
		} else if prov, err := cfg.Providers.Get(provName); err == nil {
			if core.SupportsSessionCatalog(prov) {
				if instance != "" || (sessionProviderHydrationAttemptedFromContext(ctx, provName, "") && !sessionProviderHydratedFromContext(ctx, provName, "")) {
					return mcpgo.NewToolResultError("requested instance is unavailable for this tool"), nil
				}
			}
		} else if instance != "" {
			return mcpgo.NewToolResultError("requested instance is unavailable for this tool"), nil
		}
		ctx = mcpupstream.WithCallToolMeta(ctx, req.Params.Meta)
		result, err := cfg.Invoker.Invoke(ctx, p, provName, instance, opName, args)
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
