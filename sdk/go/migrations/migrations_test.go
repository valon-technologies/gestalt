package migrations_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	"github.com/valon-technologies/gestalt/sdk/go/migrations"
)

type fakeDB struct {
	stores      map[string]map[string]indexeddb.Record
	indexes     map[string]struct{}
	calls       []string
	failStore   string
	failStoreErr error
}

func newFakeDB() *fakeDB {
	return &fakeDB{
		stores:  make(map[string]map[string]indexeddb.Record),
		indexes: make(map[string]struct{}),
	}
}

func (f *fakeDB) CreateObjectStore(_ context.Context, name string, schema indexeddb.ObjectStoreOptions) (indexeddb.ObjectStore, error) {
	f.calls = append(f.calls, "create:"+name)
	if name == f.failStore && f.failStoreErr != nil {
		return nil, f.failStoreErr
	}
	if _, ok := f.stores[name]; ok {
		return nil, indexeddb.ErrAlreadyExists
	}
	f.stores[name] = make(map[string]indexeddb.Record)
	for _, index := range schema.Indexes {
		f.indexes[fmt.Sprintf("%s/%s", name, index.Name)] = struct{}{}
	}
	return f.store(name), nil
}

func (f *fakeDB) DeleteObjectStore(_ context.Context, name string) error {
	if _, ok := f.stores[name]; !ok {
		return indexeddb.ErrNotFound
	}
	delete(f.stores, name)
	return nil
}

func (f *fakeDB) CreateIndex(_ context.Context, store string, index indexeddb.IndexDefinition) error {
	key := fmt.Sprintf("%s/%s", store, index.Name)
	if _, ok := f.indexes[key]; ok {
		return indexeddb.ErrAlreadyExists
	}
	f.indexes[key] = struct{}{}
	return nil
}

func (f *fakeDB) DeleteIndex(_ context.Context, store, name string) error {
	key := fmt.Sprintf("%s/%s", store, name)
	if _, ok := f.indexes[key]; !ok {
		return indexeddb.ErrNotFound
	}
	delete(f.indexes, key)
	return nil
}

func (f *fakeDB) Transaction(context.Context, []string, indexeddb.TransactionMode, indexeddb.TransactionOptions) (indexeddb.Transaction, error) {
	return nil, indexeddb.ErrUnsupported
}

func (f *fakeDB) ObjectStore(name string) indexeddb.ObjectStore { return f.store(name) }
func (f *fakeDB) Close() error                                   { return nil }

func (f *fakeDB) store(name string) *fakeStore {
	if _, ok := f.stores[name]; !ok {
		f.stores[name] = make(map[string]indexeddb.Record)
	}
	return &fakeStore{db: f, name: name}
}

type fakeStore struct {
	db   *fakeDB
	name string
}

func (s *fakeStore) Get(_ context.Context, id string) (indexeddb.Record, error) {
	row, ok := s.db.stores[s.name][id]
	if !ok {
		return nil, indexeddb.ErrNotFound
	}
	return row, nil
}

func (s *fakeStore) GetKey(_ context.Context, id string) (string, error) {
	if _, ok := s.db.stores[s.name][id]; !ok {
		return "", indexeddb.ErrNotFound
	}
	return id, nil
}

func (s *fakeStore) Put(_ context.Context, record indexeddb.Record) error {
	id := recordID(record)
	s.db.stores[s.name][id] = record
	return nil
}

func (s *fakeStore) Add(_ context.Context, record indexeddb.Record) error {
	id := recordID(record)
	if _, ok := s.db.stores[s.name][id]; ok {
		return indexeddb.ErrAlreadyExists
	}
	s.db.stores[s.name][id] = record
	return nil
}

func (s *fakeStore) Delete(_ context.Context, id string) error {
	delete(s.db.stores[s.name], id)
	return nil
}

func (s *fakeStore) Clear(_ context.Context) error {
	s.db.stores[s.name] = make(map[string]indexeddb.Record)
	return nil
}

func (s *fakeStore) GetAll(context.Context, any, ...uint32) ([]indexeddb.Record, error) {
	return nil, indexeddb.ErrUnsupported
}

