package gestalt

import (
	"fmt"

	"github.com/valon-technologies/gestalt/sdk/go/internal/indexeddbcodec"
)

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
