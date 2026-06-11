package authorization

import (
	"context"
	"time"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/providergateway"
	"google.golang.org/grpc"
)

type Options struct {
	UnaryTimeout time.Duration
	ProviderID   string
}

func NewClient(grpcClient proto.AuthorizationClient, opts Options, gateway providergateway.ProviderGateway) *Client {
	return &Client{grpc: grpcClient, opts: opts, gateway: gateway}
}

func NewConn(conn grpc.ClientConnInterface, opts Options, gateway providergateway.ProviderGateway) *Client {
	return NewClient(proto.NewAuthorizationClient(conn), opts, gateway)
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
