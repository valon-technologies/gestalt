package coredata

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

type AgentRunInteractionService struct {
	store indexeddb.ObjectStore
}

func NewAgentRunInteractionService(ds indexeddb.IndexedDB) *AgentRunInteractionService {
	return &AgentRunInteractionService{
		store: ds.ObjectStore(StoreAgentRunInteractions),
	}
}

func (s *AgentRunInteractionService) Create(ctx context.Context, interaction coreagent.Interaction) (*coreagent.Interaction, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("create agent run interaction: interaction store is not configured")
	}
	runID := strings.TrimSpace(interaction.RunID)
	interactionType := strings.TrimSpace(string(interaction.Type))
	if runID == "" || interactionType == "" {
		return nil, fmt.Errorf("create agent run interaction: run_id and type are required")
	}
	requestJSON, err := marshalJSON(interaction.Request)
	if err != nil {
		return nil, fmt.Errorf("create agent run interaction: marshal request: %w", err)
	}
	resolutionJSON, err := marshalJSON(interaction.Resolution)
	if err != nil {
		return nil, fmt.Errorf("create agent run interaction: marshal resolution: %w", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	createdAt := interaction.CreatedAt
	if createdAt == nil || createdAt.IsZero() {
		createdAt = &now
	}
	state := strings.TrimSpace(string(interaction.State))
	if state == "" {
		state = string(coreagent.InteractionStatePending)
	}
	rec := indexeddb.Record{
		"id":              strings.TrimSpace(interaction.ID),
		"run_id":          runID,
		"type":            interactionType,
		"state":           state,
		"title":           strings.TrimSpace(interaction.Title),
		"prompt":          strings.TrimSpace(interaction.Prompt),
		"request_json":    requestJSON,
		"resolution_json": resolutionJSON,
		"created_at":      *createdAt,
		"resolved_at":     timeOrNil(interaction.ResolvedAt),
	}
	if rec["id"] == "" {
		rec["id"] = uuid.NewString()
	}
	if err := s.store.Add(ctx, rec); err != nil {
		return nil, fmt.Errorf("create agent run interaction: %w", err)
	}
	return recordToAgentRunInteraction(rec), nil
}

func (s *AgentRunInteractionService) Get(ctx context.Context, id string) (*coreagent.Interaction, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("get agent run interaction: interaction store is not configured")
	}
	rec, err := s.store.Get(ctx, strings.TrimSpace(id))
	if err != nil {
		if err == indexeddb.ErrNotFound {
			return nil, indexeddb.ErrNotFound
		}
		return nil, fmt.Errorf("get agent run interaction: %w", err)
	}
	return recordToAgentRunInteraction(rec), nil
}

func (s *AgentRunInteractionService) ListByRun(ctx context.Context, runID string) ([]*coreagent.Interaction, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("list agent run interactions: interaction store is not configured")
	}
	recs, err := s.store.Index("by_run").GetAll(ctx, nil, strings.TrimSpace(runID))
	if err != nil {
		return nil, fmt.Errorf("list agent run interactions: %w", err)
	}
	out := make([]*coreagent.Interaction, 0, len(recs))
	for _, rec := range recs {
		out = append(out, recordToAgentRunInteraction(rec))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt == nil || out[j].CreatedAt == nil {
			return out[i].ID < out[j].ID
		}
		if out[i].CreatedAt.Equal(*out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(*out[j].CreatedAt)
	})
	return out, nil
}

func (s *AgentRunInteractionService) Resolve(ctx context.Context, id string, resolution map[string]any, resolvedAt time.Time) (*coreagent.Interaction, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("resolve agent run interaction: interaction store is not configured")
	}
	if resolvedAt.IsZero() {
		resolvedAt = time.Now()
	}
	resolvedAt = resolvedAt.UTC().Truncate(time.Second)
	current, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	resolutionJSON, err := marshalJSON(resolution)
	if err != nil {
		return nil, fmt.Errorf("resolve agent run interaction: marshal resolution: %w", err)
	}
	rec := indexeddb.Record{
		"id":              current.ID,
		"run_id":          current.RunID,
		"type":            string(current.Type),
		"state":           string(coreagent.InteractionStateResolved),
		"title":           current.Title,
		"prompt":          current.Prompt,
		"request_json":    mustMarshalJSON(current.Request),
		"resolution_json": resolutionJSON,
		"created_at":      timeOrNil(current.CreatedAt),
		"resolved_at":     resolvedAt,
	}
	if err := s.store.Put(ctx, rec); err != nil {
		return nil, fmt.Errorf("resolve agent run interaction: %w", err)
	}
	return recordToAgentRunInteraction(rec), nil
}

func (s *AgentRunInteractionService) CancelByRun(ctx context.Context, runID string, canceledAt time.Time) error {
	if s == nil || s.store == nil {
		return nil
	}
	if canceledAt.IsZero() {
		canceledAt = time.Now()
	}
	canceledAt = canceledAt.UTC().Truncate(time.Second)
	interactions, err := s.ListByRun(ctx, runID)
	if err != nil {
		return err
	}
	for _, interaction := range interactions {
		if interaction == nil || interaction.State != coreagent.InteractionStatePending {
			continue
		}
		rec := indexeddb.Record{
			"id":              interaction.ID,
			"run_id":          interaction.RunID,
			"type":            string(interaction.Type),
			"state":           string(coreagent.InteractionStateCanceled),
			"title":           interaction.Title,
			"prompt":          interaction.Prompt,
			"request_json":    mustMarshalJSON(interaction.Request),
			"resolution_json": mustMarshalJSON(interaction.Resolution),
			"created_at":      timeOrNil(interaction.CreatedAt),
			"resolved_at":     canceledAt,
		}
		if err := s.store.Put(ctx, rec); err != nil {
			return fmt.Errorf("cancel agent run interaction %q: %w", interaction.ID, err)
		}
	}
	return nil
}

func (s *AgentRunInteractionService) DeleteByRun(ctx context.Context, runID string) error {
	if s == nil || s.store == nil {
		return nil
	}
	recs, err := s.store.Index("by_run").GetAll(ctx, nil, strings.TrimSpace(runID))
	if err != nil {
		return fmt.Errorf("delete agent run interactions: %w", err)
	}
	for _, rec := range recs {
		id := recString(rec, "id")
		if id == "" {
			continue
		}
		if err := s.store.Delete(ctx, id); err != nil && err != indexeddb.ErrNotFound {
			return fmt.Errorf("delete agent run interaction %q: %w", id, err)
		}
	}
	return nil
}

func recordToAgentRunInteraction(rec indexeddb.Record) *coreagent.Interaction {
	if rec == nil {
		return nil
	}
	return &coreagent.Interaction{
		ID:         recString(rec, "id"),
		RunID:      recString(rec, "run_id"),
		Type:       coreagent.InteractionType(recString(rec, "type")),
		State:      coreagent.InteractionState(recString(rec, "state")),
		Title:      recString(rec, "title"),
		Prompt:     recString(rec, "prompt"),
		Request:    recAgentRunInteractionJSON(rec, "request_json"),
		Resolution: recAgentRunInteractionJSON(rec, "resolution_json"),
		CreatedAt:  recTimePtr(rec, "created_at"),
		ResolvedAt: recTimePtr(rec, "resolved_at"),
	}
}

func recAgentRunInteractionJSON(rec indexeddb.Record, key string) map[string]any {
	raw := recString(rec, key)
	if raw == "" {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return maps.Clone(out)
}

func mustMarshalJSON(value any) string {
	raw, err := marshalJSON(value)
	if err != nil {
		return ""
	}
	return raw
}
