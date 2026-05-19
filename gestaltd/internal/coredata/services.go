package coredata

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"github.com/valon-technologies/gestalt/server/services/runtimehost/runtimelogs"
)

type Services struct {
	Users               *UserService
	ExternalCredentials core.ExternalCredentialProvider
	APITokens           *APITokenService
	ManagedSubjects     *ManagedSubjectService
	RuntimeSessionLogs  runtimelogs.Store
	DB                  indexeddb.IndexedDB
}

// SystemDatabaseName is the logical IndexedDB database that stores gestaltd
// core object stores.
const SystemDatabaseName = "gestalt"

const systemDatabaseName = SystemDatabaseName

var systemObjectStores = []struct {
	name   string
	schema indexeddb.ObjectStoreSchema
}{
	{name: StoreUsers, schema: UsersSchema},
	{name: StoreAPITokens, schema: APITokensSchema},
	{name: StoreManagedSubjects, schema: ManagedSubjectsSchema},
}

func New(ds indexeddb.IndexedDB) (*Services, error) {
	return NewWithContext(context.Background(), ds)
}

func NewWithContext(ctx context.Context, ds indexeddb.IndexedDB) (*Services, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	db, err := prepareSystemDatabase(ctx, ds)
	if err != nil {
		return nil, err
	}

	runtimeSessionLogs := runtimelogs.NewMemoryStore()

	users := NewUserService(db)
	apiTokens := NewAPITokenService(db)
	managedSubjects := NewManagedSubjectService(db)
	return &Services{
		ExternalCredentials: nil,
		Users:               users,
		APITokens:           apiTokens,
		ManagedSubjects:     managedSubjects,
		RuntimeSessionLogs:  runtimeSessionLogs,
		DB:                  db,
	}, nil
}

func prepareSystemDatabase(ctx context.Context, ds indexeddb.IndexedDB) (indexeddb.IndexedDB, error) {
	factory, ok := metricutil.UnwrapIndexedDB(ds).(indexeddb.Factory)
	if !ok || metricutil.IndexedDBName(ds) == "" {
		for _, store := range systemObjectStores {
			if err := ds.CreateObjectStore(ctx, store.name, store.schema); err != nil {
				return nil, fmt.Errorf("create %s store: %w", store.name, err)
			}
		}
		return ds, nil
	}

	db, err := openDatabaseWithObjectStores(ctx, factory, systemDatabaseName, systemObjectStores)
	if err != nil {
		return nil, err
	}
	return &databaseBackedIndexedDB{
		root:         ds,
		factory:      factory,
		dbName:       systemDatabaseName,
		db:           db,
		metricDBName: metricutil.IndexedDBName(ds),
	}, nil
}

func openDatabaseWithObjectStores(
	ctx context.Context,
	factory indexeddb.Factory,
	name string,
	stores []struct {
		name   string
		schema indexeddb.ObjectStoreSchema
	},
) (indexeddb.Database, error) {
	db, err := factory.OpenCurrent(ctx, name, indexeddb.OpenOptions{})
	if errors.Is(err, indexeddb.ErrNotFound) {
		version := uint64(1)
		return factory.Open(ctx, name, indexeddb.OpenOptions{
			Version: &version,
			Upgrade: func(ctx context.Context, upgrade indexeddb.UpgradeContext) error {
				return createObjectStores(ctx, upgrade, stores)
			},
		})
	}
	if err != nil {
		return nil, err
	}

	names, err := db.ObjectStoreNames(ctx)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	missing := missingObjectStores(names, stores)
	if len(missing) == 0 {
		return db, nil
	}
	version := db.Version() + 1
	_ = db.Close()
	return factory.Open(ctx, name, indexeddb.OpenOptions{
		Version: &version,
		Upgrade: func(ctx context.Context, upgrade indexeddb.UpgradeContext) error {
			return createObjectStores(ctx, upgrade, missing)
		},
	})
}

func missingObjectStores(
	names []string,
	stores []struct {
		name   string
		schema indexeddb.ObjectStoreSchema
	},
) []struct {
	name   string
	schema indexeddb.ObjectStoreSchema
} {
	existing := make(map[string]struct{}, len(names))
	for _, name := range names {
		existing[name] = struct{}{}
	}
	missing := make([]struct {
		name   string
		schema indexeddb.ObjectStoreSchema
	}, 0, len(stores))
	for _, store := range stores {
		if _, ok := existing[store.name]; !ok {
			missing = append(missing, store)
		}
	}
	return missing
}

