package cache

import (
	"context"
	"time"

	sdkcache "github.com/valon-technologies/gestalt/sdk/go/cache"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

var _ sdkcache.Cache = (*rpcClient)(nil)

type rpcClient struct {
	grpc proto.CacheClient
	opts Options
}

// Close is a no-op because this client uses shared transport.
func (c *rpcClient) Close() error { return nil }

func (c *rpcClient) Get(ctx context.Context, key string) ([]byte, bool, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.grpc.Get(ctx, &proto.CacheGetRequest{Key: key})
	if err != nil {
		return nil, false, err
	}
	if !resp.GetFound() {
		return nil, false, nil
	}
	return append([]byte(nil), resp.GetValue()...), true, nil
}

func (c *rpcClient) GetMany(ctx context.Context, keys []string) (map[string][]byte, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.grpc.GetMany(ctx, &proto.CacheGetManyRequest{Keys: append([]string(nil), keys...)})
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

func (c *rpcClient) Set(ctx context.Context, key string, value []byte, opts sdkcache.SetOptions) error {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	_, err := c.grpc.Set(ctx, &proto.CacheSetRequest{
		Key:   key,
		Value: append([]byte(nil), value...),
		Ttl:   ttlToProto(opts.TTL),
	})
	return err
}

func (c *rpcClient) SetMany(ctx context.Context, entries []sdkcache.Entry, opts sdkcache.SetOptions) error {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	protoEntries := make([]*proto.CacheSetEntry, 0, len(entries))
	for _, entry := range entries {
		protoEntries = append(protoEntries, &proto.CacheSetEntry{
			Key:   entry.Key,
			Value: append([]byte(nil), entry.Value...),
		})
	}
	_, err := c.grpc.SetMany(ctx, &proto.CacheSetManyRequest{
		Entries: protoEntries,
		Ttl:     ttlToProto(opts.TTL),
	})
	return err
}

func (c *rpcClient) Delete(ctx context.Context, key string) (bool, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.grpc.Delete(ctx, &proto.CacheDeleteRequest{Key: key})
	if err != nil {
		return false, err
	}
	return resp.GetDeleted(), nil
}

func (c *rpcClient) DeleteMany(ctx context.Context, keys []string) (int64, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.grpc.DeleteMany(ctx, &proto.CacheDeleteManyRequest{Keys: append([]string(nil), keys...)})
	if err != nil {
		return 0, err
	}
	return resp.GetDeleted(), nil
}

func (c *rpcClient) Touch(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.grpc.Touch(ctx, &proto.CacheTouchRequest{Key: key, Ttl: ttlToProto(ttl)})
	if err != nil {
		return false, err
	}
	return resp.GetTouched(), nil
}
