package gestalt

import proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"

// IndexedDBProvider is implemented by providers that serve an IndexedDB-style
// datastore over gRPC.
type IndexedDBProvider interface {
	Provider
	proto.IndexedDBServer
}
