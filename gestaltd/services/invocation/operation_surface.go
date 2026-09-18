package invocation

import (
	"context"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
)

func operationExposedOnInvocationSurface(ctx context.Context, op catalog.CatalogOperation) bool {
	caller := CallerProviderFromContext(ctx)
	if isPublicIngress(ctx) {
		switch InvocationSurfaceFromContext(ctx) {
		case InvocationSurfaceHTTP:
			return catalog.OperationExposedOnAPI(op)
		case InvocationSurfaceMCP:
			return catalog.OperationExposedOnMCP(op)
		default:
			// Public gRPC dispatches may not carry an invocation surface. They
			// are the public API by default, never an internal invocation.
			return catalog.OperationExposedOnAPI(op)
		}
	}
	// Internal callers retain the original ingress surface for tracing, so the
	// verified caller identity takes precedence over that inherited value.
	switch caller.Kind {
	case ProviderKindApp, ProviderKindWorkflow:
		return true
	case ProviderKindAgent:
		// Agents did not have an internal surface bypass before private
		// operations. Permit them only when the operation explicitly opts out
		// of both public surfaces; authorization still requires a caller grant.
		if privateOperation(op) {
			return true
		}
	}
	switch InvocationSurfaceFromContext(ctx) {
	case InvocationSurfaceHTTP:
		return catalog.OperationExposedOnAPI(op)
	case InvocationSurfaceMCP:
		return catalog.OperationExposedOnMCP(op)
	default:
		return true
	}
}

// isPublicIngress distinguishes a public gateway dispatch from a provider
// request that merely carries the public surface for tracing. The marker is
// installed by the public gRPC/REST registration and cannot be supplied in a
// request context by the caller.
func isPublicIngress(ctx context.Context) bool {
	_, ok := publicrpc.PublicOriginFromContext(ctx)
	return ok
}
