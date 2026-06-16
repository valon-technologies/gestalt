package coretesting

import (
	"fmt"
	"sort"

	sdkclient "github.com/valon-technologies/gestalt/sdk/go/client"
	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	"github.com/valon-technologies/gestalt/server/internal/indexeddbcodec"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

type stubCursor struct {
	store       *stubObjectStore
	keys        []string
	indexKeys   []any
	snapshot    map[string]idb.Record
	pos         int
	keysOnly    bool
	reverse     bool
	unique      bool
	err         error
	filterIndex *stubIndex
	query       *sdkclient.IndexedDBQuery
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
			c.indexKeys[i] = rec[kp[0]]
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
	cmp := indexeddbcodec.CompareKeys(s.indexKeys[i], s.indexKeys[j])
	if cmp == 0 {
		cmp = indexeddbcodec.CompareKeys(s.keys[i], s.keys[j])
	}
	if s.reverse {
		return cmp > 0
	}
	return cmp < 0
}

func normalizeStubQuery(query any) *sdkclient.IndexedDBQuery {
	if query == nil {
		return nil
	}
	switch q := query.(type) {
	case *sdkclient.IndexedDBQuery:
		return q
	case *proto.IndexedDBQuery:
		return sdkclient.FromWireIndexedDBQuery(q)
	default:
		return idb.ToQuery(query)
	}
}

func (c *stubCursor) applyQuery(query any) {
	nativeQuery := normalizeStubQuery(query)
	c.query = nativeQuery
	filtered := make([]string, 0, len(c.keys))
	var filteredIdx []any
	for i, k := range c.keys {
		var cur any = k
		if c.indexKeys != nil {
			cur = c.indexKeys[i]
		}
		ok, err := idb.MatchQuery(cur, nativeQuery)
		if err != nil {
			c.err = err
			return
		}
		if !ok {
			continue
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

func (c *stubCursor) Continue() bool {
	if c.err != nil {
		return false
	}
	if c.unique && c.indexKeys != nil && c.pos >= 0 && c.pos < len(c.indexKeys) {
		prev := c.indexKeys[c.pos]
		for c.pos++; c.pos < len(c.keys); c.pos++ {
			if indexeddbcodec.CompareKeys(c.indexKeys[c.pos], prev) != 0 {
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
	if key == nil {
		c.err = fmt.Errorf("continue key is required")
		return false
	}
	kv, err := idb.CursorKeyToProto(key)
	if err != nil {
		c.err = fmt.Errorf("indexeddb: invalid continue key: %w", err)
		return false
	}
	target, err := idb.KeyValueToAny(kv)
	if err != nil {
		c.err = err
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
		if c.unique && prevKey != nil && indexeddbcodec.CompareKeys(cur, prevKey) == 0 {
			continue
		}
		cmp := indexeddbcodec.CompareKeys(cur, target)
		if c.reverse {
			if cmp <= 0 {
				return true
			}
		} else if cmp >= 0 {
			return true
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

func applyCountLimit[T any](items []T, count ...uint32) []T {
	if len(count) == 0 || count[0] == 0 {
		return items
	}
	limit := int(count[0])
	if limit >= len(items) {
		return items
	}
	return items[:limit]
}
