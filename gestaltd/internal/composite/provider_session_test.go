package composite_test

import (
	"context"
	"net/http"
	"reflect"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/composite"
)

type fakeMCPUpstream struct {
	*fakeSessionProvider
}

func (p *fakeMCPUpstream) CallTool(context.Context, string, map[string]any) (*mcpgo.CallToolResult, error) {
	return &mcpgo.CallToolResult{}, nil
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
			fakeProvider: &fakeProvider{name: "mcp", connMode: core.ConnectionModeUser},
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

func TestCompositeExecuteDelegatesDynamicAPISessionOperation(t *testing.T) {
	t.Parallel()

	dynamicHit := false
	api := &fakeSessionProvider{
		fakeProvider: &fakeProvider{
			name: "api",
			execFn: func(_ context.Context, op string, _ map[string]any, _ string) (*core.OperationResult, error) {
				dynamicHit = true
				return &core.OperationResult{Status: http.StatusOK, Body: `{"operation":"` + op + `"}`}, nil
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

type fakePostConnectProvider struct {
	*fakeProvider
	metadata map[string]string
}

func (p *fakePostConnectProvider) PostConnect(_ context.Context, _ *core.ExternalCredential) (map[string]string, error) {
	return p.metadata, nil
}

func TestCompositePreservesPostConnectCapability(t *testing.T) {
	t.Parallel()

	api := &fakePostConnectProvider{
		fakeProvider: &fakeProvider{name: "api"},
		metadata: map[string]string{
			"gestalt.external_identity.type": "slack_identity",
			"gestalt.external_identity.id":   "team:T123:user:U456",
		},
	}
	mcp := &fakeMCPUpstream{
		fakeSessionProvider: &fakeSessionProvider{
			fakeProvider: &fakeProvider{name: "mcp", connMode: core.ConnectionModeUser},
			sessionCat:   &catalog.Catalog{Name: "test"},
		},
	}

	prov := composite.New("test", api, mcp)
	if !core.SupportsPostConnect(prov) {
		t.Fatal("expected composite provider to expose post-connect support")
	}

	got, supported, err := core.PostConnect(context.Background(), prov, &core.ExternalCredential{
		Integration: "slack",
		Connection:  "default",
		AccessToken: "tok",
	})
	if err != nil {
		t.Fatalf("PostConnect: %v", err)
	}
	if !supported {
		t.Fatal("expected core.PostConnect to report support")
	}
	if !reflect.DeepEqual(got, api.metadata) {
		t.Fatalf("PostConnect metadata = %#v, want %#v", got, api.metadata)
	}
}
