package runtimehost

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
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

type blockingCacheServer struct {
	proto.UnimplementedCacheServer
	started chan<- struct{}
	release <-chan struct{}
}

func (s blockingCacheServer) Get(context.Context, *proto.CacheGetRequest) (*proto.CacheGetResponse, error) {
	select {
	case s.started <- struct{}{}:
	default:
	}
	<-s.release
	return &proto.CacheGetResponse{Found: true, Value: []byte("done")}, nil
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
	attrs := map[string]string{
		"gestaltd.rpc.role":          "host_service_server",
		"gestaltd.provider.name":     providerName,
		"gestaltd.host_service.name": "cache",
	}
	metrictest.RequireFloat64Histogram(t, rm, "rpc.server.call.duration", attrs)
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "rpc.server.call.duration", attrs, "gestalt.rpc.role")
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "rpc.server.call.duration", attrs, "gestalt.provider")
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "rpc.server.call.duration", attrs, "gestalt.host_service")
}

func TestStartedHostServicesCloseWaitsForInflightRPC(t *testing.T) {
	t.Parallel()

	started := make(chan struct{}, 1)
	release := make(chan struct{})

	hostServices, err := StartHostServices([]HostService{{
		Name:   "cache",
		EnvVar: "GESTALT_TEST_CACHE_SOCKET",
		Register: func(srv *grpc.Server) {
			proto.RegisterCacheServer(srv, blockingCacheServer{
				started: started,
				release: release,
			})
		},
	}})
	if err != nil {
		t.Fatalf("StartHostServices: %v", err)
	}

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

	rpcErrCh := make(chan error, 1)
	go func() {
		resp, err := proto.NewCacheClient(conn).Get(context.Background(), &proto.CacheGetRequest{Key: "slow"})
		if err != nil {
			rpcErrCh <- err
			return
		}
		if !resp.GetFound() || string(resp.GetValue()) != "done" {
			rpcErrCh <- fmt.Errorf("unexpected response: found=%v value=%q", resp.GetFound(), resp.GetValue())
			return
		}
		rpcErrCh <- nil
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for in-flight RPC to start")
	}

	closeErrCh := make(chan error, 1)
	go func() {
		closeErrCh <- hostServices.Close()
	}()

	select {
	case err := <-closeErrCh:
		t.Fatalf("Close returned before in-flight RPC finished: %v", err)
	case <-time.After(150 * time.Millisecond):
	}

	close(release)

	select {
	case err := <-rpcErrCh:
		if err != nil {
			t.Fatalf("in-flight RPC failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for in-flight RPC to finish")
	}

	select {
	case err := <-closeErrCh:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for Close to return")
	}
}
