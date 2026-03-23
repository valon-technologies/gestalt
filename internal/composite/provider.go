package composite

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/catalog"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

type MCPUpstream interface {
	core.CatalogProvider
	CallTool(ctx context.Context, name string, args map[string]any) (*mcpgo.CallToolResult, error)
	SupportsManualAuth() bool
	Close() error
}

// Provider wraps an HTTP API provider and an MCP upstream for a single
// integration. Execute goes to the API; CallTool goes to the upstream.
type Provider struct {
	name string
	api  core.Provider
	mcp  MCPUpstream
	cat  *catalog.Catalog
}

var (
	_ core.Provider        = (*Provider)(nil)
	_ core.CatalogProvider = (*Provider)(nil)
)

// New creates a composite provider. If the API provider implements
// OAuthProvider, the returned provider does too.
func New(name string, apiProv core.Provider, mcpUp MCPUpstream) core.Provider {
	p := &Provider{
		name: name,
		api:  apiProv,
		mcp:  mcpUp,
	}
	p.cat = p.buildCatalog()
	if oauth, ok := apiProv.(core.OAuthProvider); ok {
		return &oauthProvider{Provider: p, oauth: oauth}
	}
	return p
}

func (p *Provider) Name() string                        { return p.name }
func (p *Provider) DisplayName() string                 { return p.api.DisplayName() }
func (p *Provider) Description() string                 { return p.api.Description() }
func (p *Provider) ConnectionMode() core.ConnectionMode { return p.api.ConnectionMode() }
func (p *Provider) ListOperations() []core.Operation    { return p.api.ListOperations() }
func (p *Provider) Catalog() *catalog.Catalog           { return p.cat }

func (p *Provider) Execute(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error) {
	return p.api.Execute(ctx, operation, params, token)
}

func (p *Provider) CallTool(ctx context.Context, name string, args map[string]any) (*mcpgo.CallToolResult, error) {
	return p.mcp.CallTool(ctx, name, args)
}

func (p *Provider) Inner() core.Provider { return p.api }

// SupportsManualAuth delegates to the API provider only. The MCP
// upstream's manual-auth support is irrelevant — the server uses this
// to decide whether to block the OAuth connection flow.
func (p *Provider) SupportsManualAuth() bool {
	if mp, ok := p.api.(core.ManualProvider); ok {
		return mp.SupportsManualAuth()
	}
	return false
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

// oauthProvider wraps a composite Provider and delegates OAuth methods
// to the API provider, following the same pattern as restrictedOAuth.
type oauthProvider struct {
	*Provider
	oauth core.OAuthProvider
}

func (o *oauthProvider) AuthorizationURL(state string, scopes []string) string {
	return o.oauth.AuthorizationURL(state, scopes)
}

func (o *oauthProvider) ExchangeCode(ctx context.Context, code string) (*core.TokenResponse, error) {
	return o.oauth.ExchangeCode(ctx, code)
}

func (o *oauthProvider) RefreshToken(ctx context.Context, refreshToken string) (*core.TokenResponse, error) {
	return o.oauth.RefreshToken(ctx, refreshToken)
}

func (p *Provider) buildCatalog() *catalog.Catalog {
	mcpCat := p.mcp.Catalog()

	var apiCat *catalog.Catalog
	if cp, ok := p.api.(core.CatalogProvider); ok {
		apiCat = cp.Catalog()
	}
	if apiCat == nil {
		return tagMCPCatalog(mcpCat)
	}

	merged := &catalog.Catalog{
		Name:        p.name,
		DisplayName: mcpCat.DisplayName,
		Description: mcpCat.Description,
		Operations:  make([]catalog.CatalogOperation, 0, len(mcpCat.Operations)+len(apiCat.Operations)),
	}
	for i := range mcpCat.Operations {
		op := mcpCat.Operations[i]
		op.Transport = catalog.TransportMCPPassthrough
		merged.Operations = append(merged.Operations, op)
	}
	for i := range apiCat.Operations {
		op := apiCat.Operations[i]
		op.Transport = catalog.TransportHTTP
		merged.Operations = append(merged.Operations, op)
	}
	return merged
}

func tagMCPCatalog(src *catalog.Catalog) *catalog.Catalog {
	out := &catalog.Catalog{
		Name:        src.Name,
		DisplayName: src.DisplayName,
		Description: src.Description,
		Operations:  make([]catalog.CatalogOperation, len(src.Operations)),
	}
	copy(out.Operations, src.Operations)
	for i := range out.Operations {
		out.Operations[i].Transport = catalog.TransportMCPPassthrough
	}
	return out
}
