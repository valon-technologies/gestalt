package coredata_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	corecrypto "github.com/valon-technologies/gestalt/server/core/crypto"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/emailutil"
	"github.com/valon-technologies/gestalt/server/internal/principal"
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

	t.Run("backfills_one_canonical_identity_for_case_insensitive_duplicate_legacy_users", func(t *testing.T) {
		t.Parallel()

		db := &coretesting.StubIndexedDB{}
		ctx := context.Background()
		older := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
		newer := older.Add(time.Hour)
		if err := db.ObjectStore(coredata.StoreUsers).Add(ctx, indexeddb.Record{
			"id":               "legacy-mixed",
			"email":            "User@Example.com",
			"normalized_email": "user@example.com",
			"display_name":     "",
			"created_at":       older,
			"updated_at":       older,
		}); err != nil {
			t.Fatalf("seed mixed-case legacy user: %v", err)
		}
		if err := db.ObjectStore(coredata.StoreUsers).Add(ctx, indexeddb.Record{
			"id":               "legacy-canonical",
			"email":            "user@example.com",
			"normalized_email": "user@example.com",
			"display_name":     "",
			"created_at":       newer,
			"updated_at":       newer,
		}); err != nil {
			t.Fatalf("seed canonical legacy user: %v", err)
		}

		enc, err := corecrypto.NewAESGCM([]byte(testEncryptionKey))
		if err != nil {
			t.Fatalf("NewAESGCM: %v", err)
		}
		svc, err := coredata.New(db, enc)
		if err != nil {
			t.Fatalf("coredata.New: %v", err)
		}

		if _, err := svc.Identities.GetIdentity(ctx, "legacy-canonical"); err != nil {
			t.Fatalf("GetIdentity(canonical winner): %v", err)
		}
		if _, err := svc.Identities.GetIdentity(ctx, "legacy-mixed"); err != core.ErrNotFound {
			t.Fatalf("GetIdentity(mixed-case loser) = %v, want ErrNotFound", err)
		}
		binding, err := svc.IdentityAuthBindings.GetBinding(ctx, core.IdentityAuthBindingKindEmail, "legacy", "user@example.com")
		if err != nil {
			t.Fatalf("GetBinding(canonical email): %v", err)
		}
		if binding.IdentityID != "legacy-canonical" {
			t.Fatalf("binding.IdentityID = %q, want %q", binding.IdentityID, "legacy-canonical")
		}
		count, err := svc.DB.ObjectStore(coredata.StoreIdentities).Count(ctx, nil)
		if err != nil {
			t.Fatalf("Count identities: %v", err)
		}
		if count != 1 {
			t.Fatalf("identities count = %d, want 1", count)
		}
	})

	t.Run("skips_unreadable_legacy_integration_tokens_during_canonical_backfill", func(t *testing.T) {
		t.Parallel()

		db := &coretesting.StubIndexedDB{}
		ctx := context.Background()
		createdAt := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
		seedLegacyUserRecord(t, &coredata.Services{DB: db}, "legacy-user", "user@example.com", createdAt)

		enc, err := corecrypto.NewAESGCM([]byte(testEncryptionKey))
		if err != nil {
			t.Fatalf("NewAESGCM: %v", err)
		}
		accessEnc, refreshEnc, err := enc.EncryptTokenPair("plaintext-access", "")
		if err != nil {
			t.Fatalf("EncryptTokenPair: %v", err)
		}
		if err := db.ObjectStore(coredata.StoreIntegrationTokens).Add(ctx, indexeddb.Record{
			"id":                      "good-token",
			"user_id":                 "legacy-user",
			"integration":             "slack",
			"connection":              "workspace",
			"instance":                "",
			"access_token_encrypted":  accessEnc,
			"refresh_token_encrypted": refreshEnc,
			"scopes":                  "channels:read",
			"created_at":              createdAt,
			"updated_at":              createdAt,
		}); err != nil {
			t.Fatalf("seed valid integration token: %v", err)
		}
		if err := db.ObjectStore(coredata.StoreIntegrationTokens).Add(ctx, indexeddb.Record{
			"id":                      "bad-token",
			"user_id":                 "legacy-user",
			"integration":             "github",
			"connection":              "workspace",
			"instance":                "",
			"access_token_encrypted":  "not-valid-ciphertext",
			"refresh_token_encrypted": "",
			"created_at":              createdAt,
			"updated_at":              createdAt,
		}); err != nil {
			t.Fatalf("seed invalid integration token: %v", err)
		}

		svc, err := coredata.New(db, enc)
		if err != nil {
			t.Fatalf("coredata.New: %v", err)
		}

		good, err := svc.ExternalCredentials.GetCredential(ctx, "legacy-user", "slack", "workspace", "")
		if err != nil {
			t.Fatalf("GetCredential valid token: %v", err)
		}
		if good.IdentityID != "legacy-user" {
			t.Fatalf("valid credential identity_id = %q, want %q", good.IdentityID, "legacy-user")
		}
		if _, err := svc.ExternalCredentials.GetCredential(ctx, "legacy-user", "github", "workspace", ""); !errors.Is(err, core.ErrNotFound) {
			t.Fatalf("GetCredential invalid token err = %v, want ErrNotFound", err)
		}
	})

	t.Run("skips_malformed_managed_identity_grants_during_canonical_backfill", func(t *testing.T) {
		t.Parallel()

		db := &coretesting.StubIndexedDB{}
		ctx := context.Background()
		createdAt := time.Date(2026, 1, 3, 12, 0, 0, 0, time.UTC)
		if err := db.ObjectStore(coredata.StoreManagedIdentities).Add(ctx, indexeddb.Record{
			"id":                     "mi-1",
			"display_name":           "Deploy Bot",
			"created_by_identity_id": "legacy-user",
			"created_at":             createdAt,
			"updated_at":             createdAt,
		}); err != nil {
			t.Fatalf("seed managed identity: %v", err)
		}
		if err := db.ObjectStore(coredata.StoreManagedIdentityGrants).Add(ctx, indexeddb.Record{
			"id":              "grant-good",
			"identity_id":     "mi-1",
			"plugin":          "slack",
			"operations_json": `["read"]`,
			"created_at":      createdAt,
			"updated_at":      createdAt,
		}); err != nil {
			t.Fatalf("seed valid managed identity grant: %v", err)
		}
		if err := db.ObjectStore(coredata.StoreManagedIdentityGrants).Add(ctx, indexeddb.Record{
			"id":              "grant-bad",
			"identity_id":     "mi-1",
			"plugin":          "github",
			"operations_json": `{`,
			"created_at":      createdAt,
			"updated_at":      createdAt,
		}); err != nil {
			t.Fatalf("seed invalid managed identity grant: %v", err)
		}

		enc, err := corecrypto.NewAESGCM([]byte(testEncryptionKey))
		if err != nil {
			t.Fatalf("NewAESGCM: %v", err)
		}
		svc, err := coredata.New(db, enc)
		if err != nil {
			t.Fatalf("coredata.New: %v", err)
		}

		access, err := svc.IdentityPluginAccess.GetAccess(ctx, "mi-1", "slack")
		if err != nil {
			t.Fatalf("GetAccess valid grant: %v", err)
		}
		if access.Plugin != "slack" {
			t.Fatalf("valid access plugin = %q, want %q", access.Plugin, "slack")
		}
		if _, err := svc.IdentityPluginAccess.GetAccess(ctx, "mi-1", "github"); !errors.Is(err, core.ErrNotFound) {
			t.Fatalf("GetAccess invalid grant err = %v, want ErrNotFound", err)
		}
	})

	t.Run("backfills_multiple_identity_bindings_without_blank_auth_subject_keys", func(t *testing.T) {
		t.Parallel()

		db := &coretesting.StubIndexedDB{}
		ctx := context.Background()
		createdAt := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
		for _, rec := range []indexeddb.Record{
			{
				"id":               "legacy-a",
				"email":            "alice@example.com",
				"normalized_email": "alice@example.com",
				"display_name":     "",
				"created_at":       createdAt,
				"updated_at":       createdAt,
			},
			{
				"id":               "legacy-b",
				"email":            "bob@example.com",
				"normalized_email": "bob@example.com",
				"display_name":     "",
				"created_at":       createdAt.Add(time.Minute),
				"updated_at":       createdAt.Add(time.Minute),
			},
		} {
			if err := db.ObjectStore(coredata.StoreUsers).Add(ctx, rec); err != nil {
				t.Fatalf("seed legacy user %q: %v", rec["id"], err)
			}
		}

		enc, err := corecrypto.NewAESGCM([]byte(testEncryptionKey))
		if err != nil {
			t.Fatalf("NewAESGCM: %v", err)
		}
		svc, err := coredata.New(db, enc)
		if err != nil {
			t.Fatalf("coredata.New: %v", err)
		}

		bindings, err := svc.DB.ObjectStore(coredata.StoreIdentityAuthBindings).GetAll(ctx, nil)
		if err != nil {
			t.Fatalf("GetAll identity auth bindings: %v", err)
		}
		if len(bindings) != 2 {
			t.Fatalf("identity auth bindings len = %d, want 2", len(bindings))
		}
		for _, rec := range bindings {
			if got := rec["binding_kind"]; got != core.IdentityAuthBindingKindEmail {
				t.Fatalf("binding_kind = %v, want %q", got, core.IdentityAuthBindingKindEmail)
			}
			if got := rec["authority"]; got != "legacy" {
				t.Fatalf("authority = %v, want %q", got, "legacy")
			}
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
		credential, err := svc.ExternalCredentials.GetCredential(ctx, user.ID, "test-svc", "default", "inst-1")
		if err != nil {
			t.Fatalf("GetCredential: %v", err)
		}
		if credential.AuthType != "oauth2" {
			t.Errorf("AuthType = %q, want %q", credential.AuthType, "oauth2")
		}
		if credential.MetadataJSON != `{"key":"val"}` {
			t.Errorf("MetadataJSON = %q, want %q", credential.MetadataJSON, `{"key":"val"}`)
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
		if _, err := svc.ExternalCredentials.GetCredential(ctx, user.ID, "svc", "default", "i1"); err != core.ErrNotFound {
			t.Fatalf("GetCredential after delete = %v, want ErrNotFound", err)
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

		if err := svc.Tokens.DeleteToken(ctx, newest.ID); err != nil {
			t.Fatalf("DeleteToken newest: %v", err)
		}
		credential, err := svc.ExternalCredentials.GetCredential(ctx, user.ID, "svc", "default", "i1")
		if err != nil {
			t.Fatalf("GetCredential after deleting newest duplicate: %v", err)
		}
		var payload map[string]string
		if err := json.Unmarshal([]byte(credential.PayloadEncrypted), &payload); err != nil {
			t.Fatalf("Unmarshal credential payload: %v", err)
		}
		wantAccess, _ := duplicate["access_token_encrypted"].(string)
		wantRefresh, _ := duplicate["refresh_token_encrypted"].(string)
		if payload["access_token_encrypted"] != wantAccess {
			t.Fatalf("access_token_encrypted = %q, want %q", payload["access_token_encrypted"], wantAccess)
		}
		if payload["refresh_token_encrypted"] != wantRefresh {
			t.Fatalf("refresh_token_encrypted = %q, want %q", payload["refresh_token_encrypted"], wantRefresh)
		}
	})

	t.Run("startup_backfill_uses_deduped_legacy_winner_for_external_credentials", func(t *testing.T) {
		t.Parallel()

		svc, db := newTestServicesWithDB(t)
		ctx := context.Background()
		user := mustCreateUser(t, svc, "startup-dupes@test.com")

		newest := &core.IntegrationToken{
			ID:           "tok-startup-primary",
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
			ID:           "tok-startup-legacy",
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
		duplicate["id"] = "tok-startup-duplicate"
		duplicate["user_id"] = user.ID
		duplicate["integration"] = "svc"
		duplicate["connection"] = "default"
		duplicate["instance"] = "i1"
		duplicate["created_at"] = recOrNow(primaryRaw, "created_at").Add(-time.Minute)
		duplicate["updated_at"] = recOrNow(primaryRaw, "updated_at").Add(-time.Minute)
		if err := store.Put(ctx, duplicate); err != nil {
			t.Fatalf("Put duplicate raw token: %v", err)
		}

		enc, err := corecrypto.NewAESGCM([]byte(testEncryptionKey))
		if err != nil {
			t.Fatalf("NewAESGCM: %v", err)
		}
		reloaded, err := coredata.New(db, enc)
		if err != nil {
			t.Fatalf("reload services for startup backfill: %v", err)
		}

		credential, err := reloaded.ExternalCredentials.GetCredential(ctx, user.ID, "svc", "default", "i1")
		if err != nil {
			t.Fatalf("GetCredential after reload: %v", err)
		}
		var payload map[string]string
		if err := json.Unmarshal([]byte(credential.PayloadEncrypted), &payload); err != nil {
			t.Fatalf("Unmarshal credential payload: %v", err)
		}
		wantAccess, _ := primaryRaw["access_token_encrypted"].(string)
		wantRefresh, _ := primaryRaw["refresh_token_encrypted"].(string)
		if payload["access_token_encrypted"] != wantAccess {
			t.Fatalf("access_token_encrypted after reload = %q, want %q", payload["access_token_encrypted"], wantAccess)
		}
		if payload["refresh_token_encrypted"] != wantRefresh {
			t.Fatalf("refresh_token_encrypted after reload = %q, want %q", payload["refresh_token_encrypted"], wantRefresh)
		}
	})

	t.Run("startup_backfill_preserves_external_credential_auth_type", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name         string
			accessToken  string
			refreshToken string
			wantAuthType string
		}{
			{name: "oauth2", accessToken: "access", refreshToken: "refresh", wantAuthType: "oauth2"},
			{name: "bearer", accessToken: "access", wantAuthType: "bearer"},
			{name: "manual", wantAuthType: "manual"},
		}

		for _, tc := range tests {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				svc, db := newTestServicesWithDB(t)
				ctx := context.Background()
				user := mustCreateUser(t, svc, tc.name+"@test.com")

				token := &core.IntegrationToken{
					ID:           "tok-" + tc.name,
					UserID:       user.ID,
					Integration:  "svc",
					Connection:   "default",
					Instance:     "i1",
					AccessToken:  tc.accessToken,
					RefreshToken: tc.refreshToken,
				}
				if err := svc.Tokens.StoreToken(ctx, token); err != nil {
					t.Fatalf("StoreToken: %v", err)
				}

				enc, err := corecrypto.NewAESGCM([]byte(testEncryptionKey))
				if err != nil {
					t.Fatalf("NewAESGCM: %v", err)
				}
				reloaded, err := coredata.New(db, enc)
				if err != nil {
					t.Fatalf("reload services for startup backfill: %v", err)
				}

				credential, err := reloaded.ExternalCredentials.GetCredential(ctx, user.ID, "svc", "default", "i1")
				if err != nil {
					t.Fatalf("GetCredential after reload: %v", err)
				}
				if credential.AuthType != tc.wantAuthType {
					t.Fatalf("AuthType after reload = %q, want %q", credential.AuthType, tc.wantAuthType)
				}
			})
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
			Permissions: []core.AccessPermission{
				{Plugin: "sample", Operations: []string{"read"}},
				{Plugin: "other"},
			},
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
		access, err := svc.APITokenAccess.ListByToken(ctx, token.ID)
		if err != nil {
			t.Fatalf("ListByToken: %v", err)
		}
		if len(access) != 2 {
			t.Fatalf("token access len = %d, want 2", len(access))
		}
		if access[0].Plugin != "other" || !access[0].InvokeAllOperations {
			t.Fatalf("first token access = %+v, want plugin other with invoke-all", access[0])
		}
		if access[1].Plugin != "sample" || access[1].InvokeAllOperations || !reflect.DeepEqual(access[1].Operations, []string{"read"}) {
			t.Fatalf("second token access = %+v, want sample [read]", access[1])
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
		access, err := svc.APITokenAccess.ListByToken(ctx, token.ID)
		if err != nil {
			t.Fatalf("ListByToken after revoke: %v", err)
		}
		if len(access) != 0 {
			t.Fatalf("token access after revoke = %+v, want none", access)
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

	t.Run("RevokeAllAPITokens_preserves_access_for_owner_only_survivors", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)
		ctx := context.Background()

		user := mustCreateUser(t, svc, "alice@test.com")
		legacy := &core.APIToken{
			ID:          "legacy-token",
			UserID:      user.ID,
			Name:        "legacy",
			HashedToken: "sha256:legacy",
			Permissions: []core.AccessPermission{{Plugin: "legacy"}},
		}
		if err := svc.APITokens.StoreAPIToken(ctx, legacy); err != nil {
			t.Fatalf("StoreAPIToken legacy: %v", err)
		}

		permissionsJSON, err := json.Marshal([]core.AccessPermission{{Plugin: "owner-only"}})
		if err != nil {
			t.Fatalf("Marshal permissions: %v", err)
		}
		now := time.Now()
		if err := svc.DB.ObjectStore(coredata.StoreAPITokens).Add(ctx, indexeddb.Record{
			"id":               "owner-only-token",
			"owner_kind":       core.APITokenOwnerKindUser,
			"owner_id":         user.ID,
			"name":             "owner-only",
			"hashed_token":     "sha256:owner-only",
			"permissions_json": string(permissionsJSON),
			"created_at":       now,
			"updated_at":       now,
		}); err != nil {
			t.Fatalf("seed owner-only token: %v", err)
		}
		if err := svc.APITokens.BackfillTokenAccess(ctx); err != nil {
			t.Fatalf("BackfillTokenAccess: %v", err)
		}

		deleted, err := svc.APITokens.RevokeAllAPITokens(ctx, user.ID)
		if err != nil {
			t.Fatalf("RevokeAllAPITokens: %v", err)
		}
		if deleted != 1 {
			t.Fatalf("deleted = %d, want 1", deleted)
		}

		tokens, err := svc.APITokens.ListAPITokens(ctx, user.ID)
		if err != nil {
			t.Fatalf("ListAPITokens: %v", err)
		}
		if len(tokens) != 1 || tokens[0].ID != "owner-only-token" {
			t.Fatalf("remaining tokens = %+v, want owner-only survivor", tokens)
		}

		access, err := svc.APITokenAccess.ListByToken(ctx, "owner-only-token")
		if err != nil {
			t.Fatalf("ListByToken owner-only-token: %v", err)
		}
		if len(access) != 1 || access[0].Plugin != "owner-only" {
			t.Fatalf("owner-only token access = %+v, want surviving access", access)
		}
	})

	t.Run("BackfillTokenAccess_uses_scopes_when_permissions_are_empty", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)
		ctx := context.Background()

		user := mustCreateUser(t, svc, "scoped@test.com")
		now := time.Now().UTC()
		if err := svc.DB.ObjectStore(coredata.StoreAPITokens).Add(ctx, indexeddb.Record{
			"id":           "scoped-token",
			"user_id":      user.ID,
			"name":         "scoped",
			"hashed_token": "sha256:scoped",
			"scopes":       "alpha beta alpha",
			"created_at":   now,
			"updated_at":   now,
		}); err != nil {
			t.Fatalf("seed scoped api token: %v", err)
		}
		if err := svc.APITokens.BackfillTokenAccess(ctx); err != nil {
			t.Fatalf("BackfillTokenAccess: %v", err)
		}

		access, err := svc.APITokenAccess.ListByToken(ctx, "scoped-token")
		if err != nil {
			t.Fatalf("ListByToken: %v", err)
		}
		if len(access) != 2 {
			t.Fatalf("scope-backed access len = %d, want 2: %+v", len(access), access)
		}
		got := make(map[string]bool, len(access))
		for _, row := range access {
			got[row.Plugin] = row.InvokeAllOperations
		}
		if !reflect.DeepEqual(got, map[string]bool{"alpha": true, "beta": true}) {
			t.Fatalf("scope-backed access = %+v, want alpha/beta invoke-all", got)
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

func TestWorkflowExecutionRefService(t *testing.T) {
	t.Parallel()

	t.Run("PutGet_round_trips_permissions", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)
		ctx := context.Background()

		ref, err := svc.WorkflowExecutionRefs.Put(ctx, &coreworkflow.ExecutionReference{
			ID:           "exec-ref-123",
			ProviderName: "basic",
			Target: coreworkflow.Target{
				PluginName: "roadmap",
				Operation:  "sync",
			},
			SubjectID:   principal.UserSubjectID("user-123"),
			Permissions: []core.AccessPermission{{Plugin: "roadmap", Operations: []string{"sync"}}},
		})
		if err != nil {
			t.Fatalf("Put: %v", err)
		}
		if ref.SubjectID != principal.UserSubjectID("user-123") {
			t.Fatalf("SubjectID = %q, want %q", ref.SubjectID, principal.UserSubjectID("user-123"))
		}

		got, err := svc.WorkflowExecutionRefs.Get(ctx, "exec-ref-123")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		want := []core.AccessPermission{{Plugin: "roadmap", Operations: []string{"sync"}}}
		if !reflect.DeepEqual(got.Permissions, want) {
			t.Fatalf("Permissions = %#v, want %#v", got.Permissions, want)
		}
	})

	t.Run("Put_rejects_non_user_subject", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)
		ctx := context.Background()

		_, err := svc.WorkflowExecutionRefs.Put(ctx, &coreworkflow.ExecutionReference{
			ID:           "exec-ref-bad",
			ProviderName: "basic",
			Target: coreworkflow.Target{
				PluginName: "roadmap",
				Operation:  "sync",
			},
			SubjectID: principal.ManagedIdentitySubjectID("managed-123"),
		})
		if err == nil {
			t.Fatal("expected Put to reject non-user subject_id")
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
