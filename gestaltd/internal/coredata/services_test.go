package coredata_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

func newTestServices(t *testing.T) *coredata.Services {
	t.Helper()
	return testutil.NewStubServices(t)
}

func newTestServicesWithDB(t *testing.T) (*coredata.Services, *coretesting.StubIndexedDB) {
	t.Helper()
	db := &coretesting.StubIndexedDB{}
	svc, err := coredata.New(db)
	if err != nil {
		t.Fatalf("coredata.New: %v", err)
	}
	testutil.AttachStubExternalCredentials(svc)
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

func seedUserRecord(t *testing.T, svc *coredata.Services, id, email string, createdAt time.Time) *core.User {
	t.Helper()
	ctx := context.Background()
	rec := idb.Record{
		"id":               id,
		"email":            email,
		"normalized_email": strings.ToLower(strings.TrimSpace(email)),
		"display_name":     "",
		"created_at":       createdAt,
		"updated_at":       createdAt,
	}
	if err := svc.DB.ObjectStore(coredata.StoreUsers).Add(ctx, rec); err != nil {
		t.Fatalf("seedUserRecord: %v", err)
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

func (d *countingIndexedDB) ObjectStore(name string) idb.ObjectStore {
	return &countingObjectStore{name: name, db: d, inner: d.inner.ObjectStore(name)}
}

func (d *countingIndexedDB) Transaction(ctx context.Context, stores []string, mode idb.TransactionMode, opts idb.TransactionOptions) (idb.Transaction, error) {
	return d.inner.Transaction(ctx, stores, mode, opts)
}

func (d *countingIndexedDB) CreateObjectStore(ctx context.Context, name string, schema idb.ObjectStoreOptions) (idb.ObjectStore, error) {
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

type createContextRecordingIndexedDB struct {
	inner          indexeddb.IndexedDB
	mu             sync.Mutex
	createContexts map[string]context.Context
}

func newCreateContextRecordingIndexedDB(inner indexeddb.IndexedDB) *createContextRecordingIndexedDB {
	return &createContextRecordingIndexedDB{
		inner:          inner,
		createContexts: make(map[string]context.Context),
	}
}

func (d *createContextRecordingIndexedDB) ObjectStore(name string) idb.ObjectStore {
	return d.inner.ObjectStore(name)
}

func (d *createContextRecordingIndexedDB) Transaction(ctx context.Context, stores []string, mode idb.TransactionMode, opts idb.TransactionOptions) (idb.Transaction, error) {
	return d.inner.Transaction(ctx, stores, mode, opts)
}

func (d *createContextRecordingIndexedDB) CreateObjectStore(ctx context.Context, name string, schema idb.ObjectStoreOptions) (idb.ObjectStore, error) {
	d.mu.Lock()
	d.createContexts[name] = ctx
	d.mu.Unlock()
	return d.inner.CreateObjectStore(ctx, name, schema)
}

func (d *createContextRecordingIndexedDB) DeleteObjectStore(ctx context.Context, name string) error {
	return d.inner.DeleteObjectStore(ctx, name)
}

func (d *createContextRecordingIndexedDB) Ping(ctx context.Context) error { return d.inner.Ping(ctx) }
func (d *createContextRecordingIndexedDB) Close() error                   { return d.inner.Close() }

func (d *createContextRecordingIndexedDB) createdStoreContexts() map[string]context.Context {
	d.mu.Lock()
	defer d.mu.Unlock()
	contexts := make(map[string]context.Context, len(d.createContexts))
	for name, ctx := range d.createContexts {
		contexts[name] = ctx
	}
	return contexts
}

type countingObjectStore struct {
	name  string
	db    *countingIndexedDB
	inner idb.ObjectStore
}

func (o *countingObjectStore) Get(ctx context.Context, id string) (idb.Record, error) {
	return o.inner.Get(ctx, id)
}

func (o *countingObjectStore) GetKey(ctx context.Context, id string) (string, error) {
	return o.inner.GetKey(ctx, id)
}

func (o *countingObjectStore) Add(ctx context.Context, record idb.Record) error {
	return o.inner.Add(ctx, record)
}

func (o *countingObjectStore) Put(ctx context.Context, record idb.Record) error {
	return o.inner.Put(ctx, record)
}

func (o *countingObjectStore) Delete(ctx context.Context, id string) error {
	return o.inner.Delete(ctx, id)
}

func (o *countingObjectStore) Clear(ctx context.Context) error {
	return o.inner.Clear(ctx)
}

func (o *countingObjectStore) GetAll(ctx context.Context, query any, count ...uint32) ([]idb.Record, error) {
	o.db.recordGetAll(o.name)
	return o.inner.GetAll(ctx, query, count...)
}

func (o *countingObjectStore) GetAllKeys(ctx context.Context, query any, count ...uint32) ([]string, error) {
	return o.inner.GetAllKeys(ctx, query, count...)
}

func (o *countingObjectStore) Count(ctx context.Context, query any) (int64, error) {
	return o.inner.Count(ctx, query)
}

func (o *countingObjectStore) DeleteRange(ctx context.Context, query any) (int64, error) {
	return o.inner.DeleteRange(ctx, query)
}

func (o *countingObjectStore) Index(name string) idb.Index {
	return o.inner.Index(name)
}

func (o *countingObjectStore) OpenCursor(ctx context.Context, query any, dir idb.CursorDirection) (idb.Cursor, error) {
	return o.inner.OpenCursor(ctx, query, dir)
}

func (o *countingObjectStore) OpenKeyCursor(ctx context.Context, query any, dir idb.CursorDirection) (idb.Cursor, error) {
	return o.inner.OpenKeyCursor(ctx, query, dir)
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

	t.Run("new_with_context_uses_caller_context_for_schema_creation", func(t *testing.T) {
		t.Parallel()

		type schemaContextKey struct{}
		const marker = "schema-migration"
		ctx := context.WithValue(context.Background(), schemaContextKey{}, marker)
		db := newCreateContextRecordingIndexedDB(&coretesting.StubIndexedDB{})
		if _, err := coredata.NewWithContext(ctx, db); err != nil {
			t.Fatalf("coredata.NewWithContext: %v", err)
		}

		contexts := db.createdStoreContexts()
		if len(contexts) == 0 {
			t.Fatal("NewWithContext did not create any object stores")
		}
		for _, store := range []string{
			coredata.StoreUsers,
			coredata.StoreAppVersionCatalog,
		} {
			if _, ok := contexts[store]; !ok {
				t.Fatalf("NewWithContext did not create store %q", store)
			}
		}
		for store, createCtx := range contexts {
			if got := createCtx.Value(schemaContextKey{}); got != marker {
				t.Fatalf("CreateObjectStore(%q) context marker = %v, want %q", store, got, marker)
			}
		}
	})

	t.Run("skip_schema_bootstrap", func(t *testing.T) {
		t.Parallel()

		db := newCreateContextRecordingIndexedDB(&coretesting.StubIndexedDB{})
		if _, err := coredata.NewWithOptions(context.Background(), db, coredata.NewOptions{SkipSchemaBootstrap: true}); err != nil {
			t.Fatalf("coredata.NewWithOptions: %v", err)
		}
		if len(db.createdStoreContexts()) != 0 {
			t.Fatalf("CreateObjectStore calls = %d, want 0", len(db.createdStoreContexts()))
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

	t.Run("FindOrCreateUserWithName_stores_name", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)

		user, err := svc.Users.FindOrCreateUserWithName(context.Background(), "dana@test.com", "Dana Example")
		if err != nil {
			t.Fatalf("FindOrCreateUserWithName: %v", err)
		}
		if user.DisplayName != "Dana Example" {
			t.Fatalf("DisplayName = %q, want Dana Example", user.DisplayName)
		}
	})

	t.Run("FindOrCreateUserWithName_updates_changed_name", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)
		ctx := context.Background()

		user, err := svc.Users.FindOrCreateUserWithName(ctx, "dana@test.com", "Dana Example")
		if err != nil {
			t.Fatalf("first create: %v", err)
		}
		updated, err := svc.Users.FindOrCreateUserWithName(ctx, "dana@test.com", "Dana Updated")
		if err != nil {
			t.Fatalf("update name: %v", err)
		}
		if updated.ID != user.ID {
			t.Fatalf("user id changed from %q to %q", user.ID, updated.ID)
		}
		if updated.DisplayName != "Dana Updated" {
			t.Fatalf("DisplayName = %q, want Dana Updated", updated.DisplayName)
		}
	})

	t.Run("FindOrCreateUserWithName_does_not_clear_existing_name", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)
		ctx := context.Background()

		_, err := svc.Users.FindOrCreateUserWithName(ctx, "dana@test.com", "Dana Example")
		if err != nil {
			t.Fatalf("first create: %v", err)
		}
		updated, err := svc.Users.FindOrCreateUserWithName(ctx, "dana@test.com", "")
		if err != nil {
			t.Fatalf("empty name update: %v", err)
		}
		if updated.DisplayName != "Dana Example" {
			t.Fatalf("DisplayName = %q, want Dana Example", updated.DisplayName)
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

	t.Run("FindOrCreateUser_rejects_duplicate_normalized_email_rows", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)
		ctx := context.Background()

		seedUserRecord(t, svc, "user-a", "user@example.com", time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
		seedUserRecord(t, svc, "user-b", "USER@example.com", time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC))

		_, err := svc.Users.FindOrCreateUser(ctx, "USER@example.com")
		if err == nil {
			t.Fatal("expected duplicate normalized email error, got nil")
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
		token := &core.ExternalCredential{
			ID:           "tok-1",
			Subject:      principal.UserSubjectID(user.ID),
			Audience:     "test-svc:default",
			Qualifier:    "inst-1",
			Grant:        &core.ExternalCredentialGrant{AccessToken: "access-secret", RefreshToken: "refresh-secret", Scope: "read,write", ExpiresAt: &expires},
			MetadataJSON: `{"key":"val"}`,
		}
		if err := svc.ExternalCredentials.UpsertCredential(ctx, token); err != nil {
			t.Fatalf("UpsertCredential: %v", err)
		}

		got, err := svc.ExternalCredentials.GetCredential(ctx, principal.UserSubjectID(user.ID), "test-svc:default", "inst-1")
		if err != nil {
			t.Fatalf("Token: %v", err)
		}
		if got.ID != "tok-1" {
			t.Errorf("ID = %q, want %q", got.ID, "tok-1")
		}
		if got.Grant == nil {
			t.Fatal("Grant = nil, want stored grant")
		}
		if got.Grant.AccessToken != "access-secret" {
			t.Errorf("AccessToken = %q, want %q", got.Grant.AccessToken, "access-secret")
		}
		if got.Grant.RefreshToken != "refresh-secret" {
			t.Errorf("RefreshToken = %q, want %q", got.Grant.RefreshToken, "refresh-secret")
		}
		if got.Grant.Scope != "read,write" {
			t.Errorf("Scope = %q, want %q", got.Grant.Scope, "read,write")
		}
		if got.MetadataJSON != `{"key":"val"}` {
			t.Errorf("MetadataJSON = %q, want %q", got.MetadataJSON, `{"key":"val"}`)
		}
	})

	t.Run("GetCredential_not_found", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)

		_, err := svc.ExternalCredentials.GetCredential(context.Background(), "no-user", "no-svc:no-conn", "no-inst")
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

		for _, tok := range []*core.ExternalCredential{
			{
				ID:        "tok-a1",
				Subject:   principal.UserSubjectID(userA.ID),
				Audience:  "svc-a:default",
				Qualifier: "i1",
				Grant:     &core.ExternalCredentialGrant{AccessToken: "a1", RefreshToken: "r1"},
			},
			{
				ID:        "tok-a2",
				Subject:   principal.UserSubjectID(userA.ID),
				Audience:  "svc-b:default",
				Qualifier: "i2",
				Grant:     &core.ExternalCredentialGrant{AccessToken: "a2", RefreshToken: "r2"},
			},
			{
				ID:        "tok-b1",
				Subject:   principal.UserSubjectID(userB.ID),
				Audience:  "svc-a:default",
				Qualifier: "i1",
				Grant:     &core.ExternalCredentialGrant{AccessToken: "a3", RefreshToken: "r3"},
			},
		} {
			if err := svc.ExternalCredentials.UpsertCredential(ctx, tok); err != nil {
				t.Fatalf("UpsertCredential(%s): %v", tok.ID, err)
			}
		}

		tokens, err := svc.ExternalCredentials.ListCredentials(ctx, principal.UserSubjectID(userA.ID), "")
		if err != nil {
			t.Fatalf("ListCredentials: %v", err)
		}
		if len(tokens) != 2 {
			t.Fatalf("ListCredentials: got %d, want 2", len(tokens))
		}
	})

	t.Run("ListCredentials_by_audience", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)
		ctx := context.Background()

		user := mustCreateUser(t, svc, "alice@test.com")
		for _, tok := range []*core.ExternalCredential{
			{
				ID:        "tok-1",
				Subject:   principal.UserSubjectID(user.ID),
				Audience:  "svc:conn-a",
				Qualifier: "i1",
				Grant:     &core.ExternalCredentialGrant{AccessToken: "a", RefreshToken: "r"},
			},
			{
				ID:        "tok-2",
				Subject:   principal.UserSubjectID(user.ID),
				Audience:  "svc:conn-a",
				Qualifier: "i2",
				Grant:     &core.ExternalCredentialGrant{AccessToken: "b", RefreshToken: "s"},
			},
			{
				ID:        "tok-3",
				Subject:   principal.UserSubjectID(user.ID),
				Audience:  "svc:conn-b",
				Qualifier: "i1",
				Grant:     &core.ExternalCredentialGrant{AccessToken: "c", RefreshToken: "u"},
			},
		} {
			if err := svc.ExternalCredentials.UpsertCredential(ctx, tok); err != nil {
				t.Fatalf("UpsertCredential(%s): %v", tok.ID, err)
			}
		}

		tokens, err := svc.ExternalCredentials.ListCredentials(ctx, principal.UserSubjectID(user.ID), "svc:conn-a")
		if err != nil {
			t.Fatalf("ListCredentials: %v", err)
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
		tok := &core.ExternalCredential{
			ID:        "tok-del",
			Subject:   principal.UserSubjectID(user.ID),
			Audience:  "svc:default",
			Qualifier: "i1",
			Grant:     &core.ExternalCredentialGrant{AccessToken: "a", RefreshToken: "r"},
		}
		if err := svc.ExternalCredentials.UpsertCredential(ctx, tok); err != nil {
			t.Fatalf("UpsertCredential: %v", err)
		}

		if err := svc.ExternalCredentials.DeleteCredential(ctx, "tok-del"); err != nil {
			t.Fatalf("DeleteCredential: %v", err)
		}

		_, err := svc.ExternalCredentials.GetCredential(ctx, principal.UserSubjectID(user.ID), "svc:default", "i1")
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

	t.Run("UpsertCredential_replaces_existing", func(t *testing.T) {
		t.Parallel()
		svc := newTestServices(t)
		ctx := context.Background()

		user := mustCreateUser(t, svc, "alice@test.com")
		tok := &core.ExternalCredential{
			ID:        "tok-upsert",
			Subject:   principal.UserSubjectID(user.ID),
			Audience:  "svc:default",
			Qualifier: "i1",
			Grant:     &core.ExternalCredentialGrant{AccessToken: "original", RefreshToken: "r"},
		}
		if err := svc.ExternalCredentials.UpsertCredential(ctx, tok); err != nil {
			t.Fatalf("first UpsertCredential: %v", err)
		}

		tok.ID = "tok-upsert-replacement"
		tok.Grant.AccessToken = "updated"
		if err := svc.ExternalCredentials.UpsertCredential(ctx, tok); err != nil {
			t.Fatalf("second UpsertCredential: %v", err)
		}

		got, err := svc.ExternalCredentials.GetCredential(ctx, principal.UserSubjectID(user.ID), "svc:default", "i1")
		if err != nil {
			t.Fatalf("Token: %v", err)
		}
		if got.ID != "tok-upsert" {
			t.Errorf("ID = %q, want %q", got.ID, "tok-upsert")
		}
		if got.Grant == nil || got.Grant.AccessToken != "updated" {
			t.Errorf("Grant = %+v, want access token %q", got.Grant, "updated")
		}

		tokens, err := svc.ExternalCredentials.ListCredentials(ctx, principal.UserSubjectID(user.ID), "svc:default")
		if err != nil {
			t.Fatalf("ListCredentials: %v", err)
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
				errs[idx] = svc.ExternalCredentials.UpsertCredential(ctx, &core.ExternalCredential{
					ID:        fmt.Sprintf("tok-%d", idx),
					Subject:   principal.UserSubjectID(user.ID),
					Audience:  "svc:default",
					Qualifier: fmt.Sprintf("inst-%d", idx),
					Grant:     &core.ExternalCredentialGrant{AccessToken: "access", RefreshToken: "refresh"},
				})
			}(i)
		}
		wg.Wait()

		for i, err := range errs {
			if err != nil {
				t.Errorf("goroutine %d: %v", i, err)
			}
		}

		tokens, err := svc.ExternalCredentials.ListCredentials(ctx, principal.UserSubjectID(user.ID), "")
		if err != nil {
			t.Fatalf("ListCredentials: %v", err)
		}
		if len(tokens) != count {
			t.Fatalf("got %d tokens, want %d", len(tokens), count)
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
