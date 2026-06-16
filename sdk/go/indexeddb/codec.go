package indexeddb

import (
	"fmt"

	"github.com/valon-technologies/gestalt/sdk/go/client"
	"github.com/valon-technologies/gestalt/sdk/go/internal/indexeddbcodec"
)

func typedValueFromAny(v any) (*client.TypedValue, error) {
	return TypedValueFromAny(v)
}

// TypedValueFromAny encodes one value for RPC requests.
func TypedValueFromAny(v any) (*client.TypedValue, error) {
	wire, err := indexeddbcodec.TypedValueFromAny(v)
	if err != nil {
		return nil, err
	}
	return client.FromWireTypedValue(wire), nil
}

func anyFromTypedValue(v *client.TypedValue) (any, error) {
	return AnyFromTypedValue(v)
}

// AnyFromTypedValue decodes one typed value.
func AnyFromTypedValue(v *client.TypedValue) (any, error) {
	if v == nil {
		return nil, nil
	}
	return indexeddbcodec.AnyFromTypedValue(client.ToWireTypedValue(v))
}

func anyFromTypedValues(values []*client.TypedValue) ([]any, error) {
	return AnyFromTypedValues(values)
}

// AnyFromTypedValues decodes typed values.
func AnyFromTypedValues(values []*client.TypedValue) ([]any, error) {
	out := make([]any, len(values))
	for i, v := range values {
		item, err := AnyFromTypedValue(v)
		if err != nil {
			return nil, err
		}
		out[i] = item
	}
	return out, nil
}

func recordToProto(record Record) (*client.Record, error) {
	return RecordToProto(record)
}

// RecordToProto encodes a record.
func RecordToProto(record Record) (*client.Record, error) {
	wire, err := indexeddbcodec.RecordToProto(record)
	if err != nil {
		return nil, err
	}
	return client.FromWireRecord(wire), nil
}

func recordFromProto(record *client.Record) (Record, error) {
	return RecordFromProto(record)
}

// RecordFromProto decodes a record.
func RecordFromProto(record *client.Record) (Record, error) {
	if record == nil {
		return nil, fmt.Errorf("record is required")
	}
	return indexeddbcodec.RecordFromProto(client.ToWireRecord(record))
}

func recordsFromProto(records []*client.Record) ([]Record, error) {
	return RecordsFromProto(records)
}

// RecordsFromProto decodes records.
func RecordsFromProto(records []*client.Record) ([]Record, error) {
	out := make([]Record, len(records))
	for i, r := range records {
		item, err := RecordFromProto(r)
		if err != nil {
			return nil, err
		}
		out[i] = item
	}
	return out, nil
}

func recordsToProto(records []Record) ([]*client.Record, error) {
	return RecordsToProto(records)
}

// RecordsToProto encodes records.
func RecordsToProto(records []Record) ([]*client.Record, error) {
	wire, err := indexeddbcodec.RecordsToProto(records)
	if err != nil {
		return nil, err
	}
	out := make([]*client.Record, len(wire))
	for i, r := range wire {
		out[i] = client.FromWireRecord(r)
	}
	return out, nil
}

func keyValuesToAny(kvs []*client.KeyValue) ([]any, error) {
	return KeyValuesToAny(kvs)
}

// KeyValuesToAny decodes key values.
func KeyValuesToAny(kvs []*client.KeyValue) ([]any, error) {
	out := make([]any, len(kvs))
	for i, kv := range kvs {
		item, err := KeyValueToAny(kv)
		if err != nil {
			return nil, err
		}
		out[i] = item
	}
	return out, nil
}

func keyValueToAny(kv *client.KeyValue) (any, error) {
	return KeyValueToAny(kv)
}

// KeyValueToAny decodes one key value.
func KeyValueToAny(kv *client.KeyValue) (any, error) {
	if kv == nil {
		return nil, nil
	}
	return indexeddbcodec.KeyValueToAny(client.ToWireKeyValue(kv))
}

func anyToKeyValue(v any) (*client.KeyValue, error) {
	return AnyToKeyValue(v)
}

// AnyToKeyValue encodes a key for RPC requests.
func AnyToKeyValue(v any) (*client.KeyValue, error) {
	wire, err := indexeddbcodec.AnyToKeyValue(v)
	if err != nil {
		return nil, err
	}
	return client.FromWireKeyValue(wire), nil
}

func cursorKeyToProto(key any) (*client.KeyValue, error) {
	return CursorKeyToProto(key)
}

// CursorKeyToProto encodes a cursor key.
func CursorKeyToProto(key any) (*client.KeyValue, error) {
	wire, err := indexeddbcodec.CursorKeyToProto(key)
	if err != nil {
		return nil, err
	}
	return client.FromWireKeyValue(wire), nil
}

// CompareKeys compares native IndexedDB keys using W3C ordering.
func CompareKeys(a, b any) int {
	return indexeddbcodec.CompareKeys(a, b)
}

// KeyInRange reports whether key satisfies kr. A nil range includes all keys.
func KeyInRange(key any, kr *client.KeyRange) (bool, error) {
	if kr == nil {
		return true, nil
	}
	return indexeddbcodec.KeyInRange(key, client.ToWireKeyRange(kr))
}

// MatchQuery reports whether key satisfies query.
func MatchQuery(key any, query *client.IndexedDBQuery) (bool, error) {
	return indexeddbcodec.MatchQuery(key, client.ToWireIndexedDBQuery(query))
}

// EncodeIndexedDBKey serializes an IndexedDB key using the SDK's stable
// provider storage format.
func EncodeIndexedDBKey(value any) ([]byte, error) {
	return indexeddbcodec.EncodeKey(value)
}

// DecodeIndexedDBKey decodes a key previously written by EncodeIndexedDBKey or
// by the older protobuf-based helper.
func DecodeIndexedDBKey(data []byte) (any, error) {
	return indexeddbcodec.DecodeKey(data)
}

// EncodeIndexedDBRecord serializes a record using the SDK's stable provider
// storage format.
func EncodeIndexedDBRecord(record Record) ([]byte, error) {
	return indexeddbcodec.EncodeRecord(record)
}

// DecodeIndexedDBRecord decodes a record previously written by
// EncodeIndexedDBRecord or by the older protobuf-based helper.
func DecodeIndexedDBRecord(data []byte) (Record, error) {
	return indexeddbcodec.DecodeRecord(data)
}

// CloneIndexedDBRecordWithField returns a shallow clone of record with one
// field replaced. It is useful for cursor updates that must preserve the native
// primary key value.
func CloneIndexedDBRecordWithField(record Record, field string, value any) (Record, error) {
	if record == nil {
		return nil, fmt.Errorf("record is required")
	}
	cloned := make(Record, len(record)+1)
	for key, item := range record {
		cloned[key] = item
	}
	cloned[field] = value
	return cloned, nil
}

// IndexedDBRecordField returns one field from a record.
func IndexedDBRecordField(record Record, field string) (any, error) {
	if record == nil {
		return nil, fmt.Errorf("record is required")
	}
	value, ok := record[field]
	if !ok {
		return nil, fmt.Errorf("field %q not found", field)
	}
	return value, nil
}
