package invocation_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coreintegration "github.com/valon-technologies/gestalt/server/core/integration"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

type stubProviderWithOps struct {
	coretesting.StubIntegration
	ops []core.Operation
}

func (s *stubProviderWithOps) Catalog() *catalog.Catalog {
	cat := &catalog.Catalog{
		Name:       s.N,
		Operations: make([]catalog.CatalogOperation, 0, len(s.ops)),
	}
	for _, op := range s.ops {
		params := make([]catalog.CatalogParameter, 0, len(op.Parameters))
		for _, param := range op.Parameters {
			params = append(params, catalog.CatalogParameter{
				Name:        param.Name,
				Type:        param.Type,
				Description: param.Description,
				Required:    param.Required,
				Default:     param.Default,
			})
		}
		cat.Operations = append(cat.Operations, catalog.CatalogOperation{
			ID:          op.Name,
			Method:      op.Method,
			Path:        "/" + op.Name,
			Description: op.Description,
			Parameters:  params,
		})
	}
	coreintegration.CompileSchemas(cat)
	return cat
}

type capturingSink struct {
	entries []core.AuditEntry
}

func (s *capturingSink) Log(_ context.Context, entry core.AuditEntry) {
	s.entries = append(s.entries, entry)
}

type funcInvoker struct {
	invoke func(ctx context.Context, p *principal.Principal, providerName, instance, operation string, params map[string]any) (*core.OperationResult, error)
}

func (f funcInvoker) Invoke(ctx context.Context, p *principal.Principal, providerName, instance, operation string, params map[string]any) (*core.OperationResult, error) {
	return f.invoke(ctx, p, providerName, instance, operation, params)
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
	return invocation.NewBroker(testutil.NewProviderRegistry(t, providers...), nil, nil)
}

func TestGuardedInvoker_AllowedProvider(t *testing.T) {
	t.Parallel()

	base := newGuardedTestInvoker(t, guardTestProvider("alpha"))
	guarded := invocation.NewGuarded(base, base, "test", &capturingSink{}, invocation.WithAllowedProviders([]string{"alpha"}), invocation.WithoutRateLimit())

	result, err := guarded.Invoke(context.Background(), nil, "alpha", "", "ping", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("expected 200, got %d", result.Status)
	}
}

