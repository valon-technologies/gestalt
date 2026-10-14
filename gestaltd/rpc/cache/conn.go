package cache

import (
	"context"
	"time"

	sdkcache "github.com/valon-technologies/gestalt/sdk/go/cache"
	"github.com/valon-technologies/gestalt/server/rpc/internal/contextutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/durationpb"
)

type Options struct {
	UnaryTimeout time.Duration
}

// NewClient wraps a generated cache gRPC client as the SDK cache contract.
func NewClient(grpcClient proto.CacheClient, opts Options) sdkcache.Cache {
	return &rpcClient{grpc: grpcClient, opts: opts}
}

// NewConn builds a cache client from a gRPC connection.
func NewConn(conn grpc.ClientConnInterface, opts Options) sdkcache.Cache {
	return NewClient(proto.NewCacheClient(conn), opts)
}

func ttlToProto(ttl time.Duration) *durationpb.Duration {
	if ttl <= 0 {
		return nil
	}
	return durationpb.New(ttl)
}

func (c *rpcClient) callCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return contextutil.WithOptionalTimeout(ctx, c.opts.UnaryTimeout)
}
