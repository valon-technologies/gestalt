package bootstrap

import (
	"context"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

func TestShouldRouteRemoteHelpers(t *testing.T) {
	t.Parallel()

	plan, err := NewPlacementPlan(&config.Config{
		Server: config.ServerConfig{
			Remote:      "https://valon.tools",
			RemoteToken: "gst_api_test",
		},
		Apps: map[string]*config.ProviderEntry{
			"linear":        {},
			"valon-profile": {DevActive: true},
		},
		Providers: config.ProvidersConfig{
			Agent:    map[string]*config.ProviderEntry{"default": {}},
			Workflow: map[string]*config.ProviderEntry{"default": {}},
		},
	})
	if err != nil {
		t.Fatalf("NewPlacementPlan: %v", err)
	}
	deps := Deps{Placement: plan}

	if !shouldRouteRemoteApp(deps, "linear") {
		t.Fatal("linear should route remote")
	}
	if shouldRouteRemoteApp(deps, "valon-profile") {
		t.Fatal("dev-active app should stay local")
	}
	if !shouldRouteRemoteAgent(deps, "default") {
		t.Fatal("agent should route remote")
	}
	if !shouldRouteRemoteWorkflow(deps, "default") {
		t.Fatal("workflow should route remote")
	}
}

func TestBuildAgentRequiresRemoteClientWhenRouted(t *testing.T) {
	t.Parallel()

	plan, err := NewPlacementPlan(&config.Config{
		Server: config.ServerConfig{
			Remote:      "https://valon.tools",
			RemoteToken: "gst_api_test",
		},
		Providers: config.ProvidersConfig{
			Agent: map[string]*config.ProviderEntry{"default": {}},
		},
	})
	if err != nil {
		t.Fatalf("NewPlacementPlan: %v", err)
	}
	_, err = buildAgent(context.Background(), "default", &config.ProviderEntry{}, NewFactoryRegistry(), Deps{Placement: plan})
	if err == nil {
		t.Fatal("buildAgent = nil, want missing remote client error")
	}
}

func TestBuildAgentKeepsLocalFactoryWithoutRemote(t *testing.T) {
	t.Parallel()

	plan, err := NewPlacementPlan(&config.Config{
		Providers: config.ProvidersConfig{
			Agent: map[string]*config.ProviderEntry{"default": {Default: true}},
		},
	})
	if err != nil {
		t.Fatalf("NewPlacementPlan: %v", err)
	}
	_, err = buildAgent(context.Background(), "default", &config.ProviderEntry{}, NewFactoryRegistry(), Deps{Placement: plan})
	if err == nil {
		t.Fatal("buildAgent = nil, want missing factory error")
	}
}
