package invocation

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/core/integration"
	"github.com/valon-technologies/gestalt/server/internal/authorization"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

type TokenResolver interface {
	ResolveToken(ctx context.Context, p *principal.Principal, providerName, connection, instance string) (context.Context, string, error)
}

func ResolveCatalogWithMetadata(ctx context.Context, prov core.Provider, provName string, resolver TokenResolver, p *principal.Principal, defaultConnection, instance string, strictSession bool) (*catalog.Catalog, bool, error) {
	staticCat := prov.Catalog()
	sessionCat, err := resolveSessionCatalog(ctx, prov, provName, resolver, p, defaultConnection, instance)
	if err != nil {
		if strictSession || staticCat == nil {
			return nil, true, err
		}
		slog.WarnContext(ctx, "catalog session resolution failed", "provider", provName, "error", err)
		merged, err := mergeCatalogs(provName, staticCat, sessionCat)
		return merged, true, err
	}
	merged, err := mergeCatalogs(provName, staticCat, sessionCat)
	return merged, false, err
}

func ResolveOperation(ctx context.Context, prov core.Provider, provName string, resolver TokenResolver, p *principal.Principal, operation string, sessionConnections []string, instance string) (catalog.CatalogOperation, string, string, error) {
	staticOp, staticOK := CatalogOperation(providerCatalog(prov), operation)
	if op, ok := catalog.OperationFromContext(ctx, provName, operation); ok {
		return op, OperationTransport(op), "", nil
	}
	sessionOp, sessionConnection, sessionFound, err := resolveSessionOperation(ctx, prov, provName, resolver, p, operation, sessionConnections, instance)
	if err != nil {
		return catalog.CatalogOperation{}, "", "", err
	}
	if sessionFound {
		return sessionOp, OperationTransport(sessionOp), sessionConnection, nil
	}
	if staticOK && (!core.SupportsSessionCatalog(prov) || instance == "") {
		return staticOp, OperationTransport(staticOp), "", nil
	}
	return catalog.CatalogOperation{}, "", "", fmt.Errorf("%w: %q on provider %q", ErrOperationNotFound, operation, provName)
}

func resolveSessionCatalog(ctx context.Context, prov core.Provider, provName string, resolver TokenResolver, p *principal.Principal, connection, instance string) (*catalog.Catalog, error) {
	if !core.SupportsSessionCatalog(prov) || (prov.ConnectionMode() != core.ConnectionModeNone && (resolver == nil || p == nil)) {
		return nil, nil
	}
	token := ""
	if resolver != nil && p != nil {
		var err error
		ctx, token, err = resolver.ResolveToken(ctx, p, provName, connection, instance)
		if err != nil {
			return nil, err
		}
	} else {
		ctx = WithCredentialContext(ctx, CredentialContext{Mode: core.ConnectionModeNone})
	}
	cat, err := core.CatalogForRequest(ctx, prov, token)
	return cat, err
}

func resolveSessionOperation(ctx context.Context, prov core.Provider, provName string, resolver TokenResolver, p *principal.Principal, operation string, connections []string, instance string) (catalog.CatalogOperation, string, bool, error) {
	if len(connections) == 0 {
		connections = []string{""}
	}
	firstErr, resolved := error(nil), false
	for _, connection := range connections {
		cat, err := resolveSessionCatalog(ctx, prov, provName, resolver, p, connection, instance)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if cat == nil {
			continue
		}
		resolved = true
		if op, ok := CatalogOperation(cat, operation); ok {
			return op, connection, true, nil
		}
	}
	if firstErr != nil && !resolved {
		return catalog.CatalogOperation{}, "", false, firstErr
	}
	return catalog.CatalogOperation{}, "", false, nil
}

func mergeCatalogs(provName string, staticCat, sessionCat *catalog.Catalog) (*catalog.Catalog, error) {
	if sessionCat == nil && staticCat == nil {
		return nil, fmt.Errorf("provider %q does not expose a catalog", provName)
	}
	if sessionCat == nil {
		merged := staticCat.Clone()
		integration.CompileSchemas(merged)
		return merged, nil
	}
	merged := sessionCat.Clone()
	if staticCat != nil {
		sessionIndexes := make(map[string]struct{}, len(merged.Operations))
		for i := range merged.Operations {
			sessionIndexes[merged.Operations[i].ID] = struct{}{}
		}
		for i := range staticCat.Operations {
			if _, exists := sessionIndexes[staticCat.Operations[i].ID]; exists {
				continue
			}
			merged.Operations = append(merged.Operations, staticCat.Operations[i])
		}
	}
	integration.CompileSchemas(merged)
	return merged, nil
}

func FilterCatalogForPrincipal(cat *catalog.Catalog, provName string, p *principal.Principal, authorizer *authorization.Authorizer) *catalog.Catalog {
	if cat == nil || authorizer == nil {
		return cat
	}
	filtered := cat.Clone()
	ops := filtered.Operations[:0]
	for i := range filtered.Operations {
		if authorizer.AllowCatalogOperation(p, provName, filtered.Operations[i]) {
			ops = append(ops, filtered.Operations[i])
		}
	}
	filtered.Operations = ops
	return filtered
}
