package s3

import (
	"context"
	"time"

	s3 "github.com/valon-technologies/gestalt/sdk/go/s3"
	"github.com/valon-technologies/gestalt/server/rpc/internal/contextutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
)

// Options configures an S3 gRPC client.
type Options struct {
	UnaryTimeout time.Duration
}

// NewClient returns an s3.S3 implementation backed by existing gRPC stubs.
func NewClient(grpcClient proto.S3Client, objectAccess proto.S3ObjectAccessClient, opts Options) s3.S3 {
	return &rpcClient{
		grpc:               grpcClient,
		objectAccessClient: objectAccess,
		opts:               opts,
	}
}

// NewConn builds an s3.S3 implementation from an existing connection.
func NewConn(conn grpc.ClientConnInterface, opts Options) s3.S3 {
	return NewClient(proto.NewS3Client(conn), proto.NewS3ObjectAccessClient(conn), opts)
}

func (c *rpcClient) callCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return contextutil.WithOptionalTimeout(ctx, c.opts.UnaryTimeout)
}
