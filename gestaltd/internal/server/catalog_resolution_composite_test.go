package server

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/composite"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

type stubCompositeMCPUpstream struct {
	stubProvider
	cat                 *catalog.Catalog
	catalogForRequestFn func(context.Context, string) (*catalog.Catalog, error)
}

func (s *stubCompositeMCPUpstream) Catalog() *catalog.Catalog {
	return s.cat
}

func (s *stubCompositeMCPUpstream) CatalogForRequest(ctx context.Context, token string) (*catalog.Catalog, error) {
	if s.catalogForRequestFn != nil {
		return s.catalogForRequestFn(ctx, token)
	}
	return s.cat, nil
}

func (s *stubCompositeMCPUpstream) CallTool(context.Context, string, map[string]any) (*mcpgo.CallToolResult, error) {
	return mcpgo.NewToolResultText("ok"), nil
}

func (s *stubCompositeMCPUpstream) Close() error { return nil }

func TestResolveOperation_CompositeProviderPrefersStaticRESTWithoutSessionLookup(t *testing.T) {
	t.Parallel()

	apiProv := &stubCatalogProvider{
		stubProvider: stubProvider{
			name:     "notion",
			connMode: core.ConnectionModeUser,
		},
		cat: &catalog.Catalog{
			Name: "notion",
			Operations: []catalog.CatalogOperation{
				{ID: "api_get_self", Method: http.MethodGet, Transport: catalog.TransportREST},
			},
		},
	}

	var requestCatalogCalls int
	mcpUpstream := &stubCompositeMCPUpstream{
		stubProvider: stubProvider{
			name:     "notion",
			connMode: core.ConnectionModeUser,
		},
		catalogForRequestFn: func(context.Context, string) (*catalog.Catalog, error) {
			requestCatalogCalls++
			return nil, fmt.Errorf("mcpupstream notion: initialize: transport error: unauthorized (401)")
		},
	}

	op, transport, connection, err := invocation.ResolveOperation(
		context.Background(),
		composite.New("notion", apiProv, mcpUpstream),
		"notion",
		&stubTokenResolver{err: fmt.Errorf("unexpected token resolution")},
		&principal.Principal{UserID: "u1"},
		"api_get_self",
		[]string{"MCP"},
		"",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if op.ID != "api_get_self" {
		t.Fatalf("operation = %q, want %q", op.ID, "api_get_self")
	}
	if transport != catalog.TransportREST {
		t.Fatalf("transport = %q, want %q", transport, catalog.TransportREST)
	}
	if connection != "" {
		t.Fatalf("connection = %q, want empty", connection)
	}
	if requestCatalogCalls != 0 {
		t.Fatalf("request catalog calls = %d, want 0", requestCatalogCalls)
	}
}

func TestResolveOperation_CompositeProviderPreservesGraphQLTransport(t *testing.T) {
	t.Parallel()

	apiProv := &stubCatalogProvider{
		stubProvider: stubProvider{
			name:     "linear",
			connMode: core.ConnectionModeUser,
		},
		cat: &catalog.Catalog{
			Name: "linear",
			Operations: []catalog.CatalogOperation{
				{ID: "viewer", Transport: "graphql", Query: "query Viewer { viewer { id } }"},
			},
		},
	}

	var requestCatalogCalls int
	mcpUpstream := &stubCompositeMCPUpstream{
		stubProvider: stubProvider{
			name:     "linear",
			connMode: core.ConnectionModeUser,
		},
		cat: &catalog.Catalog{Name: "linear"},
		catalogForRequestFn: func(context.Context, string) (*catalog.Catalog, error) {
			requestCatalogCalls++
			return nil, fmt.Errorf("unexpected session catalog lookup")
		},
	}

	prov := composite.New("linear", apiProv, mcpUpstream)
	cat := prov.Catalog()
	if got, ok := invocation.CatalogOperationTransport(cat, "viewer"); !ok || got != "graphql" {
		t.Fatalf("viewer transport = %q, ok=%v, want %q", got, ok, "graphql")
	}

	op, transport, connection, err := invocation.ResolveOperation(
		context.Background(),
		prov,
		"linear",
		&stubTokenResolver{err: fmt.Errorf("unexpected token resolution")},
		&principal.Principal{UserID: "u1"},
		"viewer",
		[]string{"MCP"},
		"",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if op.ID != "viewer" {
		t.Fatalf("operation = %q, want %q", op.ID, "viewer")
	}
	if transport != "graphql" {
		t.Fatalf("transport = %q, want %q", transport, "graphql")
	}
	if connection != "" {
		t.Fatalf("connection = %q, want empty", connection)
	}
	if requestCatalogCalls != 0 {
		t.Fatalf("request catalog calls = %d, want 0", requestCatalogCalls)
	}
}
