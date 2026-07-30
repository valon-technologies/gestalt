package appregistry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
)

type RecoveryChangeRequests interface {
	ListAllKnownVersions(context.Context) ([]*core.AppInstallation, error)
	LatestDesiredRevisionID(context.Context, string, string) (string, error)
}

type RecoveryOutcomes interface {
	Get(context.Context, string) (*core.AppVersionRolloutOutcome, error)
}

type RecoveryObservations interface {
	RecordIfCurrentFailed(context.Context, *core.AppVersionRecoveryObservation) (*core.AppVersionRecoveryObservation, bool, error)
}

type RecoveryObserver struct {
	ChangeRequests  RecoveryChangeRequests
	Outcomes        RecoveryOutcomes
	Observations    RecoveryObservations
	SourceVersions  FleetSourceVersions
	Heartbeats      FleetHeartbeats
	HeartbeatTTL    time.Duration
	StabilityWindow time.Duration
	Interval        time.Duration
	Ready           <-chan struct{}
	Now             func() time.Time
	NewTicker       func(time.Duration) (<-chan time.Time, func())

	startOnce sync.Once
	startMu   sync.Mutex
	cancel    context.CancelFunc
	done      chan struct{}
	passMu    sync.Mutex
	stateMu   sync.Mutex
	healthy   map[string]recoveryStability
	completed map[string]struct{}
}

type RecoveryObserverConfig struct {
	ChangeRequests  RecoveryChangeRequests
	Outcomes        RecoveryOutcomes
	Observations    RecoveryObservations
	SourceVersions  FleetSourceVersions
	Heartbeats      FleetHeartbeats
	HeartbeatTTL    time.Duration
	StabilityWindow time.Duration
	Interval        time.Duration
	Ready           <-chan struct{}
	Now             func() time.Time
	NewTicker       func(time.Duration) (<-chan time.Time, func())
}

type recoveryStability struct {
	changeRequestID string
	version         string
	sourceVersion   string
	minimum         int
	healthySince    time.Time
}

func NewRecoveryObserver(cfg RecoveryObserverConfig) *RecoveryObserver {
	return &RecoveryObserver{
		ChangeRequests:  cfg.ChangeRequests,
		Outcomes:        cfg.Outcomes,
		Observations:    cfg.Observations,
		SourceVersions:  cfg.SourceVersions,
		Heartbeats:      cfg.Heartbeats,
		HeartbeatTTL:    cfg.HeartbeatTTL,
		StabilityWindow: cfg.StabilityWindow,
		Interval:        cfg.Interval,
		Ready:           cfg.Ready,
		Now:             cfg.Now,
		NewTicker:       cfg.NewTicker,
		healthy:         make(map[string]recoveryStability),
		completed:       make(map[string]struct{}),
	}
}

func (o *RecoveryObserver) Start(ctx context.Context) {
	if !o.configured() {
		return
	}
	o.startOnce.Do(func() {
		loopCtx, cancel := context.WithCancel(ctx)
		o.startMu.Lock()
		o.cancel = cancel
		o.done = make(chan struct{})
		o.startMu.Unlock()
		go func() {
			defer close(o.done)
			o.loop(loopCtx)
		}()
	})
}

func (o *RecoveryObserver) Stop() {
	if o == nil {
		return
	}
	o.startMu.Lock()
	cancel := o.cancel
	done := o.done
	o.startMu.Unlock()
	if cancel == nil || done == nil {
		return
	}
	cancel()
	<-done
}

func (o *RecoveryObserver) loop(ctx context.Context) {
	if o.Ready != nil {
		select {
		case <-ctx.Done():
			return
		case <-o.Ready:
		}
	}
	o.observeAndLog(ctx)
	ticks, stop := o.newTicker(o.interval())
	defer stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			o.observeAndLog(ctx)
		}
	}
}

func (o *RecoveryObserver) observeAndLog(ctx context.Context) {
	if err := o.ObserveOnce(ctx); err != nil && ctx.Err() == nil {
		slog.Warn("app version recovery observation pass failed", "error", err)
	}
}

