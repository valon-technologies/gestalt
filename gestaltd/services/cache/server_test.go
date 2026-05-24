package cache

import (
	"context"
	"testing"
	"time"

	corecache "github.com/valon-technologies/gestalt/server/core/cache"
	proto "github.com/valon-technologies/gestalt/sdk/go/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/grpc/metadata"
)

type testCache struct {
	values map[string][]byte
}

func newTestCache() *testCache {
	return &testCache{values: map[string][]byte{}}
}

func (c *testCache) Get(_ context.Context, key string) ([]byte, bool, error) {
	value, ok := c.values[key]
	return append([]byte(nil), value...), ok, nil
}

func (c *testCache) GetMany(_ context.Context, keys []string) (map[string][]byte, error) {
	values := make(map[string][]byte, len(keys))
	for _, key := range keys {
		if value, ok := c.values[key]; ok {
			values[key] = append([]byte(nil), value...)
		}
	}
	return values, nil
}

func (c *testCache) Set(_ context.Context, key string, value []byte, _ corecache.SetOptions) error {
	c.values[key] = append([]byte(nil), value...)
	return nil
}

func (c *testCache) SetMany(ctx context.Context, entries []corecache.Entry, opts corecache.SetOptions) error {
	for _, entry := range entries {
		if err := c.Set(ctx, entry.Key, entry.Value, opts); err != nil {
			return err
		}
	}
	return nil
}

func (c *testCache) Delete(_ context.Context, key string) (bool, error) {
	_, ok := c.values[key]
	delete(c.values, key)
	return ok, nil
}

func (c *testCache) DeleteMany(ctx context.Context, keys []string) (int64, error) {
	var deleted int64
	for _, key := range keys {
		ok, err := c.Delete(ctx, key)
		if err != nil {
			return 0, err
		}
		if ok {
			deleted++
		}
	}
	return deleted, nil
}

func (c *testCache) Touch(_ context.Context, key string, _ time.Duration) (bool, error) {
	_, ok := c.values[key]
	return ok, nil
}

func (c *testCache) Ping(context.Context) error { return nil }

func (c *testCache) Close() error { return nil }

func TestRoutingCacheServerRoutesByHostBindingMetadata(t *testing.T) {
	t.Parallel()

	main := newTestCache()
	archive := newTestCache()
	srv := NewRoutingServer(map[string]corecache.Cache{
		"main":    main,
		"archive": archive,
	}, "", "roadmap")
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(runtimehost.HostServiceBindingHeader, "archive"))

	if _, err := srv.Set(ctx, &proto.CacheSetRequest{Key: "flag", Value: []byte("ok")}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, ok := main.values["roadmap:flag"]; ok {
		t.Fatal("main cache received archive write")
	}
	if got := string(archive.values["roadmap:flag"]); got != "ok" {
		t.Fatalf("archive value = %q, want ok", got)
	}
}
