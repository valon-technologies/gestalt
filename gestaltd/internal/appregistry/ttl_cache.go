package appregistry

import (
	"sync"
	"time"
)

// noExpiry is a TTL for values that never go stale.
const noExpiry time.Duration = 0

type ttlItem[V any] struct {
	value   V
	expires time.Time
	forever bool
}

// ttlCache is a bounded map whose entries expire after a per-entry TTL. When
// full, it drops everything rather than tracking recency.
type ttlCache[K comparable, V any] struct {
	now   func() time.Time
	limit int

	mu    sync.Mutex
	items map[K]ttlItem[V]
}

func newTTLCache[K comparable, V any](limit int, now func() time.Time) *ttlCache[K, V] {
	return &ttlCache[K, V]{now: now, limit: limit, items: make(map[K]ttlItem[V])}
}

func (c *ttlCache[K, V]) get(key K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	item, ok := c.items[key]
	if ok && !item.forever && !c.now().Before(item.expires) {
		delete(c.items, key)
		ok = false
	}
	if !ok {
		var zero V
		return zero, false
	}
	return item.value, true
}

func (c *ttlCache[K, V]) put(key K, value V, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.items[key]; !exists && len(c.items) >= c.limit {
		clear(c.items)
	}
	c.items[key] = ttlItem[V]{value: value, expires: c.now().Add(ttl), forever: ttl == noExpiry}
}
