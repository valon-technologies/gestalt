package coretesting

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"sort"
	"sync"

	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

type StubIndexedDB struct {
	mu      sync.RWMutex
	txMu    sync.RWMutex
	name    string
	version uint64
	stores  map[string]*stubObjectStore
	Err     error
}

func (s *StubIndexedDB) Open(ctx context.Context, name string, opts indexeddb.OpenOptions) (indexeddb.Database, error) {
	s.mu.Lock()
	oldVersion := s.version
	newVersion := oldVersion
	if opts.Version != nil {
		newVersion = *opts.Version
	} else if newVersion == 0 {
		newVersion = 1
	}
	if oldVersion != 0 && newVersion < oldVersion {
		s.mu.Unlock()
		return nil, indexeddb.ErrInvalidTransaction
	}
	s.name = name
	s.mu.Unlock()

	if newVersion > oldVersion && opts.Upgrade != nil {
		if err := opts.Upgrade(ctx, stubUpgradeContext{db: s, oldVersion: oldVersion, newVersion: newVersion}); err != nil {
			return nil, err
		}
	}

	s.mu.Lock()
	s.name = name
	s.version = newVersion
	s.mu.Unlock()
	return s, nil
}

func (s *StubIndexedDB) OpenCurrent(_ context.Context, name string, _ indexeddb.OpenOptions) (indexeddb.Database, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.version == 0 || s.name != name {
		return nil, indexeddb.ErrNotFound
	}
	return s, nil
}

func (s *StubIndexedDB) DeleteDatabase(_ context.Context, name string, _ indexeddb.DeleteOptions) (indexeddb.DeleteDatabaseResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.name != name {
		return indexeddb.DeleteDatabaseResult{Name: name}, nil
	}
	oldVersion := s.version
	s.name = ""
	s.version = 0
	s.stores = make(map[string]*stubObjectStore)
	return indexeddb.DeleteDatabaseResult{Name: name, OldVersion: oldVersion}, nil
}

func (s *StubIndexedDB) Databases(context.Context) ([]indexeddb.DatabaseInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.version == 0 {
		return nil, nil
	}
	return []indexeddb.DatabaseInfo{{Name: s.name, Version: s.version}}, nil
}

func (s *StubIndexedDB) CompareKeys(first any, second any) (int, error) {
	if reflect.DeepEqual(first, second) {
		return 0, nil
	}
	left := fmt.Sprint(first)
	right := fmt.Sprint(second)
	if left < right {
		return -1, nil
	}
	if left > right {
		return 1, nil
	}
	return 0, nil
}

func (s *StubIndexedDB) Name() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.name
}

func (s *StubIndexedDB) Version() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.version
}

