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

// CacheClient speaks to a running cache provider over the unified host-service socket.
type CacheClient struct {
	client sdkcache.Client
}

var sharedCacheTransports sync.Map

// Cache connects to the cache provider exposed by gestaltd.
func Cache(name ...string) (*CacheClient, error) {
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
	return &CacheClient{client: rpccache.NewClient(client, rpccache.Options{})}, nil
}

func getSharedCacheTransport(binding string) *sharedManagerTransport[proto.CacheClient] {
	val, _ := sharedCacheTransports.LoadOrStore(binding, &sharedManagerTransport[proto.CacheClient]{})
	return val.(*sharedManagerTransport[proto.CacheClient])
}

// Close is a no-op because this client uses shared transport.
func (c *CacheClient) Close() error { return nil }

// Get loads one cached value.
func (c *CacheClient) Get(ctx context.Context, key string) ([]byte, bool, error) {
	return c.client.Get(ctx, key)
}

// GetMany loads all present values for keys.
func (c *CacheClient) GetMany(ctx context.Context, keys []string) (map[string][]byte, error) {
	return c.client.GetMany(ctx, keys)
}

// Set stores one value, replacing any existing entry at key.
func (c *CacheClient) Set(ctx context.Context, key string, value []byte, opts CacheSetOptions) error {
	return c.client.Set(ctx, key, value, opts)
}

// SetMany stores multiple entries in one RPC.
func (c *CacheClient) SetMany(ctx context.Context, entries []CacheEntry, opts CacheSetOptions) error {
	return c.client.SetMany(ctx, entries, opts)
}

// Delete removes one cached value and reports whether it existed.
func (c *CacheClient) Delete(ctx context.Context, key string) (bool, error) {
	return c.client.Delete(ctx, key)
}

// DeleteMany removes multiple cached values and reports how many were deleted.
func (c *CacheClient) DeleteMany(ctx context.Context, keys []string) (int64, error) {
	return c.client.DeleteMany(ctx, keys)
}

// Touch updates the TTL for one cached value.
func (c *CacheClient) Touch(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	return c.client.Touch(ctx, key, ttl)
}

func firstCacheName(name []string) string {
	if len(name) == 0 {
		return ""
	}
	return name[0]
}
