package coredata

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

type AgentRunEventService struct {
	store indexeddb.ObjectStore
	mu    sync.Mutex
}

func NewAgentRunEventService(ds indexeddb.IndexedDB) *AgentRunEventService {
	return &AgentRunEventService{
		store: ds.ObjectStore(StoreAgentRunEvents),
	}
}

func (s *AgentRunEventService) Append(ctx context.Context, event coreagent.RunEvent) (*coreagent.RunEvent, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("append agent run event: event store is not configured")
	}
	runID := strings.TrimSpace(event.RunID)
	eventType := strings.TrimSpace(event.Type)
	if runID == "" || eventType == "" {
		return nil, fmt.Errorf("append agent run event: run_id and type are required")
	}

	dataJSON, err := marshalJSON(event.Data)
	if err != nil {
		return nil, fmt.Errorf("append agent run event: marshal data: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	maxSeq, err := s.lastSeq(ctx, runID)
	if err != nil {
		return nil, err
	}

	createdAt := event.CreatedAt
	now := time.Now().UTC().Truncate(time.Second)
	if createdAt == nil || createdAt.IsZero() {
		createdAt = &now
	}
	rec := indexeddb.Record{
		"id":         strings.TrimSpace(event.ID),
		"run_id":     runID,
		"seq":        maxSeq + 1,
		"type":       eventType,
		"source":     strings.TrimSpace(event.Source),
		"visibility": strings.TrimSpace(event.Visibility),
		"data_json":  dataJSON,
		"created_at": *createdAt,
	}
	if rec["id"] == "" {
		rec["id"] = uuid.NewString()
	}
	if err := s.store.Add(ctx, rec); err != nil {
		return nil, fmt.Errorf("append agent run event: %w", err)
	}
	return recordToAgentRunEvent(rec), nil
}

func (s *AgentRunEventService) lastSeq(ctx context.Context, runID string) (int64, error) {
	cursor, err := s.store.Index("by_run_seq").OpenCursor(ctx, nil, indexeddb.CursorPrev, runID)
	if err != nil {
		return 0, fmt.Errorf("append agent run event: load last event: %w", err)
	}
	defer func() { _ = cursor.Close() }()

	if !cursor.Continue() {
		if err := cursor.Err(); err != nil {
			return 0, fmt.Errorf("append agent run event: scan last event: %w", err)
		}
		return 0, nil
	}
	rec, err := cursor.Value()
	if err != nil {
		return 0, fmt.Errorf("append agent run event: load last event value: %w", err)
	}
	if err := cursor.Err(); err != nil {
		return 0, fmt.Errorf("append agent run event: scan last event: %w", err)
	}
	return recInt64(rec, "seq"), nil
}

func (s *AgentRunEventService) ListByRun(ctx context.Context, runID string, afterSeq int64, limit int) ([]*coreagent.RunEvent, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("list agent run events: event store is not configured")
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, fmt.Errorf("list agent run events: run_id is required")
	}
	recs, err := s.store.Index("by_run").GetAll(ctx, nil, runID)
	if err != nil {
		return nil, fmt.Errorf("list agent run events: %w", err)
	}
	out := make([]*coreagent.RunEvent, 0, len(recs))
	for _, rec := range recs {
		event := recordToAgentRunEvent(rec)
		if event == nil || event.Seq <= afterSeq {
			continue
		}
		out = append(out, event)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Seq == out[j].Seq {
			return out[i].ID < out[j].ID
		}
		return out[i].Seq < out[j].Seq
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *AgentRunEventService) DeleteByRun(ctx context.Context, runID string) error {
	if s == nil || s.store == nil {
		return nil
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	recs, err := s.store.Index("by_run").GetAll(ctx, nil, runID)
	if err != nil {
		return fmt.Errorf("delete agent run events: %w", err)
	}
	for _, rec := range recs {
		id := recString(rec, "id")
		if id == "" {
			continue
		}
		if err := s.store.Delete(ctx, id); err != nil && err != indexeddb.ErrNotFound {
			return fmt.Errorf("delete agent run event %q: %w", id, err)
		}
	}
	return nil
}

func recordToAgentRunEvent(rec indexeddb.Record) *coreagent.RunEvent {
	if rec == nil {
		return nil
	}
	return &coreagent.RunEvent{
		ID:         recString(rec, "id"),
		RunID:      recString(rec, "run_id"),
		Seq:        recInt64(rec, "seq"),
		Type:       recString(rec, "type"),
		Source:     recString(rec, "source"),
		Visibility: recString(rec, "visibility"),
		Data:       recAgentRunEventData(rec),
		CreatedAt:  recTimePtr(rec, "created_at"),
	}
}

func recAgentRunEventData(rec indexeddb.Record) map[string]any {
	raw := recString(rec, "data_json")
	if raw == "" {
		return nil
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return nil
	}
	return maps.Clone(data)
}
