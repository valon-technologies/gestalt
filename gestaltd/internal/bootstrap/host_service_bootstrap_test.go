package bootstrap

import (
	"context"
	"testing"

	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
)

func eagerHostServiceTestDeps(registry *runtimehost.PublicHostServiceRegistry) Deps {
	return Deps{
		BaseURL:               "https://gestalt.example.test",
		EncryptionKey:         []byte("0123456789abcdef0123456789abcdef"),
		SelectedIndexedDBName: "main",
		IndexedDBs: map[string]indexeddb.IndexedDB{
			"main": &coretesting.StubIndexedDB{},
		},
		PublicHostServices: registry,
		Runtime:            newCapturingRuntime(),
	}
}

func TestRegisterConfiguredAppPublicHostServicesRegistersBeforeActivation(t *testing.T) {
	t.Parallel()

	registry := runtimehost.NewPublicHostServiceRegistry()
	deps := eagerHostServiceTestDeps(registry)
	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"gIssues": {
				Runtime:   &config.RuntimePlacementConfig{},
				IndexedDB: &config.IndexedDBBindingConfig{Provider: "main"},
			},
		},
	}

	cleanup := registerConfiguredAppPublicHostServices(context.Background(), cfg, deps)

	assertPublicHostServicesVerified(t, registry, "indexeddb")
	found := false
	for _, service := range registry.Snapshot() {
		if service.AppName == "gIssues" && service.Service.Name == "indexeddb" {
			found = true
		}
	}
	if !found {
		t.Fatalf("registry = %#v, want indexeddb host service for gIssues before activation", registry.Snapshot())
	}

	cleanup()
	if services := registry.Snapshot(); len(services) != 0 {
		t.Fatalf("after cleanup registry = %#v, want none", services)
	}
}

func TestRegisterConfiguredAppPublicHostServicesSkipsNonRuntimeApps(t *testing.T) {
	t.Parallel()

	registry := runtimehost.NewPublicHostServiceRegistry()
	deps := eagerHostServiceTestDeps(registry)
	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"localApp": {
				IndexedDB: &config.IndexedDBBindingConfig{Provider: "main"},
			},
		},
	}

	registerConfiguredAppPublicHostServices(context.Background(), cfg, deps)

	if services := registry.Snapshot(); len(services) != 0 {
		t.Fatalf("registry = %#v, want no eager registration for a non-runtime-placed app", services)
	}
}

func TestRegisterConfiguredAppPublicHostServicesNoopWithoutRelay(t *testing.T) {
	t.Parallel()

	registry := runtimehost.NewPublicHostServiceRegistry()
	deps := eagerHostServiceTestDeps(registry)
	deps.EncryptionKey = nil
	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"gIssues": {
				Runtime:   &config.RuntimePlacementConfig{},
				IndexedDB: &config.IndexedDBBindingConfig{Provider: "main"},
			},
		},
	}

	registerConfiguredAppPublicHostServices(context.Background(), cfg, deps)

	if services := registry.Snapshot(); len(services) != 0 {
		t.Fatalf("registry = %#v, want no registration when the public relay is unavailable", services)
	}
}
