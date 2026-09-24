package autodeploy

import (
	"context"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/internal/testutil/metrictest"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
)

// supersedingFleetHealthReader reproduces, inside a single Reconcile call, the
// same staleness that a genuine concurrent manual retry would cause in
// production: the fleet health check itself races a rollout replacement, so
// by the time Reconcile applies its pause/resume decision, the rollout it
// read at the top of the function is no longer the current one.
type supersedingFleetHealthReader struct {
	services   *testutil.Services
	projection *core.AppFleetProjection
}

func (r *supersedingFleetHealthReader) ProjectForRollout(ctx context.Context, rollout *core.AppRollout) (*core.AppFleetProjection, error) {
	replacement, err := r.services.AppRollouts.Create(ctx, &core.AppRollout{
		App:              rollout.App,
		Version:          rollout.Version,
		State:            core.AppRolloutStateEnrolling,
		CreatedAt:        rollout.CreatedAt.Add(time.Hour),
		EnrollmentEndsAt: rollout.CreatedAt.Add(time.Hour).Add(time.Minute),
		Deadline:         rollout.CreatedAt.Add(time.Hour).Add(15 * time.Minute),
	})
	if err != nil {
		return nil, err
	}
	if _, err := r.services.AppRollouts.MarkFailedForRollout(ctx, replacement, replacement.Deadline); err != nil {
		return nil, err
	}
	return r.projection, nil
}

func TestControllerPausesOnFailedRolloutRecordsMetric(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	ctx := metricutil.WithMeterProvider(t.Context(), metrics.Provider)

	services := testutil.NewStubServices(t)
	enableAutoDeploy(t, services, "g-issues")
	start := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	rollout, err := services.AppRollouts.Create(ctx, &core.AppRollout{
		App:              "g-issues",
		Version:          "v1",
		State:            core.AppRolloutStateEnrolling,
		CreatedAt:        start,
		EnrollmentEndsAt: start.Add(time.Minute),
		Deadline:         start.Add(15 * time.Minute),
	})
	if err != nil {
		t.Fatalf("Create rollout: %v", err)
	}
	if _, err := services.AppRollouts.MarkFailedForRollout(ctx, rollout, start.Add(15*time.Minute)); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	reader := &fakeReader{results: []*appregistry.AppIndexFetchResult{{Index: testIndex("g-issues", "v2")}}}
	controller := testController(services, reader, &fakeInstaller{})
	controller.Fleet = fakeFleetHealthReader{projection: &core.AppFleetProjection{
		State:          core.AppFleetStateDegraded,
		DesiredVersion: "v1",
	}}

	if err := controller.Reconcile(ctx, "g-issues"); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	attrs := map[string]string{"gestaltd.appregistry.app": "g-issues"}
	metrictest.RequireInt64Sum(t, rm, "gestaltd.appregistry.autodeploy.paused_count", 1, attrs)
}

func TestControllerHealthyFailedRolloutRecordsResumedMetric(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	ctx := metricutil.WithMeterProvider(t.Context(), metrics.Provider)

	services := testutil.NewStubServices(t)
	enableAutoDeploy(t, services, "g-issues")
	start := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	rollout, err := services.AppRollouts.Create(ctx, &core.AppRollout{
		App:              "g-issues",
		Version:          "v1",
		State:            core.AppRolloutStateEnrolling,
		CreatedAt:        start,
		EnrollmentEndsAt: start.Add(time.Minute),
		Deadline:         start.Add(15 * time.Minute),
	})
	if err != nil {
		t.Fatalf("Create rollout: %v", err)
	}
	if _, err := services.AppRollouts.MarkFailedForRollout(ctx, rollout, start.Add(15*time.Minute)); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	reader := &fakeReader{results: []*appregistry.AppIndexFetchResult{{Index: testIndex("g-issues", "v1")}}}
	controller := testController(services, reader, &fakeInstaller{})
	controller.Fleet = fakeFleetHealthReader{projection: &core.AppFleetProjection{
		State:          core.AppFleetStateHealthy,
		DesiredVersion: "v1",
	}}

	if err := controller.Reconcile(ctx, "g-issues"); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	attrs := map[string]string{
		"gestaltd.appregistry.app":                "g-issues",
		"gestaltd.appregistry.autodeploy_trigger": "auto_health_check",
	}
	metrictest.RequireInt64Sum(t, rm, "gestaltd.appregistry.autodeploy.resumed_count", 1, attrs)
}

func TestControllerStaleFailedRolloutRecordsNoAutoDeployMetric(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	ctx := metricutil.WithMeterProvider(t.Context(), metrics.Provider)

	services := testutil.NewStubServices(t)
	enableAutoDeploy(t, services, "g-issues")
	start := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	rollout, err := services.AppRollouts.Create(ctx, &core.AppRollout{
		App:              "g-issues",
		Version:          "v1",
		State:            core.AppRolloutStateEnrolling,
		CreatedAt:        start,
		EnrollmentEndsAt: start.Add(time.Minute),
		Deadline:         start.Add(15 * time.Minute),
	})
	if err != nil {
		t.Fatalf("Create rollout: %v", err)
	}
	if _, err := services.AppRollouts.MarkFailedForRollout(ctx, rollout, start.Add(15*time.Minute)); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	reader := &fakeReader{results: []*appregistry.AppIndexFetchResult{{Index: testIndex("g-issues", "v1")}}}
	controller := testController(services, reader, &fakeInstaller{})
	controller.Fleet = &supersedingFleetHealthReader{
		services:   services,
		projection: &core.AppFleetProjection{State: core.AppFleetStateHealthy, DesiredVersion: "v1"},
	}

	if err := controller.Reconcile(ctx, "g-issues"); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	attrs := map[string]string{"gestaltd.appregistry.app": "g-issues"}
	metrictest.RequireNoInt64Sum(t, rm, "gestaltd.appregistry.autodeploy.paused_count", attrs)
	metrictest.RequireNoInt64Sum(t, rm, "gestaltd.appregistry.autodeploy.resumed_count", attrs)
}
