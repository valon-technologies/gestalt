package composite

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

type MCPUpstream interface {
	core.CatalogProvider
	core.SessionCatalogProvider
	CallTool(ctx context.Context, name string, args map[string]any) (*mcpgo.CallToolResult, error)
	SupportsManualAuth() bool
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
	_ core.CatalogProvider        = (*Provider)(nil)
	_ core.SessionCatalogProvider = (*Provider)(nil)
	_ core.AuthTypeLister         = (*Provider)(nil)
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
		return &oauthProvider{Provider: p, oauthDelegator: oauthDelegator{oauth: oauthProv}}
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
	core.ConnectionModeNone:     0,
	core.ConnectionModeEither:   1,
	core.ConnectionModeIdentity: 2,
	core.ConnectionModeUser:     2,
}

func stricterConnectionMode(a, b core.ConnectionMode) core.ConnectionMode {
	if connectionModeRank[b] > connectionModeRank[a] {
		return b
	}
	return a
}

func (p *Provider) ListOperations() []core.Operation { return p.api.ListOperations() }
func (p *Provider) Catalog() *catalog.Catalog        { return p.buildCatalog() }

func (p *Provider) Execute(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error) {
	return p.api.Execute(ctx, operation, params, token)
}

func (p *Provider) CatalogForRequest(ctx context.Context, token string) (*catalog.Catalog, error) {
	cat, err := p.mcp.CatalogForRequest(ctx, token)
	if err != nil || cat == nil {
		return cat, err
	}
	return tagMCPCatalog(cat), nil
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

func (p *Provider) AuthTypes() []string {
	if atl, ok := p.api.(core.AuthTypeLister); ok {
		return atl.AuthTypes()
	}
	return nil
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
	oauthDelegator
}

func (p *Provider) buildCatalog() *catalog.Catalog {
	mcpCat := p.mcp.Catalog()

	var apiCat *catalog.Catalog
	if cp, ok := p.api.(core.CatalogProvider); ok {
		apiCat = cp.Catalog()
	}
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
		op.Transport = catalog.TransportREST
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
