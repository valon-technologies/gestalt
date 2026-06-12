package gestalt

import (
	"github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

func recordFromProto(record *proto.Record) (Record, error) {
	return indexeddb.RecordFromProto(record)
}

func recordsFromProto(records []*proto.Record) ([]Record, error) {
	return indexeddb.RecordsFromProto(records)
}

func recordToProto(record Record) (*proto.Record, error) {
	return indexeddb.RecordToProto(record)
}

func recordsToProto(records []Record) ([]*proto.Record, error) {
	return indexeddb.RecordsToProto(records)
}

func anyFromTypedValue(v *proto.TypedValue) (any, error) {
	return indexeddb.AnyFromTypedValue(v)
}

func anyFromTypedValues(values []*proto.TypedValue) ([]any, error) {
	return indexeddb.AnyFromTypedValues(values)
}

func keyValuesToAny(kvs []*proto.KeyValue) ([]any, error) {
	return indexeddb.KeyValuesToAny(kvs)
}

func cursorKeyToProto(key any, indexCursor bool) ([]*proto.KeyValue, error) {
	return indexeddb.CursorKeyToProto(key, indexCursor)
}

// EncodeIndexedDBKey serializes an IndexedDB key (re-export).
func EncodeIndexedDBKey(value any) ([]byte, error) {
	return indexeddb.EncodeIndexedDBKey(value)
}

// DecodeIndexedDBKey decodes an IndexedDB key (re-export).
func DecodeIndexedDBKey(data []byte) (any, error) {
	return indexeddb.DecodeIndexedDBKey(data)
}

// EncodeIndexedDBRecord serializes a record (re-export).
func EncodeIndexedDBRecord(record Record) ([]byte, error) {
	return indexeddb.EncodeIndexedDBRecord(record)
}

// DecodeIndexedDBRecord decodes a record (re-export).
func DecodeIndexedDBRecord(data []byte) (Record, error) {
	return indexeddb.DecodeIndexedDBRecord(data)
}

// EncodeIndexedDBIndexValues serializes index values (re-export).
func EncodeIndexedDBIndexValues(values []any) ([]byte, error) {
	return indexeddb.EncodeIndexedDBIndexValues(values)
}

// DecodeIndexedDBIndexValues decodes index values (re-export).
func DecodeIndexedDBIndexValues(data []byte, keyParts int) ([]any, error) {
	return indexeddb.DecodeIndexedDBIndexValues(data, keyParts)
}

// CloneIndexedDBRecordWithField clones a record with one field replaced (re-export).
func CloneIndexedDBRecordWithField(record Record, field string, value any) (Record, error) {
	return indexeddb.CloneIndexedDBRecordWithField(record, field, value)
}

// IndexedDBRecordField returns one field from a record (re-export).
func IndexedDBRecordField(record Record, field string) (any, error) {
	return indexeddb.IndexedDBRecordField(record, field)
}
