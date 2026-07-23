package appregistry_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/appregistry/registrytest"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

type recordingAppRestarter struct {
	stopCalls      []string
	startCalls     []string
	startVersions  []string
	runningVersion string
	startErr       error
}

func (r *recordingAppRestarter) Restartable(app string) (bool, error) {
	return true, nil
}

func (r *recordingAppRestarter) StopApp(_ context.Context, app string) error {
	r.stopCalls = append(r.stopCalls, app)
	return nil
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

type pollerMaterializationHarness struct {
	ctx          context.Context
	services     *coredata.Services
	fixture      registrytest.InstallFixture
	artifactsDir string
	clock        time.Time
	restartReady chan struct{}
	restarter    *recordingAppRestarter
	poller       *appregistry.CatalogPoller
}

func newPollerMaterializationHarness(t *testing.T, registry string, withMaterializer bool) *pollerMaterializationHarness {
	t.Helper()
	h := &pollerMaterializationHarness{
		ctx:          context.Background(),
		services:     testutil.NewStubServices(t),
		fixture:      registrytest.NewInstallFixture(t),
		artifactsDir: t.TempDir(),
		clock:        time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC),
		restartReady: make(chan struct{}),
		restarter:    &recordingAppRestarter{},
	}
	if _, err := h.services.AppVersionChangeRequests.AppendRequest(h.ctx, &core.AppVersionChangeRequest{
		App:         "g-issues",
		FromVersion: "previous",
		ToVersion:   h.fixture.Version,
		Timestamp:   h.clock,
		Metadata:    map[string]any{"registry": registry},
	}); err != nil {
		t.Fatalf("AppendRequest: %v", err)
	}
	cfg := appregistry.CatalogPollerConfig{
		ChangeRequests:      h.services.AppVersionChangeRequests,
		Materializations:    h.services.AppInstanceMaterializations,
		Rollouts:            h.services.AppRollouts,
		AppRestarter:        h.restarter,
		InstanceID:          "replica-a",
		DisableRestartDelay: true,
		RestartReady:        h.restartReady,
		Now:                 func() time.Time { return h.clock },
	}
	if withMaterializer {
		cfg.AppMaterializer = &appregistry.Materializer{
			Registries:   map[string]config.AppRegistryConfig{"toolshed": h.fixture.Registry},
			Reader:       h.fixture.Reader,
			ArtifactsDir: h.artifactsDir,
		}
	}
	h.poller = appregistry.NewCatalogPoller(cfg)
	return h
}

func (h *pollerMaterializationHarness) materializedPath() string {
	return appregistry.MaterializedPath(h.artifactsDir, "g-issues", h.fixture.Version)
}

func (h *pollerMaterializationHarness) materialization(t *testing.T) *core.AppInstanceMaterialization {
	t.Helper()
	materialization, err := h.services.AppInstanceMaterializations.Get(h.ctx, "replica-a", "g-issues", h.fixture.Version)
	if err != nil {
		t.Fatalf("Get materialization: %v", err)
	}
	return materialization
}

func TestCatalogPollerMaterializesBeforeStop(t *testing.T) {
	t.Parallel()
	h := newPollerMaterializationHarness(t, "toolshed", true)

	if err := h.poller.ReconcileOnce(h.ctx); err != nil {
		t.Fatalf("ReconcileOnce before providers ready: %v", err)
	}
	if got := len(h.restarter.stopCalls); got != 0 {
		t.Fatalf("stopCalls before providers ready = %d, want 0", got)
	}

	materialization := h.materialization(t)
	if materialization.MaterializedAt != h.clock {
		t.Fatalf("MaterializedAt = %v, want %v", materialization.MaterializedAt, h.clock)
	}
	wantPath := h.materializedPath()
	if _, err := os.Stat(filepath.Join(wantPath, "manifest.yaml")); err != nil {
		t.Fatalf("stat materialized manifest: %v", err)
	}

	close(h.restartReady)
	if err := h.poller.ReconcileOnce(h.ctx); err != nil {
		t.Fatalf("ReconcileOnce after providers ready: %v", err)
	}
	if got := h.restarter.stopCalls; len(got) != 1 || got[0] != "g-issues" {
		t.Fatalf("stopCalls after providers ready = %#v, want [g-issues]", got)
	}
}

