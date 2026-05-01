package plugins

import (
	"context"
	"testing"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/services/testutil/metrictest"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel/metric"
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

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	attrs := map[string]string{
		"gestaltd.rpc.role":      "provider_server",
		"gestaltd.provider.name": providerName,
	}
	metrictest.RequireFloat64Histogram(t, rm, "rpc.server.call.duration", attrs)
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "rpc.server.call.duration", attrs, "gestalt.rpc.role")
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "rpc.server.call.duration", attrs, "gestalt.provider")
}
