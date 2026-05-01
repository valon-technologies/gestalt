package metrictest

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

type ManualMeterProvider struct {
	Provider *sdkmetric.MeterProvider
	Reader   *sdkmetric.ManualReader
}

func NewManualMeterProvider(t *testing.T) *ManualMeterProvider {
	t.Helper()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
	})
	return &ManualMeterProvider{
		Provider: provider,
		Reader:   reader,
	}
}

func CollectMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	return rm
}

func RequireInt64Sum(t *testing.T, rm metricdata.ResourceMetrics, name string, want int64, attrs map[string]string) {
	t.Helper()

	for _, scope := range rm.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name != name {
				continue
			}
			sum, ok := metric.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %q is %T, want Sum[int64]", name, metric.Data)
			}
			for _, point := range sum.DataPoints {
				if AttrsMatch(point.Attributes, attrs) {
					if point.Value != want {
						t.Fatalf("metric %q attrs %v = %d, want %d", name, attrs, point.Value, want)
					}
					return
				}
			}
		}
	}

	t.Fatalf("metric %q with attrs %v not found", name, attrs)
}

func RequireNoInt64Sum(t *testing.T, rm metricdata.ResourceMetrics, name string, attrs map[string]string) {
	t.Helper()

	for _, scope := range rm.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name != name {
				continue
			}
			sum, ok := metric.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %q is %T, want Sum[int64]", name, metric.Data)
			}
			for _, point := range sum.DataPoints {
				if AttrsMatch(point.Attributes, attrs) {
					t.Fatalf("metric %q with attrs %v unexpectedly found", name, attrs)
				}
			}
		}
	}
}

func RequireInt64SumOmitsAttr(t *testing.T, rm metricdata.ResourceMetrics, name string, attrs map[string]string, forbiddenKey string) {
	t.Helper()

	found := false
	for _, scope := range rm.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name != name {
				continue
			}
			sum, ok := metric.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %q is %T, want Sum[int64]", name, metric.Data)
			}
			for _, point := range sum.DataPoints {
				if !AttrsMatch(point.Attributes, attrs) {
					continue
				}
				found = true
				if _, ok := point.Attributes.Value(attribute.Key(forbiddenKey)); ok {
					t.Fatalf("metric %q attrs %v unexpectedly include %q", name, attrs, forbiddenKey)
				}
			}
		}
	}

	if !found {
		t.Fatalf("metric %q with attrs %v not found", name, attrs)
	}
}

func RequireFloat64Histogram(t *testing.T, rm metricdata.ResourceMetrics, name string, attrs map[string]string) {
	t.Helper()

	for _, scope := range rm.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name != name {
				continue
			}
			histogram, ok := metric.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("metric %q is %T, want Histogram[float64]", name, metric.Data)
			}
			for _, point := range histogram.DataPoints {
				if AttrsMatch(point.Attributes, attrs) {
					return
				}
			}
		}
	}

	t.Fatalf("metric %q with attrs %v not found", name, attrs)
}

func RequireNoMetric(t *testing.T, rm metricdata.ResourceMetrics, name string) {
	t.Helper()

	for _, scope := range rm.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name == name {
				t.Fatalf("metric %q unexpectedly found", name)
			}
		}
	}
}

func RequireFloat64HistogramOmitsAttr(t *testing.T, rm metricdata.ResourceMetrics, name string, attrs map[string]string, forbiddenKey string) {
	t.Helper()

	found := false
	for _, scope := range rm.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name != name {
				continue
			}
			histogram, ok := metric.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("metric %q is %T, want Histogram[float64]", name, metric.Data)
			}
			for _, point := range histogram.DataPoints {
				if !AttrsMatch(point.Attributes, attrs) {
					continue
				}
				found = true
				if _, ok := point.Attributes.Value(attribute.Key(forbiddenKey)); ok {
					t.Fatalf("metric %q attrs %v unexpectedly include %q", name, attrs, forbiddenKey)
				}
			}
		}
	}

	if !found {
		t.Fatalf("metric %q with attrs %v not found", name, attrs)
	}
}

func AttrsMatch(set attribute.Set, want map[string]string) bool {
	for key, expected := range want {
		value, ok := set.Value(attribute.Key(key))
		if !ok || value.AsString() != expected {
			return false
		}
	}
	return true
}
