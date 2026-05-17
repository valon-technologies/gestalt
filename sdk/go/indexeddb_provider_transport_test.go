package gestalt_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/sdk/go/internal/gen/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestServeIndexedDBProvider_NativeCursorAndErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	socket := nativeIndexedDBSocket(t, "cursor")
	t.Setenv(proto.EnvProviderSocket, socket)
	t.Setenv(gestalt.EnvIndexedDBSocket, socket)

	provider := newNativeIndexedDBRootProvider()
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- gestalt.ServeIndexedDBProvider(ctx, provider)
	}()
	waitForSocket(t, socket, serveErr)

	client, err := gestalt.IndexedDB()
	if err != nil {
		t.Fatalf("IndexedDB: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	store := "native_cursor"
	db, err := client.Open(ctx, "cursor-db", gestalt.OpenOptions{
		Upgrade: func(ctx context.Context, upgrade gestalt.UpgradeContext) error {
			return upgrade.CreateObjectStore(ctx, store, gestalt.ObjectStoreSchema{
				Indexes: []gestalt.IndexSchema{
					{Name: "by_pair", KeyPath: []string{"status", "priority"}},
				},
			})
		},
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	for _, record := range []gestalt.Record{
		{"id": "a", "status": "active", "priority": int64(2), "name": "A"},
		{"id": "b", "status": "active", "priority": int64(1), "name": "B"},
		{"id": "c", "status": "inactive", "priority": int64(1), "name": "C"},
	} {
		if err := db.ObjectStore(store).Put(ctx, record); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}

	cursor, err := db.ObjectStore(store).Index("by_pair").OpenCursor(ctx, nil, gestalt.CursorNext)
	if err != nil {
		t.Fatalf("OpenCursor: %v", err)
	}
	defer func() { _ = cursor.Close() }()

	var keys []any
	for cursor.Continue() {
		keys = append(keys, cursor.Key())
	}
	if err := cursor.Err(); err != nil {
		t.Fatalf("cursor Err: %v", err)
	}
	wantKeys := []any{
		[]any{"active", int64(1)},
		[]any{"active", int64(2)},
		[]any{"inactive", int64(1)},
	}
	if !reflect.DeepEqual(keys, wantKeys) {
		t.Fatalf("cursor keys = %#v, want %#v", keys, wantKeys)
	}

	_, err = db.ObjectStore(store).Get(ctx, "missing")
	if !errors.Is(err, gestalt.ErrNotFound) {
		t.Fatalf("missing Get error = %v, want ErrNotFound", err)
	}
}

func TestServeIndexedDBProvider_OpenUpgradeAndScopedOperations(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	socket := nativeIndexedDBSocket(t, "provider-open")
	t.Setenv(proto.EnvProviderSocket, socket)
	t.Setenv(gestalt.EnvIndexedDBSocket, socket)

	provider := newNativeIndexedDBRootProvider()
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- gestalt.ServeIndexedDBProvider(ctx, provider)
	}()
	waitForSocket(t, socket, serveErr)

	client, err := gestalt.IndexedDB()
	if err != nil {
		t.Fatalf("IndexedDB: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	if _, err := client.OpenCurrent(ctx, "missing", gestalt.OpenOptions{}); !errors.Is(err, gestalt.ErrNotFound) {
		t.Fatalf("OpenCurrent missing = %v, want ErrNotFound", err)
	}

	version := uint64(2)
	sawUpgrade := false
	db, err := client.Open(ctx, "app", gestalt.OpenOptions{
		Version: &version,
		Upgrade: func(ctx context.Context, upgrade gestalt.UpgradeContext) error {
			sawUpgrade = true
			if upgrade.OldVersion() != 0 || upgrade.NewVersion() != version {
				t.Fatalf("upgrade versions = %d -> %d, want 0 -> %d", upgrade.OldVersion(), upgrade.NewVersion(), version)
			}
			names, err := upgrade.ObjectStoreNames(ctx)
			if err != nil {
				return err
			}
			if len(names) != 0 {
				t.Fatalf("initial object store names = %v, want empty", names)
			}
			return upgrade.CreateObjectStore(ctx, "items", gestalt.ObjectStoreSchema{
				Indexes: []gestalt.IndexSchema{{Name: "by_status", KeyPath: []string{"status"}}},
			})
		},
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	if !sawUpgrade {
		t.Fatal("Open did not run upgrade callback")
	}
	if db.Name() != "app" || db.Version() != version {
		t.Fatalf("database metadata = %q v%d, want app v%d", db.Name(), db.Version(), version)
	}
	names, err := db.ObjectStoreNames(ctx)
	if err != nil {
		t.Fatalf("ObjectStoreNames: %v", err)
	}
	if !reflect.DeepEqual(names, []string{"items"}) {
		t.Fatalf("ObjectStoreNames = %v, want [items]", names)
	}

	if err := db.ObjectStore("items").Put(ctx, gestalt.Record{"id": "row-1", "status": "active"}); err != nil {
		t.Fatalf("Put via database handle: %v", err)
	}
	record, err := db.ObjectStore("items").Get(ctx, "row-1")
	if err != nil {
		t.Fatalf("Get via database handle: %v", err)
	}
	if record["status"] != "active" {
		t.Fatalf("record status = %v, want active", record["status"])
	}

	infos, err := client.Databases(ctx)
	if err != nil {
		t.Fatalf("Databases: %v", err)
	}
	if !reflect.DeepEqual(infos, []gestalt.DatabaseInfo{{Name: "app", Version: version}}) {
		t.Fatalf("Databases = %#v, want app v%d", infos, version)
	}
	cmp, err := client.CompareKeys(ctx, "a", "b")
	if err != nil {
		t.Fatalf("CompareKeys: %v", err)
	}
	if cmp >= 0 {
		t.Fatalf("CompareKeys(a,b) = %d, want negative", cmp)
	}

	_, err = client.ObjectStore("items").Get(ctx, "row-1")
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("no-connection Get on provider = %v, want FailedPrecondition", err)
	}
}

func waitForSocket(t *testing.T, socket string, serveErr <-chan error) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-serveErr:
			t.Fatalf("ServeIndexedDBProvider exited: %v", err)
		default:
		}
		if _, err := os.Stat(socket); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("socket %q was not created", socket)
}

func nativeIndexedDBSocket(t *testing.T, name string) string {
	t.Helper()
	socket := filepath.Join(os.TempDir(), fmt.Sprintf("idb-native-%d-%s.sock", os.Getpid(), name))
	_ = os.Remove(socket)
	t.Cleanup(func() { _ = os.Remove(socket) })
	return socket
}

type nativeIndexedDBProvider struct {
	mu      sync.Mutex
	stores  map[string]map[string]gestalt.Record
	schemas map[string]gestalt.ObjectStoreSchema
}

func newNativeIndexedDBProvider() *nativeIndexedDBProvider {
	return &nativeIndexedDBProvider{
		stores:  map[string]map[string]gestalt.Record{},
		schemas: map[string]gestalt.ObjectStoreSchema{},
	}
}

func (p *nativeIndexedDBProvider) Configure(context.Context, string, map[string]any) error {
	return nil
}

type nativeIndexedDBRootProvider struct {
	mu        sync.Mutex
	databases map[string]*nativeIndexedDBDatabase
}

type nativeIndexedDBDatabase struct {
	*nativeIndexedDBProvider
	name    string
	version uint64
}

func newNativeIndexedDBRootProvider() *nativeIndexedDBRootProvider {
	return &nativeIndexedDBRootProvider{
		databases: map[string]*nativeIndexedDBDatabase{},
	}
}

func (p *nativeIndexedDBRootProvider) Configure(context.Context, string, map[string]any) error {
	return nil
}

func (p *nativeIndexedDBRootProvider) OpenDatabase(ctx context.Context, name string, opts gestalt.OpenOptions) (gestalt.IndexedDBDatabase, error) {
	p.mu.Lock()
	db := p.databases[name]
	oldVersion := uint64(0)
	if db != nil {
		oldVersion = db.version
	}
	newVersion := uint64(1)
	if opts.Version != nil {
		newVersion = *opts.Version
	} else if db != nil {
		newVersion = oldVersion
	}
	if newVersion < oldVersion {
		p.mu.Unlock()
		return nil, gestalt.InvalidArgument("indexeddb open version is lower than current version")
	}
	if db == nil {
		db = &nativeIndexedDBDatabase{
			nativeIndexedDBProvider: newNativeIndexedDBProvider(),
			name:                    name,
			version:                 newVersion,
		}
		p.databases[name] = db
	} else {
		db.version = newVersion
	}
	needsUpgrade := oldVersion == 0 || newVersion > oldVersion
	p.mu.Unlock()

	if needsUpgrade && opts.Upgrade != nil {
		if err := opts.Upgrade(ctx, &nativeIndexedDBUpgradeContext{
			db:         db,
			oldVersion: oldVersion,
			newVersion: newVersion,
		}); err != nil {
			return nil, err
		}
	}
	return db, nil
}

func (p *nativeIndexedDBRootProvider) OpenCurrentDatabase(_ context.Context, name string, _ gestalt.OpenOptions) (gestalt.IndexedDBDatabase, error) {
	p.mu.Lock()
	db := p.databases[name]
	p.mu.Unlock()
	if db == nil {
		return nil, gestalt.ErrNotFound
	}
	return db, nil
}

func (p *nativeIndexedDBRootProvider) DeleteDatabase(_ context.Context, name string, _ gestalt.DeleteOptions) (gestalt.DeleteDatabaseResult, error) {
	p.mu.Lock()
	db := p.databases[name]
	delete(p.databases, name)
	p.mu.Unlock()
	if db == nil {
		return gestalt.DeleteDatabaseResult{Name: name}, nil
	}
	return gestalt.DeleteDatabaseResult{Name: name, OldVersion: db.version}, nil
}

func (p *nativeIndexedDBRootProvider) Databases(context.Context) ([]gestalt.DatabaseInfo, error) {
	p.mu.Lock()
	out := make([]gestalt.DatabaseInfo, 0, len(p.databases))
	for _, db := range p.databases {
		out = append(out, gestalt.DatabaseInfo{Name: db.name, Version: db.version})
	}
	p.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (p *nativeIndexedDBRootProvider) CompareKeys(_ context.Context, first any, second any) (int, error) {
	left := fmt.Sprint(first)
	right := fmt.Sprint(second)
	return strings.Compare(left, right), nil
}

func (db *nativeIndexedDBDatabase) Name() string { return db.name }

func (db *nativeIndexedDBDatabase) Version() uint64 { return db.version }

func (db *nativeIndexedDBDatabase) ObjectStoreNames(context.Context) ([]string, error) {
	db.mu.Lock()
	names := make([]string, 0, len(db.schemas))
	for name := range db.schemas {
		names = append(names, name)
	}
	db.mu.Unlock()
	sort.Strings(names)
	return names, nil
}

func (db *nativeIndexedDBDatabase) Close() error { return nil }

func (db *nativeIndexedDBDatabase) CreateIndex(_ context.Context, store string, index gestalt.IndexSchema) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	schema, ok := db.schemas[store]
	if !ok {
		return gestalt.ErrNotFound
	}
	schema.Indexes = append(schema.Indexes, index)
	db.schemas[store] = schema
	return nil
}

func (db *nativeIndexedDBDatabase) DeleteIndex(_ context.Context, store string, name string) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	schema, ok := db.schemas[store]
	if !ok {
		return gestalt.ErrNotFound
	}
	filtered := schema.Indexes[:0]
	for _, index := range schema.Indexes {
		if index.Name != name {
			filtered = append(filtered, index)
		}
	}
	schema.Indexes = filtered
	db.schemas[store] = schema
	return nil
}

type nativeIndexedDBUpgradeContext struct {
	db         *nativeIndexedDBDatabase
	oldVersion uint64
	newVersion uint64
}

func (u *nativeIndexedDBUpgradeContext) OldVersion() uint64 { return u.oldVersion }

func (u *nativeIndexedDBUpgradeContext) NewVersion() uint64 { return u.newVersion }

func (u *nativeIndexedDBUpgradeContext) Database() gestalt.UpgradeDatabase {
	return (*nativeIndexedDBUpgradeDatabase)(u)
}

func (u *nativeIndexedDBUpgradeContext) ObjectStoreNames(ctx context.Context) ([]string, error) {
	return u.db.ObjectStoreNames(ctx)
}

func (u *nativeIndexedDBUpgradeContext) CreateObjectStore(ctx context.Context, name string, schema gestalt.ObjectStoreSchema) error {
	return u.db.CreateObjectStore(ctx, name, schema)
}

func (u *nativeIndexedDBUpgradeContext) DeleteObjectStore(ctx context.Context, name string) error {
	return u.db.DeleteObjectStore(ctx, name)
}

func (u *nativeIndexedDBUpgradeContext) CreateIndex(ctx context.Context, store string, index gestalt.IndexSchema) error {
	return u.db.CreateIndex(ctx, store, index)
}

func (u *nativeIndexedDBUpgradeContext) DeleteIndex(ctx context.Context, store string, name string) error {
	return u.db.DeleteIndex(ctx, store, name)
}

type nativeIndexedDBUpgradeDatabase nativeIndexedDBUpgradeContext

func (db *nativeIndexedDBUpgradeDatabase) Name() string { return db.db.name }

func (db *nativeIndexedDBUpgradeDatabase) ObjectStoreNames(ctx context.Context) ([]string, error) {
	return (*nativeIndexedDBUpgradeContext)(db).ObjectStoreNames(ctx)
}

func (db *nativeIndexedDBUpgradeDatabase) CreateObjectStore(ctx context.Context, name string, schema gestalt.ObjectStoreSchema) error {
	return (*nativeIndexedDBUpgradeContext)(db).CreateObjectStore(ctx, name, schema)
}

func (db *nativeIndexedDBUpgradeDatabase) DeleteObjectStore(ctx context.Context, name string) error {
	return (*nativeIndexedDBUpgradeContext)(db).DeleteObjectStore(ctx, name)
}

func (db *nativeIndexedDBUpgradeDatabase) CreateIndex(ctx context.Context, store string, index gestalt.IndexSchema) error {
	return (*nativeIndexedDBUpgradeContext)(db).CreateIndex(ctx, store, index)
}

func (db *nativeIndexedDBUpgradeDatabase) DeleteIndex(ctx context.Context, store string, name string) error {
	return (*nativeIndexedDBUpgradeContext)(db).DeleteIndex(ctx, store, name)
}

func (p *nativeIndexedDBProvider) CreateObjectStore(_ context.Context, name string, schema gestalt.ObjectStoreSchema) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.stores[name]; ok {
		return fmt.Errorf("%w: object store %q", gestalt.ErrAlreadyExists, name)
	}
	p.stores[name] = map[string]gestalt.Record{}
	p.schemas[name] = schema
	return nil
}

