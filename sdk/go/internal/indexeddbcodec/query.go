package indexeddbcodec

import (
	"fmt"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

// MatchQuery reports whether key satisfies query. A nil or unset query matches
// all keys, matching W3C undefined/null query arguments.
func MatchQuery(key any, query *proto.IndexedDBQuery) (bool, error) {
	if query == nil {
		return true, nil
	}
	switch q := query.GetQuery().(type) {
	case *proto.IndexedDBQuery_Key:
		target, err := KeyValueToAny(q.Key)
		if err != nil {
			return false, err
		}
		return CompareKeys(key, target) == 0, nil
	case *proto.IndexedDBQuery_Range:
		if q.Range == nil {
			return false, fmt.Errorf("indexeddb query range is required")
		}
		return KeyInRange(key, q.Range)
	default:
		return true, nil
	}
}
