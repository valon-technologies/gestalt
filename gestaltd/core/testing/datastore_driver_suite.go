package coretesting

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
)

type DatastoreDriverHooks struct {
	AssertTokenEncryptedAtRest     func(t *testing.T, ctx context.Context, ds core.Datastore, token *core.IntegrationToken)
	AssertRejectsOrphanTokenInsert func(t *testing.T, ctx context.Context, ds core.Datastore)
}

type DatastoreVersionHooks struct {
	SupportedVersions []string
	OpenStore         func(t *testing.T, version string) (core.Datastore, error)
	DetectVersion     func(ctx context.Context, ds core.Datastore, requested string) (string, error)
}

// RunDatastoreDriverTests adds shared driver-level integration coverage.
func RunDatastoreDriverTests(t *testing.T, newStore func(t *testing.T) core.Datastore, hooks DatastoreDriverHooks) {
	t.Helper()

	RunDatastoreTests(t, newStore)

	t.Run("ConcurrentTokenWrites", func(t *testing.T) {
		ds := newStore(t)
		t.Cleanup(func() { _ = ds.Close() })
		ctx := context.Background()

		user := mustCreateUser(t, ctx, ds, "concurrent@example.com")
		now := time.Now().UTC().Truncate(time.Second)

		var wg sync.WaitGroup
		errs := make([]error, 10)

		for i := range errs {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				errs[idx] = ds.StoreToken(ctx, &core.IntegrationToken{
					ID:          uuid.NewString(),
					UserID:      user.ID,
					Integration: "svc",
					Instance:    uuid.NewString(),
					AccessToken: "tok",
					CreatedAt:   now,
					UpdatedAt:   now,
				})
			}(i)
		}
		wg.Wait()

		for i, err := range errs {
			if err != nil {
				t.Errorf("concurrent write %d: %v", i, err)
			}
		}

		tokens, err := ds.ListTokens(ctx, user.ID)
		if err != nil {
			t.Fatalf("ListTokens: %v", err)
		}
		if len(tokens) != len(errs) {
			t.Fatalf("ListTokens: got %d tokens, want %d", len(tokens), len(errs))
		}
	})

	t.Run("FindOrCreateUserRace", func(t *testing.T) {
		ds := newStore(t)
		t.Cleanup(func() { _ = ds.Close() })
		ctx := context.Background()

		const goroutines = 20
		email := "race@example.com"

		var wg sync.WaitGroup
		users := make([]*core.User, goroutines)
		errs := make([]error, goroutines)

		for i := range goroutines {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				users[idx], errs[idx] = ds.FindOrCreateUser(ctx, email)
			}(i)
		}
		wg.Wait()

		for i, err := range errs {
			if err != nil {
				t.Fatalf("goroutine %d: %v", i, err)
			}
		}

		firstID := users[0].ID
		for i, user := range users[1:] {
			if user == nil {
				t.Fatalf("goroutine %d returned nil user", i+1)
			}
			if user.ID != firstID {
				t.Errorf("goroutine %d returned ID %q, want %q", i+1, user.ID, firstID)
			}
		}
	})

	t.Run("StoreTokenUpsert", func(t *testing.T) {
		ds := newStore(t)
		t.Cleanup(func() { _ = ds.Close() })
		ctx := context.Background()

		user := mustCreateUser(t, ctx, ds, "upsert@example.com")
		now := time.Now().UTC().Truncate(time.Second)
		token := &core.IntegrationToken{
			ID:          "upsert-tok",
			UserID:      user.ID,
			Integration: "svc",
			Instance:    "i1",
			AccessToken: "first",
			CreatedAt:   now,
			UpdatedAt:   now,
		}

		mustStoreToken(t, ctx, ds, token)

		token.AccessToken = "second"
		token.UpdatedAt = now.Add(time.Minute)
		mustStoreToken(t, ctx, ds, token)

		got, err := ds.Token(ctx, user.ID, "svc", "", "i1")
		if err != nil {
			t.Fatalf("Token: %v", err)
		}
		if got == nil {
			t.Fatal("Token returned nil")
		}
		if got.AccessToken != "second" {
			t.Errorf("AccessToken after upsert: got %q, want %q", got.AccessToken, "second")
		}
	})

	t.Run("DeleteNonexistentTokenNoError", func(t *testing.T) {
		ds := newStore(t)
		t.Cleanup(func() { _ = ds.Close() })

		if err := ds.DeleteToken(context.Background(), "does-not-exist"); err != nil {
			t.Fatalf("DeleteToken for nonexistent: %v", err)
		}
	})

	t.Run("RevokeNonexistentAPITokenReturnsNotFound", func(t *testing.T) {
		ds := newStore(t)
		t.Cleanup(func() { _ = ds.Close() })

		err := ds.RevokeAPIToken(context.Background(), "no-user", "does-not-exist")
		if err != core.ErrNotFound {
			t.Fatalf("RevokeAPIToken for nonexistent: expected ErrNotFound, got %v", err)
		}
	})

	if hooks.AssertTokenEncryptedAtRest != nil {
		t.Run("EncryptsTokensAtRest", func(t *testing.T) {
			ds := newStore(t)
			t.Cleanup(func() { _ = ds.Close() })
			ctx := context.Background()

			user := mustCreateUser(t, ctx, ds, "enc@example.com")
			now := time.Now().UTC().Truncate(time.Second)
			token := &core.IntegrationToken{
				ID:           "enc-tok-1",
				UserID:       user.ID,
				Integration:  "test",
				Instance:     "i1",
				AccessToken:  "secret-access-token",
				RefreshToken: "secret-refresh-token",
				CreatedAt:    now,
				UpdatedAt:    now,
			}

			mustStoreToken(t, ctx, ds, token)
			hooks.AssertTokenEncryptedAtRest(t, ctx, ds, token)

			got, err := ds.Token(ctx, user.ID, "test", "", "i1")
			if err != nil {
				t.Fatalf("Token: %v", err)
			}
			if got == nil {
				t.Fatal("Token returned nil")
			}
			if got.AccessToken != token.AccessToken {
				t.Errorf("AccessToken: got %q, want %q", got.AccessToken, token.AccessToken)
			}
			if got.RefreshToken != token.RefreshToken {
				t.Errorf("RefreshToken: got %q, want %q", got.RefreshToken, token.RefreshToken)
			}
		})
	}

	if hooks.AssertRejectsOrphanTokenInsert != nil {
		t.Run("RejectsOrphanTokenInsert", func(t *testing.T) {
			ds := newStore(t)
			t.Cleanup(func() { _ = ds.Close() })
			hooks.AssertRejectsOrphanTokenInsert(t, context.Background(), ds)
		})
	}
}

