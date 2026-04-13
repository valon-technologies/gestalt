package coretesting

import (
	"context"
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
