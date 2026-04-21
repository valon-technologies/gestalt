package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
)

// ServePluginRuntimeProvider starts a gRPC server for a [PluginRuntimeProvider].
func ServePluginRuntimeProvider(ctx context.Context, provider PluginRuntimeProvider) error {
	return serveProvider(withProviderCloser(ctx, provider), func(srv *grpc.Server) {
		proto.RegisterProviderLifecycleServer(srv, newRuntimeServer(ProviderKindRuntime, provider))
		proto.RegisterPluginRuntimeProviderServer(srv, provider)
	})
}
