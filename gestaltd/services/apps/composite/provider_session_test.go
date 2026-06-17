package composite_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/services/apps/composite"
)

type fakeMCPUpstream struct {
	*fakeSessionProvider
}

func (p *fakeMCPUpstream) CallTool(context.Context, string, map[string]any) (*mcpgo.CallToolResult, error) {
	return &mcpgo.CallToolResult{}, nil
}

type countingSessionProvider struct {
	*fakeSessionProvider
	onCatalogForRequest func()
	unauthorized        bool
}

func (p *countingSessionProvider) CatalogForRequest(ctx context.Context, token string) (*catalog.Catalog, error) {
	if p.onCatalogForRequest != nil {
		p.onCatalogForRequest()
	}
	if p.unauthorized {
		return nil, fmt.Errorf("api unauthorized")
	}
	return p.fakeSessionProvider.CatalogForRequest(ctx, token)
}

type countingMCPUpstream struct {
	*fakeMCPUpstream
	onCatalogForRequest func()
	unauthorized        bool
}

func (p *countingMCPUpstream) CatalogForRequest(ctx context.Context, token string) (*catalog.Catalog, error) {
	if p.onCatalogForRequest != nil {
		p.onCatalogForRequest()
	}
	if p.unauthorized {
		return nil, fmt.Errorf("mcp unauthorized")
	}
	return p.fakeMCPUpstream.CatalogForRequest(ctx, token)
}

func TestCompositeCatalogForRequestMergesAPISessionAndMCPSession(t *testing.T) {
	t.Parallel()

	api := &fakeSessionProvider{
		fakeProvider: &fakeProvider{name: "api"},
		sessionCat: &catalog.Catalog{
			Name: "test",
			Operations: []catalog.CatalogOperation{{
				ID:        "viewer",
				Transport: "graphql",
				Query:     "query Viewer { viewer { id } }",
			}},
		},
	}
	mcp := &fakeMCPUpstream{
		fakeSessionProvider: &fakeSessionProvider{
			fakeProvider: &fakeProvider{name: "mcp", connMode: core.ConnectionModeSubject},
			sessionCat: &catalog.Catalog{
				Name: "test",
				Operations: []catalog.CatalogOperation{{
					ID: "search",
				}},
			},
		},
	}

	prov := composite.New("test", api, mcp)
	scp, ok := prov.(core.SessionCatalogProvider)
	if !ok {
		t.Fatal("expected composite provider to expose SessionCatalogProvider")
	}

	cat, err := scp.CatalogForRequest(context.Background(), "token-123")
	if err != nil {
		t.Fatalf("CatalogForRequest: %v", err)
	}

	viewer, ok := catalogOperation(cat, "viewer")
	if !ok {
		t.Fatalf("session catalog operations = %#v, want viewer", cat.Operations)
	}
	if viewer.Transport != "graphql" {
		t.Fatalf("viewer transport = %q, want %q", viewer.Transport, "graphql")
	}

	search, ok := catalogOperation(cat, "search")
	if !ok {
		t.Fatalf("session catalog operations = %#v, want search", cat.Operations)
	}
	if search.Transport != catalog.TransportMCPPassthrough {
		t.Fatalf("search transport = %q, want %q", search.Transport, catalog.TransportMCPPassthrough)
	}
}

