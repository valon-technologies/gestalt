package contracttest

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"sort"
	"testing"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

const (
	bufSize = 1024 * 1024

	typeString int32 = 0
	typeInt    int32 = 1
	typeTime   int32 = 4
	typeBytes  int32 = 5
)

type Capabilities struct {
	TypedPrimaryKeys     bool
	NestedIndexPaths     bool
	UnreadablePayloadRow bool
}

type Harness interface {
	Name() string
	Capabilities() Capabilities
	NewServer(t *testing.T) (proto.IndexedDBServer, func())
	InsertUnreadablePayloadRow(t *testing.T, store, id, status string)
}

type session struct {
	harness Harness
	client  proto.IndexedDBClient
	close   func()
}

func Run(t *testing.T, harness Harness) {
	t.Helper()

	t.Run("TypedPrimaryKeyFidelity", func(t *testing.T) {
		if !harness.Capabilities().TypedPrimaryKeys {
			t.Skip("backend does not support typed primary keys")
		}
		runTypedPrimaryKeyFidelity(t, harness)
	})

	t.Run("TypedIndexRangeFidelity", func(t *testing.T) {
		runTypedIndexRangeFidelity(t, harness)
	})

	t.Run("KeyOnlyCursorSkipsUnreadableValues", func(t *testing.T) {
		if !harness.Capabilities().UnreadablePayloadRow {
			t.Skip("backend cannot inject unreadable payload rows under its native type constraints")
		}
		runKeyOnlyCursorSkipsUnreadableValues(t, harness)
	})

	t.Run("CursorMutationWithTypedKeys", func(t *testing.T) {
		if !harness.Capabilities().TypedPrimaryKeys {
			t.Skip("backend does not support typed primary keys")
		}
		runCursorMutationWithTypedKeys(t, harness)
	})

	t.Run("BulkConsistency", func(t *testing.T) {
		runBulkConsistency(t, harness)
	})

	t.Run("ExplicitTransactionWireContract", func(t *testing.T) {
		runExplicitTransactionWireContract(t, harness)
	})

	t.Run("TypedDeleteRangeFidelity", func(t *testing.T) {
		if !harness.Capabilities().TypedPrimaryKeys {
			t.Skip("backend does not support typed primary keys")
		}
		runTypedDeleteRangeFidelity(t, harness)
	})

	t.Run("RestartReconfigurePersistsIndexes", func(t *testing.T) {
		runRestartReconfigurePersistsIndexes(t, harness)
	})

	t.Run("MissingIndexFieldExclusion", func(t *testing.T) {
		runMissingIndexFieldExclusion(t, harness)
	})

	t.Run("UniqueIndexConflictOnCursorUpdate", func(t *testing.T) {
		runUniqueIndexConflictOnCursorUpdate(t, harness)
	})

	t.Run("NestedIndexPaths", func(t *testing.T) {
		if !harness.Capabilities().NestedIndexPaths {
			t.Skip("backend does not support nested index paths")
		}
		runNestedIndexPaths(t, harness)
	})
}

func runTypedPrimaryKeyFidelity(t *testing.T, harness Harness) {
	t.Helper()

	sess := newSession(t, harness)
	t.Cleanup(sess.Close)

	timeA := time.Date(2024, time.January, 1, 8, 0, 0, 0, time.UTC)
	timeB := time.Date(2024, time.January, 2, 8, 0, 0, 0, time.UTC)
	timeC := time.Date(2024, time.January, 10, 8, 0, 0, 0, time.UTC)

	cases := []struct {
		name       string
		store      string
		columnType int32
		ids        []any
		rangeWant  []any
		lower      any
		upper      any
	}{
		{
			name:       "String",
			store:      "typed_string_ids",
			columnType: typeString,
			ids:        []any{"a", "b", "c"},
			rangeWant:  []any{"b", "c"},
			lower:      "b",
			upper:      "c",
		},
		{
			name:       "Int64",
			store:      "typed_int_ids",
			columnType: typeInt,
			ids: []any{
				int64(9007199254740993),
				int64(9007199254741001),
				int64(9007199254740991),
			},
			rangeWant: []any{
				int64(9007199254740993),
				int64(9007199254741001),
			},
			lower: int64(9007199254740993),
			upper: int64(9007199254741001),
		},
		{
			name:       "Time",
			store:      "typed_time_ids",
			columnType: typeTime,
			ids:        []any{timeB, timeC, timeA},
			rangeWant:  []any{timeB, timeC},
			lower:      timeB,
			upper:      timeC,
		},
		{
			name:       "Bytes",
			store:      "typed_bytes_ids",
			columnType: typeBytes,
			ids:        []any{[]byte{0x02}, []byte{0x0A}, []byte{0x01}},
			rangeWant:  []any{[]byte{0x02}, []byte{0x0A}},
			lower:      []byte{0x02},
			upper:      []byte{0x0A},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mustCreateObjectStore(t, sess.client, tc.store, typedPrimaryKeySchema(tc.columnType))
			for i, id := range tc.ids {
				mustAddRecord(t, sess.client, tc.store, map[string]any{
					"id":   id,
					"name": fmt.Sprintf("%s-%d", tc.name, i),
				})
			}

			records := mustGetAll(t, sess.client, tc.store, nil)
			gotIDs := sortedRecordIDs(t, records)
			wantIDs := sortedValues(append([]any(nil), tc.ids...))
			assertValueSliceEqual(t, gotIDs, wantIDs)

			ranged := mustGetAll(t, sess.client, tc.store, &proto.KeyRange{
				Lower: mustTypedValue(t, tc.lower),
				Upper: mustTypedValue(t, tc.upper),
			})
			gotRangeIDs := sortedRecordIDs(t, ranged)
			wantRangeIDs := sortedValues(append([]any(nil), tc.rangeWant...))
			assertValueSliceEqual(t, gotRangeIDs, wantRangeIDs)

			entries := collectCursorEntries(t, sess.client, &proto.OpenCursorRequest{
				Store:     tc.store,
				Direction: proto.CursorDirection_CURSOR_NEXT,
			})
			gotCursorIDs := make([]any, 0, len(entries))
			gotCursorKeys := make([]any, 0, len(entries))
			for _, entry := range entries {
				gotCursorKeys = append(gotCursorKeys, cursorScalarKey(t, entry))
				record := mustRecord(t, entry.GetRecord())
				gotCursorIDs = append(gotCursorIDs, record["id"])
			}
			assertValueSliceEqual(t, gotCursorIDs, wantIDs)
			assertValueSliceEqual(t, gotCursorKeys, wantIDs)
		})
	}
}