func TestGuardedInvoker_DisallowedProvider(t *testing.T) {
	t.Parallel()

	base := newGuardedTestInvoker(t, guardTestProvider("alpha"))
	guarded := invocation.NewGuarded(base, base, "test", &capturingSink{}, invocation.WithAllowedProviders([]string{"beta"}), invocation.WithoutRateLimit())

	_, err := guarded.Invoke(context.Background(), nil, "alpha", "", "ping", nil)
	if err == nil {
		t.Fatal("expected error for disallowed provider")
	}
	if !strings.Contains(err.Error(), "not available in this scope") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGuardedInvoker_NoAllowlist(t *testing.T) {
	t.Parallel()

	base := newGuardedTestInvoker(t, guardTestProvider("alpha"), guardTestProvider("beta"))
	guarded := invocation.NewGuarded(base, base, "test", &capturingSink{}, invocation.WithoutRateLimit())

	for _, provider := range []string{"alpha", "beta"} {
		result, err := guarded.Invoke(context.Background(), nil, provider, "", "ping", nil)
		if err != nil {
			t.Fatalf("Invoke %s: %v", provider, err)
		}
		if result.Status != http.StatusOK {
			t.Fatalf("expected 200, got %d", result.Status)
		}
	}
}

func TestGuardedInvoker_MaxDepthEnforced(t *testing.T) {
	t.Parallel()

	base := newGuardedTestInvoker(t, guardTestProvider("alpha"))
	guarded := invocation.NewGuarded(base, base, "test", &capturingSink{}, invocation.WithMaxDepth(2), invocation.WithoutRateLimit())

	ctx := invocation.ContextWithMeta(context.Background(), &invocation.InvocationMeta{RequestID: "test-id", Depth: 2})
	_, err := guarded.Invoke(ctx, nil, "alpha", "", "ping", nil)
	if err == nil {
		t.Fatal("expected max depth error")
	}
	var maxDepthErr *invocation.MaxDepthError
	if !errors.As(err, &maxDepthErr) {
		t.Fatalf("expected MaxDepthError, got %T: %v", err, err)
	}
	if maxDepthErr.Depth != 2 || maxDepthErr.Max != 2 {
		t.Fatalf("expected depth=2 max=2, got depth=%d max=%d", maxDepthErr.Depth, maxDepthErr.Max)
	}
}

func TestGuardedInvoker_RecursionDetected(t *testing.T) {
	t.Parallel()

	base := newGuardedTestInvoker(t, guardTestProvider("alpha"))
	guarded := invocation.NewGuarded(base, base, "test", &capturingSink{}, invocation.WithoutRateLimit())

	ctx := invocation.ContextWithMeta(context.Background(), &invocation.InvocationMeta{
		RequestID: "test-id",
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
}

func TestGuardedInvoker_RateLimitExceeded(t *testing.T) {
	t.Parallel()

	base := newGuardedTestInvoker(t, guardTestProvider("alpha"))
	guarded := invocation.NewGuarded(base, base, "test", &capturingSink{}, invocation.WithRateLimit(1, 1))

	if _, err := guarded.Invoke(context.Background(), nil, "alpha", "", "ping", nil); err != nil {
		t.Fatalf("first Invoke: %v", err)
	}

	var rateLimited bool
	for i := 0; i < 10; i++ {
		if _, err := guarded.Invoke(context.Background(), nil, "alpha", "", "ping", nil); err != nil {
			var rateLimitErr *invocation.RateLimitError
			if errors.As(err, &rateLimitErr) {
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

func TestGuardedInvoker_AuditLogged(t *testing.T) {
	t.Parallel()

	sink := &capturingSink{}
	base := newGuardedTestInvoker(t, guardTestProvider("alpha"))
	guarded := invocation.NewGuarded(base, base, "binding:my-hook", sink, invocation.WithAllowedProviders([]string{"alpha"}), invocation.WithoutRateLimit())

	_, _ = guarded.Invoke(context.Background(), nil, "alpha", "", "ping", nil)
	if len(sink.entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(sink.entries))
	}
	entry := sink.entries[0]
	if entry.Source != "binding:my-hook" {
		t.Errorf("expected source binding:my-hook, got %q", entry.Source)
	}
	if entry.Provider != "alpha" {
		t.Errorf("expected provider alpha, got %q", entry.Provider)
	}
	if entry.Operation != "ping" {
		t.Errorf("expected operation ping, got %q", entry.Operation)
	}
	if !entry.Allowed {
		t.Error("expected Allowed=true")
	}
	if entry.RequestID == "" {
		t.Error("expected non-empty RequestID")
	}

	_, _ = guarded.Invoke(context.Background(), nil, "beta", "", "ping", nil)
	if len(sink.entries) != 2 {
		t.Fatalf("expected 2 audit entries, got %d", len(sink.entries))
	}
	denied := sink.entries[1]
	if denied.Allowed {
		t.Error("expected denied call to be logged with Allowed=false")
	}
	if denied.Error == "" {
		t.Error("expected denied call to include an error")
	}
}

func TestGuardedInvoker_AuditLoggedOnPanic(t *testing.T) {
	t.Parallel()

	sink := &capturingSink{}
	guarded := invocation.NewGuarded(funcInvoker{
		invoke: func(ctx context.Context, _ *principal.Principal, _ string, _ string, _ string, _ map[string]any) (*core.OperationResult, error) {
			invocation.SetCredentialAudit(ctx, core.ConnectionModeUser, "system:config", "workspace", "team-a")
			panic("boom")
		},
	}, nil, "test", sink, invocation.WithoutRateLimit())

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic")
		}
		if r != "boom" {
			t.Fatalf("unexpected panic: %v", r)
		}
		if len(sink.entries) != 1 {
			t.Fatalf("expected 1 audit entry, got %d", len(sink.entries))
		}
		entry := sink.entries[0]
		if !entry.Allowed {
			t.Fatal("expected panicing invocation to keep allowed=true audit entry")
		}
		if entry.CredentialMode != string(core.ConnectionModeUser) {
			t.Fatalf("expected credential mode identity, got %q", entry.CredentialMode)
		}
		if entry.CredentialSubjectID != "system:config" {
			t.Fatalf("expected credential subject %q, got %q", "system:config", entry.CredentialSubjectID)
		}
		if entry.CredentialConnection != "workspace" {
			t.Fatalf("expected credential connection workspace, got %q", entry.CredentialConnection)
		}
		if entry.CredentialInstance != "team-a" {
			t.Fatalf("expected credential instance team-a, got %q", entry.CredentialInstance)
		}
	}()

	_, _ = guarded.Invoke(context.Background(), nil, "alpha", "", "ping", nil)
}

func TestGuardedInvoker_ListCapabilities(t *testing.T) {
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

	t.Run("with allowlist", func(t *testing.T) {
		t.Parallel()
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
	})

	t.Run("without allowlist", func(t *testing.T) {
		t.Parallel()
		guarded := invocation.NewGuarded(base, base, "test", &capturingSink{})
		caps := guarded.ListCapabilities()
		if len(caps) != 3 {
			t.Fatalf("expected 3 capabilities, got %d", len(caps))
		}
	})
}
