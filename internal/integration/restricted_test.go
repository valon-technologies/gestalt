package integration_test

import (
	"context"
	"testing"

	"github.com/valon-technologies/gestalt/core"
	coretesting "github.com/valon-technologies/gestalt/core/testing"
	"github.com/valon-technologies/gestalt/internal/integration"
)

// stubWithOps extends StubIntegration with concrete operations.
type stubWithOps struct {
	coretesting.StubIntegration
	ops []core.Operation
}

func (s *stubWithOps) ListOperations() []core.Operation {
	return s.ops
}

type specStubWithOps struct {
	stubWithOps
	spec core.ConnectionSpec
}

func (s *specStubWithOps) ConnectionSpec() core.ConnectionSpec {
	return s.spec
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
		StubIntegration: coretesting.StubIntegration{N: "slack"},
		ops:             sampleOps(),
	}

	r := integration.NewRestricted(inner, []string{"list_channels", "send_message"})
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
		StubIntegration: coretesting.StubIntegration{N: "slack"},
		ops:             sampleOps(),
	}

	// Allowlist in different order from inner; output should follow inner's order.
	r := integration.NewRestricted(inner, []string{"delete_message", "list_channels"})
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
			N: "slack",
			ExecuteFn: func(_ context.Context, op string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return want, nil
			},
		},
		ops: sampleOps(),
	}

	r := integration.NewRestricted(inner, []string{"send_message"})
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
		StubIntegration: coretesting.StubIntegration{N: "slack"},
		ops:             sampleOps(),
	}

	r := integration.NewRestricted(inner, []string{"list_channels"})
	_, err := r.Execute(context.Background(), "delete_message", nil, "tok")
	if err == nil {
		t.Fatal("Execute: expected error for disallowed op, got nil")
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

	r := integration.NewRestricted(inner, []string{"list_channels"})

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

func TestConnectionSpecDelegatesAndClones(t *testing.T) {
	t.Parallel()

	inner := &specStubWithOps{
		stubWithOps: stubWithOps{
			StubIntegration: coretesting.StubIntegration{N: "slack"},
			ops:             sampleOps(),
		},
		spec: core.ConnectionSpec{
			AuthTypes: []string{"oauth", "manual"},
			ConnectionParams: map[string]core.ConnectionParamDef{
				"tenant": {Required: true, Description: "Workspace"},
			},
			Discovery: &core.DiscoveryConfig{
				URL:             "https://example.com/discover",
				MetadataMapping: map[string]string{"team_id": "team.id"},
			},
		},
	}

	prov := integration.NewRestricted(inner, []string{"list_channels"})
	csp, ok := prov.(core.ConnectionSpecProvider)
	if !ok {
		t.Fatal("expected restricted provider to implement ConnectionSpecProvider")
	}

	got := csp.ConnectionSpec()
	if got.Discovery == nil || got.Discovery.URL != "https://example.com/discover" {
		t.Fatalf("ConnectionSpec().Discovery = %+v", got.Discovery)
	}

	got.AuthTypes[0] = "mutated"
	got.ConnectionParams["tenant"] = core.ConnectionParamDef{Description: "mutated"}
	got.Discovery.URL = "https://mutated.invalid"
	got.Discovery.MetadataMapping["team_id"] = "mutated"

	if inner.spec.AuthTypes[0] != "oauth" {
		t.Fatalf("inner auth types were mutated: %+v", inner.spec.AuthTypes)
	}
	if inner.spec.ConnectionParams["tenant"].Description != "Workspace" {
		t.Fatalf("inner connection params were mutated: %+v", inner.spec.ConnectionParams["tenant"])
	}
	if inner.spec.Discovery.URL != "https://example.com/discover" {
		t.Fatalf("inner discovery URL was mutated: %q", inner.spec.Discovery.URL)
	}
	if inner.spec.Discovery.MetadataMapping["team_id"] != "team.id" {
		t.Fatalf("inner discovery metadata was mutated: %+v", inner.spec.Discovery.MetadataMapping)
	}
}
