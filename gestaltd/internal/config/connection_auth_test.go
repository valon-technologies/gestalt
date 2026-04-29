package config

import (
	"testing"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func TestMergeConnectionAuthExplicitEmptyCredentialsClearsInherited(t *testing.T) {
	t.Parallel()

	dst := &ConnectionAuthDef{
		Type: providermanifestv1.AuthTypeManual,
		Credentials: []CredentialFieldDef{
			{Name: "api_key"},
			{Name: "api_secret"},
		},
	}

	MergeConnectionAuth(dst, ConnectionAuthDef{
		Type:        providermanifestv1.AuthTypeManual,
		Credentials: []CredentialFieldDef{},
	})

	if len(dst.Credentials) != 0 {
		t.Fatalf("Credentials len = %d, want 0: %#v", len(dst.Credentials), dst.Credentials)
	}
}
