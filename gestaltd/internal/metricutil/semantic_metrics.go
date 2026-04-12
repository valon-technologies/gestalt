package metricutil

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const meterName = "gestaltd"

var (
	attrProvider       = attribute.Key("gestalt.provider")
	attrAction         = attribute.Key("gestalt.action")
	attrType           = attribute.Key("gestalt.type")
	attrConnectionMode = attribute.Key("gestalt.connection_mode")
)

type counterMetrics struct {
	count      metric.Int64Counter
	errorCount metric.Int64Counter
	duration   metric.Float64Histogram
}

func newCounterMetrics(meter metric.Meter, prefix, desc string) counterMetrics {
	return counterMetrics{
		count: NewInt64Counter(
			meter,
			prefix+".count",
			"Counts "+desc+".",
		),
		errorCount: NewInt64Counter(
			meter,
			prefix+".error_count",
			"Counts failed "+desc+".",
		),
		duration: NewFloat64Histogram(
			meter,
			prefix+".duration",
			"Measures "+desc+" duration.",
			"s",
		),
	}
}

var (
	authMetricsCache           MeterCache[counterMetrics]
	connectionAuthMetricsCache MeterCache[counterMetrics]
)

func RecordAuthMetrics(ctx context.Context, startedAt time.Time, provider string, action string, failed bool) {
	metrics := authMetricsCache.Load(meterName, func(meter metric.Meter) counterMetrics {
		return newCounterMetrics(meter, "gestaltd.auth", "gestaltd auth actions")
	})
	recordCounterMetrics(ctx, metrics, startedAt, failed,
		attrProvider.String(AttrValue(provider)),
		attrAction.String(AttrValue(action)),
	)
}

func RecordConnectionAuthMetrics(ctx context.Context, startedAt time.Time, provider string, authType string, action string, connectionMode string, failed bool) {
	metrics := connectionAuthMetricsCache.Load(meterName, func(meter metric.Meter) counterMetrics {
		return newCounterMetrics(meter, "gestaltd.connection.auth", "gestaltd connection auth actions")
	})
	recordCounterMetrics(ctx, metrics, startedAt, failed,
		attrProvider.String(AttrValue(provider)),
		attrType.String(AttrValue(authType)),
		attrAction.String(AttrValue(action)),
		attrConnectionMode.String(AttrValue(connectionMode)),
	)
}

func recordCounterMetrics(ctx context.Context, metrics counterMetrics, startedAt time.Time, failed bool, attrs ...attribute.KeyValue) {
	if ctx == nil {
		ctx = context.Background()
	}
	metrics.count.Add(ctx, 1, metric.WithAttributes(attrs...))
	metrics.duration.Record(ctx, time.Since(startedAt).Seconds(), metric.WithAttributes(attrs...))
	if failed {
		metrics.errorCount.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
}
