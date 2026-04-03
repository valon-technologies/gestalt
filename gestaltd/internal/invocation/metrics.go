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

var (
	operationMetricsOnce sync.Once
	operationMetricState operationMetrics
)

func recordOperationMetrics(
	ctx context.Context,
	startedAt time.Time,
	provider string,
	operation string,
	transport string,
	connectionMode string,
	failed bool,
) {
	metrics := loadOperationMetrics()
	attrs := []attribute.KeyValue{
		attrProvider.String(metricAttrValue(provider)),
		attrOperation.String(metricAttrValue(operation)),
		attrTransport.String(metricAttrValue(transport)),
		attrConnectionMode.String(metricAttrValue(connectionMode)),
	}

	metrics.count.Add(ctx, 1, metric.WithAttributes(attrs...))
	metrics.duration.Record(ctx, time.Since(startedAt).Seconds(), metric.WithAttributes(attrs...))
	if failed {
		metrics.errorCount.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
}

func loadOperationMetrics() operationMetrics {
	operationMetricsOnce.Do(func() {
		meter := otel.Meter(tracerName)

		count, err := meter.Int64Counter(
			"gestaltd.operation.count",
			metric.WithDescription("Counts gestaltd operation invocations."),
		)
		if err != nil {
			otel.Handle(err)
			count = noopmetric.Int64Counter{}
		}

		errorCount, err := meter.Int64Counter(
			"gestaltd.operation.error_count",
			metric.WithDescription("Counts gestaltd operation invocations that fail."),
		)
		if err != nil {
			otel.Handle(err)
			errorCount = noopmetric.Int64Counter{}
		}

		duration, err := meter.Float64Histogram(
			"gestaltd.operation.duration",
			metric.WithDescription("Measures gestaltd operation invocation duration."),
			metric.WithUnit("s"),
		)
		if err != nil {
			otel.Handle(err)
			duration = noopmetric.Float64Histogram{}
		}

		operationMetricState = operationMetrics{
			count:      count,
			errorCount: errorCount,
			duration:   duration,
		}
	})

	return operationMetricState
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