func TestCompositeCatalogForRequestAPISurfaceSkipsMCP(t *testing.T) {
	t.Parallel()

	mcpCalls := 0
	api := &fakeSessionProvider{
		fakeProvider: &fakeProvider{name: "api"},
		sessionCat: &catalog.Catalog{
			Name: "test",
			Operations: []catalog.CatalogOperation{{
				ID:        "search",
				Transport: catalog.TransportREST,
				Method:    http.MethodPost,
			}},
		},
	}
	mcp := &countingMCPUpstream{
		fakeMCPUpstream: &fakeMCPUpstream{
			fakeSessionProvider: &fakeSessionProvider{
				fakeProvider: &fakeProvider{name: "mcp", connMode: core.ConnectionModeSubject},
				sessionCat: &catalog.Catalog{
					Name: "test",
					Operations: []catalog.CatalogOperation{{
						ID: "get_page_content",
					}},
				},
			},
		},
		onCatalogForRequest: func() {
			mcpCalls++
		},
		unauthorized: true,
	}

	prov := composite.New("test", api, mcp)
	scp, ok := prov.(core.SessionCatalogProvider)
	if !ok {
		t.Fatal("expected composite provider to expose SessionCatalogProvider")
	}

	ctx := core.WithCatalogSurface(context.Background(), core.CatalogSurfaceAPI)
	cat, err := scp.CatalogForRequest(ctx, "token-123")
	if err != nil {
		t.Fatalf("CatalogForRequest: %v", err)
	}
	if _, ok := catalogOperation(cat, "search"); !ok {
		t.Fatalf("operations = %#v, want search", cat.Operations)
	}
	if _, ok := catalogOperation(cat, "get_page_content"); ok {
		t.Fatalf("operations = %#v, did not expect MCP operation", cat.Operations)
	}
	if mcpCalls != 0 {
		t.Fatalf("MCP catalog calls = %d, want 0", mcpCalls)
	}
}

func TestCompositeCatalogForRequestMCPSurfaceSkipsAPI(t *testing.T) {
	t.Parallel()

	apiCalls := 0
	api := &countingSessionProvider{
		fakeSessionProvider: &fakeSessionProvider{
			fakeProvider: &fakeProvider{name: "api"},
			sessionCat: &catalog.Catalog{
				Name: "test",
				Operations: []catalog.CatalogOperation{{
					ID:        "search",
					Transport: catalog.TransportREST,
					Method:    http.MethodPost,
				}},
			},
		},
		onCatalogForRequest: func() {
			apiCalls++
		},
		unauthorized: true,
	}
	mcp := &fakeMCPUpstream{
		fakeSessionProvider: &fakeSessionProvider{
			fakeProvider: &fakeProvider{name: "mcp", connMode: core.ConnectionModeSubject},
			sessionCat: &catalog.Catalog{
				Name: "test",
				Operations: []catalog.CatalogOperation{{
					ID: "get_page_content",
				}},
			},
		},
	}

	prov := composite.New("test", api, mcp)
	scp, ok := prov.(core.SessionCatalogProvider)
	if !ok {
		t.Fatal("expected composite provider to expose SessionCatalogProvider")
	}

	ctx := core.WithCatalogSurface(context.Background(), core.CatalogSurfaceMCP)
	cat, err := scp.CatalogForRequest(ctx, "token-123")
	if err != nil {
		t.Fatalf("CatalogForRequest: %v", err)
	}
	if _, ok := catalogOperation(cat, "get_page_content"); !ok {
		t.Fatalf("operations = %#v, want get_page_content", cat.Operations)
	}
	if _, ok := catalogOperation(cat, "search"); ok {
		t.Fatalf("operations = %#v, did not expect API operation", cat.Operations)
	}
	if apiCalls != 0 {
		t.Fatalf("API catalog calls = %d, want 0", apiCalls)
	}
}

func TestCompositeExecuteDelegatesDynamicAPISessionOperation(t *testing.T) {
	t.Parallel()

	dynamicHit := false
	api := &fakeSessionProvider{
		fakeProvider: &fakeProvider{
			name: "api",
			execFn: func(_ context.Context, op string, _ map[string]any, _ string) (*core.OperationResult, error) {
				dynamicHit = true
				return &core.OperationResult{Status: http.StatusOK, Body: []byte(`{"operation":"` + op + `"}`)}, nil
			},
		},
		sessionCat: &catalog.Catalog{
			Name: "test",
			Operations: []catalog.CatalogOperation{{
				ID:        "viewer",
				Transport: "graphql",
				Query:     "query Viewer { viewer { id } }",
			}},
		},
	}
	mcp := &fakeMCPUpstream{
		fakeSessionProvider: &fakeSessionProvider{
			fakeProvider: &fakeProvider{name: "mcp"},
			sessionCat:   &catalog.Catalog{Name: "test"},
		},
	}

	prov := composite.New("test", api, mcp)
	if _, err := prov.Execute(context.Background(), "viewer", nil, "token-123"); err != nil {
		t.Fatalf("Execute(viewer): %v", err)
	}
	if !dynamicHit {
		t.Fatal("expected API provider to execute dynamic session-backed viewer operation")
	}
}
