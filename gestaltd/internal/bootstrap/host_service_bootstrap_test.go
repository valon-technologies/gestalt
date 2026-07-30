package bootstrap

import (
	"testing"

	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
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

func appHostServiceRegistered(registry *runtimehost.PublicHostServiceRegistry) bool {
	for _, service := range registry.Snapshot() {
		if service.AppName == appInvocationPublicProviderKey && service.Service.Name == "app" {
			return true
		}
	}
	return false
}

func TestRegisterGlobalAppInvocationPublicHostServiceRegistersOnce(t *testing.T) {
	t.Parallel()

	registry := runtimehost.NewPublicHostServiceRegistry()
	deps := eagerHostServiceTestDeps(registry)

	cleanup := registerGlobalAppInvocationPublicHostService(deps)

	assertPublicHostServicesVerified(t, registry, "app")
	if !appHostServiceRegistered(registry) {
		t.Fatalf("registry = %#v, want global app host service under %q", registry.Snapshot(), appInvocationPublicProviderKey)
	}

	cleanup()
	if services := registry.Snapshot(); len(services) != 0 {
		t.Fatalf("after cleanup registry = %#v, want none", services)
	}
}

func TestRegisterGlobalAppInvocationPublicHostServiceNoopWithoutRelay(t *testing.T) {
	t.Parallel()

	registry := runtimehost.NewPublicHostServiceRegistry()
	deps := eagerHostServiceTestDeps(registry)
	deps.EncryptionKey = nil

	registerGlobalAppInvocationPublicHostService(deps)

	if services := registry.Snapshot(); len(services) != 0 {
		t.Fatalf("registry = %#v, want no registration when the public relay is unavailable", services)
	}
}
