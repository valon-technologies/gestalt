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

func ResolveCatalog(ctx context.Context, prov core.Provider, provName string, resolver TokenResolver, p *principal.Principal, defaultConnection, instance string) (*catalog.Catalog, error) {
	return resolveCatalog(ctx, prov, provName, resolver, p, defaultConnection, instance, false)
}

func ResolveCatalogStrict(ctx context.Context, prov core.Provider, provName string, resolver TokenResolver, p *principal.Principal, defaultConnection, instance string) (*catalog.Catalog, error) {
	return resolveCatalog(ctx, prov, provName, resolver, p, defaultConnection, instance, true)
}

func ResolveSessionCatalog(ctx context.Context, prov core.Provider, provName string, resolver TokenResolver, p *principal.Principal, connection, instance string) (*catalog.Catalog, error) {
	return resolveSessionCatalog(ctx, prov, provName, resolver, p, connection, instance)
}

func resolveCatalog(ctx context.Context, prov core.Provider, provName string, resolver TokenResolver, p *principal.Principal, defaultConnection, instance string, strictSession bool) (*catalog.Catalog, error) {
	staticCat := prov.Catalog()

	sessionCat, err := resolveSessionCatalog(ctx, prov, provName, resolver, p, defaultConnection, instance)
	if err != nil {
		if strictSession || staticCat == nil {
			return nil, err
		}
		slog.WarnContext(ctx, "catalog session resolution failed", "provider", provName, "error", err)
	}

	return mergeCatalogs(provName, staticCat, sessionCat)
}

func resolveSessionCatalog(ctx context.Context, prov core.Provider, provName string, resolver TokenResolver, p *principal.Principal, connection, instance string) (*catalog.Catalog, error) {
	scp, ok := prov.(core.SessionCatalogProvider)
	if !ok {
		return nil, nil
	}
	if prov.ConnectionMode() == core.ConnectionModeNone {
		if resolver != nil && p != nil {
			enrichedCtx, token, err := resolver.ResolveToken(ctx, p, provName, connection, instance)
			if err != nil {
				return nil, err
			}
			return scp.CatalogForRequest(enrichedCtx, token)
		}
		ctx = WithCredentialContext(ctx, CredentialContext{Mode: core.ConnectionModeNone})
		return scp.CatalogForRequest(ctx, "")
	}
	if resolver == nil || p == nil {
		return nil, nil
	}

	ctx, token, err := resolver.ResolveToken(ctx, p, provName, connection, instance)
	if err != nil {
		return nil, err
	}
	return scp.CatalogForRequest(ctx, token)
}

func mergeCatalogs(provName string, staticCat, sessionCat *catalog.Catalog) (*catalog.Catalog, error) {
	var merged *catalog.Catalog
	switch {
	case staticCat == nil && sessionCat == nil:
		return nil, fmt.Errorf("provider %q does not expose a catalog", provName)
	case staticCat != nil && sessionCat == nil:
		merged = staticCat.Clone()
	case staticCat == nil && sessionCat != nil:
		merged = sessionCat.Clone()
	default:
		merged = staticCat.Clone()
		staticIndexes := make(map[string]int, len(merged.Operations))
		for i := range merged.Operations {
			staticIndexes[merged.Operations[i].ID] = i
		}
		for i := range sessionCat.Operations {
			if idx, exists := staticIndexes[sessionCat.Operations[i].ID]; exists {
				merged.Operations[idx] = sessionCat.Operations[i]
				continue
			}
			merged.Operations = append(merged.Operations, sessionCat.Operations[i])
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