func (p *nativeIndexedDBProvider) DeleteObjectStore(_ context.Context, name string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.stores, name)
	delete(p.schemas, name)
	return nil
}

func (p *nativeIndexedDBProvider) Get(_ context.Context, req gestalt.IndexedDBObjectStoreRequest) (gestalt.Record, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	record, ok := p.stores[req.Store][req.ID]
	if !ok {
		return nil, fmt.Errorf("%w: record %q", gestalt.ErrNotFound, req.ID)
	}
	return cloneRecord(record), nil
}

func (p *nativeIndexedDBProvider) GetKey(ctx context.Context, req gestalt.IndexedDBObjectStoreRequest) (string, error) {
	if _, err := p.Get(ctx, req); err != nil {
		return "", err
	}
	return req.ID, nil
}

func (p *nativeIndexedDBProvider) Add(ctx context.Context, req gestalt.IndexedDBRecordRequest) error {
	id, err := recordID(req.Record)
	if err != nil {
		return err
	}
	p.mu.Lock()
	_, exists := p.stores[req.Store][id]
	p.mu.Unlock()
	if exists {
		return fmt.Errorf("%w: record %q", gestalt.ErrAlreadyExists, id)
	}
	return p.Put(ctx, req)
}

func (p *nativeIndexedDBProvider) Put(_ context.Context, req gestalt.IndexedDBRecordRequest) error {
	id, err := recordID(req.Record)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.stores[req.Store]; !ok {
		p.stores[req.Store] = map[string]gestalt.Record{}
	}
	p.stores[req.Store][id] = cloneRecord(req.Record)
	return nil
}

