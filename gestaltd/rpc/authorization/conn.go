package authorization

import (
	"context"
	"time"

	sdkauthorization "github.com/valon-technologies/gestalt/sdk/go/authorization"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
)

type Options struct {
	UnaryTimeout time.Duration
}

// NewClient wraps a generated authorization gRPC client as the SDK
// authorization contract.
func NewClient(grpcClient proto.AuthorizationProviderClient, opts Options) sdkauthorization.Authorization {
	return &rpcClient{grpc: grpcClient, opts: opts}
}

// NewConn builds an authorization client from a gRPC connection.
func NewConn(conn grpc.ClientConnInterface, opts Options) sdkauthorization.Authorization {
	return NewClient(proto.NewAuthorizationProviderClient(conn), opts)
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

func (c *rpcClient) callCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return attachTimeout(ctx, c.opts.UnaryTimeout)
}
