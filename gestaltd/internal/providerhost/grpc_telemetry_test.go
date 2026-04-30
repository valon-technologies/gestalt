package providerhost

import (
	"context"
	"testing"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/testutil/metrictest"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	nooptrace "go.opentelemetry.io/otel/trace/noop"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

type providerGRPCMetricsTelemetry struct {
	meterProvider metric.MeterProvider
}

func (t providerGRPCMetricsTelemetry) MeterProvider() metric.MeterProvider {
	return t.meterProvider
}

func (providerGRPCMetricsTelemetry) TracerProvider() trace.TracerProvider {
	return nooptrace.NewTracerProvider()
}

func TestProviderGRPCOptionsRecordClientAndServerDurationWithTelemetryAttrs(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	const providerName = "provider-rpc-metrics"
	telemetry := providerGRPCMetricsTelemetry{meterProvider: metrics.Provider}

	conn := newBufconnConnWithOptions(t,
		[]grpc.ServerOption{grpc.StatsHandler(otelgrpc.NewServerHandler(providerServerGRPCOptions(providerName, telemetry)...))},
		[]grpc.DialOption{grpc.WithStatsHandler(otelgrpc.NewClientHandler(providerClientGRPCOptions(providerName, telemetry)...))},
		func(srv *grpc.Server) {
			proto.RegisterIntegrationProviderServer(srv, NewProviderServer(&coretesting.StubIntegration{N: providerName}))
		},
	)

	if _, err := proto.NewIntegrationProviderClient(conn).GetMetadata(context.Background(), &emptypb.Empty{}); err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	clientAttrs := map[string]string{
		"gestaltd.rpc.role":      "provider_client",
		"gestaltd.provider.name": providerName,
	}
	metrictest.RequireFloat64Histogram(t, rm, "rpc.client.call.duration", clientAttrs)
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "rpc.client.call.duration", clientAttrs, "gestalt.rpc.role")
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "rpc.client.call.duration", clientAttrs, "gestalt.provider")

	serverAttrs := map[string]string{
		"gestaltd.rpc.role":      "provider_server",
		"gestaltd.provider.name": providerName,
	}
	metrictest.RequireFloat64Histogram(t, rm, "rpc.server.call.duration", serverAttrs)
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "rpc.server.call.duration", serverAttrs, "gestalt.rpc.role")
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "rpc.server.call.duration", serverAttrs, "gestalt.provider")
}
