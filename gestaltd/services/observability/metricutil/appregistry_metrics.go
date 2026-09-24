package metricutil

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Auto-deploy resume triggers recorded on
// gestaltd.appregistry.autodeploy.resumed_count.
const (
	AutoDeployResumeTriggerHealthCheck = "auto_health_check"
	AutoDeployResumeTriggerManualRetry = "manual_retry"
	AutoDeployResumeTriggerToggle      = "toggle"
)

var (
	attrAppRegistryApp    = attribute.Key("gestaltd.appregistry.app")
	attrAppRolloutMode    = attribute.Key("gestaltd.appregistry.rollout_mode")
	attrAutoDeployTrigger = attribute.Key("gestaltd.appregistry.autodeploy_trigger")
)

var (
	appRolloutMetricsCache         MeterCache[counterMetrics]
	autoDeployPausedMetricsCache   MeterCache[metric.Int64Counter]
	autoDeployResumedMetricsCache  MeterCache[metric.Int64Counter]
	appRolloutRecoveryMetricsCache MeterCache[appRolloutRecoveryMetrics]
)

type appRolloutRecoveryMetrics struct {
	count    metric.Int64Counter
	duration metric.Float64Histogram
}

// RecordAppRolloutOutcome records a terminal app-registry rollout transition
// (complete or failed). Duration is measured between rollout creation and the
// terminal transition, both from the rollout's own timestamps rather than
// wall-clock elapsed test/process time.
func RecordAppRolloutOutcome(ctx context.Context, app, mode string, failed bool, createdAt, terminalAt time.Time) {
	metrics := appRolloutMetricsCache.Load(ctx, meterName, func(meter metric.Meter) counterMetrics {
		return newCounterMetrics(meter, "gestaltd.appregistry.rollout", "gestaltd app registry rollout terminal outcomes")
	})
	recordAppRegistryCounterMetrics(ctx, metrics, terminalAt.Sub(createdAt), failed,
		attrAppRegistryApp.String(AttrValue(app)),
		attrAppRolloutMode.String(AttrValue(mode)),
	)
}

// RecordAppAutoDeployPaused records the auto-deploy controller pausing
// automatic admissions for an app after a rollout reached `failed`.
func RecordAppAutoDeployPaused(ctx context.Context, app string) {
	counter := autoDeployPausedMetricsCache.Load(ctx, meterName, func(meter metric.Meter) metric.Int64Counter {
		return NewInt64Counter(
			meter,
			"gestaltd.appregistry.autodeploy.paused_count",
			"Counts app registry auto-deploy pauses after a failed rollout.",
		)
	})
	counter.Add(ctx, 1, metric.WithAttributes(attrAppRegistryApp.String(AttrValue(app))))
}

// RecordAppAutoDeployResumed records the auto-deploy controller clearing a
// runtime pause for an app, tagged by what triggered the resume.
func RecordAppAutoDeployResumed(ctx context.Context, app, trigger string) {
	counter := autoDeployResumedMetricsCache.Load(ctx, meterName, func(meter metric.Meter) metric.Int64Counter {
		return NewInt64Counter(
			meter,
			"gestaltd.appregistry.autodeploy.resumed_count",
			"Counts app registry auto-deploy resumptions after a runtime pause.",
		)
	})
	counter.Add(ctx, 1, metric.WithAttributes(
		attrAppRegistryApp.String(AttrValue(app)),
		attrAutoDeployTrigger.String(AttrValue(trigger)),
	))
}

// RecordAppRolloutRecovery records the recovery observer finding that a
// previously failed rollout's version later reached stable fleet health.
// This is additive to the failed outcome; it never rewrites it.
func RecordAppRolloutRecovery(ctx context.Context, app string, failedAt, recoveredAt time.Time) {
	metrics := appRolloutRecoveryMetricsCache.Load(ctx, meterName, func(meter metric.Meter) appRolloutRecoveryMetrics {
		return appRolloutRecoveryMetrics{
			count: NewInt64Counter(
				meter,
				"gestaltd.appregistry.recovery.count",
				"Counts app registry rollouts that recovered fleet health after a failed outcome.",
			),
			duration: NewFloat64Histogram(
				meter,
				"gestaltd.appregistry.recovery.time_to_recover",
				"Measures time between a failed rollout outcome and its recovery observation.",
				"s",
			),
		}
	})
	opts := metric.WithAttributes(attrAppRegistryApp.String(AttrValue(app)))
	metrics.count.Add(ctx, 1, opts)
	metrics.duration.Record(ctx, recoveredAt.Sub(failedAt).Seconds(), opts)
}

func recordAppRegistryCounterMetrics(ctx context.Context, metrics counterMetrics, duration time.Duration, failed bool, attrs ...attribute.KeyValue) {
	if ctx == nil {
		ctx = context.Background()
	}
	opts := metric.WithAttributes(attrs...)
	metrics.count.Add(ctx, 1, opts)
	metrics.duration.Record(ctx, duration.Seconds(), opts)
	if failed {
		metrics.errorCount.Add(ctx, 1, opts)
	}
}
