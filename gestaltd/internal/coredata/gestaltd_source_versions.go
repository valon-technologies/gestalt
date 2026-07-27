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

var (
	ErrGestaltdSourceVersionPromoting   = errors.New("gestaltd deployment is in progress")
	ErrGestaltdSourceVersionUnavailable = errors.New("gestaltd source version is unavailable")
	ErrGestaltdSourceVersionMismatch    = errors.New("gestaltd source version does not match candidate")
)

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
	if state.State == core.GestaltdSourceVersionStatePromoting {
		return "", ErrGestaltdSourceVersionPromoting
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
	if state.State == core.GestaltdSourceVersionStatePromoting {
		return nil, ErrGestaltdSourceVersionPromoting
	}
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

func (s *GestaltdSourceVersionService) BeginPromotion(ctx context.Context, sourceVersion string, at time.Time) (*core.GestaltdSourceVersionState, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("begin gestaltd source version promotion: service is not configured")
	}
	sourceVersion = strings.TrimSpace(sourceVersion)
	if sourceVersion == "" {
		return nil, fmt.Errorf("begin gestaltd source version promotion: source version is required")
	}
	tx, err := s.db.Transaction(ctx, []string{StoreGestaltdSourceVersionState}, idb.TransactionReadwrite, idb.TransactionOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin gestaltd source version promotion: begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Abort(context.WithoutCancel(ctx))
		}
	}()

	store := tx.ObjectStore(StoreGestaltdSourceVersionState)
	state := &core.GestaltdSourceVersionState{}
	if rec, getErr := store.Get(ctx, gestaltdSourceVersionStateID); getErr == nil {
		state = recordToGestaltdSourceVersionState(rec)
	} else if !errors.Is(getErr, idb.ErrNotFound) {
		return nil, fmt.Errorf("begin gestaltd source version promotion: load state: %w", getErr)
	}
	if candidate := strings.TrimSpace(state.CandidateSourceVersion); state.State == core.GestaltdSourceVersionStatePromoting &&
		candidate != "" && candidate != sourceVersion {
		return nil, ErrGestaltdSourceVersionMismatch
	}
	state.CandidateSourceVersion = sourceVersion
	state.State = core.GestaltdSourceVersionStatePromoting
	state.UpdatedAt = normalizedSourceVersionTime(at)
	if err := store.Put(ctx, gestaltdSourceVersionStateRecord(state)); err != nil {
		return nil, fmt.Errorf("begin gestaltd source version promotion: write state: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("begin gestaltd source version promotion: commit: %w", err)
	}
	committed = true
	return state, nil
}

func (s *GestaltdSourceVersionService) CancelPromotion(ctx context.Context, sourceVersion string, at time.Time) (*core.GestaltdSourceVersionState, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("cancel gestaltd source version promotion: service is not configured")
	}
	sourceVersion = strings.TrimSpace(sourceVersion)
	if sourceVersion == "" {
		return nil, fmt.Errorf("cancel gestaltd source version promotion: source version is required")
	}
	tx, err := s.db.Transaction(ctx, []string{StoreGestaltdSourceVersionState}, idb.TransactionReadwrite, idb.TransactionOptions{})
	if err != nil {
		return nil, fmt.Errorf("cancel gestaltd source version promotion: begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Abort(context.WithoutCancel(ctx))
		}
	}()

	store := tx.ObjectStore(StoreGestaltdSourceVersionState)
	rec, err := store.Get(ctx, gestaltdSourceVersionStateID)
	if err != nil {
		return nil, fmt.Errorf("cancel gestaltd source version promotion: load state: %w", err)
	}
	state := recordToGestaltdSourceVersionState(rec)
	if candidate := strings.TrimSpace(state.CandidateSourceVersion); candidate != "" && candidate != sourceVersion {
		return nil, ErrGestaltdSourceVersionMismatch
	}
	state.CandidateSourceVersion = ""
	state.State = core.GestaltdSourceVersionStateStable
	state.UpdatedAt = normalizedSourceVersionTime(at)
	if err := store.Put(ctx, gestaltdSourceVersionStateRecord(state)); err != nil {
		return nil, fmt.Errorf("cancel gestaltd source version promotion: write state: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("cancel gestaltd source version promotion: commit: %w", err)
	}
	committed = true
	return state, nil
}