func runTypedIndexRangeFidelity(t *testing.T, harness Harness) {
	t.Helper()

	sess := newSession(t, harness)
	t.Cleanup(sess.Close)

	store := "typed_index_range_fidelity"
	mustSeedNumericIndexItems(t, sess.client, store)

	rangeReq := &proto.KeyRange{
		Lower: mustTypedValue(t, int64(9007199254740993)),
		Upper: mustTypedValue(t, int64(9007199254741001)),
	}

	records := mustIndexGetAllWithRange(t, sess.client, store, "by_rank", rangeReq)
	gotIDs := recordPrimaryKeys(t, records)
	if !stringSlicesEqual(gotIDs, []string{"b", "c"}) {
		t.Fatalf("IndexGetAll by_rank ids = %#v, want %#v", gotIDs, []string{"b", "c"})
	}

	keys := mustIndexGetAllKeysWithRange(t, sess.client, store, "by_rank", rangeReq)
	if !stringSlicesEqual(keys, []string{"b", "c"}) {
		t.Fatalf("IndexGetAllKeys by_rank ids = %#v, want %#v", keys, []string{"b", "c"})
	}

	count := mustIndexCountWithRange(t, sess.client, store, "by_rank", rangeReq)
	if count != 2 {
		t.Fatalf("IndexCount by_rank = %d, want 2", count)
	}

	entries := collectCursorEntries(t, sess.client, &proto.OpenCursorRequest{
		Store:     store,
		Index:     "by_rank",
		Direction: proto.CursorDirection_CURSOR_NEXT,
		Range:     rangeReq,
	})
	if got := cursorPrimaryKeys(entries); !stringSlicesEqual(got, []string{"b", "c"}) {
		t.Fatalf("index cursor ids = %#v, want %#v", got, []string{"b", "c"})
	}
	gotKeys := []any{
		cursorScalarKey(t, entries[0]),
		cursorScalarKey(t, entries[1]),
	}
	assertValueSliceEqual(t, gotKeys, []any{int64(9007199254740993), int64(9007199254741001)})
}

func runKeyOnlyCursorSkipsUnreadableValues(t *testing.T, harness Harness) {
	t.Helper()

	sess := newSession(t, harness)
	t.Cleanup(sess.Close)

	store := "key_only_unreadable_rows"
	mustCreateObjectStore(t, sess.client, store, unreadablePayloadSchema())
	harness.InsertUnreadablePayloadRow(t, store, "broken", "active")

	objectStoreEntries := collectCursorEntries(t, sess.client, &proto.OpenCursorRequest{
		Store:     store,
		Direction: proto.CursorDirection_CURSOR_NEXT,
		KeysOnly:  true,
	})
	if len(objectStoreEntries) != 1 {
		t.Fatalf("object-store key cursor entry count = %d, want 1", len(objectStoreEntries))
	}
	if got := objectStoreEntries[0].GetPrimaryKey(); got != "broken" {
		t.Fatalf("object-store key cursor primary key = %q, want %q", got, "broken")
	}
	if objectStoreEntries[0].GetRecord() != nil {
		t.Fatalf("object-store key cursor returned record: %+v", objectStoreEntries[0].GetRecord())
	}

	indexEntries := collectCursorEntries(t, sess.client, &proto.OpenCursorRequest{
		Store:     store,
		Index:     "by_status",
		Direction: proto.CursorDirection_CURSOR_NEXT,
		Range: &proto.KeyRange{
			Lower: mustTypedValue(t, "active"),
			Upper: mustTypedValue(t, "active"),
		},
		KeysOnly: true,
	})
	if len(indexEntries) != 1 {
		t.Fatalf("index key cursor entry count = %d, want 1", len(indexEntries))
	}
	if got := indexEntries[0].GetPrimaryKey(); got != "broken" {
		t.Fatalf("index key cursor primary key = %q, want %q", got, "broken")
	}
	if indexEntries[0].GetRecord() != nil {
		t.Fatalf("index key cursor returned record: %+v", indexEntries[0].GetRecord())
	}
	if got := cursorKeyValues(t, indexEntries[0]); len(got) != 1 || got[0] != "active" {
		t.Fatalf("index key cursor key = %#v, want [\"active\"]", got)
	}

	rangeReq := &proto.KeyRange{
		Lower: mustTypedValue(t, "active"),
		Upper: mustTypedValue(t, "active"),
	}
	keys := mustIndexGetAllKeysWithRange(t, sess.client, store, "by_status", rangeReq)
	if !stringSlicesEqual(keys, []string{"broken"}) {
		t.Fatalf("IndexGetAllKeys unreadable ids = %#v, want %#v", keys, []string{"broken"})
	}
	count := mustIndexCountWithRange(t, sess.client, store, "by_status", rangeReq)
	if count != 1 {
		t.Fatalf("IndexCount unreadable = %d, want 1", count)
	}
	deleted := mustIndexDeleteWithRange(t, sess.client, store, "by_status", rangeReq)
	if deleted != 1 {
		t.Fatalf("IndexDelete unreadable deleted = %d, want 1", deleted)
	}
	if remaining := mustCount(t, sess.client, store, nil); remaining != 0 {
		t.Fatalf("remaining unreadable rows after IndexDelete = %d, want 0", remaining)
	}
}

func runCursorMutationWithTypedKeys(t *testing.T, harness Harness) {
	t.Helper()

	cases := []struct {
		name       string
		columnType int32
		id         any
	}{
		{name: "Numeric", columnType: typeInt, id: int64(42)},
		{name: "Binary", columnType: typeBytes, id: []byte{0x42}},
	}

	for _, tc := range cases {
		t.Run(tc.name+"Delete", func(t *testing.T) {
			sess := newSession(t, harness)
			t.Cleanup(sess.Close)

			store := fmt.Sprintf("cursor_delete_%s", tc.name)
			mustCreateObjectStore(t, sess.client, store, typedPrimaryKeySchema(tc.columnType))
			mustAddRecord(t, sess.client, store, map[string]any{"id": tc.id, "name": "before"})

			stream := openCursorStream(t, sess.client, &proto.OpenCursorRequest{
				Store:     store,
				Direction: proto.CursorDirection_CURSOR_NEXT,
			})
			t.Cleanup(func() { _ = closeCursorStream(stream) })

			_ = cursorEntryFromResponse(t, sendCursorCommand(t, stream, &proto.CursorCommand{
				Command: &proto.CursorCommand_Next{Next: true},
			}))

			deleteResp := sendCursorCommand(t, stream, &proto.CursorCommand{
				Command: &proto.CursorCommand_Delete{Delete: true},
			})
			if cursorDoneFromResponse(t, deleteResp) {
				t.Fatal("delete ack unexpectedly marked cursor exhausted")
			}

			records := mustGetAll(t, sess.client, store, nil)
			if len(records) != 0 {
				t.Fatalf("record count after delete = %d, want 0", len(records))
			}
		})

		t.Run(tc.name+"Update", func(t *testing.T) {
			sess := newSession(t, harness)
			t.Cleanup(sess.Close)

			store := fmt.Sprintf("cursor_update_%s", tc.name)
			mustCreateObjectStore(t, sess.client, store, typedPrimaryKeySchema(tc.columnType))
			mustAddRecord(t, sess.client, store, map[string]any{"id": tc.id, "name": "before"})

			stream := openCursorStream(t, sess.client, &proto.OpenCursorRequest{
				Store:     store,
				Direction: proto.CursorDirection_CURSOR_NEXT,
			})
			t.Cleanup(func() { _ = closeCursorStream(stream) })

			_ = cursorEntryFromResponse(t, sendCursorCommand(t, stream, &proto.CursorCommand{
				Command: &proto.CursorCommand_Next{Next: true},
			}))

			update := mustRecordProto(t, map[string]any{"name": "after"})
			resp := sendCursorCommand(t, stream, &proto.CursorCommand{
				Command: &proto.CursorCommand_Update{Update: update},
			})
			updated := mustRecord(t, cursorEntryFromResponse(t, resp).GetRecord())
			assertValueEqual(t, updated["id"], tc.id)
			if got := updated["name"]; got != "after" {
				t.Fatalf("updated name = %#v, want %q", got, "after")
			}

			records := mustGetAll(t, sess.client, store, nil)
			if len(records) != 1 {
				t.Fatalf("record count after update = %d, want 1", len(records))
			}
			decoded := mustRecord(t, records[0])
			assertValueEqual(t, decoded["id"], tc.id)
			if got := decoded["name"]; got != "after" {
				t.Fatalf("persisted name = %#v, want %q", got, "after")
			}
		})
	}
}

