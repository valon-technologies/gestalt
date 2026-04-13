package gestalt

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
)

func TestCursor_ContinueToKeyRejectsUnsupportedKey(t *testing.T) {
	cursor := &Cursor{}

	if cursor.ContinueToKey(make(chan int)) {
		t.Fatal("ContinueToKey returned true")
	}
	if cursor.Err() == nil {
		t.Fatal("Err() = nil, want conversion error")
	}
	if !strings.Contains(cursor.Err().Error(), "marshal") {
		t.Fatalf("Err() = %v, want marshal error", cursor.Err())
	}
}

func TestCursor_CloseClearsCurrentEntry(t *testing.T) {
	kv, err := AnyToKeyValue("active")
	if err != nil {
		t.Fatalf("AnyToKeyValue: %v", err)
	}

	cursor := &Cursor{
		entry: &proto.CursorEntry{
			Key:        []*proto.KeyValue{kv},
			PrimaryKey: "a",
		},
	}

	if err := cursor.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := cursor.Key(); got != nil {
		t.Fatalf("Key() after Close = %v, want nil", got)
	}
	if got := cursor.PrimaryKey(); got != "" {
		t.Fatalf("PrimaryKey() after Close = %q, want empty", got)
	}
	if _, err := cursor.Value(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Value() after Close = %v, want ErrNotFound", err)
	}
}

func TestCursorKeyCodec_RoundTripArrayValuedIndexComponent(t *testing.T) {
	key := []any{[]any{"x", "y"}}

	kvs, err := CursorKeyToProto(key, true)
	if err != nil {
		t.Fatalf("CursorKeyToProto: %v", err)
	}
	got, err := KeyValuesToAny(kvs)
	if err != nil {
		t.Fatalf("KeyValuesToAny: %v", err)
	}
	if !reflect.DeepEqual(got, key) {
		t.Fatalf("round trip = %#v, want %#v", got, key)
	}
}
