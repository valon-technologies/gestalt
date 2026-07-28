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

const gestaltdSourceVersionStateID = "gestaltd"

var ErrGestaltdSourceVersionUnavailable = errors.New("gestaltd source version is unavailable")

type GestaltdSourceVersionService struct {
	db    indexeddb.IndexedDB
	store idb.ObjectStore
}

func NewGestaltdSourceVersionService(ds indexeddb.IndexedDB) *GestaltdSourceVersionService {
	return &GestaltdSourceVersionService{
		db:    ds,
		store: ds.ObjectStore(StoreGestaltdSourceVersionState),
	}
}

func (s *GestaltdSourceVersionService) Get(ctx context.Context) (*core.GestaltdSourceVersionState, error) {
	if s == nil {
		return nil, fmt.Errorf("get gestaltd source version: service is not configured")
	}
	rec, err := s.store.Get(ctx, gestaltdSourceVersionStateID)
	if err != nil {
		if errors.Is(err, idb.ErrNotFound) {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("get gestaltd source version: %w", err)
	}
	return recordToGestaltdSourceVersionState(rec), nil
}

func (s *GestaltdSourceVersionService) CurrentForAdmission(ctx context.Context) (string, error) {
	state, err := s.Get(ctx)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return "", ErrGestaltdSourceVersionUnavailable
		}
		return "", err
	}
	current := strings.TrimSpace(state.CurrentSourceVersion)
	if current == "" {
		return "", ErrGestaltdSourceVersionUnavailable
	}
	return current, nil
}

