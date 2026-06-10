package gestalt

import sdkcache "github.com/valon-technologies/gestalt/sdk/go/cache"

// CacheEntry is one key/value pair accepted by CacheProvider.SetMany.
type CacheEntry = sdkcache.Entry

// CacheSetOptions carries the optional TTL accepted by CacheProvider.Set and
// CacheProvider.SetMany.
type CacheSetOptions = sdkcache.SetOptions

// CacheProvider is implemented by providers that serve a cache over gRPC.
type CacheProvider interface {
	Provider
	sdkcache.Cache
}
