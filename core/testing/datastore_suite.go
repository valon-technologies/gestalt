package coretesting

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/core"
)

// RunDatastoreTests validates a Datastore implementation against the full
// interface contract. The factory must return a freshly-migrated store.
func RunDatastoreTests(t *testing.T, newStore func(t *testing.T) core.Datastore) {
	t.Run("Migrate", func(t *testing.T) {
		testDatastoreMigrate(t, newStore)
	})
	t.Run("Users", func(t *testing.T) {
		testDatastoreUsers(t, newStore)
	})
	t.Run("IntegrationTokens", func(t *testing.T) {
		testDatastoreIntegrationTokens(t, newStore)
	})
	t.Run("APITokens", func(t *testing.T) {
		testDatastoreAPITokens(t, newStore)
	})
	t.Run("StagedConnections", func(t *testing.T) {
		testDatastoreStagedConnections(t, newStore)
	})
	t.Run("EgressClients", func(t *testing.T) {
		testDatastoreEgressClients(t, newStore)
	})
	t.Run("EgressClientTokens", func(t *testing.T) {
		testDatastoreEgressClientTokens(t, newStore)
	})
}

func mustCreateUser(t *testing.T, ctx context.Context, ds core.Datastore, email string) *core.User {
	t.Helper()
	user, err := ds.FindOrCreateUser(ctx, email)
	if err != nil {
		t.Fatalf("FindOrCreateUser(%q): %v", email, err)
	}
	return user
}

func mustStoreToken(t *testing.T, ctx context.Context, ds core.Datastore, token *core.IntegrationToken) {
	t.Helper()
	if err := ds.StoreToken(ctx, token); err != nil {
		t.Fatalf("StoreToken(%q): %v", token.ID, err)
	}
}

func mustStoreAPIToken(t *testing.T, ctx context.Context, ds core.Datastore, token *core.APIToken) {
	t.Helper()
	if err := ds.StoreAPIToken(ctx, token); err != nil {
		t.Fatalf("StoreAPIToken(%q): %v", token.ID, err)
	}
}

func testDatastoreMigrate(t *testing.T, newStore func(t *testing.T) core.Datastore) {
	t.Run("idempotent", func(t *testing.T) {
		ds := newStore(t)
		t.Cleanup(func() { ds.Close() })
		ctx := context.Background()

		// newStore already called Migrate once; calling again should be safe.
		if err := ds.Migrate(ctx); err != nil {
			t.Fatalf("second Migrate: %v", err)
		}
	})
}

func testDatastoreUsers(t *testing.T, newStore func(t *testing.T) core.Datastore) {
	t.Run("get user by id", func(t *testing.T) {
		ds := newStore(t)
		t.Cleanup(func() { ds.Close() })
		ctx := context.Background()
		user := mustCreateUser(t, ctx, ds, "getuser@example.com")
		got, err := ds.GetUser(ctx, user.ID)
		if err != nil {
			t.Fatalf("GetUser: %v", err)
		}
		if got == nil {
			t.Fatal("GetUser returned nil")
		}
		if got.Email != "getuser@example.com" {
			t.Errorf("Email: got %q, want %q", got.Email, "getuser@example.com")
		}
	})

	t.Run("get user not found", func(t *testing.T) {
		ds := newStore(t)
		t.Cleanup(func() { ds.Close() })
		ctx := context.Background()
		_, err := ds.GetUser(ctx, "nonexistent-id")
		if err == nil {
			t.Fatal("expected error for nonexistent user")
		}
	})

	t.Run("creates a new user", func(t *testing.T) {
		ds := newStore(t)
		t.Cleanup(func() { ds.Close() })
		ctx := context.Background()

		user, err := ds.FindOrCreateUser(ctx, "alice@example.com")
		if err != nil {
			t.Fatalf("FindOrCreateUser: %v", err)
		}
		if user == nil {
			t.Fatal("FindOrCreateUser returned nil user")
		}
		if user.Email != "alice@example.com" {
			t.Errorf("user.Email: got %q, want %q", user.Email, "alice@example.com")
		}
		if user.ID == "" {
			t.Error("user.ID should not be empty")
		}
	})

	t.Run("is idempotent", func(t *testing.T) {
		ds := newStore(t)
		t.Cleanup(func() { ds.Close() })
		ctx := context.Background()

		u1, err := ds.FindOrCreateUser(ctx, "bob@example.com")
		if err != nil {
			t.Fatalf("first FindOrCreateUser: %v", err)
		}

		u2, err := ds.FindOrCreateUser(ctx, "bob@example.com")
		if err != nil {
			t.Fatalf("second FindOrCreateUser: %v", err)
		}

		if u1.ID != u2.ID {
			t.Errorf("not idempotent: first ID %q, second ID %q", u1.ID, u2.ID)
		}
	})
}

