package mcp

import (
	"context"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

func validateToolInvocation(ctx context.Context, cfg Config, provName, opName string, fallbackOp catalog.CatalogOperation, hasFallbackOp bool, args map[string]any) error {
	if cfg.InvocationValidator == nil || cfg.Providers == nil {
		return nil
	}
	prov, err := cfg.Providers.GetWithContext(ctx, provName)
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
