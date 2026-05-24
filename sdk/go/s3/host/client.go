package host

import (
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/protov1/v1"
	"github.com/valon-technologies/gestalt/sdk/go/s3"
	"google.golang.org/grpc"
)

// New returns an S3 client over existing gRPC stubs.
func New(client proto.S3Client, objectAccess proto.S3ObjectAccessClient) s3.Client {
	return &HostClient{client: client, objectAccessClient: objectAccess}
}

// NewConn constructs gRPC stubs from conn.
func NewConn(conn grpc.ClientConnInterface) s3.Client {
	return New(proto.NewS3Client(conn), proto.NewS3ObjectAccessClient(conn))
}

// NewProviderConn applies rpcTimeout to unary S3 RPCs.
func NewProviderConn(conn grpc.ClientConnInterface, rpcTimeout time.Duration) s3.Client {
	return &HostClient{
		client:             proto.NewS3Client(conn),
		objectAccessClient: proto.NewS3ObjectAccessClient(conn),
		rpcConfig:          rpcConfig{rpcTimeout: rpcTimeout},
	}
}
