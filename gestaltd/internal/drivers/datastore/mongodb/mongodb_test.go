package mongodb

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
)

func testURI(t *testing.T) string {
	t.Helper()
	uri := os.Getenv("GESTALT_TEST_MONGODB_URI")
	if uri == "" {
		t.Skip("GESTALT_TEST_MONGODB_URI not set")
	}
	return uri
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	uri := testURI(t)
	database := "gestalt_test_" + uuid.NewString()

	store, err := New(uri, database, coretesting.EncryptionKey(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		_ = store.Close()
		t.Fatalf("Migrate: %v", err)
	}

	t.Cleanup(func() {
		_ = store.db.Drop(context.Background())
		_ = store.Close()
	})

	return store
}

func TestMongoDBDatastoreConformance(t *testing.T) {
	t.Parallel()
	coretesting.RunDatastoreTests(t, func(t *testing.T) core.Datastore {
		return newTestStore(t)
	})
}
