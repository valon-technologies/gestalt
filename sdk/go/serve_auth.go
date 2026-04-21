package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
)

// ServeAuthenticationProvider starts a gRPC server for an
// [AuthenticationProvider].
func ServeAuthenticationProvider(ctx context.Context, auth AuthenticationProvider) error {
	return serveAuthenticationPlugin(ctx, auth)
}

// ServeLegacyAuthenticationProvider starts a gRPC server for a
// [LegacyAuthenticationProvider].
func ServeLegacyAuthenticationProvider(ctx context.Context, auth LegacyAuthenticationProvider) error {
	return serveAuthenticationPlugin(ctx, auth)
}

func serveAuthenticationPlugin(ctx context.Context, auth PluginProvider) error {
	return serveProvider(withProviderCloser(ctx, auth), func(srv *grpc.Server) {
		server := newAuthenticationProviderServer(auth)
		proto.RegisterProviderLifecycleServer(srv, newRuntimeServer(ProviderKindAuthentication, auth))
		proto.RegisterAuthenticationProviderServer(srv, server)
	})
}
