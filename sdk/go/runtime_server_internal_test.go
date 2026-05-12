package gestalt

import (
	"testing"

	proto "github.com/valon-technologies/gestalt/sdk/go/internal/gen/v1"
)

func TestProviderKindToProtoAcceptsExternalCredentialsAliases(t *testing.T) {
	t.Parallel()

	for _, kind := range []ProviderKind{ProviderKindExternalCredential, ProviderKindExternalCredentialLegacy} {
		if got := providerKindToProto(kind); got != proto.ProviderKind_PROVIDER_KIND_EXTERNAL_CREDENTIAL {
			t.Fatalf("providerKindToProto(%q) = %v, want %v", kind, got, proto.ProviderKind_PROVIDER_KIND_EXTERNAL_CREDENTIAL)
		}
	}
}
