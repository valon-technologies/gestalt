package coredata

import (
	"context"
	"errors"
	"testing"

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
