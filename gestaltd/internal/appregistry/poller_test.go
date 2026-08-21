package appregistry

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

type recordingAppRestarter struct {
	stopCalls      []string
	startCalls     []string
	startVersions  []string
	stopErr        error
	startErr       error
	restartable    map[string]bool
	restartableErr error
	afterStop      func()
	runningVersion string
}

func (r *recordingAppRestarter) Restartable(app string) (bool, error) {
	if r == nil {
		return false, nil
	}
	if r.restartableErr != nil {
		return false, r.restartableErr
	}
	if r.restartable != nil {
		if restartable, ok := r.restartable[app]; ok {
			return restartable, nil
		}
	}
	return true, nil
}

func (r *recordingAppRestarter) StopApp(_ context.Context, app string) error {
	r.stopCalls = append(r.stopCalls, app)
	if r.stopErr == nil && r.afterStop != nil {
		r.afterStop()
	}
	return r.stopErr
}

func (r *recordingAppRestarter) StartApp(_ context.Context, app, version string) error {
	r.startCalls = append(r.startCalls, app)
	r.startVersions = append(r.startVersions, version)
	return r.startErr
}

func (r *recordingAppRestarter) AbortRestarts() {}

func (r *recordingAppRestarter) RunningVersion(string) (string, bool) {
	return r.runningVersion, r.runningVersion != ""
}

func TestCatalogPollerNotifyCoalesces(t *testing.T) {
	t.Parallel()

	poller := NewCatalogPoller(CatalogPollerConfig{})
	poller.Notify("g-issues")
	poller.Notify("another-app")
	if got := len(poller.notify); got != 1 {
		t.Fatalf("queued notifications = %d, want 1", got)
	}
}

func TestCatalogPollerNotifyIsNonBlocking(t *testing.T) {
	t.Parallel()

	poller := NewCatalogPoller(CatalogPollerConfig{})
	poller.notify <- struct{}{}
	done := make(chan struct{})
	go func() {
		for range 1000 {
			poller.Notify("g-issues")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Notify blocked while notification buffer was full")
	}
}

func TestCatalogPollerNotifyTriggersReconcile(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	services := testutil.NewStubServices(t)
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	appendRolloutFixture(t, services, "g-issues", "v1", now)

	ready := make(chan struct{})
	restarter := &recordingAppRestarter{}
	poller := NewCatalogPoller(CatalogPollerConfig{
		ChangeRequests:      services.AppVersionChangeRequests,
		Materializations:    services.AppInstanceMaterializations,
		Rollouts:            services.AppRollouts,
		AppRestarter:        restarter,
		InstanceID:          "replica-a",
		Interval:            time.Hour,
		RestartReady:        ready,
		BootstrapReady:      ready,
		DisableRestartDelay: true,
		Now:                 func() time.Time { return now },
	})
	poller.Start(ctx)
	close(ready)

	waitForStartCalls := func(want int) {
		t.Helper()
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			if len(restarter.startCalls) >= want {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("startCalls = %d, want >= %d", len(restarter.startCalls), want)
	}
	waitForStartCalls(1)

	appendRolloutFixture(t, services, "g-issues", "v2", now.Add(time.Minute))
	poller.Notify("g-issues")
	waitForStartCalls(2)
	if got := restarter.startVersions[len(restarter.startVersions)-1]; got != "v2" {
		t.Fatalf("last started version = %q, want v2", got)
	}

	poller.Stop()
}

func TestCatalogPollerNotifyRetriesAfterReconcileFailure(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	services := testutil.NewStubServices(t)
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	appendRolloutFixture(t, services, "g-issues", "v1", now)

	ready := make(chan struct{})
	restarter := &recordingAppRestarter{startErr: errors.New("start failed")}
	poller := NewCatalogPoller(CatalogPollerConfig{
		ChangeRequests:       services.AppVersionChangeRequests,
		Materializations:     services.AppInstanceMaterializations,
		Rollouts:             services.AppRollouts,
		AppRestarter:         restarter,
		InstanceID:           "replica-a",
		Interval:             time.Hour,
		RestartReady:         ready,
		BootstrapReady:       ready,
		DisableRestartDelay:  true,
		MaxReconcileAttempts: 3,
		Now:                  func() time.Time { return now },
	})
	poller.Start(ctx)
	close(ready)

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		materialization, err := services.AppInstanceMaterializations.Get(
			context.Background(), "replica-a", "g-issues", "v1",
		)
		if err == nil && materialization.AttemptCount > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	materialization, err := services.AppInstanceMaterializations.Get(
		context.Background(), "replica-a", "g-issues", "v1",
	)
	if err != nil {
		t.Fatalf("Get materialization: %v", err)
	}
	if materialization.AttemptCount == 0 {
		t.Fatal("expected a recorded reconciliation failure before retry")
	}

	restarter.startErr = nil
	poller.Notify("g-issues")

	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		materialization, err = services.AppInstanceMaterializations.Get(
			context.Background(), "replica-a", "g-issues", "v1",
		)
		if err == nil && materialization.RestartedAt.Equal(now) {
			poller.Stop()
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	poller.Stop()
	t.Fatalf("materialization after notify = %#v, err = %v", materialization, err)
}

func TestCatalogPollerStopsRetryingAtConfiguredLimit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	services := testutil.NewStubServices(t)
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	appendRolloutFixture(t, services, "g-issues", "v1", now)
	restarter := &recordingAppRestarter{startErr: errors.New("start failed")}
	poller := NewCatalogPoller(CatalogPollerConfig{
		ChangeRequests:       services.AppVersionChangeRequests,
		Materializations:     services.AppInstanceMaterializations,
		Rollouts:             services.AppRollouts,
		AppRestarter:         restarter,
		InstanceID:           "replica-a",
		DisableRestartDelay:  true,
		MaxReconcileAttempts: 2,
		Now:                  func() time.Time { return now },
	})

	for attempt := 1; attempt <= 2; attempt++ {
		if err := poller.ReconcileOnce(ctx); err == nil {
			t.Fatalf("attempt %d unexpectedly succeeded", attempt)
		}
		row, err := services.AppInstanceMaterializations.Get(ctx, "replica-a", "g-issues", "v1")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if row.AttemptCount != attempt || row.LastErrorMessage == "" {
			t.Fatalf("attempt %d row = %#v", attempt, row)
		}
	}
	if err := poller.ReconcileOnce(ctx); err != nil {
		t.Fatalf("limited pass: %v", err)
	}
	if got := len(restarter.startCalls); got != 2 {
		t.Fatalf("start calls = %d, want 2", got)
	}
}

func TestCatalogPollerAtRetryLimitRecordsObservedConvergence(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	services := testutil.NewStubServices(t)
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	if _, err := services.AppVersionChangeRequests.AppendRequest(ctx, &core.AppVersionChangeRequest{
		App:         "g-issues",
		FromVersion: "previous",
		ToVersion:   "v1",
		Timestamp:   now,
	}); err != nil {
		t.Fatalf("AppendRequest: %v", err)
	}
	if _, err := services.AppInstanceMaterializations.Acknowledge(ctx, &core.AppInstanceMaterialization{
		InstanceID:     "replica-a",
		App:            "g-issues",
		Version:        "v1",
		AcknowledgedAt: now,
	}); err != nil {
		t.Fatalf("Acknowledge: %v", err)
	}
	if _, err := services.AppInstanceMaterializations.RecordFailure(ctx, "replica-a", "g-issues", "v1", now, "prior failure"); err != nil {
		t.Fatalf("RecordFailure: %v", err)
	}
	restarter := &recordingAppRestarter{runningVersion: "v1"}
	poller := NewCatalogPoller(CatalogPollerConfig{
		ChangeRequests:       services.AppVersionChangeRequests,
		Materializations:     services.AppInstanceMaterializations,
		Rollouts:             services.AppRollouts,
		AppRestarter:         restarter,
		InstanceID:           "replica-a",
		MaxReconcileAttempts: 1,
		Now:                  func() time.Time { return now },
	})

	if err := poller.ReconcileOnce(ctx); err != nil {
		t.Fatalf("ReconcileOnce: %v", err)
	}
	row, err := services.AppInstanceMaterializations.Get(ctx, "replica-a", "g-issues", "v1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if row.RestartedAt.IsZero() {
		t.Fatal("RestartedAt is zero")
	}
	if len(restarter.stopCalls) != 0 || len(restarter.startCalls) != 0 {
		t.Fatalf("unexpected restart calls: stop=%v start=%v", restarter.stopCalls, restarter.startCalls)
	}
}

func TestCatalogPollerRolloutEnrollmentAndCompletion(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	start := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	clock := start
	appendRolloutFixture(t, services, "g-issues", "v1", start)
	createRolloutFixture(t, services, "g-issues", "v1", start)
	restarter := &recordingAppRestarter{}
	poller := NewCatalogPoller(CatalogPollerConfig{
		ChangeRequests:      services.AppVersionChangeRequests,
		Materializations:    services.AppInstanceMaterializations,
		Rollouts:            services.AppRollouts,
		AppRestarter:        restarter,
		InstanceID:          "replica-a",
		DisableRestartDelay: true,
		Now:                 func() time.Time { return clock },
	})

	if err := poller.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("enrollment pass: %v", err)
	}
	if len(restarter.stopCalls) != 0 || len(restarter.startCalls) != 0 {
		t.Fatalf("restart calls during enrollment: stop=%v start=%v", restarter.stopCalls, restarter.startCalls)
	}
	rollout, err := services.AppRollouts.Get(context.Background(), "g-issues")
	if err != nil {
		t.Fatalf("Get enrolling rollout: %v", err)
	}
	if rollout.State != core.AppRolloutStateEnrolling {
		t.Fatalf("state = %q, want enrolling", rollout.State)
	}

	clock = start.Add(2 * time.Minute)
	if err := poller.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("restart pass: %v", err)
	}
	rollout, err = services.AppRollouts.Get(context.Background(), "g-issues")
	if err != nil {
		t.Fatalf("Get completed rollout: %v", err)
	}
	if rollout.State != core.AppRolloutStateComplete {
		t.Fatalf("state = %q, want complete", rollout.State)
	}
	if len(restarter.stopCalls) != 1 || len(restarter.startCalls) != 1 {
		t.Fatalf("restart calls: stop=%v start=%v", restarter.stopCalls, restarter.startCalls)
	}
}

