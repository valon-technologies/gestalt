package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
)

func serveDatastoreProvider(ctx context.Context, datastore DatastoreProvider) error {
	return serveProvider(withProviderCloser(ctx, datastore), func(srv *grpc.Server) {
		proto.RegisterProviderLifecycleServer(srv, newRuntimeServer(ProviderKindDatastore, datastore))
		proto.RegisterIndexedDBServer(srv, datastore)
	})
}

// ServeDatastoreProvider starts a gRPC server for a [DatastoreProvider].
func ServeDatastoreProvider(ctx context.Context, datastore DatastoreProvider) error {
	return serveDatastoreProvider(ctx, datastore)
}
