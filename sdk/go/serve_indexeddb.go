package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
)

// ServeIndexedDBProvider starts a gRPC server for a [IndexedDBProvider].
func ServeIndexedDBProvider(ctx context.Context, datastore IndexedDBProvider) error {
	return serveProvider(withProviderCloser(ctx, datastore), func(srv *grpc.Server) {
		proto.RegisterProviderLifecycleServer(srv, newRuntimeServer(ProviderKindDatastore, datastore))
		proto.RegisterIndexedDBServer(srv, datastore)
	})
}
