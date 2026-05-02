package gestalt

import (
	"context"
	"time"
)

// CacheProvider is implemented by providers that serve a cache over gRPC.
type CacheProvider interface {
	Provider
	Get(ctx context.Context, key string) ([]byte, bool, error)
	GetMany(ctx context.Context, keys []string) (map[string][]byte, error)
	Set(ctx context.Context, key string, value []byte, opts CacheSetOptions) error
	SetMany(ctx context.Context, entries []CacheEntry, opts CacheSetOptions) error
	Delete(ctx context.Context, key string) (bool, error)
	DeleteMany(ctx context.Context, keys []string) (int64, error)
	Touch(ctx context.Context, key string, ttl time.Duration) (bool, error)
}
