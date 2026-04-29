package coretesting

import (
	"context"
	"errors"
	"testing"

	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

func TestStubCursorAdvanceSkipsRequestedRows(t *testing.T) {
	t.Parallel()

	db := &StubIndexedDB{}
	ctx := context.Background()
	if err := db.CreateObjectStore(ctx, "items", indexeddb.ObjectStoreSchema{}); err != nil {
		t.Fatalf("CreateObjectStore: %v", err)
	}

	store := db.ObjectStore("items")
	for _, record := range []indexeddb.Record{
		{"id": "a"},
		{"id": "b"},
		{"id": "c"},
	} {
		if err := store.Add(ctx, record); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}

	cursor, err := store.OpenCursor(ctx, nil, indexeddb.CursorNext)
	if err != nil {
		t.Fatalf("OpenCursor: %v", err)
	}
	defer func() { _ = cursor.Close() }()

	if !cursor.Advance(1) {
		t.Fatalf("Advance(1) returned false")
	}
	if cursor.PrimaryKey() != "b" {
		t.Fatalf("PrimaryKey after Advance(1) = %q, want b", cursor.PrimaryKey())
	}
}

func TestStubIndexCursorOrdersBinaryKeysBytewise(t *testing.T) {
	t.Parallel()

	db := &StubIndexedDB{}
	ctx := context.Background()
	if err := db.CreateObjectStore(ctx, "items", indexeddb.ObjectStoreSchema{
		Indexes: []indexeddb.IndexSchema{{Name: "by_blob", KeyPath: []string{"blob"}}},
	}); err != nil {
		t.Fatalf("CreateObjectStore: %v", err)
	}

	store := db.ObjectStore("items")
	for _, record := range []indexeddb.Record{
		{"id": "a", "blob": []byte{10}},
		{"id": "b", "blob": []byte{2}},
		{"id": "c", "blob": []byte{2, 0}},
	} {
		if err := store.Add(ctx, record); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}

	cursor, err := store.Index("by_blob").OpenCursor(ctx, nil, indexeddb.CursorNext)
	if err != nil {
		t.Fatalf("OpenCursor: %v", err)
	}
	defer func() { _ = cursor.Close() }()

	var keys []string
	for cursor.Continue() {
		keys = append(keys, cursor.PrimaryKey())
	}
	want := []string{"b", "c", "a"}
	if len(keys) != len(want) {
		t.Fatalf("got %d keys, want %d: %v", len(keys), len(want), keys)
	}
	for i, key := range want {
		if keys[i] != key {
			t.Fatalf("keys[%d] = %q, want %q (full order %v)", i, keys[i], key, keys)
		}
	}
}

func TestStubTransactionAbortsOnOperationError(t *testing.T) {
	t.Parallel()

	db := &StubIndexedDB{}
	ctx := context.Background()
	if err := db.CreateObjectStore(ctx, "users", indexeddb.ObjectStoreSchema{
		Indexes: []indexeddb.IndexSchema{{Name: "by_email", KeyPath: []string{"email"}, Unique: true}},
	}); err != nil {
		t.Fatalf("CreateObjectStore: %v", err)
	}
	store := db.ObjectStore("users")
	if err := store.Add(ctx, indexeddb.Record{"id": "user-1", "email": "same@example.com"}); err != nil {
		t.Fatalf("seed user-1: %v", err)
	}

	tx, err := db.Transaction(ctx, []string{"users"}, indexeddb.TransactionReadwrite, indexeddb.TransactionOptions{})
	if err != nil {
		t.Fatalf("Transaction: %v", err)
	}
	txStore := tx.ObjectStore("users")
	if err := txStore.Add(ctx, indexeddb.Record{"id": "user-2", "email": "same@example.com"}); !errors.Is(err, indexeddb.ErrAlreadyExists) {
		t.Fatalf("conflicting Add error = %v, want indexeddb.ErrAlreadyExists", err)
	}
	if err := txStore.Put(ctx, indexeddb.Record{"id": "user-3", "email": "unique@example.com"}); !errors.Is(err, indexeddb.ErrAlreadyExists) {
		t.Fatalf("Put after failed Add error = %v, want original indexeddb.ErrAlreadyExists", err)
	}
	if err := tx.Commit(ctx); !errors.Is(err, indexeddb.ErrAlreadyExists) {
		t.Fatalf("Commit after failed Add error = %v, want original indexeddb.ErrAlreadyExists", err)
	}
	if _, err := store.Get(ctx, "user-3"); !errors.Is(err, indexeddb.ErrNotFound) {
		t.Fatalf("user-3 after aborted transaction error = %v, want indexeddb.ErrNotFound", err)
	}
}
