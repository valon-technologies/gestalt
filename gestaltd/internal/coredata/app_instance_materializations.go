package coredata

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

type AppInstanceMaterializationService struct {
	db    indexeddb.IndexedDB
	store idb.ObjectStore
}

func NewAppInstanceMaterializationService(ds indexeddb.IndexedDB) *AppInstanceMaterializationService {
	return &AppInstanceMaterializationService{db: ds, store: ds.ObjectStore(StoreAppInstanceMaterializations)}
}

func (s *AppInstanceMaterializationService) HasAcknowledged(ctx context.Context, instanceID, app, version string) (bool, error) {
	if s == nil {
		return false, fmt.Errorf("has app instance materialization: service is not configured")
	}
	_, err := s.findByInstanceAppVersion(ctx, instanceID, app, version)
	if errors.Is(err, idb.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("has app instance materialization: %w", err)
	}
	return true, nil
}

func (s *AppInstanceMaterializationService) Get(ctx context.Context, instanceID, app, version string) (*core.AppInstanceMaterialization, error) {
	if s == nil {
		return nil, fmt.Errorf("get app instance materialization: service is not configured")
	}
	rec, err := s.findByInstanceAppVersion(ctx, instanceID, app, version)
	if err != nil {
		return nil, fmt.Errorf("get app instance materialization: %w", err)
	}
	return recordToAppInstanceMaterialization(rec), nil
}

func (s *AppInstanceMaterializationService) ListByAppVersion(ctx context.Context, app, version string) ([]*core.AppInstanceMaterialization, error) {
	if s == nil {
		return nil, fmt.Errorf("list app instance materializations: service is not configured")
	}
	app = strings.TrimSpace(app)
	version = strings.TrimSpace(version)
	if app == "" || version == "" {
		return nil, fmt.Errorf("list app instance materializations: app and version are required")
	}
	recs, err := s.store.Index("by_app_version").GetAll(ctx, []any{app, version})
	if err != nil {
		return nil, fmt.Errorf("list app instance materializations: %w", err)
	}
	out := make([]*core.AppInstanceMaterialization, 0, len(recs))
	for _, rec := range recs {
		out = append(out, recordToAppInstanceMaterialization(rec))
	}
	return out, nil
}

func (s *AppInstanceMaterializationService) Acknowledge(ctx context.Context, materialization *core.AppInstanceMaterialization) (*core.AppInstanceMaterialization, error) {
	return s.acknowledge(ctx, materialization, time.Time{})
}

// AcknowledgeForRollout resets progress left by an earlier rollout of the same
// version before acknowledging the current rollout.
func (s *AppInstanceMaterializationService) AcknowledgeForRollout(ctx context.Context, materialization *core.AppInstanceMaterialization, rolloutCreatedAt time.Time) (*core.AppInstanceMaterialization, error) {
	return s.acknowledge(ctx, materialization, rolloutCreatedAt.UTC().Truncate(time.Millisecond))
}

