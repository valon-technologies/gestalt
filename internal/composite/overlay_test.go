package composite

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/catalog"
)

type stubProvider struct {
	name       string
	operations []core.Operation
	executeFn  func(ctx context.Context, op string, params map[string]any, token string) (*core.OperationResult, error)
	closed     bool
}

func (s *stubProvider) Name() string                        { return s.name }
func (s *stubProvider) DisplayName() string                 { return s.name + " display" }
func (s *stubProvider) Description() string                 { return s.name + " desc" }
func (s *stubProvider) ConnectionMode() core.ConnectionMode { return core.ConnectionModeUser }
func (s *stubProvider) ListOperations() []core.Operation    { return s.operations }
func (s *stubProvider) Execute(ctx context.Context, op string, params map[string]any, token string) (*core.OperationResult, error) {
	if s.executeFn != nil {
		return s.executeFn(ctx, op, params, token)
	}
	return &core.OperationResult{Status: 200, Body: s.name + ":" + op}, nil
}
func (s *stubProvider) Close() error { s.closed = true; return nil }

type stubCatalogProvider struct {
	stubProvider
	cat *catalog.Catalog
}

func (s *stubCatalogProvider) Catalog() *catalog.Catalog { return s.cat }

type stubOAuthProvider struct {
	stubCatalogProvider
	authURL string
}

func (s *stubOAuthProvider) AuthorizationURL(state string, scopes []string) string {
	return s.authURL + "?state=" + state
}
func (s *stubOAuthProvider) ExchangeCode(ctx context.Context, code string) (*core.TokenResponse, error) {
	return &core.TokenResponse{AccessToken: "tok-" + code}, nil
}
func (s *stubOAuthProvider) RefreshToken(ctx context.Context, rt string) (*core.TokenResponse, error) {
	return &core.TokenResponse{AccessToken: "refreshed"}, nil
}

type stubManualProvider struct {
	stubProvider
	manual bool
}

func (s *stubManualProvider) SupportsManualAuth() bool { return s.manual }

type stubConnectionParamProvider struct {
	stubProvider
	defs map[string]core.ConnectionParamDef
}

func (s *stubConnectionParamProvider) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return s.defs
}

type stubPostConnectProvider struct {
	stubProvider
	hook core.PostConnectHook
}

func (s *stubPostConnectProvider) PostConnectHook() core.PostConnectHook { return s.hook }

func TestOverlayExecuteRouting(t *testing.T) {
	t.Parallel()

	base := &stubProvider{
		name:       "base",
		operations: []core.Operation{{Name: "base_op"}, {Name: "shared_op"}},
	}
	overlay := &stubProvider{
		name:       "overlay",
		operations: []core.Operation{{Name: "overlay_op"}, {Name: "shared_op"}},
	}

	p := NewOverlay("test", base, overlay)

	res, err := p.Execute(context.Background(), "overlay_op", nil, "")
	if err != nil {
		t.Fatalf("Execute overlay_op: %v", err)
	}
	if res.Body != "overlay:overlay_op" {
		t.Errorf("overlay_op body = %q, want %q", res.Body, "overlay:overlay_op")
	}

	res, err = p.Execute(context.Background(), "base_op", nil, "")
	if err != nil {
		t.Fatalf("Execute base_op: %v", err)
	}
	if res.Body != "base:base_op" {
		t.Errorf("base_op body = %q, want %q", res.Body, "base:base_op")
	}

	res, err = p.Execute(context.Background(), "shared_op", nil, "")
	if err != nil {
		t.Fatalf("Execute shared_op: %v", err)
	}
	if res.Body != "overlay:shared_op" {
		t.Errorf("shared_op body = %q, want %q", res.Body, "overlay:shared_op")
	}
}

func TestOverlayExecuteUnknown(t *testing.T) {
	t.Parallel()

	base := &stubProvider{
		name:       "base",
		operations: []core.Operation{{Name: "base_op"}},
		executeFn: func(_ context.Context, op string, _ map[string]any, _ string) (*core.OperationResult, error) {
			return nil, errors.New("unknown operation: " + op)
		},
	}
	overlay := &stubProvider{
		name:       "overlay",
		operations: []core.Operation{{Name: "overlay_op"}},
	}

	p := NewOverlay("test", base, overlay)
	_, err := p.Execute(context.Background(), "no_such_op", nil, "")
	if err == nil {
		t.Fatal("expected error for unknown operation")
	}
}

