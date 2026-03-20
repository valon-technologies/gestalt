package bootstrap_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/valon-technologies/toolshed/core"
	coretesting "github.com/valon-technologies/toolshed/core/testing"
	"github.com/valon-technologies/toolshed/internal/bootstrap"
	"github.com/valon-technologies/toolshed/internal/config"
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
	if result.Broker == nil {
		t.Fatal("expected Broker to be non-nil")
	}
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
	factories.Providers["beta"] = func(_ context.Context, _ string, _ config.IntegrationDef, _ bootstrap.Deps) (core.Provider, error) {
		return &stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N:        "beta",
				ConnMode: core.ConnectionModeNone,
				ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
					return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
				},
			},
			ops: []core.Operation{{Name: "do"}},
		}, nil
	}

	var runtimeBroker core.Broker
	factories.Runtimes["echo"] = func(_ context.Context, name string, _ config.RuntimeDef, brk core.Broker) (core.Runtime, error) {
		runtimeBroker = brk
		return &coretesting.StubRuntime{N: name}, nil
	}

	var bindingBroker core.Broker
	factories.Bindings["test-binding"] = func(_ context.Context, name string, _ config.BindingDef, brk core.Broker) (core.Binding, error) {
		bindingBroker = brk
		return &coretesting.StubBinding{N: name, K: core.BindingTrigger}, nil
	}

	result, err := bootstrap.Bootstrap(ctx, cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if result.AuditSink == nil {
		t.Fatal("expected AuditSink to be non-nil")
	}

	rtCaps := runtimeBroker.ListCapabilities()
	for _, cap := range rtCaps {
		if cap.Provider != "alpha" {
			t.Errorf("runtime broker should only see alpha, got %q", cap.Provider)
		}
	}

	bndCaps := bindingBroker.ListCapabilities()
	for _, cap := range bndCaps {
		if cap.Provider != "beta" {
			t.Errorf("binding broker should only see beta, got %q", cap.Provider)
		}
	}
}
