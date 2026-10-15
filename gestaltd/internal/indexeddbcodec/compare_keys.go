package indexeddbcodec

import (
	"fmt"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

// CompareKeys compares two native IndexedDB keys using W3C ordering:
// number < date < string < binary < array, with element-wise array compare
// and shorter prefixes sorting first.
func CompareKeys(a, b any) int {
	return idb.CompareKeys(a, b)
}

// KeyInRange reports whether key satisfies kr. A nil range includes all keys.
func KeyInRange(key any, kr *proto.KeyRange) (bool, error) {
	if kr == nil {
		return true, nil
	}
	if kr.GetLower() != nil {
		lower, err := KeyValueToAny(kr.GetLower())
		if err != nil {
			return false, err
		}
		cmp := CompareKeys(key, lower)
		if kr.GetLowerOpen() {
			if cmp <= 0 {
				return false, nil
			}
		} else if cmp < 0 {
			return false, nil
		}
	}
	if kr.GetUpper() != nil {
		upper, err := KeyValueToAny(kr.GetUpper())
		if err != nil {
			return false, err
		}
		cmp := CompareKeys(key, upper)
		if kr.GetUpperOpen() {
			if cmp >= 0 {
				return false, nil
			}
		} else if cmp > 0 {
			return false, nil
		}
	}
	return true, nil
}

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
