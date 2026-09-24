package appregistry

import (
	"sync"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/coredata"
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

// TestConcurrentRecoveryObserversRecordMetricExactlyOnce reproduces the
// production overcount: RecordIfCurrentFailed returns recorded=true for a
// duplicate hit too (a second replica racing the same recovery), so naively
// firing the metric on every recorded==true multiplies one real recovery by
// however many replicas raced it. Distinct Now clocks stand in for distinct
// replicas' wall clocks, which is what actually distinguishes the winner in
// production.
func TestConcurrentRecoveryObserversRecordMetricExactlyOnce(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	fixture := newRecoveryFixture(t)
	fixture.ctx = metricutil.WithMeterProvider(fixture.ctx, metrics.Provider)

	firstNow := fixture.now
	secondNow := fixture.now.Add(time.Millisecond)
	first := NewRecoveryObserver(RecoveryObserverConfig{
		ChangeRequests:  fixture.services.AppVersionChangeRequests,
		Outcomes:        fixture.services.AppVersionRolloutOutcomes,
		Observations:    fixture.services.AppVersionRecoveryObservations,
		SourceVersions:  fixture.services.GestaltdSourceVersionState,
		Heartbeats:      fixture.services.GestaltdInstanceHeartbeats,
		HeartbeatTTL:    fixture.ttl,
		StabilityWindow: fixture.window,
		Interval:        10 * time.Second,
		Now:             func() time.Time { return firstNow },
	})
	second := NewRecoveryObserver(RecoveryObserverConfig{
		ChangeRequests:  fixture.services.AppVersionChangeRequests,
		Outcomes:        fixture.services.AppVersionRolloutOutcomes,
		Observations:    fixture.services.AppVersionRecoveryObservations,
		SourceVersions:  fixture.services.GestaltdSourceVersionState,
		Heartbeats:      fixture.services.GestaltdInstanceHeartbeats,
		HeartbeatTTL:    fixture.ttl,
		StabilityWindow: fixture.window,
		Interval:        10 * time.Second,
		Now:             func() time.Time { return secondNow },
	})

	// Prime each observer's own stability tracking sequentially, exactly as
	// TestConcurrentRecoveryObserversRecordExactlyOnce does, so the race
	// below is only about the DB write, not the stability computation.
	if err := first.ObserveOnce(fixture.ctx); err != nil {
		t.Fatalf("prime first: %v", err)
	}
	if err := second.ObserveOnce(fixture.ctx); err != nil {
		t.Fatalf("prime second: %v", err)
	}
	firstNow = firstNow.Add(fixture.window)
	secondNow = secondNow.Add(fixture.window)

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

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	metrictest.RequireInt64Sum(t, rm, "gestaltd.appregistry.recovery.count", 1, map[string]string{
		"gestaltd.appregistry.app": fixture.app,
	})
}
