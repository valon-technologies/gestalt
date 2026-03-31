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

func newProviderPluginClient(t *testing.T, server proto.ProviderPluginServer) proto.ProviderPluginClient {
	t.Helper()

	conn := newBufconnConn(t, func(srv *grpc.Server) {
		proto.RegisterProviderPluginServer(srv, server)
	})
	return proto.NewProviderPluginClient(conn)
}

func newRuntimePluginClient(t *testing.T, server proto.RuntimePluginServer) proto.RuntimePluginClient {
	t.Helper()

	conn := newBufconnConn(t, func(srv *grpc.Server) {
		proto.RegisterRuntimePluginServer(srv, server)
	})
	return proto.NewRuntimePluginClient(conn)
}

func newRuntimeHostClient(t *testing.T, server proto.RuntimeHostServer) proto.RuntimeHostClient {
	t.Helper()

	conn := newBufconnConn(t, func(srv *grpc.Server) {
		proto.RegisterRuntimeHostServer(srv, server)
	})
	return proto.NewRuntimeHostClient(conn)
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
