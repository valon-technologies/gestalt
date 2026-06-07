package runtimeprovider

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

type dialedHostedAppConn struct {
	conn      *grpc.ClientConn
	lifecycle proto.ProviderLifecycleClient
	app       proto.AppProviderClient
}

type dialedHostedAgentConn struct {
	conn      *grpc.ClientConn
	lifecycle proto.ProviderLifecycleClient
	agent     proto.AgentProviderClient
}

type dialedHostedWorkflowConn struct {
	conn      *grpc.ClientConn
	lifecycle proto.ProviderLifecycleClient
	workflow  proto.WorkflowProviderClient
}

type DialOption func(*dialConfig)

type dialConfig struct {
	providerName   string
	telemetry      metricutil.TelemetryProviders
	meterProvider  metric.MeterProvider
	tracerProvider trace.TracerProvider
}

type dialTelemetryProviders struct {
	meterProvider  metric.MeterProvider
	tracerProvider trace.TracerProvider
}

const hostedGRPCReadyTimeout = 30 * time.Second

// maxHostedAppGRPCMsgSize bounds the size of gRPC messages exchanged with
// hosted-app providers. The grpc-go default client receive cap is 4 MiB,
// which truncates large provider responses (e.g. BigQuery query results)
// with ResourceExhausted. 64 MiB matches the largest payloads we have
// observed in production with headroom; revisit if providers begin
// returning larger responses.
const maxHostedAppGRPCMsgSize = 64 * 1024 * 1024

func (p dialTelemetryProviders) MeterProvider() metric.MeterProvider {
	return p.meterProvider
}

func (p dialTelemetryProviders) TracerProvider() trace.TracerProvider {
	return p.tracerProvider
}

func WithProviderName(name string) DialOption {
	return func(cfg *dialConfig) {
		cfg.providerName = strings.TrimSpace(name)
	}
}

func WithTelemetry(telemetry metricutil.TelemetryProviders) DialOption {
	return func(cfg *dialConfig) {
		cfg.telemetry = telemetry
	}
}

func WithMeterProvider(provider metric.MeterProvider) DialOption {
	return func(cfg *dialConfig) {
		cfg.meterProvider = provider
	}
}

func WithTracerProvider(provider trace.TracerProvider) DialOption {
	return func(cfg *dialConfig) {
		cfg.tracerProvider = provider
	}
}

func DialHostedApp(ctx context.Context, target string, opts ...DialOption) (HostedAppConn, error) {
	conn, err := dialHostedConn(ctx, target, opts...)
	if err != nil {
		return nil, err
	}
	return &dialedHostedAppConn{
		conn:      conn,
		lifecycle: proto.NewProviderLifecycleClient(conn),
		app:       proto.NewAppProviderClient(conn),
	}, nil
}

func DialHostedAgent(ctx context.Context, target string, opts ...DialOption) (HostedAgentConn, error) {
	conn, err := dialHostedConn(ctx, target, opts...)
	if err != nil {
		return nil, err
	}
	return &dialedHostedAgentConn{
		conn:      conn,
		lifecycle: proto.NewProviderLifecycleClient(conn),
		agent:     proto.NewAgentProviderClient(conn),
	}, nil
}

func DialHostedWorkflow(ctx context.Context, target string, opts ...DialOption) (HostedWorkflowConn, error) {
	conn, err := dialHostedConn(ctx, target, opts...)
	if err != nil {
		return nil, err
	}
	return &dialedHostedWorkflowConn{
		conn:      conn,
		lifecycle: proto.NewProviderLifecycleClient(conn),
		workflow:  proto.NewWorkflowProviderClient(conn),
	}, nil
}

func dialHostedConn(ctx context.Context, target string, opts ...DialOption) (*grpc.ClientConn, error) {
	var cfg dialConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	network, address, err := dialTarget(target)
	if err != nil {
		return nil, err
	}
	var conn *grpc.ClientConn
	switch network {
	case "unix":
		conn, err = dialUnixTarget(ctx, address, cfg)
	case "tcp":
		conn, err = dialTCPTarget(address, cfg)
	case "tls":
		conn, err = dialTLSTarget(address, cfg)
	default:
		err = fmt.Errorf("unsupported hosted app dial network %q", network)
	}
	if err != nil {
		return nil, err
	}
	if err := waitForHostedGRPCReady(ctx, conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func waitForHostedGRPCReady(ctx context.Context, conn *grpc.ClientConn) error {
	if ctx == nil {
		ctx = context.Background()
	}
	readyCtx, cancel := context.WithTimeout(ctx, hostedGRPCReadyTimeout)
	defer cancel()
	for {
		state := conn.GetState()
		if state == connectivity.Ready {
			return nil
		}
		if state == connectivity.Shutdown {
			return fmt.Errorf("hosted app gRPC connection shut down before ready")
		}

		waitCtx, cancel := context.WithTimeout(readyCtx, 25*time.Millisecond)
		changed := conn.WaitForStateChange(waitCtx, state)
		cancel()
		if !changed && readyCtx.Err() != nil {
			return fmt.Errorf("waiting for hosted app gRPC ready: %w", readyCtx.Err())
		}
	}
}

func (c *dialedHostedAppConn) Lifecycle() proto.ProviderLifecycleClient {
	if c == nil {
		return nil
	}
	return c.lifecycle
}

func (c *dialedHostedAppConn) Integration() proto.AppProviderClient {
	if c == nil {
		return nil
	}
	return c.app
}

func (c *dialedHostedAppConn) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *dialedHostedAgentConn) Lifecycle() proto.ProviderLifecycleClient {
	if c == nil {
		return nil
	}
	return c.lifecycle
}

func (c *dialedHostedAgentConn) Agent() proto.AgentProviderClient {
	if c == nil {
		return nil
	}
	return c.agent
}

func (c *dialedHostedAgentConn) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *dialedHostedWorkflowConn) Lifecycle() proto.ProviderLifecycleClient {
	if c == nil {
		return nil
	}
	return c.lifecycle
}

