package bootstrap

import (
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

func TestPlacementPlanLocalAppOverridesRemote(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Server: config.ServerConfig{
			Remote:      "https://valon.tools",
			RemoteToken: "token",
		},
		Apps: map[string]*config.ProviderEntry{
			"demo":   {DevActive: true},
			"linear": {},
		},
	}
	plan := NewPlacementPlan(cfg)

	if !plan.ShouldBuildLocal(RemoteProviderKindApp, "demo") {
		t.Fatal("expected local dev-active app to build locally")
	}
	if plan.ShouldRouteRemote(RemoteProviderKindApp, "demo") {
		t.Fatal("expected local dev-active app to stay local")
	}
	if plan.Placement(RemoteProviderKindApp, "demo") != ProviderPlacementLocal {
		t.Fatalf("placement = %v, want local", plan.Placement(RemoteProviderKindApp, "demo"))
	}
}

func TestPlacementPlanDeclaredAppRoutesRemote(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Server: config.ServerConfig{
			Remote:      "https://valon.tools",
			RemoteToken: "token",
		},
		Apps: map[string]*config.ProviderEntry{
			"linear": {},
		},
	}
	plan := NewPlacementPlan(cfg)

	if plan.ShouldBuildLocal(RemoteProviderKindApp, "linear") {
		t.Fatal("expected declared app without local overlay to skip local build")
	}
	if !plan.ShouldRouteRemote(RemoteProviderKindApp, "linear") {
		t.Fatal("expected declared app to route remote")
	}
}

func TestPlacementPlanDeclaredHostProvidersRouteRemote(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Server: config.ServerConfig{
			Remote:      "https://valon.tools",
			RemoteToken: "token",
		},
		Providers: config.ProvidersConfig{
			Agent:     map[string]*config.ProviderEntry{"managed": {}},
			Workflow:  map[string]*config.ProviderEntry{"default": {}},
			IndexedDB: map[string]*config.ProviderEntry{"main": {}},
		},
	}
	plan := NewPlacementPlan(cfg)

	for _, tc := range []struct {
		kind RemoteProviderKind
		name string
	}{
		{RemoteProviderKindAgent, "managed"},
		{RemoteProviderKindWorkflow, "default"},
		{RemoteProviderKindIndexedDB, "main"},
	} {
		if plan.ShouldBuildLocal(tc.kind, tc.name) {
			t.Fatalf("%s/%s: expected remote placement", tc.kind, tc.name)
		}
		if !plan.ShouldRouteRemote(tc.kind, tc.name) {
			t.Fatalf("%s/%s: expected remote routing", tc.kind, tc.name)
		}
	}
}

func TestPlacementPlanUndeclaredProviderAbsent(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Server: config.ServerConfig{
			Remote:      "https://valon.tools",
			RemoteToken: "token",
		},
	}
	plan := NewPlacementPlan(cfg)

	if plan.Placement(RemoteProviderKindApp, "missing") != ProviderPlacementAbsent {
		t.Fatalf("placement = %v, want absent", plan.Placement(RemoteProviderKindApp, "missing"))
	}
	if plan.ShouldRouteRemote(RemoteProviderKindApp, "missing") {
		t.Fatal("expected undeclared provider to remain absent")
	}
}

func TestPlacementPlanWithoutRemoteBuildsLocally(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"linear": {},
		},
		Providers: config.ProvidersConfig{
			Agent: map[string]*config.ProviderEntry{"managed": {}},
		},
	}
	plan := NewPlacementPlan(cfg)

	if !plan.ShouldBuildLocal(RemoteProviderKindApp, "linear") {
		t.Fatal("expected declared app to build locally without remote")
	}
	if plan.ShouldRouteRemote(RemoteProviderKindApp, "linear") {
		t.Fatal("expected no remote routing without server.remote")
	}
	if !plan.ShouldBuildLocal(RemoteProviderKindAgent, "managed") {
		t.Fatal("expected declared agent to build locally without remote")
	}
}

func TestPlacementPlanLocalProviderStartupFailureIsLocal(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"demo": {DevActive: true},
		},
	}
	plan := NewPlacementPlan(cfg)

	if !plan.ShouldBuildLocal(RemoteProviderKindApp, "demo") {
		t.Fatal("expected dev-active app to require local startup")
	}
	if plan.ShouldRouteRemote(RemoteProviderKindApp, "demo") {
		t.Fatal("expected local placement to block remote fallback")
	}
}
