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

type fakeStoreState struct {
	pk   string
	rows map[string]indexeddb.Record
}

type fakeIndexedDB struct {
	stores                 map[string]*fakeStoreState
	indexes                map[string]struct{}
	calls                  []string
	createObjectStoreError error
}

func newFakeIndexedDB() *fakeIndexedDB {
	return &fakeIndexedDB{
		stores:  make(map[string]*fakeStoreState),
		indexes: make(map[string]struct{}),
	}
}

func (f *fakeIndexedDB) CreateObjectStore(_ context.Context, name string, schema indexeddb.ObjectStoreOptions) (indexeddb.ObjectStore, error) {
	f.calls = append(f.calls, "createObjectStore:"+name)
	if f.createObjectStoreError != nil {
		return nil, f.createObjectStoreError
	}
	if _, ok := f.stores[name]; ok {
		return nil, indexeddb.ErrAlreadyExists
	}
	pk := "id"
	for _, column := range schema.Columns {
		if column.PrimaryKey {
			pk = column.Name
			break
		}
	}
	f.stores[name] = &fakeStoreState{pk: pk, rows: make(map[string]indexeddb.Record)}
	for _, index := range schema.Indexes {
		f.indexes[fmt.Sprintf("%s/%s", name, index.Name)] = struct{}{}
	}
	return f.objectStore(name), nil
}

func (f *fakeIndexedDB) DeleteObjectStore(_ context.Context, name string) error {
	f.calls = append(f.calls, "deleteObjectStore:"+name)
	if _, ok := f.stores[name]; !ok {
		return indexeddb.ErrNotFound
	}
	delete(f.stores, name)
	return nil
}

func (f *fakeIndexedDB) CreateIndex(_ context.Context, store string, index indexeddb.IndexDefinition) error {
	f.calls = append(f.calls, "createIndex:"+store+"/"+index.Name)
	key := fmt.Sprintf("%s/%s", store, index.Name)
	if _, ok := f.indexes[key]; ok {
		return indexeddb.ErrAlreadyExists
	}
	f.indexes[key] = struct{}{}
	return nil
}

func (f *fakeIndexedDB) DeleteIndex(_ context.Context, store, name string) error {
	f.calls = append(f.calls, "deleteIndex:"+store+"/"+name)
	key := fmt.Sprintf("%s/%s", store, name)
	if _, ok := f.indexes[key]; !ok {
		return indexeddb.ErrNotFound
	}
	delete(f.indexes, key)
	return nil
}

func (f *fakeIndexedDB) Transaction(context.Context, []string, indexeddb.TransactionMode, indexeddb.TransactionOptions) (indexeddb.Transaction, error) {
	return nil, indexeddb.ErrUnsupported
}

func (f *fakeIndexedDB) ObjectStore(name string) indexeddb.ObjectStore {
	return f.objectStore(name)
}

func (f *fakeIndexedDB) Close() error { return nil }

func (f *fakeIndexedDB) objectStore(name string) *fakeObjectStore {
	return &fakeObjectStore{db: f, name: name}
}

type fakeObjectStore struct {
	db   *fakeIndexedDB
	name string
}

func (s *fakeObjectStore) state() (*fakeStoreState, error) {
	found, ok := s.db.stores[s.name]
	if !ok {
		return nil, fmt.Errorf("store %s missing: %w", s.name, indexeddb.ErrNotFound)
	}
	return found, nil
}

func (s *fakeObjectStore) Add(ctx context.Context, record indexeddb.Record) error {
	return s.Put(ctx, record)
}

func (s *fakeObjectStore) Put(_ context.Context, record indexeddb.Record) error {
	state, err := s.state()
	if err != nil {
		return err
	}
	pkVal, ok := record[state.pk]
	if !ok {
		return fmt.Errorf("record missing primary key %q", state.pk)
	}
	state.rows[fmt.Sprint(pkVal)] = record
	return nil
}

func (s *fakeObjectStore) Get(_ context.Context, id string) (indexeddb.Record, error) {
	state, err := s.state()
	if err != nil {
		return nil, err
	}
	row, ok := state.rows[id]
	if !ok {
		return nil, indexeddb.ErrNotFound
	}
	return row, nil
}

