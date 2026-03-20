package broker_test

import (
	"testing"

	"github.com/valon-technologies/toolshed/core"
	coretesting "github.com/valon-technologies/toolshed/core/testing"
	"github.com/valon-technologies/toolshed/internal/broker"
	"github.com/valon-technologies/toolshed/internal/registry"
)

type stubProviderWithOps struct {
	coretesting.StubIntegration
	ops []core.Operation
}

func (s *stubProviderWithOps) ListOperations() []core.Operation { return s.ops }

func newBrokerWithProviders(t *testing.T, providers ...core.Provider) *broker.Broker {
	t.Helper()
	reg := registry.New()
	for _, p := range providers {
		if err := reg.Providers.Register(p.Name(), p); err != nil {
			t.Fatalf("registering provider: %v", err)
		}
	}
	ds := &coretesting.StubDatastore{}
	return broker.New(&reg.Providers, ds)
}
