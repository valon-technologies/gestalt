package bootstrap_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/core"
	coretesting "github.com/valon-technologies/gestalt/core/testing"
	"github.com/valon-technologies/gestalt/internal/bootstrap"
	"github.com/valon-technologies/gestalt/internal/registry"
)

type closableProvider struct {
	coretesting.StubIntegration
	closeFn func() error
}

func (p *closableProvider) Close() error {
	if p.closeFn != nil {
		return p.closeFn()
	}
	return nil
}

type closableDatastore struct {
	coretesting.StubDatastore
	closeFn func() error
}

func (d *closableDatastore) Close() error {
	if d.closeFn != nil {
		return d.closeFn()
	}
	return nil
}

type closableSecretManager struct {
	coretesting.StubSecretManager
	closeFn func() error
}

func (s *closableSecretManager) Close() error {
	if s.closeFn != nil {
		return s.closeFn()
	}
	return nil
}

func TestResultStart_StartsRuntimesBeforeBindings(t *testing.T) {
	t.Parallel()

	runtimeStarted := false
	bindingSawRuntimeStarted := false

	result := &bootstrap.Result{
		Runtimes: registryWithRuntime(t, "echo", &coretesting.StubRuntime{
			N: "echo",
			StartFn: func(context.Context) error {
				runtimeStarted = true
				return nil
			},
		}),
		Bindings: registryWithBinding(t, "hook", &coretesting.StubBinding{
			N: "hook",
			StartFn: func(context.Context) error {
				bindingSawRuntimeStarted = runtimeStarted
				return nil
			},
		}),
	}

	if err := result.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !runtimeStarted {
		t.Fatal("expected Start to start runtimes")
	}
	if !bindingSawRuntimeStarted {
		t.Fatal("expected bindings to start after runtimes")
	}
}

func TestResultStart_CleansUpFailedBindingAndStartedRuntimes(t *testing.T) {
	t.Parallel()

	runtimeStopped := false
	bindingClosed := false

	result := &bootstrap.Result{
		Runtimes: registryWithRuntime(t, "echo", &coretesting.StubRuntime{
			N: "echo",
			StopFn: func(context.Context) error {
				runtimeStopped = true
				return nil
			},
		}),
		Bindings: registryWithBinding(t, "hook", &coretesting.StubBinding{
			N: "hook",
			StartFn: func(context.Context) error {
				return errors.New("boom")
			},
			CloseFn: func() error {
				bindingClosed = true
				return nil
			},
		}),
	}

	err := result.Start(context.Background())
	if err == nil {
		t.Fatal("expected Start to fail")
	}
	if !strings.Contains(err.Error(), `starting binding "hook"`) {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bindingClosed {
		t.Fatal("expected failed binding to be closed")
	}
	if !runtimeStopped {
		t.Fatal("expected started runtimes to be stopped")
	}
}

func TestResultClose_ShutsDownConstructedResources(t *testing.T) {
	t.Parallel()

	bindingClosed := false
	runtimeStopped := false
	providerClosed := false
	datastoreClosed := false
	secretManagerClosed := false

	result := &bootstrap.Result{
		Bindings: registryWithBinding(t, "hook", &coretesting.StubBinding{
			N: "hook",
			CloseFn: func() error {
				bindingClosed = true
				return nil
			},
		}),
		Runtimes: registryWithRuntime(t, "echo", &coretesting.StubRuntime{
			N: "echo",
			StopFn: func(context.Context) error {
				runtimeStopped = true
				return nil
			},
		}),
		Providers: registryWithProvider(t, "acme", &closableProvider{
			StubIntegration: coretesting.StubIntegration{N: "acme"},
			closeFn: func() error {
				providerClosed = true
				return nil
			},
		}),
		Datastore: &closableDatastore{
			closeFn: func() error {
				datastoreClosed = true
				return nil
			},
		},
		SecretManager: &closableSecretManager{
			closeFn: func() error {
				secretManagerClosed = true
				return nil
			},
		},
	}

	if err := result.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !bindingClosed {
		t.Fatal("expected Close to close bindings")
	}
	if !runtimeStopped {
		t.Fatal("expected Close to stop runtimes")
	}
	if !providerClosed {
		t.Fatal("expected Close to close providers")
	}
	if !datastoreClosed {
		t.Fatal("expected Close to close datastore")
	}
	if !secretManagerClosed {
		t.Fatal("expected Close to close secret manager")
	}
}

func registryWithProvider(t *testing.T, name string, provider core.Provider) *registry.PluginMap[core.Provider] {
	t.Helper()

	reg := registry.New()
	if err := reg.Providers.Register(name, provider); err != nil {
		t.Fatalf("Register provider %q: %v", name, err)
	}
	return &reg.Providers
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
