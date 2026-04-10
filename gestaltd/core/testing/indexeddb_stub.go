package coretesting

import (
	"context"
	"sync"

	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

type StubIndexedDB struct {
	mu     sync.RWMutex
	stores map[string]*stubObjectStore
	Err    error
}

func (s *StubIndexedDB) ObjectStore(name string) indexeddb.ObjectStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stores == nil {
		s.stores = make(map[string]*stubObjectStore)
	}
	if st, ok := s.stores[name]; ok {
		return st
	}
	st := &stubObjectStore{db: s, records: make(map[string]indexeddb.Record)}
	s.stores[name] = st
	return st
}

func (s *StubIndexedDB) CreateObjectStore(_ context.Context, name string, schema indexeddb.ObjectStoreSchema) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stores == nil {
		s.stores = make(map[string]*stubObjectStore)
	}
	if existing, ok := s.stores[name]; ok {
		existing.schema = schema
	} else {
		s.stores[name] = &stubObjectStore{db: s, records: make(map[string]indexeddb.Record), schema: schema}
	}
	return nil
}

func (s *StubIndexedDB) DeleteObjectStore(_ context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.stores, name)
	return nil
}

func (s *StubIndexedDB) Ping(context.Context) error { return s.Err }
func (s *StubIndexedDB) Close() error               { return nil }

type stubObjectStore struct {
	db      *StubIndexedDB
	mu      sync.RWMutex
	records map[string]indexeddb.Record
	schema  indexeddb.ObjectStoreSchema
}

func (o *stubObjectStore) Get(_ context.Context, id string) (indexeddb.Record, error) {
	if o.db.Err != nil {
		return nil, o.db.Err
	}
	o.mu.RLock()
	defer o.mu.RUnlock()
	r, ok := o.records[id]
	if !ok {
		return nil, indexeddb.ErrNotFound
	}
	return r, nil
}

func (o *stubObjectStore) GetKey(_ context.Context, id string) (string, error) {
	if o.db.Err != nil {
		return "", o.db.Err
	}
	o.mu.RLock()
	defer o.mu.RUnlock()
	if _, ok := o.records[id]; !ok {
		return "", indexeddb.ErrNotFound
	}
	return id, nil
}

func (o *stubObjectStore) Add(_ context.Context, record indexeddb.Record) error {
	if o.db.Err != nil {
		return o.db.Err
	}
	id, _ := record["id"].(string)
	o.mu.Lock()
	defer o.mu.Unlock()
	if _, ok := o.records[id]; ok {
		return indexeddb.ErrAlreadyExists
	}
	o.records[id] = record
	return nil
}

func (o *stubObjectStore) Put(_ context.Context, record indexeddb.Record) error {
	if o.db.Err != nil {
		return o.db.Err
	}
	id, _ := record["id"].(string)
	o.mu.Lock()
	defer o.mu.Unlock()
	o.records[id] = record
	return nil
}

func (o *stubObjectStore) Delete(_ context.Context, id string) error {
	if o.db.Err != nil {
		return o.db.Err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.records, id)
	return nil
}

