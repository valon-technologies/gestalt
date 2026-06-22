package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
)

// ServeIdentityProvider starts a gRPC server for an [IdentityProvider].
func ServeIdentityProvider(ctx context.Context, identity IdentityProvider) error {
	return serveProvider(withProviderCloser(ctx, identity), func(srv *grpc.Server) {
		server := newIdentityProviderServer(identity)
		proto.RegisterProviderLifecycleServer(srv, newRuntimeServer(ProviderKindIdentity, identity))
		proto.RegisterIdentityServer(srv, server)
	})
}
