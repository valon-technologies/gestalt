package server

import (
	"context"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
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

func TestDedupeInstancesByAccount_PreservesKeylessAccountsWithSameName(t *testing.T) {
	t.Parallel()
	instances := []instanceInfo{
		{Name: "Shared label"},
		{Name: "Shared label"},
	}

	got := dedupeInstancesByAccount(instances, "")
	if len(got) != len(instances) {
		t.Fatalf("instances = %+v, want both keyless accounts retained", got)
	}
}

func TestStoreCredentialFromMaterial_CollapsesCanonicalDuplicates(t *testing.T) {
	t.Parallel()
	provider := coretesting.NewStubExternalCredentialProvider()
	ctx := context.Background()
	identity := &accountIdentity{Facts: []identityFact{
		{Kind: "workspace", Value: "Valon"},
		{Kind: "login", Value: "example-user"},
	}}
	metadata, err := setAccountIdentity("", identity)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err = setAccountKey(metadata, accountKeyFromProviderID("slack", "T123:U456"))
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

func TestStoreCredentialFromMaterial_RetainsPreferredCanonicalDuplicate(t *testing.T) {
	t.Parallel()
	provider := coretesting.NewStubExternalCredentialProvider()
	services := testutil.NewStubServices(t)
	ctx := context.Background()
	identity := &accountIdentity{Facts: []identityFact{
		{Kind: "workspace", Value: "Example Workspace"},
		{Kind: "login", Value: "example-user"},
	}}
	metadata, err := setAccountIdentity("", identity)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err = setAccountKey(metadata, accountKeyFromProviderID("slack", "T123:U456"))
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
	seed("credential-1", "Old label", time.Unix(1, 0))
	seed("credential-2", "Preferred label", time.Unix(2, 0))
	if _, err := services.ConnectionInstancePreferences.Set(ctx, "user:1", "slack:default", "Preferred label"); err != nil {
		t.Fatalf("set preference: %v", err)
	}

	s := &Server{
		externalCredentials:           provider,
		connectionInstancePreferences: services.ConnectionInstancePreferences,
		now:                           func() time.Time { return time.Unix(3, 0) },
	}
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
	if stored.ID != "credential-2" || stored.Qualifier != "Preferred label" {
		t.Fatalf("stored = %+v, want preferred canonical credential retained", stored)
	}
	credentials, err := provider.ListCredentials(ctx, "user:1", "slack:default")
	if err != nil {
		t.Fatal(err)
	}
	if len(credentials) != 1 || credentials[0].ID != "credential-2" || credentials[0].Grant.AccessToken != "fresh-token" {
		t.Fatalf("credentials = %+v, want preferred credential refreshed and old duplicate removed", credentials)
	}
}

func TestStoreCredentialFromMaterial_DoesNotCollapseWithoutProviderKey(t *testing.T) {
	t.Parallel()
	provider := coretesting.NewStubExternalCredentialProvider()
	ctx := context.Background()
	metadata, err := setAccountIdentity("", &accountIdentity{Facts: []identityFact{
		{Kind: "email", Value: "shared@example.com"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.UpsertCredential(ctx, &core.ExternalCredential{
		ID:           "legacy-credential",
		Subject:      "user:1",
		Audience:     "provider:default",
		Qualifier:    "existing",
		MetadataJSON: metadata,
		Grant:        &core.ExternalCredentialGrant{AccessToken: "old-token"},
		CreatedAt:    time.Unix(1, 0),
	}); err != nil {
		t.Fatal(err)
	}

	s := &Server{externalCredentials: provider, now: func() time.Time { return time.Unix(2, 0) }}
	if _, err := s.storeCredentialFromMaterial(ctx, credentialMaterial{
		SubjectID:    "user:1",
		ConnectionID: "provider:default",
		Instance:     "new-label",
		MetadataJSON: metadata,
		AccessToken:  "new-token",
	}); err != nil {
		t.Fatalf("store credential: %v", err)
	}
	credentials, err := provider.ListCredentials(ctx, "user:1", "provider:default")
	if err != nil {
		t.Fatal(err)
	}
	if len(credentials) != 2 {
		t.Fatalf("credentials = %+v, want keyless credentials to remain distinct", credentials)
	}
}

func TestStoreCredentialFromMaterial_MigratesMatchingLegacyCredential(t *testing.T) {
	t.Parallel()
	provider := coretesting.NewStubExternalCredentialProvider()
	ctx := context.Background()
	legacyMetadata, err := setAccountIdentity("", &accountIdentity{Facts: []identityFact{
		{Kind: "workspace", Value: "Example Workspace"},
		{Kind: "login", Value: "example-user"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	candidateMetadata, err := setAccountKey(legacyMetadata, accountKeyFromProviderID("slack", "T123:U456"))
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.UpsertCredential(ctx, &core.ExternalCredential{
		ID:           "legacy-credential",
		Subject:      "user:1",
		Audience:     "slack:default",
		Qualifier:    "legacy-label",
		MetadataJSON: legacyMetadata,
		Grant:        &core.ExternalCredentialGrant{AccessToken: "old-token"},
		CreatedAt:    time.Unix(1, 0),
	}); err != nil {
		t.Fatal(err)
	}

	s := &Server{externalCredentials: provider, now: func() time.Time { return time.Unix(2, 0) }}
	stored, err := s.storeCredentialFromMaterial(ctx, credentialMaterial{
		SubjectID:    "user:1",
		ConnectionID: "slack:default",
		Instance:     "new-label",
		MetadataJSON: candidateMetadata,
		AccessToken:  "new-token",
	})
	if err != nil {
		t.Fatalf("store credential: %v", err)
	}
	if stored.ID != "legacy-credential" {
		t.Fatalf("stored id = %q, want legacy credential upgraded in place", stored.ID)
	}
	credentials, err := provider.ListCredentials(ctx, "user:1", "slack:default")
	if err != nil {
		t.Fatal(err)
	}
	if len(credentials) != 1 || credentials[0].MetadataJSON != candidateMetadata || credentials[0].Grant.AccessToken != "new-token" {
		t.Fatalf("credentials = %+v, want one upgraded legacy credential", credentials)
	}
}
