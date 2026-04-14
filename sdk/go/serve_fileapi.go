package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
)

// ServeFileAPIProvider starts a gRPC server for a [FileAPIProvider].
func ServeFileAPIProvider(ctx context.Context, provider FileAPIProvider) error {
	return serveProvider(withProviderCloser(ctx, provider), func(srv *grpc.Server) {
		proto.RegisterProviderLifecycleServer(srv, newRuntimeServer(ProviderKindFileAPI, provider))
		proto.RegisterFileAPIServer(srv, provider)
	})
}