func (s *AppInstanceMaterializationService) acknowledge(ctx context.Context, materialization *core.AppInstanceMaterialization, rolloutCreatedAt time.Time) (*core.AppInstanceMaterialization, error) {
	if s == nil {
		return nil, fmt.Errorf("acknowledge app instance materialization: service is not configured")
	}
	if materialization == nil {
		return nil, fmt.Errorf("acknowledge app instance materialization: record is required")
	}
	instanceID := strings.TrimSpace(materialization.InstanceID)
	app := strings.TrimSpace(materialization.App)
	version := strings.TrimSpace(materialization.Version)
	if instanceID == "" || app == "" || version == "" {
		return nil, fmt.Errorf("acknowledge app instance materialization: instance_id, app, and version are required")
	}

	if existing, err := s.findByInstanceAppVersion(ctx, instanceID, app, version); err == nil {
		current := recordToAppInstanceMaterialization(existing)
		if rolloutCreatedAt.IsZero() || !current.AcknowledgedAt.Before(rolloutCreatedAt) {
			return current, nil
		}
		acknowledgedAt := normalizedMaterializationTime(materialization.AcknowledgedAt)
		existing["acknowledged_at"] = acknowledgedAt
		delete(existing, "materialized_at")
		delete(existing, "stopped_at")
		delete(existing, "restarted_at")
		existing["attempt_count"] = 0
		delete(existing, "last_error_at")
		delete(existing, "last_error_message")
		if err := s.store.Put(ctx, existing); err != nil {
			return nil, fmt.Errorf("acknowledge app instance materialization: reset stale progress: %w", err)
		}
		return recordToAppInstanceMaterialization(existing), nil
	} else if !errors.Is(err, idb.ErrNotFound) {
		return nil, fmt.Errorf("acknowledge app instance materialization: %w", err)
	}

	acknowledgedAt := normalizedMaterializationTime(materialization.AcknowledgedAt)
	rec := idb.Record{
		"id":              uuid.NewString(),
		"instance_id":     instanceID,
		"app":             app,
		"version":         version,
		"acknowledged_at": acknowledgedAt,
		"attempt_count":   0,
	}
	if err := s.store.Add(ctx, rec); err != nil {
		if errors.Is(err, idb.ErrAlreadyExists) {
			existing, getErr := s.findByInstanceAppVersion(ctx, instanceID, app, version)
			if getErr != nil {
				return nil, fmt.Errorf("acknowledge app instance materialization: %w", getErr)
			}
			return recordToAppInstanceMaterialization(existing), nil
		}
		return nil, fmt.Errorf("acknowledge app instance materialization: %w", err)
	}
	return recordToAppInstanceMaterialization(rec), nil
}

func normalizedMaterializationTime(value time.Time) time.Time {
	if value.IsZero() {
		value = time.Now()
	}
	return value.UTC().Truncate(time.Millisecond)
}

func (s *AppInstanceMaterializationService) MarkMaterialized(ctx context.Context, instanceID, app, version string, materializedAt time.Time) (*core.AppInstanceMaterialization, error) {
	return s.updateTimestamps(ctx, instanceID, app, version, func(rec idb.Record) idb.Record {
		if materializedAt.IsZero() {
			materializedAt = time.Now().UTC().Truncate(time.Millisecond)
		} else {
			materializedAt = materializedAt.UTC().Truncate(time.Millisecond)
		}
		rec["materialized_at"] = materializedAt
		return rec
	})
}

func (s *AppInstanceMaterializationService) MarkStopped(ctx context.Context, instanceID, app, version string, stoppedAt time.Time) (*core.AppInstanceMaterialization, error) {
	return s.updateTimestamps(ctx, instanceID, app, version, func(rec idb.Record) idb.Record {
		if stoppedAt.IsZero() {
			stoppedAt = time.Now().UTC().Truncate(time.Millisecond)
		} else {
			stoppedAt = stoppedAt.UTC().Truncate(time.Millisecond)
		}
		rec["stopped_at"] = stoppedAt
		return rec
	})
}

func (s *AppInstanceMaterializationService) MarkRestarted(ctx context.Context, instanceID, app, version string, restartedAt time.Time) (*core.AppInstanceMaterialization, error) {
	return s.updateTimestamps(ctx, instanceID, app, version, func(rec idb.Record) idb.Record {
		if restartedAt.IsZero() {
			restartedAt = time.Now().UTC().Truncate(time.Millisecond)
		} else {
			restartedAt = restartedAt.UTC().Truncate(time.Millisecond)
		}
		rec["restarted_at"] = restartedAt
		return rec
	})
}