func TestCatalogPollerRolloutWaitsForEveryEnrolledReplica(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	services := testutil.NewStubServices(t)
	start := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	clock := start
	appendRolloutFixture(t, services, "g-issues", "v1", start)
	createRolloutFixture(t, services, "g-issues", "v1", start)
	if _, err := services.AppInstanceMaterializations.Acknowledge(ctx, &core.AppInstanceMaterialization{
		InstanceID:     "replica-b",
		App:            "g-issues",
		Version:        "v1",
		AcknowledgedAt: start.Add(time.Minute),
	}); err != nil {
		t.Fatalf("Acknowledge replica-b: %v", err)
	}
	poller := NewCatalogPoller(CatalogPollerConfig{
		ChangeRequests:      services.AppVersionChangeRequests,
		Materializations:    services.AppInstanceMaterializations,
		Rollouts:            services.AppRollouts,
		AppRestarter:        &recordingAppRestarter{},
		InstanceID:          "replica-a",
		DisableRestartDelay: true,
		Now:                 func() time.Time { return clock },
	})
	if err := poller.ReconcileOnce(ctx); err != nil {
		t.Fatalf("enrollment pass: %v", err)
	}
	clock = start.Add(2 * time.Minute)
	if err := poller.ReconcileOnce(ctx); err != nil {
		t.Fatalf("replica-a restart pass: %v", err)
	}
	rollout, err := services.AppRollouts.Get(ctx, "g-issues")
	if err != nil {
		t.Fatalf("Get restarting rollout: %v", err)
	}
	if rollout.State != core.AppRolloutStateRestarting {
		t.Fatalf("state = %q, want restarting", rollout.State)
	}
	if _, err := services.AppInstanceMaterializations.MarkRestarted(ctx, "replica-b", "g-issues", "v1", clock.Add(time.Second)); err != nil {
		t.Fatalf("MarkRestarted replica-b: %v", err)
	}
	clock = clock.Add(2 * time.Second)
	if err := poller.ReconcileOnce(ctx); err != nil {
		t.Fatalf("completion pass: %v", err)
	}
	rollout, err = services.AppRollouts.Get(ctx, "g-issues")
	if err != nil {
		t.Fatalf("Get completed rollout: %v", err)
	}
	if rollout.State != core.AppRolloutStateComplete {
		t.Fatalf("state = %q, want complete", rollout.State)
	}
}

