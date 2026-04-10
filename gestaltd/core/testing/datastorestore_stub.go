package coretesting

import (
	"context"
	"sync"

	"github.com/valon-technologies/gestalt/server/core/datastore"
)

type StubObjectDatastore struct {
	mu     sync.RWMutex
	stores map[string]*stubObjectStore
}

func (s *StubObjectDatastore) ObjectStore(name string) datastore.ObjectStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stores == nil {
		s.stores = make(map[string]*stubObjectStore)
	}
	if st, ok := s.stores[name]; ok {
		return st
	}
	st := &stubObjectStore{records: make(map[string]datastore.Record)}
	s.stores[name] = st
	return st
}

func (s *StubObjectDatastore) CreateObjectStore(_ context.Context, name string, schema datastore.ObjectStoreSchema) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stores == nil {
		s.stores = make(map[string]*stubObjectStore)
	}
	if existing, ok := s.stores[name]; ok {
		existing.schema = schema
	} else {
		s.stores[name] = &stubObjectStore{records: make(map[string]datastore.Record), schema: schema}
	}
	return nil
}

func (s *StubObjectDatastore) DeleteObjectStore(_ context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.stores, name)
	return nil
}

func (s *StubObjectDatastore) Ping(context.Context) error { return nil }
func (s *StubObjectDatastore) Close() error               { return nil }

type stubObjectStore struct {
	mu      sync.RWMutex
	records map[string]datastore.Record
	schema  datastore.ObjectStoreSchema
}

func (o *stubObjectStore) Get(_ context.Context, id string) (datastore.Record, error) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	r, ok := o.records[id]
	if !ok {
		return nil, datastore.ErrNotFound
	}
	return r, nil
}

func (o *stubObjectStore) GetKey(_ context.Context, id string) (string, error) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	if _, ok := o.records[id]; !ok {
		return "", datastore.ErrNotFound
	}
	return id, nil
}

func (o *stubObjectStore) Add(_ context.Context, record datastore.Record) error {
	id, _ := record["id"].(string)
	o.mu.Lock()
	defer o.mu.Unlock()
	if _, ok := o.records[id]; ok {
		return datastore.ErrAlreadyExists
	}
	o.records[id] = record
	return nil
}

func (o *stubObjectStore) Put(_ context.Context, record datastore.Record) error {
	id, _ := record["id"].(string)
	o.mu.Lock()
	defer o.mu.Unlock()
	o.records[id] = record
	return nil
}

func (o *stubObjectStore) Delete(_ context.Context, id string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.records, id)
	return nil
}

func (o *stubObjectStore) Clear(context.Context) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.records = make(map[string]datastore.Record)
	return nil
}

func (o *stubObjectStore) GetAll(_ context.Context, _ *datastore.KeyRange) ([]datastore.Record, error) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	out := make([]datastore.Record, 0, len(o.records))
	for _, r := range o.records {
		out = append(out, r)
	}
	return out, nil
}

func (o *stubObjectStore) GetAllKeys(_ context.Context, _ *datastore.KeyRange) ([]string, error) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	out := make([]string, 0, len(o.records))
	for k := range o.records {
		out = append(out, k)
	}
	return out, nil
}

func (o *stubObjectStore) Count(_ context.Context, _ *datastore.KeyRange) (int64, error) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return int64(len(o.records)), nil
}

func (o *stubObjectStore) DeleteRange(_ context.Context, _ datastore.KeyRange) (int64, error) {
	return 0, nil
}

func (o *stubObjectStore) Index(name string) datastore.Index {
	return &stubIndex{store: o, name: name, schema: o.schema}
}

type stubIndex struct {
	store  *stubObjectStore
	name   string
	schema datastore.ObjectStoreSchema
}

func (idx *stubIndex) keyPath() []string {
	for _, is := range idx.schema.Indexes {
		if is.Name == idx.name {
			return is.KeyPath
		}
	}
	return nil
}

func (idx *stubIndex) matches(record datastore.Record, values []any) bool {
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

func (idx *stubIndex) Get(ctx context.Context, values ...any) (datastore.Record, error) {
	records, err := idx.GetAll(ctx, nil, values...)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, datastore.ErrNotFound
	}
	return records[0], nil
}

func (idx *stubIndex) GetKey(ctx context.Context, values ...any) (string, error) {
	rec, err := idx.Get(ctx, values...)
	if err != nil {
		return "", err
	}
	id, _ := rec["id"].(string)
	return id, nil
}

func (idx *stubIndex) GetAll(_ context.Context, _ *datastore.KeyRange, values ...any) ([]datastore.Record, error) {
	idx.store.mu.RLock()
	defer idx.store.mu.RUnlock()
	var out []datastore.Record
	for _, r := range idx.store.records {
		if idx.matches(r, values) {
			out = append(out, r)
		}
	}
	return out, nil
}

func (idx *stubIndex) GetAllKeys(ctx context.Context, r *datastore.KeyRange, values ...any) ([]string, error) {
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

func (idx *stubIndex) Count(ctx context.Context, r *datastore.KeyRange, values ...any) (int64, error) {
	records, err := idx.GetAll(ctx, r, values...)
	if err != nil {
		return 0, err
	}
	return int64(len(records)), nil
}

func (idx *stubIndex) Delete(_ context.Context, values ...any) (int64, error) {
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

var _ datastore.Datastore = (*StubObjectDatastore)(nil)
