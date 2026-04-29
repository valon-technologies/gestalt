package bootstrap

import (
	"context"
	"errors"
	"testing"

	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
)

func TestPluginIndexedDBTransactionMissingStoreAbortsInnerTransaction(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	inner := &coretesting.StubIndexedDB{}
	if err := inner.CreateObjectStore(ctx, "plugin_tasks", indexeddb.ObjectStoreSchema{}); err != nil {
		t.Fatalf("CreateObjectStore: %v", err)
	}
	db := newPluginIndexedDBTransport(inner, pluginIndexedDBTransportOptions{
		StorePrefix:   "plugin_",
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
