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
	store idb.ObjectStore
}

func NewAppVersionRecoveryObservationService(ds indexeddb.IndexedDB) *AppVersionRecoveryObservationService {
	return &AppVersionRecoveryObservationService{store: ds.ObjectStore(StoreAppVersionRecoveryObservations)}
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
