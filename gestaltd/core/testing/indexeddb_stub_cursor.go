package coretesting

import (
	"bytes"
	"fmt"
	"sort"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
)

type stubCursor struct {
	store        *stubObjectStore
	keys         []string
	indexKeys    []any
	snapshot     map[string]idb.Record
	pos          int
	keysOnly     bool
	reverse      bool
	unique       bool
	err          error
	filterIndex  *stubIndex
	filterValues []any
}

func (c *stubCursor) buildIndexKeys() {
	if c.filterIndex == nil {
		return
	}
	kp := c.filterIndex.keyPath()
	if kp == nil {
		return
	}
	c.indexKeys = make([]any, len(c.keys))
	for i, k := range c.keys {
		rec := c.snapshot[k]
		if len(kp) == 1 {
			c.indexKeys[i] = []any{rec[kp[0]]}
		} else {
			vals := make([]any, len(kp))
			for j, field := range kp {
				vals[j] = rec[field]
			}
			c.indexKeys[i] = vals
		}
	}
	sort.Sort(&indexKeySorter{keys: c.keys, indexKeys: c.indexKeys, reverse: c.reverse})
}

type indexKeySorter struct {
	keys      []string
	indexKeys []any
	reverse   bool
}

func (s *indexKeySorter) Len() int { return len(s.keys) }

func (s *indexKeySorter) Swap(i, j int) {
	s.keys[i], s.keys[j] = s.keys[j], s.keys[i]
	s.indexKeys[i], s.indexKeys[j] = s.indexKeys[j], s.indexKeys[i]
}

func (s *indexKeySorter) Less(i, j int) bool {
	cmp := compareIndexKeys(s.indexKeys[i], s.indexKeys[j])
	if cmp == 0 {
		cmp = compareIndexKeys(s.keys[i], s.keys[j])
	}
	if s.reverse {
		return cmp > 0
	}
	return cmp < 0
}

func compareIndexKeys(a, b any) int {
	switch av := a.(type) {
	case []any:
		if bv, ok := b.([]any); ok {
			for i := range av {
				if i >= len(bv) {
					return 1
				}
				if cmp := compareIndexKeys(av[i], bv[i]); cmp != 0 {
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
			if av < bv {
				return -1
			}
			if av > bv {
				return 1
			}
			return 0
		}
	case int:
		if bv, ok := b.(int); ok {
			if av < bv {
				return -1
			}
			if av > bv {
				return 1
			}
			return 0
		}
	case int64:
		if bv, ok := b.(int64); ok {
			if av < bv {
				return -1
			}
			if av > bv {
				return 1
			}
			return 0
		}
	case float64:
		if bv, ok := b.(float64); ok {
			if av < bv {
				return -1
			}
			if av > bv {
				return 1
			}
			return 0
		}
	case []byte:
		if bv, ok := b.([]byte); ok {
			return bytes.Compare(av, bv)
		}
	}
	// Coerce numeric types for cross-type comparison (e.g. int vs int64 after gRPC round-trip).
	af, aOk := toFloat64(a)
	bf, bOk := toFloat64(b)
	if aOk && bOk {
		if af < bf {
			return -1
		}
		if af > bf {
			return 1
		}
		return 0
	}
	as, bs := fmt.Sprint(a), fmt.Sprint(b)
	if as < bs {
		return -1
	}
	if as > bs {
		return 1
	}
	return 0
}

func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	default:
		return 0, false
	}
}

func (c *stubCursor) applyKeyRange(r *idb.KeyRange) {
	if r == nil {
		return
	}
	lower, upper := r.Lower, r.Upper
	if c.indexKeys != nil {
		lower = normalizeIndexRangeBound(lower)
		upper = normalizeIndexRangeBound(upper)
	}
	filtered := make([]string, 0, len(c.keys))
	var filteredIdx []any
	for i, k := range c.keys {
		var cur any = k
		if c.indexKeys != nil {
			cur = c.indexKeys[i]
		}
		if lower != nil {
			cmp := compareIndexKeys(cur, lower)
			if r.LowerOpen && cmp <= 0 {
				continue
			}
			if !r.LowerOpen && cmp < 0 {
				continue
			}
		}
		if upper != nil {
			cmp := compareIndexKeys(cur, upper)
			if r.UpperOpen && cmp >= 0 {
				continue
			}
			if !r.UpperOpen && cmp > 0 {
				continue
			}
		}
		filtered = append(filtered, k)
		if c.indexKeys != nil {
			filteredIdx = append(filteredIdx, c.indexKeys[i])
		}
	}
	c.keys = filtered
	if c.indexKeys != nil {
		c.indexKeys = filteredIdx
	}
}