func (p *nativeIndexedDBProvider) Delete(_ context.Context, req gestalt.IndexedDBObjectStoreRequest) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.stores[req.Store], req.ID)
	return nil
}

func (p *nativeIndexedDBProvider) Clear(_ context.Context, store string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stores[store] = map[string]gestalt.Record{}
	return nil
}

func (p *nativeIndexedDBProvider) GetAll(_ context.Context, req gestalt.IndexedDBObjectStoreRangeRequest) ([]gestalt.Record, error) {
	entries, err := p.objectEntries(req.Store, req.Range)
	if err != nil {
		return nil, err
	}
	records := make([]gestalt.Record, len(entries))
	for i, entry := range entries {
		records[i] = cloneRecord(entry.Record)
	}
	return records, nil
}

func (p *nativeIndexedDBProvider) GetAllKeys(_ context.Context, req gestalt.IndexedDBObjectStoreRangeRequest) ([]string, error) {
	entries, err := p.objectEntries(req.Store, req.Range)
	if err != nil {
		return nil, err
	}
	keys := make([]string, len(entries))
	for i, entry := range entries {
		keys[i] = entry.PrimaryKey
	}
	return keys, nil
}

func (p *nativeIndexedDBProvider) Count(ctx context.Context, req gestalt.IndexedDBObjectStoreRangeRequest) (int64, error) {
	records, err := p.GetAll(ctx, req)
	return int64(len(records)), err
}

