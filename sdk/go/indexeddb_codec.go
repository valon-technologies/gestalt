package gestalt

import (
	"fmt"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	"github.com/valon-technologies/gestalt/internal/indexeddbcodec"
)

func typedValueFromAny(v any) (*proto.TypedValue, error) {
	return indexeddbcodec.TypedValueFromAny(v)
}

// TypedValueFromAny converts a Go IndexedDB value into the provider wire
// representation used by cursor and record APIs.
func TypedValueFromAny(v any) (*proto.TypedValue, error) {
	return typedValueFromAny(v)
}

func anyFromTypedValue(v *proto.TypedValue) (any, error) {
	return indexeddbcodec.AnyFromTypedValue(v)
}

// AnyFromTypedValue converts a provider wire value into the corresponding Go
// IndexedDB value.
func AnyFromTypedValue(v *proto.TypedValue) (any, error) {
	return anyFromTypedValue(v)
}

func typedValuesFromAny(values []any) ([]*proto.TypedValue, error) {
	return indexeddbcodec.TypedValuesFromAny(values)
}

// TypedValuesFromAny converts ordered Go IndexedDB values into provider wire
// values.
func TypedValuesFromAny(values []any) ([]*proto.TypedValue, error) {
	return typedValuesFromAny(values)
}

func anyFromTypedValues(values []*proto.TypedValue) ([]any, error) {
	return indexeddbcodec.AnyFromTypedValues(values)
}

// AnyFromTypedValues converts ordered provider wire values into Go IndexedDB
// values.
func AnyFromTypedValues(values []*proto.TypedValue) ([]any, error) {
	return anyFromTypedValues(values)
}

func recordToProto(record Record) (*proto.Record, error) {
	return indexeddbcodec.RecordToProto(record)
}

// RecordToProto converts a Go IndexedDB record into the provider wire
// representation used by lower-level datastore APIs.
func RecordToProto(record Record) (*proto.Record, error) {
	return recordToProto(record)
}

func recordFromProto(record *proto.Record) (Record, error) {
	return indexeddbcodec.RecordFromProto(record)
}

// RecordFromProto converts a provider wire record into a Go IndexedDB record.
func RecordFromProto(record *proto.Record) (Record, error) {
	return recordFromProto(record)
}

func recordsFromProto(records []*proto.Record) ([]Record, error) {
	return indexeddbcodec.RecordsFromProto(records)
}

// RecordsFromProto converts provider wire records into Go IndexedDB records.
func RecordsFromProto(records []*proto.Record) ([]Record, error) {
	return recordsFromProto(records)
}

func recordsToProto(records []Record) ([]*proto.Record, error) {
	return indexeddbcodec.RecordsToProto(records)
}

// RecordsToProto converts Go IndexedDB records into provider wire records.
func RecordsToProto(records []Record) ([]*proto.Record, error) {
	return recordsToProto(records)
}

func keyValuesToAny(kvs []*proto.KeyValue) ([]any, error) {
	return indexeddbcodec.KeyValuesToAny(kvs)
}

// KeyValuesToAny converts provider cursor key parts into Go IndexedDB key
// values.
func KeyValuesToAny(kvs []*proto.KeyValue) ([]any, error) {
	return keyValuesToAny(kvs)
}

func keyValueToAny(kv *proto.KeyValue) (any, error) {
	return indexeddbcodec.KeyValueToAny(kv)
}

// KeyValueToAny converts one provider cursor key part into a Go IndexedDB key
// value.
func KeyValueToAny(kv *proto.KeyValue) (any, error) {
	return keyValueToAny(kv)
}

func anyToKeyValue(v any) (*proto.KeyValue, error) {
	return indexeddbcodec.AnyToKeyValue(v)
}

// AnyToKeyValue converts a Go IndexedDB key value into one provider cursor key
// part.
func AnyToKeyValue(v any) (*proto.KeyValue, error) {
	return anyToKeyValue(v)
}

func cursorKeyToProto(key any, indexCursor bool) ([]*proto.KeyValue, error) {
	return indexeddbcodec.CursorKeyToProto(key, indexCursor)
}

// CursorKeyToProto converts a primary-key or index-key cursor target into the
// provider wire key representation.
func CursorKeyToProto(key any, indexCursor bool) ([]*proto.KeyValue, error) {
	return cursorKeyToProto(key, indexCursor)
}

// EncodeIndexedDBKey serializes an IndexedDB key using the SDK's stable
// provider storage format. It preserves the previous protobuf-backed encoding
// while keeping generated types out of provider code.
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

// EncodeIndexedDBIndexValues serializes an ordered index-key value list using
// the SDK's previous deterministic protobuf record shape.
func EncodeIndexedDBIndexValues(values []any) ([]byte, error) {
	return indexeddbcodec.EncodeIndexValues(values)
}

// DecodeIndexedDBIndexValues decodes the stable index-key list encoding written
// by EncodeIndexedDBIndexValues.
func DecodeIndexedDBIndexValues(data []byte, keyParts int) ([]any, error) {
	return indexeddbcodec.DecodeIndexValues(data, keyParts)
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
