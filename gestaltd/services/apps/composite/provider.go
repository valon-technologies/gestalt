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

// Provider wraps an API provider and an MCP upstream for a single integration.
// Execute routes by operation ownership.
type Provider struct {
	name string
	api  core.Provider
	mcp  MCPUpstream
}

type operationOwner int

const (
	operationOwnerNone operationOwner = iota
	operationOwnerAPI
	operationOwnerMCP
)

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

func stricterConnectionMode(a, b core.ConnectionMode) core.ConnectionMode {
	a = core.NormalizeConnectionMode(a)
	b = core.NormalizeConnectionMode(b)
	if connectionModeRank(b) > connectionModeRank(a) {
		return b
	}
	return a
}

func connectionModeRank(mode core.ConnectionMode) int {
	switch mode {
	case core.ConnectionModeNone:
		return 0
	case core.ConnectionModeSubject:
		return 1
	default:
		return 2
	}
}

func (p *Provider) Catalog() *catalog.Catalog { return p.buildCatalog() }

func (p *Provider) Execute(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error) {
	switch p.staticOperationOwner(operation) {
	case operationOwnerAPI:
		return p.api.Execute(ctx, operation, params, token)
	case operationOwnerMCP:
		return p.mcp.Execute(ctx, operation, params, token)
	}
	owner, err := sessionProviderForOperation(ctx, p.sessionProvidersForSurface(core.CatalogSurfaceFromContext(ctx)), operation, token)
	if err != nil {
		return nil, err
	}
	if owner != nil {
		return owner.Execute(ctx, operation, params, token)
	}
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
	surface := core.CatalogSurfaceFromContext(ctx)

	var apiCat *catalog.Catalog
	if surface == core.CatalogSurfaceAll || surface == core.CatalogSurfaceAPI {
		if core.SupportsSessionCatalog(p.api) {
			var err error
			apiCat, _, err = core.CatalogForRequest(ctx, p.api, token)
			if err != nil {
				return nil, err
			}
		}
	}

	var mcpCat *catalog.Catalog
	if surface == core.CatalogSurfaceAll || surface == core.CatalogSurfaceMCP {
		var err error
		mcpCat, err = p.mcp.CatalogForRequest(ctx, token)
		if err != nil {
			return nil, err
		}
	}

	switch surface {
	case core.CatalogSurfaceAPI:
		if apiCat != nil {
			return tagRESTCatalog(apiCat), nil
		}
		return tagRESTCatalog(p.api.Catalog()), nil
	case core.CatalogSurfaceMCP:
		if mcpCat != nil {
			return tagMCPTransportCatalog(mcpCat), nil
		}
		return tagMCPTransportCatalog(p.mcp.Catalog()), nil
	default:
		return p.buildCatalogFromSources(apiCat, mcpCat), nil
	}
}

func (p *Provider) ConnectionForOperation(operation string) string {
	switch p.staticOperationOwner(operation) {
	case operationOwnerAPI:
		return p.api.ConnectionForOperation(operation)
	case operationOwnerMCP:
		return p.mcp.ConnectionForOperation(operation)
	}
	return p.api.ConnectionForOperation(operation)
}

func (p *Provider) ResolveConnectionForOperation(operation string, params map[string]any) (string, error) {
	if p.staticOperationOwner(operation) == operationOwnerMCP {
		return resolveProviderConnection(p.mcp, operation, params)
	}
	return resolveProviderConnection(p.api, operation, params)
}

func (p *Provider) OperationConnectionOverrideAllowed(operation string, params map[string]any) bool {
	if p.staticOperationOwner(operation) == operationOwnerMCP {
		return providerConnectionOverrideAllowed(p.mcp, operation, params)
	}
	return providerConnectionOverrideAllowed(p.api, operation, params)
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

func (p *Provider) SupportsHTTPSubject() bool {
	return core.SupportsHTTPSubject(p.api)
}

func (p *Provider) ResolveHTTPSubject(ctx context.Context, req *core.HTTPSubjectResolveRequest) (*core.HTTPResolvedSubject, error) {
	subject, _, err := core.ResolveHTTPSubject(ctx, p.api, req)
	return subject, err
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
		return tagMCPTransportCatalog(mcpCat)
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

func tagMCPTransportCatalog(src *catalog.Catalog) *catalog.Catalog {
	return tagCatalog(src, catalog.TransportMCPPassthrough)
}

func (p *Provider) staticOperationOwner(operation string) operationOwner {
	// Static REST/API operations win ID collisions for compatibility with the
	// existing REST catalog surface; dynamic session collisions are rejected.
	if _, ok := catalog.OperationByID(p.api.Catalog(), operation); ok {
		return operationOwnerAPI
	}
	if _, ok := catalog.OperationByID(p.mcp.Catalog(), operation); ok {
		return operationOwnerMCP
	}
	return operationOwnerNone
}

func resolveProviderConnection(prov core.Provider, operation string, params map[string]any) (string, error) {
	if resolver, ok := prov.(core.OperationConnectionResolver); ok {
		return resolver.ResolveConnectionForOperation(operation, params)
	}
	return prov.ConnectionForOperation(operation), nil
}

func providerConnectionOverrideAllowed(prov core.Provider, operation string, params map[string]any) bool {
	if policy, ok := prov.(core.OperationConnectionOverridePolicy); ok {
		return policy.OperationConnectionOverrideAllowed(operation, params)
	}
	return false
}

func (p *Provider) sessionProvidersForSurface(surface core.CatalogSurface) []core.Provider {
	switch surface {
	case core.CatalogSurfaceAPI:
		return []core.Provider{p.api}
	case core.CatalogSurfaceMCP:
		return []core.Provider{p.mcp}
	default:
		return []core.Provider{p.api, p.mcp}
	}
}

func sessionProviderForOperation(ctx context.Context, providers []core.Provider, operation, token string) (core.Provider, error) {
	var (
		match    core.Provider
		firstErr error
	)
	for _, provider := range providers {
		if !core.SupportsSessionCatalog(provider) {
			continue
		}
		cat, _, err := core.CatalogForRequest(ctx, provider, token)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if _, ok := catalog.OperationByID(cat, operation); !ok {
			continue
		}
		if match != nil {
			return nil, fmt.Errorf("operation %q provided by both %q and %q", operation, match.Name(), provider.Name())
		}
		match = provider
	}
	if match != nil {
		return match, nil
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return nil, nil
}
