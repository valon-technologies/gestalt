package indexeddb

import (
	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	coreindexeddb "github.com/valon-technologies/gestalt/server/core/indexeddb"
	proto "github.com/valon-technologies/gestalt/server/internal/gen/v1"
	"github.com/valon-technologies/gestalt/server/internal/indexeddbcodec"
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

func recordToProto(record idb.Record) (*proto.Record, error) {
	return indexeddbcodec.RecordToProto(record)
}

func recordsToProto(records []idb.Record) ([]*proto.Record, error) {
	return indexeddbcodec.RecordsToProto(records)
}

func recordFromProto(record *proto.Record) (idb.Record, error) {
	return indexeddbcodec.RecordFromProto(record)
}

func recordsFromProto(records []*proto.Record) ([]idb.Record, error) {
	return indexeddbcodec.RecordsFromProto(records)
}

func keyValuesToAny(kvs []*proto.KeyValue) ([]any, error) {
	return indexeddbcodec.KeyValuesToAny(kvs)
}

func anyToKeyValue(v any) (*proto.KeyValue, error) {
	return indexeddbcodec.AnyToKeyValue(v)
}

func cursorKeyToProto(key any, indexCursor bool) ([]*proto.KeyValue, error) {
	return indexeddbcodec.CursorKeyToProto(key, indexCursor)
}

var _ coreindexeddb.IndexedDB = (*remoteIndexedDB)(nil)
