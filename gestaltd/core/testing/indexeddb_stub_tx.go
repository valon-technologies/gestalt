package coretesting

import (
	"context"
	"sync"

	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

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
