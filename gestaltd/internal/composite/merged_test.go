package composite_test

import (
	"context"
	"net/http"
	"reflect"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/composite"
)

type fakeProvider struct {
	name     string
	connMode core.ConnectionMode
	ops      []core.Operation
	opConn   map[string]string
	execFn   func(ctx context.Context, op string, params map[string]any, token string) (*core.OperationResult, error)
	closed   bool
}

func (p *fakeProvider) Name() string                        { return p.name }
func (p *fakeProvider) DisplayName() string                 { return p.name }
func (p *fakeProvider) Description() string                 { return "" }
func (p *fakeProvider) ConnectionMode() core.ConnectionMode { return p.connMode }
func (p *fakeProvider) AuthTypes() []string                 { return nil }
func (p *fakeProvider) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return nil
}
func (p *fakeProvider) CredentialFields() []core.CredentialFieldDef { return nil }
func (p *fakeProvider) DiscoveryConfig() *core.DiscoveryConfig      { return nil }
func (p *fakeProvider) ConnectionForOperation(operation string) string {
	return p.opConn[operation]
}
func (p *fakeProvider) ResolveConnectionForOperation(operation string, _ map[string]any) (string, error) {
	return p.ConnectionForOperation(operation), nil
}
func (p *fakeProvider) Catalog() *catalog.Catalog {
	cat := &catalog.Catalog{
		Name:       p.name,
		Operations: make([]catalog.CatalogOperation, 0, len(p.ops)),
	}
	for _, op := range p.ops {
		cat.Operations = append(cat.Operations, catalog.CatalogOperation{
			ID:          op.Name,
			Method:      op.Method,
			Path:        "/" + op.Name,
			Description: op.Description,
		})
	}
	return cat
}

func (p *fakeProvider) Execute(ctx context.Context, op string, params map[string]any, token string) (*core.OperationResult, error) {
	if p.execFn != nil {
		return p.execFn(ctx, op, params, token)
	}
	return &core.OperationResult{Status: http.StatusOK, Body: `{"source":"` + p.name + `"}`}, nil
}

func (p *fakeProvider) Close() error { p.closed = true; return nil }

type fakeSessionProvider struct {
	*fakeProvider
	sessionCat *catalog.Catalog
	sessionErr error
}

func (p *fakeSessionProvider) SupportsSessionCatalog() bool { return true }

func (p *fakeSessionProvider) CatalogForRequest(_ context.Context, _ string) (*catalog.Catalog, error) {
	if p.sessionErr != nil {
		return nil, p.sessionErr
	}
	if p.sessionCat == nil {
		return nil, nil
	}
	return p.sessionCat.Clone(), nil
}

func TestNewMergedRejectsOperationCollision(t *testing.T) {
	t.Parallel()

	_, err := composite.NewMergedWithConnections("test", "Test", "desc", "",
		composite.BoundProvider{Provider: &fakeProvider{name: "api", ops: []core.Operation{{Name: "search"}}}},
		composite.BoundProvider{Provider: &fakeProvider{name: "plugin", ops: []core.Operation{{Name: "search"}}}},
	)
	if err == nil {
		t.Fatal("expected error for duplicate operation name")
	}
	want := `operation "search" provided by both "api" and "plugin"`
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestNewMergedRoutesExecuteByOperationName(t *testing.T) {
	t.Parallel()

	apiHit := false
	pluginHit := false
	merged, err := composite.NewMergedWithConnections("test", "Test", "desc", "",
		composite.BoundProvider{Provider: &fakeProvider{
			name: "api",
			ops:  []core.Operation{{Name: "list_items"}},
			execFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				apiHit = true
				return &core.OperationResult{Status: http.StatusOK, Body: `{"from":"api"}`}, nil
			},
		}},
		composite.BoundProvider{Provider: &fakeProvider{
			name: "plugin",
			ops:  []core.Operation{{Name: "query"}},
			execFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				pluginHit = true
				return &core.OperationResult{Status: http.StatusOK, Body: `{"from":"plugin"}`}, nil
			},
		}},
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := merged.Execute(context.Background(), "list_items", nil, ""); err != nil {
		t.Fatal(err)
	}
	if !apiHit {
		t.Error("expected api provider to handle list_items")
	}

	if _, err := merged.Execute(context.Background(), "query", nil, ""); err != nil {
		t.Fatal(err)
	}
	if !pluginHit {
		t.Error("expected plugin provider to handle query")
	}

	if _, err := merged.Execute(context.Background(), "nonexistent", nil, ""); err == nil {
		t.Error("expected error for unknown operation")
	}
}

