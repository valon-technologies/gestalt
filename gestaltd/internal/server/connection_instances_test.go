package server

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestDedupeInstancesByAccount_PreservesDistinctAccounts(t *testing.T) {
	t.Parallel()
	instances := []instanceInfo{
		{Name: "Example Workspace", AccountKey: "v1:shared", credentialCreated: time.Unix(1, 0)},
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

func TestDedupeInstancesByAccount_PrefersUsableCredentialOverInvalidPreference(t *testing.T) {
	t.Parallel()
	instances := []instanceInfo{
		{Name: "Preferred label", AccountKey: "v1:shared", credentialInvalid: true},
		{Name: "Usable label", AccountKey: "v1:shared"},
	}

	got := dedupeInstancesByAccount(instances, "Preferred label")
	if len(got) != 1 || got[0].Name != "Usable label" {
		t.Fatalf("instances = %+v, want usable duplicate retained", got)
	}
}

func TestStoreCredentialFromMaterial_SerializesCanonicalReconciliation(t *testing.T) {
	t.Parallel()
	provider := coretesting.NewStubExternalCredentialProvider()
	ctx := context.Background()
	metadata, err := setAccountKey(`{"account_identity":"{\"facts\":[{\"kind\":\"login\",\"value\":\"example-user\"}]}"}`, accountKeyFromProviderID("slack", "T123:U456"))
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		externalCredentials: provider,
		now:                 func() time.Time { return time.Unix(3, 0) },
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, token := range []string{"token-a", "token-b"} {
		wg.Add(1)
		go func(token string) {
			defer wg.Done()
			_, err := s.storeCredentialFromMaterial(ctx, credentialMaterial{
				SubjectID:    "user:1",
				ConnectionID: "slack:default",
				Instance:     token,
				MetadataJSON: metadata,
				AccessToken:  token,
			})
			errs <- err
		}(token)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("store credential: %v", err)
		}
	}
	credentials, err := provider.ListCredentials(ctx, "user:1", "slack:default")
	if err != nil {
		t.Fatal(err)
	}
	if len(credentials) != 1 {
		t.Fatalf("credentials = %+v, want one credential after concurrent reconnects", credentials)
	}
}

func TestStoreCredentialFromMaterial_ConvergesAcrossServerInstances(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	provider := coretesting.NewStubExternalCredentialProvider()
	metadata, err := setAccountKey(`{"account_identity":"{\"facts\":[{\"kind\":\"login\",\"value\":\"example-user\"}]}"}`, accountKeyFromProviderID("slack", "T123:U456"))
	if err != nil {
		t.Fatal(err)
	}
	servers := []*Server{
		{externalCredentials: provider, now: func() time.Time { return time.Unix(3, 0) }},
		{externalCredentials: provider, now: func() time.Time { return time.Unix(3, 0) }},
	}
	var wg sync.WaitGroup
	errs := make(chan error, len(servers))
	for i, s := range servers {
		wg.Add(1)
		go func(i int, s *Server) {
			defer wg.Done()
			_, err := s.storeCredentialFromMaterial(ctx, credentialMaterial{
				SubjectID:    "user:1",
				ConnectionID: "slack:default",
				Instance:     []string{"label-a", "label-b"}[i],
				MetadataJSON: metadata,
				AccessToken:  []string{"token-a", "token-b"}[i],
			})
			errs <- err
		}(i, s)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("store credential: %v", err)
		}
	}
	credentials, err := provider.ListCredentials(ctx, "user:1", "slack:default")
	if err != nil {
		t.Fatal(err)
	}
	if len(credentials) != 1 {
		t.Fatalf("credentials = %+v, want one credential after cross-server reconciliation", credentials)
	}
}

