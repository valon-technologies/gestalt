package appregistry

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestRecoveryObserverStabilityBoundaryAndNoRolloutMutation(t *testing.T) {
	t.Parallel()

	fixture := newRecoveryFixture(t)
	observer := fixture.observer(fixture.services.AppVersionRecoveryObservations)
	beforeRequests := fixture.countStore(coredata.StoreAppVersionChangeRequests)
	beforeOutcomes := fixture.countStore(coredata.StoreAppVersionRolloutOutcomes)
	beforeAutoDeploy := fixture.countStore(coredata.StoreAppAutoDeploySettings)
	autoDeployBefore, err := fixture.services.AutoDeploySettings.Get(fixture.ctx, fixture.app)
	if err != nil {
		t.Fatalf("Get auto-deploy before recovery: %v", err)
	}

	fixture.observe(t, observer)
	fixture.assertNoRecovery(t)
	fixture.now = fixture.now.Add(fixture.window - time.Millisecond)
	fixture.observe(t, observer)
	fixture.assertNoRecovery(t)
	fixture.now = fixture.now.Add(time.Millisecond)
	fixture.observe(t, observer)

	recovery, err := fixture.services.AppVersionRecoveryObservations.Get(fixture.ctx, fixture.requestID)
	if err != nil {
		t.Fatalf("Get recovery: %v", err)
	}
	if !recovery.RecoveredAt.Equal(fixture.now) || recovery.LiveInstances != fixture.minimum {
		t.Fatalf("recovery = %#v", recovery)
	}
	rollout, err := fixture.services.AppRollouts.Get(fixture.ctx, fixture.app)
	if err != nil {
		t.Fatalf("Get rollout: %v", err)
	}
	if rollout.State != core.AppRolloutStateFailed || rollout.FailedAt.IsZero() {
		t.Fatalf("failed rollout was mutated: %#v", rollout)
	}
	if got := fixture.countStore(coredata.StoreAppVersionChangeRequests); got != beforeRequests {
		t.Fatalf("change request count = %d, want %d", got, beforeRequests)
	}
	if got := fixture.countStore(coredata.StoreAppVersionRolloutOutcomes); got != beforeOutcomes {
		t.Fatalf("rollout outcome count = %d, want %d", got, beforeOutcomes)
	}
	if got := fixture.countStore(coredata.StoreAppAutoDeploySettings); got != beforeAutoDeploy {
		t.Fatalf("auto-deploy count = %d, want %d", got, beforeAutoDeploy)
	}
	autoDeployAfter, err := fixture.services.AutoDeploySettings.Get(fixture.ctx, fixture.app)
	if err != nil {
		t.Fatalf("Get auto-deploy after recovery: %v", err)
	}
	if autoDeployAfter.Enabled != autoDeployBefore.Enabled ||
		autoDeployAfter.PendingVersion != autoDeployBefore.PendingVersion ||
		autoDeployAfter.LastSeenVersion != autoDeployBefore.LastSeenVersion ||
		autoDeployAfter.LastError != autoDeployBefore.LastError ||
		!autoDeployAfter.LastFailedRolloutAt.Equal(autoDeployBefore.LastFailedRolloutAt) {
		t.Fatalf("auto-deploy mutated: before=%#v after=%#v", autoDeployBefore, autoDeployAfter)
	}
}

func TestRecoveryObserverResetsStabilityWhenHealthIsLost(t *testing.T) {
	t.Parallel()

	fixture := newRecoveryFixture(t)
	observer := fixture.observer(fixture.services.AppVersionRecoveryObservations)
	fixture.observe(t, observer)

	fixture.now = fixture.now.Add(20 * time.Second)
	fixture.writeHeartbeats(t, core.GestaltdInstanceAppStateNotRunning, "")
	fixture.observe(t, observer)

	fixture.now = fixture.now.Add(10 * time.Second)
	fixture.writeHeartbeats(t, core.GestaltdInstanceAppStateRunning, fixture.version)
	fixture.observe(t, observer)
	fixture.now = fixture.now.Add(fixture.window - time.Millisecond)
	fixture.observe(t, observer)
	fixture.assertNoRecovery(t)
	fixture.now = fixture.now.Add(time.Millisecond)
	fixture.observe(t, observer)
	if _, err := fixture.services.AppVersionRecoveryObservations.Get(fixture.ctx, fixture.requestID); err != nil {
		t.Fatalf("Get recovery after reset window: %v", err)
	}
}

