package authorization

import (
	"context"
	"time"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
)

type Options struct {
	UnaryTimeout time.Duration
	ProviderID   string
}

func NewClient(grpcClient proto.AuthorizationClient, opts Options) *Client {
	return &Client{grpc: grpcClient, opts: opts}
}

func NewConn(conn grpc.ClientConnInterface, opts Options) *Client {
	return NewClient(proto.NewAuthorizationClient(conn), opts)
}

func attachTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		if ctx == nil {
			return context.Background(), func() {}
		}
		return ctx, func() {}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, timeout)
}

func (c *Client) callCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return attachTimeout(ctx, c.opts.UnaryTimeout)
}