func TestCatalogPollerRolloutOnlyWaitsForTargetSourceVersion(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	services := testutil.NewStubServices(t)
	start := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	clock := start
	appendRolloutFixture(t, services, "g-issues", "v1", start)
	if _, err := services.AppRollouts.Create(ctx, &core.AppRollout{
		App:                 "g-issues",
		Version:             "v1",
		State:               core.AppRolloutStateEnrolling,
		TargetSourceVersion: "source-new",
		CreatedAt:           start,
		EnrollmentEndsAt:    start.Add(2 * time.Minute),
		Deadline:            start.Add(15 * time.Minute),
	}); err != nil {
		t.Fatalf("Create rollout: %v", err)
	}
	for replica := 0; replica < 5; replica++ {
		if _, err := services.AppInstanceMaterializations.Acknowledge(ctx, &core.AppInstanceMaterialization{
			InstanceID:     "old-replica-" + strconv.Itoa(replica),
			SourceVersion:  "source-old",
			App:            "g-issues",
			Version:        "v1",
			AcknowledgedAt: start.Add(time.Minute),
		}); err != nil {
			t.Fatalf("Acknowledge old replica %d: %v", replica, err)
		}
	}
	pollers := make([]*CatalogPoller, 0, 5)
	for replica := 0; replica < 5; replica++ {
		poller := NewCatalogPoller(CatalogPollerConfig{
			ChangeRequests:      services.AppVersionChangeRequests,
			Materializations:    services.AppInstanceMaterializations,
			Rollouts:            services.AppRollouts,
			AppRestarter:        &recordingAppRestarter{},
			InstanceID:          "new-replica-" + strconv.Itoa(replica),
			SourceVersion:       "source-new",
			DisableRestartDelay: true,
			Now:                 func() time.Time { return clock },
		})
		pollers = append(pollers, poller)
		if err := poller.ReconcileOnce(ctx); err != nil {
			t.Fatalf("enrollment pass %d: %v", replica, err)
		}
	}

	clock = start.Add(2 * time.Minute)
	for replica, poller := range pollers {
		if err := poller.ReconcileOnce(ctx); err != nil {
			t.Fatalf("restart pass %d: %v", replica, err)
		}
	}

	rollout, err := services.AppRollouts.Get(ctx, "g-issues")
	if err != nil {
		t.Fatalf("Get rollout: %v", err)
	}
	if rollout.State != core.AppRolloutStateComplete {
		t.Fatalf("state = %q, want complete", rollout.State)
	}
	for replica := 0; replica < 5; replica++ {
		oldReplica, err := services.AppInstanceMaterializations.Get(ctx, "old-replica-"+strconv.Itoa(replica), "g-issues", "v1")
		if err != nil {
			t.Fatalf("Get old replica %d: %v", replica, err)
		}
		if !oldReplica.RestartedAt.IsZero() {
			t.Fatalf("old replica %d restarted_at = %v, want zero", replica, oldReplica.RestartedAt)
		}
	}
}

func TestCatalogPollerNonTargetSourceWaitsDuringEnrollment(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	services := testutil.NewStubServices(t)
	start := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	appendRolloutFixture(t, services, "g-issues", "v1", start)
	if _, err := services.AppRollouts.Create(ctx, &core.AppRollout{
		App:                 "g-issues",
		Version:             "v1",
		State:               core.AppRolloutStateEnrolling,
		TargetSourceVersion: "source-new",
		CreatedAt:           start,
		EnrollmentEndsAt:    start.Add(2 * time.Minute),
		Deadline:            start.Add(15 * time.Minute),
	}); err != nil {
		t.Fatalf("Create rollout: %v", err)
	}
	restarter := &recordingAppRestarter{}
	poller := NewCatalogPoller(CatalogPollerConfig{
		ChangeRequests:      services.AppVersionChangeRequests,
		Materializations:    services.AppInstanceMaterializations,
		Rollouts:            services.AppRollouts,
		AppRestarter:        restarter,
		InstanceID:          "old-replica",
		SourceVersion:       "source-old",
		DisableRestartDelay: true,
		Now:                 func() time.Time { return start.Add(time.Minute) },
	})
	if err := poller.ReconcileOnce(ctx); err != nil {
		t.Fatalf("ReconcileOnce: %v", err)
	}
	if len(restarter.stopCalls) != 0 || len(restarter.startCalls) != 0 {
		t.Fatalf("non-target restarted during enrollment: stop=%v start=%v", restarter.stopCalls, restarter.startCalls)
	}
}

func TestCatalogPollerCompletedOldSourceCannotCompleteTargetRollout(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	services := testutil.NewStubServices(t)
	start := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	if _, err := services.AppRollouts.Create(ctx, &core.AppRollout{
		App:                 "g-issues",
		Version:             "v1",
		State:               core.AppRolloutStateEnrolling,
		TargetSourceVersion: "source-new",
		CreatedAt:           start,
		EnrollmentEndsAt:    start.Add(2 * time.Minute),
		Deadline:            start.Add(15 * time.Minute),
	}); err != nil {
		t.Fatalf("Create rollout: %v", err)
	}
	if _, err := services.AppInstanceMaterializations.Acknowledge(ctx, &core.AppInstanceMaterialization{
		InstanceID:     "old-replica",
		SourceVersion:  "source-old",
		App:            "g-issues",
		Version:        "v1",
		AcknowledgedAt: start.Add(time.Minute),
	}); err != nil {
		t.Fatalf("Acknowledge old replica: %v", err)
	}
	if _, err := services.AppInstanceMaterializations.MarkRestarted(ctx, "old-replica", "g-issues", "v1", start.Add(3*time.Minute)); err != nil {
		t.Fatalf("MarkRestarted old replica: %v", err)
	}
	rollout, err := services.AppRollouts.MarkRestarting(ctx, "g-issues", "v1")
	if err != nil {
		t.Fatalf("MarkRestarting: %v", err)
	}
	var terminalApp string
	poller := NewCatalogPoller(CatalogPollerConfig{
		Materializations:  services.AppInstanceMaterializations,
		Rollouts:          services.AppRollouts,
		Now:               func() time.Time { return start.Add(15 * time.Minute) },
		OnRolloutTerminal: func(app string) { terminalApp = app },
	})
	terminal, err := poller.updateRolloutOutcome(ctx, rollout)
	if err != nil {
		t.Fatalf("updateRolloutOutcome: %v", err)
	}
	if !terminal {
		t.Fatal("updateRolloutOutcome terminal = false, want true")
	}
	rollout, err = services.AppRollouts.Get(ctx, "g-issues")
	if err != nil {
		t.Fatalf("Get rollout: %v", err)
	}
	if rollout.State != core.AppRolloutStateFailed {
		t.Fatalf("state = %q, want failed", rollout.State)
	}
	if terminalApp != "g-issues" {
		t.Fatalf("terminal callback app = %q, want g-issues", terminalApp)
	}
}

