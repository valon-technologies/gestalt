package gestalt

import (
	"context"

	"github.com/valon-technologies/gestalt/sdk/go/client"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
)

// ServeAuthenticationProvider starts a gRPC server for an
// [AuthenticationProvider].
func ServeAuthenticationProvider(ctx context.Context, auth AuthenticationProvider) error {
	return serveProvider(withProviderCloser(ctx, auth), func(srv *grpc.Server) {
		proto.RegisterProviderLifecycleServer(srv, newRuntimeServer(ProviderKindAuthentication, auth))
		proto.RegisterAuthenticationServer(srv, client.NewAuthenticationProviderServer(authenticationHandler{auth: auth}))
	})
}
