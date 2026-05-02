package cache

import (
	"context"
	"fmt"
	"io"
	"time"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	corecache "github.com/valon-technologies/gestalt/server/core/cache"
	"github.com/valon-technologies/gestalt/server/services/egress"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
)

type ExecConfig struct {
	Command    string
	Args       []string
	Env        map[string]string
	Config     map[string]any
	Egress     egress.Policy
	HostBinary string
	Cleanup    func()
	Name       string
}

type remoteCache struct {
	client  proto.CacheClient
	runtime proto.ProviderLifecycleClient
	closer  io.Closer
}

func NewExecutable(ctx context.Context, cfg ExecConfig) (corecache.Cache, error) {
	proc, err := runtimehost.StartPluginProcess(ctx, runtimehost.ProcessConfig{
		Command:      cfg.Command,
		Args:         cfg.Args,
		Env:          cfg.Env,
		Egress:       cfg.Egress,
		HostBinary:   cfg.HostBinary,
		Cleanup:      cfg.Cleanup,
		ProviderName: cfg.Name,
	})
	if err != nil {
		return nil, err
	}

	runtimeClient := proc.Lifecycle()
	cacheClient := proto.NewCacheClient(proc.Conn())
	_, err = runtimehost.ConfigureRuntimeProvider(ctx, runtimeClient, proto.ProviderKind_PROVIDER_KIND_CACHE, cfg.Name, cfg.Config)
	if err != nil {
		_ = proc.Close()
		return nil, err
	}

	return &remoteCache{client: cacheClient, runtime: runtimeClient, closer: proc}, nil
}

func (r *remoteCache) Get(ctx context.Context, key string) ([]byte, bool, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()

	resp, err := r.client.Get(ctx, &proto.CacheGetRequest{Key: key})
	if err != nil {
		return nil, false, err
	}
	if !resp.GetFound() {
		return nil, false, nil
	}
	return append([]byte(nil), resp.GetValue()...), true, nil
}

func (r *remoteCache) GetMany(ctx context.Context, keys []string) (map[string][]byte, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()

	resp, err := r.client.GetMany(ctx, &proto.CacheGetManyRequest{Keys: keys})
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

func (r *remoteCache) Set(ctx context.Context, key string, value []byte, opts corecache.SetOptions) error {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()

	_, err := r.client.Set(ctx, &proto.CacheSetRequest{
		Key:   key,
		Value: append([]byte(nil), value...),
		Ttl:   ttlToProto(opts.TTL),
	})
	return err
}

func (r *remoteCache) SetMany(ctx context.Context, entries []corecache.Entry, opts corecache.SetOptions) error {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()

	protoEntries := make([]*proto.CacheSetEntry, 0, len(entries))
	for _, entry := range entries {
		protoEntries = append(protoEntries, &proto.CacheSetEntry{
			Key:   entry.Key,
			Value: append([]byte(nil), entry.Value...),
		})
	}
	_, err := r.client.SetMany(ctx, &proto.CacheSetManyRequest{
		Entries: protoEntries,
		Ttl:     ttlToProto(opts.TTL),
	})
	return err
}

func (r *remoteCache) Delete(ctx context.Context, key string) (bool, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()

	resp, err := r.client.Delete(ctx, &proto.CacheDeleteRequest{Key: key})
	if err != nil {
		return false, err
	}
	return resp.GetDeleted(), nil
}

func (r *remoteCache) DeleteMany(ctx context.Context, keys []string) (int64, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()

	resp, err := r.client.DeleteMany(ctx, &proto.CacheDeleteManyRequest{Keys: keys})
	if err != nil {
		return 0, err
	}
	return resp.GetDeleted(), nil
}

func (r *remoteCache) Touch(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()

	resp, err := r.client.Touch(ctx, &proto.CacheTouchRequest{Key: key, Ttl: ttlToProto(ttl)})
	if err != nil {
		return false, err
	}
	return resp.GetTouched(), nil
}

func (r *remoteCache) Ping(ctx context.Context) error {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	_, err := r.runtime.HealthCheck(ctx, &emptypb.Empty{})
	return err
}

func (r *remoteCache) Close() error {
	if r == nil || r.closer == nil {
		return nil
	}
	return r.closer.Close()
}

func ttlToProto(ttl time.Duration) *durationpb.Duration {
	if ttl <= 0 {
		return nil
	}
	return durationpb.New(ttl)
}

func ttlFromProto(ttl *durationpb.Duration) (time.Duration, error) {
	if ttl == nil {
		return 0, nil
	}
	d := ttl.AsDuration()
	if d < 0 {
		return 0, fmt.Errorf("ttl must be >= 0")
	}
	return d, nil
}

var _ corecache.Cache = (*remoteCache)(nil)