func normalizeIndexRangeBound(bound any) any {
	if bound == nil {
		return nil
	}
	if _, ok := bound.([]any); ok {
		return bound
	}
	return []any{bound}
}

func (c *stubCursor) applyIndexFilter() {
	if c.filterIndex == nil {
		return
	}
	filtered := c.keys[:0]
	for _, k := range c.keys {
		if rec, ok := c.snapshot[k]; ok && c.filterIndex.matches(rec, c.filterValues) {
			filtered = append(filtered, k)
		}
	}
	c.keys = filtered
}

func (c *stubCursor) Continue() bool {
	if c.err != nil {
		return false
	}
	if c.unique && c.indexKeys != nil && c.pos >= 0 && c.pos < len(c.indexKeys) {
		prev := c.indexKeys[c.pos]
		for c.pos++; c.pos < len(c.keys); c.pos++ {
			if compareIndexKeys(c.indexKeys[c.pos], prev) != 0 {
				return true
			}
		}
		return false
	}
	c.pos++
	return c.pos < len(c.keys)
}

func (c *stubCursor) ContinueToKey(key any) bool {
	if c.err != nil {
		return false
	}
	var prevKey any
	if c.unique && c.indexKeys != nil && c.pos >= 0 && c.pos < len(c.indexKeys) {
		prevKey = c.indexKeys[c.pos]
	}
	for c.pos++; c.pos < len(c.keys); c.pos++ {
		var cur any = c.keys[c.pos]
		if c.indexKeys != nil {
			cur = c.indexKeys[c.pos]
		}
		if c.unique && prevKey != nil && compareIndexKeys(cur, prevKey) == 0 {
			continue
		}
		cmp := compareIndexKeys(cur, key)
		if c.reverse {
			if cmp <= 0 {
				return true
			}
		} else {
			if cmp >= 0 {
				return true
			}
		}
	}
	return false
}

func (c *stubCursor) Advance(count int) bool {
	if count <= 0 {
		c.err = fmt.Errorf("advance count must be positive")
		return false
	}
	for i := 0; i <= count; i++ {
		if !c.Continue() {
			return false
		}
	}
	return true
}

func (c *stubCursor) Key() any {
	if c.pos < 0 || c.pos >= len(c.keys) {
		return nil
	}
	if c.indexKeys != nil {
		return c.indexKeys[c.pos]
	}
	return c.keys[c.pos]
}

func (c *stubCursor) PrimaryKey() string {
	if c.pos < 0 || c.pos >= len(c.keys) {
		return ""
	}
	return c.keys[c.pos]
}

func (c *stubCursor) Value() (idb.Record, error) {
	if c.keysOnly {
		return nil, idb.ErrKeysOnly
	}
	if c.pos < 0 || c.pos >= len(c.keys) {
		return nil, idb.ErrNotFound
	}
	return c.snapshot[c.keys[c.pos]], nil
}

func (c *stubCursor) Delete() error {
	if c.pos < 0 || c.pos >= len(c.keys) {
		return idb.ErrNotFound
	}
	c.store.mu.Lock()
	delete(c.store.records, c.keys[c.pos])
	c.store.mu.Unlock()
	return nil
}

func (c *stubCursor) Update(value idb.Record) error {
	if c.pos < 0 || c.pos >= len(c.keys) {
		return idb.ErrNotFound
	}
	curID := c.keys[c.pos]
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	if c.store.hasUniqueConflict(value, &curID) {
		return idb.ErrAlreadyExists
	}
	c.store.records[curID] = value
	c.snapshot[curID] = value
	return nil
}

func (c *stubCursor) Err() error   { return c.err }
func (c *stubCursor) Close() error { return nil }
