package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
)

// ServeAuthenticationProvider starts a gRPC server for an
// [AuthenticationProvider] or [LegacyAuthenticationProvider].
func ServeAuthenticationProvider(ctx context.Context, auth PluginProvider) error {
	return serveProvider(withProviderCloser(ctx, auth), func(srv *grpc.Server) {
		server := newAuthenticationProviderServer(auth)
		proto.RegisterProviderLifecycleServer(srv, newRuntimeServer(ProviderKindAuthentication, auth))
		proto.RegisterAuthenticationProviderServer(srv, server)
	})
}
