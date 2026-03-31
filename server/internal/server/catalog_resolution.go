package server

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/core/integration"
	"github.com/valon-technologies/gestalt/server/internal/principal"
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
		return nil, fmt.Errorf("provider %q does not expose a catalog", provName)
	}

	integration.CompileSchemas(merged)
	return merged, nil
}