func runBulkConsistency(t *testing.T, harness Harness) {
	t.Helper()

	t.Run("ObjectStoreRange", func(t *testing.T) {
		sess := newSession(t, harness)
		t.Cleanup(sess.Close)

		store := "bulk_object_store_range"
		mustSeedBulkItems(t, sess.client, store)

		rangeReq := &proto.KeyRange{
			Lower: mustTypedValue(t, "b"),
			Upper: mustTypedValue(t, "c"),
		}
		records := mustGetAll(t, sess.client, store, rangeReq)
		keys := mustGetAllKeys(t, sess.client, store, rangeReq)
		count := mustCount(t, sess.client, store, rangeReq)

		gotIDs := sortedStrings(recordPrimaryKeys(t, records))
		want := []string{"b", "c"}
		if !stringSlicesEqual(gotIDs, want) {
			t.Fatalf("GetAll range ids = %#v, want %#v", gotIDs, want)
		}
		if !stringSlicesEqual(sortedStrings(keys), want) {
			t.Fatalf("GetAllKeys range ids = %#v, want %#v", sortedStrings(keys), want)
		}
		if count != int64(len(want)) {
			t.Fatalf("Count range = %d, want %d", count, len(want))
		}

		deleted := mustDeleteRange(t, sess.client, store, rangeReq)
		if deleted != int64(len(want)) {
			t.Fatalf("DeleteRange deleted = %d, want %d", deleted, len(want))
		}
		remaining := sortedStrings(recordPrimaryKeys(t, mustGetAll(t, sess.client, store, nil)))
		if !stringSlicesEqual(remaining, []string{"a", "d"}) {
			t.Fatalf("remaining ids after DeleteRange = %#v, want %#v", remaining, []string{"a", "d"})
		}
	})

	t.Run("IndexQuery", func(t *testing.T) {
		sess := newSession(t, harness)
		t.Cleanup(sess.Close)

		store := "bulk_index_query"
		mustSeedBulkItems(t, sess.client, store)

		values := []*proto.TypedValue{mustTypedValue(t, "active")}
		records := mustIndexGetAll(t, sess.client, store, "by_status", values)
		keys := mustIndexGetAllKeys(t, sess.client, store, "by_status", values)
		count := mustIndexCount(t, sess.client, store, "by_status", values)

		want := []string{"a", "b", "d"}
		gotIDs := recordPrimaryKeys(t, records)
		if !stringSlicesEqual(gotIDs, want) {
			t.Fatalf("IndexGetAll ids = %#v, want %#v", gotIDs, want)
		}
		if !stringSlicesEqual(keys, want) {
			t.Fatalf("IndexGetAllKeys ids = %#v, want %#v", keys, want)
		}
		if count != int64(len(want)) {
			t.Fatalf("IndexCount = %d, want %d", count, len(want))
		}

		deleted := mustIndexDelete(t, sess.client, store, "by_status", values)
		if deleted != int64(len(want)) {
			t.Fatalf("IndexDelete deleted = %d, want %d", deleted, len(want))
		}
		remaining := sortedStrings(recordPrimaryKeys(t, mustGetAll(t, sess.client, store, nil)))
		if !stringSlicesEqual(remaining, []string{"c"}) {
			t.Fatalf("remaining ids after IndexDelete = %#v, want %#v", remaining, []string{"c"})
		}
	})

	t.Run("IndexRange", func(t *testing.T) {
		sess := newSession(t, harness)
		t.Cleanup(sess.Close)

		store := "bulk_index_range"
		mustSeedBulkItems(t, sess.client, store)

		rangeReq := &proto.KeyRange{
			Lower: mustTypedValue(t, "active"),
			Upper: mustTypedValue(t, "active"),
		}
		records := mustIndexGetAllWithRange(t, sess.client, store, "by_status", rangeReq)
		keys := mustIndexGetAllKeysWithRange(t, sess.client, store, "by_status", rangeReq)
		count := mustIndexCountWithRange(t, sess.client, store, "by_status", rangeReq)

		want := []string{"a", "b", "d"}
		gotIDs := recordPrimaryKeys(t, records)
		if !stringSlicesEqual(gotIDs, want) {
			t.Fatalf("IndexGetAll range ids = %#v, want %#v", gotIDs, want)
		}
		if !stringSlicesEqual(keys, want) {
			t.Fatalf("IndexGetAllKeys range ids = %#v, want %#v", keys, want)
		}
		if count != int64(len(want)) {
			t.Fatalf("IndexCount range = %d, want %d", count, len(want))
		}

		deleted := mustIndexDeleteWithRange(t, sess.client, store, "by_status", rangeReq)
		if deleted != int64(len(want)) {
			t.Fatalf("IndexDelete range deleted = %d, want %d", deleted, len(want))
		}
		remaining := sortedStrings(recordPrimaryKeys(t, mustGetAll(t, sess.client, store, nil)))
		if !stringSlicesEqual(remaining, []string{"c"}) {
			t.Fatalf("remaining ids after IndexDelete(range) = %#v, want %#v", remaining, []string{"c"})
		}
	})
}

