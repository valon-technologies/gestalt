package cache

import (
	"context"
	"time"
)

// Entry is one key/value pair written through Cache.SetMany.
type Entry struct {
	Key   string
	Value []byte
}

// SetOptions controls cache writes.
type SetOptions struct {
	TTL time.Duration
}

// Cache is the app-facing cache capability exposed by gestaltd and implemented
// by cache providers.
type Cache interface {
	Get(ctx context.Context, key string) ([]byte, bool, error)
	GetMany(ctx context.Context, keys []string) (map[string][]byte, error)
	Set(ctx context.Context, key string, value []byte, opts SetOptions) error
	SetMany(ctx context.Context, entries []Entry, opts SetOptions) error
	Delete(ctx context.Context, key string) (bool, error)
	DeleteMany(ctx context.Context, keys []string) (int64, error)
	Touch(ctx context.Context, key string, ttl time.Duration) (bool, error)
}
