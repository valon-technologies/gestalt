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
			RemoteToken: "gst_api_test",
		},
		Apps: map[string]*config.ProviderEntry{
			"ci-cd":         {},
			"valon-profile": {DevActive: true},
		},
	}
	plan, err := NewPlacementPlan(cfg)
	if err != nil {
		t.Fatalf("NewPlacementPlan: %v", err)
	}

	if got := plan.Placement(RemoteProviderKindApp, "valon-profile"); got != PlacementLocal {
		t.Fatalf("valon-profile placement = %v, want local", got)
	}
	if plan.ShouldRouteRemote(RemoteProviderKindApp, "valon-profile") {
		t.Fatal("local dev-active app should not route remote")
	}
	if got := plan.Placement(RemoteProviderKindApp, "ci-cd"); got != PlacementRemote {
		t.Fatalf("ci-cd placement = %v, want remote", got)
	}
}

func TestPlacementPlanDeclaredAppRoutesRemoteWhenConfigured(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Server: config.ServerConfig{
			Remote:      "https://valon.tools",
			RemoteToken: "gst_api_test",
		},
		Apps: map[string]*config.ProviderEntry{
			"linear": {},
		},
	}
	plan, err := NewPlacementPlan(cfg)
	if err != nil {
		t.Fatalf("NewPlacementPlan: %v", err)
	}
	if !plan.ShouldRouteRemote(RemoteProviderKindApp, "linear") {
		t.Fatal("declared app should route remote when server.remote is configured")
	}
}

func TestPlacementPlanDeclaredHostServicesRouteRemoteWhenConfigured(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Server: config.ServerConfig{
			Remote:      "https://valon.tools",
			RemoteToken: "gst_api_test",
			Providers: config.ServerProvidersConfig{
				IndexedDB: "main",
			},
		},
		Providers: config.ProvidersConfig{
			Agent: map[string]*config.ProviderEntry{
				"default": {},
			},
			Workflow: map[string]*config.ProviderEntry{
				"default": {},
			},
			IndexedDB: map[string]*config.ProviderEntry{
				"main":  {Default: true},
				"extra": {},
			},
		},
	}
	plan, err := NewPlacementPlan(cfg)
	if err != nil {
		t.Fatalf("NewPlacementPlan: %v", err)
	}

	for _, tc := range []struct {
		kind RemoteProviderKind
		name string
	}{
		{RemoteProviderKindAgent, "default"},
		{RemoteProviderKindWorkflow, "default"},
		{RemoteProviderKindIndexedDB, "extra"},
	} {
		if !plan.ShouldRouteRemote(tc.kind, tc.name) {
			t.Fatalf("%s/%s should route remote", tc.kind, tc.name)
		}
	}
	if plan.ShouldRouteRemote(RemoteProviderKindIndexedDB, "main") {
		t.Fatal("selected system indexeddb must stay local")
	}
	if got := plan.Placement(RemoteProviderKindIndexedDB, "main"); got != PlacementLocal {
		t.Fatalf("main indexeddb placement = %v, want local", got)
	}
}

func TestPlacementPlanUndeclaredProviderRemainsUndeclared(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Server: config.ServerConfig{
			Remote:      "https://valon.tools",
			RemoteToken: "gst_api_test",
		},
		Apps: map[string]*config.ProviderEntry{
			"ci-cd": {},
		},
	}
	plan, err := NewPlacementPlan(cfg)
	if err != nil {
		t.Fatalf("NewPlacementPlan: %v", err)
	}
	if plan.IsDeclared(RemoteProviderKindApp, "missing-app") {
		t.Fatal("undeclared app should not be declared")
	}
	if got := plan.Placement(RemoteProviderKindApp, "missing-app"); got != PlacementUndeclared {
		t.Fatalf("missing-app placement = %v, want undeclared", got)
	}
	if plan.ShouldRouteRemote(RemoteProviderKindApp, "missing-app") {
		t.Fatal("undeclared app should not route remote")
	}
}

func TestPlacementPlanLocalOnlyBehaviorWithoutRemote(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"linear":        {},
			"valon-profile": {DevActive: true},
		},
		Providers: config.ProvidersConfig{
			Agent: map[string]*config.ProviderEntry{
				"default": {},
			},
		},
	}
	plan, err := NewPlacementPlan(cfg)
	if err != nil {
		t.Fatalf("NewPlacementPlan: %v", err)
	}
	for _, name := range []string{"linear", "valon-profile"} {
		if plan.ShouldRouteRemote(RemoteProviderKindApp, name) {
			t.Fatalf("app %q should not route remote without server.remote", name)
		}
		if got := plan.Placement(RemoteProviderKindApp, name); got != PlacementLocal {
			t.Fatalf("app %q placement = %v, want local", name, got)
		}
	}
	if plan.ShouldRouteRemote(RemoteProviderKindAgent, "default") {
		t.Fatal("agent should not route remote without server.remote")
	}
}

func TestPlacementPlanDevActiveNeverRoutesRemoteAfterLocalSelection(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Server: config.ServerConfig{
			Remote:      "https://valon.tools",
			RemoteToken: "gst_api_test",
		},
		Apps: map[string]*config.ProviderEntry{
			"ci-cd": {DevActive: true},
		},
	}
	plan, err := NewPlacementPlan(cfg)
	if err != nil {
		t.Fatalf("NewPlacementPlan: %v", err)
	}
	if plan.ShouldRouteRemote(RemoteProviderKindApp, "ci-cd") {
		t.Fatal("dev-active local app must never route remote, even after local startup failure")
	}
}