func (p *nativeIndexedDBProvider) DeleteRange(ctx context.Context, req gestalt.IndexedDBObjectStoreRangeRequest) (int64, error) {
	keys, err := p.GetAllKeys(ctx, req)
	if err != nil {
		return 0, err
	}
	for _, key := range keys {
		if err := p.Delete(ctx, gestalt.IndexedDBObjectStoreRequest{Store: req.Store, ID: key}); err != nil {
			return 0, err
		}
	}
	return int64(len(keys)), nil
}

func (p *nativeIndexedDBProvider) IndexGet(ctx context.Context, req gestalt.IndexedDBIndexQueryRequest) (gestalt.Record, error) {
	records, err := p.IndexGetAll(ctx, req)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, gestalt.ErrNotFound
	}
	return records[0], nil
}

func (p *nativeIndexedDBProvider) IndexGetKey(ctx context.Context, req gestalt.IndexedDBIndexQueryRequest) (string, error) {
	keys, err := p.IndexGetAllKeys(ctx, req)
	if err != nil {
		return "", err
	}
	if len(keys) == 0 {
		return "", gestalt.ErrNotFound
	}
	return keys[0], nil
}

func (p *nativeIndexedDBProvider) IndexGetAll(_ context.Context, req gestalt.IndexedDBIndexQueryRequest) ([]gestalt.Record, error) {
	entries, err := p.indexEntries(req)
	if err != nil {
		return nil, err
	}
	records := make([]gestalt.Record, len(entries))
	for i, entry := range entries {
		records[i] = cloneRecord(entry.Record)
	}
	return records, nil
}

