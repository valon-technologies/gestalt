package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
)

// ServeAuthenticationProvider starts a gRPC server for an
// [AuthenticationProvider].
func ServeAuthenticationProvider(ctx context.Context, auth AuthenticationProvider) error {
	return serveProvider(withProviderCloser(ctx, auth), func(srv *grpc.Server) {
		server := newAuthenticationProviderServer(auth)
		proto.RegisterProviderLifecycleServer(srv, newRuntimeServer(ProviderKindAuthentication, auth))
		proto.RegisterAuthenticationServer(srv, server)
	})
}


// ServeIdentityProvider starts a gRPC server for an [IdentityProvider].
// It is the canonical alias for ServeAuthenticationProvider.
func ServeIdentityProvider(ctx context.Context, identity IdentityProvider) error {
	return ServeAuthenticationProvider(ctx, identity)
}
