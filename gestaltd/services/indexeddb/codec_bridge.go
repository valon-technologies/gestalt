package indexeddb

import (
	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	"github.com/valon-technologies/gestalt/server/internal/indexeddbcodec"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

func recordToProto(record idb.Record) (*proto.Record, error) {
	return indexeddbcodec.RecordToProto(record)
}

func recordsToProto(records []idb.Record) ([]*proto.Record, error) {
	return indexeddbcodec.RecordsToProto(records)
}

func recordFromProto(record *proto.Record) (idb.Record, error) {
	return indexeddbcodec.RecordFromProto(record)
}

func anyToKeyValue(v any) (*proto.KeyValue, error) {
	return indexeddbcodec.AnyToKeyValue(v)
}