func runExplicitTransactionWireContract(t *testing.T, harness Harness) {
	t.Helper()

	sess := newSession(t, harness)
	t.Cleanup(sess.Close)

	store := "explicit_transaction_wire_contract"
	mustCreateObjectStore(t, sess.client, store, &proto.ObjectStoreSchema{
		Indexes: []*proto.IndexSchema{
			{Name: "by_status", KeyPath: []string{"status"}},
		},
	})
	mustAddRecord(t, sess.client, store, map[string]any{
		"id":     "a",
		"name":   "Alpha",
		"status": "active",
	})

	tx := mustBeginTransaction(t, sess.client, []string{store}, proto.TransactionMode_TRANSACTION_READWRITE)
	if err := txPut(t, tx, store, map[string]any{"id": "b", "name": "Beta", "status": "active"}); err != nil {
		t.Fatalf("transaction Put(b): %v", err)
	}
	gotB := mustTxGet(t, tx, store, "b")
	if gotB["name"] != "Beta" {
		t.Fatalf("transaction Get(b).name = %#v, want Beta", gotB["name"])
	}
	if got := mustTxIndexCount(t, tx, store, "by_status", "active"); got != 2 {
		t.Fatalf("transaction IndexCount(active) = %d, want 2", got)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("transaction Commit: %v", err)
	}
	tx.Close()
	mustGet(t, sess.client, store, "b")

	tx = mustBeginTransaction(t, sess.client, []string{store}, proto.TransactionMode_TRANSACTION_READWRITE)
	if err := txPut(t, tx, store, map[string]any{"id": "c", "name": "Gamma", "status": "pending"}); err != nil {
		t.Fatalf("transaction Put(c): %v", err)
	}
	if err := txDelete(t, tx, store, "b"); err != nil {
		t.Fatalf("transaction Delete(b): %v", err)
	}
	if err := tx.Abort(); err != nil {
		t.Fatalf("transaction Abort: %v", err)
	}
	tx.Close()
	mustGet(t, sess.client, store, "b")
	mustGetNotFound(t, sess.client, store, "c")

	tx = mustBeginTransaction(t, sess.client, []string{store}, proto.TransactionMode_TRANSACTION_READONLY)
	err := txPut(t, tx, store, map[string]any{"id": "readonly", "name": "Read Only", "status": "active"})
	if got := status.Code(err); got != codes.FailedPrecondition {
		t.Fatalf("readonly transaction Put error = %v (%s), want %s", err, got, codes.FailedPrecondition)
	}
	if err := tx.ExpectAbort(); err != nil {
		t.Fatalf("readonly transaction abort response: %v", err)
	}
	tx.Close()
	mustGetNotFound(t, sess.client, store, "readonly")

	tx = mustBeginTransaction(t, sess.client, []string{store}, proto.TransactionMode_TRANSACTION_READWRITE)
	if err := txPut(t, tx, store, map[string]any{"id": "d", "name": "Delta", "status": "active"}); err != nil {
		t.Fatalf("transaction Put(d): %v", err)
	}
	err = txAdd(t, tx, store, map[string]any{"id": "a", "name": "Duplicate", "status": "active"})
	if got := status.Code(err); got != codes.AlreadyExists {
		t.Fatalf("duplicate Add error = %v (%s), want %s", err, got, codes.AlreadyExists)
	}
	if err := tx.ExpectAbort(); err != nil {
		t.Fatalf("operation-error transaction abort response: %v", err)
	}
	tx.Close()
	mustGetNotFound(t, sess.client, store, "d")
}

func runTypedDeleteRangeFidelity(t *testing.T, harness Harness) {
	t.Helper()

	sess := newSession(t, harness)
	t.Cleanup(sess.Close)

	timeA := time.Date(2024, time.January, 1, 8, 0, 0, 0, time.UTC)
	timeB := time.Date(2024, time.January, 2, 8, 0, 0, 0, time.UTC)
	timeC := time.Date(2024, time.January, 3, 8, 0, 0, 0, time.UTC)

	cases := []struct {
		name       string
		store      string
		columnType int32
		ids        []any
		lower      any
		upper      any
		remaining  []any
	}{
		{
			name:       "Int64",
			store:      "typed_delete_range_int",
			columnType: typeInt,
			ids:        []any{int64(10), int64(20), int64(30)},
			lower:      int64(20),
			upper:      int64(30),
			remaining:  []any{int64(10)},
		},
		{
			name:       "Time",
			store:      "typed_delete_range_time",
			columnType: typeTime,
			ids:        []any{timeA, timeB, timeC},
			lower:      timeB,
			upper:      timeC,
			remaining:  []any{timeA},
		},
		{
			name:       "Bytes",
			store:      "typed_delete_range_bytes",
			columnType: typeBytes,
			ids:        []any{[]byte{0x01}, []byte{0x02}, []byte{0x03}},
			lower:      []byte{0x02},
			upper:      []byte{0x03},
			remaining:  []any{[]byte{0x01}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mustCreateObjectStore(t, sess.client, tc.store, typedPrimaryKeySchema(tc.columnType))
			for i, id := range tc.ids {
				mustAddRecord(t, sess.client, tc.store, map[string]any{
					"id":   id,
					"name": fmt.Sprintf("%s-%d", tc.name, i),
				})
			}

			deleted := mustDeleteRange(t, sess.client, tc.store, &proto.KeyRange{
				Lower: mustTypedValue(t, tc.lower),
				Upper: mustTypedValue(t, tc.upper),
			})
			if deleted != 2 {
				t.Fatalf("DeleteRange deleted = %d, want 2", deleted)
			}

			remaining := sortedRecordIDs(t, mustGetAll(t, sess.client, tc.store, nil))
			assertValueSliceEqual(t, remaining, sortedValues(append([]any(nil), tc.remaining...)))
		})
	}
}

func runRestartReconfigurePersistsIndexes(t *testing.T, harness Harness) {
	t.Helper()

	sess := newSession(t, harness)
	t.Cleanup(sess.Close)

	store := "restart_persists_indexes"
	mustSeedBulkItems(t, sess.client, store)
	sess.Restart(t)

	values := []*proto.TypedValue{mustTypedValue(t, "active")}
	records := mustIndexGetAll(t, sess.client, store, "by_status", values)
	gotIDs := sortedStrings(recordPrimaryKeys(t, records))
	want := []string{"a", "b", "d"}
	if !stringSlicesEqual(gotIDs, want) {
		t.Fatalf("IndexGetAll ids after restart = %#v, want %#v", gotIDs, want)
	}

	entries := collectCursorEntries(t, sess.client, &proto.OpenCursorRequest{
		Store:     store,
		Index:     "by_status",
		Direction: proto.CursorDirection_CURSOR_NEXT,
		Range: &proto.KeyRange{
			Lower: mustTypedValue(t, "active"),
			Upper: mustTypedValue(t, "active"),
		},
	})
	if got := sortedStrings(cursorPrimaryKeys(entries)); !stringSlicesEqual(got, want) {
		t.Fatalf("index cursor ids after restart = %#v, want %#v", got, want)
	}
}

