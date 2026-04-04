package oracle

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
	return newTestStoreWithVersion(t, "")
}

func newTestStoreWithVersion(t *testing.T, version string) *Store {
	t.Helper()
	dsn := testDSN(t)
	resetTestSchema(t, dsn)

	store, err := New(dsn, version, coretesting.EncryptionKey(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		_ = store.Close()
		t.Fatalf("Migrate: %v", err)
	}

	t.Cleanup(func() {
		_ = store.Close()
		resetTestSchema(t, dsn)
	})
	return store
}

func TestOracleDatastoreConformance(t *testing.T) {
	t.Parallel()
	coretesting.RunDatastoreTests(t, func(t *testing.T) core.Datastore {
		return newTestStore(t)
	})
}

func TestOracleVersionSelection(t *testing.T) {
	t.Parallel()

	autoStore := newTestStoreWithVersion(t, "auto")
	autoVersion, err := resolveVersion(context.Background(), autoStore.DB, "auto")
	if err != nil {
		t.Fatalf("resolveVersion(auto): %v", err)
	}

	explicitStore := newTestStoreWithVersion(t, autoVersion)
	explicitVersion, err := resolveVersion(context.Background(), explicitStore.DB, autoVersion)
	if err != nil {
		t.Fatalf("resolveVersion(%q): %v", autoVersion, err)
	}
	if explicitVersion != autoVersion {
		t.Fatalf("resolved version = %q, want %q", explicitVersion, autoVersion)
	}

	dsn := testDSN(t)
	for _, version := range supportedVersions {
		if version == autoVersion {
			continue
		}
		if _, err := New(dsn, version, coretesting.EncryptionKey(t)); err == nil {
			t.Fatalf("New(%q) succeeded against %q", version, autoVersion)
		}
		return
	}
}