func TestRecoveryObserverResetsStabilityForNewSourceEpoch(t *testing.T) {
	t.Parallel()

	fixture := newRecoveryFixture(t)
	observer := fixture.observer(fixture.services.AppVersionRecoveryObservations)
	fixture.observe(t, observer)

	fixture.now = fixture.now.Add(20 * time.Second)
	fixture.source = "source-b"
	if _, err := fixture.services.GestaltdSourceVersionState.Activate(
		fixture.ctx, fixture.source, fixture.now, false, time.Minute, 15*time.Minute, fixture.minimum,
	); err != nil {
		t.Fatalf("Activate new source: %v", err)
	}
	fixture.writeHeartbeats(t, core.GestaltdInstanceAppStateRunning, fixture.version)
	fixture.observe(t, observer)

	fixture.now = fixture.now.Add(fixture.window - time.Millisecond)
	fixture.observe(t, observer)
	fixture.assertNoRecovery(t)
	fixture.now = fixture.now.Add(time.Millisecond)
	fixture.observe(t, observer)
	recovery, err := fixture.services.AppVersionRecoveryObservations.Get(fixture.ctx, fixture.requestID)
	if err != nil {
		t.Fatalf("Get recovery after source reset: %v", err)
	}
	if recovery.SourceVersion != "source-b" {
		t.Fatalf("recovery source = %q, want source-b", recovery.SourceVersion)
	}
}

func TestRecoveryObserverRetriesWriteFailure(t *testing.T) {
	t.Parallel()

	fixture := newRecoveryFixture(t)
	recorder := &failOnceRecoveryRecorder{delegate: fixture.services.AppVersionRecoveryObservations}
	observer := fixture.observer(recorder)
	fixture.observe(t, observer)
	fixture.now = fixture.now.Add(fixture.window)
	if err := observer.ObserveOnce(fixture.ctx); err == nil {
		t.Fatal("ObserveOnce at boundary succeeded, want write error")
	}
	fixture.assertNoRecovery(t)

	fixture.now = fixture.now.Add(time.Second)
	fixture.observe(t, observer)
	recovery, err := fixture.services.AppVersionRecoveryObservations.Get(fixture.ctx, fixture.requestID)
	if err != nil {
		t.Fatalf("Get retried recovery: %v", err)
	}
	if !recovery.RecoveredAt.Equal(fixture.now) || recorder.calls != 2 {
		t.Fatalf("retried recovery = %#v, calls = %d", recovery, recorder.calls)
	}
}

func TestConcurrentRecoveryObserversRecordExactlyOnce(t *testing.T) {
	t.Parallel()

	fixture := newRecoveryFixture(t)
	first := fixture.observer(fixture.services.AppVersionRecoveryObservations)
	second := fixture.observer(fixture.services.AppVersionRecoveryObservations)
	fixture.observe(t, first)
	fixture.observe(t, second)
	fixture.now = fixture.now.Add(fixture.window)

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, observer := range []*RecoveryObserver{first, second} {
		wg.Add(1)
		go func(observer *RecoveryObserver) {
			defer wg.Done()
			errs <- observer.ObserveOnce(fixture.ctx)
		}(observer)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent ObserveOnce: %v", err)
		}
	}
	if got := fixture.countStore(coredata.StoreAppVersionRecoveryObservations); got != 1 {
		t.Fatalf("recovery observation count = %d, want 1", got)
	}
}

type recoveryFixture struct {
	t         *testing.T
	ctx       context.Context
	services  *coredata.Services
	now       time.Time
	window    time.Duration
	ttl       time.Duration
	app       string
	version   string
	requestID string
	source    string
	minimum   int
}