func testDatastoreIntegrationTokens(t *testing.T, newStore func(t *testing.T) core.Datastore) {
	t.Run("store and get round-trip", func(t *testing.T) {
		ds := newStore(t)
		t.Cleanup(func() { ds.Close() })
		ctx := context.Background()

		user := mustCreateUser(t, ctx, ds, "carol@example.com")

		now := time.Now().Truncate(time.Second)
		expires := now.Add(time.Hour)
		token := &core.IntegrationToken{
			ID:              "tok-1",
			UserID:          user.ID,
			Integration:     "test-service",
			Instance:        "instance-1",
			AccessToken:     "access-token-value",
			RefreshToken:    "refresh-token-value",
			Scopes:          "scope-a,scope-b",
			ExpiresAt:       &expires,
			LastRefreshedAt: &now,
			MetadataJSON:    `{"key":"value"}`,
			CreatedAt:       now,
			UpdatedAt:       now,
		}

		mustStoreToken(t, ctx, ds, token)

		got, err := ds.Token(ctx, user.ID, "test-service", "instance-1")
		if err != nil {
			t.Fatalf("Token: %v", err)
		}
		if got == nil {
			t.Fatal("Token returned nil")
		}
		if got.AccessToken != "access-token-value" {
			t.Errorf("AccessToken: got %q, want %q", got.AccessToken, "access-token-value")
		}
		if got.RefreshToken != "refresh-token-value" {
			t.Errorf("RefreshToken: got %q, want %q", got.RefreshToken, "refresh-token-value")
		}
		if got.Scopes != "scope-a,scope-b" {
			t.Errorf("Scopes: got %q, want %q", got.Scopes, "scope-a,scope-b")
		}
	})

	t.Run("list returns user tokens", func(t *testing.T) {
		ds := newStore(t)
		t.Cleanup(func() { ds.Close() })
		ctx := context.Background()

		user := mustCreateUser(t, ctx, ds, "dave@example.com")
		now := time.Now().Truncate(time.Second)

		mustStoreToken(t, ctx, ds, &core.IntegrationToken{
			ID: "tok-a", UserID: user.ID, Integration: "svc-a",
			Instance: "i1", AccessToken: "a", CreatedAt: now, UpdatedAt: now,
		})
		mustStoreToken(t, ctx, ds, &core.IntegrationToken{
			ID: "tok-b", UserID: user.ID, Integration: "svc-b",
			Instance: "i2", AccessToken: "b", CreatedAt: now, UpdatedAt: now,
		})

		tokens, err := ds.ListTokens(ctx, user.ID)
		if err != nil {
			t.Fatalf("ListTokens: %v", err)
		}
		if len(tokens) != 2 {
			t.Fatalf("ListTokens: got %d tokens, want 2", len(tokens))
		}
	})

	t.Run("delete removes a token", func(t *testing.T) {
		ds := newStore(t)
		t.Cleanup(func() { ds.Close() })
		ctx := context.Background()

		user := mustCreateUser(t, ctx, ds, "eve@example.com")
		now := time.Now().Truncate(time.Second)

		mustStoreToken(t, ctx, ds, &core.IntegrationToken{
			ID: "tok-del", UserID: user.ID, Integration: "svc",
			Instance: "i1", AccessToken: "x", CreatedAt: now, UpdatedAt: now,
		})

		if err := ds.DeleteToken(ctx, "tok-del"); err != nil {
			t.Fatalf("DeleteToken: %v", err)
		}

		got, err := ds.Token(ctx, user.ID, "svc", "i1")
		if err != nil {
			t.Fatalf("Token after delete: unexpected error: %v", err)
		}
		if got != nil {
			t.Fatalf("Token after delete: expected nil, got %+v", got)
		}
	})

	t.Run("get nonexistent returns nil", func(t *testing.T) {
		ds := newStore(t)
		t.Cleanup(func() { ds.Close() })
		ctx := context.Background()

		got, err := ds.Token(ctx, "no-user", "no-svc", "no-instance")
		if err != nil {
			t.Fatalf("Token for nonexistent: unexpected error: %v", err)
		}
		if got != nil {
			t.Errorf("Token for nonexistent: expected nil, got %+v", got)
		}
	})
}

