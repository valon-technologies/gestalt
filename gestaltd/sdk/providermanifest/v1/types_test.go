package providermanifestv1

import "testing"

func TestNormalizeKindCanonicalizesExternalCredentials(t *testing.T) {
	t.Parallel()

	for _, input := range []string{"externalcredentials", "external_credentials", "external-credentials", " External_Credentials "} {
		if got := NormalizeKind(input); got != KindExternalCredentials {
			t.Fatalf("NormalizeKind(%q) = %q, want %q", input, got, KindExternalCredentials)
		}
	}
}
