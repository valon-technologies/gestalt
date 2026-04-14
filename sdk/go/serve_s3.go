package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
)

// ServeS3Provider starts a gRPC server for an [S3Provider].
func ServeS3Provider(ctx context.Context, provider S3Provider) error {
	return serveProvider(withProviderCloser(ctx, provider), func(srv *grpc.Server) {
		proto.RegisterProviderLifecycleServer(srv, newRuntimeServer(ProviderKindS3, provider))
		proto.RegisterS3Server(srv, provider)
	})
}