func testDatastoreAPITokens(t *testing.T, newStore func(t *testing.T) core.Datastore) {
	t.Run("store and validate round-trip", func(t *testing.T) {
		ds := newStore(t)
		t.Cleanup(func() { ds.Close() })
		ctx := context.Background()

		user := mustCreateUser(t, ctx, ds, "frank@example.com")
		now := time.Now().Truncate(time.Second)

		apiToken := &core.APIToken{
			ID:          "api-1",
			UserID:      user.ID,
			Name:        "CI token",
			HashedToken: "sha256:abc123",
			Scopes:      "read:tokens",
			CreatedAt:   now,
			UpdatedAt:   now,
		}

		mustStoreAPIToken(t, ctx, ds, apiToken)

		got, err := ds.ValidateAPIToken(ctx, "sha256:abc123")
		if err != nil {
			t.Fatalf("ValidateAPIToken: %v", err)
		}
		if got == nil {
			t.Fatal("ValidateAPIToken returned nil")
		}
		if got.UserID != user.ID {
			t.Errorf("UserID: got %q, want %q", got.UserID, user.ID)
		}
		if got.Name != "CI token" {
			t.Errorf("Name: got %q, want %q", got.Name, "CI token")
		}
	})

	t.Run("list returns user tokens", func(t *testing.T) {
		ds := newStore(t)
		t.Cleanup(func() { ds.Close() })
		ctx := context.Background()

		user := mustCreateUser(t, ctx, ds, "grace@example.com")
		now := time.Now().Truncate(time.Second)

		mustStoreAPIToken(t, ctx, ds, &core.APIToken{
			ID: "api-a", UserID: user.ID, Name: "token-a",
			HashedToken: "sha256:aaa", CreatedAt: now, UpdatedAt: now,
		})
		mustStoreAPIToken(t, ctx, ds, &core.APIToken{
			ID: "api-b", UserID: user.ID, Name: "token-b",
			HashedToken: "sha256:bbb", CreatedAt: now, UpdatedAt: now,
		})

		tokens, err := ds.ListAPITokens(ctx, user.ID)
		if err != nil {
			t.Fatalf("ListAPITokens: %v", err)
		}
		if len(tokens) != 2 {
			t.Fatalf("ListAPITokens: got %d, want 2", len(tokens))
		}
	})

	t.Run("revoke removes a token", func(t *testing.T) {
		ds := newStore(t)
		t.Cleanup(func() { ds.Close() })
		ctx := context.Background()

		user := mustCreateUser(t, ctx, ds, "heidi@example.com")
		now := time.Now().Truncate(time.Second)

		mustStoreAPIToken(t, ctx, ds, &core.APIToken{
			ID: "api-rev", UserID: user.ID, Name: "revokable",
			HashedToken: "sha256:revme", CreatedAt: now, UpdatedAt: now,
		})

		if err := ds.RevokeAPIToken(ctx, user.ID, "api-rev"); err != nil {
			t.Fatalf("RevokeAPIToken: %v", err)
		}

		got, err := ds.ValidateAPIToken(ctx, "sha256:revme")
		if err != nil {
			t.Fatalf("ValidateAPIToken after revoke: unexpected error: %v", err)
		}
		if got != nil {
			t.Errorf("ValidateAPIToken after revoke: expected nil, got %+v", got)
		}
	})

	t.Run("validate nonexistent returns nil", func(t *testing.T) {
		ds := newStore(t)
		t.Cleanup(func() { ds.Close() })
		ctx := context.Background()

		got, err := ds.ValidateAPIToken(ctx, "sha256:nonexistent")
		if err != nil {
			t.Fatalf("ValidateAPIToken for nonexistent: unexpected error: %v", err)
		}
		if got != nil {
			t.Errorf("ValidateAPIToken for nonexistent: expected nil, got %+v", got)
		}
	})

	t.Run("revoke with wrong user_id returns ErrNotFound", func(t *testing.T) {
		ds := newStore(t)
		t.Cleanup(func() { ds.Close() })
		ctx := context.Background()

		userA := mustCreateUser(t, ctx, ds, "revoke-owner-a@example.com")
		userB := mustCreateUser(t, ctx, ds, "revoke-owner-b@example.com")
		now := time.Now().Truncate(time.Second)

		mustStoreAPIToken(t, ctx, ds, &core.APIToken{
			ID: "api-owner-check", UserID: userA.ID, Name: "owned-by-a",
			HashedToken: "sha256:ownercheck", CreatedAt: now, UpdatedAt: now,
		})

		err := ds.RevokeAPIToken(ctx, userB.ID, "api-owner-check")
		if !errors.Is(err, core.ErrNotFound) {
			t.Fatalf("RevokeAPIToken with wrong user: expected ErrNotFound, got %v", err)
		}

		got, err := ds.ValidateAPIToken(ctx, "sha256:ownercheck")
		if err != nil {
			t.Fatalf("ValidateAPIToken after failed revoke: %v", err)
		}
		if got == nil {
			t.Fatal("token should still exist after revoke with wrong user_id")
		}
	})

	t.Run("validate expired token returns nil", func(t *testing.T) {
		ds := newStore(t)
		t.Cleanup(func() { ds.Close() })
		ctx := context.Background()

		user := mustCreateUser(t, ctx, ds, "expired-token@example.com")
		now := time.Now().Truncate(time.Second)
		pastExpiry := now.Add(-1 * time.Hour)

		mustStoreAPIToken(t, ctx, ds, &core.APIToken{
			ID: "api-expired", UserID: user.ID, Name: "expired",
			HashedToken: "sha256:expired", ExpiresAt: &pastExpiry,
			CreatedAt: now, UpdatedAt: now,
		})

		got, err := ds.ValidateAPIToken(ctx, "sha256:expired")
		if err != nil {
			t.Fatalf("ValidateAPIToken for expired: unexpected error: %v", err)
		}
		if got != nil {
			t.Errorf("ValidateAPIToken for expired: expected nil, got %+v", got)
		}
	})
}

