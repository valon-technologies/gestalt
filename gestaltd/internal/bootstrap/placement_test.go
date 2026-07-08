package bootstrap

import (
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

func TestPlacementPlan(t *testing.T) {
	t.Parallel()

	remoteCfg := &config.Config{
		Server: config.ServerConfig{Remote: "https://valon.tools"},
	}
	localApp := &config.ProviderEntry{DevActive: true}
	remoteApp := &config.ProviderEntry{}
	host := &config.ProviderEntry{}

	tests := []struct {
		name        string
		plan        *PlacementPlan
		entry       *config.ProviderEntry
		buildLocal  bool
		routeRemote bool
	}{
		{name: "dev-active wins", plan: NewPlacementPlan(remoteCfg), entry: localApp, buildLocal: true},
		{name: "declared app remote", plan: NewPlacementPlan(remoteCfg), entry: remoteApp, routeRemote: true},
		{name: "host provider remote", plan: NewPlacementPlan(remoteCfg), entry: host, routeRemote: true},
		{name: "absent", plan: NewPlacementPlan(remoteCfg), entry: nil},
		{name: "no remote configured", plan: NewPlacementPlan(&config.Config{}), entry: remoteApp, buildLocal: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.plan.ShouldBuildLocal(tc.entry); got != tc.buildLocal {
				t.Fatalf("ShouldBuildLocal = %v, want %v", got, tc.buildLocal)
			}
			if got := tc.plan.ShouldRouteRemote(tc.entry); got != tc.routeRemote {
				t.Fatalf("ShouldRouteRemote = %v, want %v", got, tc.routeRemote)
			}
		})
	}
}
