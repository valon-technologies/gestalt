package gestalt

import (
	"context"
	"fmt"
	"net"
	"os"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
)

// ServeProvider starts a gRPC server for the given [Provider] on the Unix
// socket specified by the GESTALT_PLUGIN_SOCKET environment variable. It
// blocks until ctx is cancelled, at which point it drains in-flight requests
// and returns nil. This is the main entry point for provider plugins.
func ServeProvider(ctx context.Context, provider Provider) error {
	return servePlugin(ctx, func(srv *grpc.Server) {
		proto.RegisterProviderPluginServer(srv, NewProviderServer(provider))
	})
}

func servePlugin(ctx context.Context, register func(*grpc.Server)) error {
	socket := os.Getenv(proto.EnvPluginSocket)
	if socket == "" {
		return fmt.Errorf("%s is required", proto.EnvPluginSocket)
	}
	if err := os.RemoveAll(socket); err != nil {
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

	srv := grpc.NewServer()
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
