package sqlserver

import (
	"context"
	"database/sql"
	"fmt"
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
	store, err := openTestStore(t, "")
	if err != nil {
		t.Fatalf("openTestStore: %v", err)
	}
	return store
}

func openTestStore(t *testing.T, version string) (*Store, error) {
	t.Helper()
	dsn := testDSN(t)
	resetTestDB(t, dsn)

	store, err := New(dsn, version, coretesting.EncryptionKey(t))
	if err != nil {
		return nil, fmt.Errorf("New: %w", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("Migrate: %w", err)
	}

	t.Cleanup(func() {
		_ = store.Close()
		resetTestDB(t, dsn)
	})
	return store, nil
}

func TestSQLServerDatastoreConformance(t *testing.T) {
	t.Parallel()
	coretesting.RunDatastoreTests(t, func(t *testing.T) core.Datastore {
		return newTestStore(t)
	})
}

func TestSQLServerVersionSelection(t *testing.T) {
	t.Parallel()
	coretesting.RunDatastoreVersionTests(t, coretesting.DatastoreVersionHooks{
		SupportedVersions: supportedVersions,
		OpenStore: func(t *testing.T, version string) (core.Datastore, error) {
			return openTestStore(t, version)
		},
		DetectVersion: func(ctx context.Context, ds core.Datastore, requested string) (string, error) {
			return resolveVersion(ctx, ds.(*Store).DB, requested)
		},
	})
}
