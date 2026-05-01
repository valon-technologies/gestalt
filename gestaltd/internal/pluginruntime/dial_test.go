package pluginruntime

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/testutil/metrictest"
	pluginservice "github.com/valon-technologies/gestalt/server/services/plugins"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestValidatePlaintextTCPTargetRefusesNonLoopback(t *testing.T) {
	t.Setenv(allowInsecureRemotePluginTCPEnv, "")

	err := validatePlaintextTCPTarget("192.0.2.10:443")
	if err == nil {
		t.Fatal("expected non-loopback plaintext TCP target to be refused")
	}
	if want := allowInsecureRemotePluginTCPEnv; !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want opt-in env %s", err, want)
	}
}

func TestValidatePlaintextTCPTargetAllowsLoopback(t *testing.T) {
	t.Parallel()

	if err := validatePlaintextTCPTarget("127.0.0.1:50051"); err != nil {
		t.Fatalf("validatePlaintextTCPTarget loopback: %v", err)
	}
	if err := validatePlaintextTCPTarget("[::1]:50051"); err != nil {
		t.Fatalf("validatePlaintextTCPTarget IPv6 loopback: %v", err)
	}
}

func TestValidatePlaintextTCPTargetAllowsExplicitDevOptIn(t *testing.T) {
	t.Setenv(allowInsecureRemotePluginTCPEnv, "1")

	if err := validatePlaintextTCPTarget("192.0.2.10:443"); err != nil {
		t.Fatalf("validatePlaintextTCPTarget with opt-in: %v", err)
	}
}

func TestDialHostedPluginRecordsRPCClientDurationWithTelemetryAttrs(t *testing.T) {
	t.Parallel()

	metrics := metrictest.NewManualMeterProvider(t)
	const providerName = "hosted-metrics"

	dir, err := runtimehost.NewPluginTempDir("grpc-metrics-")
	if err != nil {
		t.Fatalf("NewPluginTempDir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "plugin.sock")
	lis, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	srv := grpc.NewServer()
	proto.RegisterIntegrationProviderServer(srv, pluginservice.NewProviderServer(&coretesting.StubIntegration{N: providerName}))
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(lis)
	}()
	t.Cleanup(func() {
		srv.Stop()
		_ = lis.Close()
		<-errCh
	})

	conn, err := DialHostedPlugin(context.Background(), "unix://"+socket,
		WithProviderName(providerName),
		WithMeterProvider(metrics.Provider),
	)
	if err != nil {
		t.Fatalf("DialHostedPlugin: %v", err)
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
