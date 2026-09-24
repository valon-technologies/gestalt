package appregistry

import (
	"context"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/internal/testutil/metrictest"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
)

func TestHeartbeatRolloutCompleteRecordsAppRolloutOutcomeMetric(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	ctx := metricutil.WithMeterProvider(context.Background(), metrics.Provider)
	services := testutil.NewStubServices(t)
	start := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	rollout := heartbeatPollerRollout(t, services, start, 1)
	clock := start
	poller := heartbeatRolloutPoller(services, &clock)

	upsertRolloutHeartbeat(t, services, "replica-a", "source-a", start, "v2", core.GestaltdInstanceAppStateRunning)
	if terminal, err := poller.updateHeartbeatRolloutOutcome(ctx, rollout); err != nil || terminal {
		t.Fatalf("first evaluation terminal=%v err=%v", terminal, err)
	}

	clock = start.Add(time.Minute)
	upsertRolloutHeartbeat(t, services, "replica-a", "source-a", clock, "v2", core.GestaltdInstanceAppStateRunning)
	current, err := services.AppRollouts.Get(ctx, rollout.App)
	if err != nil {
		t.Fatal(err)
	}
	if terminal, err := poller.updateHeartbeatRolloutOutcome(ctx, current); err != nil || !terminal {
		t.Fatalf("second evaluation terminal=%v err=%v", terminal, err)
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	attrs := map[string]string{
		"gestaltd.appregistry.app":          "g-issues",
		"gestaltd.appregistry.rollout_mode": "heartbeat",
	}
	metrictest.RequireInt64Sum(t, rm, "gestaltd.appregistry.rollout.count", 1, attrs)
	metrictest.RequireNoInt64Sum(t, rm, "gestaltd.appregistry.rollout.error_count", attrs)
	metrictest.RequireFloat64Histogram(t, rm, "gestaltd.appregistry.rollout.duration", attrs)
}

func TestHeartbeatRolloutFailedRecordsAppRolloutOutcomeMetric(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	ctx := metricutil.WithMeterProvider(context.Background(), metrics.Provider)
	services := testutil.NewStubServices(t)
	start := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	rollout := heartbeatPollerRollout(t, services, start, 2)
	clock := start.Add(15 * time.Minute)
	poller := heartbeatRolloutPoller(services, &clock)

	upsertRolloutHeartbeat(t, services, "running", "source-a", clock, "v2", core.GestaltdInstanceAppStateRunning)
	upsertRolloutHeartbeat(t, services, "starting", "source-a", clock, "v2", core.GestaltdInstanceAppStateStarting)
	if terminal, err := poller.updateHeartbeatRolloutOutcome(ctx, rollout); err != nil || !terminal {
		t.Fatalf("deadline evaluation terminal=%v err=%v", terminal, err)
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	attrs := map[string]string{
		"gestaltd.appregistry.app":          "g-issues",
		"gestaltd.appregistry.rollout_mode": "heartbeat",
	}
	metrictest.RequireInt64Sum(t, rm, "gestaltd.appregistry.rollout.count", 1, attrs)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.appregistry.rollout.error_count", 1, attrs)
	metrictest.RequireFloat64Histogram(t, rm, "gestaltd.appregistry.rollout.duration", attrs)
}
