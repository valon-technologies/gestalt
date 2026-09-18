package invocation

import (
	"context"

	"github.com/valon-technologies/gestalt/server/core/catalog"
)

func operationExposedOnInvocationSurface(ctx context.Context, op catalog.CatalogOperation) bool {
	// Internal callers retain the original ingress surface for tracing, so the
	// verified caller identity takes precedence over that inherited value.
	switch CallerProviderFromContext(ctx).Kind {
	case ProviderKindApp, ProviderKindWorkflow:
		return true
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
