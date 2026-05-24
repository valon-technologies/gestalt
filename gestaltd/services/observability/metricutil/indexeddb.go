package metricutil

import (
	"context"
	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	"time"

	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

type instrumentedIndexedDB struct {
	inner indexeddb.IndexedDB
	db    string
}

// InstrumentIndexedDB wraps an IndexedDB instance to record metrics on every
// ObjectStore and Index operation.
func InstrumentIndexedDB(db indexeddb.IndexedDB, dbName string) indexeddb.IndexedDB {
	return &instrumentedIndexedDB{inner: db, db: dbName}
}

// UnwrapIndexedDB returns the underlying IndexedDB if db is instrumented,
// or db itself otherwise. Use this before type-asserting optional interfaces
// (e.g. RegistrationStore, Warnings) that the wrapper does not implement.
func UnwrapIndexedDB(db indexeddb.IndexedDB) indexeddb.IndexedDB {
	if w, ok := db.(*instrumentedIndexedDB); ok {
		return w.inner
	}
	return db
}

// IndexedDBName returns the logical IndexedDB resource name when db is instrumented.
func IndexedDBName(db indexeddb.IndexedDB) string {
	if w, ok := db.(*instrumentedIndexedDB); ok {
		return w.db
	}
	return ""
}

// InstrumentObjectStore wraps an ObjectStore to record metrics with the provided labels.
func InstrumentObjectStore(store idb.ObjectStore, labels IndexedDBMetricLabels) idb.ObjectStore {
	return &instrumentedObjectStore{inner: store, labels: labels}
}

func (d *instrumentedIndexedDB) ObjectStore(name string) idb.ObjectStore {
	return InstrumentObjectStore(d.inner.ObjectStore(name), IndexedDBMetricLabels{
		DB:          d.db,
		ObjectStore: name,
	})
}

func (d *instrumentedIndexedDB) Transaction(ctx context.Context, stores []string, mode idb.TransactionMode, opts idb.TransactionOptions) (idb.Transaction, error) {
	return d.inner.Transaction(ctx, stores, mode, opts)
}

func (d *instrumentedIndexedDB) CreateObjectStore(ctx context.Context, name string, schema idb.ObjectStoreSchema) (idb.ObjectStore, error) {
	return d.inner.CreateObjectStore(ctx, name, schema)
}

func (d *instrumentedIndexedDB) DeleteObjectStore(ctx context.Context, name string) error {
	return d.inner.DeleteObjectStore(ctx, name)
}

func (d *instrumentedIndexedDB) Ping(ctx context.Context) error {
	return d.inner.Ping(ctx)
}

func (d *instrumentedIndexedDB) Close() error {
	return d.inner.Close()
}

type instrumentedObjectStore struct {
	inner  idb.ObjectStore
	labels IndexedDBMetricLabels
}

func (s *instrumentedObjectStore) Get(ctx context.Context, id string) (idb.Record, error) {
	startedAt := time.Now()
	rec, err := s.inner.Get(ctx, id)
	RecordIndexedDBOperation(ctx, startedAt, s.labels, "Get", err)
	return rec, err
}

func (s *instrumentedObjectStore) GetKey(ctx context.Context, id string) (string, error) {
	startedAt := time.Now()
	key, err := s.inner.GetKey(ctx, id)
	RecordIndexedDBOperation(ctx, startedAt, s.labels, "GetKey", err)
	return key, err
}

func (s *instrumentedObjectStore) Add(ctx context.Context, record idb.Record) error {
	startedAt := time.Now()
	err := s.inner.Add(ctx, record)
	RecordIndexedDBOperation(ctx, startedAt, s.labels, "Add", err)
	return err
}

func (s *instrumentedObjectStore) Put(ctx context.Context, record idb.Record) error {
	startedAt := time.Now()
	err := s.inner.Put(ctx, record)
	RecordIndexedDBOperation(ctx, startedAt, s.labels, "Put", err)
	return err
}

func (s *instrumentedObjectStore) Delete(ctx context.Context, id string) error {
	startedAt := time.Now()
	err := s.inner.Delete(ctx, id)
	RecordIndexedDBOperation(ctx, startedAt, s.labels, "Delete", err)
	return err
}

func (s *instrumentedObjectStore) Clear(ctx context.Context) error {
	startedAt := time.Now()
	err := s.inner.Clear(ctx)
	RecordIndexedDBOperation(ctx, startedAt, s.labels, "Clear", err)
	return err
}

func (s *instrumentedObjectStore) GetAll(ctx context.Context, r *idb.KeyRange) ([]idb.Record, error) {
	startedAt := time.Now()
	recs, err := s.inner.GetAll(ctx, r)
	RecordIndexedDBOperation(ctx, startedAt, s.labels, "GetAll", err)
	return recs, err
}

func (s *instrumentedObjectStore) GetAllKeys(ctx context.Context, r *idb.KeyRange) ([]string, error) {
	startedAt := time.Now()
	keys, err := s.inner.GetAllKeys(ctx, r)
	RecordIndexedDBOperation(ctx, startedAt, s.labels, "GetAllKeys", err)
	return keys, err
}

func (s *instrumentedObjectStore) Count(ctx context.Context, r *idb.KeyRange) (int64, error) {
	startedAt := time.Now()
	n, err := s.inner.Count(ctx, r)
	RecordIndexedDBOperation(ctx, startedAt, s.labels, "Count", err)
	return n, err
}

func (s *instrumentedObjectStore) DeleteRange(ctx context.Context, r idb.KeyRange) (int64, error) {
	startedAt := time.Now()
	n, err := s.inner.DeleteRange(ctx, r)
	RecordIndexedDBOperation(ctx, startedAt, s.labels, "DeleteRange", err)
	return n, err
}

func (s *instrumentedObjectStore) Index(name string) idb.Index {
	labels := s.labels
	labels.IndexName = name
	return &instrumentedIndex{inner: s.inner.Index(name), labels: labels}
}

func (s *instrumentedObjectStore) OpenCursor(ctx context.Context, r *idb.KeyRange, dir idb.CursorDirection) (idb.Cursor, error) {
	startedAt := time.Now()
	c, err := s.inner.OpenCursor(ctx, r, dir)
	RecordIndexedDBOperation(ctx, startedAt, s.labels, "OpenCursor", err)
	return c, err
}

func (s *instrumentedObjectStore) OpenKeyCursor(ctx context.Context, r *idb.KeyRange, dir idb.CursorDirection) (idb.Cursor, error) {
	startedAt := time.Now()
	c, err := s.inner.OpenKeyCursor(ctx, r, dir)
	RecordIndexedDBOperation(ctx, startedAt, s.labels, "OpenKeyCursor", err)
	return c, err
}

type instrumentedIndex struct {
	inner  idb.Index
	labels IndexedDBMetricLabels
}

func (i *instrumentedIndex) Get(ctx context.Context, values ...any) (idb.Record, error) {
	startedAt := time.Now()
	rec, err := i.inner.Get(ctx, values...)
	RecordIndexedDBOperation(ctx, startedAt, i.labels, "Index.Get", err)
	return rec, err
}

func (i *instrumentedIndex) GetKey(ctx context.Context, values ...any) (string, error) {
	startedAt := time.Now()
	key, err := i.inner.GetKey(ctx, values...)
	RecordIndexedDBOperation(ctx, startedAt, i.labels, "Index.GetKey", err)
	return key, err
}

func (i *instrumentedIndex) GetAll(ctx context.Context, r *idb.KeyRange, values ...any) ([]idb.Record, error) {
	startedAt := time.Now()
	recs, err := i.inner.GetAll(ctx, r, values...)
	RecordIndexedDBOperation(ctx, startedAt, i.labels, "Index.GetAll", err)
	return recs, err
}

func (i *instrumentedIndex) GetAllKeys(ctx context.Context, r *idb.KeyRange, values ...any) ([]string, error) {
	startedAt := time.Now()
	keys, err := i.inner.GetAllKeys(ctx, r, values...)
	RecordIndexedDBOperation(ctx, startedAt, i.labels, "Index.GetAllKeys", err)
	return keys, err
}

func (i *instrumentedIndex) Count(ctx context.Context, r *idb.KeyRange, values ...any) (int64, error) {
	startedAt := time.Now()
	n, err := i.inner.Count(ctx, r, values...)
	RecordIndexedDBOperation(ctx, startedAt, i.labels, "Index.Count", err)
	return n, err
}

func (i *instrumentedIndex) Delete(ctx context.Context, values ...any) (int64, error) {
	startedAt := time.Now()
	n, err := i.inner.Delete(ctx, values...)
	RecordIndexedDBOperation(ctx, startedAt, i.labels, "Index.Delete", err)
	return n, err
}

func (i *instrumentedIndex) DeleteRange(ctx context.Context, r *idb.KeyRange, values ...any) (int64, error) {
	startedAt := time.Now()
	n, err := i.inner.DeleteRange(ctx, r, values...)
	RecordIndexedDBOperation(ctx, startedAt, i.labels, "Index.DeleteRange", err)
	return n, err
}

func (i *instrumentedIndex) OpenCursor(ctx context.Context, r *idb.KeyRange, dir idb.CursorDirection, values ...any) (idb.Cursor, error) {
	startedAt := time.Now()
	c, err := i.inner.OpenCursor(ctx, r, dir, values...)
	RecordIndexedDBOperation(ctx, startedAt, i.labels, "Index.OpenCursor", err)
	return c, err
}

func (i *instrumentedIndex) OpenKeyCursor(ctx context.Context, r *idb.KeyRange, dir idb.CursorDirection, values ...any) (idb.Cursor, error) {
	startedAt := time.Now()
	c, err := i.inner.OpenKeyCursor(ctx, r, dir, values...)
	RecordIndexedDBOperation(ctx, startedAt, i.labels, "Index.OpenKeyCursor", err)
	return c, err
}
