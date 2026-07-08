package bootstrap

import (
	"context"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

func TestPublishRemoteProvidersSkipsWithoutRemote(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Providers: config.ProvidersConfig{
			Agent: map[string]*config.ProviderEntry{
				"managed": {},
			},
		},
	}
	deps := Deps{Placement: NewPlacementPlan(cfg)}
	out, err := publishRemoteProviders(context.Background(), cfg, &deps)
	if err != nil {
		t.Fatalf("publishRemoteProviders: %v", err)
	}
	if len(out.extraIndexedDBs) != 0 {
		t.Fatalf("extraIndexedDBs = %#v, want none", out.extraIndexedDBs)
	}
}