func createObjectStores(
	ctx context.Context,
	upgrade interface {
		CreateObjectStore(context.Context, string, indexeddb.ObjectStoreSchema) error
	},
	stores []struct {
		name   string
		schema indexeddb.ObjectStoreSchema
	},
) error {
	for _, store := range stores {
		if err := upgrade.CreateObjectStore(ctx, store.name, store.schema); err != nil && !errors.Is(err, indexeddb.ErrAlreadyExists) {
			return fmt.Errorf("create %s store: %w", store.name, err)
		}
	}
	return nil
}

type databaseBackedIndexedDB struct {
	mu           sync.RWMutex
	root         indexeddb.IndexedDB
	factory      indexeddb.Factory
	dbName       string
	db           indexeddb.Database
	metricDBName string
}

func (d *databaseBackedIndexedDB) currentFactory() (indexeddb.Factory, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.root == nil || d.factory == nil {
		return nil, indexeddb.ErrNotFound
	}
	return d.factory, nil
}

func (d *databaseBackedIndexedDB) Open(ctx context.Context, name string, opts indexeddb.OpenOptions) (indexeddb.Database, error) {
	factory, err := d.currentFactory()
	if err != nil {
		return nil, err
	}
	return factory.Open(ctx, name, opts)
}

func (d *databaseBackedIndexedDB) OpenCurrent(ctx context.Context, name string, opts indexeddb.OpenOptions) (indexeddb.Database, error) {
	factory, err := d.currentFactory()
	if err != nil {
		return nil, err
	}
	return factory.OpenCurrent(ctx, name, opts)
}

func (d *databaseBackedIndexedDB) DeleteDatabase(ctx context.Context, name string, opts indexeddb.DeleteOptions) (indexeddb.DeleteDatabaseResult, error) {
	factory, err := d.currentFactory()
	if err != nil {
		return indexeddb.DeleteDatabaseResult{Name: name}, err
	}
	return factory.DeleteDatabase(ctx, name, opts)
}

func (d *databaseBackedIndexedDB) Databases(ctx context.Context) ([]indexeddb.DatabaseInfo, error) {
	factory, err := d.currentFactory()
	if err != nil {
		return nil, err
	}
	return factory.Databases(ctx)
}

func (d *databaseBackedIndexedDB) CompareKeys(first any, second any) (int, error) {
	factory, err := d.currentFactory()
	if err != nil {
		return 0, err
	}
	return factory.CompareKeys(first, second)
}

func (d *databaseBackedIndexedDB) ObjectStore(name string) indexeddb.ObjectStore {
	store := databaseBackedObjectStore{db: d, name: name}
	if d.metricDBName == "" {
		return store
	}
	return metricutil.InstrumentObjectStore(store, metricutil.IndexedDBMetricLabels{
		DB:          d.metricDBName,
		ObjectStore: name,
	})
}

func (d *databaseBackedIndexedDB) Transaction(ctx context.Context, stores []string, mode indexeddb.TransactionMode, opts indexeddb.TransactionOptions) (indexeddb.Transaction, error) {
	d.mu.RLock()
	db := d.db
	if db == nil {
		d.mu.RUnlock()
		return nil, indexeddb.ErrNotFound
	}
	tx, err := db.Transaction(ctx, stores, mode, opts)
	if err != nil {
		d.mu.RUnlock()
		return nil, err
	}
	wrapped := &databaseBackedTransaction{inner: tx, release: d.mu.RUnlock}
	if d.metricDBName == "" {
		return wrapped, nil
	}
	return metricutil.InstrumentTransaction(wrapped, d.metricDBName), nil
}

func (d *databaseBackedIndexedDB) CreateObjectStore(ctx context.Context, name string, schema indexeddb.ObjectStoreSchema) error {
	return d.upgrade(ctx, func(ctx context.Context, upgrade indexeddb.UpgradeContext) error {
		if err := upgrade.CreateObjectStore(ctx, name, schema); err != nil && !errors.Is(err, indexeddb.ErrAlreadyExists) {
			return err
		}
		return nil
	})
}

func (d *databaseBackedIndexedDB) DeleteObjectStore(ctx context.Context, name string) error {
	return d.upgrade(ctx, func(ctx context.Context, upgrade indexeddb.UpgradeContext) error {
		return upgrade.DeleteObjectStore(ctx, name)
	})
}

func (d *databaseBackedIndexedDB) Ping(ctx context.Context) error {
	d.mu.RLock()
	root := d.root
	if root == nil {
		d.mu.RUnlock()
		return indexeddb.ErrNotFound
	}
	defer d.mu.RUnlock()
	return root.Ping(ctx)
}

