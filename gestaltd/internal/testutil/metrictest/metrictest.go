package metrictest

import (
	"context"
	"sync"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

var meterProviderMu sync.Mutex

func UseManualMeterProvider(t *testing.T) *sdkmetric.ManualReader {
	t.Helper()

	meterProviderMu.Lock()
	prev := otel.GetMeterProvider()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetMeterProvider(prev)
		meterProviderMu.Unlock()
	})
	return reader
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

func AttrsMatch(set attribute.Set, want map[string]string) bool {
	for key, expected := range want {
		value, ok := set.Value(attribute.Key(key))
		if !ok || value.AsString() != expected {
			return false
		}
	}
	return true
}

type NamedStubDatastore struct {
	coretesting.StubDatastore
	N               string
	ListAPITokensFn func(context.Context, string) ([]*core.APIToken, error)
}

func NewNamedStubDatastore(name string, stub coretesting.StubDatastore) *NamedStubDatastore {
	return &NamedStubDatastore{StubDatastore: stub, N: name}
}

func (s *NamedStubDatastore) Name() string { return s.N }

func (s *NamedStubDatastore) ListAPITokens(ctx context.Context, userID string) ([]*core.APIToken, error) {
	if s.ListAPITokensFn != nil {
		return s.ListAPITokensFn(ctx, userID)
	}
	return s.StubDatastore.ListAPITokens(ctx, userID)
}

var _ core.Datastore = (*NamedStubDatastore)(nil)
