package runtimelogs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ErrSessionNotFound indicates that logs were requested for an unknown or evicted runtime session.
var ErrSessionNotFound = errors.New("runtime session not found")

const (
	memoryStoreMaxSessions          = 256
	memoryStoreMaxRecordsPerSession = 4096
)

// MemoryStore keeps runtime session logs in process memory.
type MemoryStore struct {
	mu       sync.Mutex
	sessions map[string]memorySession
	logs     map[string][]Record
}

type memorySession struct {
	lastUsedAt time.Time
}

// NewMemoryStore returns an empty in-memory runtime log store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		sessions: make(map[string]memorySession),
		logs:     make(map[string][]Record),
	}
}

func (s *MemoryStore) RegisterSession(_ context.Context, registration SessionRegistration) error {
	if s == nil {
		return fmt.Errorf("register runtime session: log store is not configured")
	}
	runtimeProviderName := strings.TrimSpace(registration.RuntimeProviderName)
	sessionID := strings.TrimSpace(registration.SessionID)
	if runtimeProviderName == "" || sessionID == "" {
		return fmt.Errorf("register runtime session: runtime provider name and session id are required")
	}
	key := memorySessionKey(runtimeProviderName, sessionID)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessions == nil {
		s.sessions = make(map[string]memorySession)
	}
	if s.logs == nil {
		s.logs = make(map[string][]Record)
	}
	s.sessions[key] = memorySession{lastUsedAt: time.Now().UTC()}
	s.logs[key] = nil
	s.evictOldSessionsLocked()
	return nil
}

func (s *MemoryStore) AppendSessionLogs(_ context.Context, runtimeProviderName, sessionID string, entries []AppendEntry) (int64, error) {
	if s == nil {
		return 0, fmt.Errorf("append runtime session logs: log store is not configured")
	}
	runtimeProviderName = strings.TrimSpace(runtimeProviderName)
	sessionID = strings.TrimSpace(sessionID)
	if runtimeProviderName == "" || sessionID == "" {
		return 0, fmt.Errorf("append runtime session logs: runtime provider name and session id are required")
	}
	if len(entries) == 0 {
		return 0, nil
	}
	key := memorySessionKey(runtimeProviderName, sessionID)

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[key]; !ok {
		return 0, fmt.Errorf("append runtime session logs: %w", ErrSessionNotFound)
	}
	s.sessions[key] = memorySession{lastUsedAt: time.Now().UTC()}
	current := s.logs[key]
	lastSeq := int64(0)
	if len(current) > 0 {
		lastSeq = current[len(current)-1].Seq
	}
	now := time.Now().UTC()
	for _, entry := range entries {
		if entry.Message == "" {
			continue
		}
		lastSeq++
		observedAt := entry.ObservedAt
		if observedAt.IsZero() {
			observedAt = now
		}
		current = append(current, Record{
			Seq:        lastSeq,
			SourceSeq:  entry.SourceSeq,
			Stream:     Stream(strings.TrimSpace(string(entry.Stream))),
			Message:    entry.Message,
			ObservedAt: observedAt,
			AppendedAt: now,
		})
	}
	if len(current) > memoryStoreMaxRecordsPerSession {
		current = append([]Record(nil), current[len(current)-memoryStoreMaxRecordsPerSession:]...)
	}
	s.logs[key] = current
	s.evictOldSessionsLocked()
	return lastSeq, nil
}

func (s *MemoryStore) ListSessionLogs(_ context.Context, runtimeProviderName, sessionID string, afterSeq int64, limit int) ([]Record, error) {
	if s == nil {
		return nil, fmt.Errorf("list runtime session logs: log store is not configured")
	}
	runtimeProviderName = strings.TrimSpace(runtimeProviderName)
	sessionID = strings.TrimSpace(sessionID)
	if runtimeProviderName == "" || sessionID == "" {
		return nil, fmt.Errorf("list runtime session logs: runtime provider name and session id are required")
	}
	key := memorySessionKey(runtimeProviderName, sessionID)

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[key]; !ok {
		return nil, fmt.Errorf("list runtime session logs: %w", ErrSessionNotFound)
	}
	records := s.logs[key]
	out := make([]Record, 0, len(records))
	for _, record := range records {
		if record.Seq <= afterSeq {
			continue
		}
		out = append(out, record)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *MemoryStore) TailSessionLogs(_ context.Context, runtimeProviderName, sessionID string, limit int) ([]Record, error) {
	if s == nil {
		return nil, fmt.Errorf("tail runtime session logs: log store is not configured")
	}
	runtimeProviderName = strings.TrimSpace(runtimeProviderName)
	sessionID = strings.TrimSpace(sessionID)
	if runtimeProviderName == "" || sessionID == "" {
		return nil, fmt.Errorf("tail runtime session logs: runtime provider name and session id are required")
	}
	if limit <= 0 {
		limit = 20
	}
	key := memorySessionKey(runtimeProviderName, sessionID)

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[key]; !ok {
		return nil, fmt.Errorf("tail runtime session logs: %w", ErrSessionNotFound)
	}
	records := s.logs[key]
	start := len(records) - limit
	if start < 0 {
		start = 0
	}
	out := make([]Record, len(records[start:]))
	copy(out, records[start:])
	return out, nil
}

func (s *MemoryStore) MarkSessionStopped(_ context.Context, runtimeProviderName, sessionID string, stoppedAt time.Time) error {
	if s == nil {
		return nil
	}
	runtimeProviderName = strings.TrimSpace(runtimeProviderName)
	sessionID = strings.TrimSpace(sessionID)
	if runtimeProviderName == "" || sessionID == "" {
		return nil
	}
	now := time.Now().UTC()
	if stoppedAt.IsZero() || stoppedAt.Before(now) {
		stoppedAt = now
	}
	key := memorySessionKey(runtimeProviderName, sessionID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[key]; ok {
		s.sessions[key] = memorySession{lastUsedAt: stoppedAt.UTC()}
	}
	return nil
}

func memorySessionKey(runtimeProviderName, sessionID string) string {
	return strings.TrimSpace(runtimeProviderName) + "\x00" + strings.TrimSpace(sessionID)
}

func (s *MemoryStore) evictOldSessionsLocked() {
	for len(s.sessions) > memoryStoreMaxSessions {
		oldestKey := ""
		oldestAt := time.Time{}
		for key, session := range s.sessions {
			if oldestKey == "" || session.lastUsedAt.Before(oldestAt) {
				oldestKey = key
				oldestAt = session.lastUsedAt
			}
		}
		if oldestKey == "" {
			return
		}
		delete(s.sessions, oldestKey)
		delete(s.logs, oldestKey)
	}
}