func (s *GestaltdSourceVersionService) Promote(
	ctx context.Context,
	sourceVersion string,
	at time.Time,
	enrollmentWindow time.Duration,
	rolloutTimeout time.Duration,
) (*core.GestaltdSourceVersionState, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("promote gestaltd source version: service is not configured")
	}
	sourceVersion = strings.TrimSpace(sourceVersion)
	if sourceVersion == "" {
		return nil, fmt.Errorf("promote gestaltd source version: source version is required")
	}
	at = normalizedSourceVersionTime(at)
	if enrollmentWindow <= 0 || rolloutTimeout <= enrollmentWindow {
		return nil, fmt.Errorf("promote gestaltd source version: invalid rollout windows")
	}

	tx, err := s.db.Transaction(
		ctx,
		[]string{StoreGestaltdSourceVersionState, StoreAppRollouts},
		idb.TransactionReadwrite,
		idb.TransactionOptions{},
	)
	if err != nil {
		return nil, fmt.Errorf("promote gestaltd source version: begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Abort(context.WithoutCancel(ctx))
		}
	}()

	stateStore := tx.ObjectStore(StoreGestaltdSourceVersionState)
	state := &core.GestaltdSourceVersionState{}
	if rec, getErr := stateStore.Get(ctx, gestaltdSourceVersionStateID); getErr == nil {
		state = recordToGestaltdSourceVersionState(rec)
	} else if !errors.Is(getErr, idb.ErrNotFound) {
		return nil, fmt.Errorf("promote gestaltd source version: load state: %w", getErr)
	}
	if candidate := strings.TrimSpace(state.CandidateSourceVersion); candidate != "" && candidate != sourceVersion {
		return nil, ErrGestaltdSourceVersionMismatch
	}

	if strings.TrimSpace(state.CurrentSourceVersion) != sourceVersion {
		promotionStartedAt := time.Time{}
		if state.State == core.GestaltdSourceVersionStatePromoting {
			promotionStartedAt = state.UpdatedAt
		}
		rolloutStore := tx.ObjectStore(StoreAppRollouts)
		recs, listErr := rolloutStore.GetAll(ctx, nil)
		if listErr != nil {
			return nil, fmt.Errorf("promote gestaltd source version: list app rollouts: %w", listErr)
		}
		for _, rec := range recs {
			rollout := recordToAppRollout(rec)
			retarget := isActiveRolloutState(rollout.State)
			if !retarget && strings.TrimSpace(rollout.TargetSourceVersion) != sourceVersion && !promotionStartedAt.IsZero() {
				terminalAt := rollout.CompletedAt
				if rollout.State == core.AppRolloutStateFailed {
					terminalAt = rollout.FailedAt
				}
				retarget = !terminalAt.IsZero() && !terminalAt.Before(promotionStartedAt)
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
				return nil, fmt.Errorf("promote gestaltd source version: retarget rollout %s: %w", rollout.App, err)
			}
		}
	}

	state.CurrentSourceVersion = sourceVersion
	state.CandidateSourceVersion = ""
	state.State = core.GestaltdSourceVersionStateStable
	state.UpdatedAt = at
	if err := stateStore.Put(ctx, gestaltdSourceVersionStateRecord(state)); err != nil {
		return nil, fmt.Errorf("promote gestaltd source version: write state: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("promote gestaltd source version: commit: %w", err)
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
	rec := idb.Record{
		"id":                     gestaltdSourceVersionStateID,
		"current_source_version": strings.TrimSpace(state.CurrentSourceVersion),
		"state":                  strings.TrimSpace(state.State),
		"updated_at":             normalizedSourceVersionTime(state.UpdatedAt),
	}
	if candidate := strings.TrimSpace(state.CandidateSourceVersion); candidate != "" {
		rec["candidate_source_version"] = candidate
	}
	return rec
}

func recordToGestaltdSourceVersionState(rec idb.Record) *core.GestaltdSourceVersionState {
	return &core.GestaltdSourceVersionState{
		CurrentSourceVersion:   recString(rec, "current_source_version"),
		CandidateSourceVersion: recString(rec, "candidate_source_version"),
		State:                  recString(rec, "state"),
		UpdatedAt:              recTime(rec, "updated_at"),
	}
}
