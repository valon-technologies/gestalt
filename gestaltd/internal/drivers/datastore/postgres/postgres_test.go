package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
)

func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("GESTALT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("GESTALT_TEST_POSTGRES_DSN not set")
	}
	return dsn
}

// newTestStore creates a Store using a unique schema for test isolation.
// Each test gets its own PostgreSQL schema so tests do not interfere with
// each other.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	dsn := testDSN(t)

	schema := "test_" + uuidNoDashes(t)

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("opening postgres for schema setup: %v", err)
	}

	_, err = db.Exec(fmt.Sprintf("CREATE SCHEMA %s", schema))
	if err != nil {
		_ = db.Close()
		t.Fatalf("creating schema %s: %v", schema, err)
	}
	_ = db.Close()

	// Append search_path to the DSN so all tables land in the test schema.
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	schemaDSN := dsn + sep + "search_path=" + schema

	store, err := New(schemaDSN, coretesting.EncryptionKey(t))
	if err != nil {
		cleanDB, cleanErr := sql.Open("pgx", dsn)
		if cleanErr == nil {
			_, _ = cleanDB.Exec(fmt.Sprintf("DROP SCHEMA %s CASCADE", schema))
			_ = cleanDB.Close()
		}
		t.Fatalf("New: %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		_ = store.Close()
		cleanDB, cleanErr := sql.Open("pgx", dsn)
		if cleanErr == nil {
			_, _ = cleanDB.Exec(fmt.Sprintf("DROP SCHEMA %s CASCADE", schema))
			_ = cleanDB.Close()
		}
		t.Fatalf("Migrate: %v", err)
	}

	t.Cleanup(func() {
		_ = store.Close()
		cleanDB, err := sql.Open("pgx", dsn)
		if err == nil {
			_, _ = cleanDB.Exec(fmt.Sprintf("DROP SCHEMA %s CASCADE", schema))
			_ = cleanDB.Close()
		}
	})

	return store
}

func uuidNoDashes(t *testing.T) string {
	t.Helper()
	return strings.ReplaceAll(uuid.NewString(), "-", "")
}

func TestPostgresDatastoreConformance(t *testing.T) {
	t.Parallel()
	coretesting.RunDatastoreDriverTests(t, func(t *testing.T) core.Datastore {
		return newTestStore(t)
	}, coretesting.DatastoreDriverHooks{
		AssertTokenEncryptedAtRest: func(t *testing.T, ctx context.Context, ds core.Datastore, token *core.IntegrationToken) {
			store := ds.(*Store)

			var accessEnc, refreshEnc string
			err := store.DB.QueryRowContext(ctx,
				"SELECT access_token_encrypted, refresh_token_encrypted FROM integration_tokens WHERE id = $1",
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
				"INSERT INTO integration_tokens (id, user_id, integration, instance, access_token_encrypted, created_at, updated_at, last_refreshed_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)",
				"fk-tok", "nonexistent-user", "svc", "i1", "enc", now, now, now,
			)
			if err == nil {
				t.Error("expected foreign key violation, got nil error")
			}
		},
	})
}
