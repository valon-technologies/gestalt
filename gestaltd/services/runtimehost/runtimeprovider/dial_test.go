package runtimeprovider

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/testutil/metrictest"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	appservice "github.com/valon-technologies/gestalt/server/services/apps"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestDialHostedAppRecordsRPCClientDurationWithTelemetryAttrs(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	const providerName = "hosted-metrics"

	dir, err := runtimehost.NewPluginTempDir("grpc-metrics-")
	if err != nil {
		t.Fatalf("NewPluginTempDir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "app.sock")
	lis, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	srv := grpc.NewServer()
	proto.RegisterAppProviderServer(srv, appservice.NewProviderServer(&coretesting.StubIntegration{N: providerName}))
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(lis)
	}()
	t.Cleanup(func() {
		srv.Stop()
		_ = lis.Close()
		<-errCh
	})

	conn, err := DialHostedApp(context.Background(), "unix://"+socket,
		WithProviderName(providerName),
		WithMeterProvider(metrics.Provider),
	)
	if err != nil {
		t.Fatalf("DialHostedApp: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	if _, err := proto.NewAppProviderClient(conn.Conn()).GetMetadata(context.Background(), &emptypb.Empty{}); err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}

	rm := metrictest.CollectMetrics(t, metrics.Reader)
	attrs := map[string]string{
		"gestaltd.rpc.role":      "hosted_app_client",
		"gestaltd.provider.name": providerName,
	}
	metrictest.RequireFloat64Histogram(t, rm, "rpc.client.call.duration", attrs)
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "rpc.client.call.duration", attrs, "gestalt.rpc.role")
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "rpc.client.call.duration", attrs, "gestalt.provider")
}

// TestDialHostedAppAcceptsResponsesLargerThanGRPCDefault verifies that the
// hosted-app gRPC client raises its receive cap above grpc-go's 4 MiB default.
// Pre-fix, a hosted-app Execute response larger than 4 MiB would fail with
// `rpc error: code = ResourceExhausted desc = grpc: received message larger
// than max`, which was the source of the BigQuery query 502s on
// /api/v1/{integration}/{operation}. The payload size here (8 MiB) is twice the
// grpc-go default cap, so the call would fail without the fix and succeeds
// with it.
func TestDialHostedAppAcceptsResponsesLargerThanGRPCDefault(t *testing.T) {
	t.Parallel()

	const providerName = "hosted-large-response"
	// 8 MiB is comfortably above the grpc-go default 4 MiB client receive cap
	// while staying well under the 64 MiB hosted-app cap, so this exercises the
	// fix without exhausting test runner memory.
	const payloadSize = 8 * 1024 * 1024

	dir, err := runtimehost.NewPluginTempDir("grpc-large-")
	if err != nil {
		t.Fatalf("NewPluginTempDir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "app.sock")
	lis, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}

	body := strings.Repeat("a", payloadSize)
	stub := &coretesting.StubIntegration{
		N: providerName,
		ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
			return &core.OperationResult{Status: 200, Body: body}, nil
		},
	}
	srv := grpc.NewServer()
	proto.RegisterAppProviderServer(srv, appservice.NewProviderServer(stub))
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(lis)
	}()
	t.Cleanup(func() {
		srv.Stop()
		_ = lis.Close()
		<-errCh
	})

	conn, err := DialHostedApp(context.Background(), "unix://"+socket, WithProviderName(providerName))
	if err != nil {
		t.Fatalf("DialHostedApp: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	res, err := conn.Integration().Execute(context.Background(), &proto.ExecuteRequest{Operation: "noop"})
	if err != nil {
		t.Fatalf("Execute with %d-byte response body: %v", payloadSize, err)
	}
	if got := len(res.GetBody()); got != payloadSize {
		t.Fatalf("expected response body of %d bytes, got %d", payloadSize, got)
	}
}
