package testutil

import (
	"testing"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/registry"
)

// NewProviderRegistry creates a PluginMap populated with the given providers.
func NewProviderRegistry(t *testing.T, providers ...core.Provider) *registry.PluginMap[core.Provider] {
	t.Helper()
	reg := registry.New()
	for _, p := range providers {
		if err := reg.Providers.Register(p.Name(), p); err != nil {
			t.Fatalf("registering provider: %v", err)
		}
	}
	return &reg.Providers
}
