package coredata

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	"github.com/valon-technologies/gestalt/server/internal/runtimelogs"
)

type RuntimeSessionLogService struct {
	sessions indexeddb.ObjectStore
	logs     indexeddb.ObjectStore
	mu       sync.Mutex
}

func NewRuntimeSessionLogService(ds indexeddb.IndexedDB) *RuntimeSessionLogService {
	return &RuntimeSessionLogService{
		sessions: ds.ObjectStore(StoreRuntimeSessions),
		logs:     ds.ObjectStore(StoreRuntimeSessionLogs),
	}
}

func (s *RuntimeSessionLogService) RegisterSession(ctx context.Context, registration runtimelogs.SessionRegistration) error {
	if s == nil || s.sessions == nil {
		return fmt.Errorf("register runtime session: session store is not configured")
	}
	runtimeProviderName := strings.TrimSpace(registration.RuntimeProviderName)
	sessionID := strings.TrimSpace(registration.SessionID)
	if runtimeProviderName == "" || sessionID == "" {
		return fmt.Errorf("register runtime session: runtime provider name and session id are required")
	}
	now := time.Now().UTC()
	id := runtimeSessionStoreID(runtimeProviderName, sessionID)
	rec := indexeddb.Record{
		"id":                    id,
		"runtime_provider_name": runtimeProviderName,
		"session_id":            sessionID,
		"provider_name":         strings.TrimSpace(registration.Metadata["provider_name"]),
		"provider_kind":         strings.TrimSpace(registration.Metadata["provider_kind"]),
		"owner_kind":            strings.TrimSpace(registration.Metadata["owner_kind"]),
		"owner_id":              strings.TrimSpace(registration.Metadata["owner_id"]),
		"created_at":            now,
		"updated_at":            now,
	}
	if metadataJSON, err := marshalJSON(registration.Metadata); err != nil {
		return fmt.Errorf("register runtime session: marshal metadata: %w", err)
	} else if metadataJSON != "" {
		rec["metadata_json"] = metadataJSON
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	existing, err := s.sessions.Get(ctx, id)
	switch {
	case err == nil:
		rec["created_at"] = existing["created_at"]
		rec["stopped_at"] = existing["stopped_at"]
		if rec["provider_name"] == "" {
			rec["provider_name"] = recString(existing, "provider_name")
		}
		if rec["provider_kind"] == "" {
			rec["provider_kind"] = recString(existing, "provider_kind")
		}
		if rec["owner_kind"] == "" {
			rec["owner_kind"] = recString(existing, "owner_kind")
		}
		if rec["owner_id"] == "" {
			rec["owner_id"] = recString(existing, "owner_id")
		}
		if rec["metadata_json"] == "" {
			rec["metadata_json"] = recString(existing, "metadata_json")
		}
		if err := s.sessions.Put(ctx, rec); err != nil {
			return fmt.Errorf("register runtime session: %w", err)
		}
		return nil
	case err != indexeddb.ErrNotFound:
		return fmt.Errorf("register runtime session: %w", err)
	default:
		if err := s.sessions.Add(ctx, rec); err != nil {
			return fmt.Errorf("register runtime session: %w", err)
		}
		return nil
	}
}

func (s *RuntimeSessionLogService) AppendSessionLogs(ctx context.Context, runtimeProviderName, sessionID string, entries []runtimelogs.AppendEntry) (int64, error) {
	if s == nil || s.logs == nil || s.sessions == nil {
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

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.sessions.Get(ctx, runtimeSessionStoreID(runtimeProviderName, sessionID)); err != nil {
		return 0, fmt.Errorf("append runtime session logs: %w", err)
	}

	lastSeq, err := s.lastSeq(ctx, runtimeProviderName, sessionID)
	if err != nil {
		return 0, err
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
		rec := indexeddb.Record{
			"id":                    uuid.NewString(),
			"runtime_provider_name": runtimeProviderName,
			"session_id":            sessionID,
			"seq":                   lastSeq,
			"source_seq":            entry.SourceSeq,
			"stream":                strings.TrimSpace(string(entry.Stream)),
			"message":               entry.Message,
			"observed_at":           observedAt,
			"appended_at":           now,
		}
		if err := s.logs.Add(ctx, rec); err != nil {
			return 0, fmt.Errorf("append runtime session logs: %w", err)
		}
	}

	sessionRec, err := s.sessions.Get(ctx, runtimeSessionStoreID(runtimeProviderName, sessionID))
	if err == nil {
		sessionRec["updated_at"] = now
		if putErr := s.sessions.Put(ctx, sessionRec); putErr != nil {
			return 0, fmt.Errorf("append runtime session logs: update session timestamp: %w", putErr)
		}
	}
	return lastSeq, nil
}

func (s *RuntimeSessionLogService) ListSessionLogs(ctx context.Context, runtimeProviderName, sessionID string, afterSeq int64, limit int) ([]runtimelogs.Record, error) {
	if s == nil || s.logs == nil {
		return nil, fmt.Errorf("list runtime session logs: log store is not configured")
	}
	runtimeProviderName = strings.TrimSpace(runtimeProviderName)
	sessionID = strings.TrimSpace(sessionID)
	if runtimeProviderName == "" || sessionID == "" {
		return nil, fmt.Errorf("list runtime session logs: runtime provider name and session id are required")
	}
	if _, err := s.sessions.Get(ctx, runtimeSessionStoreID(runtimeProviderName, sessionID)); err != nil {
		return nil, fmt.Errorf("list runtime session logs: %w", err)
	}
	recs, err := s.logs.Index("by_session").GetAll(ctx, nil, runtimeProviderName, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list runtime session logs: %w", err)
	}
	out := make([]runtimelogs.Record, 0, len(recs))
	for _, rec := range recs {
		value := runtimeSessionLogRecordFromRecord(rec)
		if value.Seq <= afterSeq {
			continue
		}
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Seq < out[j].Seq
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *RuntimeSessionLogService) TailSessionLogs(ctx context.Context, runtimeProviderName, sessionID string, limit int) ([]runtimelogs.Record, error) {
	if s == nil || s.logs == nil {
		return nil, fmt.Errorf("tail runtime session logs: log store is not configured")
	}
	runtimeProviderName = strings.TrimSpace(runtimeProviderName)
	sessionID = strings.TrimSpace(sessionID)
	if runtimeProviderName == "" || sessionID == "" {
		return nil, fmt.Errorf("tail runtime session logs: runtime provider name and session id are required")
	}
	if _, err := s.sessions.Get(ctx, runtimeSessionStoreID(runtimeProviderName, sessionID)); err != nil {
		return nil, fmt.Errorf("tail runtime session logs: %w", err)
	}
	if limit <= 0 {
		limit = 20
	}
	cursor, err := s.logs.Index("by_session_seq").OpenCursor(ctx, nil, indexeddb.CursorPrev, runtimeProviderName, sessionID)
	if err != nil {
		return nil, fmt.Errorf("tail runtime session logs: %w", err)
	}
	defer func() { _ = cursor.Close() }()

	out := make([]runtimelogs.Record, 0, limit)
	for cursor.Continue() {
		rec, valueErr := cursor.Value()
		if valueErr != nil {
			return nil, fmt.Errorf("tail runtime session logs: %w", valueErr)
		}
		out = append(out, runtimeSessionLogRecordFromRecord(rec))
		if len(out) >= limit {
			break
		}
	}
	if err := cursor.Err(); err != nil {
		return nil, fmt.Errorf("tail runtime session logs: %w", err)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

func (s *RuntimeSessionLogService) MarkSessionStopped(ctx context.Context, runtimeProviderName, sessionID string, stoppedAt time.Time) error {
	if s == nil || s.sessions == nil {
		return nil
	}
	runtimeProviderName = strings.TrimSpace(runtimeProviderName)
	sessionID = strings.TrimSpace(sessionID)
	if runtimeProviderName == "" || sessionID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.sessions.Get(ctx, runtimeSessionStoreID(runtimeProviderName, sessionID))
	if err != nil {
		if err == indexeddb.ErrNotFound {
			return nil
		}
		return fmt.Errorf("mark runtime session stopped: %w", err)
	}
	rec["updated_at"] = stoppedAt
	rec["stopped_at"] = stoppedAt
	if err := s.sessions.Put(ctx, rec); err != nil {
		return fmt.Errorf("mark runtime session stopped: %w", err)
	}
	return nil
}

func (s *RuntimeSessionLogService) lastSeq(ctx context.Context, runtimeProviderName, sessionID string) (int64, error) {
	cursor, err := s.logs.Index("by_session_seq").OpenCursor(ctx, nil, indexeddb.CursorPrev, runtimeProviderName, sessionID)
	if err != nil {
		return 0, fmt.Errorf("append runtime session logs: load last log: %w", err)
	}
	defer func() { _ = cursor.Close() }()
	if !cursor.Continue() {
		if err := cursor.Err(); err != nil {
			return 0, fmt.Errorf("append runtime session logs: scan last log: %w", err)
		}
		return 0, nil
	}
	rec, err := cursor.Value()
	if err != nil {
		return 0, fmt.Errorf("append runtime session logs: load last log value: %w", err)
	}
	if err := cursor.Err(); err != nil {
		return 0, fmt.Errorf("append runtime session logs: scan last log: %w", err)
	}
	return recInt64(rec, "seq"), nil
}

func runtimeSessionStoreID(runtimeProviderName, sessionID string) string {
	return strings.TrimSpace(runtimeProviderName) + ":" + strings.TrimSpace(sessionID)
}

func runtimeSessionLogRecordFromRecord(rec indexeddb.Record) runtimelogs.Record {
	return runtimelogs.Record{
		Seq:        recInt64(rec, "seq"),
		SourceSeq:  recInt64(rec, "source_seq"),
		Stream:     runtimelogs.Stream(recString(rec, "stream")),
		Message:    recString(rec, "message"),
		ObservedAt: recTime(rec, "observed_at"),
		AppendedAt: recTime(rec, "appended_at"),
	}
}
