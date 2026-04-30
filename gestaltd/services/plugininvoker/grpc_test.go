package plugininvoker

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func newBufconnConn(t *testing.T, register func(*grpc.Server)) *grpc.ClientConn {
	t.Helper()
	return newBufconnConnWithOptions(t, nil, nil, register)
}

func newBufconnConnWithOptions(t *testing.T, serverOpts []grpc.ServerOption, dialOpts []grpc.DialOption, register func(*grpc.Server)) *grpc.ClientConn {
	t.Helper()

	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer(serverOpts...)
	register(srv)
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(func() {
		srv.Stop()
		_ = lis.Close()
	})

	dialOptions := []grpc.DialOption{
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}
	dialOptions = append(dialOptions, dialOpts...)
	conn, err := grpc.NewClient("passthrough:///bufnet", dialOptions...)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}
