package broker_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/valon-technologies/toolshed/core"
	coretesting "github.com/valon-technologies/toolshed/core/testing"
	"github.com/valon-technologies/toolshed/internal/broker"
)

type capturingSink struct {
	entries []core.AuditEntry
}

func (s *capturingSink) Log(e core.AuditEntry) {
	s.entries = append(s.entries, e)
}

func echoProvider(name string) *stubProviderWithOps {
	return &stubProviderWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        name,
			ConnMode: core.ConnectionModeNone,
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
			},
		},
		ops: []core.Operation{{Name: "ping", Method: "GET"}},
	}
}

func TestGuardedBroker_AllowedProvider(t *testing.T) {
	t.Parallel()

	b := newBrokerWithProviders(t, echoProvider("alpha"))
	g := broker.NewGuarded(b, "test", &capturingSink{}, broker.WithAllowedProviders([]string{"alpha"}), broker.WithoutRateLimit())

	result, err := g.Invoke(context.Background(), core.InvocationRequest{
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

func TestGuardedBroker_DisallowedProvider(t *testing.T) {
	t.Parallel()

	b := newBrokerWithProviders(t, echoProvider("alpha"))
	g := broker.NewGuarded(b, "test", &capturingSink{}, broker.WithAllowedProviders([]string{"beta"}), broker.WithoutRateLimit())

	_, err := g.Invoke(context.Background(), core.InvocationRequest{
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

func TestGuardedBroker_NoAllowlist(t *testing.T) {
	t.Parallel()

	b := newBrokerWithProviders(t, echoProvider("alpha"), echoProvider("beta"))
	g := broker.NewGuarded(b, "test", &capturingSink{}, broker.WithoutRateLimit())

	for _, prov := range []string{"alpha", "beta"} {
		result, err := g.Invoke(context.Background(), core.InvocationRequest{
			Provider:  prov,
			Operation: "ping",
		})
		if err != nil {
			t.Fatalf("Invoke %s: %v", prov, err)
		}
		if result.Status != http.StatusOK {
			t.Fatalf("expected 200, got %d", result.Status)
		}
	}
}

func TestGuardedBroker_MaxDepthEnforced(t *testing.T) {
	t.Parallel()

	b := newBrokerWithProviders(t, echoProvider("alpha"))
	g := broker.NewGuarded(b, "test", &capturingSink{}, broker.WithMaxDepth(2), broker.WithoutRateLimit())

	// Simulate a context already at depth 2
	meta := &broker.InvocationMeta{RequestID: "test-id", Depth: 2}
	ctx := broker.ContextWithMeta(context.Background(), meta)

	_, err := g.Invoke(ctx, core.InvocationRequest{
		Provider:  "alpha",
		Operation: "ping",
	})
	if err == nil {
		t.Fatal("expected max depth error")
	}
	var mde *broker.MaxDepthError
	if !errors.As(err, &mde) {
		t.Fatalf("expected MaxDepthError, got %T: %v", err, err)
	}
	if mde.Depth != 2 || mde.Max != 2 {
		t.Fatalf("expected depth=2 max=2, got depth=%d max=%d", mde.Depth, mde.Max)
	}
}

func TestGuardedBroker_RecursionDetected(t *testing.T) {
	t.Parallel()

	b := newBrokerWithProviders(t, echoProvider("alpha"))
	g := broker.NewGuarded(b, "test", &capturingSink{}, broker.WithoutRateLimit())

	meta := &broker.InvocationMeta{
		RequestID: "test-id",
		Depth:     1,
		CallChain: []string{"alpha/ping"},
	}
	ctx := broker.ContextWithMeta(context.Background(), meta)

	_, err := g.Invoke(ctx, core.InvocationRequest{
		Provider:  "alpha",
		Operation: "ping",
	})
	if err == nil {
		t.Fatal("expected recursion error")
	}
	var re *broker.RecursionError
	if !errors.As(err, &re) {
		t.Fatalf("expected RecursionError, got %T: %v", err, err)
	}
}

func TestGuardedBroker_RateLimitExceeded(t *testing.T) {
	t.Parallel()

	b := newBrokerWithProviders(t, echoProvider("alpha"))
	g := broker.NewGuarded(b, "test", &capturingSink{}, broker.WithRateLimit(1, 1))

	// First call should succeed (uses the burst)
	_, err := g.Invoke(context.Background(), core.InvocationRequest{
		Provider:  "alpha",
		Operation: "ping",
	})
	if err != nil {
		t.Fatalf("first Invoke: %v", err)
	}

	// Rapid subsequent calls should hit the limit
	var rateLimited bool
	for i := 0; i < 10; i++ {
		_, err = g.Invoke(context.Background(), core.InvocationRequest{
			Provider:  "alpha",
			Operation: "ping",
		})
		if err != nil {
			var rle *broker.RateLimitError
			if errors.As(err, &rle) {
				rateLimited = true
				break
			}
			t.Fatalf("unexpected error type: %T: %v", err, err)
		}
	}
	if !rateLimited {
		t.Fatal("expected rate limit error after burst")
	}
}

func TestGuardedBroker_AuditLogged(t *testing.T) {
	t.Parallel()

	sink := &capturingSink{}
	b := newBrokerWithProviders(t, echoProvider("alpha"))
	g := broker.NewGuarded(b, "binding:my-hook", sink, broker.WithAllowedProviders([]string{"alpha"}), broker.WithoutRateLimit())

	_, _ = g.Invoke(context.Background(), core.InvocationRequest{
		Provider:  "alpha",
		Operation: "ping",
	})

	if len(sink.entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(sink.entries))
	}
	e := sink.entries[0]
	if e.Source != "binding:my-hook" {
		t.Errorf("expected source binding:my-hook, got %q", e.Source)
	}
	if e.Provider != "alpha" {
		t.Errorf("expected provider alpha, got %q", e.Provider)
	}
	if e.Operation != "ping" {
		t.Errorf("expected operation ping, got %q", e.Operation)
	}
	if !e.Allowed {
		t.Error("expected Allowed=true")
	}
	if e.RequestID == "" {
		t.Error("expected non-empty RequestID")
	}

	// Denied call should also be logged
	_, _ = g.Invoke(context.Background(), core.InvocationRequest{
		Provider:  "beta",
		Operation: "ping",
	})
	if len(sink.entries) != 2 {
		t.Fatalf("expected 2 audit entries, got %d", len(sink.entries))
	}
	denied := sink.entries[1]
	if denied.Allowed {
		t.Error("expected Allowed=false for denied call")
	}
	if denied.Error == "" {
		t.Error("expected non-empty Error for denied call")
	}
}

func TestGuardedBroker_ListCapabilities(t *testing.T) {
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

	t.Run("with allowlist", func(t *testing.T) {
		t.Parallel()
		g := broker.NewGuarded(b, "test", &capturingSink{}, broker.WithAllowedProviders([]string{"alpha"}))
		caps := g.ListCapabilities()
		if len(caps) != 2 {
			t.Fatalf("expected 2 capabilities, got %d", len(caps))
		}
		for _, cap := range caps {
			if cap.Provider != "alpha" {
				t.Fatalf("expected provider alpha, got %q", cap.Provider)
			}
		}
	})

	t.Run("without allowlist", func(t *testing.T) {
		t.Parallel()
		g := broker.NewGuarded(b, "test", &capturingSink{})
		caps := g.ListCapabilities()
		if len(caps) != 3 {
			t.Fatalf("expected 3 capabilities, got %d", len(caps))
		}
	})
}