func runMissingIndexFieldExclusion(t *testing.T, harness Harness) {
	t.Helper()

	sess := newSession(t, harness)
	t.Cleanup(sess.Close)

	store := "missing_index_field_exclusion"
	mustCreateObjectStore(t, sess.client, store, bulkItemsSchema())
	mustAddRecord(t, sess.client, store, map[string]any{
		"id":     "a",
		"name":   "Alice",
		"status": "active",
	})
	mustAddRecord(t, sess.client, store, map[string]any{
		"id":     "b",
		"name":   "Bob",
		"status": "active",
	})

	stream := openCursorStream(t, sess.client, &proto.OpenCursorRequest{
		Store:     store,
		Index:     "by_status",
		Direction: proto.CursorDirection_CURSOR_NEXT,
		Range: &proto.KeyRange{
			Lower: mustTypedValue(t, "active"),
			Upper: mustTypedValue(t, "active"),
		},
	})
	t.Cleanup(func() { _ = closeCursorStream(stream) })

	first := cursorEntryFromResponse(t, sendCursorCommand(t, stream, &proto.CursorCommand{
		Command: &proto.CursorCommand_Next{Next: true},
	}))
	if got := first.GetPrimaryKey(); got != "a" {
		t.Fatalf("first active cursor id = %q, want %q", got, "a")
	}

	update := mustRecordProto(t, map[string]any{
		"name": "Alice",
	})
	_ = cursorEntryFromResponse(t, sendCursorCommand(t, stream, &proto.CursorCommand{
		Command: &proto.CursorCommand_Update{Update: update},
	}))

	values := []*proto.TypedValue{mustTypedValue(t, "active")}
	records := mustIndexGetAll(t, sess.client, store, "by_status", values)
	gotIDs := sortedStrings(recordPrimaryKeys(t, records))
	if !stringSlicesEqual(gotIDs, []string{"b"}) {
		t.Fatalf("active ids after clearing indexed field = %#v, want %#v", gotIDs, []string{"b"})
	}

	entries := collectCursorEntries(t, sess.client, &proto.OpenCursorRequest{
		Store:     store,
		Index:     "by_status",
		Direction: proto.CursorDirection_CURSOR_NEXT,
		Range: &proto.KeyRange{
			Lower: mustTypedValue(t, "active"),
			Upper: mustTypedValue(t, "active"),
		},
	})
	if got := cursorPrimaryKeys(entries); !stringSlicesEqual(got, []string{"b"}) {
		t.Fatalf("active cursor ids after clearing indexed field = %#v, want %#v", got, []string{"b"})
	}
}

func runUniqueIndexConflictOnCursorUpdate(t *testing.T, harness Harness) {
	t.Helper()

	sess := newSession(t, harness)
	t.Cleanup(sess.Close)

	store := "unique_index_conflict_update"
	mustCreateObjectStore(t, sess.client, store, uniqueEmailSchema())
	mustAddRecord(t, sess.client, store, map[string]any{
		"id":    "a",
		"name":  "Alice",
		"email": "alice@test.com",
	})
	mustAddRecord(t, sess.client, store, map[string]any{
		"id":    "b",
		"name":  "Bob",
		"email": "bob@test.com",
	})

	stream := openCursorStream(t, sess.client, &proto.OpenCursorRequest{
		Store:     store,
		Direction: proto.CursorDirection_CURSOR_NEXT,
	})
	t.Cleanup(func() { _ = closeCursorStream(stream) })

	_ = cursorEntryFromResponse(t, sendCursorCommand(t, stream, &proto.CursorCommand{
		Command: &proto.CursorCommand_Advance{Advance: 1},
	}))

	update := mustRecordProto(t, map[string]any{
		"name":  "Bob",
		"email": "alice@test.com",
	})
	err := sendCursorCommandExpectError(t, stream, &proto.CursorCommand{
		Command: &proto.CursorCommand_Update{Update: update},
	})
	if got := status.Code(err); got != codes.AlreadyExists {
		t.Fatalf("cursor update error code = %s, want %s", got, codes.AlreadyExists)
	}
}

func runNestedIndexPaths(t *testing.T, harness Harness) {
	t.Helper()

	sess := newSession(t, harness)
	t.Cleanup(sess.Close)

	store := "nested_index_paths"
	mustCreateObjectStore(t, sess.client, store, &proto.ObjectStoreSchema{
		Indexes: []*proto.IndexSchema{
			{Name: "by_profile_name", KeyPath: []string{"profile.name"}},
		},
	})
	mustAddRecord(t, sess.client, store, map[string]any{
		"id":      "a",
		"profile": map[string]any{"name": "Alice"},
	})
	mustAddRecord(t, sess.client, store, map[string]any{
		"id":      "b",
		"profile": map[string]any{"name": "Bob"},
	})

	entries := collectCursorEntries(t, sess.client, &proto.OpenCursorRequest{
		Store:     store,
		Index:     "by_profile_name",
		Direction: proto.CursorDirection_CURSOR_NEXT,
		Range: &proto.KeyRange{
			Lower: mustTypedValue(t, "Alice"),
			Upper: mustTypedValue(t, "Alice"),
		},
	})
	if len(entries) != 1 {
		t.Fatalf("nested index entry count = %d, want 1", len(entries))
	}
	if got := entries[0].GetPrimaryKey(); got != "a" {
		t.Fatalf("nested index primary key = %q, want %q", got, "a")
	}
	if got := cursorKeyValues(t, entries[0]); len(got) != 1 || got[0] != "Alice" {
		t.Fatalf("nested index key = %#v, want [\"Alice\"]", got)
	}
}

func newSession(t *testing.T, harness Harness) *session {
	t.Helper()

	serverImpl, providerClose := harness.NewServer(t)
	client, grpcClose := newBufconnClient(t, serverImpl)
	return &session{
		harness: harness,
		client:  client,
		close: func() {
			grpcClose()
			providerClose()
		},
	}
}

func (s *session) Restart(t *testing.T) {
	t.Helper()
	s.Close()
	next := newSession(t, s.harness)
	s.client = next.client
	s.close = next.close
}

func (s *session) Close() {
	if s.close != nil {
		s.close()
		s.close = nil
	}
}