func (s *fakeObjectStore) GetKey(_ context.Context, id string) (string, error) {
	if _, err := s.Get(context.Background(), id); err != nil {
		return "", err
	}
	return id, nil
}

func (s *fakeObjectStore) Delete(_ context.Context, id string) error {
	state, err := s.state()
	if err != nil {
		return err
	}
	delete(state.rows, id)
	return nil
}

func (s *fakeObjectStore) Clear(_ context.Context) error {
	state, err := s.state()
	if err != nil {
		return err
	}
	state.rows = make(map[string]indexeddb.Record)
	return nil
}

func (s *fakeObjectStore) GetAll(_ context.Context, _ any, _ ...uint32) ([]indexeddb.Record, error) {
	state, err := s.state()
	if err != nil {
		return nil, err
	}
	out := make([]indexeddb.Record, 0, len(state.rows))
	for _, row := range state.rows {
		out = append(out, row)
	}
	return out, nil
}

func (s *fakeObjectStore) GetAllKeys(_ context.Context, _ any, _ ...uint32) ([]string, error) {
	state, err := s.state()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(state.rows))
	for key := range state.rows {
		out = append(out, key)
	}
	return out, nil
}

func (s *fakeObjectStore) Count(ctx context.Context, _ any) (int64, error) {
	keys, err := s.GetAllKeys(ctx, nil)
	if err != nil {
		return 0, err
	}
	return int64(len(keys)), nil
}

func (s *fakeObjectStore) DeleteRange(context.Context, any) (int64, error) {
	return 0, indexeddb.ErrUnsupported
}

func (s *fakeObjectStore) Index(string) indexeddb.Index { return nil }

func (s *fakeObjectStore) OpenCursor(_ context.Context, _ any, _ indexeddb.CursorDirection) (indexeddb.Cursor, error) {
	state, err := s.state()
	if err != nil {
		return nil, err
	}
	entries := make([]struct {
		key string
		val indexeddb.Record
	}, 0, len(state.rows))
	for key, val := range state.rows {
		entries = append(entries, struct {
			key string
			val indexeddb.Record
		}{key: key, val: val})
	}
	return newFakeCursor(entries), nil
}

func (s *fakeObjectStore) OpenKeyCursor(context.Context, any, indexeddb.CursorDirection) (indexeddb.Cursor, error) {
	return nil, indexeddb.ErrUnsupported
}

type fakeCursor struct {
	entries []struct {
		key string
		val indexeddb.Record
	}
	i    int
	done bool
	err  error
}

func newFakeCursor(entries []struct {
	key string
	val indexeddb.Record
}) *fakeCursor {
	return &fakeCursor{entries: entries, i: -1}
}

func (c *fakeCursor) Continue() bool {
	c.i++
	if c.i >= len(c.entries) {
		c.done = true
		return false
	}
	return true
}

func (c *fakeCursor) ContinueToKey(any) bool { return false }

func (c *fakeCursor) Advance(int) bool { return false }

func (c *fakeCursor) Key() any {
	if c.i < 0 || c.i >= len(c.entries) {
		return nil
	}
	return c.entries[c.i].key
}

func (c *fakeCursor) PrimaryKey() string {
	if c.i < 0 || c.i >= len(c.entries) {
		return ""
	}
	return c.entries[c.i].key
}

func (c *fakeCursor) Value() (indexeddb.Record, error) {
	if c.done || c.i < 0 || c.i >= len(c.entries) {
		return nil, indexeddb.ErrNotFound
	}
	return c.entries[c.i].val, nil
}

func (c *fakeCursor) Delete() error { return indexeddb.ErrUnsupported }

func (c *fakeCursor) Update(indexeddb.Record) error { return indexeddb.ErrUnsupported }

func (c *fakeCursor) Err() error { return c.err }

func (c *fakeCursor) Close() error { return nil }

var issuesRevision = migrations.Revision{
	ID: "0001_issues",
	Schema: &migrations.SchemaDeclaration{
		Stores: []migrations.StoreDeclaration{{
			Name: "issues",
			Columns: []indexeddb.ColumnDef{
				{Name: "id", PrimaryKey: true, NotNull: true},
				{Name: "payload", NotNull: true},
			},
		}},
	},
}

