package providerhost

import (
	"context"
	"net"
	"testing"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/internal/testutil/metrictest"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	nooptrace "go.opentelemetry.io/otel/trace/noop"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type hostServiceMetricsTelemetry struct {
	meterProvider metric.MeterProvider
}

func (t hostServiceMetricsTelemetry) MeterProvider() metric.MeterProvider {
	return t.meterProvider
}

func (hostServiceMetricsTelemetry) TracerProvider() trace.TracerProvider {
	return nooptrace.NewTracerProvider()
}

type metricsCacheServer struct {
	proto.UnimplementedCacheServer
}

func (metricsCacheServer) Get(context.Context, *proto.CacheGetRequest) (*proto.CacheGetResponse, error) {
	return &proto.CacheGetResponse{Found: true, Value: []byte("ok")}, nil
}

func TestStartHostServicesRecordsRPCServerDurationWithTelemetryAttrs(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	const providerName = "metrics-plugin"

	hostServices, err := StartHostServices([]HostService{{
		Name:   "cache",
		EnvVar: "GESTALT_TEST_CACHE_SOCKET",
		Register: func(srv *grpc.Server) {
			proto.RegisterCacheServer(srv, metricsCacheServer{})
		},
	}},
		WithHostServicesProviderName(providerName),
		WithHostServicesTelemetry(hostServiceMetricsTelemetry{meterProvider: metrics.Provider}),
	)
	if err != nil {
		t.Fatalf("StartHostServices: %v", err)
	}
	t.Cleanup(func() { _ = hostServices.Close() })

	bindings := hostServices.Bindings()
	if len(bindings) != 1 {
		t.Fatalf("host service bindings len = %d, want 1", len(bindings))
	}

	conn, err := grpc.NewClient("passthrough:///host-service",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithAuthority("localhost"),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", bindings[0].SocketPath)
		}),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	resp, err := proto.NewCacheClient(conn).Get(context.Background(), &proto.CacheGetRequest{Key: "hello"})
	if err != nil {
		t.Fatalf("Cache.Get: %v", err)
	}
	if !resp.GetFound() {
		t.Fatal("Cache.Get found = false, want true")
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	metrictest.RequireFloat64Histogram(t, rm, "rpc.server.call.duration", map[string]string{
		"gestalt.rpc.role":     "host_service_server",
		"gestalt.provider":     providerName,
		"gestalt.host_service": "cache",
	})
}