func newBufconnClient(t *testing.T, serverImpl proto.IndexedDBServer) (proto.IndexedDBClient, func()) {
	t.Helper()

	listener := bufconn.Listen(bufSize)
	server := grpc.NewServer()
	proto.RegisterIndexedDBServer(server, serverImpl)
	go func() {
		_ = server.Serve(listener)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	conn, err := grpc.DialContext(
		ctx,
		"bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	cancel()
	if err != nil {
		server.Stop()
		_ = listener.Close()
		t.Fatalf("DialContext: %v", err)
	}

	return proto.NewIndexedDBClient(conn), func() {
		_ = conn.Close()
		server.Stop()
		_ = listener.Close()
	}
}

type contractTransaction struct {
	stream proto.IndexedDB_TransactionClient
	cancel context.CancelFunc
	nextID uint64
}

func mustBeginTransaction(t *testing.T, client proto.IndexedDBClient, stores []string, mode proto.TransactionMode) *contractTransaction {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	stream, err := client.Transaction(ctx)
	if err != nil {
		cancel()
		t.Fatalf("Transaction: %v", err)
	}
	if err := stream.Send(&proto.TransactionClientMessage{
		Msg: &proto.TransactionClientMessage_Begin{Begin: &proto.BeginTransactionRequest{
			Stores: stores,
			Mode:   mode,
		}},
	}); err != nil {
		cancel()
		t.Fatalf("Transaction send begin: %v", err)
	}
	resp, err := stream.Recv()
	if err != nil {
		cancel()
		t.Fatalf("Transaction recv begin: %v", err)
	}
	if resp.GetBegin() == nil {
		cancel()
		t.Fatalf("Transaction begin response = %T, want begin", resp.GetMsg())
	}
	return &contractTransaction{stream: stream, cancel: cancel}
}

func (tx *contractTransaction) Close() {
	if tx.stream != nil {
		_ = tx.stream.CloseSend()
		tx.stream = nil
	}
	if tx.cancel != nil {
		tx.cancel()
		tx.cancel = nil
	}
}

func (tx *contractTransaction) Operation(op *proto.TransactionOperation) (*proto.TransactionOperationResponse, error) {
	tx.nextID++
	op.RequestId = tx.nextID
	if err := tx.stream.Send(&proto.TransactionClientMessage{
		Msg: &proto.TransactionClientMessage_Operation{Operation: op},
	}); err != nil {
		return nil, err
	}
	resp, err := tx.stream.Recv()
	if err != nil {
		return nil, err
	}
	opResp := resp.GetOperation()
	if opResp == nil {
		return nil, fmt.Errorf("transaction response = %T, want operation", resp.GetMsg())
	}
	if opResp.GetRequestId() != op.GetRequestId() {
		return nil, fmt.Errorf("transaction response request id = %d, want %d", opResp.GetRequestId(), op.GetRequestId())
	}
	if err := transactionStatusErr(opResp.GetError()); err != nil {
		return nil, err
	}
	return opResp, nil
}

func (tx *contractTransaction) Commit() error {
	if err := tx.stream.Send(&proto.TransactionClientMessage{
		Msg: &proto.TransactionClientMessage_Commit{Commit: &proto.TransactionCommitRequest{}},
	}); err != nil {
		return err
	}
	resp, err := tx.stream.Recv()
	if err != nil {
		return err
	}
	commit := resp.GetCommit()
	if commit == nil {
		return fmt.Errorf("transaction response = %T, want commit", resp.GetMsg())
	}
	return transactionStatusErr(commit.GetError())
}

func (tx *contractTransaction) Abort() error {
	if err := tx.stream.Send(&proto.TransactionClientMessage{
		Msg: &proto.TransactionClientMessage_Abort{Abort: &proto.TransactionAbortRequest{}},
	}); err != nil {
		return err
	}
	return tx.ExpectAbort()
}

func (tx *contractTransaction) ExpectAbort() error {
	resp, err := tx.stream.Recv()
	if err != nil {
		return err
	}
	abort := resp.GetAbort()
	if abort == nil {
		return fmt.Errorf("transaction response = %T, want abort", resp.GetMsg())
	}
	return transactionStatusErr(abort.GetError())
}

func transactionStatusErr(st *rpcstatus.Status) error {
	if st == nil || st.GetCode() == int32(codes.OK) {
		return nil
	}
	return status.Error(codes.Code(st.GetCode()), st.GetMessage())
}

func bulkItemsSchema() *proto.ObjectStoreSchema {
	return &proto.ObjectStoreSchema{
		Indexes: []*proto.IndexSchema{
			{Name: "by_status", KeyPath: []string{"status"}},
		},
	}
}

func unreadablePayloadSchema() *proto.ObjectStoreSchema {
	return &proto.ObjectStoreSchema{
		Indexes: []*proto.IndexSchema{
			{Name: "by_status", KeyPath: []string{"status"}},
		},
		Columns: []*proto.ColumnDef{
			{Name: "id", Type: typeString, PrimaryKey: true, NotNull: true},
			{Name: "status", Type: typeString},
			{Name: "payload", Type: typeInt},
		},
	}
}

func uniqueEmailSchema() *proto.ObjectStoreSchema {
	return &proto.ObjectStoreSchema{
		Indexes: []*proto.IndexSchema{
			{Name: "by_email", KeyPath: []string{"email"}, Unique: true},
		},
	}
}

func numericIndexSchema() *proto.ObjectStoreSchema {
	return &proto.ObjectStoreSchema{
		Indexes: []*proto.IndexSchema{
			{Name: "by_rank", KeyPath: []string{"rank"}},
		},
	}
}

func typedPrimaryKeySchema(columnType int32) *proto.ObjectStoreSchema {
	return &proto.ObjectStoreSchema{
		Columns: []*proto.ColumnDef{
			{Name: "id", Type: columnType, PrimaryKey: true, NotNull: true},
			{Name: "name", Type: typeString},
		},
	}
}

func mustSeedBulkItems(t *testing.T, client proto.IndexedDBClient, store string) {
	t.Helper()

	mustCreateObjectStore(t, client, store, bulkItemsSchema())
	for _, record := range []map[string]any{
		{"id": "a", "name": "Alice", "status": "active"},
		{"id": "b", "name": "Bob", "status": "active"},
		{"id": "c", "name": "Carol", "status": "inactive"},
		{"id": "d", "name": "Dave", "status": "active"},
	} {
		mustAddRecord(t, client, store, record)
	}
}

func mustSeedNumericIndexItems(t *testing.T, client proto.IndexedDBClient, store string) {
	t.Helper()

	mustCreateObjectStore(t, client, store, numericIndexSchema())
	for _, record := range []map[string]any{
		{"id": "a", "name": "Alpha", "rank": int64(9007199254740991)},
		{"id": "b", "name": "Beta", "rank": int64(9007199254740993)},
		{"id": "c", "name": "Gamma", "rank": int64(9007199254741001)},
		{"id": "d", "name": "Delta", "rank": int64(9007199254741013)},
	} {
		mustAddRecord(t, client, store, record)
	}
}

func mustCreateObjectStore(t *testing.T, client proto.IndexedDBClient, store string, schema *proto.ObjectStoreSchema) {
	t.Helper()
	if _, err := client.CreateObjectStore(context.Background(), &proto.CreateObjectStoreRequest{
		Name:   store,
		Schema: schema,
	}); err != nil {
		t.Fatalf("CreateObjectStore(%s): %v", store, err)
	}
}

func mustAddRecord(t *testing.T, client proto.IndexedDBClient, store string, record map[string]any) {
	t.Helper()
	if _, err := client.Add(context.Background(), &proto.RecordRequest{
		Store:  store,
		Record: mustRecordProto(t, record),
	}); err != nil {
		t.Fatalf("Add(%s): %v", store, err)
	}
}

func mustGet(t *testing.T, client proto.IndexedDBClient, store, id string) map[string]any {
	t.Helper()
	resp, err := client.Get(context.Background(), &proto.ObjectStoreRequest{Store: store, Id: id})
	if err != nil {
		t.Fatalf("Get(%s/%s): %v", store, id, err)
	}
	return mustRecord(t, resp.GetRecord())
}

func mustGetNotFound(t *testing.T, client proto.IndexedDBClient, store, id string) {
	t.Helper()
	_, err := client.Get(context.Background(), &proto.ObjectStoreRequest{Store: store, Id: id})
	if got := status.Code(err); got != codes.NotFound {
		t.Fatalf("Get(%s/%s) error = %v (%s), want %s", store, id, err, got, codes.NotFound)
	}
}

func txAdd(t *testing.T, tx *contractTransaction, store string, record map[string]any) error {
	t.Helper()
	_, err := tx.Operation(&proto.TransactionOperation{
		Operation: &proto.TransactionOperation_Add{Add: &proto.RecordRequest{
			Store:  store,
			Record: mustRecordProto(t, record),
		}},
	})
	return err
}

func txPut(t *testing.T, tx *contractTransaction, store string, record map[string]any) error {
	t.Helper()
	_, err := tx.Operation(&proto.TransactionOperation{
		Operation: &proto.TransactionOperation_Put{Put: &proto.RecordRequest{
			Store:  store,
			Record: mustRecordProto(t, record),
		}},
	})
	return err
}

func txDelete(t *testing.T, tx *contractTransaction, store, id string) error {
	t.Helper()
	_, err := tx.Operation(&proto.TransactionOperation{
		Operation: &proto.TransactionOperation_Delete{Delete: &proto.ObjectStoreRequest{
			Store: store,
			Id:    id,
		}},
	})
	return err
}

func mustTxGet(t *testing.T, tx *contractTransaction, store, id string) map[string]any {
	t.Helper()
	resp, err := tx.Operation(&proto.TransactionOperation{
		Operation: &proto.TransactionOperation_Get{Get: &proto.ObjectStoreRequest{
			Store: store,
			Id:    id,
		}},
	})
	if err != nil {
		t.Fatalf("transaction Get(%s/%s): %v", store, id, err)
	}
	return mustRecord(t, resp.GetRecord().GetRecord())
}

func mustTxIndexCount(t *testing.T, tx *contractTransaction, store, index string, values ...any) int64 {
	t.Helper()
	typedValues := make([]*proto.TypedValue, 0, len(values))
	for _, value := range values {
		typedValues = append(typedValues, mustTypedValue(t, value))
	}
	resp, err := tx.Operation(&proto.TransactionOperation{
		Operation: &proto.TransactionOperation_IndexCount{IndexCount: &proto.IndexQueryRequest{
			Store:  store,
			Index:  index,
			Values: typedValues,
		}},
	})
	if err != nil {
		t.Fatalf("transaction IndexCount(%s/%s): %v", store, index, err)
	}
	return resp.GetCount().GetCount()
}

func mustRecordProto(t *testing.T, record map[string]any) *proto.Record {
	t.Helper()
	out, err := gestalt.RecordToProto(record)
	if err != nil {
		t.Fatalf("RecordToProto(%#v): %v", record, err)
	}
	return out
}

func mustRecord(t *testing.T, record *proto.Record) map[string]any {
	t.Helper()
	out, err := gestalt.RecordFromProto(record)
	if err != nil {
		t.Fatalf("RecordFromProto: %v", err)
	}
	return out
}

func mustTypedValue(t *testing.T, value any) *proto.TypedValue {
	t.Helper()
	out, err := gestalt.TypedValueFromAny(value)
	if err != nil {
		t.Fatalf("TypedValueFromAny(%#v): %v", value, err)
	}
	return out
}

func mustGetAll(t *testing.T, client proto.IndexedDBClient, store string, keyRange *proto.KeyRange) []*proto.Record {
	t.Helper()
	resp, err := client.GetAll(context.Background(), &proto.ObjectStoreRangeRequest{
		Store: store,
		Range: keyRange,
	})
	if err != nil {
		t.Fatalf("GetAll(%s): %v", store, err)
	}
	return resp.GetRecords()
}

func mustGetAllKeys(t *testing.T, client proto.IndexedDBClient, store string, keyRange *proto.KeyRange) []string {
	t.Helper()
	resp, err := client.GetAllKeys(context.Background(), &proto.ObjectStoreRangeRequest{
		Store: store,
		Range: keyRange,
	})
	if err != nil {
		t.Fatalf("GetAllKeys(%s): %v", store, err)
	}
	return resp.GetKeys()
}

func mustCount(t *testing.T, client proto.IndexedDBClient, store string, keyRange *proto.KeyRange) int64 {
	t.Helper()
	resp, err := client.Count(context.Background(), &proto.ObjectStoreRangeRequest{
		Store: store,
		Range: keyRange,
	})
	if err != nil {
		t.Fatalf("Count(%s): %v", store, err)
	}
	return resp.GetCount()
}

func mustDeleteRange(t *testing.T, client proto.IndexedDBClient, store string, keyRange *proto.KeyRange) int64 {
	t.Helper()
	resp, err := client.DeleteRange(context.Background(), &proto.ObjectStoreRangeRequest{
		Store: store,
		Range: keyRange,
	})
	if err != nil {
		t.Fatalf("DeleteRange(%s): %v", store, err)
	}
	return resp.GetDeleted()
}

func mustIndexGetAll(t *testing.T, client proto.IndexedDBClient, store, index string, values []*proto.TypedValue) []*proto.Record {
	t.Helper()
	return mustIndexGetAllWithRange(t, client, store, index, nil, values...)
}

func mustIndexGetAllWithRange(t *testing.T, client proto.IndexedDBClient, store, index string, keyRange *proto.KeyRange, values ...*proto.TypedValue) []*proto.Record {
	t.Helper()
	resp, err := client.IndexGetAll(context.Background(), &proto.IndexQueryRequest{
		Store:  store,
		Index:  index,
		Values: values,
		Range:  keyRange,
	})
	if err != nil {
		t.Fatalf("IndexGetAll(%s/%s): %v", store, index, err)
	}
	return resp.GetRecords()
}

func mustIndexGetAllKeys(t *testing.T, client proto.IndexedDBClient, store, index string, values []*proto.TypedValue) []string {
	t.Helper()
	return mustIndexGetAllKeysWithRange(t, client, store, index, nil, values...)
}

func mustIndexGetAllKeysWithRange(t *testing.T, client proto.IndexedDBClient, store, index string, keyRange *proto.KeyRange, values ...*proto.TypedValue) []string {
	t.Helper()
	resp, err := client.IndexGetAllKeys(context.Background(), &proto.IndexQueryRequest{
		Store:  store,
		Index:  index,
		Values: values,
		Range:  keyRange,
	})
	if err != nil {
		t.Fatalf("IndexGetAllKeys(%s/%s): %v", store, index, err)
	}
	return resp.GetKeys()
}

func mustIndexCount(t *testing.T, client proto.IndexedDBClient, store, index string, values []*proto.TypedValue) int64 {
	t.Helper()
	return mustIndexCountWithRange(t, client, store, index, nil, values...)
}

func mustIndexCountWithRange(t *testing.T, client proto.IndexedDBClient, store, index string, keyRange *proto.KeyRange, values ...*proto.TypedValue) int64 {
	t.Helper()
	resp, err := client.IndexCount(context.Background(), &proto.IndexQueryRequest{
		Store:  store,
		Index:  index,
		Values: values,
		Range:  keyRange,
	})
	if err != nil {
		t.Fatalf("IndexCount(%s/%s): %v", store, index, err)
	}
	return resp.GetCount()
}

func mustIndexDelete(t *testing.T, client proto.IndexedDBClient, store, index string, values []*proto.TypedValue) int64 {
	t.Helper()
	return mustIndexDeleteWithRange(t, client, store, index, nil, values...)
}

func mustIndexDeleteWithRange(t *testing.T, client proto.IndexedDBClient, store, index string, keyRange *proto.KeyRange, values ...*proto.TypedValue) int64 {
	t.Helper()
	resp, err := client.IndexDelete(context.Background(), &proto.IndexQueryRequest{
		Store:  store,
		Index:  index,
		Values: values,
		Range:  keyRange,
	})
	if err != nil {
		t.Fatalf("IndexDelete(%s/%s): %v", store, index, err)
	}
	return resp.GetDeleted()
}

func openCursorStream(t *testing.T, client proto.IndexedDBClient, req *proto.OpenCursorRequest) proto.IndexedDB_OpenCursorClient {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	stream, err := client.OpenCursor(ctx)
	if err != nil {
		cancel()
		t.Fatalf("OpenCursor: %v", err)
	}

	if err := stream.Send(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Open{Open: req},
	}); err != nil {
		cancel()
		t.Fatalf("Send(open): %v", err)
	}

	resp, err := stream.Recv()
	if err != nil {
		cancel()
		t.Fatalf("Recv(open ack): %v", err)
	}
	done, ok := resp.GetResult().(*proto.CursorResponse_Done)
	if !ok || done.Done {
		cancel()
		t.Fatalf("open ack = %T %+v, want done=false", resp.GetResult(), resp)
	}

	return &cursorStream{
		IndexedDB_OpenCursorClient: stream,
		cancel:                     cancel,
	}
}

type cursorStream struct {
	proto.IndexedDB_OpenCursorClient
	cancel context.CancelFunc
}

func (c *cursorStream) CloseSend() error {
	if c.cancel != nil {
		defer c.cancel()
	}
	return c.IndexedDB_OpenCursorClient.CloseSend()
}

func closeCursorStream(stream proto.IndexedDB_OpenCursorClient) error {
	if stream == nil {
		return nil
	}
	_ = stream.Send(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Command{
			Command: &proto.CursorCommand{Command: &proto.CursorCommand_Close{Close: true}},
		},
	})
	return stream.CloseSend()
}

