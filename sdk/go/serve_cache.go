package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
)

// ServeCacheProvider starts a gRPC server for a [CacheProvider].
func ServeCacheProvider(ctx context.Context, cache CacheProvider) error {
	return serveProvider(withProviderCloser(ctx, cache), func(srv *grpc.Server) {
		proto.RegisterProviderLifecycleServer(srv, newRuntimeServer(ProviderKindCache, cache))
		proto.RegisterCacheServer(srv, cache)
	})
}
