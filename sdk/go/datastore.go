package gestalt

import proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"

// DatastoreProvider is implemented by providers that serve an IndexedDB-style
// datastore over gRPC.
type DatastoreProvider interface {
	Provider
	proto.IndexedDBServer
}
