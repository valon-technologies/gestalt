package indexeddb

import (
	"errors"
	"strings"
	"testing"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

func TestCursor_ContinueToKeyRejectsUnsupportedKey(t *testing.T) {
	cursor := &rpcCursor{}

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
	kv, err := idb.AnyToKeyValue("active")
	if err != nil {
		t.Fatalf("AnyToKeyValue: %v", err)
	}

	cursor := &rpcCursor{
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
	if _, err := cursor.Value(); !errors.Is(err, idb.ErrNotFound) {
		t.Fatalf("Value() after Close = %v, want ErrNotFound", err)
	}
}

func TestCursor_ValueRejectsNilRecord(t *testing.T) {
	cursor := &rpcCursor{
		entry: &proto.CursorEntry{
			PrimaryKey: "a",
		},
	}

	if _, err := cursor.Value(); !errors.Is(err, idb.ErrNotFound) {
		t.Fatalf("Value() = %v, want ErrNotFound", err)
	}
}