func sendCursorCommand(t *testing.T, stream proto.IndexedDB_OpenCursorClient, cmd *proto.CursorCommand) *proto.CursorResponse {
	t.Helper()

	if err := stream.Send(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Command{Command: cmd},
	}); err != nil {
		t.Fatalf("Send(command): %v", err)
	}

	resp, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv(command): %v", err)
	}
	return resp
}

func sendCursorCommandExpectError(t *testing.T, stream proto.IndexedDB_OpenCursorClient, cmd *proto.CursorCommand) error {
	t.Helper()

	if err := stream.Send(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Command{Command: cmd},
	}); err != nil {
		t.Fatalf("Send(command): %v", err)
	}

	_, err := stream.Recv()
	if err == nil {
		t.Fatal("Recv(command) = nil, want error")
	}
	return err
}

func collectCursorEntries(t *testing.T, client proto.IndexedDBClient, req *proto.OpenCursorRequest) []*proto.CursorEntry {
	t.Helper()

	stream := openCursorStream(t, client, req)
	defer closeCursorStream(stream)

	var entries []*proto.CursorEntry
	for {
		resp := sendCursorCommand(t, stream, &proto.CursorCommand{
			Command: &proto.CursorCommand_Next{Next: true},
		})
		if _, ok := resp.GetResult().(*proto.CursorResponse_Done); ok {
			if !cursorDoneFromResponse(t, resp) {
				t.Fatal("expected exhausted cursor response")
			}
			return entries
		}
		entries = append(entries, cursorEntryFromResponse(t, resp))
	}
}

