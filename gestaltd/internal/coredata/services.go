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

const systemDatabaseName = "gestalt"

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
	mu           sync.Mutex
	root         indexeddb.IndexedDB
	factory      indexeddb.Factory
	dbName       string
	db           indexeddb.Database
	metricDBName string
}

func (d *databaseBackedIndexedDB) ObjectStore(name string) indexeddb.ObjectStore {
	d.mu.Lock()
	db := d.db
	d.mu.Unlock()
	store := db.ObjectStore(name)
	if d.metricDBName == "" {
		return store
	}
	return metricutil.InstrumentObjectStore(store, metricutil.IndexedDBMetricLabels{
		DB:          d.metricDBName,
		ObjectStore: name,
	})
}

func (d *databaseBackedIndexedDB) Transaction(ctx context.Context, stores []string, mode indexeddb.TransactionMode, opts indexeddb.TransactionOptions) (indexeddb.Transaction, error) {
	d.mu.Lock()
	db := d.db
	d.mu.Unlock()
	return db.Transaction(ctx, stores, mode, opts)
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
	return d.root.Ping(ctx)
}

func (d *databaseBackedIndexedDB) Close() error {
	d.mu.Lock()
	db := d.db
	d.db = nil
	d.mu.Unlock()
	var errs []error
	if db != nil {
		errs = append(errs, db.Close())
	}
	if d.root != nil {
		errs = append(errs, d.root.Close())
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

func (s *Services) Ping(ctx context.Context) error {
	return s.DB.Ping(ctx)
}

func (s *Services) Close() error {
	return s.DB.Close()
}
