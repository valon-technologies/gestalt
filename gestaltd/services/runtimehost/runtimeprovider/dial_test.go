package runtimeprovider

import (
	"context"
	"net"
	"os"
	"path/filepath"
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

	if _, err := conn.Integration().GetMetadata(context.Background(), &emptypb.Empty{}); err != nil {
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

// TestDialHostedAppReceivesResponseLargerThan4MiB exercises the hosted-app gRPC
// client receive cap. The gRPC default of 4 MiB rejects responses produced by
// real-world provider operations such as Slack files.get, which observed in
// production at roughly 5-7 MB. The runtimeprovider client therefore raises
// MaxCallRecvMsgSize via hostedAppCallOptions; this test pins that behavior by
// returning a payload larger than 4 MiB but smaller than the new 64 MiB cap.
func TestDialHostedAppReceivesResponseLargerThan4MiB(t *testing.T) {
	t.Parallel()

	const providerName = "hosted-large-response"
	// 6 MiB matches the observed Slack files.get payload size and exceeds the
	// default 4 MiB gRPC client receive cap.
	const payloadSize = 6 * 1024 * 1024
	largeBody := make([]byte, payloadSize)

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

	stub := &coretesting.StubIntegration{
		N: providerName,
		ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
			return &core.OperationResult{Status: 200, Body: largeBody}, nil
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

	result, err := conn.Integration().Execute(context.Background(), &proto.ExecuteRequest{Operation: "large"})
	if err != nil {
		t.Fatalf("Execute returned error for %d-byte response (this is the regression the hosted-app recv cap bump prevents): %v", payloadSize, err)
	}
	if got := len(result.GetBody()); got != payloadSize {
		t.Fatalf("Execute response body size = %d, want %d", got, payloadSize)
	}
}
