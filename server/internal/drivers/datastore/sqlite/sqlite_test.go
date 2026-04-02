package sqlite

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/crypto"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := New(dbPath, coretesting.EncryptionKey(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return store
}

func TestSQLiteDatastoreConformance(t *testing.T) {
	t.Parallel()
	coretesting.RunDatastoreTests(t, func(t *testing.T) core.Datastore {
		return newTestStore(t)
	})
}

func TestEncryptionRoundTrip(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()

	user, err := store.FindOrCreateUser(ctx, "enc@example.com")
	if err != nil {
		t.Fatalf("FindOrCreateUser: %v", err)
	}

	now := time.Now().Truncate(time.Second)
	token := &core.IntegrationToken{
		ID:           "enc-tok-1",
		UserID:       user.ID,
		Integration:  "test",
		Instance:     "i1",
		AccessToken:  "secret-access-token",
		RefreshToken: "secret-refresh-token",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := store.StoreToken(ctx, token); err != nil {
		t.Fatalf("StoreToken: %v", err)
	}

	var accessEnc, refreshEnc string
	err = store.DB.QueryRowContext(ctx,
		"SELECT access_token_encrypted, refresh_token_encrypted FROM integration_tokens WHERE id = ?",
		"enc-tok-1",
	).Scan(&accessEnc, &refreshEnc)
	if err != nil {
		t.Fatalf("raw query: %v", err)
	}
	if accessEnc == "secret-access-token" {
		t.Error("access token stored in plaintext")
	}
	if refreshEnc == "secret-refresh-token" {
		t.Error("refresh token stored in plaintext")
	}
	if accessEnc == "" {
		t.Error("access_token_encrypted is empty")
	}

	got, err := store.Token(ctx, user.ID, "test", "", "i1")
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if got.AccessToken != "secret-access-token" {
		t.Errorf("AccessToken: got %q, want %q", got.AccessToken, "secret-access-token")
	}
	if got.RefreshToken != "secret-refresh-token" {
		t.Errorf("RefreshToken: got %q, want %q", got.RefreshToken, "secret-refresh-token")
	}
}

func TestEncryptionFallbackDecryptsLegacyCiphertext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	legacyKey := coretesting.EncryptionKey(t)
	currentKey := coretesting.EncryptionKey(t)

	legacyStore, err := New(dbPath, legacyKey)
	if err != nil {
		t.Fatalf("New legacy store: %v", err)
	}
	if err := legacyStore.Migrate(ctx); err != nil {
		t.Fatalf("Migrate legacy store: %v", err)
	}

	user, err := legacyStore.FindOrCreateUser(ctx, "legacy@example.com")
	if err != nil {
		t.Fatalf("FindOrCreateUser: %v", err)
	}
	now := time.Now().Truncate(time.Second)
	if err := legacyStore.StoreToken(ctx, &core.IntegrationToken{
		ID:           "legacy-tok-1",
		UserID:       user.ID,
		Integration:  "test",
		Instance:     "i1",
		AccessToken:  "legacy-access-token",
		RefreshToken: "legacy-refresh-token",
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		t.Fatalf("StoreToken legacy: %v", err)
	}
	if err := legacyStore.Close(); err != nil {
		t.Fatalf("Close legacy store: %v", err)
	}

	crypto.SetDefaultAESGCMFallbackKey(legacyKey)
	t.Cleanup(func() { crypto.SetDefaultAESGCMFallbackKey(nil) })

	currentStore, err := New(dbPath, currentKey)
	if err != nil {
		t.Fatalf("New current store: %v", err)
	}
	t.Cleanup(func() { _ = currentStore.Close() })

	got, err := currentStore.Token(ctx, user.ID, "test", "", "i1")
	if err != nil {
		t.Fatalf("Token with fallback: %v", err)
	}
	if got.AccessToken != "legacy-access-token" {
		t.Errorf("AccessToken: got %q, want %q", got.AccessToken, "legacy-access-token")
	}
	if got.RefreshToken != "legacy-refresh-token" {
		t.Errorf("RefreshToken: got %q, want %q", got.RefreshToken, "legacy-refresh-token")
	}
}

func TestWALMode(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	t.Cleanup(func() { _ = store.Close() })

	var mode string
	err := store.DB.QueryRowContext(context.Background(), "PRAGMA journal_mode").Scan(&mode)
	if err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode: got %q, want %q", mode, "wal")
	}
}

func TestConcurrentReadWrite(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()

	user, err := store.FindOrCreateUser(ctx, "concurrent@example.com")
	if err != nil {
		t.Fatalf("FindOrCreateUser: %v", err)
	}

	now := time.Now().Truncate(time.Second)
	var wg sync.WaitGroup
	errs := make([]error, 10)

	for i := range 10 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			tok := &core.IntegrationToken{
				ID:          uuid.NewString(),
				UserID:      user.ID,
				Integration: "svc",
				Instance:    uuid.NewString(),
				AccessToken: "tok",
				CreatedAt:   now,
				UpdatedAt:   now,
			}
			errs[idx] = store.StoreToken(ctx, tok)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("concurrent write %d: %v", i, err)
		}
	}

	tokens, err := store.ListTokens(ctx, user.ID)
	if err != nil {
		t.Fatalf("ListTokens: %v", err)
	}
	if len(tokens) != 10 {
		t.Errorf("expected 10 tokens, got %d", len(tokens))
	}
}

func TestStoreTokenUpsert(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()

	user, err := store.FindOrCreateUser(ctx, "upsert@example.com")
	if err != nil {
		t.Fatalf("FindOrCreateUser: %v", err)
	}

	now := time.Now().Truncate(time.Second)
	token := &core.IntegrationToken{
		ID:          "upsert-tok",
		UserID:      user.ID,
		Integration: "svc",
		Instance:    "i1",
		AccessToken: "first",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store.StoreToken(ctx, token); err != nil {
		t.Fatalf("first StoreToken: %v", err)
	}

	token.AccessToken = "second"
	token.UpdatedAt = now.Add(time.Minute)
	if err := store.StoreToken(ctx, token); err != nil {
		t.Fatalf("second StoreToken: %v", err)
	}

	got, err := store.Token(ctx, user.ID, "svc", "", "i1")
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if got.AccessToken != "second" {
		t.Errorf("AccessToken after upsert: got %q, want %q", got.AccessToken, "second")
	}
}

func TestDeleteNonexistentTokenNoError(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	t.Cleanup(func() { _ = store.Close() })

	if err := store.DeleteToken(context.Background(), "does-not-exist"); err != nil {
		t.Fatalf("DeleteToken for nonexistent: %v", err)
	}
}

func TestRevokeNonexistentAPITokenReturnsNotFound(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	t.Cleanup(func() { _ = store.Close() })

	if err := store.RevokeAPIToken(context.Background(), "no-user", "does-not-exist"); err != core.ErrNotFound {
		t.Fatalf("RevokeAPIToken for nonexistent: expected ErrNotFound, got %v", err)
	}
}

func TestForeignKeyEnforcement(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()

	now := time.Now().Truncate(time.Second)
	_, err := store.DB.ExecContext(ctx,
		"INSERT INTO integration_tokens (id, user_id, integration, instance, access_token_encrypted, created_at, updated_at, last_refreshed_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		"fk-tok", "nonexistent-user", "svc", "i1", "enc", now, now, now,
	)
	if err == nil {
		t.Error("expected foreign key violation, got nil error")
	}
}
