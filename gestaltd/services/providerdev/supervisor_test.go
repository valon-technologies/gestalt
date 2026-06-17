package providerdev

import (
	"strings"
	"testing"
)

func TestDevCommandEnvReservedPortNotOverridable(t *testing.T) {
	t.Parallel()
	const reservedPort = 42424
	env := devCommandEnv(reservedPort, Target{
		BasePath: "/demo",
		Env: map[string]string{
			"GESTALT_DEV_PORT":      "9",
			"GESTALT_DEV_BASE_PATH": "/override",
			"GESTALT_DEV":           "0",
			"CUSTOM":                "ok",
		},
	})
	got := map[string]string{}
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		got[key] = value
	}
	if got["GESTALT_DEV_PORT"] != "42424" {
		t.Fatalf("GESTALT_DEV_PORT = %q, want %q", got["GESTALT_DEV_PORT"], "42424")
	}
	if got["GESTALT_DEV_BASE_PATH"] != "/demo" {
		t.Fatalf("GESTALT_DEV_BASE_PATH = %q, want /demo", got["GESTALT_DEV_BASE_PATH"])
	}
	if got["GESTALT_DEV"] != "1" {
		t.Fatalf("GESTALT_DEV = %q, want 1", got["GESTALT_DEV"])
	}
	if got["CUSTOM"] != "ok" {
		t.Fatalf("CUSTOM = %q, want ok", got["CUSTOM"])
	}
}