func (p *nativeIndexedDBProvider) IndexGetAllKeys(_ context.Context, req gestalt.IndexedDBIndexQueryRequest) ([]string, error) {
	entries, err := p.indexEntries(req)
	if err != nil {
		return nil, err
	}
	keys := make([]string, len(entries))
	for i, entry := range entries {
		keys[i] = entry.PrimaryKey
	}
	return keys, nil
}

func (p *nativeIndexedDBProvider) IndexCount(ctx context.Context, req gestalt.IndexedDBIndexQueryRequest) (int64, error) {
	records, err := p.IndexGetAll(ctx, req)
	return int64(len(records)), err
}

func (p *nativeIndexedDBProvider) IndexDelete(ctx context.Context, req gestalt.IndexedDBIndexQueryRequest) (int64, error) {
	keys, err := p.IndexGetAllKeys(ctx, req)
	if err != nil {
		return 0, err
	}
	for _, key := range keys {
		if err := p.Delete(ctx, gestalt.IndexedDBObjectStoreRequest{Store: req.Store, ID: key}); err != nil {
			return 0, err
		}
	}
	return int64(len(keys)), nil
}

func (p *nativeIndexedDBProvider) OpenCursor(_ context.Context, req gestalt.IndexedDBOpenCursorRequest) (gestalt.IndexedDBCursor, error) {
	var entries []gestalt.IndexedDBCursorSnapshotEntry
	var err error
	if req.Index == "" {
		entries, err = p.objectEntries(req.Store, req.Range)
	} else {
		entries, err = p.indexEntries(gestalt.IndexedDBIndexQueryRequest{
			Store: req.Store, Index: req.Index, Values: req.Values, Range: req.Range,
		})
	}
	if err != nil {
		return nil, err
	}
	snapshot := gestalt.NewIndexedDBCursorSnapshot(req)
	if err := snapshot.Load(entries, req.Range); err != nil {
		return nil, err
	}
	return &nativeIndexedDBCursor{provider: p, store: req.Store, snapshot: snapshot}, nil
}

