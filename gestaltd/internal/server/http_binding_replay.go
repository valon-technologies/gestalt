package server

import (
	"sync"
	"time"
)

type httpBindingReplayStore interface {
	MarkIfNew(key string, ttl time.Duration) bool
}

type memoryHTTPBindingReplayStore struct {
	mu      sync.Mutex
	entries map[string]time.Time
}

func newMemoryHTTPBindingReplayStore() httpBindingReplayStore {
	return &memoryHTTPBindingReplayStore{entries: map[string]time.Time{}}
}

func (s *memoryHTTPBindingReplayStore) MarkIfNew(key string, ttl time.Duration) bool {
	if s == nil || key == "" {
		return false
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}

	now := time.Now()
	expiresAt := now.Add(ttl)

	s.mu.Lock()
	defer s.mu.Unlock()

	for existingKey, deadline := range s.entries {
		if !deadline.After(now) {
			delete(s.entries, existingKey)
		}
	}
	if deadline, ok := s.entries[key]; ok && deadline.After(now) {
		return false
	}
	s.entries[key] = expiresAt
	return true
}