func TestCatalogPollerRolloutFailsWhenEnrolledReplicaMissesDeadline(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	services := testutil.NewStubServices(t)
	start := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	clock := start
	appendRolloutFixture(t, services, "g-issues", "v1", start)
	createRolloutFixture(t, services, "g-issues", "v1", start)
	if _, err := services.AppInstanceMaterializations.Acknowledge(ctx, &core.AppInstanceMaterialization{
		InstanceID:     "replica-b",
		App:            "g-issues",
		Version:        "v1",
		AcknowledgedAt: start.Add(time.Minute),
	}); err != nil {
		t.Fatalf("Acknowledge replica-b: %v", err)
	}
	poller := NewCatalogPoller(CatalogPollerConfig{
		ChangeRequests:      services.AppVersionChangeRequests,
		Materializations:    services.AppInstanceMaterializations,
		Rollouts:            services.AppRollouts,
		AppRestarter:        &recordingAppRestarter{},
		InstanceID:          "replica-a",
		DisableRestartDelay: true,
		Now:                 func() time.Time { return clock },
	})
	if err := poller.ReconcileOnce(ctx); err != nil {
		t.Fatalf("enrollment pass: %v", err)
	}
	clock = start.Add(15 * time.Minute)
	if err := poller.ReconcileOnce(ctx); err != nil {
		t.Fatalf("deadline pass: %v", err)
	}
	rollout, err := services.AppRollouts.Get(ctx, "g-issues")
	if err != nil {
		t.Fatalf("Get failed rollout: %v", err)
	}
	if rollout.State != core.AppRolloutStateFailed {
		t.Fatalf("state = %q, want failed", rollout.State)
	}
}

func TestCatalogPollerLateReplicaConvergesWithoutReopeningRollout(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	services := testutil.NewStubServices(t)
	start := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	clock := start
	appendRolloutFixture(t, services, "g-issues", "v1", start)
	createRolloutFixture(t, services, "g-issues", "v1", start)
	firstRestarter := &recordingAppRestarter{}
	first := NewCatalogPoller(CatalogPollerConfig{
		ChangeRequests:      services.AppVersionChangeRequests,
		Materializations:    services.AppInstanceMaterializations,
		Rollouts:            services.AppRollouts,
		AppRestarter:        firstRestarter,
		InstanceID:          "replica-a",
		DisableRestartDelay: true,
		Now:                 func() time.Time { return clock },
	})
	if err := first.ReconcileOnce(ctx); err != nil {
		t.Fatalf("enrollment pass: %v", err)
	}
	clock = start.Add(2 * time.Minute)
	if err := first.ReconcileOnce(ctx); err != nil {
		t.Fatalf("completion pass: %v", err)
	}

	lateRestarter := &recordingAppRestarter{}
	late := NewCatalogPoller(CatalogPollerConfig{
		ChangeRequests:      services.AppVersionChangeRequests,
		Materializations:    services.AppInstanceMaterializations,
		Rollouts:            services.AppRollouts,
		AppRestarter:        lateRestarter,
		InstanceID:          "replica-b",
		DisableRestartDelay: true,
		Now:                 func() time.Time { return clock },
	})
	if err := late.ReconcileOnce(ctx); err != nil {
		t.Fatalf("late replica pass: %v", err)
	}
	if len(lateRestarter.stopCalls) != 1 || len(lateRestarter.startCalls) != 1 {
		t.Fatalf("late restart calls: stop=%v start=%v", lateRestarter.stopCalls, lateRestarter.startCalls)
	}
	rollout, err := services.AppRollouts.Get(ctx, "g-issues")
	if err != nil {
		t.Fatalf("Get rollout: %v", err)
	}
	if rollout.State != core.AppRolloutStateComplete {
		t.Fatalf("state = %q, want complete", rollout.State)
	}
}

func appendRolloutFixture(t *testing.T, services *coredata.Services, app, version string, at time.Time) {
	t.Helper()
	if _, err := services.AppVersionChangeRequests.AppendRequest(context.Background(), &core.AppVersionChangeRequest{
		App:         app,
		FromVersion: "previous",
		ToVersion:   version,
		Timestamp:   at,
	}); err != nil {
		t.Fatalf("AppendRequest: %v", err)
	}
}

func createRolloutFixture(t *testing.T, services *coredata.Services, app, version string, at time.Time) {
	t.Helper()
	if _, err := services.AppRollouts.Create(context.Background(), &core.AppRollout{
		App:              app,
		Version:          version,
		State:            core.AppRolloutStateEnrolling,
		CreatedAt:        at,
		EnrollmentEndsAt: at.Add(2 * time.Minute),
		Deadline:         at.Add(15 * time.Minute),
	}); err != nil {
		t.Fatalf("Create rollout: %v", err)
	}
}

