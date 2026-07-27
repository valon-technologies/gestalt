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

var (
	ErrAppRolloutActive          = errors.New("app rollout is active")
	ErrAppRolloutEpochMismatch   = errors.New("app rollout epoch does not match")
	ErrAppRolloutVersionMismatch = errors.New("app rollout version does not match")
)

type AppRolloutService struct {
	db    indexeddb.IndexedDB
	store idb.ObjectStore
}

func NewAppRolloutService(ds indexeddb.IndexedDB) *AppRolloutService {
	return &AppRolloutService{db: ds, store: ds.ObjectStore(StoreAppRollouts)}
}

func (s *AppRolloutService) Get(ctx context.Context, app string) (*core.AppRollout, error) {
	if s == nil {
		return nil, fmt.Errorf("get app rollout: service is not configured")
	}
	app = strings.TrimSpace(app)
	if app == "" {
		return nil, fmt.Errorf("get app rollout: app is required")
	}
	rec, err := s.store.Get(ctx, app)
	if err != nil {
		if errors.Is(err, idb.ErrNotFound) {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("get app rollout: %w", err)
	}
	return recordToAppRollout(rec), nil
}

func (s *AppRolloutService) Create(ctx context.Context, rollout *core.AppRollout) (*core.AppRollout, error) {
	if s == nil {
		return nil, fmt.Errorf("create app rollout: service is not configured")
	}
	if err := validateAppRollout(rollout); err != nil {
		return nil, fmt.Errorf("create app rollout: %w", err)
	}
	rollout = normalizeAppRollout(rollout)
	tx, err := s.db.Transaction(ctx, []string{StoreAppRollouts}, idb.TransactionReadwrite, idb.TransactionOptions{})
	if err != nil {
		return nil, fmt.Errorf("create app rollout: begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Abort(context.WithoutCancel(ctx))
		}
	}()
	store := tx.ObjectStore(StoreAppRollouts)
	existing, err := store.GetAll(ctx, rollout.App, 1)
	switch {
	case err != nil:
		return nil, fmt.Errorf("create app rollout: load current rollout: %w", err)
	case len(existing) > 0:
		if isActiveRolloutState(recordToAppRollout(existing[0]).State) {
			return nil, ErrAppRolloutActive
		}
		if err := store.Put(ctx, appRolloutRecord(rollout)); err != nil {
			return nil, fmt.Errorf("create app rollout: replace terminal rollout: %w", err)
		}
	default:
		if err := store.Add(ctx, appRolloutRecord(rollout)); err != nil {
			if errors.Is(err, idb.ErrAlreadyExists) {
				return nil, ErrAppRolloutActive
			}
			return nil, fmt.Errorf("create app rollout: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("create app rollout: commit: %w", err)
	}
	committed = true
	return rollout, nil
}

func (s *AppRolloutService) ListActive(ctx context.Context) ([]*core.AppRollout, error) {
	if s == nil {
		return nil, fmt.Errorf("list active app rollouts: service is not configured")
	}
	recs, err := s.store.GetAll(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("list active app rollouts: %w", err)
	}
	out := make([]*core.AppRollout, 0, len(recs))
	for _, rec := range recs {
		rollout := recordToAppRollout(rec)
		if isActiveRolloutState(rollout.State) {
			out = append(out, rollout)
		}
	}
	return out, nil
}

func (s *AppRolloutService) ListActiveAndRecentTerminal(ctx context.Context, since time.Time) ([]*core.AppRollout, error) {
	if s == nil {
		return nil, fmt.Errorf("list active and recent terminal app rollouts: service is not configured")
	}
	recs, err := s.store.GetAll(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("list active and recent terminal app rollouts: %w", err)
	}
	since = since.UTC()
	out := make([]*core.AppRollout, 0, len(recs))
	for _, rec := range recs {
		rollout := recordToAppRollout(rec)
		if isActiveRolloutState(rollout.State) {
			out = append(out, rollout)
			continue
		}
		terminalAt := rollout.CompletedAt
		if rollout.State == core.AppRolloutStateFailed {
			terminalAt = rollout.FailedAt
		}
		if !terminalAt.IsZero() && !terminalAt.Before(since) {
			out = append(out, rollout)
		}
	}
	return out, nil
}

func (s *AppRolloutService) MarkRestarting(ctx context.Context, app, version string) (*core.AppRollout, error) {
	return s.transition(ctx, app, version, core.AppRolloutStateRestarting, time.Time{}, nil)
}

func (s *AppRolloutService) MarkComplete(ctx context.Context, app, version string, completedAt time.Time) (*core.AppRollout, error) {
	return s.transition(ctx, app, version, core.AppRolloutStateComplete, completedAt, nil)
}

func (s *AppRolloutService) MarkFailed(ctx context.Context, app, version string, failedAt time.Time) (*core.AppRollout, error) {
	return s.transition(ctx, app, version, core.AppRolloutStateFailed, failedAt, nil)
}

func (s *AppRolloutService) MarkRestartingForRollout(ctx context.Context, expected *core.AppRollout) (*core.AppRollout, error) {
	return s.transitionForRollout(ctx, expected, core.AppRolloutStateRestarting, time.Time{})
}

func (s *AppRolloutService) MarkCompleteForRollout(ctx context.Context, expected *core.AppRollout, completedAt time.Time) (*core.AppRollout, error) {
	return s.transitionForRollout(ctx, expected, core.AppRolloutStateComplete, completedAt)
}

func (s *AppRolloutService) MarkFailedForRollout(ctx context.Context, expected *core.AppRollout, failedAt time.Time) (*core.AppRollout, error) {
	return s.transitionForRollout(ctx, expected, core.AppRolloutStateFailed, failedAt)
}

func (s *AppRolloutService) transitionForRollout(ctx context.Context, expected *core.AppRollout, state core.AppRolloutState, at time.Time) (*core.AppRollout, error) {
	if expected == nil {
		return nil, fmt.Errorf("transition app rollout: expected rollout is required")
	}
	return s.transition(ctx, expected.App, expected.Version, state, at, expected)
}

func (s *AppRolloutService) transition(
	ctx context.Context,
	app, version string,
	state core.AppRolloutState,
	at time.Time,
	expected *core.AppRollout,
) (*core.AppRollout, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("transition app rollout: service is not configured")
	}
	app = strings.TrimSpace(app)
	version = strings.TrimSpace(version)
	if app == "" || version == "" {
		return nil, fmt.Errorf("transition app rollout: app and version are required")
	}
	tx, err := s.db.Transaction(ctx, []string{StoreAppRollouts}, idb.TransactionReadwrite, idb.TransactionOptions{})
	if err != nil {
		return nil, fmt.Errorf("transition app rollout: begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Abort(context.WithoutCancel(ctx))
		}
	}()
	store := tx.ObjectStore(StoreAppRollouts)
	rec, err := store.Get(ctx, app)
	if err != nil {
		return nil, fmt.Errorf("transition app rollout: load current rollout: %w", err)
	}
	rollout := recordToAppRollout(rec)
	if strings.TrimSpace(rollout.Version) != version {
		return nil, ErrAppRolloutVersionMismatch
	}
	if expected != nil &&
		(!rollout.CreatedAt.Equal(expected.CreatedAt) ||
			strings.TrimSpace(rollout.TargetSourceVersion) != strings.TrimSpace(expected.TargetSourceVersion)) {
		return nil, ErrAppRolloutEpochMismatch
	}
	if rollout.State == state {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("transition app rollout: commit: %w", err)
		}
		committed = true
		return rollout, nil
	}
	if !isActiveRolloutState(rollout.State) {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("transition app rollout: commit: %w", err)
		}
		committed = true
		return rollout, nil
	}
	rollout.State = state
	switch state {
	case core.AppRolloutStateComplete:
		rollout.CompletedAt = normalizedRolloutTime(at)
	case core.AppRolloutStateFailed:
		rollout.FailedAt = normalizedRolloutTime(at)
	case core.AppRolloutStateRestarting:
	default:
		return nil, fmt.Errorf("transition app rollout: invalid target state %q", state)
	}
	if err := store.Put(ctx, appRolloutRecord(rollout)); err != nil {
		return nil, fmt.Errorf("transition app rollout: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("transition app rollout: commit: %w", err)
	}
	committed = true
	return rollout, nil
}

func validateAppRollout(rollout *core.AppRollout) error {
	if rollout == nil {
		return fmt.Errorf("record is required")
	}
	if strings.TrimSpace(rollout.App) == "" || strings.TrimSpace(rollout.Version) == "" {
		return fmt.Errorf("app and version are required")
	}
	if rollout.State != core.AppRolloutStateEnrolling {
		return fmt.Errorf("initial state must be %q", core.AppRolloutStateEnrolling)
	}
	if rollout.CreatedAt.IsZero() || !rollout.EnrollmentEndsAt.After(rollout.CreatedAt) || !rollout.Deadline.After(rollout.EnrollmentEndsAt) {
		return fmt.Errorf("created_at, enrollment_ends_at, and deadline must be ordered")
	}
	return nil
}

func normalizeAppRollout(rollout *core.AppRollout) *core.AppRollout {
	copy := *rollout
	copy.App = strings.TrimSpace(copy.App)
	copy.Version = strings.TrimSpace(copy.Version)
	copy.TargetSourceVersion = strings.TrimSpace(copy.TargetSourceVersion)
	copy.CreatedAt = normalizedRolloutTime(copy.CreatedAt)
	copy.EnrollmentEndsAt = normalizedRolloutTime(copy.EnrollmentEndsAt)
	copy.Deadline = normalizedRolloutTime(copy.Deadline)
	return &copy
}

func normalizedRolloutTime(value time.Time) time.Time {
	if value.IsZero() {
		value = time.Now()
	}
	return value.UTC().Truncate(time.Millisecond)
}

func isActiveRolloutState(state core.AppRolloutState) bool {
	return state == core.AppRolloutStateEnrolling || state == core.AppRolloutStateRestarting
}

func appRolloutRecord(rollout *core.AppRollout) idb.Record {
	rec := idb.Record{
		"id":                    rollout.App,
		"app":                   rollout.App,
		"version":               rollout.Version,
		"state":                 string(rollout.State),
		"target_source_version": rollout.TargetSourceVersion,
		"created_at":            rollout.CreatedAt,
		"enrollment_ends_at":    rollout.EnrollmentEndsAt,
		"deadline":              rollout.Deadline,
	}
	if !rollout.CompletedAt.IsZero() {
		rec["completed_at"] = rollout.CompletedAt
	}
	if !rollout.FailedAt.IsZero() {
		rec["failed_at"] = rollout.FailedAt
	}
	return rec
}

func recordToAppRollout(rec idb.Record) *core.AppRollout {
	return &core.AppRollout{
		App:                 recString(rec, "app"),
		Version:             recString(rec, "version"),
		State:               core.AppRolloutState(recString(rec, "state")),
		TargetSourceVersion: recString(rec, "target_source_version"),
		CreatedAt:           recTime(rec, "created_at"),
		EnrollmentEndsAt:    recTime(rec, "enrollment_ends_at"),
		Deadline:            recTime(rec, "deadline"),
		CompletedAt:         recTime(rec, "completed_at"),
		FailedAt:            recTime(rec, "failed_at"),
	}
}
