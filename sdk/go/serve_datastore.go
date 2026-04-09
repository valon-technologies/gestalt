package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
)

// ServeDatastoreProvider starts a gRPC server for a [DatastoreProvider].
func ServeDatastoreProvider(ctx context.Context, store DatastoreProvider) error {
	return serveProvider(withProviderCloser(ctx, store), func(srv *grpc.Server) {
		proto.RegisterProviderLifecycleServer(srv, newRuntimeServer(ProviderKindDatastore, store))
		proto.RegisterDatastoreProviderServer(srv, newDatastoreProviderServer(store))
	})
}
