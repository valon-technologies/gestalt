package gestalt

import "context"

var _ SessionCatalogProvider = (*catalogAliasProvider)(nil)

type catalogAliasProvider struct{}

func (*catalogAliasProvider) CatalogForRequest(context.Context, string) (*Catalog, error) {
	return &Catalog{
		Operations: []*CatalogOperation{
			{
				Id:       "test.operation",
				Method:   "GET",
				ReadOnly: true,
			},
		},
	}, nil
}