func TestCatalogPollerReconcileOnceAcknowledgesAndRestarts(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	clock := now

	if _, err := services.AppVersionChangeRequests.AppendRequest(context.Background(), &core.AppVersionChangeRequest{
		App:         "g-issues",
		FromVersion: "0.0.0-snapshot.gdeadbeef",
		ToVersion:   "0.0.0-snapshot.gabc123",
		Timestamp:   now,
	}); err != nil {
		t.Fatalf("AppendRequest: %v", err)
	}

	restarter := &recordingAppRestarter{}
	poller := NewCatalogPoller(CatalogPollerConfig{
		ChangeRequests:      services.AppVersionChangeRequests,
		Materializations:    services.AppInstanceMaterializations,
		Rollouts:            services.AppRollouts,
		AppRestarter:        restarter,
		InstanceID:          "replica-a",
		DisableRestartDelay: true,
		Now:                 func() time.Time { return clock },
	})

	if err := poller.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("first ReconcileOnce: %v", err)
	}
	if got := restarter.stopCalls; len(got) != 1 || got[0] != "g-issues" {
		t.Fatalf("stopCalls after first pass = %#v, want [g-issues]", got)
	}
	if got := restarter.startCalls; len(got) != 1 || got[0] != "g-issues" {
		t.Fatalf("startCalls after first pass = %#v, want [g-issues]", got)
	}

	materialization, err := services.AppInstanceMaterializations.Get(context.Background(), "replica-a", "g-issues", "0.0.0-snapshot.gabc123")
	if err != nil {
		t.Fatalf("Get materialization: %v", err)
	}
	if materialization.AcknowledgedAt != now {
		t.Fatalf("AcknowledgedAt = %v, want %v", materialization.AcknowledgedAt, now)
	}
	if materialization.StoppedAt != now {
		t.Fatalf("StoppedAt = %v, want %v", materialization.StoppedAt, now)
	}
	if materialization.RestartedAt != now {
		t.Fatalf("RestartedAt = %v, want %v", materialization.RestartedAt, now)
	}

	if err := poller.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("second ReconcileOnce: %v", err)
	}
	if got := len(restarter.stopCalls); got != 1 {
		t.Fatalf("stopCalls after second pass = %d, want 1", got)
	}
	if got := len(restarter.startCalls); got != 1 {
		t.Fatalf("startCalls after second pass = %d, want 1", got)
	}
}

func TestCatalogPollerReconcileOnceWaitsForRestartDelay(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	start := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	clock := start

	if _, err := services.AppVersionChangeRequests.AppendRequest(context.Background(), &core.AppVersionChangeRequest{
		App:         "g-issues",
		FromVersion: "0.0.0-snapshot.gdeadbeef",
		ToVersion:   "0.0.0-snapshot.gabc123",
		Timestamp:   start,
	}); err != nil {
		t.Fatalf("AppendRequest: %v", err)
	}

	restarter := &recordingAppRestarter{}
	poller := NewCatalogPoller(CatalogPollerConfig{
		ChangeRequests:   services.AppVersionChangeRequests,
		Materializations: services.AppInstanceMaterializations,
		Rollouts:         services.AppRollouts,
		AppRestarter:     restarter,
		InstanceID:       "replica-a",
		Now:              func() time.Time { return clock },
	})

	if err := poller.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("first ReconcileOnce: %v", err)
	}
	if got := restarter.stopCalls; len(got) != 1 || got[0] != "g-issues" {
		t.Fatalf("stopCalls after first pass = %#v, want [g-issues]", got)
	}
	if got := restarter.startCalls; len(got) != 0 {
		t.Fatalf("startCalls after first pass = %#v, want none", got)
	}

	clock = start.Add(30 * time.Second)
	if err := poller.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("second ReconcileOnce: %v", err)
	}
	if got := len(restarter.startCalls); got != 0 {
		t.Fatalf("startCalls after partial delay = %d, want 0", got)
	}

	clock = start.Add(time.Minute)
	if err := poller.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("third ReconcileOnce: %v", err)
	}
	if got := restarter.startCalls; len(got) != 1 || got[0] != "g-issues" {
		t.Fatalf("startCalls after delay elapsed = %#v, want [g-issues]", got)
	}
}

func TestCatalogPollerReconcileOncePreservesDelayAfterStoppedAtWriteFailure(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	db, ok := services.DB.(*coretesting.StubIndexedDB)
	if !ok {
		t.Fatalf("DB type = %T", services.DB)
	}
	start := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	clock := start
	if _, err := services.AppVersionChangeRequests.AppendRequest(context.Background(), &core.AppVersionChangeRequest{
		App:         "g-issues",
		FromVersion: "previous",
		ToVersion:   "v1",
		Timestamp:   start,
	}); err != nil {
		t.Fatalf("AppendRequest: %v", err)
	}
	restarter := &recordingAppRestarter{
		afterStop: func() { db.Err = errors.New("stopped_at write failed") },
	}
	poller := NewCatalogPoller(CatalogPollerConfig{
		ChangeRequests:   services.AppVersionChangeRequests,
		Materializations: services.AppInstanceMaterializations,
		Rollouts:         services.AppRollouts,
		AppRestarter:     restarter,
		InstanceID:       "replica-a",
		RestartDelay:     time.Minute,
		Now:              func() time.Time { return clock },
	})

	if err := poller.ReconcileOnce(context.Background()); err == nil || !strings.Contains(err.Error(), "stopped_at write failed") {
		t.Fatalf("first ReconcileOnce error = %v", err)
	}
	db.Err = nil
	clock = start.Add(30 * time.Second)
	if err := poller.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("retry ReconcileOnce: %v", err)
	}
	if len(restarter.stopCalls) != 1 {
		t.Fatalf("stopCalls = %v, want one stop", restarter.stopCalls)
	}
	if len(restarter.startCalls) != 0 {
		t.Fatalf("startCalls = %v, want none before original delay", restarter.startCalls)
	}
	materialization, err := services.AppInstanceMaterializations.Get(context.Background(), "replica-a", "g-issues", "v1")
	if err != nil {
		t.Fatalf("Get materialization: %v", err)
	}
	if materialization.StoppedAt != start {
		t.Fatalf("StoppedAt = %v, want %v", materialization.StoppedAt, start)
	}
	clock = start.Add(time.Minute)
	if err := poller.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("restart ReconcileOnce: %v", err)
	}
	if len(restarter.startCalls) != 1 {
		t.Fatalf("startCalls = %v, want one start", restarter.startCalls)
	}
}