func (o *stubObjectStore) Clear(_ context.Context) error {
	if o.db.Err != nil {
		return o.db.Err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.records = make(map[string]indexeddb.Record)
	return nil
}

func (o *stubObjectStore) GetAll(_ context.Context, _ *indexeddb.KeyRange) ([]indexeddb.Record, error) {
	if o.db.Err != nil {
		return nil, o.db.Err
	}
	o.mu.RLock()
	defer o.mu.RUnlock()
	out := make([]indexeddb.Record, 0, len(o.records))
	for _, r := range o.records {
		out = append(out, r)
	}
	return out, nil
}

func (o *stubObjectStore) GetAllKeys(_ context.Context, _ *indexeddb.KeyRange) ([]string, error) {
	if o.db.Err != nil {
		return nil, o.db.Err
	}
	o.mu.RLock()
	defer o.mu.RUnlock()
	out := make([]string, 0, len(o.records))
	for k := range o.records {
		out = append(out, k)
	}
	return out, nil
}

func (o *stubObjectStore) Count(_ context.Context, _ *indexeddb.KeyRange) (int64, error) {
	if o.db.Err != nil {
		return 0, o.db.Err
	}
	o.mu.RLock()
	defer o.mu.RUnlock()
	return int64(len(o.records)), nil
}

func (o *stubObjectStore) DeleteRange(_ context.Context, _ indexeddb.KeyRange) (int64, error) {
	if o.db.Err != nil {
		return 0, o.db.Err
	}
	return 0, nil
}

func (o *stubObjectStore) Index(name string) indexeddb.Index {
	return &stubIndex{store: o, name: name, schema: o.schema}
}

type stubIndex struct {
	store  *stubObjectStore
	name   string
	schema indexeddb.ObjectStoreSchema
}

func (idx *stubIndex) keyPath() []string {
	for _, is := range idx.schema.Indexes {
		if is.Name == idx.name {
			return is.KeyPath
		}
	}
	return nil
}

func (idx *stubIndex) matches(record indexeddb.Record, values []any) bool {
	kp := idx.keyPath()
	if kp == nil {
		return false
	}
	for i, field := range kp {
		if i >= len(values) {
			break
		}
		rv := record[field]
		if rv != values[i] {
			return false
		}
	}
	return true
}

func (idx *stubIndex) Get(ctx context.Context, values ...any) (indexeddb.Record, error) {
	if idx.store.db.Err != nil {
		return nil, idx.store.db.Err
	}
	records, err := idx.GetAll(ctx, nil, values...)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, indexeddb.ErrNotFound
	}
	return records[0], nil
}

func (idx *stubIndex) GetKey(ctx context.Context, values ...any) (string, error) {
	if idx.store.db.Err != nil {
		return "", idx.store.db.Err
	}
	rec, err := idx.Get(ctx, values...)
	if err != nil {
		return "", err
	}
	id, _ := rec["id"].(string)
	return id, nil
}

func (idx *stubIndex) GetAll(_ context.Context, _ *indexeddb.KeyRange, values ...any) ([]indexeddb.Record, error) {
	if idx.store.db.Err != nil {
		return nil, idx.store.db.Err
	}
	idx.store.mu.RLock()
	defer idx.store.mu.RUnlock()
	var out []indexeddb.Record
	for _, r := range idx.store.records {
		if idx.matches(r, values) {
			out = append(out, r)
		}
	}
	return out, nil
}

func (idx *stubIndex) GetAllKeys(ctx context.Context, r *indexeddb.KeyRange, values ...any) ([]string, error) {
	if idx.store.db.Err != nil {
		return nil, idx.store.db.Err
	}
	records, err := idx.GetAll(ctx, r, values...)
	if err != nil {
		return nil, err
	}
	keys := make([]string, len(records))
	for i, rec := range records {
		keys[i], _ = rec["id"].(string)
	}
	return keys, nil
}

func (idx *stubIndex) Count(ctx context.Context, r *indexeddb.KeyRange, values ...any) (int64, error) {
	if idx.store.db.Err != nil {
		return 0, idx.store.db.Err
	}
	records, err := idx.GetAll(ctx, r, values...)
	if err != nil {
		return 0, err
	}
	return int64(len(records)), nil
}

func (idx *stubIndex) Delete(_ context.Context, values ...any) (int64, error) {
	if idx.store.db.Err != nil {
		return 0, idx.store.db.Err
	}
	idx.store.mu.Lock()
	defer idx.store.mu.Unlock()
	var toDelete []string
	for id, r := range idx.store.records {
		if idx.matches(r, values) {
			toDelete = append(toDelete, id)
		}
	}
	for _, id := range toDelete {
		delete(idx.store.records, id)
	}
	return int64(len(toDelete)), nil
}

var _ indexeddb.IndexedDB = (*StubIndexedDB)(nil)
