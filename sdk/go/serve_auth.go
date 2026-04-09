package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
)

// ServeAuthProvider starts a gRPC server for an [AuthProvider].
func ServeAuthProvider(ctx context.Context, auth AuthProvider) error {
	return serveProvider(withProviderCloser(ctx, auth), func(srv *grpc.Server) {
		proto.RegisterProviderLifecycleServer(srv, newRuntimeServer(ProviderKindAuth, auth))
		proto.RegisterAuthProviderServer(srv, newAuthProviderServer(auth))
	})
}
