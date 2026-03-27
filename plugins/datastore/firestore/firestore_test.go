package firestore

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	gcpfirestore "cloud.google.com/go/firestore"
	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/core"
	coretesting "github.com/valon-technologies/gestalt/core/testing"
	"github.com/valon-technologies/gestalt/plugins/datastore"
	"google.golang.org/api/iterator"
)

func testProjectID() string {
	if id := os.Getenv("FIRESTORE_PROJECT_ID"); id != "" {
		return id
	}
	return "test-project"
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set")
	}

	projectID := testProjectID()
	store, err := New(projectID, "", coretesting.EncryptionKey(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		_ = store.Close()
		t.Fatalf("Migrate: %v", err)
	}

	t.Cleanup(func() {
		deleteAllDocs(t, store.client, datastore.UsersCollection)
		deleteAllDocs(t, store.client, usersByEmailCollection)
		deleteAllDocs(t, store.client, datastore.IntegrationTokensCollection)
		deleteAllDocs(t, store.client, datastore.APITokensCollection)
		_ = store.Close()
	})

	return store
}

func deleteAllDocs(t *testing.T, client *gcpfirestore.Client, collection string) {
	t.Helper()
	ctx := context.Background()
	iter := client.Collection(collection).Documents(ctx)
	defer iter.Stop()
	for {
		snap, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			t.Logf("cleanup %s: %v", collection, err)
			return
		}
		_, _ = snap.Ref.Delete(ctx)
	}
}

func TestFirestoreDatastoreConformance(t *testing.T) {
	t.Parallel()
	coretesting.RunDatastoreTests(t, func(t *testing.T) core.Datastore {
		return newTestStore(t)
	})
}

func TestEncryptionRoundTrip(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	user, err := store.FindOrCreateUser(ctx, "enc@example.com")
	if err != nil {
		t.Fatalf("FindOrCreateUser: %v", err)
	}

	now := time.Now().Truncate(time.Second)
	token := &core.IntegrationToken{
		ID:           fmt.Sprintf("enc-tok-%s", uuid.NewString()),
		UserID:       user.ID,
		Integration:  "test",
		Instance:     "i1",
		AccessToken:  "secret-access-token",
		RefreshToken: "secret-refresh-token",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := store.StoreToken(ctx, token); err != nil {
		t.Fatalf("StoreToken: %v", err)
	}

	// Verify the raw document stores encrypted values.
	snap, err := store.client.Collection(datastore.IntegrationTokensCollection).Doc(token.ID).Get(ctx)
	if err != nil {
		t.Fatalf("raw Get: %v", err)
	}
	var doc integrationTokenDoc
	if err := snap.DataTo(&doc); err != nil {
		t.Fatalf("DataTo: %v", err)
	}
	if doc.AccessTokenEncrypted == "secret-access-token" {
		t.Error("access token stored in plaintext")
	}
	if doc.RefreshTokenEncrypted == "secret-refresh-token" {
		t.Error("refresh token stored in plaintext")
	}
	if doc.AccessTokenEncrypted == "" {
		t.Error("access_token_encrypted is empty")
	}

	got, err := store.Token(ctx, user.ID, "test", "", "i1")
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if got.AccessToken != "secret-access-token" {
		t.Errorf("AccessToken: got %q, want %q", got.AccessToken, "secret-access-token")
	}
	if got.RefreshToken != "secret-refresh-token" {
		t.Errorf("RefreshToken: got %q, want %q", got.RefreshToken, "secret-refresh-token")
	}
}
