package coretesting

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
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

		got, err := ds.Token(ctx, user.ID, "test-service", "", "instance-1")
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

		got, err := ds.Token(ctx, user.ID, "svc", "", "i1")
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

		got, err := ds.Token(ctx, "no-user", "no-svc", "", "no-instance")
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

	t.Run("revoke all tokens for user", func(t *testing.T) {
		ds := newStore(t)
		t.Cleanup(func() { ds.Close() })
		ctx := context.Background()

		userA := mustCreateUser(t, ctx, ds, "revoke-all-a@example.com")
		userB := mustCreateUser(t, ctx, ds, "revoke-all-b@example.com")
		now := time.Now().Truncate(time.Second)

		mustStoreAPIToken(t, ctx, ds, &core.APIToken{
			ID: "rall-1", UserID: userA.ID, Name: "a-tok-1",
			HashedToken: "sha256:rall1", CreatedAt: now, UpdatedAt: now,
		})
		mustStoreAPIToken(t, ctx, ds, &core.APIToken{
			ID: "rall-2", UserID: userA.ID, Name: "a-tok-2",
			HashedToken: "sha256:rall2", CreatedAt: now, UpdatedAt: now,
		})
		mustStoreAPIToken(t, ctx, ds, &core.APIToken{
			ID: "rall-3", UserID: userB.ID, Name: "b-tok-1",
			HashedToken: "sha256:rall3", CreatedAt: now, UpdatedAt: now,
		})

		count, err := ds.RevokeAllAPITokens(ctx, userA.ID)
		if err != nil {
			t.Fatalf("RevokeAllAPITokens: %v", err)
		}
		if count != 2 {
			t.Fatalf("RevokeAllAPITokens: got count %d, want 2", count)
		}

		tokensA, err := ds.ListAPITokens(ctx, userA.ID)
		if err != nil {
			t.Fatalf("ListAPITokens after revoke-all: %v", err)
		}
		if len(tokensA) != 0 {
			t.Errorf("expected 0 tokens for userA, got %d", len(tokensA))
		}

		tokensB, err := ds.ListAPITokens(ctx, userB.ID)
		if err != nil {
			t.Fatalf("ListAPITokens for userB: %v", err)
		}
		if len(tokensB) != 1 {
			t.Errorf("expected 1 token for userB, got %d", len(tokensB))
		}
	})

	t.Run("revoke all tokens returns zero when none exist", func(t *testing.T) {
		ds := newStore(t)
		t.Cleanup(func() { ds.Close() })
		ctx := context.Background()

		user := mustCreateUser(t, ctx, ds, "revoke-all-empty@example.com")
		count, err := ds.RevokeAllAPITokens(ctx, user.ID)
		if err != nil {
			t.Fatalf("RevokeAllAPITokens: %v", err)
		}
		if count != 0 {
			t.Fatalf("RevokeAllAPITokens: got count %d, want 0", count)
		}
	})
}
