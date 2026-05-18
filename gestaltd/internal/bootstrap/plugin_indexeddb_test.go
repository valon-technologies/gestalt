package bootstrap

import (
	"context"
	"errors"
	"testing"

	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
)

func TestIndexedDBStoreAllowlistTransactionMissingStoreAbortsInnerTransaction(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	inner := &coretesting.StubIndexedDB{}
	if err := inner.CreateObjectStore(ctx, "tasks", indexeddb.ObjectStoreSchema{}); err != nil {
		t.Fatalf("CreateObjectStore: %v", err)
	}
	db := newIndexedDBStoreAllowlist(inner, indexedDBStoreAllowlistOptions{
		AllowedStores: []string{"tasks"},
	})

	tx, err := db.Transaction(ctx, []string{"tasks"}, indexeddb.TransactionReadwrite, indexeddb.TransactionOptions{})
	if err != nil {
		t.Fatalf("Transaction: %v", err)
	}
	if err := tx.ObjectStore("tasks").Put(ctx, indexeddb.Record{"id": "task-1"}); err != nil {
		t.Fatalf("Put allowed task: %v", err)
	}
	if err := tx.ObjectStore("notes").Put(ctx, indexeddb.Record{"id": "note-1"}); !errors.Is(err, indexeddb.ErrNotFound) {
		t.Fatalf("Put disallowed store error = %v, want indexeddb.ErrNotFound", err)
	}
	if err := tx.Commit(ctx); !errors.Is(err, indexeddb.ErrTransactionDone) {
		t.Fatalf("Commit after disallowed store error = %v, want indexeddb.ErrTransactionDone", err)
	}
	if _, err := db.ObjectStore("tasks").Get(ctx, "task-1"); !errors.Is(err, indexeddb.ErrNotFound) {
		t.Fatalf("task-1 after aborted transaction error = %v, want indexeddb.ErrNotFound", err)
	}
}

func TestIndexedDBStoreAllowlistFactoryCapability(t *testing.T) {
	t.Parallel()

	t.Run("legacy_inner_does_not_satisfy_factory", func(t *testing.T) {
		t.Parallel()

		db := newIndexedDBStoreAllowlist(&legacyOnlyIndexedDB{inner: &coretesting.StubIndexedDB{}}, indexedDBStoreAllowlistOptions{
			AllowedStores: []string{"tasks"},
		})
		if _, ok := db.(indexeddb.Factory); ok {
			t.Fatal("allowlisted legacy IndexedDB unexpectedly satisfies indexeddb.Factory")
		}
	})

	t.Run("factory_inner_satisfies_factory_and_enforces_upgrade_allowlist", func(t *testing.T) {
		t.Parallel()

		db := newIndexedDBStoreAllowlist(&coretesting.StubIndexedDB{}, indexedDBStoreAllowlistOptions{
			AllowedStores: []string{"tasks"},
		})
		factory, ok := db.(indexeddb.Factory)
		if !ok {
			t.Fatal("allowlisted factory IndexedDB does not satisfy indexeddb.Factory")
		}
		version := uint64(1)
		_, err := factory.Open(context.Background(), "app", indexeddb.OpenOptions{
			Version: &version,
			Upgrade: func(ctx context.Context, upgrade indexeddb.UpgradeContext) error {
				if err := upgrade.CreateObjectStore(ctx, "tasks", indexeddb.ObjectStoreSchema{}); err != nil {
					return err
				}
				return upgrade.CreateObjectStore(ctx, "notes", indexeddb.ObjectStoreSchema{})
			},
		})
		if !errors.Is(err, indexeddb.ErrNotFound) {
			t.Fatalf("disallowed upgrade CreateObjectStore error = %v, want ErrNotFound", err)
		}
	})
}

func TestIndexedDBStoreAllowlistFilterStoreNamesDoesNotMutateInput(t *testing.T) {
	t.Parallel()

	allowlist := &indexedDBStoreAllowlist{allowed: map[string]struct{}{"tasks": {}}}
	names := []string{"notes", "tasks", "events"}
	filtered := allowlist.filterStoreNames(names)

	if got, want := filtered, []string{"tasks"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("filtered names = %v, want %v", got, want)
	}
	if got, want := names, []string{"notes", "tasks", "events"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("input names mutated to %v, want %v", got, want)
	}
}

type legacyOnlyIndexedDB struct {
	inner indexeddb.IndexedDB
}

func (d *legacyOnlyIndexedDB) ObjectStore(name string) indexeddb.ObjectStore {
	return d.inner.ObjectStore(name)
}

func (d *legacyOnlyIndexedDB) Transaction(ctx context.Context, stores []string, mode indexeddb.TransactionMode, opts indexeddb.TransactionOptions) (indexeddb.Transaction, error) {
	return d.inner.Transaction(ctx, stores, mode, opts)
}

func (d *legacyOnlyIndexedDB) CreateObjectStore(ctx context.Context, name string, schema indexeddb.ObjectStoreSchema) error {
	return d.inner.CreateObjectStore(ctx, name, schema)
}

func (d *legacyOnlyIndexedDB) DeleteObjectStore(ctx context.Context, name string) error {
	return d.inner.DeleteObjectStore(ctx, name)
}

func (d *legacyOnlyIndexedDB) Ping(ctx context.Context) error { return d.inner.Ping(ctx) }

func (d *legacyOnlyIndexedDB) Close() error { return d.inner.Close() }