func (d *databaseBackedIndexedDB) Close() error {
	d.mu.Lock()
	db := d.db
	root := d.root
	d.db = nil
	d.root = nil
	d.mu.Unlock()
	var errs []error
	if db != nil {
		errs = append(errs, db.Close())
	}
	if root != nil {
		errs = append(errs, root.Close())
	}
	return errors.Join(errs...)
}

func (d *databaseBackedIndexedDB) upgrade(ctx context.Context, fn func(context.Context, indexeddb.UpgradeContext) error) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.db == nil {
		return indexeddb.ErrNotFound
	}
	version := d.db.Version() + 1
	_ = d.db.Close()
	d.db = nil
	db, err := d.factory.Open(ctx, d.dbName, indexeddb.OpenOptions{
		Version: &version,
		Upgrade: fn,
	})
	if err != nil {
		current, currentErr := d.factory.OpenCurrent(ctx, d.dbName, indexeddb.OpenOptions{})
		if currentErr == nil {
			d.db = current
		}
		return err
	}
	d.db = db
	return nil
}

type databaseBackedObjectStore struct {
	db   *databaseBackedIndexedDB
	name string
}

func (s databaseBackedObjectStore) current() (indexeddb.ObjectStore, func(), error) {
	s.db.mu.RLock()
	db := s.db.db
	if db == nil {
		s.db.mu.RUnlock()
		return nil, nil, indexeddb.ErrNotFound
	}
	store := db.ObjectStore(s.name)
	if store == nil {
		s.db.mu.RUnlock()
		return nil, nil, indexeddb.ErrNotFound
	}
	return store, s.db.mu.RUnlock, nil
}

func (s databaseBackedObjectStore) Get(ctx context.Context, id string) (indexeddb.Record, error) {
	store, release, err := s.current()
	if err != nil {
		return nil, err
	}
	defer release()
	return store.Get(ctx, id)
}

func (s databaseBackedObjectStore) GetKey(ctx context.Context, id string) (string, error) {
	store, release, err := s.current()
	if err != nil {
		return "", err
	}
	defer release()
	return store.GetKey(ctx, id)
}

func (s databaseBackedObjectStore) Add(ctx context.Context, record indexeddb.Record) error {
	store, release, err := s.current()
	if err != nil {
		return err
	}
	defer release()
	return store.Add(ctx, record)
}

func (s databaseBackedObjectStore) Put(ctx context.Context, record indexeddb.Record) error {
	store, release, err := s.current()
	if err != nil {
		return err
	}
	defer release()
	return store.Put(ctx, record)
}

func (s databaseBackedObjectStore) Delete(ctx context.Context, id string) error {
	store, release, err := s.current()
	if err != nil {
		return err
	}
	defer release()
	return store.Delete(ctx, id)
}

func (s databaseBackedObjectStore) Clear(ctx context.Context) error {
	store, release, err := s.current()
	if err != nil {
		return err
	}
	defer release()
	return store.Clear(ctx)
}

func (s databaseBackedObjectStore) GetAll(ctx context.Context, r *indexeddb.KeyRange) ([]indexeddb.Record, error) {
	store, release, err := s.current()
	if err != nil {
		return nil, err
	}
	defer release()
	return store.GetAll(ctx, r)
}

func (s databaseBackedObjectStore) GetAllKeys(ctx context.Context, r *indexeddb.KeyRange) ([]string, error) {
	store, release, err := s.current()
	if err != nil {
		return nil, err
	}
	defer release()
	return store.GetAllKeys(ctx, r)
}

func (s databaseBackedObjectStore) Count(ctx context.Context, r *indexeddb.KeyRange) (int64, error) {
	store, release, err := s.current()
	if err != nil {
		return 0, err
	}
	defer release()
	return store.Count(ctx, r)
}

func (s databaseBackedObjectStore) DeleteRange(ctx context.Context, r indexeddb.KeyRange) (int64, error) {
	store, release, err := s.current()
	if err != nil {
		return 0, err
	}
	defer release()
	return store.DeleteRange(ctx, r)
}

func (s databaseBackedObjectStore) Index(name string) indexeddb.Index {
	return databaseBackedIndex{db: s.db, storeName: s.name, name: name}
}

func (s databaseBackedObjectStore) OpenCursor(ctx context.Context, r *indexeddb.KeyRange, dir indexeddb.CursorDirection) (indexeddb.Cursor, error) {
	store, release, err := s.current()
	if err != nil {
		return nil, err
	}
	cursor, err := store.OpenCursor(ctx, r, dir)
	if err != nil || cursor == nil {
		release()
		return cursor, err
	}
	return &databaseBackedCursor{inner: cursor, release: release}, nil
}