func (s *AppInstanceMaterializationService) RecordFailure(ctx context.Context, instanceID, app, version string, failedAt time.Time, message string) (*core.AppInstanceMaterialization, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("record app instance materialization failure: service is not configured")
	}
	tx, err := s.db.Transaction(ctx, []string{StoreAppInstanceMaterializations}, idb.TransactionReadwrite, idb.TransactionOptions{})
	if err != nil {
		return nil, fmt.Errorf("record app instance materialization failure: begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Abort(context.WithoutCancel(ctx))
		}
	}()
	store := tx.ObjectStore(StoreAppInstanceMaterializations)
	query := idb.Bound([]any{strings.TrimSpace(instanceID), strings.TrimSpace(app), strings.TrimSpace(version)}, []any{strings.TrimSpace(instanceID), strings.TrimSpace(app), strings.TrimSpace(version)}, false, false)
	recs, err := store.Index("by_instance_app_version").GetAll(ctx, query)
	if err != nil || len(recs) == 0 {
		if err == nil {
			err = idb.ErrNotFound
		}
		return nil, fmt.Errorf("record app instance materialization failure: load record: %w", err)
	}
	rec := recs[0]
	rec["attempt_count"] = recordInt(rec, "attempt_count") + 1
	if failedAt.IsZero() {
		failedAt = time.Now()
	}
	rec["last_error_at"] = failedAt.UTC().Truncate(time.Millisecond)
	rec["last_error_message"] = strings.TrimSpace(message)
	if err := store.Put(ctx, rec); err != nil {
		return nil, fmt.Errorf("record app instance materialization failure: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("record app instance materialization failure: commit: %w", err)
	}
	committed = true
	return recordToAppInstanceMaterialization(rec), nil
}

func (s *AppInstanceMaterializationService) updateTimestamps(
	ctx context.Context,
	instanceID, app, version string,
	mutate func(idb.Record) idb.Record,
) (*core.AppInstanceMaterialization, error) {
	if s == nil {
		return nil, fmt.Errorf("update app instance materialization: service is not configured")
	}
	rec, err := s.findByInstanceAppVersion(ctx, instanceID, app, version)
	if err != nil {
		return nil, fmt.Errorf("update app instance materialization: %w", err)
	}
	rec = mutate(rec)
	if err := s.store.Put(ctx, rec); err != nil {
		return nil, fmt.Errorf("update app instance materialization: %w", err)
	}
	return recordToAppInstanceMaterialization(rec), nil
}

func (s *AppInstanceMaterializationService) findByInstanceAppVersion(ctx context.Context, instanceID, app, version string) (idb.Record, error) {
	instanceID = strings.TrimSpace(instanceID)
	app = strings.TrimSpace(app)
	version = strings.TrimSpace(version)
	if instanceID == "" || app == "" || version == "" {
		return nil, fmt.Errorf("find app instance materialization: instance_id, app, and version are required")
	}
	query := idb.Bound(
		[]any{instanceID, app, version},
		[]any{instanceID, app, version},
		false,
		false,
	)
	recs, err := s.store.Index("by_instance_app_version").GetAll(ctx, query)
	if err != nil {
		return nil, err
	}
	if len(recs) == 0 {
		return nil, idb.ErrNotFound
	}
	return recs[0], nil
}

func recordToAppInstanceMaterialization(rec idb.Record) *core.AppInstanceMaterialization {
	return &core.AppInstanceMaterialization{
		InstanceID:       recString(rec, "instance_id"),
		App:              recString(rec, "app"),
		Version:          recString(rec, "version"),
		AcknowledgedAt:   recTime(rec, "acknowledged_at"),
		MaterializedAt:   recTime(rec, "materialized_at"),
		StoppedAt:        recTime(rec, "stopped_at"),
		RestartedAt:      recTime(rec, "restarted_at"),
		AttemptCount:     recordInt(rec, "attempt_count"),
		LastErrorAt:      recTime(rec, "last_error_at"),
		LastErrorMessage: recString(rec, "last_error_message"),
	}
}

func recordInt(rec idb.Record, key string) int {
	switch value := rec[key].(type) {
	case int:
		return value
	case int32:
		return int(value)
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}
