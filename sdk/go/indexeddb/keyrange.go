package indexeddb

import (
	"fmt"

	"github.com/valon-technologies/gestalt/sdk/go/client"
	"github.com/valon-technologies/gestalt/sdk/go/internal/indexeddbcodec"
)

// KeyRange is a W3C IDBKeyRange-shaped bound on full IndexedDB keys.
type KeyRange = client.KeyRange

// KeyValue is one IndexedDB key (scalar or nested array).
type KeyValue = client.KeyValue

// Query is a single IndexedDB query argument (exact key or key range).
type Query = client.IndexedDBQuery

func mustKeyValue(v any) *client.KeyValue {
	wire, err := indexeddbcodec.AnyToKeyValue(v)
	if err != nil {
		panic(fmt.Errorf("indexeddb: invalid key: %w", err))
	}
	return client.FromWireKeyValue(wire)
}

// Only returns a range containing only key v.
func Only(v any) *KeyRange {
	kv := mustKeyValue(v)
	return &KeyRange{Lower: kv, Upper: kv}
}

// Bound returns a range between lower and upper with optional open bounds.
func Bound(lower, upper any, lowerOpen, upperOpen bool) *KeyRange {
	kr := &KeyRange{LowerOpen: lowerOpen, UpperOpen: upperOpen}
	if lower != nil {
		kr.Lower = mustKeyValue(lower)
	}
	if upper != nil {
		kr.Upper = mustKeyValue(upper)
	}
	return kr
}

// LowerBound returns a range with only a lower bound.
func LowerBound(v any, open bool) *KeyRange {
	return &KeyRange{Lower: mustKeyValue(v), LowerOpen: open}
}

// UpperBound returns a range with only an upper bound.
func UpperBound(v any, open bool) *KeyRange {
	return &KeyRange{Upper: mustKeyValue(v), UpperOpen: open}
}
