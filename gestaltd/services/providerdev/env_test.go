package providerdev

import (
	"strconv"
	"strings"
	"testing"
)

func TestDevCommandEnvReservesRequestedPort(t *testing.T) {
	t.Parallel()

	env := devCommandEnv(43127, Target{
		BasePath: "/demo",
		Env: map[string]string{
			"GESTALT_DEV_PORT":      "9999",
			"GESTALT_DEV_BASE_PATH": "/wrong",
			"GESTALT_DEV":           "0",
		},
	})
	values := make(map[string]string)
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = value
		}
	}
	if got, want := values["GESTALT_DEV_PORT"], strconv.Itoa(43127); got != want {
		t.Fatalf("GESTALT_DEV_PORT = %q, want %q", got, want)
	}
	if got, want := values["GESTALT_DEV_BASE_PATH"], "/demo"; got != want {
		t.Fatalf("GESTALT_DEV_BASE_PATH = %q, want %q", got, want)
	}
	if got, want := values["GESTALT_DEV"], "1"; got != want {
		t.Fatalf("GESTALT_DEV = %q, want %q", got, want)
	}
}
