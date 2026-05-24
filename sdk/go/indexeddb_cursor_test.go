package gestalt

import (
	"math"
	"reflect"
	"testing"
)

func TestCursorKeyCodec_RoundTripArrayValuedIndexComponent(t *testing.T) {
	key := []any{[]any{"x", "y"}}

	kvs, err := cursorKeyToProto(key, true)
	if err != nil {
		t.Fatalf("cursorKeyToProto: %v", err)
	}
	got, err := keyValuesToAny(kvs)
	if err != nil {
		t.Fatalf("keyValuesToAny: %v", err)
	}
	if !reflect.DeepEqual(got, key) {
		t.Fatalf("round trip = %#v, want %#v", got, key)
	}
}

func TestCursorKeyCodec_AcceptsTypedSliceCompositeKeys(t *testing.T) {
	key := []string{"a", "b"}

	kvs, err := cursorKeyToProto(key, true)
	if err != nil {
		t.Fatalf("cursorKeyToProto: %v", err)
	}
	got, err := keyValuesToAny(kvs)
	if err != nil {
		t.Fatalf("keyValuesToAny: %v", err)
	}
	want := []any{"a", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %#v, want %#v", got, want)
	}
}

func TestCursorKeyCodec_RoundTripTypedArrayValuedIndexComponent(t *testing.T) {
	key := []any{[]string{"x", "y"}}

	kvs, err := cursorKeyToProto(key, true)
	if err != nil {
		t.Fatalf("cursorKeyToProto: %v", err)
	}
	got, err := keyValuesToAny(kvs)
	if err != nil {
		t.Fatalf("keyValuesToAny: %v", err)
	}
	want := []any{[]any{"x", "y"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %#v, want %#v", got, want)
	}
}

func TestIndexedDBCursorSnapshot_AdvanceFromCurrentPosition(t *testing.T) {
	snapshot := NewIndexedDBCursorSnapshot(IndexedDBOpenCursorRequest{})
	if err := snapshot.Load([]IndexedDBCursorSnapshotEntry{
		{Key: "a", PrimaryKey: "a", PrimaryKeyValue: "a"},
		{Key: "b", PrimaryKey: "b", PrimaryKeyValue: "b"},
		{Key: "c", PrimaryKey: "c", PrimaryKeyValue: "c"},
	}, nil); err != nil {
		t.Fatalf("Load: %v", err)
	}

	first, err := snapshot.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if first == nil || first.PrimaryKey != "a" {
		t.Fatalf("Next primary key = %#v, want a", first)
	}

	second, err := snapshot.Advance(1)
	if err != nil {
		t.Fatalf("Advance(1): %v", err)
	}
	if second == nil || second.PrimaryKey != "b" {
		t.Fatalf("Advance(1) primary key = %#v, want b", second)
	}

	third, err := snapshot.Advance(1)
	if err != nil {
		t.Fatalf("second Advance(1): %v", err)
	}
	if third == nil || third.PrimaryKey != "c" {
		t.Fatalf("second Advance(1) primary key = %#v, want c", third)
	}
}

func TestIndexedDBCursorSnapshot_IndexRangeAcceptsScalarEntryKeys(t *testing.T) {
	snapshot := NewIndexedDBCursorSnapshot(IndexedDBOpenCursorRequest{
		Index: "by_status",
	})
	if err := snapshot.Load([]IndexedDBCursorSnapshotEntry{
		{Key: "done", PrimaryKey: "issue-2", PrimaryKeyValue: "issue-2"},
		{Key: "active", PrimaryKey: "issue-1", PrimaryKeyValue: "issue-1"},
	}, &KeyRange{Lower: "active", Upper: "active"}); err != nil {
		t.Fatalf("Load: %v", err)
	}

	first, err := snapshot.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if first == nil || first.PrimaryKey != "issue-1" || first.Key != "active" {
		t.Fatalf("Next = %#v, want active scalar index key", first)
	}
	second, err := snapshot.Next()
	if err != nil {
		t.Fatalf("second Next: %v", err)
	}
	if second != nil {
		t.Fatalf("second Next = %#v, want nil", second)
	}
}

func TestIndexedDBCursorSnapshot_LoadResetsPosition(t *testing.T) {
	snapshot := NewIndexedDBCursorSnapshot(IndexedDBOpenCursorRequest{})
	if err := snapshot.Load([]IndexedDBCursorSnapshotEntry{
		{Key: "a", PrimaryKey: "a", PrimaryKeyValue: "a"},
		{Key: "b", PrimaryKey: "b", PrimaryKeyValue: "b"},
	}, nil); err != nil {
		t.Fatalf("first Load: %v", err)
	}
	if _, err := snapshot.Next(); err != nil {
		t.Fatalf("Next: %v", err)
	}

	if err := snapshot.Load([]IndexedDBCursorSnapshotEntry{
		{Key: "c", PrimaryKey: "c", PrimaryKeyValue: "c"},
	}, nil); err != nil {
		t.Fatalf("second Load: %v", err)
	}

	first, err := snapshot.Next()
	if err != nil {
		t.Fatalf("Next after reload: %v", err)
	}
	if first == nil || first.PrimaryKey != "c" {
		t.Fatalf("Next after reload primary key = %#v, want c", first)
	}
}

func TestCompareIndexedDBValues_UnsignedIntegersUseNumericOrdering(t *testing.T) {
	tests := []struct {
		name string
		a    any
		b    any
		want int
	}{
		{name: "uint", a: uint(10), b: uint(9), want: 1},
		{name: "uint8", a: uint8(10), b: uint8(9), want: 1},
		{name: "uint16", a: uint16(10), b: uint16(9), want: 1},
		{name: "uint32", a: uint32(10), b: uint32(9), want: 1},
		{name: "uint64", a: uint64(10), b: uint64(9), want: 1},
		{name: "uint64 max", a: uint64(math.MaxUint64), b: int64(math.MaxInt64), want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CompareIndexedDBValues(tt.a, tt.b); got != tt.want {
				t.Fatalf("CompareIndexedDBValues(%T(%v), %T(%v)) = %d, want %d", tt.a, tt.a, tt.b, tt.b, got, tt.want)
			}
		})
	}
}
