package bootstrap_test

import (
	"context"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/registry"
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

func TestResultClose_ShutsDownConstructedResources(t *testing.T) {
	t.Parallel()

	providerClosed := false
	datastoreClosed := false
	secretManagerClosed := false

	result := &bootstrap.Result{
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

	if err := result.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := result.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
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
