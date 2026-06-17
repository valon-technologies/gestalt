package indexeddb

import "github.com/valon-technologies/gestalt/sdk/go/client"

// ToQuery converts an ergonomic query (nil, key, *KeyRange, or native query) to
// the sdkgen-native query type. Nil means all records. Invalid keys panic.
//
// Query parameters accept any because Go has no recursive union type for
// IndexedDB keys (number, date, string, binary, or array thereof). Pass a bare
// scalar or []any composite key, a *KeyRange from Only/Bound/LowerBound/
// UpperBound, or a native *Query.
func ToQuery(query any) *client.IndexedDBQuery {
	if query == nil {
		return nil
	}
	switch q := query.(type) {
	case *client.KeyRange:
		return &client.IndexedDBQuery{Query: &client.IndexedDBQueryQueryRange{Value: q}}
	case *client.IndexedDBQuery:
		return q
	default:
		kv := mustKeyValue(query)
		return &client.IndexedDBQuery{Query: &client.IndexedDBQueryQueryKey{Value: kv}}
	}
}
