package gestalt

import (
	"bytes"
	"fmt"
	"math/big"
	"reflect"
	"sort"
	"time"
)

// IndexedDBCursorSnapshotEntry is one provider-side cursor row.
type IndexedDBCursorSnapshotEntry struct {
	// Key is the cursor key. For index cursors, this is the index key.
	Key any
	// PrimaryKey is the canonical object-store primary key for the row.
	PrimaryKey string
	// PrimaryKeyValue is the primary key value used as a stable tie-breaker
	// when multiple index rows share the same index key.
	PrimaryKeyValue any
	// Record is the row value. It is omitted when a key-only cursor entry is
	// returned to clients.
	Record Record
}

// IndexedDBCursorSnapshot provides IndexedDB cursor ordering, range filtering,
// and movement semantics for native providers.
type IndexedDBCursorSnapshot struct {
	// IndexCursor indicates whether Key contains secondary-index values.
	IndexCursor bool
	// KeysOnly indicates whether callers should omit Record from returned
	// cursor entries.
	KeysOnly bool
	// Reverse orders entries from greatest to least key.
	Reverse bool
	// Unique collapses duplicate index keys while iterating.
	Unique bool
	// Entries is the sorted and range-filtered snapshot used by cursor movement.
	Entries []IndexedDBCursorSnapshotEntry
	// Pos is the current cursor position. A value of -1 means unpositioned.
	Pos int
}

// NewIndexedDBCursorSnapshot creates an empty provider-side cursor snapshot
// configured from a native open-cursor request.
func NewIndexedDBCursorSnapshot(req IndexedDBOpenCursorRequest) IndexedDBCursorSnapshot {
	return IndexedDBCursorSnapshot{
		IndexCursor: req.Index != "",
		KeysOnly:    req.KeysOnly,
		Reverse:     req.Direction == CursorPrev || req.Direction == CursorPrevUnique,
		Unique:      req.Direction == CursorNextUnique || req.Direction == CursorPrevUnique,
		Pos:         -1,
	}
}

// Load sorts entries, applies the supplied key range, and stores the resulting
// cursor snapshot.
func (s *IndexedDBCursorSnapshot) Load(entries []IndexedDBCursorSnapshotEntry, r *KeyRange) error {
	sort.Slice(entries, func(i, j int) bool {
		cmp := CompareIndexedDBValues(entries[i].Key, entries[j].Key)
		if cmp == 0 {
			cmp = CompareIndexedDBValues(entries[i].PrimaryKeyValue, entries[j].PrimaryKeyValue)
		}
		if s.Reverse {
			return cmp > 0
		}
		return cmp < 0
	})

	filtered, err := s.ApplyRange(entries, r)
	if err != nil {
		return err
	}
	s.Entries = filtered
	s.Pos = -1
	return nil
}

// ApplyRange returns entries that satisfy the supplied key range without
// mutating the snapshot.
func (s *IndexedDBCursorSnapshot) ApplyRange(entries []IndexedDBCursorSnapshotEntry, r *KeyRange) ([]IndexedDBCursorSnapshotEntry, error) {
	if r == nil {
		return entries, nil
	}
	lower, upper, err := IndexedDBRangeBounds(r, s.IndexCursor)
	if err != nil {
		return nil, err
	}
	filtered := make([]IndexedDBCursorSnapshotEntry, 0, len(entries))
	for _, entry := range entries {
		key := normalizeIndexedDBBound(entry.Key, s.IndexCursor)
		if lower != nil {
			cmp := CompareIndexedDBValues(key, lower)
			if r.LowerOpen && cmp <= 0 {
				continue
			}
			if !r.LowerOpen && cmp < 0 {
				continue
			}
		}
		if upper != nil {
			cmp := CompareIndexedDBValues(key, upper)
			if r.UpperOpen && cmp >= 0 {
				continue
			}
			if !r.UpperOpen && cmp > 0 {
				continue
			}
		}
		filtered = append(filtered, entry)
	}
	return filtered, nil
}

// Next advances the snapshot to the next entry. It returns nil when the
// snapshot is exhausted.
func (s *IndexedDBCursorSnapshot) Next() (*IndexedDBCursorSnapshotEntry, error) {
	if s.Unique && s.IndexCursor && s.Pos >= 0 && s.Pos < len(s.Entries) {
		prev := s.Entries[s.Pos].Key
		for s.Pos++; s.Pos < len(s.Entries); s.Pos++ {
			if CompareIndexedDBValues(s.Entries[s.Pos].Key, prev) != 0 {
				return s.Current()
			}
		}
		return nil, nil
	}

	s.Pos++
	if s.Pos >= len(s.Entries) {
		return nil, nil
	}
	return s.Current()
}

// ContinueToKey advances to target or the next entry past target according to
// the snapshot direction. It returns nil when the snapshot is exhausted.
func (s *IndexedDBCursorSnapshot) ContinueToKey(target any) (*IndexedDBCursorSnapshotEntry, error) {
	var prev any
	if s.Unique && s.IndexCursor && s.Pos >= 0 && s.Pos < len(s.Entries) {
		prev = s.Entries[s.Pos].Key
	}
	for s.Pos++; s.Pos < len(s.Entries); s.Pos++ {
		cur := s.Entries[s.Pos].Key
		if prev != nil && s.Unique && s.IndexCursor && CompareIndexedDBValues(cur, prev) == 0 {
			continue
		}
		cmp := CompareIndexedDBValues(cur, target)
		if s.Reverse {
			if cmp <= 0 {
				return s.Current()
			}
			continue
		}
		if cmp >= 0 {
			return s.Current()
		}
	}
	return nil, nil
}