func TestCatalogPollerRolloutMaterializesDuringEnrollment(t *testing.T) {
	t.Parallel()
	h := newPollerMaterializationHarness(t, "toolshed", true)
	start := h.clock
	if _, err := h.services.AppRollouts.Create(h.ctx, &core.AppRollout{
		App:              "g-issues",
		Version:          h.fixture.Version,
		State:            core.AppRolloutStateEnrolling,
		CreatedAt:        start,
		EnrollmentEndsAt: start.Add(2 * time.Minute),
		Deadline:         start.Add(15 * time.Minute),
	}); err != nil {
		t.Fatalf("Create rollout: %v", err)
	}

	if err := h.poller.ReconcileOnce(h.ctx); err != nil {
		t.Fatalf("enrollment pass: %v", err)
	}
	if len(h.restarter.stopCalls) != 0 || len(h.restarter.startCalls) != 0 {
		t.Fatalf("restart calls during enrollment: stop=%v start=%v", h.restarter.stopCalls, h.restarter.startCalls)
	}
	materialization := h.materialization(t)
	if materialization.MaterializedAt != start {
		t.Fatalf("MaterializedAt = %v, want %v", materialization.MaterializedAt, start)
	}
	if !materialization.RestartedAt.IsZero() {
		t.Fatalf("RestartedAt = %v, want zero during enrollment", materialization.RestartedAt)
	}
	rollout, err := h.services.AppRollouts.Get(h.ctx, "g-issues")
	if err != nil {
		t.Fatalf("Get rollout: %v", err)
	}
	if rollout.State != core.AppRolloutStateEnrolling {
		t.Fatalf("state = %q, want enrolling", rollout.State)
	}

	close(h.restartReady)
	h.clock = start.Add(2 * time.Minute)
	if err := h.poller.ReconcileOnce(h.ctx); err != nil {
		t.Fatalf("restart pass: %v", err)
	}
	if len(h.restarter.stopCalls) != 1 || len(h.restarter.startCalls) != 1 {
		t.Fatalf("restart calls after enrollment: stop=%v start=%v", h.restarter.stopCalls, h.restarter.startCalls)
	}
	materialization = h.materialization(t)
	if materialization.RestartedAt.IsZero() {
		t.Fatal("RestartedAt is zero after enrollment")
	}
}

func TestCatalogPollerDoesNotStopWithoutMaterializer(t *testing.T) {
	t.Parallel()
	h := newPollerMaterializationHarness(t, "toolshed", false)

	err := h.poller.ReconcileOnce(h.ctx)
	if err == nil || !strings.Contains(err.Error(), "app registry materializer is required") {
		t.Fatalf("ReconcileOnce error = %v, want missing materializer error", err)
	}
	if len(h.restarter.stopCalls) != 0 {
		t.Fatalf("stopCalls = %v, want none", h.restarter.stopCalls)
	}
}

func TestCatalogPollerSkipsMaterializationForLegacyNonRegistryVersion(t *testing.T) {
	t.Parallel()
	h := newPollerMaterializationHarness(t, "", true)
	close(h.restartReady)

	if err := h.poller.ReconcileOnce(h.ctx); err != nil {
		t.Fatalf("ReconcileOnce: %v", err)
	}
	if got := h.restarter.stopCalls; len(got) != 1 || got[0] != "g-issues" {
		t.Fatalf("stopCalls = %#v, want [g-issues]", got)
	}
	if materializedAt := h.materialization(t).MaterializedAt; !materializedAt.IsZero() {
		t.Fatalf("MaterializedAt = %v, want zero for non-registry version", materializedAt)
	}
}

