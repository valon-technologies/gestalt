package invocation_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/core"
	coretesting "github.com/valon-technologies/gestalt/core/testing"
	"github.com/valon-technologies/gestalt/internal/invocation"
	"github.com/valon-technologies/gestalt/internal/testutil"
)

type stubProviderWithOps struct {
	coretesting.StubIntegration
	ops []core.Operation
}

func (s *stubProviderWithOps) ListOperations() []core.Operation {
	return s.ops
}

type capturingSink struct {
	entries []core.AuditEntry
}

func (s *capturingSink) Log(entry core.AuditEntry) {
	s.entries = append(s.entries, entry)
}

func guardTestProvider(name string) *stubProviderWithOps {
	return &stubProviderWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        name,
			ConnMode: core.ConnectionModeNone,
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
			},
		},
		ops: []core.Operation{{Name: "ping", Method: http.MethodGet}},
	}
}

func newGuardedTestInvoker(t *testing.T, providers ...core.Provider) *invocation.Broker {
	t.Helper()
	return invocation.NewBroker(testutil.NewProviderRegistry(t, providers...), &coretesting.StubDatastore{})
}

func TestGuardedInvoker_EnforcesProviderScopeAndAudit(t *testing.T) {
	t.Parallel()

	sink := &capturingSink{}
	base := newGuardedTestInvoker(t, guardTestProvider("alpha"))
	guarded := invocation.NewGuarded(base, base, "binding:test", sink, invocation.WithAllowedProviders([]string{"alpha"}), invocation.WithoutRateLimit())

	result, err := guarded.Invoke(context.Background(), nil, "alpha", "", "ping", nil)
	if err != nil {
		t.Fatalf("allowed Invoke: %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("expected 200, got %d", result.Status)
	}

	_, err = guarded.Invoke(context.Background(), nil, "beta", "", "ping", nil)
	if err == nil {
		t.Fatal("expected error for disallowed provider")
	}
	if !strings.Contains(err.Error(), "not available in this scope") {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sink.entries) != 2 {
		t.Fatalf("expected 2 audit entries, got %d", len(sink.entries))
	}
	if !sink.entries[0].Allowed || sink.entries[0].Provider != "alpha" || sink.entries[0].Operation != "ping" {
		t.Fatalf("unexpected allowed audit entry: %+v", sink.entries[0])
	}
	if sink.entries[1].Allowed || sink.entries[1].Provider != "beta" || sink.entries[1].Error == "" {
		t.Fatalf("unexpected denied audit entry: %+v", sink.entries[1])
	}
}

func TestGuardedInvoker_ProtectsInvocationChain(t *testing.T) {
	t.Parallel()

	t.Run("max depth", func(t *testing.T) {
		t.Parallel()

		base := newGuardedTestInvoker(t, guardTestProvider("alpha"))
		guarded := invocation.NewGuarded(base, base, "test", &capturingSink{}, invocation.WithMaxDepth(2), invocation.WithoutRateLimit())

		ctx := invocation.ContextWithMeta(context.Background(), &invocation.InvocationMeta{RequestID: "depth-test", Depth: 2})
		_, err := guarded.Invoke(ctx, nil, "alpha", "", "ping", nil)
		if err == nil {
			t.Fatal("expected max depth error")
		}
		var maxDepthErr *invocation.MaxDepthError
		if !errors.As(err, &maxDepthErr) {
			t.Fatalf("expected MaxDepthError, got %T: %v", err, err)
		}
	})

	t.Run("recursion", func(t *testing.T) {
		t.Parallel()

		base := newGuardedTestInvoker(t, guardTestProvider("alpha"))
		guarded := invocation.NewGuarded(base, base, "test", &capturingSink{}, invocation.WithoutRateLimit())

		ctx := invocation.ContextWithMeta(context.Background(), &invocation.InvocationMeta{
			RequestID: "recur-test",
			Depth:     1,
			CallChain: []string{"alpha/default/ping"},
		})
		_, err := guarded.Invoke(ctx, nil, "alpha", "", "ping", nil)
		if err == nil {
			t.Fatal("expected recursion error")
		}
		var recursionErr *invocation.RecursionError
		if !errors.As(err, &recursionErr) {
			t.Fatalf("expected RecursionError, got %T: %v", err, err)
		}
	})

	t.Run("rate limit", func(t *testing.T) {
		t.Parallel()

		base := newGuardedTestInvoker(t, guardTestProvider("alpha"))
		guarded := invocation.NewGuarded(base, base, "test", &capturingSink{}, invocation.WithRateLimit(1, 1))

		if _, err := guarded.Invoke(context.Background(), nil, "alpha", "", "ping", nil); err != nil {
			t.Fatalf("first Invoke: %v", err)
		}

		var rateLimitErr *invocation.RateLimitError
		for i := 0; i < 10; i++ {
			if _, err := guarded.Invoke(context.Background(), nil, "alpha", "", "ping", nil); err != nil {
				if errors.As(err, &rateLimitErr) {
					return
				}
				t.Fatalf("unexpected error type: %T: %v", err, err)
			}
		}
		t.Fatal("expected rate limit error after burst")
	})
}

func TestGuardedInvoker_ListCapabilities_FiltersAllowlist(t *testing.T) {
	t.Parallel()

	alpha := &stubProviderWithOps{
		StubIntegration: coretesting.StubIntegration{N: "alpha"},
		ops: []core.Operation{
			{Name: "op1", Description: "Alpha op 1", Method: http.MethodGet},
			{Name: "op2", Description: "Alpha op 2", Method: http.MethodPost},
		},
	}
	beta := &stubProviderWithOps{
		StubIntegration: coretesting.StubIntegration{N: "beta"},
		ops: []core.Operation{
			{Name: "op3", Description: "Beta op 3", Method: http.MethodGet},
		},
	}

	base := newGuardedTestInvoker(t, alpha, beta)
	guarded := invocation.NewGuarded(base, base, "test", &capturingSink{}, invocation.WithAllowedProviders([]string{"alpha"}))

	caps := guarded.ListCapabilities()
	if len(caps) != 2 {
		t.Fatalf("expected 2 capabilities, got %d", len(caps))
	}
	for _, cap := range caps {
		if cap.Provider != "alpha" {
			t.Fatalf("expected provider alpha, got %q", cap.Provider)
		}
	}
}