func ledgerIDs(fake *fakeIndexedDB) ([]string, error) {
	state, ok := fake.stores["_gestalt_migrations"]
	if !ok {
		return nil, nil
	}
	out := make([]string, 0, len(state.rows))
	for key := range state.rows {
		out = append(out, key)
	}
	return out, nil
}

func countCalls(fake *fakeIndexedDB, prefix string) int {
	n := 0
	for _, call := range fake.calls {
		if strings.HasPrefix(call, prefix) {
			n++
		}
	}
	return n
}

func TestRunMigrations_FreshInstall(t *testing.T) {
	ctx := context.Background()
	fake := newFakeIndexedDB()
	if _, err := migrations.Run(ctx, fake, migrations.RunOptions{Revisions: []migrations.Revision{issuesRevision}}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, ok := fake.stores["issues"]; !ok {
		t.Fatal("issues store missing")
	}
	ids, err := ledgerIDs(fake)
	if err != nil {
		t.Fatalf("ledgerIDs: %v", err)
	}
	if len(ids) != 1 || ids[0] != "0001_issues" {
		t.Fatalf("ledger = %v, want [0001_issues]", ids)
	}
}

func TestRunMigrations_ReturnsAppliedAndHead(t *testing.T) {
	ctx := context.Background()
	fake := newFakeIndexedDB()
	second := migrations.Revision{
		ID: "0002_more",
		Schema: &migrations.SchemaDeclaration{
			Stores: []migrations.StoreDeclaration{{
				Name:    "more",
				Columns: []indexeddb.ColumnDef{{Name: "id", PrimaryKey: true}},
			}},
		},
	}

	first, err := migrations.Run(ctx, fake, migrations.RunOptions{Revisions: []migrations.Revision{issuesRevision, second}})
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if len(first.Applied) != 2 || first.Applied[0] != "0001_issues" || first.Applied[1] != "0002_more" {
		t.Fatalf("first applied = %v", first.Applied)
	}
	if first.Head != "0002_more" {
		t.Fatalf("first head = %q", first.Head)
	}

	again, err := migrations.Run(ctx, fake, migrations.RunOptions{Revisions: []migrations.Revision{issuesRevision, second}})
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if len(again.Applied) != 0 {
		t.Fatalf("again applied = %v, want empty", again.Applied)
	}
	if again.Head != "0002_more" {
		t.Fatalf("again head = %q", again.Head)
	}
}

func TestRunMigrations_RestartNoOp(t *testing.T) {
	ctx := context.Background()
	fake := newFakeIndexedDB()
	if _, err := migrations.Run(ctx, fake, migrations.RunOptions{Revisions: []migrations.Revision{issuesRevision}}); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	before := countCalls(fake, "createObjectStore:issues")
	if _, err := migrations.Run(ctx, fake, migrations.RunOptions{Revisions: []migrations.Revision{issuesRevision}}); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	after := countCalls(fake, "createObjectStore:issues")
	if after != before {
		t.Fatalf("createObjectStore calls changed: before=%d after=%d", before, after)
	}
	ids, err := ledgerIDs(fake)
	if err != nil {
		t.Fatalf("ledgerIDs: %v", err)
	}
	if len(ids) != 1 || ids[0] != "0001_issues" {
		t.Fatalf("ledger = %v", ids)
	}
}

func TestRunMigrations_AddsSecondRevision(t *testing.T) {
	ctx := context.Background()
	fake := newFakeIndexedDB()
	if _, err := migrations.Run(ctx, fake, migrations.RunOptions{Revisions: []migrations.Revision{issuesRevision}}); err != nil {
		t.Fatalf("first Run: %v", err)
	}

	addIndex := migrations.Revision{
		ID: "0002_index",
		Schema: &migrations.SchemaDeclaration{
			AddIndexes: []migrations.AddIndexDeclaration{{
				Store: "issues",
				Index: indexeddb.IndexSchema{Name: "by_status", KeyPath: []string{"status"}},
			}},
		},
	}
	if _, err := migrations.Run(ctx, fake, migrations.RunOptions{Revisions: []migrations.Revision{issuesRevision, addIndex}}); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if _, ok := fake.indexes["issues/by_status"]; !ok {
		t.Fatal("index issues/by_status missing")
	}
	ids, err := ledgerIDs(fake)
	if err != nil {
		t.Fatalf("ledgerIDs: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("ledger = %v", ids)
	}
}

func TestRunMigrations_RejectsBackfillFromEqualsInto(t *testing.T) {
	ctx := context.Background()
	fake := newFakeIndexedDB()
	inPlace := migrations.Revision{
		ID: "0001_inplace",
		Backfill: &migrations.BackfillTransform{
			From:  "issues",
			Into:  "issues",
			Value: func(row indexeddb.Record) indexeddb.Record { return row },
		},
	}
	_, err := migrations.Run(ctx, fake, migrations.RunOptions{Revisions: []migrations.Revision{inPlace}})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), `"from" and "into" must differ`) {
		t.Fatalf("error = %v", err)
	}
}

