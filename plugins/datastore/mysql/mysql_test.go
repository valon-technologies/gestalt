package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/core"
	coretesting "github.com/valon-technologies/gestalt/core/testing"
)

func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("MYSQL_TEST_DSN not set; skipping MySQL tests")
	}
	return dsn
}

func newTestDatabase(t *testing.T) string {
	t.Helper()
	baseDSN := testDSN(t)

	cfg, err := mysqldriver.ParseDSN(baseDSN)
	if err != nil {
		t.Fatalf("parsing base DSN: %v", err)
	}

	adminCfg := cfg.Clone()
	adminCfg.DBName = ""
	adminCfg.ParseTime = true
	adminDB, err := sql.Open("mysql", adminCfg.FormatDSN())
	if err != nil {
		t.Fatalf("opening admin connection: %v", err)
	}
	defer func() { _ = adminDB.Close() }()

	dbName := fmt.Sprintf("toolshed_test_%s", shortUUID())
	if _, err := adminDB.ExecContext(context.Background(), "CREATE DATABASE "+dbName); err != nil {
		t.Fatalf("creating test database %s: %v", dbName, err)
	}
	t.Cleanup(func() {
		cleanupDB, err := sql.Open("mysql", adminCfg.FormatDSN())
		if err != nil {
			t.Logf("warning: opening admin connection for cleanup: %v", err)
			return
		}
		defer func() { _ = cleanupDB.Close() }()
		if _, err := cleanupDB.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+dbName); err != nil {
			t.Logf("warning: dropping test database %s: %v", dbName, err)
		}
	})

	cfg.DBName = dbName
	cfg.ParseTime = true
	return cfg.FormatDSN()
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dsn := newTestDatabase(t)
	store, err := New(dsn, coretesting.EncryptionKey(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return store
}

func TestMySQLDatastoreConformance(t *testing.T) {
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

	// Verify tokens are encrypted at rest.
	var accessEnc, refreshEnc string
	err = store.DB.QueryRowContext(ctx,
		"SELECT access_token_encrypted, refresh_token_encrypted FROM integration_tokens WHERE id = ?",
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

	// Verify round-trip decryption.
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
				ID:          uuid.NewString(),
				UserID:      user.ID,
				Integration: "svc",
				Instance:    uuid.NewString(),
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

func TestFindOrCreateUserRace(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
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
			users[idx], errs[idx] = store.FindOrCreateUser(ctx, email)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
	}

	// All goroutines must return the same user ID.
	firstID := users[0].ID
	for i, u := range users[1:] {
		if u.ID != firstID {
			t.Errorf("goroutine %d returned ID %q, want %q", i+1, u.ID, firstID)
		}
	}
}

func TestStoreTokenUpsert(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	user, err := store.FindOrCreateUser(ctx, "upsert@example.com")
	if err != nil {
		t.Fatalf("FindOrCreateUser: %v", err)
	}

	now := time.Now().Truncate(time.Second)
	token := &core.IntegrationToken{
		ID:          "upsert-tok",
		UserID:      user.ID,
		Integration: "svc",
		Instance:    "i1",
		AccessToken: "first",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store.StoreToken(ctx, token); err != nil {
		t.Fatalf("first StoreToken: %v", err)
	}

	token.AccessToken = "second"
	token.UpdatedAt = now.Add(time.Minute)
	if err := store.StoreToken(ctx, token); err != nil {
		t.Fatalf("second StoreToken: %v", err)
	}

	got, err := store.Token(ctx, user.ID, "svc", "i1")
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if got.AccessToken != "second" {
		t.Errorf("AccessToken after upsert: got %q, want %q", got.AccessToken, "second")
	}
}

func TestDeleteNonexistentTokenNoError(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)

	if err := store.DeleteToken(context.Background(), "does-not-exist"); err != nil {
		t.Fatalf("DeleteToken for nonexistent: %v", err)
	}
}

func TestRevokeNonexistentAPITokenNoError(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)

	if err := store.RevokeAPIToken(context.Background(), "does-not-exist"); err != nil {
		t.Fatalf("RevokeAPIToken for nonexistent: %v", err)
	}
}

func TestForeignKeyEnforcement(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Second)
	_, err := store.DB.ExecContext(ctx,
		"INSERT INTO integration_tokens (id, user_id, integration, instance, access_token_encrypted, refresh_token_encrypted, scopes, metadata_json, created_at, updated_at, last_refreshed_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"fk-tok", "nonexistent-user", "svc", "i1", "enc", "", "", "", now, now, now,
	)
	if err == nil {
		t.Fatal("expected foreign key violation, got nil error")
	}

	// MySQL error 1452: Cannot add or update a child row: a foreign key constraint fails
	var mysqlErr *mysqldriver.MySQLError
	if !errors.As(err, &mysqlErr) || mysqlErr.Number != 1452 {
		t.Errorf("expected MySQL error 1452 (FK violation), got: %v", err)
	}
}

func shortUUID() string {
	return uuid.NewString()[:8]
}
