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

type AppVersionRecoveryObservationService struct {
	db    indexeddb.IndexedDB
	store idb.ObjectStore
}

func NewAppVersionRecoveryObservationService(ds indexeddb.IndexedDB) *AppVersionRecoveryObservationService {
	return &AppVersionRecoveryObservationService{
		db:    ds,
		store: ds.ObjectStore(StoreAppVersionRecoveryObservations),
	}
}

func (s *AppVersionRecoveryObservationService) Get(ctx context.Context, changeRequestID string) (*core.AppVersionRecoveryObservation, error) {
	if s == nil {
		return nil, fmt.Errorf("get app version recovery observation: service is not configured")
	}
	changeRequestID = strings.TrimSpace(changeRequestID)
	if changeRequestID == "" {
		return nil, fmt.Errorf("get app version recovery observation: change request id is required")
	}
	rec, err := s.store.Get(ctx, changeRequestID)
	if err != nil {
		if errors.Is(err, idb.ErrNotFound) {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("get app version recovery observation: %w", err)
	}
	return recordToAppVersionRecoveryObservation(rec), nil
}

func (s *AppVersionRecoveryObservationService) GetMany(ctx context.Context, changeRequestIDs []string) (map[string]*core.AppVersionRecoveryObservation, error) {
	if s == nil {
		return nil, fmt.Errorf("get app version recovery observations: service is not configured")
	}
	out := make(map[string]*core.AppVersionRecoveryObservation, len(changeRequestIDs))
	for _, id := range changeRequestIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		observation, err := s.Get(ctx, id)
		if errors.Is(err, core.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out[id] = observation
	}
	return out, nil
}

// Record persists the first recovery observation for a change request. Later
// calls return the original observation without rewriting the historical fact.
func (s *AppVersionRecoveryObservationService) Record(ctx context.Context, observation *core.AppVersionRecoveryObservation) (*core.AppVersionRecoveryObservation, error) {
	if s == nil {
		return nil, fmt.Errorf("record app version recovery observation: service is not configured")
	}
	rec, normalized, err := appVersionRecoveryObservationRecord(observation)
	if err != nil {
		return nil, fmt.Errorf("record app version recovery observation: %w", err)
	}
	if err := s.store.Add(ctx, rec); err != nil {
		if errors.Is(err, idb.ErrAlreadyExists) {
			return s.Get(ctx, normalized.ID)
		}
		return nil, fmt.Errorf("record app version recovery observation: %w", err)
	}
	return normalized, nil
}

// RecordIfCurrentFailed atomically verifies that observation still describes
// the current desired-version change request and source-capacity epoch, that
// its immutable rollout outcome is failed, and that no recovery was previously
// recorded. A stale fence returns (nil, false, nil). A duplicate returns the
// original observation with recorded=true.
func (s *AppVersionRecoveryObservationService) RecordIfCurrentFailed(
	ctx context.Context,
	observation *core.AppVersionRecoveryObservation,
) (*core.AppVersionRecoveryObservation, bool, error) {
	if s == nil || s.db == nil {
		return nil, false, fmt.Errorf("record fenced app version recovery observation: service is not configured")
	}
	rec, normalized, err := appVersionRecoveryObservationRecord(observation)
	if err != nil {
		return nil, false, fmt.Errorf("record fenced app version recovery observation: %w", err)
	}
	stores := []string{
		StoreAppVersionChangeRequests,
		StoreAppVersionRolloutOutcomes,
		StoreGestaltdSourceVersionState,
		StoreAppVersionRecoveryObservations,
	}
	tx, err := s.db.Transaction(ctx, stores, idb.TransactionReadwrite, idb.TransactionOptions{})
	if err != nil {
		return nil, false, fmt.Errorf("record fenced app version recovery observation: begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Abort(context.WithoutCancel(ctx))
		}
	}()

	recoveryStore := tx.ObjectStore(StoreAppVersionRecoveryObservations)
	existing, err := recoveryStore.Get(ctx, normalized.ID)
	if err == nil {
		return recordToAppVersionRecoveryObservation(existing), true, nil
	}
	if !errors.Is(err, idb.ErrNotFound) {
		return nil, false, fmt.Errorf("record fenced app version recovery observation: load existing: %w", err)
	}

	requests, err := tx.ObjectStore(StoreAppVersionChangeRequests).
		Index("by_app").
		GetAll(ctx, idb.Only(normalized.App))
	if err != nil {
		return nil, false, fmt.Errorf("record fenced app version recovery observation: load change requests: %w", err)
	}
	latest := latestChangeRequestRecord(requests)
	if latest == nil ||
		recString(latest, "id") != normalized.ID ||
		recString(latest, "app") != normalized.App ||
		recString(latest, "to_version") != normalized.Version {
		return nil, false, nil
	}

	outcomeRec, err := tx.ObjectStore(StoreAppVersionRolloutOutcomes).Get(ctx, normalized.ID)
	if errors.Is(err, idb.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("record fenced app version recovery observation: load rollout outcome: %w", err)
	}
	outcome := recordToAppVersionRolloutOutcome(outcomeRec)
	if outcome.App != normalized.App ||
		outcome.Version != normalized.Version ||
		outcome.FailedAt.IsZero() ||
		!outcome.CompletedAt.IsZero() {
		return nil, false, nil
	}

	sourceRec, err := tx.ObjectStore(StoreGestaltdSourceVersionState).Get(ctx, gestaltdSourceVersionStateID)
	if errors.Is(err, idb.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("record fenced app version recovery observation: load source state: %w", err)
	}
	source := recordToGestaltdSourceVersionState(sourceRec)
	if strings.TrimSpace(source.CurrentSourceVersion) != normalized.SourceVersion ||
		source.MinimumHealthyInstances != normalized.MinimumHealthyInstances {
		return nil, false, nil
	}

	if err := recoveryStore.Add(ctx, rec); err != nil {
		if errors.Is(err, idb.ErrAlreadyExists) {
			_ = tx.Abort(context.WithoutCancel(ctx))
			committed = true
			existing, getErr := s.Get(context.WithoutCancel(ctx), normalized.ID)
			if getErr != nil {
				return nil, false, fmt.Errorf("record fenced app version recovery observation: load concurrent observation: %w", getErr)
			}
			return existing, true, nil
		}
		return nil, false, fmt.Errorf("record fenced app version recovery observation: add: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		if errors.Is(err, idb.ErrAlreadyExists) {
			_ = tx.Abort(context.WithoutCancel(ctx))
			committed = true
			existing, getErr := s.Get(context.WithoutCancel(ctx), normalized.ID)
			if getErr != nil {
				return nil, false, fmt.Errorf("record fenced app version recovery observation: load concurrent observation: %w", getErr)
			}
			return existing, true, nil
		}
		return nil, false, fmt.Errorf("record fenced app version recovery observation: commit: %w", err)
	}
	committed = true
	return normalized, true, nil
}

func latestChangeRequestRecord(records []idb.Record) idb.Record {
	var latest idb.Record
	for _, rec := range records {
		if latest == nil ||
			recTime(rec, "timestamp").After(recTime(latest, "timestamp")) ||
			(recTime(rec, "timestamp").Equal(recTime(latest, "timestamp")) &&
				recString(rec, "id") > recString(latest, "id")) {
			latest = rec
		}
	}
	return latest
}

func appVersionRecoveryObservationRecord(observation *core.AppVersionRecoveryObservation) (idb.Record, *core.AppVersionRecoveryObservation, error) {
	if observation == nil {
		return nil, nil, fmt.Errorf("record is required")
	}
	normalized := &core.AppVersionRecoveryObservation{
		ID:                      strings.TrimSpace(observation.ID),
		App:                     strings.TrimSpace(observation.App),
		Version:                 strings.TrimSpace(observation.Version),
		RecoveredAt:             observation.RecoveredAt.UTC().Truncate(time.Millisecond),
		SourceVersion:           strings.TrimSpace(observation.SourceVersion),
		LiveInstances:           observation.LiveInstances,
		MinimumHealthyInstances: observation.MinimumHealthyInstances,
	}
	if normalized.ID == "" || normalized.App == "" || normalized.Version == "" || normalized.SourceVersion == "" {
		return nil, nil, fmt.Errorf("change request id, app, version, and source version are required")
	}
	if observation.RecoveredAt.IsZero() {
		return nil, nil, fmt.Errorf("recovered at is required")
	}
	if normalized.LiveInstances < 0 || normalized.MinimumHealthyInstances < 0 {
		return nil, nil, fmt.Errorf("instance counts must not be negative")
	}
	return idb.Record{
		"id":                        normalized.ID,
		"app":                       normalized.App,
		"version":                   normalized.Version,
		"recovered_at":              normalized.RecoveredAt,
		"source_version":            normalized.SourceVersion,
		"live_instances":            normalized.LiveInstances,
		"minimum_healthy_instances": normalized.MinimumHealthyInstances,
	}, normalized, nil
}

func recordToAppVersionRecoveryObservation(rec idb.Record) *core.AppVersionRecoveryObservation {
	return &core.AppVersionRecoveryObservation{
		ID:                      recString(rec, "id"),
		App:                     recString(rec, "app"),
		Version:                 recString(rec, "version"),
		RecoveredAt:             recTime(rec, "recovered_at"),
		SourceVersion:           recString(rec, "source_version"),
		LiveInstances:           recordInt(rec, "live_instances"),
		MinimumHealthyInstances: recordInt(rec, "minimum_healthy_instances"),
	}
}
