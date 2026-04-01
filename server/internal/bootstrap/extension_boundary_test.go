package bootstrap

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/registry"
)

func TestBuildExtensions_ShutsDownRuntimesWhenBindingConstructionFails(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Runtimes: map[string]config.RuntimeDef{
			"echo": {Type: "test-runtime"},
		},
		Bindings: map[string]config.BindingDef{
			"bad": {Type: "test-binding"},
		},
	}

	runtimeStopped := false
	factories := NewFactoryRegistry()
	factories.Runtimes["test-runtime"] = func(_ context.Context, name string, _ config.RuntimeDef, _ RuntimeDeps) (core.Runtime, error) {
		return &coretesting.StubRuntime{
			N: name,
			StopFn: func(context.Context) error {
				runtimeStopped = true
				return nil
			},
		}, nil
	}
	factories.Bindings["test-binding"] = func(_ context.Context, _ string, _ config.BindingDef, _ BindingDeps) (core.Binding, error) {
		return nil, errors.New("boom")
	}

	_, _, err := buildExtensions(context.Background(), cfg, factories, nil, nil, nil, EgressDeps{})
	if err == nil {
		t.Fatal("expected buildExtensions to fail")
	}
	if !strings.Contains(err.Error(), `binding "bad"`) {
		t.Fatalf("unexpected error: %v", err)
	}
	if !runtimeStopped {
		t.Fatal("expected buildExtensions to stop constructed runtimes when binding construction fails")
	}
}

func TestShutdownExtensions_ClosesBindingsAndRuntimes(t *testing.T) {
	t.Parallel()

	bindingClosed := false
	runtimeStopped := false

	runtimes := registryWithRuntime(t, "echo", &coretesting.StubRuntime{
		N: "echo",
		StopFn: func(context.Context) error {
			runtimeStopped = true
			return nil
		},
	})
	bindings := registryWithBinding(t, "hook", &coretesting.StubBinding{
		N: "hook",
		CloseFn: func() error {
			bindingClosed = true
			return nil
		},
	})

	if err := shutdownExtensions(context.Background(), runtimes, bindings); err != nil {
		t.Fatalf("shutdownExtensions: %v", err)
	}
	if !bindingClosed {
		t.Fatal("expected shutdownExtensions to close bindings")
	}
	if !runtimeStopped {
		t.Fatal("expected shutdownExtensions to stop runtimes")
	}
}

func registryWithRuntime(t *testing.T, name string, runtime core.Runtime) *registry.PluginMap[core.Runtime] {
	t.Helper()

	runtimes := registry.NewRuntimeMap()
	if err := runtimes.Register(name, runtime); err != nil {
		t.Fatalf("Register runtime %q: %v", name, err)
	}
	return runtimes
}

func registryWithBinding(t *testing.T, name string, binding core.Binding) *registry.PluginMap[core.Binding] {
	t.Helper()

	bindings := registry.NewBindingMap()
	if err := bindings.Register(name, binding); err != nil {
		t.Fatalf("Register binding %q: %v", name, err)
	}
	return bindings
}
