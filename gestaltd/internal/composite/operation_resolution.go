package composite

import (
	"context"

	"github.com/valon-technologies/gestalt/server/core/catalog"
)

// ResolveStaticOperationForRequest marks API-backed operations as authoritative
// for request execution. Composite session catalogs only describe the MCP
// surface, so static REST ops should not trigger MCP session initialization.
func (p *Provider) ResolveStaticOperationForRequest(_ context.Context, operation string) (catalog.CatalogOperation, bool) {
	return taggedCatalogOperation(p.api.Catalog(), operation, "")
}

// ResolveStaticOperationForRequest marks statically merged operations as
// authoritative for request execution. Merged providers can combine source
// plugin operations with providers that expose session catalogs; static
// operations should still route directly to their owning provider.
func (m *MergedProvider) ResolveStaticOperationForRequest(_ context.Context, operation string) (catalog.CatalogOperation, bool) {
	return taggedCatalogOperation(m.catalog, operation, "")
}

func taggedCatalogOperation(cat *catalog.Catalog, operation, transport string) (catalog.CatalogOperation, bool) {
	if cat == nil {
		return catalog.CatalogOperation{}, false
	}
	for i := range cat.Operations {
		if cat.Operations[i].ID != operation {
			continue
		}
		op := cat.Operations[i]
		if transport != "" {
			op.Transport = transport
		} else if op.Transport == "" {
			op.Transport = catalog.TransportREST
		}
		return op, true
	}
	return catalog.CatalogOperation{}, false
}