func (s *fakeStore) GetAllKeys(_ context.Context, _ any, _ ...uint32) ([]string, error) {
	keys := make([]string, 0, len(s.db.stores[s.name]))
	for id := range s.db.stores[s.name] {
		keys = append(keys, id)
	}
	return keys, nil
}

func (s *fakeStore) Count(context.Context, any) (int64, error) { return 0, indexeddb.ErrUnsupported }
func (s *fakeStore) DeleteRange(context.Context, any) (int64, error) {
	return 0, indexeddb.ErrUnsupported
}
func (s *fakeStore) Index(string) indexeddb.Index                          { return fakeIndex{s} }
func (s *fakeStore) OpenCursor(context.Context, any, indexeddb.CursorDirection) (indexeddb.Cursor, error) {
	return nil, indexeddb.ErrUnsupported
}
func (s *fakeStore) OpenKeyCursor(context.Context, any, indexeddb.CursorDirection) (indexeddb.Cursor, error) {
	return nil, indexeddb.ErrUnsupported
}

type fakeIndex struct{ store *fakeStore }

func (fakeIndex) Get(context.Context, any) (indexeddb.Record, error) {
	return nil, indexeddb.ErrUnsupported
}
func (fakeIndex) GetKey(context.Context, any) (string, error) { return "", indexeddb.ErrUnsupported }
func (fakeIndex) GetAll(context.Context, any, ...uint32) ([]indexeddb.Record, error) {
	return nil, indexeddb.ErrUnsupported
}
func (fakeIndex) GetAllKeys(context.Context, any, ...uint32) ([]string, error) {
	return nil, indexeddb.ErrUnsupported
}
func (fakeIndex) Count(context.Context, any) (int64, error) { return 0, indexeddb.ErrUnsupported }
func (fakeIndex) Delete(context.Context, any) (int64, error) { return 0, indexeddb.ErrUnsupported }
func (fakeIndex) OpenCursor(context.Context, any, indexeddb.CursorDirection) (indexeddb.Cursor, error) {
	return nil, indexeddb.ErrUnsupported
}
func (fakeIndex) OpenKeyCursor(context.Context, any, indexeddb.CursorDirection) (indexeddb.Cursor, error) {
	return nil, indexeddb.ErrUnsupported
}

func recordID(record indexeddb.Record) string {
	if id, ok := record["id"].(string); ok && id != "" {
		return id
	}
	if id, ok := record["revision_id"].(string); ok {
		return id
	}
	return ""
}

func initRevision(id, store string) migrations.Revision {
	return migrations.Revision{
		ID: id,
		Schema: &migrations.SchemaDeclaration{
			Stores: []migrations.StoreDeclaration{{Name: store}},
		},
	}
}

func ledgerKeys(db *fakeDB) []string {
	keys, _ := db.store("_gestalt_migrations").GetAllKeys(context.Background(), nil)
	return keys
}

func TestRun_FreshInstallAndRestart(t *testing.T) {
	ctx := context.Background()
	db := newFakeDB()
	revisions := []migrations.Revision{initRevision("0001_init", "widgets")}

	result, err := migrations.Run(ctx, db, migrations.RunOptions{Revisions: revisions})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(result.Applied) != 1 || result.Head != "0001_init" {
		t.Fatalf("result = %#v", result)
	}
	if _, ok := db.stores["widgets"]; !ok {
		t.Fatal("widgets store was not created")
	}

	callsBefore := len(filterCreateCalls(db.calls))
	result, err = migrations.Run(ctx, db, migrations.RunOptions{Revisions: revisions})
	if err != nil {
		t.Fatalf("Run(restart) error = %v", err)
	}
	if len(result.Applied) != 0 {
		t.Fatalf("restart applied = %#v, want none", result.Applied)
	}
	if len(filterCreateCalls(db.calls)) != callsBefore {
		t.Fatalf("restart calls = %v, want no new data-store creates", db.calls[callsBefore:])
	}
}