func (p *nativeIndexedDBProvider) BeginTransaction(_ context.Context, req gestalt.IndexedDBBeginTransactionRequest) (gestalt.IndexedDBTransaction, error) {
	if len(req.Stores) == 0 {
		return nil, fmt.Errorf("%w: at least one store is required", gestalt.ErrInvalidTransaction)
	}
	return &nativeIndexedDBTransaction{provider: p, mode: req.Mode}, nil
}

func (p *nativeIndexedDBProvider) objectEntries(store string, r *gestalt.KeyRange) ([]gestalt.IndexedDBCursorSnapshotEntry, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	records := p.stores[store]
	entries := make([]gestalt.IndexedDBCursorSnapshotEntry, 0, len(records))
	for id, record := range records {
		entry := gestalt.IndexedDBCursorSnapshotEntry{
			Key:             id,
			PrimaryKey:      id,
			PrimaryKeyValue: id,
			Record:          cloneRecord(record),
		}
		entries = append(entries, entry)
	}
	snapshot := gestalt.NewIndexedDBCursorSnapshot(gestalt.IndexedDBOpenCursorRequest{})
	return snapshot.ApplyRange(entries, r)
}

func (p *nativeIndexedDBProvider) indexEntries(req gestalt.IndexedDBIndexQueryRequest) ([]gestalt.IndexedDBCursorSnapshotEntry, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	records := p.stores[req.Store]
	entries := make([]gestalt.IndexedDBCursorSnapshotEntry, 0, len(records))
	for id, record := range records {
		key, ok := p.indexKey(req.Store, req.Index, record)
		if !ok || !indexValuesMatch(key, req.Values) {
			continue
		}
		entries = append(entries, gestalt.IndexedDBCursorSnapshotEntry{
			Key:             key,
			PrimaryKey:      id,
			PrimaryKeyValue: id,
			Record:          cloneRecord(record),
		})
	}
	snapshot := gestalt.NewIndexedDBCursorSnapshot(gestalt.IndexedDBOpenCursorRequest{Index: req.Index})
	return snapshot.ApplyRange(entries, req.Range)
}

func (p *nativeIndexedDBProvider) indexKey(store, index string, record gestalt.Record) (any, bool) {
	for _, idx := range p.schemas[store].Indexes {
		if idx.Name != index {
			continue
		}
		parts := make([]any, len(idx.KeyPath))
		for i, field := range idx.KeyPath {
			value, ok := record[field]
			if !ok {
				return nil, false
			}
			parts[i] = value
		}
		if len(parts) == 1 {
			return parts[0], true
		}
		return parts, true
	}
	return nil, false
}

type nativeIndexedDBCursor struct {
	provider *nativeIndexedDBProvider
	store    string
	snapshot gestalt.IndexedDBCursorSnapshot
}

func (c *nativeIndexedDBCursor) Next(context.Context) (*gestalt.IndexedDBCursorEntry, error) {
	entry, err := c.snapshot.Next()
	return publicCursorEntry(entry), err
}

func (c *nativeIndexedDBCursor) ContinueToKey(_ context.Context, key any) (*gestalt.IndexedDBCursorEntry, error) {
	entry, err := c.snapshot.ContinueToKey(key)
	return publicCursorEntry(entry), err
}

func (c *nativeIndexedDBCursor) Advance(_ context.Context, count int) (*gestalt.IndexedDBCursorEntry, error) {
	entry, err := c.snapshot.Advance(count)
	return publicCursorEntry(entry), err
}

