package integration_test

import (
	"context"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coreintegration "github.com/valon-technologies/gestalt/server/core/integration"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
)

// stubWithOps extends StubIntegration with concrete operations.
type stubWithOps struct {
	coretesting.StubIntegration
	ops []core.Operation
}

func (s *stubWithOps) Catalog() *catalog.Catalog {
	return restrictedTestCatalog(s.N, s.ops)
}

func sampleOps() []core.Operation {
	return []core.Operation{
		{Name: "list_channels", Description: "List channels"},
		{Name: "send_message", Description: "Send a message"},
		{Name: "delete_message", Description: "Delete a message"},
	}
}

func restrictedTestCatalog(name string, ops []core.Operation) *catalog.Catalog {
	cat := &catalog.Catalog{
		Name:       name,
		Operations: make([]catalog.CatalogOperation, 0, len(ops)),
	}
	for _, op := range ops {
		cat.Operations = append(cat.Operations, catalog.CatalogOperation{
			ID:          op.Name,
			Method:      op.Method,
			Path:        "/" + op.Name,
			Description: op.Description,
		})
	}
	coreintegration.CompileSchemas(cat)
	return cat
}

func TestCatalogFilters(t *testing.T) {
	t.Parallel()

	inner := &stubWithOps{
		StubIntegration: coretesting.StubIntegration{N: "test_provider"},
		ops:             sampleOps(),
	}

	r := coreintegration.NewRestricted(inner, map[string]string{"list_channels": "", "send_message": ""})

	cat := r.Catalog()
	if cat == nil {
		t.Fatal("Catalog() returned nil")
		return
	}
	if len(cat.Operations) != 2 {
		t.Fatalf("Catalog().Operations: got %d, want 2", len(cat.Operations))
	}
	catIDs := make(map[string]bool, len(cat.Operations))
	for _, op := range cat.Operations {
		catIDs[op.ID] = true
	}
	if !catIDs["list_channels"] {
		t.Error("expected list_channels in Catalog().Operations")
	}
	if !catIDs["send_message"] {
		t.Error("expected send_message in Catalog().Operations")
	}

	ops := coreintegration.OperationsList(cat)
	if len(ops) != 2 {
		t.Fatalf("OperationsList: got %d ops, want 2", len(ops))
	}
	if ops[0].Name != "list_channels" {
		t.Errorf("ops[0].Name: got %q, want %q", ops[0].Name, "list_channels")
	}
	if ops[1].Name != "send_message" {
		t.Errorf("ops[1].Name: got %q, want %q", ops[1].Name, "send_message")
	}
}

func TestCatalogFiltersCanOverrideAllowedRoles(t *testing.T) {
	t.Parallel()

	inner := &stubWithOps{
		StubIntegration: coretesting.StubIntegration{N: "test_provider"},
		ops:             sampleOps(),
	}

	r := coreintegration.NewRestricted(
		inner,
		map[string]string{"list_channels": "", "send_message": ""},
		coreintegration.WithAllowedRoles(map[string][]string{
			"list_channels": {"admin"},
		}),
	)

	cat := r.Catalog()
	if cat == nil {
		t.Fatal("Catalog() returned nil")
	}
	if len(cat.Operations) != 2 {
		t.Fatalf("Catalog().Operations: got %d, want 2", len(cat.Operations))
	}
	if got := cat.Operations[0].AllowedRoles; len(got) != 1 || got[0] != "admin" {
		t.Fatalf("list_channels AllowedRoles = %#v, want [admin]", got)
	}
	if got := cat.Operations[1].AllowedRoles; len(got) != 0 {
		t.Fatalf("send_message AllowedRoles = %#v, want empty", got)
	}
}

func TestCatalogPreservesOrder(t *testing.T) {
	t.Parallel()

	inner := &stubWithOps{
		StubIntegration: coretesting.StubIntegration{N: "test_provider"},
		ops:             sampleOps(),
	}

	r := coreintegration.NewRestricted(inner, map[string]string{"delete_message": "", "list_channels": ""})
	ops := coreintegration.OperationsList(r.Catalog())

	if len(ops) != 2 {
		t.Fatalf("OperationsList: got %d ops, want 2", len(ops))
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

	ops := coreintegration.OperationsList(r.Catalog())
	if len(ops) != 2 {
		t.Fatalf("OperationsList: got %d ops, want 2", len(ops))
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
	refreshResp := &core.TokenResponse{AccessToken: "refresh-abc"}
	inner := &stubOAuth{
		stubWithOps: stubWithOps{
			StubIntegration: coretesting.StubIntegration{
				N:    "my-integration",
				DN:   "My Integration",
				Desc: "A test integration",
				ExchangeCodeFn: func(context.Context, string) (*core.TokenResponse, error) {
					return exchangeResp, nil
				},
			},
			ops: sampleOps(),
		},
	}
	inner.refreshTokenFn = func(context.Context, string) (*core.TokenResponse, error) {
		return refreshResp, nil
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
		t.Fatal("expected restricted wrapper to implement core OAuth provider")
	}

	if got := oauthR.AuthorizationURL("state", []string{"read"}); got != "https://example.com/start?state=state" {
		t.Errorf("AuthorizationURL: got %q", got)
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
	if refreshTok != refreshResp {
		t.Errorf("RefreshToken: got %+v, want %+v", refreshTok, refreshResp)
	}
}

type stubCatalogProvider struct {
	stubWithOps
	cat *catalog.Catalog
}

func (s *stubCatalogProvider) Catalog() *catalog.Catalog { return s.cat }

type stubOAuth struct {
	stubWithOps
	refreshTokenFn func(context.Context, string) (*core.TokenResponse, error)
}

func (s *stubOAuth) AuthorizationURL(state string, _ []string) string {
	return "https://example.com/start?state=" + state
}

func (s *stubOAuth) RefreshToken(ctx context.Context, refreshToken string) (*core.TokenResponse, error) {
	if s.refreshTokenFn != nil {
		return s.refreshTokenFn(ctx, refreshToken)
	}
	return nil, nil
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

	cat := r.Catalog()
	if cat == nil {
		t.Fatal("Catalog() returned nil")
		return
	}
	if len(cat.Operations) != 1 {
		t.Fatalf("Catalog().Operations: got %d, want 1", len(cat.Operations))
	}
	if cat.Operations[0].ID != "renamed_alpha" {
		t.Errorf("expected aliased ID %q, got %q", "renamed_alpha", cat.Operations[0].ID)
	}
}
