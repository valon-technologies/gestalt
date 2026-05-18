package bootstrap

import (
	"context"

	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

type indexedDBStoreAllowlistOptions struct {
	AllowedStores []string
}

func newIndexedDBStoreAllowlist(ds indexeddb.IndexedDB, opts indexedDBStoreAllowlistOptions) indexeddb.IndexedDB {
	if ds == nil || len(opts.AllowedStores) == 0 {
		return ds
	}
	allowed := make(map[string]struct{}, len(opts.AllowedStores))
	for _, store := range opts.AllowedStores {
		allowed[store] = struct{}{}
	}
	return &indexedDBStoreAllowlist{
		inner:   ds,
		allowed: allowed,
	}
}

type indexedDBStoreAllowlist struct {
	inner   indexeddb.IndexedDB
	allowed map[string]struct{}
}

func (d *indexedDBStoreAllowlist) checkStore(name string) error {
	if _, ok := d.allowed[name]; !ok {
		return indexeddb.ErrNotFound
	}
	return nil
}

func (d *indexedDBStoreAllowlist) ObjectStore(name string) indexeddb.ObjectStore {
	if err := d.checkStore(name); err != nil {
		return missingObjectStore{}
	}
	return d.inner.ObjectStore(name)
}

func (d *indexedDBStoreAllowlist) Transaction(ctx context.Context, stores []string, mode indexeddb.TransactionMode, opts indexeddb.TransactionOptions) (indexeddb.Transaction, error) {
	for _, store := range stores {
		if err := d.checkStore(store); err != nil {
			return nil, err
		}
	}
	tx, err := d.inner.Transaction(ctx, stores, mode, opts)
	if err != nil {
		return nil, err
	}
	return &indexedDBStoreAllowlistTransaction{allowlist: d, inner: tx}, nil
}

func (d *indexedDBStoreAllowlist) CreateObjectStore(ctx context.Context, name string, schema indexeddb.ObjectStoreSchema) error {
	if err := d.checkStore(name); err != nil {
		return err
	}
	return d.inner.CreateObjectStore(ctx, name, schema)
}

func (d *indexedDBStoreAllowlist) DeleteObjectStore(ctx context.Context, name string) error {
	if err := d.checkStore(name); err != nil {
		return err
	}
	return d.inner.DeleteObjectStore(ctx, name)
}

func (d *indexedDBStoreAllowlist) Ping(ctx context.Context) error {
	return d.inner.Ping(ctx)
}

func (d *indexedDBStoreAllowlist) Close() error {
	return d.inner.Close()
}

func (d *indexedDBStoreAllowlist) Open(ctx context.Context, name string, opts indexeddb.OpenOptions) (indexeddb.Database, error) {
	factory, ok := d.inner.(indexeddb.Factory)
	if !ok {
		return nil, indexeddb.ErrInvalidTransaction
	}
	opts.Upgrade = d.wrapUpgrade(opts.Upgrade)
	db, err := factory.Open(ctx, name, opts)
	if err != nil {
		return nil, err
	}
	return &indexedDBDatabaseAllowlist{allowlist: d, inner: db}, nil
}

func (d *indexedDBStoreAllowlist) OpenCurrent(ctx context.Context, name string, opts indexeddb.OpenOptions) (indexeddb.Database, error) {
	factory, ok := d.inner.(indexeddb.Factory)
	if !ok {
		return nil, indexeddb.ErrInvalidTransaction
	}
	db, err := factory.OpenCurrent(ctx, name, opts)
	if err != nil {
		return nil, err
	}
	return &indexedDBDatabaseAllowlist{allowlist: d, inner: db}, nil
}

func (d *indexedDBStoreAllowlist) DeleteDatabase(ctx context.Context, name string, opts indexeddb.DeleteOptions) (indexeddb.DeleteDatabaseResult, error) {
	factory, ok := d.inner.(indexeddb.Factory)
	if !ok {
		return indexeddb.DeleteDatabaseResult{}, indexeddb.ErrInvalidTransaction
	}
	return factory.DeleteDatabase(ctx, name, opts)
}

func (d *indexedDBStoreAllowlist) Databases(ctx context.Context) ([]indexeddb.DatabaseInfo, error) {
	factory, ok := d.inner.(indexeddb.Factory)
	if !ok {
		return nil, indexeddb.ErrInvalidTransaction
	}
	return factory.Databases(ctx)
}

func (d *indexedDBStoreAllowlist) CompareKeys(first any, second any) (int, error) {
	factory, ok := d.inner.(indexeddb.Factory)
	if !ok {
		return 0, indexeddb.ErrInvalidTransaction
	}
	return factory.CompareKeys(first, second)
}

func (d *indexedDBStoreAllowlist) wrapUpgrade(upgrade func(context.Context, indexeddb.UpgradeContext) error) func(context.Context, indexeddb.UpgradeContext) error {
	if upgrade == nil {
		return nil
	}
	return func(ctx context.Context, inner indexeddb.UpgradeContext) error {
		return upgrade(ctx, indexedDBUpgradeContextAllowlist{allowlist: d, inner: inner})
	}
}

func (d *indexedDBStoreAllowlist) filterStoreNames(names []string) []string {
	if len(d.allowed) == 0 {
		return names
	}
	filtered := names[:0]
	for _, name := range names {
		if _, ok := d.allowed[name]; ok {
			filtered = append(filtered, name)
		}
	}
	return filtered
}

type indexedDBDatabaseAllowlist struct {
	allowlist *indexedDBStoreAllowlist
	inner     indexeddb.Database
}

func (d *indexedDBDatabaseAllowlist) Name() string { return d.inner.Name() }

func (d *indexedDBDatabaseAllowlist) Version() uint64 { return d.inner.Version() }

func (d *indexedDBDatabaseAllowlist) ObjectStoreNames(ctx context.Context) ([]string, error) {
	names, err := d.inner.ObjectStoreNames(ctx)
	if err != nil {
		return nil, err
	}
	return d.allowlist.filterStoreNames(names), nil
}

func (d *indexedDBDatabaseAllowlist) ObjectStore(name string) indexeddb.ObjectStore {
	if err := d.allowlist.checkStore(name); err != nil {
		return missingObjectStore{}
	}
	return d.inner.ObjectStore(name)
}

func (d *indexedDBDatabaseAllowlist) Transaction(ctx context.Context, stores []string, mode indexeddb.TransactionMode, opts indexeddb.TransactionOptions) (indexeddb.Transaction, error) {
	for _, store := range stores {
		if err := d.allowlist.checkStore(store); err != nil {
			return nil, err
		}
	}
	tx, err := d.inner.Transaction(ctx, stores, mode, opts)
	if err != nil {
		return nil, err
	}
	return &indexedDBStoreAllowlistTransaction{allowlist: d.allowlist, inner: tx}, nil
}

func (d *indexedDBDatabaseAllowlist) Close() error { return d.inner.Close() }

type indexedDBUpgradeContextAllowlist struct {
	allowlist *indexedDBStoreAllowlist
	inner     indexeddb.UpgradeContext
}

func (u indexedDBUpgradeContextAllowlist) OldVersion() uint64 { return u.inner.OldVersion() }

func (u indexedDBUpgradeContextAllowlist) NewVersion() uint64 { return u.inner.NewVersion() }

func (u indexedDBUpgradeContextAllowlist) Database() indexeddb.UpgradeDatabase {
	return indexedDBUpgradeDatabaseAllowlist{allowlist: u.allowlist, inner: u.inner.Database()}
}

func (u indexedDBUpgradeContextAllowlist) ObjectStoreNames(ctx context.Context) ([]string, error) {
	names, err := u.inner.ObjectStoreNames(ctx)
	if err != nil {
		return nil, err
	}
	return u.allowlist.filterStoreNames(names), nil
}

func (u indexedDBUpgradeContextAllowlist) CreateObjectStore(ctx context.Context, name string, schema indexeddb.ObjectStoreSchema) error {
	if err := u.allowlist.checkStore(name); err != nil {
		return err
	}
	return u.inner.CreateObjectStore(ctx, name, schema)
}

func (u indexedDBUpgradeContextAllowlist) DeleteObjectStore(ctx context.Context, name string) error {
	if err := u.allowlist.checkStore(name); err != nil {
		return err
	}
	return u.inner.DeleteObjectStore(ctx, name)
}

func (u indexedDBUpgradeContextAllowlist) CreateIndex(ctx context.Context, store string, index indexeddb.IndexSchema) error {
	if err := u.allowlist.checkStore(store); err != nil {
		return err
	}
	return u.inner.CreateIndex(ctx, store, index)
}

func (u indexedDBUpgradeContextAllowlist) DeleteIndex(ctx context.Context, store string, name string) error {
	if err := u.allowlist.checkStore(store); err != nil {
		return err
	}
	return u.inner.DeleteIndex(ctx, store, name)
}

type indexedDBUpgradeDatabaseAllowlist struct {
	allowlist *indexedDBStoreAllowlist
	inner     indexeddb.UpgradeDatabase
}

func (d indexedDBUpgradeDatabaseAllowlist) Name() string { return d.inner.Name() }

func (d indexedDBUpgradeDatabaseAllowlist) ObjectStoreNames(ctx context.Context) ([]string, error) {
	names, err := d.inner.ObjectStoreNames(ctx)
	if err != nil {
		return nil, err
	}
	return d.allowlist.filterStoreNames(names), nil
}

func (d indexedDBUpgradeDatabaseAllowlist) CreateObjectStore(ctx context.Context, name string, schema indexeddb.ObjectStoreSchema) error {
	if err := d.allowlist.checkStore(name); err != nil {
		return err
	}
	return d.inner.CreateObjectStore(ctx, name, schema)
}

func (d indexedDBUpgradeDatabaseAllowlist) DeleteObjectStore(ctx context.Context, name string) error {
	if err := d.allowlist.checkStore(name); err != nil {
		return err
	}
	return d.inner.DeleteObjectStore(ctx, name)
}

func (d indexedDBUpgradeDatabaseAllowlist) CreateIndex(ctx context.Context, store string, index indexeddb.IndexSchema) error {
	if err := d.allowlist.checkStore(store); err != nil {
		return err
	}
	return d.inner.CreateIndex(ctx, store, index)
}

func (d indexedDBUpgradeDatabaseAllowlist) DeleteIndex(ctx context.Context, store string, name string) error {
	if err := d.allowlist.checkStore(store); err != nil {
		return err
	}
	return d.inner.DeleteIndex(ctx, store, name)
}

type indexedDBStoreAllowlistTransaction struct {
	allowlist *indexedDBStoreAllowlist
	inner     indexeddb.Transaction
}

func (tx *indexedDBStoreAllowlistTransaction) ObjectStore(name string) indexeddb.TransactionObjectStore {
	if err := tx.allowlist.checkStore(name); err != nil {
		return missingTransactionObjectStore{tx: tx.inner}
	}
	return tx.inner.ObjectStore(name)
}

func (tx *indexedDBStoreAllowlistTransaction) Commit(ctx context.Context) error {
	return tx.inner.Commit(ctx)
}

func (tx *indexedDBStoreAllowlistTransaction) Abort(ctx context.Context) error {
	return tx.inner.Abort(ctx)
}

type missingObjectStore struct{}

func (missingObjectStore) Get(context.Context, string) (indexeddb.Record, error) {
	return nil, indexeddb.ErrNotFound
}

func (missingObjectStore) GetKey(context.Context, string) (string, error) {
	return "", indexeddb.ErrNotFound
}

func (missingObjectStore) Add(context.Context, indexeddb.Record) error {
	return indexeddb.ErrNotFound
}

func (missingObjectStore) Put(context.Context, indexeddb.Record) error {
	return indexeddb.ErrNotFound
}

func (missingObjectStore) Delete(context.Context, string) error {
	return indexeddb.ErrNotFound
}

func (missingObjectStore) Clear(context.Context) error {
	return indexeddb.ErrNotFound
}

func (missingObjectStore) GetAll(context.Context, *indexeddb.KeyRange) ([]indexeddb.Record, error) {
	return nil, indexeddb.ErrNotFound
}

func (missingObjectStore) GetAllKeys(context.Context, *indexeddb.KeyRange) ([]string, error) {
	return nil, indexeddb.ErrNotFound
}

func (missingObjectStore) Count(context.Context, *indexeddb.KeyRange) (int64, error) {
	return 0, indexeddb.ErrNotFound
}

func (missingObjectStore) DeleteRange(context.Context, indexeddb.KeyRange) (int64, error) {
	return 0, indexeddb.ErrNotFound
}

func (missingObjectStore) Index(string) indexeddb.Index {
	return missingIndex{}
}

func (missingObjectStore) OpenCursor(context.Context, *indexeddb.KeyRange, indexeddb.CursorDirection) (indexeddb.Cursor, error) {
	return nil, indexeddb.ErrNotFound
}

func (missingObjectStore) OpenKeyCursor(context.Context, *indexeddb.KeyRange, indexeddb.CursorDirection) (indexeddb.Cursor, error) {
	return nil, indexeddb.ErrNotFound
}

type missingIndex struct{}

func (missingIndex) Get(context.Context, ...any) (indexeddb.Record, error) {
	return nil, indexeddb.ErrNotFound
}

func (missingIndex) GetKey(context.Context, ...any) (string, error) {
	return "", indexeddb.ErrNotFound
}

func (missingIndex) GetAll(context.Context, *indexeddb.KeyRange, ...any) ([]indexeddb.Record, error) {
	return nil, indexeddb.ErrNotFound
}

func (missingIndex) GetAllKeys(context.Context, *indexeddb.KeyRange, ...any) ([]string, error) {
	return nil, indexeddb.ErrNotFound
}

func (missingIndex) Count(context.Context, *indexeddb.KeyRange, ...any) (int64, error) {
	return 0, indexeddb.ErrNotFound
}

func (missingIndex) Delete(context.Context, ...any) (int64, error) {
	return 0, indexeddb.ErrNotFound
}

func (missingIndex) DeleteRange(context.Context, *indexeddb.KeyRange, ...any) (int64, error) {
	return 0, indexeddb.ErrNotFound
}

func (missingIndex) OpenCursor(context.Context, *indexeddb.KeyRange, indexeddb.CursorDirection, ...any) (indexeddb.Cursor, error) {
	return nil, indexeddb.ErrNotFound
}

func (missingIndex) OpenKeyCursor(context.Context, *indexeddb.KeyRange, indexeddb.CursorDirection, ...any) (indexeddb.Cursor, error) {
	return nil, indexeddb.ErrNotFound
}

type missingTransactionObjectStore struct {
	tx indexeddb.Transaction
}

func (s missingTransactionObjectStore) fail(ctx context.Context) error {
	_ = s.tx.Abort(ctx)
	return indexeddb.ErrNotFound
}

func (s missingTransactionObjectStore) Get(ctx context.Context, _ string) (indexeddb.Record, error) {
	return nil, s.fail(ctx)
}

func (s missingTransactionObjectStore) GetKey(ctx context.Context, _ string) (string, error) {
	return "", s.fail(ctx)
}

func (s missingTransactionObjectStore) Add(ctx context.Context, _ indexeddb.Record) error {
	return s.fail(ctx)
}

func (s missingTransactionObjectStore) Put(ctx context.Context, _ indexeddb.Record) error {
	return s.fail(ctx)
}

func (s missingTransactionObjectStore) Delete(ctx context.Context, _ string) error {
	return s.fail(ctx)
}

func (s missingTransactionObjectStore) Clear(ctx context.Context) error {
	return s.fail(ctx)
}

func (s missingTransactionObjectStore) GetAll(ctx context.Context, _ *indexeddb.KeyRange) ([]indexeddb.Record, error) {
	return nil, s.fail(ctx)
}

func (s missingTransactionObjectStore) GetAllKeys(ctx context.Context, _ *indexeddb.KeyRange) ([]string, error) {
	return nil, s.fail(ctx)
}

func (s missingTransactionObjectStore) Count(ctx context.Context, _ *indexeddb.KeyRange) (int64, error) {
	return 0, s.fail(ctx)
}

func (s missingTransactionObjectStore) DeleteRange(ctx context.Context, _ indexeddb.KeyRange) (int64, error) {
	return 0, s.fail(ctx)
}

func (s missingTransactionObjectStore) Index(string) indexeddb.TransactionIndex {
	return missingTransactionIndex(s)
}

type missingTransactionIndex struct {
	tx indexeddb.Transaction
}

func (i missingTransactionIndex) fail(ctx context.Context) error {
	_ = i.tx.Abort(ctx)
	return indexeddb.ErrNotFound
}

func (i missingTransactionIndex) Get(ctx context.Context, _ ...any) (indexeddb.Record, error) {
	return nil, i.fail(ctx)
}

func (i missingTransactionIndex) GetKey(ctx context.Context, _ ...any) (string, error) {
	return "", i.fail(ctx)
}

func (i missingTransactionIndex) GetAll(ctx context.Context, _ *indexeddb.KeyRange, _ ...any) ([]indexeddb.Record, error) {
	return nil, i.fail(ctx)
}

func (i missingTransactionIndex) GetAllKeys(ctx context.Context, _ *indexeddb.KeyRange, _ ...any) ([]string, error) {
	return nil, i.fail(ctx)
}

func (i missingTransactionIndex) Count(ctx context.Context, _ *indexeddb.KeyRange, _ ...any) (int64, error) {
	return 0, i.fail(ctx)
}

func (i missingTransactionIndex) Delete(ctx context.Context, _ ...any) (int64, error) {
	return 0, i.fail(ctx)
}

func (i missingTransactionIndex) DeleteRange(ctx context.Context, _ *indexeddb.KeyRange, _ ...any) (int64, error) {
	return 0, i.fail(ctx)
}