func filterCreateCalls(calls []string) []string {
	filtered := make([]string, 0, len(calls))
	for _, call := range calls {
		if call == "create:_gestalt_migrations" {
			continue
		}
		filtered = append(filtered, call)
	}
	return filtered
}

func TestRun_FailingRevisionNotRecorded(t *testing.T) {
	ctx := context.Background()
	db := newFakeDB()
	db.failStore = "widgets"
	db.failStoreErr = errors.New("boom")
	revisions := []migrations.Revision{initRevision("0001_init", "widgets")}

	_, err := migrations.Run(ctx, db, migrations.RunOptions{Revisions: revisions})
	if err == nil {
		t.Fatal("Run() error = nil, want failure")
	}
	if keys := ledgerKeys(db); len(keys) != 0 {
		t.Fatalf("ledger = %v, want empty", keys)
	}
}

func TestRun_LedgerAheadOfCode(t *testing.T) {
	ctx := context.Background()
	db := newFakeDB()
	revisions := []migrations.Revision{initRevision("0001_init", "widgets")}
	if _, err := migrations.Run(ctx, db, migrations.RunOptions{Revisions: revisions}); err != nil {
		t.Fatalf("seed Run() error = %v", err)
	}
	_ = db.store("_gestalt_migrations").Put(ctx, indexeddb.Record{
		"revision_id": "0002_future",
		"id":          "0002_future",
		"applied_at":  "2026-01-01T00:00:00Z",
	})

	_, err := migrations.Run(ctx, db, migrations.RunOptions{Revisions: revisions})
	if err == nil {
		t.Fatal("Run() error = nil, want ledger ahead failure")
	}
	if !strings.Contains(err.Error(), "ledger is ahead of code") {
		t.Fatalf("error = %v", err)
	}
}

func TestRun_IgnoresOtherProviderLedgerRowsOnSharedDB(t *testing.T) {
	ctx := context.Background()
	db := newFakeDB()
	otherRevision := initRevision("authorization/indexeddb/0001_init", "relationships")
	if _, err := migrations.Run(ctx, db, migrations.RunOptions{Revisions: []migrations.Revision{otherRevision}}); err != nil {
		t.Fatalf("seed other provider Run() error = %v", err)
	}

	revisions := []migrations.Revision{initRevision("auth/oidc/0001_init", "grants")}
	if _, err := migrations.Run(ctx, db, migrations.RunOptions{Revisions: revisions}); err != nil {
		t.Fatalf("Run() error = %v, want success on shared ledger", err)
	}
}

func TestRun_IgnoresDeeperNamespaceLedgerRowsOnSharedDB(t *testing.T) {
	ctx := context.Background()
	db := newFakeDB()
	otherRevision := initRevision("auth/oidc/nested/0001_init", "nested")
	if _, err := migrations.Run(ctx, db, migrations.RunOptions{Revisions: []migrations.Revision{otherRevision}}); err != nil {
		t.Fatalf("seed deeper namespace Run() error = %v", err)
	}

	revisions := []migrations.Revision{initRevision("auth/oidc/0001_init", "grants")}
	if _, err := migrations.Run(ctx, db, migrations.RunOptions{Revisions: revisions}); err != nil {
		t.Fatalf("Run() error = %v, want success when ancestor prefix differs", err)
	}
}

func TestRun_DuplicateRevisionIDs(t *testing.T) {
	ctx := context.Background()
	db := newFakeDB()
	revisions := []migrations.Revision{
		initRevision("0001_init", "widgets"),
		initRevision("0001_init", "gadgets"),
	}
	_, err := migrations.Run(ctx, db, migrations.RunOptions{Revisions: revisions})
	if err == nil {
		t.Fatal("Run() error = nil, want duplicate id failure")
	}
}

func TestRun_RejectsNeitherSchemaNorBackfill(t *testing.T) {
	ctx := context.Background()
	db := newFakeDB()
	revisions := []migrations.Revision{{ID: "0001_init"}}
	_, err := migrations.Run(ctx, db, migrations.RunOptions{Revisions: revisions})
	if err == nil {
		t.Fatal("Run() error = nil, want validation failure")
	}
}
