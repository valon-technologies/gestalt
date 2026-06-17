package invocation

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

type surfaceFilterStaticProvider struct {
	static *catalog.Catalog
}

func (p *surfaceFilterStaticProvider) Name() string        { return "filter-test" }
func (p *surfaceFilterStaticProvider) DisplayName() string { return "filter-test" }
func (p *surfaceFilterStaticProvider) Description() string { return "" }
func (p *surfaceFilterStaticProvider) ConnectionMode() core.ConnectionMode {
	return core.ConnectionModeNone
}
func (p *surfaceFilterStaticProvider) Catalog() *catalog.Catalog { return p.static }
func (p *surfaceFilterStaticProvider) Execute(context.Context, string, map[string]any, string) (*core.OperationResult, error) {
	return nil, errors.New("not implemented")
}

func (p *surfaceFilterStaticProvider) AuthTypes() []string { return nil }
func (p *surfaceFilterStaticProvider) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return nil
}
func (p *surfaceFilterStaticProvider) CredentialFields() []core.CredentialFieldDef { return nil }
func (p *surfaceFilterStaticProvider) DiscoveryConfig() *core.DiscoveryConfig      { return nil }
func (p *surfaceFilterStaticProvider) ConnectionForOperation(string) string        { return "" }

func TestFilterCatalogBySurface_MCPSurfaceExcludesStaticREST(t *testing.T) {
	t.Parallel()

	static := &catalog.Catalog{
		Name: "filter-test",
		Operations: []catalog.CatalogOperation{
			{ID: "search", Method: http.MethodPost, Transport: catalog.TransportREST},
			{ID: "get_page_content", Method: http.MethodPost, Transport: catalog.TransportMCPPassthrough},
		},
	}
	prov := &surfaceFilterStaticProvider{static: static}

	cat, _, err := ResolveCatalogForTargetsWithMetadata(
		context.Background(),
		prov,
		prov.Name(),
		nil,
		nil,
		[]CatalogResolutionTarget{{Surface: core.CatalogSurfaceMCP}},
		false,
	)
	if err != nil {
		t.Fatalf("ResolveCatalogForTargetsWithMetadata() error = %v", err)
	}
	if len(cat.Operations) != 1 || cat.Operations[0].ID != "get_page_content" {
		t.Fatalf("operations = %#v, want [get_page_content]", cat.Operations)
	}
}

func TestFilterCatalogBySurface_APISurfaceExcludesStaticMCP(t *testing.T) {
	t.Parallel()

	static := &catalog.Catalog{
		Name: "filter-test",
		Operations: []catalog.CatalogOperation{
			{ID: "search", Method: http.MethodPost, Transport: catalog.TransportREST},
			{ID: "get_page_content", Method: http.MethodPost, Transport: catalog.TransportMCPPassthrough},
		},
	}
	prov := &surfaceFilterStaticProvider{static: static}

	cat, _, err := ResolveCatalogForTargetsWithMetadata(
		context.Background(),
		prov,
		prov.Name(),
		nil,
		nil,
		[]CatalogResolutionTarget{{Surface: core.CatalogSurfaceAPI}},
		false,
	)
	if err != nil {
		t.Fatalf("ResolveCatalogForTargetsWithMetadata() error = %v", err)
	}
	if len(cat.Operations) != 1 || cat.Operations[0].ID != "search" {
		t.Fatalf("operations = %#v, want [search]", cat.Operations)
	}
}

type surfaceFilterSessionProvider struct {
	surfaceFilterStaticProvider
}

func (p *surfaceFilterSessionProvider) SupportsSessionCatalog() bool { return true }

func (p *surfaceFilterSessionProvider) CatalogForRequest(context.Context, string) (*catalog.Catalog, error) {
	return nil, core.ErrSessionCatalogUnavailable
}

func TestFilterCatalogBySurface_StaticFallbackRespectsMCPSurface(t *testing.T) {
	t.Parallel()

	static := &catalog.Catalog{
		Name: "filter-test",
		Operations: []catalog.CatalogOperation{
			{ID: "search", Method: http.MethodPost, Transport: catalog.TransportREST},
			{ID: "get_page_content", Method: http.MethodPost, Transport: catalog.TransportMCPPassthrough},
		},
	}
	prov := &surfaceFilterSessionProvider{surfaceFilterStaticProvider{static: static}}

	cat, meta, err := ResolveCatalogForTargetsWithMetadata(
		context.Background(),
		prov,
		prov.Name(),
		&stubTokenResolver{token: "tok"},
		&principal.Principal{UserID: "u1"},
		[]CatalogResolutionTarget{{Connection: "OAuth", Surface: core.CatalogSurfaceMCP}},
		false,
	)
	if err != nil {
		t.Fatalf("ResolveCatalogForTargetsWithMetadata() error = %v", err)
	}
	if !meta.SessionFailed {
		t.Fatal("expected session failure metadata")
	}
	if len(cat.Operations) != 1 || cat.Operations[0].ID != "get_page_content" {
		t.Fatalf("operations = %#v, want [get_page_content]", cat.Operations)
	}
}

func TestFilterCatalogBySurface_StaticFallbackRespectsAPISurface(t *testing.T) {
	t.Parallel()

	static := &catalog.Catalog{
		Name: "filter-test",
		Operations: []catalog.CatalogOperation{
			{ID: "search", Method: http.MethodPost, Transport: catalog.TransportREST},
			{ID: "get_page_content", Method: http.MethodPost, Transport: catalog.TransportMCPPassthrough},
		},
	}
	prov := &surfaceFilterSessionProvider{surfaceFilterStaticProvider{static: static}}

	cat, _, err := ResolveCatalogForTargetsWithMetadata(
		context.Background(),
		prov,
		prov.Name(),
		&stubTokenResolver{token: "tok"},
		&principal.Principal{UserID: "u1"},
		[]CatalogResolutionTarget{{Connection: "OAuth", Surface: core.CatalogSurfaceAPI}},
		false,
	)
	if err != nil {
		t.Fatalf("ResolveCatalogForTargetsWithMetadata() error = %v", err)
	}
	if len(cat.Operations) != 1 || cat.Operations[0].ID != "search" {
		t.Fatalf("operations = %#v, want [search]", cat.Operations)
	}
}

type stubTokenResolver struct {
	token string
	err   error
}

func (r *stubTokenResolver) ResolveToken(context.Context, *principal.Principal, string, string, string) (context.Context, string, error) {
	if r.err != nil {
		return nil, "", r.err
	}
	return context.Background(), r.token, nil
}
