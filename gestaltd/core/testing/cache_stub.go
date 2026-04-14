package coretesting

import (
	"context"
	"sync"
	"time"

	corecache "github.com/valon-technologies/gestalt/server/core/cache"
)

type StubCache struct {
	mu      sync.RWMutex
	entries map[string]stubCacheEntry
	now     func() time.Time
}

type stubCacheEntry struct {
	value     []byte
	expiresAt time.Time
}

func NewStubCache() *StubCache {
	return &StubCache{
		entries: make(map[string]stubCacheEntry),
		now:     time.Now,
	}
}

func (s *StubCache) Get(_ context.Context, key string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.getLocked(key)
	if !ok {
		return nil, false, nil
	}
	return cloneBytes(entry.value), true, nil
}

func (s *StubCache) GetMany(_ context.Context, keys []string) (map[string][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make(map[string][]byte, len(keys))
	for _, key := range keys {
		entry, ok := s.getLocked(key)
		if !ok {
			continue
		}
		out[key] = cloneBytes(entry.value)
	}
	return out, nil
}

func (s *StubCache) Set(_ context.Context, key string, value []byte, opts corecache.SetOptions) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.entries[key] = stubCacheEntry{
		value:     cloneBytes(value),
		expiresAt: s.expiry(opts.TTL),
	}
	return nil
}

func (s *StubCache) SetMany(_ context.Context, entries []corecache.Entry, opts corecache.SetOptions) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	expiresAt := s.expiry(opts.TTL)
	for _, entry := range entries {
		s.entries[entry.Key] = stubCacheEntry{
			value:     cloneBytes(entry.Value),
			expiresAt: expiresAt,
		}
	}
	return nil
}

func (s *StubCache) Delete(_ context.Context, key string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.getLocked(key); !ok {
		return false, nil
	}
	delete(s.entries, key)
	return true, nil
}

func (s *StubCache) DeleteMany(_ context.Context, keys []string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var deleted int64
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if _, ok := s.getLocked(key); !ok {
			continue
		}
		delete(s.entries, key)
		deleted++
	}
	return deleted, nil
}

func (s *StubCache) Touch(_ context.Context, key string, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.getLocked(key)
	if !ok {
		return false, nil
	}
	entry.expiresAt = s.expiry(ttl)
	s.entries[key] = entry
	return true, nil
}

func (s *StubCache) Ping(context.Context) error { return nil }

func (s *StubCache) Close() error { return nil }

func (s *StubCache) getLocked(key string) (stubCacheEntry, bool) {
	entry, ok := s.entries[key]
	if !ok {
		return stubCacheEntry{}, false
	}
	if !entry.expiresAt.IsZero() && !entry.expiresAt.After(s.now()) {
		delete(s.entries, key)
		return stubCacheEntry{}, false
	}
	return entry, true
}

func (s *StubCache) expiry(ttl time.Duration) time.Time {
	if ttl <= 0 {
		return time.Time{}
	}
	return s.now().Add(ttl)
}

func cloneBytes(value []byte) []byte {
	if value == nil {
		return nil
	}
	return append([]byte(nil), value...)
}

var _ corecache.Cache = (*StubCache)(nil)
