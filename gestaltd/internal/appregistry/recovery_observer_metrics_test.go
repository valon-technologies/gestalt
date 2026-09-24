package appregistry

import (
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/testutil/metrictest"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
)

func TestRecoveryObserverRecordsAppRolloutRecoveryMetric(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	fixture := newRecoveryFixture(t)
	fixture.ctx = metricutil.WithMeterProvider(fixture.ctx, metrics.Provider)
	observer := fixture.observer(fixture.services.AppVersionRecoveryObservations)

	fixture.observe(t, observer)
	fixture.assertNoRecovery(t)
	fixture.now = fixture.now.Add(fixture.window - time.Millisecond)
	fixture.observe(t, observer)
	fixture.assertNoRecovery(t)
	fixture.now = fixture.now.Add(time.Millisecond)
	fixture.observe(t, observer)

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	attrs := map[string]string{"gestaltd.appregistry.app": fixture.app}
	metrictest.RequireInt64Sum(t, rm, "gestaltd.appregistry.recovery.count", 1, attrs)
	metrictest.RequireFloat64Histogram(t, rm, "gestaltd.appregistry.recovery.time_to_recover", attrs)
}