func TestStoreCredentialFromMaterial_CollapsesCanonicalDuplicates(t *testing.T) {
	t.Parallel()
	provider := coretesting.NewStubExternalCredentialProvider()
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
	seed("credential-1", "Example Workspace", time.Unix(1, 0))
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
	if stored.ID != "credential-1" || stored.Qualifier != "Example Workspace" {
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

func TestReconcileCanonicalAccountCredentialDeletesPersistedCandidateWhenCanonicalChanges(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	provider := coretesting.NewStubExternalCredentialProvider()
	metadata, err := setAccountKey("", accountKeyFromProviderID("slack", "T123:U456"))
	if err != nil {
		t.Fatal(err)
	}
	for _, credential := range []*core.ExternalCredential{
		{
			ID:           "credential-old",
			Subject:      "user:1",
			Audience:     "slack:default",
			Qualifier:    "old-label",
			MetadataJSON: metadata,
			Grant:        &core.ExternalCredentialGrant{AccessToken: "old-token"},
			CreatedAt:    time.Unix(1, 0),
		},
		{
			ID:           "credential-new",
			Subject:      "user:1",
			Audience:     "slack:default",
			Qualifier:    "new-label",
			MetadataJSON: metadata,
			Grant:        &core.ExternalCredentialGrant{AccessToken: "new-token"},
			CreatedAt:    time.Unix(2, 0),
		},
	} {
		if err := provider.UpsertCredential(ctx, credential); err != nil {
			t.Fatal(err)
		}
	}

	candidate := &core.ExternalCredential{
		ID:           "credential-new",
		Subject:      "user:1",
		Audience:     "slack:default",
		Qualifier:    "new-label",
		MetadataJSON: metadata,
		Grant:        &core.ExternalCredentialGrant{AccessToken: "new-token"},
		CreatedAt:    time.Unix(2, 0),
	}
	s := &Server{externalCredentials: provider, now: func() time.Time { return time.Unix(3, 0) }}
	duplicates, changed, err := s.reconcileCanonicalAccountCredential(ctx, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || candidate.ID != "credential-old" {
		t.Fatalf("candidate = %+v, changed=%v, want old canonical credential", candidate, changed)
	}
	if len(duplicates) != 1 || duplicates[0] != "credential-new" {
		t.Fatalf("duplicates = %v, want persisted candidate ID", duplicates)
	}
	s.deleteCanonicalDuplicates(ctx, candidate, duplicates)

	credentials, err := provider.ListCredentials(ctx, "user:1", "slack:default")
	if err != nil {
		t.Fatal(err)
	}
	if len(credentials) != 1 || credentials[0].ID != "credential-old" {
		t.Fatalf("credentials = %+v, want only canonical credential", credentials)
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

func TestStoreCredentialFromMaterial_RetainsUnprovenLegacyCredentials(t *testing.T) {
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
	differentMetadata, err := setAccountKey(legacyMetadata, accountKeyFromProviderID("slack", "T999:U999"))
	if err != nil {
		t.Fatal(err)
	}
	for _, credential := range []*core.ExternalCredential{
		{
			ID:           "legacy-credential-a",
			Subject:      "user:1",
			Audience:     "slack:default",
			Qualifier:    "legacy-label-a",
			MetadataJSON: legacyMetadata,
			Grant:        &core.ExternalCredentialGrant{AccessToken: "old-token-a"},
			CreatedAt:    time.Unix(1, 0),
		},
		{
			ID:           "legacy-credential-b",
			Subject:      "user:1",
			Audience:     "slack:default",
			Qualifier:    "legacy-label-b",
			MetadataJSON: legacyMetadata,
			Grant:        &core.ExternalCredentialGrant{AccessToken: "old-token-b"},
			CreatedAt:    time.Unix(2, 0),
		},
		{
			ID:           "different-account",
			Subject:      "user:1",
			Audience:     "slack:default",
			Qualifier:    "different-label",
			MetadataJSON: differentMetadata,
			Grant:        &core.ExternalCredentialGrant{AccessToken: "different-token"},
			CreatedAt:    time.Unix(3, 0),
		},
	} {
		if err := provider.UpsertCredential(ctx, credential); err != nil {
			t.Fatal(err)
		}
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
	if stored.ID == "legacy-credential-a" || stored.ID == "legacy-credential-b" || stored.ID == "different-account" {
		t.Fatalf("stored id = %q, must not reuse an unproven credential", stored.ID)
	}
	credentials, err := provider.ListCredentials(ctx, "user:1", "slack:default")
	if err != nil {
		t.Fatal(err)
	}
	if len(credentials) != 4 {
		t.Fatalf("credentials = %+v, want new credential plus all unproven records", credentials)
	}
	byID := make(map[string]*core.ExternalCredential, len(credentials))
	for _, credential := range credentials {
		byID[credential.ID] = credential
	}
	if byID[stored.ID] == nil || byID[stored.ID].MetadataJSON != candidateMetadata || byID[stored.ID].Grant.AccessToken != "new-token" {
		t.Fatalf("stored credentials = %+v, want new explicit-key credential", credentials)
	}
	for _, id := range []string{"legacy-credential-a", "legacy-credential-b", "different-account"} {
		if byID[id] == nil {
			t.Fatalf("credential %q was removed despite lacking a proven matching account key", id)
		}
	}
}

func TestStoreCredentialFromMaterial_ReportsSuccessWhenDuplicateCleanupFails(t *testing.T) {
	t.Parallel()
	provider := coretesting.NewStubExternalCredentialProvider()
	ctx := context.Background()
	metadata, err := setAccountKey(`{"account_identity":"{\"facts\":[{\"kind\":\"login\",\"value\":\"example-user\"}]}"}`, accountKeyFromProviderID("slack", "T123:U456"))
	if err != nil {
		t.Fatal(err)
	}
	for _, credential := range []*core.ExternalCredential{
		{
			ID:           "credential-1",
			Subject:      "user:1",
			Audience:     "slack:default",
			Qualifier:    "first",
			MetadataJSON: metadata,
			Grant:        &core.ExternalCredentialGrant{AccessToken: "old-token"},
			CreatedAt:    time.Unix(1, 0),
		},
		{
			ID:           "credential-2",
			Subject:      "user:1",
			Audience:     "slack:default",
			Qualifier:    "second",
			MetadataJSON: metadata,
			Grant:        &core.ExternalCredentialGrant{AccessToken: "duplicate-token"},
			CreatedAt:    time.Unix(2, 0),
		},
	} {
		if err := provider.UpsertCredential(ctx, credential); err != nil {
			t.Fatal(err)
		}
	}
	provider.DeleteErr = errors.New("cleanup unavailable")

	s := &Server{externalCredentials: provider, now: func() time.Time { return time.Unix(3, 0) }}
	if _, err := s.storeCredentialFromMaterial(ctx, credentialMaterial{
		SubjectID:    "user:1",
		ConnectionID: "slack:default",
		Instance:     "new-label",
		MetadataJSON: metadata,
		AccessToken:  "fresh-token",
	}); err != nil {
		t.Fatalf("store credential = %v, want successful save despite deferred cleanup", err)
	}
	credentials, err := provider.ListCredentials(ctx, "user:1", "slack:default")
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]*core.ExternalCredential, len(credentials))
	for _, credential := range credentials {
		byID[credential.ID] = credential
	}
	if len(credentials) != 2 || byID["credential-1"] == nil || byID["credential-1"].Grant == nil || byID["credential-1"].Grant.AccessToken != "fresh-token" || byID["credential-2"] == nil {
		t.Fatalf("credentials = %+v, want refreshed credential plus retained duplicate until cleanup retry", credentials)
	}
}
