package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
)

func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("MYSQL_TEST_DSN not set; skipping MySQL tests")
	}
	return dsn
}

func newTestDatabase(t *testing.T) string {
	t.Helper()
	baseDSN := testDSN(t)

	cfg, err := mysqldriver.ParseDSN(baseDSN)
	if err != nil {
		t.Fatalf("parsing base DSN: %v", err)
	}

	adminCfg := cfg.Clone()
	adminCfg.DBName = ""
	adminCfg.ParseTime = true
	adminDB, err := sql.Open("mysql", adminCfg.FormatDSN())
	if err != nil {
		t.Fatalf("opening admin connection: %v", err)
	}
	defer func() { _ = adminDB.Close() }()

	dbName := fmt.Sprintf("gestalt_test_%s", shortUUID())
	if _, err := adminDB.ExecContext(context.Background(), "CREATE DATABASE "+dbName); err != nil {
		t.Fatalf("creating test database %s: %v", dbName, err)
	}
	t.Cleanup(func() {
		cleanupDB, err := sql.Open("mysql", adminCfg.FormatDSN())
		if err != nil {
			t.Logf("warning: opening admin connection for cleanup: %v", err)
			return
		}
		defer func() { _ = cleanupDB.Close() }()
		if _, err := cleanupDB.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+dbName); err != nil {
			t.Logf("warning: dropping test database %s: %v", dbName, err)
		}
	})

	cfg.DBName = dbName
	cfg.ParseTime = true
	return cfg.FormatDSN()
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dsn := newTestDatabase(t)
	store, err := New(dsn, coretesting.EncryptionKey(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return store
}

func TestMySQLDatastoreConformance(t *testing.T) {
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
				"INSERT INTO integration_tokens (id, user_id, integration, instance, access_token_encrypted, refresh_token_encrypted, scopes, metadata_json, created_at, updated_at, last_refreshed_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
				"fk-tok", "nonexistent-user", "svc", "i1", "enc", "", "", "", now, now, now,
			)
			if err == nil {
				t.Fatal("expected foreign key violation, got nil error")
			}

			var mysqlErr *mysqldriver.MySQLError
			if !errors.As(err, &mysqlErr) || mysqlErr.Number != 1452 {
				t.Errorf("expected MySQL error 1452 (FK violation), got: %v", err)
			}
		},
	})
}

func shortUUID() string {
	return uuid.NewString()[:8]
}