func TestCatalogPollerReconcileOncePropagatesRestartErrors(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)

	if _, err := services.AppVersionChangeRequests.AppendRequest(context.Background(), &core.AppVersionChangeRequest{
		App:         "g-issues",
		FromVersion: "0.0.0-snapshot.gdeadbeef",
		ToVersion:   "0.0.0-snapshot.gabc123",
		Timestamp:   now,
	}); err != nil {
		t.Fatalf("AppendRequest: %v", err)
	}

	restarter := &recordingAppRestarter{startErr: errors.New("start failed")}
	poller := NewCatalogPoller(CatalogPollerConfig{
		ChangeRequests:      services.AppVersionChangeRequests,
		Materializations:    services.AppInstanceMaterializations,
		Rollouts:            services.AppRollouts,
		AppRestarter:        restarter,
		InstanceID:          "replica-a",
		DisableRestartDelay: true,
		Now:                 func() time.Time { return now },
	})

	err := poller.ReconcileOnce(context.Background())
	if err == nil || err.Error() != `start app g-issues for g-issues@0.0.0-snapshot.gabc123: start failed` {
		t.Fatalf("ReconcileOnce error = %v, want start failure", err)
	}

	materialization, getErr := services.AppInstanceMaterializations.Get(context.Background(), "replica-a", "g-issues", "0.0.0-snapshot.gabc123")
	if getErr != nil {
		t.Fatalf("Get materialization: %v", getErr)
	}
	if materialization.StoppedAt.IsZero() {
		t.Fatal("expected stopped_at to be recorded before start failure")
	}
	if !materialization.RestartedAt.IsZero() {
		t.Fatal("expected restarted_at to remain unset after start failure")
	}
}

func TestCatalogPollerReconcileOnceRestartsOnceForMultipleVersions(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)

	requests := []struct {
		from    string
		to      string
		updated time.Time
	}{
		{"0.0.0-snapshot.g111111", "0.0.0-snapshot.g222222", now},
		{"0.0.0-snapshot.g222222", "0.0.0-snapshot.g333333", now.Add(time.Minute)},
	}
	for _, req := range requests {
		if _, err := services.AppVersionChangeRequests.AppendRequest(context.Background(), &core.AppVersionChangeRequest{
			App:         "g-issues",
			FromVersion: req.from,
			ToVersion:   req.to,
			Timestamp:   req.updated,
		}); err != nil {
			t.Fatalf("AppendRequest(%s): %v", req.to, err)
		}
	}

	restarter := &recordingAppRestarter{}
	poller := NewCatalogPoller(CatalogPollerConfig{
		ChangeRequests:      services.AppVersionChangeRequests,
		Materializations:    services.AppInstanceMaterializations,
		Rollouts:            services.AppRollouts,
		AppRestarter:        restarter,
		InstanceID:          "replica-a",
		DisableRestartDelay: true,
		Now:                 func() time.Time { return now },
	})

	if err := poller.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("ReconcileOnce: %v", err)
	}
	if got := len(restarter.stopCalls); got != 1 {
		t.Fatalf("stopCalls = %d, want 1", got)
	}
	if got := len(restarter.startCalls); got != 1 {
		t.Fatalf("startCalls = %d, want 1", got)
	}

	for _, version := range []string{"0.0.0-snapshot.g222222", "0.0.0-snapshot.g333333"} {
		materialization, err := services.AppInstanceMaterializations.Get(context.Background(), "replica-a", "g-issues", version)
		if err != nil {
			t.Fatalf("Get materialization for %s: %v", version, err)
		}
		if materialization.RestartedAt != now {
			t.Fatalf("RestartedAt for %s = %v, want %v", version, materialization.RestartedAt, now)
		}
	}
}

func TestCatalogPollerReconcileOnceRetriesStartAfterRecordedStop(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)

	if _, err := services.AppVersionChangeRequests.AppendRequest(context.Background(), &core.AppVersionChangeRequest{
		App:         "g-issues",
		FromVersion: "0.0.0-snapshot.gdeadbeef",
		ToVersion:   "0.0.0-snapshot.gabc123",
		Timestamp:   now,
	}); err != nil {
		t.Fatalf("AppendRequest: %v", err)
	}

	if _, err := services.AppInstanceMaterializations.Acknowledge(context.Background(), &core.AppInstanceMaterialization{
		InstanceID:     "replica-a",
		App:            "g-issues",
		Version:        "0.0.0-snapshot.gabc123",
		AcknowledgedAt: now,
	}); err != nil {
		t.Fatalf("Acknowledge: %v", err)
	}
	if _, err := services.AppInstanceMaterializations.MarkStopped(context.Background(), "replica-a", "g-issues", "0.0.0-snapshot.gabc123", now); err != nil {
		t.Fatalf("MarkStopped: %v", err)
	}
	restarter := &recordingAppRestarter{}
	poller := NewCatalogPoller(CatalogPollerConfig{
		ChangeRequests:      services.AppVersionChangeRequests,
		Materializations:    services.AppInstanceMaterializations,
		Rollouts:            services.AppRollouts,
		AppRestarter:        restarter,
		InstanceID:          "replica-a",
		DisableRestartDelay: true,
		Now:                 func() time.Time { return now },
	})

	if err := poller.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("ReconcileOnce: %v", err)
	}
	if got := len(restarter.stopCalls); got != 0 {
		t.Fatalf("stopCalls = %d, want 0", got)
	}
	if got := len(restarter.startCalls); got != 1 || restarter.startCalls[0] != "g-issues" {
		t.Fatalf("startCalls = %#v, want [g-issues]", restarter.startCalls)
	}

	materialization, err := services.AppInstanceMaterializations.Get(context.Background(), "replica-a", "g-issues", "0.0.0-snapshot.gabc123")
	if err != nil {
		t.Fatalf("Get materialization: %v", err)
	}
	if materialization.RestartedAt != now {
		t.Fatalf("RestartedAt = %v, want %v", materialization.RestartedAt, now)
	}
}
func TestCatalogPollerReconcileOnceDoesNotResetRestartDelayForNewVersion(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	start := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	clock := start

	if _, err := services.AppVersionChangeRequests.AppendRequest(context.Background(), &core.AppVersionChangeRequest{
		App:         "g-issues",
		FromVersion: "0.0.0-snapshot.g111111",
		ToVersion:   "0.0.0-snapshot.g222222",
		Timestamp:   start,
	}); err != nil {
		t.Fatalf("AppendRequest v1: %v", err)
	}

	restarter := &recordingAppRestarter{}
	poller := NewCatalogPoller(CatalogPollerConfig{
		ChangeRequests:   services.AppVersionChangeRequests,
		Materializations: services.AppInstanceMaterializations,
		Rollouts:         services.AppRollouts,
		AppRestarter:     restarter,
		InstanceID:       "replica-a",
		RestartDelay:     time.Minute,
		Now:              func() time.Time { return clock },
	})

	if err := poller.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("first ReconcileOnce: %v", err)
	}
	if got := len(restarter.stopCalls); got != 1 {
		t.Fatalf("stopCalls after first pass = %d, want 1", got)
	}

	if _, err := services.AppVersionChangeRequests.AppendRequest(context.Background(), &core.AppVersionChangeRequest{
		App:         "g-issues",
		FromVersion: "0.0.0-snapshot.g222222",
		ToVersion:   "0.0.0-snapshot.g333333",
		Timestamp:   start.Add(30 * time.Second),
	}); err != nil {
		t.Fatalf("AppendRequest v2: %v", err)
	}

	clock = start.Add(45 * time.Second)
	if err := poller.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("second ReconcileOnce: %v", err)
	}
	if got := len(restarter.stopCalls); got != 1 {
		t.Fatalf("stopCalls after new version = %d, want 1", got)
	}
	if got := len(restarter.startCalls); got != 0 {
		t.Fatalf("startCalls after new version = %d, want 0", got)
	}

	clock = start.Add(time.Minute)
	if err := poller.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("third ReconcileOnce: %v", err)
	}
	if got := len(restarter.startCalls); got != 1 {
		t.Fatalf("startCalls after delay elapsed = %d, want 1", got)
	}

	for _, version := range []string{"0.0.0-snapshot.g222222", "0.0.0-snapshot.g333333"} {
		materialization, err := services.AppInstanceMaterializations.Get(context.Background(), "replica-a", "g-issues", version)
		if err != nil {
			t.Fatalf("Get materialization for %s: %v", version, err)
		}
		if materialization.StoppedAt != start {
			t.Fatalf("StoppedAt for %s = %v, want %v", version, materialization.StoppedAt, start)
		}
	}
}

