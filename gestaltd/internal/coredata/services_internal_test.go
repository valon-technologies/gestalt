package coredata

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

func TestDatabaseBackedIndexedDBClearsClosedHandleWhenUpgradeRecoveryFails(t *testing.T) {
	t.Parallel()

	upgradeErr := errors.New("upgrade failed")
	recoveryErr := errors.New("recovery failed")
	db := &failingUpgradeDatabase{version: 3}
	adapter := &databaseBackedIndexedDB{
		factory: &failingUpgradeFactory{
			openErr:        upgradeErr,
			openCurrentErr: recoveryErr,
		},
		dbName: "gestalt",
		db:     db,
	}

	err := adapter.CreateObjectStore(context.Background(), "sessions", indexeddb.ObjectStoreSchema{})
	if !errors.Is(err, upgradeErr) {
		t.Fatalf("CreateObjectStore error = %v, want %v", err, upgradeErr)
	}
	if !db.closed {
		t.Fatal("previous database handle was not closed")
	}
	if adapter.db != nil {
		t.Fatalf("database handle after failed recovery = %#v, want nil", adapter.db)
	}
	if _, err := adapter.ObjectStore("sessions").Get(context.Background(), "session-1"); !errors.Is(err, indexeddb.ErrNotFound) {
		t.Fatalf("ObjectStore after failed recovery error = %v, want ErrNotFound", err)
	}
	if _, err := adapter.Transaction(context.Background(), []string{"sessions"}, indexeddb.TransactionReadonly, indexeddb.TransactionOptions{}); !errors.Is(err, indexeddb.ErrNotFound) {
		t.Fatalf("Transaction after failed recovery error = %v, want ErrNotFound", err)
	}
	if err := adapter.CreateObjectStore(context.Background(), "sessions", indexeddb.ObjectStoreSchema{}); !errors.Is(err, indexeddb.ErrNotFound) {
		t.Fatalf("CreateObjectStore after failed recovery error = %v, want ErrNotFound", err)
	}
}

func TestDatabaseBackedIndexedDBCloseIsIdempotent(t *testing.T) {
	t.Parallel()

	root := &closeCountingIndexedDB{}
	db := &failingUpgradeDatabase{version: 1}
	adapter := &databaseBackedIndexedDB{
		root: root,
		db:   db,
	}

	if err := adapter.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := adapter.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if root.closeCount != 1 {
		t.Fatalf("root Close count = %d, want 1", root.closeCount)
	}
	if db.closeCount != 1 {
		t.Fatalf("database Close count = %d, want 1", db.closeCount)
	}
	if err := adapter.Ping(context.Background()); !errors.Is(err, indexeddb.ErrNotFound) {
		t.Fatalf("Ping after Close error = %v, want ErrNotFound", err)
	}
}

