package integration_test

import (
	"context"
	"testing"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/catalog"
	coreintegration "github.com/valon-technologies/gestalt/core/integration"
	coretesting "github.com/valon-technologies/gestalt/core/testing"
)

// stubWithOps extends StubIntegration with concrete operations.
type stubWithOps struct {
	coretesting.StubIntegration
	ops []core.Operation
}

func (s *stubWithOps) ListOperations() []core.Operation {
	return s.ops
}

func sampleOps() []core.Operation {
	return []core.Operation{
		{Name: "list_channels", Description: "List channels"},
		{Name: "send_message", Description: "Send a message"},
		{Name: "delete_message", Description: "Delete a message"},
	}
}

func TestListOperationsFilters(t *testing.T) {
	t.Parallel()

	inner := &stubWithOps{
		StubIntegration: coretesting.StubIntegration{N: "test_provider"},
		ops:             sampleOps(),
	}

	r := coreintegration.NewRestricted(inner, map[string]string{"list_channels": "", "send_message": ""})
	ops := r.ListOperations()

	if len(ops) != 2 {
		t.Fatalf("ListOperations: got %d ops, want 2", len(ops))
	}
	if ops[0].Name != "list_channels" {
		t.Errorf("ops[0].Name: got %q, want %q", ops[0].Name, "list_channels")
	}
	if ops[1].Name != "send_message" {
		t.Errorf("ops[1].Name: got %q, want %q", ops[1].Name, "send_message")
	}
}

func TestListOperationsPreservesOrder(t *testing.T) {
	t.Parallel()

	inner := &stubWithOps{
		StubIntegration: coretesting.StubIntegration{N: "test_provider"},
		ops:             sampleOps(),
	}

	r := coreintegration.NewRestricted(inner, map[string]string{"delete_message": "", "list_channels": ""})
	ops := r.ListOperations()

	if len(ops) != 2 {
		t.Fatalf("ListOperations: got %d ops, want 2", len(ops))
	}
	if ops[0].Name != "list_channels" {
		t.Errorf("ops[0].Name: got %q, want %q", ops[0].Name, "list_channels")
	}
	if ops[1].Name != "delete_message" {
		t.Errorf("ops[1].Name: got %q, want %q", ops[1].Name, "delete_message")
	}
}

func TestExecuteAllowed(t *testing.T) {
	t.Parallel()

	want := &core.OperationResult{Status: 200, Body: "ok"}
	inner := &stubWithOps{
		StubIntegration: coretesting.StubIntegration{
			N: "test_provider",
			ExecuteFn: func(_ context.Context, op string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return want, nil
			},
		},
		ops: sampleOps(),
	}

	r := coreintegration.NewRestricted(inner, map[string]string{"send_message": ""})
	got, err := r.Execute(context.Background(), "send_message", nil, "tok")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got != want {
		t.Errorf("Execute result: got %+v, want %+v", got, want)
	}
}

func TestExecuteDisallowed(t *testing.T) {
	t.Parallel()

	inner := &stubWithOps{
		StubIntegration: coretesting.StubIntegration{N: "test_provider"},
		ops:             sampleOps(),
	}

	r := coreintegration.NewRestricted(inner, map[string]string{"list_channels": ""})
	_, err := r.Execute(context.Background(), "delete_message", nil, "tok")
	if err == nil {
		t.Fatal("Execute: expected error for disallowed op, got nil")
	}
}

func TestRestrictedWithAliases(t *testing.T) {
	t.Parallel()

	var executedOp string
	inner := &stubWithOps{
		StubIntegration: coretesting.StubIntegration{
			N: "test_provider",
			ExecuteFn: func(_ context.Context, op string, _ map[string]any, _ string) (*core.OperationResult, error) {
				executedOp = op
				return &core.OperationResult{Status: 200, Body: "ok"}, nil
			},
		},
		ops: sampleOps(),
	}

	r := coreintegration.NewRestricted(inner, map[string]string{
		"my_channels": "list_channels",
		"send":        "send_message",
	})

	ops := r.ListOperations()
	if len(ops) != 2 {
		t.Fatalf("ListOperations: got %d ops, want 2", len(ops))
	}

	names := make(map[string]bool)
	for _, op := range ops {
		names[op.Name] = true
	}
	if !names["my_channels"] {
		t.Error("expected aliased name my_channels")
	}
	if !names["send"] {
		t.Error("expected aliased name send")
	}

	_, err := r.Execute(context.Background(), "my_channels", nil, "tok")
	if err != nil {
		t.Fatalf("Execute(my_channels): %v", err)
	}
	if executedOp != "list_channels" {
		t.Errorf("inner received op %q, want list_channels", executedOp)
	}

	_, err = r.Execute(context.Background(), "list_channels", nil, "tok")
	if err == nil {
		t.Fatal("expected error when calling by original name after aliasing")
	}
}

