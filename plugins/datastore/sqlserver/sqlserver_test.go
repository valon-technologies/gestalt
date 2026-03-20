package sqlserver

import (
	"context"
	"os"
	"testing"

	"github.com/valon-technologies/toolshed/core"
	coretesting "github.com/valon-technologies/toolshed/core/testing"
)

func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TOOLSHED_TEST_SQLSERVER_DSN")
	if dsn == "" {
		t.Skip("TOOLSHED_TEST_SQLSERVER_DSN not set")
	}
	return dsn
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dsn := testDSN(t)

	store, err := New(dsn, coretesting.EncryptionKey(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		_ = store.Close()
		t.Fatalf("Migrate: %v", err)
	}

	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestSQLServerDatastoreConformance(t *testing.T) {
	t.Parallel()
	coretesting.RunDatastoreTests(t, func(t *testing.T) core.Datastore {
		return newTestStore(t)
	})
}
