package appregistry

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

type recordingAppRestarter struct {
	stopCalls      []string
	startCalls     []string
	stopErr        error
	startErr       error
	restartable    map[string]bool
	restartableErr error
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
	return r.stopErr
}

func (r *recordingAppRestarter) StartApp(_ context.Context, app string) error {
	r.startCalls = append(r.startCalls, app)
	return r.startErr
}

func (r *recordingAppRestarter) AbortRestarts() {}

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
		AppRestarter:     &recordingAppRestarter{restartableErr: errors.New("restart mode failed")},
		InstanceID:       "replica-a",
		Now:              func() time.Time { return now },
	})

	err := poller.ReconcileOnce(context.Background())
	if err == nil || !strings.Contains(err.Error(), "app-a") || !strings.Contains(err.Error(), "app-b") {
		t.Fatalf("ReconcileOnce error = %v, want both app failures", err)
	}
}
