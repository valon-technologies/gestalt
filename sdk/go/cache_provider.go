package gestalt

import proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"

// CacheProvider is implemented by providers that serve a cache over gRPC.
type CacheProvider interface {
	Provider
	proto.CacheServer
}
