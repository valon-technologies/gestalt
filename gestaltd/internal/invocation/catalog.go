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

type CatalogResolutionMetadata struct {
	SessionAttempted bool
	SessionFailed    bool
}

func ResolveCatalog(ctx context.Context, prov core.Provider, provName string, resolver TokenResolver, p *principal.Principal, defaultConnection, instance string) (*catalog.Catalog, error) {
	cat, _, err := ResolveCatalogWithMetadata(ctx, prov, provName, resolver, p, defaultConnection, instance)
	return cat, err
}

func ResolveCatalogStrict(ctx context.Context, prov core.Provider, provName string, resolver TokenResolver, p *principal.Principal, defaultConnection, instance string) (*catalog.Catalog, error) {
	cat, _, err := ResolveCatalogStrictWithMetadata(ctx, prov, provName, resolver, p, defaultConnection, instance)
	return cat, err
}

func ResolveCatalogWithMetadata(ctx context.Context, prov core.Provider, provName string, resolver TokenResolver, p *principal.Principal, defaultConnection, instance string) (*catalog.Catalog, CatalogResolutionMetadata, error) {
	return resolveCatalog(ctx, prov, provName, resolver, p, defaultConnection, instance, false)
}

func ResolveCatalogStrictWithMetadata(ctx context.Context, prov core.Provider, provName string, resolver TokenResolver, p *principal.Principal, defaultConnection, instance string) (*catalog.Catalog, CatalogResolutionMetadata, error) {
	return resolveCatalog(ctx, prov, provName, resolver, p, defaultConnection, instance, true)
}

func ResolveSessionCatalog(ctx context.Context, prov core.Provider, provName string, resolver TokenResolver, p *principal.Principal, connection, instance string) (*catalog.Catalog, error) {
	cat, _, err := resolveSessionCatalog(ctx, prov, provName, resolver, p, connection, instance)
	return cat, err
}

func resolveCatalog(ctx context.Context, prov core.Provider, provName string, resolver TokenResolver, p *principal.Principal, defaultConnection, instance string, strictSession bool) (*catalog.Catalog, CatalogResolutionMetadata, error) {
	meta := CatalogResolutionMetadata{}
	staticCat := prov.Catalog()

	sessionCat, attempted, err := resolveSessionCatalog(ctx, prov, provName, resolver, p, defaultConnection, instance)
	meta.SessionAttempted = attempted
	if err != nil {
		meta.SessionFailed = true
		if strictSession || staticCat == nil {
			return nil, meta, err
		}
		slog.WarnContext(ctx, "catalog session resolution failed", "provider", provName, "error", err)
	}

	merged, err := mergeCatalogs(provName, staticCat, sessionCat)
	return merged, meta, err
}

func resolveSessionCatalog(ctx context.Context, prov core.Provider, provName string, resolver TokenResolver, p *principal.Principal, connection, instance string) (*catalog.Catalog, bool, error) {
	scp, ok := prov.(core.SessionCatalogProvider)
	if !ok {
		return nil, false, nil
	}
	if prov.ConnectionMode() == core.ConnectionModeNone {
		if resolver != nil && p != nil {
			enrichedCtx, token, err := resolver.ResolveToken(ctx, p, provName, connection, instance)
			if err != nil {
				return nil, true, err
			}
			cat, err := scp.CatalogForRequest(enrichedCtx, token)
			return cat, true, err
		}
		ctx = WithCredentialContext(ctx, CredentialContext{Mode: core.ConnectionModeNone})
		cat, err := scp.CatalogForRequest(ctx, "")
		return cat, true, err
	}
	if resolver == nil || p == nil {
		return nil, false, nil
	}

	ctx, token, err := resolver.ResolveToken(ctx, p, provName, connection, instance)
	if err != nil {
		return nil, true, err
	}
	cat, err := scp.CatalogForRequest(ctx, token)
	return cat, true, err
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
