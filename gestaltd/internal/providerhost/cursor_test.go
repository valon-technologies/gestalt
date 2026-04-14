package providerhost

import (
	"context"
	"errors"
	"testing"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func newCursorTestDB(t *testing.T) (*coretesting.StubIndexedDB, indexeddb.IndexedDB) {
	t.Helper()
	stub := &coretesting.StubIndexedDB{}

	schema := indexeddb.ObjectStoreSchema{
		Indexes: []indexeddb.IndexSchema{
			{Name: "by_status", KeyPath: []string{"status"}, Unique: false},
			{Name: "by_email", KeyPath: []string{"email"}, Unique: true},
		},
	}
	ctx := context.Background()
	if err := stub.CreateObjectStore(ctx, "items", schema); err != nil {
		t.Fatal(err)
	}

	store := stub.ObjectStore("items")
	records := []indexeddb.Record{
		{"id": "a", "name": "Alice", "status": "active", "email": "alice@test.com"},
		{"id": "b", "name": "Bob", "status": "active", "email": "bob@test.com"},
		{"id": "c", "name": "Carol", "status": "inactive", "email": "carol@test.com"},
		{"id": "d", "name": "Dave", "status": "active", "email": "dave@test.com"},
	}
	for _, r := range records {
		if err := store.Add(ctx, r); err != nil {
			t.Fatal(err)
		}
	}

	conn := newBufconnConn(t, func(srv *grpc.Server) {
		proto.RegisterIndexedDBServer(srv, NewIndexedDBServer(stub, ""))
	})
	remote := &remoteIndexedDB{
		client: proto.NewIndexedDBClient(conn),
	}
	return stub, remote
}

// --- Basic iteration ---

func TestCursor_ForwardIteration(t *testing.T) {
	t.Parallel()

	_, db := newCursorTestDB(t)
	ctx := context.Background()

	cursor, err := db.ObjectStore("items").OpenCursor(ctx, nil, indexeddb.CursorNext)
	if err != nil {
		t.Fatalf("OpenCursor: %v", err)
	}
	defer func() { _ = cursor.Close() }()

	var keys []string
	for cursor.Continue() {
		keys = append(keys, cursor.PrimaryKey())
	}
	if err := cursor.Err(); err != nil {
		t.Fatalf("Err: %v", err)
	}
	if len(keys) != 4 {
		t.Fatalf("got %d records, want 4", len(keys))
	}
	// Primary keys should be sorted ascending
	for i := 1; i < len(keys); i++ {
		if keys[i] <= keys[i-1] {
			t.Errorf("keys not sorted: %v", keys)
			break
		}
	}
}

func TestCursor_EmptyCursor(t *testing.T) {
	t.Parallel()

	stub := &coretesting.StubIndexedDB{}
	ctx := context.Background()
	_ = stub.CreateObjectStore(ctx, "empty", indexeddb.ObjectStoreSchema{})

	conn := newBufconnConn(t, func(srv *grpc.Server) {
		proto.RegisterIndexedDBServer(srv, NewIndexedDBServer(stub, ""))
	})
	remote := &remoteIndexedDB{client: proto.NewIndexedDBClient(conn)}

	cursor, err := remote.ObjectStore("empty").OpenCursor(ctx, nil, indexeddb.CursorNext)
	if err != nil {
		t.Fatalf("OpenCursor: %v", err)
	}
	defer func() { _ = cursor.Close() }()

	if cursor.Continue() {
		t.Fatal("Continue returned true on empty store")
	}
}

func TestCursor_KeysOnly(t *testing.T) {
	t.Parallel()

	_, db := newCursorTestDB(t)
	ctx := context.Background()

	cursor, err := db.ObjectStore("items").OpenKeyCursor(ctx, nil, indexeddb.CursorNext)
	if err != nil {
		t.Fatalf("OpenKeyCursor: %v", err)
	}
	defer func() { _ = cursor.Close() }()

	if !cursor.Continue() {
		t.Fatal("Continue returned false")
	}

	if cursor.PrimaryKey() == "" {
		t.Error("PrimaryKey is empty")
	}

	_, err = cursor.Value()
	if !errors.Is(err, indexeddb.ErrKeysOnly) {
		t.Errorf("Value() error = %v, want ErrKeysOnly", err)
	}
}

func TestCursor_ExhaustedState(t *testing.T) {
	t.Parallel()

	_, db := newCursorTestDB(t)
	ctx := context.Background()

	cursor, err := db.ObjectStore("items").OpenCursor(ctx, nil, indexeddb.CursorNext)
	if err != nil {
		t.Fatalf("OpenCursor: %v", err)
	}
	defer func() { _ = cursor.Close() }()

	for cursor.Continue() {
	}

	if cursor.Key() != nil {
		t.Errorf("Key() after exhaustion = %v, want nil", cursor.Key())
	}
	if cursor.PrimaryKey() != "" {
		t.Errorf("PrimaryKey() after exhaustion = %q, want empty", cursor.PrimaryKey())
	}
	_, err = cursor.Value()
	if !errors.Is(err, indexeddb.ErrNotFound) {
		t.Errorf("Value() after exhaustion = %v, want ErrNotFound", err)
	}
}

// --- Positional mutation ---

func TestCursor_DeleteAtPosition(t *testing.T) {
	t.Parallel()

	_, db := newCursorTestDB(t)
	ctx := context.Background()

	cursor, err := db.ObjectStore("items").OpenCursor(ctx, nil, indexeddb.CursorNext)
	if err != nil {
		t.Fatalf("OpenCursor: %v", err)
	}
	defer func() { _ = cursor.Close() }()

	// Advance to first record and delete it
	if !cursor.Continue() {
		t.Fatal("Continue returned false")
	}
	deletedKey := cursor.PrimaryKey()
	if err := cursor.Delete(); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Cursor should still be usable
	count := 0
	for cursor.Continue() {
		if cursor.PrimaryKey() == deletedKey {
			t.Errorf("deleted record %q still appears", deletedKey)
		}
		count++
	}
	if count != 3 {
		t.Errorf("remaining records = %d, want 3", count)
	}
}

func TestCursor_UpdateAtPosition(t *testing.T) {
	t.Parallel()

	_, db := newCursorTestDB(t)
	ctx := context.Background()

	cursor, err := db.ObjectStore("items").OpenCursor(ctx, nil, indexeddb.CursorNext)
	if err != nil {
		t.Fatalf("OpenCursor: %v", err)
	}
	defer func() { _ = cursor.Close() }()

	if !cursor.Continue() {
		t.Fatal("Continue returned false")
	}
	pk := cursor.PrimaryKey()

	updated := indexeddb.Record{"id": pk, "name": "Updated", "status": "updated", "email": "updated@test.com"}
	if err := cursor.Update(updated); err != nil {
		t.Fatalf("Update: %v", err)
	}
	curRec, err := cursor.Value()
	if err != nil {
		t.Fatalf("Value after Update: %v", err)
	}
	if curRec["name"] != "Updated" {
		t.Errorf("cursor.Value().name = %v, want Updated", curRec["name"])
	}

	// Verify via direct read
	rec, err := db.ObjectStore("items").Get(ctx, pk)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if rec["name"] != "Updated" {
		t.Errorf("name = %v, want Updated", rec["name"])
	}
}

func TestCursor_MutationOnExhaustedCursor(t *testing.T) {
	t.Parallel()

	_, db := newCursorTestDB(t)
	ctx := context.Background()

	cursor, err := db.ObjectStore("items").OpenCursor(ctx, nil, indexeddb.CursorNext)
	if err != nil {
		t.Fatalf("OpenCursor: %v", err)
	}
	defer func() { _ = cursor.Close() }()

	for cursor.Continue() {
	}

	if err := cursor.Delete(); !errors.Is(err, indexeddb.ErrNotFound) {
		t.Errorf("Delete after exhaustion = %v, want ErrNotFound", err)
	}
	if err := cursor.Update(indexeddb.Record{"id": "x"}); !errors.Is(err, indexeddb.ErrNotFound) {
		t.Errorf("Update after exhaustion = %v, want ErrNotFound", err)
	}
}

// --- Direction and unique ---

func TestCursor_ReverseIteration(t *testing.T) {
	t.Parallel()

	_, db := newCursorTestDB(t)
	ctx := context.Background()

	cursor, err := db.ObjectStore("items").OpenCursor(ctx, nil, indexeddb.CursorPrev)
	if err != nil {
		t.Fatalf("OpenCursor: %v", err)
	}
	defer func() { _ = cursor.Close() }()

	var keys []string
	for cursor.Continue() {
		keys = append(keys, cursor.PrimaryKey())
	}
	if len(keys) != 4 {
		t.Fatalf("got %d records, want 4", len(keys))
	}
	for i := 1; i < len(keys); i++ {
		if keys[i] >= keys[i-1] {
			t.Errorf("keys not reverse sorted: %v", keys)
			break
		}
	}
}

func TestCursor_NextUnique(t *testing.T) {
	t.Parallel()

	_, db := newCursorTestDB(t)
	ctx := context.Background()

	// "by_status" index: 3 records have "active", 1 has "inactive"
	cursor, err := db.ObjectStore("items").Index("by_status").OpenCursor(ctx, nil, indexeddb.CursorNextUnique)
	if err != nil {
		t.Fatalf("OpenCursor: %v", err)
	}
	defer func() { _ = cursor.Close() }()

	var keys []any
	for cursor.Continue() {
		keys = append(keys, cursor.Key())
	}
	if len(keys) != 2 {
		t.Fatalf("got %d unique keys, want 2: %v", len(keys), keys)
	}
}

func TestCursor_PrevUnique(t *testing.T) {
	t.Parallel()

	_, db := newCursorTestDB(t)
	ctx := context.Background()

	cursor, err := db.ObjectStore("items").Index("by_status").OpenCursor(ctx, nil, indexeddb.CursorPrevUnique)
	if err != nil {
		t.Fatalf("OpenCursor: %v", err)
	}
	defer func() { _ = cursor.Close() }()

	var keys []any
	for cursor.Continue() {
		keys = append(keys, cursor.Key())
	}
	if len(keys) != 2 {
		t.Fatalf("got %d unique keys, want 2: %v", len(keys), keys)
	}
}

func TestCursor_KeyRange(t *testing.T) {
	t.Parallel()

	_, db := newCursorTestDB(t)
	ctx := context.Background()

	kr := indexeddb.Only("b")
	cursor, err := db.ObjectStore("items").OpenCursor(ctx, kr, indexeddb.CursorNext)
	if err != nil {
		t.Fatalf("OpenCursor: %v", err)
	}
	defer func() { _ = cursor.Close() }()

	var keys []string
	for cursor.Continue() {
		keys = append(keys, cursor.PrimaryKey())
	}
	if len(keys) != 1 || keys[0] != "b" {
		t.Errorf("keys = %v, want [b]", keys)
	}
}

func TestCursor_IndexKeyRangeSingleField(t *testing.T) {
	t.Parallel()

	_, db := newCursorTestDB(t)
	ctx := context.Background()

	cursor, err := db.ObjectStore("items").Index("by_status").OpenCursor(ctx, indexeddb.Only("active"), indexeddb.CursorNext)
	if err != nil {
		t.Fatalf("OpenCursor: %v", err)
	}
	defer func() { _ = cursor.Close() }()

	var keys []string
	for cursor.Continue() {
		keys = append(keys, cursor.PrimaryKey())
	}
	if len(keys) != 3 {
		t.Fatalf("got %d active records, want 3: %v", len(keys), keys)
	}
	for _, key := range keys {
		rec, err := db.ObjectStore("items").Get(ctx, key)
		if err != nil {
			t.Fatalf("Get(%q): %v", key, err)
		}
		if rec["status"] != "active" {
			t.Fatalf("record %q status = %v, want active", key, rec["status"])
		}
	}
}

// --- Protocol edge cases ---

func TestCursor_Advance(t *testing.T) {
	t.Parallel()

	_, db := newCursorTestDB(t)
	ctx := context.Background()

	cursor, err := db.ObjectStore("items").OpenCursor(ctx, nil, indexeddb.CursorNext)
	if err != nil {
		t.Fatalf("OpenCursor: %v", err)
	}
	defer func() { _ = cursor.Close() }()

	// Advance(2) should skip the first 2 records and land on the next one.
	if !cursor.Advance(2) {
		t.Fatal("Advance(2) returned false")
	}
	pk := cursor.PrimaryKey()
	if pk != "c" {
		t.Errorf("PrimaryKey after Advance(2) = %q, want c", pk)
	}
}

func TestCursor_ContinueToKey(t *testing.T) {
	t.Parallel()

	_, db := newCursorTestDB(t)
	ctx := context.Background()

	cursor, err := db.ObjectStore("items").OpenCursor(ctx, nil, indexeddb.CursorNext)
	if err != nil {
		t.Fatalf("OpenCursor: %v", err)
	}
	defer func() { _ = cursor.Close() }()

	if !cursor.ContinueToKey("c") {
		t.Fatal("ContinueToKey returned false")
	}
	if cursor.PrimaryKey() != "c" {
		t.Errorf("PrimaryKey = %q, want c", cursor.PrimaryKey())
	}
}

func TestCursor_ReverseContinueToKey(t *testing.T) {
	t.Parallel()

	_, db := newCursorTestDB(t)
	ctx := context.Background()

	cursor, err := db.ObjectStore("items").OpenCursor(ctx, nil, indexeddb.CursorPrev)
	if err != nil {
		t.Fatalf("OpenCursor: %v", err)
	}
	defer func() { _ = cursor.Close() }()

	if !cursor.ContinueToKey("c") {
		t.Fatalf("ContinueToKey returned false err=%v", cursor.Err())
	}
	if cursor.PrimaryKey() != "c" {
		t.Fatalf("PrimaryKey = %q, want c", cursor.PrimaryKey())
	}
}

func TestCursor_AdvanceRejectsNonPositiveCounts(t *testing.T) {
	t.Parallel()

	_, db := newCursorTestDB(t)
	ctx := context.Background()

	cursor, err := db.ObjectStore("items").OpenCursor(ctx, nil, indexeddb.CursorNext)
	if err != nil {
		t.Fatalf("OpenCursor: %v", err)
	}
	defer func() { _ = cursor.Close() }()

	if cursor.Advance(0) {
		t.Fatal("Advance(0) returned true")
	}
	err = cursor.Err()
	if err == nil {
		t.Fatal("Err() = nil, want invalid argument")
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("Err() = %T, want gRPC status", err)
	}
	if st.Code() != codes.InvalidArgument {
		t.Fatalf("Err() code = %v, want %v", st.Code(), codes.InvalidArgument)
	}
}

// --- Index cursor ---

func TestCursor_IndexIteration(t *testing.T) {
	t.Parallel()

	_, db := newCursorTestDB(t)
	ctx := context.Background()

	// Filter by status=active (3 records)
	cursor, err := db.ObjectStore("items").Index("by_status").OpenCursor(ctx, nil, indexeddb.CursorNext, "active")
	if err != nil {
		t.Fatalf("OpenCursor: %v", err)
	}
	defer func() { _ = cursor.Close() }()

	count := 0
	for cursor.Continue() {
		count++
		rec, err := cursor.Value()
		if err != nil {
			t.Fatalf("Value: %v", err)
		}
		if rec["status"] != "active" {
			t.Errorf("record status = %v, want active", rec["status"])
		}
	}
	if count != 3 {
		t.Errorf("got %d active records, want 3", count)
	}
}

func TestCursor_IndexKeyReturnsIndexValues(t *testing.T) {
	t.Parallel()

	_, db := newCursorTestDB(t)
	ctx := context.Background()

	cursor, err := db.ObjectStore("items").Index("by_status").OpenCursor(ctx, nil, indexeddb.CursorNext)
	if err != nil {
		t.Fatalf("OpenCursor: %v", err)
	}
	defer func() { _ = cursor.Close() }()

	if !cursor.Continue() {
		t.Fatal("Continue returned false")
	}

	key := cursor.Key()
	keyArr, ok := key.([]any)
	if !ok {
		t.Fatalf("Key() type = %T, want []any", key)
	}
	if len(keyArr) != 1 {
		t.Fatalf("Key() len = %d, want 1", len(keyArr))
	}
	keyStr, _ := keyArr[0].(string)
	if keyStr != "active" && keyStr != "inactive" {
		t.Errorf("Key() = %q, want active or inactive", keyStr)
	}

	// PrimaryKey should be a record id, not the index key
	pk := cursor.PrimaryKey()
	if pk != "a" && pk != "b" && pk != "c" && pk != "d" {
		t.Errorf("PrimaryKey() = %q, want a/b/c/d", pk)
	}
}

func TestCursor_IndexContinueToKeyRoundTrip(t *testing.T) {
	t.Parallel()

	stub := &coretesting.StubIndexedDB{}
	ctx := context.Background()
	schema := indexeddb.ObjectStoreSchema{
		Indexes: []indexeddb.IndexSchema{{Name: "by_num", KeyPath: []string{"n"}}},
	}
	if err := stub.CreateObjectStore(ctx, "items", schema); err != nil {
		t.Fatal(err)
	}

	store := stub.ObjectStore("items")
	for _, r := range []indexeddb.Record{
		{"id": "a", "n": 1},
		{"id": "b", "n": 2},
		{"id": "c", "n": 3},
	} {
		if err := store.Add(ctx, r); err != nil {
			t.Fatal(err)
		}
	}

	conn := newBufconnConn(t, func(srv *grpc.Server) {
		proto.RegisterIndexedDBServer(srv, NewIndexedDBServer(stub, ""))
	})
	remote := &remoteIndexedDB{client: proto.NewIndexedDBClient(conn)}

	cursor, err := remote.ObjectStore("items").Index("by_num").OpenCursor(ctx, nil, indexeddb.CursorNext)
	if err != nil {
		t.Fatalf("OpenCursor: %v", err)
	}
	defer func() { _ = cursor.Close() }()

	if !cursor.Continue() {
		t.Fatal("Continue returned false")
	}
	if !cursor.ContinueToKey(cursor.Key()) {
		t.Fatalf("ContinueToKey returned false err=%v", cursor.Err())
	}
	if cursor.PrimaryKey() != "b" {
		t.Fatalf("PrimaryKey = %q, want b", cursor.PrimaryKey())
	}
}

func TestCursor_StubSingleFieldIndexKeyMatchesRemoteShape(t *testing.T) {
	t.Parallel()

	stub, _ := newCursorTestDB(t)
	ctx := context.Background()

	cursor, err := stub.ObjectStore("items").Index("by_status").OpenCursor(ctx, nil, indexeddb.CursorNext)
	if err != nil {
		t.Fatalf("OpenCursor: %v", err)
	}
	defer func() { _ = cursor.Close() }()

	if !cursor.Continue() {
		t.Fatal("Continue returned false")
	}

	key, ok := cursor.Key().([]any)
	if !ok {
		t.Fatalf("Key() type = %T, want []any", cursor.Key())
	}
	if len(key) != 1 {
		t.Fatalf("Key() len = %d, want 1", len(key))
	}
}

// --- Exhaustion protocol tests ---

func TestCursor_EmptyResultSetDoneOnly(t *testing.T) {
	t.Parallel()
	stub := &coretesting.StubIndexedDB{}
	ctx := context.Background()
	_ = stub.CreateObjectStore(ctx, "empty", indexeddb.ObjectStoreSchema{})

	conn := newBufconnConn(t, func(srv *grpc.Server) {
		proto.RegisterIndexedDBServer(srv, NewIndexedDBServer(stub, ""))
	})
	remote := &remoteIndexedDB{client: proto.NewIndexedDBClient(conn)}

	// Value cursor on empty store
	cursor, err := remote.ObjectStore("empty").OpenCursor(ctx, nil, indexeddb.CursorNext)
	if err != nil {
		t.Fatalf("OpenCursor: %v", err)
	}
	defer func() { _ = cursor.Close() }()

	if cursor.Continue() {
		t.Fatal("Continue returned true on empty store")
	}
	if cursor.Err() != nil {
		t.Fatalf("unexpected error: %v", cursor.Err())
	}
	// Key-only cursor on empty store
	kcursor, err := remote.ObjectStore("empty").OpenKeyCursor(ctx, nil, indexeddb.CursorNext)
	if err != nil {
		t.Fatalf("OpenKeyCursor: %v", err)
	}
	defer func() { _ = kcursor.Close() }()

	if kcursor.Continue() {
		t.Fatal("key cursor Continue returned true on empty store")
	}
	if kcursor.Err() != nil {
		t.Fatalf("key cursor unexpected error: %v", kcursor.Err())
	}
}

func TestCursor_ValueCursorExhaustionNoExtraEntry(t *testing.T) {
	t.Parallel()
	_, db := newCursorTestDB(t)
	ctx := context.Background()

	cursor, err := db.ObjectStore("items").OpenCursor(ctx, nil, indexeddb.CursorNext)
	if err != nil {
		t.Fatalf("OpenCursor: %v", err)
	}
	defer func() { _ = cursor.Close() }()

	count := 0
	for cursor.Continue() {
		count++
	}
	if cursor.Err() != nil {
		t.Fatalf("Err after exhaustion: %v", cursor.Err())
	}
	if count != 4 {
		t.Fatalf("got %d records, want 4", count)
	}
	// Verify no INTERNAL error from cursorEntryToProto on exhausted cursor
	if cursor.Key() != nil {
		t.Error("Key should be nil after exhaustion")
	}
}

func TestCursor_KeyOnlyCursorExhaustionNoSpuriousEntry(t *testing.T) {
	t.Parallel()
	_, db := newCursorTestDB(t)
	ctx := context.Background()

	cursor, err := db.ObjectStore("items").OpenKeyCursor(ctx, nil, indexeddb.CursorNext)
	if err != nil {
		t.Fatalf("OpenKeyCursor: %v", err)
	}
	defer func() { _ = cursor.Close() }()

	count := 0
	for cursor.Continue() {
		count++
	}
	if cursor.Err() != nil {
		t.Fatalf("Err after exhaustion: %v", cursor.Err())
	}
	if count != 4 {
		t.Fatalf("got %d records, want 4", count)
	}
}

func TestCursor_ContinueToKeyBeyondEnd(t *testing.T) {
	t.Parallel()
	_, db := newCursorTestDB(t)
	ctx := context.Background()

	cursor, err := db.ObjectStore("items").OpenCursor(ctx, nil, indexeddb.CursorNext)
	if err != nil {
		t.Fatalf("OpenCursor: %v", err)
	}
	defer func() { _ = cursor.Close() }()

	// Seek beyond all keys
	if cursor.ContinueToKey("z") {
		t.Fatal("ContinueToKey beyond end returned true")
	}
	if cursor.Err() != nil {
		t.Fatalf("Err: %v", cursor.Err())
	}
}

func TestCursor_AdvancePastEnd(t *testing.T) {
	t.Parallel()
	_, db := newCursorTestDB(t)
	ctx := context.Background()

	cursor, err := db.ObjectStore("items").OpenCursor(ctx, nil, indexeddb.CursorNext)
	if err != nil {
		t.Fatalf("OpenCursor: %v", err)
	}
	defer func() { _ = cursor.Close() }()

	// Advance past all records
	if cursor.Advance(100) {
		t.Fatal("Advance(100) returned true")
	}
	if cursor.Err() != nil {
		t.Fatalf("Err: %v", cursor.Err())
	}
}

func TestCursor_PostExhaustionFollowUp(t *testing.T) {
	t.Parallel()
	_, db := newCursorTestDB(t)
	ctx := context.Background()

	cursor, err := db.ObjectStore("items").OpenCursor(ctx, nil, indexeddb.CursorNext)
	if err != nil {
		t.Fatalf("OpenCursor: %v", err)
	}
	defer func() { _ = cursor.Close() }()

	for cursor.Continue() {
	}
	// Stream should still be synchronized after exhaustion
	if cursor.Continue() {
		t.Fatal("Continue after exhaustion returned true")
	}
	if cursor.Err() != nil {
		t.Fatalf("Err after post-exhaustion Continue: %v", cursor.Err())
	}
	// Delete on exhausted cursor should return ErrNotFound
	if err := cursor.Delete(); !errors.Is(err, indexeddb.ErrNotFound) {
		t.Errorf("Delete after exhaustion = %v, want ErrNotFound", err)
	}
}

func TestCursor_CloseMakesFurtherCallsInert(t *testing.T) {
	t.Parallel()
	_, db := newCursorTestDB(t)
	ctx := context.Background()

	cursor, err := db.ObjectStore("items").OpenCursor(ctx, nil, indexeddb.CursorNext)
	if err != nil {
		t.Fatalf("OpenCursor: %v", err)
	}
	if err := cursor.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if cursor.Continue() {
		t.Fatal("Continue after Close returned true")
	}
	if cursor.ContinueToKey("b") {
		t.Fatal("ContinueToKey after Close returned true")
	}
	if cursor.Advance(1) {
		t.Fatal("Advance after Close returned true")
	}
	if err := cursor.Delete(); !errors.Is(err, indexeddb.ErrNotFound) {
		t.Fatalf("Delete after Close = %v, want ErrNotFound", err)
	}
	if err := cursor.Update(indexeddb.Record{"id": "x"}); !errors.Is(err, indexeddb.ErrNotFound) {
		t.Fatalf("Update after Close = %v, want ErrNotFound", err)
	}
}
