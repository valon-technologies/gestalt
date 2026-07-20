package plugins

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
)

func NewPluginTempDir(pattern string) (string, error) {
	return runtimehost.NewPluginTempDir(pattern)
}

func ServeProvider(ctx context.Context, provider core.Provider) error {
	return serveProvider(ctx, func(srv *grpc.Server) {
		proto.RegisterAppProviderServer(srv, NewProviderServer(provider))
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
		return fmt.Errorf("listen on app socket %q: %w", socket, err)
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
