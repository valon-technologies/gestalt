package composite

import (
	"context"
	"net/http"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

type hybridCatalogProvider struct {
	name string
	cat  *catalog.Catalog
}

func (p *hybridCatalogProvider) Name() string                        { return p.name }
func (p *hybridCatalogProvider) DisplayName() string                 { return p.name }
func (p *hybridCatalogProvider) Description() string                 { return "" }
func (p *hybridCatalogProvider) ConnectionMode() core.ConnectionMode { return core.ConnectionModeNone }
func (p *hybridCatalogProvider) Catalog() *catalog.Catalog           { return p.cat }
func (p *hybridCatalogProvider) Execute(context.Context, string, map[string]any, string) (*core.OperationResult, error) {
	return nil, nil
}

type hybridMCPUpstream struct {
	hybridCatalogProvider
}

func (u *hybridMCPUpstream) CatalogForRequest(context.Context, string) (*catalog.Catalog, error) {
	return u.cat, nil
}
func (u *hybridMCPUpstream) CallTool(context.Context, string, map[string]any) (*mcpgo.CallToolResult, error) {
	return nil, nil
}
func (u *hybridMCPUpstream) SupportsManualAuth() bool { return false }
func (u *hybridMCPUpstream) Close() error             { return nil }

func TestProviderBuildCatalogSortsOperationsByID(t *testing.T) {
	t.Parallel()

	api := &hybridCatalogProvider{
		name: "api",
		cat: &catalog.Catalog{
			Name: "api",
			Operations: []catalog.CatalogOperation{
				{ID: "zeta.rest", Method: http.MethodGet},
			},
		},
	}
	mcp := &hybridMCPUpstream{
		hybridCatalogProvider: hybridCatalogProvider{
			name: "mcp",
			cat: &catalog.Catalog{
				Name: "mcp",
				Operations: []catalog.CatalogOperation{
					{ID: "alpha.mcp", Method: http.MethodPost},
				},
			},
		},
	}

	provider := &Provider{name: "hybrid", api: api, mcp: mcp}
	cat := provider.Catalog()
	if len(cat.Operations) != 2 {
		t.Fatalf("len(Operations) = %d, want 2", len(cat.Operations))
	}
	if cat.Operations[0].ID != "alpha.mcp" {
		t.Fatalf("operation[0].ID = %q, want %q", cat.Operations[0].ID, "alpha.mcp")
	}
	if cat.Operations[1].ID != "zeta.rest" {
		t.Fatalf("operation[1].ID = %q, want %q", cat.Operations[1].ID, "zeta.rest")
	}
}
