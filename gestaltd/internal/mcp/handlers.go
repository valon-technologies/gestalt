package mcp

import (
	"context"
	"errors"
	"net/http"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/plugins/mcpupstream"

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
		ctx = invocation.WithInvocationSurface(ctx, invocation.InvocationSurfaceMCP)
		if sessionCatalogOperationSuppressedFromContext(ctx, provName, opName, instance) {
			return mcpgo.NewToolResultError("requested instance is unavailable for this tool"), nil
		}
		var validationOp catalog.CatalogOperation
		var hasValidationOp bool
		opMeta, sessionConnection, ok := sessionCatalogOperationFromContext(ctx, provName, opName, instance)
		if ok {
			ctx = invocation.WithCatalogOperation(ctx, provName, opMeta)
			validationOp = opMeta
			hasValidationOp = true
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
		if err := validateSessionCatalogInvocation(ctx, provName, opName, instance, args); err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if err := validateToolInvocation(ctx, cfg, provName, opName, validationOp, hasValidationOp, args); err != nil {
			if errors.Is(err, invocation.ErrAuthorizationDenied) || errors.Is(err, invocation.ErrScopeDenied) {
				return mcpgo.NewToolResultError("operation access denied"), nil
			}
			return mcpgo.NewToolResultError(err.Error()), nil
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

func validateToolInvocation(ctx context.Context, cfg Config, provName, opName string, fallbackOp catalog.CatalogOperation, hasFallbackOp bool, args map[string]any) error {
	if cfg.InvocationValidator == nil || cfg.Providers == nil {
		return nil
	}
	prov, err := cfg.Providers.Get(provName)
	if err != nil {
		return err
	}
	op := fallbackOp
	hasOp := hasFallbackOp
	if staticOp, ok := invocation.CatalogOperation(prov.Catalog(), opName); ok {
		op = staticOp
		hasOp = true
	}
	if !hasOp {
		return nil
	}
	return cfg.InvocationValidator(ctx, provName, prov, op, args, invocation.ConnectionFromContext(ctx))
}
