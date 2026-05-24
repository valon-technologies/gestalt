package s3

import (
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
