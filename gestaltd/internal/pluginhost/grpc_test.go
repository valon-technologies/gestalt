package pluginhost

import (
	"context"
	"net"
	"testing"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func newPluginProviderClient(t *testing.T, server proto.PluginProviderServer) proto.PluginProviderClient {
	t.Helper()

	conn := newBufconnConn(t, func(srv *grpc.Server) {
		proto.RegisterPluginProviderServer(srv, server)
	})
	return proto.NewPluginProviderClient(conn)
}

func newBufconnConn(t *testing.T, register func(*grpc.Server)) *grpc.ClientConn {
	t.Helper()

	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	register(srv)
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(func() {
		srv.Stop()
		_ = lis.Close()
	})

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}
