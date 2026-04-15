package coredata_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	corecrypto "github.com/valon-technologies/gestalt/server/core/crypto"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/emailutil"
)

const testEncryptionKey = "0123456789abcdef0123456789abcdef"

func newTestServices(t *testing.T) *coredata.Services {
	t.Helper()
	return coretesting.NewStubServices(t)
}

func newTestServicesWithDB(t *testing.T) (*coredata.Services, *coretesting.StubIndexedDB) {
	t.Helper()
	db := &coretesting.StubIndexedDB{}
	enc, err := corecrypto.NewAESGCM([]byte(testEncryptionKey))
	if err != nil {
		t.Fatalf("NewAESGCM: %v", err)
	}
	svc, err := coredata.New(db, enc)
	if err != nil {
		t.Fatalf("coredata.New: %v", err)
	}
	return svc, db
}

func mustCreateUser(t *testing.T, svc *coredata.Services, email string) *core.User {
	t.Helper()
	user, err := svc.Users.FindOrCreateUser(context.Background(), email)
	if err != nil {
		t.Fatalf("FindOrCreateUser(%q): %v", email, err)
	}
	return user
}

func seedLegacyUserRecord(t *testing.T, svc *coredata.Services, id, email string, createdAt time.Time) *core.User {
	t.Helper()
	ctx := context.Background()
	rec := indexeddb.Record{
		"id":               id,
		"email":            email,
		"normalized_email": emailutil.Normalize(email),
		"display_name":     "",
		"created_at":       createdAt,
		"updated_at":       createdAt,
	}
	if err := svc.DB.ObjectStore(coredata.StoreUsers).Add(ctx, rec); err != nil {
		t.Fatalf("seedLegacyUserRecord: %v", err)
	}
	return &core.User{
		ID:          id,
		Email:       email,
		DisplayName: "",
		CreatedAt:   createdAt,
		UpdatedAt:   createdAt,
	}
}

type countingIndexedDB struct {
	inner        indexeddb.IndexedDB
	mu           sync.Mutex
	getAllCounts map[string]int
}

func newCountingIndexedDB(inner indexeddb.IndexedDB) *countingIndexedDB {
	return &countingIndexedDB{
		inner:        inner,
		getAllCounts: make(map[string]int),
	}
}

func (d *countingIndexedDB) ObjectStore(name string) indexeddb.ObjectStore {
	return &countingObjectStore{name: name, db: d, inner: d.inner.ObjectStore(name)}
}

func (d *countingIndexedDB) CreateObjectStore(ctx context.Context, name string, schema indexeddb.ObjectStoreSchema) error {
	return d.inner.CreateObjectStore(ctx, name, schema)
}

func (d *countingIndexedDB) DeleteObjectStore(ctx context.Context, name string) error {
	return d.inner.DeleteObjectStore(ctx, name)
}

func (d *countingIndexedDB) Ping(ctx context.Context) error { return d.inner.Ping(ctx) }
func (d *countingIndexedDB) Close() error                   { return d.inner.Close() }

func (d *countingIndexedDB) recordGetAll(name string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.getAllCounts[name]++
}

func (d *countingIndexedDB) getAllCount(name string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.getAllCounts[name]
}

type countingObjectStore struct {
	name  string
	db    *countingIndexedDB
	inner indexeddb.ObjectStore
}

func (o *countingObjectStore) Get(ctx context.Context, id string) (indexeddb.Record, error) {
	return o.inner.Get(ctx, id)
}

func (o *countingObjectStore) GetKey(ctx context.Context, id string) (string, error) {
	return o.inner.GetKey(ctx, id)
}

func (o *countingObjectStore) Add(ctx context.Context, record indexeddb.Record) error {
	return o.inner.Add(ctx, record)
}

func (o *countingObjectStore) Put(ctx context.Context, record indexeddb.Record) error {
	return o.inner.Put(ctx, record)
}

func (o *countingObjectStore) Delete(ctx context.Context, id string) error {
	return o.inner.Delete(ctx, id)
}

func (o *countingObjectStore) Clear(ctx context.Context) error {
	return o.inner.Clear(ctx)
}

func (o *countingObjectStore) GetAll(ctx context.Context, r *indexeddb.KeyRange) ([]indexeddb.Record, error) {
	o.db.recordGetAll(o.name)
	return o.inner.GetAll(ctx, r)
}

