package coredata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

type GestaltdInstanceHeartbeatService struct {
	db    indexeddb.IndexedDB
	store idb.ObjectStore
}

func NewGestaltdInstanceHeartbeatService(ds indexeddb.IndexedDB) *GestaltdInstanceHeartbeatService {
	return &GestaltdInstanceHeartbeatService{
		db:    ds,
		store: ds.ObjectStore(StoreGestaltdInstanceHeartbeats),
	}
}

func (s *GestaltdInstanceHeartbeatService) Get(ctx context.Context, instanceID string) (*core.GestaltdInstanceHeartbeat, error) {
	if s == nil {
		return nil, fmt.Errorf("get gestaltd instance heartbeat: service is not configured")
	}
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return nil, fmt.Errorf("get gestaltd instance heartbeat: instance id is required")
	}
	rec, err := s.store.Get(ctx, instanceID)
	if err != nil {
		if errors.Is(err, idb.ErrNotFound) {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("get gestaltd instance heartbeat: %w", err)
	}
	return recordToGestaltdInstanceHeartbeat(rec), nil
}

func (s *GestaltdInstanceHeartbeatService) List(ctx context.Context) ([]*core.GestaltdInstanceHeartbeat, error) {
	if s == nil {
		return nil, fmt.Errorf("list gestaltd instance heartbeats: service is not configured")
	}
	recs, err := s.store.GetAll(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("list gestaltd instance heartbeats: %w", err)
	}
	return recordsToGestaltdInstanceHeartbeats(recs), nil
}

func (s *GestaltdInstanceHeartbeatService) ListBySourceVersion(ctx context.Context, sourceVersion string) ([]*core.GestaltdInstanceHeartbeat, error) {
	if s == nil {
		return nil, fmt.Errorf("list gestaltd instance heartbeats by source version: service is not configured")
	}
	sourceVersion = strings.TrimSpace(sourceVersion)
	if sourceVersion == "" {
		return nil, fmt.Errorf("list gestaltd instance heartbeats by source version: source version is required")
	}
	recs, err := s.store.Index("by_source_version").GetAll(ctx, sourceVersion)
	if err != nil {
		return nil, fmt.Errorf("list gestaltd instance heartbeats by source version: %w", err)
	}
	return recordsToGestaltdInstanceHeartbeats(recs), nil
}

func (s *GestaltdInstanceHeartbeatService) ListFreshBySourceVersion(
	ctx context.Context,
	sourceVersion string,
	cutoff time.Time,
) ([]*core.GestaltdInstanceHeartbeat, error) {
	if s == nil {
		return nil, fmt.Errorf("list fresh gestaltd instance heartbeats: service is not configured")
	}
	sourceVersion = strings.TrimSpace(sourceVersion)
	if sourceVersion == "" {
		return nil, fmt.Errorf("list fresh gestaltd instance heartbeats: source version is required")
	}
	cutoff = cutoff.UTC()
	query := idb.Bound(
		[]any{sourceVersion, cutoff},
		[]any{sourceVersion, indexedDBMaxTime},
		false,
		false,
	)
	recs, err := s.store.Index("by_source_version_heartbeat_at").GetAll(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list fresh gestaltd instance heartbeats: %w", err)
	}
	return recordsToGestaltdInstanceHeartbeats(recs), nil
}

func (s *GestaltdInstanceHeartbeatService) Upsert(ctx context.Context, heartbeat *core.GestaltdInstanceHeartbeat) (*core.GestaltdInstanceHeartbeat, error) {
	if s == nil {
		return nil, fmt.Errorf("upsert gestaltd instance heartbeat: service is not configured")
	}
	rec, normalized, err := gestaltdInstanceHeartbeatRecord(heartbeat)
	if err != nil {
		return nil, fmt.Errorf("upsert gestaltd instance heartbeat: %w", err)
	}
	if err := s.store.Put(ctx, rec); err != nil {
		return nil, fmt.Errorf("upsert gestaltd instance heartbeat: %w", err)
	}
	return normalized, nil
}

func (s *GestaltdInstanceHeartbeatService) Delete(ctx context.Context, instanceID string) error {
	if s == nil {
		return fmt.Errorf("delete gestaltd instance heartbeat: service is not configured")
	}
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return fmt.Errorf("delete gestaltd instance heartbeat: instance id is required")
	}
	if err := s.store.Delete(ctx, instanceID); err != nil {
		return fmt.Errorf("delete gestaltd instance heartbeat: %w", err)
	}
	return nil
}

