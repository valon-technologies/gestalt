package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

type stubProvider struct {
	name        string
	displayName string
	description string
	connMode    core.ConnectionMode
	ops         []core.Operation
}

func (s *stubProvider) Name() string                        { return s.name }
func (s *stubProvider) DisplayName() string                 { return s.displayName }
func (s *stubProvider) Description() string                 { return s.description }
func (s *stubProvider) ConnectionMode() core.ConnectionMode { return s.connMode }
func (s *stubProvider) Catalog() *catalog.Catalog           { return nil }
func (s *stubProvider) Execute(context.Context, string, map[string]any, string) (*core.OperationResult, error) {
	return nil, nil
}

type stubCatalogProvider struct {
	stubProvider
	cat *catalog.Catalog
}

func (s *stubCatalogProvider) Catalog() *catalog.Catalog { return s.cat }

type stubSessionProvider struct {
	stubCatalogProvider
	sessionCat *catalog.Catalog
	sessionErr error
}

func (s *stubSessionProvider) CatalogForRequest(_ context.Context, _ string) (*catalog.Catalog, error) {
	return s.sessionCat, s.sessionErr
}

type stubTokenResolver struct {
	token string
	err   error
}

func (s *stubTokenResolver) ResolveToken(context.Context, *principal.Principal, string, string, string) (string, error) {
	return s.token, s.err
}

func TestResolveCatalog_StaticCatalog(t *testing.T) {
	t.Parallel()

	prov := &stubCatalogProvider{
		stubProvider: stubProvider{
			name:        "widget-api",
			displayName: "Widget API",
			description: "Manages widgets",
			connMode:    core.ConnectionModeUser,
		},
		cat: &catalog.Catalog{
			Name:        "widget-api",
			DisplayName: "Widget API",
			Operations: []catalog.CatalogOperation{
				{
					ID:     "list_widgets",
					Method: http.MethodGet,
					Path:   "/widgets",
					Parameters: []catalog.CatalogParameter{
						{Name: "page", Type: "integer", Location: "query", Required: false},
						{Name: "limit", Type: "integer", Location: "query", Required: true},
					},
				},
			},
		},
	}

	cat, err := invocation.ResolveCatalog(context.Background(), prov, "widget-api", nil, nil, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cat.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(cat.Operations))
	}
	op := cat.Operations[0]
	if op.ID != "list_widgets" {
		t.Fatalf("expected id %q, got %q", "list_widgets", op.ID)
	}
	if op.InputSchema == nil {
		t.Fatal("expected inputSchema to be synthesized")
	}
	var schema map[string]any
	if err := json.Unmarshal(op.InputSchema, &schema); err != nil {
		t.Fatalf("invalid inputSchema JSON: %v", err)
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties in schema")
	}
	if _, ok := props["page"]; !ok {
		t.Fatal("expected page in schema properties")
	}
	if _, ok := props["limit"]; !ok {
		t.Fatal("expected limit in schema properties")
	}
}

