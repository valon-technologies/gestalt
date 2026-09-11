package appregistry

import (
	"testing"
)

func TestResolveProcessIDFromEnv(t *testing.T) {
	t.Setenv(processIDEnvVar, "process-a")
	if got := resolveProcessID("process-a"); got != "process-a" {
		t.Fatalf("resolveProcessID() = %q, want process-a", got)
	}
}

func TestResolveProcessIDGeneratesUUIDWhenUnset(t *testing.T) {
	t.Parallel()
	got := resolveProcessID("")
	if got == "" {
		t.Fatal("resolveProcessID returned empty string")
	}
}

func TestResolveProcessIDIsStableWithinProcess(t *testing.T) {
	t.Setenv(processIDEnvVar, "")
	first := ResolveProcessID()
	second := ResolveProcessID()
	if first == "" || second == "" {
		t.Fatal("ResolveProcessID returned empty string")
	}
	if first != second {
		t.Fatalf("ResolveProcessID() = %q then %q, want stable value within process", first, second)
	}
}