// Advance skips count entries from the current position and returns the new
// current entry. It returns nil when the snapshot is exhausted.
func (s *IndexedDBCursorSnapshot) Advance(count int) (*IndexedDBCursorSnapshotEntry, error) {
	if count <= 0 {
		return nil, InvalidArgument("advance count must be positive")
	}
	var entry *IndexedDBCursorSnapshotEntry
	for i := 0; i < count; i++ {
		var err error
		entry, err = s.Next()
		if entry == nil || err != nil {
			return entry, err
		}
	}
	return entry, nil
}

// Current returns the currently positioned entry.
func (s *IndexedDBCursorSnapshot) Current() (*IndexedDBCursorSnapshotEntry, error) {
	if s.Pos < 0 || s.Pos >= len(s.Entries) {
		return nil, ErrNotFound
	}
	return &s.Entries[s.Pos], nil
}

// IndexedDBRangeBounds normalizes range bounds for object-store and index
// cursor comparisons. Index cursor scalar bounds are compared as single-part
// composite keys.
func IndexedDBRangeBounds(r *KeyRange, indexCursor bool) (any, any, error) {
	if r == nil {
		return nil, nil, nil
	}
	var lower any
	if r.Lower != nil {
		lower = normalizeIndexedDBBound(r.Lower, indexCursor)
	}
	var upper any
	if r.Upper != nil {
		upper = normalizeIndexedDBBound(r.Upper, indexCursor)
	}
	return lower, upper, nil
}

func normalizeIndexedDBBound(value any, indexCursor bool) any {
	if !indexCursor {
		return value
	}
	if parts, ok := indexedDBArrayParts(value); ok {
		return parts
	}
	return []any{value}
}

func indexedDBArrayParts(v any) ([]any, bool) {
	if arr, ok := v.([]any); ok {
		return append([]any(nil), arr...), true
	}
	if _, ok := v.([]byte); ok {
		return nil, false
	}
	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return nil, false
	}
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
	default:
		return nil, false
	}
	if rv.Type().Elem().Kind() == reflect.Uint8 {
		return nil, false
	}
	parts := make([]any, rv.Len())
	for i := range parts {
		parts[i] = rv.Index(i).Interface()
	}
	return parts, true
}

// CompareIndexedDBValues compares native IndexedDB key values using the ordering
// shared by the SDK cursor snapshot helpers.
func CompareIndexedDBValues(a, b any) int {
	switch av := a.(type) {
	case []any:
		if bv, ok := b.([]any); ok {
			for i := range av {
				if i >= len(bv) {
					return 1
				}
				if cmp := CompareIndexedDBValues(av[i], bv[i]); cmp != 0 {
					return cmp
				}
			}
			if len(av) < len(bv) {
				return -1
			}
			return 0
		}
	case string:
		if bv, ok := b.(string); ok {
			switch {
			case av < bv:
				return -1
			case av > bv:
				return 1
			default:
				return 0
			}
		}
	case time.Time:
		if bv, ok := b.(time.Time); ok {
			switch {
			case av.Before(bv):
				return -1
			case av.After(bv):
				return 1
			default:
				return 0
			}
		}
	case []byte:
		if bv, ok := b.([]byte); ok {
			return bytes.Compare(av, bv)
		}
	case bool:
		if bv, ok := b.(bool); ok {
			switch {
			case !av && bv:
				return -1
			case av && !bv:
				return 1
			default:
				return 0
			}
		}
	}

	if af, ok := indexedDBNumber(a); ok {
		if bf, ok := indexedDBNumber(b); ok {
			return af.Cmp(bf)
		}
	}

	as := fmt.Sprint(a)
	bs := fmt.Sprint(b)
	switch {
	case as < bs:
		return -1
	case as > bs:
		return 1
	default:
		return 0
	}
}

func indexedDBNumber(v any) (*big.Rat, bool) {
	switch n := v.(type) {
	case int:
		return big.NewRat(int64(n), 1), true
	case int8:
		return big.NewRat(int64(n), 1), true
	case int16:
		return big.NewRat(int64(n), 1), true
	case int32:
		return big.NewRat(int64(n), 1), true
	case int64:
		return big.NewRat(n, 1), true
	case uint:
		return new(big.Rat).SetInt(new(big.Int).SetUint64(uint64(n))), true
	case uint8:
		return new(big.Rat).SetInt(new(big.Int).SetUint64(uint64(n))), true
	case uint16:
		return new(big.Rat).SetInt(new(big.Int).SetUint64(uint64(n))), true
	case uint32:
		return new(big.Rat).SetInt(new(big.Int).SetUint64(uint64(n))), true
	case uint64:
		return new(big.Rat).SetInt(new(big.Int).SetUint64(n)), true
	case float32:
		return indexedDBFloatRat(float64(n))
	case float64:
		return indexedDBFloatRat(n)
	default:
		return nil, false
	}
}

func indexedDBFloatRat(v float64) (*big.Rat, bool) {
	r := new(big.Rat).SetFloat64(v)
	if r == nil {
		return nil, false
	}
	return r, true
}
