package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
)

// ServeExternalCredentialProvider starts a gRPC server for an
// [ExternalCredentialProvider].
func ServeExternalCredentialProvider(ctx context.Context, provider ExternalCredentialProvider) error {
	return serveProvider(withProviderCloser(ctx, provider), func(srv *grpc.Server) {
		proto.RegisterProviderLifecycleServer(srv, newRuntimeServer(ProviderKindExternalCredential, provider))
		proto.RegisterExternalCredentialProviderServer(srv, newExternalCredentialProviderServer(provider))
	})
}
