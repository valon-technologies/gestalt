package publicrpc

import (
	"context"
	"fmt"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

const inProcessBufSize = 1024 * 1024

// InProcessConn exposes a grpc.ClientConn that dispatches unary calls to server
// without binding a loopback TCP listener.
type InProcessConn struct {
	Server   *grpc.Server
	conn     *grpc.ClientConn
	listener *bufconn.Listener
}

// NewInProcessConn starts serving server on an in-memory listener and returns
// a client connection that routes through the server's public handler stack.
func NewInProcessConn(server *grpc.Server) (*InProcessConn, error) {
	if server == nil {
		return nil, fmt.Errorf("publicrpc: grpc server is required")
	}
	lis := bufconn.Listen(inProcessBufSize)
	go func() { _ = server.Serve(lis) }()
	conn, err := grpc.NewClient("passthrough:///publicrpc.inprocess",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		_ = lis.Close()
		server.Stop()
		return nil, err
	}
	return &InProcessConn{
		Server:   server,
		conn:     conn,
		listener: lis,
	}, nil
}

// ClientConn returns the in-process client connection for grpc-gateway handlers.
func (c *InProcessConn) ClientConn() *grpc.ClientConn {
	if c == nil {
		return nil
	}
	return c.conn
}

// Close stops the in-process server and client connection.
func (c *InProcessConn) Close() {
	if c == nil {
		return
	}
	if c.conn != nil {
		_ = c.conn.Close()
	}
	if c.Server != nil {
		c.Server.Stop()
	}
	if c.listener != nil {
		_ = c.listener.Close()
	}
}
