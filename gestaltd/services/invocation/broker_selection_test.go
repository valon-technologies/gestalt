package invocation

import (
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
)

func TestChosenCredentialInstance_CollapsesExplicitAccountDuplicates(t *testing.T) {
	t.Parallel()

	credentials := []*core.ExternalCredential{
		{ID: "credential-1", Qualifier: "old-label", MetadataJSON: `{"account_key":"provider:v1:shared"}`, CreatedAt: time.Unix(1, 0)},
		{ID: "credential-2", Qualifier: "new-label", MetadataJSON: `{"account_key":"provider:v1:shared"}`, CreatedAt: time.Unix(2, 0)},
	}
	if got, ok := chosenCredentialInstance(credentials, ""); !ok || got != "old-label" {
		t.Fatalf("chosen instance = %q, ok=%v, want oldest duplicate account", got, ok)
	}
}

func TestChosenCredentialInstance_PrefersUsableDuplicate(t *testing.T) {
	t.Parallel()

	credentials := []*core.ExternalCredential{
		{
			ID:           "credential-1",
			Qualifier:    "preferred-label",
			MetadataJSON: `{"account_key":"provider:v1:shared"}`,
			Grant:        &core.ExternalCredentialGrant{RefreshErrorCount: 1, ExpiresAt: timePtr(time.Now().Add(-time.Minute))},
		},
		{ID: "credential-2", Qualifier: "usable-label", MetadataJSON: `{"account_key":"provider:v1:shared"}`},
	}
	if got, ok := chosenCredentialInstance(credentials, "preferred-label"); !ok || got != "usable-label" {
		t.Fatalf("chosen instance = %q, ok=%v, want usable duplicate account", got, ok)
	}
}

func TestChosenCredentialInstance_KeepsKeylessCredentialsAmbiguous(t *testing.T) {
	t.Parallel()

	credentials := []*core.ExternalCredential{
		{ID: "credential-1", Qualifier: "first"},
		{ID: "credential-2", Qualifier: "second"},
	}
	if got, ok := chosenCredentialInstance(credentials, ""); ok || got != "" {
		t.Fatalf("chosen instance = %q, ok=%v, want ambiguous keyless credentials", got, ok)
	}
}

func timePtr(value time.Time) *time.Time {
	return &value
}
