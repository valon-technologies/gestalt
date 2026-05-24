package gestalt

import (
	"context"
	"fmt"
	"sync"
	"time"

	sdkcache "github.com/valon-technologies/gestalt/sdk/go/cache"
	rpccache "github.com/valon-technologies/gestalt/server/rpc/cache"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

type CacheEntry = sdkcache.Entry
type CacheSetOptions = sdkcache.SetOptions

var sharedCacheTransports sync.Map

// Cache connects to the cache provider exposed by gestaltd.
func Cache(name ...string) (sdkcache.Cache, error) {
	target, token, err := hostServiceTarget("cache")
	if err != nil {
		return nil, err
	}
	binding := firstCacheName(name)
	transport := getSharedCacheTransport(binding)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := hostServiceTransportClient(ctx, "cache", target, token, binding, transport, proto.NewCacheClient)
	if err != nil {
		return nil, fmt.Errorf("cache: connect to host: %w", err)
	}
	return rpccache.NewClient(client, rpccache.Options{}), nil
}

func getSharedCacheTransport(binding string) *sharedManagerTransport[proto.CacheClient] {
	val, _ := sharedCacheTransports.LoadOrStore(binding, &sharedManagerTransport[proto.CacheClient]{})
	return val.(*sharedManagerTransport[proto.CacheClient])
}

func firstCacheName(name []string) string {
	if len(name) == 0 {
		return ""
	}
	return name[0]
}
