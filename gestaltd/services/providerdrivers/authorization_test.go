package providerdrivers

import "testing"

func TestAuthorizationEnvWithCallerTokenPublicKey(t *testing.T) {
	t.Parallel()

	env := authorizationEnvWithCallerTokenPublicKey(map[string]string{"EXISTING": "value"}, "public-key-pem")

	if env["EXISTING"] != "value" {
		t.Fatalf("EXISTING = %q, want value", env["EXISTING"])
	}
	if env[CallerTokenPublicKeyEnv] != "public-key-pem" {
		t.Fatalf("%s = %q, want public-key-pem", CallerTokenPublicKeyEnv, env[CallerTokenPublicKeyEnv])
	}
}

func TestAuthorizationEnvWithCallerTokenPublicKeyPreservesConfiguredEnv(t *testing.T) {
	t.Parallel()

	env := authorizationEnvWithCallerTokenPublicKey(map[string]string{
		CallerTokenPublicKeyEnv: "configured-public-key",
	}, "secret-public-key")

	if env[CallerTokenPublicKeyEnv] != "configured-public-key" {
		t.Fatalf("%s = %q, want configured-public-key", CallerTokenPublicKeyEnv, env[CallerTokenPublicKeyEnv])
	}
}
