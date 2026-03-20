package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/toolshed/core"
	coretesting "github.com/valon-technologies/toolshed/core/testing"
)

func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TOOLSHED_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TOOLSHED_TEST_POSTGRES_DSN not set")
	}
	return dsn
}

// newTestStore creates a Store using a unique schema for test isolation.
// Each test gets its own PostgreSQL schema so tests do not interfere with
// each other.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	dsn := testDSN(t)

	schema := "test_" + uuidNoDashes(t)

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("opening postgres for schema setup: %v", err)
	}

	_, err = db.Exec(fmt.Sprintf("CREATE SCHEMA %s", schema))
	if err != nil {
		_ = db.Close()
		t.Fatalf("creating schema %s: %v", schema, err)
	}
	_ = db.Close()

	// Append search_path to the DSN so all tables land in the test schema.
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	schemaDSN := dsn + sep + "search_path=" + schema

	store, err := New(schemaDSN, coretesting.EncryptionKey(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		_ = store.Close()
		t.Fatalf("Migrate: %v", err)
	}

	t.Cleanup(func() {
		_ = store.Close()
		cleanDB, err := sql.Open("pgx", dsn)
		if err == nil {
			_, _ = cleanDB.Exec(fmt.Sprintf("DROP SCHEMA %s CASCADE", schema))
			_ = cleanDB.Close()
		}
	})

	return store
}

func uuidNoDashes(t *testing.T) string {
	t.Helper()
	return strings.ReplaceAll(uuid.NewString(), "-", "")
}

func TestPostgresDatastoreConformance(t *testing.T) {
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
		ID:           "enc-tok-1",
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

	var accessEnc, refreshEnc string
	err = store.DB.QueryRowContext(ctx,
		"SELECT access_token_encrypted, refresh_token_encrypted FROM integration_tokens WHERE id = $1",
		"enc-tok-1",
	).Scan(&accessEnc, &refreshEnc)
	if err != nil {
		t.Fatalf("raw query: %v", err)
	}
	if accessEnc == "secret-access-token" {
		t.Error("access token stored in plaintext")
	}
	if refreshEnc == "secret-refresh-token" {
		t.Error("refresh token stored in plaintext")
	}
	if accessEnc == "" {
		t.Error("access_token_encrypted is empty")
	}

	got, err := store.Token(ctx, user.ID, "test", "i1")
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

func TestConcurrentReadWrite(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	user, err := store.FindOrCreateUser(ctx, "concurrent@example.com")
	if err != nil {
		t.Fatalf("FindOrCreateUser: %v", err)
	}

	now := time.Now().Truncate(time.Second)
	var wg sync.WaitGroup
	errs := make([]error, 10)

	for i := range 10 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			tok := &core.IntegrationToken{
				ID:          newTestID(t),
				UserID:      user.ID,
				Integration: "svc",
				Instance:    newTestID(t),
				AccessToken: "tok",
				CreatedAt:   now,
				UpdatedAt:   now,
			}
			errs[idx] = store.StoreToken(ctx, tok)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("concurrent write %d: %v", i, err)
		}
	}

	tokens, err := store.ListTokens(ctx, user.ID)
	if err != nil {
		t.Fatalf("ListTokens: %v", err)
	}
	if len(tokens) != 10 {
		t.Errorf("expected 10 tokens, got %d", len(tokens))
	}
}

func TestForeignKeyEnforcement(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Second)
	_, err := store.DB.ExecContext(ctx,
		"INSERT INTO integration_tokens (id, user_id, integration, instance, access_token_encrypted, created_at, updated_at, last_refreshed_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)",
		"fk-tok", "nonexistent-user", "svc", "i1", "enc", now, now, now,
	)
	if err == nil {
		t.Error("expected foreign key violation, got nil error")
	}
}

func newTestID(t *testing.T) string {
	t.Helper()
	return uuid.NewString()
}
