package runtimehost_test

import (
	"testing"

	"github.com/valon-technologies/gestalt/server/services/runtimehost"
)

func TestConfigureSessionRegistry(t *testing.T) {
	t.Parallel()
	registry := runtimehost.NewConfigureSessionRegistry()
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
