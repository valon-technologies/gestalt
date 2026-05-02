package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	"google.golang.org/grpc"
)

// ServeAgentProvider starts a gRPC server for an [AgentProvider].
func ServeAgentProvider(ctx context.Context, provider AgentProvider) error {
	return serveProvider(withProviderCloser(ctx, provider), func(srv *grpc.Server) {
		proto.RegisterProviderLifecycleServer(srv, newRuntimeServer(ProviderKindAgent, provider))
		proto.RegisterAgentProviderServer(srv, provider)
	})
}
