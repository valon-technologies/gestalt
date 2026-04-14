package cache

import (
	"context"
	"time"
)

type Entry struct {
	Key   string
	Value []byte
}

type SetOptions struct {
	TTL time.Duration
}

// Cache is the portable cache interface every cache provider implements.
// Implementations must be safe for concurrent use.
type Cache interface {
	Get(ctx context.Context, key string) ([]byte, bool, error)
	GetMany(ctx context.Context, keys []string) (map[string][]byte, error)
	Set(ctx context.Context, key string, value []byte, opts SetOptions) error
	SetMany(ctx context.Context, entries []Entry, opts SetOptions) error
	Delete(ctx context.Context, key string) (bool, error)
	DeleteMany(ctx context.Context, keys []string) (int64, error)
	Touch(ctx context.Context, key string, ttl time.Duration) (bool, error)
	Ping(ctx context.Context) error
	Close() error
}
