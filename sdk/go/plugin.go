package gestalt

import (
	"context"
	"fmt"
	"net"
	"os"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
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

// ServeRuntime starts a gRPC server for the given [Runtime] on the Unix
// socket specified by the GESTALT_PLUGIN_SOCKET environment variable. It
// blocks until ctx is cancelled, at which point it drains in-flight requests
// and returns nil. This is the main entry point for runtime plugins.
func ServeRuntime(ctx context.Context, runtime Runtime) error {
	return servePlugin(ctx, func(srv *grpc.Server) {
		proto.RegisterRuntimePluginServer(srv, NewRuntimeServer(runtime))
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

func dialUnixSocket(ctx context.Context, socket string) (*grpc.ClientConn, error) {
	conn, err := grpc.NewClient(
		"passthrough:///"+socket,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", addr)
		}),
	)
	if err != nil {
		return nil, err
	}
	conn.Connect()
	return conn, nil
}
