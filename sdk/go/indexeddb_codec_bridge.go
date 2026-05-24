package gestalt

import (
	"github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	proto "github.com/valon-technologies/gestalt/sdk/go/internal/gen/v1"
	"github.com/valon-technologies/gestalt/sdk/go/internal/indexeddbcodec"
)

func recordFromProto(record *proto.Record) (Record, error) {
	return indexeddbcodec.RecordFromProto(record)
}

func recordsFromProto(records []*proto.Record) ([]Record, error) {
	return indexeddbcodec.RecordsFromProto(records)
}

func recordToProto(record Record) (*proto.Record, error) {
	return indexeddbcodec.RecordToProto(record)
}

func recordsToProto(records []Record) ([]*proto.Record, error) {
	return indexeddbcodec.RecordsToProto(records)
}

func anyFromTypedValue(v *proto.TypedValue) (any, error) {
	return indexeddbcodec.AnyFromTypedValue(v)
}

func typedValueFromAny(v any) (*proto.TypedValue, error) {
	return indexeddbcodec.TypedValueFromAny(v)
}

func anyFromTypedValues(values []*proto.TypedValue) ([]any, error) {
	return indexeddbcodec.AnyFromTypedValues(values)
}

func typedValuesFromAny(values []any) ([]*proto.TypedValue, error) {
	return indexeddbcodec.TypedValuesFromAny(values)
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

// EncodeIndexedDBKey serializes an IndexedDB key (re-export).
func EncodeIndexedDBKey(value any) ([]byte, error) {
	return indexeddbcodec.EncodeKey(value)
}

// DecodeIndexedDBKey decodes an IndexedDB key (re-export).
func DecodeIndexedDBKey(data []byte) (any, error) {
	return indexeddbcodec.DecodeKey(data)
}

// EncodeIndexedDBRecord serializes a record (re-export).
func EncodeIndexedDBRecord(record Record) ([]byte, error) {
	return indexeddbcodec.EncodeRecord(record)
}

// DecodeIndexedDBRecord decodes a record (re-export).
func DecodeIndexedDBRecord(data []byte) (Record, error) {
	return indexeddbcodec.DecodeRecord(data)
}

// EncodeIndexedDBIndexValues serializes index values (re-export).
func EncodeIndexedDBIndexValues(values []any) ([]byte, error) {
	return indexeddbcodec.EncodeIndexValues(values)
}

// DecodeIndexedDBIndexValues decodes index values (re-export).
func DecodeIndexedDBIndexValues(data []byte, keyParts int) ([]any, error) {
	return indexeddbcodec.DecodeIndexValues(data, keyParts)
}

// CloneIndexedDBRecordWithField clones a record with one field replaced (re-export).
func CloneIndexedDBRecordWithField(record Record, field string, value any) (Record, error) {
	return indexeddb.CloneIndexedDBRecordWithField(record, field, value)
}

// IndexedDBRecordField returns one field from a record (re-export).
func IndexedDBRecordField(record Record, field string) (any, error) {
	return indexeddb.IndexedDBRecordField(record, field)
}
