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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

// TestDialHostedAppAcceptsLargeProviderPayloads verifies that hosted-provider
// dials lift gRPC's default 4 MiB per-call message-size cap so that payloads
// larger than 4 MiB (such as Slack's base64-encoded files.get bodies up to
// ~6.7 MiB) flow through without surfacing as ResourceExhausted to callers.
// Regression test for the gestalt.operation.error_count spike on
// gestalt.operation:files.get,gestalt.provider:slack observed in production.
func TestDialHostedAppAcceptsLargeProviderPayloads(t *testing.T) {
	t.Parallel()

	const providerName = "hosted-large-payload"

	for _, tc := range []struct {
		name string
		size int
	}{
		{name: "1MiB", size: 1 * 1024 * 1024},
		{name: "6MiB_above_default_grpc_cap", size: 6 * 1024 * 1024},
		{name: "16MiB", size: 16 * 1024 * 1024},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			body := make([]byte, tc.size)
			for i := range body {
				body[i] = byte(i % 251)
			}

			stub := &coretesting.StubIntegration{
				N: providerName,
				ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
					return &core.OperationResult{Status: 200, Body: body}, nil
				},
			}

			dir, err := runtimehost.NewPluginTempDir("grpc-large-payload-")
			if err != nil {
				t.Fatalf("NewPluginTempDir: %v", err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(dir) })
			socket := filepath.Join(dir, "app.sock")
			lis, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatalf("listen unix: %v", err)
			}
			// The server side must also be willing to send messages larger
			// than the gRPC default, mirroring how real hosted providers run.
			srv := grpc.NewServer(
				grpc.MaxRecvMsgSize(hostedGRPCMaxMessageBytes),
				grpc.MaxSendMsgSize(hostedGRPCMaxMessageBytes),
			)
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

			conn, err := DialHostedApp(context.Background(), "unix://"+socket,
				WithProviderName(providerName),
			)
			if err != nil {
				t.Fatalf("DialHostedApp: %v", err)
			}
			t.Cleanup(func() { _ = conn.Close() })

			result, err := conn.Integration().Execute(context.Background(), &proto.ExecuteRequest{
				Operation: "test.large_payload",
			})
			if err != nil {
				if s, ok := status.FromError(err); ok && s.Code() == codes.ResourceExhausted {
					t.Fatalf("Execute returned ResourceExhausted for %d-byte payload; hosted dial caps not applied: %v", tc.size, err)
				}
				t.Fatalf("Execute: %v", err)
			}
			if got, want := len(result.GetBody()), tc.size; got != want {
				t.Fatalf("Execute body length = %d, want %d", got, want)
			}
		})
	}
}