func (o *countingObjectStore) GetAllKeys(ctx context.Context, r *indexeddb.KeyRange) ([]string, error) {
	return o.inner.GetAllKeys(ctx, r)
}

func (o *countingObjectStore) Count(ctx context.Context, r *indexeddb.KeyRange) (int64, error) {
	return o.inner.Count(ctx, r)
}

func (o *countingObjectStore) DeleteRange(ctx context.Context, r indexeddb.KeyRange) (int64, error) {
	return o.inner.DeleteRange(ctx, r)
}

func (o *countingObjectStore) Index(name string) indexeddb.Index {
	return o.inner.Index(name)
}

func (o *countingObjectStore) OpenCursor(ctx context.Context, r *indexeddb.KeyRange, dir indexeddb.CursorDirection) (indexeddb.Cursor, error) {
	return o.inner.OpenCursor(ctx, r, dir)
}

func (o *countingObjectStore) OpenKeyCursor(ctx context.Context, r *indexeddb.KeyRange, dir indexeddb.CursorDirection) (indexeddb.Cursor, error) {
	return o.inner.OpenKeyCursor(ctx, r, dir)
}

func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("idempotent", func(t *testing.T) {
		t.Parallel()
		db := &coretesting.StubIndexedDB{}
		enc, err := corecrypto.NewAESGCM([]byte(testEncryptionKey))
		if err != nil {
			t.Fatalf("NewAESGCM: %v", err)
		}
		if _, err := coredata.New(db, enc); err != nil {
			t.Fatalf("first New: %v", err)
		}
		if _, err := coredata.New(db, enc); err != nil {
			t.Fatalf("second New: %v", err)
		}
	})

	t.Run("backfills_normalized_email_for_legacy_users", func(t *testing.T) {
		t.Parallel()

		db := &coretesting.StubIndexedDB{}
		ctx := context.Background()
		createdAt := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
		legacy := indexeddb.Record{
			"id":           "legacy-user",
			"email":        "User@Example.com",
			"display_name": "",
			"created_at":   createdAt,
			"updated_at":   createdAt,
		}
		if err := db.ObjectStore(coredata.StoreUsers).Add(ctx, legacy); err != nil {
			t.Fatalf("seed legacy user: %v", err)
		}

		enc, err := corecrypto.NewAESGCM([]byte(testEncryptionKey))
		if err != nil {
			t.Fatalf("NewAESGCM: %v", err)
		}
		svc, err := coredata.New(db, enc)
		if err != nil {
			t.Fatalf("coredata.New: %v", err)
		}

		raw, err := svc.DB.ObjectStore(coredata.StoreUsers).Get(ctx, "legacy-user")
		if err != nil {
			t.Fatalf("Get legacy user: %v", err)
		}
		if got := raw["normalized_email"]; got != "user@example.com" {
			t.Fatalf("normalized_email = %v, want %q", got, "user@example.com")
		}
	})
}

