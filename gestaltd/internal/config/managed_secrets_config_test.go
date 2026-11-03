package config

import "testing"

func TestValidateManagedSecretsConfig(t *testing.T) {
	ok := "projects/p/locations/us-east1/keyRings/r/cryptoKeys/k"
	for _, key := range []string{"", ok} {
		if err := validateManagedSecretsConfig(ManagedSecretsConfig{KMSKey: key}); err != nil {
			t.Fatalf("%q: %v", key, err)
		}
	}
	for _, key := range []string{"nope", ok + "/cryptoKeyVersions/1"} {
		if err := validateManagedSecretsConfig(ManagedSecretsConfig{KMSKey: key}); err == nil {
			t.Fatalf("%q should be rejected", key)
		}
	}
}
