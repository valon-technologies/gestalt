package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
)

// ServeDatastoreProvider starts a gRPC server for a [DatastoreProvider].
// The provider must also implement one or more capability interfaces
// ([KeyValueDatastoreProvider], [SQLDatastoreProvider], [BlobStoreDatastoreProvider]).
func ServeDatastoreProvider(ctx context.Context, provider DatastoreProvider) error {
	return servePlugin(withPluginCloser(ctx, provider), func(srv *grpc.Server) {
		proto.RegisterResourceRuntimeServer(srv, newResourceRuntimeServer(provider))
		if kv, ok := provider.(KeyValueDatastoreProvider); ok {
			proto.RegisterKeyValueResourceServer(srv, newKVResourceServer(kv))
		}
		if sql, ok := provider.(SQLDatastoreProvider); ok {
			proto.RegisterSQLResourceServer(srv, newSQLResourceServer(sql))
		}
		if blob, ok := provider.(BlobStoreDatastoreProvider); ok {
			proto.RegisterBlobStoreResourceServer(srv, newBlobResourceServer(blob))
		}
	})
}
