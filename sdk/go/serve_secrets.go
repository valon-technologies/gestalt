package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
)

// ServeSecretsProvider starts a gRPC server for a [SecretsProvider].
func ServeSecretsProvider(ctx context.Context, secrets SecretsProvider) error {
	return servePlugin(withPluginCloser(ctx, secrets), func(srv *grpc.Server) {
		proto.RegisterProviderLifecycleServer(srv, newPluginProviderServer(ProviderKindSecrets, secrets))
		proto.RegisterSecretsProviderServer(srv, newSecretsProviderServer(secrets))
	})
}
