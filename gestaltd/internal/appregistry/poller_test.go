package appregistry

import (
	"context"
	"errors"
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

type versionReportingAppRestarter struct {
	*recordingAppRestarter
	runningVersion string
	running        bool
}

func (r *versionReportingAppRestarter) RunningVersion(string) (string, bool) {
	return r.runningVersion, r.running
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

func TestCatalogPollerRecordsAlreadyRunningVersionWithoutRestart(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	version := "0.0.0-snapshot.gabc123"
	if _, err := services.AppVersionChangeRequests.AppendRequest(context.Background(), &core.AppVersionChangeRequest{
		App: "g-issues", FromVersion: "registry:first-install", ToVersion: version, Timestamp: now,
	}); err != nil {
		t.Fatalf("AppendRequest: %v", err)
	}
	recording := &recordingAppRestarter{}
	restarter := &versionReportingAppRestarter{
		recordingAppRestarter: recording,
		runningVersion:        version,
		running:               true,
	}
	poller := NewCatalogPoller(CatalogPollerConfig{
		ChangeRequests: services.AppVersionChangeRequests, Materializations: services.AppInstanceMaterializations,
		Rollouts: services.AppRollouts, AppRestarter: restarter, InstanceID: "replica-a",
		DisableRestartDelay: true, Now: func() time.Time { return now },
	})

	if err := poller.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("ReconcileOnce: %v", err)
	}
	if len(recording.stopCalls) != 0 || len(recording.startCalls) != 0 {
		t.Fatalf("restart calls: stop=%v start=%v, want none", recording.stopCalls, recording.startCalls)
	}
	materialization, err := services.AppInstanceMaterializations.Get(context.Background(), "replica-a", "g-issues", version)
	if err != nil {
		t.Fatalf("Get materialization: %v", err)
	}
	if materialization.RestartedAt != now {
		t.Fatalf("RestartedAt = %v, want %v", materialization.RestartedAt, now)
	}
}

func TestCatalogPollerStopsRunningOldVersionDespiteRecordedStop(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	version := "0.0.0-snapshot.gnew"
	if _, err := services.AppVersionChangeRequests.AppendRequest(context.Background(), &core.AppVersionChangeRequest{
		App: "g-issues", FromVersion: "0.0.0-snapshot.gold", ToVersion: version, Timestamp: now,
	}); err != nil {
		t.Fatalf("AppendRequest: %v", err)
	}
	if _, err := services.AppInstanceMaterializations.Acknowledge(context.Background(), &core.AppInstanceMaterialization{
		InstanceID: "replica-a", App: "g-issues", Version: version, AcknowledgedAt: now,
	}); err != nil {
		t.Fatalf("Acknowledge: %v", err)
	}
	if _, err := services.AppInstanceMaterializations.MarkStopped(context.Background(), "replica-a", "g-issues", version, now); err != nil {
		t.Fatalf("MarkStopped: %v", err)
	}
	recording := &recordingAppRestarter{}
	restarter := &versionReportingAppRestarter{
		recordingAppRestarter: recording,
		runningVersion:        "0.0.0-snapshot.gold",
		running:               true,
	}
	poller := NewCatalogPoller(CatalogPollerConfig{
		ChangeRequests: services.AppVersionChangeRequests, Materializations: services.AppInstanceMaterializations,
		Rollouts: services.AppRollouts, AppRestarter: restarter, InstanceID: "replica-a",
		DisableRestartDelay: true, Now: func() time.Time { return now },
	})

	if err := poller.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("ReconcileOnce: %v", err)
	}
	if len(recording.stopCalls) != 1 || len(recording.startCalls) != 1 {
		t.Fatalf("restart calls: stop=%v start=%v, want one each", recording.stopCalls, recording.startCalls)
	}
	if got := recording.startVersions; len(got) != 1 || got[0] != version {
		t.Fatalf("startVersions = %v, want [%s]", got, version)
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
