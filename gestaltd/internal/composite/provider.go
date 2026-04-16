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
)

// New creates a composite provider. Execute goes to the API provider; MCP
// passthrough and session catalog calls go to the upstream.
func New(name string, apiProv core.Provider, mcpUp MCPUpstream) core.Provider {
	return &Provider{
		name: name,
		api:  apiProv,
		mcp:  mcpUp,
	}
}

func (p *Provider) Name() string        { return p.name }
func (p *Provider) DisplayName() string { return p.api.DisplayName() }
func (p *Provider) Description() string { return p.api.Description() }
func (p *Provider) ConnectionMode() core.ConnectionMode {
	mcpMode := core.ConnectionModeNone
	if cm, ok := p.mcp.(interface{ ConnectionMode() core.ConnectionMode }); ok {
		mcpMode = cm.ConnectionMode()
	}
	return stricterConnectionMode(p.api.ConnectionMode(), mcpMode)
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

func (p *Provider) Catalog() *catalog.Catalog { return p.buildCatalog() }

func (p *Provider) Execute(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error) {
	return p.api.Execute(ctx, operation, params, token)
}

func (p *Provider) CatalogForRequest(ctx context.Context, token string) (*catalog.Catalog, error) {
	cat, err := p.mcp.CatalogForRequest(ctx, token)
	if err != nil || cat == nil {
		return cat, err
	}
	return tagCatalog(cat, catalog.TransportMCPPassthrough), nil
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

func (p *Provider) AuthorizationURL(state string, scopes []string) string {
	return p.api.AuthorizationURL(state, scopes)
}

func (p *Provider) ExchangeCode(ctx context.Context, code string) (*core.TokenResponse, error) {
	return p.api.ExchangeCode(ctx, code)
}

func (p *Provider) RefreshToken(ctx context.Context, refreshToken string) (*core.TokenResponse, error) {
	return p.api.RefreshToken(ctx, refreshToken)
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

func (p *Provider) buildCatalog() *catalog.Catalog {
	mcpCat := p.mcp.Catalog()

	apiCat := p.api.Catalog()
	if mcpCat == nil && apiCat == nil {
		return nil
	}
	if apiCat == nil {
		return tagCatalog(mcpCat, catalog.TransportMCPPassthrough)
	}
	if mcpCat == nil {
		return tagCatalog(apiCat, catalog.TransportREST)
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
