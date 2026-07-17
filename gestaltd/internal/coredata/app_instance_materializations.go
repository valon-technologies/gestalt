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
	store idb.ObjectStore
}

func NewAppInstanceMaterializationService(ds indexeddb.IndexedDB) *AppInstanceMaterializationService {
	return &AppInstanceMaterializationService{store: ds.ObjectStore(StoreAppInstanceMaterializations)}
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
		return recordToAppInstanceMaterialization(existing), nil
	} else if !errors.Is(err, idb.ErrNotFound) {
		return nil, fmt.Errorf("acknowledge app instance materialization: %w", err)
	}

	acknowledgedAt := materialization.AcknowledgedAt
	if acknowledgedAt.IsZero() {
		acknowledgedAt = time.Now().UTC().Truncate(time.Millisecond)
	} else {
		acknowledgedAt = acknowledgedAt.UTC().Truncate(time.Millisecond)
	}
	rec := idb.Record{
		"id":              uuid.NewString(),
		"instance_id":     instanceID,
		"app":             app,
		"version":         version,
		"acknowledged_at": acknowledgedAt,
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

func (s *AppInstanceMaterializationService) MarkMaterialized(ctx context.Context, instanceID, app, version, materializedPath string, materializedAt time.Time) (*core.AppInstanceMaterialization, error) {
	return s.updateTimestamps(ctx, instanceID, app, version, func(rec idb.Record) idb.Record {
		if materializedAt.IsZero() {
			materializedAt = time.Now().UTC().Truncate(time.Millisecond)
		} else {
			materializedAt = materializedAt.UTC().Truncate(time.Millisecond)
		}
		rec["materialized_at"] = materializedAt
		rec["materialized_path"] = strings.TrimSpace(materializedPath)
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
		MaterializedPath: recString(rec, "materialized_path"),
		StoppedAt:        recTime(rec, "stopped_at"),
		RestartedAt:      recTime(rec, "restarted_at"),
	}
}
