package server

import (
	"context"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
)

func TestDedupeInstancesByAccount_PreservesDistinctAccounts(t *testing.T) {
	t.Parallel()
	instances := []instanceInfo{
		{Name: "Valon", AccountKey: "v1:shared", credentialCreated: time.Unix(1, 0)},
		{Name: "Personal", AccountKey: "v1:shared", credentialCreated: time.Unix(2, 0)},
		{Name: "Other", AccountKey: "v1:other", credentialCreated: time.Unix(3, 0)},
		{Name: "Legacy A", credentialCreated: time.Unix(4, 0)},
		{Name: "Legacy B", credentialCreated: time.Unix(5, 0)},
	}

	got := dedupeInstancesByAccount(instances, "Personal")
	if len(got) != 4 {
		t.Fatalf("instances = %+v, want one shared account removed", got)
	}
	if got[0].Name != "Personal" {
		t.Fatalf("shared account winner = %q, want preferred instance", got[0].Name)
	}
}

func TestStoreCredentialFromMaterial_CollapsesCanonicalDuplicates(t *testing.T) {
	t.Parallel()
	provider := coretesting.NewStubExternalCredentialProvider()
	ctx := context.Background()
	identity := &accountIdentity{Facts: []identityFact{
		{Kind: "workspace", Value: "Valon"},
		{Kind: "login", Value: "giovannivocale"},
	}}
	metadata, err := setAccountIdentity("", identity)
	if err != nil {
		t.Fatal(err)
	}
	seed := func(id, qualifier string, created time.Time) {
		t.Helper()
		if err := provider.UpsertCredential(ctx, &core.ExternalCredential{
			ID:           id,
			Subject:      "user:1",
			Audience:     "slack:default",
			Qualifier:    qualifier,
			MetadataJSON: metadata,
			Grant:        &core.ExternalCredentialGrant{AccessToken: id},
			CreatedAt:    created,
		}); err != nil {
			t.Fatal(err)
		}
	}
	seed("credential-1", "Valon", time.Unix(1, 0))
	seed("credential-2", "Personal", time.Unix(2, 0))

	s := &Server{externalCredentials: provider, now: func() time.Time { return time.Unix(3, 0) }}
	stored, err := s.storeCredentialFromMaterial(ctx, credentialMaterial{
		SubjectID:    "user:1",
		ConnectionID: "slack:default",
		Instance:     "new-label",
		MetadataJSON: metadata,
		AccessToken:  "fresh-token",
	})
	if err != nil {
		t.Fatalf("store credential: %v", err)
	}
	if stored.ID != "credential-1" || stored.Qualifier != "Valon" {
		t.Fatalf("stored = %+v, want oldest canonical credential retained", stored)
	}
	credentials, err := provider.ListCredentials(ctx, "user:1", "slack:default")
	if err != nil {
		t.Fatal(err)
	}
	if len(credentials) != 1 || credentials[0].ID != "credential-1" || credentials[0].Grant.AccessToken != "fresh-token" {
		t.Fatalf("credentials = %+v, want one refreshed canonical credential", credentials)
	}
}
