package sqlserver

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
)

func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("GESTALT_TEST_SQLSERVER_DSN")
	if dsn == "" {
		t.Skip("GESTALT_TEST_SQLSERVER_DSN not set")
	}
	return dsn
}

func resetTestDB(t *testing.T, dsn string) {
	t.Helper()

	db, err := sql.Open(driverName, dsn)
	if err != nil {
		t.Fatalf("opening sqlserver for cleanup: %v", err)
	}
	defer func() { _ = db.Close() }()

	const dropTables = `
IF OBJECT_ID('oauth_registrations', 'U') IS NOT NULL DROP TABLE oauth_registrations;
IF OBJECT_ID('staged_connections', 'U') IS NOT NULL DROP TABLE staged_connections;
IF OBJECT_ID('api_tokens', 'U') IS NOT NULL DROP TABLE api_tokens;
IF OBJECT_ID('integration_tokens', 'U') IS NOT NULL DROP TABLE integration_tokens;
IF OBJECT_ID('users', 'U') IS NOT NULL DROP TABLE users;
`
	if _, err := db.ExecContext(context.Background(), dropTables); err != nil {
		t.Fatalf("resetting sqlserver test database: %v", err)
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dsn := testDSN(t)
	resetTestDB(t, dsn)

	store, err := New(dsn, coretesting.EncryptionKey(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		_ = store.Close()
		t.Fatalf("Migrate: %v", err)
	}

	t.Cleanup(func() {
		_ = store.Close()
		resetTestDB(t, dsn)
	})
	return store
}

func TestSQLServerDatastoreConformance(t *testing.T) {
	t.Parallel()
	coretesting.RunDatastoreTests(t, func(t *testing.T) core.Datastore {
		return newTestStore(t)
	})
}
