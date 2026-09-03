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

func TestStoreCredentialFromMaterial_UsesProviderUniquenessForConcurrentReconnects(t *testing.T) {
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
	if len(credentials) != 2 {
		t.Fatalf("credentials = %+v, want one record per instance", credentials)
	}
	if accounts := core.GroupCredentialAccounts(credentials, "", time.Unix(3, 0)); len(accounts) != 1 {
		t.Fatalf("logical accounts = %+v, want one account projection", accounts)
	}
}

func TestStoreCredentialFromMaterial_IsSafeAcrossServerInstances(t *testing.T) {
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
	if len(credentials) != 2 {
		t.Fatalf("credentials = %+v, want both provider-owned instance records", credentials)
	}
}

func TestStoreCredentialFromMaterial_PreservesAccountInstances(t *testing.T) {
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
	if stored.Qualifier != "new-label" || stored.ID == "credential-1" || stored.ID == "credential-2" {
		t.Fatalf("stored = %+v, want a new record for the new instance", stored)
	}
	credentials, err := provider.ListCredentials(ctx, "user:1", "slack:default")
	if err != nil {
		t.Fatal(err)
	}
	if len(credentials) != 3 {
		t.Fatalf("credentials = %+v, want all account instances retained", credentials)
	}
	if accounts := core.GroupCredentialAccounts(credentials, "", time.Unix(3, 0)); len(accounts) != 1 {
		t.Fatalf("logical accounts = %+v, want one account projection", accounts)
	}
}

func TestStoreCredentialFromMaterial_PreferenceSelectsProjectionOnly(t *testing.T) {
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
	if stored.Qualifier != "new-label" || stored.ID == "credential-1" || stored.ID == "credential-2" {
		t.Fatalf("stored = %+v, want a new record for the new instance", stored)
	}
	credentials, err := provider.ListCredentials(ctx, "user:1", "slack:default")
	if err != nil {
		t.Fatal(err)
	}
	if len(credentials) != 3 {
		t.Fatalf("credentials = %+v, want all account instances retained", credentials)
	}
	accounts := core.GroupCredentialAccounts(credentials, "Preferred label", time.Unix(3, 0))
	if len(accounts) != 1 || accounts[0].ID != "credential-2" {
		t.Fatalf("logical accounts = %+v, want preferred instance selected in projection", accounts)
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

func TestStoreCredentialFromMaterial_DoesNotOverwriteDifferentAccountWithSameInstance(t *testing.T) {
	t.Parallel()
	provider := coretesting.NewStubExternalCredentialProvider()
	ctx := context.Background()
	existingMetadata, err := setAccountKey("", accountKeyFromProviderID("slack", "T123:U123"))
	if err != nil {
		t.Fatal(err)
	}
	candidateMetadata, err := setAccountKey("", accountKeyFromProviderID("slack", "T456:U456"))
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.UpsertCredential(ctx, &core.ExternalCredential{
		ID:           "existing-account",
		Subject:      "user:1",
		Audience:     "slack:default",
		Qualifier:    "shared-label",
		MetadataJSON: existingMetadata,
		Grant:        &core.ExternalCredentialGrant{AccessToken: "existing-token"},
		CreatedAt:    time.Unix(1, 0),
	}); err != nil {
		t.Fatal(err)
	}

	s := &Server{externalCredentials: provider, now: func() time.Time { return time.Unix(2, 0) }}
	if _, err := s.storeCredentialFromMaterial(ctx, credentialMaterial{
		SubjectID:    "user:1",
		ConnectionID: "slack:default",
		Instance:     "shared-label",
		MetadataJSON: candidateMetadata,
		AccessToken:  "candidate-token",
	}); !errors.Is(err, core.ErrAlreadyExists) {
		t.Fatalf("store credential error = %v, want account-instance conflict", err)
	}

	credentials, err := provider.ListCredentials(ctx, "user:1", "slack:default")
	if err != nil {
		t.Fatal(err)
	}
	if len(credentials) != 1 || credentials[0].ID != "existing-account" || credentials[0].Grant.AccessToken != "existing-token" {
		t.Fatalf("credentials = %+v, want existing account preserved", credentials)
	}
}

func TestStoreCredentialFromMaterial_UpgradesKeylessCredentialForSameInstance(t *testing.T) {
	t.Parallel()
	provider := coretesting.NewStubExternalCredentialProvider()
	ctx := context.Background()
	legacyMetadata, err := setAccountIdentity("", &accountIdentity{Facts: []identityFact{
		{Kind: "workspace", Value: "Example Workspace"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	candidateMetadata, err := setAccountKey(legacyMetadata, accountKeyFromProviderID("slack", "T123:U456"))
	if err != nil {
		t.Fatal(err)
	}
	expiresAt := time.Unix(1, 0)
	if err := provider.UpsertCredential(ctx, &core.ExternalCredential{
		ID:           "legacy-credential",
		Subject:      "user:1",
		Audience:     "slack:default",
		Qualifier:    "shared-label",
		MetadataJSON: legacyMetadata,
		Grant:        &core.ExternalCredentialGrant{AccessToken: "expired-token", ExpiresAt: &expiresAt, RefreshErrorCount: 1},
		CreatedAt:    time.Unix(1, 0),
	}); err != nil {
		t.Fatal(err)
	}

	s := &Server{externalCredentials: provider, now: func() time.Time { return time.Unix(2, 0) }}
	stored, err := s.storeCredentialFromMaterial(ctx, credentialMaterial{
		SubjectID:    "user:1",
		ConnectionID: "slack:default",
		Instance:     "shared-label",
		MetadataJSON: candidateMetadata,
		AccessToken:  "fresh-token",
	})
	if err != nil {
		t.Fatalf("store credential error = %v, want legacy instance upgrade", err)
	}
	if stored.ID != "legacy-credential" {
		t.Fatalf("stored id = %q, want legacy row retained during upgrade", stored.ID)
	}
	credentials, err := provider.ListCredentials(ctx, "user:1", "slack:default")
	if err != nil {
		t.Fatal(err)
	}
	if len(credentials) != 1 || credentials[0].ID != "legacy-credential" || credentials[0].AccountKey != accountKeyFromProviderID("slack", "T123:U456") || accountKeyStoredInMetadataJSON(credentials[0].MetadataJSON) != "" || credentials[0].Grant.AccessToken != "fresh-token" {
		t.Fatalf("credentials = %+v, want upgraded legacy credential", credentials)
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
	if byID[stored.ID] == nil || byID[stored.ID].AccountKey != accountKeyFromProviderID("slack", "T123:U456") || accountKeyStoredInMetadataJSON(byID[stored.ID].MetadataJSON) != "" || byID[stored.ID].Grant.AccessToken != "new-token" {
		t.Fatalf("stored credentials = %+v, want new explicit-key credential", credentials)
	}
	for _, id := range []string{"legacy-credential-a", "legacy-credential-b", "different-account"} {
		if byID[id] == nil {
			t.Fatalf("credential %q was removed despite lacking a proven matching account key", id)
		}
	}
}

func TestStoreCredentialFromMaterial_DoesNotNeedDuplicateCleanup(t *testing.T) {
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
	if len(credentials) != 3 {
		t.Fatalf("credentials = %+v, want all account instances retained", credentials)
	}
	if accounts := core.GroupCredentialAccounts(credentials, "", time.Unix(3, 0)); len(accounts) != 1 {
		t.Fatalf("logical accounts = %+v, want one account projection", accounts)
	}
}
