package gestalt

import (
	"context"
	"fmt"
	"sync"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/protov1/v1"
	"google.golang.org/protobuf/types/known/durationpb"
)

// CacheEntry is one key/value pair written through [CacheClient.SetMany].
type CacheEntry struct {
	Key   string
	Value []byte
}

// CacheSetOptions controls cache writes.
type CacheSetOptions struct {
	TTL time.Duration
}

// CacheClient speaks to a running cache provider over the unified host-service socket.
type CacheClient struct {
	client proto.CacheClient
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
	return &CacheClient{client: client}, nil
}

func getSharedCacheTransport(binding string) *sharedManagerTransport[proto.CacheClient] {
	val, _ := sharedCacheTransports.LoadOrStore(binding, &sharedManagerTransport[proto.CacheClient]{})
	return val.(*sharedManagerTransport[proto.CacheClient])
}

// Close is a no-op because this client uses shared transport.
func (c *CacheClient) Close() error { return nil }

// Get loads one cached value.
func (c *CacheClient) Get(ctx context.Context, key string) ([]byte, bool, error) {
	resp, err := c.client.Get(ctx, &proto.CacheGetRequest{Key: key})
	if err != nil {
		return nil, false, err
	}
	if !resp.GetFound() {
		return nil, false, nil
	}
	return append([]byte(nil), resp.GetValue()...), true, nil
}

// GetMany loads all present values for keys.
func (c *CacheClient) GetMany(ctx context.Context, keys []string) (map[string][]byte, error) {
	resp, err := c.client.GetMany(ctx, &proto.CacheGetManyRequest{Keys: keys})
	if err != nil {
		return nil, err
	}
	out := make(map[string][]byte, len(resp.GetEntries()))
	for _, entry := range resp.GetEntries() {
		if !entry.GetFound() {
			continue
		}
		out[entry.GetKey()] = append([]byte(nil), entry.GetValue()...)
	}
	return out, nil
}

// Set stores one value, replacing any existing entry at key.
func (c *CacheClient) Set(ctx context.Context, key string, value []byte, opts CacheSetOptions) error {
	_, err := c.client.Set(ctx, &proto.CacheSetRequest{
		Key:   key,
		Value: append([]byte(nil), value...),
		Ttl:   cacheTTLToProto(opts.TTL),
	})
	return err
}

// SetMany stores multiple entries in one RPC.
func (c *CacheClient) SetMany(ctx context.Context, entries []CacheEntry, opts CacheSetOptions) error {
	protoEntries := make([]*proto.CacheSetEntry, 0, len(entries))
	for _, entry := range entries {
		protoEntries = append(protoEntries, &proto.CacheSetEntry{
			Key:   entry.Key,
			Value: append([]byte(nil), entry.Value...),
		})
	}
	_, err := c.client.SetMany(ctx, &proto.CacheSetManyRequest{
		Entries: protoEntries,
		Ttl:     cacheTTLToProto(opts.TTL),
	})
	return err
}

// Delete removes one cached value and reports whether it existed.
func (c *CacheClient) Delete(ctx context.Context, key string) (bool, error) {
	resp, err := c.client.Delete(ctx, &proto.CacheDeleteRequest{Key: key})
	if err != nil {
		return false, err
	}
	return resp.GetDeleted(), nil
}

// DeleteMany removes multiple cached values and reports how many were deleted.
func (c *CacheClient) DeleteMany(ctx context.Context, keys []string) (int64, error) {
	resp, err := c.client.DeleteMany(ctx, &proto.CacheDeleteManyRequest{Keys: keys})
	if err != nil {
		return 0, err
	}
	return resp.GetDeleted(), nil
}

// Touch updates the TTL for one cached value.
func (c *CacheClient) Touch(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	resp, err := c.client.Touch(ctx, &proto.CacheTouchRequest{Key: key, Ttl: cacheTTLToProto(ttl)})
	if err != nil {
		return false, err
	}
	return resp.GetTouched(), nil
}

func cacheTTLToProto(ttl time.Duration) *durationpb.Duration {
	if ttl <= 0 {
		return nil
	}
	return durationpb.New(ttl)
}

func firstCacheName(name []string) string {
	if len(name) == 0 {
		return ""
	}
	return name[0]
}