func (s *GestaltdSourceVersionService) CreateAppRollout(ctx context.Context, rollout *core.AppRollout) (*core.AppRollout, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("create source-version app rollout: service is not configured")
	}
	if err := validateAppRollout(rollout); err != nil {
		return nil, fmt.Errorf("create source-version app rollout: %w", err)
	}

	tx, err := s.db.Transaction(
		ctx,
		[]string{StoreGestaltdSourceVersionState, StoreAppRollouts},
		idb.TransactionReadwrite,
		idb.TransactionOptions{},
	)
	if err != nil {
		return nil, fmt.Errorf("create source-version app rollout: begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Abort(context.WithoutCancel(ctx))
		}
	}()

	stateRec, err := tx.ObjectStore(StoreGestaltdSourceVersionState).Get(ctx, gestaltdSourceVersionStateID)
	if err != nil {
		if errors.Is(err, idb.ErrNotFound) {
			return nil, ErrGestaltdSourceVersionUnavailable
		}
		return nil, fmt.Errorf("create source-version app rollout: load source version: %w", err)
	}
	state := recordToGestaltdSourceVersionState(stateRec)
	current := strings.TrimSpace(state.CurrentSourceVersion)
	if current == "" {
		return nil, ErrGestaltdSourceVersionUnavailable
	}

	rollout = normalizeAppRollout(rollout)
	rollout.TargetSourceVersion = current
	rolloutStore := tx.ObjectStore(StoreAppRollouts)
	existing, err := rolloutStore.GetAll(ctx, rollout.App, 1)
	switch {
	case err != nil:
		return nil, fmt.Errorf("create source-version app rollout: load current rollout: %w", err)
	case len(existing) > 0:
		if isActiveRolloutState(recordToAppRollout(existing[0]).State) {
			return nil, ErrAppRolloutActive
		}
		if err := rolloutStore.Put(ctx, appRolloutRecord(rollout)); err != nil {
			return nil, fmt.Errorf("create source-version app rollout: replace terminal rollout: %w", err)
		}
	default:
		if err := rolloutStore.Add(ctx, appRolloutRecord(rollout)); err != nil {
			if errors.Is(err, idb.ErrAlreadyExists) {
				return nil, ErrAppRolloutActive
			}
			return nil, fmt.Errorf("create source-version app rollout: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("create source-version app rollout: commit: %w", err)
	}
	committed = true
	return rollout, nil
}

func (s *GestaltdSourceVersionService) Activate(
	ctx context.Context,
	sourceVersion string,
	at time.Time,
	retry bool,
	enrollmentWindow time.Duration,
	rolloutTimeout time.Duration,
) (*core.GestaltdSourceVersionState, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("activate gestaltd source version: service is not configured")
	}
	sourceVersion = strings.TrimSpace(sourceVersion)
	if sourceVersion == "" {
		return nil, fmt.Errorf("activate gestaltd source version: source version is required")
	}
	at = normalizedSourceVersionTime(at)
	if enrollmentWindow <= 0 || rolloutTimeout <= enrollmentWindow {
		return nil, fmt.Errorf("activate gestaltd source version: invalid rollout windows")
	}

	tx, err := s.db.Transaction(
		ctx,
		[]string{StoreGestaltdSourceVersionState, StoreAppRollouts},
		idb.TransactionReadwrite,
		idb.TransactionOptions{},
	)
	if err != nil {
		return nil, fmt.Errorf("activate gestaltd source version: begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Abort(context.WithoutCancel(ctx))
		}
	}()

	stateStore := tx.ObjectStore(StoreGestaltdSourceVersionState)
	state := &core.GestaltdSourceVersionState{}
	stateRecs, getErr := stateStore.GetAll(ctx, gestaltdSourceVersionStateID, 1)
	if getErr != nil {
		return nil, fmt.Errorf("activate gestaltd source version: load state: %w", getErr)
	}
	if len(stateRecs) > 0 {
		state = recordToGestaltdSourceVersionState(stateRecs[0])
	}
	sourceChanged := strings.TrimSpace(state.CurrentSourceVersion) != sourceVersion
	if sourceChanged || retry {
		rolloutStore := tx.ObjectStore(StoreAppRollouts)
		recs, listErr := rolloutStore.GetAll(ctx, nil)
		if listErr != nil {
			return nil, fmt.Errorf("activate gestaltd source version: list app rollouts: %w", listErr)
		}
		for _, rec := range recs {
			rollout := recordToAppRollout(rec)
			retarget := sourceChanged && isActiveRolloutState(rollout.State)
			if retry && strings.TrimSpace(rollout.TargetSourceVersion) == sourceVersion {
				retarget = isActiveRolloutState(rollout.State) ||
					(rollout.State == core.AppRolloutStateFailed &&
						!rollout.FailedAt.IsZero() &&
						!rollout.FailedAt.Before(state.UpdatedAt))
			}
			if !retarget {
				continue
			}
			rollout.TargetSourceVersion = sourceVersion
			rollout.State = core.AppRolloutStateEnrolling
			rollout.CreatedAt = at
			rollout.EnrollmentEndsAt = at.Add(enrollmentWindow)
			rollout.Deadline = at.Add(rolloutTimeout)
			rollout.CompletedAt = time.Time{}
			rollout.FailedAt = time.Time{}
			if err := rolloutStore.Put(ctx, appRolloutRecord(rollout)); err != nil {
				return nil, fmt.Errorf("activate gestaltd source version: retarget rollout %s: %w", rollout.App, err)
			}
		}
	}

	state.CurrentSourceVersion = sourceVersion
	if sourceChanged || retry || state.UpdatedAt.IsZero() {
		state.UpdatedAt = at
	}
	if err := stateStore.Put(ctx, gestaltdSourceVersionStateRecord(state)); err != nil {
		return nil, fmt.Errorf("activate gestaltd source version: write state: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("activate gestaltd source version: commit: %w", err)
	}
	committed = true
	return state, nil
}

func normalizedSourceVersionTime(value time.Time) time.Time {
	if value.IsZero() {
		value = time.Now()
	}
	return value.UTC().Truncate(time.Millisecond)
}

func gestaltdSourceVersionStateRecord(state *core.GestaltdSourceVersionState) idb.Record {
	return idb.Record{
		"id":                     gestaltdSourceVersionStateID,
		"current_source_version": strings.TrimSpace(state.CurrentSourceVersion),
		"updated_at":             normalizedSourceVersionTime(state.UpdatedAt),
	}
}

func recordToGestaltdSourceVersionState(rec idb.Record) *core.GestaltdSourceVersionState {
	return &core.GestaltdSourceVersionState{
		CurrentSourceVersion: recString(rec, "current_source_version"),
		UpdatedAt:            recTime(rec, "updated_at"),
	}
}