func TestUserService(t *testing.T) {
	t.Parallel()

	t.Run("GetUser_by_ID", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)
		ctx := context.Background()

		user := mustCreateUser(t, svc, "alice@test.com")

		got, err := svc.Users.GetUser(ctx, user.ID)
		if err != nil {
			t.Fatalf("GetUser: %v", err)
		}
		if got.ID != user.ID {
			t.Errorf("ID = %q, want %q", got.ID, user.ID)
		}
		if got.Email != "alice@test.com" {
			t.Errorf("Email = %q, want %q", got.Email, "alice@test.com")
		}
	})

	t.Run("GetUser_not_found", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)

		_, err := svc.Users.GetUser(context.Background(), "nonexistent")
		if err != core.ErrNotFound {
			t.Fatalf("GetUser = %v, want ErrNotFound", err)
		}
	})

	t.Run("FindOrCreateUser_creates_new", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)

		user, err := svc.Users.FindOrCreateUser(context.Background(), "bob@test.com")
		if err != nil {
			t.Fatalf("FindOrCreateUser: %v", err)
		}
		if user.ID == "" {
			t.Error("ID should not be empty")
		}
		if user.Email != "bob@test.com" {
			t.Errorf("Email = %q, want %q", user.Email, "bob@test.com")
		}
		if user.CreatedAt.IsZero() {
			t.Error("CreatedAt should not be zero")
		}
	})

	t.Run("FindOrCreateUser_creates_new_without_full_table_scan", func(t *testing.T) {
		t.Parallel()

		db := newCountingIndexedDB(&coretesting.StubIndexedDB{})
		enc, err := corecrypto.NewAESGCM([]byte(testEncryptionKey))
		if err != nil {
			t.Fatalf("NewAESGCM: %v", err)
		}
		svc, err := coredata.New(db, enc)
		if err != nil {
			t.Fatalf("coredata.New: %v", err)
		}

		before := db.getAllCount(coredata.StoreUsers)
		user, err := svc.Users.FindOrCreateUser(context.Background(), "New@Example.com")
		if err != nil {
			t.Fatalf("FindOrCreateUser: %v", err)
		}
		if got := db.getAllCount(coredata.StoreUsers); got != before {
			t.Fatalf("users GetAll count = %d, want %d", got, before)
		}
		if user.Email != "new@example.com" {
			t.Fatalf("Email = %q, want %q", user.Email, "new@example.com")
		}
	})

	t.Run("FindOrCreateUser_idempotent", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)
		ctx := context.Background()

		u1, err := svc.Users.FindOrCreateUser(ctx, "carol@test.com")
		if err != nil {
			t.Fatalf("first call: %v", err)
		}
		u2, err := svc.Users.FindOrCreateUser(ctx, "carol@test.com")
		if err != nil {
			t.Fatalf("second call: %v", err)
		}
		if u1.ID != u2.ID {
			t.Errorf("not idempotent: first ID %q, second ID %q", u1.ID, u2.ID)
		}
	})

	t.Run("FindOrCreateUser_prefers_canonical_row_over_raw_case_duplicate", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)
		ctx := context.Background()

		canonical := seedLegacyUserRecord(t, svc, "user-canonical", "user@example.com", time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
		seedLegacyUserRecord(t, svc, "user-duplicate", "USER@example.com", time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC))

		user, err := svc.Users.FindOrCreateUser(ctx, "USER@example.com")
		if err != nil {
			t.Fatalf("FindOrCreateUser: %v", err)
		}
		if user.ID != canonical.ID {
			t.Fatalf("ID = %q, want canonical %q", user.ID, canonical.ID)
		}
		if user.Email != canonical.Email {
			t.Fatalf("Email = %q, want canonical %q", user.Email, canonical.Email)
		}
	})

	t.Run("FindOrCreateUser_canonicalizes_single_legacy_mixed_case_row", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)
		ctx := context.Background()

		legacy := seedLegacyUserRecord(t, svc, "user-legacy", "USER@example.com", time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))

		user, err := svc.Users.FindOrCreateUser(ctx, "USER@example.com")
		if err != nil {
			t.Fatalf("FindOrCreateUser: %v", err)
		}
		if user.ID != legacy.ID {
			t.Fatalf("ID = %q, want legacy %q", user.ID, legacy.ID)
		}
		if user.Email != "user@example.com" {
			t.Fatalf("Email = %q, want canonical %q", user.Email, "user@example.com")
		}
	})

	t.Run("FindOrCreateUser_concurrent_same_email", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)
		ctx := context.Background()

		const goroutines = 20
		users := make([]*core.User, goroutines)
		errs := make([]error, goroutines)

		var wg sync.WaitGroup
		for i := range goroutines {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				users[idx], errs[idx] = svc.Users.FindOrCreateUser(ctx, "race@test.com")
			}(i)
		}
		wg.Wait()

		for i, err := range errs {
			if err != nil {
				t.Fatalf("goroutine %d: %v", i, err)
			}
		}
		firstID := users[0].ID
		for i, u := range users[1:] {
			if u.ID != firstID {
				t.Errorf("goroutine %d: ID %q, want %q", i+1, u.ID, firstID)
			}
		}
	})

	t.Run("FindOrCreateUser_db_error", func(t *testing.T) {
		t.Parallel()
		svc, db := newTestServicesWithDB(t)
		db.Err = errors.New("db down")

		_, err := svc.Users.FindOrCreateUser(context.Background(), "error@test.com")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestTokenService(t *testing.T) {
	t.Parallel()

	t.Run("StoreAndRetrieve_round_trip", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)
		ctx := context.Background()

		user := mustCreateUser(t, svc, "alice@test.com")
		expires := time.Now().Add(time.Hour).Truncate(time.Second)
		token := &core.IntegrationToken{
			ID:           "tok-1",
			UserID:       user.ID,
			Integration:  "test-svc",
			Connection:   "default",
			Instance:     "inst-1",
			AccessToken:  "access-secret",
			RefreshToken: "refresh-secret",
			Scopes:       "read,write",
			ExpiresAt:    &expires,
			MetadataJSON: `{"key":"val"}`,
		}
		if err := svc.Tokens.StoreToken(ctx, token); err != nil {
			t.Fatalf("StoreToken: %v", err)
		}

		got, err := svc.Tokens.Token(ctx, user.ID, "test-svc", "default", "inst-1")
		if err != nil {
			t.Fatalf("Token: %v", err)
		}
		if got.ID != "tok-1" {
			t.Errorf("ID = %q, want %q", got.ID, "tok-1")
		}
		if got.AccessToken != "access-secret" {
			t.Errorf("AccessToken = %q, want %q", got.AccessToken, "access-secret")
		}
		if got.RefreshToken != "refresh-secret" {
			t.Errorf("RefreshToken = %q, want %q", got.RefreshToken, "refresh-secret")
		}
		if got.Scopes != "read,write" {
			t.Errorf("Scopes = %q, want %q", got.Scopes, "read,write")
		}
		if got.MetadataJSON != `{"key":"val"}` {
			t.Errorf("MetadataJSON = %q, want %q", got.MetadataJSON, `{"key":"val"}`)
		}
	})

	t.Run("Token_not_found", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)

		_, err := svc.Tokens.Token(context.Background(), "no-user", "no-svc", "no-conn", "no-inst")
		if err != core.ErrNotFound {
			t.Fatalf("Token = %v, want ErrNotFound", err)
		}
	})

	t.Run("ListTokens_by_user", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)
		ctx := context.Background()

		userA := mustCreateUser(t, svc, "alice@test.com")
		userB := mustCreateUser(t, svc, "bob@test.com")

		for _, tok := range []*core.IntegrationToken{
			{ID: "tok-a1", UserID: userA.ID, Integration: "svc-a", Connection: "default", Instance: "i1", AccessToken: "a1", RefreshToken: "r1"},
			{ID: "tok-a2", UserID: userA.ID, Integration: "svc-b", Connection: "default", Instance: "i2", AccessToken: "a2", RefreshToken: "r2"},
			{ID: "tok-b1", UserID: userB.ID, Integration: "svc-a", Connection: "default", Instance: "i1", AccessToken: "a3", RefreshToken: "r3"},
		} {
			if err := svc.Tokens.StoreToken(ctx, tok); err != nil {
				t.Fatalf("StoreToken(%s): %v", tok.ID, err)
			}
		}

		tokens, err := svc.Tokens.ListTokens(ctx, userA.ID)
		if err != nil {
			t.Fatalf("ListTokens: %v", err)
		}
		if len(tokens) != 2 {
			t.Fatalf("ListTokens: got %d, want 2", len(tokens))
		}
	})

	t.Run("ListTokensForIntegration", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)
		ctx := context.Background()

		user := mustCreateUser(t, svc, "alice@test.com")
		for _, tok := range []*core.IntegrationToken{
			{ID: "tok-1", UserID: user.ID, Integration: "svc-a", Connection: "default", Instance: "i1", AccessToken: "a", RefreshToken: "r"},
			{ID: "tok-2", UserID: user.ID, Integration: "svc-a", Connection: "default", Instance: "i2", AccessToken: "b", RefreshToken: "s"},
			{ID: "tok-3", UserID: user.ID, Integration: "svc-b", Connection: "default", Instance: "i1", AccessToken: "c", RefreshToken: "u"},
		} {
			if err := svc.Tokens.StoreToken(ctx, tok); err != nil {
				t.Fatalf("StoreToken(%s): %v", tok.ID, err)
			}
		}

		tokens, err := svc.Tokens.ListTokensForIntegration(ctx, user.ID, "svc-a")
		if err != nil {
			t.Fatalf("ListTokensForIntegration: %v", err)
		}
		if len(tokens) != 2 {
			t.Fatalf("got %d tokens, want 2", len(tokens))
		}
	})

	t.Run("ListTokensForConnection", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)
		ctx := context.Background()

		user := mustCreateUser(t, svc, "alice@test.com")
		for _, tok := range []*core.IntegrationToken{
			{ID: "tok-1", UserID: user.ID, Integration: "svc", Connection: "conn-a", Instance: "i1", AccessToken: "a", RefreshToken: "r"},
			{ID: "tok-2", UserID: user.ID, Integration: "svc", Connection: "conn-a", Instance: "i2", AccessToken: "b", RefreshToken: "s"},
			{ID: "tok-3", UserID: user.ID, Integration: "svc", Connection: "conn-b", Instance: "i1", AccessToken: "c", RefreshToken: "u"},
		} {
			if err := svc.Tokens.StoreToken(ctx, tok); err != nil {
				t.Fatalf("StoreToken(%s): %v", tok.ID, err)
			}
		}

		tokens, err := svc.Tokens.ListTokensForConnection(ctx, user.ID, "svc", "conn-a")
		if err != nil {
			t.Fatalf("ListTokensForConnection: %v", err)
		}
		if len(tokens) != 2 {
			t.Fatalf("got %d tokens, want 2", len(tokens))
		}
	})

	t.Run("DeleteToken", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)
		ctx := context.Background()

		user := mustCreateUser(t, svc, "alice@test.com")
		tok := &core.IntegrationToken{
			ID: "tok-del", UserID: user.ID, Integration: "svc",
			Connection: "default", Instance: "i1",
			AccessToken: "a", RefreshToken: "r",
		}
		if err := svc.Tokens.StoreToken(ctx, tok); err != nil {
			t.Fatalf("StoreToken: %v", err)
		}

		if err := svc.Tokens.DeleteToken(ctx, "tok-del"); err != nil {
			t.Fatalf("DeleteToken: %v", err)
		}

		_, err := svc.Tokens.Token(ctx, user.ID, "svc", "default", "i1")
		if err != core.ErrNotFound {
			t.Fatalf("Token after delete = %v, want ErrNotFound", err)
		}
	})

	t.Run("DeleteToken_nonexistent_no_error", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)

		if err := svc.Tokens.DeleteToken(context.Background(), "does-not-exist"); err != nil {
			t.Fatalf("DeleteToken nonexistent: %v", err)
		}
	})

	t.Run("StoreToken_upsert", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)
		ctx := context.Background()

		user := mustCreateUser(t, svc, "alice@test.com")
		tok := &core.IntegrationToken{
			ID: "tok-upsert", UserID: user.ID, Integration: "svc",
			Connection: "default", Instance: "i1",
			AccessToken: "original", RefreshToken: "r",
		}
		if err := svc.Tokens.StoreToken(ctx, tok); err != nil {
			t.Fatalf("first StoreToken: %v", err)
		}

		tok.ID = "tok-upsert-replacement"
		tok.AccessToken = "updated"
		if err := svc.Tokens.StoreToken(ctx, tok); err != nil {
			t.Fatalf("second StoreToken: %v", err)
		}

		got, err := svc.Tokens.Token(ctx, user.ID, "svc", "default", "i1")
		if err != nil {
			t.Fatalf("Token: %v", err)
		}
		if got.ID != "tok-upsert" {
			t.Errorf("ID = %q, want %q", got.ID, "tok-upsert")
		}
		if got.AccessToken != "updated" {
			t.Errorf("AccessToken = %q, want %q", got.AccessToken, "updated")
		}

		tokens, err := svc.Tokens.ListTokensForConnection(ctx, user.ID, "svc", "default")
		if err != nil {
			t.Fatalf("ListTokensForConnection: %v", err)
		}
		if len(tokens) != 1 {
			t.Fatalf("got %d tokens, want 1", len(tokens))
		}
	})

	t.Run("ListTokensForConnection_dedupes_legacy_duplicate_rows", func(t *testing.T) {
		t.Parallel()
		svc, db := newTestServicesWithDB(t)
		ctx := context.Background()

		user := mustCreateUser(t, svc, "dupes@test.com")
		newest := &core.IntegrationToken{
			ID:           "tok-primary",
			UserID:       user.ID,
			Integration:  "svc",
			Connection:   "default",
			Instance:     "i1",
			AccessToken:  "newest",
			RefreshToken: "refresh-newest",
		}
		if err := svc.Tokens.StoreToken(ctx, newest); err != nil {
			t.Fatalf("StoreToken newest: %v", err)
		}

		legacySource := &core.IntegrationToken{
			ID:           "tok-legacy-source",
			UserID:       user.ID,
			Integration:  "svc",
			Connection:   "default",
			Instance:     "legacy-source",
			AccessToken:  "legacy",
			RefreshToken: "refresh-legacy",
		}
		if err := svc.Tokens.StoreToken(ctx, legacySource); err != nil {
			t.Fatalf("StoreToken legacy source: %v", err)
		}

		store := db.ObjectStore(coredata.StoreIntegrationTokens)
		primaryRaw, err := store.Get(ctx, newest.ID)
		if err != nil {
			t.Fatalf("Get primary raw: %v", err)
		}
		legacyRaw, err := store.Get(ctx, legacySource.ID)
		if err != nil {
			t.Fatalf("Get legacy raw: %v", err)
		}
		if err := store.Delete(ctx, legacySource.ID); err != nil {
			t.Fatalf("Delete legacy source: %v", err)
		}

		duplicate := indexeddb.Record{}
		for k, v := range legacyRaw {
			duplicate[k] = v
		}
		duplicate["id"] = "tok-legacy-duplicate"
		duplicate["user_id"] = user.ID
		duplicate["integration"] = "svc"
		duplicate["connection"] = "default"
		duplicate["instance"] = "i1"
		duplicate["created_at"] = recOrNow(primaryRaw, "created_at").Add(-time.Minute)
		duplicate["updated_at"] = recOrNow(primaryRaw, "updated_at").Add(-time.Minute)
		if err := store.Put(ctx, duplicate); err != nil {
			t.Fatalf("Put duplicate raw token: %v", err)
		}

		tokens, err := svc.Tokens.ListTokensForConnection(ctx, user.ID, "svc", "default")
		if err != nil {
			t.Fatalf("ListTokensForConnection: %v", err)
		}
		if len(tokens) != 1 {
			t.Fatalf("got %d tokens, want 1", len(tokens))
		}
		if tokens[0].ID != newest.ID {
			t.Fatalf("ID = %q, want %q", tokens[0].ID, newest.ID)
		}
		if tokens[0].AccessToken != "newest" {
			t.Fatalf("AccessToken = %q, want %q", tokens[0].AccessToken, "newest")
		}
	})

	t.Run("ConcurrentTokenWrites", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)
		ctx := context.Background()

		user := mustCreateUser(t, svc, "concurrent@test.com")

		const count = 10
		errs := make([]error, count)
		var wg sync.WaitGroup
		for i := range count {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				errs[idx] = svc.Tokens.StoreToken(ctx, &core.IntegrationToken{
					ID:           fmt.Sprintf("tok-%d", idx),
					UserID:       user.ID,
					Integration:  "svc",
					Connection:   "default",
					Instance:     fmt.Sprintf("inst-%d", idx),
					AccessToken:  "access",
					RefreshToken: "refresh",
				})
			}(i)
		}
		wg.Wait()

		for i, err := range errs {
			if err != nil {
				t.Errorf("goroutine %d: %v", i, err)
			}
		}

		tokens, err := svc.Tokens.ListTokens(ctx, user.ID)
		if err != nil {
			t.Fatalf("ListTokens: %v", err)
		}
		if len(tokens) != count {
			t.Fatalf("got %d tokens, want %d", len(tokens), count)
		}
	})

	t.Run("EncryptsTokensAtRest", func(t *testing.T) {
		t.Parallel()
		svc, db := newTestServicesWithDB(t)
		ctx := context.Background()

		user := mustCreateUser(t, svc, "enc@test.com")
		tok := &core.IntegrationToken{
			ID:           "enc-tok",
			UserID:       user.ID,
			Integration:  "svc",
			Connection:   "default",
			Instance:     "i1",
			AccessToken:  "plaintext-access",
			RefreshToken: "plaintext-refresh",
		}
		if err := svc.Tokens.StoreToken(ctx, tok); err != nil {
			t.Fatalf("StoreToken: %v", err)
		}

		raw, err := db.ObjectStore(coredata.StoreIntegrationTokens).Get(ctx, "enc-tok")
		if err != nil {
			t.Fatalf("raw Get: %v", err)
		}
		accessEncrypted, _ := raw["access_token_encrypted"].(string)
		refreshEncrypted, _ := raw["refresh_token_encrypted"].(string)

		if accessEncrypted == "plaintext-access" {
			t.Error("access_token_encrypted stored as plaintext")
		}
		if refreshEncrypted == "plaintext-refresh" {
			t.Error("refresh_token_encrypted stored as plaintext")
		}
		if accessEncrypted == "" {
			t.Error("access_token_encrypted should not be empty")
		}
		if refreshEncrypted == "" {
			t.Error("refresh_token_encrypted should not be empty")
		}

		got, err := svc.Tokens.Token(ctx, user.ID, "svc", "default", "i1")
		if err != nil {
			t.Fatalf("Token: %v", err)
		}
		if got.AccessToken != "plaintext-access" {
			t.Errorf("decrypted AccessToken = %q, want %q", got.AccessToken, "plaintext-access")
		}
		if got.RefreshToken != "plaintext-refresh" {
			t.Errorf("decrypted RefreshToken = %q, want %q", got.RefreshToken, "plaintext-refresh")
		}
	})
}

