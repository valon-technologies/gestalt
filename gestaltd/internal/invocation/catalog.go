package invocation

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/core/integration"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

type TokenResolver interface {
	ResolveToken(ctx context.Context, p *principal.Principal, providerName, connection, instance string) (string, error)
}

func ResolveCatalog(ctx context.Context, prov core.Provider, provName string, resolver TokenResolver, p *principal.Principal, defaultConnection, instance string) (*catalog.Catalog, error) {
	return resolveCatalog(ctx, prov, provName, resolver, p, defaultConnection, instance, false)
}

func ResolveCatalogStrict(ctx context.Context, prov core.Provider, provName string, resolver TokenResolver, p *principal.Principal, defaultConnection, instance string) (*catalog.Catalog, error) {
	return resolveCatalog(ctx, prov, provName, resolver, p, defaultConnection, instance, true)
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
	if !ok || resolver == nil || prov.ConnectionMode() == core.ConnectionModeNone || p == nil {
		return nil, nil
	}

	token, err := resolver.ResolveToken(ctx, p, provName, connection, instance)
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
		staticIDs := make(map[string]struct{}, len(merged.Operations))
		for i := range merged.Operations {
			staticIDs[merged.Operations[i].ID] = struct{}{}
		}
		for i := range sessionCat.Operations {
			if _, exists := staticIDs[sessionCat.Operations[i].ID]; !exists {
				merged.Operations = append(merged.Operations, sessionCat.Operations[i])
			}
		}
	}

	merged.SortOperations()
	integration.CompileSchemas(merged)
	return merged, nil
}