func TestOverlayListOperationsUnion(t *testing.T) {
	t.Parallel()

	base := &stubProvider{
		name:       "base",
		operations: []core.Operation{{Name: "base_op"}, {Name: "shared_op", Description: "from base"}},
	}
	overlay := &stubProvider{
		name:       "overlay",
		operations: []core.Operation{{Name: "overlay_op"}, {Name: "shared_op", Description: "from overlay"}},
	}

	p := NewOverlay("test", base, overlay)
	ops := p.ListOperations()

	if len(ops) != 3 {
		t.Fatalf("ListOperations: got %d, want 3", len(ops))
	}

	byName := make(map[string]core.Operation)
	for _, op := range ops {
		byName[op.Name] = op
	}
	if _, ok := byName["base_op"]; !ok {
		t.Error("missing base_op")
	}
	if _, ok := byName["overlay_op"]; !ok {
		t.Error("missing overlay_op")
	}
	if byName["shared_op"].Description != "from overlay" {
		t.Errorf("shared_op description = %q, want %q", byName["shared_op"].Description, "from overlay")
	}
}

func TestOverlayCatalogMerge(t *testing.T) {
	t.Parallel()

	base := &stubCatalogProvider{
		stubProvider: stubProvider{name: "base", operations: []core.Operation{{Name: "base_op"}}},
		cat: &catalog.Catalog{
			Name:        "base",
			DisplayName: "Base",
			Description: "base desc",
			Operations: []catalog.CatalogOperation{
				{ID: "base_op", Transport: catalog.TransportHTTP},
				{ID: "shared_op", Transport: catalog.TransportHTTP},
			},
		},
	}
	overlay := &stubCatalogProvider{
		stubProvider: stubProvider{name: "overlay", operations: []core.Operation{{Name: "overlay_op"}, {Name: "shared_op"}}},
		cat: &catalog.Catalog{
			Name: "overlay",
			Operations: []catalog.CatalogOperation{
				{ID: "overlay_op"},
				{ID: "shared_op"},
			},
		},
	}

	p := NewOverlay("test", base, overlay)
	cp := p.(core.CatalogProvider)
	cat := cp.Catalog()

	if cat == nil {
		t.Fatal("Catalog() returned nil")
	}
	if cat.DisplayName != "Base" {
		t.Errorf("DisplayName = %q, want %q", cat.DisplayName, "Base")
	}

	if len(cat.Operations) != 3 {
		t.Fatalf("got %d ops, want 3", len(cat.Operations))
	}

	byID := make(map[string]catalog.CatalogOperation)
	for _, op := range cat.Operations {
		byID[op.ID] = op
	}
	if byID["overlay_op"].Transport != catalog.TransportPlugin {
		t.Errorf("overlay_op transport = %q, want %q", byID["overlay_op"].Transport, catalog.TransportPlugin)
	}
	if byID["shared_op"].Transport != catalog.TransportPlugin {
		t.Errorf("shared_op transport = %q, want %q", byID["shared_op"].Transport, catalog.TransportPlugin)
	}
	if byID["base_op"].Transport != catalog.TransportHTTP {
		t.Errorf("base_op transport = %q, want %q", byID["base_op"].Transport, catalog.TransportHTTP)
	}
}

func TestOverlayOAuthDelegation(t *testing.T) {
	t.Parallel()

	base := &stubOAuthProvider{
		stubCatalogProvider: stubCatalogProvider{
			stubProvider: stubProvider{name: "base", operations: []core.Operation{{Name: "base_op"}}},
		},
		authURL: "https://auth.example.com",
	}
	overlay := &stubProvider{
		name:       "overlay",
		operations: []core.Operation{{Name: "overlay_op"}},
	}

	p := NewOverlay("test", base, overlay)

	oauthP, ok := p.(core.OAuthProvider)
	if !ok {
		t.Fatal("expected OAuthProvider interface")
	}

	url := oauthP.AuthorizationURL("abc", nil)
	if url != "https://auth.example.com?state=abc" {
		t.Errorf("AuthorizationURL = %q", url)
	}

	tok, err := oauthP.ExchangeCode(context.Background(), "code123")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if tok.AccessToken != "tok-code123" {
		t.Errorf("AccessToken = %q", tok.AccessToken)
	}
}

func TestOverlaySupportsManualAuth(t *testing.T) {
	t.Parallel()

	base := &stubManualProvider{
		stubProvider: stubProvider{name: "base", operations: []core.Operation{{Name: "op1"}}},
		manual:       true,
	}
	overlay := &stubProvider{
		name:       "overlay",
		operations: []core.Operation{{Name: "op2"}},
	}

	p := NewOverlay("test", base, overlay).(*OverlayProvider)
	if !p.SupportsManualAuth() {
		t.Error("SupportsManualAuth should delegate to base")
	}
}

