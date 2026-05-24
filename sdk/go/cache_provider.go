package gestalt

import sdkcache "github.com/valon-technologies/gestalt/sdk/go/cache"

// CacheProvider is implemented by providers that serve a cache over gRPC.
type CacheProvider interface {
	Provider
	sdkcache.Client
}
