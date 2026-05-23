package coretesting

import (
	"context"
	"sort"
	"sync"

	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

type StubIndexedDB struct {
	mu     sync.RWMutex
	txMu   sync.RWMutex
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

func (s *StubIndexedDB) Transaction(_ context.Context, stores []string, mode indexeddb.TransactionMode, _ indexeddb.TransactionOptions) (indexeddb.Transaction, error) {
	if len(stores) == 0 {
		return nil, indexeddb.ErrInvalidTransaction
	}
	if mode != indexeddb.TransactionReadonly && mode != indexeddb.TransactionReadwrite {
		return nil, indexeddb.ErrInvalidTransaction
	}

	scope := uniqueSortedStores(stores)
	if mode == indexeddb.TransactionReadwrite {
		s.txMu.Lock()
	} else {
		s.txMu.RLock()
	}

	tx := &stubTransaction{
		db:     s,
		mode:   mode,
		stores: make(map[string]*stubObjectStore, len(scope)),
	}
	cloneDB := &StubIndexedDB{Err: s.Err}

	s.mu.Lock()
	if s.stores == nil {
		s.stores = make(map[string]*stubObjectStore)
	}
	for _, name := range scope {
		store, ok := s.stores[name]
		if !ok {
			store = &stubObjectStore{db: s, records: make(map[string]indexeddb.Record)}
			s.stores[name] = store
		}
		tx.stores[name] = store.clone(cloneDB)
	}
	s.mu.Unlock()

	return tx, nil
}

func (s *StubIndexedDB) Ping(context.Context) error { return s.Err }
func (s *StubIndexedDB) Close() error               { return nil }

func (s *StubIndexedDB) HasObjectStore(name string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.stores == nil {
		return false
	}
	_, ok := s.stores[name]
	return ok
}

func uniqueSortedStores(stores []string) []string {
	seen := make(map[string]struct{}, len(stores))
	out := make([]string, 0, len(stores))
	for _, store := range stores {
		if _, ok := seen[store]; ok {
			continue
		}
		seen[store] = struct{}{}
		out = append(out, store)
	}
	sort.Strings(out)
	return out
}

func cloneRecord(record indexeddb.Record) indexeddb.Record {
	if record == nil {
		return nil
	}
	out := make(indexeddb.Record, len(record))
	for k, v := range record {
		out[k] = v
	}
	return out
}

var _ indexeddb.IndexedDB = (*StubIndexedDB)(nil)