func TestNewMergedConnectionModeNone(t *testing.T) {
	t.Parallel()

	merged, err := composite.NewMergedWithConnections("test", "Test", "desc", "",
		composite.BoundProvider{Provider: &fakeProvider{name: "a", connMode: core.ConnectionModeNone, ops: []core.Operation{{Name: "a"}}}},
		composite.BoundProvider{Provider: &fakeProvider{name: "b", connMode: core.ConnectionModeNone, ops: []core.Operation{{Name: "b"}}}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if merged.ConnectionMode() != core.ConnectionModeNone {
		t.Errorf("expected %q, got %q", core.ConnectionModeNone, merged.ConnectionMode())
	}
}

func TestMergedCatalogIncludesConstructorMetadata(t *testing.T) {
	t.Parallel()

	merged, err := composite.NewMergedWithConnections("test", "Override", "Override description", "<svg/>",
		composite.BoundProvider{Provider: &fakeProvider{name: "api", ops: []core.Operation{{Name: "list_items"}}}},
		composite.BoundProvider{Provider: &fakeProvider{name: "plugin", ops: []core.Operation{{Name: "query"}}}},
	)
	if err != nil {
		t.Fatal(err)
	}

	cat := merged.Catalog()
	if cat == nil {
		t.Fatal("expected non-nil catalog")
		return
	}
	if cat.DisplayName != "Override" {
		t.Fatalf("DisplayName = %q, want %q", cat.DisplayName, "Override")
	}
	if cat.Description != "Override description" {
		t.Fatalf("Description = %q, want %q", cat.Description, "Override description")
	}
	if cat.IconSVG != "<svg/>" {
		t.Fatalf("IconSVG = %q, want %q", cat.IconSVG, "<svg/>")
	}
}

func TestMergedConnectionBindingPrecedence(t *testing.T) {
	t.Parallel()

	forced, err := composite.NewMergedWithConnections("test", "Test", "desc", "",
		composite.BoundProvider{
			Provider: &fakeProvider{
				name:   "api",
				ops:    []core.Operation{{Name: "list_items"}},
				opConn: map[string]string{"list_items": "reported"},
			},
			Connection: "forced",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := forced.ConnectionForOperation("list_items"); got != "forced" {
		t.Fatalf("forced ConnectionForOperation = %q, want %q", got, "forced")
	}
	resolved, err := forced.ResolveConnectionForOperation("list_items", nil)
	if err != nil {
		t.Fatalf("forced ResolveConnectionForOperation: %v", err)
	}
	if resolved != "forced" {
		t.Fatalf("forced ResolveConnectionForOperation = %q, want %q", resolved, "forced")
	}
	if forced.OperationConnectionOverrideAllowed("list_items", nil) {
		t.Fatal("forced binding should not allow explicit connection override")
	}

	fallback, err := composite.NewMergedWithConnections("test", "Test", "desc", "",
		composite.BoundProvider{
			Provider: &fakeProvider{
				name: "api",
				ops: []core.Operation{
					{Name: "list_items"},
					{Name: "get_item"},
				},
				opConn: map[string]string{"list_items": "reported"},
			},
			FallbackConnection: "fallback",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := fallback.ConnectionForOperation("list_items"); got != "reported" {
		t.Fatalf("fallback reported ConnectionForOperation = %q, want %q", got, "reported")
	}
	if got := fallback.ConnectionForOperation("get_item"); got != "fallback" {
		t.Fatalf("fallback default ConnectionForOperation = %q, want %q", got, "fallback")
	}
	resolved, err = fallback.ResolveConnectionForOperation("get_item", nil)
	if err != nil {
		t.Fatalf("fallback ResolveConnectionForOperation: %v", err)
	}
	if resolved != "fallback" {
		t.Fatalf("fallback ResolveConnectionForOperation = %q, want %q", resolved, "fallback")
	}
	if !fallback.OperationConnectionOverrideAllowed("get_item", nil) {
		t.Fatal("fallback binding should allow explicit connection override")
	}
}

func TestMergedSessionCatalogRoutesDynamicOperation(t *testing.T) {
	t.Parallel()

	dynamicHit := false
	merged, err := composite.NewMergedWithConnections("test", "Test", "desc", "",
		composite.BoundProvider{Provider: &fakeProvider{name: "rest", ops: []core.Operation{{Name: "status"}}}},
		composite.BoundProvider{Provider: &fakeSessionProvider{
			fakeProvider: &fakeProvider{
				name: "graphql",
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
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !core.SupportsSessionCatalog(merged) {
		t.Fatal("expected merged provider to support session catalogs")
	}

	sessionCat, err := merged.CatalogForRequest(context.Background(), "token-123")
	if err != nil {
		t.Fatalf("CatalogForRequest: %v", err)
	}
	if _, ok := catalogOperation(sessionCat, "viewer"); !ok {
		t.Fatalf("session catalog operations = %#v, want viewer", sessionCat.Operations)
	}

	if _, err := merged.Execute(context.Background(), "viewer", nil, "token-123"); err != nil {
		t.Fatalf("Execute(viewer): %v", err)
	}
	if !dynamicHit {
		t.Fatal("expected dynamic session-backed provider to execute viewer")
	}
}

func TestMergedPreservesPostConnectCapability(t *testing.T) {
	t.Parallel()

	want := map[string]string{
		"gestalt.external_identity.type": "slack_identity",
		"gestalt.external_identity.id":   "team:T123:user:U456",
	}

	merged, err := composite.NewMergedWithConnections("test", "Test", "desc", "",
		composite.BoundProvider{Provider: &fakeProvider{name: "rest", ops: []core.Operation{{Name: "status"}}}},
		composite.BoundProvider{Provider: &fakePostConnectProvider{
			fakeProvider: &fakeProvider{name: "slack"},
			metadata:     want,
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !core.SupportsPostConnect(merged) {
		t.Fatal("expected merged provider to expose post-connect support")
	}

	got, supported, err := core.PostConnect(context.Background(), merged, &core.ExternalCredential{
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
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PostConnect metadata = %#v, want %#v", got, want)
	}
}

type falsePositivePostConnectProvider struct {
	*fakeProvider
}

func (p *falsePositivePostConnectProvider) SupportsPostConnect() bool {
	return false
}

func (p *falsePositivePostConnectProvider) PostConnect(_ context.Context, _ *core.ExternalCredential) (map[string]string, error) {
	return nil, core.ErrPostConnectUnsupported
}

func TestMergedPostConnectSkipsProvidersThatAdvertiseNoSupport(t *testing.T) {
	t.Parallel()

	want := map[string]string{
		"gestalt.external_identity.type": "slack_identity",
		"gestalt.external_identity.id":   "team:T123:user:U456",
	}

	merged, err := composite.NewMergedWithConnections("test", "Test", "desc", "",
		composite.BoundProvider{Provider: &falsePositivePostConnectProvider{
			fakeProvider: &fakeProvider{name: "wrapper"},
		}},
		composite.BoundProvider{Provider: &fakePostConnectProvider{
			fakeProvider: &fakeProvider{name: "slack"},
			metadata:     want,
		}},
	)
	if err != nil {
		t.Fatal(err)
	}

	got, supported, err := core.PostConnect(context.Background(), merged, &core.ExternalCredential{
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
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PostConnect metadata = %#v, want %#v", got, want)
	}
}

func catalogOperation(cat *catalog.Catalog, id string) (catalog.CatalogOperation, bool) {
	if cat == nil {
		return catalog.CatalogOperation{}, false
	}
	for i := range cat.Operations {
		if cat.Operations[i].ID == id {
			return cat.Operations[i], true
		}
	}
	return catalog.CatalogOperation{}, false
}
