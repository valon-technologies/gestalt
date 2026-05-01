package plugins

import (
	"context"
	"testing"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/testutil/metrictest"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/trace"
	nooptrace "go.opentelemetry.io/otel/trace/noop"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

type providerServerMetricsTelemetry struct {
	meterProvider metric.MeterProvider
}

func (t providerServerMetricsTelemetry) MeterProvider() metric.MeterProvider {
	return t.meterProvider
}

func (providerServerMetricsTelemetry) TracerProvider() trace.TracerProvider {
	return nooptrace.NewTracerProvider()
}

func TestProviderServerGRPCOptionsRecordServerDurationWithTelemetryAttrs(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	const providerName = "provider-rpc-metrics"
	telemetry := providerServerMetricsTelemetry{meterProvider: metrics.Provider}

	conn := newBufconnConnWithOptions(t,
		[]grpc.ServerOption{grpc.StatsHandler(otelgrpc.NewServerHandler(providerServerGRPCOptions(providerName, telemetry)...))},
		nil,
		func(srv *grpc.Server) {
			proto.RegisterIntegrationProviderServer(srv, NewProviderServer(&coretesting.StubIntegration{N: providerName}))
		},
	)

	if _, err := proto.NewIntegrationProviderClient(conn).GetMetadata(context.Background(), &emptypb.Empty{}); err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}

	attrs := map[string]string{
		"gestaltd.rpc.role":      "provider_server",
		"gestaltd.provider.name": providerName,
	}
	rm := collectMetricsUntilFloat64Histogram(t, metrics.Reader, "rpc.server.call.duration", attrs)
	metrictest.RequireFloat64Histogram(t, rm, "rpc.server.call.duration", attrs)
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "rpc.server.call.duration", attrs, "gestalt.rpc.role")
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "rpc.server.call.duration", attrs, "gestalt.provider")
}

func collectMetricsUntilFloat64Histogram(t *testing.T, reader *sdkmetric.ManualReader, name string, attrs map[string]string) metricdata.ResourceMetrics {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	var rm metricdata.ResourceMetrics
	for {
		rm = metrictest.CollectMetrics(t, reader)
		if hasFloat64Histogram(rm, name, attrs) {
			return rm
		}
		if time.Now().After(deadline) {
			return rm
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func hasFloat64Histogram(rm metricdata.ResourceMetrics, name string, attrs map[string]string) bool {
	for _, scope := range rm.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name != name {
				continue
			}
			histogram, ok := metric.Data.(metricdata.Histogram[float64])
			if !ok {
				continue
			}
			for _, point := range histogram.DataPoints {
				if metrictest.AttrsMatch(point.Attributes, attrs) {
					return true
				}
			}
		}
	}
	return false
}