func (s databaseBackedObjectStore) OpenKeyCursor(ctx context.Context, r *indexeddb.KeyRange, dir indexeddb.CursorDirection) (indexeddb.Cursor, error) {
	store, release, err := s.current()
	if err != nil {
		return nil, err
	}
	cursor, err := store.OpenKeyCursor(ctx, r, dir)
	if err != nil || cursor == nil {
		release()
		return cursor, err
	}
	return &databaseBackedCursor{inner: cursor, release: release}, nil
}

type databaseBackedIndex struct {
	db        *databaseBackedIndexedDB
	storeName string
	name      string
}

func (i databaseBackedIndex) current() (indexeddb.Index, func(), error) {
	i.db.mu.RLock()
	db := i.db.db
	if db == nil {
		i.db.mu.RUnlock()
		return nil, nil, indexeddb.ErrNotFound
	}
	store := db.ObjectStore(i.storeName)
	if store == nil {
		i.db.mu.RUnlock()
		return nil, nil, indexeddb.ErrNotFound
	}
	index := store.Index(i.name)
	if index == nil {
		i.db.mu.RUnlock()
		return nil, nil, indexeddb.ErrNotFound
	}
	return index, i.db.mu.RUnlock, nil
}

func (i databaseBackedIndex) Get(ctx context.Context, values ...any) (indexeddb.Record, error) {
	index, release, err := i.current()
	if err != nil {
		return nil, err
	}
	defer release()
	return index.Get(ctx, values...)
}

func (i databaseBackedIndex) GetKey(ctx context.Context, values ...any) (string, error) {
	index, release, err := i.current()
	if err != nil {
		return "", err
	}
	defer release()
	return index.GetKey(ctx, values...)
}

func (i databaseBackedIndex) GetAll(ctx context.Context, r *indexeddb.KeyRange, values ...any) ([]indexeddb.Record, error) {
	index, release, err := i.current()
	if err != nil {
		return nil, err
	}
	defer release()
	return index.GetAll(ctx, r, values...)
}

func (i databaseBackedIndex) GetAllKeys(ctx context.Context, r *indexeddb.KeyRange, values ...any) ([]string, error) {
	index, release, err := i.current()
	if err != nil {
		return nil, err
	}
	defer release()
	return index.GetAllKeys(ctx, r, values...)
}

func (i databaseBackedIndex) Count(ctx context.Context, r *indexeddb.KeyRange, values ...any) (int64, error) {
	index, release, err := i.current()
	if err != nil {
		return 0, err
	}
	defer release()
	return index.Count(ctx, r, values...)
}

func (i databaseBackedIndex) Delete(ctx context.Context, values ...any) (int64, error) {
	index, release, err := i.current()
	if err != nil {
		return 0, err
	}
	defer release()
	return index.Delete(ctx, values...)
}

func (i databaseBackedIndex) DeleteRange(ctx context.Context, r *indexeddb.KeyRange, values ...any) (int64, error) {
	index, release, err := i.current()
	if err != nil {
		return 0, err
	}
	defer release()
	return index.DeleteRange(ctx, r, values...)
}

func (i databaseBackedIndex) OpenCursor(ctx context.Context, r *indexeddb.KeyRange, dir indexeddb.CursorDirection, values ...any) (indexeddb.Cursor, error) {
	index, release, err := i.current()
	if err != nil {
		return nil, err
	}
	cursor, err := index.OpenCursor(ctx, r, dir, values...)
	if err != nil || cursor == nil {
		release()
		return cursor, err
	}
	return &databaseBackedCursor{inner: cursor, release: release}, nil
}

func (i databaseBackedIndex) OpenKeyCursor(ctx context.Context, r *indexeddb.KeyRange, dir indexeddb.CursorDirection, values ...any) (indexeddb.Cursor, error) {
	index, release, err := i.current()
	if err != nil {
		return nil, err
	}
	cursor, err := index.OpenKeyCursor(ctx, r, dir, values...)
	if err != nil || cursor == nil {
		release()
		return cursor, err
	}
	return &databaseBackedCursor{inner: cursor, release: release}, nil
}

type databaseBackedTransaction struct {
	inner   indexeddb.Transaction
	release func()
	once    sync.Once
}

func (tx *databaseBackedTransaction) ObjectStore(name string) indexeddb.TransactionObjectStore {
	return tx.inner.ObjectStore(name)
}

func (tx *databaseBackedTransaction) Commit(ctx context.Context) error {
	err := tx.inner.Commit(ctx)
	tx.once.Do(tx.release)
	return err
}