func (c *dialedHostedWorkflowConn) Workflow() proto.WorkflowProviderClient {
	if c == nil {
		return nil
	}
	return c.workflow
}

func (c *dialedHostedWorkflowConn) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func dialTarget(raw string) (network string, address string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", fmt.Errorf("hosted app dial target is required")
	}
	if strings.HasPrefix(raw, "tcp://") {
		address = strings.TrimSpace(strings.TrimPrefix(raw, "tcp://"))
		if address == "" {
			return "", "", fmt.Errorf("hosted app tcp dial target %q is missing host:port", raw)
		}
		return "tcp", address, nil
	}
	if strings.HasPrefix(raw, "tls://") {
		address = strings.TrimSpace(strings.TrimPrefix(raw, "tls://"))
		if address == "" {
			return "", "", fmt.Errorf("hosted app tls dial target %q is missing host:port", raw)
		}
		return "tls", address, nil
	}
	if strings.HasPrefix(raw, "unix://") {
		address = strings.TrimSpace(strings.TrimPrefix(raw, "unix://"))
		if address == "" {
			return "", "", fmt.Errorf("hosted app unix dial target %q is missing a socket path", raw)
		}
		return "unix", address, nil
	}
	if strings.Contains(raw, "://") {
		parsed, parseErr := url.Parse(raw)
		if parseErr != nil {
			return "", "", fmt.Errorf("parse hosted app dial target %q: %w", raw, parseErr)
		}
		return "", "", fmt.Errorf("unsupported hosted app dial target scheme %q", parsed.Scheme)
	}
	return "unix", filepath.Clean(raw), nil
}

func dialUnixTarget(ctx context.Context, socket string, cfg dialConfig) (*grpc.ClientConn, error) {
	conn, err := grpc.NewClient(
		"passthrough:///localhost",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithAuthority("localhost"),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		}),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler(hostedAppGRPCOptions(cfg)...)),
		hostedAppCallOptions(),
	)
	if err != nil {
		return nil, err
	}
	conn.Connect()
	return conn, nil
}

func dialTCPTarget(address string, cfg dialConfig) (*grpc.ClientConn, error) {
	conn, err := grpc.NewClient(
		address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler(hostedAppGRPCOptions(cfg)...)),
		hostedAppCallOptions(),
	)
	if err != nil {
		return nil, err
	}
	conn.Connect()
	return conn, nil
}

func dialTLSTarget(address string, cfg dialConfig) (*grpc.ClientConn, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("parse hosted app tls dial target %q: %w", address, err)
	}
	conn, err := grpc.NewClient(
		address,
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			MinVersion: tls.VersionTLS12,
			ServerName: host,
			NextProtos: []string{"h2"},
		})),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler(hostedAppGRPCOptions(cfg)...)),
		hostedAppCallOptions(),
	)
	if err != nil {
		return nil, err
	}
	conn.Connect()
	return conn, nil
}

func hostedAppGRPCOptions(cfg dialConfig) []otelgrpc.Option {
	return metricutil.GRPCMetricOptions(cfg.telemetryProviders(), metricutil.RPCMetricDims{
		Role:         metricutil.RPCRoleHostedAppClient,
		ProviderName: cfg.providerName,
	})
}

// hostedAppCallOptions returns the gRPC default call options applied to every
// hosted-app client connection. Bumping both the receive and send caps to
// maxHostedAppGRPCMsgSize replaces grpc-go's 4 MiB client receive default,
// which was rejecting legitimate large provider responses with
// ResourceExhausted. Raising the send cap is defensive: it has no effect on
// today's request shapes but avoids the inverse failure mode if a future
// provider streams a large request body.
func hostedAppCallOptions() grpc.DialOption {
	return grpc.WithDefaultCallOptions(
		grpc.MaxCallRecvMsgSize(maxHostedAppGRPCMsgSize),
		grpc.MaxCallSendMsgSize(maxHostedAppGRPCMsgSize),
	)
}

func (cfg dialConfig) telemetryProviders() metricutil.TelemetryProviders {
	if cfg.telemetry != nil {
		return cfg.telemetry
	}
	if cfg.meterProvider == nil && cfg.tracerProvider == nil {
		return nil
	}
	return dialTelemetryProviders{
		meterProvider:  cfg.meterProvider,
		tracerProvider: cfg.tracerProvider,
	}
}
