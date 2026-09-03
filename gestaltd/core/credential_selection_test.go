package core

import (
	"testing"
	"time"
)

func TestChooseCredentialInstanceGroupsExplicitAccountDuplicates(t *testing.T) {
	t.Parallel()

	now := time.Unix(10, 0)
	credentials := []*ExternalCredential{
		{ID: "credential-new", Qualifier: "new-label", MetadataJSON: `{"account_key":"provider:v1:shared"}`, CreatedAt: time.Unix(2, 0)},
		{ID: "credential-old", Qualifier: "old-label", MetadataJSON: `{"account_key":"provider:v1:shared"}`, CreatedAt: time.Unix(1, 0)},
	}

	if got, ok := ChooseCredentialInstance(credentials, "", now); !ok || got != "old-label" {
		t.Fatalf("chosen instance = %q, ok=%v, want oldest duplicate account", got, ok)
	}
}

func TestChooseCredentialInstancePrefersUsableCredential(t *testing.T) {
	t.Parallel()

	now := time.Unix(10, 0)
	expires := time.Unix(9, 0)
	credentials := []*ExternalCredential{
		{
			ID:           "credential-preferred",
			Qualifier:    "preferred-label",
			MetadataJSON: `{"account_key":"provider:v1:shared"}`,
			Grant:        &ExternalCredentialGrant{ExpiresAt: &expires, RefreshErrorCount: 1},
		},
		{
			ID:           "credential-usable",
			Qualifier:    "usable-label",
			MetadataJSON: `{"account_key":"provider:v1:shared"}`,
		},
	}

	if got, ok := ChooseCredentialInstance(credentials, "preferred-label", now); !ok || got != "usable-label" {
		t.Fatalf("chosen instance = %q, ok=%v, want usable duplicate account", got, ok)
	}
}

func TestChooseCredentialInstanceSkipsInvalidSiblingWhenOneUsableAccountRemains(t *testing.T) {
	t.Parallel()

	now := time.Unix(10, 0)
	expires := time.Unix(9, 0)
	credentials := []*ExternalCredential{
		{
			ID:           "credential-invalid",
			Qualifier:    "dead-label",
			MetadataJSON: `{"account_key":"provider:v1:dead"}`,
			Grant:        &ExternalCredentialGrant{ExpiresAt: &expires, RefreshErrorCount: 1},
		},
		{
			ID:           "credential-usable",
			Qualifier:    "healthy-label",
			MetadataJSON: `{"account_key":"provider:v1:healthy"}`,
		},
	}

	if got, ok := ChooseCredentialInstance(credentials, "dead-label", now); !ok || got != "healthy-label" {
		t.Fatalf("chosen instance = %q, ok=%v, want sole usable account", got, ok)
	}
}

func TestChooseCredentialInstanceDoesNotTreatEmptyPreferenceAsSelection(t *testing.T) {
	t.Parallel()

	credentials := []*ExternalCredential{
		{ID: "credential-labeled", Qualifier: "labeled", MetadataJSON: `{"account_key":"provider:v1:shared"}`, CreatedAt: time.Unix(1, 0)},
		{ID: "credential-empty", MetadataJSON: `{"account_key":"provider:v1:shared"}`, CreatedAt: time.Unix(2, 0)},
	}

	if got, ok := ChooseCredentialInstance(credentials, "", time.Unix(10, 0)); !ok || got != "labeled" {
		t.Fatalf("chosen instance = %q, ok=%v, want oldest labeled credential", got, ok)
	}
}

func TestChooseCredentialInstanceKeepsKeylessCredentialsAmbiguous(t *testing.T) {
	t.Parallel()

	credentials := []*ExternalCredential{
		{ID: "credential-a", Qualifier: "first-label"},
		{ID: "credential-b", Qualifier: "second-label"},
	}

	if got, ok := ChooseCredentialInstance(credentials, "", time.Now()); ok || got != "" {
		t.Fatalf("chosen instance = %q, ok=%v, want ambiguous keyless credentials", got, ok)
	}
}
