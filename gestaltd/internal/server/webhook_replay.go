package server

import (
	"sync"
	"time"
)

type webhookReplayStore interface {
	MarkIfNew(key string, ttl time.Duration) bool
}

type memoryWebhookReplayStore struct {
	mu      sync.Mutex
	entries map[string]time.Time
}

func newMemoryWebhookReplayStore() webhookReplayStore {
	return &memoryWebhookReplayStore{entries: map[string]time.Time{}}
}

func (s *memoryWebhookReplayStore) MarkIfNew(key string, ttl time.Duration) bool {
	if s == nil || key == "" {
		return true
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
