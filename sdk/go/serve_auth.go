package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
)

// ServeAuthenticationProvider starts a gRPC server for an
// [AuthenticationProvider].
func ServeAuthenticationProvider(ctx context.Context, auth AuthenticationProvider) error {
	return serveProvider(withProviderCloser(ctx, auth), func(srv *grpc.Server) {
		server := newAuthenticationProviderServer(auth)
		proto.RegisterProviderLifecycleServer(srv, newRuntimeServer(ProviderKindAuthentication, auth))
		proto.RegisterAuthenticationProviderServer(srv, server)
		proto.RegisterAuthProviderServer(srv, server)
	})
}

// ServeAuthProvider is a deprecated compatibility wrapper for
// [ServeAuthenticationProvider].
func ServeAuthProvider(ctx context.Context, auth AuthProvider) error {
	return ServeAuthenticationProvider(ctx, auth)
}
