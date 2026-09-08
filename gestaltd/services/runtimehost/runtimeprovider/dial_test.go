package runtimeprovider

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

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

func TestDialHostedAppReceivesLargeProviderResponse(t *testing.T) {
	t.Parallel()

	// This matches the 4,438,910-byte production response generated while
	// reading the 3,312,204-byte Bradley workbook after encoding and wrapping.
	const providerResponseBodyBytes = 4_438_910
	const providerName = "large-response"

	dir, err := runtimehost.NewPluginTempDir("grpc-large-response-")
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
	proto.RegisterAppProviderServer(srv, appservice.NewProviderServer(&coretesting.StubIntegration{
		N: providerName,
		ExecuteFn: func(context.Context, string, map[string]any, string) (*core.OperationResult, error) {
			return &core.OperationResult{
				Status: http.StatusOK,
				Body:   make([]byte, providerResponseBodyBytes),
			}, nil
		},
	}))
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(lis)
	}()
	t.Cleanup(func() {
		srv.Stop()
		_ = lis.Close()
		<-errCh
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := DialHostedApp(ctx, "unix://"+socket, WithProviderName(providerName))
	if err != nil {
		t.Fatalf("DialHostedApp: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	resp, err := proto.NewAppProviderClient(conn.Conn()).Execute(ctx, &proto.ExecuteRequest{})
	if err != nil {
		t.Fatalf("execute large provider response: %v", err)
	}
	if got := len(resp.GetBody()); got != providerResponseBodyBytes {
		t.Fatalf("provider response body length = %d, want %d", got, providerResponseBodyBytes)
	}
}
