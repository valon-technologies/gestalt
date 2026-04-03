package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
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
	coretesting.RunDatastoreDriverTests(t, func(t *testing.T) core.Datastore {
		return newTestStore(t)
	}, coretesting.DatastoreDriverHooks{
		AssertTokenEncryptedAtRest: func(t *testing.T, ctx context.Context, ds core.Datastore, token *core.IntegrationToken) {
			store := ds.(*Store)

			var accessEnc, refreshEnc string
			err := store.DB.QueryRowContext(ctx,
				"SELECT access_token_encrypted, refresh_token_encrypted FROM integration_tokens WHERE id = ?",
				token.ID,
			).Scan(&accessEnc, &refreshEnc)
			if err != nil {
				t.Fatalf("raw query: %v", err)
			}
			if accessEnc == token.AccessToken {
				t.Error("access token stored in plaintext")
			}
			if refreshEnc == token.RefreshToken {
				t.Error("refresh token stored in plaintext")
			}
			if accessEnc == "" {
				t.Error("access_token_encrypted is empty")
			}
		},
		AssertRejectsOrphanTokenInsert: func(t *testing.T, ctx context.Context, ds core.Datastore) {
			store := ds.(*Store)
			now := time.Now().UTC().Truncate(time.Second)

			_, err := store.DB.ExecContext(ctx,
				"INSERT INTO integration_tokens (id, user_id, integration, instance, access_token_encrypted, created_at, updated_at, last_refreshed_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
				"fk-tok", "nonexistent-user", "svc", "i1", "enc", now, now, now,
			)
			if err == nil {
				t.Error("expected foreign key violation, got nil error")
			}
		},
	})
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
