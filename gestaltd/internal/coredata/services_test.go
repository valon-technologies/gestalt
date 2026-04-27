package coredata_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/emailutil"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

func newTestServices(t *testing.T) *coredata.Services {
	t.Helper()
	return coretesting.NewStubServices(t)
}

func newTestServicesWithDB(t *testing.T) (*coredata.Services, *coretesting.StubIndexedDB) {
	t.Helper()
	db := &coretesting.StubIndexedDB{}
	svc, err := coredata.New(db)
	if err != nil {
		t.Fatalf("coredata.New: %v", err)
	}
	coretesting.AttachStubExternalCredentials(svc)
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
		if _, err := coredata.New(db); err != nil {
			t.Fatalf("first New: %v", err)
		}
		if _, err := coredata.New(db); err != nil {
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

		svc, err := coredata.New(db)
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

		svc, err := coredata.New(db)
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

		svc, err := coredata.New(db)
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
		svc, err := coredata.New(db)
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

func TestExternalCredentialProvider(t *testing.T) {
	t.Parallel()

	t.Run("StoreAndRetrieve_round_trip", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)
		ctx := context.Background()

		user := mustCreateUser(t, svc, "alice@test.com")
		expires := time.Now().Add(time.Hour).Truncate(time.Second)
		token := &core.IntegrationToken{
			ID:           "tok-1",
			SubjectID:    principal.UserSubjectID(user.ID),
			Integration:  "test-svc",
			Connection:   "default",
			Instance:     "inst-1",
			AccessToken:  "access-secret",
			RefreshToken: "refresh-secret",
			Scopes:       "read,write",
			ExpiresAt:    &expires,
			MetadataJSON: `{"key":"val"}`,
		}
		if err := svc.ExternalCredentials.PutCredential(ctx, token); err != nil {
			t.Fatalf("PutCredential: %v", err)
		}

		got, err := svc.ExternalCredentials.GetCredential(ctx, principal.UserSubjectID(user.ID), "test-svc", "default", "inst-1")
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

	t.Run("GetCredential_not_found", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)

		_, err := svc.ExternalCredentials.GetCredential(context.Background(), "no-user", "no-svc", "no-conn", "no-inst")
		if err != core.ErrNotFound {
			t.Fatalf("Token = %v, want ErrNotFound", err)
		}
	})

	t.Run("ListCredentials_by_user", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)
		ctx := context.Background()

		userA := mustCreateUser(t, svc, "alice@test.com")
		userB := mustCreateUser(t, svc, "bob@test.com")

		for _, tok := range []*core.IntegrationToken{
			{ID: "tok-a1", SubjectID: principal.UserSubjectID(userA.ID), Integration: "svc-a", Connection: "default", Instance: "i1", AccessToken: "a1", RefreshToken: "r1"},
			{ID: "tok-a2", SubjectID: principal.UserSubjectID(userA.ID), Integration: "svc-b", Connection: "default", Instance: "i2", AccessToken: "a2", RefreshToken: "r2"},
			{ID: "tok-b1", SubjectID: principal.UserSubjectID(userB.ID), Integration: "svc-a", Connection: "default", Instance: "i1", AccessToken: "a3", RefreshToken: "r3"},
		} {
			if err := svc.ExternalCredentials.PutCredential(ctx, tok); err != nil {
				t.Fatalf("PutCredential(%s): %v", tok.ID, err)
			}
		}

		tokens, err := svc.ExternalCredentials.ListCredentials(ctx, principal.UserSubjectID(userA.ID))
		if err != nil {
			t.Fatalf("ListCredentials: %v", err)
		}
		if len(tokens) != 2 {
			t.Fatalf("ListCredentials: got %d, want 2", len(tokens))
		}
	})

	t.Run("ListCredentialsForProvider", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)
		ctx := context.Background()

		user := mustCreateUser(t, svc, "alice@test.com")
		for _, tok := range []*core.IntegrationToken{
			{ID: "tok-1", SubjectID: principal.UserSubjectID(user.ID), Integration: "svc-a", Connection: "default", Instance: "i1", AccessToken: "a", RefreshToken: "r"},
			{ID: "tok-2", SubjectID: principal.UserSubjectID(user.ID), Integration: "svc-a", Connection: "default", Instance: "i2", AccessToken: "b", RefreshToken: "s"},
			{ID: "tok-3", SubjectID: principal.UserSubjectID(user.ID), Integration: "svc-b", Connection: "default", Instance: "i1", AccessToken: "c", RefreshToken: "u"},
		} {
			if err := svc.ExternalCredentials.PutCredential(ctx, tok); err != nil {
				t.Fatalf("PutCredential(%s): %v", tok.ID, err)
			}
		}

		tokens, err := svc.ExternalCredentials.ListCredentialsForProvider(ctx, principal.UserSubjectID(user.ID), "svc-a")
		if err != nil {
			t.Fatalf("ListCredentialsForProvider: %v", err)
		}
		if len(tokens) != 2 {
			t.Fatalf("got %d tokens, want 2", len(tokens))
		}
	})

	t.Run("ListCredentialsForConnection", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)
		ctx := context.Background()

		user := mustCreateUser(t, svc, "alice@test.com")
		for _, tok := range []*core.IntegrationToken{
			{ID: "tok-1", SubjectID: principal.UserSubjectID(user.ID), Integration: "svc", Connection: "conn-a", Instance: "i1", AccessToken: "a", RefreshToken: "r"},
			{ID: "tok-2", SubjectID: principal.UserSubjectID(user.ID), Integration: "svc", Connection: "conn-a", Instance: "i2", AccessToken: "b", RefreshToken: "s"},
			{ID: "tok-3", SubjectID: principal.UserSubjectID(user.ID), Integration: "svc", Connection: "conn-b", Instance: "i1", AccessToken: "c", RefreshToken: "u"},
		} {
			if err := svc.ExternalCredentials.PutCredential(ctx, tok); err != nil {
				t.Fatalf("PutCredential(%s): %v", tok.ID, err)
			}
		}

		tokens, err := svc.ExternalCredentials.ListCredentialsForConnection(ctx, principal.UserSubjectID(user.ID), "svc", "conn-a")
		if err != nil {
			t.Fatalf("ListCredentialsForConnection: %v", err)
		}
		if len(tokens) != 2 {
			t.Fatalf("got %d tokens, want 2", len(tokens))
		}
	})

	t.Run("DeleteCredential", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)
		ctx := context.Background()

		user := mustCreateUser(t, svc, "alice@test.com")
		tok := &core.IntegrationToken{
			ID: "tok-del", SubjectID: principal.UserSubjectID(user.ID), Integration: "svc",
			Connection: "default", Instance: "i1",
			AccessToken: "a", RefreshToken: "r",
		}
		if err := svc.ExternalCredentials.PutCredential(ctx, tok); err != nil {
			t.Fatalf("PutCredential: %v", err)
		}

		if err := svc.ExternalCredentials.DeleteCredential(ctx, "tok-del"); err != nil {
			t.Fatalf("DeleteCredential: %v", err)
		}

		_, err := svc.ExternalCredentials.GetCredential(ctx, principal.UserSubjectID(user.ID), "svc", "default", "i1")
		if err != core.ErrNotFound {
			t.Fatalf("Token after delete = %v, want ErrNotFound", err)
		}
	})

	t.Run("DeleteCredential_nonexistent_no_error", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)

		if err := svc.ExternalCredentials.DeleteCredential(context.Background(), "does-not-exist"); err != nil {
			t.Fatalf("DeleteCredential nonexistent: %v", err)
		}
	})

	t.Run("PutCredential_upsert", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)
		ctx := context.Background()

		user := mustCreateUser(t, svc, "alice@test.com")
		tok := &core.IntegrationToken{
			ID: "tok-upsert", SubjectID: principal.UserSubjectID(user.ID), Integration: "svc",
			Connection: "default", Instance: "i1",
			AccessToken: "original", RefreshToken: "r",
		}
		if err := svc.ExternalCredentials.PutCredential(ctx, tok); err != nil {
			t.Fatalf("first PutCredential: %v", err)
		}

		tok.ID = "tok-upsert-replacement"
		tok.AccessToken = "updated"
		if err := svc.ExternalCredentials.PutCredential(ctx, tok); err != nil {
			t.Fatalf("second PutCredential: %v", err)
		}

		got, err := svc.ExternalCredentials.GetCredential(ctx, principal.UserSubjectID(user.ID), "svc", "default", "i1")
		if err != nil {
			t.Fatalf("Token: %v", err)
		}
		if got.ID != "tok-upsert" {
			t.Errorf("ID = %q, want %q", got.ID, "tok-upsert")
		}
		if got.AccessToken != "updated" {
			t.Errorf("AccessToken = %q, want %q", got.AccessToken, "updated")
		}

		tokens, err := svc.ExternalCredentials.ListCredentialsForConnection(ctx, principal.UserSubjectID(user.ID), "svc", "default")
		if err != nil {
			t.Fatalf("ListCredentialsForConnection: %v", err)
		}
		if len(tokens) != 1 {
			t.Fatalf("got %d tokens, want 1", len(tokens))
		}
	})

	t.Run("ConcurrentCredentialWrites", func(t *testing.T) {
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
				errs[idx] = svc.ExternalCredentials.PutCredential(ctx, &core.IntegrationToken{
					ID:           fmt.Sprintf("tok-%d", idx),
					SubjectID:    principal.UserSubjectID(user.ID),
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

		tokens, err := svc.ExternalCredentials.ListCredentials(ctx, principal.UserSubjectID(user.ID))
		if err != nil {
			t.Fatalf("ListCredentials: %v", err)
		}
		if len(tokens) != count {
			t.Fatalf("got %d tokens, want %d", len(tokens), count)
		}
	})

}

func TestAPITokenService(t *testing.T) {
	t.Parallel()

	t.Run("StoreAndValidate_round_trip", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)
		ctx := context.Background()

		user := mustCreateUser(t, svc, "alice@test.com")
		token := &core.APIToken{
			ID:                  "api-1",
			OwnerKind:           core.APITokenOwnerKindUser,
			OwnerID:             user.ID,
			CredentialSubjectID: principal.UserSubjectID(user.ID),
			Name:                "ci-token",
			HashedToken:         "sha256:abc123",
			Scopes:              "read:tokens",
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
		if got.OwnerKind != core.APITokenOwnerKindUser || got.OwnerID != user.ID {
			t.Errorf("owner = (%q, %q), want (%q, %q)", got.OwnerKind, got.OwnerID, core.APITokenOwnerKindUser, user.ID)
		}
		if got.CredentialSubjectID != principal.UserSubjectID(user.ID) {
			t.Errorf("CredentialSubjectID = %q, want %q", got.CredentialSubjectID, principal.UserSubjectID(user.ID))
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
			ID:                  "api-expired",
			OwnerKind:           core.APITokenOwnerKindUser,
			OwnerID:             user.ID,
			CredentialSubjectID: principal.UserSubjectID(user.ID),
			Name:                "expired",
			HashedToken:         "sha256:expired",
			ExpiresAt:           &past,
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
			ID:                  "api-valid",
			OwnerKind:           core.APITokenOwnerKindUser,
			OwnerID:             user.ID,
			CredentialSubjectID: principal.UserSubjectID(user.ID),
			Name:                "valid",
			HashedToken:         "sha256:valid",
			ExpiresAt:           &future,
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
			{ID: "api-a", OwnerKind: core.APITokenOwnerKindUser, OwnerID: user.ID, CredentialSubjectID: principal.UserSubjectID(user.ID), Name: "a", HashedToken: "sha256:aaa"},
			{ID: "api-b", OwnerKind: core.APITokenOwnerKindUser, OwnerID: user.ID, CredentialSubjectID: principal.UserSubjectID(user.ID), Name: "b", HashedToken: "sha256:bbb"},
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
			ID:                  "api-rev",
			OwnerKind:           core.APITokenOwnerKindUser,
			OwnerID:             user.ID,
			CredentialSubjectID: principal.UserSubjectID(user.ID),
			Name:                "revokable",
			HashedToken:         "sha256:revoke",
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
				ID:                  fmt.Sprintf("api-%d", i),
				OwnerKind:           core.APITokenOwnerKindUser,
				OwnerID:             user.ID,
				CredentialSubjectID: principal.UserSubjectID(user.ID),
				Name:                fmt.Sprintf("token-%d", i),
				HashedToken:         hash,
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

	t.Run("RevokeAllAPITokens_preserves_access_for_other_owners", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)
		ctx := context.Background()

		user := mustCreateUser(t, svc, "alice@test.com")
		owned := &core.APIToken{
			ID:                  "owned-token",
			OwnerKind:           core.APITokenOwnerKindUser,
			OwnerID:             user.ID,
			CredentialSubjectID: principal.UserSubjectID(user.ID),
			Name:                "owned",
			HashedToken:         "sha256:owned",
			Permissions:         []core.AccessPermission{{Plugin: "owned"}},
		}
		if err := svc.APITokens.StoreAPIToken(ctx, owned); err != nil {
			t.Fatalf("StoreAPIToken owned: %v", err)
		}

		otherUser := mustCreateUser(t, svc, "other@test.com")
		otherOwner := &core.APIToken{
			ID:                  "other-token",
			OwnerKind:           core.APITokenOwnerKindUser,
			OwnerID:             otherUser.ID,
			CredentialSubjectID: principal.UserSubjectID(otherUser.ID),
			Name:                "other",
			HashedToken:         "sha256:other",
			Permissions:         []core.AccessPermission{{Plugin: "other"}},
		}
		if err := svc.APITokens.StoreAPIToken(ctx, otherOwner); err != nil {
			t.Fatalf("StoreAPIToken other: %v", err)
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
		if len(tokens) != 0 {
			t.Fatalf("remaining user tokens = %+v, want none", tokens)
		}

		otherTokens, err := svc.APITokens.ListAPITokens(ctx, otherUser.ID)
		if err != nil {
			t.Fatalf("ListAPITokens(other): %v", err)
		}
		if len(otherTokens) != 1 || otherTokens[0].ID != otherOwner.ID {
			t.Fatalf("remaining other-owner tokens = %+v, want survivor", otherTokens)
		}

		access, err := svc.APITokenAccess.ListByToken(ctx, otherOwner.ID)
		if err != nil {
			t.Fatalf("ListByToken other-token: %v", err)
		}
		if len(access) != 1 || access[0].Plugin != "other" {
			t.Fatalf("other token access = %+v, want surviving access", access)
		}
	})

	t.Run("BackfillTokenAccess_uses_scopes_when_permissions_are_empty", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)
		ctx := context.Background()

		user := mustCreateUser(t, svc, "scoped@test.com")
		now := time.Now().UTC()
		if err := svc.DB.ObjectStore(coredata.StoreAPITokens).Add(ctx, indexeddb.Record{
			"id":                    "scoped-token",
			"owner_kind":            core.APITokenOwnerKindUser,
			"owner_id":              user.ID,
			"credential_subject_id": principal.UserSubjectID(user.ID),
			"name":                  "scoped",
			"hashed_token":          "sha256:scoped",
			"scopes":                "alpha beta alpha",
			"created_at":            now,
			"updated_at":            now,
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
			OwnerKind:           core.APITokenOwnerKindUser,
			OwnerID:             user.ID,
			CredentialSubjectID: principal.UserSubjectID(user.ID),
			Name:                "auto-id",
			HashedToken:         "sha256:auto",
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
