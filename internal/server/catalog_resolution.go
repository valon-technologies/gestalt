package server

import (
	"context"
	"log/slog"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/catalog"
	"github.com/valon-technologies/gestalt/core/integration"
	"github.com/valon-technologies/gestalt/internal/principal"
)

type tokenResolver interface {
	ResolveToken(ctx context.Context, p *principal.Principal, providerName, connection, instance string) (string, error)
}

func resolveCatalog(ctx context.Context, prov core.Provider, provName string, resolver tokenResolver, p *principal.Principal, defaultConnection string) (*catalog.Catalog, error) {
	var staticCat *catalog.Catalog
	if cp, ok := prov.(core.CatalogProvider); ok {
		staticCat = cp.Catalog()
	}

	var sessionCat *catalog.Catalog
	if scp, ok := prov.(core.SessionCatalogProvider); ok {
		if resolver != nil && prov.ConnectionMode() != core.ConnectionModeNone && p != nil {
			token, err := resolver.ResolveToken(ctx, p, provName, defaultConnection, "")
			if err != nil {
				slog.WarnContext(ctx, "catalog token resolution failed", "provider", provName, "error", err)
			} else {
				cat, err := scp.CatalogForRequest(ctx, token)
				if err != nil {
					slog.WarnContext(ctx, "catalog session resolution failed", "provider", provName, "error", err)
				} else {
					sessionCat = cat
				}
			}
		}
	}

	var merged *catalog.Catalog
	switch {
	case staticCat == nil && sessionCat == nil:
		// fall through to synthesis
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

	if merged == nil {
		merged = synthesizeCatalog(prov, provName)
	}

	integration.CompileSchemas(merged)
	return merged, nil
}

func synthesizeCatalog(prov core.Provider, provName string) *catalog.Catalog {
	ops := prov.ListOperations()
	cat := &catalog.Catalog{
		Name:        provName,
		DisplayName: prov.DisplayName(),
		Description: prov.Description(),
		Operations:  make([]catalog.CatalogOperation, 0, len(ops)),
	}
	for _, op := range ops {
		params := make([]catalog.CatalogParameter, 0, len(op.Parameters))
		for _, p := range op.Parameters {
			params = append(params, catalog.CatalogParameter{
				Name:        p.Name,
				Type:        p.Type,
				Description: p.Description,
				Required:    p.Required,
				Default:     p.Default,
			})
		}
		cat.Operations = append(cat.Operations, catalog.CatalogOperation{
			ID:          op.Name,
			Method:      op.Method,
			Description: op.Description,
			Parameters:  params,
			Transport:   catalog.TransportREST,
		})
	}
	return cat
}
