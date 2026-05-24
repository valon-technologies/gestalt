package s3

import (
	"context"
	"time"

	s3 "github.com/valon-technologies/gestalt/sdk/go/s3"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
)

// Options configures an S3 gRPC client.
type Options struct {
	UnaryTimeout time.Duration
}

// NewClient returns an s3.Client backed by existing gRPC stubs.
func NewClient(grpcClient proto.S3Client, objectAccess proto.S3ObjectAccessClient, opts Options) s3.Client {
	return &rpcClient{
		grpc:               grpcClient,
		objectAccessClient: objectAccess,
		opts:               opts,
	}
}

// NewConn builds an s3.Client from an existing connection.
func NewConn(conn grpc.ClientConnInterface, opts Options) s3.Client {
	return NewClient(proto.NewS3Client(conn), proto.NewS3ObjectAccessClient(conn), opts)
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
