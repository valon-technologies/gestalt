package oracle

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
)

var testSchemaMu sync.Mutex

func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("GESTALT_TEST_ORACLE_DSN")
	if dsn == "" {
		t.Skip("GESTALT_TEST_ORACLE_DSN not set")
	}
	return dsn
}

func resetTestSchema(t *testing.T, dsn string) {
	t.Helper()

	db, err := sql.Open("oracle", dsn)
	if err != nil {
		t.Fatalf("opening oracle for cleanup: %v", err)
	}
	defer func() { _ = db.Close() }()

	drops := []string{
		"BEGIN EXECUTE IMMEDIATE 'DROP TABLE oauth_registrations CASCADE CONSTRAINTS'; EXCEPTION WHEN OTHERS THEN IF SQLCODE != -942 THEN RAISE; END IF; END;",
		"BEGIN EXECUTE IMMEDIATE 'DROP TABLE staged_connections CASCADE CONSTRAINTS'; EXCEPTION WHEN OTHERS THEN IF SQLCODE != -942 THEN RAISE; END IF; END;",
		"BEGIN EXECUTE IMMEDIATE 'DROP TABLE api_tokens CASCADE CONSTRAINTS'; EXCEPTION WHEN OTHERS THEN IF SQLCODE != -942 THEN RAISE; END IF; END;",
		"BEGIN EXECUTE IMMEDIATE 'DROP TABLE integration_tokens CASCADE CONSTRAINTS'; EXCEPTION WHEN OTHERS THEN IF SQLCODE != -942 THEN RAISE; END IF; END;",
		"BEGIN EXECUTE IMMEDIATE 'DROP TABLE users CASCADE CONSTRAINTS'; EXCEPTION WHEN OTHERS THEN IF SQLCODE != -942 THEN RAISE; END IF; END;",
	}
	for _, stmt := range drops {
		if _, err := db.ExecContext(context.Background(), stmt); err != nil {
			t.Fatalf("resetting oracle test schema: %v", err)
		}
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
	resetTestSchema(t, dsn)

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
		resetTestSchema(t, dsn)
	})
	return store, nil
}

func TestOracleDatastoreConformance(t *testing.T) {
	t.Parallel()
	testSchemaMu.Lock()
	defer testSchemaMu.Unlock()

	coretesting.RunDatastoreTests(t, func(t *testing.T) core.Datastore {
		return newTestStore(t)
	})
}

func TestOracleVersionSelection(t *testing.T) {
	t.Parallel()
	testSchemaMu.Lock()
	defer testSchemaMu.Unlock()

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