func (c *nativeIndexedDBCursor) Delete(ctx context.Context) error {
	entry, err := c.snapshot.Current()
	if err != nil {
		return err
	}
	return c.provider.Delete(ctx, gestalt.IndexedDBObjectStoreRequest{Store: c.store, ID: entry.PrimaryKey})
}

func (c *nativeIndexedDBCursor) Update(ctx context.Context, record gestalt.Record) (*gestalt.IndexedDBCursorEntry, error) {
	entry, err := c.snapshot.Current()
	if err != nil {
		return nil, err
	}
	updated := cloneRecord(record)
	updated["id"] = entry.PrimaryKey
	if err := c.provider.Put(ctx, gestalt.IndexedDBRecordRequest{Store: c.store, Record: updated}); err != nil {
		return nil, err
	}
	entry.Record = updated
	return publicCursorEntry(entry), nil
}

func (c *nativeIndexedDBCursor) Close() error { return nil }

type nativeIndexedDBTransaction struct {
	provider *nativeIndexedDBProvider
	mode     gestalt.TransactionMode
	failed   error
	done     bool
}

func (tx *nativeIndexedDBTransaction) Commit(context.Context) error {
	if tx.failed != nil {
		tx.done = true
		return tx.failed
	}
	if tx.done {
		return gestalt.ErrTransactionDone
	}
	tx.done = true
	return nil
}

func (tx *nativeIndexedDBTransaction) Abort(context.Context) error {
	if tx.done {
		return gestalt.ErrTransactionDone
	}
	tx.done = true
	return nil
}

func (tx *nativeIndexedDBTransaction) Get(ctx context.Context, req gestalt.IndexedDBObjectStoreRequest) (gestalt.Record, error) {
	return tx.provider.Get(ctx, req)
}

func (tx *nativeIndexedDBTransaction) GetKey(ctx context.Context, req gestalt.IndexedDBObjectStoreRequest) (string, error) {
	return tx.provider.GetKey(ctx, req)
}

func (tx *nativeIndexedDBTransaction) Add(ctx context.Context, req gestalt.IndexedDBRecordRequest) error {
	if err := tx.writeAllowed(); err != nil {
		return err
	}
	return tx.provider.Add(ctx, req)
}

func (tx *nativeIndexedDBTransaction) Put(ctx context.Context, req gestalt.IndexedDBRecordRequest) error {
	if err := tx.writeAllowed(); err != nil {
		return err
	}
	return tx.provider.Put(ctx, req)
}

func (tx *nativeIndexedDBTransaction) Delete(ctx context.Context, req gestalt.IndexedDBObjectStoreRequest) error {
	if err := tx.writeAllowed(); err != nil {
		return err
	}
	return tx.provider.Delete(ctx, req)
}

func (tx *nativeIndexedDBTransaction) Clear(ctx context.Context, store string) error {
	if err := tx.writeAllowed(); err != nil {
		return err
	}
	return tx.provider.Clear(ctx, store)
}

func (tx *nativeIndexedDBTransaction) GetAll(ctx context.Context, req gestalt.IndexedDBObjectStoreRangeRequest) ([]gestalt.Record, error) {
	return tx.provider.GetAll(ctx, req)
}

func (tx *nativeIndexedDBTransaction) GetAllKeys(ctx context.Context, req gestalt.IndexedDBObjectStoreRangeRequest) ([]string, error) {
	return tx.provider.GetAllKeys(ctx, req)
}

func (tx *nativeIndexedDBTransaction) Count(ctx context.Context, req gestalt.IndexedDBObjectStoreRangeRequest) (int64, error) {
	return tx.provider.Count(ctx, req)
}

func (tx *nativeIndexedDBTransaction) DeleteRange(ctx context.Context, req gestalt.IndexedDBObjectStoreRangeRequest) (int64, error) {
	if err := tx.writeAllowed(); err != nil {
		return 0, err
	}
	return tx.provider.DeleteRange(ctx, req)
}

func (tx *nativeIndexedDBTransaction) IndexGet(ctx context.Context, req gestalt.IndexedDBIndexQueryRequest) (gestalt.Record, error) {
	return tx.provider.IndexGet(ctx, req)
}

