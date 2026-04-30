package plugins

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/egress"
	"github.com/valon-technologies/gestalt/server/internal/metricutil"
	plugininvokerservice "github.com/valon-technologies/gestalt/server/services/plugininvoker"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
)

type ExecConfig struct {
	Command          string
	Args             []string
	Env              map[string]string
	StaticSpec       StaticProviderSpec
	Config           map[string]any
	Egress           egress.Policy
	HostBinary       string
	Cleanup          func()
	HostServices     []runtimehost.HostService
	InvocationTokens *plugininvokerservice.InvocationTokenManager
	InvocationGrants plugininvokerservice.InvocationGrants
	ProviderName     string
	Telemetry        metricutil.TelemetryProviders
}

func NewExecutable(ctx context.Context, cfg ExecConfig) (core.Provider, error) {
	process, err := runtimehost.StartPluginProcess(ctx, cfg.processConfig())
	if err != nil {
		return nil, err
	}

	opts := []RemoteProviderOption{WithCloser(process)}
	if cfg.InvocationTokens != nil {
		opts = append(opts,
			WithInvocationTokens(cfg.InvocationTokens),
			WithInvocationTokenSubject(cfg.StaticSpec.Name, cfg.InvocationGrants),
		)
	}
	prov, err := NewRemote(
		ctx,
		process.Integration(),
		cfg.StaticSpec,
		cfg.Config,
		opts...,
	)
	if err != nil {
		_ = process.Close()
		return nil, err
	}
	return prov, nil
}

func (c ExecConfig) processConfig() runtimehost.ProcessConfig {
	return runtimehost.ProcessConfig{
		Command:      c.Command,
		Args:         c.Args,
		Env:          c.Env,
		Egress:       cloneEgressPolicy(c.Egress),
		HostBinary:   c.HostBinary,
		Cleanup:      c.Cleanup,
		HostServices: c.HostServices,
		ProviderName: firstNonBlank(c.ProviderName, c.StaticSpec.Name),
		Telemetry:    c.Telemetry,
	}
}

func NewPluginTempDir(pattern string) (string, error) {
	return runtimehost.NewPluginTempDir(pattern)
}

func cloneEgressPolicy(policy egress.Policy) egress.Policy {
	return egress.Policy{
		AllowedHosts:  append([]string(nil), policy.AllowedHosts...),
		DefaultAction: policy.DefaultAction,
	}
}

func ServeProvider(ctx context.Context, provider core.Provider) error {
	return serveProvider(ctx, func(srv *grpc.Server) {
		proto.RegisterIntegrationProviderServer(srv, NewProviderServer(provider))
	})
}

func serveProvider(ctx context.Context, register func(*grpc.Server)) error {
	socket := os.Getenv(proto.EnvProviderSocket)
	if socket == "" {
		return fmt.Errorf("%s is required", proto.EnvProviderSocket)
	}
	if err := os.Remove(socket); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale socket %q: %w", socket, err)
	}

	lis, err := net.Listen("unix", socket)
	if err != nil {
		return fmt.Errorf("listen on plugin socket %q: %w", socket, err)
	}
	defer func() {
		_ = lis.Close()
		_ = os.Remove(socket)
	}()

	providerName := strings.TrimSpace(os.Getenv(proto.EnvProviderName))
	srv := grpc.NewServer(grpc.StatsHandler(otelgrpc.NewServerHandler(providerServerGRPCOptions(providerName, nil)...)))
	register(srv)

	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		<-ctx.Done()
		srv.GracefulStop()
	}()

	err = srv.Serve(lis)
	if ctx.Err() != nil {
		<-stopped
		return nil
	}
	return err
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