func newRecoveryFixture(t *testing.T) *recoveryFixture {
	t.Helper()
	f := &recoveryFixture{
		t:         t,
		ctx:       context.Background(),
		services:  testutil.NewStubServices(t),
		now:       time.Date(2026, 7, 30, 13, 0, 0, 0, time.UTC),
		window:    30 * time.Second,
		ttl:       2 * time.Minute,
		app:       "g-issues",
		version:   "v2",
		requestID: "request-v2",
		source:    "source-a",
		minimum:   2,
	}
	if _, err := f.services.GestaltdSourceVersionState.Activate(
		f.ctx, f.source, f.now.Add(-time.Hour), false, time.Minute, 15*time.Minute, f.minimum,
	); err != nil {
		t.Fatalf("Activate source: %v", err)
	}
	if _, err := f.services.AppVersionChangeRequests.AppendRequest(f.ctx, &core.AppVersionChangeRequest{
		ID:          f.requestID,
		App:         f.app,
		FromVersion: "v1",
		ToVersion:   f.version,
		Timestamp:   f.now.Add(-20 * time.Minute),
	}); err != nil {
		t.Fatalf("AppendRequest: %v", err)
	}
	rollout, err := f.services.GestaltdSourceVersionState.CreateAppRollout(f.ctx, &core.AppRollout{
		App:              f.app,
		Version:          f.version,
		State:            core.AppRolloutStateEnrolling,
		CreatedAt:        f.now.Add(-20 * time.Minute),
		EnrollmentEndsAt: f.now.Add(-19 * time.Minute),
		Deadline:         f.now.Add(-5 * time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateAppRollout: %v", err)
	}
	failed, err := f.services.AppRollouts.MarkFailedForRollout(f.ctx, rollout, f.now.Add(-5*time.Minute))
	if err != nil {
		t.Fatalf("MarkFailedForRollout: %v", err)
	}
	if err := f.services.AppVersionRolloutOutcomes.RecordFailed(
		f.ctx, f.requestID, f.app, f.version, failed.FailedAt,
	); err != nil {
		t.Fatalf("RecordFailed: %v", err)
	}
	if _, err := f.services.AutoDeploySettings.Update(f.ctx, f.app, func(settings *core.AppAutoDeploySettings) error {
		settings.Enabled = true
		settings.PendingVersion = "v3"
		settings.LastSeenVersion = "v3"
		settings.LastError = "pending"
		settings.LastFailedRolloutAt = failed.FailedAt
		return nil
	}); err != nil {
		t.Fatalf("Seed auto-deploy settings: %v", err)
	}
	f.writeHeartbeats(t, core.GestaltdInstanceAppStateRunning, f.version)
	return f
}

func (f *recoveryFixture) observer(observations RecoveryObservations) *RecoveryObserver {
	return NewRecoveryObserver(RecoveryObserverConfig{
		ChangeRequests:  f.services.AppVersionChangeRequests,
		Outcomes:        f.services.AppVersionRolloutOutcomes,
		Observations:    observations,
		SourceVersions:  f.services.GestaltdSourceVersionState,
		Heartbeats:      f.services.GestaltdInstanceHeartbeats,
		HeartbeatTTL:    f.ttl,
		StabilityWindow: f.window,
		Interval:        10 * time.Second,
		Now:             func() time.Time { return f.now },
	})
}

func (f *recoveryFixture) writeHeartbeats(t *testing.T, state core.GestaltdInstanceAppState, runningVersion string) {
	t.Helper()
	for i := 0; i < f.minimum; i++ {
		if _, err := f.services.GestaltdInstanceHeartbeats.Upsert(f.ctx, &core.GestaltdInstanceHeartbeat{
			InstanceID:    []string{"instance-a", "instance-b"}[i],
			SourceVersion: f.source,
			StartedAt:     f.now.Add(-time.Hour),
			HeartbeatAt:   f.now,
			Apps: map[string]core.GestaltdInstanceAppHeartbeat{
				f.app: {
					State:          state,
					DesiredVersion: f.version,
					RunningVersion: runningVersion,
					ObservedAt:     f.now,
				},
			},
		}); err != nil {
			t.Fatalf("Upsert heartbeat: %v", err)
		}
	}
}

func (f *recoveryFixture) observe(t *testing.T, observer *RecoveryObserver) {
	t.Helper()
	if err := observer.ObserveOnce(f.ctx); err != nil {
		t.Fatalf("ObserveOnce: %v", err)
	}
}

func (f *recoveryFixture) assertNoRecovery(t *testing.T) {
	t.Helper()
	if _, err := f.services.AppVersionRecoveryObservations.Get(f.ctx, f.requestID); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("Get recovery error = %v, want not found", err)
	}
}

func (f *recoveryFixture) countStore(store string) int64 {
	f.t.Helper()
	count, err := f.services.DB.ObjectStore(store).Count(f.ctx, nil)
	if err != nil {
		f.t.Fatalf("Count %s: %v", store, err)
	}
	return count
}

type failOnceRecoveryRecorder struct {
	delegate RecoveryObservations
	calls    int
}

func (r *failOnceRecoveryRecorder) RecordIfCurrentFailed(
	ctx context.Context,
	observation *core.AppVersionRecoveryObservation,
) (*core.AppVersionRecoveryObservation, bool, error) {
	r.calls++
	if r.calls == 1 {
		return nil, false, errors.New("injected write failure")
	}
	return r.delegate.RecordIfCurrentFailed(ctx, observation)
}
