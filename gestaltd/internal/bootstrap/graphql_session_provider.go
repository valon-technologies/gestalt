package bootstrap

import (
	"context"
	"fmt"
	"io"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/graphql"
	"github.com/valon-technologies/gestalt/server/internal/provider"
)

type graphQLSessionCatalogProvider struct {
	core.Provider
	graphQL            core.GraphQLSurfaceInvoker
	name               string
	endpoint           string
	allowedOperations  map[string]*config.OperationOverride
	selectionOverrides map[string]string
}

var (
	_ core.Provider               = (*graphQLSessionCatalogProvider)(nil)
	_ core.SessionCatalogProvider = (*graphQLSessionCatalogProvider)(nil)
	_ core.GraphQLSurfaceInvoker  = (*graphQLSessionCatalogProvider)(nil)
)

func wrapGraphQLSessionCatalogProvider(
	prov core.Provider,
	name string,
	endpoint string,
	allowedOperations map[string]*config.OperationOverride,
	selectionOverrides map[string]string,
) core.Provider {
	graphQLInvoker, ok := prov.(core.GraphQLSurfaceInvoker)
	if !ok {
		return prov
	}

	wrapped := &graphQLSessionCatalogProvider{
		Provider:           prov,
		graphQL:            graphQLInvoker,
		name:               name,
		endpoint:           endpoint,
		allowedOperations:  allowedOperations,
		selectionOverrides: selectionOverrides,
	}
	if auth, ok := prov.(core.OAuthProvider); ok {
		return &graphQLSessionCatalogOAuthProvider{
			graphQLSessionCatalogProvider: wrapped,
			auth:                          auth,
		}
	}
	return wrapped
}

func (p *graphQLSessionCatalogProvider) SupportsSessionCatalog() bool {
	return true
}

func (p *graphQLSessionCatalogProvider) SupportsPostConnect() bool {
	return core.SupportsPostConnect(p.Provider)
}

func (p *graphQLSessionCatalogProvider) PostConnect(ctx context.Context, token *core.IntegrationToken) (map[string]string, error) {
	metadata, supported, err := core.PostConnect(ctx, p.Provider, token)
	if !supported {
		return nil, core.ErrPostConnectUnsupported
	}
	return metadata, err
}

func (p *graphQLSessionCatalogProvider) SupportsHTTPSubject() bool {
	return core.SupportsHTTPSubject(p.Provider)
}

func (p *graphQLSessionCatalogProvider) ResolveHTTPSubject(ctx context.Context, req *core.HTTPSubjectResolveRequest) (*core.HTTPResolvedSubject, error) {
	subject, _, err := core.ResolveHTTPSubject(ctx, p.Provider, req)
	return subject, err
}

func (p *graphQLSessionCatalogProvider) Execute(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error) {
	if _, ok := graphQLCatalogOperation(p.Catalog(), operation); ok {
		return p.Provider.Execute(ctx, operation, params, token)
	}

	cat, err := p.CatalogForRequest(ctx, token)
	if err != nil {
		return nil, err
	}
	op, ok := graphQLCatalogOperation(cat, operation)
	if !ok {
		return nil, fmt.Errorf("unknown operation: %s", operation)
	}
	if op.Query == "" {
		return nil, fmt.Errorf("graphql operation %q has no query template", operation)
	}
	return p.graphQL.InvokeGraphQL(ctx, core.GraphQLRequest{
		Document:  op.Query,
		Variables: params,
	}, token)
}

func (p *graphQLSessionCatalogProvider) InvokeGraphQL(ctx context.Context, request core.GraphQLRequest, token string) (*core.OperationResult, error) {
	return p.graphQL.InvokeGraphQL(ctx, request, token)
}

func (p *graphQLSessionCatalogProvider) CatalogForRequest(ctx context.Context, token string) (*catalog.Catalog, error) {
	result, err := p.graphQL.InvokeGraphQL(ctx, graphql.IntrospectionRequest(), token)
	if err != nil {
		return nil, err
	}
	schema, err := graphql.SchemaFromResult(result)
	if err != nil {
		return nil, err
	}
	def, err := graphql.DefinitionFromSchema(p.name, p.endpoint, schema, p.allowedOperations, p.selectionOverrides)
	if err != nil {
		return nil, err
	}
	cat := provider.CatalogFromDefinition(def)
	inheritCatalogMetadata(cat, p.Provider)
	return cat, nil
}

func (p *graphQLSessionCatalogProvider) Close() error {
	if closer, ok := p.Provider.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

type graphQLSessionCatalogOAuthProvider struct {
	*graphQLSessionCatalogProvider
	auth core.OAuthProvider
}

func (p *graphQLSessionCatalogOAuthProvider) AuthorizationURL(state string, scopes []string) string {
	return p.auth.AuthorizationURL(state, scopes)
}

func (p *graphQLSessionCatalogOAuthProvider) ExchangeCode(ctx context.Context, code string) (*core.TokenResponse, error) {
	return p.auth.ExchangeCode(ctx, code)
}

func (p *graphQLSessionCatalogOAuthProvider) RefreshToken(ctx context.Context, refreshToken string) (*core.TokenResponse, error) {
	return p.auth.RefreshToken(ctx, refreshToken)
}

func graphQLCatalogOperation(cat *catalog.Catalog, operation string) (catalog.CatalogOperation, bool) {
	if cat == nil {
		return catalog.CatalogOperation{}, false
	}
	for i := range cat.Operations {
		if cat.Operations[i].ID == operation {
			return cat.Operations[i], true
		}
	}
	return catalog.CatalogOperation{}, false
}

func inheritCatalogMetadata(cat *catalog.Catalog, prov core.Provider) {
	if cat == nil || prov == nil {
		return
	}
	staticCat := prov.Catalog()
	if staticCat != nil {
		if cat.Name == "" {
			cat.Name = staticCat.Name
		}
		if cat.DisplayName == "" {
			cat.DisplayName = staticCat.DisplayName
		}
		if cat.Description == "" {
			cat.Description = staticCat.Description
		}
		if cat.IconSVG == "" {
			cat.IconSVG = staticCat.IconSVG
		}
		if cat.BaseURL == "" {
			cat.BaseURL = staticCat.BaseURL
		}
	}
	if cat.Name == "" {
		cat.Name = prov.Name()
	}
	if cat.DisplayName == "" {
		cat.DisplayName = prov.DisplayName()
	}
	if cat.Description == "" {
		cat.Description = prov.Description()
	}
}
