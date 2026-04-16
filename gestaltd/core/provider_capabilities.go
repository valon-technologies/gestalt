package core

import (
	"context"

	"github.com/valon-technologies/gestalt/server/core/catalog"
)

func SupportsSessionCatalog(prov Provider) bool {
	_, ok := prov.(SessionCatalogProvider)
	return ok
}

func CatalogForRequest(ctx context.Context, prov Provider, token string) (*catalog.Catalog, bool, error) {
	scp, ok := prov.(SessionCatalogProvider)
	if !ok {
		return nil, false, nil
	}
	cat, err := scp.CatalogForRequest(ctx, token)
	return cat, true, err
}
