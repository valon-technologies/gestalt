//go:build linux

package sandbox

import "testing"

func TestAllowNetworkRestrictionUnavailableRequiresExplicitEnv(t *testing.T) {
	t.Setenv(allowNetworkRestrictionUnavailableEnv, "")
	if allowNetworkRestrictionUnavailable() {
		t.Fatal("expected network restriction opt-out to default false")
	}

	t.Setenv(allowNetworkRestrictionUnavailableEnv, "1")
	if !allowNetworkRestrictionUnavailable() {
		t.Fatal("expected network restriction opt-out to accept 1")
	}

	t.Setenv(allowNetworkRestrictionUnavailableEnv, "true")
	if !allowNetworkRestrictionUnavailable() {
		t.Fatal("expected network restriction opt-out to accept true")
	}
}

func TestSandboxWrapperEnvFiltersPluginOptOut(t *testing.T) {
	t.Setenv(allowNetworkRestrictionUnavailableEnv, "")

	env := sandboxWrapperEnv([]string{
		"KEEP=value",
		allowNetworkRestrictionUnavailableEnv + "=1",
	})

	if envValue(env, allowNetworkRestrictionUnavailableEnv) != "" {
		t.Fatal("expected plugin-supplied sandbox opt-out to be filtered")
	}
	if got := envValue(env, "KEEP"); got != "value" {
		t.Fatalf("KEEP = %q, want value", got)
	}
}

func TestSandboxWrapperEnvPropagatesParentOptOut(t *testing.T) {
	t.Setenv(allowNetworkRestrictionUnavailableEnv, "true")

	env := sandboxWrapperEnv([]string{
		"KEEP=value",
		allowNetworkRestrictionUnavailableEnv + "=0",
	})

	if got := envValue(env, allowNetworkRestrictionUnavailableEnv); got != "true" {
		t.Fatalf("%s = %q, want true", allowNetworkRestrictionUnavailableEnv, got)
	}
}

func TestSandboxExecEnvRemovesOptOut(t *testing.T) {
	t.Setenv(allowNetworkRestrictionUnavailableEnv, "true")

	env := sandboxExecEnv()
	if envValue(env, allowNetworkRestrictionUnavailableEnv) != "" {
		t.Fatal("expected sandbox control env to be removed before executing plugin")
	}
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, entry := range env {
		if len(entry) > len(prefix) && entry[:len(prefix)] == prefix {
			return entry[len(prefix):]
		}
		if entry == prefix {
			return ""
		}
	}
	return ""
}
