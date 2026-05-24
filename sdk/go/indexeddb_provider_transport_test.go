package gestalt_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/sdk/go/protov1/v1"
)

const nativeIndexedDBProviderBinding = "native-provider"

func TestServeIndexedDBProvider_NativeCursorAndErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	socket := nativeIndexedDBSocket(t, "cursor")
	t.Setenv(proto.EnvProviderSocket, socket)
	t.Setenv(gestalt.EnvHostServiceSocket, socket)

	provider := newNativeIndexedDBProvider()
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- gestalt.ServeIndexedDBProvider(ctx, provider)
	}()
	waitForSocket(t, socket, serveErr)

	client, err := gestalt.IndexedDB(context.Background(), nativeIndexedDBProviderBinding)
	if err != nil {
		t.Fatalf("IndexedDB: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	store := "native_cursor"
	_, err = client.CreateObjectStore(ctx, store, gestalt.ObjectStoreSchema{
		Indexes: []gestalt.IndexSchema{
			{Name: "by_pair", KeyPath: []string{"status", "priority"}},
		},
	})
	if err != nil {
		t.Fatalf("CreateObjectStore: %v", err)
	}
	for _, record := range []gestalt.Record{
		{"id": "a", "status": "active", "priority": int64(2), "name": "A"},
		{"id": "b", "status": "active", "priority": int64(1), "name": "B"},
		{"id": "c", "status": "inactive", "priority": int64(1), "name": "C"},
	} {
		if err := client.ObjectStore(store).Put(ctx, record); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}

	cursor, err := client.ObjectStore(store).Index("by_pair").OpenCursor(ctx, nil, gestalt.CursorNext)
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

	_, err = client.ObjectStore(store).Get(ctx, "missing")
	if !errors.Is(err, gestalt.ErrNotFound) {
		t.Fatalf("missing Get error = %v, want ErrNotFound", err)
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
	t.Setenv(gestalt.EnvHostServiceSocket, socket)

	provider := newNativeIndexedDBProvider()
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- gestalt.ServeIndexedDBProvider(ctx, provider)
	}()
	waitForSocket(t, socket, serveErr)

	client, err := gestalt.IndexedDB(context.Background(), nativeIndexedDBProviderBinding)
	if err != nil {
		t.Fatalf("IndexedDB: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	store := "readonly_native"
	if _, err := client.CreateObjectStore(ctx, store, gestalt.ObjectStoreSchema{}); err != nil {
		t.Fatalf("CreateObjectStore: %v", err)
	}
	tx, err := client.Transaction(ctx, []string{store}, gestalt.TransactionReadonly, gestalt.TransactionOptions{})
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
