package coretesting

import (
	"context"
	"errors"
	"testing"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
)

func TestStubCursorAdvanceSkipsRequestedRows(t *testing.T) {
	t.Parallel()

	db := &StubIndexedDB{}
	ctx := context.Background()
	if _, err := db.CreateObjectStore(ctx, "items", idb.ObjectStoreOptions{}); err != nil {
		t.Fatalf("CreateObjectStore: %v", err)
	}

	store := db.ObjectStore("items")
	for _, record := range []idb.Record{
		{"id": "a"},
		{"id": "b"},
		{"id": "c"},
	} {
		if err := store.Add(ctx, record); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}

	cursor, err := store.OpenCursor(ctx, nil, idb.CursorNext)
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
	if _, err := db.CreateObjectStore(ctx, "items", idb.ObjectStoreOptions{
		Indexes: []idb.IndexSchema{{Name: "by_blob", KeyPath: []string{"blob"}}},
	}); err != nil {
		t.Fatalf("CreateObjectStore: %v", err)
	}

	store := db.ObjectStore("items")
	for _, record := range []idb.Record{
		{"id": "a", "blob": []byte{10}},
		{"id": "b", "blob": []byte{2}},
		{"id": "c", "blob": []byte{2, 0}},
	} {
		if err := store.Add(ctx, record); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}

	cursor, err := store.Index("by_blob").OpenCursor(ctx, nil, idb.CursorNext)
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

func TestStubIndexCompoundArrayRange(t *testing.T) {
	t.Parallel()

	db := &StubIndexedDB{}
	ctx := context.Background()
	if _, err := db.CreateObjectStore(ctx, "events", idb.ObjectStoreOptions{
		Indexes: []idb.IndexSchema{{Name: "by_vendor_date", KeyPath: []string{"vendor", "date"}}},
	}); err != nil {
		t.Fatalf("CreateObjectStore: %v", err)
	}

	store := db.ObjectStore("events")
	for _, record := range []idb.Record{
		{"id": "1", "vendor": "claude_code", "date": "2026-03-15"},
		{"id": "2", "vendor": "claude_code", "date": "2026-04-10"},
		{"id": "3", "vendor": "claude_code", "date": "2026-04-30"},
		{"id": "4", "vendor": "claude_code", "date": "2026-05-01"},
		{"id": "5", "vendor": "other", "date": "2026-04-15"},
	} {
		if err := store.Add(ctx, record); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}

	rangeQuery := idb.Bound(
		[]any{"claude_code", "2026-04-01"},
		[]any{"claude_code", "2026-04-30"},
		false, false,
	)

	records, err := store.Index("by_vendor_date").GetAll(ctx, rangeQuery)
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("GetAll len = %d, want 2 (April claude_code rows)", len(records))
	}
	ids := []string{records[0]["id"].(string), records[1]["id"].(string)}
	if ids[0] != "2" || ids[1] != "3" {
		t.Fatalf("GetAll ids = %v, want [2 3]", ids)
	}
}

func TestStubCursorContinueToKeyRejectsInvalidKey(t *testing.T) {
	t.Parallel()

	db := &StubIndexedDB{}
	ctx := context.Background()
	if _, err := db.CreateObjectStore(ctx, "items", idb.ObjectStoreOptions{}); err != nil {
		t.Fatalf("CreateObjectStore: %v", err)
	}

	cursor, err := db.ObjectStore("items").OpenCursor(ctx, nil, idb.CursorNext)
	if err != nil {
		t.Fatalf("OpenCursor: %v", err)
	}
	defer func() { _ = cursor.Close() }()

	if cursor.ContinueToKey(make(chan int)) {
		t.Fatal("ContinueToKey returned true")
	}
	if cursor.Err() == nil {
		t.Fatal("Err() = nil, want error")
	}
}

func TestStubTransactionAbortsOnOperationError(t *testing.T) {
	t.Parallel()

	db := &StubIndexedDB{}
	ctx := context.Background()
	if _, err := db.CreateObjectStore(ctx, "users", idb.ObjectStoreOptions{
		Indexes: []idb.IndexSchema{{Name: "by_email", KeyPath: []string{"email"}, Unique: true}},
	}); err != nil {
		t.Fatalf("CreateObjectStore: %v", err)
	}
	store := db.ObjectStore("users")
	if err := store.Add(ctx, idb.Record{"id": "user-1", "email": "same@example.com"}); err != nil {
		t.Fatalf("seed user-1: %v", err)
	}

	tx, err := db.Transaction(ctx, []string{"users"}, idb.TransactionReadwrite, idb.TransactionOptions{})
	if err != nil {
		t.Fatalf("Transaction: %v", err)
	}
	txStore := tx.ObjectStore("users")
	if err := txStore.Add(ctx, idb.Record{"id": "user-2", "email": "same@example.com"}); !errors.Is(err, idb.ErrAlreadyExists) {
		t.Fatalf("conflicting Add error = %v, want idb.ErrAlreadyExists", err)
	}
	if err := txStore.Put(ctx, idb.Record{"id": "user-3", "email": "unique@example.com"}); !errors.Is(err, idb.ErrAlreadyExists) {
		t.Fatalf("Put after failed Add error = %v, want original idb.ErrAlreadyExists", err)
	}
	if err := tx.Commit(ctx); !errors.Is(err, idb.ErrAlreadyExists) {
		t.Fatalf("Commit after failed Add error = %v, want original idb.ErrAlreadyExists", err)
	}
	if _, err := store.Get(ctx, "user-3"); !errors.Is(err, idb.ErrNotFound) {
		t.Fatalf("user-3 after aborted transaction error = %v, want idb.ErrNotFound", err)
	}
}

func TestStubUniqueIndexOmitsMissingAndNullKeys(t *testing.T) {
	t.Parallel()

	db := &StubIndexedDB{}
	ctx := context.Background()
	if _, err := db.CreateObjectStore(ctx, "items", idb.ObjectStoreOptions{
		Indexes: []idb.IndexSchema{{Name: "by_external_id", KeyPath: []string{"external_id"}, Unique: true}},
	}); err != nil {
		t.Fatalf("CreateObjectStore: %v", err)
	}
	store := db.ObjectStore("items")
	for _, record := range []idb.Record{
		{"id": "missing-1"},
		{"id": "missing-2"},
		{"id": "null-1", "external_id": nil},
		{"id": "null-2", "external_id": nil},
		{"id": "present", "external_id": "employee-1"},
	} {
		if err := store.Add(ctx, record); err != nil {
			t.Fatalf("Add(%q): %v", record["id"], err)
		}
	}
	if err := store.Add(ctx, idb.Record{"id": "duplicate", "external_id": "employee-1"}); !errors.Is(err, idb.ErrAlreadyExists) {
		t.Fatalf("duplicate present key error = %v, want idb.ErrAlreadyExists", err)
	}
	records, err := store.Index("by_external_id").GetAll(ctx, nil)
	if err != nil {
		t.Fatalf("index GetAll: %v", err)
	}
	if len(records) != 1 || records[0]["id"] != "present" {
		t.Fatalf("indexed records = %#v, want only present key", records)
	}
}