func TestResolveCatalog_FlatProviderErrors(t *testing.T) {
	t.Parallel()

	prov := &stubProvider{
		name:        "gadget-svc",
		displayName: "Gadget Service",
		description: "Gadget operations",
		connMode:    core.ConnectionModeNone,
		ops: []core.Operation{
			{
				Name:        "create_gadget",
				Method:      http.MethodPost,
				Description: "Creates a gadget",
				Parameters: []core.Parameter{
					{Name: "label", Type: "string", Required: true},
					{Name: "count", Type: "integer", Required: false, Default: 1},
				},
			},
			{
				Name:        "get_gadget",
				Method:      http.MethodGet,
				Description: "Gets a gadget",
			},
		},
	}

	cat, err := invocation.ResolveCatalog(context.Background(), prov, "gadget-svc", nil, nil, "", "")
	if err == nil {
		t.Fatalf("expected error for provider without catalog, got catalog %+v", cat)
	}
	if got, want := err.Error(), `provider "gadget-svc" does not expose a catalog`; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

func TestResolveCatalog_SessionAndStaticMerge(t *testing.T) {
	t.Parallel()

	prov := &stubSessionProvider{
		stubCatalogProvider: stubCatalogProvider{
			stubProvider: stubProvider{
				name:     "combo-api",
				connMode: core.ConnectionModeUser,
			},
			cat: &catalog.Catalog{
				Name: "combo-api",
				Operations: []catalog.CatalogOperation{
					{ID: "rest_op", Method: http.MethodGet, Transport: catalog.TransportREST},
				},
			},
		},
		sessionCat: &catalog.Catalog{
			Name: "combo-api",
			Operations: []catalog.CatalogOperation{
				{ID: "mcp_op", Method: http.MethodPost, Transport: catalog.TransportMCPPassthrough},
			},
		},
	}

	resolver := &stubTokenResolver{token: "tok_123"}
	p := &principal.Principal{UserID: "u1"}

	cat, err := invocation.ResolveCatalog(context.Background(), prov, "combo-api", resolver, p, "default", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cat.Operations) != 2 {
		t.Fatalf("expected 2 operations, got %d", len(cat.Operations))
	}

	ids := map[string]bool{}
	for _, op := range cat.Operations {
		ids[op.ID] = true
	}
	if !ids["rest_op"] {
		t.Fatal("expected rest_op in merged catalog")
	}
	if !ids["mcp_op"] {
		t.Fatal("expected mcp_op in merged catalog")
	}
}

func TestResolveCatalog_SortsMergedOperationsByID(t *testing.T) {
	t.Parallel()

	prov := &stubSessionProvider{
		stubCatalogProvider: stubCatalogProvider{
			stubProvider: stubProvider{
				name:     "combo-api",
				connMode: core.ConnectionModeUser,
			},
			cat: &catalog.Catalog{
				Name: "combo-api",
				Operations: []catalog.CatalogOperation{
					{ID: "zeta_rest", Method: http.MethodGet, Transport: catalog.TransportREST},
				},
			},
		},
		sessionCat: &catalog.Catalog{
			Name: "combo-api",
			Operations: []catalog.CatalogOperation{
				{ID: "alpha_mcp", Method: http.MethodPost, Transport: catalog.TransportMCPPassthrough},
			},
		},
	}

	resolver := &stubTokenResolver{token: "tok_sorted"}
	p := &principal.Principal{UserID: "u1"}

	cat, err := invocation.ResolveCatalog(context.Background(), prov, "combo-api", resolver, p, "default", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cat.Operations) != 2 {
		t.Fatalf("expected 2 operations, got %d", len(cat.Operations))
	}
	if cat.Operations[0].ID != "alpha_mcp" {
		t.Fatalf("operation[0].ID = %q, want %q", cat.Operations[0].ID, "alpha_mcp")
	}
	if cat.Operations[1].ID != "zeta_rest" {
		t.Fatalf("operation[1].ID = %q, want %q", cat.Operations[1].ID, "zeta_rest")
	}
}

func TestResolveCatalog_SameIDCollision_StaticWins(t *testing.T) {
	t.Parallel()

	prov := &stubSessionProvider{
		stubCatalogProvider: stubCatalogProvider{
			stubProvider: stubProvider{
				name:     "clash-api",
				connMode: core.ConnectionModeUser,
			},
			cat: &catalog.Catalog{
				Name: "clash-api",
				Operations: []catalog.CatalogOperation{
					{ID: "shared_op", Method: http.MethodGet, Transport: catalog.TransportREST, Description: "static version"},
				},
			},
		},
		sessionCat: &catalog.Catalog{
			Name: "clash-api",
			Operations: []catalog.CatalogOperation{
				{ID: "shared_op", Method: http.MethodPost, Transport: catalog.TransportMCPPassthrough, Description: "session version"},
			},
		},
	}

	resolver := &stubTokenResolver{token: "tok_456"}
	p := &principal.Principal{UserID: "u1"}

	cat, err := invocation.ResolveCatalog(context.Background(), prov, "clash-api", resolver, p, "default", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cat.Operations) != 1 {
		t.Fatalf("expected 1 operation after collision, got %d", len(cat.Operations))
	}
	if cat.Operations[0].Description != "static version" {
		t.Fatalf("expected static version to win, got %q", cat.Operations[0].Description)
	}
	if cat.Operations[0].Method != http.MethodGet {
		t.Fatalf("expected GET from static, got %q", cat.Operations[0].Method)
	}
}

func TestResolveCatalog_TokenResolutionFailure_NonFatal(t *testing.T) {
	t.Parallel()

	prov := &stubSessionProvider{
		stubCatalogProvider: stubCatalogProvider{
			stubProvider: stubProvider{
				name:     "auth-api",
				connMode: core.ConnectionModeUser,
			},
			cat: &catalog.Catalog{
				Name: "auth-api",
				Operations: []catalog.CatalogOperation{
					{ID: "static_op", Method: http.MethodGet},
				},
			},
		},
		sessionCat: &catalog.Catalog{
			Name: "auth-api",
			Operations: []catalog.CatalogOperation{
				{ID: "session_only", Method: http.MethodPost},
			},
		},
	}

	resolver := &stubTokenResolver{err: fmt.Errorf("token expired")}
	p := &principal.Principal{UserID: "u1"}

	cat, err := invocation.ResolveCatalog(context.Background(), prov, "auth-api", resolver, p, "default", "")
	if err != nil {
		t.Fatalf("expected no error on token failure, got: %v", err)
	}
	if len(cat.Operations) != 1 {
		t.Fatalf("expected 1 operation (static only), got %d", len(cat.Operations))
	}
	if cat.Operations[0].ID != "static_op" {
		t.Fatalf("expected static_op, got %q", cat.Operations[0].ID)
	}
}

func TestResolveCatalog_NilResolver(t *testing.T) {
	t.Parallel()

	prov := &stubSessionProvider{
		stubCatalogProvider: stubCatalogProvider{
			stubProvider: stubProvider{
				name:     "noauth-api",
				connMode: core.ConnectionModeUser,
			},
			cat: &catalog.Catalog{
				Name: "noauth-api",
				Operations: []catalog.CatalogOperation{
					{ID: "the_op", Method: http.MethodPut},
				},
			},
		},
		sessionCat: &catalog.Catalog{
			Name: "noauth-api",
			Operations: []catalog.CatalogOperation{
				{ID: "hidden_op", Method: http.MethodPost},
			},
		},
	}

	cat, err := invocation.ResolveCatalog(context.Background(), prov, "noauth-api", nil, &principal.Principal{UserID: "u1"}, "default", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cat.Operations) != 1 {
		t.Fatalf("expected 1 operation (static only), got %d", len(cat.Operations))
	}
	if cat.Operations[0].ID != "the_op" {
		t.Fatalf("expected the_op, got %q", cat.Operations[0].ID)
	}
}

func TestResolveCatalog_IconOnlyCatalogPreserved(t *testing.T) {
	t.Parallel()

	prov := &stubCatalogProvider{
		stubProvider: stubProvider{
			name:        "icon-api",
			displayName: "Icon API",
			description: "Has icon and operations",
			connMode:    core.ConnectionModeUser,
			ops: []core.Operation{
				{
					Name:        "do_thing",
					Method:      http.MethodPost,
					Description: "Does a thing",
					Parameters: []core.Parameter{
						{Name: "input", Type: "string", Required: true},
					},
				},
			},
		},
		cat: &catalog.Catalog{
			Name:    "icon-api",
			IconSVG: `<svg/>`,
		},
	}

	cat, err := invocation.ResolveCatalog(context.Background(), prov, "icon-api", nil, nil, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cat.IconSVG != `<svg/>` {
		t.Fatalf("IconSVG = %q, want %q", cat.IconSVG, `<svg/>`)
	}
	if len(cat.Operations) != 0 {
		t.Fatalf("got %d operations, want 0", len(cat.Operations))
	}
}

func TestResolveCatalog_CloneSafety(t *testing.T) {
	t.Parallel()

	original := &catalog.Catalog{
		Name: "clone-api",
		Operations: []catalog.CatalogOperation{
			{
				ID:     "safe_op",
				Method: http.MethodPost,
				Parameters: []catalog.CatalogParameter{
					{Name: "data", Type: "string", Required: true},
				},
			},
		},
	}

	prov := &stubCatalogProvider{
		stubProvider: stubProvider{
			name:     "clone-api",
			connMode: core.ConnectionModeNone,
		},
		cat: original,
	}

	_, err := invocation.ResolveCatalog(context.Background(), prov, "clone-api", nil, nil, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if original.Operations[0].InputSchema != nil {
		t.Fatal("CompileSchemas mutated the provider's original catalog")
	}
}
