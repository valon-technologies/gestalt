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
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
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
