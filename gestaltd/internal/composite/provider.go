package composite

import (
	"cmp"
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

type MCPUpstream interface {
	core.Provider
	core.SessionCatalogProvider
	CallTool(ctx context.Context, name string, args map[string]any) (*mcpgo.CallToolResult, error)
	Close() error
}

// Provider wraps a REST-backed API provider and an MCP upstream for a single
// integration. Execute goes to the API; CallTool goes to the upstream.
type Provider struct {
	name string
	api  core.Provider
	mcp  MCPUpstream
}

var (
	_ core.Provider               = (*Provider)(nil)
	_ core.SessionCatalogProvider = (*Provider)(nil)
	_ core.GraphQLSurfaceInvoker  = (*Provider)(nil)
)

// New creates a composite provider. If the API provider implements
// OAuthProvider, the returned provider does too.
func New(name string, apiProv core.Provider, mcpUp MCPUpstream) core.Provider {
	p := &Provider{
		name: name,
		api:  apiProv,
		mcp:  mcpUp,
	}
	if oauthProv, ok := apiProv.(core.OAuthProvider); ok {
		return &oauthProvider{Provider: p, auth: oauthProv}
	}
	return p
}

func (p *Provider) Name() string        { return p.name }
func (p *Provider) DisplayName() string { return p.api.DisplayName() }
func (p *Provider) Description() string { return p.api.Description() }
func (p *Provider) ConnectionMode() core.ConnectionMode {
	return stricterConnectionMode(p.api.ConnectionMode(), p.mcpConnectionMode())
}

func (p *Provider) mcpConnectionMode() core.ConnectionMode {
	if cm, ok := p.mcp.(interface{ ConnectionMode() core.ConnectionMode }); ok {
		return cm.ConnectionMode()
	}
	return core.ConnectionModeNone
}

var connectionModeRank = map[core.ConnectionMode]int{
	core.ConnectionModeNone: 0,
	core.ConnectionModeUser: 1,
}

func stricterConnectionMode(a, b core.ConnectionMode) core.ConnectionMode {
	if connectionModeRank[b] > connectionModeRank[a] {
		return b
	}
	return a
}

func (p *Provider) Catalog() *catalog.Catalog { return p.buildCatalog() }

func (p *Provider) Execute(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error) {
	return p.api.Execute(ctx, operation, params, token)
}

func (p *Provider) InvokeGraphQL(ctx context.Context, request core.GraphQLRequest, token string) (*core.OperationResult, error) {
	invoker, ok := p.api.(core.GraphQLSurfaceInvoker)
	if !ok {
		return nil, fmt.Errorf("graphql surface is not available")
	}
	return invoker.InvokeGraphQL(ctx, request, token)
}

func (p *Provider) CatalogForRequest(ctx context.Context, token string) (*catalog.Catalog, error) {
	var apiCat *catalog.Catalog
	if core.SupportsSessionCatalog(p.api) {
		var err error
		apiCat, _, err = core.CatalogForRequest(ctx, p.api, token)
		if err != nil {
			return nil, err
		}
	}
	mcpCat, err := p.mcp.CatalogForRequest(ctx, token)
	if err != nil {
		return nil, err
	}
	return p.buildCatalogFromSources(apiCat, mcpCat), nil
}

func (p *Provider) ConnectionForOperation(operation string) string {
	return p.api.ConnectionForOperation(operation)
}

func (p *Provider) CallTool(ctx context.Context, name string, args map[string]any) (*mcpgo.CallToolResult, error) {
	return p.mcp.CallTool(ctx, name, args)
}

func (p *Provider) Inner() core.Provider { return p.api }

func (p *Provider) AuthTypes() []string {
	return p.api.AuthTypes()
}

func (p *Provider) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return p.api.ConnectionParamDefs()
}

func (p *Provider) CredentialFields() []core.CredentialFieldDef {
	return p.api.CredentialFields()
}

func (p *Provider) DiscoveryConfig() *core.DiscoveryConfig {
	return p.api.DiscoveryConfig()
}

func (p *Provider) SupportsPostConnect() bool {
	return core.SupportsPostConnect(p.api)
}

func (p *Provider) PostConnect(ctx context.Context, token *core.IntegrationToken) (map[string]string, error) {
	if pcp, ok := p.api.(core.PostConnectCapable); ok {
		return pcp.PostConnect(ctx, token)
	}
	return nil, core.ErrPostConnectUnsupported
}

func (p *Provider) Close() error {
	var firstErr error
	if err := p.mcp.Close(); err != nil {
		firstErr = fmt.Errorf("closing mcp upstream: %w", err)
	}
	if c, ok := p.api.(interface{ Close() error }); ok {
		if err := c.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("closing api provider: %w", err)
		}
	}
	return firstErr
}

type oauthProvider struct {
	*Provider
	auth core.OAuthProvider
}

func (p *oauthProvider) AuthorizationURL(state string, scopes []string) string {
	return p.auth.AuthorizationURL(state, scopes)
}

func (p *oauthProvider) ExchangeCode(ctx context.Context, code string) (*core.TokenResponse, error) {
	return p.auth.ExchangeCode(ctx, code)
}

func (p *oauthProvider) RefreshToken(ctx context.Context, refreshToken string) (*core.TokenResponse, error) {
	return p.auth.RefreshToken(ctx, refreshToken)
}

func (p *Provider) buildCatalog() *catalog.Catalog {
	return p.buildCatalogFromSources(p.api.Catalog(), p.mcp.Catalog())
}

func (p *Provider) buildCatalogFromSources(apiCat, mcpCat *catalog.Catalog) *catalog.Catalog {
	if mcpCat == nil && apiCat == nil {
		return nil
	}
	if apiCat == nil {
		return tagMCPCatalog(mcpCat)
	}
	if mcpCat == nil {
		return tagRESTCatalog(apiCat)
	}

	merged := &catalog.Catalog{
		Name:        p.name,
		DisplayName: cmp.Or(apiCat.DisplayName, mcpCat.DisplayName),
		Description: cmp.Or(apiCat.Description, mcpCat.Description),
		IconSVG:     cmp.Or(apiCat.IconSVG, mcpCat.IconSVG),
		Operations:  make([]catalog.CatalogOperation, 0, len(mcpCat.Operations)+len(apiCat.Operations)),
	}
	for i := range mcpCat.Operations {
		op := mcpCat.Operations[i]
		op.Transport = catalog.TransportMCPPassthrough
		merged.Operations = append(merged.Operations, op)
	}
	for i := range apiCat.Operations {
		op := apiCat.Operations[i]
		if op.Transport == "" {
			op.Transport = catalog.TransportREST
		}
		merged.Operations = append(merged.Operations, op)
	}
	return merged
}

func tagCatalog(src *catalog.Catalog, transport string) *catalog.Catalog {
	out := src.Clone()
	for i := range out.Operations {
		out.Operations[i].Transport = transport
	}
	return out
}

func tagRESTCatalog(src *catalog.Catalog) *catalog.Catalog {
	return tagCatalog(src, catalog.TransportREST)
}

func tagMCPCatalog(src *catalog.Catalog) *catalog.Catalog {
	return tagCatalog(src, catalog.TransportMCPPassthrough)
}
