package invocation

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	noopmetric "go.opentelemetry.io/otel/metric/noop"
)

const unknownMetricAttrValue = "unknown"

type operationMetrics struct {
	count      metric.Int64Counter
	errorCount metric.Int64Counter
	duration   metric.Float64Histogram
}

var operationMetricsOnce = sync.OnceValue(func() operationMetrics {
	meter := otel.Meter(tracerName)

	return operationMetrics{
		count: newInt64Counter(
			meter,
			"gestaltd.operation.count",
			"Counts gestaltd operation invocations.",
		),
		errorCount: newInt64Counter(
			meter,
			"gestaltd.operation.error_count",
			"Counts gestaltd operation invocations that fail.",
		),
		duration: newFloat64Histogram(
			meter,
			"gestaltd.operation.duration",
			"Measures gestaltd operation invocation duration.",
			"s",
		),
	}
})

func recordOperationMetrics(
	ctx context.Context,
	startedAt time.Time,
	provider string,
	operation string,
	transport string,
	connectionMode string,
	failed bool,
) {
	metrics := operationMetricsOnce()
	attrs := []attribute.KeyValue{
		attrProvider.String(metricAttrValue(provider)),
		attrOperation.String(metricAttrValue(operation)),
		attrTransport.String(metricAttrValue(transport)),
		attrConnectionMode.String(metricAttrValue(connectionMode)),
	}

	metrics.count.Add(ctx, 1, metric.WithAttributes(attrs...))
	duration := time.Since(startedAt)
	metrics.duration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))
	if failed {
		metrics.errorCount.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
}

func newInt64Counter(meter metric.Meter, name, desc string) metric.Int64Counter {
	counter, err := meter.Int64Counter(name, metric.WithDescription(desc))
	if err != nil {
		otel.Handle(err)
		return noopmetric.Int64Counter{}
	}
	return counter
}

func newFloat64Histogram(meter metric.Meter, name, desc, unit string) metric.Float64Histogram {
	histogram, err := meter.Float64Histogram(
		name,
		metric.WithDescription(desc),
		metric.WithUnit(unit),
	)
	if err != nil {
		otel.Handle(err)
		return noopmetric.Float64Histogram{}
	}
	return histogram
}

func metricAttrValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return unknownMetricAttrValue
	}
	return value
}

func normalizeConnectionMode(mode core.ConnectionMode) string {
	if mode == "" {
		return string(core.ConnectionModeUser)
	}
	return string(mode)
}
