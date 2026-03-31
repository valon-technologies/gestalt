package pluginhost

import (
	"context"
	"net"
	"testing"

	pluginapiv1 "github.com/valon-technologies/gestalt/sdk/pluginsdk/proto/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func newProviderPluginClient(t *testing.T, server pluginapiv1.ProviderPluginServer) pluginapiv1.ProviderPluginClient {
	t.Helper()

	conn := newBufconnConn(t, func(srv *grpc.Server) {
		pluginapiv1.RegisterProviderPluginServer(srv, server)
	})
	return pluginapiv1.NewProviderPluginClient(conn)
}

func newRuntimePluginClient(t *testing.T, server pluginapiv1.RuntimePluginServer) pluginapiv1.RuntimePluginClient {
	t.Helper()

	conn := newBufconnConn(t, func(srv *grpc.Server) {
		pluginapiv1.RegisterRuntimePluginServer(srv, server)
	})
	return pluginapiv1.NewRuntimePluginClient(conn)
}

func newRuntimeHostClient(t *testing.T, server pluginapiv1.RuntimeHostServer) pluginapiv1.RuntimeHostClient {
	t.Helper()

	conn := newBufconnConn(t, func(srv *grpc.Server) {
		pluginapiv1.RegisterRuntimeHostServer(srv, server)
	})
	return pluginapiv1.NewRuntimeHostClient(conn)
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