// ObserveOnce samples fleet health once. A pass never overlaps another pass on
// the same observer; separate replicas are made idempotent by the fenced
// recovery-observation transaction.
func (o *RecoveryObserver) ObserveOnce(ctx context.Context) error {
	if !o.configured() {
		return fmt.Errorf("app version recovery observer is not configured")
	}
	if !o.passMu.TryLock() {
		return nil
	}
	defer o.passMu.Unlock()

	now := o.now()
	ttl := o.HeartbeatTTL
	if ttl <= 0 {
		return fmt.Errorf("app version recovery observer: heartbeat TTL must be positive")
	}
	if o.StabilityWindow <= 0 {
		return fmt.Errorf("app version recovery observer: stability window must be positive")
	}

	known, err := o.ChangeRequests.ListAllKnownVersions(ctx)
	if err != nil {
		o.resetAll()
		return fmt.Errorf("observe app recovery: load desired versions: %w", err)
	}
	source, err := o.SourceVersions.Get(ctx)
	if err != nil {
		o.resetAll()
		if errors.Is(err, core.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("observe app recovery: load source state: %w", err)
	}
	sourceVersion := strings.TrimSpace(source.CurrentSourceVersion)
	minimum := source.MinimumHealthyInstances
	if sourceVersion == "" || minimum <= 0 {
		o.resetAll()
		return nil
	}
	heartbeats, err := o.Heartbeats.ListFreshBySourceVersion(ctx, sourceVersion, now.Add(-ttl))
	if err != nil {
		o.resetAll()
		return fmt.Errorf("observe app recovery: load fresh heartbeats: %w", err)
	}

	byApp := groupInstallationsByApp(known)
	seen := make(map[string]struct{}, len(byApp))
	var errs []error
	for app, installations := range byApp {
		seen[app] = struct{}{}
		desiredVersion := coredata.LatestKnownVersion(installations)
		changeRequestID, err := o.ChangeRequests.LatestDesiredRevisionID(ctx, app, desiredVersion)
		if err != nil {
			o.reset(app)
			errs = append(errs, fmt.Errorf("observe app recovery for %s: load desired revision: %w", app, err))
			continue
		}
		if changeRequestID == "" || o.isCompleted(changeRequestID) {
			o.reset(app)
			continue
		}
		outcome, err := o.Outcomes.Get(ctx, changeRequestID)
		if errors.Is(err, core.ErrNotFound) {
			o.reset(app)
			continue
		}
		if err != nil {
			o.reset(app)
			errs = append(errs, fmt.Errorf("observe app recovery for %s: load rollout outcome: %w", app, err))
			continue
		}
		if outcome == nil || outcome.FailedAt.IsZero() || !outcome.CompletedAt.IsZero() ||
			strings.TrimSpace(outcome.App) != app ||
			strings.TrimSpace(outcome.Version) != desiredVersion {
			o.reset(app)
			continue
		}

		projection := EvaluateFleetState(FleetEvaluation{
			App:                     app,
			DesiredVersion:          desiredVersion,
			SourceVersion:           sourceVersion,
			MinimumHealthyInstances: minimum,
			Cutoff:                  now.Add(-ttl),
			EvaluatedAt:             now,
			Heartbeats:              heartbeats,
		})
		if projection.State != core.AppFleetStateHealthy {
			o.reset(app)
			continue
		}
		stability := recoveryStability{
			changeRequestID: changeRequestID,
			version:         desiredVersion,
			sourceVersion:   sourceVersion,
			minimum:         minimum,
		}
		healthySince, stable := o.advance(app, stability, now)
		if !stable || now.Sub(healthySince) < o.StabilityWindow {
			continue
		}
		_, recorded, err := o.Observations.RecordIfCurrentFailed(ctx, &core.AppVersionRecoveryObservation{
			ID:                      changeRequestID,
			App:                     app,
			Version:                 desiredVersion,
			RecoveredAt:             now,
			SourceVersion:           sourceVersion,
			LiveInstances:           projection.LiveInstances,
			MinimumHealthyInstances: minimum,
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("observe app recovery for %s: persist observation: %w", app, err))
			continue
		}
		o.reset(app)
		if recorded {
			o.markCompleted(changeRequestID)
		}
	}
	o.resetUnseen(seen)
	return errors.Join(errs...)
}

func (o *RecoveryObserver) advance(app string, next recoveryStability, now time.Time) (time.Time, bool) {
	o.stateMu.Lock()
	defer o.stateMu.Unlock()
	current, ok := o.healthy[app]
	if !ok ||
		current.changeRequestID != next.changeRequestID ||
		current.version != next.version ||
		current.sourceVersion != next.sourceVersion ||
		current.minimum != next.minimum {
		next.healthySince = now
		o.healthy[app] = next
		return now, false
	}
	return current.healthySince, true
}

func (o *RecoveryObserver) reset(app string) {
	o.stateMu.Lock()
	defer o.stateMu.Unlock()
	delete(o.healthy, app)
}

func (o *RecoveryObserver) resetAll() {
	o.stateMu.Lock()
	defer o.stateMu.Unlock()
	clear(o.healthy)
}

func (o *RecoveryObserver) resetUnseen(seen map[string]struct{}) {
	o.stateMu.Lock()
	defer o.stateMu.Unlock()
	for app := range o.healthy {
		if _, ok := seen[app]; !ok {
			delete(o.healthy, app)
		}
	}
}

func (o *RecoveryObserver) isCompleted(id string) bool {
	o.stateMu.Lock()
	defer o.stateMu.Unlock()
	_, ok := o.completed[id]
	return ok
}

func (o *RecoveryObserver) markCompleted(id string) {
	o.stateMu.Lock()
	defer o.stateMu.Unlock()
	o.completed[id] = struct{}{}
}

func (o *RecoveryObserver) configured() bool {
	return o != nil &&
		o.ChangeRequests != nil &&
		o.Outcomes != nil &&
		o.Observations != nil &&
		o.SourceVersions != nil &&
		o.Heartbeats != nil
}

func (o *RecoveryObserver) interval() time.Duration {
	if o.Interval > 0 {
		return o.Interval
	}
	return config.DefaultAppRegistryHeartbeatInterval
}

func (o *RecoveryObserver) now() time.Time {
	if o.Now != nil {
		return o.Now().UTC().Truncate(time.Millisecond)
	}
	return time.Now().UTC().Truncate(time.Millisecond)
}

func (o *RecoveryObserver) newTicker(interval time.Duration) (<-chan time.Time, func()) {
	if o.NewTicker != nil {
		return o.NewTicker(interval)
	}
	ticker := time.NewTicker(interval)
	return ticker.C, ticker.Stop
}
