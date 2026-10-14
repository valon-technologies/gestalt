package gestalt

import (
	"github.com/valon-technologies/gestalt/sdk/go/client"
	"github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

func recordFromProto(record *proto.Record) (Record, error) {
	return indexeddb.RecordFromProto(client.FromWireRecord(record))
}

func recordToProto(record Record) (*proto.Record, error) {
	native, err := indexeddb.RecordToProto(record)
	if err != nil {
		return nil, err
	}
	return client.ToWireRecord(native), nil
}

func recordsToProto(records []Record) ([]*proto.Record, error) {
	native, err := indexeddb.RecordsToProto(records)
	if err != nil {
		return nil, err
	}
	out := make([]*proto.Record, len(native))
	for i, r := range native {
		out[i] = client.ToWireRecord(r)
	}
	return out, nil
}

func keyValueToAny(kv *proto.KeyValue) (any, error) {
	return indexeddb.KeyValueToAny(client.FromWireKeyValue(kv))
}

func cursorKeyToProto(key any) (*proto.KeyValue, error) {
	native, err := indexeddb.CursorKeyToProto(key)
	if err != nil {
		return nil, err
	}
	return client.ToWireKeyValue(native), nil
}

// CursorKeyToProto encodes a native key for cursor commands (re-export).
func CursorKeyToProto(key any) (*KeyValue, error) {
	return indexeddb.CursorKeyToProto(key)
}
