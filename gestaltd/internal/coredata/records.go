package coredata

import "github.com/valon-technologies/gestalt/server/core/indexeddb"

func cloneRecord(rec indexeddb.Record) indexeddb.Record {
	if rec == nil {
		return nil
	}
	clone := make(indexeddb.Record, len(rec))
	for key, value := range rec {
		clone[key] = value
	}
	return clone
}
