package cache

import (
	"context"
	"fmt"
	"io"
	"time"

	sdkcache "github.com/valon-technologies/gestalt/sdk/go/cache"
	corecache "github.com/valon-technologies/gestalt/server/core/cache"
	rpccache "github.com/valon-technologies/gestalt/server/rpc/cache"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/egress"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
)

type ExecConfig struct {
	Command    string
	Args       []string
	Workdir    string
	Env        map[string]string
	Config     map[string]any
	Egress     egress.Policy
	HostBinary string
	Cleanup    func()
	Name       string
}

type remoteCache struct {
	client  sdkcache.Cache
	runtime proto.ProviderLifecycleClient
	closer  io.Closer
}

func NewExecutable(ctx context.Context, cfg ExecConfig) (corecache.Cache, error) {
	proc, err := runtimehost.StartAppProcess(ctx, runtimehost.ProcessConfig{
		Command:      cfg.Command,
		Args:         cfg.Args,
		Workdir:      cfg.Workdir,
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
	_, err = runtimehost.ConfigureRuntimeProvider(ctx, runtimeClient, proto.ProviderKind_PROVIDER_KIND_CACHE, cfg.Name, cfg.Config)
	if err != nil {
		_ = proc.Close()
		return nil, err
	}

	return &remoteCache{client: rpccache.NewConn(proc.Conn(), rpccache.Options{}), runtime: runtimeClient, closer: proc}, nil
}

func (r *remoteCache) Get(ctx context.Context, key string) ([]byte, bool, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()

	return r.client.Get(ctx, key)
}

func (r *remoteCache) GetMany(ctx context.Context, keys []string) (map[string][]byte, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()

	return r.client.GetMany(ctx, keys)
}

func (r *remoteCache) Set(ctx context.Context, key string, value []byte, opts corecache.SetOptions) error {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()

	return r.client.Set(ctx, key, value, sdkcache.SetOptions{TTL: opts.TTL})
}

func (r *remoteCache) SetMany(ctx context.Context, entries []corecache.Entry, opts corecache.SetOptions) error {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()

	cacheEntries := make([]sdkcache.Entry, 0, len(entries))
	for _, entry := range entries {
		cacheEntries = append(cacheEntries, sdkcache.Entry{
			Key:   entry.Key,
			Value: append([]byte(nil), entry.Value...),
		})
	}
	return r.client.SetMany(ctx, cacheEntries, sdkcache.SetOptions{TTL: opts.TTL})
}

func (r *remoteCache) Delete(ctx context.Context, key string) (bool, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()

	return r.client.Delete(ctx, key)
}

func (r *remoteCache) DeleteMany(ctx context.Context, keys []string) (int64, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()

	return r.client.DeleteMany(ctx, keys)
}

func (r *remoteCache) Touch(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()

	return r.client.Touch(ctx, key, ttl)
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