func (tx *nativeIndexedDBTransaction) IndexGetKey(ctx context.Context, req gestalt.IndexedDBIndexQueryRequest) (string, error) {
	return tx.provider.IndexGetKey(ctx, req)
}

func (tx *nativeIndexedDBTransaction) IndexGetAll(ctx context.Context, req gestalt.IndexedDBIndexQueryRequest) ([]gestalt.Record, error) {
	return tx.provider.IndexGetAll(ctx, req)
}

func (tx *nativeIndexedDBTransaction) IndexGetAllKeys(ctx context.Context, req gestalt.IndexedDBIndexQueryRequest) ([]string, error) {
	return tx.provider.IndexGetAllKeys(ctx, req)
}

func (tx *nativeIndexedDBTransaction) IndexCount(ctx context.Context, req gestalt.IndexedDBIndexQueryRequest) (int64, error) {
	return tx.provider.IndexCount(ctx, req)
}

func (tx *nativeIndexedDBTransaction) IndexDelete(ctx context.Context, req gestalt.IndexedDBIndexQueryRequest) (int64, error) {
	if err := tx.writeAllowed(); err != nil {
		return 0, err
	}
	return tx.provider.IndexDelete(ctx, req)
}

func (tx *nativeIndexedDBTransaction) writeAllowed() error {
	if tx.mode == gestalt.TransactionReadonly {
		return gestalt.ErrReadOnly
	}
	return nil
}

func publicCursorEntry(entry *gestalt.IndexedDBCursorSnapshotEntry) *gestalt.IndexedDBCursorEntry {
	if entry == nil {
		return nil
	}
	return &gestalt.IndexedDBCursorEntry{
		Key:        entry.Key,
		PrimaryKey: entry.PrimaryKey,
		Record:     cloneRecord(entry.Record),
	}
}

func recordID(record gestalt.Record) (string, error) {
	id, ok := record["id"]
	if !ok {
		return "", gestalt.InvalidArgument("id is required")
	}
	return fmt.Sprint(id), nil
}

func cloneRecord(record gestalt.Record) gestalt.Record {
	if record == nil {
		return nil
	}
	out := make(gestalt.Record, len(record))
	for key, value := range record {
		out[key] = value
	}
	return out
}

func indexValuesMatch(key any, values []any) bool {
	if len(values) == 0 {
		return true
	}
	var parts []any
	if arr, ok := key.([]any); ok {
		parts = arr
	} else {
		parts = []any{key}
	}
	if len(values) > len(parts) {
		return false
	}
	for i, value := range values {
		if gestalt.CompareIndexedDBValues(parts[i], value) != 0 {
			return false
		}
	}
	return true
}

func TestServeIndexedDBProvider_NativeReadonlySentinel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	socket := nativeIndexedDBSocket(t, "readonly")
	t.Setenv(proto.EnvProviderSocket, socket)
	t.Setenv(gestalt.EnvIndexedDBSocket, socket)

	provider := newNativeIndexedDBRootProvider()
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- gestalt.ServeIndexedDBProvider(ctx, provider)
	}()
	waitForSocket(t, socket, serveErr)

	client, err := gestalt.IndexedDB()
	if err != nil {
		t.Fatalf("IndexedDB: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	store := "readonly_native"
	db, err := client.Open(ctx, "readonly-db", gestalt.OpenOptions{
		Upgrade: func(ctx context.Context, upgrade gestalt.UpgradeContext) error {
			return upgrade.CreateObjectStore(ctx, store, gestalt.ObjectStoreSchema{})
		},
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	tx, err := db.Transaction(ctx, []string{store}, gestalt.TransactionReadonly, gestalt.TransactionOptions{})
	if err != nil {
		t.Fatalf("Transaction: %v", err)
	}
	err = tx.ObjectStore(store).Put(ctx, gestalt.Record{"id": "a"})
	if !errors.Is(err, gestalt.ErrReadOnly) {
		t.Fatalf("readonly Put error = %v, want ErrReadOnly", err)
	}
	if err := tx.Commit(ctx); err == nil || !strings.Contains(err.Error(), "readonly") {
		t.Fatalf("Commit after readonly failure = %v, want readonly failure", err)
	}
}