func RunDatastoreVersionTests(t *testing.T, hooks DatastoreVersionHooks) {
	t.Helper()

	ctx := context.Background()

	autoStore, err := hooks.OpenStore(t, "auto")
	if err != nil {
		t.Fatalf("OpenStore(auto): %v", err)
	}

	autoVersion, err := hooks.DetectVersion(ctx, autoStore, "auto")
	if err != nil {
		t.Fatalf("DetectVersion(auto): %v", err)
	}

	explicitStore, err := hooks.OpenStore(t, autoVersion)
	if err != nil {
		t.Fatalf("OpenStore(%q): %v", autoVersion, err)
	}

	explicitVersion, err := hooks.DetectVersion(ctx, explicitStore, autoVersion)
	if err != nil {
		t.Fatalf("DetectVersion(%q): %v", autoVersion, err)
	}
	if explicitVersion != autoVersion {
		t.Fatalf("resolved version = %q, want %q", explicitVersion, autoVersion)
	}

	for _, version := range hooks.SupportedVersions {
		if version == autoVersion {
			continue
		}
		ds, err := hooks.OpenStore(t, version)
		if err == nil {
			if ds != nil {
				_ = ds.Close()
			}
			t.Fatalf("OpenStore(%q) succeeded against %q", version, autoVersion)
		}
		return
	}

	t.Fatal("SupportedVersions did not include a mismatched version to test")
}