func TestCatalogPollerReconcileOnceDefersRestartUntilProvidersReady(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	restartReady := make(chan struct{})

	if _, err := services.AppVersionChangeRequests.AppendRequest(context.Background(), &core.AppVersionChangeRequest{
		App:         "g-issues",
		FromVersion: "0.0.0-snapshot.gdeadbeef",
		ToVersion:   "0.0.0-snapshot.gabc123",
		Timestamp:   now,
	}); err != nil {
		t.Fatalf("AppendRequest: %v", err)
	}

	restarter := &recordingAppRestarter{}
	poller := NewCatalogPoller(CatalogPollerConfig{
		ChangeRequests:      services.AppVersionChangeRequests,
		Materializations:    services.AppInstanceMaterializations,
		Rollouts:            services.AppRollouts,
		AppRestarter:        restarter,
		InstanceID:          "replica-a",
		DisableRestartDelay: true,
		RestartReady:        restartReady,
		Now:                 func() time.Time { return now },
	})

	if err := poller.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("ReconcileOnce before providers ready: %v", err)
	}
	if got := len(restarter.stopCalls); got != 0 {
		t.Fatalf("stopCalls before providers ready = %d, want 0", got)
	}
	if got := len(restarter.startCalls); got != 0 {
		t.Fatalf("startCalls before providers ready = %d, want 0", got)
	}

	materialization, err := services.AppInstanceMaterializations.Get(context.Background(), "replica-a", "g-issues", "0.0.0-snapshot.gabc123")
	if err != nil {
		t.Fatalf("Get materialization: %v", err)
	}
	if materialization.AcknowledgedAt != now {
		t.Fatalf("AcknowledgedAt = %v, want %v", materialization.AcknowledgedAt, now)
	}
	if !materialization.RestartedAt.IsZero() {
		t.Fatalf("RestartedAt = %v, want zero before providers ready", materialization.RestartedAt)
	}

	close(restartReady)
	if err := poller.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("ReconcileOnce after providers ready: %v", err)
	}
	if got := len(restarter.stopCalls); got != 1 || restarter.stopCalls[0] != "g-issues" {
		t.Fatalf("stopCalls after providers ready = %#v, want [g-issues]", restarter.stopCalls)
	}
	if got := len(restarter.startCalls); got != 1 || restarter.startCalls[0] != "g-issues" {
		t.Fatalf("startCalls after providers ready = %#v, want [g-issues]", restarter.startCalls)
	}
}

func TestCatalogPollerReconcileOnceDoesNotConvergeWithoutAppRestarter(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)

	if _, err := services.AppVersionChangeRequests.AppendRequest(context.Background(), &core.AppVersionChangeRequest{
		App:         "g-issues",
		FromVersion: "0.0.0-snapshot.gdeadbeef",
		ToVersion:   "0.0.0-snapshot.gabc123",
		Timestamp:   now,
	}); err != nil {
		t.Fatalf("AppendRequest: %v", err)
	}

	poller := NewCatalogPoller(CatalogPollerConfig{
		ChangeRequests:      services.AppVersionChangeRequests,
		Materializations:    services.AppInstanceMaterializations,
		Rollouts:            services.AppRollouts,
		InstanceID:          "replica-a",
		DisableRestartDelay: true,
		Now:                 func() time.Time { return now },
	})

	if err := poller.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("ReconcileOnce: %v", err)
	}

	materialization, err := services.AppInstanceMaterializations.Get(context.Background(), "replica-a", "g-issues", "0.0.0-snapshot.gabc123")
	if err != nil {
		t.Fatalf("Get materialization: %v", err)
	}
	if materialization.AcknowledgedAt != now {
		t.Fatalf("AcknowledgedAt = %v, want %v", materialization.AcknowledgedAt, now)
	}
	if !materialization.RestartedAt.IsZero() {
		t.Fatalf("RestartedAt = %v, want zero without AppRestarter", materialization.RestartedAt)
	}
}

func TestCatalogPollerReconcileOnceMarksNonLocalAppsConverged(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)

	if _, err := services.AppVersionChangeRequests.AppendRequest(context.Background(), &core.AppVersionChangeRequest{
		App:         "remote-app",
		FromVersion: "1.0.0",
		ToVersion:   "1.0.1",
		Timestamp:   now,
	}); err != nil {
		t.Fatalf("AppendRequest: %v", err)
	}

	restarter := &recordingAppRestarter{
		restartable: map[string]bool{"remote-app": false},
	}
	poller := NewCatalogPoller(CatalogPollerConfig{
		ChangeRequests:   services.AppVersionChangeRequests,
		Materializations: services.AppInstanceMaterializations,
		Rollouts:         services.AppRollouts,
		AppRestarter:     restarter,
		InstanceID:       "replica-a",
		RestartDelay:     time.Minute,
		Now:              func() time.Time { return now },
	})

	if err := poller.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("ReconcileOnce: %v", err)
	}
	if got := len(restarter.stopCalls); got != 0 {
		t.Fatalf("stopCalls = %d, want 0", got)
	}
	if got := len(restarter.startCalls); got != 0 {
		t.Fatalf("startCalls = %d, want 0", got)
	}

	materialization, err := services.AppInstanceMaterializations.Get(context.Background(), "replica-a", "remote-app", "1.0.1")
	if err != nil {
		t.Fatalf("Get materialization: %v", err)
	}
	if materialization.RestartedAt != now {
		t.Fatalf("RestartedAt = %v, want %v", materialization.RestartedAt, now)
	}
}

