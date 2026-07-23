package appregistry

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestResolveInstanceIDFromEnv(t *testing.T) {
	t.Parallel()
	got := resolveInstanceID("replica-a", "localhost")
	if got != "replica-a" {
		t.Fatalf("resolveInstanceID() = %q, want replica-a", got)
	}
}

func TestResolveInstanceIDFromHostname(t *testing.T) {
	t.Parallel()
	got := resolveInstanceID("", "gestaltd-6f57fd4c4b-bbzzk")
	if got != "gestaltd-6f57fd4c4b-bbzzk" {
		t.Fatalf("resolveInstanceID() = %q, want pod hostname", got)
	}
}

func TestResolveInstanceIDIgnoresLocalhostHostname(t *testing.T) {
	t.Parallel()
	got := resolveInstanceID("", "localhost")
	if _, err := uuid.Parse(got); err != nil {
		t.Fatalf("resolveInstanceID(localhost) = %q, want UUID: %v", got, err)
	}
}

func TestResolveInstanceIDGeneratesUUIDWhenHostnameMissing(t *testing.T) {
	t.Parallel()
	got := resolveInstanceID("", "")
	if _, err := uuid.Parse(got); err != nil {
		t.Fatalf("resolveInstanceID(empty) = %q, want UUID: %v", got, err)
	}
}

func TestResolveInstanceIDEnvOverridesLocalhostHostname(t *testing.T) {
	t.Parallel()
	got := resolveInstanceID("cloud-run-replica-2", "localhost")
	if got != "cloud-run-replica-2" {
		t.Fatalf("resolveInstanceID() = %q, want explicit env override", got)
	}
}

func TestResolveInstanceIDIsStableWithinProcess(t *testing.T) {
	t.Parallel()
	first := ResolveInstanceID()
	second := ResolveInstanceID()
	if first == "" || second == "" {
		t.Fatal("ResolveInstanceID returned empty string")
	}
	if first != second {
		t.Fatalf("ResolveInstanceID() = %q then %q, want stable value within process", first, second)
	}
	if strings.EqualFold(first, "localhost") {
		t.Fatalf("ResolveInstanceID() = localhost, want generated identity")
	}
}
