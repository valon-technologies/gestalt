package authorization

import (
	"context"
	"time"

	"github.com/valon-technologies/gestalt/server/rpc/internal/contextutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
)

type Options struct {
	UnaryTimeout time.Duration
}

func NewClient(grpcClient proto.AuthorizationClient, opts Options) *Client {
	return &Client{grpc: grpcClient, opts: opts}
}

func NewConn(conn grpc.ClientConnInterface, opts Options) *Client {
	return NewClient(proto.NewAuthorizationClient(conn), opts)
}

func (c *Client) callCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return contextutil.WithOptionalTimeout(ctx, c.opts.UnaryTimeout)
}
