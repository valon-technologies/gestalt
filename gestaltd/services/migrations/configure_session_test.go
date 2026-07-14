package migrations_test

import (
	"testing"

	migrationsservice "github.com/valon-technologies/gestalt/server/services/migrations"
)

func TestConfigureSessionRegistry(t *testing.T) {
	registry := migrationsservice.NewConfigureSessionRegistry()
	if registry.Active("dealHub") {
		t.Fatal("expected inactive session before configure")
	}
	registry.Begin("dealHub")
	if !registry.Active("dealHub") {
		t.Fatal("expected active configure session")
	}
	registry.End("dealHub")
	if registry.Active("dealHub") {
		t.Fatal("expected inactive session after configure")
	}
}
