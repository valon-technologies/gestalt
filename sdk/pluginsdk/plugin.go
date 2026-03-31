package pluginsdk

import (
	"context"
	"fmt"
	"net"
	"os"

	pluginapiv1 "github.com/valon-technologies/gestalt/sdk/pluginapi/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func ServeProvider(ctx context.Context, provider Provider) error {
	return servePlugin(ctx, func(srv *grpc.Server) {
		pluginapiv1.RegisterProviderPluginServer(srv, NewProviderServer(provider))
	})
}

func ServeRuntime(ctx context.Context, runtime Runtime) error {
	return servePlugin(ctx, func(srv *grpc.Server) {
		pluginapiv1.RegisterRuntimePluginServer(srv, NewRuntimeServer(runtime))
	})
}

func servePlugin(ctx context.Context, register func(*grpc.Server)) error {
	socket := os.Getenv(pluginapiv1.EnvPluginSocket)
	if socket == "" {
		return fmt.Errorf("%s is required", pluginapiv1.EnvPluginSocket)
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
