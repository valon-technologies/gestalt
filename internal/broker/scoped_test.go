package broker_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/valon-technologies/toolshed/core"
	coretesting "github.com/valon-technologies/toolshed/core/testing"
	"github.com/valon-technologies/toolshed/internal/broker"
	"github.com/valon-technologies/toolshed/internal/registry"
)

type stubProviderWithOps struct {
	coretesting.StubIntegration
	ops []core.Operation
}

func (s *stubProviderWithOps) ListOperations() []core.Operation { return s.ops }

func newBrokerWithProviders(t *testing.T, providers ...core.Provider) *broker.Broker {
	t.Helper()
	reg := registry.New()
	for _, p := range providers {
		if err := reg.Providers.Register(p.Name(), p); err != nil {
			t.Fatalf("registering provider: %v", err)
		}
	}
	ds := &coretesting.StubDatastore{}
	return broker.New(&reg.Providers, ds)
}

func TestScopedBroker_InvokeAllowed(t *testing.T) {
	t.Parallel()

	prov := &stubProviderWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "alpha",
			ConnMode: core.ConnectionModeNone,
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
			},
		},
		ops: []core.Operation{{Name: "ping", Method: "GET"}},
	}

	b := newBrokerWithProviders(t, prov)
	scoped := broker.NewScoped(b, []string{"alpha"})

	result, err := scoped.Invoke(context.Background(), core.InvocationRequest{
		Provider:  "alpha",
		Operation: "ping",
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("expected 200, got %d", result.Status)
	}
}

func TestScopedBroker_InvokeDisallowed(t *testing.T) {
	t.Parallel()

	prov := &stubProviderWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "alpha",
			ConnMode: core.ConnectionModeNone,
		},
		ops: []core.Operation{{Name: "ping", Method: "GET"}},
	}

	b := newBrokerWithProviders(t, prov)
	scoped := broker.NewScoped(b, []string{"beta"})

	_, err := scoped.Invoke(context.Background(), core.InvocationRequest{
		Provider:  "alpha",
		Operation: "ping",
	})
	if err == nil {
		t.Fatal("expected error for disallowed provider")
	}
	if !strings.Contains(err.Error(), "not available in this scope") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestScopedBroker_ListCapabilities(t *testing.T) {
	t.Parallel()

	alpha := &stubProviderWithOps{
		StubIntegration: coretesting.StubIntegration{N: "alpha"},
		ops: []core.Operation{
			{Name: "op1", Description: "Alpha op 1"},
			{Name: "op2", Description: "Alpha op 2"},
		},
	}
	beta := &stubProviderWithOps{
		StubIntegration: coretesting.StubIntegration{N: "beta"},
		ops: []core.Operation{
			{Name: "op3", Description: "Beta op 3"},
		},
	}

	b := newBrokerWithProviders(t, alpha, beta)
	scoped := broker.NewScoped(b, []string{"alpha"})

	caps := scoped.ListCapabilities()
	if len(caps) != 2 {
		t.Fatalf("expected 2 capabilities, got %d", len(caps))
	}
	for _, cap := range caps {
		if cap.Provider != "alpha" {
			t.Fatalf("expected provider alpha, got %q", cap.Provider)
		}
	}
}
