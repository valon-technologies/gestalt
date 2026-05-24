package appruntime

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"

	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/internal/testutil/metrictest"
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
		"gestaltd.rpc.role":      "hosted_plugin_client",
		"gestaltd.provider.name": providerName,
	}
	metrictest.RequireFloat64Histogram(t, rm, "rpc.client.call.duration", attrs)
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "rpc.client.call.duration", attrs, "gestalt.rpc.role")
	metrictest.RequireFloat64HistogramOmitsAttr(t, rm, "rpc.client.call.duration", attrs, "gestalt.provider")
}