func TestDelegationMethods(t *testing.T) {
	t.Parallel()

	exchangeResp := &core.TokenResponse{AccessToken: "abc"}
	inner := &stubWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:    "my-integration",
			DN:   "My Integration",
			Desc: "A test integration",
			ExchangeCodeFn: func(context.Context, string) (*core.TokenResponse, error) {
				return exchangeResp, nil
			},
		},
		ops: sampleOps(),
	}

	r := coreintegration.NewRestricted(inner, map[string]string{"list_channels": ""})

	if got := r.Name(); got != "my-integration" {
		t.Errorf("Name: got %q, want %q", got, "my-integration")
	}
	if got := r.DisplayName(); got != "My Integration" {
		t.Errorf("DisplayName: got %q, want %q", got, "My Integration")
	}
	if got := r.Description(); got != "A test integration" {
		t.Errorf("Description: got %q, want %q", got, "A test integration")
	}

	oauthR, ok := r.(core.OAuthProvider)
	if !ok {
		t.Fatal("expected restricted wrapper to implement OAuthProvider")
	}

	if got := oauthR.AuthorizationURL("state", []string{"read"}); got != "" {
		t.Errorf("AuthorizationURL: got %q, want empty", got)
	}

	tok, err := oauthR.ExchangeCode(context.Background(), "code")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if tok != exchangeResp {
		t.Errorf("ExchangeCode: got %+v, want %+v", tok, exchangeResp)
	}

	refreshTok, err := oauthR.RefreshToken(context.Background(), "refresh")
	if err != nil {
		t.Fatalf("RefreshToken: %v", err)
	}
	if refreshTok != nil {
		t.Errorf("RefreshToken: got %+v, want nil", refreshTok)
	}
}

type stubCatalogProvider struct {
	stubWithOps
	cat *catalog.Catalog
}

func (s *stubCatalogProvider) Catalog() *catalog.Catalog { return s.cat }

func TestCatalogFiltersOperations(t *testing.T) {
	t.Parallel()

	inner := &stubCatalogProvider{
		stubWithOps: stubWithOps{
			StubIntegration: coretesting.StubIntegration{N: "test_provider"},
			ops: []core.Operation{
				{Name: "op_alpha"},
				{Name: "op_beta"},
				{Name: "op_gamma"},
			},
		},
		cat: &catalog.Catalog{
			Name: "test_provider",
			Operations: []catalog.CatalogOperation{
				{ID: "op_alpha", Title: "Alpha"},
				{ID: "op_beta", Title: "Beta"},
				{ID: "op_gamma", Title: "Gamma"},
			},
		},
	}

	r := coreintegration.NewRestricted(inner, map[string]string{
		"op_alpha": "",
		"op_gamma": "",
	})

	cp, ok := r.(core.CatalogProvider)
	if !ok {
		t.Fatal("restricted provider does not implement CatalogProvider")
	}
	cat := cp.Catalog()
	if cat == nil {
		t.Fatal("Catalog() returned nil")
	}
	if len(cat.Operations) != 2 {
		t.Fatalf("Catalog().Operations: got %d, want 2", len(cat.Operations))
	}

	ids := make(map[string]bool, len(cat.Operations))
	for _, op := range cat.Operations {
		ids[op.ID] = true
	}
	if !ids["op_alpha"] {
		t.Error("expected op_alpha in catalog")
	}
	if !ids["op_gamma"] {
		t.Error("expected op_gamma in catalog")
	}
	if ids["op_beta"] {
		t.Error("op_beta should not appear in filtered catalog")
	}
}

func TestCatalogRenamesAliasedOperations(t *testing.T) {
	t.Parallel()

	inner := &stubCatalogProvider{
		stubWithOps: stubWithOps{
			StubIntegration: coretesting.StubIntegration{N: "test_provider"},
			ops: []core.Operation{
				{Name: "op_alpha"},
				{Name: "op_beta"},
			},
		},
		cat: &catalog.Catalog{
			Name: "test_provider",
			Operations: []catalog.CatalogOperation{
				{ID: "op_alpha", Title: "Alpha"},
				{ID: "op_beta", Title: "Beta"},
			},
		},
	}

	r := coreintegration.NewRestricted(inner, map[string]string{
		"renamed_alpha": "op_alpha",
	})

	cp, ok := r.(core.CatalogProvider)
	if !ok {
		t.Fatal("restricted provider does not implement CatalogProvider")
	}
	cat := cp.Catalog()
	if cat == nil {
		t.Fatal("Catalog() returned nil")
	}
	if len(cat.Operations) != 1 {
		t.Fatalf("Catalog().Operations: got %d, want 1", len(cat.Operations))
	}
	if cat.Operations[0].ID != "renamed_alpha" {
		t.Errorf("expected aliased ID %q, got %q", "renamed_alpha", cat.Operations[0].ID)
	}
}
