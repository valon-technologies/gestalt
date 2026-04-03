package firestore

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/drivers/datastore"
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

	projectID := fmt.Sprintf("%s-%s", testProjectID(), strings.ToLower(uuid.NewString()[:8]))
	store, err := New(projectID, "", coretesting.EncryptionKey(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		_ = store.Close()
		t.Fatalf("Migrate: %v", err)
	}

	t.Cleanup(func() {
		_ = store.Close()
	})

	return store
}

func TestFirestoreDatastoreConformance(t *testing.T) {
	t.Parallel()
	coretesting.RunDatastoreDriverTests(t, func(t *testing.T) core.Datastore {
		return newTestStore(t)
	}, coretesting.DatastoreDriverHooks{
		AssertTokenEncryptedAtRest: func(t *testing.T, ctx context.Context, ds core.Datastore, token *core.IntegrationToken) {
			store := ds.(*Store)

			snap, err := store.client.Collection(datastore.IntegrationTokensCollection).Doc(token.ID).Get(ctx)
			if err != nil {
				t.Fatalf("raw Get: %v", err)
			}
			var doc integrationTokenDoc
			if err := snap.DataTo(&doc); err != nil {
				t.Fatalf("DataTo: %v", err)
			}
			if doc.AccessTokenEncrypted == token.AccessToken {
				t.Error("access token stored in plaintext")
			}
			if doc.RefreshTokenEncrypted == token.RefreshToken {
				t.Error("refresh token stored in plaintext")
			}
			if doc.AccessTokenEncrypted == "" {
				t.Error("access_token_encrypted is empty")
			}
		},
	})
}

func TestFindOrCreateUserHonorsLegacyEmailLookupKey(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	userID := "legacy-user"
	email := "legacy@example.com"

	_, err := store.client.Collection(datastore.UsersCollection).Doc(userID).Set(ctx, userDoc{
		Email:     email,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	_, err = store.client.Collection(usersByEmailCollection).Doc(email).Set(ctx, userLookupDoc{
		UserID: userID,
	})
	if err != nil {
		t.Fatalf("seed user lookup: %v", err)
	}

	user, err := store.FindOrCreateUser(ctx, email)
	if err != nil {
		t.Fatalf("FindOrCreateUser: %v", err)
	}
	if user.ID != userID {
		t.Fatalf("FindOrCreateUser returned %q, want %q", user.ID, userID)
	}
}

func TestStoreTokenAndAPITokenAllowLookupKeyChanges(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	user, err := store.FindOrCreateUser(ctx, "rekey@example.com")
	if err != nil {
		t.Fatalf("FindOrCreateUser: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	token := &core.IntegrationToken{
		ID:          "rekey-int-token",
		UserID:      user.ID,
		Integration: "svc-a",
		Instance:    "i1",
		AccessToken: "a1",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store.StoreToken(ctx, token); err != nil {
		t.Fatalf("StoreToken initial: %v", err)
	}
	token.Integration = "svc-b"
	token.Instance = "i2"
	token.AccessToken = "a2"
	token.UpdatedAt = now.Add(time.Second)
	if err := store.StoreToken(ctx, token); err != nil {
		t.Fatalf("StoreToken update: %v", err)
	}

	oldToken, err := store.Token(ctx, user.ID, "svc-a", "", "i1")
	if err != nil {
		t.Fatalf("Token old lookup: %v", err)
	}
	if oldToken != nil {
		t.Fatalf("old token lookup should be gone, got %+v", oldToken)
	}
	newToken, err := store.Token(ctx, user.ID, "svc-b", "", "i2")
	if err != nil {
		t.Fatalf("Token new lookup: %v", err)
	}
	if newToken == nil || newToken.AccessToken != "a2" {
		t.Fatalf("new token lookup = %+v, want updated token", newToken)
	}

	apiToken := &core.APIToken{
		ID:          "rekey-api-token",
		UserID:      user.ID,
		Name:        "token",
		HashedToken: "sha256:old",
		Scopes:      "read",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store.StoreAPIToken(ctx, apiToken); err != nil {
		t.Fatalf("StoreAPIToken initial: %v", err)
	}
	apiToken.HashedToken = "sha256:new"
	apiToken.UpdatedAt = now.Add(2 * time.Second)
	if err := store.StoreAPIToken(ctx, apiToken); err != nil {
		t.Fatalf("StoreAPIToken update: %v", err)
	}

	oldAPIToken, err := store.ValidateAPIToken(ctx, "sha256:old")
	if err != nil {
		t.Fatalf("ValidateAPIToken old hash: %v", err)
	}
	if oldAPIToken != nil {
		t.Fatalf("old api token hash should be gone, got %+v", oldAPIToken)
	}
	newAPIToken, err := store.ValidateAPIToken(ctx, "sha256:new")
	if err != nil {
		t.Fatalf("ValidateAPIToken new hash: %v", err)
	}
	if newAPIToken == nil || newAPIToken.ID != apiToken.ID {
		t.Fatalf("new api token lookup = %+v, want %q", newAPIToken, apiToken.ID)
	}
}