func cursorEntryFromResponse(t *testing.T, resp *proto.CursorResponse) *proto.CursorEntry {
	t.Helper()

	entry, ok := resp.GetResult().(*proto.CursorResponse_Entry)
	if !ok {
		t.Fatalf("response = %T %+v, want entry", resp.GetResult(), resp)
	}
	return entry.Entry
}

func cursorDoneFromResponse(t *testing.T, resp *proto.CursorResponse) bool {
	t.Helper()

	done, ok := resp.GetResult().(*proto.CursorResponse_Done)
	if !ok {
		t.Fatalf("response = %T %+v, want done", resp.GetResult(), resp)
	}
	return done.Done
}

func cursorKeyValues(t *testing.T, entry *proto.CursorEntry) []any {
	t.Helper()
	values, err := gestalt.KeyValuesToAny(entry.GetKey())
	if err != nil {
		t.Fatalf("KeyValuesToAny: %v", err)
	}
	return values
}

func cursorScalarKey(t *testing.T, entry *proto.CursorEntry) any {
	t.Helper()
	values := cursorKeyValues(t, entry)
	if len(values) != 1 {
		t.Fatalf("cursor key length = %d, want 1", len(values))
	}
	return values[0]
}

func cursorPrimaryKeys(entries []*proto.CursorEntry) []string {
	keys := make([]string, len(entries))
	for i, entry := range entries {
		keys[i] = entry.GetPrimaryKey()
	}
	return keys
}

func recordPrimaryKeys(t *testing.T, records []*proto.Record) []string {
	t.Helper()
	keys := make([]string, len(records))
	for i, record := range records {
		decoded := mustRecord(t, record)
		keys[i] = fmt.Sprint(decoded["id"])
	}
	return keys
}

func sortedRecordIDs(t *testing.T, records []*proto.Record) []any {
	t.Helper()
	ids := make([]any, len(records))
	for i, record := range records {
		decoded := mustRecord(t, record)
		ids[i] = decoded["id"]
	}
	return sortedValues(ids)
}

func sortedValues(values []any) []any {
	sort.Slice(values, func(i, j int) bool {
		return compareValues(values[i], values[j]) < 0
	})
	return values
}

func sortedStrings(values []string) []string {
	sort.Strings(values)
	return values
}

func stringSlicesEqual(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func assertValueSliceEqual(t *testing.T, got, want []any) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("value count = %d, want %d", len(got), len(want))
	}
	for i := range got {
		assertValueEqual(t, got[i], want[i])
	}
}

func assertValueEqual(t *testing.T, got, want any) {
	t.Helper()
	if compareValues(got, want) != 0 {
		t.Fatalf("value = %#v (%T), want %#v (%T)", got, got, want, want)
	}
}

func compareValues(a, b any) int {
	switch av := a.(type) {
	case string:
		bv := b.(string)
		switch {
		case av < bv:
			return -1
		case av > bv:
			return 1
		default:
			return 0
		}
	case int64:
		bv := b.(int64)
		switch {
		case av < bv:
			return -1
		case av > bv:
			return 1
		default:
			return 0
		}
	case time.Time:
		bv := b.(time.Time)
		switch {
		case av.Before(bv):
			return -1
		case av.After(bv):
			return 1
		default:
			return 0
		}
	case []byte:
		return bytes.Compare(av, b.([]byte))
	default:
		panic(fmt.Sprintf("unsupported comparison type %T", a))
	}
}
