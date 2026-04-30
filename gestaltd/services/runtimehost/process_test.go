package runtimehost

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/metric"
	noopmetric "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	nooptrace "go.opentelemetry.io/otel/trace/noop"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func TestResolvePluginTempBaseDirCreatesMissingCandidate(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	missing := filepath.Join(root, "missing")

	got, err := resolvePluginTempBaseDir([]string{missing})
	if err != nil {
		t.Fatalf("resolvePluginTempBaseDir() error: %v", err)
	}
	if got != missing {
		t.Fatalf("resolvePluginTempBaseDir() = %q, want %q", got, missing)
	}
	info, err := os.Stat(missing)
	if err != nil {
		t.Fatalf("stat created directory: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("created candidate is not a directory")
	}
}

func TestResolvePluginTempBaseDirSkipsFileCandidate(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	fileCandidate := filepath.Join(root, "candidate-file")
	if err := os.WriteFile(fileCandidate, []byte("not-a-dir"), 0o644); err != nil {
		t.Fatalf("write file candidate: %v", err)
	}
	dirCandidate := filepath.Join(root, "candidate-dir")

	got, err := resolvePluginTempBaseDir([]string{fileCandidate, dirCandidate})
	if err != nil {
		t.Fatalf("resolvePluginTempBaseDir() error: %v", err)
	}
	if got != dirCandidate {
		t.Fatalf("resolvePluginTempBaseDir() = %q, want %q", got, dirCandidate)
	}
	info, err := os.Stat(dirCandidate)
	if err != nil {
		t.Fatalf("stat created directory: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("created fallback candidate is not a directory")
	}
}

func TestWaitForPluginConnWaitsForGRPCReady(t *testing.T) {
	t.Parallel()

	root, err := os.MkdirTemp("/tmp", "gstp-process-test-")
	if err != nil {
		t.Fatalf("create short temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	socket := filepath.Join(root, "plugin.sock")
	rawLis, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen raw unix socket: %v", err)
	}

	rawDone := make(chan struct{})
	go func() {
		defer close(rawDone)
		deadline := time.Now().Add(150 * time.Millisecond)
		for time.Now().Before(deadline) {
			_ = rawLis.(*net.UnixListener).SetDeadline(time.Now().Add(25 * time.Millisecond))
			conn, err := rawLis.Accept()
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}
				return
			}
			_ = conn.Close()
		}
	}()

	grpcReady := make(chan struct{})
	grpcStop := make(chan struct{})
	go func() {
		<-rawDone
		_ = rawLis.Close()
		_ = os.Remove(socket)

		grpcLis, err := net.Listen("unix", socket)
		if err != nil {
			t.Errorf("listen grpc unix socket: %v", err)
			close(grpcReady)
			return
		}
		srv := grpc.NewServer()
		healthServer := health.NewServer()
		healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
		healthpb.RegisterHealthServer(srv, healthServer)
		defer srv.Stop()
		go func() {
			_ = srv.Serve(grpcLis)
		}()
		waitForTestGRPCHealth(t, socket)
		close(grpcReady)
		<-grpcStop
	}()

	result := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		conn, err := waitForPluginConn(ctx, socket, make(chan error), ProcessConfig{})
		if conn != nil {
			_ = conn.Close()
		}
		result <- err
	}()

	select {
	case err := <-result:
		t.Fatalf("waitForPluginConn returned before grpc server was ready: %v", err)
	case <-grpcReady:
	}
	defer close(grpcStop)

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("waitForPluginConn() error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waitForPluginConn() did not return after grpc server became ready")
	}
}

func TestWaitForPluginConnReturnsProcessExitBeforeReady(t *testing.T) {
	t.Parallel()

	root, err := os.MkdirTemp("/tmp", "gstp-process-exit-test-")
	if err != nil {
		t.Fatalf("create short temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	socket := filepath.Join(root, "plugin.sock")
	rawLis, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen raw unix socket: %v", err)
	}
	t.Cleanup(func() { _ = rawLis.Close() })
	go func() {
		for {
			conn, err := rawLis.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	exitErr := errors.New("provider crashed")
	waitCh := make(chan error, 1)
	go func() {
		time.Sleep(75 * time.Millisecond)
		waitCh <- exitErr
		close(waitCh)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := waitForPluginConn(ctx, socket, waitCh, ProcessConfig{})
	if conn != nil {
		_ = conn.Close()
	}
	if err == nil {
		t.Fatal("waitForPluginConn() error = nil, want provider exit error")
	}
	if !strings.Contains(err.Error(), "plugin process exited before serving gRPC") ||
		!strings.Contains(err.Error(), "provider crashed") {
		t.Fatalf("waitForPluginConn() error = %v, want provider exit context", err)
	}
}

func TestProviderProcessEnvAddsTelemetryDefaultsWithoutOverridingProviderEnv(t *testing.T) {
	t.Parallel()

	env := providerProcessEnv(ProcessConfig{
		ProviderName: "simple",
		Telemetry:    testProviderTelemetry{},
		Env: map[string]string{
			"OTEL_SERVICE_NAME": "custom-provider-service",
			"CUSTOM":            "provider",
		},
	}, map[string]string{
		"GESTALT_PLUGIN_SOCKET": "/tmp/provider.sock",
		"CUSTOM":                "host",
	})

	if got := env["OTEL_EXPORTER_OTLP_ENDPOINT"]; got != "otel-collector:4317" {
		t.Fatalf("OTEL_EXPORTER_OTLP_ENDPOINT = %q, want host telemetry env", got)
	}
	if got := env["OTEL_SERVICE_NAME"]; got != "custom-provider-service" {
		t.Fatalf("OTEL_SERVICE_NAME = %q, want provider env override", got)
	}
	if got := env["CUSTOM"]; got != "host" {
		t.Fatalf("CUSTOM = %q, want reserved exec env override", got)
	}
}

func waitForTestGRPCHealth(t *testing.T, socket string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := grpc.NewClient(
		"passthrough:///localhost",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithAuthority("localhost"),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		}),
	)
	if err != nil {
		t.Fatalf("create grpc health client: %v", err)
	}
	defer func() { _ = conn.Close() }()
	client := healthpb.NewHealthClient(conn)
	for {
		_, err := client.Check(ctx, &healthpb.HealthCheckRequest{})
		if err == nil {
			return
		}
		if ctx.Err() != nil {
			t.Fatalf("grpc health never became ready: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type testProviderTelemetry struct{}

func (testProviderTelemetry) MeterProvider() metric.MeterProvider {
	return noopmetric.NewMeterProvider()
}

func (testProviderTelemetry) TracerProvider() trace.TracerProvider {
	return nooptrace.NewTracerProvider()
}

func (testProviderTelemetry) ProviderTelemetryEnv(providerName string) map[string]string {
	return map[string]string{
		"OTEL_EXPORTER_OTLP_ENDPOINT": "otel-collector:4317",
		"OTEL_SERVICE_NAME":           "gestalt-provider-" + providerName,
		"CUSTOM":                      "telemetry",
	}
}
