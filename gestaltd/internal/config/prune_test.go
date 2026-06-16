package config

import (
	"slices"
	"testing"
)

func TestRetainAppsPrunesToClosure(t *testing.T) {
	cfg := &Config{
		Apps: map[string]*ProviderEntry{
			"alpha": {UI: "alphaUI", IndexedDB: &IndexedDBBindingConfig{Provider: "main"}},
			"beta":  {UI: "betaUI", S3: []string{"betaStore"}},
		},
		Providers: ProvidersConfig{
			UI: map[string]*UIEntry{
				"alphaUI": {OwnerApp: "alpha"},
				"betaUI":  {OwnerApp: "beta"},
			},
			IndexedDB: map[string]*ProviderEntry{"main": {}},
			S3:        map[string]*ProviderEntry{"betaStore": {}},
			// Shared infra is retained wholesale, even though no app names it.
			Secrets: map[string]*ProviderEntry{"env": {}},
		},
		Server: ServerConfig{
			Providers: ServerProvidersConfig{IndexedDB: "main"},
		},
		Workflows: WorkflowsConfig{
			Definitions: map[string]WorkflowDefinitionConfig{
				"betaFlow":  {Steps: []WorkflowStepConfig{{App: &WorkflowStepAppCallConfig{Name: "beta"}}}},
				"alphaFlow": {Steps: []WorkflowStepConfig{{App: &WorkflowStepAppCallConfig{Name: "alpha"}}}},
			},
		},
	}

	result, err := RetainApps(cfg, []string{"alpha"})
	if err != nil {
		t.Fatalf("RetainApps: %v", err)
	}

	assertPresent := func(label string, present bool, want bool) {
		t.Helper()
		if present != want {
			t.Fatalf("%s present=%v, want %v", label, present, want)
		}
	}
	_, alphaApp := cfg.Apps["alpha"]
	_, betaApp := cfg.Apps["beta"]
	_, alphaUI := cfg.Providers.UI["alphaUI"]
	_, betaUI := cfg.Providers.UI["betaUI"]
	_, mainDB := cfg.Providers.IndexedDB["main"]
	_, betaStore := cfg.Providers.S3["betaStore"]
	_, secrets := cfg.Providers.Secrets["env"]
	_, betaFlow := cfg.Workflows.Definitions["betaFlow"]
	_, alphaFlow := cfg.Workflows.Definitions["alphaFlow"]

	assertPresent("apps.alpha", alphaApp, true)
	assertPresent("apps.beta", betaApp, false)
	assertPresent("ui.alphaUI", alphaUI, true)
	assertPresent("ui.betaUI (orphaned)", betaUI, false)
	assertPresent("indexeddb.main (referenced+selected)", mainDB, true)
	assertPresent("s3.betaStore (only beta used it)", betaStore, false)
	assertPresent("secrets.env (shared infra)", secrets, true)
	assertPresent("workflow betaFlow (targets dropped app)", betaFlow, false)
	assertPresent("workflow alphaFlow", alphaFlow, true)

	if !slices.Equal(result.Kept, []string{"alpha"}) {
		t.Fatalf("Kept = %v, want [alpha]", result.Kept)
	}
	if !slices.Equal(result.DroppedWorkflows, []string{"betaFlow"}) {
		t.Fatalf("DroppedWorkflows = %v, want [betaFlow]", result.DroppedWorkflows)
	}
}

func TestRetainAppsKeepsDefaultProviders(t *testing.T) {
	cfg := &Config{
		Apps: map[string]*ProviderEntry{"alpha": {}},
		Providers: ProvidersConfig{
			IndexedDB: map[string]*ProviderEntry{
				"unreferencedDefault": {Default: true},
				"unreferenced":        {},
			},
		},
	}
	if _, err := RetainApps(cfg, []string{"alpha"}); err != nil {
		t.Fatalf("RetainApps: %v", err)
	}
	if _, ok := cfg.Providers.IndexedDB["unreferencedDefault"]; !ok {
		t.Fatal("default indexeddb dropped, want retained")
	}
	if _, ok := cfg.Providers.IndexedDB["unreferenced"]; ok {
		t.Fatal("unreferenced non-default indexeddb retained, want dropped")
	}
}

func TestRetainAppsUnknownApp(t *testing.T) {
	cfg := &Config{Apps: map[string]*ProviderEntry{"alpha": {}}}
	if _, err := RetainApps(cfg, []string{"ghost"}); err == nil {
		t.Fatal("expected error for unknown app, got nil")
	}
}

func TestRetainAppsEmptyKeepIsNoOp(t *testing.T) {
	cfg := &Config{Apps: map[string]*ProviderEntry{"alpha": {}, "beta": {}}}
	if _, err := RetainApps(cfg, nil); err != nil {
		t.Fatalf("RetainApps nil: %v", err)
	}
	if len(cfg.Apps) != 2 {
		t.Fatalf("apps pruned on no-op: %d, want 2", len(cfg.Apps))
	}
}
