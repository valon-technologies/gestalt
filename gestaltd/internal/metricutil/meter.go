package metricutil

import (
	"context"
	"reflect"
	"strconv"
	"sync"

	"github.com/valon-technologies/gestalt/server/core"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	noopmetric "go.opentelemetry.io/otel/metric/noop"
)

type meterProviderContextKey struct{}

type MeterCache[T any] struct {
	mu      sync.Mutex
	key     string
	metrics T
}

func WithMeterProvider(ctx context.Context, provider metric.MeterProvider) context.Context {
	if ctx == nil || provider == nil {
		return ctx
	}
	return context.WithValue(ctx, meterProviderContextKey{}, provider)
}

func MeterProviderFromContext(ctx context.Context) metric.MeterProvider {
	if ctx != nil {
		if provider, _ := ctx.Value(meterProviderContextKey{}).(metric.MeterProvider); provider != nil {
			return provider
		}
	}
	return otel.GetMeterProvider()
}

func (c *MeterCache[T]) Load(ctx context.Context, meterName string, build func(metric.Meter) T) T {
	provider := MeterProviderFromContext(ctx)
	if key, ok := meterProviderCacheKey(provider); ok {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.key == key {
			return c.metrics
		}
		metrics := build(provider.Meter(meterName))
		c.key = key
		c.metrics = metrics
		return metrics
	}
	return build(provider.Meter(meterName))
}

func NewInt64Counter(meter metric.Meter, name, desc string) metric.Int64Counter {
	counter, err := meter.Int64Counter(name, metric.WithDescription(desc))
	if err != nil {
		otel.Handle(err)
		return noopmetric.Int64Counter{}
	}
	return counter
}

func NewInt64Gauge(meter metric.Meter, name, desc string) metric.Int64Gauge {
	gauge, err := meter.Int64Gauge(name, metric.WithDescription(desc))
	if err != nil {
		otel.Handle(err)
		return noopmetric.Int64Gauge{}
	}
	return gauge
}

func NewFloat64Histogram(meter metric.Meter, name, desc, unit string, opts ...metric.Float64HistogramOption) metric.Float64Histogram {
	histogramOpts := []metric.Float64HistogramOption{
		metric.WithDescription(desc),
		metric.WithUnit(unit),
	}
	histogramOpts = append(histogramOpts, opts...)
	histogram, err := meter.Float64Histogram(
		name,
		histogramOpts...,
	)
	if err != nil {
		otel.Handle(err)
		return noopmetric.Float64Histogram{}
	}
	return histogram
}

func NormalizeConnectionMode(mode core.ConnectionMode) string {
	return string(core.NormalizeConnectionMode(mode))
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
