package invocation

import (
	"context"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/metricutil"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type operationMetrics struct {
	count      metric.Int64Counter
	errorCount metric.Int64Counter
	duration   metric.Float64Histogram
}

func newOperationMetrics(meter metric.Meter) operationMetrics {
	return operationMetrics{
		count: metricutil.NewInt64Counter(
			meter,
			"gestaltd.operation.count",
			"Counts gestaltd operation invocations.",
		),
		errorCount: metricutil.NewInt64Counter(
			meter,
			"gestaltd.operation.error_count",
			"Counts gestaltd operation invocations that fail.",
		),
		duration: metricutil.NewFloat64Histogram(
			meter,
			"gestaltd.operation.duration",
			"Measures gestaltd operation invocation duration.",
			"s",
		),
	}
}

var operationMetricsCache metricutil.MeterCache[operationMetrics]

func recordOperationMetrics(
	ctx context.Context,
	startedAt time.Time,
	provider string,
	operation string,
	transport string,
	connectionMode string,
	failed bool,
) {
	metrics := operationMetricsCache.Load(ctx, tracerName, newOperationMetrics)
	attrs := []attribute.KeyValue{
		attrProvider.String(metricutil.AttrValue(provider)),
		attrOperation.String(metricutil.AttrValue(operation)),
		attrTransport.String(metricutil.AttrValue(transport)),
		attrConnectionMode.String(metricutil.AttrValue(connectionMode)),
	}

	metrics.count.Add(ctx, 1, metric.WithAttributes(attrs...))
	duration := time.Since(startedAt)
	metrics.duration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))
	if failed {
		metrics.errorCount.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
}