func TestCatalogPollerReconcileOnceDoesNotConvergeWhenRestartModeFails(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	if _, err := services.AppVersionChangeRequests.AppendRequest(context.Background(), &core.AppVersionChangeRequest{
		App:         "unknown-app",
		FromVersion: "0.9.0",
		ToVersion:   "1.0.0",
		Timestamp:   now,
	}); err != nil {
		t.Fatalf("AppendRequest: %v", err)
	}

	restarter := &recordingAppRestarter{restartableErr: errors.New("app is not configured")}
	poller := NewCatalogPoller(CatalogPollerConfig{
		ChangeRequests:      services.AppVersionChangeRequests,
		Materializations:    services.AppInstanceMaterializations,
		Rollouts:            services.AppRollouts,
		AppRestarter:        restarter,
		InstanceID:          "replica-a",
		DisableRestartDelay: true,
		Now:                 func() time.Time { return now },
	})

	err := poller.ReconcileOnce(context.Background())
	if err == nil || err.Error() != "determine restart mode for app unknown-app: app is not configured" {
		t.Fatalf("ReconcileOnce error = %v, want restart mode failure", err)
	}
	materialization, getErr := services.AppInstanceMaterializations.Get(context.Background(), "replica-a", "unknown-app", "1.0.0")
	if getErr != nil {
		t.Fatalf("Get materialization: %v", getErr)
	}
	if !materialization.RestartedAt.IsZero() {
		t.Fatalf("RestartedAt = %v, want zero", materialization.RestartedAt)
	}
}

func TestCatalogPollerReconcileOnceRestartsRevertedVersionWithStaleProgress(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	services := testutil.NewStubServices(t)
	start := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	appendRolloutFixture(t, services, "g-issues", "v1", start)
	if _, err := services.AppInstanceMaterializations.Acknowledge(ctx, &core.AppInstanceMaterialization{
		InstanceID:     "replica-a",
		App:            "g-issues",
		Version:        "v1",
		AcknowledgedAt: start,
	}); err != nil {
		t.Fatalf("Acknowledge v1: %v", err)
	}
	if _, err := services.AppInstanceMaterializations.MarkRestarted(ctx, "replica-a", "g-issues", "v1", start.Add(time.Minute)); err != nil {
		t.Fatalf("MarkRestarted v1: %v", err)
	}

	upgradeAt := start.Add(24 * time.Hour)
	appendRolloutFixture(t, services, "g-issues", "v2", upgradeAt)
	if _, err := services.AppInstanceMaterializations.Acknowledge(ctx, &core.AppInstanceMaterialization{
		InstanceID:     "replica-a",
		App:            "g-issues",
		Version:        "v2",
		AcknowledgedAt: upgradeAt,
	}); err != nil {
		t.Fatalf("Acknowledge v2: %v", err)
	}
	if _, err := services.AppInstanceMaterializations.MarkRestarted(ctx, "replica-a", "g-issues", "v2", upgradeAt.Add(time.Minute)); err != nil {
		t.Fatalf("MarkRestarted v2: %v", err)
	}

	revertAt := upgradeAt.Add(24 * time.Hour)
	appendRolloutFixture(t, services, "g-issues", "v1", revertAt)

	restarter := &recordingAppRestarter{runningVersion: "v2"}
	poller := NewCatalogPoller(CatalogPollerConfig{
		ChangeRequests:      services.AppVersionChangeRequests,
		Materializations:    services.AppInstanceMaterializations,
		Rollouts:            services.AppRollouts,
		AppRestarter:        restarter,
		InstanceID:          "replica-a",
		DisableRestartDelay: true,
		Now:                 func() time.Time { return revertAt.Add(5 * time.Minute) },
	})

	if err := poller.ReconcileOnce(ctx); err != nil {
		t.Fatalf("ReconcileOnce: %v", err)
	}
	if len(restarter.stopCalls) != 1 || len(restarter.startCalls) != 1 {
		t.Fatalf("restart calls: stop=%v start=%v", restarter.stopCalls, restarter.startCalls)
	}
	if got := restarter.startVersions; len(got) != 1 || got[0] != "v1" {
		t.Fatalf("startVersions = %#v, want [v1]", got)
	}
	materialization, err := services.AppInstanceMaterializations.Get(ctx, "replica-a", "g-issues", "v1")
	if err != nil {
		t.Fatalf("Get v1 materialization: %v", err)
	}
	if materialization.RestartedAt.Before(revertAt) {
		t.Fatalf("RestartedAt = %v, want current revert progress after %v", materialization.RestartedAt, revertAt)
	}
}

func TestCatalogPollerReconcileOnceReportsEveryFailingApp(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	for _, app := range []string{"app-a", "app-b"} {
		if _, err := services.AppVersionChangeRequests.AppendRequest(context.Background(), &core.AppVersionChangeRequest{
			App:         app,
			FromVersion: "0.9.0",
			ToVersion:   "1.0.0",
			Timestamp:   now,
		}); err != nil {
			t.Fatalf("AppendRequest(%s): %v", app, err)
		}
	}
	poller := NewCatalogPoller(CatalogPollerConfig{
		ChangeRequests:   services.AppVersionChangeRequests,
		Materializations: services.AppInstanceMaterializations,
		Rollouts:         services.AppRollouts,
		AppRestarter:     &recordingAppRestarter{restartableErr: errors.New("restart mode failed")},
		InstanceID:       "replica-a",
		Now:              func() time.Time { return now },
	})

	err := poller.ReconcileOnce(context.Background())
	if err == nil || !strings.Contains(err.Error(), "app-a") || !strings.Contains(err.Error(), "app-b") {
		t.Fatalf("ReconcileOnce error = %v, want both app failures", err)
	}
}
