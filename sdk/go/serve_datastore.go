package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
)

// ServeDatastoreProvider starts a gRPC server for a [DatastoreProvider].
func ServeDatastoreProvider(ctx context.Context, store DatastoreProvider) error {
	return servePlugin(withPluginCloser(ctx, store), func(srv *grpc.Server) {
		proto.RegisterPluginRuntimeServer(srv, NewRuntimeProviderServer(ProviderKindDatastore, store))
		proto.RegisterDatastorePluginServer(srv, NewDatastoreProviderServer(store))
	})
}
