package invocation_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coreintegration "github.com/valon-technologies/gestalt/server/core/integration"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
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

func testProvider(name string) *stubProviderWithOps {
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

func newTestBroker(t *testing.T, providers ...core.Provider) *invocation.Broker {
	t.Helper()
	return invocation.NewBroker(testutil.NewProviderRegistry(t, providers...), &coretesting.StubDatastore{})
}

func TestAuditedInvoker_Success(t *testing.T) {
	t.Parallel()

	broker := newTestBroker(t, testProvider("alpha"))
	invoker := invocation.NewAuditedInvoker(broker, "test", &capturingSink{})

	result, err := invoker.Invoke(context.Background(), nil, "alpha", "", "ping", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("expected 200, got %d", result.Status)
	}
}

func TestAuditedInvoker_MaxDepthEnforced(t *testing.T) {
	t.Parallel()

	broker := newTestBroker(t, testProvider("alpha"))
	invoker := invocation.NewAuditedInvoker(broker, "test", &capturingSink{})

	ctx := invocation.ContextWithMeta(context.Background(), &invocation.InvocationMeta{RequestID: "test-id", Depth: 5})
	_, err := invoker.Invoke(ctx, nil, "alpha", "", "ping", nil)
	if err == nil {
		t.Fatal("expected max depth error")
	}
	var maxDepthErr *invocation.MaxDepthError
	if !errors.As(err, &maxDepthErr) {
		t.Fatalf("expected MaxDepthError, got %T: %v", err, err)
	}
	if maxDepthErr.Depth != 5 || maxDepthErr.Max != 5 {
		t.Fatalf("expected depth=5 max=5, got depth=%d max=%d", maxDepthErr.Depth, maxDepthErr.Max)
	}
}

func TestAuditedInvoker_RecursionDetected(t *testing.T) {
	t.Parallel()

	broker := newTestBroker(t, testProvider("alpha"))
	invoker := invocation.NewAuditedInvoker(broker, "test", &capturingSink{})

	ctx := invocation.ContextWithMeta(context.Background(), &invocation.InvocationMeta{
		RequestID: "test-id",
		Depth:     1,
		CallChain: []string{"alpha/default/ping"},
	})
	_, err := invoker.Invoke(ctx, nil, "alpha", "", "ping", nil)
	if err == nil {
		t.Fatal("expected recursion error")
	}
	var recursionErr *invocation.RecursionError
	if !errors.As(err, &recursionErr) {
		t.Fatalf("expected RecursionError, got %T: %v", err, err)
	}
}

func TestAuditedInvoker_AuditLogged(t *testing.T) {
	t.Parallel()

	sink := &capturingSink{}
	broker := newTestBroker(t, testProvider("alpha"))
	invoker := invocation.NewAuditedInvoker(broker, "binding:my-hook", sink)

	_, _ = invoker.Invoke(context.Background(), nil, "alpha", "", "ping", nil)
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
}

func TestAuditedInvoker_DeniedCallAudited(t *testing.T) {
	t.Parallel()

	sink := &capturingSink{}
	broker := newTestBroker(t, testProvider("alpha"))
	invoker := invocation.NewAuditedInvoker(broker, "test", sink)

	ctx := invocation.ContextWithMeta(context.Background(), &invocation.InvocationMeta{RequestID: "test-id", Depth: 5})
	_, _ = invoker.Invoke(ctx, nil, "alpha", "", "ping", nil)
	if len(sink.entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(sink.entries))
	}
	denied := sink.entries[0]
	if denied.Allowed {
		t.Error("expected denied call to be logged with Allowed=false")
	}
	if denied.Error == "" {
		t.Error("expected denied call to include an error")
	}
}

func TestAuditedInvoker_ListCapabilities(t *testing.T) {
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

	broker := newTestBroker(t, alpha, beta)
	invoker := invocation.NewAuditedInvoker(broker, "test", &capturingSink{})

	caps := invoker.ListCapabilities()
	if len(caps) != 3 {
		t.Fatalf("expected 3 capabilities, got %d", len(caps))
	}
}