func recOrNow(rec indexeddb.Record, key string) time.Time {
	if v, ok := rec[key].(time.Time); ok && !v.IsZero() {
		return v
	}
	return time.Now()
}

func TestAPITokenService(t *testing.T) {
	t.Parallel()

	t.Run("StoreAndValidate_round_trip", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)
		ctx := context.Background()

		user := mustCreateUser(t, svc, "alice@test.com")
		token := &core.APIToken{
			ID:          "api-1",
			UserID:      user.ID,
			Name:        "ci-token",
			HashedToken: "sha256:abc123",
			Scopes:      "read:tokens",
		}
		if err := svc.APITokens.StoreAPIToken(ctx, token); err != nil {
			t.Fatalf("StoreAPIToken: %v", err)
		}

		got, err := svc.APITokens.ValidateAPIToken(ctx, "sha256:abc123")
		if err != nil {
			t.Fatalf("ValidateAPIToken: %v", err)
		}
		if got.UserID != user.ID {
			t.Errorf("UserID = %q, want %q", got.UserID, user.ID)
		}
		if got.Name != "ci-token" {
			t.Errorf("Name = %q, want %q", got.Name, "ci-token")
		}
		if got.Scopes != "read:tokens" {
			t.Errorf("Scopes = %q, want %q", got.Scopes, "read:tokens")
		}
	})

	t.Run("ValidateAPIToken_not_found", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)

		_, err := svc.APITokens.ValidateAPIToken(context.Background(), "sha256:nonexistent")
		if err != core.ErrNotFound {
			t.Fatalf("ValidateAPIToken = %v, want ErrNotFound", err)
		}
	})

	t.Run("ValidateAPIToken_expired", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)
		ctx := context.Background()

		user := mustCreateUser(t, svc, "alice@test.com")
		past := time.Now().Add(-time.Hour)
		token := &core.APIToken{
			ID:          "api-expired",
			UserID:      user.ID,
			Name:        "expired",
			HashedToken: "sha256:expired",
			ExpiresAt:   &past,
		}
		if err := svc.APITokens.StoreAPIToken(ctx, token); err != nil {
			t.Fatalf("StoreAPIToken: %v", err)
		}

		_, err := svc.APITokens.ValidateAPIToken(ctx, "sha256:expired")
		if err != core.ErrNotFound {
			t.Fatalf("ValidateAPIToken expired = %v, want ErrNotFound", err)
		}
	})

	t.Run("ValidateAPIToken_not_expired", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)
		ctx := context.Background()

		user := mustCreateUser(t, svc, "alice@test.com")
		future := time.Now().Add(time.Hour)
		token := &core.APIToken{
			ID:          "api-valid",
			UserID:      user.ID,
			Name:        "valid",
			HashedToken: "sha256:valid",
			ExpiresAt:   &future,
		}
		if err := svc.APITokens.StoreAPIToken(ctx, token); err != nil {
			t.Fatalf("StoreAPIToken: %v", err)
		}

		got, err := svc.APITokens.ValidateAPIToken(ctx, "sha256:valid")
		if err != nil {
			t.Fatalf("ValidateAPIToken: %v", err)
		}
		if got.Name != "valid" {
			t.Errorf("Name = %q, want %q", got.Name, "valid")
		}
	})

	t.Run("ListAPITokens_by_user", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)
		ctx := context.Background()

		user := mustCreateUser(t, svc, "alice@test.com")
		for _, tok := range []*core.APIToken{
			{ID: "api-a", UserID: user.ID, Name: "a", HashedToken: "sha256:aaa"},
			{ID: "api-b", UserID: user.ID, Name: "b", HashedToken: "sha256:bbb"},
		} {
			if err := svc.APITokens.StoreAPIToken(ctx, tok); err != nil {
				t.Fatalf("StoreAPIToken(%s): %v", tok.ID, err)
			}
		}

		tokens, err := svc.APITokens.ListAPITokens(ctx, user.ID)
		if err != nil {
			t.Fatalf("ListAPITokens: %v", err)
		}
		if len(tokens) != 2 {
			t.Fatalf("got %d, want 2", len(tokens))
		}
	})

	t.Run("RevokeAPIToken", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)
		ctx := context.Background()

		user := mustCreateUser(t, svc, "alice@test.com")
		token := &core.APIToken{
			ID:          "api-rev",
			UserID:      user.ID,
			Name:        "revokable",
			HashedToken: "sha256:revoke",
		}
		if err := svc.APITokens.StoreAPIToken(ctx, token); err != nil {
			t.Fatalf("StoreAPIToken: %v", err)
		}

		if err := svc.APITokens.RevokeAPIToken(ctx, user.ID, "api-rev"); err != nil {
			t.Fatalf("RevokeAPIToken: %v", err)
		}

		_, err := svc.APITokens.ValidateAPIToken(ctx, "sha256:revoke")
		if err != core.ErrNotFound {
			t.Fatalf("ValidateAPIToken after revoke = %v, want ErrNotFound", err)
		}
	})

	t.Run("RevokeAPIToken_nonexistent", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)

		err := svc.APITokens.RevokeAPIToken(context.Background(), "no-user", "no-id")
		if err != core.ErrNotFound {
			t.Fatalf("RevokeAPIToken = %v, want ErrNotFound", err)
		}
	})

	t.Run("RevokeAllAPITokens", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)
		ctx := context.Background()

		user := mustCreateUser(t, svc, "alice@test.com")
		for i, hash := range []string{"sha256:one", "sha256:two", "sha256:three"} {
			tok := &core.APIToken{
				ID:          fmt.Sprintf("api-%d", i),
				UserID:      user.ID,
				Name:        fmt.Sprintf("token-%d", i),
				HashedToken: hash,
			}
			if err := svc.APITokens.StoreAPIToken(ctx, tok); err != nil {
				t.Fatalf("StoreAPIToken(%d): %v", i, err)
			}
		}

		deleted, err := svc.APITokens.RevokeAllAPITokens(ctx, user.ID)
		if err != nil {
			t.Fatalf("RevokeAllAPITokens: %v", err)
		}
		if deleted != 3 {
			t.Errorf("deleted = %d, want 3", deleted)
		}

		tokens, err := svc.APITokens.ListAPITokens(ctx, user.ID)
		if err != nil {
			t.Fatalf("ListAPITokens: %v", err)
		}
		if len(tokens) != 0 {
			t.Errorf("got %d tokens after revoke-all, want 0", len(tokens))
		}
	})

	t.Run("StoreAPIToken_generates_ID", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)
		ctx := context.Background()

		user := mustCreateUser(t, svc, "alice@test.com")
		token := &core.APIToken{
			UserID:      user.ID,
			Name:        "auto-id",
			HashedToken: "sha256:auto",
		}
		if err := svc.APITokens.StoreAPIToken(ctx, token); err != nil {
			t.Fatalf("StoreAPIToken: %v", err)
		}
		if token.ID == "" {
			t.Error("ID should be auto-generated")
		}
	})
}

func TestServicesPingAndClose(t *testing.T) {
	t.Parallel()

	t.Run("Ping_succeeds", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)

		if err := svc.Ping(context.Background()); err != nil {
			t.Fatalf("Ping: %v", err)
		}
	})

	t.Run("Ping_propagates_error", func(t *testing.T) {
		t.Parallel()
		svc, db := newTestServicesWithDB(t)
		db.Err = errors.New("db down")

		if err := svc.Ping(context.Background()); err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}
