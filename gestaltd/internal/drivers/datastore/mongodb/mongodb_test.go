package mongodb

import (
	"context"
	"fmt"
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
	store, err := openTestStore(t, "")
	if err != nil {
		t.Fatalf("openTestStore: %v", err)
	}
	return store
}

func openTestStore(t *testing.T, version string) (*Store, error) {
	t.Helper()
	uri := testURI(t)
	database := "gestalt_test_" + uuid.NewString()

	store, err := New(uri, database, version, coretesting.EncryptionKey(t))
	if err != nil {
		return nil, fmt.Errorf("New: %w", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("Migrate: %w", err)
	}

	t.Cleanup(func() {
		_ = store.db.Drop(context.Background())
		_ = store.Close()
	})

	return store, nil
}

func TestMongoDBDatastoreConformance(t *testing.T) {
	t.Parallel()
	coretesting.RunDatastoreTests(t, func(t *testing.T) core.Datastore {
		return newTestStore(t)
	})
}

func TestMongoDBVersionSelection(t *testing.T) {
	t.Parallel()
	coretesting.RunDatastoreVersionTests(t, coretesting.DatastoreVersionHooks{
		SupportedVersions: supportedVersions,
		OpenStore: func(t *testing.T, version string) (core.Datastore, error) {
			return openTestStore(t, version)
		},
		DetectVersion: func(ctx context.Context, ds core.Datastore, requested string) (string, error) {
			return resolveVersion(ctx, ds.(*Store).client, requested)
		},
	})
}
