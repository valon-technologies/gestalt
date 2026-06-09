package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
)

// ServeSecretsProvider starts a gRPC server for a [SecretsProvider].
func ServeSecretsProvider(ctx context.Context, secrets SecretsProvider) error {
	return serveProvider(withProviderCloser(ctx, secrets), func(srv *grpc.Server) {
		proto.RegisterProviderLifecycleServer(srv, newRuntimeServer(ProviderKindSecrets, secrets))
		proto.RegisterSecretsServer(srv, newSecretsProviderServer(secrets))
	})
}