func (tx *databaseBackedTransaction) Abort(ctx context.Context) error {
	err := tx.inner.Abort(ctx)
	tx.once.Do(tx.release)
	return err
}

type databaseBackedCursor struct {
	inner    indexeddb.Cursor
	release  func()
	once     sync.Once
	closeErr error
}

func (c *databaseBackedCursor) Continue() bool {
	return c.inner.Continue()
}

func (c *databaseBackedCursor) ContinueToKey(key any) bool {
	return c.inner.ContinueToKey(key)
}

func (c *databaseBackedCursor) Advance(count int) bool {
	return c.inner.Advance(count)
}

func (c *databaseBackedCursor) Key() any {
	return c.inner.Key()
}

func (c *databaseBackedCursor) PrimaryKey() string {
	return c.inner.PrimaryKey()
}

func (c *databaseBackedCursor) Value() (indexeddb.Record, error) {
	return c.inner.Value()
}

func (c *databaseBackedCursor) Delete() error {
	return c.inner.Delete()
}

func (c *databaseBackedCursor) Update(value indexeddb.Record) error {
	return c.inner.Update(value)
}

func (c *databaseBackedCursor) Err() error {
	return c.inner.Err()
}

func (c *databaseBackedCursor) Close() error {
	c.once.Do(func() {
		c.closeErr = c.inner.Close()
		c.release()
	})
	return c.closeErr
}

type errObjectStore struct {
	err error
}

func (s errObjectStore) Get(context.Context, string) (indexeddb.Record, error) {
	return nil, s.err
}

func (s errObjectStore) GetKey(context.Context, string) (string, error) {
	return "", s.err
}

func (s errObjectStore) Add(context.Context, indexeddb.Record) error {
	return s.err
}

func (s errObjectStore) Put(context.Context, indexeddb.Record) error {
	return s.err
}

func (s errObjectStore) Delete(context.Context, string) error {
	return s.err
}

func (s errObjectStore) Clear(context.Context) error {
	return s.err
}

func (s errObjectStore) GetAll(context.Context, *indexeddb.KeyRange) ([]indexeddb.Record, error) {
	return nil, s.err
}

func (s errObjectStore) GetAllKeys(context.Context, *indexeddb.KeyRange) ([]string, error) {
	return nil, s.err
}

func (s errObjectStore) Count(context.Context, *indexeddb.KeyRange) (int64, error) {
	return 0, s.err
}

func (s errObjectStore) DeleteRange(context.Context, indexeddb.KeyRange) (int64, error) {
	return 0, s.err
}

func (s errObjectStore) Index(string) indexeddb.Index {
	return errIndex(s)
}

func (s errObjectStore) OpenCursor(context.Context, *indexeddb.KeyRange, indexeddb.CursorDirection) (indexeddb.Cursor, error) {
	return nil, s.err
}

func (s errObjectStore) OpenKeyCursor(context.Context, *indexeddb.KeyRange, indexeddb.CursorDirection) (indexeddb.Cursor, error) {
	return nil, s.err
}

type errIndex struct {
	err error
}

func (i errIndex) Get(context.Context, ...any) (indexeddb.Record, error) {
	return nil, i.err
}

func (i errIndex) GetKey(context.Context, ...any) (string, error) {
	return "", i.err
}

func (i errIndex) GetAll(context.Context, *indexeddb.KeyRange, ...any) ([]indexeddb.Record, error) {
	return nil, i.err
}

func (i errIndex) GetAllKeys(context.Context, *indexeddb.KeyRange, ...any) ([]string, error) {
	return nil, i.err
}

func (i errIndex) Count(context.Context, *indexeddb.KeyRange, ...any) (int64, error) {
	return 0, i.err
}

func (i errIndex) Delete(context.Context, ...any) (int64, error) {
	return 0, i.err
}

func (i errIndex) DeleteRange(context.Context, *indexeddb.KeyRange, ...any) (int64, error) {
	return 0, i.err
}

func (i errIndex) OpenCursor(context.Context, *indexeddb.KeyRange, indexeddb.CursorDirection, ...any) (indexeddb.Cursor, error) {
	return nil, i.err
}

func (i errIndex) OpenKeyCursor(context.Context, *indexeddb.KeyRange, indexeddb.CursorDirection, ...any) (indexeddb.Cursor, error) {
	return nil, i.err
}

func (s *Services) Ping(ctx context.Context) error {
	return s.DB.Ping(ctx)
}

func (s *Services) Close() error {
	return s.DB.Close()
}