func TestRunMigrations_BackfillCopiesRows(t *testing.T) {
	ctx := context.Background()
	fake := newFakeIndexedDB()
	seed := migrations.Revision{
		ID: "0001_seed",
		Schema: &migrations.SchemaDeclaration{
			Stores: []migrations.StoreDeclaration{
				{Name: "issues", Columns: []indexeddb.ColumnDef{{Name: "id", PrimaryKey: true}}},
				{Name: "issue_index", Columns: []indexeddb.ColumnDef{{Name: "id", PrimaryKey: true}}},
			},
		},
	}
	backfill := migrations.Revision{
		ID: "0002_index",
		Backfill: &migrations.BackfillTransform{
			From: "issues",
			Into: "issue_index",
			Value: func(row indexeddb.Record) indexeddb.Record {
				return indexeddb.Record{"id": row["id"], "text": fmt.Sprintf("issue-%v", row["id"])}
			},
		},
	}

	if _, err := migrations.Run(ctx, fake, migrations.RunOptions{Revisions: []migrations.Revision{seed}}); err != nil {
		t.Fatalf("seed Run: %v", err)
	}
	if err := fake.ObjectStore("issues").Put(ctx, indexeddb.Record{"id": "a"}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if _, err := migrations.Run(ctx, fake, migrations.RunOptions{Revisions: []migrations.Revision{seed, backfill}}); err != nil {
		t.Fatalf("backfill Run: %v", err)
	}
	row, err := fake.ObjectStore("issue_index").Get(ctx, "a")
	if err != nil {
		t.Fatalf("get issue_index: %v", err)
	}
	if row["text"] != "issue-a" {
		t.Fatalf("text = %v", row["text"])
	}
	src, err := fake.ObjectStore("issues").Get(ctx, "a")
	if err != nil {
		t.Fatalf("get issues: %v", err)
	}
	if src["id"] != "a" {
		t.Fatalf("issues id = %v", src["id"])
	}
	ids, err := ledgerIDs(fake)
	if err != nil {
		t.Fatalf("ledgerIDs: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("ledger = %v", ids)
	}
}

func TestRunMigrations_FailingRevisionNotRecorded(t *testing.T) {
	ctx := context.Background()
	fake := newFakeIndexedDB()
	boom := migrations.Revision{
		ID: "0002_boom",
		Backfill: &migrations.BackfillTransform{
			From:  "missing_src",
			Into:  "missing_dst",
			Value: func(row indexeddb.Record) indexeddb.Record { return row },
		},
	}
	_, err := migrations.Run(ctx, fake, migrations.RunOptions{Revisions: []migrations.Revision{issuesRevision, boom}})
	if err == nil {
		t.Fatal("expected error")
	}
	var migErr *migrations.MigrationError
	if !errors.As(err, &migErr) {
		t.Fatalf("error type = %T", err)
	}
	ids, err := ledgerIDs(fake)
	if err != nil {
		t.Fatalf("ledgerIDs: %v", err)
	}
	if len(ids) != 1 || ids[0] != "0001_issues" {
		t.Fatalf("ledger = %v", ids)
	}
}

func TestRunMigrations_FailureReportsCurrent(t *testing.T) {
	ctx := context.Background()
	fake := newFakeIndexedDB()
	boom := migrations.Revision{
		ID: "0002_boom",
		Backfill: &migrations.BackfillTransform{
			From:  "missing_src",
			Into:  "missing_dst",
			Value: func(row indexeddb.Record) indexeddb.Record { return row },
		},
	}
	_, err := migrations.Run(ctx, fake, migrations.RunOptions{Revisions: []migrations.Revision{issuesRevision, boom}})
	var migErr *migrations.MigrationError
	if !errors.As(err, &migErr) {
		t.Fatalf("error type = %T", err)
	}
	if migErr.Current != "0001_issues" {
		t.Fatalf("current = %q", migErr.Current)
	}
	if migErr.Attempted != "0002_boom" {
		t.Fatalf("attempted = %q", migErr.Attempted)
	}
}

func TestRunMigrations_ConcurrentRunnersConverge(t *testing.T) {
	ctx := context.Background()
	fake := newFakeIndexedDB()
	seed := migrations.Revision{
		ID: "0001_seed",
		Schema: &migrations.SchemaDeclaration{
			Stores: []migrations.StoreDeclaration{{
				Name:    "issues",
				Columns: []indexeddb.ColumnDef{{Name: "id", PrimaryKey: true}},
			}},
		},
	}
	if _, err := migrations.Run(ctx, fake, migrations.RunOptions{Revisions: []migrations.Revision{seed}}); err != nil {
		t.Fatalf("seed Run: %v", err)
	}
	if err := fake.ObjectStore("issues").Put(ctx, indexeddb.Record{"id": "a"}); err != nil {
		t.Fatalf("put a: %v", err)
	}
	if err := fake.ObjectStore("issues").Put(ctx, indexeddb.Record{"id": "b", "status": "closed"}); err != nil {
		t.Fatalf("put b: %v", err)
	}

	backfill := migrations.Revision{
		ID: "0002_index",
		Backfill: &migrations.BackfillTransform{
			From: "issues",
			Into: "issue_index",
			Value: func(row indexeddb.Record) indexeddb.Record {
				return indexeddb.Record{"id": row["id"], "text": fmt.Sprintf("issue-%v", row["id"])}
			},
		},
	}
	full := []migrations.Revision{
		seed,
		{
			ID: "0001_5_index",
			Schema: &migrations.SchemaDeclaration{
				Stores: []migrations.StoreDeclaration{{
					Name:    "issue_index",
					Columns: []indexeddb.ColumnDef{{Name: "id", PrimaryKey: true}},
				}},
			},
		},
		backfill,
	}

	done := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := migrations.Run(ctx, fake, migrations.RunOptions{Revisions: full})
			done <- err
		}()
	}
	for range 2 {
		if err := <-done; err != nil {
			t.Fatalf("concurrent Run: %v", err)
		}
	}

	ids, err := ledgerIDs(fake)
	if err != nil {
		t.Fatalf("ledgerIDs: %v", err)
	}
	want := map[string]struct{}{
		"0001_seed":      {},
		"0001_5_index":   {},
		"0002_index":     {},
	}
	if len(ids) != len(want) {
		t.Fatalf("ledger = %v", ids)
	}
	for _, id := range ids {
		if _, ok := want[id]; !ok {
			t.Fatalf("unexpected ledger id %q", id)
		}
	}
	rowA, err := fake.ObjectStore("issue_index").Get(ctx, "a")
	if err != nil {
		t.Fatalf("get a: %v", err)
	}
	if rowA["text"] != "issue-a" {
		t.Fatalf("text a = %v", rowA["text"])
	}
	rowB, err := fake.ObjectStore("issue_index").Get(ctx, "b")
	if err != nil {
		t.Fatalf("get b: %v", err)
	}
	if rowB["text"] != "issue-b" {
		t.Fatalf("text b = %v", rowB["text"])
	}
}