func testDatastoreStagedConnections(t *testing.T, newStore func(t *testing.T) core.Datastore) {
	t.Run("store get delete lifecycle", func(t *testing.T) {
		ds := newStore(t)
		t.Cleanup(func() { ds.Close() })

		scs, ok := ds.(core.StagedConnectionStore)
		if !ok {
			t.Skip("datastore does not implement StagedConnectionStore")
		}

		ctx := context.Background()

		user := mustCreateUser(t, ctx, ds, "staged@example.com")
		now := time.Now().Truncate(time.Second)
		expiry := now.Add(10 * time.Minute)

		sc := &core.StagedConnection{
			ID:             "sc-1",
			UserID:         user.ID,
			Integration:    "test-provider",
			Instance:       "default",
			AccessToken:    "staged-access-token",
			RefreshToken:   "staged-refresh-token",
			TokenExpiresAt: &expiry,
			MetadataJSON:   `{"env":"prod"}`,
			CandidatesJSON: `[{"id":"c1","name":"Site A"},{"id":"c2","name":"Site B"}]`,
			CreatedAt:      now,
			ExpiresAt:      expiry,
		}

		if err := scs.StoreStagedConnection(ctx, sc); err != nil {
			t.Fatalf("StoreStagedConnection: %v", err)
		}

		got, err := scs.GetStagedConnection(ctx, "sc-1")
		if err != nil {
			t.Fatalf("GetStagedConnection: %v", err)
		}
		if got == nil {
			t.Fatal("GetStagedConnection returned nil")
		}
		if got.AccessToken != "staged-access-token" {
			t.Errorf("AccessToken: got %q, want %q", got.AccessToken, "staged-access-token")
		}
		if got.RefreshToken != "staged-refresh-token" {
			t.Errorf("RefreshToken: got %q, want %q", got.RefreshToken, "staged-refresh-token")
		}
		if got.Integration != "test-provider" {
			t.Errorf("Integration: got %q, want %q", got.Integration, "test-provider")
		}
		if got.TokenExpiresAt == nil || !got.TokenExpiresAt.Truncate(time.Second).Equal(expiry.Truncate(time.Second)) {
			t.Errorf("TokenExpiresAt: got %v, want %v", got.TokenExpiresAt, expiry)
		}
		if got.CandidatesJSON != sc.CandidatesJSON {
			t.Errorf("CandidatesJSON: got %q, want %q", got.CandidatesJSON, sc.CandidatesJSON)
		}

		if err := scs.DeleteStagedConnection(ctx, "sc-1"); err != nil {
			t.Fatalf("DeleteStagedConnection: %v", err)
		}

		_, err = scs.GetStagedConnection(ctx, "sc-1")
		if !errors.Is(err, core.ErrNotFound) {
			t.Fatalf("GetStagedConnection after delete: expected ErrNotFound, got %v", err)
		}
	})

	t.Run("get nonexistent returns ErrNotFound", func(t *testing.T) {
		ds := newStore(t)
		t.Cleanup(func() { ds.Close() })

		scs, ok := ds.(core.StagedConnectionStore)
		if !ok {
			t.Skip("datastore does not implement StagedConnectionStore")
		}

		ctx := context.Background()

		_, err := scs.GetStagedConnection(ctx, "nonexistent-id")
		if !errors.Is(err, core.ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})
}

func testDatastoreEgressClients(t *testing.T, newStore func(t *testing.T) core.Datastore) {
	t.Run("create personal default and get", func(t *testing.T) {
		ds := newStore(t)
		t.Cleanup(func() { ds.Close() })

		ecs, ok := ds.(core.EgressClientStore)
		if !ok {
			t.Skip("datastore does not implement EgressClientStore")
		}

		ctx := context.Background()
		user := mustCreateUser(t, ctx, ds, "egress-admin@example.com")
		now := time.Now().Truncate(time.Second)

		client := &core.EgressClient{
			ID:          "ec-1",
			Name:        "ci-bot",
			Description: "CI/CD pipeline agent",
			CreatedByID: user.ID,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := ecs.CreateEgressClient(ctx, client); err != nil {
			t.Fatalf("CreateEgressClient: %v", err)
		}

		got, err := ecs.GetEgressClient(ctx, "ec-1")
		if err != nil {
			t.Fatalf("GetEgressClient: %v", err)
		}
		if got.Name != "ci-bot" {
			t.Errorf("Name: got %q, want %q", got.Name, "ci-bot")
		}
		if got.Description != "CI/CD pipeline agent" {
			t.Errorf("Description: got %q, want %q", got.Description, "CI/CD pipeline agent")
		}
		if got.CreatedByID != user.ID {
			t.Errorf("CreatedByID: got %q, want %q", got.CreatedByID, user.ID)
		}
		if got.Scope != core.EgressClientScopePersonal {
			t.Errorf("Scope: got %q, want %q", got.Scope, core.EgressClientScopePersonal)
		}
	})

	t.Run("create global client", func(t *testing.T) {
		ds := newStore(t)
		t.Cleanup(func() { ds.Close() })

		ecs, ok := ds.(core.EgressClientStore)
		if !ok {
			t.Skip("datastore does not implement EgressClientStore")
		}

		ctx := context.Background()
		user := mustCreateUser(t, ctx, ds, "egress-global@example.com")
		now := time.Now().Truncate(time.Second)

		if err := ecs.CreateEgressClient(ctx, &core.EgressClient{
			ID: "ec-g1", Name: "shared-bot", Scope: core.EgressClientScopeGlobal,
			CreatedByID: user.ID, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("CreateEgressClient: %v", err)
		}

		got, err := ecs.GetEgressClient(ctx, "ec-g1")
		if err != nil {
			t.Fatalf("GetEgressClient: %v", err)
		}
		if got.Scope != core.EgressClientScopeGlobal {
			t.Errorf("Scope: got %q, want %q", got.Scope, core.EgressClientScopeGlobal)
		}
	})

	t.Run("delete removes client", func(t *testing.T) {
		ds := newStore(t)
		t.Cleanup(func() { ds.Close() })

		ecs, ok := ds.(core.EgressClientStore)
		if !ok {
			t.Skip("datastore does not implement EgressClientStore")
		}

		ctx := context.Background()
		user := mustCreateUser(t, ctx, ds, "egress-del@example.com")
		now := time.Now().Truncate(time.Second)

		if err := ecs.CreateEgressClient(ctx, &core.EgressClient{
			ID: "ec-del", Name: "deletable", Scope: core.EgressClientScopePersonal,
			CreatedByID: user.ID, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("CreateEgressClient: %v", err)
		}

		if err := ecs.DeleteEgressClient(ctx, "ec-del"); err != nil {
			t.Fatalf("DeleteEgressClient: %v", err)
		}

		_, err := ecs.GetEgressClient(ctx, "ec-del")
		if !errors.Is(err, core.ErrNotFound) {
			t.Fatalf("GetEgressClient after delete: expected ErrNotFound, got %v", err)
		}
	})

	t.Run("delete nonexistent returns ErrNotFound", func(t *testing.T) {
		ds := newStore(t)
		t.Cleanup(func() { ds.Close() })

		ecs, ok := ds.(core.EgressClientStore)
		if !ok {
			t.Skip("datastore does not implement EgressClientStore")
		}

		ctx := context.Background()
		err := ecs.DeleteEgressClient(ctx, "nonexistent-id")
		if !errors.Is(err, core.ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("duplicate personal name same creator returns ErrAlreadyRegistered", func(t *testing.T) {
		ds := newStore(t)
		t.Cleanup(func() { ds.Close() })

		ecs, ok := ds.(core.EgressClientStore)
		if !ok {
			t.Skip("datastore does not implement EgressClientStore")
		}

		ctx := context.Background()
		user := mustCreateUser(t, ctx, ds, "egress-dup@example.com")
		now := time.Now().Truncate(time.Second)

		if err := ecs.CreateEgressClient(ctx, &core.EgressClient{
			ID: "ec-dup-1", Name: "same-name", Scope: core.EgressClientScopePersonal,
			CreatedByID: user.ID, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("first CreateEgressClient: %v", err)
		}

		err := ecs.CreateEgressClient(ctx, &core.EgressClient{
			ID: "ec-dup-2", Name: "same-name", Scope: core.EgressClientScopePersonal,
			CreatedByID: user.ID, CreatedAt: now, UpdatedAt: now,
		})
		if !errors.Is(err, core.ErrAlreadyRegistered) {
			t.Fatalf("expected ErrAlreadyRegistered, got %v", err)
		}
	})

	t.Run("same personal name different creators allowed", func(t *testing.T) {
		ds := newStore(t)
		t.Cleanup(func() { ds.Close() })

		ecs, ok := ds.(core.EgressClientStore)
		if !ok {
			t.Skip("datastore does not implement EgressClientStore")
		}

		ctx := context.Background()
		userA := mustCreateUser(t, ctx, ds, "egress-a@example.com")
		userB := mustCreateUser(t, ctx, ds, "egress-b@example.com")
		now := time.Now().Truncate(time.Second)

		if err := ecs.CreateEgressClient(ctx, &core.EgressClient{
			ID: "ec-a", Name: "shared-name", Scope: core.EgressClientScopePersonal,
			CreatedByID: userA.ID, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("CreateEgressClient userA: %v", err)
		}
		if err := ecs.CreateEgressClient(ctx, &core.EgressClient{
			ID: "ec-b", Name: "shared-name", Scope: core.EgressClientScopePersonal,
			CreatedByID: userB.ID, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("CreateEgressClient userB: %v", err)
		}
	})

	t.Run("duplicate global name across creators returns ErrAlreadyRegistered", func(t *testing.T) {
		ds := newStore(t)
		t.Cleanup(func() { ds.Close() })

		ecs, ok := ds.(core.EgressClientStore)
		if !ok {
			t.Skip("datastore does not implement EgressClientStore")
		}

		ctx := context.Background()
		userA := mustCreateUser(t, ctx, ds, "egress-ga@example.com")
		userB := mustCreateUser(t, ctx, ds, "egress-gb@example.com")
		now := time.Now().Truncate(time.Second)

		if err := ecs.CreateEgressClient(ctx, &core.EgressClient{
			ID: "ec-g-a", Name: "global-bot", Scope: core.EgressClientScopeGlobal,
			CreatedByID: userA.ID, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("first global CreateEgressClient: %v", err)
		}

		err := ecs.CreateEgressClient(ctx, &core.EgressClient{
			ID: "ec-g-b", Name: "global-bot", Scope: core.EgressClientScopeGlobal,
			CreatedByID: userB.ID, CreatedAt: now, UpdatedAt: now,
		})
		if !errors.Is(err, core.ErrAlreadyRegistered) {
			t.Fatalf("expected ErrAlreadyRegistered, got %v", err)
		}
	})

	t.Run("same name personal and global allowed for same creator", func(t *testing.T) {
		ds := newStore(t)
		t.Cleanup(func() { ds.Close() })

		ecs, ok := ds.(core.EgressClientStore)
		if !ok {
			t.Skip("datastore does not implement EgressClientStore")
		}

		ctx := context.Background()
		user := mustCreateUser(t, ctx, ds, "egress-both@example.com")
		now := time.Now().Truncate(time.Second)

		if err := ecs.CreateEgressClient(ctx, &core.EgressClient{
			ID: "ec-p", Name: "dual-name", Scope: core.EgressClientScopePersonal,
			CreatedByID: user.ID, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("personal CreateEgressClient: %v", err)
		}
		if err := ecs.CreateEgressClient(ctx, &core.EgressClient{
			ID: "ec-g", Name: "dual-name", Scope: core.EgressClientScopeGlobal,
			CreatedByID: user.ID, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("global CreateEgressClient: %v", err)
		}

		p, err := ecs.GetEgressClient(ctx, "ec-p")
		if err != nil {
			t.Fatalf("GetEgressClient personal: %v", err)
		}
		if p.Scope != core.EgressClientScopePersonal {
			t.Errorf("personal Scope: got %q", p.Scope)
		}

		g, err := ecs.GetEgressClient(ctx, "ec-g")
		if err != nil {
			t.Fatalf("GetEgressClient global: %v", err)
		}
		if g.Scope != core.EgressClientScopeGlobal {
			t.Errorf("global Scope: got %q", g.Scope)
		}
	})

	t.Run("list filter by CreatedByID only", func(t *testing.T) {
		ds := newStore(t)
		t.Cleanup(func() { ds.Close() })

		ecs, ok := ds.(core.EgressClientStore)
		if !ok {
			t.Skip("datastore does not implement EgressClientStore")
		}

		ctx := context.Background()
		userA := mustCreateUser(t, ctx, ds, "egress-la@example.com")
		userB := mustCreateUser(t, ctx, ds, "egress-lb@example.com")
		now := time.Now().Truncate(time.Second)

		if err := ecs.CreateEgressClient(ctx, &core.EgressClient{
			ID: "ec-la", Name: "bot-a", Scope: core.EgressClientScopePersonal,
			CreatedByID: userA.ID, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("CreateEgressClient A: %v", err)
		}
		if err := ecs.CreateEgressClient(ctx, &core.EgressClient{
			ID: "ec-lb", Name: "bot-b", Scope: core.EgressClientScopePersonal,
			CreatedByID: userB.ID, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("CreateEgressClient B: %v", err)
		}

		got, err := ecs.ListEgressClients(ctx, core.EgressClientFilter{CreatedByID: userA.ID})
		if err != nil {
			t.Fatalf("ListEgressClients: %v", err)
		}
		if len(got) != 1 || got[0].ID != "ec-la" {
			t.Fatalf("ListEgressClients: got %d, want 1 (ec-la)", len(got))
		}
	})

	t.Run("list filter by Scope only", func(t *testing.T) {
		ds := newStore(t)
		t.Cleanup(func() { ds.Close() })

		ecs, ok := ds.(core.EgressClientStore)
		if !ok {
			t.Skip("datastore does not implement EgressClientStore")
		}

		ctx := context.Background()
		user := mustCreateUser(t, ctx, ds, "egress-ls@example.com")
		now := time.Now().Truncate(time.Second)

		if err := ecs.CreateEgressClient(ctx, &core.EgressClient{
			ID: "ec-sp", Name: "personal-bot", Scope: core.EgressClientScopePersonal,
			CreatedByID: user.ID, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("CreateEgressClient personal: %v", err)
		}
		if err := ecs.CreateEgressClient(ctx, &core.EgressClient{
			ID: "ec-sg", Name: "global-bot", Scope: core.EgressClientScopeGlobal,
			CreatedByID: user.ID, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("CreateEgressClient global: %v", err)
		}

		personal, err := ecs.ListEgressClients(ctx, core.EgressClientFilter{Scope: core.EgressClientScopePersonal})
		if err != nil {
			t.Fatalf("ListEgressClients personal: %v", err)
		}
		if len(personal) != 1 || personal[0].ID != "ec-sp" {
			t.Fatalf("ListEgressClients personal: got %d, want 1 (ec-sp)", len(personal))
		}

		global, err := ecs.ListEgressClients(ctx, core.EgressClientFilter{Scope: core.EgressClientScopeGlobal})
		if err != nil {
			t.Fatalf("ListEgressClients global: %v", err)
		}
		if len(global) != 1 || global[0].ID != "ec-sg" {
			t.Fatalf("ListEgressClients global: got %d, want 1 (ec-sg)", len(global))
		}
	})

	t.Run("list filter by both CreatedByID and Scope", func(t *testing.T) {
		ds := newStore(t)
		t.Cleanup(func() { ds.Close() })

		ecs, ok := ds.(core.EgressClientStore)
		if !ok {
			t.Skip("datastore does not implement EgressClientStore")
		}

		ctx := context.Background()
		userA := mustCreateUser(t, ctx, ds, "egress-fa@example.com")
		userB := mustCreateUser(t, ctx, ds, "egress-fb@example.com")
		now := time.Now().Truncate(time.Second)

		if err := ecs.CreateEgressClient(ctx, &core.EgressClient{
			ID: "ec-fa-p", Name: "bot-a-p", Scope: core.EgressClientScopePersonal,
			CreatedByID: userA.ID, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("CreateEgressClient: %v", err)
		}
		if err := ecs.CreateEgressClient(ctx, &core.EgressClient{
			ID: "ec-fa-g", Name: "bot-a-g", Scope: core.EgressClientScopeGlobal,
			CreatedByID: userA.ID, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("CreateEgressClient: %v", err)
		}
		if err := ecs.CreateEgressClient(ctx, &core.EgressClient{
			ID: "ec-fb-p", Name: "bot-b-p", Scope: core.EgressClientScopePersonal,
			CreatedByID: userB.ID, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("CreateEgressClient: %v", err)
		}

		got, err := ecs.ListEgressClients(ctx, core.EgressClientFilter{
			CreatedByID: userA.ID, Scope: core.EgressClientScopePersonal,
		})
		if err != nil {
			t.Fatalf("ListEgressClients: %v", err)
		}
		if len(got) != 1 || got[0].ID != "ec-fa-p" {
			t.Fatalf("ListEgressClients AND filter: got %d, want 1 (ec-fa-p)", len(got))
		}
	})

	t.Run("migration idempotence with scope data", func(t *testing.T) {
		ds := newStore(t)
		t.Cleanup(func() { ds.Close() })

		ecs, ok := ds.(core.EgressClientStore)
		if !ok {
			t.Skip("datastore does not implement EgressClientStore")
		}

		ctx := context.Background()
		user := mustCreateUser(t, ctx, ds, "egress-mig@example.com")
		now := time.Now().Truncate(time.Second)

		if err := ecs.CreateEgressClient(ctx, &core.EgressClient{
			ID: "ec-mp", Name: "dual-mig", Scope: core.EgressClientScopePersonal,
			CreatedByID: user.ID, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("CreateEgressClient personal: %v", err)
		}
		if err := ecs.CreateEgressClient(ctx, &core.EgressClient{
			ID: "ec-mg", Name: "dual-mig", Scope: core.EgressClientScopeGlobal,
			CreatedByID: user.ID, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("CreateEgressClient global: %v", err)
		}

		if err := ds.Migrate(ctx); err != nil {
			t.Fatalf("re-Migrate: %v", err)
		}

		p, err := ecs.GetEgressClient(ctx, "ec-mp")
		if err != nil {
			t.Fatalf("GetEgressClient personal after re-migrate: %v", err)
		}
		if p.Scope != core.EgressClientScopePersonal {
			t.Errorf("personal Scope after re-migrate: got %q", p.Scope)
		}

		g, err := ecs.GetEgressClient(ctx, "ec-mg")
		if err != nil {
			t.Fatalf("GetEgressClient global after re-migrate: %v", err)
		}
		if g.Scope != core.EgressClientScopeGlobal {
			t.Errorf("global Scope after re-migrate: got %q", g.Scope)
		}
	})
}

func testDatastoreEgressClientTokens(t *testing.T, newStore func(t *testing.T) core.Datastore) {
	t.Run("create validate and list lifecycle", func(t *testing.T) {
		ds := newStore(t)
		t.Cleanup(func() { ds.Close() })

		ecs, ok := ds.(core.EgressClientStore)
		if !ok {
			t.Skip("datastore does not implement EgressClientStore")
		}

		ctx := context.Background()
		user := mustCreateUser(t, ctx, ds, "token-admin@example.com")
		now := time.Now().Truncate(time.Second)

		if err := ecs.CreateEgressClient(ctx, &core.EgressClient{
			ID: "ec-tok", Name: "token-client", Scope: core.EgressClientScopePersonal, CreatedByID: user.ID,
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("CreateEgressClient: %v", err)
		}

		token := &core.EgressClientToken{
			ID:          "eclt-1",
			ClientID:    "ec-tok",
			Name:        "deploy-token",
			HashedToken: "sha256:eclt-hash-1",
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := ecs.CreateEgressClientToken(ctx, token); err != nil {
			t.Fatalf("CreateEgressClientToken: %v", err)
		}

		got, err := ecs.ValidateEgressClientToken(ctx, "sha256:eclt-hash-1")
		if err != nil {
			t.Fatalf("ValidateEgressClientToken: %v", err)
		}
		if got == nil {
			t.Fatal("ValidateEgressClientToken returned nil")
		}
		if got.ClientID != "ec-tok" {
			t.Errorf("ClientID: got %q, want %q", got.ClientID, "ec-tok")
		}
		if got.Name != "deploy-token" {
			t.Errorf("Name: got %q, want %q", got.Name, "deploy-token")
		}

		tokens, err := ecs.ListEgressClientTokens(ctx, "ec-tok")
		if err != nil {
			t.Fatalf("ListEgressClientTokens: %v", err)
		}
		if len(tokens) != 1 {
			t.Fatalf("ListEgressClientTokens: got %d, want 1", len(tokens))
		}
	})

	t.Run("validate expired token returns nil", func(t *testing.T) {
		ds := newStore(t)
		t.Cleanup(func() { ds.Close() })

		ecs, ok := ds.(core.EgressClientStore)
		if !ok {
			t.Skip("datastore does not implement EgressClientStore")
		}

		ctx := context.Background()
		user := mustCreateUser(t, ctx, ds, "expired-ect@example.com")
		now := time.Now().Truncate(time.Second)
		pastExpiry := now.Add(-1 * time.Hour)

		if err := ecs.CreateEgressClient(ctx, &core.EgressClient{
			ID: "ec-exp", Name: "expired-client", Scope: core.EgressClientScopePersonal, CreatedByID: user.ID,
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("CreateEgressClient: %v", err)
		}

		if err := ecs.CreateEgressClientToken(ctx, &core.EgressClientToken{
			ID: "eclt-exp", ClientID: "ec-exp", Name: "expired",
			HashedToken: "sha256:eclt-expired", ExpiresAt: &pastExpiry,
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("CreateEgressClientToken: %v", err)
		}

		got, err := ecs.ValidateEgressClientToken(ctx, "sha256:eclt-expired")
		if err != nil {
			t.Fatalf("ValidateEgressClientToken: %v", err)
		}
		if got != nil {
			t.Errorf("expected nil for expired token, got %+v", got)
		}
	})

	t.Run("revoke removes token", func(t *testing.T) {
		ds := newStore(t)
		t.Cleanup(func() { ds.Close() })

		ecs, ok := ds.(core.EgressClientStore)
		if !ok {
			t.Skip("datastore does not implement EgressClientStore")
		}

		ctx := context.Background()
		user := mustCreateUser(t, ctx, ds, "revoke-ect@example.com")
		now := time.Now().Truncate(time.Second)

		if err := ecs.CreateEgressClient(ctx, &core.EgressClient{
			ID: "ec-rev", Name: "revoke-client", Scope: core.EgressClientScopePersonal, CreatedByID: user.ID,
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("CreateEgressClient: %v", err)
		}

		if err := ecs.CreateEgressClientToken(ctx, &core.EgressClientToken{
			ID: "eclt-rev", ClientID: "ec-rev", Name: "revokable",
			HashedToken: "sha256:eclt-revoke", CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("CreateEgressClientToken: %v", err)
		}

		if err := ecs.RevokeEgressClientToken(ctx, "ec-rev", "eclt-rev"); err != nil {
			t.Fatalf("RevokeEgressClientToken: %v", err)
		}

		got, err := ecs.ValidateEgressClientToken(ctx, "sha256:eclt-revoke")
		if err != nil {
			t.Fatalf("ValidateEgressClientToken after revoke: %v", err)
		}
		if got != nil {
			t.Errorf("expected nil after revoke, got %+v", got)
		}
	})

	t.Run("revoke with wrong client_id returns ErrNotFound", func(t *testing.T) {
		ds := newStore(t)
		t.Cleanup(func() { ds.Close() })

		ecs, ok := ds.(core.EgressClientStore)
		if !ok {
			t.Skip("datastore does not implement EgressClientStore")
		}

		ctx := context.Background()
		user := mustCreateUser(t, ctx, ds, "wrong-client-ect@example.com")
		now := time.Now().Truncate(time.Second)

		if err := ecs.CreateEgressClient(ctx, &core.EgressClient{
			ID: "ec-owner", Name: "owner-client", Scope: core.EgressClientScopePersonal, CreatedByID: user.ID,
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("CreateEgressClient: %v", err)
		}

		if err := ecs.CreateEgressClientToken(ctx, &core.EgressClientToken{
			ID: "eclt-owned", ClientID: "ec-owner", Name: "owned-token",
			HashedToken: "sha256:eclt-owned", CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("CreateEgressClientToken: %v", err)
		}

		err := ecs.RevokeEgressClientToken(ctx, "wrong-client-id", "eclt-owned")
		if !errors.Is(err, core.ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("cascade delete removes tokens", func(t *testing.T) {
		ds := newStore(t)
		t.Cleanup(func() { ds.Close() })

		ecs, ok := ds.(core.EgressClientStore)
		if !ok {
			t.Skip("datastore does not implement EgressClientStore")
		}

		ctx := context.Background()
		user := mustCreateUser(t, ctx, ds, "cascade-ect@example.com")
		now := time.Now().Truncate(time.Second)

		if err := ecs.CreateEgressClient(ctx, &core.EgressClient{
			ID: "ec-cascade", Name: "cascade-client", Scope: core.EgressClientScopePersonal, CreatedByID: user.ID,
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("CreateEgressClient: %v", err)
		}

		if err := ecs.CreateEgressClientToken(ctx, &core.EgressClientToken{
			ID: "eclt-cascade", ClientID: "ec-cascade", Name: "cascade-token",
			HashedToken: "sha256:eclt-cascade", CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("CreateEgressClientToken: %v", err)
		}

		if err := ecs.DeleteEgressClient(ctx, "ec-cascade"); err != nil {
			t.Fatalf("DeleteEgressClient: %v", err)
		}

		got, err := ecs.ValidateEgressClientToken(ctx, "sha256:eclt-cascade")
		if err != nil {
			t.Fatalf("ValidateEgressClientToken after cascade delete: %v", err)
		}
		if got != nil {
			t.Errorf("expected nil after cascade delete, got %+v", got)
		}
	})
}
