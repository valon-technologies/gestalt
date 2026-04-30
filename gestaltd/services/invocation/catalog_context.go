package invocation

import (
	"context"

	"github.com/valon-technologies/gestalt/server/core/catalog"
)

type catalogOperationContextKey struct{}

type catalogOperationContextValue struct {
	Provider  string
	Operation catalog.CatalogOperation
}

func WithCatalogOperation(ctx context.Context, provider string, op catalog.CatalogOperation) context.Context {
	if ctx == nil {
		return nil
	}
	return context.WithValue(ctx, catalogOperationContextKey{}, catalogOperationContextValue{
		Provider:  provider,
		Operation: op,
	})
}

func CatalogOperationFromContext(ctx context.Context, provider, operation string) (catalog.CatalogOperation, bool) {
	if ctx == nil {
		return catalog.CatalogOperation{}, false
	}
	value, ok := ctx.Value(catalogOperationContextKey{}).(catalogOperationContextValue)
	if !ok || value.Provider != provider || value.Operation.ID != operation {
		return catalog.CatalogOperation{}, false
	}
	return value.Operation, true
}
