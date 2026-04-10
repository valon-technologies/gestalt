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
	secretManagerClosed := false

	result := &bootstrap.Result{
		Providers: registryWithProvider(t, "acme", &closableProvider{
			StubIntegration: coretesting.StubIntegration{N: "acme"},
			closeFn: func() error {
				providerClosed = true
				return nil
			},
		}),
		Services: coretesting.NewStubServices(t),
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
		t.Error("expected provider to be closed")
	}
	if !secretManagerClosed {
		t.Error("expected secret manager to be closed")
	}
}

func registryWithProvider(t *testing.T, name string, p *closableProvider) *registry.PluginMap[core.Provider] {
	t.Helper()
	r := registry.New()
	_ = r.Providers.Register(name, p)
	return &r.Providers
}
