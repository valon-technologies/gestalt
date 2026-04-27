package bootstrap

import (
	"context"
	"fmt"
	"io"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
)

func bindProviderConnection(prov core.Provider, connection string) core.Provider {
	if prov == nil || connection == "" {
		return prov
	}

	var wrapped core.Provider = &connectedProvider{
		inner:      prov,
		connection: connection,
	}
	if session, ok := prov.(core.SessionCatalogProvider); ok {
		wrapped = &connectedSessionCatalogProvider{
			Provider: wrapped,
			session:  session,
		}
	}
	if graphQL, ok := prov.(core.GraphQLSurfaceInvoker); ok {
		wrapped = &connectedGraphQLProvider{
			Provider: wrapped,
			graphQL:  graphQL,
		}
	}
	if auth, ok := prov.(core.OAuthProvider); ok {
		wrapped = &connectedOAuthProvider{
			Provider: wrapped,
			auth:     auth,
		}
	}
	return wrapped
}

type connectedProvider struct {
	inner      core.Provider
	connection string
}

func (p *connectedProvider) Name() string                        { return p.inner.Name() }
func (p *connectedProvider) DisplayName() string                 { return p.inner.DisplayName() }
func (p *connectedProvider) Description() string                 { return p.inner.Description() }
func (p *connectedProvider) ConnectionMode() core.ConnectionMode { return p.inner.ConnectionMode() }
func (p *connectedProvider) AuthTypes() []string                 { return p.inner.AuthTypes() }
func (p *connectedProvider) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return p.inner.ConnectionParamDefs()
}
func (p *connectedProvider) CredentialFields() []core.CredentialFieldDef {
	return p.inner.CredentialFields()
}
func (p *connectedProvider) DiscoveryConfig() *core.DiscoveryConfig {
	return p.inner.DiscoveryConfig()
}
func (p *connectedProvider) ConnectionForOperation(string) string { return p.connection }
func (p *connectedProvider) Catalog() *catalog.Catalog            { return p.inner.Catalog() }
func (p *connectedProvider) SupportsPostConnect() bool            { return core.SupportsPostConnect(p.inner) }
func (p *connectedProvider) Execute(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error) {
	return p.inner.Execute(ctx, operation, params, token)
}
func (p *connectedProvider) PostConnect(ctx context.Context, token *core.ExternalCredential) (map[string]string, error) {
	metadata, supported, err := core.PostConnect(ctx, p.inner, token)
	if !supported {
		return nil, core.ErrPostConnectUnsupported
	}
	return metadata, err
}
func (p *connectedProvider) SupportsHTTPSubject() bool { return core.SupportsHTTPSubject(p.inner) }
func (p *connectedProvider) ResolveHTTPSubject(ctx context.Context, req *core.HTTPSubjectResolveRequest) (*core.HTTPResolvedSubject, error) {
	subject, _, err := core.ResolveHTTPSubject(ctx, p.inner, req)
	return subject, err
}
func (p *connectedProvider) Close() error {
	if closer, ok := p.inner.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

type connectedSessionCatalogProvider struct {
	core.Provider
	session core.SessionCatalogProvider
}

func (p *connectedSessionCatalogProvider) SupportsPostConnect() bool {
	return core.SupportsPostConnect(p.Provider)
}

func (p *connectedSessionCatalogProvider) PostConnect(ctx context.Context, token *core.ExternalCredential) (map[string]string, error) {
	metadata, supported, err := core.PostConnect(ctx, p.Provider, token)
	if !supported {
		return nil, core.ErrPostConnectUnsupported
	}
	return metadata, err
}

func (p *connectedSessionCatalogProvider) SupportsHTTPSubject() bool {
	return core.SupportsHTTPSubject(p.Provider)
}

func (p *connectedSessionCatalogProvider) ResolveHTTPSubject(ctx context.Context, req *core.HTTPSubjectResolveRequest) (*core.HTTPResolvedSubject, error) {
	subject, _, err := core.ResolveHTTPSubject(ctx, p.Provider, req)
	return subject, err
}

func (p *connectedSessionCatalogProvider) CatalogForRequest(ctx context.Context, token string) (*catalog.Catalog, error) {
	return p.session.CatalogForRequest(ctx, token)
}

func (p *connectedSessionCatalogProvider) Close() error {
	if closer, ok := p.Provider.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

type connectedGraphQLProvider struct {
	core.Provider
	graphQL core.GraphQLSurfaceInvoker
}

func (p *connectedGraphQLProvider) SupportsPostConnect() bool {
	return core.SupportsPostConnect(p.Provider)
}

func (p *connectedGraphQLProvider) PostConnect(ctx context.Context, token *core.ExternalCredential) (map[string]string, error) {
	metadata, supported, err := core.PostConnect(ctx, p.Provider, token)
	if !supported {
		return nil, core.ErrPostConnectUnsupported
	}
	return metadata, err
}

func (p *connectedGraphQLProvider) SupportsHTTPSubject() bool {
	return core.SupportsHTTPSubject(p.Provider)
}

func (p *connectedGraphQLProvider) ResolveHTTPSubject(ctx context.Context, req *core.HTTPSubjectResolveRequest) (*core.HTTPResolvedSubject, error) {
	subject, _, err := core.ResolveHTTPSubject(ctx, p.Provider, req)
	return subject, err
}

func (p *connectedGraphQLProvider) InvokeGraphQL(ctx context.Context, request core.GraphQLRequest, token string) (*core.OperationResult, error) {
	return p.graphQL.InvokeGraphQL(ctx, request, token)
}

func (p *connectedGraphQLProvider) CatalogForRequest(ctx context.Context, token string) (*catalog.Catalog, error) {
	session, ok := p.Provider.(core.SessionCatalogProvider)
	if !ok {
		return nil, core.WrapSessionCatalogUnsupported(fmt.Errorf("provider %q does not support session catalogs", p.Name()))
	}
	return session.CatalogForRequest(ctx, token)
}

func (p *connectedGraphQLProvider) Close() error {
	if closer, ok := p.Provider.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

type connectedOAuthProvider struct {
	core.Provider
	auth core.OAuthProvider
}

func (p *connectedOAuthProvider) SupportsPostConnect() bool {
	return core.SupportsPostConnect(p.Provider)
}

func (p *connectedOAuthProvider) PostConnect(ctx context.Context, token *core.ExternalCredential) (map[string]string, error) {
	metadata, supported, err := core.PostConnect(ctx, p.Provider, token)
	if !supported {
		return nil, core.ErrPostConnectUnsupported
	}
	return metadata, err
}

func (p *connectedOAuthProvider) SupportsHTTPSubject() bool {
	return core.SupportsHTTPSubject(p.Provider)
}

func (p *connectedOAuthProvider) ResolveHTTPSubject(ctx context.Context, req *core.HTTPSubjectResolveRequest) (*core.HTTPResolvedSubject, error) {
	subject, _, err := core.ResolveHTTPSubject(ctx, p.Provider, req)
	return subject, err
}

func (p *connectedOAuthProvider) AuthorizationURL(state string, scopes []string) string {
	return p.auth.AuthorizationURL(state, scopes)
}

func (p *connectedOAuthProvider) ExchangeCode(ctx context.Context, code string) (*core.TokenResponse, error) {
	return p.auth.ExchangeCode(ctx, code)
}

func (p *connectedOAuthProvider) RefreshToken(ctx context.Context, refreshToken string) (*core.TokenResponse, error) {
	return p.auth.RefreshToken(ctx, refreshToken)
}

func (p *connectedOAuthProvider) CatalogForRequest(ctx context.Context, token string) (*catalog.Catalog, error) {
	session, ok := p.Provider.(core.SessionCatalogProvider)
	if !ok {
		return nil, core.WrapSessionCatalogUnsupported(fmt.Errorf("provider %q does not support session catalogs", p.Name()))
	}
	return session.CatalogForRequest(ctx, token)
}

func (p *connectedOAuthProvider) InvokeGraphQL(ctx context.Context, request core.GraphQLRequest, token string) (*core.OperationResult, error) {
	invoker, ok := p.Provider.(core.GraphQLSurfaceInvoker)
	if !ok {
		return nil, fmt.Errorf("graphql surface is not available")
	}
	return invoker.InvokeGraphQL(ctx, request, token)
}

func (p *connectedOAuthProvider) Close() error {
	if closer, ok := p.Provider.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}