func TestDatabaseBackedIndexedDBObjectStoreBlocksConcurrentUpgrade(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := &blockingObjectStore{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	oldDB := newObservableDatabase(1)
	oldDB.store = store
	adapter := &databaseBackedIndexedDB{
		factory: &replacementFactory{db: newObservableDatabase(2)},
		dbName:  "gestalt",
		db:      oldDB,
	}

	getDone := make(chan error, 1)
	go func() {
		_, err := adapter.ObjectStore("sessions").Get(ctx, "session-1")
		getDone <- err
	}()
	<-store.started

	upgradeDone := make(chan error, 1)
	go func() {
		upgradeDone <- adapter.CreateObjectStore(ctx, "sessions", indexeddb.ObjectStoreSchema{})
	}()

	assertNotClosedSoon(t, oldDB.closed, "old database was closed while object-store operation was still running")
	close(store.release)
	if err := <-getDone; err != nil {
		t.Fatalf("Get: %v", err)
	}
	select {
	case err := <-upgradeDone:
		if err != nil {
			t.Fatalf("CreateObjectStore: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("CreateObjectStore did not complete after object-store operation finished")
	}
	select {
	case <-oldDB.closed:
	case <-time.After(time.Second):
		t.Fatal("old database was not closed during upgrade")
	}
}

func TestDatabaseBackedIndexedDBTransactionBlocksConcurrentUpgrade(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	oldDB := newObservableDatabase(1)
	oldDB.tx = &blockingTransaction{}
	adapter := &databaseBackedIndexedDB{
		factory: &replacementFactory{db: newObservableDatabase(2)},
		dbName:  "gestalt",
		db:      oldDB,
	}

	tx, err := adapter.Transaction(ctx, []string{"sessions"}, indexeddb.TransactionReadwrite, indexeddb.TransactionOptions{})
	if err != nil {
		t.Fatalf("Transaction: %v", err)
	}

	upgradeDone := make(chan error, 1)
	go func() {
		upgradeDone <- adapter.CreateObjectStore(ctx, "sessions", indexeddb.ObjectStoreSchema{})
	}()

	assertNotClosedSoon(t, oldDB.closed, "old database was closed while transaction was still running")
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	select {
	case err := <-upgradeDone:
		if err != nil {
			t.Fatalf("CreateObjectStore: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("CreateObjectStore did not complete after transaction committed")
	}
	select {
	case <-oldDB.closed:
	case <-time.After(time.Second):
		t.Fatal("old database was not closed during upgrade")
	}
}

func assertNotClosedSoon(t *testing.T, ch <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-ch:
		t.Fatal(message)
	case <-time.After(25 * time.Millisecond):
	}
}

type failingUpgradeFactory struct {
	openErr        error
	openCurrentErr error
}

func (f *failingUpgradeFactory) Open(context.Context, string, indexeddb.OpenOptions) (indexeddb.Database, error) {
	return nil, f.openErr
}

func (f *failingUpgradeFactory) OpenCurrent(context.Context, string, indexeddb.OpenOptions) (indexeddb.Database, error) {
	return nil, f.openCurrentErr
}

func (f *failingUpgradeFactory) DeleteDatabase(context.Context, string, indexeddb.DeleteOptions) (indexeddb.DeleteDatabaseResult, error) {
	return indexeddb.DeleteDatabaseResult{}, nil
}

func (f *failingUpgradeFactory) Databases(context.Context) ([]indexeddb.DatabaseInfo, error) {
	return nil, nil
}

func (f *failingUpgradeFactory) CompareKeys(any, any) (int, error) { return 0, nil }

func (f *failingUpgradeFactory) Close() error { return nil }

type failingUpgradeDatabase struct {
	version    uint64
	closed     bool
	closeCount int
}

func (d *failingUpgradeDatabase) Name() string { return "gestalt" }

func (d *failingUpgradeDatabase) Version() uint64 { return d.version }

func (d *failingUpgradeDatabase) ObjectStoreNames(context.Context) ([]string, error) {
	return nil, nil
}

func (d *failingUpgradeDatabase) ObjectStore(string) indexeddb.ObjectStore { return nil }

func (d *failingUpgradeDatabase) Transaction(context.Context, []string, indexeddb.TransactionMode, indexeddb.TransactionOptions) (indexeddb.Transaction, error) {
	return nil, indexeddb.ErrNotFound
}

func (d *failingUpgradeDatabase) Close() error {
	d.closed = true
	d.closeCount++
	return nil
}

type closeCountingIndexedDB struct {
	closeCount int
}

func (d *closeCountingIndexedDB) ObjectStore(string) indexeddb.ObjectStore { return nil }

func (d *closeCountingIndexedDB) Transaction(context.Context, []string, indexeddb.TransactionMode, indexeddb.TransactionOptions) (indexeddb.Transaction, error) {
	return nil, indexeddb.ErrNotFound
}

func (d *closeCountingIndexedDB) CreateObjectStore(context.Context, string, indexeddb.ObjectStoreSchema) error {
	return nil
}

func (d *closeCountingIndexedDB) DeleteObjectStore(context.Context, string) error {
	return nil
}

func (d *closeCountingIndexedDB) Ping(context.Context) error { return nil }

func (d *closeCountingIndexedDB) Close() error {
	d.closeCount++
	return nil
}

type replacementFactory struct {
	db indexeddb.Database
}

func (f *replacementFactory) Open(context.Context, string, indexeddb.OpenOptions) (indexeddb.Database, error) {
	return f.db, nil
}

func (f *replacementFactory) OpenCurrent(context.Context, string, indexeddb.OpenOptions) (indexeddb.Database, error) {
	return nil, indexeddb.ErrNotFound
}

func (f *replacementFactory) DeleteDatabase(context.Context, string, indexeddb.DeleteOptions) (indexeddb.DeleteDatabaseResult, error) {
	return indexeddb.DeleteDatabaseResult{}, nil
}

func (f *replacementFactory) Databases(context.Context) ([]indexeddb.DatabaseInfo, error) {
	return nil, nil
}

func (f *replacementFactory) CompareKeys(any, any) (int, error) { return 0, nil }

func (f *replacementFactory) Close() error { return nil }

type observableDatabase struct {
	version uint64
	store   indexeddb.ObjectStore
	tx      indexeddb.Transaction
	closed  chan struct{}
	once    sync.Once
}

func newObservableDatabase(version uint64) *observableDatabase {
	return &observableDatabase{
		version: version,
		closed:  make(chan struct{}),
	}
}

func (d *observableDatabase) Name() string { return "gestalt" }

func (d *observableDatabase) Version() uint64 { return d.version }

func (d *observableDatabase) ObjectStoreNames(context.Context) ([]string, error) {
	return []string{"sessions"}, nil
}

func (d *observableDatabase) ObjectStore(string) indexeddb.ObjectStore { return d.store }

func (d *observableDatabase) Transaction(context.Context, []string, indexeddb.TransactionMode, indexeddb.TransactionOptions) (indexeddb.Transaction, error) {
	if d.tx == nil {
		return nil, indexeddb.ErrNotFound
	}
	return d.tx, nil
}

func (d *observableDatabase) Close() error {
	d.once.Do(func() {
		close(d.closed)
	})
	return nil
}

type blockingObjectStore struct {
	errObjectStore
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockingObjectStore) Get(_ context.Context, id string) (indexeddb.Record, error) {
	s.once.Do(func() {
		close(s.started)
	})
	<-s.release
	return indexeddb.Record{"id": id}, nil
}

type blockingTransaction struct{}

func (tx *blockingTransaction) ObjectStore(string) indexeddb.TransactionObjectStore { return nil }

func (tx *blockingTransaction) Commit(context.Context) error { return tx.finish() }

func (tx *blockingTransaction) Abort(context.Context) error { return tx.finish() }

func (tx *blockingTransaction) finish() error {
	return nil
}
