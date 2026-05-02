package gestalt

import (
	"fmt"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	"github.com/valon-technologies/gestalt/internal/indexeddbcodec"
)

func typedValueFromAny(v any) (*proto.TypedValue, error) {
	return indexeddbcodec.TypedValueFromAny(v)
}

func anyFromTypedValue(v *proto.TypedValue) (any, error) {
	return indexeddbcodec.AnyFromTypedValue(v)
}

func typedValuesFromAny(values []any) ([]*proto.TypedValue, error) {
	return indexeddbcodec.TypedValuesFromAny(values)
}

func anyFromTypedValues(values []*proto.TypedValue) ([]any, error) {
	return indexeddbcodec.AnyFromTypedValues(values)
}

func recordToProto(record Record) (*proto.Record, error) {
	return indexeddbcodec.RecordToProto(record)
}

func recordFromProto(record *proto.Record) (Record, error) {
	return indexeddbcodec.RecordFromProto(record)
}

func recordsFromProto(records []*proto.Record) ([]Record, error) {
	return indexeddbcodec.RecordsFromProto(records)
}

func recordsToProto(records []Record) ([]*proto.Record, error) {
	return indexeddbcodec.RecordsToProto(records)
}

func keyValuesToAny(kvs []*proto.KeyValue) ([]any, error) {
	return indexeddbcodec.KeyValuesToAny(kvs)
}

func keyValueToAny(kv *proto.KeyValue) (any, error) {
	return indexeddbcodec.KeyValueToAny(kv)
}

func anyToKeyValue(v any) (*proto.KeyValue, error) {
	return indexeddbcodec.AnyToKeyValue(v)
}

func cursorKeyToProto(key any, indexCursor bool) ([]*proto.KeyValue, error) {
	return indexeddbcodec.CursorKeyToProto(key, indexCursor)
}

// TypedValueFromAny converts a Go value into the legacy IndexedDB protocol
// value shape.
//
// Deprecated: prefer authored IndexedDB provider APIs and the storage helpers
// such as EncodeIndexedDBKey and EncodeIndexedDBRecord.
func TypedValueFromAny(v any) (*proto.TypedValue, error) {
	return typedValueFromAny(v)
}

// AnyFromTypedValue converts a legacy IndexedDB protocol value into a Go value.
//
// Deprecated: prefer authored IndexedDB provider APIs and the storage helpers
// such as DecodeIndexedDBKey and DecodeIndexedDBRecord.
func AnyFromTypedValue(v *proto.TypedValue) (any, error) {
	return anyFromTypedValue(v)
}

// AnyToKeyValue converts a Go value into the legacy IndexedDB protocol key
// shape.
//
// Deprecated: prefer authored IndexedDB provider APIs and EncodeIndexedDBKey.
func AnyToKeyValue(v any) (*proto.KeyValue, error) {
	return anyToKeyValue(v)
}

// KeyValuesToAny converts a legacy IndexedDB protocol key tuple into Go values.
//
// Deprecated: prefer authored IndexedDB provider APIs and DecodeIndexedDBKey.
func KeyValuesToAny(kvs []*proto.KeyValue) ([]any, error) {
	return keyValuesToAny(kvs)
}

// KeyValueToAny converts one legacy IndexedDB protocol key component into a Go
// value.
//
// Deprecated: prefer authored IndexedDB provider APIs and DecodeIndexedDBKey.
func KeyValueToAny(kv *proto.KeyValue) (any, error) {
	return keyValueToAny(kv)
}

// TypedValuesFromAny converts Go values into the legacy IndexedDB protocol
// typed-value list.
//
// Deprecated: prefer authored IndexedDB provider APIs and
// EncodeIndexedDBIndexValues.
func TypedValuesFromAny(values []any) ([]*proto.TypedValue, error) {
	return typedValuesFromAny(values)
}

// AnyFromTypedValues converts legacy IndexedDB protocol typed values into Go
// values.
//
// Deprecated: prefer authored IndexedDB provider APIs and
// DecodeIndexedDBIndexValues.
func AnyFromTypedValues(values []*proto.TypedValue) ([]any, error) {
	return anyFromTypedValues(values)
}

// RecordToProto converts a Go record into the legacy IndexedDB protocol record
// shape.
//
// Deprecated: prefer authored IndexedDB provider APIs and
// EncodeIndexedDBRecord.
func RecordToProto(record Record) (*proto.Record, error) {
	return recordToProto(record)
}

// RecordFromProto converts a legacy IndexedDB protocol record into a Go record.
//
// Deprecated: prefer authored IndexedDB provider APIs and
// DecodeIndexedDBRecord.
func RecordFromProto(record *proto.Record) (Record, error) {
	return recordFromProto(record)
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