func TestCatalogPollerRematerializesWhenArtifactMissing(t *testing.T) {
	t.Parallel()
	h := newPollerMaterializationHarness(t, "toolshed", true)

	if err := h.poller.ReconcileOnce(h.ctx); err != nil {
		t.Fatalf("first ReconcileOnce: %v", err)
	}
	wantPath := h.materializedPath()
	if err := os.RemoveAll(wantPath); err != nil {
		t.Fatalf("RemoveAll materialized path: %v", err)
	}

	h.clock = h.clock.Add(time.Minute)
	if err := h.poller.ReconcileOnce(h.ctx); err != nil {
		t.Fatalf("second ReconcileOnce after artifact removal: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wantPath, "manifest.yaml")); err != nil {
		t.Fatalf("stat rematerialized manifest: %v", err)
	}

	materialization := h.materialization(t)
	if materialization.MaterializedAt != h.clock {
		t.Fatalf("MaterializedAt = %v, want %v", materialization.MaterializedAt, h.clock)
	}

	close(h.restartReady)
	if err := h.poller.ReconcileOnce(h.ctx); err != nil {
		t.Fatalf("ReconcileOnce after providers ready: %v", err)
	}
	if got := len(h.restarter.stopCalls); got != 1 {
		t.Fatalf("stopCalls = %d, want 1", got)
	}
}

func TestCatalogPollerStartAppPassesDriverVersion(t *testing.T) {
	t.Parallel()
	h := newPollerMaterializationHarness(t, "toolshed", true)
	close(h.restartReady)

	if err := h.poller.ReconcileOnce(h.ctx); err != nil {
		t.Fatalf("first ReconcileOnce: %v", err)
	}
	if err := h.poller.ReconcileOnce(h.ctx); err != nil {
		t.Fatalf("second ReconcileOnce: %v", err)
	}
	if got := h.restarter.startVersions; len(got) != 1 || got[0] != h.fixture.Version {
		t.Fatalf("startVersions = %#v, want [%q]", got, h.fixture.Version)
	}
}

func TestCatalogPollerRunningLatestSkipsSupersededMaterialization(t *testing.T) {
	t.Parallel()
	h := newPollerMaterializationHarness(t, "toolshed", true)
	oldVersion := "0.0.0-snapshot.gold"
	if _, err := h.services.AppVersionChangeRequests.AppendRequest(h.ctx, &core.AppVersionChangeRequest{
		App:         "g-issues",
		FromVersion: "previous",
		ToVersion:   oldVersion,
		Timestamp:   h.clock.Add(-time.Minute),
		Metadata:    map[string]any{"registry": "toolshed"},
	}); err != nil {
		t.Fatalf("AppendRequest(old version): %v", err)
	}
	h.restarter.runningVersion = h.fixture.Version

	if err := h.poller.ReconcileOnce(h.ctx); err != nil {
		t.Fatalf("ReconcileOnce: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.materializedPath(), "manifest.yaml")); err != nil {
		t.Fatalf("stat materialized manifest: %v", err)
	}
	materialization := h.materialization(t)
	if materialization.MaterializedAt.IsZero() {
		t.Fatal("MaterializedAt is zero")
	}
	if materialization.RestartedAt.IsZero() {
		t.Fatal("RestartedAt is zero")
	}
	oldMaterialization, err := h.services.AppInstanceMaterializations.Get(h.ctx, "replica-a", "g-issues", oldVersion)
	if err != nil {
		t.Fatalf("Get old materialization: %v", err)
	}
	if !oldMaterialization.MaterializedAt.IsZero() {
		t.Fatalf("old MaterializedAt = %v, want zero", oldMaterialization.MaterializedAt)
	}
	if oldMaterialization.RestartedAt.IsZero() {
		t.Fatal("old RestartedAt is zero")
	}
	oldPath := appregistry.MaterializedPath(h.artifactsDir, "g-issues", oldVersion)
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old version stat error = %v, want not exist", err)
	}
	if len(h.restarter.stopCalls) != 0 || len(h.restarter.startCalls) != 0 {
		t.Fatalf("unexpected restart calls: stop=%v start=%v", h.restarter.stopCalls, h.restarter.startCalls)
	}
}

func TestCatalogPollerAtRetryLimitRecordsMaterializedRunningVersion(t *testing.T) {
	t.Parallel()
	h := newPollerMaterializationHarness(t, "toolshed", true)
	h.poller.MaxReconcileAttempts = 1

	if err := h.poller.ReconcileOnce(h.ctx); err != nil {
		t.Fatalf("materialization pass: %v", err)
	}
	if _, err := h.services.AppInstanceMaterializations.RecordFailure(
		h.ctx,
		"replica-a",
		"g-issues",
		h.fixture.Version,
		h.clock,
		"prior start failure",
	); err != nil {
		t.Fatalf("RecordFailure: %v", err)
	}
	h.restarter.runningVersion = h.fixture.Version

	if err := h.poller.ReconcileOnce(h.ctx); err != nil {
		t.Fatalf("limited convergence pass: %v", err)
	}
	materialization := h.materialization(t)
	if materialization.RestartedAt.IsZero() {
		t.Fatal("RestartedAt is zero")
	}
	if len(h.restarter.stopCalls) != 0 || len(h.restarter.startCalls) != 0 {
		t.Fatalf("unexpected restart calls: stop=%v start=%v", h.restarter.stopCalls, h.restarter.startCalls)
	}
}

func TestCatalogPollerRetainsOldVersionUntilDesiredVersionStarts(t *testing.T) {
	t.Parallel()
	h := newPollerMaterializationHarness(t, "toolshed", true)
	oldPath := appregistry.MaterializedPath(h.artifactsDir, "g-issues", "old")
	if err := os.MkdirAll(oldPath, 0o755); err != nil {
		t.Fatalf("MkdirAll old version: %v", err)
	}
	h.restarter.startErr = errors.New("start failed")
	close(h.restartReady)

	err := h.poller.ReconcileOnce(h.ctx)
	if err == nil || !strings.Contains(err.Error(), "start failed") {
		t.Fatalf("ReconcileOnce error = %v, want start failure", err)
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("old version removed before desired start: %v", err)
	}
}
