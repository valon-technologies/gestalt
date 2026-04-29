package declarative

import "github.com/valon-technologies/gestalt/server/core/catalog"

func setTestCatalog(b *Base, ops ...catalog.CatalogOperation) {
	b.SetCatalog(&catalog.Catalog{
		Name:       "test",
		Operations: ops,
	})
}

func restCatalogOp(id, method, path string, params ...catalog.CatalogParameter) catalog.CatalogOperation {
	return catalog.CatalogOperation{
		ID:         id,
		Method:     method,
		Path:       path,
		Parameters: params,
	}
}

func graphQLCatalogOp(id, query string) catalog.CatalogOperation {
	return catalog.CatalogOperation{
		ID:        id,
		Query:     query,
		Transport: transportGraphQL,
	}
}
