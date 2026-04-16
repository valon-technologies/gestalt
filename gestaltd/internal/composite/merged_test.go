package composite_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/composite"
)

type fakeProvider struct {
	core.NoOAuth
	name     string
	connMode core.ConnectionMode
	ops      []core.Operation
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
func (p *fakeProvider) ConnectionForOperation(string) string        { return "" }
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