func (s *GestaltdInstanceHeartbeatService) PruneBefore(ctx context.Context, cutoff time.Time) (int, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("prune gestaltd instance heartbeats: service is not configured")
	}
	tx, err := s.db.Transaction(
		ctx,
		[]string{StoreGestaltdInstanceHeartbeats},
		idb.TransactionReadwrite,
		idb.TransactionOptions{},
	)
	if err != nil {
		return 0, fmt.Errorf("prune gestaltd instance heartbeats: begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Abort(context.WithoutCancel(ctx))
		}
	}()
	store := tx.ObjectStore(StoreGestaltdInstanceHeartbeats)
	recs, err := store.Index("by_heartbeat_at").GetAll(ctx, idb.UpperBound(cutoff.UTC(), true))
	if err != nil {
		return 0, fmt.Errorf("prune gestaltd instance heartbeats: list expired rows: %w", err)
	}
	pruned := 0
	for _, rec := range recs {
		instanceID := recString(rec, "instance_id")
		if instanceID == "" {
			continue
		}
		if err := store.Delete(ctx, instanceID); err != nil && !errors.Is(err, idb.ErrNotFound) {
			return pruned, fmt.Errorf("prune gestaltd instance heartbeats: delete %q: %w", instanceID, err)
		}
		pruned++
	}
	if err := tx.Commit(ctx); err != nil {
		return pruned, fmt.Errorf("prune gestaltd instance heartbeats: commit: %w", err)
	}
	committed = true
	return pruned, nil
}

func gestaltdInstanceHeartbeatRecord(heartbeat *core.GestaltdInstanceHeartbeat) (idb.Record, *core.GestaltdInstanceHeartbeat, error) {
	if heartbeat == nil {
		return nil, nil, fmt.Errorf("record is required")
	}
	normalized := &core.GestaltdInstanceHeartbeat{
		InstanceID:    strings.TrimSpace(heartbeat.InstanceID),
		SourceVersion: strings.TrimSpace(heartbeat.SourceVersion),
		StartedAt:     normalizedHeartbeatTime(heartbeat.StartedAt),
		HeartbeatAt:   normalizedHeartbeatTime(heartbeat.HeartbeatAt),
		Apps:          cloneHeartbeatApps(heartbeat.Apps),
	}
	if normalized.InstanceID == "" || normalized.SourceVersion == "" {
		return nil, nil, fmt.Errorf("instance id and source version are required")
	}
	if heartbeat.StartedAt.IsZero() || heartbeat.HeartbeatAt.IsZero() {
		return nil, nil, fmt.Errorf("started at and heartbeat at are required")
	}
	if normalized.HeartbeatAt.Before(normalized.StartedAt) {
		return nil, nil, fmt.Errorf("heartbeat at must not be before started at")
	}
	if normalized.Apps == nil {
		normalized.Apps = map[string]core.GestaltdInstanceAppHeartbeat{}
	}
	return idb.Record{
		"id":             normalized.InstanceID,
		"instance_id":    normalized.InstanceID,
		"source_version": normalized.SourceVersion,
		"started_at":     normalized.StartedAt,
		"heartbeat_at":   normalized.HeartbeatAt,
		"apps":           heartbeatAppsJSONValue(normalized.Apps),
	}, normalized, nil
}

func normalizedHeartbeatTime(value time.Time) time.Time {
	return value.UTC().Truncate(time.Millisecond)
}

func heartbeatAppsJSONValue(apps map[string]core.GestaltdInstanceAppHeartbeat) any {
	encoded, err := json.Marshal(apps)
	if err != nil {
		return map[string]any{}
	}
	var value map[string]any
	if err := json.Unmarshal(encoded, &value); err != nil {
		return map[string]any{}
	}
	return value
}

func cloneHeartbeatApps(apps map[string]core.GestaltdInstanceAppHeartbeat) map[string]core.GestaltdInstanceAppHeartbeat {
	if apps == nil {
		return nil
	}
	out := make(map[string]core.GestaltdInstanceAppHeartbeat, len(apps))
	for app, observation := range apps {
		observation.ObservedAt = normalizedHeartbeatTime(observation.ObservedAt)
		out[app] = observation
	}
	return out
}

func recordToGestaltdInstanceHeartbeat(rec idb.Record) *core.GestaltdInstanceHeartbeat {
	apps := make(map[string]core.GestaltdInstanceAppHeartbeat)
	if raw := recJSON(rec, "apps"); len(raw) > 0 {
		_ = json.Unmarshal(raw, &apps)
	}
	return &core.GestaltdInstanceHeartbeat{
		InstanceID:    recString(rec, "instance_id"),
		SourceVersion: recString(rec, "source_version"),
		StartedAt:     recTime(rec, "started_at"),
		HeartbeatAt:   recTime(rec, "heartbeat_at"),
		Apps:          apps,
	}
}

func recordsToGestaltdInstanceHeartbeats(recs []idb.Record) []*core.GestaltdInstanceHeartbeat {
	out := make([]*core.GestaltdInstanceHeartbeat, 0, len(recs))
	for _, rec := range recs {
		out = append(out, recordToGestaltdInstanceHeartbeat(rec))
	}
	return out
}
