package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
)

// ServeAuthorizationProvider starts a gRPC server for an [AuthorizationProvider].
func ServeAuthorizationProvider(ctx context.Context, provider AuthorizationProvider) error {
	return serveProvider(withProviderCloser(ctx, provider), func(srv *grpc.Server) {
		proto.RegisterProviderLifecycleServer(srv, newRuntimeServer(ProviderKindAuthorization, provider))
		proto.RegisterAuthorizationProviderServer(srv, newAuthorizationProviderServer(provider))
	})
}
