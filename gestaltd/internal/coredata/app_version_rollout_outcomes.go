package coredata

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

type AppVersionRolloutOutcomeService struct {
	db    indexeddb.IndexedDB
	store idb.ObjectStore
}

func NewAppVersionRolloutOutcomeService(ds indexeddb.IndexedDB) *AppVersionRolloutOutcomeService {
	return &AppVersionRolloutOutcomeService{
		db:    ds,
		store: ds.ObjectStore(StoreAppVersionRolloutOutcomes),
	}
}

func (s *AppVersionRolloutOutcomeService) EnsureStore(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("ensure app version rollout outcomes store: service is not configured")
	}
	return ensureAppVersionRolloutOutcomesStore(ctx, s.db)
}

func (s *AppVersionRolloutOutcomeService) Get(ctx context.Context, changeRequestID string) (*core.AppVersionRolloutOutcome, error) {
	if s == nil {
		return nil, fmt.Errorf("get app version rollout outcome: service is not configured")
	}
	changeRequestID = strings.TrimSpace(changeRequestID)
	if changeRequestID == "" {
		return nil, fmt.Errorf("get app version rollout outcome: change request id is required")
	}
	rec, err := s.store.Get(ctx, changeRequestID)
	if err != nil {
		if errors.Is(err, idb.ErrNotFound) {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("get app version rollout outcome: %w", err)
	}
	return recordToAppVersionRolloutOutcome(rec), nil
}

func (s *AppVersionRolloutOutcomeService) GetMany(ctx context.Context, changeRequestIDs []string) (map[string]*core.AppVersionRolloutOutcome, error) {
	if s == nil {
		return nil, fmt.Errorf("get app version rollout outcomes: service is not configured")
	}
	out := make(map[string]*core.AppVersionRolloutOutcome, len(changeRequestIDs))
	for _, id := range changeRequestIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		outcome, err := s.Get(ctx, id)
		if errors.Is(err, core.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out[id] = outcome
	}
	return out, nil
}

func (s *AppVersionRolloutOutcomeService) RecordComplete(
	ctx context.Context,
	changeRequestID, app, version string,
	completedAt time.Time,
) error {
	return s.record(ctx, changeRequestID, app, version, completedAt, time.Time{})
}

func (s *AppVersionRolloutOutcomeService) RecordFailed(
	ctx context.Context,
	changeRequestID, app, version string,
	failedAt time.Time,
) error {
	return s.record(ctx, changeRequestID, app, version, time.Time{}, failedAt)
}

func (s *AppVersionRolloutOutcomeService) record(
	ctx context.Context,
	changeRequestID, app, version string,
	completedAt, failedAt time.Time,
) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("record app version rollout outcome: service is not configured")
	}
	changeRequestID = strings.TrimSpace(changeRequestID)
	app = strings.TrimSpace(app)
	version = strings.TrimSpace(version)
	if changeRequestID == "" || app == "" || version == "" {
		return fmt.Errorf("record app version rollout outcome: change request id, app, and version are required")
	}
	if completedAt.IsZero() == failedAt.IsZero() {
		return fmt.Errorf("record app version rollout outcome: exactly one terminal timestamp is required")
	}
	if err := s.EnsureStore(ctx); err != nil {
		return err
	}
	if _, err := s.Get(ctx, changeRequestID); err == nil {
		return nil
	} else if !errors.Is(err, core.ErrNotFound) {
		return err
	}
	rec := idb.Record{
		"id":      changeRequestID,
		"app":     app,
		"version": version,
	}
	if !completedAt.IsZero() {
		rec["completed_at"] = completedAt.UTC().Truncate(time.Millisecond)
	}
	if !failedAt.IsZero() {
		rec["failed_at"] = failedAt.UTC().Truncate(time.Millisecond)
	}
	if err := s.store.Add(ctx, rec); err != nil {
		return fmt.Errorf("record app version rollout outcome: %w", err)
	}
	return nil
}

func recordToAppVersionRolloutOutcome(rec idb.Record) *core.AppVersionRolloutOutcome {
	return &core.AppVersionRolloutOutcome{
		ID:          recString(rec, "id"),
		App:         recString(rec, "app"),
		Version:     recString(rec, "version"),
		CompletedAt: recTime(rec, "completed_at"),
		FailedAt:    recTime(rec, "failed_at"),
	}
}

func ensureAppVersionRolloutOutcomesStore(ctx context.Context, ds indexeddb.IndexedDB) error {
	if _, err := ds.CreateObjectStore(ctx, StoreAppVersionRolloutOutcomes, AppVersionRolloutOutcomesSchema); err != nil {
		return fmt.Errorf("ensure app_version_rollout_outcomes store: %w", err)
	}
	return nil
}
