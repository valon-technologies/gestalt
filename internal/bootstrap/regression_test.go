package bootstrap_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/core"
	coretesting "github.com/valon-technologies/gestalt/core/testing"
	"github.com/valon-technologies/gestalt/internal/bootstrap"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/invocation"
	"github.com/valon-technologies/gestalt/internal/principal"
)

func TestGatewayMode_NoRuntimesOrBindingsRequired(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cfg := validConfig()
	cfg.Runtimes = nil
	cfg.Bindings = nil

	result, err := bootstrap.Bootstrap(ctx, cfg, validFactories())
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if result.Invoker == nil {
		t.Fatal("expected Invoker to be non-nil")
	}
	if result.CapabilityLister == nil {
		t.Fatal("expected CapabilityLister to be non-nil")
	}
	invoker, ok := result.Invoker.(*invocation.Broker)
	if !ok {
		t.Fatalf("expected Invoker to be *invocation.Broker, got %T", result.Invoker)
	}
	lister, ok := result.CapabilityLister.(*invocation.Broker)
	if !ok {
		t.Fatalf("expected CapabilityLister to be *invocation.Broker, got %T", result.CapabilityLister)
	}
	if invoker != lister {
		t.Fatal("expected Invoker and CapabilityLister to reference the same shared instance")
	}
	<-result.ProvidersReady
	names := result.Providers.List()
	if len(names) != 1 || names[0] != "alpha" {
		t.Errorf("Providers.List: got %v, want [alpha]", names)
	}
	if result.Runtimes != nil {
		t.Error("expected Runtimes to be nil")
	}
	if result.Bindings != nil {
		t.Error("expected Bindings to be nil")
	}
}

func TestPlatformMode_BindingsAndRuntimesWithSafetyLayer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cfg := validConfig()
	cfg.Integrations["beta"] = config.IntegrationDef{}

	cfg.Runtimes = map[string]config.RuntimeDef{
		"my-runtime": {
			Type:      "echo",
			Providers: []string{"alpha"},
		},
	}
	cfg.Bindings = map[string]config.BindingDef{
		"my-binding": {
			Type:      "test-binding",
			Providers: []string{"beta"},
		},
	}

	factories := validFactories()
	factories.Providers["alpha"] = func(_ context.Context, _ string, _ config.IntegrationDef, _ bootstrap.Deps) (core.Provider, error) {
		return &stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N:        "alpha",
				ConnMode: core.ConnectionModeNone,
				ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
					return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
				},
			},
			ops: []core.Operation{{Name: "do", Method: http.MethodPost}},
		}, nil
	}
	factories.Providers["beta"] = func(_ context.Context, _ string, _ config.IntegrationDef, _ bootstrap.Deps) (core.Provider, error) {
		return &stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N:        "beta",
				ConnMode: core.ConnectionModeNone,
				ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
					return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
				},
			},
			ops: []core.Operation{{Name: "do", Method: http.MethodPost}},
		}, nil
	}

	var runtimeDeps bootstrap.RuntimeDeps
	factories.Runtimes["echo"] = func(_ context.Context, name string, _ config.RuntimeDef, deps bootstrap.RuntimeDeps) (core.Runtime, error) {
		runtimeDeps = deps
		return &coretesting.StubRuntime{N: name}, nil
	}

	var bindingDeps bootstrap.BindingDeps
	factories.Bindings["test-binding"] = func(_ context.Context, name string, _ config.BindingDef, deps bootstrap.BindingDeps) (core.Binding, error) {
		bindingDeps = deps
		return &coretesting.StubBinding{N: name, K: core.BindingTrigger}, nil
	}

	result, err := bootstrap.Bootstrap(ctx, cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	<-result.ProvidersReady
	if result.AuditSink == nil {
		t.Fatal("expected AuditSink to be non-nil")
	}

	if runtimeDeps.Invoker == nil {
		t.Fatal("expected runtime invoker to be non-nil")
	}
	if runtimeDeps.CapabilityLister == nil {
		t.Fatal("expected runtime capability lister to be non-nil")
	}
	if any(runtimeDeps.Invoker) != any(runtimeDeps.CapabilityLister) {
		t.Fatal("expected runtime invoker and capability lister to be the same scoped instance")
	}

	rtCaps := runtimeDeps.CapabilityLister.ListCapabilities()
	for _, cap := range rtCaps {
		if cap.Provider != "alpha" {
			t.Errorf("runtime broker should only see alpha, got %q", cap.Provider)
		}
	}

	if bindingDeps.Invoker == nil {
		t.Fatal("expected binding deps to carry an invoker")
	}
	if bindingDeps.Invoker == result.Invoker {
		t.Fatal("expected binding deps to be scoped, not the shared invoker")
	}

	_, err = bindingDeps.Invoker.Invoke(ctx, &principal.Principal{}, "alpha", "", "do", nil)
	if err == nil || !strings.Contains(err.Error(), "not available in this scope") {
		t.Fatalf("expected scoped binding invoker to reject alpha, got %v", err)
	}

	resultOp, err := bindingDeps.Invoker.Invoke(ctx, &principal.Principal{}, "beta", "", "do", nil)
	if err != nil {
		t.Fatalf("expected scoped binding invoker to allow beta: %v", err)
	}
	if resultOp.Status != http.StatusOK {
		t.Fatalf("expected binding invoke status 200, got %d", resultOp.Status)
	}
}