func (s *StubIndexedDB) ObjectStoreNames(context.Context) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.stores))
	for name := range s.stores {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
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

type stubUpgradeContext struct {
	db         *StubIndexedDB
	oldVersion uint64
	newVersion uint64
}

func (u stubUpgradeContext) OldVersion() uint64 { return u.oldVersion }

func (u stubUpgradeContext) NewVersion() uint64 { return u.newVersion }

func (u stubUpgradeContext) Database() indexeddb.UpgradeDatabase {
	return stubUpgradeDatabase{db: u.db}
}

func (u stubUpgradeContext) ObjectStoreNames(ctx context.Context) ([]string, error) {
	return u.db.ObjectStoreNames(ctx)
}

func (u stubUpgradeContext) CreateObjectStore(ctx context.Context, name string, schema indexeddb.ObjectStoreSchema) error {
	return u.db.CreateObjectStore(ctx, name, schema)
}

func (u stubUpgradeContext) DeleteObjectStore(ctx context.Context, name string) error {
	return u.db.DeleteObjectStore(ctx, name)
}

func (u stubUpgradeContext) CreateIndex(_ context.Context, store string, index indexeddb.IndexSchema) error {
	u.db.mu.Lock()
	defer u.db.mu.Unlock()
	if u.db.stores == nil {
		u.db.stores = make(map[string]*stubObjectStore)
	}
	st := u.db.stores[store]
	if st == nil {
		st = &stubObjectStore{db: u.db, records: make(map[string]indexeddb.Record)}
		u.db.stores[store] = st
	}
	for i, existing := range st.schema.Indexes {
		if existing.Name == index.Name {
			st.schema.Indexes[i] = index
			return nil
		}
	}
	st.schema.Indexes = append(st.schema.Indexes, index)
	return nil
}

func (u stubUpgradeContext) DeleteIndex(_ context.Context, store string, name string) error {
	u.db.mu.Lock()
	defer u.db.mu.Unlock()
	st := u.db.stores[store]
	if st == nil {
		return nil
	}
	indexes := st.schema.Indexes[:0]
	for _, existing := range st.schema.Indexes {
		if existing.Name != name {
			indexes = append(indexes, existing)
		}
	}
	st.schema.Indexes = indexes
	return nil
}

type stubUpgradeDatabase struct {
	db *StubIndexedDB
}

func (d stubUpgradeDatabase) Name() string { return d.db.Name() }

func (d stubUpgradeDatabase) ObjectStoreNames(ctx context.Context) ([]string, error) {
	return d.db.ObjectStoreNames(ctx)
}

func (d stubUpgradeDatabase) CreateObjectStore(ctx context.Context, name string, schema indexeddb.ObjectStoreSchema) error {
	return d.db.CreateObjectStore(ctx, name, schema)
}

func (d stubUpgradeDatabase) DeleteObjectStore(ctx context.Context, name string) error {
	return d.db.DeleteObjectStore(ctx, name)
}

func (d stubUpgradeDatabase) CreateIndex(ctx context.Context, store string, index indexeddb.IndexSchema) error {
	return stubUpgradeContext{db: d.db}.CreateIndex(ctx, store, index)
}

func (d stubUpgradeDatabase) DeleteIndex(ctx context.Context, store string, name string) error {
	return stubUpgradeContext{db: d.db}.DeleteIndex(ctx, store, name)
}

type stubObjectStore struct {
	db      *StubIndexedDB
	mu      sync.RWMutex
	records map[string]indexeddb.Record
	schema  indexeddb.ObjectStoreSchema
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

func (o *stubObjectStore) clone(db *StubIndexedDB) *stubObjectStore {
	o.mu.RLock()
	defer o.mu.RUnlock()
	records := make(map[string]indexeddb.Record, len(o.records))
	for id, record := range o.records {
		records[id] = cloneRecord(record)
	}
	return &stubObjectStore{
		db:      db,
		records: records,
		schema:  o.schema,
	}
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

func (o *stubObjectStore) readSchedule() func() {
	if o.db == nil {
		return func() {}
	}
	o.db.txMu.RLock()
	return o.db.txMu.RUnlock
}

func (o *stubObjectStore) writeSchedule() func() {
	if o.db == nil {
		return func() {}
	}
	o.db.txMu.Lock()
	return o.db.txMu.Unlock
}

func (o *stubObjectStore) Get(_ context.Context, id string) (indexeddb.Record, error) {
	if o.db.Err != nil {
		return nil, o.db.Err
	}
	done := o.readSchedule()
	defer done()
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
	done := o.readSchedule()
	defer done()
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
	done := o.writeSchedule()
	defer done()
	id, _ := record["id"].(string)
	o.mu.Lock()
	defer o.mu.Unlock()
	if _, ok := o.records[id]; ok {
		return indexeddb.ErrAlreadyExists
	}
	if o.hasUniqueConflict(record, nil) {
		return indexeddb.ErrAlreadyExists
	}
	o.records[id] = record
	return nil
}

func (o *stubObjectStore) Put(_ context.Context, record indexeddb.Record) error {
	if o.db.Err != nil {
		return o.db.Err
	}
	done := o.writeSchedule()
	defer done()
	id, _ := record["id"].(string)
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.hasUniqueConflict(record, &id) {
		return indexeddb.ErrAlreadyExists
	}
	o.records[id] = record
	return nil
}

func (o *stubObjectStore) hasUniqueConflict(record indexeddb.Record, ignoreID *string) bool {
	for _, idx := range o.schema.Indexes {
		if !idx.Unique {
			continue
		}
		for id, existing := range o.records {
			if ignoreID != nil && id == *ignoreID {
				continue
			}
			match := true
			for _, field := range idx.KeyPath {
				if existing[field] != record[field] {
					match = false
					break
				}
			}
			if match {
				return true
			}
		}
	}
	return false
}

func (o *stubObjectStore) Delete(_ context.Context, id string) error {
	if o.db.Err != nil {
		return o.db.Err
	}
	done := o.writeSchedule()
	defer done()
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.records, id)
	return nil
}

func (o *stubObjectStore) Clear(_ context.Context) error {
	if o.db.Err != nil {
		return o.db.Err
	}
	done := o.writeSchedule()
	defer done()
	o.mu.Lock()
	defer o.mu.Unlock()
	o.records = make(map[string]indexeddb.Record)
	return nil
}

func (o *stubObjectStore) GetAll(_ context.Context, r *indexeddb.KeyRange) ([]indexeddb.Record, error) {
	if o.db.Err != nil {
		return nil, o.db.Err
	}
	done := o.readSchedule()
	defer done()
	c := o.newCursor(indexeddb.CursorNext, false)
	c.applyKeyRange(r)
	out := make([]indexeddb.Record, 0, len(c.keys))
	for _, key := range c.keys {
		out = append(out, c.snapshot[key])
	}
	return out, nil
}

func (o *stubObjectStore) GetAllKeys(_ context.Context, r *indexeddb.KeyRange) ([]string, error) {
	if o.db.Err != nil {
		return nil, o.db.Err
	}
	done := o.readSchedule()
	defer done()
	c := o.newCursor(indexeddb.CursorNext, true)
	c.applyKeyRange(r)
	return append([]string(nil), c.keys...), nil
}

func (o *stubObjectStore) Count(_ context.Context, r *indexeddb.KeyRange) (int64, error) {
	if o.db.Err != nil {
		return 0, o.db.Err
	}
	done := o.readSchedule()
	defer done()
	c := o.newCursor(indexeddb.CursorNext, true)
	c.applyKeyRange(r)
	return int64(len(c.keys)), nil
}

func (o *stubObjectStore) DeleteRange(_ context.Context, r indexeddb.KeyRange) (int64, error) {
	if o.db.Err != nil {
		return 0, o.db.Err
	}
	done := o.writeSchedule()
	defer done()
	c := o.newCursor(indexeddb.CursorNext, true)
	c.applyKeyRange(&r)
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, key := range c.keys {
		delete(o.records, key)
	}
	return int64(len(c.keys)), nil
}

func (o *stubObjectStore) Index(name string) indexeddb.Index {
	return &stubIndex{store: o, name: name, schema: o.schema}
}

func (o *stubObjectStore) OpenCursor(_ context.Context, r *indexeddb.KeyRange, dir indexeddb.CursorDirection) (indexeddb.Cursor, error) {
	if o.db.Err != nil {
		return nil, o.db.Err
	}
	done := o.readSchedule()
	defer done()
	c := o.newCursor(dir, false)
	c.applyKeyRange(r)
	return c, nil
}

func (o *stubObjectStore) OpenKeyCursor(_ context.Context, r *indexeddb.KeyRange, dir indexeddb.CursorDirection) (indexeddb.Cursor, error) {
	if o.db.Err != nil {
		return nil, o.db.Err
	}
	done := o.readSchedule()
	defer done()
	c := o.newCursor(dir, true)
	c.applyKeyRange(r)
	return c, nil
}

type stubTransaction struct {
	db     *StubIndexedDB
	mode   indexeddb.TransactionMode
	mu     sync.Mutex
	done   bool
	err    error
	stores map[string]*stubObjectStore
}

func (tx *stubTransaction) ObjectStore(name string) indexeddb.TransactionObjectStore {
	store := tx.stores[name]
	if store == nil {
		return transactionMissingObjectStore{tx: tx}
	}
	return &stubTransactionObjectStore{tx: tx, store: store}
}

func (tx *stubTransaction) Commit(context.Context) error {
	if err := tx.finish(true); err != nil {
		return err
	}
	return nil
}

func (tx *stubTransaction) Abort(context.Context) error {
	if err := tx.finish(false); err != nil {
		return err
	}
	return nil
}

func (tx *stubTransaction) finish(commit bool) error {
	tx.mu.Lock()
	if tx.done {
		err := tx.err
		tx.mu.Unlock()
		if err != nil {
			return err
		}
		return indexeddb.ErrTransactionDone
	}
	tx.done = true
	tx.mu.Unlock()

	if commit && tx.mode == indexeddb.TransactionReadwrite {
		tx.db.mu.RLock()
		for name, clone := range tx.stores {
			original := tx.db.stores[name]
			if original == nil {
				continue
			}
			clone.mu.RLock()
			records := make(map[string]indexeddb.Record, len(clone.records))
			for id, record := range clone.records {
				records[id] = cloneRecord(record)
			}
			schema := clone.schema
			clone.mu.RUnlock()

			original.mu.Lock()
			original.records = records
			original.schema = schema
			original.mu.Unlock()
		}
		tx.db.mu.RUnlock()
	}
	tx.unlockSchedule()
	return nil
}

func (tx *stubTransaction) unlockSchedule() {
	if tx.mode == indexeddb.TransactionReadwrite {
		tx.db.txMu.Unlock()
	} else {
		tx.db.txMu.RUnlock()
	}
}

func (tx *stubTransaction) ensureActive(write bool) error {
	tx.mu.Lock()
	if tx.done {
		err := tx.err
		tx.mu.Unlock()
		if err != nil {
			return err
		}
		return indexeddb.ErrTransactionDone
	}
	if write && tx.mode == indexeddb.TransactionReadonly {
		tx.done = true
		tx.err = indexeddb.ErrReadOnly
		tx.mu.Unlock()
		tx.unlockSchedule()
		return indexeddb.ErrReadOnly
	}
	tx.mu.Unlock()
	return nil
}

func (tx *stubTransaction) abortWithError(err error) error {
	if err == nil {
		return nil
	}
	tx.mu.Lock()
	if tx.done {
		existing := tx.err
		tx.mu.Unlock()
		if existing != nil {
			return existing
		}
		return indexeddb.ErrTransactionDone
	}
	tx.done = true
	tx.err = err
	tx.mu.Unlock()
	tx.unlockSchedule()
	return err
}

type stubTransactionObjectStore struct {
	tx    *stubTransaction
	store *stubObjectStore
}

func (s *stubTransactionObjectStore) Get(ctx context.Context, id string) (indexeddb.Record, error) {
	if err := s.tx.ensureActive(false); err != nil {
		return nil, err
	}
	record, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, s.tx.abortWithError(err)
	}
	return record, nil
}

func (s *stubTransactionObjectStore) GetKey(ctx context.Context, id string) (string, error) {
	if err := s.tx.ensureActive(false); err != nil {
		return "", err
	}
	key, err := s.store.GetKey(ctx, id)
	if err != nil {
		return "", s.tx.abortWithError(err)
	}
	return key, nil
}

func (s *stubTransactionObjectStore) Add(ctx context.Context, record indexeddb.Record) error {
	if err := s.tx.ensureActive(true); err != nil {
		return err
	}
	return s.tx.abortWithError(s.store.Add(ctx, record))
}

func (s *stubTransactionObjectStore) Put(ctx context.Context, record indexeddb.Record) error {
	if err := s.tx.ensureActive(true); err != nil {
		return err
	}
	return s.tx.abortWithError(s.store.Put(ctx, record))
}

func (s *stubTransactionObjectStore) Delete(ctx context.Context, id string) error {
	if err := s.tx.ensureActive(true); err != nil {
		return err
	}
	return s.tx.abortWithError(s.store.Delete(ctx, id))
}

func (s *stubTransactionObjectStore) Clear(ctx context.Context) error {
	if err := s.tx.ensureActive(true); err != nil {
		return err
	}
	return s.tx.abortWithError(s.store.Clear(ctx))
}

func (s *stubTransactionObjectStore) GetAll(ctx context.Context, r *indexeddb.KeyRange) ([]indexeddb.Record, error) {
	if err := s.tx.ensureActive(false); err != nil {
		return nil, err
	}
	records, err := s.store.GetAll(ctx, r)
	if err != nil {
		return nil, s.tx.abortWithError(err)
	}
	return records, nil
}

func (s *stubTransactionObjectStore) GetAllKeys(ctx context.Context, r *indexeddb.KeyRange) ([]string, error) {
	if err := s.tx.ensureActive(false); err != nil {
		return nil, err
	}
	keys, err := s.store.GetAllKeys(ctx, r)
	if err != nil {
		return nil, s.tx.abortWithError(err)
	}
	return keys, nil
}

func (s *stubTransactionObjectStore) Count(ctx context.Context, r *indexeddb.KeyRange) (int64, error) {
	if err := s.tx.ensureActive(false); err != nil {
		return 0, err
	}
	count, err := s.store.Count(ctx, r)
	if err != nil {
		return 0, s.tx.abortWithError(err)
	}
	return count, nil
}

func (s *stubTransactionObjectStore) DeleteRange(ctx context.Context, r indexeddb.KeyRange) (int64, error) {
	if err := s.tx.ensureActive(true); err != nil {
		return 0, err
	}
	deleted, err := s.store.DeleteRange(ctx, r)
	if err != nil {
		return 0, s.tx.abortWithError(err)
	}
	return deleted, nil
}

func (s *stubTransactionObjectStore) Index(name string) indexeddb.TransactionIndex {
	return &stubTransactionIndex{tx: s.tx, index: s.store.Index(name)}
}

type stubTransactionIndex struct {
	tx    *stubTransaction
	index indexeddb.Index
}

func (i *stubTransactionIndex) Get(ctx context.Context, values ...any) (indexeddb.Record, error) {
	if err := i.tx.ensureActive(false); err != nil {
		return nil, err
	}
	record, err := i.index.Get(ctx, values...)
	if err != nil {
		return nil, i.tx.abortWithError(err)
	}
	return record, nil
}

func (i *stubTransactionIndex) GetKey(ctx context.Context, values ...any) (string, error) {
	if err := i.tx.ensureActive(false); err != nil {
		return "", err
	}
	key, err := i.index.GetKey(ctx, values...)
	if err != nil {
		return "", i.tx.abortWithError(err)
	}
	return key, nil
}

func (i *stubTransactionIndex) GetAll(ctx context.Context, r *indexeddb.KeyRange, values ...any) ([]indexeddb.Record, error) {
	if err := i.tx.ensureActive(false); err != nil {
		return nil, err
	}
	records, err := i.index.GetAll(ctx, r, values...)
	if err != nil {
		return nil, i.tx.abortWithError(err)
	}
	return records, nil
}

func (i *stubTransactionIndex) GetAllKeys(ctx context.Context, r *indexeddb.KeyRange, values ...any) ([]string, error) {
	if err := i.tx.ensureActive(false); err != nil {
		return nil, err
	}
	keys, err := i.index.GetAllKeys(ctx, r, values...)
	if err != nil {
		return nil, i.tx.abortWithError(err)
	}
	return keys, nil
}

func (i *stubTransactionIndex) Count(ctx context.Context, r *indexeddb.KeyRange, values ...any) (int64, error) {
	if err := i.tx.ensureActive(false); err != nil {
		return 0, err
	}
	count, err := i.index.Count(ctx, r, values...)
	if err != nil {
		return 0, i.tx.abortWithError(err)
	}
	return count, nil
}

func (i *stubTransactionIndex) Delete(ctx context.Context, values ...any) (int64, error) {
	return i.DeleteRange(ctx, nil, values...)
}

func (i *stubTransactionIndex) DeleteRange(ctx context.Context, r *indexeddb.KeyRange, values ...any) (int64, error) {
	if err := i.tx.ensureActive(true); err != nil {
		return 0, err
	}
	deleted, err := i.index.DeleteRange(ctx, r, values...)
	if err != nil {
		return 0, i.tx.abortWithError(err)
	}
	return deleted, nil
}

type transactionMissingObjectStore struct {
	tx *stubTransaction
}

func (s transactionMissingObjectStore) Get(context.Context, string) (indexeddb.Record, error) {
	return nil, s.tx.abortWithError(indexeddb.ErrNotFound)
}

func (s transactionMissingObjectStore) GetKey(context.Context, string) (string, error) {
	return "", s.tx.abortWithError(indexeddb.ErrNotFound)
}

func (s transactionMissingObjectStore) Add(context.Context, indexeddb.Record) error {
	return s.tx.abortWithError(indexeddb.ErrNotFound)
}

func (s transactionMissingObjectStore) Put(context.Context, indexeddb.Record) error {
	return s.tx.abortWithError(indexeddb.ErrNotFound)
}

func (s transactionMissingObjectStore) Delete(context.Context, string) error {
	return s.tx.abortWithError(indexeddb.ErrNotFound)
}

func (s transactionMissingObjectStore) Clear(context.Context) error {
	return s.tx.abortWithError(indexeddb.ErrNotFound)
}

func (s transactionMissingObjectStore) GetAll(context.Context, *indexeddb.KeyRange) ([]indexeddb.Record, error) {
	return nil, s.tx.abortWithError(indexeddb.ErrNotFound)
}

func (s transactionMissingObjectStore) GetAllKeys(context.Context, *indexeddb.KeyRange) ([]string, error) {
	return nil, s.tx.abortWithError(indexeddb.ErrNotFound)
}

func (s transactionMissingObjectStore) Count(context.Context, *indexeddb.KeyRange) (int64, error) {
	return 0, s.tx.abortWithError(indexeddb.ErrNotFound)
}

func (s transactionMissingObjectStore) DeleteRange(context.Context, indexeddb.KeyRange) (int64, error) {
	return 0, s.tx.abortWithError(indexeddb.ErrNotFound)
}

func (s transactionMissingObjectStore) Index(string) indexeddb.TransactionIndex {
	return transactionMissingIndex(s)
}

type transactionMissingIndex struct {
	tx *stubTransaction
}

func (i transactionMissingIndex) Get(context.Context, ...any) (indexeddb.Record, error) {
	return nil, i.tx.abortWithError(indexeddb.ErrNotFound)
}

func (i transactionMissingIndex) GetKey(context.Context, ...any) (string, error) {
	return "", i.tx.abortWithError(indexeddb.ErrNotFound)
}

func (i transactionMissingIndex) GetAll(context.Context, *indexeddb.KeyRange, ...any) ([]indexeddb.Record, error) {
	return nil, i.tx.abortWithError(indexeddb.ErrNotFound)
}

func (i transactionMissingIndex) GetAllKeys(context.Context, *indexeddb.KeyRange, ...any) ([]string, error) {
	return nil, i.tx.abortWithError(indexeddb.ErrNotFound)
}

func (i transactionMissingIndex) Count(context.Context, *indexeddb.KeyRange, ...any) (int64, error) {
	return 0, i.tx.abortWithError(indexeddb.ErrNotFound)
}

func (i transactionMissingIndex) Delete(context.Context, ...any) (int64, error) {
	return 0, i.tx.abortWithError(indexeddb.ErrNotFound)
}

func (i transactionMissingIndex) DeleteRange(context.Context, *indexeddb.KeyRange, ...any) (int64, error) {
	return 0, i.tx.abortWithError(indexeddb.ErrNotFound)
}

func (o *stubObjectStore) newCursor(dir indexeddb.CursorDirection, keysOnly bool) *stubCursor {
	o.mu.RLock()
	keys := make([]string, 0, len(o.records))
	snapshot := make(map[string]indexeddb.Record, len(o.records))
	for k, r := range o.records {
		keys = append(keys, k)
		snapshot[k] = r
	}
	o.mu.RUnlock()

	sort.Strings(keys)
	if dir == indexeddb.CursorPrev || dir == indexeddb.CursorPrevUnique {
		sort.Sort(sort.Reverse(sort.StringSlice(keys)))
	}

	reverse := dir == indexeddb.CursorPrev || dir == indexeddb.CursorPrevUnique
	unique := dir == indexeddb.CursorNextUnique || dir == indexeddb.CursorPrevUnique
	return &stubCursor{
		store:    o,
		keys:     keys,
		snapshot: snapshot,
		pos:      -1,
		keysOnly: keysOnly,
		reverse:  reverse,
		unique:   unique,
	}
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

func (idx *stubIndex) newCursor(dir indexeddb.CursorDirection, r *indexeddb.KeyRange, keysOnly bool, values ...any) *stubCursor {
	c := idx.store.newCursor(dir, keysOnly)
	c.filterIndex = idx
	c.filterValues = values
	c.applyIndexFilter()
	c.buildIndexKeys()
	c.applyKeyRange(r)
	return c
}

func (idx *stubIndex) GetAll(_ context.Context, r *indexeddb.KeyRange, values ...any) ([]indexeddb.Record, error) {
	if idx.store.db.Err != nil {
		return nil, idx.store.db.Err
	}
	done := idx.store.readSchedule()
	defer done()
	c := idx.newCursor(indexeddb.CursorNext, r, false, values...)
	out := make([]indexeddb.Record, 0, len(c.keys))
	for _, key := range c.keys {
		out = append(out, c.snapshot[key])
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
	done := idx.store.readSchedule()
	defer done()
	c := idx.newCursor(indexeddb.CursorNext, r, true, values...)
	return int64(len(c.keys)), nil
}

func (idx *stubIndex) Delete(ctx context.Context, values ...any) (int64, error) {
	return idx.DeleteRange(ctx, nil, values...)
}

func (idx *stubIndex) DeleteRange(_ context.Context, r *indexeddb.KeyRange, values ...any) (int64, error) {
	if idx.store.db.Err != nil {
		return 0, idx.store.db.Err
	}
	done := idx.store.writeSchedule()
	defer done()
	c := idx.newCursor(indexeddb.CursorNext, r, true, values...)
	idx.store.mu.Lock()
	defer idx.store.mu.Unlock()
	for _, id := range c.keys {
		delete(idx.store.records, id)
	}
	return int64(len(c.keys)), nil
}

func (idx *stubIndex) OpenCursor(_ context.Context, r *indexeddb.KeyRange, dir indexeddb.CursorDirection, values ...any) (indexeddb.Cursor, error) {
	if idx.store.db.Err != nil {
		return nil, idx.store.db.Err
	}
	done := idx.store.readSchedule()
	defer done()
	return idx.newCursor(dir, r, false, values...), nil
}

func (idx *stubIndex) OpenKeyCursor(_ context.Context, r *indexeddb.KeyRange, dir indexeddb.CursorDirection, values ...any) (indexeddb.Cursor, error) {
	if idx.store.db.Err != nil {
		return nil, idx.store.db.Err
	}
	done := idx.store.readSchedule()
	defer done()
	return idx.newCursor(dir, r, true, values...), nil
}

type stubCursor struct {
	store        *stubObjectStore
	keys         []string
	indexKeys    []any
	snapshot     map[string]indexeddb.Record
	pos          int
	keysOnly     bool
	reverse      bool
	unique       bool
	err          error
	filterIndex  *stubIndex
	filterValues []any
}

func (c *stubCursor) buildIndexKeys() {
	if c.filterIndex == nil {
		return
	}
	kp := c.filterIndex.keyPath()
	if kp == nil {
		return
	}
	c.indexKeys = make([]any, len(c.keys))
	for i, k := range c.keys {
		rec := c.snapshot[k]
		if len(kp) == 1 {
			c.indexKeys[i] = []any{rec[kp[0]]}
		} else {
			vals := make([]any, len(kp))
			for j, field := range kp {
				vals[j] = rec[field]
			}
			c.indexKeys[i] = vals
		}
	}
	sort.Sort(&indexKeySorter{keys: c.keys, indexKeys: c.indexKeys, reverse: c.reverse})
}

type indexKeySorter struct {
	keys      []string
	indexKeys []any
	reverse   bool
}

func (s *indexKeySorter) Len() int { return len(s.keys) }

func (s *indexKeySorter) Swap(i, j int) {
	s.keys[i], s.keys[j] = s.keys[j], s.keys[i]
	s.indexKeys[i], s.indexKeys[j] = s.indexKeys[j], s.indexKeys[i]
}

func (s *indexKeySorter) Less(i, j int) bool {
	cmp := compareIndexKeys(s.indexKeys[i], s.indexKeys[j])
	if cmp == 0 {
		cmp = compareIndexKeys(s.keys[i], s.keys[j])
	}
	if s.reverse {
		return cmp > 0
	}
	return cmp < 0
}

func compareIndexKeys(a, b any) int {
	switch av := a.(type) {
	case []any:
		if bv, ok := b.([]any); ok {
			for i := range av {
				if i >= len(bv) {
					return 1
				}
				if cmp := compareIndexKeys(av[i], bv[i]); cmp != 0 {
					return cmp
				}
			}
			if len(av) < len(bv) {
				return -1
			}
			return 0
		}
	case string:
		if bv, ok := b.(string); ok {
			if av < bv {
				return -1
			}
			if av > bv {
				return 1
			}
			return 0
		}
	case int:
		if bv, ok := b.(int); ok {
			if av < bv {
				return -1
			}
			if av > bv {
				return 1
			}
			return 0
		}
	case int64:
		if bv, ok := b.(int64); ok {
			if av < bv {
				return -1
			}
			if av > bv {
				return 1
			}
			return 0
		}
	case float64:
		if bv, ok := b.(float64); ok {
			if av < bv {
				return -1
			}
			if av > bv {
				return 1
			}
			return 0
		}
	case []byte:
		if bv, ok := b.([]byte); ok {
			return bytes.Compare(av, bv)
		}
	}
	// Coerce numeric types for cross-type comparison (e.g. int vs int64 after gRPC round-trip).
	af, aOk := toFloat64(a)
	bf, bOk := toFloat64(b)
	if aOk && bOk {
		if af < bf {
			return -1
		}
		if af > bf {
			return 1
		}
		return 0
	}
	as, bs := fmt.Sprint(a), fmt.Sprint(b)
	if as < bs {
		return -1
	}
	if as > bs {
		return 1
	}
	return 0
}

func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	default:
		return 0, false
	}
}

func (c *stubCursor) applyKeyRange(r *indexeddb.KeyRange) {
	if r == nil {
		return
	}
	lower, upper := r.Lower, r.Upper
	if c.indexKeys != nil {
		lower = normalizeIndexRangeBound(lower)
		upper = normalizeIndexRangeBound(upper)
	}
	filtered := make([]string, 0, len(c.keys))
	var filteredIdx []any
	for i, k := range c.keys {
		var cur any = k
		if c.indexKeys != nil {
			cur = c.indexKeys[i]
		}
		if lower != nil {
			cmp := compareIndexKeys(cur, lower)
			if r.LowerOpen && cmp <= 0 {
				continue
			}
			if !r.LowerOpen && cmp < 0 {
				continue
			}
		}
		if upper != nil {
			cmp := compareIndexKeys(cur, upper)
			if r.UpperOpen && cmp >= 0 {
				continue
			}
			if !r.UpperOpen && cmp > 0 {
				continue
			}
		}
		filtered = append(filtered, k)
		if c.indexKeys != nil {
			filteredIdx = append(filteredIdx, c.indexKeys[i])
		}
	}
	c.keys = filtered
	if c.indexKeys != nil {
		c.indexKeys = filteredIdx
	}
}

func normalizeIndexRangeBound(bound any) any {
	if bound == nil {
		return nil
	}
	if _, ok := bound.([]any); ok {
		return bound
	}
	return []any{bound}
}

func (c *stubCursor) applyIndexFilter() {
	if c.filterIndex == nil {
		return
	}
	filtered := c.keys[:0]
	for _, k := range c.keys {
		if rec, ok := c.snapshot[k]; ok && c.filterIndex.matches(rec, c.filterValues) {
			filtered = append(filtered, k)
		}
	}
	c.keys = filtered
}

func (c *stubCursor) Continue() bool {
	if c.err != nil {
		return false
	}
	if c.unique && c.indexKeys != nil && c.pos >= 0 && c.pos < len(c.indexKeys) {
		prev := c.indexKeys[c.pos]
		for c.pos++; c.pos < len(c.keys); c.pos++ {
			if compareIndexKeys(c.indexKeys[c.pos], prev) != 0 {
				return true
			}
		}
		return false
	}
	c.pos++
	return c.pos < len(c.keys)
}

func (c *stubCursor) ContinueToKey(key any) bool {
	if c.err != nil {
		return false
	}
	var prevKey any
	if c.unique && c.indexKeys != nil && c.pos >= 0 && c.pos < len(c.indexKeys) {
		prevKey = c.indexKeys[c.pos]
	}
	for c.pos++; c.pos < len(c.keys); c.pos++ {
		var cur any = c.keys[c.pos]
		if c.indexKeys != nil {
			cur = c.indexKeys[c.pos]
		}
		if c.unique && prevKey != nil && compareIndexKeys(cur, prevKey) == 0 {
			continue
		}
		cmp := compareIndexKeys(cur, key)
		if c.reverse {
			if cmp <= 0 {
				return true
			}
		} else {
			if cmp >= 0 {
				return true
			}
		}
	}
	return false
}

func (c *stubCursor) Advance(count int) bool {
	if count <= 0 {
		c.err = fmt.Errorf("advance count must be positive")
		return false
	}
	for i := 0; i <= count; i++ {
		if !c.Continue() {
			return false
		}
	}
	return true
}

func (c *stubCursor) Key() any {
	if c.pos < 0 || c.pos >= len(c.keys) {
		return nil
	}
	if c.indexKeys != nil {
		return c.indexKeys[c.pos]
	}
	return c.keys[c.pos]
}

func (c *stubCursor) PrimaryKey() string {
	if c.pos < 0 || c.pos >= len(c.keys) {
		return ""
	}
	return c.keys[c.pos]
}

func (c *stubCursor) Value() (indexeddb.Record, error) {
	if c.keysOnly {
		return nil, indexeddb.ErrKeysOnly
	}
	if c.pos < 0 || c.pos >= len(c.keys) {
		return nil, indexeddb.ErrNotFound
	}
	return c.snapshot[c.keys[c.pos]], nil
}

func (c *stubCursor) Delete() error {
	if c.pos < 0 || c.pos >= len(c.keys) {
		return indexeddb.ErrNotFound
	}
	c.store.mu.Lock()
	delete(c.store.records, c.keys[c.pos])
	c.store.mu.Unlock()
	return nil
}

func (c *stubCursor) Update(value indexeddb.Record) error {
	if c.pos < 0 || c.pos >= len(c.keys) {
		return indexeddb.ErrNotFound
	}
	curID := c.keys[c.pos]
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	if c.store.hasUniqueConflict(value, &curID) {
		return indexeddb.ErrAlreadyExists
	}
	c.store.records[curID] = value
	c.snapshot[curID] = value
	return nil
}

func (c *stubCursor) Err() error   { return c.err }
func (c *stubCursor) Close() error { return nil }

var _ indexeddb.IndexedDB = (*StubIndexedDB)(nil)
