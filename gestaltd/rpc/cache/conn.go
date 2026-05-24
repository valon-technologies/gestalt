package cache

import (
	"context"
	"time"

	sdkcache "github.com/valon-technologies/gestalt/sdk/go/cache"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/durationpb"
)

type Options struct {
	UnaryTimeout time.Duration
}

// NewClient wraps a generated cache gRPC client as the SDK cache contract.
func NewClient(grpcClient proto.CacheClient, opts Options) sdkcache.Runtime {
	return &rpcClient{grpc: grpcClient, opts: opts}
}

// NewConn builds a cache client from a gRPC connection.
func NewConn(conn grpc.ClientConnInterface, opts Options) sdkcache.Runtime {
	return NewClient(proto.NewCacheClient(conn), opts)
}

func ttlToProto(ttl time.Duration) *durationpb.Duration {
	if ttl <= 0 {
		return nil
	}
	return durationpb.New(ttl)
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