func TestOverlayConnectionParamDefs(t *testing.T) {
	t.Parallel()

	defs := map[string]core.ConnectionParamDef{
		"project_id": {Required: true, Description: "Project ID"},
	}
	base := &stubConnectionParamProvider{
		stubProvider: stubProvider{name: "base", operations: []core.Operation{{Name: "op1"}}},
		defs:         defs,
	}
	overlay := &stubProvider{
		name:       "overlay",
		operations: []core.Operation{{Name: "op2"}},
	}

	p := NewOverlay("test", base, overlay).(*OverlayProvider)
	got := p.ConnectionParamDefs()
	if len(got) != 1 || got["project_id"].Description != "Project ID" {
		t.Errorf("ConnectionParamDefs = %v", got)
	}
}

func TestOverlayPostConnectHook(t *testing.T) {
	t.Parallel()

	base := &stubPostConnectProvider{
		stubProvider: stubProvider{name: "base", operations: []core.Operation{{Name: "op1"}}},
		hook: func(_ context.Context, _ *core.IntegrationToken, _ *http.Client) (map[string]string, error) {
			return nil, nil
		},
	}
	overlay := &stubProvider{
		name:       "overlay",
		operations: []core.Operation{{Name: "op2"}},
	}

	p := NewOverlay("test", base, overlay).(*OverlayProvider)
	if p.PostConnectHook() == nil {
		t.Error("PostConnectHook should delegate to base")
	}
}

func TestOverlayClose(t *testing.T) {
	t.Parallel()

	base := &stubProvider{name: "base", operations: []core.Operation{{Name: "op1"}}}
	overlay := &stubProvider{name: "overlay", operations: []core.Operation{{Name: "op2"}}}

	p := NewOverlay("test", base, overlay).(*OverlayProvider)
	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !base.closed {
		t.Error("base not closed")
	}
	if !overlay.closed {
		t.Error("overlay not closed")
	}
}

type stubSessionCatalogProvider struct {
	stubProvider
	cat    *catalog.Catalog
	catErr error
}

func (s *stubSessionCatalogProvider) CatalogForRequest(_ context.Context, _ string) (*catalog.Catalog, error) {
	return s.cat, s.catErr
}

func TestOverlayCatalogForRequest(t *testing.T) {
	t.Parallel()

	t.Run("delegates to base", func(t *testing.T) {
		t.Parallel()

		want := &catalog.Catalog{Name: "session-cat", Operations: []catalog.CatalogOperation{{ID: "op1"}}}
		base := &stubSessionCatalogProvider{
			stubProvider: stubProvider{name: "base", operations: []core.Operation{{Name: "op1"}}},
			cat:          want,
		}
		overlay := &stubProvider{name: "overlay", operations: []core.Operation{{Name: "op2"}}}

		p := NewOverlay("test", base, overlay).(*OverlayProvider)
		got, err := p.CatalogForRequest(context.Background(), "tok-123")
		if err != nil {
			t.Fatalf("CatalogForRequest: %v", err)
		}
		if got != want {
			t.Errorf("CatalogForRequest = %v, want %v", got, want)
		}
	})

	t.Run("returns nil when base lacks interface", func(t *testing.T) {
		t.Parallel()

		base := &stubProvider{name: "base", operations: []core.Operation{{Name: "op1"}}}
		overlay := &stubProvider{name: "overlay", operations: []core.Operation{{Name: "op2"}}}

		p := NewOverlay("test", base, overlay).(*OverlayProvider)
		got, err := p.CatalogForRequest(context.Background(), "tok-123")
		if err != nil {
			t.Fatalf("CatalogForRequest: %v", err)
		}
		if got != nil {
			t.Errorf("CatalogForRequest = %v, want nil", got)
		}
	})
}

func TestOverlayNonOAuthBase(t *testing.T) {
	t.Parallel()

	base := &stubProvider{name: "base", operations: []core.Operation{{Name: "op1"}}}
	overlay := &stubProvider{name: "overlay", operations: []core.Operation{{Name: "op2"}}}

	p := NewOverlay("test", base, overlay)
	if _, ok := p.(*OverlayProvider); !ok {
		t.Errorf("expected *OverlayProvider, got %T", p)
	}
	if _, ok := p.(core.OAuthProvider); ok {
		t.Error("non-OAuth base should not produce OAuthProvider")
	}
}