func TestRunMigrations_LedgerAheadOfCode(t *testing.T) {
	ctx := context.Background()
	fake := newFakeIndexedDB()
	if _, err := migrations.Run(ctx, fake, migrations.RunOptions{Revisions: []migrations.Revision{issuesRevision}}); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if err := fake.ObjectStore("_gestalt_migrations").Put(ctx, indexeddb.Record{
		"revision_id": "0002_future",
		"applied_at":  "2026-01-01T00:00:00Z",
	}); err != nil {
		t.Fatalf("put future: %v", err)
	}
	_, err := migrations.Run(ctx, fake, migrations.RunOptions{Revisions: []migrations.Revision{issuesRevision}})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "ledger is ahead") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunMigrations_LedgerGaps(t *testing.T) {
	ctx := context.Background()
	fake := newFakeIndexedDB()
	later := migrations.Revision{
		ID: "0003_more",
		Schema: &migrations.SchemaDeclaration{
			Stores: []migrations.StoreDeclaration{{
				Name:    "more",
				Columns: []indexeddb.ColumnDef{{Name: "id", PrimaryKey: true}},
			}},
		},
	}
	if _, err := migrations.Run(ctx, fake, migrations.RunOptions{Revisions: []migrations.Revision{issuesRevision, later}}); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	inserted := migrations.Revision{
		ID: "0002_between",
		Schema: &migrations.SchemaDeclaration{
			Stores: []migrations.StoreDeclaration{{
				Name:    "between",
				Columns: []indexeddb.ColumnDef{{Name: "id", PrimaryKey: true}},
			}},
		},
	}
	_, err := migrations.Run(ctx, fake, migrations.RunOptions{Revisions: []migrations.Revision{issuesRevision, inserted, later}})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "ledger has gaps") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunMigrations_DuplicateRevisionIDs(t *testing.T) {
	ctx := context.Background()
	fake := newFakeIndexedDB()
	_, err := migrations.Run(ctx, fake, migrations.RunOptions{Revisions: []migrations.Revision{issuesRevision, issuesRevision}})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "duplicate revision id") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunMigrations_RejectsNeitherSchemaNorBackfill(t *testing.T) {
	ctx := context.Background()
	fake := newFakeIndexedDB()
	bad := migrations.Revision{ID: "0001_bad"}
	_, err := migrations.Run(ctx, fake, migrations.RunOptions{Revisions: []migrations.Revision{bad}})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "exactly one of") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunMigrations_NullSchemaWithBackfillRunsBackfill(t *testing.T) {
	ctx := context.Background()
	fake := newFakeIndexedDB()
	seed := migrations.Revision{
		ID: "0001_seed",
		Schema: &migrations.SchemaDeclaration{
			Stores: []migrations.StoreDeclaration{
				{Name: "src", Columns: []indexeddb.ColumnDef{{Name: "id", PrimaryKey: true}}},
				{Name: "dst", Columns: []indexeddb.ColumnDef{{Name: "id", PrimaryKey: true}}},
			},
		},
	}
	if _, err := migrations.Run(ctx, fake, migrations.RunOptions{Revisions: []migrations.Revision{seed}}); err != nil {
		t.Fatalf("seed Run: %v", err)
	}
	if err := fake.ObjectStore("src").Put(ctx, indexeddb.Record{"id": "a"}); err != nil {
		t.Fatalf("put: %v", err)
	}
	weird := migrations.Revision{
		ID: "0002_weird",
		Backfill: &migrations.BackfillTransform{
			From:  "src",
			Into:  "dst",
			Value: func(row indexeddb.Record) indexeddb.Record { return row },
		},
	}
	if _, err := migrations.Run(ctx, fake, migrations.RunOptions{Revisions: []migrations.Revision{seed, weird}}); err != nil {
		t.Fatalf("weird Run: %v", err)
	}
	row, err := fake.ObjectStore("dst").Get(ctx, "a")
	if err != nil {
		t.Fatalf("get dst: %v", err)
	}
	if row["id"] != "a" {
		t.Fatalf("dst id = %v", row["id"])
	}
	ids, err := ledgerIDs(fake)
	if err != nil {
		t.Fatalf("ledgerIDs: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("ledger = %v", ids)
	}
}
