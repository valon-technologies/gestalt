package main

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

var cmdMeterProviderTestMu sync.Mutex

func useManualMeterProvider(t *testing.T) *sdkmetric.ManualReader {
	t.Helper()

	cmdMeterProviderTestMu.Lock()
	prev := otel.GetMeterProvider()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetMeterProvider(prev)
		cmdMeterProviderTestMu.Unlock()
	})
	return reader
}

func collectMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	return rm
}

func requireInt64Sum(t *testing.T, rm metricdata.ResourceMetrics, name string, want int64, attrs map[string]string) {
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
				if attrsMatch(point.Attributes, attrs) {
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

func attrsMatch(set attribute.Set, want map[string]string) bool {
	for key, expected := range want {
		value, ok := set.Value(attribute.Key(key))
		if !ok || value.AsString() != expected {
			return false
		}
	}
	return true
}

type namedStubDatastore struct {
	coretesting.StubDatastore
	name string
}

func (s *namedStubDatastore) Name() string { return s.name }

func TestDatastoreReadinessEmitsPingMetric(t *testing.T) {
	t.Parallel()

	reader := useManualMeterProvider(t)
	check := datastoreReadiness(&namedStubDatastore{
		name: "ready-store",
		StubDatastore: coretesting.StubDatastore{
			PingFn: func(context.Context) error { return nil },
		},
	})

	if got := check(); got != "" {
		t.Fatalf("readiness = %q, want ready", got)
	}

	rm := collectMetrics(t, reader)
	requireInt64Sum(t, rm, "gestaltd.datastore.count", 1, map[string]string{
		"gestalt.provider": "ready-store",
		"gestalt.method":   "ping",
	})
}

var _ core.Datastore = (*namedStubDatastore)(nil)
