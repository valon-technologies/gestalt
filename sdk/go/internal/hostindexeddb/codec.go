package hostindexeddb

import (
	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	proto "github.com/valon-technologies/gestalt/sdk/go/internal/gen/v1"
	"github.com/valon-technologies/gestalt/sdk/go/internal/indexeddbcodec"
)

func typedValueFromAny(v any) (*proto.TypedValue, error) {
	return indexeddbcodec.TypedValueFromAny(v)
}

func anyFromTypedValue(v *proto.TypedValue) (any, error) {
	return AnyFromTypedValue(v)
}

// AnyFromTypedValue decodes one typed protobuf value.
func AnyFromTypedValue(v *proto.TypedValue) (any, error) {
	return indexeddbcodec.AnyFromTypedValue(v)
}

func typedValuesFromAny(values []any) ([]*proto.TypedValue, error) {
	return indexeddbcodec.TypedValuesFromAny(values)
}

func anyFromTypedValues(values []*proto.TypedValue) ([]any, error) {
	return AnyFromTypedValues(values)
}

// AnyFromTypedValues decodes typed protobuf values.
func AnyFromTypedValues(values []*proto.TypedValue) ([]any, error) {
	return indexeddbcodec.AnyFromTypedValues(values)
}

func recordToProto(record idb.Record) (*proto.Record, error) {
	return RecordToProto(record)
}

// RecordToProto encodes a record.
func RecordToProto(record idb.Record) (*proto.Record, error) {
	return indexeddbcodec.RecordToProto(record)
}

func recordFromProto(record *proto.Record) (idb.Record, error) {
	return RecordFromProto(record)
}

// RecordFromProto decodes a protobuf record.
func RecordFromProto(record *proto.Record) (idb.Record, error) {
	return indexeddbcodec.RecordFromProto(record)
}

func recordsFromProto(records []*proto.Record) ([]idb.Record, error) {
	return RecordsFromProto(records)
}

// RecordsFromProto decodes protobuf records.
func RecordsFromProto(records []*proto.Record) ([]idb.Record, error) {
	return indexeddbcodec.RecordsFromProto(records)
}

func recordsToProto(records []idb.Record) ([]*proto.Record, error) {
	return RecordsToProto(records)
}

// RecordsToProto encodes records.
func RecordsToProto(records []idb.Record) ([]*proto.Record, error) {
	return indexeddbcodec.RecordsToProto(records)
}

func keyValuesToAny(kvs []*proto.KeyValue) ([]any, error) {
	return KeyValuesToAny(kvs)
}

// KeyValuesToAny decodes key values.
func KeyValuesToAny(kvs []*proto.KeyValue) ([]any, error) {
	return indexeddbcodec.KeyValuesToAny(kvs)
}

func keyValueToAny(kv *proto.KeyValue) (any, error) {
	return indexeddbcodec.KeyValueToAny(kv)
}

func anyToKeyValue(v any) (*proto.KeyValue, error) {
	return indexeddbcodec.AnyToKeyValue(v)
}

func cursorKeyToProto(key any, indexCursor bool) ([]*proto.KeyValue, error) {
	return CursorKeyToProto(key, indexCursor)
}

// CursorKeyToProto encodes a cursor key.
func CursorKeyToProto(key any, indexCursor bool) ([]*proto.KeyValue, error) {
	return indexeddbcodec.CursorKeyToProto(key, indexCursor)
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
func EncodeIndexedDBRecord(record idb.Record) ([]byte, error) {
	return indexeddbcodec.EncodeRecord(record)
}

// DecodeIndexedDBRecord decodes a record previously written by
// EncodeIndexedDBRecord or by the older protobuf-based helper.
func DecodeIndexedDBRecord(data []byte) (idb.Record, error) {
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
