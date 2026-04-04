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
	return newTestStoreWithVersion(t, "")
}

func newTestStoreWithVersion(t *testing.T, version string) *Store {
	t.Helper()
	uri := testURI(t)
	database := "gestalt_test_" + uuid.NewString()

	store, err := New(uri, database, version, coretesting.EncryptionKey(t))
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

func TestMongoDBVersionSelection(t *testing.T) {
	t.Parallel()

	autoStore := newTestStoreWithVersion(t, "auto")
	autoVersion, err := resolveVersion(context.Background(), autoStore.client, "auto")
	if err != nil {
		t.Fatalf("resolveVersion(auto): %v", err)
	}

	explicitStore := newTestStoreWithVersion(t, autoVersion)
	explicitVersion, err := resolveVersion(context.Background(), explicitStore.client, autoVersion)
	if err != nil {
		t.Fatalf("resolveVersion(%q): %v", autoVersion, err)
	}
	if explicitVersion != autoVersion {
		t.Fatalf("resolved version = %q, want %q", explicitVersion, autoVersion)
	}

	uri := testURI(t)
	for _, version := range supportedVersions {
		if version == autoVersion {
			continue
		}
		if _, err := New(uri, "gestalt_test_"+uuid.NewString(), version, coretesting.EncryptionKey(t)); err == nil {
			t.Fatalf("New(%q) succeeded against %q", version, autoVersion)
		}
		return
	}
}
