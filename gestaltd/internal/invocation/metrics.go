package invocation

import (
	"context"
	"reflect"
	"strconv"
	"sync"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/metricutil"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	noopmetric "go.opentelemetry.io/otel/metric/noop"
)

type operationMetrics struct {
	count      metric.Int64Counter
	errorCount metric.Int64Counter
	duration   metric.Float64Histogram
	refresh    metric.Int64Counter
}

func newOperationMetrics(meter metric.Meter) operationMetrics {
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
		refresh: newInt64Counter(
			meter,
			"gestaltd.oauth.token_refresh.count",
			"Counts OAuth token refresh attempts performed by gestaltd.",
		),
	}
}

var operationMetricsCache metricCache[operationMetrics]

func recordOperationMetrics(
	ctx context.Context,
	startedAt time.Time,
	provider string,
	operation string,
	transport string,
	connectionMode string,
	failed bool,
) {
	metrics := operationMetricsCache.load(newOperationMetrics)
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

func recordTokenRefreshMetrics(ctx context.Context, provider string, connectionMode string, failed bool) {
	metrics := operationMetricsCache.load(newOperationMetrics)
	metrics.refresh.Add(ctx, 1, metric.WithAttributes(
		attrProvider.String(metricutil.AttrValue(provider)),
		attrConnectionMode.String(metricutil.AttrValue(connectionMode)),
		attrResult.String(metricutil.ResultValue(failed)),
	))
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

func normalizeConnectionMode(mode core.ConnectionMode) string {
	if mode == "" {
		return string(core.ConnectionModeUser)
	}
	return string(mode)
}

type metricCache[T any] struct {
	mu      sync.Mutex
	key     string
	metrics T
}

func (c *metricCache[T]) load(build func(metric.Meter) T) T {
	provider := otel.GetMeterProvider()
	if key, ok := meterProviderCacheKey(provider); ok {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.key == key {
			return c.metrics
		}
		metrics := build(provider.Meter(tracerName))
		c.key = key
		c.metrics = metrics
		return metrics
	}
	return build(provider.Meter(tracerName))
}

func meterProviderCacheKey(provider metric.MeterProvider) (string, bool) {
	if provider == nil {
		return "", false
	}

	value := reflect.ValueOf(provider)
	if !value.IsValid() {
		return "", false
	}

	switch value.Kind() {
	case reflect.Pointer, reflect.UnsafePointer:
		return value.Type().String() + ":" + strconv.FormatUint(uint64(value.Pointer()), 16), true
	default:
		return "", false
	}
}
