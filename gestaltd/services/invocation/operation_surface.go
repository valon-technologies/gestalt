package invocation

import (
	"context"

	"github.com/valon-technologies/gestalt/server/core/catalog"
)

// OperationExposedOnInvocationSurface applies the catalog's public-surface and
// verified internal-caller policy to an invocation context. Public gateway
// requests are evaluated as public even when their request context contains
// the gateway's synthetic caller identity. A nested provider call is marked by
// the gRPC entry plus invocation metadata created by GuardedInvoker.
func OperationExposedOnInvocationSurface(ctx context.Context, op catalog.CatalogOperation) bool {
	surface := InvocationSurfaceFromContext(ctx)
	caller := CallerProviderFromContext(ctx)
	if isPublicIngress(ctx, surface, caller) {
		switch surface {
		case InvocationSurfaceHTTP:
			return catalog.OperationExposedOnAPI(op)
		case InvocationSurfaceMCP:
			return catalog.OperationExposedOnMCP(op)
		}
	}

	if caller.Kind != "" || caller.Name != "" {
		if len(op.InternalCallers) > 0 {
			return internalCallerAllowed(op, caller)
		}
		// Internal callers retain the original ingress surface for tracing, so
		// the verified caller identity takes precedence over that inherited value.
		switch caller.Kind {
		case ProviderKindApp, ProviderKindWorkflow:
			return true
		}
	}

	// A configured allowlist fails closed for an internal invocation with no
	// verified caller. Public HTTP/MCP requests were handled above.
	if len(op.InternalCallers) > 0 {
		return false
	}

	switch surface {
	case InvocationSurfaceHTTP:
		return catalog.OperationExposedOnAPI(op)
	case InvocationSurfaceMCP:
		return catalog.OperationExposedOnMCP(op)
	default:
		return true
	}
}

// Keep this unexported name for package-local callers while exposing the same
// predicate to operation-exposure wrappers that sit at another package layer.
func operationExposedOnInvocationSurface(ctx context.Context, op catalog.CatalogOperation) bool {
	return OperationExposedOnInvocationSurface(ctx, op)
}

func isPublicIngress(ctx context.Context, surface InvocationSurface, caller CallerProvider) bool {
	if surface != InvocationSurfaceHTTP && surface != InvocationSurfaceMCP {
		return false
	}
	entry := EntryFromContext(ctx)
	if entry == EntryHTTP {
		return true
	}
	if entry != EntryGRPC {
		// Contexts assembled by the broker itself use EntryInternal and are
		// internal even when they inherit an HTTP/MCP surface value.
		return false
	}
	meta := MetaFromContext(ctx)
	return caller.Kind == "" || caller.Name == "" || meta == nil || (meta.Depth == 0 && len(meta.CallChain) == 0)
}

func internalCallerAllowed(op catalog.CatalogOperation, caller CallerProvider) bool {
	ref := string(caller.Kind) + ":" + caller.Name
	for _, allowed := range op.InternalCallers {
		if allowed == ref {
			return true
		}
	}
	return false
}
